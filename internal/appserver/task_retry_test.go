package appserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/tools"
)

// Retry budget + failure / fallback / replan (plan §T8). A plan node that dies
// with a transient runtime failure is retried against its budget; an exhausted
// budget (or a hard failure retrying cannot fix) fails the node, blocks the
// task, and wakes the lead; a user interrupt (cancelled) is left alone; and an
// assignee whose upstream handoff is insufficient bounces the work back to the
// upstream node with need_upstream.

// taskPlanDispatchEnv is the shape of a plan-dispatch wake (system "task plan"
// carrying the task cth id) — the batch signature the turn-failure hook keys on.
func taskPlanDispatchEnv(taskID string) MessageEnvelope {
	return MessageEnvelope{SenderKind: "system", SenderName: "task plan", SourceSubthreadID: taskID}
}

func synthFailedTurn(category, message string) Turn {
	return Turn{Status: TurnStatusFailed, Error: &TurnError{Category: category, Message: message}}
}

func loadPiece(t *testing.T, srv *Server, taskID, pieceID string) session.TaskPiece {
	t.Helper()
	th, err := session.FindConversationThreadByID(srv.rt.SessionDir, taskID)
	if err != nil {
		t.Fatalf("load task %q: %v", taskID, err)
	}
	for _, p := range th.Plan {
		if p.ID == pieceID {
			return p
		}
	}
	t.Fatalf("task %q has no piece %q", taskID, pieceID)
	return session.TaskPiece{}
}

func countTaskEventKind(t *testing.T, srv *Server, taskID, kind string) int {
	t.Helper()
	events, err := session.TaskEvents(srv.rt.SessionDir, taskID)
	if err != nil {
		t.Fatalf("TaskEvents %q: %v", taskID, err)
	}
	n := 0
	for _, ev := range events {
		if ev.Kind == kind {
			n++
		}
	}
	return n
}

func findTaskEventForNode(t *testing.T, srv *Server, taskID, kind, nodeID string) (session.TaskEvent, bool) {
	t.Helper()
	events, err := session.TaskEvents(srv.rt.SessionDir, taskID)
	if err != nil {
		t.Fatalf("TaskEvents %q: %v", taskID, err)
	}
	for _, ev := range events {
		if ev.Kind == kind && (nodeID == "" || ev.NodeID == nodeID) {
			return ev, true
		}
	}
	return session.TaskEvent{}, false
}

func waitForPieceState(t *testing.T, srv *Server, taskID, pieceID, status string, attempts int) session.TaskPiece {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last session.TaskPiece
	for time.Now().Before(deadline) {
		last = loadPiece(t, srv, taskID, pieceID)
		if last.Status == status && last.Attempts == attempts {
			return last
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("piece %q never reached status=%q attempts=%d (last status=%q attempts=%d)", pieceID, status, attempts, last.Status, last.Attempts)
	return session.TaskPiece{}
}

func waitForExecState(t *testing.T, srv *Server, taskID, state string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	last := ""
	for time.Now().Before(deadline) {
		th, err := session.FindConversationThreadByID(srv.rt.SessionDir, taskID)
		if err != nil {
			t.Fatalf("load task %q: %v", taskID, err)
		}
		last = th.ExecState
		if last == state {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %q exec state never reached %q (last %q)", taskID, state, last)
}

// A retryable runtime failure with budget left retries the same node: one
// attempt is spent, the node is marked retrying, a retrying trace is recorded,
// and the assignee is re-dispatched (a second node_started).
func TestRetryableFailureRetriesWithinBudget(t *testing.T) {
	srv, groupID, andy, mia, _, _ := planFixture(t)
	lead := srv.residentTaskManager(andy)
	task, err := createPromotedTaskForTest(context.Background(), lead, groupID, "重试预算")
	if err != nil {
		t.Fatalf("createPromotedTaskForTest: %v", err)
	}
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "跑一段活", Assignee: mia},
	}); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	// Dispatch stamps the first attempt.
	if p := loadPiece(t, srv, task.ID, "p1"); p.Status != session.TaskPieceActive || p.Attempts != 1 {
		t.Fatalf("after dispatch p1 = %q attempts=%d, want active attempts 1", p.Status, p.Attempts)
	}
	nodeStartedBefore := countTaskEventKind(t, srv, task.ID, session.TaskEventNodeStarted)

	srv.retryOrFailTaskNodesAfterTurn(mia, []MessageEnvelope{taskPlanDispatchEnv(task.ID)},
		synthFailedTurn("network", "connection reset by peer"))

	p := loadPiece(t, srv, task.ID, "p1")
	if p.Status != session.TaskPieceRetrying {
		t.Fatalf("p1 status = %q, want retrying", p.Status)
	}
	if p.Attempts != 2 {
		t.Fatalf("p1 attempts = %d, want 2", p.Attempts)
	}
	if !strings.Contains(p.FailureReason, "connection reset") {
		t.Fatalf("p1 failure reason = %q, want the runtime error", p.FailureReason)
	}
	ev, ok := findTaskEventForNode(t, srv, task.ID, session.TaskEventRetrying, "p1")
	if !ok {
		t.Fatal("expected a retrying trace event for p1")
	}
	if !strings.Contains(ev.Summary, "2/3") {
		t.Fatalf("retrying trace summary = %q, want the attempt count", ev.Summary)
	}
	// The re-dispatch recorded a second node_started (assignee re-woken).
	if got := countTaskEventKind(t, srv, task.ID, session.TaskEventNodeStarted); got <= nodeStartedBefore {
		t.Fatalf("node_started events = %d, want > %d (re-dispatch)", got, nodeStartedBefore)
	}
	waitForResidentDMHistoryContains(t, srv, mia, "跑一段活")
}

// A user interrupt (cancelled) leaves the node exactly as it was: no attempt is
// spent, no trace is written, nobody is woken. The user chose to stop.
func TestCancelledTurnLeavesNodeUntouched(t *testing.T) {
	srv, groupID, andy, mia, _, _ := planFixture(t)
	lead := srv.residentTaskManager(andy)
	task, err := createPromotedTaskForTest(context.Background(), lead, groupID, "用户中断")
	if err != nil {
		t.Fatalf("createPromotedTaskForTest: %v", err)
	}
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "被打断的活", Assignee: mia},
	}); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	before := loadPiece(t, srv, task.ID, "p1")
	retryingBefore := countTaskEventKind(t, srv, task.ID, session.TaskEventRetrying)
	failedBefore := countTaskEventKind(t, srv, task.ID, session.TaskEventNodeFailed)
	leadBefore := countTaskEventKind(t, srv, task.ID, session.TaskEventLeadInvoked)

	srv.retryOrFailTaskNodesAfterTurn(mia, []MessageEnvelope{taskPlanDispatchEnv(task.ID)},
		synthFailedTurn("cancelled", "user interrupted the turn"))

	after := loadPiece(t, srv, task.ID, "p1")
	if after.Status != before.Status || after.Attempts != before.Attempts || after.FailureReason != before.FailureReason {
		t.Fatalf("cancelled must leave the node untouched: before {status:%q attempts:%d reason:%q} after {status:%q attempts:%d reason:%q}",
			before.Status, before.Attempts, before.FailureReason, after.Status, after.Attempts, after.FailureReason)
	}
	if got := countTaskEventKind(t, srv, task.ID, session.TaskEventRetrying); got != retryingBefore {
		t.Fatalf("cancelled wrote a retrying trace: %d -> %d", retryingBefore, got)
	}
	if got := countTaskEventKind(t, srv, task.ID, session.TaskEventNodeFailed); got != failedBefore {
		t.Fatalf("cancelled wrote a node_failed trace: %d -> %d", failedBefore, got)
	}
	if got := countTaskEventKind(t, srv, task.ID, session.TaskEventLeadInvoked); got != leadBefore {
		t.Fatalf("cancelled woke the lead: %d -> %d lead_invoked", leadBefore, got)
	}
}

// A hard failure retrying cannot fix (auth) fails the node immediately —
// without spending a retry — blocks the task, and wakes the lead.
func TestHardFailureFailsNodeWithoutSpendingBudget(t *testing.T) {
	srv, groupID, andy, mia, _, _ := planFixture(t)
	lead := srv.residentTaskManager(andy)
	task, err := createPromotedTaskForTest(context.Background(), lead, groupID, "硬失败")
	if err != nil {
		t.Fatalf("createPromotedTaskForTest: %v", err)
	}
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "缺凭证的活", Assignee: mia},
	}); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	before := loadPiece(t, srv, task.ID, "p1")

	srv.retryOrFailTaskNodesAfterTurn(mia, []MessageEnvelope{taskPlanDispatchEnv(task.ID)},
		synthFailedTurn("auth", "401 unauthorized"))

	p := loadPiece(t, srv, task.ID, "p1")
	if p.Status != session.TaskPieceFailed {
		t.Fatalf("p1 status = %q, want failed", p.Status)
	}
	if p.Attempts != before.Attempts {
		t.Fatalf("a hard failure must not spend budget: attempts %d -> %d", before.Attempts, p.Attempts)
	}
	th, err := session.FindConversationThreadByID(srv.rt.SessionDir, task.ID)
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	if th.ExecState != session.ExecStateBlocked {
		t.Fatalf("exec state = %q, want blocked", th.ExecState)
	}
	if _, ok := findTaskEventForNode(t, srv, task.ID, session.TaskEventNodeFailed, "p1"); !ok {
		t.Fatal("expected a node_failed trace for the hard failure")
	}
	hist := waitForResidentDMHistoryContains(t, srv, andy, "retries are spent")
	if !strings.Contains(historyUserContent(hist), "缺凭证的活") {
		t.Fatal("lead failure wake should name the failed node")
	}
}

// A real failed turn drives the whole retry budget through the drain: the
// dispatched node's turn keeps hitting a retryable provider failure, so the
// engine retries it until the budget is spent, then fails the node, blocks the
// task, records node_failed, and wakes the lead — no synthesized turn.
func TestBudgetExhaustionThroughRealFailedTurns(t *testing.T) {
	client := &fakeClient{
		err:      &providers.HTTPError{StatusCode: 503, Body: "Service Unavailable"},
		response: providersResponse(""),
	}
	rt := newTestRuntime(t, client)
	srv := New(rt, &lockedBuffer{})
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	andy := saveNamedParticipant(t, rt, "Andy", "lead", "")
	mia := saveNamedParticipant(t, rt, "Mia", "worker", "")
	groupID := startNamedGroupThreadForTest(t, srv, "retry-drive").ID
	for _, id := range []string{andy, mia} {
		if err := session.AddThreadMember(rt.SessionDir, groupID, id); err != nil {
			t.Fatalf("AddThreadMember: %v", err)
		}
	}
	lead := srv.residentTaskManager(andy)
	task, err := createPromotedTaskForTest(context.Background(), lead, groupID, "真实失败重试")
	if err != nil {
		t.Fatalf("createPromotedTaskForTest: %v", err)
	}
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "会断网的活", Assignee: mia},
	}); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}

	// Every dispatched turn hits the 503 (a network-category failure). The node
	// is retried until its default budget of 3 attempts is spent, then failed
	// and the task blocked.
	p := waitForPieceState(t, srv, task.ID, "p1", session.TaskPieceFailed, 3)
	if strings.TrimSpace(p.FailureReason) == "" {
		t.Fatal("a failed node should carry the runtime failure reason")
	}
	waitForExecState(t, srv, task.ID, session.ExecStateBlocked)
	// Two retries preceded the terminal failure.
	if got := countTaskEventKind(t, srv, task.ID, session.TaskEventRetrying); got != 2 {
		t.Fatalf("retrying traces = %d, want 2 (attempts 2 and 3)", got)
	}
	ev, ok := findTaskEventForNode(t, srv, task.ID, session.TaskEventNodeFailed, "p1")
	if !ok {
		t.Fatal("expected a node_failed trace after budget exhaustion")
	}
	if !strings.Contains(ev.Payload, "network") {
		t.Fatalf("node_failed payload = %q, want the network category", ev.Payload)
	}
	// The lead is woken to recover (its own DM turn also fails under the
	// persistent error, but the wake message still landed in its history).
	waitForResidentDMHistoryContains(t, srv, andy, "retries are spent")
}

// need_upstream is the assignee's fallback: a downstream node whose upstream
// handoff is insufficient bounces the work back rather than working around it.
// The downstream parks to pending, the upstream re-activates and is re-woken
// with the feedback, and when the upstream re-files piece_done the engine
// re-dispatches the downstream.
func TestNeedUpstreamBouncesWorkBackToUpstream(t *testing.T) {
	srv, groupID, andy, _, han, vera := planFixture(t)
	lead := srv.residentTaskManager(andy)
	task, err := createPromotedTaskForTest(context.Background(), lead, groupID, "上下游回退")
	if err != nil {
		t.Fatalf("createPromotedTaskForTest: %v", err)
	}
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "上游产出", Assignee: han, Prompt: "起草接口"},
		{ID: "p2", Title: "下游消费", Assignee: vera, DependsOn: []string{"p1"}},
	}); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	waitForResidentDMHistoryContains(t, srv, han, "上游产出")

	// Han finishes p1 → the engine dispatches p2 to Vera.
	afterP1, err := srv.residentTaskManager(han).PieceDone(context.Background(), task.ID, "p1",
		&tools.TaskHandoff{Done: "草稿", NextGoal: "验证"})
	if err != nil {
		t.Fatalf("Han piece_done p1: %v", err)
	}
	if got := pieceStatus(afterP1, "p2"); got != session.TaskPieceActive {
		t.Fatalf("p2 status = %q after p1 done, want active", got)
	}
	waitForResidentDMHistoryContains(t, srv, vera, "下游消费")

	// Vera finds the handoff insufficient and bounces the work upstream.
	reason := "缺少接口契约细节"
	view, err := srv.residentTaskManager(vera).NeedUpstream(context.Background(), task.ID, "p2", reason)
	if err != nil {
		t.Fatalf("Vera need_upstream: %v", err)
	}
	if got := pieceStatus(view, "p2"); got != session.TaskPiecePending {
		t.Fatalf("p2 status = %q after need_upstream, want pending", got)
	}
	if got := pieceStatus(view, "p1"); got != session.TaskPieceActive {
		t.Fatalf("p1 status = %q after need_upstream, want active", got)
	}
	if p2 := loadPiece(t, srv, task.ID, "p2"); p2.FailureReason != reason {
		t.Fatalf("p2 failure reason = %q, want %q", p2.FailureReason, reason)
	}
	if p1 := loadPiece(t, srv, task.ID, "p1"); p1.Attempts < 1 {
		t.Fatalf("re-activated upstream p1 attempts = %d, want >= 1", p1.Attempts)
	}
	// The upstream (Han) is re-woken with the downstream's feedback.
	hist := waitForResidentDMHistoryContains(t, srv, han, reason)
	if !strings.Contains(historyUserContent(hist), "下游消费") {
		t.Fatal("the fallback wake should name the downstream node")
	}
	// A blocked trace on the downstream (with the reason + upstream ids) and a
	// retrying trace on the upstream.
	blocked, ok := findTaskEventForNode(t, srv, task.ID, session.TaskEventBlocked, "p2")
	if !ok {
		t.Fatal("expected a blocked trace on the downstream")
	}
	if !strings.Contains(blocked.Payload, "p1") || !strings.Contains(blocked.Payload, reason) {
		t.Fatalf("blocked payload = %q, want the reason and upstream ids", blocked.Payload)
	}
	if _, ok := findTaskEventForNode(t, srv, task.ID, session.TaskEventRetrying, "p1"); !ok {
		t.Fatal("expected a retrying trace on the re-opened upstream")
	}

	// Han revises and re-files p1 → the engine re-dispatches the now-ready p2.
	afterRedo, err := srv.residentTaskManager(han).PieceDone(context.Background(), task.ID, "p1",
		&tools.TaskHandoff{Done: "补齐契约", NextGoal: "验证"})
	if err != nil {
		t.Fatalf("Han re-file piece_done p1: %v", err)
	}
	if got := pieceStatus(afterRedo, "p2"); got != session.TaskPieceActive {
		t.Fatalf("p2 status = %q after upstream re-filed, want active (re-dispatched)", got)
	}
}
