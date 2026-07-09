package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
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
	parentSeq := 0
	parentAuthor := ""
	if anchorSeq > 0 {
		anchorItem, err := m.server.mainStreamItemForSeq(threadID, anchorSeq)
		if err != nil {
			return tools.TaskView{}, fmt.Errorf("create: %w", err)
		}
		anchorItemID = anchorItem.ID
		// Bind the task to the message it was created from: its seq and author
		// (T3). The seq is the anchor seq the caller passed; the author is read
		// off the resolved item.
		parentSeq = anchorSeq
		parentAuthor = parentAuthorParticipantID(anchorItem)
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
	// the only state that means "unfinished and claimable". A resolved task
	// is history — re-creating its title is a legitimate redo, not a
	// duplicate. Open discussion replies never collide (they are not tasks
	// yet). Anchored cths that escalated to task carry the same status and
	// are caught by the same filter, so the dedup surface covers anchored
	// and standalone same-title collisions alike.
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
		SessionID:                 threadID,
		AnchorItemID:              anchorItemID,
		Title:                     title,
		Status:                    session.ConversationThreadTask,
		CreatedBy:                 m.participantID,
		ParentSeq:                 parentSeq,
		ParentAuthorParticipantID: parentAuthor,
	})
	if err != nil {
		return tools.TaskView{}, fmt.Errorf("create: %w", err)
	}
	// Stamp task provenance (escalated_at/escalated_by). The lead stays empty:
	// an agent-created task grants no lead identity (owner != lead,
	// user-adjudicated 2026-07-06).
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
	m.server.recordTaskEventFor(thread, "", session.TaskEventTaskCreated, m.participantID,
		fmt.Sprintf("task created: %q", firstNonEmpty(title, "untitled")), "")
	m.server.notifySubthreadUpdated(threadID, thread.ID)
	m.server.wakeTaskBoardForOwnerlessTask(thread, m.participantID, "opened on the board")
	return m.taskView(thread), nil
}

// EscalateTask promotes an open discussion reply to a board task once the
// conversation in it has converged. The lead defaults to the reply's parent
// message author (T3) — the person whose message spawned the discussion leads
// the task it became — when that author is a named agent; a human/unknown
// parent author leaves the lead empty for the first planner to claim (T6).
// Provenance (EscalatedBy) still records the converting agent. Escalating a
// reply on your own message is refused. Escalation IS the start of execution —
// no human approval step follows: the task enters exec state planning
// immediately, and the lead (when one is bound) is woken to author the plan.
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
	// You cannot converge a task on your own message: escalating a reply whose
	// parent message you authored would make you both the subject and the lead
	// of the work (T3). Refuse loudly.
	parentAuthor := strings.TrimSpace(thread.ParentAuthorParticipantID)
	if parentAuthor != "" && parentAuthor == m.participantID {
		return tools.TaskView{}, fmt.Errorf("escalate: cannot open a reply thread on your own message (reply %q anchors your message)", thread.ID)
	}
	// DM tasks are born owned (task-rail design §7) — same forcing as create.
	if !meta.Group {
		claim = true
	}
	sessionDir := m.server.rt.SessionDir
	// Lead defaults to the parent message's author (T3): the person whose
	// message spawned the discussion leads the task it converged into, when they
	// are a named agent. A human/unknown parent author leaves the lead empty —
	// the first planner claims it (T6). resolveTaskLead never errors on an empty
	// picked lead, so the fallback is safe to swallow.
	lead, _ := m.server.resolveTaskLead("", parentAuthor)
	escalated, err := session.EscalateConversationThread(sessionDir, thread.ID, m.participantID, lead, title)
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
	if err := session.SetConversationThreadExecState(sessionDir, escalated.ID, session.ExecStatePlanning); err != nil {
		return tools.TaskView{}, fmt.Errorf("escalate: enter planning: %w", err)
	}
	escalated.ExecState = session.ExecStatePlanning
	m.server.recordTaskEventFor(escalated, "", session.TaskEventTaskCreated, m.participantID,
		fmt.Sprintf("reply escalated to task: %q", firstNonEmpty(strings.TrimSpace(escalated.Title), "untitled")), "")
	m.server.notifySubthreadUpdated(escalated.SessionID, escalated.ID)
	m.server.wakeTaskBoardForOwnerlessTask(escalated, m.participantID, "opened on the board")
	m.server.wakePlanLeadForPlanning(escalated)
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

// ConcludeTask files the task's conclusion and completes it in one act (the
// manage_task update_status action): a single store CAS resolves the task —
// owner OR lead may conclude, so an unclaimed plan-task is concluded by its
// lead — then the summary bubbles to the parent main stream under the
// caller's identity. Filing IS the completion report; there is no review
// gate and no human approval step.
func (m *residentTaskManager) ConcludeTask(ctx context.Context, subthreadID, summary string) (tools.TaskView, error) {
	_ = ctx
	if err := m.ready("update_status"); err != nil {
		return tools.TaskView{}, err
	}
	thread, _, err := m.memberTask("update_status", subthreadID)
	if err != nil {
		return tools.TaskView{}, err
	}
	concluded, err := session.ConcludeConversationThread(m.server.rt.SessionDir, thread.ID, m.participantID, summary)
	if err != nil {
		return tools.TaskView{}, err
	}
	// The conclusion posts to the parent's main stream (empty ThreadID =
	// full-roster visible), same publish path the human bubble click uses.
	if err := m.server.publishParticipantMessage(concluded.SessionID, agentcontrol.ParticipantMessage{
		ParticipantID: m.participantID,
		Kind:          "result",
		Text:          concluded.Summary,
	}); err != nil {
		return tools.TaskView{}, fmt.Errorf("update_status: bubble conclusion: %w", err)
	}
	// Execution is over. Best-effort: the resolved status is the durable
	// completion signal; a failed exec-state write must not undo it.
	if err := session.SetConversationThreadExecState(m.server.rt.SessionDir, concluded.ID, session.ExecStateCompleted); err != nil {
		providers.DebugLogf("update_status: mark exec completed %q: %v", concluded.ID, err)
	} else {
		concluded.ExecState = session.ExecStateCompleted
	}
	m.server.recordTaskEventFor(concluded, "", session.TaskEventTaskCompleted, m.participantID,
		concluded.Summary, "")
	m.server.notifySubthreadUpdated(concluded.SessionID, concluded.ID)
	return m.taskView(concluded), nil
}

// NeedHuman flags a task as waiting on a decision that genuinely belongs to
// the human. It is the explicit human-decision point of the execution state
// machine — never a default gate: exec state needs_human plus a blocked
// trace event carrying the reason. It wakes nobody; the human decides from
// the board.
func (m *residentTaskManager) NeedHuman(ctx context.Context, subthreadID, reason string) (tools.TaskView, error) {
	_ = ctx
	if err := m.ready("need_human"); err != nil {
		return tools.TaskView{}, err
	}
	thread, _, err := m.memberTask("need_human", subthreadID)
	if err != nil {
		return tools.TaskView{}, err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return tools.TaskView{}, errors.New("need_human: reason is required")
	}
	if err := session.SetConversationThreadExecState(m.server.rt.SessionDir, thread.ID, session.ExecStateNeedsHuman); err != nil {
		return tools.TaskView{}, fmt.Errorf("need_human: %w", err)
	}
	thread.ExecState = session.ExecStateNeedsHuman
	payload, err := json.Marshal(map[string]string{"reason": reason})
	if err != nil {
		payload = nil
	}
	m.server.recordTaskEventFor(thread, "", session.TaskEventBlocked, m.participantID,
		fmt.Sprintf("flagged for the human: %s", reason), string(payload))
	m.server.notifySubthreadUpdated(thread.SessionID, thread.ID)
	return m.taskView(thread), nil
}

// NeedUpstream is the assignee-driven fallback (plan §T8): a node discovers the
// handoff its upstream gave it is insufficient and bounces the work back rather
// than silently working around a bad input or rewriting the plan (that is the
// lead's alone). The caller must be the downstream piece's assignee and be
// actively running it; the piece must actually depend on an upstream to fall
// back to. The downstream node is parked back to pending (it re-runs once the
// refreshed upstream re-files), every upstream is re-activated and its assignee
// woken with a targeted directive naming what was missing, and the fallback is
// traced (a blocked event on the downstream, a retrying event on each upstream).
// The engine is not advanced here — the downstream waits on the upstream's fresh
// piece_done, which advancePlan then acts on.
func (m *residentTaskManager) NeedUpstream(ctx context.Context, subthreadID, pieceID, reason string) (tools.TaskView, error) {
	_ = ctx
	if err := m.ready("need_upstream"); err != nil {
		return tools.TaskView{}, err
	}
	thread, _, err := m.memberTask("need_upstream", subthreadID)
	if err != nil {
		return tools.TaskView{}, err
	}
	pieceID = strings.TrimSpace(pieceID)
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return tools.TaskView{}, errors.New("need_upstream: reason is required (what the upstream handoff is missing)")
	}
	var piece *session.TaskPiece
	for i := range thread.Plan {
		if thread.Plan[i].ID == pieceID {
			piece = &thread.Plan[i]
			break
		}
	}
	if piece == nil {
		return tools.TaskView{}, fmt.Errorf("need_upstream: task %q has no piece %q", thread.ID, pieceID)
	}
	if piece.Assignee != m.participantID {
		return tools.TaskView{}, fmt.Errorf("need_upstream: piece %q is assigned to %q, not you — only its assignee may bounce work back to the upstream", pieceID, piece.Assignee)
	}
	if piece.Status != session.TaskPieceActive && piece.Status != session.TaskPieceRetrying {
		return tools.TaskView{}, fmt.Errorf("need_upstream: piece %q is %q, not active — only a node you are actively running can report its upstream insufficient", pieceID, piece.Status)
	}
	if len(piece.DependsOn) == 0 {
		return tools.TaskView{}, fmt.Errorf("need_upstream: piece %q depends on nothing — there is no upstream node to fall back to", pieceID)
	}
	downstreamTitle := piece.Title
	upstreamIDs := make([]string, 0, len(piece.DependsOn))
	for _, dep := range piece.DependsOn {
		if dep = strings.TrimSpace(dep); dep != "" {
			upstreamIDs = append(upstreamIDs, dep)
		}
	}
	// Park the downstream back to pending: it re-runs once a refreshed upstream
	// re-files piece_done, which advancePlan will act on.
	if _, err := session.UpdateTaskPiece(m.server.rt.SessionDir, thread.ID, pieceID, func(p *session.TaskPiece) {
		p.Status = session.TaskPiecePending
		p.FailureReason = reason
		p.LastActivityAt = time.Now().UTC()
	}); err != nil {
		return tools.TaskView{}, fmt.Errorf("need_upstream: park downstream: %w", err)
	}
	payload, err := json.Marshal(map[string]any{"reason": reason, "upstreams": upstreamIDs})
	if err != nil {
		payload = nil
	}
	m.server.recordTaskEventFor(thread, pieceID, session.TaskEventBlocked, m.participantID,
		fmt.Sprintf("bounced to upstream: %s", truncate(reason, 80)), string(payload))
	// Re-activate every upstream and wake its assignee with targeted feedback.
	for _, upID := range upstreamIDs {
		reactivated, err := session.UpdateTaskPiece(m.server.rt.SessionDir, thread.ID, upID, func(p *session.TaskPiece) {
			p.Status = session.TaskPieceActive
			if p.Attempts < 1 {
				p.Attempts = 1
			}
			p.LastActivityAt = time.Now().UTC()
		})
		if err != nil {
			providers.DebugLogf("need_upstream reactivate %q/%q: %v", thread.ID, upID, err)
			continue
		}
		up := findPieceByID(reactivated.Plan, upID)
		if up == nil {
			continue
		}
		// The upstream re-follows the task so it sees the wrap-up traffic again
		// (piece_done auto-unfollowed it when it first finished).
		if err := session.AddConversationThreadMember(m.server.rt.SessionDir, thread.ID, up.Assignee); err != nil {
			providers.DebugLogf("need_upstream re-follow %q to %q: %v", up.Assignee, thread.ID, err)
		}
		m.server.recordTaskEventFor(reactivated, upID, session.TaskEventRetrying, up.Assignee,
			fmt.Sprintf("re-opened to fix handoff for %q", downstreamTitle), "")
		m.server.wakeUpstreamForFallback(reactivated, *up, downstreamTitle, reason)
	}
	m.server.notifySubthreadUpdated(thread.SessionID, thread.ID)
	final, err := session.FindConversationThreadByID(m.server.rt.SessionDir, thread.ID)
	if err != nil {
		return tools.TaskView{}, err
	}
	return m.taskView(final), nil
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
		if thread.Status == session.ConversationThreadTask {
			views = append(views, m.taskView(thread))
		}
	}
	return views, nil
}

// SetPlan declares the lead's team work breakdown on a task and immediately
// advances the plan (task-rail design §8). Assignees are pulled into the task
// thread's team so they follow it. The plan is the durable declaration the
// engine executes — the lead authors it once, then steps out of the loop.
func (m *residentTaskManager) SetPlan(ctx context.Context, subthreadID string, pieces []tools.TaskPiece) (tools.TaskView, error) {
	_ = ctx
	if err := m.ready("set_plan"); err != nil {
		return tools.TaskView{}, err
	}
	thread, _, err := m.memberTask("set_plan", subthreadID)
	if err != nil {
		return tools.TaskView{}, err
	}
	// Planning is only for a live task: a resolved task whose lead survived the
	// resolve must not be silently re-planned and re-executed — that needs a
	// fresh escalation (status back to task), not a stray set_plan.
	if thread.Status != session.ConversationThreadTask {
		return tools.TaskView{}, fmt.Errorf("set_plan: task %q is %q, not an active task; only a live task can be planned", thread.ID, thread.Status)
	}
	// Plan authority = the lead's orchestration right (plan §T6): the lead is
	// the single orchestrator, and only the lead may declare or revise the plan
	// (a replan is just another set_plan). A human-escalated task is born with
	// its lead; an agent-created standalone task is born leadless, so the first
	// board member to plan it atomically takes the lead here (CAS). This gate
	// runs before any validation or write so a non-lead's set_plan cannot even
	// touch the plan.
	lead := strings.TrimSpace(thread.LeadParticipantID)
	if lead == "" {
		claimed, becameLead, err := session.SetConversationThreadLeadIfEmpty(m.server.rt.SessionDir, thread.ID, m.participantID)
		if err != nil {
			return tools.TaskView{}, fmt.Errorf("set_plan: claim orchestration lead: %w", err)
		}
		thread = claimed
		lead = strings.TrimSpace(claimed.LeadParticipantID)
		if becameLead {
			m.server.recordTaskEventFor(claimed, "", session.TaskEventLeadInvoked, m.participantID,
				"claimed orchestration lead", "")
		}
		// If becameLead is false someone raced in first; lead now holds their
		// id and the ownership check below refuses this caller.
	}
	if m.participantID != lead {
		return tools.TaskView{}, fmt.Errorf("set_plan: task %q is led by %q; only its lead may declare or revise the plan", thread.ID, lead)
	}
	if len(pieces) == 0 {
		return tools.TaskView{}, errors.New("set_plan: plan is required")
	}
	ids := map[string]bool{}
	sessionPieces := make([]session.TaskPiece, 0, len(pieces))
	for _, p := range pieces {
		id := strings.TrimSpace(p.ID)
		if id == "" || strings.TrimSpace(p.Title) == "" || strings.TrimSpace(p.Assignee) == "" {
			return tools.TaskView{}, errors.New("set_plan: every piece needs id, title, and assignee")
		}
		if ids[id] {
			return tools.TaskView{}, fmt.Errorf("set_plan: duplicate piece id %q", id)
		}
		// Backward-only dependencies: a piece may depend only on pieces declared
		// EARLIER in the plan (already in ids). The plan's own order is then a
		// topological order by construction, so a dependency cycle, a self-loop,
		// or a forward/unknown reference cannot be expressed at all — the engine
		// never has to detect a cycle because a cycle cannot be named. This mirrors
		// how, in a program, control-flow order IS the dependency.
		for _, dep := range p.DependsOn {
			d := strings.TrimSpace(dep)
			if d == id {
				return tools.TaskView{}, fmt.Errorf("set_plan: piece %q cannot depend on itself", id)
			}
			if !ids[d] {
				return tools.TaskView{}, fmt.Errorf("set_plan: piece %q depends on %q, which is not declared before it; a piece may depend only on pieces listed earlier in the plan", id, d)
			}
		}
		ids[id] = true
		// Prompt is the one node field the lead authors here; handoff,
		// attempts, and retry budget stay engine-owned.
		sessionPieces = append(sessionPieces, session.TaskPiece{
			ID: id, Title: strings.TrimSpace(p.Title), Assignee: strings.TrimSpace(p.Assignee),
			DependsOn: p.DependsOn, Status: session.TaskPiecePending,
			Prompt: strings.TrimSpace(p.Prompt),
		})
	}
	// Pull assignees onto the task thread's team so the board card shows them
	// and they follow the task's traffic.
	for _, p := range sessionPieces {
		if err := session.AddConversationThreadMember(m.server.rt.SessionDir, thread.ID, p.Assignee); err != nil {
			providers.DebugLogf("set_plan: add assignee %q to task %q: %v", p.Assignee, thread.ID, err)
		}
	}
	updated, err := session.SetConversationThreadPlan(m.server.rt.SessionDir, thread.ID, sessionPieces)
	if err != nil {
		return tools.TaskView{}, fmt.Errorf("set_plan: %w", err)
	}
	// The declared plan starts running now — planning is over, no approval
	// step sits between the plan and its first dispatch.
	if err := session.SetConversationThreadExecState(m.server.rt.SessionDir, updated.ID, session.ExecStateExecuting); err != nil {
		return tools.TaskView{}, fmt.Errorf("set_plan: enter executing: %w", err)
	}
	updated.ExecState = session.ExecStateExecuting
	m.server.recordTaskEventFor(updated, "", session.TaskEventWorkflowPlanned, m.participantID,
		fmt.Sprintf("lead declared a %d-piece workflow plan", len(sessionPieces)), planTracePayload(sessionPieces))
	m.server.notifySubthreadUpdated(updated.SessionID, updated.ID)
	m.server.advancePlan(updated.ID)
	final, err := session.FindConversationThreadByID(m.server.rt.SessionDir, thread.ID)
	if err != nil {
		return tools.TaskView{}, err
	}
	return m.taskView(final), nil
}

// PieceDone marks the caller's plan piece complete, records the structured
// handoff it produced, and advances the plan. Only the piece's assignee may
// report it done — the durable plan, not chat chatter, is what the engine reads
// to decide who runs next. It is the EARLY / RICH completion path: a node also
// completes when its dispatched turn ends (autoCompleteTaskNodesAfterTurn), so
// piece_done is how an assignee finishes ahead of turn end, or hands a structured
// handoff to the next node. The handoff is the input that node runs on; a public
// post_message update to the task thread is user-visible progress that wakes no
// teammate and is never a downstream node's input. The completion itself runs
// through completePieceAndAdvance, the path shared with turn-end capture.
func (m *residentTaskManager) PieceDone(ctx context.Context, subthreadID, pieceID string, handoff *tools.TaskHandoff) (tools.TaskView, error) {
	_ = ctx
	if err := m.ready("piece_done"); err != nil {
		return tools.TaskView{}, err
	}
	thread, _, err := m.memberTask("piece_done", subthreadID)
	if err != nil {
		return tools.TaskView{}, err
	}
	pieceID = strings.TrimSpace(pieceID)
	var piece *session.TaskPiece
	for i := range thread.Plan {
		if thread.Plan[i].ID == pieceID {
			piece = &thread.Plan[i]
			break
		}
	}
	if piece == nil {
		return tools.TaskView{}, fmt.Errorf("piece_done: task %q has no piece %q", thread.ID, pieceID)
	}
	if piece.Assignee != m.participantID {
		return tools.TaskView{}, fmt.Errorf("piece_done: piece %q is assigned to %q, not you — only its assignee may report it done", pieceID, piece.Assignee)
	}
	var sessionHandoff *session.TaskHandoff
	if handoff != nil {
		sessionHandoff = &session.TaskHandoff{
			Done:       handoff.Done,
			Findings:   handoff.Findings,
			Artifacts:  handoff.Artifacts,
			Limits:     handoff.Limits,
			NextGoal:   handoff.NextGoal,
			Acceptance: handoff.Acceptance,
			Notes:      handoff.Notes,
		}
	}
	if err := m.server.completePieceAndAdvance(thread, *piece, sessionHandoff, m.participantID); err != nil {
		return tools.TaskView{}, err
	}
	final, err := session.FindConversationThreadByID(m.server.rt.SessionDir, thread.ID)
	if err != nil {
		return tools.TaskView{}, err
	}
	return m.taskView(final), nil
}

// completePieceAndAdvance is the shared node-completion path: it marks one plan
// piece done, records its handoff onto the downstream node(s), notes the node's
// progress, auto-unfollows the assignee when it has no more pieces, and advances
// the plan. Both ways a node completes route through here — an explicit
// manage_task piece_done (a rich, early handoff — PieceDone delegates after its
// assignee/authorization checks) and turn-end completion capture (a Done-only or
// nil handoff synthesized from the turn's final output). handoff is nil when the
// completing node produced no structured result for a downstream; the downstream
// then wakes on its briefing alone. task is the plan snapshot from before the
// piece is marked done; actorID is the completing assignee (the trace/unfollow
// identity).
func (s *Server) completePieceAndAdvance(task session.ConversationThread, piece session.TaskPiece, handoff *session.TaskHandoff, actorID string) error {
	updated, err := session.MarkTaskPieceStatus(s.rt.SessionDir, task.ID, piece.ID, session.TaskPieceDone)
	if err != nil {
		return fmt.Errorf("piece_done: %w", err)
	}
	s.recordTaskEventFor(updated, piece.ID, session.TaskEventNodeSucceeded, actorID,
		fmt.Sprintf("piece %q reported done", piece.Title), "")
	// Carry the handoff forward before advancing, so a downstream node that
	// becomes ready in the same advancePlan already has its input attached.
	if handoff != nil {
		if err := s.recordPieceHandoff(updated, piece.ID, *handoff, actorID); err != nil {
			return err
		}
	}
	// Completing a piece — the handoff it produced — is real headway, not mere
	// activity: refresh the completing node's progress signal (plan §T9) before
	// the engine advances.
	s.noteNodeProgress(updated, piece.ID, session.TaskEventNodeProgress,
		fmt.Sprintf("piece %q completed", piece.Title))
	// Auto-unfollow when this assignee has no remaining pieces: they are done
	// with the task, so later traffic on it should not wake them (the correct
	// lever for the lead's wrap-up not re-running finished teammates). Keep
	// following if they still own an unfinished piece.
	stillBusy := false
	for _, p := range updated.Plan {
		if p.Assignee == actorID && p.ID != piece.ID && p.Status != session.TaskPieceDone {
			stillBusy = true
			break
		}
	}
	if !stillBusy {
		if err := session.RemoveConversationThreadMember(s.rt.SessionDir, task.ID, actorID); err != nil {
			providers.DebugLogf("piece_done: unfollow %q from %q: %v", actorID, task.ID, err)
		}
	}
	s.notifySubthreadUpdated(task.SessionID, task.ID)
	s.advancePlan(task.ID)
	return nil
}

// recordPieceHandoff marshals the finished piece's handoff and writes it onto
// the node(s) that consume it, then records the handoff_created trace. A node P
// consumes this piece iff P.DependsOn contains pieceID; the handoff lands on
// every such downstream node's Handoff field so the engine renders it into that
// node's wake (the wake that carries the downstream its input). A terminal piece
// (nothing depends on it) stores the handoff on itself, for the lead's final
// wrap-up and the trace. When a node has several upstreams that each hand it a
// payload, the last writer to that Handoff field wins for the wake text — a
// known limitation; the trace keeps every handoff_created event, so the full
// set is always recoverable. task is the plan snapshot taken right after this
// piece was marked done (its DependsOn edges are immutable, so it is the
// authoritative view of who is downstream); actorID is the completing assignee.
func (s *Server) recordPieceHandoff(task session.ConversationThread, pieceID string, handoff session.TaskHandoff, actorID string) error {
	marshaled, err := session.MarshalTaskHandoff(handoff)
	if err != nil {
		return fmt.Errorf("piece_done: %w", err)
	}
	var downstream []string
	for _, p := range task.Plan {
		for _, dep := range p.DependsOn {
			if strings.TrimSpace(dep) == pieceID {
				downstream = append(downstream, p.ID)
				break
			}
		}
	}
	targets := downstream
	if len(targets) == 0 {
		// Terminal node: keep the handoff on the piece itself so the lead's
		// wrap-up (and the trace) can still read the final result.
		targets = []string{pieceID}
	}
	for _, target := range targets {
		if _, err := session.UpdateTaskPiece(s.rt.SessionDir, task.ID, target, func(p *session.TaskPiece) {
			p.Handoff = marshaled
		}); err != nil {
			return fmt.Errorf("piece_done: attach handoff to piece %q: %w", target, err)
		}
	}
	summary := firstNonEmpty(strings.TrimSpace(handoff.NextGoal), strings.TrimSpace(handoff.Done), "handoff")
	s.recordTaskEventFor(task, pieceID, session.TaskEventHandoffCreated, actorID,
		"handoff: "+truncate(summary, 80), marshaled)
	return nil
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
		ExecState:    thread.ExecState,
		Owner:        thread.OwnerParticipantID,
		CreatedBy:    thread.CreatedBy,
		Summary:      thread.Summary,
	}
	if view.Owner != "" {
		if summary, ok := m.server.resolveParticipantSummary(view.Owner); ok {
			view.OwnerName = summary.Name
		}
	}
	for _, p := range thread.Plan {
		view.Plan = append(view.Plan, tools.TaskPiece{
			ID: p.ID, Title: p.Title, Assignee: p.Assignee, DependsOn: p.DependsOn, Status: p.Status,
			Prompt:         p.Prompt,
			State:          deriveNodeState(p.Status),
			LastActivityAt: p.LastActivityAt,
			LastProgressAt: p.LastProgressAt,
		})
	}
	return view
}

// deriveNodeState maps a plan piece's Status to the stable display label the
// task panel renders (plan §T9). It is PURELY status-derived: done -> completed,
// every other status passes through by its own name. It deliberately does NOT
// compute a time-based "stalled" or "lost" label from the activity/progress
// timestamps: that soft staleness cue is a display-only, relative judgement the
// frontend makes by comparing LastActivityAt/LastProgressAt to each other and to
// now (T11) — never a backend transition, and never against a fixed lease
// deadline (red line §4.7).
func deriveNodeState(status string) string {
	switch strings.TrimSpace(status) {
	case session.TaskPieceDone:
		return "completed"
	case session.TaskPieceFailed:
		return "failed"
	case session.TaskPieceBlocked:
		return "blocked"
	case session.TaskPieceRetrying:
		return "retrying"
	case session.TaskPieceActive:
		return "active"
	case session.TaskPiecePending:
		return "pending"
	default:
		return strings.TrimSpace(status)
	}
}

// activePieceForAssignee returns the assignee's currently-running node (active
// or retrying) in a plan, or nil. It attributes a public progress update to the
// exact node the poster is working on (plan §T9): a bystander posting into the
// task thread holds no active piece here and so moves no node's progress signal.
func activePieceForAssignee(plan []session.TaskPiece, assignee string) *session.TaskPiece {
	assignee = strings.TrimSpace(assignee)
	if assignee == "" {
		return nil
	}
	for i := range plan {
		if plan[i].Assignee == assignee &&
			(plan[i].Status == session.TaskPieceActive || plan[i].Status == session.TaskPieceRetrying) {
			return &plan[i]
		}
	}
	return nil
}

// advancePlan is the team-task execution engine (task-rail design §8.2). It
// reads a task's declared plan and moves it forward one step: every piece
// whose dependencies are all done and is still pending gets dispatched — marked
// active and its assignee @-woken into the task thread with a directive. When
// every piece is done, the lead is @-woken to wrap up and report. The engine is
// medium-agnostic: a piece is just "assignee does X, reports done", so code,
// research, and document work ride the same path. It is called after set_plan
// and after each piece_done, and is safe to call repeatedly (dispatch only
// touches pending pieces; a piece already active or done is skipped).
func (s *Server) advancePlan(taskID string) {
	if s == nil || s.rt == nil {
		return
	}
	task, err := session.FindConversationThreadByID(s.rt.SessionDir, taskID)
	if err != nil {
		providers.DebugLogf("advancePlan load task %q: %v", taskID, err)
		return
	}
	if len(task.Plan) == 0 {
		return
	}
	done := map[string]bool{}
	allDone := true
	for _, p := range task.Plan {
		if p.Status == session.TaskPieceDone {
			done[p.ID] = true
		} else {
			allDone = false
		}
	}
	if allDone {
		// Execution is finished the moment the last piece lands — the lead's
		// wrap-up (conclude + report) happens after completed, not before it.
		if err := session.SetConversationThreadExecState(s.rt.SessionDir, task.ID, session.ExecStateCompleted); err != nil {
			providers.DebugLogf("advancePlan mark completed %q: %v", task.ID, err)
		}
		s.wakePlanLead(task)
		return
	}
	for _, p := range task.Plan {
		if p.Status != session.TaskPiecePending {
			continue
		}
		ready := true
		for _, dep := range p.DependsOn {
			if !done[strings.TrimSpace(dep)] {
				ready = false
				break
			}
		}
		if !ready {
			continue
		}
		// Activation is also the first attempt: stamp Attempts to 1 the moment
		// the node is dispatched (retry accounting, plan §T8) and refresh its
		// activity signal. max(Attempts,1) keeps an already-counted node (a
		// fallback re-open that pre-set Attempts) from being reset.
		if _, err := session.UpdateTaskPiece(s.rt.SessionDir, taskID, p.ID, func(pc *session.TaskPiece) {
			pc.Status = session.TaskPieceActive
			if pc.Attempts < 1 {
				pc.Attempts = 1
			}
			pc.LastActivityAt = time.Now().UTC()
		}); err != nil {
			providers.DebugLogf("advancePlan activate piece %q: %v", p.ID, err)
			continue
		}
		s.wakePieceAssignee(task, p)
	}
	s.notifySubthreadUpdated(task.SessionID, task.ID)
}

// planTracePayload serializes a plan's pieces for the workflow_planned trace
// event, so the lead can later see the exact breakdown it authored.
func planTracePayload(pieces []session.TaskPiece) string {
	data, err := json.Marshal(pieces)
	if err != nil {
		return ""
	}
	return string(data)
}

// recordTaskEventFor appends one event to a task's durable execution trace.
// Observability only: failures are logged, never propagated onto the task's
// critical path. task carries the ids so callers with the thread in hand avoid
// an extra lookup.
func (s *Server) recordTaskEventFor(task session.ConversationThread, nodeID, kind, actor, summary, payload string) {
	if s == nil || s.rt == nil || strings.TrimSpace(task.ID) == "" {
		return
	}
	if _, err := session.AppendTaskEvent(s.rt.SessionDir, session.TaskEvent{
		SessionID: task.SessionID,
		TaskID:    task.ID,
		NodeID:    nodeID,
		Kind:      kind,
		Actor:     actor,
		Summary:   summary,
		Payload:   payload,
	}); err != nil {
		providers.DebugLogf("record task event %s/%s: %v", task.ID, kind, err)
	}
}

// noteNodeActivity refreshes a plan node's liveness — LastActivityAt ONLY, never
// LastProgressAt (plan §T9). Activity ("still alive") is any observable action by
// the assignee: a tool call, its result, or assistant commentary. Progress ("real
// headway") is the stronger, separate signal (noteNodeProgress). Keeping the two
// apart is the whole point — stall/liveness is judged from the two timestamps
// RELATIVE to each other, never a fixed lease (red line §4.7). This is the hook
// the per-turn stream fires on: a bare timestamp bump plus one trace event
// (kind = tool_call / tool_result / commentary), so it stays cheap even when
// fired once per stream event. It no-ops on an empty taskID/pieceID, so an
// ordinary DM/chat turn (which resolves no executing node) is untouched.
// commentaryTraceSummary condenses assistant narration into a single short line
// for the activity trace. The raw message can be long and multi-line; the trace
// only needs a human-scannable hint of what the node said, so we keep the first
// line and cap it by rune count (never bytes — the text is often CJK).
func commentaryTraceSummary(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = strings.TrimSpace(text[:i])
	}
	const maxRunes = 120
	if r := []rune(text); len(r) > maxRunes {
		return string(r[:maxRunes]) + "…"
	}
	return text
}

func (s *Server) noteNodeActivity(taskID, pieceID, kind, summary string) {
	if s == nil || s.rt == nil {
		return
	}
	taskID = strings.TrimSpace(taskID)
	pieceID = strings.TrimSpace(pieceID)
	if taskID == "" || pieceID == "" {
		return
	}
	updated, err := session.UpdateTaskPiece(s.rt.SessionDir, taskID, pieceID, func(p *session.TaskPiece) {
		p.LastActivityAt = time.Now().UTC()
	})
	if err != nil {
		providers.DebugLogf("noteNodeActivity %q/%q: %v", taskID, pieceID, err)
		return
	}
	actor := ""
	if p := findPieceByID(updated.Plan, pieceID); p != nil {
		actor = p.Assignee
	}
	s.recordTaskEventFor(updated, pieceID, kind, actor, summary, "")
}

// noteNodeProgress refreshes a plan node's real-headway signal — LastProgressAt
// AND LastActivityAt (progress implies the node is also alive), the stronger of
// the two liveness signals (plan §T9). Progress is a declared step forward: a
// handoff filed on piece_done, a public kind=update posted into the task thread.
// It records one trace event (kind, e.g. node_progress). The caller passes the
// task it already loaded; it no-ops on an empty pieceID. Artifact-write progress
// is deliberately OUT OF SCOPE here — it crosses the tools/appserver boundary —
// so this covers task-update and handoff progress only.
func (s *Server) noteNodeProgress(task session.ConversationThread, pieceID, kind, summary string) {
	if s == nil || s.rt == nil {
		return
	}
	pieceID = strings.TrimSpace(pieceID)
	if strings.TrimSpace(task.ID) == "" || pieceID == "" {
		return
	}
	now := time.Now().UTC()
	updated, err := session.UpdateTaskPiece(s.rt.SessionDir, task.ID, pieceID, func(p *session.TaskPiece) {
		p.LastProgressAt = now
		p.LastActivityAt = now
	})
	if err != nil {
		providers.DebugLogf("noteNodeProgress %q/%q: %v", task.ID, pieceID, err)
		return
	}
	actor := ""
	if p := findPieceByID(updated.Plan, pieceID); p != nil {
		actor = p.Assignee
	}
	s.recordTaskEventFor(updated, pieceID, kind, actor, summary, "")
}

// wakePieceText builds the wake envelope for a dispatched piece from the two
// inputs a node runs on: the lead's per-node Prompt (the briefing authored in
// set_plan) and, when an upstream handed this node a payload, the rendered
// handoff (this node's real input). The handoff-carrying wake is the ONLY thing
// that wakes the downstream — a public thread update never is. If several
// upstreams wrote a handoff onto this node, the last writer's payload is what
// piece.Handoff holds here (last-writer-wins for the wake text); the trace
// retains every handoff_created event.
func wakePieceText(task session.ConversationThread, piece session.TaskPiece) string {
	var b strings.Builder
	fmt.Fprintf(&b,
		"Your piece of task %q is ready to start: %q. Its prerequisites are done.",
		firstNonEmpty(strings.TrimSpace(task.Title), "untitled"), piece.Title,
	)
	if prompt := strings.TrimSpace(piece.Prompt); prompt != "" {
		b.WriteString("\n\nYour briefing:\n")
		b.WriteString(prompt)
	}
	if raw := strings.TrimSpace(piece.Handoff); raw != "" {
		var upstream session.TaskHandoff
		if err := json.Unmarshal([]byte(raw), &upstream); err != nil {
			providers.DebugLogf("wakePieceAssignee decode handoff for %q/%q: %v", task.ID, piece.ID, err)
		} else if rendered := session.RenderHandoffForWake(upstream); rendered != "" {
			b.WriteString("\n\nThe upstream node handed you this input (your real input — not the public thread):\n")
			b.WriteString(rendered)
		}
	}
	fmt.Fprintf(&b,
		"\n\nDo it, working in this task thread (thread_id=%q). Your node completes when your turn ends, so you do not need piece_done to finish it — use manage_task action=piece_done piece_id=%q to finish early or to hand a structured result to the next node in the handoff. If you are blocked, say so before ending (post a kind=question, or need_human / need_upstream). Post here only what a teammate needs — @ them if you need something.",
		task.ID, piece.ID,
	)
	return b.String()
}

// wakePieceAssignee @-wakes one assignee into the task thread to start a piece
// whose dependencies are satisfied. The directive is medium-agnostic — it names
// the piece, not a code action — so the same wake serves research, documents,
// and code.
func (s *Server) wakePieceAssignee(task session.ConversationThread, piece session.TaskPiece) {
	title, workspace := s.taskThreadContext(task.SessionID)
	text := wakePieceText(task, piece)
	s.recordTaskEventFor(task, piece.ID, session.TaskEventNodeStarted, piece.Assignee,
		fmt.Sprintf("node started: %q", piece.Title), "")
	s.deliverEnvelopeToMembers([]string{piece.Assignee}, MessageEnvelope{
		SourceThreadID:      task.SessionID,
		SourceSubthreadID:   task.ID,
		SourceTitle:         title,
		SenderKind:          "system",
		SenderName:          "task plan",
		SenderParticipantID: "",
		Text:                text,
		CreatedAt:           time.Now().UTC(),
		Workspace:           workspace,
	}, nil, true)
}

// wakePlanLead @-wakes the lead once every piece of the plan is done, so the
// lead wraps the task up and reports to the user. The lead is the declared
// LeadParticipantID, falling back to the task's creator.
func (s *Server) wakePlanLead(task session.ConversationThread) {
	lead := firstNonEmpty(strings.TrimSpace(task.LeadParticipantID), strings.TrimSpace(task.CreatedBy))
	if lead == "" {
		return
	}
	title, workspace := s.taskThreadContext(task.SessionID)
	s.recordTaskEventFor(task, "", session.TaskEventLeadInvoked, lead,
		"all pieces done — lead woken to wrap up", "")
	text := fmt.Sprintf(
		"Every piece of task %q is done. Wrap it up: file the task's conclusion (manage_task action=update_status subthread_id=%q), then report the result to the user (@ the user so teammates are not re-woken).",
		firstNonEmpty(strings.TrimSpace(task.Title), "untitled"), task.ID,
	)
	s.deliverEnvelopeToMembers([]string{lead}, MessageEnvelope{
		SourceThreadID:      task.SessionID,
		SourceSubthreadID:   task.ID,
		SourceTitle:         title,
		SenderKind:          "system",
		SenderName:          "task plan",
		SenderParticipantID: "",
		Text:                text,
		CreatedAt:           time.Now().UTC(),
		Workspace:           workspace,
	}, nil, true)
}

// wakePlanLeadForPlanning @-wakes the task's lead immediately after
// escalation with the directive to author the workflow plan. Escalation is
// the start of execution — no human approval step sits between the upgrade
// and the plan. Only the declared lead is woken (no creator fallback: an
// unled task has nobody with orchestration authority to plan it), and an
// empty lead is a loudly-logged no-op rather than a silent skip.
func (s *Server) wakePlanLeadForPlanning(task session.ConversationThread) {
	lead := strings.TrimSpace(task.LeadParticipantID)
	if lead == "" {
		providers.DebugLogf("wakePlanLeadForPlanning: task %q has no lead — nobody to wake for planning", task.ID)
		return
	}
	title, workspace := s.taskThreadContext(task.SessionID)
	s.recordTaskEventFor(task, "", session.TaskEventLeadInvoked, lead,
		"lead woken to plan", "")
	text := fmt.Sprintf(
		"You lead task %q. Author its workflow plan NOW: manage_task action=set_plan subthread_id=%q with pieces {id, title, assignee, prompt, depends_on}. Each piece's prompt is the briefing its assignee starts from — write it so they can begin without asking. Assign pieces to existing teammates; a task you can do alone is a one-piece plan assigned to yourself.",
		firstNonEmpty(strings.TrimSpace(task.Title), "untitled"), task.ID,
	)
	s.deliverEnvelopeToMembers([]string{lead}, MessageEnvelope{
		SourceThreadID:      task.SessionID,
		SourceSubthreadID:   task.ID,
		SourceTitle:         title,
		SenderKind:          "system",
		SenderName:          "task plan",
		SenderParticipantID: "",
		Text:                text,
		CreatedAt:           time.Now().UTC(),
		Workspace:           workspace,
	}, nil, true)
}

// wakePlanLeadOnFailure @-wakes the task's lead after a node has failed
// terminally (its retries are spent, or it hit a failure retrying cannot fix).
// This is the plan §T6 failure wake: the successful path never wakes the lead
// mid-run, but a dead node pauses the task (ExecState blocked) pending the
// lead's decision. The lead falls back to LeadParticipantID then CreatedBy; a
// leadless task is a loudly-logged no-op (nobody holds orchestration authority
// to recover it). The wake names the failed node and its reason and points the
// lead at the three recovery levers — revise the plan (set_plan re-dispatches),
// flag the human (need_human), or wrap it up (update_status) — after reading
// the task's trace to see what happened.
func (s *Server) wakePlanLeadOnFailure(task session.ConversationThread, piece session.TaskPiece, reason string) {
	lead := firstNonEmpty(strings.TrimSpace(task.LeadParticipantID), strings.TrimSpace(task.CreatedBy))
	if lead == "" {
		providers.DebugLogf("wakePlanLeadOnFailure: task %q has no lead to wake on node %q failure", task.ID, piece.ID)
		return
	}
	pieceLabel := firstNonEmpty(strings.TrimSpace(piece.Title), piece.ID)
	title, workspace := s.taskThreadContext(task.SessionID)
	s.recordTaskEventFor(task, "", session.TaskEventLeadInvoked, lead,
		fmt.Sprintf("lead woken on node failure: %s", pieceLabel), "")
	text := fmt.Sprintf(
		"Node %q of task %q failed and its retries are spent: %s. The task is paused (blocked) pending your decision. Read its trace to see what happened, then recover: revise the plan (manage_task action=set_plan — reassign or resequence; that re-dispatches), flag it for the human (manage_task action=need_human subthread_id=%q), or wrap it up (manage_task action=update_status subthread_id=%q).",
		pieceLabel, firstNonEmpty(strings.TrimSpace(task.Title), "untitled"), reason, task.ID, task.ID,
	)
	s.deliverEnvelopeToMembers([]string{lead}, MessageEnvelope{
		SourceThreadID:      task.SessionID,
		SourceSubthreadID:   task.ID,
		SourceTitle:         title,
		SenderKind:          "system",
		SenderName:          "task plan",
		SenderParticipantID: "",
		Text:                text,
		CreatedAt:           time.Now().UTC(),
		Workspace:           workspace,
	}, nil, true)
}

// wakeUpstreamForFallback re-dispatches an upstream node whose handoff a
// downstream node reported insufficient (the need_upstream fallback, plan §T8).
// It is a plain plan-engine dispatch (same system "task plan" envelope shape as
// wakePieceAssignee) carrying the upstream's original briefing plus a labeled
// block naming the downstream node and exactly what it found missing, so the
// upstream can revise its work and re-file piece_done with an updated handoff.
func (s *Server) wakeUpstreamForFallback(task session.ConversationThread, upstream session.TaskPiece, downstreamTitle, reason string) {
	title, workspace := s.taskThreadContext(task.SessionID)
	var b strings.Builder
	fmt.Fprintf(&b,
		"A downstream node (%q) reports your handoff was insufficient: %s. Revise your work and re-file piece_done with an updated handoff.",
		downstreamTitle, reason,
	)
	fmt.Fprintf(&b,
		"\n\nRe-open your piece of task %q: %q.",
		firstNonEmpty(strings.TrimSpace(task.Title), "untitled"), upstream.Title,
	)
	if prompt := strings.TrimSpace(upstream.Prompt); prompt != "" {
		b.WriteString("\n\nYour original briefing:\n")
		b.WriteString(prompt)
	}
	fmt.Fprintf(&b,
		"\n\nWork in this task thread (thread_id=%q); hand your revised result to the downstream with manage_task action=piece_done piece_id=%q; your node also completes when your turn ends.",
		task.ID, upstream.ID,
	)
	s.deliverEnvelopeToMembers([]string{upstream.Assignee}, MessageEnvelope{
		SourceThreadID:      task.SessionID,
		SourceSubthreadID:   task.ID,
		SourceTitle:         title,
		SenderKind:          "system",
		SenderName:          "task plan",
		SenderParticipantID: "",
		Text:                b.String(),
		CreatedAt:           time.Now().UTC(),
		Workspace:           workspace,
	}, nil, true)
}

// findPieceByID returns a pointer to the plan piece with the given id, or nil.
// Used to re-read a piece out of the thread returned by UpdateTaskPiece (which
// already carries the just-applied mutation) so a follow-up wake dispatches the
// fresh node state.
func findPieceByID(plan []session.TaskPiece, id string) *session.TaskPiece {
	id = strings.TrimSpace(id)
	for i := range plan {
		if plan[i].ID == id {
			return &plan[i]
		}
	}
	return nil
}

// taskThreadContext returns the parent thread's display title and workspace
// focus for building a plan-engine wake envelope.
func (s *Server) taskThreadContext(parentThreadID string) (title, workspace string) {
	title = parentThreadID
	if th := s.thread(parentThreadID); th != nil {
		th.mu.Lock()
		title = residentEnvelopeSourceTitleLocked(th)
		workspace = th.FocusWorkspace
		th.mu.Unlock()
	}
	return title, workspace
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
	}, nil, true)
}

// mainStreamItemIDForSeq resolves a message seq (the stable per-thread address
// agents see on envelopes and pass to react) to the main-stream item id the
// GUI anchors reply badges on. It reconstructs the main-stream turns with the
// exact logic the renderer consumes, so the anchor always matches a rendered
// item; a seq that is hidden, folded into a subthread, or nonexistent is an
// error rather than a blind write.
func (s *Server) mainStreamItemIDForSeq(threadID string, seq int) (string, error) {
	item, err := s.mainStreamItemForSeq(threadID, seq)
	if err != nil {
		return "", err
	}
	return item.ID, nil
}

// mainStreamItemForSeq is mainStreamItemIDForSeq's core: the reconstructed
// main-stream ThreadItem addressed by seq. It backs both the seq->item-id map
// and the parent-message binding (seq + author) a task records when created
// from an anchor.
func (s *Server) mainStreamItemForSeq(threadID string, seq int) (ThreadItem, error) {
	records, err := loadPersistedMessages(s.rt.SessionDir, threadID, false)
	if err != nil {
		return ThreadItem{}, fmt.Errorf("load thread history: %w", err)
	}
	turns := turnsFromPersistedHistoryInScope(threadID, "", records, time.Now().UTC(), s.resolveParticipantSummary)
	for _, turn := range turns {
		for _, item := range turn.Items {
			if item.Seq == seq {
				return item, nil
			}
		}
	}
	return ThreadItem{}, fmt.Errorf("no visible main-stream message with seq %d in thread %q", seq, threadID)
}

// mainStreamAnchorBinding resolves a main-stream anchor item id (a rendered GUI
// item id, the inverse of mainStreamItemIDForSeq) to the parent-message binding
// a reply subthread records: the message's seq and its author's participant id.
// It is best-effort — an anchor that matches no rendered main-stream item (a
// synthetic/forged id, or a not-yet-persisted anchor) yields (0, "", false) so
// the caller can proceed with an unbound reply rather than fail; a matched item
// yields (seq, author, true).
func (s *Server) mainStreamAnchorBinding(threadID, anchorItemID string) (seq int, author string, ok bool) {
	anchorItemID = strings.TrimSpace(anchorItemID)
	if anchorItemID == "" {
		return 0, "", false
	}
	records, err := loadPersistedMessages(s.rt.SessionDir, threadID, false)
	if err != nil {
		providers.DebugLogf("mainStreamAnchorBinding load %q: %v", threadID, err)
		return 0, "", false
	}
	turns := turnsFromPersistedHistoryInScope(threadID, "", records, time.Now().UTC(), s.resolveParticipantSummary)
	for _, turn := range turns {
		for _, item := range turn.Items {
			if item.ID == anchorItemID {
				return item.Seq, parentAuthorParticipantID(item), true
			}
		}
	}
	return 0, "", false
}

// parentAuthorParticipantID is the participant id credited as a main-stream
// item's author for parent-message binding: the item's participant id when it
// is agent-originated, else the stable "human" identity (a nil participant is
// the thread owner — the human user posting a top-level message).
func parentAuthorParticipantID(item ThreadItem) string {
	if item.Participant != nil {
		if id := strings.TrimSpace(item.Participant.ID); id != "" {
			return id
		}
	}
	return humanReactionParticipantID
}

// bubbleSubthreadResolve wraps a subthread up: the summary posts to the
// parent's main stream under author's identity and the subthread resolves
// with that summary stored. Serves the human bubble RPC
// (handleThreadBubbleSub) and plain-reply wrap-ups; agents conclude tasks
// through ConcludeTask instead.
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
// still flows to resolved like any other; a resolved task means the work
// was delivered, so re-creating the title is a legitimate redo and must
// not be blocked. Open discussion replies are not tasks and never collide.
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
