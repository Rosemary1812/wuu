package cron

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestScheduler_fireOneShot(t *testing.T) {
	store := NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))

	var fired atomic.Int32
	done := make(chan struct{}, 1)
	onFire := func(_ context.Context, task Task) error {
		if task.Prompt == "" {
			t.Fatal("expected fired task prompt")
		}
		fired.Add(1)
		done <- struct{}{}
		return nil
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
		OnFire: func(context.Context, Task) error {
			fired.Add(1)
			done <- struct{}{}
			return nil
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
		OnFire: func(_ context.Context, task Task) error {
			fired = append(fired, task.Prompt)
			done <- struct{}{}
			return nil
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
		OnFire: func(_ context.Context, task Task) error {
			done <- task
			return nil
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
// running. Start must claim and dispatch one attempt via the catch-up pass —
// without waiting for a tick — and remove it from the store.
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
	if err := fileStore.Add(Task{
		ID:        "paused-missed",
		Cron:      missedCronFor(missedAt),
		Prompt:    "paused missed",
		CreatedAt: missedAt.Add(-time.Hour).UnixMilli(),
		Recurring: false,
		Paused:    true,
	}); err != nil {
		t.Fatalf("fileStore.Add paused: %v", err)
	}

	firedCh := make(chan string, 4)
	s := NewScheduler(SchedulerConfig{
		Store:        fileStore,
		SessionStore: sessionStore,
		OnFire: func(_ context.Context, task Task) error {
			firedCh <- task.Prompt
			return nil
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
	remaining := map[string]bool{}
	for _, task := range fileTasks {
		remaining[task.ID] = true
	}
	if len(fileTasks) != 2 || !remaining["recurring-untouched"] || !remaining["paused-missed"] {
		t.Fatalf("expected recurring and paused tasks to remain, got %#v", fileTasks)
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
		OnFire: func(_ context.Context, task Task) error {
			firedCh <- task.Prompt
			return nil
		},
		IsOwner: func() bool { return false },
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

func TestScheduler_claimsBeforeDispatch(t *testing.T) {
	store := NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
	task := Task{
		ID:        "claim-before-dispatch",
		Cron:      "* * * * *",
		Prompt:    "verify claim",
		CreatedAt: time.Now().Add(-2 * time.Minute).UnixMilli(),
	}
	if err := store.Add(task); err != nil {
		t.Fatalf("Add: %v", err)
	}

	result := make(chan error, 1)
	s := NewScheduler(SchedulerConfig{
		Store:   store,
		IsOwner: func() bool { return true },
		OnFire: func(context.Context, Task) error {
			tasks, err := store.List()
			if err != nil {
				result <- err
				return nil
			}
			if len(tasks) != 0 {
				result <- fmt.Errorf("one-shot still present during callback: %#v", tasks)
				return nil
			}
			result <- nil
			return nil
		},
	})
	t.Cleanup(s.Stop)
	s.check()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestScheduler_claimFailureDoesNotDispatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	store := NewTaskStore(path)
	task := Task{
		ID:        "blocked-claim",
		Cron:      "* * * * *",
		Prompt:    "must not run before persistence",
		CreatedAt: time.Now().Add(-2 * time.Minute).UnixMilli(),
	}
	if err := store.Add(task); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := os.Remove(path + ".lock"); err != nil {
		t.Fatalf("Remove lock sidecar: %v", err)
	}
	if err := os.Mkdir(path+".lock", 0o700); err != nil {
		t.Fatalf("replace lock sidecar with directory: %v", err)
	}

	fired := make(chan struct{}, 1)
	errorsCh := make(chan error, 2)
	s := NewScheduler(SchedulerConfig{
		Store:   store,
		IsOwner: func() bool { return true },
		OnFire: func(context.Context, Task) error {
			fired <- struct{}{}
			return nil
		},
		OnError: func(err error) { errorsCh <- err },
	})
	t.Cleanup(s.Stop)
	s.check()

	select {
	case <-fired:
		t.Fatal("callback ran despite failed durable claim")
	case err := <-errorsCh:
		if !strings.Contains(err.Error(), "claim scheduled task") {
			t.Fatalf("unexpected scheduler error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("claim failure was not reported")
	}
	tasks, err := store.List()
	if err != nil || len(tasks) != 1 {
		t.Fatalf("task changed after failed claim: tasks=%#v err=%v", tasks, err)
	}

	if err := os.Remove(path + ".lock"); err != nil {
		t.Fatalf("remove invalid lock directory: %v", err)
	}
	s.check()
	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("task did not run after persistence recovered")
	}
}

func TestScheduler_concurrentSchedulersDispatchOneAttempt(t *testing.T) {
	store := NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
	if err := store.Add(Task{
		ID:        "shared-task",
		Cron:      "* * * * *",
		Prompt:    "run once",
		CreatedAt: time.Now().Add(-2 * time.Minute).UnixMilli(),
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	fired := make(chan struct{}, 2)
	newScheduler := func() *Scheduler {
		return NewScheduler(SchedulerConfig{
			Store:   NewTaskStore(store.path),
			IsOwner: func() bool { return true },
			OnFire: func(context.Context, Task) error {
				fired <- struct{}{}
				return nil
			},
		})
	}
	first, second := newScheduler(), newScheduler()
	t.Cleanup(first.Stop)
	t.Cleanup(second.Stop)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); first.check() }()
	go func() { defer wg.Done(); second.check() }()
	wg.Wait()

	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("expected one dispatch")
	}
	select {
	case <-fired:
		t.Fatal("same occurrence dispatched by both schedulers")
	case <-time.After(150 * time.Millisecond):
	}
}

func TestScheduler_callbackErrorConsumesOccurrence(t *testing.T) {
	store := NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
	if err := store.Add(Task{
		ID:        "failing-callback",
		Cron:      "* * * * *",
		Prompt:    "fail once",
		CreatedAt: time.Now().Add(-2 * time.Minute).UnixMilli(),
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	var calls atomic.Int32
	errorsCh := make(chan error, 1)
	s := NewScheduler(SchedulerConfig{
		Store:   store,
		IsOwner: func() bool { return true },
		OnFire: func(context.Context, Task) error {
			calls.Add(1)
			return errors.New("callback failed")
		},
		OnError: func(err error) { errorsCh <- err },
	})
	t.Cleanup(s.Stop)
	s.check()
	select {
	case err := <-errorsCh:
		if !strings.Contains(err.Error(), "callback failed") {
			t.Fatalf("unexpected callback error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("callback failure was not reported")
	}
	s.check()
	if got := calls.Load(); got != 1 {
		t.Fatalf("callback calls = %d, want one at-most-once attempt", got)
	}
}

func TestScheduler_durableListFailureDoesNotBlockSessionTask(t *testing.T) {
	dir := t.TempDir()
	durablePath := filepath.Join(dir, "tasks.json")
	if err := os.WriteFile(durablePath, []byte("not json"), 0o600); err != nil {
		t.Fatalf("write corrupt durable store: %v", err)
	}
	sessionStore := NewSessionTaskStore(t.TempDir())
	if err := sessionStore.Add(Task{
		ID:        "session-task",
		Cron:      "* * * * *",
		Prompt:    "session survives",
		CreatedAt: time.Now().Add(-2 * time.Minute).UnixMilli(),
	}); err != nil {
		t.Fatalf("session Add: %v", err)
	}

	fired := make(chan struct{}, 1)
	errorsCh := make(chan error, 1)
	s := NewScheduler(SchedulerConfig{
		Store:        NewTaskStore(durablePath),
		SessionStore: sessionStore,
		IsOwner:      func() bool { return true },
		OnFire: func(context.Context, Task) error {
			fired <- struct{}{}
			return nil
		},
		OnError: func(err error) { errorsCh <- err },
	})
	t.Cleanup(s.Stop)
	s.check()
	select {
	case <-errorsCh:
	case <-time.After(time.Second):
		t.Fatal("durable list failure was not reported")
	}
	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("durable list failure blocked session task")
	}
}

func TestScheduler_StopCancelsAndWaitsForCallbacks(t *testing.T) {
	store := NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
	if err := store.Add(Task{
		ID:        "blocking-callback",
		Cron:      "* * * * *",
		Prompt:    "wait for shutdown",
		CreatedAt: time.Now().Add(-2 * time.Minute).UnixMilli(),
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	started := make(chan struct{})
	cancelled := make(chan struct{})
	release := make(chan struct{})
	s := NewScheduler(SchedulerConfig{
		Store:   store,
		IsOwner: func() bool { return true },
		OnFire: func(ctx context.Context, _ Task) error {
			close(started)
			<-ctx.Done()
			close(cancelled)
			<-release
			return nil
		},
	})
	s.check()
	<-started

	stopped := make(chan struct{})
	go func() {
		s.Stop()
		close(stopped)
	}()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("Stop did not cancel callback context")
	}
	select {
	case <-stopped:
		t.Fatal("Stop returned while callback was still active")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop did not return after callback exited")
	}
	s.Stop()
}
