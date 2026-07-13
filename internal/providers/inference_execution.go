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

type InferenceSubmissionOutcome string

const (
	InferenceSubmissionInFlight  InferenceSubmissionOutcome = "in_flight"
	InferenceSubmissionSucceeded InferenceSubmissionOutcome = "succeeded"
	InferenceSubmissionFailed    InferenceSubmissionOutcome = "failed"
	InferenceSubmissionFallback  InferenceSubmissionOutcome = "transport_fallback"
	InferenceSubmissionAbandoned InferenceSubmissionOutcome = "abandoned"
	// Interrupted is written by crash recovery when a submission may have
	// crossed the wire but the process died before observing a terminal result.
	InferenceSubmissionInterrupted InferenceSubmissionOutcome = "interrupted"
)

type InferenceCostState string

const (
	// Unknown-but-billable is the safe initial state once a payload may have
	// crossed the wire. It must never be aggregated as zero cost.
	InferenceCostUnknownBillable InferenceCostState = "unknown_but_billable"
	InferenceCostEstimated       InferenceCostState = "estimated"
	InferenceCostKnown           InferenceCostState = "known"
)

// InferenceSubmission is one physical request that may have crossed the wire
// boundary. Multiple submissions under one attempt expose legacy adapter
// retries and protocol fallbacks until those paths are moved into the engine.
type InferenceSubmission struct {
	ID              string
	Ordinal         int
	AttemptID       string
	AttemptOrdinal  int
	Provider        string
	Protocol        string
	Transport       string
	Mode            string
	Reason          string
	StartedAt       time.Time
	CompletedAt     time.Time
	Outcome         InferenceSubmissionOutcome
	FailureCategory FailureCategory
	CostState       InferenceCostState
	ReportedUsage   *TokenUsage
	EstimatedUsage  *TokenUsage
	OutputBytes     int

	execution *InferenceExecution
}

func (s InferenceSubmission) Valid() bool {
	return strings.TrimSpace(s.ID) != ""
}

func (s InferenceSubmission) mutable() bool {
	return s.execution != nil && s.Valid()
}

// ObserveOutput records a conservative partial-output estimate. Provider
// usage, when it arrives, always supersedes this estimate.
func (s InferenceSubmission) ObserveOutput(content string) {
	if !s.mutable() || content == "" {
		return
	}
	s.execution.updateSubmission(s.ID, func(current *InferenceSubmission) {
		current.OutputBytes += len([]byte(content))
		if current.CostState == InferenceCostKnown {
			return
		}
		if current.EstimatedUsage == nil {
			current.EstimatedUsage = &TokenUsage{}
		}
		current.EstimatedUsage.OutputTokens = estimateOutputTokens(current.OutputBytes)
	})
}

// ObserveEstimatedUsage promotes a submission only when the caller has a
// complete conservative estimate, including input. Partial output evidence
// alone remains unknown-but-billable because input often dominates cost.
func (s InferenceSubmission) ObserveEstimatedUsage(usage *TokenUsage) {
	if !s.mutable() || usage == nil {
		return
	}
	copyUsage := *usage
	s.execution.updateSubmission(s.ID, func(current *InferenceSubmission) {
		if current.CostState == InferenceCostKnown {
			return
		}
		if current.EstimatedUsage != nil && current.EstimatedUsage.OutputTokens > copyUsage.OutputTokens {
			copyUsage.OutputTokens = current.EstimatedUsage.OutputTokens
		}
		current.CostState = InferenceCostEstimated
		current.EstimatedUsage = &copyUsage
	})
}

func (s InferenceSubmission) ObserveUsage(usage *TokenUsage) {
	if !s.mutable() || usage == nil {
		return
	}
	copyUsage := *usage
	s.execution.updateSubmission(s.ID, func(current *InferenceSubmission) {
		current.CostState = InferenceCostKnown
		current.ReportedUsage = &copyUsage
	})
}

func (s InferenceSubmission) CompleteSuccess(usage *TokenUsage) {
	s.complete(InferenceSubmissionSucceeded, NormalizedFailure{}, usage)
}

func (s InferenceSubmission) CompleteFailure(failure NormalizedFailure) {
	s.complete(InferenceSubmissionFailed, failure, nil)
}

func (s InferenceSubmission) CompleteFallback(failure NormalizedFailure) {
	s.complete(InferenceSubmissionFallback, failure, nil)
}

func (s InferenceSubmission) Abandon() {
	s.complete(InferenceSubmissionAbandoned, NormalizedFailure{}, nil)
}

func (s InferenceSubmission) complete(outcome InferenceSubmissionOutcome, failure NormalizedFailure, usage *TokenUsage) {
	if !s.mutable() {
		return
	}
	var copyUsage *TokenUsage
	if usage != nil {
		value := *usage
		copyUsage = &value
	}
	s.execution.updateSubmission(s.ID, func(current *InferenceSubmission) {
		if current.Outcome != InferenceSubmissionInFlight {
			if copyUsage != nil && current.CostState != InferenceCostKnown {
				current.CostState = InferenceCostKnown
				current.ReportedUsage = copyUsage
			}
			return
		}
		current.Outcome = outcome
		current.CompletedAt = time.Now().UTC()
		current.FailureCategory = failure.Category
		if copyUsage != nil {
			current.CostState = InferenceCostKnown
			current.ReportedUsage = copyUsage
		}
	})
}

func estimateOutputTokens(outputBytes int) int {
	if outputBytes <= 0 {
		return 0
	}
	return (outputBytes + 3) / 4
}

// InferenceExecutionSnapshot is a race-free diagnostic view of one logical
// operation. It is intentionally metadata-only.
type InferenceExecutionSnapshot struct {
	Operation   InferenceOperation
	Attempts    int
	Submissions []InferenceSubmission
}

type InferenceCostSummary struct {
	KnownSubmissions           int
	EstimatedSubmissions       int
	UnknownBillableSubmissions int
	KnownUsage                 TokenUsage
	EstimatedUsage             TokenUsage
}

// CostSummary preserves cost confidence instead of silently adding unknown
// billable submissions as zero usage.
func (s InferenceExecutionSnapshot) CostSummary() InferenceCostSummary {
	var summary InferenceCostSummary
	for _, submission := range s.Submissions {
		switch submission.CostState {
		case InferenceCostKnown:
			summary.KnownSubmissions++
			if submission.ReportedUsage != nil {
				addTokenUsage(&summary.KnownUsage, *submission.ReportedUsage)
			}
		case InferenceCostEstimated:
			summary.EstimatedSubmissions++
			if submission.EstimatedUsage != nil {
				addTokenUsage(&summary.EstimatedUsage, *submission.EstimatedUsage)
			}
		default:
			summary.UnknownBillableSubmissions++
		}
	}
	return summary
}

func addTokenUsage(total *TokenUsage, usage TokenUsage) {
	if total == nil {
		return
	}
	total.InputTokens += usage.InputTokens
	total.OutputTokens += usage.OutputTokens
	total.CacheCreationTokens += usage.CacheCreationTokens
	total.CacheReadTokens += usage.CacheReadTokens
	total.CacheCreationUnknown = total.CacheCreationUnknown || usage.CacheCreationUnknown
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
		Outcome:        InferenceSubmissionInFlight,
		CostState:      InferenceCostUnknownBillable,
		execution:      e,
	}
	e.submissions = append(e.submissions, submission)
	return submission
}

// ObserveStreamEvent attributes provider-neutral stream evidence to the most
// recent physical submission in this attempt. Protocol adapters may also
// update an exact submission handle; both paths are idempotent at terminal.
func (a InferenceAttempt) ObserveStreamEvent(event StreamEvent) {
	if !a.Valid() {
		return
	}
	submission, ok := a.latestSubmission()
	if !ok {
		return
	}
	switch event.Type {
	case EventContentDelta, EventThinkingDelta, EventToolUseDelta:
		submission.ObserveOutput(event.Content)
	case EventUsage:
		submission.ObserveUsage(event.Usage)
	case EventDone:
		submission.CompleteSuccess(event.Usage)
	case EventError:
		err := event.Error
		if err == nil {
			err = NewIncompleteStreamError("provider stream failed")
		}
		submission.CompleteFailure(NormalizeFailure(err))
	}
}

func (a InferenceAttempt) latestSubmission() (InferenceSubmission, bool) {
	e := a.execution
	e.mu.Lock()
	defer e.mu.Unlock()
	for index := len(e.submissions) - 1; index >= 0; index-- {
		if e.submissions[index].AttemptID == a.ID {
			return e.submissions[index], true
		}
	}
	return InferenceSubmission{}, false
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
			out = append(out, snapshotSubmission(submission))
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
	submissions := make([]InferenceSubmission, len(e.submissions))
	for index, submission := range e.submissions {
		submissions[index] = snapshotSubmission(submission)
	}
	return InferenceExecutionSnapshot{
		Operation:   e.operation,
		Attempts:    e.nextAttempt,
		Submissions: submissions,
	}
}

func (e *InferenceExecution) updateSubmission(id string, update func(*InferenceSubmission)) {
	if e == nil || strings.TrimSpace(id) == "" || update == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for index := range e.submissions {
		if e.submissions[index].ID == id {
			update(&e.submissions[index])
			return
		}
	}
}

func snapshotSubmission(submission InferenceSubmission) InferenceSubmission {
	submission.execution = nil
	if submission.ReportedUsage != nil {
		usage := *submission.ReportedUsage
		submission.ReportedUsage = &usage
	}
	if submission.EstimatedUsage != nil {
		usage := *submission.EstimatedUsage
		submission.EstimatedUsage = &usage
	}
	return submission
}
