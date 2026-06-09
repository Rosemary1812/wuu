package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/cron"
	"github.com/blueberrycongee/wuu/internal/workflow"
)

func TestScheduleCronTool_DefaultsToSessionOnly(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	env := &Env{RootDir: dir, StateDir: stateDir}
	tool := NewScheduleCronTool(env)

	result, err := tool.Execute(context.Background(), `{"cron":"*/5 * * * *","prompt":"check deploy","recurring":true}`)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}

	fileTasks, err := cron.NewTaskStore(taskStorePath(stateDir)).List()
	if err != nil {
		t.Fatalf("file store list: %v", err)
	}
	if len(fileTasks) != 0 {
		t.Fatalf("expected no durable tasks, got %d", len(fileTasks))
	}
	sessionTasks, err := cron.NewSessionTaskStore(stateDir).List()
	if err != nil {
		t.Fatalf("session store list: %v", err)
	}
	if len(sessionTasks) != 1 {
		t.Fatalf("expected 1 session task, got %d", len(sessionTasks))
	}
	if !strings.Contains(result, `"durability":"session-only"`) {
		t.Fatalf("expected session-only result, got %s", result)
	}
}

func TestScheduleCronTool_DurablePersistsToDisk(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	env := &Env{RootDir: dir, StateDir: stateDir}
	tool := NewScheduleCronTool(env)

	result, err := tool.Execute(context.Background(), `{"cron":"*/5 * * * *","prompt":"check deploy","recurring":true,"durable":true}`)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}

	fileTasks, err := cron.NewTaskStore(taskStorePath(stateDir)).List()
	if err != nil {
		t.Fatalf("file store list: %v", err)
	}
	if len(fileTasks) != 1 {
		t.Fatalf("expected 1 durable task, got %d", len(fileTasks))
	}
}

func TestScheduleCronTool_SavesWorkflowTask(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	env := &Env{
		RootDir:  dir,
		StateDir: stateDir,
		Workflows: []workflow.Definition{{
			Name:        "weekly-qa",
			Description: "Run weekly QA.",
			Kind:        workflow.DefinitionKindMarkdown,
		}},
	}
	tool := NewScheduleCronTool(env)

	result, err := tool.Execute(context.Background(), `{
		"cron":"*/5 * * * *",
		"workflow_name":"weekly-qa",
		"workflow_arguments":"settings search",
		"recurring":true,
		"durable":true
	}`)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !strings.Contains(result, `"kind":"workflow"`) || !strings.Contains(result, `"workflow_name":"weekly-qa"`) {
		t.Fatalf("expected workflow result, got %s", result)
	}

	fileTasks, err := cron.NewTaskStore(taskStorePath(stateDir)).List()
	if err != nil {
		t.Fatalf("file store list: %v", err)
	}
	if len(fileTasks) != 1 || fileTasks[0].Metadata["kind"] != "workflow" {
		t.Fatalf("expected one workflow task, got %+v", fileTasks)
	}
	if fileTasks[0].Metadata["workflow_kind"] != workflow.DefinitionKindMarkdown {
		t.Fatalf("expected markdown workflow kind metadata, got %+v", fileTasks[0].Metadata)
	}
	if fileTasks[0].Metadata["workflow_arguments"] != "settings search" || fileTasks[0].Prompt == "" {
		t.Fatalf("workflow task metadata missing: %+v", fileTasks[0])
	}
	if !strings.Contains(fileTasks[0].Prompt, `Start the saved workflow "weekly-qa"`) {
		t.Fatalf("workflow task prompt missing workflow instruction: %q", fileTasks[0].Prompt)
	}
	if !strings.Contains(fileTasks[0].Prompt, "record the Workflow Team") {
		t.Fatalf("workflow task prompt missing team planning instruction: %q", fileTasks[0].Prompt)
	}
	if !strings.Contains(fileTasks[0].Prompt, "start_workflow") || !strings.Contains(fileTasks[0].Prompt, "driver=auto") {
		t.Fatalf("agent-managed workflow task prompt should use start_workflow: %q", fileTasks[0].Prompt)
	}
	if !strings.Contains(fileTasks[0].Prompt, "Base Agent Brief Contract") || !strings.Contains(fileTasks[0].Prompt, "Workflow Context Extension") {
		t.Fatalf("workflow task prompt missing shared brief contract: %q", fileTasks[0].Prompt)
	}
}

func TestScheduleCronTool_SavesScriptWorkflowTaskUsesStartWorkflow(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	env := &Env{
		RootDir:  dir,
		StateDir: stateDir,
		Workflows: []workflow.Definition{{
			Name:        "dynamic-qa",
			Description: "Run dynamic QA.",
			Kind:        workflow.DefinitionKindScript,
		}},
	}
	tool := NewScheduleCronTool(env)

	_, err := tool.Execute(context.Background(), `{
		"cron":"*/5 * * * *",
		"workflow_name":"dynamic-qa",
		"workflow_arguments":"settings search",
		"recurring":true,
		"durable":true
	}`)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	fileTasks, err := cron.NewTaskStore(taskStorePath(stateDir)).List()
	if err != nil {
		t.Fatalf("file store list: %v", err)
	}
	if len(fileTasks) != 1 || fileTasks[0].Metadata["workflow_kind"] != workflow.DefinitionKindScript {
		t.Fatalf("expected one script workflow task, got %+v", fileTasks)
	}
	for _, want := range []string{"load_workflow", "start_workflow", "definition_name", "driver=auto", "script driver"} {
		if !strings.Contains(fileTasks[0].Prompt, want) {
			t.Fatalf("script workflow prompt missing %q: %q", want, fileTasks[0].Prompt)
		}
	}
	if strings.Contains(fileTasks[0].Prompt, "record the Workflow Team") || strings.Contains(fileTasks[0].Prompt, "call create_workflow") || strings.Contains(fileTasks[0].Prompt, "call run_workflow") {
		t.Fatalf("script workflow prompt should not use agent-managed workflow path: %q", fileTasks[0].Prompt)
	}
}

func TestCancelCronTool(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	env := &Env{RootDir: dir, StateDir: stateDir}
	fileStore := cron.NewTaskStore(taskStorePath(stateDir))
	if err := fileStore.Add(cron.Task{ID: "abc123", Cron: "* * * * *", Prompt: "x"}); err != nil {
		t.Fatalf("fileStore.Add: %v", err)
	}
	sessionStore := cron.NewSessionTaskStore(stateDir)
	if err := sessionStore.Add(cron.Task{ID: "def456", Cron: "* * * * *", Prompt: "y"}); err != nil {
		t.Fatalf("sessionStore.Add: %v", err)
	}

	tool := NewCancelCronTool(env)
	result, err := tool.Execute(context.Background(), `{"id":"def456"}`)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}

	sessionTasks, err := sessionStore.List()
	if err != nil {
		t.Fatalf("sessionStore.List: %v", err)
	}
	if len(sessionTasks) != 0 {
		t.Fatalf("expected session task removed, got %d", len(sessionTasks))
	}
}

func TestListCronTool(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	env := &Env{RootDir: dir, StateDir: stateDir}
	fileStore := cron.NewTaskStore(taskStorePath(stateDir))
	if err := fileStore.Add(cron.Task{ID: "abc", Cron: "*/5 * * * *", Prompt: "check"}); err != nil {
		t.Fatalf("fileStore.Add: %v", err)
	}
	sessionStore := cron.NewSessionTaskStore(stateDir)
	if err := sessionStore.Add(cron.Task{ID: "def", Cron: "*/10 * * * *", Prompt: "ping", Recurring: true}); err != nil {
		t.Fatalf("sessionStore.Add: %v", err)
	}

	tool := NewListCronTool(env)
	result, err := tool.Execute(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if !strings.Contains(result, "[session-only]") {
		t.Fatalf("expected session-only task in result, got %s", result)
	}
}
