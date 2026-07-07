package appserver

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/participant"
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

// handleThreadEscalateSub promotes a reply subthread to a task. This RPC is
// the HUMAN escalation path and the only one that can grant a lead (workflow
// orchestration authority stays a human act). Agents have their own
// conversion since 2026-07-06 — manage_task action=escalate — which performs
// the same open -> task transition but never assigns a lead (owner != lead,
// agent-task-rail design). It advances the subthread from the discussion
// state (open) to the execution state (task) and hangs a task_card off it;
// execution round-trips fold into the same cth via the existing
// thread_id-tagged post_message path, so no new thread is spawned.
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
	// Escalation grants both lead identity and workflow-orchestration authority in
	// one act. The lead is the named member the human picked (LeadParticipantID),
	// falling back to the escalator when that is itself a named agent. Escalation
	// is a human click, so CreatedBy is normally the human/empty and the picked
	// lead field is the load-bearing one; a non-named lead is rejected so the
	// runtime workflow gate never authorizes a phantom identity.
	lead, err := s.resolveTaskLead(params.LeadParticipantID, params.CreatedBy)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	thread, err := session.EscalateConversationThread(s.rt.SessionDir, subthreadID, params.CreatedBy, lead, params.Title)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	view, err := s.conversationSubthreadView(threadID, thread, true)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, ThreadEscalateSubResult{Subthread: view}, nil)
}

// resolveTaskLead determines the single named agent that becomes the task lead on
// escalation. A picked lead (leadParticipantID) wins but must be an active named
// participant — a human/unknown id is rejected so the runtime workflow gate cannot
// authorize a phantom identity. When no lead is picked, the escalator (createdBy)
// is used only if it is itself a named agent; otherwise the lead is left empty (no
// one holds orchestration authority until a later escalation names one). Returning
// empty is valid: it simply means the task has no workflow lead yet.
func (s *Server) resolveTaskLead(leadParticipantID, createdBy string) (string, error) {
	lead := strings.TrimSpace(leadParticipantID)
	if lead != "" {
		if !s.isNamedParticipant(lead) {
			return "", fmt.Errorf("task lead %q is not an active named participant", lead)
		}
		return lead, nil
	}
	if candidate := strings.TrimSpace(createdBy); candidate != "" && s.isNamedParticipant(candidate) {
		return candidate, nil
	}
	return "", nil
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

// handleThreadBubbleSub wraps a reply/task up: it bubbles a one-line conclusion
// back to the main stream (full-roster visible again) as a participant_message
// and marks the subthread resolved with that summary. The main-stream post
// reuses publishParticipantMessage with an empty ThreadID so it appends to the
// group stream and fans out to everyone — the opposite of the weak-isolation
// subthread short-circuit.
func (s *Server) handleThreadBubbleSub(req Request) error {
	var params ThreadBubbleSubParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	threadID := strings.TrimSpace(params.ThreadID)
	subthreadID := strings.TrimSpace(params.SubthreadID)
	summary := strings.TrimSpace(params.Summary)
	if threadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	if subthreadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("subthread_id is required"))
	}
	if summary == "" {
		return s.writeResponse(req.ID, nil, errors.New("summary is required"))
	}

	thread, err := s.findConversationSubthread(threadID, subthreadID, "")
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}

	// Bubble the conclusion to the main stream, attributed to the lead who
	// summarized (or the escalator/opener when unspecified). ThreadID is left
	// empty so this lands in the group's main stream, not back inside the cth.
	author := firstNonEmpty(strings.TrimSpace(params.ParticipantID), thread.EscalatedBy, thread.CreatedBy)
	thread, err = s.bubbleSubthreadResolve(threadID, thread, summary, author)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}

	view, err := s.conversationSubthreadView(threadID, thread, true)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, ThreadBubbleSubResult{Subthread: view}, nil)
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
	threadID := strings.TrimSpace(params.ThreadID)
	subthreadID := strings.TrimSpace(params.SubthreadID)
	anchorItemID := strings.TrimSpace(params.AnchorItemID)
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
	thread, err := session.CreateConversationThread(s.rt.SessionDir, session.ConversationThread{
		SessionID:    threadID,
		AnchorItemID: anchorItemID,
		Title:        params.Title,
		CreatedBy:    params.CreatedBy,
	})
	if err != nil {
		return session.ConversationThread{}, err
	}
	// Seed the weak-isolation member subset with the opener and any explicitly
	// requested participants. Each Add is best-effort: a non-named opener (e.g.
	// the human user, or an empty CreatedBy) fails requireActiveNamedParticipant
	// and is simply skipped, leaving the subset to grow as agents post in or get
	// @mentioned. Members grow further via routeSubthreadParticipantMessage.
	for _, participantID := range append([]string{params.CreatedBy}, params.Participants...) {
		participantID = strings.TrimSpace(participantID)
		if participantID == "" {
			continue
		}
		if err := session.AddConversationThreadMember(s.rt.SessionDir, thread.ID, participantID); err != nil {
			// Not fatal: seeding a non-member/non-named opener is expected to
			// fail. The subthread itself is already created.
			continue
		}
	}
	return thread, nil
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
		ID:           thread.ID,
		ThreadID:     thread.SessionID,
		AnchorItemID: thread.AnchorItemID,
		Title:        thread.Title,
		Status:       string(thread.Status),
		CreatedBy:    thread.CreatedBy,
		CreatedAt:    thread.CreatedAt,
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
	view.OwnerParticipantID = thread.OwnerParticipantID
	// A reply that has been escalated to a task (human click) carries a task_card:
	// running while it executes (status task), completed once it wraps up (status
	// resolved). A never-escalated reply leaves Task nil. EscalatedAt survives the
	// resolve transition, so a resolved task still renders its result summary.
	if !thread.EscalatedAt.IsZero() {
		status := "running"
		switch thread.Status {
		case session.ConversationThreadResolved:
			status = "completed"
		case session.ConversationThreadReview:
			// Owner filed for review; awaiting the human bubble click.
			status = "review"
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
