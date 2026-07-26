package appserver

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/blueberrycongee/wuu/internal/automation"
	"github.com/blueberrycongee/wuu/internal/runtime"
)

func TestAutomationListRPC(t *testing.T) {
	stateDir := t.TempDir()
	manager := automation.NewManager(automation.Config{StateDir: stateDir})
	defer manager.Stop()
	if _, err := manager.AddTask(automation.AddTaskParams{
		Prompt: "inspect", Schedule: "*/5 * * * *", Timezone: "UTC", Durable: true,
	}); err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}
	var out bytes.Buffer
	server := New(&runtime.Session{
		StateDir: stateDir, SessionDir: filepath.Join(stateDir, "sessions"), AutomationManager: manager,
	}, &out)
	defer server.Close()
	if err := server.handleLine(context.Background(), []byte(`{"id":"list","method":"automation/list"}`)); err != nil {
		t.Fatalf("automation/list error = %v", err)
	}
	result := remarshal[AutomationListResult](t, responseByID(t, parseOutput(t, out.String()), "list")["result"])
	if len(result.Tasks) != 1 || result.Tasks[0].Prompt != "inspect" {
		t.Fatalf("tasks = %#v", result.Tasks)
	}
	taskID := result.Tasks[0].ID
	out.Reset()
	if err := server.handleLine(context.Background(), []byte(`{"id":"pause","method":"automation/update","params":{"id":"`+taskID+`","paused":true}}`)); err != nil {
		t.Fatalf("automation/update error = %v", err)
	}
	updated := remarshal[AutomationUpdateResult](t, responseByID(t, parseOutput(t, out.String()), "pause")["result"])
	if !updated.Task.Paused {
		t.Fatalf("updated task = %#v", updated.Task)
	}
	out.Reset()
	if err := server.handleLine(context.Background(), []byte(`{"id":"edit","method":"automation/update","params":{"id":"`+taskID+`","title":"Daily inspect","prompt":"inspect carefully","schedule":"0 9 * * 1-5","timezone":"UTC","mode":"new_thread","recurring":true}}`)); err != nil {
		t.Fatalf("automation/update fields error = %v", err)
	}
	edited := remarshal[AutomationUpdateResult](t, responseByID(t, parseOutput(t, out.String()), "edit")["result"])
	if edited.Task.Title != "Daily inspect" || edited.Task.Prompt != "inspect carefully" || edited.Task.Cron != "0 9 * * 1-5" || !edited.Task.Recurring {
		t.Fatalf("edited task = %#v", edited.Task)
	}
}

func TestAutomationCreateRPCPersistsPausedWorkspaceDraft(t *testing.T) {
	stateDir := t.TempDir()
	manager := automation.NewManager(automation.Config{StateDir: stateDir})
	defer manager.Stop()
	var out bytes.Buffer
	server := New(&runtime.Session{
		StateDir: stateDir, SessionDir: filepath.Join(stateDir, "sessions"), AutomationManager: manager,
	}, &out)
	defer server.Close()

	request := []byte(`{"id":"create","method":"automation/create","params":{"title":"New automation","prompt":"","schedule":"0 9 * * 1-5","timezone":"UTC","mode":"new_thread","workspace_id":"project-1","workspace_path":"/workspaces/example","recurring":true,"paused":true}}`)
	if err := server.handleLine(context.Background(), request); err != nil {
		t.Fatalf("automation/create error = %v", err)
	}
	created := remarshal[AutomationCreateResult](t, responseByID(t, parseOutput(t, out.String()), "create")["result"])
	if created.Task.Title != "New automation" || created.Task.Prompt != "" || !created.Task.Recurring || !created.Task.Paused {
		t.Fatalf("created task = %#v", created.Task)
	}
	if created.Task.WorkspaceID != "project-1" || created.Task.WorkspacePath != "/workspaces/example" {
		t.Fatalf("workspace = %q, %q", created.Task.WorkspaceID, created.Task.WorkspacePath)
	}
	if created.Task.Metadata["durability"] != "durable" {
		t.Fatalf("durability = %q", created.Task.Metadata["durability"])
	}
}
