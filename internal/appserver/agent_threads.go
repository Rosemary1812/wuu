package appserver

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/subagent"
)

func (s *Server) forwardAgentNotifications(threadID string, control *agentcontrol.AgentControl, ch <-chan subagent.Notification) {
	for n := range ch {
		now := time.Now().UTC()
		if n.Status == subagent.StatusRunning {
			_, turn, started := s.ensureLiveAgentThread(threadID, control, n.Snapshot, now)
			if started {
				_ = s.writeNotification(NotificationTurnStarted, TurnStartedNotification{
					ThreadID: n.Snapshot.ID,
					Turn:     turn,
				})
			}
		}
		_ = s.writeNotification(NotificationAgentUpdated, AgentUpdatedNotification{
			ThreadID: threadID,
			Agent:    agentFromSnapshot(control, n.Snapshot),
		})
		switch n.Status {
		case subagent.StatusCompleted, subagent.StatusFailed, subagent.StatusCancelled:
			s.completeLiveAgentThread(threadID, control, n.Snapshot, now)
			if s.isRootAgentSnapshot(control, threadID, n.Snapshot) {
				mailboxMessage := agentcontrol.NewAgentMailboxMessage(n.Snapshot)
				if control != nil {
					mailboxMessage = control.AgentMailboxMessage(n.Snapshot)
				}
				_ = s.writeNotification(NotificationAgentMailbox, AgentMailboxNotification{
					ThreadID: threadID,
					Message:  mailboxMessage,
				})
				if control != nil {
					resultID := control.AgentResultDeliveryID(n.Snapshot)
					s.enqueueAgentCompletionTurn(threadID, n.Snapshot.ID, resultID, control.AgentCompletionChatMessage(n.Snapshot, agentthread.RootPath))
				}
			}
		}
	}
}

func (s *Server) forwardAgentStreamNotifications(threadID string, control *agentcontrol.AgentControl, ch <-chan subagent.StreamNotification) {
	for n := range ch {
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
		for _, item := range batch {
			_ = s.writeNotification(item.method, item.params)
		}
		_ = s.writeNotification(NotificationTurnEvent, TurnEventNotification{
			ThreadID: th.ID,
			TurnID:   turn.ID,
			Event:    sanitizeStreamEvent(n.Event),
		})
	}
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
	th := newThreadState(snap.ID, history, s.rt.ProviderName, s.rt.Model, firstNonEmpty(snapCWD(rootThreadID, s), s.rt.RootDir), "", now)
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
		message := ""
		if turnErr != nil {
			message = turnErr.Error()
		}
		_ = s.writeNotification(NotificationTurnError, TurnErrorNotification{
			ThreadID: th.ID,
			TurnID:   turn.ID,
			Error:    message,
			Turn:     turn,
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

func agentFromSnapshot(control *agentcontrol.AgentControl, snap subagent.SubAgentSnapshot) Agent {
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
	}
	if snap.Error != nil {
		out.Error = snap.Error.Error()
	}
	return out
}

func agentResultPreview(control *agentcontrol.AgentControl, snap subagent.SubAgentSnapshot) string {
	if control == nil {
		preview, _ := agentcontrol.AgentResultPreview(snap.Result)
		return preview
	}
	return control.AgentResultReference(snap).Preview
}
