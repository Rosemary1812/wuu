package providers

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// StreamRetryHook is called just before each reconnect attempt.
// attempt is 1-based (first retry = 1). maxRetries is the configured cap.
// err is the transport error that triggered the retry.
// retryIn is the computed backoff delay before the next connect.
type StreamRetryHook func(attempt, maxRetries int, err error, retryIn time.Duration)

// StreamRetryContext is the evidence available immediately before a retry.
// A guard can use it to reject a replay that would duplicate external effects.
type StreamRetryContext struct {
	Operation          InferenceOperation
	Attempt            int
	ForwardedEvents    int
	FinalizedToolCalls []ToolCall
	Err                error
}

// StreamReplayGuard returns nil when replay is safe. A non-nil result blocks
// the retry and is surfaced as ReplayBlockedError.
type StreamReplayGuard func(StreamRetryContext) error

// ReliableStreamOption configures behavior that is orthogonal to retry count.
type ReliableStreamOption func(*ReliableStreamClient)

// WithStreamReplayGuard installs a safety fence evaluated before every retry.
func WithStreamReplayGuard(guard StreamReplayGuard) ReliableStreamOption {
	return func(client *ReliableStreamClient) {
		client.replayGuard = guard
	}
}

// ReliableStreamClient wraps a StreamClient and transparently retries dropped
// streams. Callers receive a single output channel; reconnectable inner errors
// are consumed internally and only the final unrecoverable error is forwarded.
type ReliableStreamClient struct {
	inner       StreamClient
	cfg         RetryConfig
	onRetry     StreamRetryHook
	replayGuard StreamReplayGuard
}

// ReplayBlockedError reports a retryable transport failure that Wuu chose not
// to replay because an operation-specific safety fence rejected it.
type ReplayBlockedError struct {
	Cause  error
	Reason error
}

func (e *ReplayBlockedError) Error() string {
	if e == nil {
		return "automatic replay blocked"
	}
	if e.Reason == nil {
		return fmt.Sprintf("automatic replay blocked: %v", e.Cause)
	}
	return fmt.Sprintf("automatic replay blocked: %v (original stream failure: %v)", e.Reason, e.Cause)
}

func (e *ReplayBlockedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// NewReliableStreamClient constructs a ReliableStreamClient.
// cfg is normalized before use. onRetry may be nil.
func NewReliableStreamClient(inner StreamClient, cfg RetryConfig, onRetry StreamRetryHook, options ...ReliableStreamOption) *ReliableStreamClient {
	client := &ReliableStreamClient{
		inner:   inner,
		cfg:     NormalizeRetryConfig(cfg),
		onRetry: onRetry,
	}
	for _, option := range options {
		if option != nil {
			option(client)
		}
	}
	return client
}

// Chat delegates non-streaming calls to the wrapped client.
func (r *ReliableStreamClient) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	if r == nil || r.inner == nil {
		return ChatResponse{}, errors.New("stream client is required")
	}
	return r.inner.Chat(ctx, req)
}

// StreamChat opens one logical stream and reconnects the underlying transport
// when the provider/client reports a retryable failure before EventDone.
func (r *ReliableStreamClient) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	if r == nil || r.inner == nil {
		return nil, errors.New("stream client is required")
	}
	out := make(chan StreamEvent)
	go r.run(ctx, req, out)
	return out, nil
}

func (r *ReliableStreamClient) run(ctx context.Context, req ChatRequest, out chan<- StreamEvent) {
	defer close(out)
	var err error
	req, err = prepareInferenceRequest(ctx, r.inner, req)
	if err != nil {
		r.send(ctx, out, StreamEvent{Type: EventError, Error: err})
		return
	}
	req, err = EnsureInferenceExecutionContext(ctx, req, InferenceOperationAuxiliary, InferenceProfileInteractive)
	if err != nil {
		r.send(ctx, out, StreamEvent{Type: EventError, Error: err})
		return
	}
	operation := req.Operation
	startedAt := time.Now()
	maxAttempts := req.Execution.Snapshot().Attempts + r.cfg.MaxRetries + 1
	retriesUsed := 0
	for {
		if ctx.Err() != nil {
			return
		}
		attemptReq, beginErr := BeginInferenceAttemptContext(ctx, req, operation.Kind, operation.WorkloadProfile)
		if beginErr != nil {
			r.send(ctx, out, StreamEvent{Type: EventError, Error: beginErr})
			return
		}
		attempt := attemptReq.Attempt
		if !r.sendLifecycle(ctx, out, attemptReq.Execution, operation, StreamPhaseConnecting, attempt.ID, attempt.Ordinal, maxAttempts, retriesUsed, nil, 0, false, startedAt) {
			_ = attempt.Complete(InferenceOutcomeCanceled, NormalizeFailure(ctx.Err()))
			return
		}

		attemptCtx, cancelAttempt := context.WithCancel(ctx)
		ch, err := r.inner.StreamChat(attemptCtx, attemptReq)
		forwardedEvents := 0
		var finalizedToolCalls []ToolCall
		if err == nil {
			var sawDone bool
			err, sawDone, forwardedEvents, finalizedToolCalls = r.forwardAttempt(attemptCtx, ch, out, attemptReq.Execution, operation, attempt, maxAttempts, retriesUsed, startedAt)
			cancelAttempt()
			if ctx.Err() != nil {
				failure := NormalizeFailure(ctx.Err())
				_ = attempt.Complete(InferenceOutcomeCanceled, failure)
				return
			}
			if sawDone && err == nil {
				if journalErr := attempt.Complete(InferenceOutcomeSucceeded, NormalizedFailure{}); journalErr != nil {
					r.send(ctx, out, StreamEvent{Type: EventError, Error: journalErr})
				}
				return
			}
			if err == nil {
				err = NewIncompleteStreamError("stream closed before done")
			}
		} else {
			cancelAttempt()
		}
		if err != nil {
			if journalErr := attempt.ObserveStreamEvent(StreamEvent{Type: EventError, Error: err}); journalErr != nil {
				err = journalErr
			}
		}
		failure := NormalizeFailure(err)
		plan := PlanRecovery(failure)
		canRetry := ctx.Err() == nil && retriesUsed < r.cfg.MaxRetries && streamRecoverySupported(r.inner, plan)
		if canRetry && r.replayGuard != nil {
			if guardErr := r.replayGuard(StreamRetryContext{
				Operation:          operation,
				Attempt:            attempt.Ordinal,
				ForwardedEvents:    forwardedEvents,
				FinalizedToolCalls: append([]ToolCall(nil), finalizedToolCalls...),
				Err:                err,
			}); guardErr != nil {
				err = &ReplayBlockedError{Cause: err, Reason: guardErr}
				canRetry = false
			}
		}
		if !canRetry {
			failure := NormalizeFailure(err)
			outcome := InferenceOutcomeFailed
			if failure.Category == FailureCanceled || failure.Category == FailureDeadline {
				outcome = InferenceOutcomeCanceled
			}
			if journalErr := attempt.Complete(outcome, failure); journalErr != nil {
				err = journalErr
				failure = NormalizeFailure(journalErr)
				outcome = InferenceOutcomeFailed
			}
			if !r.sendLifecycle(ctx, out, attemptReq.Execution, operation, StreamPhaseFailed, attempt.ID, attempt.Ordinal, maxAttempts, retriesUsed, err, 0, false, startedAt) {
				return
			}
			r.send(ctx, out, StreamEvent{Type: EventError, Error: err})
			return
		}

		delay := time.Duration(0)
		if plan.Action != RecoveryRefreshAuth {
			delay = backoffDelay(retriesUsed, r.cfg.InitialDelay, r.cfg.MaxDelay, err)
		}
		if journalErr := attempt.Complete(InferenceOutcomeFailed, failure); journalErr != nil {
			r.send(ctx, out, StreamEvent{Type: EventError, Error: journalErr})
			return
		}
		if journalErr := attempt.RecordRecovery(plan, time.Now().Add(delay)); journalErr != nil {
			r.send(ctx, out, StreamEvent{Type: EventError, Error: journalErr})
			return
		}
		if ctx.Err() != nil {
			return
		}
		if plan.Action == RecoveryRefreshAuth {
			applier := r.inner.(InferenceRecoveryApplier)
			if applyErr := applier.ApplyInferenceRecovery(ctx, plan); applyErr != nil {
				r.send(ctx, out, StreamEvent{Type: EventError, Error: errors.Join(err, applyErr)})
				return
			}
		}
		retriesUsed++
		nextAttemptOrdinal := attempt.Ordinal + 1
		if !r.sendLifecycle(ctx, out, attemptReq.Execution, operation, StreamPhaseReconnecting, operation.AttemptID(nextAttemptOrdinal), nextAttemptOrdinal, maxAttempts, retriesUsed, err, delay, forwardedEvents > 0, startedAt) {
			return
		}
		if r.onRetry != nil {
			r.onRetry(retriesUsed, r.cfg.MaxRetries, err, delay)
		}
		if waitWithContext(ctx, delay) != nil {
			return
		}
	}
}

func (r *ReliableStreamClient) forwardAttempt(
	ctx context.Context,
	ch <-chan StreamEvent,
	out chan<- StreamEvent,
	execution *InferenceExecution,
	operation InferenceOperation,
	attempt InferenceAttempt,
	maxAttempts int,
	retryCount int,
	startedAt time.Time,
) (error, bool, int, []ToolCall) {
	var streamErr error
	var sawDone bool
	var forwardedEvents int
	var finalizedToolCalls []ToolCall
	connected := false
	for ev := range ch {
		if err := attempt.ObserveStreamEvent(ev); err != nil {
			return err, sawDone, forwardedEvents, finalizedToolCalls
		}
		if !connected {
			if !r.sendLifecycle(ctx, out, execution, operation, StreamPhaseConnected, attempt.ID, attempt.Ordinal, maxAttempts, retryCount, nil, 0, false, startedAt) {
				return streamErr, sawDone, forwardedEvents, finalizedToolCalls
			}
			connected = true
		}
		if ev.Type == EventError {
			if ev.Error != nil {
				streamErr = ev.Error
			} else {
				streamErr = errors.New("stream error")
			}
			continue
		}
		if ev.Type == EventDone {
			sawDone = true
		}
		if ev.Type == EventToolUseEnd && ev.ToolCall != nil {
			finalizedToolCalls = append(finalizedToolCalls, *ev.ToolCall)
		}
		if !r.send(ctx, out, ev) {
			return streamErr, sawDone, forwardedEvents, finalizedToolCalls
		}
		forwardedEvents++
	}
	return streamErr, sawDone, forwardedEvents, finalizedToolCalls
}

func (r *ReliableStreamClient) sendLifecycle(
	ctx context.Context,
	out chan<- StreamEvent,
	execution *InferenceExecution,
	operation InferenceOperation,
	phase StreamLifecyclePhase,
	attemptID string,
	attempt int,
	maxAttempts int,
	retryCount int,
	reason error,
	retryIn time.Duration,
	resetPartial bool,
	startedAt time.Time,
) bool {
	snapshot := execution.Snapshot()
	lastSubmissionID := ""
	if n := len(snapshot.Submissions); n > 0 {
		lastSubmissionID = snapshot.Submissions[n-1].ID
	}
	lifecycle := &StreamLifecycle{
		Phase:           phase,
		OperationID:     operation.ID,
		OperationKind:   operation.Kind,
		WorkloadProfile: operation.WorkloadProfile,
		PayloadVersion:  operation.PayloadVersion,
		AttemptID:       attemptID,
		Attempt:         attempt,
		MaxAttempts:     maxAttempts,
		SubmissionID:    lastSubmissionID,
		SubmissionCount: len(snapshot.Submissions),
		RetryCount:      retryCount,
		MaxRetries:      r.cfg.MaxRetries,
		RetryIn:         retryIn,
		Elapsed:         time.Since(startedAt),
		ResetPartial:    resetPartial,
	}
	if reason != nil {
		lifecycle.Reason = StreamErrorSummary(reason)
	}
	return r.send(ctx, out, StreamEvent{Type: EventLifecycle, Lifecycle: lifecycle})
}

func streamRecoverySupported(client StreamClient, plan RecoveryPlan) bool {
	switch plan.Action {
	case RecoveryReplaySame, RecoveryWaitThenReplay, RecoverySwitchTransport:
		return true
	case RecoveryRefreshAuth:
		_, ok := client.(InferenceRecoveryApplier)
		return ok
	default:
		return false
	}
}

func (r *ReliableStreamClient) send(ctx context.Context, out chan<- StreamEvent, ev StreamEvent) bool {
	select {
	case out <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

func waitWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
