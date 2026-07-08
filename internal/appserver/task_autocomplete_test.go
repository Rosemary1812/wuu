package appserver

import (
	"context"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/tools"
)

// Turn-end completion capture (design: a node's COMPLETION is auto-captured at
// turn end, the way a deterministic workflow captures an agent's final output as
// its return value). A COMPLETED plan-dispatch turn advances the plan even
// without piece_done: the node it was dispatched for completes now unless the
// agent gave an explicit blocked signal (a kind=question, or need_human /
// need_upstream, which move the node/task out of the completing snapshot). These
// drive autoCompleteTaskNodesAfterTurn directly with a synthesized snapshot +
// turn — the same deterministic style the failure-hook tests use — because the
// DM toolkit is nil under test, so the background dispatch drain never runs the
// live snapshot path.

// synthCompletedTurn is a completed turn whose final visible output is finalText
// (an agent_message item), the raw material for a synthesized handoff.
func synthCompletedTurn(finalText string) Turn {
	turn := Turn{Status: TurnStatusCompleted}
	if strings.TrimSpace(finalText) != "" {
		turn.Items = []ThreadItem{{Type: ThreadItemAgentMessage, Text: finalText}}
	}
	return turn
}

func taskMembers(t *testing.T, srv *Server, taskID string) map[string]bool {
	t.Helper()
	members, err := session.ListConversationThreadMembers(srv.rt.SessionDir, taskID)
	if err != nil {
		t.Fatalf("ListConversationThreadMembers %q: %v", taskID, err)
	}
	out := make(map[string]bool, len(members))
	for _, m := range members {
		out[m] = true
	}
	return out
}

// A completed single-node dispatch turn that filed no piece_done still completes
// the node: the node flips done, a Done-only handoff synthesized from the turn's
// final visible output lands on the dependent, the engine dispatches it, and the
// finished assignee is auto-unfollowed — exactly what piece_done would have done.
func TestCompletedTurnAutoCompletesNodeWithoutPieceDone(t *testing.T) {
	srv, groupID, andy, mia, han, _ := planFixture(t)
	lead := srv.residentTaskManager(andy)
	task, err := lead.CreateTask(context.Background(), groupID, 0, "自动完成", true, "")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "上游", Assignee: mia},
		{ID: "p2", Title: "下游", Assignee: han, DependsOn: []string{"p1"}},
	}); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	// Turn-start snapshot: Mia is dispatched to run p1.
	snapshot := srv.dispatchedNodesForTurn(mia, []MessageEnvelope{taskPlanDispatchEnv(task.ID)})
	if !snapshot[task.ID]["p1"] {
		t.Fatalf("snapshot should capture p1 as dispatched to Mia, got %v", snapshot)
	}

	// Mia's turn completes with only trailing assistant text and NO piece_done.
	srv.autoCompleteTaskNodesAfterTurn(mia, []MessageEnvelope{taskPlanDispatchEnv(task.ID)},
		synthCompletedTurn("接口草稿已完成，见下"), false, snapshot)

	if p := loadPiece(t, srv, task.ID, "p1"); p.Status != session.TaskPieceDone {
		t.Fatalf("p1 status = %q, want done (auto-completed at turn end)", p.Status)
	}
	// The plan advanced: the dependent p2 was dispatched.
	p2 := loadPiece(t, srv, task.ID, "p2")
	if p2.Status != session.TaskPieceActive {
		t.Fatalf("p2 status = %q, want active (advanced by turn-end completion)", p2.Status)
	}
	// A minimal handoff (Done = the turn's final output) landed on the dependent.
	if !strings.Contains(p2.Handoff, "接口草稿已完成") {
		t.Fatalf("p2 handoff = %q, want the synthesized Done text", p2.Handoff)
	}
	// The completing node recorded node_succeeded + handoff_created, exactly as
	// piece_done would.
	if _, ok := findTaskEventForNode(t, srv, task.ID, session.TaskEventNodeSucceeded, "p1"); !ok {
		t.Fatal("expected a node_succeeded trace for the auto-completed node")
	}
	if _, ok := findTaskEventForNode(t, srv, task.ID, session.TaskEventHandoffCreated, "p1"); !ok {
		t.Fatal("expected a handoff_created trace for the synthesized handoff")
	}
	// Mia has no remaining piece → auto-unfollowed; Han (the running dependent)
	// still follows.
	members := taskMembers(t, srv, task.ID)
	if members[mia] {
		t.Fatal("finished assignee Mia should be auto-unfollowed from the task")
	}
	if !members[han] {
		t.Fatal("the running dependent Han should still follow the task")
	}
}

// A completed turn with no final assistant text at all completes the node with a
// nil (input-less) handoff: the node flips done and the plan advances, and the
// dependent wakes on its briefing alone (no synthesized Done).
func TestCompletedTurnAutoCompletesWithNilHandoffWhenSilent(t *testing.T) {
	srv, groupID, andy, mia, han, _ := planFixture(t)
	lead := srv.residentTaskManager(andy)
	task, err := lead.CreateTask(context.Background(), groupID, 0, "静默完成", true, "")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "上游", Assignee: mia},
		{ID: "p2", Title: "下游", Assignee: han, DependsOn: []string{"p1"}},
	}); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	snapshot := srv.dispatchedNodesForTurn(mia, []MessageEnvelope{taskPlanDispatchEnv(task.ID)})

	// No agent_message items at all → finalAgentMessageText == "" → nil handoff.
	srv.autoCompleteTaskNodesAfterTurn(mia, []MessageEnvelope{taskPlanDispatchEnv(task.ID)},
		synthCompletedTurn(""), false, snapshot)

	if p := loadPiece(t, srv, task.ID, "p1"); p.Status != session.TaskPieceDone {
		t.Fatalf("p1 status = %q, want done", p.Status)
	}
	p2 := loadPiece(t, srv, task.ID, "p2")
	if p2.Status != session.TaskPieceActive {
		t.Fatalf("p2 status = %q, want active", p2.Status)
	}
	if strings.TrimSpace(p2.Handoff) != "" {
		t.Fatalf("p2 handoff = %q, want empty (silent completion carries no input)", p2.Handoff)
	}
}

// F1 regression: a piece_done that activates a SAME-OWNER downstream in-turn must
// NOT auto-complete that downstream. The turn-start snapshot holds only the piece
// the turn was dispatched for (p1); the piece_done ran its own completion, and the
// downstream p2 it activated is dispatched to a LATER turn — never captured here.
func TestAutoCompleteSkipsSameOwnerDownstreamActivatedInTurn(t *testing.T) {
	srv, groupID, andy, mia, _, _ := planFixture(t)
	lead := srv.residentTaskManager(andy)
	task, err := lead.CreateTask(context.Background(), groupID, 0, "同人上下游", true, "")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// A completely ordinary plan: research then write, both by Mia.
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "调研", Assignee: mia},
		{ID: "p2", Title: "撰写", Assignee: mia, DependsOn: []string{"p1"}},
	}); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	// Snapshot at turn start: only p1 is active/dispatched to Mia.
	snapshot := srv.dispatchedNodesForTurn(mia, []MessageEnvelope{taskPlanDispatchEnv(task.ID)})
	if snapshot[task.ID]["p2"] {
		t.Fatalf("snapshot must not contain the pending downstream p2: %v", snapshot)
	}

	// Mia's turn ran p1 and filed piece_done(p1) — which durably activated p2
	// (same owner) for a future turn.
	if _, err := srv.residentTaskManager(mia).PieceDone(context.Background(), task.ID, "p1",
		&tools.TaskHandoff{Done: "调研结论", NextGoal: "撰写"}); err != nil {
		t.Fatalf("Mia piece_done p1: %v", err)
	}
	if got := loadPiece(t, srv, task.ID, "p2").Status; got != session.TaskPieceActive {
		t.Fatalf("after piece_done p1, p2 = %q, want active (dispatched for a later turn)", got)
	}

	// Turn ends. Auto-complete must complete NOTHING new: p1 is already done, and
	// p2 was not in the snapshot.
	srv.autoCompleteTaskNodesAfterTurn(mia, []MessageEnvelope{taskPlanDispatchEnv(task.ID)},
		synthCompletedTurn("撰写ing"), false, snapshot)

	if got := loadPiece(t, srv, task.ID, "p2").Status; got != session.TaskPieceActive {
		t.Fatalf("p2 status = %q, want STILL active — it was never run this turn and must not auto-complete", got)
	}
	// p2 keeps the upstream's real handoff, not a bogus synthesized one.
	if p2 := loadPiece(t, srv, task.ID, "p2"); !strings.Contains(p2.Handoff, "调研结论") {
		t.Fatalf("p2 handoff = %q, want the upstream's real handoff intact", p2.Handoff)
	}
}

// F3 regression: when the same agent owns an upstream+downstream and the
// downstream calls need_upstream, the reactivated upstream must NOT be
// auto-completed. It was not Active at turn start (the turn ran the downstream),
// so it is not in the snapshot; the parked downstream is pending, not active.
func TestAutoCompleteSkipsUpstreamReactivatedByNeedUpstream(t *testing.T) {
	srv, groupID, andy, mia, _, _ := planFixture(t)
	lead := srv.residentTaskManager(andy)
	task, err := lead.CreateTask(context.Background(), groupID, 0, "同人回退", true, "")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "上游产出", Assignee: mia, Prompt: "起草"},
		{ID: "p2", Title: "下游消费", Assignee: mia, DependsOn: []string{"p1"}},
	}); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	// Finish p1 so p2 is dispatched (both owned by Mia).
	if _, err := srv.residentTaskManager(mia).PieceDone(context.Background(), task.ID, "p1",
		&tools.TaskHandoff{Done: "草稿"}); err != nil {
		t.Fatalf("Mia piece_done p1: %v", err)
	}
	// Turn-start snapshot for the p2 turn: only p2 is active/dispatched to Mia.
	snapshot := srv.dispatchedNodesForTurn(mia, []MessageEnvelope{taskPlanDispatchEnv(task.ID)})
	if !snapshot[task.ID]["p2"] || snapshot[task.ID]["p1"] {
		t.Fatalf("snapshot should hold only p2 for the downstream turn, got %v", snapshot)
	}

	// Mia (on p2) finds the handoff insufficient and bounces it back: p2 parks to
	// pending, p1 re-activates (still Mia).
	if _, err := srv.residentTaskManager(mia).NeedUpstream(context.Background(), task.ID, "p2", "缺少契约"); err != nil {
		t.Fatalf("Mia need_upstream p2: %v", err)
	}
	if got := loadPiece(t, srv, task.ID, "p1").Status; got != session.TaskPieceActive {
		t.Fatalf("after need_upstream, p1 = %q, want active (re-opened upstream)", got)
	}

	// Turn ends. The reactivated upstream p1 must NOT be auto-completed (it is not
	// in the snapshot), and the parked downstream p2 (now pending) must not either.
	srv.autoCompleteTaskNodesAfterTurn(mia, []MessageEnvelope{taskPlanDispatchEnv(task.ID)},
		synthCompletedTurn("bounced"), false, snapshot)

	if got := loadPiece(t, srv, task.ID, "p1").Status; got != session.TaskPieceActive {
		t.Fatalf("p1 status = %q, want STILL active — the reactivated upstream must re-run, not auto-complete", got)
	}
	if got := loadPiece(t, srv, task.ID, "p2").Status; got != session.TaskPiecePending {
		t.Fatalf("p2 status = %q, want STILL pending", got)
	}
}

// F2 regression: lead-management wakes reuse the "task plan" envelope, so a lead's
// recovery turn is a plan-dispatch turn. A node runs only while the task is
// executing; a blocked task (a sibling failed) is skipped, so the lead's untouched
// active sibling is NOT auto-completed by the lead's recovery turn.
func TestAutoCompleteSkipsLeadRecoveryTurnOnBlockedTask(t *testing.T) {
	srv, groupID, andy, mia, _, _ := planFixture(t)
	lead := srv.residentTaskManager(andy)
	task, err := lead.CreateTask(context.Background(), groupID, 0, "领导恢复", true, "")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// Two parallel pieces: p1 to the lead (Andy), p2 to Mia.
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "领导的活", Assignee: andy},
		{ID: "p2", Title: "队友的活", Assignee: mia},
	}); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	if got := loadPiece(t, srv, task.ID, "p1").Status; got != session.TaskPieceActive {
		t.Fatalf("p1 = %q, want active", got)
	}
	// p2 fails terminally (a hard failure) → the task blocks and the lead is woken.
	srv.retryOrFailTaskNodesAfterTurn(mia, []MessageEnvelope{taskPlanDispatchEnv(task.ID)},
		synthFailedTurn("auth", "401 unauthorized"))
	waitForExecState(t, srv, task.ID, session.ExecStateBlocked)

	// The lead's recovery turn carries the same "task plan" envelope. Its snapshot
	// is empty (a blocked task contributes no dispatched node), and the hook's
	// executing-only guard skips it too.
	snapshot := srv.dispatchedNodesForTurn(andy, []MessageEnvelope{taskPlanDispatchEnv(task.ID)})
	if len(snapshot) != 0 {
		t.Fatalf("a blocked task must contribute no dispatched nodes, got %v", snapshot)
	}
	srv.autoCompleteTaskNodesAfterTurn(andy, []MessageEnvelope{taskPlanDispatchEnv(task.ID)},
		synthCompletedTurn("我在处理"), false, snapshot)

	if got := loadPiece(t, srv, task.ID, "p1").Status; got != session.TaskPieceActive {
		t.Fatalf("p1 status = %q, want STILL active — the lead's recovery turn must not complete its sibling", got)
	}
}

// A completed turn that posted a kind=question (askedQuestion) is waiting, not
// finished: its node is NOT auto-completed and stays open.
func TestAutoCompleteSkipsWhenTurnAskedQuestion(t *testing.T) {
	srv, groupID, andy, mia, _, _ := planFixture(t)
	lead := srv.residentTaskManager(andy)
	task, err := lead.CreateTask(context.Background(), groupID, 0, "提问阻塞", true, "")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "需要澄清", Assignee: mia},
	}); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	snapshot := srv.dispatchedNodesForTurn(mia, []MessageEnvelope{taskPlanDispatchEnv(task.ID)})

	srv.autoCompleteTaskNodesAfterTurn(mia, []MessageEnvelope{taskPlanDispatchEnv(task.ID)},
		synthCompletedTurn("我需要更多信息"), true /* askedQuestion */, snapshot)

	if got := loadPiece(t, srv, task.ID, "p1").Status; got != session.TaskPieceActive {
		t.Fatalf("p1 status = %q, want STILL active — a question is a blocked signal, not completion", got)
	}
}

// A task moved out of executing this turn (need_human -> needs_human) is not the
// turn's to complete: the hook's executing-only guard skips it even when the
// snapshot still names the piece.
func TestAutoCompleteSkipsWhenTaskLeftExecuting(t *testing.T) {
	srv, groupID, andy, mia, _, _ := planFixture(t)
	lead := srv.residentTaskManager(andy)
	task, err := lead.CreateTask(context.Background(), groupID, 0, "转人类", true, "")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "要人类决策", Assignee: mia},
	}); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	// Snapshot captures p1 while the task is still executing.
	snapshot := srv.dispatchedNodesForTurn(mia, []MessageEnvelope{taskPlanDispatchEnv(task.ID)})
	if !snapshot[task.ID]["p1"] {
		t.Fatalf("snapshot should capture p1, got %v", snapshot)
	}
	// The turn flagged the task for the human, moving it out of executing.
	if _, err := srv.residentTaskManager(mia).NeedHuman(context.Background(), task.ID, "预算超限"); err != nil {
		t.Fatalf("Mia need_human: %v", err)
	}

	srv.autoCompleteTaskNodesAfterTurn(mia, []MessageEnvelope{taskPlanDispatchEnv(task.ID)},
		synthCompletedTurn("等待决策"), false, snapshot)

	if got := loadPiece(t, srv, task.ID, "p1").Status; got != session.TaskPieceActive {
		t.Fatalf("p1 status = %q, want STILL active — a needs_human task's node is not the turn's to complete", got)
	}
	th, err := session.FindConversationThreadByID(srv.rt.SessionDir, task.ID)
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	if th.ExecState != session.ExecStateNeedsHuman {
		t.Fatalf("task exec state = %q, want needs_human", th.ExecState)
	}
}

// A piece already completed via piece_done this turn is a no-op for turn-end
// capture: the snapshot piece is no longer Active/Retrying, so it is not
// re-completed and records no duplicate node_succeeded event.
func TestAutoCompleteNoOpWhenPieceAlreadyDoneViaPieceDone(t *testing.T) {
	srv, groupID, andy, mia, _, _ := planFixture(t)
	lead := srv.residentTaskManager(andy)
	task, err := lead.CreateTask(context.Background(), groupID, 0, "已经完成", true, "")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "一次搞定", Assignee: mia},
	}); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	snapshot := srv.dispatchedNodesForTurn(mia, []MessageEnvelope{taskPlanDispatchEnv(task.ID)})

	// The agent filed piece_done mid-turn (the rich, early path).
	if _, err := srv.residentTaskManager(mia).PieceDone(context.Background(), task.ID, "p1",
		&tools.TaskHandoff{Done: "结果"}); err != nil {
		t.Fatalf("Mia piece_done p1: %v", err)
	}
	succeededBefore := countTaskEventKind(t, srv, task.ID, session.TaskEventNodeSucceeded)

	srv.autoCompleteTaskNodesAfterTurn(mia, []MessageEnvelope{taskPlanDispatchEnv(task.ID)},
		synthCompletedTurn("结果"), false, snapshot)

	if got := loadPiece(t, srv, task.ID, "p1").Status; got != session.TaskPieceDone {
		t.Fatalf("p1 status = %q, want done", got)
	}
	if got := countTaskEventKind(t, srv, task.ID, session.TaskEventNodeSucceeded); got != succeededBefore {
		t.Fatalf("node_succeeded events = %d, want unchanged %d (no double completion)", got, succeededBefore)
	}
}
