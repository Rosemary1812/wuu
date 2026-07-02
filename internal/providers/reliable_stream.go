package providers

import (
	"context"
	"errors"
	"time"
)

// StreamRetryHook is called just before each reconnect attempt.
// attempt is 1-based (first retry = 1). maxRetries is the configured cap.
// err is the transport error that triggered the retry.
// retryIn is the computed backoff delay before the next connect.
type StreamRetryHook func(attempt, maxRetries int, err error, retryIn time.Duration)

// ReliableStreamClient wraps a StreamClient and transparently retries dropped
// streams. Callers receive a single output channel; reconnectable inner errors
// are consumed internally and only the final unrecoverable error is forwarded.
type ReliableStreamClient struct {
	inner   StreamClient
	cfg     RetryConfig
	onRetry StreamRetryHook
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
func NewReliableStreamClient(inner StreamClient, cfg RetryConfig, onRetry StreamRetryHook) *ReliableStreamClient {
	return &ReliableStreamClient{
		inner:   inner,
		cfg:     NormalizeRetryConfig(cfg),
		onRetry: onRetry,
	}
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
	attempt := 0
	for {
		if ctx.Err() != nil {
			return
		}

		ch, err := r.inner.StreamChat(ctx, req)
		if err != nil {
			if !r.retry(ctx, attempt, err) {
				r.send(ctx, out, StreamEvent{Type: EventError, Error: err})
				return
			}
			delay := backoffDelay(attempt, r.cfg.InitialDelay, r.cfg.MaxDelay, err)
			attempt++
			if r.onRetry != nil {
				r.onRetry(attempt, r.cfg.MaxRetries, err, delay)
			}
			if waitWithContext(ctx, delay) != nil {
				return
			}
			continue
		}

		streamErr, sawDone, forwardedEvents := r.forwardAttempt(ctx, ch, out)
		if ctx.Err() != nil {
			return
		}
		if sawDone && streamErr == nil {
			return
		}
		if streamErr == nil {
			streamErr = NewIncompleteStreamError("stream closed before done")
		}
		if forwardedEvents > 0 {
			streamErr = &PartialStreamError{Err: streamErr, ForwardedEvents: forwardedEvents}
		}
		if !r.retry(ctx, attempt, streamErr) {
			r.send(ctx, out, StreamEvent{Type: EventError, Error: streamErr})
			return
		}
		if r.onRetry != nil && forwardedEvents > 0 && !r.send(ctx, out, StreamEvent{Type: EventReconnect, Error: streamErr}) {
			return
		}
		delay := backoffDelay(attempt, r.cfg.InitialDelay, r.cfg.MaxDelay, streamErr)
		attempt++
		if r.onRetry != nil {
			r.onRetry(attempt, r.cfg.MaxRetries, streamErr, delay)
		}
		if waitWithContext(ctx, delay) != nil {
			return
		}
	}
}

func (r *ReliableStreamClient) forwardAttempt(ctx context.Context, ch <-chan StreamEvent, out chan<- StreamEvent) (error, bool, int) {
	var streamErr error
	var sawDone bool
	var forwardedEvents int
	for ev := range ch {
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
		if !r.send(ctx, out, ev) {
			return streamErr, sawDone, forwardedEvents
		}
		forwardedEvents++
	}
	return streamErr, sawDone, forwardedEvents
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
