package goalruntime

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	SchemaVersion        = "wuu/goal-runtime/v0.1"
	RequiredBlockerTurns = 3
)

type Status string

const (
	StatusActive   Status = "active"
	StatusPaused   Status = "paused"
	StatusBlocked  Status = "blocked"
	StatusComplete Status = "complete"
)

type Actor string

const (
	ActorModel  Actor = "model"
	ActorUser   Actor = "user"
	ActorSystem Actor = "system"
)

type Goal struct {
	SchemaVersion   string       `json:"schema_version"`
	ThreadID        string       `json:"thread_id"`
	GoalID          string       `json:"goal_id"`
	Objective       string       `json:"objective"`
	Status          Status       `json:"status"`
	TokensUsed      int          `json:"tokens_used,omitempty"`
	TimeUsedSeconds int64        `json:"time_used_seconds,omitempty"`
	GoalTurns       int          `json:"goal_turns,omitempty"`
	BlockerAudit    BlockerAudit `json:"blocker_audit,omitempty"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
}

type BlockerAudit struct {
	Message          string    `json:"message,omitempty"`
	Normalized       string    `json:"normalized,omitempty"`
	ConsecutiveTurns int       `json:"consecutive_turns,omitempty"`
	UpdatedAt        time.Time `json:"updated_at,omitempty"`
}

type Spec struct {
	ThreadID  string
	GoalID    string
	Objective string
}

type UsageDelta struct {
	Tokens  int
	Elapsed time.Duration
	Turns   int
}

func NewGoal(spec Spec, now time.Time) (Goal, error) {
	threadID := strings.TrimSpace(spec.ThreadID)
	if threadID == "" {
		return Goal{}, errors.New("thread_id is required")
	}
	goalID := strings.TrimSpace(spec.GoalID)
	if goalID == "" {
		return Goal{}, errors.New("goal_id is required")
	}
	objective := strings.TrimSpace(spec.Objective)
	if objective == "" {
		return Goal{}, errors.New("objective is required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return Goal{
		SchemaVersion: SchemaVersion,
		ThreadID:      threadID,
		GoalID:        goalID,
		Objective:     objective,
		Status:        StatusActive,
		CreatedAt:     now.UTC(),
		UpdatedAt:     now.UTC(),
	}, nil
}

func (g Goal) CanAutoContinue() bool {
	return g.Status == StatusActive
}

func IsKnownStatus(status Status) bool {
	switch status {
	case StatusActive,
		StatusPaused,
		StatusBlocked,
		StatusComplete:
		return true
	default:
		return false
	}
}

func IsTerminalStatus(status Status) bool {
	switch status {
	case StatusComplete:
		return true
	default:
		return false
	}
}

func ValidateTransition(from, to Status) error {
	if !IsKnownStatus(to) {
		return fmt.Errorf("unknown goal runtime status: %s", to)
	}
	if from == "" {
		return nil
	}
	if !IsKnownStatus(from) {
		return fmt.Errorf("unknown current goal runtime status: %s", from)
	}
	if from == to {
		return nil
	}
	if goalTransitions[from][to] {
		return nil
	}
	return fmt.Errorf("invalid goal runtime transition: %s -> %s", from, to)
}

func ValidateActorTransition(actor Actor, from, to Status) error {
	if err := validateActorOwnsStatus(actor, to); err != nil {
		return err
	}
	return ValidateTransition(from, to)
}

func (g Goal) SetStatus(actor Actor, to Status, now time.Time) (Goal, error) {
	if err := ValidateActorTransition(actor, g.Status, to); err != nil {
		return Goal{}, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	g.Status = to
	g.UpdatedAt = now.UTC()
	if to != StatusBlocked {
		g.BlockerAudit = BlockerAudit{}
	}
	return g, nil
}

func (g Goal) Complete(now time.Time) (Goal, error) {
	return g.SetStatus(ActorModel, StatusComplete, now)
}

func (g Goal) EditObjective(objective string, now time.Time) (Goal, error) {
	objective = strings.TrimSpace(objective)
	if objective == "" {
		return Goal{}, errors.New("objective is required")
	}
	if IsTerminalStatus(g.Status) {
		return Goal{}, fmt.Errorf("cannot edit terminal goal: %s", g.Status)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	g.Objective = objective
	g.UpdatedAt = now.UTC()
	return g, nil
}

func (g Goal) AccountUsage(delta UsageDelta, now time.Time) (Goal, error) {
	if delta.Tokens < 0 {
		return Goal{}, errors.New("usage tokens cannot be negative")
	}
	if delta.Elapsed < 0 {
		return Goal{}, errors.New("usage elapsed cannot be negative")
	}
	if delta.Turns < 0 {
		return Goal{}, errors.New("usage turns cannot be negative")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	g.TokensUsed += delta.Tokens
	g.TimeUsedSeconds += int64(delta.Elapsed / time.Second)
	g.GoalTurns += delta.Turns
	g.UpdatedAt = now.UTC()
	return g, nil
}

func (g Goal) RecordBlocker(message string, now time.Time) (Goal, bool, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return Goal{}, false, errors.New("blocker message is required")
	}
	if g.Status != StatusActive {
		return Goal{}, false, fmt.Errorf("cannot record blocker for non-active goal: %s", g.Status)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	normalized := normalizeBlocker(message)
	if g.BlockerAudit.Normalized == normalized {
		g.BlockerAudit.ConsecutiveTurns++
	} else {
		g.BlockerAudit = BlockerAudit{
			ConsecutiveTurns: 1,
		}
	}
	g.BlockerAudit.Message = message
	g.BlockerAudit.Normalized = normalized
	g.BlockerAudit.UpdatedAt = now.UTC()
	g.UpdatedAt = now.UTC()
	if g.BlockerAudit.ConsecutiveTurns >= RequiredBlockerTurns {
		g.Status = StatusBlocked
		return g, true, nil
	}
	return g, false, nil
}

func validateActorOwnsStatus(actor Actor, to Status) error {
	switch actor {
	case ActorModel:
		switch to {
		case StatusComplete, StatusBlocked:
			return nil
		default:
			return fmt.Errorf("model cannot set goal runtime status %s", to)
		}
	case ActorUser:
		switch to {
		case StatusActive, StatusPaused:
			return nil
		default:
			return fmt.Errorf("user cannot set goal runtime status %s", to)
		}
	case ActorSystem:
		switch to {
		case StatusActive, StatusPaused, StatusBlocked, StatusComplete:
			return nil
		default:
			return fmt.Errorf("system cannot set goal runtime status %s", to)
		}
	default:
		return fmt.Errorf("unknown goal runtime actor: %s", actor)
	}
}

func normalizeBlocker(message string) string {
	return strings.ToLower(strings.Join(strings.Fields(message), " "))
}

var goalTransitions = map[Status]map[Status]bool{
	StatusActive: {
		StatusPaused:   true,
		StatusBlocked:  true,
		StatusComplete: true,
	},
	StatusPaused: {
		StatusActive: true,
	},
	StatusBlocked: {
		StatusActive: true,
	},
	StatusComplete: {},
}
