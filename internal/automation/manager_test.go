package automation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/blueberrycongee/wuu/internal/statepath"
)

type recordingExecutor struct {
	result ExecutionResult
	err    error
	tasks  []Task
	runs   []Run
}

func (e *recordingExecutor) ExecuteAutomation(_ context.Context, task Task, run Run) (ExecutionResult, error) {
	e.tasks = append(e.tasks, task)
	e.runs = append(e.runs, run)
	return e.result, e.err
}

func TestManagerAddsHeartbeatTaskWithCreatorThread(t *testing.T) {
	manager := NewManager(Config{StateDir: t.TempDir()})
	task, err := manager.AddTask(AddTaskParams{
		Prompt:          "check cache health",
		Schedule:        "*/5 * * * *",
		Timezone:        "UTC",
		Mode:            ModeThreadHeartbeat,
		CreatorThreadID: "thread-1",
		Recurring:       true,
		Durable:         true,
	})
	if err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}
	if task.HeartbeatThreadID != "thread-1" {
		t.Fatalf("HeartbeatThreadID = %q, want thread-1", task.HeartbeatThreadID)
	}
	if task.Mode != string(ModeThreadHeartbeat) {
		t.Fatalf("Mode = %q", task.Mode)
	}
	tasks, err := manager.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != task.ID || tasks[0].Metadata["durability"] != "durable" {
		t.Fatalf("ListTasks() = %#v", tasks)
	}
}

func TestManagerRejectsHeartbeatWithoutThread(t *testing.T) {
	manager := NewManager(Config{StateDir: t.TempDir()})
	_, err := manager.AddTask(AddTaskParams{
		Prompt:   "check cache health",
		Schedule: "*/5 * * * *",
		Timezone: "UTC",
		Mode:     ModeThreadHeartbeat,
	})
	if err == nil {
		t.Fatal("AddTask() error = nil")
	}
}

func TestManagerAllowsEditingPausedDraftWithoutPrompt(t *testing.T) {
	manager := NewManager(Config{StateDir: t.TempDir()})
	task, err := manager.AddTask(AddTaskParams{
		Title:     "New automation",
		Schedule:  "0 9 * * 1-5",
		Timezone:  "UTC",
		Mode:      ModeNewThread,
		Recurring: true,
		Paused:    true,
		Durable:   true,
	})
	if err != nil {
		t.Fatalf("AddTask() paused draft error = %v", err)
	}

	schedule := "0 12 * * 1-5"
	updated, err := manager.UpdateTask(task.ID, UpdateTaskParams{Schedule: &schedule})
	if err != nil {
		t.Fatalf("UpdateTask() paused draft error = %v", err)
	}
	if updated.Cron != schedule || updated.Prompt != "" || !updated.Paused {
		t.Fatalf("updated paused draft = %#v", updated)
	}

	paused := false
	if _, err := manager.UpdateTask(task.ID, UpdateTaskParams{Paused: &paused}); err == nil {
		t.Fatal("UpdateTask() enabled empty draft error = nil")
	}
}

func TestManagerRecordsAdmissionAndCompletion(t *testing.T) {
	manager := NewManager(Config{StateDir: t.TempDir()})
	manager.executor = &recordingExecutor{result: ExecutionResult{
		Status:   RunStatusRunning,
		ThreadID: "thread-2",
		TurnID:   "turn-1",
	}}
	if err := manager.Fire(context.Background(), Task{ID: "task-1", Prompt: "inspect", Mode: string(ModeNewThread)}); err != nil {
		t.Fatalf("Fire() error = %v", err)
	}
	runs, err := manager.ListRuns()
	if err != nil {
		t.Fatalf("ListRuns() error = %v", err)
	}
	if len(runs) != 1 || runs[0].Status != RunStatusRunning || runs[0].ThreadID != "thread-2" {
		t.Fatalf("runs = %#v", runs)
	}
	if err := manager.CompleteRun(runs[0].ID, "thread-2", "turn-1", nil); err != nil {
		t.Fatalf("CompleteRun() error = %v", err)
	}
	runs, _ = manager.ListRuns()
	if runs[0].Status != RunStatusCompleted || runs[0].CompletedAt.IsZero() {
		t.Fatalf("completed run = %#v", runs[0])
	}
}

func TestManagerRecordsExecutionFailure(t *testing.T) {
	manager := NewManager(Config{StateDir: t.TempDir()})
	manager.executor = &recordingExecutor{err: errors.New("start failed")}
	err := manager.Fire(context.Background(), Task{ID: "task-1", Prompt: "inspect"})
	if err == nil {
		t.Fatal("Fire() error = nil")
	}
	runs, listErr := manager.ListRuns()
	if listErr != nil {
		t.Fatalf("ListRuns() error = %v", listErr)
	}
	if len(runs) != 1 || runs[0].Status != RunStatusFailed || runs[0].Error != "start failed" {
		t.Fatalf("runs = %#v", runs)
	}
}

func TestManagerPausesDurableTask(t *testing.T) {
	manager := NewManager(Config{StateDir: t.TempDir()})
	task, err := manager.AddTask(AddTaskParams{
		Prompt: "inspect", Schedule: "*/5 * * * *", Timezone: "UTC", Durable: true,
	})
	if err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}
	paused, err := manager.SetPaused(task.ID, true)
	if err != nil {
		t.Fatalf("SetPaused() error = %v", err)
	}
	if !paused.Paused {
		t.Fatalf("paused task = %#v", paused)
	}
	tasks, err := manager.ListTasks()
	if err != nil || len(tasks) != 1 || !tasks[0].Paused {
		t.Fatalf("ListTasks() = %#v, %v", tasks, err)
	}
}

func TestRunAdmissionDoesNotOverwriteTerminalStatus(t *testing.T) {
	store := NewRunStore(t.TempDir() + "/runs.json")
	run := Run{ID: "run-1", Status: RunStatusRunning}
	if err := store.Add(run); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := store.Finish(run.ID, RunStatusCompleted, "thread-1", "turn-1", ""); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
	if err := store.UpdateAdmission(run.ID, RunStatusRunning, "thread-1", "turn-1", ""); err != nil {
		t.Fatalf("UpdateAdmission() error = %v", err)
	}
	runs, err := store.List()
	if err != nil || len(runs) != 1 || runs[0].Status != RunStatusCompleted {
		t.Fatalf("runs = %#v, %v", runs, err)
	}
}

func TestManagerRecoversQueuedRunAndFailsInterruptedRun(t *testing.T) {
	manager := NewManager(Config{StateDir: t.TempDir()})
	queuedTask := Task{
		ID: "queued-task", Prompt: "resume queued work", Mode: string(ModeThreadHeartbeat),
		HeartbeatThreadID: "thread-1", Cron: "*/5 * * * *", Timezone: "UTC",
	}
	if err := manager.runStore.Add(Run{
		ID: "queued-run", TaskID: queuedTask.ID, Task: queuedTask,
		Mode: queuedTask.Mode, Status: RunStatusQueued,
	}); err != nil {
		t.Fatalf("add queued run: %v", err)
	}
	if err := manager.runStore.Add(Run{
		ID: "running-run", TaskID: "running-task", Status: RunStatusRunning,
		ThreadID: "thread-2", TurnID: "turn-1",
	}); err != nil {
		t.Fatalf("add running run: %v", err)
	}
	executor := &recordingExecutor{result: ExecutionResult{
		Status: RunStatusRunning, ThreadID: "thread-1", TurnID: "turn-2",
	}}
	if err := manager.Start(executor); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer manager.Stop()
	if len(executor.tasks) != 1 || executor.tasks[0].Prompt != queuedTask.Prompt || len(executor.runs) != 1 || executor.runs[0].ID != "queued-run" {
		t.Fatalf("recovered execution = tasks %#v, runs %#v", executor.tasks, executor.runs)
	}
	runs, err := manager.ListRuns()
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	byID := make(map[string]Run, len(runs))
	for _, run := range runs {
		byID[run.ID] = run
	}
	if recovered := byID["queued-run"]; recovered.Status != RunStatusRunning || recovered.ThreadID != "thread-1" || recovered.TurnID != "turn-2" {
		t.Fatalf("recovered queued run = %#v", recovered)
	}
	if interrupted := byID["running-run"]; interrupted.Status != RunStatusFailed || interrupted.CompletedAt.IsZero() || interrupted.Error == "" {
		t.Fatalf("interrupted running run = %#v", interrupted)
	}
}

func TestManagerSkipsOverlappingNewThreadFire(t *testing.T) {
	manager := NewManager(Config{StateDir: t.TempDir()})
	executor := &recordingExecutor{result: ExecutionResult{
		Status: RunStatusRunning, ThreadID: "thread-1", TurnID: "turn-1",
	}}
	manager.executor = executor
	task := Task{ID: "task-1", Prompt: "inspect", Mode: string(ModeNewThread)}
	if err := manager.Fire(context.Background(), task); err != nil {
		t.Fatalf("first Fire() error = %v", err)
	}
	if err := manager.Fire(context.Background(), task); err != nil {
		t.Fatalf("overlapping Fire() error = %v", err)
	}
	runs, err := manager.ListRuns()
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs after overlapping fire = %#v, %v", runs, err)
	}
	if len(executor.tasks) != 1 {
		t.Fatalf("executor calls = %d, want 1", len(executor.tasks))
	}
	if err := manager.CompleteRun(runs[0].ID, "thread-1", "turn-1", nil); err != nil {
		t.Fatalf("CompleteRun() error = %v", err)
	}
	if err := manager.Fire(context.Background(), task); err != nil {
		t.Fatalf("Fire() after completion error = %v", err)
	}
	runs, _ = manager.ListRuns()
	if len(runs) != 2 {
		t.Fatalf("runs after completion fire = %#v", runs)
	}
}

func TestManagerHeartbeatAllowsSingleQueuedFollowUp(t *testing.T) {
	manager := NewManager(Config{StateDir: t.TempDir()})
	executor := &recordingExecutor{result: ExecutionResult{
		Status: RunStatusRunning, ThreadID: "thread-1", TurnID: "turn-1",
	}}
	manager.executor = executor
	task := Task{
		ID: "hb-task", Prompt: "inspect", Mode: string(ModeThreadHeartbeat),
		HeartbeatThreadID: "thread-1",
	}
	if err := manager.Fire(context.Background(), task); err != nil {
		t.Fatalf("first Fire() error = %v", err)
	}
	executor.result = ExecutionResult{Status: RunStatusQueued, ThreadID: "thread-1", QueueID: "queue-1"}
	if err := manager.Fire(context.Background(), task); err != nil {
		t.Fatalf("second Fire() error = %v", err)
	}
	if err := manager.Fire(context.Background(), task); err != nil {
		t.Fatalf("third Fire() error = %v", err)
	}
	runs, err := manager.ListRuns()
	if err != nil || len(runs) != 2 {
		t.Fatalf("heartbeat runs = %#v, %v", runs, err)
	}
	if len(executor.tasks) != 2 {
		t.Fatalf("executor calls = %d, want 2", len(executor.tasks))
	}
}

func TestManagerStartSurvivesCorruptRunHistory(t *testing.T) {
	stateDir := t.TempDir()
	runsPath := statepath.AutomationRunsPath(stateDir)
	if err := os.WriteFile(runsPath, []byte("not json"), 0o600); err != nil {
		t.Fatalf("write corrupt history: %v", err)
	}
	var reported []error
	manager := NewManager(Config{
		StateDir: stateDir,
		OnError:  func(err error) { reported = append(reported, err) },
	})
	if err := manager.Start(&recordingExecutor{}); err != nil {
		t.Fatalf("Start() with corrupt history error = %v", err)
	}
	defer manager.Stop()
	runs, err := manager.ListRuns()
	if err != nil || len(runs) != 0 {
		t.Fatalf("runs after quarantine = %#v, %v", runs, err)
	}
	if len(reported) == 0 {
		t.Fatal("quarantine was not reported through OnError")
	}
	if _, statErr := os.Stat(runsPath); !os.IsNotExist(statErr) {
		t.Fatalf("corrupt history still at original path: %v", statErr)
	}
	quarantined, _ := filepath.Glob(runsPath + ".corrupt-*")
	if len(quarantined) != 1 {
		t.Fatalf("quarantined files = %#v", quarantined)
	}
}

func TestRunStoreFailIfPendingUnknownIDDoesNotWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runs.json")
	store := NewRunStore(path)
	if err := store.FailIfPending("unknown-queue-id", "queued turn removed"); err != nil {
		t.Fatalf("FailIfPending() error = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("no-op FailIfPending created the runs file: %v", err)
	}
}

func TestPruneRunsDropsOldestTerminalFirst(t *testing.T) {
	runs := make([]Run, 0, maxStoredRuns+10)
	for i := 0; i < 5; i++ {
		runs = append(runs, Run{ID: fmt.Sprintf("active-%d", i), Status: RunStatusRunning})
	}
	for i := 0; i < maxStoredRuns; i++ {
		runs = append(runs, Run{ID: fmt.Sprintf("done-%d", i), Status: RunStatusCompleted})
	}
	pruned := pruneRuns(runs)
	if len(pruned) != maxStoredRuns {
		t.Fatalf("pruned length = %d, want %d", len(pruned), maxStoredRuns)
	}
	for i := 0; i < 5; i++ {
		if pruned[i].ID != fmt.Sprintf("active-%d", i) {
			t.Fatalf("non-terminal run dropped: pruned[%d] = %#v", i, pruned[i])
		}
	}
	if pruned[5].ID != "done-5" {
		t.Fatalf("oldest terminal runs were not dropped first: pruned[5] = %#v", pruned[5])
	}
}
