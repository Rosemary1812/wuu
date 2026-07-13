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

// residentTaskManager implements the group Thread/Task workflow for one named
// participant. Like
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

func (m *residentTaskManager) OpenThread(ctx context.Context, threadID string, anchorSeq int, title string) (tools.TaskView, error) {
	_ = ctx
	if err := m.ready("open_thread"); err != nil {
		return tools.TaskView{}, err
	}
	threadID = strings.TrimSpace(threadID)
	title = strings.TrimSpace(title)
	if threadID == "" {
		return tools.TaskView{}, errors.New("open_thread: thread_id is required")
	}
	if anchorSeq <= 0 {
		return tools.TaskView{}, errors.New("open_thread: anchor_seq is required; standalone Threads and Tasks do not exist")
	}
	if _, err := m.boardThreadMeta("open_thread", threadID); err != nil {
		return tools.TaskView{}, err
	}
	anchorItem, err := m.server.mainStreamItemForSeq(threadID, anchorSeq)
	if err != nil {
		return tools.TaskView{}, fmt.Errorf("open_thread: %w", err)
	}
	if anchorItem.Type != ThreadItemUserMessage && anchorItem.Type != ThreadItemParticipantMsg {
		return tools.TaskView{}, fmt.Errorf("open_thread: message seq %d is %q; only a human or named-agent message can anchor a Thread", anchorSeq, anchorItem.Type)
	}
	requestedOwner := ""
	if parentAuthorParticipantID(anchorItem) == humanReactionParticipantID {
		requestedOwner = m.participantID
	}
	thread, err := m.server.openConversationSubthreadAs(ThreadOpenSubParams{
		ThreadID: threadID, AnchorItemID: anchorItem.ID, Title: title,
		ThreadOwnerParticipantID: requestedOwner,
	}, m.participantID)
	if err != nil {
		return tools.TaskView{}, fmt.Errorf("open_thread: %w", err)
	}
	// The tool result exposes both ids, but a model can still reuse the parent
	// group id from its incoming_message when it posts the convergence note in
	// this same turn. Bind subsequent speech to the Thread mechanically.
	if current, ok := m.server.residentTurnSpeech.Load(m.participantID); ok {
		if limiter, ok := current.(*residentSpeechLimiter); ok {
			limiter.preferReplySubthread(threadID, thread.ID)
		}
	}
	m.server.notifySubthreadUpdated(threadID, thread.ID)
	return m.taskView(thread), nil
}

// PromoteThread promotes an open discussion Thread to a board Task once the
// conversation in it has converged. Only the persisted Thread owner may do so;
// that same owner becomes task lead atomically. Provenance (EscalatedBy) records
// the converting agent. Promotion starts execution immediately in planning and
// wakes the lead to author the plan.
func (m *residentTaskManager) PromoteThread(ctx context.Context, subthreadID, title string) (tools.TaskView, error) {
	_ = ctx
	if err := m.ready("promote"); err != nil {
		return tools.TaskView{}, err
	}
	thread, _, err := m.memberTask("promote", subthreadID)
	if err != nil {
		return tools.TaskView{}, err
	}
	if thread.Status != session.ConversationThreadOpen {
		return tools.TaskView{}, fmt.Errorf("promote: Thread %q is %q; only an open Thread can be promoted", thread.ID, thread.Status)
	}
	if owner := strings.TrimSpace(thread.ThreadOwnerParticipantID); owner == "" || owner != m.participantID {
		return tools.TaskView{}, fmt.Errorf("promote: Thread %q is owned by %q; only its owner may promote it", thread.ID, owner)
	}
	sessionDir := m.server.rt.SessionDir
	escalated, err := session.EscalateConversationThread(sessionDir, thread.ID, m.participantID, title)
	if err != nil {
		return tools.TaskView{}, fmt.Errorf("promote: %w", err)
	}
	m.server.recordTaskEventFor(escalated, "", session.TaskEventTaskCreated, m.participantID,
		fmt.Sprintf("Thread promoted to Task: %q", firstNonEmpty(strings.TrimSpace(escalated.Title), "untitled")), "")
	m.server.notifySubthreadUpdated(escalated.SessionID, escalated.ID)
	m.server.wakePlanLeadForPlanning(escalated)
	return m.taskView(escalated), nil
}

// ConcludeTask files the task's conclusion and completes it in one act (the
// manage_task conclude action): a single store CAS resolves the task —
// only the lead may conclude — then a short conclusion signal is published to
// the parent main stream under the caller's identity while the complete report
// stays on the Task. The store accepts this only after every worker node is
// done and execution is awaiting the lead's verification.
func (m *residentTaskManager) ConcludeTask(ctx context.Context, subthreadID, summary string) (tools.TaskView, error) {
	_ = ctx
	if err := m.ready("conclude"); err != nil {
		return tools.TaskView{}, err
	}
	thread, _, err := m.memberTask("conclude", subthreadID)
	if err != nil {
		return tools.TaskView{}, err
	}
	concluded, err := session.ConcludeConversationThread(m.server.rt.SessionDir, thread.ID, m.participantID, summary)
	if err != nil {
		return tools.TaskView{}, err
	}
	// The decision-bearing first paragraph posts to the parent's main stream
	// (empty ThreadID = full-roster visible); the durable Task keeps the full
	// report for its detail view and trace.
	if err := m.server.publishParticipantMessage(concluded.SessionID, agentcontrol.ParticipantMessage{
		ParticipantID: m.participantID,
		Kind:          "result",
		Text:          taskConclusionSignal(concluded.Summary),
	}); err != nil {
		return tools.TaskView{}, fmt.Errorf("conclude: publish conclusion: %w", err)
	}
	m.server.recordTaskEventFor(concluded, "", session.TaskEventTaskCompleted, m.participantID,
		concluded.Summary, "")
	m.server.notifySubthreadUpdated(concluded.SessionID, concluded.ID)
	return m.taskView(concluded), nil
}

// taskConclusionSignal keeps the durable Task as the home of the complete
// report while the parent group receives only its decision-bearing first
// paragraph. A leading markdown heading is treated as a label, not the signal.
func taskConclusionSignal(summary string) string {
	summary = strings.ReplaceAll(strings.TrimSpace(summary), "\r\n", "\n")
	var paragraph []string
	fallback := ""
	for _, raw := range strings.Split(summary, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			if len(paragraph) > 0 {
				break
			}
			continue
		}
		if len(paragraph) == 0 && strings.HasPrefix(line, "#") {
			fallback = strings.TrimSpace(strings.TrimLeft(line, "#"))
			continue
		}
		paragraph = append(paragraph, line)
	}
	signal := strings.Join(strings.Fields(strings.Join(paragraph, " ")), " ")
	if signal == "" {
		signal = fallback
	}
	runes := []rune(signal)
	if len(runes) > maxGroupBriefRunes {
		signal = string(runes[:maxGroupBriefRunes-1]) + "…"
	}
	return signal
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
	for _, piece := range thread.Plan {
		if piece.Assignee != m.participantID || strings.TrimSpace(piece.CurrentAttemptID) == "" {
			continue
		}
		_, updated, finishErr := session.FinishTaskAttempt(
			m.server.rt.SessionDir, piece.CurrentAttemptID, session.TaskAttemptInterrupted,
			session.TaskPieceBlocked, "need_human", reason, time.Now().UTC(),
		)
		if finishErr != nil {
			return tools.TaskView{}, fmt.Errorf("need_human: stop current attempt: %w", finishErr)
		}
		thread = updated
		m.server.recordTaskEventForAttempt(thread, piece.ID, piece.CurrentAttemptID, session.TaskEventBlocked, m.participantID,
			fmt.Sprintf("waiting for the human: %s", reason), "")
		break
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
	if strings.TrimSpace(piece.CurrentAttemptID) == "" {
		return tools.TaskView{}, fmt.Errorf("need_upstream: piece %q has no active attempt", pieceID)
	}
	if _, updated, err := session.FinishTaskAttempt(
		m.server.rt.SessionDir, piece.CurrentAttemptID, session.TaskAttemptInterrupted,
		session.TaskPieceBlocked, "need_upstream", reason, time.Now().UTC(),
	); err != nil {
		return tools.TaskView{}, fmt.Errorf("need_upstream: stop downstream attempt: %w", err)
	} else {
		thread = updated
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
			p.Status = session.TaskPieceRetrying
			p.CurrentAttemptID = ""
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
		attempt, reserved, err := session.ReserveTaskAttempt(m.server.rt.SessionDir, thread.ID, upID, up.Assignee)
		if err != nil {
			providers.DebugLogf("need_upstream reserve %q/%q: %v", thread.ID, upID, err)
			continue
		}
		fresh := findPieceByID(reserved.Plan, upID)
		if fresh != nil {
			m.server.wakeUpstreamForFallback(reserved, *fresh, attempt, downstreamTitle, reason)
		}
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

func (m *residentTaskManager) ListWorkflowThreads(ctx context.Context, threadID string) ([]tools.TaskView, error) {
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
		if thread.Status == session.ConversationThreadOpen || thread.Status == session.ConversationThreadTask {
			views = append(views, m.taskView(thread))
		}
	}
	return views, nil
}

func (m *residentTaskManager) TraceTask(ctx context.Context, subthreadID string) ([]tools.TaskEvent, error) {
	_ = ctx
	thread, _, err := m.memberTask("trace", subthreadID)
	if err != nil {
		return nil, err
	}
	events, err := session.TaskEvents(m.server.rt.SessionDir, thread.ID)
	if err != nil {
		return nil, err
	}
	out := make([]tools.TaskEvent, 0, len(events))
	for _, event := range events {
		out = append(out, tools.TaskEvent{
			Seq: event.Seq, NodeID: event.NodeID, AttemptID: event.AttemptID, Kind: event.Kind,
			Actor: event.Actor, Summary: event.Summary, Payload: event.Payload, At: event.At,
		})
	}
	return out, nil
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
	// Plan authority is immutable: promotion copied Thread owner to Task lead.
	// Legacy leadless tasks stay unreadable through this mutation path rather
	// than granting authority to the first caller.
	lead := strings.TrimSpace(thread.LeadParticipantID)
	if lead == "" {
		return tools.TaskView{}, fmt.Errorf("set_plan: task %q has no lead; only a Thread promoted by its owner can be planned", thread.ID)
	}
	if m.participantID != lead {
		return tools.TaskView{}, fmt.Errorf("set_plan: task %q is led by %q; only its lead may declare or revise the plan", thread.ID, lead)
	}
	if len(thread.Plan) > 0 {
		return tools.TaskView{}, fmt.Errorf("set_plan: task %q already has a running plan; use explicit workflow revision actions instead of replacing live nodes", thread.ID)
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
		if strings.TrimSpace(p.Assignee) == lead {
			return tools.TaskView{}, fmt.Errorf("set_plan: Task lead %q orchestrates other named agents and cannot be a piece assignee", lead)
		}
		if err := m.validateTaskAssignee(thread, strings.TrimSpace(p.Assignee)); err != nil {
			return tools.TaskView{}, fmt.Errorf("set_plan: piece %q: %w", id, err)
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

// AddTaskPiece appends one new node without replacing completed work or live
// attempt history. Dependencies may name any existing node; the resulting graph
// must remain acyclic.
func (m *residentTaskManager) AddTaskPiece(ctx context.Context, subthreadID string, piece tools.TaskPiece) (tools.TaskView, error) {
	_ = ctx
	thread, err := m.leadTask("add_piece", subthreadID)
	if err != nil {
		return tools.TaskView{}, err
	}
	piece.ID = strings.TrimSpace(piece.ID)
	piece.Title = strings.TrimSpace(piece.Title)
	piece.Assignee = strings.TrimSpace(piece.Assignee)
	piece.Prompt = strings.TrimSpace(piece.Prompt)
	if piece.ID == "" || piece.Title == "" || piece.Assignee == "" {
		return tools.TaskView{}, errors.New("add_piece: piece_id, title, and assignee are required")
	}
	if err := m.validateTaskAssignee(thread, piece.Assignee); err != nil {
		return tools.TaskView{}, fmt.Errorf("add_piece: %w", err)
	}
	updated, err := session.MutateConversationThreadPlan(m.server.rt.SessionDir, thread.ID, func(current *session.ConversationThread) error {
		for _, existing := range current.Plan {
			if existing.ID == piece.ID {
				return fmt.Errorf("add_piece: piece %q already exists", piece.ID)
			}
		}
		current.Plan = append(current.Plan, session.TaskPiece{
			ID: piece.ID, Title: piece.Title, Assignee: piece.Assignee, Prompt: piece.Prompt,
			DependsOn: cleanPieceDependencies(piece.DependsOn), Status: session.TaskPiecePending,
		})
		return validateTaskPlanGraph(current.Plan)
	})
	if err != nil {
		return tools.TaskView{}, err
	}
	if err := session.AddConversationThreadMember(m.server.rt.SessionDir, thread.ID, piece.Assignee); err != nil {
		return tools.TaskView{}, fmt.Errorf("add_piece: add assignee: %w", err)
	}
	if err := session.SetConversationThreadExecState(m.server.rt.SessionDir, thread.ID, session.ExecStateExecuting); err != nil {
		return tools.TaskView{}, err
	}
	updated.ExecState = session.ExecStateExecuting
	m.server.recordTaskEventFor(updated, piece.ID, session.TaskEventWorkflowRevised, m.participantID, "lead added a workflow node", "")
	m.server.notifySubthreadUpdated(updated.SessionID, updated.ID)
	m.server.reconcileExecutingTasks()
	final, err := session.FindConversationThreadByID(m.server.rt.SessionDir, thread.ID)
	return m.taskView(final), err
}

func (m *residentTaskManager) ReviseTaskPiece(ctx context.Context, subthreadID, pieceID, title, prompt string, dependsOn []string) (tools.TaskView, error) {
	_ = ctx
	thread, err := m.leadTask("revise_piece", subthreadID)
	if err != nil {
		return tools.TaskView{}, err
	}
	pieceID = strings.TrimSpace(pieceID)
	updated, err := session.MutateConversationThreadPlan(m.server.rt.SessionDir, thread.ID, func(current *session.ConversationThread) error {
		piece := findPieceByID(current.Plan, pieceID)
		if piece == nil {
			return fmt.Errorf("revise_piece: task %q has no piece %q", current.ID, pieceID)
		}
		if piece.CurrentAttemptID != "" || piece.Status == session.TaskPieceDone || piece.Status == session.TaskPieceCancelled {
			return fmt.Errorf("revise_piece: piece %q is %q and cannot be revised", pieceID, piece.Status)
		}
		if value := strings.TrimSpace(title); value != "" {
			piece.Title = value
		}
		if value := strings.TrimSpace(prompt); value != "" {
			piece.Prompt = value
		}
		if dependsOn != nil {
			piece.DependsOn = cleanPieceDependencies(dependsOn)
		}
		return validateTaskPlanGraph(current.Plan)
	})
	if err != nil {
		return tools.TaskView{}, err
	}
	m.server.recordTaskEventFor(updated, pieceID, session.TaskEventWorkflowRevised, m.participantID, "lead revised a workflow node", "")
	m.server.notifySubthreadUpdated(updated.SessionID, updated.ID)
	m.server.reconcileExecutingTasks()
	final, err := session.FindConversationThreadByID(m.server.rt.SessionDir, thread.ID)
	return m.taskView(final), err
}

func (m *residentTaskManager) ReassignTaskPiece(ctx context.Context, subthreadID, pieceID, assignee string) (tools.TaskView, error) {
	_ = ctx
	thread, err := m.leadTask("reassign_piece", subthreadID)
	if err != nil {
		return tools.TaskView{}, err
	}
	assignee = strings.TrimSpace(assignee)
	if err := m.validateTaskAssignee(thread, assignee); err != nil {
		return tools.TaskView{}, fmt.Errorf("reassign_piece: %w", err)
	}
	updated, err := session.MutateConversationThreadPlan(m.server.rt.SessionDir, thread.ID, func(current *session.ConversationThread) error {
		piece := findPieceByID(current.Plan, strings.TrimSpace(pieceID))
		if piece == nil {
			return fmt.Errorf("reassign_piece: task %q has no piece %q", current.ID, pieceID)
		}
		if piece.CurrentAttemptID != "" || piece.Status == session.TaskPieceDone || piece.Status == session.TaskPieceCancelled {
			return fmt.Errorf("reassign_piece: piece %q is %q and cannot be reassigned", piece.ID, piece.Status)
		}
		piece.Assignee = assignee
		return nil
	})
	if err != nil {
		return tools.TaskView{}, err
	}
	if err := session.AddConversationThreadMember(m.server.rt.SessionDir, thread.ID, assignee); err != nil {
		return tools.TaskView{}, err
	}
	m.server.recordTaskEventFor(updated, strings.TrimSpace(pieceID), session.TaskEventWorkflowRevised, m.participantID, "lead reassigned a workflow node", "")
	m.server.notifySubthreadUpdated(updated.SessionID, updated.ID)
	m.server.reconcileExecutingTasks()
	final, err := session.FindConversationThreadByID(m.server.rt.SessionDir, thread.ID)
	return m.taskView(final), err
}

func (m *residentTaskManager) RetryTaskPiece(ctx context.Context, subthreadID, pieceID, reason string) (tools.TaskView, error) {
	_ = ctx
	thread, err := m.leadTask("retry_piece", subthreadID)
	if err != nil {
		return tools.TaskView{}, err
	}
	pieceID = strings.TrimSpace(pieceID)
	updated, err := session.MutateConversationThreadPlan(m.server.rt.SessionDir, thread.ID, func(current *session.ConversationThread) error {
		piece := findPieceByID(current.Plan, pieceID)
		if piece == nil {
			return fmt.Errorf("retry_piece: task %q has no piece %q", current.ID, pieceID)
		}
		if piece.CurrentAttemptID != "" || (piece.Status != session.TaskPieceBlocked && piece.Status != session.TaskPieceFailed) {
			return fmt.Errorf("retry_piece: piece %q is %q, not blocked or failed", pieceID, piece.Status)
		}
		piece.Status = session.TaskPieceRetrying
		piece.FailureReason = strings.TrimSpace(reason)
		if piece.RetryBudget <= piece.Attempts {
			piece.RetryBudget = piece.Attempts + 1
		}
		return nil
	})
	if err != nil {
		return tools.TaskView{}, err
	}
	if err := session.SetConversationThreadExecState(m.server.rt.SessionDir, thread.ID, session.ExecStateExecuting); err != nil {
		return tools.TaskView{}, err
	}
	updated.ExecState = session.ExecStateExecuting
	m.server.recordTaskEventFor(updated, pieceID, session.TaskEventWorkflowRevised, m.participantID, "lead scheduled an explicit retry", "")
	m.server.notifySubthreadUpdated(updated.SessionID, updated.ID)
	m.server.reconcileExecutingTasks()
	final, err := session.FindConversationThreadByID(m.server.rt.SessionDir, thread.ID)
	return m.taskView(final), err
}

func (m *residentTaskManager) CancelTaskPiece(ctx context.Context, subthreadID, pieceID, reason string) (tools.TaskView, error) {
	_ = ctx
	thread, err := m.leadTask("cancel_piece", subthreadID)
	if err != nil {
		return tools.TaskView{}, err
	}
	pieceID = strings.TrimSpace(pieceID)
	piece := findPieceByID(thread.Plan, pieceID)
	if piece == nil {
		return tools.TaskView{}, fmt.Errorf("cancel_piece: task %q has no piece %q", thread.ID, pieceID)
	}
	if piece.Status == session.TaskPieceDone {
		return tools.TaskView{}, fmt.Errorf("cancel_piece: completed piece %q cannot be cancelled", pieceID)
	}
	cancelled := map[string]bool{pieceID: true}
	changed := true
	for changed {
		changed = false
		for _, candidate := range thread.Plan {
			if cancelled[candidate.ID] || candidate.Status == session.TaskPieceDone {
				continue
			}
			for _, dep := range candidate.DependsOn {
				if cancelled[dep] {
					cancelled[candidate.ID] = true
					changed = true
					break
				}
			}
		}
	}
	for _, candidate := range thread.Plan {
		if cancelled[candidate.ID] && candidate.ID != pieceID && candidate.CurrentAttemptID != "" {
			return tools.TaskView{}, fmt.Errorf("cancel_piece: dependent piece %q is running; cancel it explicitly first", candidate.ID)
		}
	}
	if piece.CurrentAttemptID != "" {
		if _, updated, err := session.FinishTaskAttempt(
			m.server.rt.SessionDir, piece.CurrentAttemptID, session.TaskAttemptInterrupted,
			session.TaskPieceCancelled, "cancelled", strings.TrimSpace(reason), time.Now().UTC(),
		); err != nil {
			return tools.TaskView{}, err
		} else {
			thread = updated
		}
	}
	updated, err := session.MutateConversationThreadPlan(m.server.rt.SessionDir, thread.ID, func(current *session.ConversationThread) error {
		cancelled := map[string]bool{pieceID: true}
		changed := true
		for changed {
			changed = false
			for i := range current.Plan {
				if cancelled[current.Plan[i].ID] || current.Plan[i].Status == session.TaskPieceDone {
					continue
				}
				for _, dep := range current.Plan[i].DependsOn {
					if cancelled[dep] {
						cancelled[current.Plan[i].ID] = true
						changed = true
						break
					}
				}
			}
		}
		for i := range current.Plan {
			if cancelled[current.Plan[i].ID] {
				current.Plan[i].Status = session.TaskPieceCancelled
				current.Plan[i].FailureReason = strings.TrimSpace(reason)
			}
		}
		return nil
	})
	if err != nil {
		return tools.TaskView{}, err
	}
	if err := session.SetConversationThreadExecState(m.server.rt.SessionDir, thread.ID, session.ExecStateExecuting); err != nil {
		return tools.TaskView{}, err
	}
	updated.ExecState = session.ExecStateExecuting
	m.server.recordTaskEventFor(updated, pieceID, session.TaskEventNodeCancelled, m.participantID, firstNonEmpty(strings.TrimSpace(reason), "lead cancelled node"), "")
	m.server.notifySubthreadUpdated(updated.SessionID, updated.ID)
	m.server.reconcileExecutingTasks()
	final, err := session.FindConversationThreadByID(m.server.rt.SessionDir, thread.ID)
	return m.taskView(final), err
}

func (m *residentTaskManager) ResumeTask(ctx context.Context, subthreadID, reason string) (tools.TaskView, error) {
	_ = ctx
	thread, err := m.leadTask("resume", subthreadID)
	if err != nil {
		return tools.TaskView{}, err
	}
	updated, err := session.MutateConversationThreadPlan(m.server.rt.SessionDir, thread.ID, func(current *session.ConversationThread) error {
		resumed := 0
		for i := range current.Plan {
			piece := &current.Plan[i]
			if piece.CurrentAttemptID == "" && (piece.Status == session.TaskPieceBlocked || piece.Status == session.TaskPieceFailed) {
				piece.Status = session.TaskPieceRetrying
				piece.FailureReason = strings.TrimSpace(reason)
				if piece.RetryBudget <= piece.Attempts {
					piece.RetryBudget = piece.Attempts + 1
				}
				resumed++
			}
		}
		if resumed == 0 {
			return errors.New("resume: task has no blocked or failed nodes to resume")
		}
		return nil
	})
	if err != nil {
		return tools.TaskView{}, err
	}
	if err := session.SetConversationThreadExecState(m.server.rt.SessionDir, thread.ID, session.ExecStateExecuting); err != nil {
		return tools.TaskView{}, err
	}
	updated.ExecState = session.ExecStateExecuting
	m.server.recordTaskEventFor(updated, "", session.TaskEventWorkflowRevised, m.participantID, firstNonEmpty(strings.TrimSpace(reason), "lead resumed workflow"), "")
	m.server.notifySubthreadUpdated(updated.SessionID, updated.ID)
	m.server.reconcileExecutingTasks()
	final, err := session.FindConversationThreadByID(m.server.rt.SessionDir, thread.ID)
	return m.taskView(final), err
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
	pieceID = strings.TrimSpace(pieceID)
	if strings.TrimSpace(subthreadID) == "" {
		attempt, err := session.ActiveTaskAttemptForAssignee(m.server.rt.SessionDir, m.participantID)
		if err != nil {
			return tools.TaskView{}, fmt.Errorf("piece_done: infer current Task: %w", err)
		}
		if attempt.NodeID != pieceID {
			return tools.TaskView{}, fmt.Errorf("piece_done: active assignment is piece %q, not %q", attempt.NodeID, pieceID)
		}
		subthreadID = attempt.TaskID
	}
	thread, _, err := m.memberTask("piece_done", subthreadID)
	if err != nil {
		return tools.TaskView{}, err
	}
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
	attemptID := strings.TrimSpace(piece.CurrentAttemptID)
	if attemptID == "" {
		return fmt.Errorf("piece_done: piece %q has no active attempt", piece.ID)
	}
	_, updated, err := session.FinishTaskAttempt(
		s.rt.SessionDir, attemptID, session.TaskAttemptSucceeded, session.TaskPieceDone, "", "", time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("piece_done: %w", err)
	}
	s.recordTaskEventForAttempt(updated, piece.ID, attemptID, session.TaskEventNodeSucceeded, actorID,
		fmt.Sprintf("piece %q reported done", piece.Title), "")
	// Carry the handoff forward before advancing, so a downstream node that
	// becomes ready in the same advancePlan already has its input attached.
	if handoff != nil {
		if err := s.recordPieceHandoff(updated, piece.ID, attemptID, *handoff, actorID); err != nil {
			return err
		}
	}
	// Completing a piece — the handoff it produced — is real headway, not mere
	// activity: refresh the completing node's progress signal (plan §T9) before
	// the engine advances.
	s.noteNodeProgress(updated, piece.ID, session.TaskEventNodeProgress,
		fmt.Sprintf("piece %q completed", piece.Title), attemptID)
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
	s.reconcileExecutingTasks()
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
func (s *Server) recordPieceHandoff(task session.ConversationThread, pieceID, attemptID string, handoff session.TaskHandoff, actorID string) error {
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
	s.recordTaskEventForAttempt(task, pieceID, attemptID, session.TaskEventHandoffCreated, actorID,
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

func (m *residentTaskManager) leadTask(action, subthreadID string) (session.ConversationThread, error) {
	if err := m.ready(action); err != nil {
		return session.ConversationThread{}, err
	}
	thread, _, err := m.memberTask(action, subthreadID)
	if err != nil {
		return session.ConversationThread{}, err
	}
	if thread.Status != session.ConversationThreadTask {
		return session.ConversationThread{}, fmt.Errorf("%s: task %q is %q, not active", action, thread.ID, thread.Status)
	}
	if lead := strings.TrimSpace(thread.LeadParticipantID); lead == "" || lead != m.participantID {
		return session.ConversationThread{}, fmt.Errorf("%s: task %q is led by %q; only its lead may revise the workflow", action, thread.ID, lead)
	}
	return thread, nil
}

func (m *residentTaskManager) validateTaskAssignee(thread session.ConversationThread, assignee string) error {
	assignee = strings.TrimSpace(assignee)
	if assignee == "" {
		return errors.New("assignee is required")
	}
	if assignee == strings.TrimSpace(thread.LeadParticipantID) {
		return fmt.Errorf("Task lead %q orchestrates and cannot be a piece assignee", assignee)
	}
	members, err := session.ListThreadMembers(m.server.rt.SessionDir, thread.SessionID)
	if err != nil {
		return err
	}
	for _, memberID := range members {
		if strings.TrimSpace(memberID) == assignee {
			return nil
		}
	}
	return fmt.Errorf("assignee %q is not an active named member of group %q", assignee, thread.SessionID)
}

func cleanPieceDependencies(raw []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, dep := range raw {
		dep = strings.TrimSpace(dep)
		if dep != "" && !seen[dep] {
			seen[dep] = true
			out = append(out, dep)
		}
	}
	return out
}

func validateTaskPlanGraph(plan []session.TaskPiece) error {
	byID := make(map[string]session.TaskPiece, len(plan))
	for _, piece := range plan {
		id := strings.TrimSpace(piece.ID)
		if id == "" || byID[id].ID != "" {
			return fmt.Errorf("workflow contains an empty or duplicate piece id %q", id)
		}
		byID[id] = piece
	}
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return fmt.Errorf("workflow dependency cycle includes %q", id)
		}
		if visited[id] {
			return nil
		}
		visiting[id] = true
		for _, dep := range byID[id].DependsOn {
			dep = strings.TrimSpace(dep)
			if _, ok := byID[dep]; !ok {
				return fmt.Errorf("piece %q depends on unknown piece %q", id, dep)
			}
			if err := visit(dep); err != nil {
				return err
			}
		}
		visiting[id] = false
		visited[id] = true
		return nil
	}
	for id := range byID {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

// boardThreadMeta gates workflow access to active named members of a group.
// DMs and ordinary work sessions have no Thread/Task workflow.
func (m *residentTaskManager) boardThreadMeta(action, threadID string) (session.Session, error) {
	sessionDir := m.server.rt.SessionDir
	meta, ok, err := session.Find(sessionDir, threadID)
	if err != nil {
		return session.Session{}, fmt.Errorf("%s: lookup thread: %w", action, err)
	}
	if !ok {
		return session.Session{}, fmt.Errorf("%s: %w: %q", action, session.ErrSessionNotFound, threadID)
	}
	if !meta.Group {
		return session.Session{}, fmt.Errorf("%s: thread %q is not a group; DMs and ordinary sessions do not have Threads or Tasks", action, threadID)
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
		ThreadOwner:  thread.ThreadOwnerParticipantID,
		Lead:         thread.LeadParticipantID,
		CreatedBy:    thread.CreatedBy,
		Summary:      thread.Summary,
	}
	if view.Lead != "" {
		if summary, ok := m.server.resolveParticipantSummary(view.Lead); ok {
			view.LeadName = summary.Name
		}
	}
	for _, p := range thread.Plan {
		view.Plan = append(view.Plan, tools.TaskPiece{
			ID: p.ID, Title: p.Title, Assignee: p.Assignee, DependsOn: p.DependsOn, Status: p.Status,
			Prompt:           p.Prompt,
			CurrentAttemptID: p.CurrentAttemptID,
			State:            deriveNodeState(p.Status),
			LastActivityAt:   p.LastActivityAt,
			LastProgressAt:   p.LastProgressAt,
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
	case session.TaskPieceCancelled:
		return "cancelled"
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
		if p.Status == session.TaskPieceDone || p.Status == session.TaskPieceCancelled {
			done[p.ID] = true
		} else {
			allDone = false
		}
	}
	if allDone {
		// Worker execution is finished, but the Task is not. Exactly one caller
		// wins executing -> awaiting_lead and wakes the lead to verify the trace.
		transitioned, err := session.TransitionConversationThreadExecState(
			s.rt.SessionDir, task.ID, session.ExecStateExecuting, session.ExecStateAwaitingLead,
		)
		if err != nil {
			providers.DebugLogf("advancePlan await lead %q: %v", task.ID, err)
			return
		}
		if transitioned {
			task.ExecState = session.ExecStateAwaitingLead
			s.notifySubthreadUpdated(task.SessionID, task.ID)
			s.wakePlanLead(task)
		}
		return
	}
	for _, p := range task.Plan {
		if p.Status != session.TaskPiecePending && p.Status != session.TaskPieceRetrying {
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
		attempt, updated, err := session.ReserveTaskAttempt(s.rt.SessionDir, taskID, p.ID, p.Assignee)
		if errors.Is(err, session.ErrTaskAssigneeBusy) {
			continue
		}
		if err != nil {
			providers.DebugLogf("advancePlan reserve piece %q: %v", p.ID, err)
			continue
		}
		fresh := findPieceByID(updated.Plan, p.ID)
		if fresh == nil {
			continue
		}
		s.wakePieceAssignee(updated, *fresh, attempt)
	}
	s.notifySubthreadUpdated(task.SessionID, task.ID)
}

// reconcileExecutingTasks gives every live workflow another scheduling pass.
// It runs after an attempt releases its assignee reservation so a ready node in
// another Task cannot remain pending merely because it lost an earlier race.
func (s *Server) reconcileExecutingTasks() {
	if s == nil || s.rt == nil {
		return
	}
	tasks, err := session.ExecutingTaskThreads(s.rt.SessionDir)
	if err != nil {
		providers.DebugLogf("reconcile executing tasks: %v", err)
		return
	}
	for _, task := range tasks {
		s.advancePlan(task.ID)
	}
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
	s.recordTaskEventForAttempt(task, nodeID, "", kind, actor, summary, payload)
}

func (s *Server) recordTaskEventForAttempt(task session.ConversationThread, nodeID, attemptID, kind, actor, summary, payload string) {
	if s == nil || s.rt == nil || strings.TrimSpace(task.ID) == "" {
		return
	}
	if _, err := session.AppendTaskEvent(s.rt.SessionDir, session.TaskEvent{
		SessionID: task.SessionID,
		TaskID:    task.ID,
		NodeID:    nodeID,
		AttemptID: attemptID,
		Kind:      kind,
		Actor:     actor,
		Summary:   summary,
		Payload:   payload,
	}); err != nil {
		providers.DebugLogf("record task event %s/%s/%s: %v", task.ID, nodeID, attemptID, err)
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

func (s *Server) noteNodeActivity(taskID, pieceID, kind, summary string, attemptIDs ...string) {
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
	attemptID := ""
	if p := findPieceByID(updated.Plan, pieceID); p != nil {
		actor = p.Assignee
		attemptID = p.CurrentAttemptID
	}
	if len(attemptIDs) > 0 {
		attemptID = strings.TrimSpace(attemptIDs[0])
	}
	s.recordTaskEventForAttempt(updated, pieceID, attemptID, kind, actor, summary, "")
}

// noteNodeProgress refreshes a plan node's real-headway signal — LastProgressAt
// AND LastActivityAt (progress implies the node is also alive), the stronger of
// the two liveness signals (plan §T9). Progress is a declared step forward: a
// handoff filed on piece_done, a public kind=update posted into the task thread.
// It records one trace event (kind, e.g. node_progress). The caller passes the
// task it already loaded; it no-ops on an empty pieceID. Artifact-write progress
// is deliberately OUT OF SCOPE here — it crosses the tools/appserver boundary —
// so this covers task-update and handoff progress only.
func (s *Server) noteNodeProgress(task session.ConversationThread, pieceID, kind, summary string, attemptIDs ...string) {
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
	attemptID := ""
	if len(attemptIDs) > 0 {
		attemptID = strings.TrimSpace(attemptIDs[0])
	} else if p := findPieceByID(task.Plan, pieceID); p != nil {
		attemptID = p.CurrentAttemptID
	}
	s.recordTaskEventForAttempt(updated, pieceID, attemptID, kind, actor, summary, "")
}

// wakePieceText builds the wake envelope for a dispatched piece from the two
// inputs a node runs on: the lead's per-node Prompt (the briefing authored in
// set_plan) and, when an upstream handed this node a payload, the rendered
// handoff (this node's real input). The handoff-carrying wake is the ONLY thing
// that wakes the downstream — a public thread update never is. If several
// upstreams wrote a handoff onto this node, the last writer's payload is what
// piece.Handoff holds here (last-writer-wins for the wake text); the trace
// retains every handoff_created event.
func wakePieceText(task session.ConversationThread, piece session.TaskPiece, attempt session.TaskAttempt) string {
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
		"\n\nDo it as attempt %q, working in this task thread (thread_id=%q). Your node completes when your turn ends, so you do not need piece_done to finish it — use manage_task action=piece_done piece_id=%q to finish early or to hand a structured result to the next node in the handoff. If you are blocked, use need_human / need_upstream before ending. Post here only what a teammate needs — @ them if you need something.",
		attempt.ID, task.ID, piece.ID,
	)
	return b.String()
}

// wakePieceAssignee @-wakes one assignee into the task thread to start a piece
// whose dependencies are satisfied. The directive is medium-agnostic — it names
// the piece, not a code action — so the same wake serves research, documents,
// and code.
func (s *Server) wakePieceAssignee(task session.ConversationThread, piece session.TaskPiece, attempt session.TaskAttempt) {
	title, workspace := s.taskThreadContext(task.SessionID)
	text := wakePieceText(task, piece, attempt)
	s.recordTaskEventForAttempt(task, piece.ID, attempt.ID, session.TaskEventNodeStarted, piece.Assignee,
		fmt.Sprintf("node started: %q", piece.Title), "")
	s.deliverEnvelopeToMembers([]string{piece.Assignee}, MessageEnvelope{
		SourceThreadID:      task.SessionID,
		SourceSubthreadID:   task.ID,
		TaskAttemptID:       attempt.ID,
		TaskNodeID:          piece.ID,
		SourceTitle:         title,
		SenderKind:          "system",
		SenderName:          "task plan",
		SenderParticipantID: "",
		Text:                text,
		CreatedAt:           time.Now().UTC(),
		Workspace:           workspace,
	}, nil, true)
}

// wakePlanLead wakes the immutable lead after the engine has atomically entered
// awaiting_lead. There is no creator fallback because only the persisted lead
// holds verification and conclusion authority.
func (s *Server) wakePlanLead(task session.ConversationThread) {
	lead := strings.TrimSpace(task.LeadParticipantID)
	if lead == "" {
		return
	}
	title, workspace := s.taskThreadContext(task.SessionID)
	s.recordTaskEventFor(task, "", session.TaskEventLeadInvoked, lead,
		"all pieces done — lead woken to wrap up", "")
	text := fmt.Sprintf(
		"Every piece of task %q is done. Review the trace and handoffs, then file the verified conclusion with manage_task action=conclude subthread_id=%q. Do not execute task work yourself.",
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
		"You lead task %q. Orchestrate it now with manage_task action=set_plan subthread_id=%q and pieces {id, title, assignee, prompt, depends_on}. Promotion does not broaden authorization: preserve every scope limit from the DM, anchor, and Thread in each worker prompt (investigate-only never becomes implementation). Use the smallest outcome-shaped graph that completes exactly what the user authorized; do not invent architecture or extra phase ceremony. Each prompt must let its assignee start without asking. Assign only other named agents: a lead coordinates, observes, revises, and concludes; it never executes a piece itself.",
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
// flag the human (need_human), or conclude with verified evidence — after reading
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
		"Node %q of task %q failed and its retries are spent: %s. The task is paused pending your decision. Read its trace, then revise the plan, flag a genuinely human decision with manage_task action=need_human subthread_id=%q, or conclude with manage_task action=conclude subthread_id=%q. Do not execute the failed work yourself.",
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
func (s *Server) wakeUpstreamForFallback(task session.ConversationThread, upstream session.TaskPiece, attempt session.TaskAttempt, downstreamTitle, reason string) {
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
		TaskAttemptID:       attempt.ID,
		TaskNodeID:          upstream.ID,
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
// Only human and named-participant messages may anchor a Thread. Synthetic,
// task-card, tool, reasoning, and other rendered items return false, as do
// forged or not-yet-persisted ids.
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
				if item.Type != ThreadItemUserMessage && item.Type != ThreadItemParticipantMsg {
					return 0, "", false
				}
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
