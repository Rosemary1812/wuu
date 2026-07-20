// Package kanban defines the kanban-OS domain model: agent-neutral Tasks
// (nestable) and Runs that bind a task to one concrete named-agent execution.
//
// The two core invariants:
//
//   - A Task never names an agent. Assignment is not task state.
//   - A Run carries the assignment: creating a run with a target participant
//     is the dispatch action itself, and it is also the entire record of it.
//     Correcting a dispatch means creating the next run with another target;
//     no assignment state is ever rolled back.
package kanban

import (
	"errors"
	"fmt"
	"time"
)

// Task statuses. The kanban columns project directly from these.
const (
	// TaskStatusDraft is a crystallized but not yet human-confirmed task.
	TaskStatusDraft = "draft"
	// TaskStatusReady is confirmed work with no active run.
	TaskStatusReady = "ready"
	// TaskStatusRunning has a queued or running run.
	TaskStatusRunning = "running"
	// TaskStatusReview has a succeeded latest run awaiting human acceptance.
	TaskStatusReview = "review"
	// TaskStatusDone is human-accepted. Terminal.
	TaskStatusDone = "done"
	// TaskStatusCancelled is human-cancelled. Terminal.
	TaskStatusCancelled = "cancelled"
)

// Run kinds.
const (
	// RunKindExecute is ordinary task execution.
	RunKindExecute = "execute"
	// RunKindReview is an opt-in second-eyes verification pass over a task's
	// artifacts. The reviewer has fresh context by construction.
	RunKindReview = "review"
)

// Run statuses.
const (
	RunStatusQueued      = "queued"
	RunStatusRunning     = "running"
	RunStatusSucceeded   = "succeeded"
	RunStatusFailed      = "failed"
	RunStatusInterrupted = "interrupted"
)

var (
	ErrTaskNotFound       = errors.New("kanban task not found")
	ErrRunNotFound        = errors.New("kanban run not found")
	ErrInvalidTransition  = errors.New("invalid kanban task status transition")
	ErrTaskNotReady       = errors.New("kanban task is not dispatchable in its current status")
	ErrTargetBusy         = errors.New("target participant already has an active run")
	ErrRunAlreadyTerminal = errors.New("kanban run already reached a terminal status")
)

// Task is one agent-neutral unit of work. ParentID nests subtasks produced by
// decomposition; SourceThreadID points back at the intake conversation the
// task crystallized from (kept as a lazy reference, never inlined).
type Task struct {
	ID             string
	SessionID      string
	ParentID       string
	Title          string
	Brief          string
	Status         string
	SourceThreadID string
	CreatedBy      string // "human" or a participant id
	SortIndex      int
	LatestRunID    string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Run is one execution attempt binding a task to a target participant.
// ThreadID is the execution site (spawned on start). HostThreadID is the
// conversation thread that owns the spawned execution (needed to redispatch
// autopilot follow-ups). Kind distinguishes execution from second-eyes review.
type Run struct {
	ID           string
	TaskID       string
	SessionID    string
	Kind         string
	TargetID     string
	ThreadID     string
	HostThreadID string
	Status       string
	Summary      string
	ErrorMessage string
	CreatedBy    string // "human" or a participant id
	CreatedAt    time.Time
	StartedAt    time.Time
	FinishedAt   time.Time
}

// Artifact is one produced output file attributed to a run and its task.
type Artifact struct {
	ID          string
	RunID       string
	TaskID      string
	SessionID   string
	Path        string
	DisplayName string
	MediaType   string
	SizeBytes   int64
	CreatedAt   time.Time
}

// taskTransitions encodes the legal status graph. Dispatch (creating a run)
// is only legal from ready or review; a succeeded run moves the task to
// review, a failed/interrupted run returns it to ready. Human acceptance and
// cancellation are explicit transitions, never side effects of runs.
var taskTransitions = map[string][]string{
	TaskStatusDraft:     {TaskStatusReady, TaskStatusCancelled},
	TaskStatusReady:     {TaskStatusRunning, TaskStatusReview, TaskStatusCancelled},
	TaskStatusRunning:   {TaskStatusReview, TaskStatusReady, TaskStatusCancelled},
	TaskStatusReview:    {TaskStatusDone, TaskStatusRunning, TaskStatusCancelled},
	TaskStatusDone:      {},
	TaskStatusCancelled: {},
}

// CheckTransition validates a task status move.
func CheckTransition(from, to string) error {
	for _, next := range taskTransitions[from] {
		if next == to {
			return nil
		}
	}
	return fmt.Errorf("%w: %q -> %q", ErrInvalidTransition, from, to)
}

// Dispatchable reports whether creating a run is legal for a task status.
func Dispatchable(status string) bool {
	return status == TaskStatusReady || status == TaskStatusReview
}

// RunTerminal reports whether a run status is terminal.
func RunTerminal(status string) bool {
	return status == RunStatusSucceeded || status == RunStatusFailed || status == RunStatusInterrupted
}

// TaskStatusAfterRun resolves the task status when a run reaches a terminal
// status: succeeded runs park the task in human review; anything else returns
// the task to ready so it can be dispatched again.
func TaskStatusAfterRun(runStatus string) string {
	if runStatus == RunStatusSucceeded {
		return TaskStatusReview
	}
	return TaskStatusReady
}
