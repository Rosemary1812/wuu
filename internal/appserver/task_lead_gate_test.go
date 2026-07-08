package appserver

import (
	"context"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/tools"
)

// Lead = the single orchestrator (plan §T6): only a task's lead may declare or
// revise its plan. An agent-created standalone task is born leadless, so the
// first board member to plan it atomically takes the lead; every later set_plan
// (a replan) must come from that same lead. Workers (piece assignees) are board
// members but never the lead, so their set_plan is refused — they do the piece
// and file piece_done, they do not rewrite the plan.

func leadOf(t *testing.T, srv *Server, taskID string) string {
	t.Helper()
	th, err := session.FindConversationThreadByID(srv.rt.SessionDir, taskID)
	if err != nil {
		t.Fatalf("FindConversationThreadByID %q: %v", taskID, err)
	}
	return th.LeadParticipantID
}

func TestSetPlanFirstBoardMemberBecomesLeadThenGatesOthers(t *testing.T) {
	srv, groupID, andy, mia, han, vera := planFixture(t)

	// Andy creates a standalone team task (born owned by claim, but LEADLESS —
	// an agent-created task carries no orchestration grant).
	task, err := srv.residentTaskManager(andy).CreateTask(context.Background(), groupID, 0, "无主任务", true, "")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if got := leadOf(t, srv, task.ID); got != "" {
		t.Fatalf("agent-created task must be born leadless, lead=%q", got)
	}

	// Mia is a board member but not the creator. Her set_plan is the first plan
	// on a leadless task, so she takes the lead and the plan is applied.
	view, err := srv.residentTaskManager(mia).SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "backend-api", Assignee: han},
	})
	if err != nil {
		t.Fatalf("first board-member SetPlan: %v", err)
	}
	if got := leadOf(t, srv, task.ID); got != mia {
		t.Fatalf("first planner must become lead, lead=%q want %q", got, mia)
	}
	if view.ExecState != session.ExecStateExecuting {
		t.Fatalf("exec_state after set_plan = %q, want executing", view.ExecState)
	}
	if got := pieceStatus(view, "p1"); got != session.TaskPieceActive {
		t.Fatalf("p1 status = %q, want active (first piece dispatched)", got)
	}
	waitForResidentDMHistoryContains(t, srv, han, "backend-api")

	// Han is the piece assignee AND a board member, but not the lead: his
	// set_plan is refused with the lead-gate message. A worker does the piece,
	// it never rewrites the plan.
	if _, err := srv.residentTaskManager(han).SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "x", Title: "worker replan", Assignee: han},
	}); err == nil {
		t.Fatal("an assignee (non-lead) set_plan must be refused")
	} else if !strings.Contains(err.Error(), "is led by") ||
		!strings.Contains(err.Error(), mia) ||
		!strings.Contains(err.Error(), "only its lead may declare or revise the plan") {
		t.Fatalf("assignee refusal should name the lead-gate, got %v", err)
	}

	// A different non-lead board member (not even an assignee) is refused too.
	if _, err := srv.residentTaskManager(vera).SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "y", Title: "outsider replan", Assignee: vera},
	}); err == nil {
		t.Fatal("a non-lead board member set_plan must be refused")
	} else if !strings.Contains(err.Error(), "only its lead may declare or revise the plan") {
		t.Fatalf("non-lead refusal should name the lead-gate, got %v", err)
	}

	// The plan is unchanged by the refused calls: still Mia's single piece,
	// still led by Mia.
	after, err := session.FindConversationThreadByID(srv.rt.SessionDir, task.ID)
	if err != nil {
		t.Fatalf("re-read task: %v", err)
	}
	if len(after.Plan) != 1 || after.Plan[0].ID != "p1" || after.Plan[0].Assignee != han {
		t.Fatalf("refused set_plan must not mutate the plan, got %+v", after.Plan)
	}
	if after.LeadParticipantID != mia {
		t.Fatalf("lead after refusals = %q, want %q", after.LeadParticipantID, mia)
	}
}

func TestSetPlanRefusedOnResolvedTask(t *testing.T) {
	srv, groupID, andy, _, han, _ := planFixture(t)
	lead := srv.residentTaskManager(andy)

	task, err := lead.CreateTask(context.Background(), groupID, 0, "收束后不可重规划", true, "")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "backend-api", Assignee: han},
	}); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	waitForResidentQuiesce(t, srv)

	// The lead concludes the task (status -> resolved); lead_participant_id
	// survives the resolve. A stray set_plan by that same surviving lead must be
	// refused — re-running a resolved task needs a fresh escalation, not a
	// plan write against a terminal task.
	if _, err := lead.ConcludeTask(context.Background(), task.ID, "done and verified"); err != nil {
		t.Fatalf("ConcludeTask: %v", err)
	}
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p2", Title: "sneak replan", Assignee: han},
	}); err == nil {
		t.Fatal("set_plan on a resolved task must be refused")
	} else if !strings.Contains(err.Error(), "not an active task") {
		t.Fatalf("resolved-task refusal should name the status guard, got %v", err)
	}
}

func TestSetPlanLeadReplanReDispatches(t *testing.T) {
	srv, groupID, andy, mia, han, vera := planFixture(t)
	lead := srv.residentTaskManager(andy)

	task, err := lead.CreateTask(context.Background(), groupID, 0, "重规划任务", true, "")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Andy plans the task first — he becomes its lead and the first pieces are
	// dispatched.
	view, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "backend-api", Assignee: han},
		{ID: "p2", Title: "mobile-ui", Assignee: mia},
	})
	if err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	if got := leadOf(t, srv, task.ID); got != andy {
		t.Fatalf("planner must become lead, lead=%q want %q", got, andy)
	}
	if view.ExecState != session.ExecStateExecuting {
		t.Fatalf("exec_state = %q, want executing", view.ExecState)
	}
	for _, id := range []string{"p1", "p2"} {
		if got := pieceStatus(view, id); got != session.TaskPieceActive {
			t.Fatalf("piece %s = %q, want active", id, got)
		}
	}
	waitForResidentDMHistoryContains(t, srv, han, "backend-api")
	waitForResidentDMHistoryContains(t, srv, mia, "mobile-ui")
	waitForResidentQuiesce(t, srv)

	// Andy (still the lead) revises the plan: a replan is just another set_plan.
	// It replaces the breakdown and re-dispatches — the new piece's assignee is
	// woken.
	replan, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "backend-api", Assignee: han},
		{ID: "p3", Title: "qa-e2e", Assignee: vera},
	})
	if err != nil {
		t.Fatalf("replan SetPlan: %v", err)
	}
	if got := pieceStatus(replan, "p3"); got != session.TaskPieceActive {
		t.Fatalf("replan p3 = %q, want active (re-dispatched)", got)
	}
	if got := pieceStatus(replan, "p2"); got != "" {
		t.Fatalf("replan must replace the plan, p2 still present with status %q", got)
	}
	waitForResidentDMHistoryContains(t, srv, vera, "qa-e2e")
}
