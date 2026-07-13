package providers

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// InferenceSubmissionMeta describes one physical request submission without
// carrying credentials, prompt text, response bodies, or provider receipts.
type InferenceSubmissionMeta struct {
	Provider  string
	Protocol  string
	Transport string
	Mode      string
	Reason    string
}

// InferenceSubmission is one physical request that may have crossed the wire
// boundary. Multiple submissions under one attempt expose legacy adapter
// retries and protocol fallbacks until those paths are moved into the engine.
type InferenceSubmission struct {
	ID             string
	Ordinal        int
	AttemptID      string
	AttemptOrdinal int
	Provider       string
	Protocol       string
	Transport      string
	Mode           string
	Reason         string
	StartedAt      time.Time
}

// InferenceExecutionSnapshot is a race-free diagnostic view of one logical
// operation. It is intentionally metadata-only.
type InferenceExecutionSnapshot struct {
	Operation   InferenceOperation
	Attempts    int
	Submissions []InferenceSubmission
}

// InferenceExecution owns attempt and submission ordinals for one operation.
// It is safe to share through copied ChatRequest values and provider goroutines.
type InferenceExecution struct {
	mu sync.Mutex

	operation      InferenceOperation
	nextAttempt    int
	nextSubmission int
	submissions    []InferenceSubmission
}

// InferenceAttempt identifies one ExecuteOnce call. The execution pointer is
// deliberately private; provider implementations record wire submissions via
// RecordSubmission instead of mutating the ledger directly.
type InferenceAttempt struct {
	ID      string
	Ordinal int

	execution *InferenceExecution
}

func NewInferenceExecution(operation InferenceOperation) *InferenceExecution {
	operation = EnsureInferenceOperation(operation, InferenceOperationAuxiliary, InferenceProfileInteractive)
	return &InferenceExecution{operation: operation}
}

// EnsureInferenceExecution attaches a ledger while preserving caller-supplied
// operation identity. Existing executions win when the request only omitted
// the duplicate Operation value.
func EnsureInferenceExecution(req ChatRequest, fallbackKind InferenceOperationKind, fallbackProfile InferenceWorkloadProfile) ChatRequest {
	if req.Execution != nil && strings.TrimSpace(req.Operation.ID) == "" {
		req.Operation = req.Execution.Operation()
	}
	req.Operation = EnsureInferenceOperation(req.Operation, fallbackKind, fallbackProfile)
	if req.Execution == nil || req.Execution.Operation().ID != req.Operation.ID {
		req.Execution = NewInferenceExecution(req.Operation)
	}
	return req
}

// EnsureInferenceAttempt preserves an existing outer-engine attempt or starts
// one for a direct unary/provider call that bypassed the stream engine.
func EnsureInferenceAttempt(req ChatRequest, fallbackKind InferenceOperationKind, fallbackProfile InferenceWorkloadProfile) ChatRequest {
	req = EnsureInferenceExecution(req, fallbackKind, fallbackProfile)
	if !req.Attempt.Valid() {
		req.Attempt = req.Execution.BeginAttempt()
	}
	return req
}

// BeginInferenceAttempt always allocates the next operation attempt. Recovery
// code uses it before switching mode or reissuing a request.
func BeginInferenceAttempt(req ChatRequest, fallbackKind InferenceOperationKind, fallbackProfile InferenceWorkloadProfile) ChatRequest {
	req = EnsureInferenceExecution(req, fallbackKind, fallbackProfile)
	req.Attempt = req.Execution.BeginAttempt()
	return req
}

func (e *InferenceExecution) Operation() InferenceOperation {
	if e == nil {
		return InferenceOperation{}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.operation
}

func (e *InferenceExecution) BeginAttempt() InferenceAttempt {
	if e == nil {
		panic("providers: begin inference attempt on nil execution")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.nextAttempt++
	return InferenceAttempt{
		ID:        e.operation.AttemptID(e.nextAttempt),
		Ordinal:   e.nextAttempt,
		execution: e,
	}
}

func (a InferenceAttempt) Valid() bool {
	return a.execution != nil && strings.TrimSpace(a.ID) != "" && a.Ordinal > 0
}

// Operation returns the immutable logical-operation metadata associated with
// this attempt. Invalid attempts return the zero value so scheduling callers
// can safely fall back to the interactive profile.
func (a InferenceAttempt) Operation() InferenceOperation {
	if !a.Valid() {
		return InferenceOperation{}
	}
	return a.execution.Operation()
}

// RecordSubmission marks the physical send boundary immediately before the
// provider transport writes the inference request.
func (a InferenceAttempt) RecordSubmission(meta InferenceSubmissionMeta) InferenceSubmission {
	if !a.Valid() {
		panic("providers: record submission without a valid inference attempt")
	}
	e := a.execution
	e.mu.Lock()
	defer e.mu.Unlock()
	e.nextSubmission++
	submission := InferenceSubmission{
		ID:             fmt.Sprintf("%s-s%d", e.operation.ID, e.nextSubmission),
		Ordinal:        e.nextSubmission,
		AttemptID:      a.ID,
		AttemptOrdinal: a.Ordinal,
		Provider:       strings.TrimSpace(meta.Provider),
		Protocol:       strings.TrimSpace(meta.Protocol),
		Transport:      strings.TrimSpace(meta.Transport),
		Mode:           strings.TrimSpace(meta.Mode),
		Reason:         strings.TrimSpace(meta.Reason),
		StartedAt:      time.Now().UTC(),
	}
	e.submissions = append(e.submissions, submission)
	return submission
}

func (a InferenceAttempt) SubmissionSnapshot() []InferenceSubmission {
	if !a.Valid() {
		return nil
	}
	e := a.execution
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]InferenceSubmission, 0)
	for _, submission := range e.submissions {
		if submission.AttemptID == a.ID {
			out = append(out, submission)
		}
	}
	return out
}

func (e *InferenceExecution) Snapshot() InferenceExecutionSnapshot {
	if e == nil {
		return InferenceExecutionSnapshot{}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return InferenceExecutionSnapshot{
		Operation:   e.operation,
		Attempts:    e.nextAttempt,
		Submissions: append([]InferenceSubmission(nil), e.submissions...),
	}
}
