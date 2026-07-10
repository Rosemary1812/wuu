package appserver

import (
	"context"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/tools"
)

func TestLeadRevisesPendingGraphWithoutReplacingAttempts(t *testing.T) {
	srv, groupID, andy, mia, han, vera := planFixture(t)
	lead := srv.residentTaskManager(andy)
	task, err := createPromotedTaskForTest(context.Background(), lead, groupID, "dynamic graph")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{{
		ID: "p1", Title: "research", Assignee: mia,
	}}); err != nil {
		t.Fatal(err)
	}
	p1 := loadPiece(t, srv, task.ID, "p1")
	firstAttempt := p1.CurrentAttemptID
	if firstAttempt == "" {
		t.Fatal("p1 must have a durable attempt")
	}
	if _, err := lead.ReviseTaskPiece(context.Background(), task.ID, "p1", "rewrite live", "", nil); err == nil || !strings.Contains(err.Error(), "cannot be revised") {
		t.Fatalf("live revision = %v", err)
	}
	if _, err := lead.AddTaskPiece(context.Background(), task.ID, tools.TaskPiece{
		ID: "p2", Title: "draft", Assignee: han, Prompt: "use p1", DependsOn: []string{"p1"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := lead.ReviseTaskPiece(context.Background(), task.ID, "p2", "verify", "verify p1", []string{"p1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := lead.ReassignTaskPiece(context.Background(), task.ID, "p2", vera); err != nil {
		t.Fatal(err)
	}
	if got := loadPiece(t, srv, task.ID, "p1").CurrentAttemptID; got != firstAttempt {
		t.Fatalf("graph revision replaced p1 attempt %q with %q", firstAttempt, got)
	}
	if _, err := srv.residentTaskManager(mia).PieceDone(context.Background(), task.ID, "p1", nil); err != nil {
		t.Fatal(err)
	}
	p2 := loadPiece(t, srv, task.ID, "p2")
	if p2.Status != session.TaskPieceActive || p2.Assignee != vera || p2.Title != "verify" || p2.CurrentAttemptID == "" {
		t.Fatalf("revised p2 = %+v", p2)
	}
}

func TestLeadRetriesInterruptedAttemptWithNewIdentity(t *testing.T) {
	srv, groupID, andy, mia, _, _ := planFixture(t)
	lead := srv.residentTaskManager(andy)
	task, err := createPromotedTaskForTest(context.Background(), lead, groupID, "retry exact attempt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{{ID: "p1", Title: "work", Assignee: mia}}); err != nil {
		t.Fatal(err)
	}
	first := loadPiece(t, srv, task.ID, "p1").CurrentAttemptID
	env := taskPlanDispatchEnv(t, srv, task.ID, mia)
	srv.interruptTaskAttemptsAfterTurn(mia, []MessageEnvelope{env}, "stopped")
	if _, err := lead.RetryTaskPiece(context.Background(), task.ID, "p1", "try a new approach"); err != nil {
		t.Fatal(err)
	}
	retried := loadPiece(t, srv, task.ID, "p1")
	if retried.Status != session.TaskPieceActive || retried.CurrentAttemptID == "" || retried.CurrentAttemptID == first || retried.Attempts != 2 {
		t.Fatalf("retried piece = %+v; first attempt %q", retried, first)
	}
	attempts, err := session.TaskAttempts(srv.rt.SessionDir, task.ID)
	if err != nil || len(attempts) != 2 || attempts[0].Status != session.TaskAttemptInterrupted || attempts[1].Status != session.TaskAttemptQueued {
		t.Fatalf("attempt history = %+v, %v", attempts, err)
	}
}

func TestLeadCancellationPropagatesAndAwaitsConclusion(t *testing.T) {
	srv, groupID, andy, mia, han, _ := planFixture(t)
	lead := srv.residentTaskManager(andy)
	task, err := createPromotedTaskForTest(context.Background(), lead, groupID, "cancel branch")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{
		{ID: "p1", Title: "obsolete", Assignee: mia},
		{ID: "p2", Title: "dependent", Assignee: han, DependsOn: []string{"p1"}},
	}); err != nil {
		t.Fatal(err)
	}
	view, err := lead.CancelTaskPiece(context.Background(), task.ID, "p1", "scope removed")
	if err != nil {
		t.Fatal(err)
	}
	if pieceStatus(view, "p1") != session.TaskPieceCancelled || pieceStatus(view, "p2") != session.TaskPieceCancelled || view.ExecState != session.ExecStateAwaitingLead {
		t.Fatalf("cancelled workflow = %+v", view)
	}
	if _, err := lead.ConcludeTask(context.Background(), task.ID, "verified: branch intentionally removed"); err != nil {
		t.Fatalf("conclude cancelled workflow: %v", err)
	}
}

func TestLeadResumesHumanBlockedWorkflow(t *testing.T) {
	srv, groupID, andy, mia, _, _ := planFixture(t)
	lead := srv.residentTaskManager(andy)
	task, err := createPromotedTaskForTest(context.Background(), lead, groupID, "resume")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{{ID: "p1", Title: "decision", Assignee: mia}}); err != nil {
		t.Fatal(err)
	}
	first := loadPiece(t, srv, task.ID, "p1").CurrentAttemptID
	if _, err := srv.residentTaskManager(mia).NeedHuman(context.Background(), task.ID, "choose a policy"); err != nil {
		t.Fatal(err)
	}
	view, err := lead.ResumeTask(context.Background(), task.ID, "human decision recorded")
	if err != nil {
		t.Fatal(err)
	}
	resumed := loadPiece(t, srv, task.ID, "p1")
	if view.ExecState != session.ExecStateExecuting || resumed.Status != session.TaskPieceActive || resumed.CurrentAttemptID == "" || resumed.CurrentAttemptID == first {
		t.Fatalf("resumed workflow = view %+v piece %+v", view, resumed)
	}
	events, err := lead.TraceTask(context.Background(), task.ID)
	if err != nil || len(events) == 0 {
		t.Fatalf("trace = %+v, %v", events, err)
	}
}
