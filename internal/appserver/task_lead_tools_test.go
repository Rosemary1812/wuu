package appserver

import (
	"context"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/tools"
)

type leadToolExecutorStub struct{ calls []string }

func (s *leadToolExecutorStub) Definitions() []providers.ToolDefinition {
	return []providers.ToolDefinition{{Name: "manage_task"}, {Name: "fetch_thread_messages"}, {Name: "bash"}, {Name: "edit_file"}}
}

func (s *leadToolExecutorStub) Execute(_ context.Context, call providers.ToolCall) (string, error) {
	s.calls = append(s.calls, call.Name)
	return "ok", nil
}

func TestTaskLeadToolExecutorCannotExecuteWorkerTools(t *testing.T) {
	base := &leadToolExecutorStub{}
	exec := allowlistedToolExecutor{base: base, allowed: taskLeadManagementTools}
	defs := map[string]bool{}
	for _, def := range exec.Definitions() {
		defs[def.Name] = true
	}
	if !defs["manage_task"] || !defs["fetch_thread_messages"] || defs["bash"] || defs["edit_file"] {
		t.Fatalf("lead definitions = %v", defs)
	}
	if _, err := exec.Execute(context.Background(), providers.ToolCall{Name: "bash"}); err == nil {
		t.Fatal("lead management turn must reject bash even if the model fabricates the call")
	}
	if _, err := exec.Execute(context.Background(), providers.ToolCall{Name: "manage_task"}); err != nil {
		t.Fatal(err)
	}
}

func TestTaskLeadManagementWakeIsRestrictedButWorkerAttemptIsNot(t *testing.T) {
	srv, groupID, andy, mia, _, _ := planFixture(t)
	lead := srv.residentTaskManager(andy)
	task, err := createPromotedTaskForTest(context.Background(), lead, groupID, "tool surface")
	if err != nil {
		t.Fatal(err)
	}
	leadWake := taskLeadWakeEnv(task.ID)
	if !srv.taskLeadManagementTurn(andy, []MessageEnvelope{leadWake}) {
		t.Fatal("lead planning wake must use the management-only tool surface")
	}
	if srv.taskLeadManagementTurn(mia, []MessageEnvelope{leadWake}) {
		t.Fatal("a non-lead cannot gain the lead management surface")
	}
	if _, err := lead.SetPlan(context.Background(), task.ID, []tools.TaskPiece{{ID: "p1", Title: "work", Assignee: mia}}); err != nil {
		t.Fatal(err)
	}
	workerWake := taskPlanDispatchEnv(t, srv, task.ID, mia)
	if srv.taskLeadManagementTurn(mia, []MessageEnvelope{workerWake}) {
		t.Fatal("worker attempt must retain its execution tool surface")
	}
}
