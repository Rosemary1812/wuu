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

// PartialStreamError wraps a retry trigger after one or more events from the
// failed attempt were already handed to the caller. Consumers that maintain
// local accumulated state can use ForwardedEvents to discard any stale events
// that race with their retry hook.
type PartialStreamError struct {
	Err             error
	ForwardedEvents int
}

func (e *PartialStreamError) Error() string {
	if e == nil || e.Err == nil {
		return "partial stream failed"
	}
	return e.Err.Error()
}

func (e *PartialStreamError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func ForwardedEventsBeforeRetry(err error) int {
	var partial *PartialStreamError
	if errors.As(err, &partial) && partial != nil {
		return partial.ForwardedEvents
	}
	return 0
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
	operation := EnsureInferenceOperation(req.Operation, InferenceOperationAuxiliary, InferenceProfileInteractive)
	req.Operation = operation
	startedAt := time.Now()
	maxAttempts := r.cfg.MaxRetries + 1
	attempt := 1
	for {
		if ctx.Err() != nil {
			return
		}
		if !r.sendLifecycle(ctx, out, operation, StreamPhaseConnecting, attempt, maxAttempts, nil, 0, false, startedAt) {
			return
		}

		ch, err := r.inner.StreamChat(ctx, req)
		forwardedEvents := 0
		var finalizedToolCalls []ToolCall
		if err == nil {
			var sawDone bool
			err, sawDone, forwardedEvents, finalizedToolCalls = r.forwardAttempt(ctx, ch, out, operation, attempt, maxAttempts, startedAt)
			if ctx.Err() != nil {
				return
			}
			if sawDone && err == nil {
				return
			}
			if err == nil {
				err = NewIncompleteStreamError("stream closed before done")
			}
		}
		if forwardedEvents > 0 {
			err = &PartialStreamError{Err: err, ForwardedEvents: forwardedEvents}
		}
		canRetry := r.retry(ctx, attempt-1, err)
		if canRetry && r.replayGuard != nil {
			if guardErr := r.replayGuard(StreamRetryContext{
				Operation:          operation,
				Attempt:            attempt,
				ForwardedEvents:    forwardedEvents,
				FinalizedToolCalls: append([]ToolCall(nil), finalizedToolCalls...),
				Err:                err,
			}); guardErr != nil {
				err = &ReplayBlockedError{Cause: err, Reason: guardErr}
				canRetry = false
			}
		}
		if !canRetry {
			if !r.sendLifecycle(ctx, out, operation, StreamPhaseFailed, attempt, maxAttempts, err, 0, false, startedAt) {
				return
			}
			r.send(ctx, out, StreamEvent{Type: EventError, Error: err})
			return
		}

		delay := backoffDelay(attempt-1, r.cfg.InitialDelay, r.cfg.MaxDelay, err)
		retryCount := attempt
		nextAttempt := attempt + 1
		if !r.sendLifecycle(ctx, out, operation, StreamPhaseReconnecting, nextAttempt, maxAttempts, err, delay, forwardedEvents > 0, startedAt) {
			return
		}
		if r.onRetry != nil {
			r.onRetry(retryCount, r.cfg.MaxRetries, err, delay)
		}
		if waitWithContext(ctx, delay) != nil {
			return
		}
		attempt = nextAttempt
	}
}

func (r *ReliableStreamClient) forwardAttempt(
	ctx context.Context,
	ch <-chan StreamEvent,
	out chan<- StreamEvent,
	operation InferenceOperation,
	attempt int,
	maxAttempts int,
	startedAt time.Time,
) (error, bool, int, []ToolCall) {
	var streamErr error
	var sawDone bool
	var forwardedEvents int
	var finalizedToolCalls []ToolCall
	connected := false
	for ev := range ch {
		if !connected {
			if !r.sendLifecycle(ctx, out, operation, StreamPhaseConnected, attempt, maxAttempts, nil, 0, false, startedAt) {
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
	operation InferenceOperation,
	phase StreamLifecyclePhase,
	attempt int,
	maxAttempts int,
	reason error,
	retryIn time.Duration,
	resetPartial bool,
	startedAt time.Time,
) bool {
	lifecycle := &StreamLifecycle{
		Phase:           phase,
		OperationID:     operation.ID,
		OperationKind:   operation.Kind,
		WorkloadProfile: operation.WorkloadProfile,
		PayloadVersion:  operation.PayloadVersion,
		AttemptID:       operation.AttemptID(attempt),
		Attempt:         attempt,
		MaxAttempts:     maxAttempts,
		RetryCount:      attempt - 1,
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

func (r *ReliableStreamClient) retry(ctx context.Context, attempt int, err error) bool {
	if ctx.Err() != nil || !IsRetryable(err) {
		return false
	}
	return attempt < r.cfg.MaxRetries
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
