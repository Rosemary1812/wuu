package appserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/tools"
)

// residentTaskManager implements tools.TaskManager for one resident
// participant (the agent task rail, 2026-07-06 design). Like
// residentGroupManager, a fresh instance is wired per turn configuration.
// Every mutation notifies the parent thread's subthread listeners so an open
// board/reply panel stays live. There are deliberately no fallbacks: a caller
// outside the group, a missing anchor, or an invalid transition is a loud,
// specific error.
type residentTaskManager struct {
	server        *Server
	participantID string
}

func (s *Server) residentTaskManager(participantID string) *residentTaskManager {
	return &residentTaskManager{
		server:        s,
		participantID: strings.TrimSpace(participantID),
	}
}

func (m *residentTaskManager) CreateTask(ctx context.Context, threadID string, anchorSeq int, title string, claim bool) (tools.TaskView, error) {
	_ = ctx
	if err := m.ready("create"); err != nil {
		return tools.TaskView{}, err
	}
	threadID = strings.TrimSpace(threadID)
	title = strings.TrimSpace(title)
	if threadID == "" {
		return tools.TaskView{}, errors.New("create: thread_id is required")
	}
	if title == "" {
		return tools.TaskView{}, errors.New("create: title is required")
	}
	if err := m.requireGroupMembership("create", threadID); err != nil {
		return tools.TaskView{}, err
	}
	sessionDir := m.server.rt.SessionDir

	anchorItemID := ""
	if anchorSeq > 0 {
		resolved, err := m.server.mainStreamItemIDForSeq(threadID, anchorSeq)
		if err != nil {
			return tools.TaskView{}, fmt.Errorf("create: %w", err)
		}
		anchorItemID = resolved
		// One anchor hosts at most one cth (open-reply dedupe invariant) — a
		// second task on the same message must be standalone instead.
		if existing, err := m.server.findConversationSubthread(threadID, "", anchorItemID); err == nil {
			return tools.TaskView{}, fmt.Errorf("create: message seq %d already hosts reply/task %q; create a standalone task instead (omit anchor_seq)", anchorSeq, existing.ID)
		} else if !errors.Is(err, session.ErrConversationThreadNotFound) {
			return tools.TaskView{}, fmt.Errorf("create: %w", err)
		}
	}

	thread, err := session.CreateConversationThread(sessionDir, session.ConversationThread{
		SessionID:    threadID,
		AnchorItemID: anchorItemID,
		Title:        title,
		Status:       session.ConversationThreadTask,
		CreatedBy:    m.participantID,
	})
	if err != nil {
		return tools.TaskView{}, fmt.Errorf("create: %w", err)
	}
	// Stamp task provenance (escalated_at/escalated_by). The lead stays empty:
	// an agent-created task carries no workflow-orchestration grant
	// (owner != lead, user-adjudicated 2026-07-06).
	thread, err = session.EscalateConversationThread(sessionDir, thread.ID, m.participantID, "", "")
	if err != nil {
		return tools.TaskView{}, fmt.Errorf("create: stamp task provenance: %w", err)
	}
	// The creator follows the task it dispatched (completion signal); a
	// non-named creator cannot happen here (participantID is required).
	if err := session.AddConversationThreadMember(sessionDir, thread.ID, m.participantID); err != nil {
		return tools.TaskView{}, fmt.Errorf("create: follow created task: %w", err)
	}
	if claim {
		claimed, ok, err := session.ClaimConversationThread(sessionDir, thread.ID, m.participantID)
		if err != nil {
			return tools.TaskView{}, fmt.Errorf("create: self-claim: %w", err)
		}
		if !ok {
			return tools.TaskView{}, fmt.Errorf("create: self-claim lost to %q on a fresh task — this should be impossible", claimed.OwnerParticipantID)
		}
		thread = claimed
	}
	m.server.notifySubthreadUpdated(threadID, thread.ID)
	return m.taskView(thread), nil
}

// EscalateTask promotes an open discussion reply to a board task once the
// conversation in it has converged. The agent path deliberately grants no
// lead: EscalateConversationThread is called with an empty lead id, so the
// human-escalation contract (lead = orchestration authority, human-granted)
// is untouched; provenance (EscalatedBy) records the converting agent.
func (m *residentTaskManager) EscalateTask(ctx context.Context, subthreadID, title string, claim bool) (tools.TaskView, error) {
	_ = ctx
	if err := m.ready("escalate"); err != nil {
		return tools.TaskView{}, err
	}
	thread, err := m.memberTask("escalate", subthreadID)
	if err != nil {
		return tools.TaskView{}, err
	}
	if thread.Status != session.ConversationThreadOpen {
		return tools.TaskView{}, fmt.Errorf("escalate: reply %q is %q; only an open discussion reply can be converted to a task", thread.ID, thread.Status)
	}
	sessionDir := m.server.rt.SessionDir
	escalated, err := session.EscalateConversationThread(sessionDir, thread.ID, m.participantID, "", title)
	if err != nil {
		return tools.TaskView{}, fmt.Errorf("escalate: %w", err)
	}
	if err := session.AddConversationThreadMember(sessionDir, escalated.ID, m.participantID); err != nil {
		return tools.TaskView{}, fmt.Errorf("escalate: follow task: %w", err)
	}
	if claim {
		claimed, ok, err := session.ClaimConversationThread(sessionDir, escalated.ID, m.participantID)
		if err != nil {
			return tools.TaskView{}, fmt.Errorf("escalate: self-claim: %w", err)
		}
		if !ok {
			return tools.TaskView{}, fmt.Errorf("escalate: self-claim lost to %q immediately after escalation", claimed.OwnerParticipantID)
		}
		escalated = claimed
	}
	m.server.notifySubthreadUpdated(escalated.SessionID, escalated.ID)
	return m.taskView(escalated), nil
}

func (m *residentTaskManager) ClaimTask(ctx context.Context, subthreadID string) (tools.TaskView, bool, error) {
	_ = ctx
	if err := m.ready("claim"); err != nil {
		return tools.TaskView{}, false, err
	}
	thread, err := m.memberTask("claim", subthreadID)
	if err != nil {
		return tools.TaskView{}, false, err
	}
	claimed, ok, err := session.ClaimConversationThread(m.server.rt.SessionDir, thread.ID, m.participantID)
	if err != nil {
		return tools.TaskView{}, false, err
	}
	if ok {
		// The new owner follows its task's traffic.
		if err := session.AddConversationThreadMember(m.server.rt.SessionDir, claimed.ID, m.participantID); err != nil {
			return tools.TaskView{}, false, fmt.Errorf("claim: follow claimed task: %w", err)
		}
		m.server.notifySubthreadUpdated(claimed.SessionID, claimed.ID)
	}
	return m.taskView(claimed), ok, nil
}

func (m *residentTaskManager) UnclaimTask(ctx context.Context, subthreadID string) (tools.TaskView, error) {
	_ = ctx
	if err := m.ready("unclaim"); err != nil {
		return tools.TaskView{}, err
	}
	thread, err := m.memberTask("unclaim", subthreadID)
	if err != nil {
		return tools.TaskView{}, err
	}
	released, err := session.UnclaimConversationThread(m.server.rt.SessionDir, thread.ID, m.participantID)
	if err != nil {
		return tools.TaskView{}, err
	}
	m.server.notifySubthreadUpdated(released.SessionID, released.ID)
	return m.taskView(released), nil
}

func (m *residentTaskManager) FileTaskReview(ctx context.Context, subthreadID, summary string) (tools.TaskView, error) {
	_ = ctx
	if err := m.ready("update_status"); err != nil {
		return tools.TaskView{}, err
	}
	thread, err := m.memberTask("update_status", subthreadID)
	if err != nil {
		return tools.TaskView{}, err
	}
	reviewed, err := session.MarkConversationThreadReview(m.server.rt.SessionDir, thread.ID, m.participantID, summary)
	if err != nil {
		return tools.TaskView{}, err
	}
	// task_review: auto resolves immediately — the owner's summary bubbles to
	// the main stream under the owner's identity, same as the human click.
	if m.server.rt.TaskReviewMode == config.TaskReviewAuto {
		resolved, err := m.server.bubbleSubthreadResolve(reviewed.SessionID, reviewed, reviewed.Summary, m.participantID)
		if err != nil {
			return tools.TaskView{}, fmt.Errorf("update_status: auto-resolve bubble: %w", err)
		}
		reviewed = resolved
	}
	m.server.notifySubthreadUpdated(reviewed.SessionID, reviewed.ID)
	return m.taskView(reviewed), nil
}

func (m *residentTaskManager) UnfollowTask(ctx context.Context, subthreadID string) error {
	_ = ctx
	if err := m.ready("unfollow"); err != nil {
		return err
	}
	thread, err := m.memberTask("unfollow", subthreadID)
	if err != nil {
		return err
	}
	if err := session.RemoveConversationThreadMember(m.server.rt.SessionDir, thread.ID, m.participantID); err != nil {
		return fmt.Errorf("unfollow: %w", err)
	}
	m.server.notifySubthreadUpdated(thread.SessionID, thread.ID)
	return nil
}

func (m *residentTaskManager) ListTasks(ctx context.Context, threadID string) ([]tools.TaskView, error) {
	_ = ctx
	if err := m.ready("list"); err != nil {
		return nil, err
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, errors.New("list: thread_id is required")
	}
	if err := m.requireGroupMembership("list", threadID); err != nil {
		return nil, err
	}
	threads, err := session.ListConversationThreads(m.server.rt.SessionDir, threadID)
	if err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}
	views := make([]tools.TaskView, 0, len(threads))
	for _, thread := range threads {
		switch thread.Status {
		case session.ConversationThreadTask, session.ConversationThreadReview:
			views = append(views, m.taskView(thread))
		}
	}
	return views, nil
}

func (m *residentTaskManager) ready(action string) error {
	if m == nil || m.server == nil || m.server.rt == nil {
		return fmt.Errorf("%s: app server not configured", action)
	}
	if m.participantID == "" {
		return fmt.Errorf("%s: participant_id is required", action)
	}
	return nil
}

// memberTask loads a task subthread and verifies the caller is a member of
// its parent group thread. Task-rail actions address the cth directly, so the
// membership boundary is checked against the parent it hangs off of.
func (m *residentTaskManager) memberTask(action, subthreadID string) (session.ConversationThread, error) {
	subthreadID = strings.TrimSpace(subthreadID)
	if subthreadID == "" {
		return session.ConversationThread{}, fmt.Errorf("%s: subthread_id is required", action)
	}
	thread, err := session.FindConversationThreadByID(m.server.rt.SessionDir, subthreadID)
	if err != nil {
		return session.ConversationThread{}, fmt.Errorf("%s: %w", action, err)
	}
	if err := m.requireGroupMembership(action, thread.SessionID); err != nil {
		return session.ConversationThread{}, err
	}
	return thread, nil
}

func (m *residentTaskManager) requireGroupMembership(action, threadID string) error {
	sessionDir := m.server.rt.SessionDir
	meta, ok, err := session.Find(sessionDir, threadID)
	if err != nil {
		return fmt.Errorf("%s: lookup thread: %w", action, err)
	}
	if !ok {
		return fmt.Errorf("%s: %w: %q", action, session.ErrSessionNotFound, threadID)
	}
	if !meta.Group {
		return fmt.Errorf("%s: thread %q is not a group thread; the task rail lives in groups", action, threadID)
	}
	members, err := session.ListThreadMembers(sessionDir, threadID)
	if err != nil {
		return fmt.Errorf("%s: list thread members: %w", action, err)
	}
	for _, memberID := range members {
		if strings.TrimSpace(memberID) == m.participantID {
			return nil
		}
	}
	return fmt.Errorf("%s: participant %q is not a member of thread %q", action, m.participantID, threadID)
}

func (m *residentTaskManager) taskView(thread session.ConversationThread) tools.TaskView {
	view := tools.TaskView{
		ID:           thread.ID,
		ThreadID:     thread.SessionID,
		AnchorItemID: thread.AnchorItemID,
		Title:        thread.Title,
		Status:       string(thread.Status),
		Owner:        thread.OwnerParticipantID,
		CreatedBy:    thread.CreatedBy,
		Summary:      thread.Summary,
	}
	if view.Owner != "" {
		if summary, ok := m.server.resolveParticipantSummary(view.Owner); ok {
			view.OwnerName = summary.Name
		}
	}
	return view
}

// mainStreamItemIDForSeq resolves a message seq (the stable per-thread address
// agents see on envelopes and pass to react) to the main-stream item id the
// GUI anchors reply badges on. It reconstructs the main-stream turns with the
// exact logic the renderer consumes, so the anchor always matches a rendered
// item; a seq that is hidden, folded into a subthread, or nonexistent is an
// error rather than a blind write.
func (s *Server) mainStreamItemIDForSeq(threadID string, seq int) (string, error) {
	records, err := loadPersistedMessages(s.rt.SessionDir, threadID, false)
	if err != nil {
		return "", fmt.Errorf("load thread history: %w", err)
	}
	turns := turnsFromPersistedHistoryInScope(threadID, "", records, time.Now().UTC(), s.resolveParticipantSummary)
	for _, turn := range turns {
		for _, item := range turn.Items {
			if item.Seq == seq {
				return item.ID, nil
			}
		}
	}
	return "", fmt.Errorf("no visible main-stream message with seq %d in thread %q", seq, threadID)
}

// bubbleSubthreadResolve wraps a task up: the summary posts to the parent's
// main stream under author's identity and the subthread resolves with that
// summary stored. Shared by the human bubble click (handleThreadBubbleSub) and
// the task_review: auto path.
func (s *Server) bubbleSubthreadResolve(threadID string, thread session.ConversationThread, summary, author string) (session.ConversationThread, error) {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return session.ConversationThread{}, errors.New("bubble: summary is required")
	}
	if err := s.publishParticipantMessage(threadID, agentcontrol.ParticipantMessage{
		ParticipantID: author,
		Kind:          "result",
		Text:          summary,
	}); err != nil {
		return session.ConversationThread{}, err
	}
	if err := session.SetConversationThreadSummary(s.rt.SessionDir, thread.ID, summary); err != nil {
		return session.ConversationThread{}, err
	}
	if err := session.UpdateConversationThreadStatus(s.rt.SessionDir, thread.ID, session.ConversationThreadResolved); err != nil {
		return session.ConversationThread{}, err
	}
	thread.Status = session.ConversationThreadResolved
	thread.Summary = summary
	return thread, nil
}
