package appserver

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
)

func (s *Server) handleThreadListSub(req Request) error {
	var params ThreadListSubParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	threadID := strings.TrimSpace(params.ThreadID)
	if threadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	threads, err := session.ListConversationThreads(s.rt.SessionDir, threadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	views, err := s.conversationSubthreadViews(threadID, threads, false)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, ThreadListSubResult{Subthreads: views}, nil)
}

func (s *Server) handleThreadOpenSub(req Request) error {
	var params ThreadOpenSubParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	threadID := strings.TrimSpace(params.ThreadID)
	if threadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}

	thread, err := s.openConversationSubthread(params)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	view, err := s.conversationSubthreadView(threadID, thread, true)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, ThreadOpenSubResult{Subthread: view}, nil)
}

func (s *Server) handleThreadResolveSub(req Request) error {
	var params ThreadResolveSubParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	threadID := strings.TrimSpace(params.ThreadID)
	subthreadID := strings.TrimSpace(params.SubthreadID)
	if threadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	if subthreadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("subthread_id is required"))
	}

	thread, err := s.findConversationSubthread(threadID, subthreadID, "")
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if thread.Status == session.ConversationThreadTask {
		return s.writeResponse(req.ID, nil, errors.New("an active Task cannot be resolved through the generic Thread control; only its lead may conclude it"))
	}
	status := session.ConversationThreadOpen
	if params.Resolved {
		status = session.ConversationThreadResolved
	}
	if err := session.UpdateConversationThreadStatus(s.rt.SessionDir, thread.ID, status); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	thread.Status = status
	view, err := s.conversationSubthreadView(threadID, thread, true)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, ThreadResolveSubResult{Subthread: view}, nil)
}

// handleThreadEscalateSub promotes a reply subthread to a task. It advances the
// same cth from the discussion state (open) to the execution state (task),
// atomically making the persisted Thread owner its lead and entering planning;
// no caller-provided lead override exists. It hangs a task_card off the cth;
// execution round-trips fold into the same cth via the existing
// thread_id-tagged post_message path, so no new thread is spawned.
// Escalation IS the start of execution: the task enters exec state planning
// and the lead is woken to author the workflow plan right away — no further
// human approval sits between this click and the work.
func (s *Server) handleThreadEscalateSub(req Request) error {
	var params ThreadEscalateSubParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	threadID := strings.TrimSpace(params.ThreadID)
	subthreadID := strings.TrimSpace(params.SubthreadID)
	if threadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	if subthreadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("subthread_id is required"))
	}

	// Confirm the subthread belongs to this parent thread before mutating it.
	if _, err := s.findConversationSubthread(threadID, subthreadID, ""); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	thread, err := session.EscalateConversationThread(s.rt.SessionDir, subthreadID, humanReactionParticipantID, params.Title)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	s.recordTaskEventFor(thread, "", session.TaskEventTaskCreated, humanReactionParticipantID,
		fmt.Sprintf("reply escalated to task: %q", firstNonEmpty(strings.TrimSpace(thread.Title), "untitled")), "")
	// The lead follows the task it now orchestrates (best-effort, same as
	// set_plan does for assignees).
	if thread.LeadParticipantID != "" {
		if err := session.AddConversationThreadMember(s.rt.SessionDir, thread.ID, thread.LeadParticipantID); err != nil {
			providers.DebugLogf("escalate: add lead %q to task %q: %v", thread.LeadParticipantID, thread.ID, err)
		}
	}
	s.notifySubthreadUpdated(threadID, thread.ID)
	s.wakePlanLeadForPlanning(thread)
	view, err := s.conversationSubthreadView(threadID, thread, true)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, ThreadEscalateSubResult{Subthread: view}, nil)
}

// isNamedParticipant reports whether id resolves to a live KindNamed participant
// (the only kind eligible to lead a task / drive workflow orchestration).
func (s *Server) isNamedParticipant(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	p, err := session.GetParticipant(s.rt.SessionDir, id)
	if err != nil {
		return false
	}
	return p.Kind == participant.KindNamed && p.RetiredAt == nil
}

// handleThreadTaskEvents returns the trace timeline of an escalated subthread
// (plan §T11): the ordered task_events recorded while the task ran, so the
// panel can render the "轨迹" timeline. It is read-only. Like the other
// subthread handlers it first confirms the subthread belongs to the named
// parent thread (ownership check) before reading its trace, then maps the
// events onto the wire in their per-task seq order.
func (s *Server) handleThreadTaskEvents(req Request) error {
	var params ThreadTaskEventsParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	threadID := strings.TrimSpace(params.ThreadID)
	subthreadID := strings.TrimSpace(params.SubthreadID)
	if threadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	if subthreadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("subthread_id is required"))
	}
	if _, err := s.findConversationSubthread(threadID, subthreadID, ""); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	events, err := session.TaskEvents(s.rt.SessionDir, subthreadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	views := make([]TaskEventView, 0, len(events))
	for _, ev := range events {
		views = append(views, TaskEventView{
			Seq:       ev.Seq,
			NodeID:    ev.NodeID,
			AttemptID: ev.AttemptID,
			Kind:      ev.Kind,
			Actor:     ev.Actor,
			Summary:   ev.Summary,
			Payload:   ev.Payload,
			At:        ev.At,
		})
	}
	return s.writeResponse(req.ID, ThreadTaskEventsResult{Events: views}, nil)
}

// handleMessagePostSubthread posts a human-authored message into a reply
// subthread (群中群). The message folds into the cth via publishParticipantMessage's
// thread_id-tagged short-circuit: stored in the parent group's history tagged
// thread_id=cth, kept OUT of the main stream, and fanned out only to the cth's
// participant subset (weak isolation). The human is attributed to the stable
// "human" identity, the same one reactions use. Returns the refreshed subthread
// view so the split reply panel shows the just-sent message immediately (cth
// messages carry no item/thread notification of their own).
func (s *Server) handleMessagePostSubthread(req Request) error {
	var params MessagePostSubthreadParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	threadID := strings.TrimSpace(params.ThreadID)
	subthreadID := strings.TrimSpace(params.SubthreadID)
	text := strings.TrimSpace(params.Text)
	images := participantImagesFromTurnStart(params.Images)
	files := participantFilesFromTurnStart(params.Files)
	if threadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	if subthreadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("subthread_id is required"))
	}
	// A post needs either text or at least one attachment — the reused full
	// composer can send a screenshot with no caption.
	if text == "" && len(images) == 0 && len(files) == 0 {
		return s.writeResponse(req.ID, nil, errors.New("text or attachment is required"))
	}
	// The subthread must belong to this parent group thread before we post to it.
	thread, err := s.findConversationSubthread(threadID, subthreadID, "")
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if err := s.ensureHumanParticipant(); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if err := s.publishParticipantMessage(threadID, agentcontrol.ParticipantMessage{
		ParticipantID: humanReactionParticipantID,
		Kind:          "result",
		Text:          text,
		ThreadID:      subthreadID,
		CreatedAt:     time.Now().UTC(),
		Images:        images,
		Files:         files,
	}); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	view, err := s.conversationSubthreadView(threadID, thread, true)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, MessagePostSubthreadResult{Subthread: view}, nil)
}

// participantImagesFromTurnStart maps the wire image payload onto the
// attachment carrier on ParticipantMessage, skipping entries with no data.
func participantImagesFromTurnStart(images []TurnStartImage) []agentcontrol.ParticipantImage {
	if len(images) == 0 {
		return nil
	}
	out := make([]agentcontrol.ParticipantImage, 0, len(images))
	for _, image := range images {
		if strings.TrimSpace(image.Data) == "" {
			continue
		}
		out = append(out, agentcontrol.ParticipantImage{
			MediaType: image.MediaType,
			Data:      image.Data,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// participantFilesFromTurnStart maps the wire file payload onto the attachment
// carrier on ParticipantMessage, skipping entries with no data.
func participantFilesFromTurnStart(files []TurnStartFile) []agentcontrol.ParticipantFile {
	if len(files) == 0 {
		return nil
	}
	out := make([]agentcontrol.ParticipantFile, 0, len(files))
	for _, file := range files {
		if strings.TrimSpace(file.Data) == "" {
			continue
		}
		out = append(out, agentcontrol.ParticipantFile{
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

func (s *Server) openConversationSubthread(params ThreadOpenSubParams) (session.ConversationThread, error) {
	return s.openConversationSubthreadAs(params, humanReactionParticipantID)
}

func (s *Server) openConversationSubthreadAs(params ThreadOpenSubParams, createdBy string) (session.ConversationThread, error) {
	threadID := strings.TrimSpace(params.ThreadID)
	subthreadID := strings.TrimSpace(params.SubthreadID)
	anchorItemID := strings.TrimSpace(params.AnchorItemID)
	var parentItem ThreadItem
	if params.ParentSeq < 0 {
		return session.ConversationThread{}, errors.New("parent_seq must be positive")
	}
	if params.ParentSeq > 0 {
		var err error
		parentItem, err = s.mainStreamItemForSeq(threadID, params.ParentSeq)
		if err != nil {
			return session.ConversationThread{}, err
		}
		if parentItem.Type != ThreadItemUserMessage && parentItem.Type != ThreadItemParticipantMsg {
			return session.ConversationThread{}, fmt.Errorf("message seq %d cannot anchor a Thread", params.ParentSeq)
		}
		// Live turns and persisted reconstruction may use different transient item
		// ids. The durable seq chooses the canonical reconstructed id stored on the
		// Thread so badges still attach after restart.
		anchorItemID = parentItem.ID
	}
	if subthreadID != "" || anchorItemID != "" {
		if thread, err := s.findConversationSubthread(threadID, subthreadID, anchorItemID); err == nil {
			return thread, nil
		} else if subthreadID != "" || !errors.Is(err, session.ErrConversationThreadNotFound) {
			return session.ConversationThread{}, err
		}
	}
	if anchorItemID == "" {
		return session.ConversationThread{}, errors.New("anchor_item_id is required")
	}
	// One layer, no nesting: a message that already lives inside a reply
	// subthread cannot itself anchor a new reply. The frontend hides the entry
	// point; the backend refuses regardless so a stale/forged anchor can't nest.
	if parentSub, nested, err := s.anchorInsideSubthread(threadID, anchorItemID); err != nil {
		return session.ConversationThread{}, err
	} else if nested {
		return session.ConversationThread{}, fmt.Errorf("cannot open a reply on a message already inside reply thread %q", parentSub)
	}
	// Bind the Thread to a real parent message. An unresolved or forged rendered
	// item id cannot create a durable Thread because it would have no owner or
	// stable parent identity.
	parentSeq := params.ParentSeq
	parentAuthor := ""
	if parentSeq > 0 {
		parentAuthor = parentAuthorParticipantID(parentItem)
	} else {
		var ok bool
		parentSeq, parentAuthor, ok = s.mainStreamAnchorBinding(threadID, anchorItemID)
		if !ok {
			return session.ConversationThread{}, fmt.Errorf("anchor item %q does not resolve to a visible main-stream message", anchorItemID)
		}
	}
	if parentSeq <= 0 || strings.TrimSpace(parentAuthor) == "" {
		return session.ConversationThread{}, fmt.Errorf("anchor item %q has no durable parent identity", anchorItemID)
	}
	if parentItem.ID == "" {
		var itemErr error
		parentItem, itemErr = s.mainStreamItemForSeq(threadID, parentSeq)
		if itemErr != nil {
			return session.ConversationThread{}, itemErr
		}
	}
	threadTitle := strings.TrimSpace(params.Title)
	if threadTitle == "" {
		threadTitle = truncateUsageTitle(strings.Join(strings.Fields(parentItem.Text), " "))
	}
	owner, err := s.resolveConversationThreadOwner(threadID, parentAuthor, params.ThreadOwnerParticipantID)
	if err != nil {
		return session.ConversationThread{}, err
	}
	thread, err := session.CreateConversationThread(s.rt.SessionDir, session.ConversationThread{
		SessionID:                 threadID,
		AnchorItemID:              anchorItemID,
		Title:                     threadTitle,
		CreatedBy:                 strings.TrimSpace(createdBy),
		ThreadOwnerParticipantID:  owner,
		ParentSeq:                 parentSeq,
		ParentAuthorParticipantID: parentAuthor,
	})
	if err != nil {
		// Two windows may race the create-or-find path. The store owns anchor
		// uniqueness; when the other request won, return that durable Thread
		// instead of surfacing a transient duplicate-key error.
		if existing, findErr := s.findConversationSubthread(threadID, "", anchorItemID); findErr == nil {
			return existing, nil
		}
		return session.ConversationThread{}, err
	}
	// CreateConversationThread writes the owner into the focused member subset
	// in the same transaction. Other members join only through real group
	// routing; the open RPC has no cross-group participant injection surface.
	s.wakeConversationThreadOwner(thread, strings.TrimSpace(createdBy))
	return thread, nil
}

func (s *Server) wakeConversationThreadOwner(thread session.ConversationThread, openedBy string) {
	ownerID := strings.TrimSpace(thread.ThreadOwnerParticipantID)
	if s == nil || s.rt == nil || ownerID == "" || ownerID == strings.TrimSpace(openedBy) {
		return
	}
	title, workspace := s.taskThreadContext(thread.SessionID)
	text := fmt.Sprintf(
		"You own Thread %q, opened from a group message. Converge the focused discussion here (thread_id=%q). This is not Task execution. Only you may promote it when scope and direction are concrete; after promotion you remain Lead and orchestrate other named agents without doing worker pieces yourself.",
		firstNonEmpty(strings.TrimSpace(thread.Title), "untitled"), thread.ID,
	)
	s.deliverEnvelopeToMembers([]string{ownerID}, MessageEnvelope{
		SourceThreadID:      thread.SessionID,
		SourceSubthreadID:   thread.ID,
		SourceTitle:         title,
		SenderKind:          "system",
		SenderName:          "Thread workflow",
		SenderParticipantID: strings.TrimSpace(openedBy),
		Text:                text,
		CreatedAt:           time.Now().UTC(),
		Workspace:           workspace,
	}, nil, true)
}

// resolveConversationThreadOwner determines the named owner at Thread creation.
// A named parent author always owns the Thread and cannot be overridden. A
// human-authored parent requires an explicit active named member of the group.
func (s *Server) resolveConversationThreadOwner(threadID, parentAuthor, requestedOwner string) (string, error) {
	parentAuthor = strings.TrimSpace(parentAuthor)
	requestedOwner = strings.TrimSpace(requestedOwner)
	if parentAuthor != humanReactionParticipantID {
		if requestedOwner != "" && requestedOwner != parentAuthor {
			return "", fmt.Errorf("thread owner must be the named parent message author %q", parentAuthor)
		}
		if err := s.requireActiveNamedGroupMember(threadID, parentAuthor); err != nil {
			return "", fmt.Errorf("parent message author cannot own this Thread: %w", err)
		}
		return parentAuthor, nil
	}
	if requestedOwner == "" {
		return "", errors.New("thread_owner_participant_id is required for a human-authored parent message")
	}
	if err := s.requireActiveNamedGroupMember(threadID, requestedOwner); err != nil {
		return "", fmt.Errorf("thread owner: %w", err)
	}
	return requestedOwner, nil
}

func (s *Server) requireActiveNamedGroupMember(threadID, participantID string) error {
	participantID = strings.TrimSpace(participantID)
	if !s.isNamedParticipant(participantID) {
		return fmt.Errorf("%q is not an active named participant", participantID)
	}
	meta, ok, err := session.Find(s.rt.SessionDir, strings.TrimSpace(threadID))
	if err != nil {
		return fmt.Errorf("resolve group: %w", err)
	}
	if !ok || !meta.Group {
		return errors.New("parent thread is not a group")
	}
	members, err := session.ListThreadMembers(s.rt.SessionDir, threadID)
	if err != nil {
		return fmt.Errorf("list group members: %w", err)
	}
	for _, memberID := range members {
		if strings.TrimSpace(memberID) == participantID {
			return nil
		}
	}
	return fmt.Errorf("%q is not a member of group %q", participantID, threadID)
}

// anchorInsideSubthread reports whether anchorItemID addresses a message that is
// already rendered inside one of threadID's reply subthreads (a cth message). It
// returns that subthread's id when so. Subthread items are reconstructed with
// the same turnsFromConversationSubthreadHistory logic the reply panel renders
// from, so a cth-scoped anchor matches exactly; a main-stream anchor matches no
// subthread scope and is reported as not-nested (allowed).
func (s *Server) anchorInsideSubthread(threadID, anchorItemID string) (string, bool, error) {
	threadID = strings.TrimSpace(threadID)
	anchorItemID = strings.TrimSpace(anchorItemID)
	if anchorItemID == "" {
		return "", false, nil
	}
	threads, err := session.ListConversationThreads(s.rt.SessionDir, threadID)
	if err != nil {
		return "", false, err
	}
	if len(threads) == 0 {
		return "", false, nil
	}
	records, err := loadPersistedMessages(s.rt.SessionDir, threadID, false)
	if err != nil {
		return "", false, err
	}
	now := time.Now().UTC()
	for _, thread := range threads {
		turns := turnsFromConversationSubthreadHistory(threadID, thread.ID, records, now, s.resolveParticipantSummary)
		for _, turn := range turns {
			for _, item := range turn.Items {
				if item.ID == anchorItemID {
					return thread.ID, true, nil
				}
			}
		}
	}
	return "", false, nil
}

func (s *Server) findConversationSubthread(threadID, subthreadID, anchorItemID string) (session.ConversationThread, error) {
	threadID = strings.TrimSpace(threadID)
	subthreadID = strings.TrimSpace(subthreadID)
	anchorItemID = strings.TrimSpace(anchorItemID)
	threads, err := session.ListConversationThreads(s.rt.SessionDir, threadID)
	if err != nil {
		return session.ConversationThread{}, err
	}
	for _, thread := range threads {
		if subthreadID != "" && thread.ID == subthreadID {
			return thread, nil
		}
		if subthreadID == "" && anchorItemID != "" && thread.AnchorItemID == anchorItemID {
			return thread, nil
		}
	}
	key := firstNonEmpty(subthreadID, anchorItemID)
	return session.ConversationThread{}, fmt.Errorf("%w: %q", session.ErrConversationThreadNotFound, key)
}

func (s *Server) conversationSubthreadViews(threadID string, threads []session.ConversationThread, includeTurns bool) ([]ConversationSubthread, error) {
	records, err := loadPersistedMessages(s.rt.SessionDir, threadID, false)
	if err != nil {
		return nil, err
	}
	views := make([]ConversationSubthread, 0, len(threads))
	for _, thread := range threads {
		view := conversationSubthreadViewFromRecords(threadID, thread, records, includeTurns, s.resolveParticipantSummary)
		view.Participants = s.subthreadParticipants(thread.ID)
		views = append(views, view)
	}
	return views, nil
}

// subthreadParticipants returns the reply subthread's weak-isolation member
// subset for the shared ConversationSubthread.Participants contract (who the
// frontend renders as being "in" the reply). Best-effort: a load error yields an
// empty subset rather than failing the whole view.
func (s *Server) subthreadParticipants(subthreadID string) []string {
	members, err := session.ListConversationThreadMembers(s.rt.SessionDir, subthreadID)
	if err != nil {
		return nil
	}
	return members
}

func (s *Server) conversationSubthreadView(threadID string, thread session.ConversationThread, includeTurns bool) (ConversationSubthread, error) {
	records, err := loadPersistedMessages(s.rt.SessionDir, threadID, false)
	if err != nil {
		return ConversationSubthread{}, err
	}
	view := conversationSubthreadViewFromRecords(threadID, thread, records, includeTurns, s.resolveParticipantSummary)
	view.Participants = s.subthreadParticipants(thread.ID)
	return view, nil
}

func conversationSubthreadViewFromRecords(threadID string, thread session.ConversationThread, records []persistedMessage, includeTurns bool, resolve participantSummaryResolver) ConversationSubthread {
	view := ConversationSubthread{
		ID:                       thread.ID,
		ThreadID:                 thread.SessionID,
		AnchorItemID:             thread.AnchorItemID,
		ParentSeq:                thread.ParentSeq,
		Title:                    thread.Title,
		Status:                   string(thread.Status),
		CreatedBy:                thread.CreatedBy,
		CreatedAt:                thread.CreatedAt,
		ThreadOwnerParticipantID: thread.ThreadOwnerParticipantID,
	}
	subthreadID := strings.TrimSpace(thread.ID)
	for _, rec := range records {
		if strings.TrimSpace(rec.ThreadID) == subthreadID && !rec.Hidden {
			view.ReplyCount++
		}
	}
	view.Summary = thread.Summary
	view.EscalatedBy = thread.EscalatedBy
	view.LeadParticipantID = thread.LeadParticipantID
	view.ExecState = thread.ExecState
	// Project the plan onto the wire so the Task panel can render the progress
	// layer (plan §T11): one row per node with its Status-derived display State
	// and its two activity timestamps. deriveNodeState is the same
	// status->label mapping the tool surface uses, so the panel and the agent
	// see the same node vocabulary. Runtime state, not a desktop timeout, decides
	// whether work is blocked or needs attention.
	for _, p := range thread.Plan {
		view.Plan = append(view.Plan, TaskPieceView{
			ID:               p.ID,
			Title:            p.Title,
			Assignee:         p.Assignee,
			DependsOn:        p.DependsOn,
			Status:           p.Status,
			State:            deriveNodeState(p.Status),
			Attempts:         p.Attempts,
			RetryBudget:      p.RetryBudget,
			CurrentAttemptID: p.CurrentAttemptID,
			FailureReason:    p.FailureReason,
			LastActivityAt:   p.LastActivityAt,
			LastProgressAt:   p.LastProgressAt,
		})
	}
	// A reply that has been escalated to a task carries a task_card: running
	// while it executes (status task), completed once it wraps up (status
	// resolved). A never-escalated reply leaves Task nil. EscalatedAt survives
	// the resolve transition, so a resolved task still renders its result
	// summary. Rows persisted by the deleted review gate (status "review") are
	// deliberately NOT migrated: the raw status string passes through and any
	// unknown status renders as running here.
	if !thread.EscalatedAt.IsZero() {
		status := "running"
		if thread.Status == session.ConversationThreadResolved {
			status = "completed"
		}
		card := &TaskCard{
			ID:          thread.ID,
			Name:        firstNonEmpty(thread.Title, "Task"),
			Status:      status,
			SubthreadID: thread.ID,
			ReplyCount:  view.ReplyCount,
			Description: thread.Summary,
			StartedAt:   thread.EscalatedAt,
		}
		if resolve != nil {
			if summary, ok := resolve(thread.EscalatedBy); ok {
				s := summary
				card.Participant = &s
			}
		}
		view.Task = card
	}
	if includeTurns {
		view.Turns = turnsFromConversationSubthreadHistory(threadID, thread.ID, records, time.Now().UTC(), resolve)
	}
	return view
}
