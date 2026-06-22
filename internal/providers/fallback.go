package providers

import (
	"context"
	"errors"
	"sync"
)

// FallbackClient wraps a StreamClient and transparently falls back to
// an alternative model after consecutive overload (429/529) errors.
//
// The fallback is temporary: once a request succeeds on the primary
// model, the counter resets and subsequent requests use the primary.
type FallbackClient struct {
	inner         StreamClient
	fallbackModel string
	threshold     int // consecutive overloads to trigger fallback

	mu            sync.Mutex
	failures      int
	usingFallback bool
}

// NewFallbackClient wraps inner with automatic model fallback.
// threshold is the number of consecutive overload errors before
// switching to fallbackModel. A typical value is 3.
func NewFallbackClient(inner StreamClient, fallbackModel string, threshold int) *FallbackClient {
	if threshold <= 0 {
		threshold = 3
	}
	return &FallbackClient{
		inner:         inner,
		fallbackModel: fallbackModel,
		threshold:     threshold,
	}
}

// Chat delegates to the inner client, applying model fallback.
func (f *FallbackClient) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	req = f.maybeSwapModel(req)
	resp, err := f.inner.Chat(ctx, req)
	f.recordOutcome(err)
	return resp, err
}

// StreamChat delegates to the inner client, applying model fallback.
func (f *FallbackClient) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	req = f.maybeSwapModel(req)
	ch, err := f.inner.StreamChat(ctx, req)
	if err != nil {
		f.recordOutcome(err)
		return nil, err
	}
	// Wrap the channel to observe the terminal event for outcome recording.
	out := make(chan StreamEvent, cap(ch))
	go func() {
		defer close(out)
		var lastErr error
		for ev := range ch {
			if ev.Type == EventError && ev.Error != nil {
				lastErr = ev.Error
			}
			out <- ev
		}
		f.recordOutcome(lastErr)
	}()
	return out, nil
}

// IsFallbackActive reports whether the client is currently using the
// fallback model. Callers can use this for status display.
func (f *FallbackClient) IsFallbackActive() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.usingFallback
}

func (f *FallbackClient) maybeSwapModel(req ChatRequest) ChatRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.usingFallback && f.fallbackModel != "" {
		req.Model = f.fallbackModel
	}
	return req
}

func (f *FallbackClient) recordOutcome(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err != nil && isOverloadError(err) {
		f.failures++
		if f.failures >= f.threshold && !f.usingFallback {
			f.usingFallback = true
		}
	} else if err == nil {
		f.failures = 0
		f.usingFallback = false
	}
}

func isOverloadError(err error) bool {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode == 429 || httpErr.StatusCode == 529
	}
	return false
}
