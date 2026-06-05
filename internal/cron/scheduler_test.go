package cron

import (
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestScheduler_fireOneShot(t *testing.T) {
	store := NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))

	var fired atomic.Int32
	onFire := func(task Task) {
		if task.Prompt == "" {
			t.Fatal("expected fired task prompt")
		}
		fired.Add(1)
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
	s := NewScheduler(SchedulerConfig{
		Store:   store,
		OnFire:  func(Task) { fired.Add(1) },
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

func TestScheduler_routesWorkflowTasks(t *testing.T) {
	store := NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))

	var promptFired atomic.Int32
	var workflowFired atomic.Int32
	s := NewScheduler(SchedulerConfig{
		Store:  store,
		OnFire: func(Task) { promptFired.Add(1) },
		OnWorkflowFire: func(task Task) {
			if task.WorkflowName != "weekly-qa" {
				t.Fatalf("unexpected workflow task: %+v", task)
			}
			workflowFired.Add(1)
		},
		IsOwner: func() bool { return true },
	})

	if err := store.Add(Task{
		ID:                "workflow-1",
		Cron:              "* * * * *",
		WorkflowName:      "weekly-qa",
		WorkflowArguments: "settings",
		CreatedAt:         time.Now().Add(-2 * time.Minute).UnixMilli(),
		Recurring:         false,
	}); err != nil {
		t.Fatalf("store.Add: %v", err)
	}

	s.check()

	if workflowFired.Load() != 1 {
		t.Fatalf("expected workflow fire, got %d", workflowFired.Load())
	}
	if promptFired.Load() != 0 {
		t.Fatalf("workflow task should not use prompt OnFire when OnWorkflowFire is set, got %d", promptFired.Load())
	}
}
