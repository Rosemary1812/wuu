package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/cron"
)

func TestScheduleCronTool_DefaultsToSessionOnly(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	env := &Env{RootDir: dir, StateDir: stateDir}
	tool := NewCronTool(env)

	result, err := tool.Execute(context.Background(), `{"action":"add","cron":"*/5 * * * *","prompt":"check deploy","recurring":true}`)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if !strings.Contains(result, `"action":"cron"`) {
		t.Fatalf("expected cron action, got %s", result)
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
	tool := NewCronTool(env)

	result, err := tool.Execute(context.Background(), `{"action":"add","cron":"*/5 * * * *","prompt":"check deploy","recurring":true,"durable":true}`)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if !strings.Contains(result, `"action":"cron"`) {
		t.Fatalf("expected cron action, got %s", result)
	}

	fileTasks, err := cron.NewTaskStore(taskStorePath(stateDir)).List()
	if err != nil {
		t.Fatalf("file store list: %v", err)
	}
	if len(fileTasks) != 1 {
		t.Fatalf("expected 1 durable task, got %d", len(fileTasks))
	}
	sessionTasks, err := cron.NewSessionTaskStore(stateDir).List()
	if err != nil {
		t.Fatalf("session store list: %v", err)
	}
	if len(sessionTasks) != 0 {
		t.Fatalf("expected no session tasks, got %d", len(sessionTasks))
	}
}

func TestScheduleCronTool_DurableWriteFailureDoesNotCreateSessionTask(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	if err := os.MkdirAll(taskStorePath(stateDir)+".lock", 0o755); err != nil {
		t.Fatalf("create invalid durable lock path: %v", err)
	}
	env := &Env{RootDir: dir, StateDir: stateDir}
	tool := NewCronTool(env)

	_, err := tool.Execute(context.Background(), `{"action":"add","cron":"*/5 * * * *","prompt":"check deploy","recurring":true,"durable":true}`)
	if err == nil {
		t.Fatal("expected durable write error")
	}
	if !strings.Contains(err.Error(), "failed to save task") {
		t.Fatalf("expected durable write error, got %v", err)
	}

	sessionTasks, listErr := cron.NewSessionTaskStore(stateDir).List()
	if listErr != nil {
		t.Fatalf("session store list: %v", listErr)
	}
	if len(sessionTasks) != 0 {
		t.Fatalf("expected no session tasks after durable write failure, got %d", len(sessionTasks))
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

	tool := NewCronTool(env)
	result, err := tool.Execute(context.Background(), `{"action":"remove","id":"def456"}`)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if !strings.Contains(result, `"action":"cron"`) || !strings.Contains(result, `"removed":"def456"`) {
		t.Fatalf("expected cron remove action, got %s", result)
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

	tool := NewCronTool(env)
	result, err := tool.Execute(context.Background(), `{"action":"list"}`)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if !strings.Contains(result, `"action":"cron"`) {
		t.Fatalf("expected cron action, got %s", result)
	}
	if !strings.Contains(result, "[session-only]") {
		t.Fatalf("expected session-only task in result, got %s", result)
	}
}
