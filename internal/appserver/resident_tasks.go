package appserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/providers"
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

func (m *residentTaskManager) CreateTask(ctx context.Context, threadID string, anchorSeq int, title string, claim bool, ackCollisionID string) (tools.TaskView, error) {
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
	meta, err := m.boardThreadMeta("create", threadID)
	if err != nil {
		return tools.TaskView{}, err
	}
	// DM tasks are born owned (task-rail design §7): the DM has exactly one
	// possible executor — the resident agent itself — so the claim parameter
	// is semantically always true and the claim race cannot exist.
	if !meta.Group {
		claim = true
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

	// Standalone collision check (issue #4 v3): when anchorSeq == 0 (no
	// message to anchor the dedup on), a same-titled unfinished task in the
	// same thread would silently spawn a duplicate card. The user spec
	// calls for a hard-block that lets the creator decide (claim it OR
	// persist with ack_collision_id citing the existing task id).
	//
	// ack_collision_id is a STRICT id-match escape hatch — it must equal
	// the existing task's id, NOT a generic ack or a made-up value. The
	// strict-matching rule is the 5th-test red line (`TestStandaloneCreateAckCollisionMismatchRefuses`
	// in standalone_create_dedup_test.go): "ack" cannot degrade into a
	// generic force bypass, because that would defeat the dedup entirely.
	// Anchored tasks skip this check entirely — one anchor, one cth,
	// period.
	//
	// The collision set is deliberately narrowed to status == task: that is
	// the only state that means "unfinished and claimable". A same-titled
	// task in review has been delivered (awaiting the human gate) and a
	// resolved one is history — re-creating either title is a legitimate
	// redo, not a duplicate. Open discussion replies never collide (they are
	// not tasks yet). Anchored cths that escalated to task carry the same
	// status and are caught by the same filter, so the dedup surface covers
	// anchored and standalone same-title collisions alike.
	if anchorSeq == 0 {
		if existing := findUnfinishedTaskByNormalizedTitle(m.server.rt.SessionDir, threadID, normalizeTitleForDedup(title)); existing != nil {
			if ackCollisionID != existing.ID {
				return tools.TaskView{}, fmt.Errorf("create: title %q already in use by open task %q in this group; claim it with manage_task action=claim, or pass ack_collision_id=%q to create a duplicate", title, existing.ID, existing.ID)
			}
			// ack_collision_id matched the existing task's id: caller has
			// explicitly ack'd this collision. Fall through to write the
			// duplicate cth (work-splitting same-topic case).
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
	m.server.wakeTaskBoardForOwnerlessTask(thread, m.participantID, "opened on the board")
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
	thread, meta, err := m.memberTask("escalate", subthreadID)
	if err != nil {
		return tools.TaskView{}, err
	}
	if thread.Status != session.ConversationThreadOpen {
		return tools.TaskView{}, fmt.Errorf("escalate: reply %q is %q; only an open discussion reply can be converted to a task", thread.ID, thread.Status)
	}
	// DM tasks are born owned (task-rail design §7) — same forcing as create.
	if !meta.Group {
		claim = true
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
	m.server.wakeTaskBoardForOwnerlessTask(escalated, m.participantID, "opened on the board")
	return m.taskView(escalated), nil
}

func (m *residentTaskManager) ClaimTask(ctx context.Context, subthreadID string) (tools.TaskView, bool, error) {
	_ = ctx
	if err := m.ready("claim"); err != nil {
		return tools.TaskView{}, false, err
	}
	thread, _, err := m.memberTask("claim", subthreadID)
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
	thread, meta, err := m.memberTask("unclaim", subthreadID)
	if err != nil {
		return tools.TaskView{}, err
	}
	// A DM task has exactly one possible executor, so releasing ownership is
	// meaningless — the board would hold a task nobody else can ever claim
	// (task-rail design §7). Refuse loudly instead of stranding it.
	if !meta.Group {
		return tools.TaskView{}, fmt.Errorf("unclaim: task %q lives in a DM; a DM task has exactly one possible owner and cannot be released — finish it (update_status) or leave it as is", thread.ID)
	}
	released, err := session.UnclaimConversationThread(m.server.rt.SessionDir, thread.ID, m.participantID)
	if err != nil {
		return tools.TaskView{}, err
	}
	m.server.notifySubthreadUpdated(released.SessionID, released.ID)
	m.server.wakeTaskBoardForOwnerlessTask(released, m.participantID, "released back to the board")
	return m.taskView(released), nil
}

func (m *residentTaskManager) FileTaskReview(ctx context.Context, subthreadID, summary string) (tools.TaskView, error) {
	_ = ctx
	if err := m.ready("update_status"); err != nil {
		return tools.TaskView{}, err
	}
	thread, _, err := m.memberTask("update_status", subthreadID)
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
	thread, _, err := m.memberTask("unfollow", subthreadID)
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
	if _, err := m.boardThreadMeta("list", threadID); err != nil {
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

// memberTask loads a task subthread and verifies the caller may act on its
// parent thread's board. Task-rail actions address the cth directly, so the
// access boundary is checked against the parent it hangs off of.
func (m *residentTaskManager) memberTask(action, subthreadID string) (session.ConversationThread, session.Session, error) {
	subthreadID = strings.TrimSpace(subthreadID)
	if subthreadID == "" {
		return session.ConversationThread{}, session.Session{}, fmt.Errorf("%s: subthread_id is required", action)
	}
	thread, err := session.FindConversationThreadByID(m.server.rt.SessionDir, subthreadID)
	if err != nil {
		return session.ConversationThread{}, session.Session{}, fmt.Errorf("%s: %w", action, err)
	}
	meta, err := m.boardThreadMeta(action, thread.SessionID)
	if err != nil {
		return session.ConversationThread{}, session.Session{}, err
	}
	return thread, meta, nil
}

// boardThreadMeta gates task-rail access and hands back the parent thread's
// meta so callers can branch on group vs DM semantics (task-rail design §7,
// 2026-07-07): a group's board is open to its members; a DM's board is open
// to exactly the DM's own resident agent. Anything else — someone else's DM,
// a non-chat work session — is a loud refusal.
func (m *residentTaskManager) boardThreadMeta(action, threadID string) (session.Session, error) {
	sessionDir := m.server.rt.SessionDir
	meta, ok, err := session.Find(sessionDir, threadID)
	if err != nil {
		return session.Session{}, fmt.Errorf("%s: lookup thread: %w", action, err)
	}
	if !ok {
		return session.Session{}, fmt.Errorf("%s: %w: %q", action, session.ErrSessionNotFound, threadID)
	}
	if dm := strings.TrimSpace(meta.DMParticipantID); dm != "" {
		if dm == m.participantID {
			return meta, nil
		}
		return session.Session{}, fmt.Errorf("%s: thread %q is another agent's DM; only its resident agent may use this board", action, threadID)
	}
	if !meta.Group {
		return session.Session{}, fmt.Errorf("%s: thread %q is neither a group nor a DM chat; the task rail lives in chat threads", action, threadID)
	}
	members, err := session.ListThreadMembers(sessionDir, threadID)
	if err != nil {
		return session.Session{}, fmt.Errorf("%s: list thread members: %w", action, err)
	}
	for _, memberID := range members {
		if strings.TrimSpace(memberID) == m.participantID {
			return meta, nil
		}
	}
	return session.Session{}, fmt.Errorf("%s: participant %q is not a member of thread %q", action, m.participantID, threadID)
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

// wakeTaskBoardForOwnerlessTask fans a lightweight from="system" envelope to
// the group's members (minus the acting participant) whenever a task lands on
// the board with no owner — born-open create, born-open escalate, unclaim
// (task-rail design §6: 无主任务唤醒). Without it, task mutations only touch
// the store and the GUI panel: a task put on the board while no resident is
// mid-turn would sit unclaimed forever. The envelope carries no seq (no read
// receipt — it is a board event, not a chat message) and is never addressed,
// so the contract's claim-or-silence rule governs the woken members. A task
// that is born owned (claim=true) wakes nobody: there is nothing for the
// others to do. Callers pass the task unconditionally; owned tasks no-op here.
func (s *Server) wakeTaskBoardForOwnerlessTask(task session.ConversationThread, actorID, event string) {
	if s == nil || s.rt == nil {
		return
	}
	threadID := strings.TrimSpace(task.SessionID)
	if threadID == "" || strings.TrimSpace(task.OwnerParticipantID) != "" {
		return
	}
	// DM tasks are born owned so this is normally unreachable for them; the
	// explicit guard keeps the invariant local (task-rail design §7: DMs have
	// no group members to call, the wake is a group-only mechanism).
	if meta, ok, err := session.Find(s.rt.SessionDir, threadID); err != nil || !ok || !meta.Group {
		return
	}
	actorID = strings.TrimSpace(actorID)
	actorName := actorID
	if summary, ok := s.resolveParticipantSummary(actorID); ok && strings.TrimSpace(summary.Name) != "" {
		actorName = summary.Name
	}
	title, workspace := threadID, ""
	if th := s.thread(threadID); th != nil {
		th.mu.Lock()
		title = residentEnvelopeSourceTitleLocked(th)
		workspace = th.FocusWorkspace
		th.mu.Unlock()
	}
	members, err := session.ListThreadMembers(s.rt.SessionDir, threadID)
	if err != nil {
		providers.DebugLogf("list thread members for task-board wake %q: %v", threadID, err)
		return
	}
	text := fmt.Sprintf(
		"Task %q was %s by %s and has no owner. If this work is yours to take, claim it now (manage_task action=claim subthread_id=%q); otherwise — or if the claim fails — end your turn without posting.",
		firstNonEmpty(strings.TrimSpace(task.Title), "untitled"), event, actorName, task.ID,
	)
	s.deliverEnvelopeToMembers(members, MessageEnvelope{
		SourceThreadID:      threadID,
		SourceSubthreadID:   task.ID,
		SourceTitle:         title,
		SenderKind:          "system",
		SenderName:          "task board",
		SenderParticipantID: actorID,
		Text:                text,
		CreatedAt:           time.Now().UTC(),
		Workspace:           workspace,
	}, nil)
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

// normalizeTitleForDedup trims, collapses whitespace runs, and lowercases
// so that titles like "Fix  Login Flake" and "fix login flake" collide on
// issue #4's check. SQLite's default collation is BINARY, so this is the
// layer that resolves case-insensitive matching — the SQL filter alone
// would miss it. Used by findUnfinishedTaskByNormalizedTitle below; callers
// should normalize once per request and reuse the result.
func normalizeTitleForDedup(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(s)), " "))
}

// findUnfinishedTaskByNormalizedTitle scans the thread's cth rows via
// session.ListConversationThreads and returns the first row whose title
// (after normalizeTitleForDedup) equals target and whose status is
// ConversationThreadTask. Returns nil when nothing matches.
//
// Why Task-status only: status == task is the only "unfinished and
// claimable" state, which is what a duplicate would pollute. A standalone
// task is BORN as task (conversation_thread.go creation constraint) but
// still flows to review/resolved like any other; those states mean the
// work was delivered or closed, so re-creating the title is a legitimate
// redo and must not be blocked. Open discussion replies are not tasks and
// never collide.
//
// Implemented as a one-thread in-Go scan rather than a new session
// helper because per-thread cth counts stay small (< ~100), and this
// matches the anchor dedup pattern at L54-68 in spirit: "no new SQL
// helper for the symmetric case".
func findUnfinishedTaskByNormalizedTitle(sessDir, threadID, normalizedTitle string) *session.ConversationThread {
	threads, err := session.ListConversationThreads(sessDir, threadID)
	if err != nil {
		providers.DebugLogf("list conversation threads for standalone dedup %q: %v", threadID, err)
		return nil
	}
	for i, t := range threads {
		// Status filter narrowed to ConversationThreadTask only (issue #4
		// v3): see the dedup block comment in CreateTask for the
		// rationale (L89-93 standalone constraint pins this single value).
		if t.Status == session.ConversationThreadTask &&
			normalizeTitleForDedup(t.Title) == normalizedTitle {
			return &threads[i]
		}
	}
	return nil
}
