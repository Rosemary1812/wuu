package appserver

import (
	"context"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/tools"
)

// Team-task plan engine (task-rail design §8.2): the lead declares a plan of
// pieces with dependencies; the engine dispatches ready pieces by @-waking
// their assignees, advances on each piece_done, and wakes the lead when all
// pieces are done. This pins the M2 shape — three independent pieces plus one
// that fans in on all three — which is exactly what stalled under the flat
// claim model (Vera claimed the verify task and waited forever).

func planFixture(t *testing.T) (srv *Server, groupID, andy, mia, han, vera string) {
	t.Helper()
	// A woken plan assignee must COMPLETE its turn: an empty model answer with
	// no stop reason classifies as a retryable runtime failure (agentcontrol),
	// which the plan engine now (plan §T8) retries. A benign non-empty answer
	// completes the turn so the dispatch is a clean no-op — plan wakes are
	// never addressed, so a completed turn posts no fallback either.
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("ok")})
	srv = New(rt, &lockedBuffer{})
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	andy = saveNamedParticipant(t, rt, "Andy", "lead", "")
	mia = saveNamedParticipant(t, rt, "Mia", "mobile", "")
	han = saveNamedParticipant(t, rt, "Han", "backend", "")
	vera = saveNamedParticipant(t, rt, "Vera", "qa", "")
	groupID = startNamedGroupThreadForTest(t, srv, "m2").ID
	for _, id := range []string{andy, mia, han, vera} {
		if err := session.AddThreadMember(rt.SessionDir, groupID, id); err != nil {
			t.Fatalf("AddThreadMember: %v", err)
		}
	}
	return srv, groupID, andy, mia, han, vera
}

func pieceStatus(view tools.TaskView, id string) string {
	for _, p := range view.Plan {
		if p.ID == id {
			return p.Status
		}
	}
	return ""
}

func TestTaskPlanFanInDispatchAndHandoff(t *testing.T) {
	srv, groupID, andy, mia, han, vera := planFixture(t)
	lead := srv.residentTaskManager(andy)

	// Andy (lead) creates the team task and declares the plan: p1/p2/p3 are
	// independent; p4 (verify) depends on all three.
	task, err := lead.CreateTask(context.Background(), groupID, 0, "M2 移动推送", true, "")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	plan := []tools.TaskPiece{
		{ID: "p1", Title: "推送协议+host派发", Assignee: han},
		{ID: "p2", Title: "mobile注册+深链", Assignee: mia},
		{ID: "p3", Title: "息屏重连", Assignee: mia},
		{ID: "p4", Title: "真机端到端验证", Assignee: vera, DependsOn: []string{"p1", "p2", "p3"}},
	}
	view, err := lead.SetPlan(context.Background(), task.ID, plan)
	if err != nil {
		t.Fatalf("SetPlan: %v", err)
	}

	// After set_plan: the three independent pieces are dispatched (active),
	// their assignees woken; p4 stays pending (deps unmet). Vera is NOT woken.
	for _, id := range []string{"p1", "p2", "p3"} {
		if got := pieceStatus(view, id); got != session.TaskPieceActive {
			t.Fatalf("piece %s status = %q, want active after set_plan", id, got)
		}
	}
	if got := pieceStatus(view, "p4"); got != session.TaskPiecePending {
		t.Fatalf("p4 status = %q, want pending (deps unmet)", got)
	}
	// Han and Mia were @-woken; Vera was not (her piece is gated).
	waitForResidentDMHistoryContains(t, srv, han, "推送协议+host派发")
	waitForResidentDMHistoryContains(t, srv, mia, "mobile注册+深链")
	if pending, err := session.PendingResidentEnvelopes(srv.rt.SessionDir, vera, 0); err != nil {
		t.Fatalf("PendingResidentEnvelopes Vera: %v", err)
	} else if len(pending) != 0 {
		t.Fatalf("Vera (gated on p1/p2/p3) must not be woken yet: %d pending", len(pending))
	}

	// Han finishes p1, Mia finishes p2. p4 still gated (p3 not done).
	if _, err := srv.residentTaskManager(han).PieceDone(context.Background(), task.ID, "p1", nil); err != nil {
		t.Fatalf("Han piece_done p1: %v", err)
	}
	if _, err := srv.residentTaskManager(mia).PieceDone(context.Background(), task.ID, "p2", nil); err != nil {
		t.Fatalf("Mia piece_done p2: %v", err)
	}
	// Drain any wake side effects, then confirm Vera still not woken.
	waitForResidentQuiesce(t, srv)
	if pending, err := session.PendingResidentEnvelopes(srv.rt.SessionDir, vera, 0); err != nil {
		t.Fatalf("PendingResidentEnvelopes Vera (mid): %v", err)
	} else if len(pending) != 0 {
		t.Fatalf("Vera must stay gated until ALL deps done: %d pending", len(pending))
	}

	// Mia finishes the last dependency p3 → the engine now dispatches p4 and
	// wakes Vera automatically (the handoff Vera never got under the flat model).
	afterP3, err := srv.residentTaskManager(mia).PieceDone(context.Background(), task.ID, "p3", nil)
	if err != nil {
		t.Fatalf("Mia piece_done p3: %v", err)
	}
	if got := pieceStatus(afterP3, "p4"); got != session.TaskPieceActive {
		t.Fatalf("p4 status = %q after all deps done, want active (auto-dispatched)", got)
	}
	hist := waitForResidentDMHistoryContains(t, srv, vera, "真机端到端验证")
	if !strings.Contains(historyUserContent(hist), task.ID) {
		t.Fatalf("Vera's wake should carry the task thread id for piece_done")
	}
}

func TestTaskPlanWakesLeadWhenAllDone(t *testing.T) {
	srv, groupID, andy, mia, _, _ := planFixture(t)
	lead := srv.residentTaskManager(andy)
	task, err := lead.CreateTask(context.Background(), groupID, 0, "小任务", true, "")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// Single-piece plan assigned to Mia.
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "only", Title: "写调研报告", Assignee: mia},
	}); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	waitForResidentDMHistoryContains(t, srv, mia, "写调研报告")
	// Mia finishes the only piece → the lead (Andy) is woken to wrap up.
	if _, err := srv.residentTaskManager(mia).PieceDone(context.Background(), task.ID, "only", nil); err != nil {
		t.Fatalf("piece_done: %v", err)
	}
	hist := waitForResidentDMHistoryContains(t, srv, andy, "Every piece of task")
	if !strings.Contains(historyUserContent(hist), "update_status") {
		t.Fatalf("lead wake should tell it to wrap up via update_status")
	}
}

func TestTaskPlanPieceDoneOnlyByAssignee(t *testing.T) {
	srv, groupID, andy, mia, han, _ := planFixture(t)
	lead := srv.residentTaskManager(andy)
	task, err := lead.CreateTask(context.Background(), groupID, 0, "活", true, "")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "某块", Assignee: mia},
	}); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	// Han is not the assignee — he cannot report Mia's piece done.
	if _, err := srv.residentTaskManager(han).PieceDone(context.Background(), task.ID, "p1", nil); err == nil {
		t.Fatal("non-assignee must not mark a piece done")
	}
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "某块", Assignee: mia, DependsOn: []string{"nope"}},
	}); err == nil {
		t.Fatal("set_plan must reject a dependency on an unknown piece")
	}
}
