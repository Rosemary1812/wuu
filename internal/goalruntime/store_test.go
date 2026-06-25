package goalruntime

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/statepath"
)

func TestStoreCreateLoadAndRejectUnfinishedReplacement(t *testing.T) {
	now := time.Date(2026, 6, 25, 11, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "goal_runtime.json"))
	store.SetClock(func() time.Time { return now })

	goal, err := store.Create(Spec{ThreadID: "thread-1", GoalID: "goal-1", Objective: "ship runtime"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if goal.Status != StatusActive {
		t.Fatalf("Status = %s, want active", goal.Status)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.GoalID != "goal-1" || loaded.Objective != "ship runtime" {
		t.Fatalf("unexpected loaded goal: %+v", loaded)
	}

	_, err = store.Create(Spec{ThreadID: "thread-1", GoalID: "goal-2", Objective: "replace"})
	if err == nil {
		t.Fatal("expected unfinished replacement to fail")
	}
	if !strings.Contains(err.Error(), "unfinished goal") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStoreCreateReplacesTerminalGoal(t *testing.T) {
	now := time.Date(2026, 6, 25, 11, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "goal_runtime.json"))
	store.SetClock(func() time.Time { return now })

	goal, err := store.Create(Spec{ThreadID: "thread-1", GoalID: "goal-1", Objective: "first"})
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	goal, err = goal.Complete(now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if err := store.Save(goal); err != nil {
		t.Fatalf("Save complete: %v", err)
	}
	next, err := store.Create(Spec{ThreadID: "thread-1", GoalID: "goal-2", Objective: "second"})
	if err != nil {
		t.Fatalf("Create second after terminal: %v", err)
	}
	if next.GoalID != "goal-2" || next.Status != StatusActive {
		t.Fatalf("unexpected replacement: %+v", next)
	}
}

func TestStoreUpdateAndClear(t *testing.T) {
	now := time.Date(2026, 6, 25, 11, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "nested", "goal_runtime.json"))
	store.SetClock(func() time.Time { return now })

	if _, err := store.Create(Spec{ThreadID: "thread-1", GoalID: "goal-1", Objective: "ship runtime"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	updated, err := store.Update(func(goal Goal) (Goal, error) {
		return goal.AccountUsage(UsageDelta{Tokens: 10, Turns: 1}, now.Add(time.Minute))
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.TokensUsed != 10 || updated.GoalTurns != 1 {
		t.Fatalf("unexpected updated goal: %+v", updated)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load updated: %v", err)
	}
	if loaded.TokensUsed != 10 || loaded.GoalTurns != 1 {
		t.Fatalf("update was not persisted: %+v", loaded)
	}
	if err := store.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, err := store.Load(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load after clear err = %v, want os.ErrNotExist", err)
	}
}

func TestThreadGoalRuntimePath(t *testing.T) {
	got := statepath.ThreadGoalRuntimePath("/tmp/wuu-state", "thread-1")
	want := filepath.Join("/tmp/wuu-state", "sessions", "thread-1", "goal_runtime.json")
	if got != want {
		t.Fatalf("ThreadGoalRuntimePath = %q, want %q", got, want)
	}
}

func TestStoreRejectsInvalidSavedGoal(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "goal_runtime.json"))
	err := store.Save(Goal{ThreadID: "thread-1", GoalID: "goal-1", Objective: "ship", Status: Status("missing")})
	if err == nil {
		t.Fatal("expected invalid status error")
	}
	if !strings.Contains(err.Error(), "unknown goal runtime status") {
		t.Fatalf("unexpected error: %v", err)
	}
}
