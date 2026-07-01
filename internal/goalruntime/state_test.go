package goalruntime

import (
	"strings"
	"testing"
	"time"
)

func TestNewGoalDefaultsActive(t *testing.T) {
	now := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	goal, err := NewGoal(Spec{
		ThreadID:  "thread-1",
		GoalID:    "goal-1",
		Objective: "  ship goal runtime  ",
	}, now)
	if err != nil {
		t.Fatalf("NewGoal: %v", err)
	}
	if goal.SchemaVersion != SchemaVersion {
		t.Fatalf("SchemaVersion = %q", goal.SchemaVersion)
	}
	if goal.Status != StatusActive {
		t.Fatalf("Status = %s, want active", goal.Status)
	}
	if goal.Objective != "ship goal runtime" {
		t.Fatalf("Objective = %q", goal.Objective)
	}
	if !goal.CanAutoContinue() {
		t.Fatal("new active goal should auto-continue")
	}
}

func TestActorStatusOwnership(t *testing.T) {
	if err := ValidateActorTransition(ActorModel, StatusActive, StatusComplete); err != nil {
		t.Fatalf("model complete should be allowed: %v", err)
	}
	if err := ValidateActorTransition(ActorModel, StatusActive, StatusBlocked); err != nil {
		t.Fatalf("model blocked should be allowed: %v", err)
	}
	if err := ValidateActorTransition(ActorModel, StatusActive, StatusPaused); err == nil {
		t.Fatal("model should not pause goals")
	}
	if err := ValidateActorTransition(ActorUser, StatusActive, StatusPaused); err != nil {
		t.Fatalf("user pause should be allowed: %v", err)
	}
	if err := ValidateActorTransition(ActorUser, StatusPaused, StatusActive); err != nil {
		t.Fatalf("user resume should be allowed: %v", err)
	}
}

func TestTerminalStatusesDoNotAutoContinue(t *testing.T) {
	for _, status := range []Status{
		StatusPaused,
		StatusBlocked,
		StatusComplete,
	} {
		goal := Goal{Status: status}
		if goal.CanAutoContinue() {
			t.Fatalf("%s should not auto-continue", status)
		}
	}
	if !IsTerminalStatus(StatusComplete) {
		t.Fatal("complete should be terminal")
	}
}

func TestAccountUsageRecordsTokensWithoutStopping(t *testing.T) {
	now := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	goal, err := NewGoal(Spec{
		ThreadID:  "thread-1",
		GoalID:    "goal-1",
		Objective: "ship goal runtime",
	}, now)
	if err != nil {
		t.Fatalf("NewGoal: %v", err)
	}
	goal, err = goal.AccountUsage(UsageDelta{Tokens: 4, Elapsed: 1500 * time.Millisecond, Turns: 1}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("AccountUsage first: %v", err)
	}
	if goal.Status != StatusActive {
		t.Fatalf("Status after first usage = %s", goal.Status)
	}
	if goal.TokensUsed != 4 || goal.TimeUsedSeconds != 1 || goal.GoalTurns != 1 {
		t.Fatalf("unexpected usage after first account: %+v", goal)
	}
	goal, err = goal.AccountUsage(UsageDelta{Tokens: 6, Elapsed: 2 * time.Second, Turns: 1}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("AccountUsage second: %v", err)
	}
	if goal.Status != StatusActive {
		t.Fatalf("Status after token accounting = %s, want active", goal.Status)
	}
	if goal.TokensUsed != 10 || goal.TimeUsedSeconds != 3 || goal.GoalTurns != 2 {
		t.Fatalf("unexpected usage after second account: %+v", goal)
	}
}

func TestEditObjectiveRequiresUnfinishedGoal(t *testing.T) {
	now := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	goal, err := NewGoal(Spec{
		ThreadID:  "thread-1",
		GoalID:    "goal-1",
		Objective: "ship goal runtime",
	}, now)
	if err != nil {
		t.Fatalf("NewGoal: %v", err)
	}
	goal, err = goal.EditObjective("  ship goal runtime controls  ", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("EditObjective: %v", err)
	}
	if goal.Objective != "ship goal runtime controls" {
		t.Fatalf("Objective = %q", goal.Objective)
	}
	goal, err = goal.SetStatus(ActorModel, StatusComplete, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("SetStatus complete: %v", err)
	}
	if _, err := goal.EditObjective("too late", now.Add(3*time.Minute)); err == nil {
		t.Fatal("editing a terminal goal should fail")
	}
}

func TestRecordBlockerRequiresConsecutiveSameBlocker(t *testing.T) {
	now := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	goal, err := NewGoal(Spec{
		ThreadID:  "thread-1",
		GoalID:    "goal-1",
		Objective: "ship goal runtime",
	}, now)
	if err != nil {
		t.Fatalf("NewGoal: %v", err)
	}

	var blocked bool
	goal, blocked, err = goal.RecordBlocker("Needs credentials", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("RecordBlocker first: %v", err)
	}
	if blocked || goal.Status != StatusActive || goal.BlockerAudit.ConsecutiveTurns != 1 {
		t.Fatalf("first blocker should not block: blocked=%v goal=%+v", blocked, goal)
	}
	goal, blocked, err = goal.RecordBlocker("different API access", now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("RecordBlocker different: %v", err)
	}
	if blocked || goal.BlockerAudit.ConsecutiveTurns != 1 {
		t.Fatalf("different blocker should reset audit: blocked=%v audit=%+v", blocked, goal.BlockerAudit)
	}
	goal, blocked, err = goal.RecordBlocker("  Different   API ACCESS  ", now.Add(3*time.Minute))
	if err != nil {
		t.Fatalf("RecordBlocker same normalized second: %v", err)
	}
	if blocked || goal.BlockerAudit.ConsecutiveTurns != 2 {
		t.Fatalf("second same blocker should not block: blocked=%v audit=%+v", blocked, goal.BlockerAudit)
	}
	goal, blocked, err = goal.RecordBlocker("different api access", now.Add(4*time.Minute))
	if err != nil {
		t.Fatalf("RecordBlocker same third: %v", err)
	}
	if !blocked || goal.Status != StatusBlocked || goal.BlockerAudit.ConsecutiveTurns != RequiredBlockerTurns {
		t.Fatalf("third same blocker should block: blocked=%v goal=%+v", blocked, goal)
	}
}

func TestInvalidUsageRejected(t *testing.T) {
	goal := Goal{Status: StatusActive}
	for name, delta := range map[string]UsageDelta{
		"negative tokens":  {Tokens: -1},
		"negative elapsed": {Elapsed: -time.Second},
		"negative turns":   {Turns: -1},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := goal.AccountUsage(delta, time.Now())
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), "cannot be negative") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
