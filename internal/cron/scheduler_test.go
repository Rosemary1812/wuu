package cron

import (
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestScheduler_fireOneShot(t *testing.T) {
	store := NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))

	var fired atomic.Int32
	done := make(chan struct{}, 1)
	onFire := func(task Task) {
		if task.Prompt == "" {
			t.Fatal("expected fired task prompt")
		}
		fired.Add(1)
		done <- struct{}{}
	}

	s := NewScheduler(SchedulerConfig{
		Store:   store,
		OnFire:  onFire,
		IsOwner: func() bool { return true },
	})

	task := Task{
		ID:        "oneshot-1",
		Cron:      "* * * * *",
		Prompt:    "hello",
		CreatedAt: time.Now().Add(-2 * time.Minute).UnixMilli(),
		Recurring: false,
	}
	store.Add(task)

	s.Start()
	defer s.Stop()

	s.check()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for one-shot task fire")
	}

	if fired.Load() != 1 {
		t.Fatalf("expected 1 fire, got %d", fired.Load())
	}

	tasks, _ := store.List()
	if len(tasks) != 0 {
		t.Fatalf("expected task removed after one-shot fire, got %d", len(tasks))
	}
}

func TestScheduler_recurringUpdatesLastFired(t *testing.T) {
	store := NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))

	var fired atomic.Int32
	done := make(chan struct{}, 1)
	s := NewScheduler(SchedulerConfig{
		Store: store,
		OnFire: func(Task) {
			fired.Add(1)
			done <- struct{}{}
		},
		IsOwner: func() bool { return true },
	})

	task := Task{
		ID:        "rec-1",
		Cron:      "* * * * *",
		Prompt:    "ping",
		CreatedAt: time.Now().Add(-2 * time.Minute).UnixMilli(),
		Recurring: true,
	}
	store.Add(task)

	s.Start()
	defer s.Stop()
	s.check()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for recurring task fire")
	}

	if fired.Load() != 1 {
		t.Fatalf("expected 1 fire, got %d", fired.Load())
	}

	tasks, _ := store.List()
	if len(tasks) != 1 {
		t.Fatalf("expected task to remain, got %d", len(tasks))
	}
	if tasks[0].LastFiredAt == 0 {
		t.Fatal("expected LastFiredAt to be updated")
	}
}

func TestScheduler_sessionTasksFireWithoutOwnerLock(t *testing.T) {
	fileStore := NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
	sessionStore := NewSessionTaskStore(t.TempDir())

	if err := fileStore.Add(Task{
		ID:        "durable-1",
		Cron:      "* * * * *",
		Prompt:    "durable",
		CreatedAt: time.Now().Add(-2 * time.Minute).UnixMilli(),
		Recurring: false,
	}); err != nil {
		t.Fatalf("fileStore.Add: %v", err)
	}
	if err := sessionStore.Add(Task{
		ID:        "session-1",
		Cron:      "* * * * *",
		Prompt:    "session",
		CreatedAt: time.Now().Add(-2 * time.Minute).UnixMilli(),
		Recurring: false,
	}); err != nil {
		t.Fatalf("sessionStore.Add: %v", err)
	}

	var fired []string
	done := make(chan struct{}, 1)
	s := NewScheduler(SchedulerConfig{
		Store:        fileStore,
		SessionStore: sessionStore,
		OnFire: func(task Task) {
			fired = append(fired, task.Prompt)
			done <- struct{}{}
		},
		IsOwner: func() bool { return false },
	})

	s.check()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for session task fire")
	}

	if len(fired) != 1 || fired[0] != "session" {
		t.Fatalf("expected only session task to fire, got %#v", fired)
	}

	fileTasks, _ := fileStore.List()
	if len(fileTasks) != 1 {
		t.Fatalf("expected durable task to remain untouched, got %d", len(fileTasks))
	}
	sessionTasks, _ := sessionStore.List()
	if len(sessionTasks) != 0 {
		t.Fatalf("expected session task removed after fire, got %d", len(sessionTasks))
	}
}

func TestScheduler_firesMetadataTasksThroughPromptCallback(t *testing.T) {
	store := NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))

	done := make(chan Task, 1)
	s := NewScheduler(SchedulerConfig{
		Store: store,
		OnFire: func(task Task) {
			done <- task
		},
		IsOwner: func() bool { return true },
	})

	if err := store.Add(Task{
		ID:        "workflow-1",
		Cron:      "* * * * *",
		Prompt:    "Run workflow weekly-qa with arguments: settings",
		Metadata:  map[string]string{"kind": "workflow", "workflow_name": "weekly-qa"},
		CreatedAt: time.Now().Add(-2 * time.Minute).UnixMilli(),
		Recurring: false,
	}); err != nil {
		t.Fatalf("store.Add: %v", err)
	}

	s.check()

	select {
	case task := <-done:
		if task.Prompt == "" || task.Metadata["workflow_name"] != "weekly-qa" {
			t.Fatalf("unexpected fired task: %+v", task)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for task fire")
	}
}

// TestScheduler_StartCatchesUpMissedOneShots is the crash/closed-workspace
// story for repair item #10: a one-shot task came due while no scheduler was
// running. Start must fire it exactly once via the FindMissedOneShots
// catch-up pass — without waiting for a tick — and remove it from the store.
func TestScheduler_StartCatchesUpMissedOneShots(t *testing.T) {
	fileStore := NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
	sessionStore := NewSessionTaskStore(t.TempDir())

	// Due yesterday at a fixed time — plainly missed.
	missedAt := time.Now().Add(-24 * time.Hour)
	if err := fileStore.Add(Task{
		ID:        "missed-durable",
		Cron:      missedCronFor(missedAt),
		Prompt:    "missed durable",
		CreatedAt: missedAt.Add(-time.Hour).UnixMilli(),
		Recurring: false,
	}); err != nil {
		t.Fatalf("fileStore.Add: %v", err)
	}
	if err := sessionStore.Add(Task{
		ID:        "missed-session",
		Cron:      missedCronFor(missedAt),
		Prompt:    "missed session",
		CreatedAt: missedAt.Add(-time.Hour).UnixMilli(),
		Recurring: false,
	}); err != nil {
		t.Fatalf("sessionStore.Add: %v", err)
	}
	// A recurring task must never be caught up by the one-shot pass.
	if err := fileStore.Add(Task{
		ID:        "recurring-untouched",
		Cron:      "0 0 1 1 *",
		Prompt:    "recurring",
		CreatedAt: missedAt.Add(-time.Hour).UnixMilli(),
		Recurring: true,
	}); err != nil {
		t.Fatalf("fileStore.Add recurring: %v", err)
	}

	firedCh := make(chan string, 4)
	s := NewScheduler(SchedulerConfig{
		Store:        fileStore,
		SessionStore: sessionStore,
		OnFire: func(task Task) {
			firedCh <- task.Prompt
		},
		IsOwner: func() bool { return true },
	})

	s.Start()
	defer s.Stop()

	fired := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case prompt := <-firedCh:
			fired[prompt] = true
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for catch-up fires, got %#v", fired)
		}
	}
	if !fired["missed durable"] || !fired["missed session"] {
		t.Fatalf("expected both missed one-shots to fire, got %#v", fired)
	}

	fileTasks, _ := fileStore.List()
	if len(fileTasks) != 1 || fileTasks[0].ID != "recurring-untouched" {
		t.Fatalf("expected only the recurring task to remain, got %#v", fileTasks)
	}
	sessionTasks, _ := sessionStore.List()
	if len(sessionTasks) != 0 {
		t.Fatalf("expected missed session one-shot removed, got %#v", sessionTasks)
	}

	select {
	case prompt := <-firedCh:
		t.Fatalf("unexpected extra fire %q (double-fire or recurring backfill)", prompt)
	case <-time.After(1500 * time.Millisecond):
	}
}

// TestScheduler_CatchUpRespectsOwnerLock asserts the catch-up pass honors
// the durable-store ownership gate: a non-owner must not fire durable
// one-shots, while session tasks still catch up.
func TestScheduler_CatchUpRespectsOwnerLock(t *testing.T) {
	fileStore := NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
	sessionStore := NewSessionTaskStore(t.TempDir())
	missedAt := time.Now().Add(-24 * time.Hour)

	if err := fileStore.Add(Task{
		ID:        "missed-durable",
		Cron:      missedCronFor(missedAt),
		Prompt:    "missed durable",
		CreatedAt: missedAt.Add(-time.Hour).UnixMilli(),
		Recurring: false,
	}); err != nil {
		t.Fatalf("fileStore.Add: %v", err)
	}
	if err := sessionStore.Add(Task{
		ID:        "missed-session",
		Cron:      missedCronFor(missedAt),
		Prompt:    "missed session",
		CreatedAt: missedAt.Add(-time.Hour).UnixMilli(),
		Recurring: false,
	}); err != nil {
		t.Fatalf("sessionStore.Add: %v", err)
	}

	firedCh := make(chan string, 2)
	s := NewScheduler(SchedulerConfig{
		Store:        fileStore,
		SessionStore: sessionStore,
		OnFire:       func(task Task) { firedCh <- task.Prompt },
		IsOwner:      func() bool { return false },
	})

	s.catchUpMissedOneShots(time.Now())

	select {
	case prompt := <-firedCh:
		if prompt != "missed session" {
			t.Fatalf("non-owner fired durable task %q", prompt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for session catch-up fire")
	}
	fileTasks, _ := fileStore.List()
	if len(fileTasks) != 1 {
		t.Fatalf("durable one-shot must remain for the owner, got %#v", fileTasks)
	}
}

// missedCronFor renders an explicit single-occurrence cron expression for
// the given time, the shape one-shot scheduling uses.
func missedCronFor(at time.Time) string {
	return fmt.Sprintf("%d %d %d %d *", at.Minute(), at.Hour(), at.Day(), int(at.Month()))
}
