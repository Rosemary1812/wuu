package appserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/subagent"
)

func (s *Server) forwardAgentNotifications(threadID string, control *agentcontrol.AgentControl, ch <-chan subagent.Notification, done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		case n, ok := <-ch:
			if !ok {
				return
			}
			if subagent.IsTerminal(n.Status) {
				// This is an idempotent fallback for controls that completed before
				// the reliable terminal finalizer was attached. In the normal path
				// AgentControl already ran the same finalizer synchronously while it
				// still owned the cross-process worker execution lease.
				if err := s.finalizeAgentTerminal(threadID, control, n); err != nil {
					providers.DebugLogf("finalize terminal agent %s fallback: %v", n.AgentID, err)
				}
				continue
			}
			now := time.Now().UTC()
			if n.Status == subagent.StatusRunning {
				_, turn, started := s.ensureLiveAgentThread(threadID, control, n.Snapshot, now)
				if started {
					_ = s.writeNotification(NotificationTurnStarted, TurnStartedNotification{
						ThreadID: n.Snapshot.ID,
						Turn:     turn,
					})
				}
				// Decision-five concurrency lock: a named agent is busy for
				// the lifetime of its run. This is the authoritative acquire
				// and the ONLY one for workflow-dispatched named runs, which
				// never pass through participant/start. It is idempotent with
				// the participant/start reservation (binds the run id).
				if pid := strings.TrimSpace(n.Snapshot.ParticipantID); pid != "" {
					s.markParticipantBusy(pid, "task", n.Snapshot.ID)
				}
			}
			_ = s.writeNotification(NotificationAgentUpdated, AgentUpdatedNotification{
				ThreadID: threadID,
				Agent:    s.agentFromSnapshot(control, n.Snapshot),
			})
		}
	}
}

type agentTerminalFinalizationKey struct {
	threadID    string
	agentID     string
	status      subagent.Status
	completedAt int64
}

const (
	participantRunCompletionMaxAttempts = 3
	participantRunCompletionRetryDelay  = 10 * time.Millisecond
)

type participantRunCompleter func(sessDir, participantID, agentID, outcome, summary string) error

func (s *Server) finalizeAgentTerminal(threadID string, control *agentcontrol.AgentControl, n subagent.Notification) error {
	return s.finalizeAgentTerminalWithCompleter(threadID, control, n, session.CompleteParticipantRun)
}

func (s *Server) finalizeAgentTerminalWithCompleter(threadID string, control *agentcontrol.AgentControl, n subagent.Notification, complete participantRunCompleter) error {
	if s == nil || !subagent.IsTerminal(n.Status) {
		return nil
	}
	agentID := strings.TrimSpace(n.Snapshot.ID)
	if agentID == "" {
		agentID = strings.TrimSpace(n.AgentID)
	}
	key := agentTerminalFinalizationKey{
		threadID:    strings.TrimSpace(threadID),
		agentID:     agentID,
		status:      n.Status,
		completedAt: n.Snapshot.CompletedAt.UnixNano(),
	}
	s.agentTerminalFinalizationMu.Lock()
	if s.agentTerminalFinalizations == nil {
		s.agentTerminalFinalizations = make(map[agentTerminalFinalizationKey]struct{})
	}
	if _, finalized := s.agentTerminalFinalizations[key]; finalized {
		s.agentTerminalFinalizationMu.Unlock()
		return nil
	}
	s.agentTerminalFinalizationMu.Unlock()

	participantID := strings.TrimSpace(n.Snapshot.ParticipantID)
	if participantID != "" {
		if agentID == "" {
			return errors.New("complete participant run: agent id is required")
		}
		summary := participantRunSummary(n.Snapshot.Result, n.Snapshot.Description)
		if n.Snapshot.Error != nil {
			summary = participantRunSummary(n.Snapshot.Error.Error(), n.Snapshot.Description)
		}
		if s.rt == nil || strings.TrimSpace(s.rt.SessionDir) == "" {
			return errors.New("complete participant run: session store is unavailable")
		}
		if complete == nil {
			return errors.New("complete participant run: completer is unavailable")
		}
		var err error
		for attempt := 1; attempt <= participantRunCompletionMaxAttempts; attempt++ {
			err = complete(
				s.rt.SessionDir,
				participantID,
				agentID,
				string(n.Status),
				summary,
			)
			if err == nil {
				break
			}
			if attempt == participantRunCompletionMaxAttempts || s.closed.Load() {
				return fmt.Errorf("complete participant run %s/%s after %d attempt(s): %w", participantID, agentID, attempt, err)
			}
			time.Sleep(participantRunCompletionRetryDelay)
		}
	}

	// Commit the in-memory dedupe only after the durable core succeeds. Two
	// concurrent replays may both issue the idempotent SQLite update, but only
	// the winner below is allowed to publish terminal side effects.
	s.agentTerminalFinalizationMu.Lock()
	if _, finalized := s.agentTerminalFinalizations[key]; finalized {
		s.agentTerminalFinalizationMu.Unlock()
		return nil
	}
	s.agentTerminalFinalizations[key] = struct{}{}
	s.agentTerminalFinalizationMu.Unlock()

	closed := s.closed.Load()
	if !closed {
		now := time.Now().UTC()
		_ = s.writeNotification(NotificationAgentUpdated, AgentUpdatedNotification{
			ThreadID: threadID,
			Agent:    s.agentFromSnapshot(control, n.Snapshot),
		})
		s.completeLiveAgentThread(threadID, control, n.Snapshot, now)
	}
	if participantID != "" {
		// Release the run's participant reservation and re-kick chat only
		// after the durable run record reflects the terminal outcome.
		s.releaseParticipantBusy(participantID, agentID)
		s.kickResidentAgent(participantID)
	}
	if closed {
		return nil
	}
	if n.Status == subagent.StatusCompleted &&
		control != nil &&
		control.ParticipantSpeechEnabled(n.Snapshot.ID) &&
		!control.ParticipantResponded(n.Snapshot.ID) {
		_, _ = control.PostParticipantMessage(context.Background(), n.Snapshot.ID, "decline", "没有需要占用主对话的回应。", "")
	}
	if s.isRootAgentSnapshot(control, threadID, n.Snapshot) {
		mailboxMessage := agentcontrol.NewAgentMailboxMessage(n.Snapshot)
		if control != nil {
			mailboxMessage = control.AgentMailboxMessage(n.Snapshot)
		}
		_ = s.writeNotification(NotificationAgentMailbox, AgentMailboxNotification{
			ThreadID: threadID,
			Message:  mailboxMessage,
		})
		if control != nil && !control.ParticipantSpeechEnabled(n.Snapshot.ID) {
			resultID := control.AgentResultDeliveryID(n.Snapshot)
			s.enqueueAgentCompletionTurn(threadID, n.Snapshot.ID, resultID, control.AgentCompletionChatMessage(n.Snapshot, agentthread.RootPath), &n.Snapshot)
		}
	}
	return nil
}

func (s *Server) forwardAgentStreamNotifications(threadID string, control *agentcontrol.AgentControl, ch <-chan subagent.StreamNotification, done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		case n, ok := <-ch:
			if !ok {
				return
			}
			now := time.Now().UTC()
			th, turn, started := s.ensureLiveAgentThread(threadID, control, n.Snapshot, now)
			if th == nil {
				continue
			}
			if started {
				_ = s.writeNotification(NotificationTurnStarted, TurnStartedNotification{
					ThreadID: th.ID,
					Turn:     turn,
				})
			}
			th.mu.Lock()
			batch := th.applyStreamEventLocked(turn.ID, n.Event, now)
			th.mu.Unlock()
			s.notifyOutboundBatch(batch)
			_ = s.writeNotification(NotificationTurnEvent, TurnEventNotification{
				ThreadID: th.ID,
				TurnID:   turn.ID,
				Event:    sanitizeStreamEvent(n.Event),
			})
		}
	}
}

func (s *Server) forwardParticipantMessages(threadID string, _ *agentcontrol.AgentControl, ch <-chan agentcontrol.ParticipantMessage, done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			participantID := strings.TrimSpace(msg.ParticipantID)
			speech, ok := s.residentParticipantSpeech(participantID).(residentParticipantSpeech)
			if !ok {
				providers.DebugLogf("resolve participant message target for thread %q: speech router unavailable", threadID)
				continue
			}
			targetThreadID, subthreadID, err := speech.resolveTargetThread(strings.TrimSpace(msg.ThreadID))
			if err != nil {
				providers.DebugLogf("resolve participant message target for thread %q: %v", threadID, err)
				continue
			}
			msg.ThreadID = subthreadID
			if err := s.publishParticipantMessage(targetThreadID, msg); err != nil {
				providers.DebugLogf("publish participant message for thread %q: %v", targetThreadID, err)
			}
		}
	}
}

// persistedImagesFromParticipant maps a human post's inline images onto the
// stored image shape, skipping entries with no data.
func persistedImagesFromParticipant(images []agentcontrol.ParticipantImage) []persistedImage {
	if len(images) == 0 {
		return nil
	}
	out := make([]persistedImage, 0, len(images))
	for _, image := range images {
		if strings.TrimSpace(image.Data) == "" {
			continue
		}
		out = append(out, persistedImage{
			MediaType: image.MediaType,
			Data:      image.Data,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// persistedFilesFromParticipant maps a human post's inline files onto the
// stored file shape, skipping entries with no data.
func persistedFilesFromParticipant(files []agentcontrol.ParticipantFile) []persistedFile {
	if len(files) == 0 {
		return nil
	}
	out := make([]persistedFile, 0, len(files))
	for _, file := range files {
		if strings.TrimSpace(file.Data) == "" {
			continue
		}
		out = append(out, persistedFile{
			MediaType: file.MediaType,
			Data:      file.Data,
			Filename:  file.Filename,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s *Server) publishParticipantMessage(threadID string, msg agentcontrol.ParticipantMessage) error {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return errors.New("thread_id is required")
	}
	var lifecycleLease *session.ThreadLifecycleLease
	if s != nil && s.rt != nil && strings.TrimSpace(s.rt.SessionDir) != "" {
		var err error
		lifecycleLease, err = s.acquireThreadLifecycleWriteLease(threadID)
		if err != nil {
			return err
		}
		defer releaseThreadLifecycleWriteLease(threadID, lifecycleLease)
	}
	now := msg.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	name := firstNonEmpty(msg.TaskName, msg.AgentType, msg.AgentID)
	if summary, ok := s.resolveParticipantSummary(msg.ParticipantID); ok && strings.TrimSpace(summary.Name) != "" {
		name = summary.Name
	}
	rec := persistedMessage{
		Role:          "participant",
		Content:       msg.Text,
		ClientID:      strings.TrimSpace(msg.AgentID),
		Name:          name,
		ParticipantID: msg.ParticipantID,
		PostKind:      msg.Kind,
		ThreadID:      msg.ThreadID,
		Images:        persistedImagesFromParticipant(msg.Images),
		Files:         persistedFilesFromParticipant(msg.Files),
		At:            now,
	}
	var sourceSeq int
	if strings.TrimSpace(s.rt.SessionDir) != "" {
		seq, err := session.AppendHistoryRecordReturningSeq(s.rt.SessionDir, threadID, session.HistoryRecord{
			Role:          rec.Role,
			Content:       rec.Content,
			ClientID:      rec.ClientID,
			Name:          rec.Name,
			ParticipantID: rec.ParticipantID,
			PostKind:      rec.PostKind,
			ThreadID:      rec.ThreadID,
			BasisSeq:      msg.BasisSeq,
			Images:        mustJSON(rec.Images),
			Files:         mustJSON(rec.Files),
			At:            rec.At,
		})
		if err != nil {
			return err
		}
		sourceSeq = seq
		if meta, ok, findErr := session.Find(s.rt.SessionDir, threadID); findErr == nil && ok {
			_ = session.UpdateIndex(s.rt.SessionDir, threadID, meta.Entries, "")
		}
	}
	if hook := s.afterLifecycleHistoryAppendForTest; hook != nil {
		hook(threadID)
	}
	if subthreadID := strings.TrimSpace(rec.ThreadID); subthreadID != "" {
		// Reply-subthread (cth-*) message: stored in the parent thread's history
		// tagged with thread_id=cth, kept OUT of the main stream (no turn append,
		// no thread notification, no full-roster fan-out). Weak isolation routes
		// it only to the subthread's participant subset — non-participants are
		// never woken and never take it into context; they can still pull it via
		// fetch_thread_messages. This replaces the old bare `return nil` that
		// dropped all subthread routing on the floor.
		s.routeSubthreadParticipantMessage(threadID, subthreadID, msg, name, sourceSeq)
		s.notifySubthreadUpdated(threadID, subthreadID)
		return nil
	}

	th := s.thread(threadID)
	if th == nil {
		return nil
	}
	rec.Seq = sourceSeq
	th.mu.Lock()
	turn, item, createdTurn := th.appendParticipantMessageLocked(rec, now, s.resolveParticipantSummary)
	thread := th.snapshotLocked()
	th.mu.Unlock()
	if createdTurn {
		_ = s.writeNotification(NotificationTurnStarted, TurnStartedNotification{
			ThreadID: threadID,
			Turn:     turn,
		})
	}
	_ = s.writeNotification(NotificationItemStarted, ItemStartedNotification{
		ThreadID:    threadID,
		TurnID:      turn.ID,
		Item:        item,
		StartedAtMS: now.UnixMilli(),
	})
	_ = s.writeNotification(NotificationItemCompleted, ItemCompletedNotification{
		ThreadID:      threadID,
		TurnID:        turn.ID,
		Item:          item,
		CompletedAtMS: now.UnixMilli(),
	})
	_ = s.notifyThreadUpdated(thread)
	s.routeParticipantMessageToResidents(th, msg, name, sourceSeq)
	return nil
}

// notifySubthreadUpdated pushes a subthread-scoped notification whenever a cth
// (reply-subthread) message is stored. cth traffic is short-circuited off the
// main stream and emits no turn/item/thread notification, so this is the sole
// live signal that keeps an open split reply panel streaming and the main-stream
// reply-count badge fresh (including agent replies, human posts, and task_card
// folds — they all flow through publishParticipantMessage's short-circuit).
//
// Best-effort: on a lookup/build error we still emit a minimal payload carrying
// only ThreadID/SubthreadID so the frontend can fall back to a nonce reload; the
// full embedded view lets it patch in place without a round-trip. ThreadID is
// the PARENT group thread id — the renderer's global-thread gate and reply-badge
// reload both key off it.
func (s *Server) notifySubthreadUpdated(parentThreadID, subthreadID string) {
	parentThreadID = strings.TrimSpace(parentThreadID)
	subthreadID = strings.TrimSpace(subthreadID)
	if parentThreadID == "" || subthreadID == "" {
		return
	}
	note := SubthreadUpdatedNotification{
		ThreadID:    parentThreadID,
		SubthreadID: subthreadID,
	}
	if thread, err := s.findConversationSubthread(parentThreadID, subthreadID, ""); err == nil {
		if view, viewErr := s.conversationSubthreadView(parentThreadID, thread, true); viewErr == nil {
			note.Subthread = view
		} else {
			providers.DebugLogf("build subthread view for %q/%q: %v", parentThreadID, subthreadID, viewErr)
		}
	} else {
		providers.DebugLogf("find subthread %q in %q: %v", subthreadID, parentThreadID, err)
	}
	_ = s.writeNotification(NotificationSubthreadUpdated, note)
}

func (s *Server) isRootAgentSnapshot(control *agentcontrol.AgentControl, threadID string, snap subagent.SubAgentSnapshot) bool {
	parentID := strings.TrimSpace(snap.ParentID)
	if parentID == "" {
		return true
	}
	if control != nil && parentID == control.SessionID() {
		return true
	}
	return parentID == strings.TrimSpace(threadID)
}

func isRunningSubAgentStatus(status subagent.Status) bool {
	switch status {
	case subagent.StatusRunning, subagent.StatusPending, subagent.StatusQueued:
		return true
	default:
		return false
	}
}

func (s *Server) ensureLiveAgentThread(rootThreadID string, control *agentcontrol.AgentControl, snap subagent.SubAgentSnapshot, now time.Time) (*threadState, Turn, bool) {
	if strings.TrimSpace(snap.ID) == "" {
		return nil, Turn{}, false
	}
	th, created := s.ensureAgentThreadState(rootThreadID, control, snap, now)
	if th == nil {
		return nil, Turn{}, false
	}
	th.mu.Lock()
	s.applyAgentSnapshotLocked(th, rootThreadID, snap, now)
	turn, started := th.startAgentTurnLocked(now)
	if created {
		started = true
	}
	th.mu.Unlock()
	return th, turn, started
}

func (s *Server) ensureAgentThreadState(rootThreadID string, control *agentcontrol.AgentControl, snap subagent.SubAgentSnapshot, now time.Time) (*threadState, bool) {
	if th := s.thread(snap.ID); th != nil {
		return th, false
	}

	history := s.agentSnapshotHistory(control, snap)
	th := newThreadState(snap.ID, history, s.rt.ProviderName, s.rt.Model, firstNonEmpty(snapCWD(rootThreadID, s), s.rt.RootDir), false, now)
	th.ParentID = strings.TrimSpace(rootThreadID)
	th.AgentPath = snap.AgentPath
	th.ReadOnly = true
	th.CreatedAt = firstNonZeroTime(snap.StartedAt, now)
	th.UpdatedAt = firstNonZeroTime(snap.ActivityAt, snap.CompletedAt, snap.StartedAt, now)

	s.mu.Lock()
	if existing := s.threads[snap.ID]; existing != nil {
		s.mu.Unlock()
		return existing, false
	}
	s.threads[snap.ID] = th
	s.mu.Unlock()
	return th, true
}

func (s *Server) agentSnapshotHistory(control *agentcontrol.AgentControl, snap subagent.SubAgentSnapshot) []providers.ChatMessage {
	if control != nil && control.Manager() != nil {
		if history, ok := control.Manager().History(snap.ID); ok && len(history) > 0 {
			return history
		}
	}
	history := make([]providers.ChatMessage, 0, 2)
	if strings.TrimSpace(snap.Description) != "" {
		history = append(history, providers.ChatMessage{Role: "user", Content: snap.Description})
	}
	if strings.TrimSpace(snap.Result) != "" {
		history = append(history, providers.ChatMessage{Role: "assistant", Content: snap.Result})
	} else if snap.Error != nil {
		history = append(history, providers.ChatMessage{Role: "assistant", Content: "Worker failed: " + snap.Error.Error()})
	}
	return history
}

func (s *Server) applyAgentSnapshotLocked(th *threadState, rootThreadID string, snap subagent.SubAgentSnapshot, now time.Time) {
	th.ParentID = strings.TrimSpace(rootThreadID)
	th.AgentPath = snap.AgentPath
	th.ReadOnly = true
	if !snap.StartedAt.IsZero() {
		th.CreatedAt = snap.StartedAt
	}
	switch snap.Status {
	case subagent.StatusRunning, subagent.StatusPending, subagent.StatusQueued:
		th.UpdatedAt = now
	case subagent.StatusCompleted, subagent.StatusFailed, subagent.StatusCancelled:
		th.UpdatedAt = firstNonZeroTime(snap.CompletedAt, now)
	default:
		th.UpdatedAt = now
	}
}

func (s *Server) completeLiveAgentThread(rootThreadID string, control *agentcontrol.AgentControl, snap subagent.SubAgentSnapshot, now time.Time) {
	th, turn, status, turnErr := s.syncFinalAgentThread(rootThreadID, control, snap, now)
	if th == nil {
		return
	}
	if status == TurnStatusFailed || status == TurnStatusInterrupted {
		structured := BuildTurnError(turnErr, th.ModelProvider)
		turn.Error = &structured
		th.mu.Lock()
		th.replaceTurnLocked(turn)
		th.mu.Unlock()
		_ = s.writeNotification(NotificationTurnError, TurnErrorNotification{
			ThreadID:   th.ID,
			TurnID:     turn.ID,
			Error:      structured.Message,
			Code:       structured.Code,
			Category:   string(structured.Category),
			Provider:   structured.Provider,
			StatusCode: structured.StatusCode,
			Turn:       turn,
		})
		return
	}
	_ = s.writeNotification(NotificationTurnCompleted, TurnCompletedNotification{
		ThreadID: th.ID,
		Turn:     turn,
		Content:  agentResultPreview(control, snap),
	})
}

func (s *Server) syncFinalAgentThread(rootThreadID string, control *agentcontrol.AgentControl, snap subagent.SubAgentSnapshot, now time.Time) (*threadState, Turn, TurnStatus, error) {
	th, _ := s.ensureAgentThreadState(rootThreadID, control, snap, now)
	if th == nil {
		return nil, Turn{}, "", nil
	}
	history := s.agentSnapshotHistory(control, snap)
	status, turnErr := turnStatusForSubAgentSnapshot(snap)

	th.mu.Lock()
	s.applyAgentSnapshotLocked(th, rootThreadID, snap, now)
	if len(history) > 0 {
		th.History = history
	}
	turnID := th.currentTurn
	if turnID == "" && len(th.Turns) > 0 {
		turnID = th.Turns[len(th.Turns)-1].ID
	}
	if turnID == "" {
		turn, _ := th.startAgentTurnLocked(now)
		turnID = turn.ID
	}
	turn := th.completeTurnLocked(turnID, status, turnErr, now, "", "", false)
	th.mu.Unlock()
	return th, turn, status, turnErr
}

func turnStatusForSubAgentSnapshot(snap subagent.SubAgentSnapshot) (TurnStatus, error) {
	switch snap.Status {
	case subagent.StatusFailed:
		if snap.Error != nil {
			return TurnStatusFailed, snap.Error
		}
		return TurnStatusFailed, errors.New("worker failed")
	case subagent.StatusCancelled:
		return TurnStatusInterrupted, context.Canceled
	default:
		return TurnStatusCompleted, nil
	}
}

func snapCWD(rootThreadID string, s *Server) string {
	if s == nil {
		return ""
	}
	if root := s.thread(rootThreadID); root != nil {
		root.mu.Lock()
		defer root.mu.Unlock()
		return root.CWD
	}
	return ""
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}

func (s *Server) agentFromSnapshot(control *agentcontrol.AgentControl, snap subagent.SubAgentSnapshot) Agent {
	ref := agentcontrol.AgentResultReference{}
	if control != nil {
		ref = control.AgentResultReference(snap)
	} else {
		preview, truncated := agentcontrol.AgentResultPreview(snap.Result)
		ref = agentcontrol.AgentResultReference{Preview: preview, Bytes: len([]byte(snap.Result)), Truncated: truncated}
	}
	out := Agent{
		ID:                  snap.ID,
		Type:                snap.Type,
		TaskName:            snap.TaskName,
		AgentProfile:        snap.AgentProfile,
		AgentPath:           snap.AgentPath,
		ParentID:            snap.ParentID,
		Description:         snap.Description,
		Status:              string(snap.Status),
		Result:              ref.Preview,
		ResultPath:          ref.Path,
		ResultBytes:         ref.Bytes,
		ResultTruncated:     ref.Truncated,
		InputTokens:         snap.InputTokens,
		OutputTokens:        snap.OutputTokens,
		CacheCreationTokens: snap.CacheCreationTokens,
		CacheReadTokens:     snap.CacheReadTokens,
		StartedAt:           snap.StartedAt,
		CompletedAt:         snap.CompletedAt,
		Participant:         s.participantSummaryForSnapshot(snap),
	}
	if snap.Error != nil {
		out.Error = snap.Error.Error()
	}
	return out
}

// participantSummaryForSnapshot resolves the participant identity for a
// sub-agent snapshot. Store hits are cached (ephemeral participants are
// immutable after creation). Any miss — empty ParticipantID on legacy
// snapshots, missing store row, or lookup error — falls back to a summary
// synthesized from the snapshot's type/task name so display attribution is
// never absent.
func (s *Server) participantSummaryForSnapshot(snap subagent.SubAgentSnapshot) *participant.Summary {
	if summary, ok := s.resolveParticipantSummary(snap.ParticipantID); ok {
		return &summary
	}
	fallback := participant.Summary{
		ID:   strings.TrimSpace(snap.ParticipantID),
		Name: participant.DeriveEphemeralName(snap.TaskName, snap.Type),
		Kind: string(participant.KindEphemeral),
		Role: snap.Type,
	}
	return &fallback
}

// resolveParticipantSummary looks up a participant summary by ID through
// the in-memory cache, falling back to the session store on miss.
func (s *Server) resolveParticipantSummary(id string) (participant.Summary, bool) {
	id = strings.TrimSpace(id)
	if id == "" || s == nil || s.rt == nil || strings.TrimSpace(s.rt.SessionDir) == "" {
		return participant.Summary{}, false
	}

	s.participantMu.Lock()
	summary, ok := s.participantSummaryCache[id]
	s.participantMu.Unlock()
	if ok {
		return summary, true
	}

	p, err := session.GetParticipant(s.rt.SessionDir, id)
	if err != nil {
		return participant.Summary{}, false
	}
	summary = p.Summary()
	// Avatar rides on the cached summary so the disk read happens once
	// per participant per cache generation (invalidateParticipantSummary
	// covers profile saves, retires, and roster edits). Capped — see
	// participantSummaryAvatarMaxBytes.
	summary.AvatarImage = participantSummaryAvatarDataURL(p.Workspace)

	s.participantMu.Lock()
	if s.participantSummaryCache == nil {
		s.participantSummaryCache = make(map[string]participant.Summary)
	}
	s.participantSummaryCache[id] = summary
	s.participantMu.Unlock()
	return summary, true
}

func agentResultPreview(control *agentcontrol.AgentControl, snap subagent.SubAgentSnapshot) string {
	if control == nil {
		preview, _ := agentcontrol.AgentResultPreview(snap.Result)
		return preview
	}
	return control.AgentResultReference(snap).Preview
}
