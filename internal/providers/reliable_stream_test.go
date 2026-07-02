package providers

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type reliableStreamAttempt struct {
	events []StreamEvent
	err    error
}

type reliableStreamMockClient struct {
	attempts  []reliableStreamAttempt
	callCount int
}

func (m *reliableStreamMockClient) Chat(context.Context, ChatRequest) (ChatResponse, error) {
	return ChatResponse{}, nil
}

func (m *reliableStreamMockClient) StreamChat(context.Context, ChatRequest) (<-chan StreamEvent, error) {
	idx := m.callCount
	m.callCount++
	if idx >= len(m.attempts) {
		return nil, errors.New("unexpected extra stream attempt")
	}
	attempt := m.attempts[idx]
	if attempt.err != nil {
		return nil, attempt.err
	}
	ch := make(chan StreamEvent, len(attempt.events))
	for _, ev := range attempt.events {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

func reliableTestRetryConfig(maxRetries int) RetryConfig {
	return RetryConfig{MaxRetries: maxRetries, InitialDelay: time.Nanosecond, MaxDelay: time.Nanosecond}
}

func collectReliableEvents(t *testing.T, ch <-chan StreamEvent) []StreamEvent {
	t.Helper()
	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	return events
}

func eventTypes(events []StreamEvent) []StreamEventType {
	out := make([]StreamEventType, 0, len(events))
	for _, ev := range events {
		out = append(out, ev.Type)
	}
	return out
}

func TestReliableStreamClientSingleSuccess(t *testing.T) {
	inner := &reliableStreamMockClient{attempts: []reliableStreamAttempt{{events: []StreamEvent{
		{Type: EventContentDelta, Content: "ok"},
		{Type: EventDone},
	}}}}
	client := NewReliableStreamClient(inner, reliableTestRetryConfig(3), nil)
	ch, err := client.StreamChat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	events := collectReliableEvents(t, ch)
	if got, want := eventTypes(events), []StreamEventType{EventContentDelta, EventDone}; !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	if inner.callCount != 1 {
		t.Fatalf("attempts = %d, want 1", inner.callCount)
	}
}

func TestReliableStreamClientRetriesStreamErrorsThenSucceeds(t *testing.T) {
	retryErr := NewIncompleteStreamError("temporary drop")
	inner := &reliableStreamMockClient{attempts: []reliableStreamAttempt{
		{events: []StreamEvent{{Type: EventError, Error: retryErr}}},
		{events: []StreamEvent{{Type: EventError, Error: retryErr}}},
		{events: []StreamEvent{{Type: EventContentDelta, Content: "ok"}, {Type: EventDone}}},
	}}
	var attempts []int
	client := NewReliableStreamClient(inner, reliableTestRetryConfig(3), func(attempt, _ int, _ error, _ time.Duration) {
		attempts = append(attempts, attempt)
	})
	ch, err := client.StreamChat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	events := collectReliableEvents(t, ch)
	if got, want := eventTypes(events), []StreamEventType{EventContentDelta, EventDone}; !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(attempts, []int{1, 2}) {
		t.Fatalf("retry attempts = %v", attempts)
	}
}

func TestReliableStreamClientRetriesConnectFailure(t *testing.T) {
	inner := &reliableStreamMockClient{attempts: []reliableStreamAttempt{
		{err: &HTTPError{StatusCode: 500, Body: "upstream"}},
		{events: []StreamEvent{{Type: EventContentDelta, Content: "ok"}, {Type: EventDone}}},
	}}
	var retries int
	client := NewReliableStreamClient(inner, reliableTestRetryConfig(3), func(int, int, error, time.Duration) { retries++ })
	ch, err := client.StreamChat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	events := collectReliableEvents(t, ch)
	if got, want := eventTypes(events), []StreamEventType{EventContentDelta, EventDone}; !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	if retries != 1 || inner.callCount != 2 {
		t.Fatalf("retries=%d attempts=%d, want 1/2", retries, inner.callCount)
	}
}

func TestReliableStreamClientEmitsFinalErrorAfterMaxRetries(t *testing.T) {
	retryErr := NewIncompleteStreamError("temporary drop")
	inner := &reliableStreamMockClient{attempts: []reliableStreamAttempt{
		{events: []StreamEvent{{Type: EventError, Error: retryErr}}},
		{events: []StreamEvent{{Type: EventError, Error: retryErr}}},
		{events: []StreamEvent{{Type: EventError, Error: retryErr}}},
		{events: []StreamEvent{{Type: EventError, Error: retryErr}}},
	}}
	var attempts []int
	client := NewReliableStreamClient(inner, reliableTestRetryConfig(3), func(attempt, _ int, _ error, _ time.Duration) {
		attempts = append(attempts, attempt)
	})
	ch, err := client.StreamChat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	events := collectReliableEvents(t, ch)
	if len(events) != 1 || events[0].Type != EventError || events[0].Error == nil {
		t.Fatalf("events = %+v, want final error", events)
	}
	if !reflect.DeepEqual(attempts, []int{1, 2, 3}) {
		t.Fatalf("retry attempts = %v", attempts)
	}
}

func TestReliableStreamClientContextCancelStopsRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	inner := &reliableStreamMockClient{attempts: []reliableStreamAttempt{
		{events: []StreamEvent{{Type: EventError, Error: NewIncompleteStreamError("temporary drop")}}},
		{events: []StreamEvent{{Type: EventDone}}},
	}}
	client := NewReliableStreamClient(inner, reliableTestRetryConfig(3), func(int, int, error, time.Duration) { cancel() })
	ch, err := client.StreamChat(ctx, ChatRequest{})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	if events := collectReliableEvents(t, ch); len(events) != 0 {
		t.Fatalf("events = %+v, want closed without error", events)
	}
}

func TestReliableStreamClientDoesNotRetryNonRetryableError(t *testing.T) {
	inner := &reliableStreamMockClient{attempts: []reliableStreamAttempt{
		{events: []StreamEvent{{Type: EventError, Error: NewNonRetryableStreamError("terminal")}}},
		{events: []StreamEvent{{Type: EventDone}}},
	}}
	var retries int
	client := NewReliableStreamClient(inner, reliableTestRetryConfig(3), func(int, int, error, time.Duration) { retries++ })
	ch, err := client.StreamChat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	events := collectReliableEvents(t, ch)
	if len(events) != 1 || events[0].Type != EventError {
		t.Fatalf("events = %+v, want final error", events)
	}
	if retries != 0 || inner.callCount != 1 {
		t.Fatalf("retries=%d attempts=%d, want 0/1", retries, inner.callCount)
	}
}

func TestReliableStreamClientRetriesTruncatedStream(t *testing.T) {
	inner := &reliableStreamMockClient{attempts: []reliableStreamAttempt{
		{events: []StreamEvent{{Type: EventContentDelta, Content: "partial"}}},
		{events: []StreamEvent{{Type: EventContentDelta, Content: "ok"}, {Type: EventDone}}},
	}}
	client := NewReliableStreamClient(inner, reliableTestRetryConfig(3), nil)
	ch, err := client.StreamChat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	events := collectReliableEvents(t, ch)
	if got, want := eventTypes(events), []StreamEventType{EventContentDelta, EventContentDelta, EventDone}; !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	if events[0].Content != "partial" || events[1].Content != "ok" {
		t.Fatalf("unexpected content events: %+v", events)
	}
}
