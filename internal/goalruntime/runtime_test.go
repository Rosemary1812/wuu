package goalruntime

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRuntimeDecideContinuationAllowsIdleActiveGoal(t *testing.T) {
	runtime := newTestRuntime(t)
	if _, err := runtime.Create(Spec{ThreadID: "thread-1", GoalID: "goal-1", Objective: "ship runtime"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	decision, err := runtime.DecideContinuation(ContinuationInput{ThreadIdle: true})
	if err != nil {
		t.Fatalf("DecideContinuation: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected continuation allowed, got %+v", decision)
	}
	if decision.Status != StatusActive {
		t.Fatalf("Status = %s, want active", decision.Status)
	}
}

func TestRuntimeDecideContinuationRejectsUnsafeConditions(t *testing.T) {
	tests := []struct {
		name  string
		input ContinuationInput
		want  ContinuationBlockReason
	}{
		{
			name:  "not idle",
			input: ContinuationInput{},
			want:  ContinuationBlockedNotIdle,
		},
		{
			name:  "active turn",
			input: ContinuationInput{ThreadIdle: true, ActiveTurn: true},
			want:  ContinuationBlockedBusy,
		},
		{
			name:  "queued user work",
			input: ContinuationInput{ThreadIdle: true, QueuedUserWork: true},
			want:  ContinuationBlockedQueuedUserWork,
		},
		{
			name:  "queued agent work",
			input: ContinuationInput{ThreadIdle: true, QueuedAgentWork: true},
			want:  ContinuationBlockedQueuedAgentWork,
		},
		{
			name:  "awaiting background work",
			input: ContinuationInput{ThreadIdle: true, AwaitingBackgroundWork: true},
			want:  ContinuationBlockedAwaitingBackground,
		},
		{
			name:  "read only",
			input: ContinuationInput{ThreadIdle: true, ReadOnly: true},
			want:  ContinuationBlockedReadOnly,
		},
		{
			name:  "plan mode",
			input: ContinuationInput{ThreadIdle: true, PlanMode: true},
			want:  ContinuationBlockedPlanMode,
		},
		{
			name:  "unsafe protocol",
			input: ContinuationInput{ThreadIdle: true, UnsafeProtocolState: true},
			want:  ContinuationBlockedUnsafeProtocolState,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime := newTestRuntime(t)
			if _, err := runtime.Create(Spec{ThreadID: "thread-1", GoalID: "goal-1", Objective: "ship runtime"}); err != nil {
				t.Fatalf("Create: %v", err)
			}
			decision, err := runtime.DecideContinuation(tt.input)
			if err != nil {
				t.Fatalf("DecideContinuation: %v", err)
			}
			if decision.Allowed || decision.Reason != tt.want {
				t.Fatalf("decision = %+v, want blocked reason %s", decision, tt.want)
			}
		})
	}
}

func TestRuntimeDecideContinuationRejectsNonActiveGoal(t *testing.T) {
	runtime := newTestRuntime(t)
	if _, err := runtime.Create(Spec{ThreadID: "thread-1", GoalID: "goal-1", Objective: "ship runtime"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := runtime.SetUserStatus(StatusPaused, time.Now()); err != nil {
		t.Fatalf("SetUserStatus paused: %v", err)
	}
	decision, err := runtime.DecideContinuation(ContinuationInput{ThreadIdle: true})
	if err != nil {
		t.Fatalf("DecideContinuation: %v", err)
	}
	if decision.Allowed || decision.Reason != ContinuationBlockedGoalNotActive || decision.Status != StatusPaused {
		t.Fatalf("unexpected paused decision: %+v", decision)
	}
}

func TestRuntimeAccountingAndBlockerPersistence(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	runtime := newTestRuntime(t)
	if _, err := runtime.Create(Spec{ThreadID: "thread-1", GoalID: "goal-1", Objective: "ship runtime"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	goal, err := runtime.AccountUsage(UsageDelta{Tokens: 10, Turns: 1}, now)
	if err != nil {
		t.Fatalf("AccountUsage: %v", err)
	}
	if goal.Status != StatusActive {
		t.Fatalf("Status = %s, want active", goal.Status)
	}
	loaded, err := runtime.CurrentGoal()
	if err != nil {
		t.Fatalf("CurrentGoal: %v", err)
	}
	if loaded.Status != StatusActive || loaded.TokensUsed != 10 {
		t.Fatalf("usage not persisted: %+v", loaded)
	}

	runtime = newTestRuntime(t)
	if _, err := runtime.Create(Spec{ThreadID: "thread-1", GoalID: "goal-1", Objective: "ship runtime"}); err != nil {
		t.Fatalf("Create blocker runtime: %v", err)
	}
	var blocked bool
	for i := 0; i < RequiredBlockerTurns; i++ {
		var err error
		goal, blocked, err = runtime.RecordBlocker("needs credentials", now.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatalf("RecordBlocker %d: %v", i, err)
		}
	}
	if !blocked || goal.Status != StatusBlocked {
		t.Fatalf("expected blocker threshold to block, got blocked=%v goal=%+v", blocked, goal)
	}
}

func TestRuntimeAccountActiveUsageSkipsMissingAndInactiveGoal(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	runtime := newTestRuntime(t)
	_, accounted, err := runtime.AccountActiveUsage(UsageDelta{Tokens: 5, Turns: 1}, now)
	if err != nil {
		t.Fatalf("AccountActiveUsage missing: %v", err)
	}
	if accounted {
		t.Fatal("missing goal should not be accounted")
	}

	if _, err := runtime.Create(Spec{ThreadID: "thread-1", GoalID: "goal-1", Objective: "ship runtime"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := runtime.SetUserStatus(StatusPaused, now); err != nil {
		t.Fatalf("SetUserStatus paused: %v", err)
	}
	goal, accounted, err := runtime.AccountActiveUsage(UsageDelta{Tokens: 5, Turns: 1}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("AccountActiveUsage paused: %v", err)
	}
	if accounted {
		t.Fatal("paused goal should not be accounted")
	}
	if goal.TokensUsed != 0 || goal.Status != StatusPaused {
		t.Fatalf("paused goal changed unexpectedly: %+v", goal)
	}
}

func TestRuntimeCompleteAndClear(t *testing.T) {
	runtime := newTestRuntime(t)
	if _, err := runtime.Create(Spec{ThreadID: "thread-1", GoalID: "goal-1", Objective: "ship runtime"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	goal, err := runtime.Complete(time.Now())
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if goal.Status != StatusComplete {
		t.Fatalf("Status = %s, want complete", goal.Status)
	}
	if err := runtime.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	decision, err := runtime.DecideContinuation(ContinuationInput{ThreadIdle: true})
	if err != nil {
		t.Fatalf("DecideContinuation after clear: %v", err)
	}
	if decision.Allowed || decision.Reason != ContinuationBlockedNoGoal {
		t.Fatalf("decision after clear = %+v, want no_goal", decision)
	}
}

func newTestRuntime(t *testing.T) *Runtime {
	t.Helper()
	return NewRuntime(NewStore(filepath.Join(t.TempDir(), "goal_runtime.json")))
}
