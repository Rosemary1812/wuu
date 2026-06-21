package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
)

type fakeStreamBackedClient struct {
	streamCalls int
	events      []providers.StreamEvent
}

func (f *fakeStreamBackedClient) Chat(_ context.Context, _ providers.ChatRequest) (providers.ChatResponse, error) {
	return providers.ChatResponse{Content: "should not be used"}, nil
}

func (f *fakeStreamBackedClient) StreamChat(_ context.Context, _ providers.ChatRequest) (<-chan providers.StreamEvent, error) {
	f.streamCalls++
	ch := make(chan providers.StreamEvent, len(f.events))
	for _, event := range f.events {
		ch <- event
	}
	close(ch)
	return ch, nil
}

func TestRunner_UsesStreamingStepPath(t *testing.T) {
	client := &fakeStreamBackedClient{events: []providers.StreamEvent{
		{Type: providers.EventContentDelta, Content: "streamed"},
		{Type: providers.EventDone},
	}}
	runner := Runner{Client: client, Model: "gpt-test"}

	answer, err := runner.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if answer != "streamed" {
		t.Fatalf("unexpected answer: %s", answer)
	}
	if client.streamCalls != 1 {
		t.Fatalf("expected StreamChat to be called once, got %d", client.streamCalls)
	}
}

func TestRunner_RunWithToolCall(t *testing.T) {
	client := &fakeClient{responses: []providers.ChatResponse{
		{
			ToolCalls: []providers.ToolCall{{ID: "call_1", Name: "run_shell", Arguments: `{"command":"echo hi"}`}},
		},
		{Content: "final answer"},
	}}
	tool := &fakeTools{}
	runner := Runner{Client: client, Tools: tool, Model: "gpt-test", SystemPrompt: "sys", MaxSteps: 4}

	answer, err := runner.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if answer != "final answer" {
		t.Fatalf("unexpected answer: %s", answer)
	}
	if len(tool.calls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(tool.calls))
	}

	lastReq := client.requests[len(client.requests)-1]
	foundToolMessage := false
	for _, msg := range lastReq.Messages {
		if msg.Role == "tool" && msg.ToolCallID == "call_1" {
			foundToolMessage = true
			break
		}
	}
	if !foundToolMessage {
		t.Fatal("expected tool message in follow-up request")
	}
}

func TestRunner_OutputTruncationCompletesRun(t *testing.T) {
	client := &fakeClient{responses: []providers.ChatResponse{
		{Content: "part one ", Truncated: true, StopReason: "length"},
	}}
	runner := Runner{Client: client, Model: "gpt-test"}

	res, err := runner.RunWithUsage(context.Background(), "write a story", nil)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if res.Content != "part one " {
		t.Fatalf("expected first partial answer, got %q", res.Content)
	}
	if res.FinishReason != providers.FinishReasonLength || res.StopReason != "length" || !res.Truncated {
		t.Fatalf("expected length finish metadata, got reason=%q stop=%q truncated=%v", res.FinishReason, res.StopReason, res.Truncated)
	}
	if len(client.requests) != 1 {
		t.Fatalf("expected 1 chat call, got %d", len(client.requests))
	}
}

func TestRunner_MaxTokensStopReasonNormalizesLength(t *testing.T) {
	client := &fakeClient{responses: []providers.ChatResponse{
		{Content: "x", Truncated: true, StopReason: "max_tokens"},
	}}
	runner := Runner{Client: client, Model: "gpt-test"}

	res, err := runner.RunWithUsage(context.Background(), "loop", nil)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if res.Content != "x" {
		t.Fatalf("expected partial answer, got %q", res.Content)
	}
	if res.FinishReason != providers.FinishReasonLength || res.StopReason != "max_tokens" || !res.Truncated {
		t.Fatalf("expected max_tokens to normalize to length, got reason=%q stop=%q truncated=%v", res.FinishReason, res.StopReason, res.Truncated)
	}
	if len(client.requests) != 1 {
		t.Fatalf("expected 1 chat call, got %d", len(client.requests))
	}
}

func TestRunner_ContextOverflowAutoCompact(t *testing.T) {
	overflow := &providers.HTTPError{StatusCode: 400, Body: "context_length_exceeded", ContextOverflow: true}
	client := &fakeClient{
		responses: []providers.ChatResponse{
			// 1) initial real call: overflow
			// 2) compact() will issue a summarization Chat call
			{Content: "summary of older turns"},
			// 3) re-issued real call after compaction: success
			{Content: "ok done"},
		},
		errors: []error{overflow, nil, nil},
	}
	// Need enough message history for compact() to actually trim some.
	runner := Runner{Client: client, Model: "gpt-test", SystemPrompt: "sys"}

	// Inject a long history by going through Run with repeated tool
	// calls? Simpler: just call directly. The compact package keeps
	// the last 4 messages, so we need >4 in the conversation. The
	// runner starts with system+user (2). To exercise the auto-compact
	// path we craft a tool-loop scenario.
	answer, err := runner.Run(context.Background(), "do work")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if answer != "ok done" {
		t.Fatalf("expected 'ok done', got %q", answer)
	}
}

func TestRunner_ContextWindowOverride(t *testing.T) {
	// Force the proactive auto-compact threshold by pinning a tiny
	// window via ContextWindowOverride. The first response reports
	// 950 tokens of usage, which exceeds 90% of 1000. The second
	// response answers cleanly.
	client := &fakeClient{responses: []providers.ChatResponse{
		{
			ToolCalls: []providers.ToolCall{{ID: "c1", Name: "run_shell", Arguments: `{}`}},
			Usage:     &providers.TokenUsage{InputTokens: 950, OutputTokens: 0},
		},
		{Content: "summarized then resumed"},
		{Content: "final"},
	}}
	tool := &fakeTools{}
	runner := Runner{
		Client:                client,
		Tools:                 tool,
		Model:                 "totally-unknown-model",
		ContextWindowOverride: 1000,
	}

	out, err := runner.Run(context.Background(), "go")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty answer")
	}
	// The override should have produced a request count >= 2
	// (the proactive compact path consumed at least one extra
	// summarization round-trip).
	if len(client.requests) < 2 {
		t.Fatalf("expected proactive compact to fire, got %d requests", len(client.requests))
	}
}

func TestRunner_MaxStepsExceeded(t *testing.T) {
	client := &fakeClient{responses: []providers.ChatResponse{{ToolCalls: []providers.ToolCall{{ID: "c", Name: "run_shell", Arguments: `{}`}}}}}
	runner := Runner{Client: client, Tools: &fakeTools{}, Model: "gpt-test", MaxSteps: 1}

	_, err := runner.Run(context.Background(), "task")
	if err == nil {
		t.Fatal("expected max steps error")
	}
}

type fakeClient struct {
	responses []providers.ChatResponse
	errors    []error // optional, indexed parallel to responses
	requests  []providers.ChatRequest
	idx       int
}

func (f *fakeClient) Chat(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	f.requests = append(f.requests, req)
	if f.idx >= len(f.responses) {
		return providers.ChatResponse{}, errors.New("unexpected chat call")
	}
	resp := f.responses[f.idx]
	var err error
	if f.idx < len(f.errors) {
		err = f.errors[f.idx]
	}
	f.idx++
	if err != nil {
		return providers.ChatResponse{}, err
	}
	return resp, nil
}

type fakeTools struct {
	calls []providers.ToolCall
}

func (f *fakeTools) Definitions() []providers.ToolDefinition {
	return []providers.ToolDefinition{{Name: "run_shell", InputSchema: map[string]any{"type": "object"}}}
}

func (f *fakeTools) Execute(_ context.Context, call providers.ToolCall) (string, error) {
	f.calls = append(f.calls, call)
	return `{"ok":true}`, nil
}
