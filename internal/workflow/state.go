package workflow

import "fmt"

type RunState string

const (
	RunStateDraft           RunState = "draft"
	RunStateApprovalPending RunState = "approval_pending"
	RunStateRunning         RunState = "running"
	RunStatePaused          RunState = "paused"
	RunStateCompleted       RunState = "completed"
	RunStateFailed          RunState = "failed"
	RunStateCancelled       RunState = "cancelled"
)

type PhaseState string

const (
	PhaseStatePending   PhaseState = "pending"
	PhaseStateRunnable  PhaseState = "runnable"
	PhaseStateRunning   PhaseState = "running"
	PhaseStateCompleted PhaseState = "completed"
	PhaseStateBlocked   PhaseState = "blocked"
	PhaseStateFailed    PhaseState = "failed"
	PhaseStateSkipped   PhaseState = "skipped"
)

type AgentRunState string

const (
	AgentRunStateQueued         AgentRunState = "queued"
	AgentRunStateStarting       AgentRunState = "starting"
	AgentRunStateRunning        AgentRunState = "running"
	AgentRunStateAwaitingReport AgentRunState = "awaiting_report"
	AgentRunStateCompleted      AgentRunState = "completed"
	AgentRunStateFailed         AgentRunState = "failed"
	AgentRunStateRetrying       AgentRunState = "retrying"
	AgentRunStateCancelled      AgentRunState = "cancelled"
)

func ValidateRunTransition(from, to RunState) error {
	if !isKnownRunState(to) {
		return fmt.Errorf("unknown workflow run state: %s", to)
	}
	if from == "" {
		return nil
	}
	if !isKnownRunState(from) {
		return fmt.Errorf("unknown current workflow run state: %s", from)
	}
	if from == to {
		return nil
	}
	if runTransitions[from][to] {
		return nil
	}
	return fmt.Errorf("invalid workflow run transition: %s -> %s", from, to)
}

func ValidatePhaseTransition(from, to PhaseState) error {
	if !isKnownPhaseState(to) {
		return fmt.Errorf("unknown workflow phase state: %s", to)
	}
	if from == "" {
		return nil
	}
	if !isKnownPhaseState(from) {
		return fmt.Errorf("unknown current workflow phase state: %s", from)
	}
	if from == to {
		return nil
	}
	if phaseTransitions[from][to] {
		return nil
	}
	return fmt.Errorf("invalid workflow phase transition: %s -> %s", from, to)
}

func ValidateAgentRunTransition(from, to AgentRunState) error {
	if !isKnownAgentRunState(to) {
		return fmt.Errorf("unknown workflow agent run state: %s", to)
	}
	if from == "" {
		return nil
	}
	if !isKnownAgentRunState(from) {
		return fmt.Errorf("unknown current workflow agent run state: %s", from)
	}
	if from == to {
		return nil
	}
	if agentRunTransitions[from][to] {
		return nil
	}
	return fmt.Errorf("invalid workflow agent run transition: %s -> %s", from, to)
}

func IsTerminalRunState(state RunState) bool {
	switch state {
	case RunStateCompleted, RunStateFailed, RunStateCancelled:
		return true
	default:
		return false
	}
}

func IsTerminalPhaseState(state PhaseState) bool {
	switch state {
	case PhaseStateCompleted, PhaseStateFailed, PhaseStateSkipped:
		return true
	default:
		return false
	}
}

func IsTerminalAgentRunState(state AgentRunState) bool {
	switch state {
	case AgentRunStateCompleted, AgentRunStateFailed, AgentRunStateCancelled:
		return true
	default:
		return false
	}
}

func isKnownRunState(state RunState) bool {
	switch state {
	case RunStateDraft,
		RunStateApprovalPending,
		RunStateRunning,
		RunStatePaused,
		RunStateCompleted,
		RunStateFailed,
		RunStateCancelled:
		return true
	default:
		return false
	}
}

func isKnownPhaseState(state PhaseState) bool {
	switch state {
	case PhaseStatePending,
		PhaseStateRunnable,
		PhaseStateRunning,
		PhaseStateCompleted,
		PhaseStateBlocked,
		PhaseStateFailed,
		PhaseStateSkipped:
		return true
	default:
		return false
	}
}

func isKnownAgentRunState(state AgentRunState) bool {
	switch state {
	case AgentRunStateQueued,
		AgentRunStateStarting,
		AgentRunStateRunning,
		AgentRunStateAwaitingReport,
		AgentRunStateCompleted,
		AgentRunStateFailed,
		AgentRunStateRetrying,
		AgentRunStateCancelled:
		return true
	default:
		return false
	}
}

var runTransitions = map[RunState]map[RunState]bool{
	RunStateDraft: {
		RunStateApprovalPending: true,
		RunStateRunning:         true,
		RunStateCancelled:       true,
	},
	RunStateApprovalPending: {
		RunStateRunning:   true,
		RunStatePaused:    true,
		RunStateFailed:    true,
		RunStateCancelled: true,
	},
	RunStateRunning: {
		RunStatePaused:    true,
		RunStateCompleted: true,
		RunStateFailed:    true,
		RunStateCancelled: true,
	},
	RunStatePaused: {
		RunStateRunning:   true,
		RunStateFailed:    true,
		RunStateCancelled: true,
	},
}

var phaseTransitions = map[PhaseState]map[PhaseState]bool{
	PhaseStatePending: {
		PhaseStateRunnable: true,
		PhaseStateSkipped:  true,
		PhaseStateFailed:   true,
	},
	PhaseStateRunnable: {
		PhaseStateRunning:   true,
		PhaseStateCompleted: true,
		PhaseStateBlocked:   true,
		PhaseStateSkipped:   true,
		PhaseStateFailed:    true,
	},
	PhaseStateRunning: {
		PhaseStateCompleted: true,
		PhaseStateBlocked:   true,
		PhaseStateFailed:    true,
		PhaseStateSkipped:   true,
	},
	PhaseStateBlocked: {
		PhaseStateRunnable: true,
		PhaseStateRunning:  true,
		PhaseStateFailed:   true,
		PhaseStateSkipped:  true,
	},
}

var agentRunTransitions = map[AgentRunState]map[AgentRunState]bool{
	AgentRunStateQueued: {
		AgentRunStateStarting:  true,
		AgentRunStateCancelled: true,
	},
	AgentRunStateStarting: {
		AgentRunStateRunning:   true,
		AgentRunStateFailed:    true,
		AgentRunStateCancelled: true,
	},
	AgentRunStateRunning: {
		AgentRunStateAwaitingReport: true,
		AgentRunStateCompleted:      true,
		AgentRunStateFailed:         true,
		AgentRunStateRetrying:       true,
		AgentRunStateCancelled:      true,
	},
	AgentRunStateAwaitingReport: {
		AgentRunStateCompleted: true,
		AgentRunStateFailed:    true,
		AgentRunStateRetrying:  true,
		AgentRunStateCancelled: true,
	},
	AgentRunStateFailed: {
		AgentRunStateRetrying:  true,
		AgentRunStateCancelled: true,
	},
	AgentRunStateRetrying: {
		AgentRunStateQueued:    true,
		AgentRunStateStarting:  true,
		AgentRunStateRunning:   true,
		AgentRunStateFailed:    true,
		AgentRunStateCancelled: true,
	},
}
