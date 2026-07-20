package session

import (
	"errors"
	"testing"

	"github.com/blueberrycongee/wuu/internal/kanban"
	"github.com/blueberrycongee/wuu/internal/participant"
)

func seedKanbanTarget(t *testing.T, dir, name string) string {
	t.Helper()
	id := participant.NewID()
	if err := UpsertParticipant(dir, participant.Participant{
		ID: id, Kind: participant.KindNamed, Name: name,
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

func seedKanbanTask(t *testing.T, dir, sessionID, status string) kanban.Task {
	t.Helper()
	task, err := CreateKanbanTask(dir, kanban.Task{
		SessionID: sessionID, Title: "Build the thing", Brief: "goal + done criteria", Status: status,
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func TestKanbanTaskCreateAndList(t *testing.T) {
	dir := t.TempDir()
	root := seedKanbanTask(t, dir, "sess-1", "")
	if root.Status != kanban.TaskStatusDraft {
		t.Fatalf("default status = %q, want draft", root.Status)
	}
	child, err := CreateKanbanTask(dir, kanban.Task{
		SessionID: "sess-1", ParentID: root.ID, Title: "Subtask", Status: kanban.TaskStatusReady,
	})
	if err != nil {
		t.Fatal(err)
	}
	roots, err := ListKanbanTasks(dir, "sess-1", "")
	if err != nil || len(roots) != 1 {
		t.Fatalf("roots = %+v, %v", roots, err)
	}
	children, err := ListKanbanTasks(dir, "sess-1", root.ID)
	if err != nil || len(children) != 1 || children[0].ID != child.ID {
		t.Fatalf("children = %+v, %v", children, err)
	}
	other, err := ListKanbanTasks(dir, "sess-2", "")
	if err != nil || len(other) != 0 {
		t.Fatalf("cross-session leak: %+v, %v", other, err)
	}
}

func TestKanbanTaskTransitionGraph(t *testing.T) {
	dir := t.TempDir()
	task := seedKanbanTask(t, dir, "sess-1", "")
	if _, err := TransitionKanbanTaskStatus(dir, task.ID, kanban.TaskStatusDone); !errors.Is(err, kanban.ErrInvalidTransition) {
		t.Fatalf("draft->done = %v, want ErrInvalidTransition", err)
	}
	if _, err := TransitionKanbanTaskStatus(dir, task.ID, kanban.TaskStatusReady); err != nil {
		t.Fatal(err)
	}
	if _, err := TransitionKanbanTaskStatus(dir, task.ID, kanban.TaskStatusCancelled); err != nil {
		t.Fatal(err)
	}
	if _, err := TransitionKanbanTaskStatus(dir, task.ID, kanban.TaskStatusReady); !errors.Is(err, kanban.ErrInvalidTransition) {
		t.Fatalf("cancelled->ready = %v, want ErrInvalidTransition", err)
	}
}

func TestKanbanRunDispatchLifecycle(t *testing.T) {
	dir := t.TempDir()
	target := seedKanbanTarget(t, dir, "worker-1")
	task := seedKanbanTask(t, dir, "sess-1", kanban.TaskStatusReady)

	run, err := CreateKanbanRun(dir, kanban.Run{TaskID: task.ID, TargetID: target, CreatedBy: "human"})
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != kanban.RunStatusQueued || run.Kind != kanban.RunKindExecute {
		t.Fatalf("run = %+v", run)
	}
	gotTask, err := GetKanbanTask(dir, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotTask.Status != kanban.TaskStatusRunning || gotTask.LatestRunID != run.ID {
		t.Fatalf("task after dispatch = %+v", gotTask)
	}

	if _, err := CreateKanbanRun(dir, kanban.Run{TaskID: task.ID, TargetID: target}); !errors.Is(err, kanban.ErrTaskNotReady) {
		t.Fatalf("redispatch while running = %v, want ErrTaskNotReady", err)
	}

	if _, err := StartKanbanRun(dir, run.ID, "cth-exec"); err != nil {
		t.Fatal(err)
	}
	finished, err := CompleteKanbanRun(dir, run.ID, kanban.RunStatusSucceeded, "shipped", "")
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != kanban.RunStatusSucceeded {
		t.Fatalf("finished = %+v", finished)
	}
	gotTask, err = GetKanbanTask(dir, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotTask.Status != kanban.TaskStatusReview {
		t.Fatalf("task after success = %q, want review", gotTask.Status)
	}
}

func TestKanbanRunBusyLock(t *testing.T) {
	dir := t.TempDir()
	target := seedKanbanTarget(t, dir, "worker-1")
	other := seedKanbanTarget(t, dir, "worker-2")
	taskA := seedKanbanTask(t, dir, "sess-1", kanban.TaskStatusReady)
	taskB := seedKanbanTask(t, dir, "sess-1", kanban.TaskStatusReady)

	if _, err := CreateKanbanRun(dir, kanban.Run{TaskID: taskA.ID, TargetID: target}); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateKanbanRun(dir, kanban.Run{TaskID: taskB.ID, TargetID: target}); !errors.Is(err, kanban.ErrTargetBusy) {
		t.Fatalf("second dispatch to busy target = %v, want ErrTargetBusy", err)
	}
	if _, err := CreateKanbanRun(dir, kanban.Run{TaskID: taskB.ID, TargetID: other}); err != nil {
		t.Fatalf("dispatch to free target should succeed: %v", err)
	}
}

func TestKanbanRunFailedReturnsTaskToReady(t *testing.T) {
	dir := t.TempDir()
	target := seedKanbanTarget(t, dir, "worker-1")
	task := seedKanbanTask(t, dir, "sess-1", kanban.TaskStatusReady)
	run, err := CreateKanbanRun(dir, kanban.Run{TaskID: task.ID, TargetID: target})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CompleteKanbanRun(dir, run.ID, kanban.RunStatusFailed, "", "boom"); err != nil {
		t.Fatal(err)
	}
	gotTask, err := GetKanbanTask(dir, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotTask.Status != kanban.TaskStatusReady {
		t.Fatalf("task after failure = %q, want ready", gotTask.Status)
	}
	// Redispatch is legal again after failure frees both task and target.
	if _, err := CreateKanbanRun(dir, kanban.Run{TaskID: task.ID, TargetID: target}); err != nil {
		t.Fatalf("redispatch after failure: %v", err)
	}
}

func TestKanbanReviewKindAndRetiredTarget(t *testing.T) {
	dir := t.TempDir()
	target := seedKanbanTarget(t, dir, "worker-1")
	task := seedKanbanTask(t, dir, "sess-1", kanban.TaskStatusReady)
	run, err := CreateKanbanRun(dir, kanban.Run{TaskID: task.ID, TargetID: target, Kind: kanban.RunKindReview})
	if err != nil {
		t.Fatal(err)
	}
	if run.Kind != kanban.RunKindReview {
		t.Fatalf("kind = %q", run.Kind)
	}
	if _, err := CompleteKanbanRun(dir, run.ID, kanban.RunStatusSucceeded, "looks good", ""); err != nil {
		t.Fatal(err)
	}

	if err := RetireParticipant(dir, target); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateKanbanRun(dir, kanban.Run{TaskID: task.ID, TargetID: target}); err == nil {
		t.Fatal("dispatch to retired target should fail")
	}
}

func TestKanbanArtifacts(t *testing.T) {
	dir := t.TempDir()
	target := seedKanbanTarget(t, dir, "worker-1")
	task := seedKanbanTask(t, dir, "sess-1", kanban.TaskStatusReady)
	run, err := CreateKanbanRun(dir, kanban.Run{TaskID: task.ID, TargetID: target})
	if err != nil {
		t.Fatal(err)
	}
	a, err := AddKanbanArtifact(dir, kanban.Artifact{
		RunID: run.ID, TaskID: task.ID, SessionID: task.SessionID,
		Path: "out/design.png", DisplayName: "design.png", MediaType: "image/png", SizeBytes: 42,
	})
	if err != nil {
		t.Fatal(err)
	}
	list, err := ListKanbanArtifacts(dir, task.ID)
	if err != nil || len(list) != 1 || list[0].ID != a.ID || list[0].Path != "out/design.png" {
		t.Fatalf("artifacts = %+v, %v", list, err)
	}
}

func TestGetActiveKanbanRunByThreadID(t *testing.T) {
	dir := t.TempDir()
	target := seedKanbanTarget(t, dir, "worker-1")
	task := seedKanbanTask(t, dir, "sess-1", kanban.TaskStatusReady)
	run, err := CreateKanbanRun(dir, kanban.Run{TaskID: task.ID, TargetID: target})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := GetActiveKanbanRunByThreadID(dir, "agt-1"); !errors.Is(err, kanban.ErrRunNotFound) {
		t.Fatalf("unbound lookup = %v, want ErrRunNotFound", err)
	}
	if _, err := StartKanbanRun(dir, run.ID, "agt-1"); err != nil {
		t.Fatal(err)
	}
	active, err := GetActiveKanbanRunByThreadID(dir, "agt-1")
	if err != nil || active.ID != run.ID {
		t.Fatalf("active = %+v, %v", active, err)
	}
	if _, err := CompleteKanbanRun(dir, run.ID, kanban.RunStatusSucceeded, "done", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := GetActiveKanbanRunByThreadID(dir, "agt-1"); !errors.Is(err, kanban.ErrRunNotFound) {
		t.Fatalf("terminal run must not be active: %v", err)
	}
}
