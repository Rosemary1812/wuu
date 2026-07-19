package goalruntime

import (
	"errors"
	"os"
	"time"
)

type Runtime struct {
	store *Store
}

type ContinuationInput struct {
	ThreadIdle             bool
	ActiveTurn             bool
	QueuedUserWork         bool
	QueuedAgentWork        bool
	AwaitingBackgroundWork bool
	ReadOnly               bool
	PlanMode               bool
	UnsafeProtocolState    bool
}

type ContinuationDecision struct {
	Allowed bool                    `json:"allowed"`
	Reason  ContinuationBlockReason `json:"reason,omitempty"`
	Status  Status                  `json:"status,omitempty"`
}

type ContinuationBlockReason string

const (
	ContinuationBlockedNoGoal              ContinuationBlockReason = "no_goal"
	ContinuationBlockedNotIdle             ContinuationBlockReason = "not_idle"
	ContinuationBlockedBusy                ContinuationBlockReason = "busy"
	ContinuationBlockedQueuedUserWork      ContinuationBlockReason = "queued_user_work"
	ContinuationBlockedQueuedAgentWork     ContinuationBlockReason = "queued_agent_work"
	ContinuationBlockedAwaitingBackground  ContinuationBlockReason = "awaiting_background_work"
	ContinuationBlockedReadOnly            ContinuationBlockReason = "read_only"
	ContinuationBlockedPlanMode            ContinuationBlockReason = "plan_mode"
	ContinuationBlockedUnsafeProtocolState ContinuationBlockReason = "unsafe_protocol_state"
	ContinuationBlockedGoalNotActive       ContinuationBlockReason = "goal_not_active"
)

func NewRuntime(store *Store) *Runtime {
	return &Runtime{store: store}
}

func (r *Runtime) Store() *Store {
	if r == nil {
		return nil
	}
	return r.store
}

func (r *Runtime) Create(spec Spec) (Goal, error) {
	if r == nil || r.store == nil {
		return Goal{}, errors.New("goal runtime store is required")
	}
	return r.store.Create(spec)
}

func (r *Runtime) CurrentGoal() (Goal, error) {
	if r == nil || r.store == nil {
		return Goal{}, errors.New("goal runtime store is required")
	}
	return r.store.Load()
}

func (r *Runtime) Complete(now time.Time) (Goal, error) {
	if r == nil || r.store == nil {
		return Goal{}, errors.New("goal runtime store is required")
	}
	return r.store.Update(func(goal Goal) (Goal, error) {
		return goal.Complete(now)
	})
}

func (r *Runtime) EditObjective(objective string, now time.Time) (Goal, error) {
	if r == nil || r.store == nil {
		return Goal{}, errors.New("goal runtime store is required")
	}
	return r.store.Update(func(goal Goal) (Goal, error) {
		return goal.EditObjective(objective, now)
	})
}

func (r *Runtime) SetUserStatus(status Status, now time.Time) (Goal, error) {
	return r.setStatus(ActorUser, status, now)
}

func (r *Runtime) SetModelStatus(status Status, now time.Time) (Goal, error) {
	return r.setStatus(ActorModel, status, now)
}

func (r *Runtime) SetSystemStatus(status Status, now time.Time) (Goal, error) {
	return r.setStatus(ActorSystem, status, now)
}

func (r *Runtime) AccountUsage(delta UsageDelta, now time.Time) (Goal, error) {
	if r == nil || r.store == nil {
		return Goal{}, errors.New("goal runtime store is required")
	}
	return r.store.Update(func(goal Goal) (Goal, error) {
		return goal.AccountUsage(delta, now)
	})
}

func (r *Runtime) AccountActiveUsage(delta UsageDelta, now time.Time) (Goal, bool, error) {
	if r == nil || r.store == nil {
		return Goal{}, false, errors.New("goal runtime store is required")
	}
	var accounted bool
	goal, err := r.store.Update(func(goal Goal) (Goal, error) {
		if goal.Status != StatusActive {
			return goal, nil
		}
		accounted = true
		return goal.AccountUsage(delta, now)
	})
	if errors.Is(err, os.ErrNotExist) {
		return Goal{}, false, nil
	}
	return goal, accounted, err
}

func (r *Runtime) RecordBlocker(message string, now time.Time) (Goal, bool, error) {
	if r == nil || r.store == nil {
		return Goal{}, false, errors.New("goal runtime store is required")
	}
	var blocked bool
	goal, err := r.store.Update(func(goal Goal) (Goal, error) {
		next, didBlock, err := goal.RecordBlocker(message, now)
		blocked = didBlock
		return next, err
	})
	return goal, blocked, err
}

func (r *Runtime) DecideContinuation(input ContinuationInput) (ContinuationDecision, error) {
	if r == nil || r.store == nil {
		return ContinuationDecision{}, errors.New("goal runtime store is required")
	}
	goal, err := r.store.Load()
	if errors.Is(err, os.ErrNotExist) {
		return ContinuationDecision{Allowed: false, Reason: ContinuationBlockedNoGoal}, nil
	}
	if err != nil {
		return ContinuationDecision{}, err
	}
	return DecideContinuation(goal, input), nil
}

func DecideContinuation(goal Goal, input ContinuationInput) ContinuationDecision {
	if !input.ThreadIdle {
		return blockedContinuation(goal.Status, ContinuationBlockedNotIdle)
	}
	if input.ActiveTurn {
		return blockedContinuation(goal.Status, ContinuationBlockedBusy)
	}
	if input.QueuedUserWork {
		return blockedContinuation(goal.Status, ContinuationBlockedQueuedUserWork)
	}
	if input.QueuedAgentWork {
		return blockedContinuation(goal.Status, ContinuationBlockedQueuedAgentWork)
	}
	if input.AwaitingBackgroundWork {
		return blockedContinuation(goal.Status, ContinuationBlockedAwaitingBackground)
	}
	if input.ReadOnly {
		return blockedContinuation(goal.Status, ContinuationBlockedReadOnly)
	}
	if input.PlanMode {
		return blockedContinuation(goal.Status, ContinuationBlockedPlanMode)
	}
	if input.UnsafeProtocolState {
		return blockedContinuation(goal.Status, ContinuationBlockedUnsafeProtocolState)
	}
	if !goal.CanAutoContinue() {
		return blockedContinuation(goal.Status, ContinuationBlockedGoalNotActive)
	}
	return ContinuationDecision{Allowed: true, Status: goal.Status}
}

func (r *Runtime) setStatus(actor Actor, status Status, now time.Time) (Goal, error) {
	if r == nil || r.store == nil {
		return Goal{}, errors.New("goal runtime store is required")
	}
	return r.store.Update(func(goal Goal) (Goal, error) {
		return goal.SetStatus(actor, status, now)
	})
}

func blockedContinuation(status Status, reason ContinuationBlockReason) ContinuationDecision {
	return ContinuationDecision{
		Allowed: false,
		Reason:  reason,
		Status:  status,
	}
}

func (r *Runtime) Clear() error {
	if r == nil || r.store == nil {
		return errors.New("goal runtime store is required")
	}
	return r.store.Clear()
}
