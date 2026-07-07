package session

import (
	"testing"
	"time"
)

func TestTaskTraceAppendOnlyAndOrdered(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "grp", "/tmp/p"); err != nil {
		t.Fatal(err)
	}
	const taskID = "cth-task-1"

	// Append a lifecycle in order; seq is assigned per-task monotonically.
	for _, kind := range []string{
		TaskEventTaskCreated, TaskEventWorkflowPlanned,
		TaskEventNodeStarted, TaskEventNodeSucceeded, TaskEventLeadInvoked,
	} {
		ev, err := AppendTaskEvent(dir, TaskEvent{
			SessionID: "grp", TaskID: taskID, Kind: kind, Actor: "prt-lead", At: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("append %s: %v", kind, err)
		}
		if ev.ID == "" || ev.Seq <= 0 {
			t.Fatalf("append %s returned no id/seq: %+v", kind, ev)
		}
	}

	events, err := TaskEvents(dir, taskID)
	if err != nil {
		t.Fatalf("TaskEvents: %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("expected 5 events, got %d", len(events))
	}
	wantKinds := []string{
		TaskEventTaskCreated, TaskEventWorkflowPlanned,
		TaskEventNodeStarted, TaskEventNodeSucceeded, TaskEventLeadInvoked,
	}
	for i, ev := range events {
		if ev.Seq != i+1 {
			t.Fatalf("event %d seq = %d, want %d", i, ev.Seq, i+1)
		}
		if ev.Kind != wantKinds[i] {
			t.Fatalf("event %d kind = %q, want %q", i, ev.Kind, wantKinds[i])
		}
	}

	// A second task keeps its own seq space.
	if ev, err := AppendTaskEvent(dir, TaskEvent{TaskID: "cth-task-2", Kind: TaskEventTaskCreated}); err != nil {
		t.Fatal(err)
	} else if ev.Seq != 1 {
		t.Fatalf("second task first event seq = %d, want 1", ev.Seq)
	}

	// task_id and kind are required.
	if _, err := AppendTaskEvent(dir, TaskEvent{Kind: TaskEventTaskCreated}); err == nil {
		t.Fatal("expected error for missing task_id")
	}
	if _, err := AppendTaskEvent(dir, TaskEvent{TaskID: taskID}); err == nil {
		t.Fatal("expected error for missing kind")
	}
}
