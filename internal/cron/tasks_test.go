package cron

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestTaskStore_CRUD(t *testing.T) {
	dir := t.TempDir()
	store := NewTaskStore(filepath.Join(dir, "tasks.json"))

	task := Task{
		ID:        "test-1",
		Cron:      "*/5 * * * *",
		Prompt:    "check deploy",
		CreatedAt: time.Now().UnixMilli(),
		Recurring: true,
	}

	if err := store.Add(task); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 task, got %d", len(list))
	}

	if err := store.Remove("test-1"); err != nil {
		t.Fatalf("Remove error: %v", err)
	}

	list, _ = store.List()
	if len(list) != 0 {
		t.Fatalf("expected 0 tasks after remove, got %d", len(list))
	}
}

func TestTaskStore_MaxJobs(t *testing.T) {
	dir := t.TempDir()
	store := NewTaskStore(filepath.Join(dir, "tasks.json"))

	for i := 0; i < MaxJobs; i++ {
		store.Add(Task{ID: fmt.Sprintf("t%d", i), Cron: "* * * * *", Prompt: "x"})
	}

	err := store.Add(Task{ID: "overflow", Cron: "* * * * *", Prompt: "x"})
	if err == nil {
		t.Fatal("expected error when exceeding max jobs")
	}
}

func TestTaskStore_ConcurrentAddsPreserveEveryTask(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	const taskCount = 40

	start := make(chan struct{})
	errs := make(chan error, taskCount)
	var wg sync.WaitGroup
	for i := 0; i < taskCount; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs <- NewTaskStore(path).Add(Task{
				ID:     fmt.Sprintf("task-%02d", i),
				Cron:   "* * * * *",
				Prompt: "concurrent task",
			})
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Add: %v", err)
		}
	}

	tasks, err := NewTaskStore(path).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != taskCount {
		t.Fatalf("stored tasks = %d, want %d", len(tasks), taskCount)
	}
	seen := make(map[string]bool, taskCount)
	for _, task := range tasks {
		seen[task.ID] = true
	}
	for i := 0; i < taskCount; i++ {
		id := fmt.Sprintf("task-%02d", i)
		if !seen[id] {
			t.Fatalf("concurrent Add lost %s", id)
		}
	}
}

func TestTaskStore_ConcurrentClaimHasOneWinner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	task := Task{
		ID:        "claim-once",
		Cron:      "* * * * *",
		Prompt:    "one winner",
		CreatedAt: time.Now().Add(-2 * time.Minute).UnixMilli(),
	}
	if err := NewTaskStore(path).Add(task); err != nil {
		t.Fatalf("Add: %v", err)
	}

	start := make(chan struct{})
	results := make(chan bool, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			claimed, err := NewTaskStore(path).ClaimForDispatch(task, time.Now().UnixMilli())
			results <- claimed
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)

	winners := 0
	for claimed := range results {
		if claimed {
			winners++
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("ClaimForDispatch: %v", err)
		}
	}
	if winners != 1 {
		t.Fatalf("claim winners = %d, want 1", winners)
	}
	tasks, err := NewTaskStore(path).List()
	if err != nil || len(tasks) != 0 {
		t.Fatalf("one-shot after claim: tasks=%#v err=%v", tasks, err)
	}
}

func TestTaskStoresRejectDuplicateIDs(t *testing.T) {
	task := Task{ID: "duplicate", Cron: "* * * * *", Prompt: "first"}
	stores := []interface{ Add(Task) error }{
		NewTaskStore(filepath.Join(t.TempDir(), "tasks.json")),
		NewSessionTaskStore(t.TempDir()),
	}
	for _, store := range stores {
		if err := store.Add(task); err != nil {
			t.Fatalf("first Add: %v", err)
		}
		if err := store.Add(task); err == nil {
			t.Fatal("duplicate task ID was accepted")
		}
	}
}

func TestSessionTaskStore_CRUD(t *testing.T) {
	store := NewSessionTaskStore(t.TempDir())

	task := Task{
		ID:        "session-1",
		Cron:      "*/5 * * * *",
		Prompt:    "check deploy",
		CreatedAt: time.Now().UnixMilli(),
		Recurring: true,
	}

	if err := store.Add(task); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 task, got %d", len(list))
	}

	claimed, err := store.ClaimForDispatch(task, 123)
	if err != nil {
		t.Fatalf("ClaimForDispatch error: %v", err)
	}
	if !claimed {
		t.Fatal("expected recurring task claim")
	}

	list, err = store.List()
	if err != nil {
		t.Fatalf("List after update error: %v", err)
	}
	if list[0].LastFiredAt != 123 {
		t.Fatalf("expected LastFiredAt=123, got %d", list[0].LastFiredAt)
	}

	if err := store.Remove("session-1"); err != nil {
		t.Fatalf("Remove error: %v", err)
	}

	list, _ = store.List()
	if len(list) != 0 {
		t.Fatalf("expected 0 tasks after remove, got %d", len(list))
	}
}

func TestJitteredNextRun_recurring(t *testing.T) {
	ce, _ := ParseCronExpression("*/5 * * * *")
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	jittered, err := JitteredNextRun(ce, "task-1", now, true)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	base, _ := ce.NextRun(now)
	if !jittered.After(base) && !jittered.Equal(base) {
		t.Fatalf("jittered %v should be >= base %v", jittered, base)
	}
	maxJittered := base.Add(15 * time.Minute)
	if jittered.After(maxJittered) {
		t.Fatalf("jittered %v exceeds cap %v", jittered, maxJittered)
	}
}

func TestIsExpired(t *testing.T) {
	now := time.Now().UnixMilli()
	old := now - (8 * 24 * 60 * 60 * 1000)

	task := Task{
		CreatedAt: old,
		Recurring: true,
	}
	if !IsExpired(task, now) {
		t.Fatal("expected expired task")
	}

	task.CreatedAt = now - (6 * 24 * 60 * 60 * 1000)
	if IsExpired(task, now) {
		t.Fatal("expected non-expired task")
	}
}
