package reviewsession

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
)

type fakeReviewClient struct {
	requests []providers.ChatRequest
	response providers.ChatResponse
	err      error
	delay    time.Duration
}

func (f *fakeReviewClient) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	f.requests = append(f.requests, req)
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return providers.ChatResponse{}, ctx.Err()
		}
	}
	if f.err != nil {
		return providers.ChatResponse{}, f.err
	}
	return f.response, nil
}

func TestNewDefaultsToRestrictedBoundary(t *testing.T) {
	client := &fakeReviewClient{}
	session, err := New(Config{Client: client, Model: "review-model"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	boundary := session.Boundary()
	if boundary.PermissionProfile != PermissionProfileReadOnly || boundary.ApprovalPolicy != ApprovalPolicyNever {
		t.Fatalf("unexpected boundary: %+v", boundary)
	}
	if boundary.Tools || boundary.MCP || boundary.Hooks || boundary.Plugins || boundary.Skills || boundary.MemoryWrites || boundary.DurableWrites {
		t.Fatalf("restricted boundary should disable extension/write surfaces: %+v", boundary)
	}
}

func TestNewRejectsUnrestrictedBoundary(t *testing.T) {
	_, err := New(Config{
		Client: &fakeReviewClient{},
		Model:  "review-model",
		Boundary: Boundary{
			PermissionProfile: PermissionProfileReadOnly,
			ApprovalPolicy:    ApprovalPolicyNever,
			Tools:             true,
		},
	})
	if err == nil {
		t.Fatal("expected unrestricted boundary to be rejected")
	}
}

func TestRunSendsProviderNeutralLLMOnlyRequest(t *testing.T) {
	client := &fakeReviewClient{response: providers.ChatResponse{Content: `{"decision":"approved"}`}}
	session, err := New(Config{
		Client:          client,
		Model:           "review-model",
		Role:            "review",
		ParentSessionID: "session-1",
		ParentTurnID:    "turn-1",
		Effort:          "low",
		ProviderOptions: map[string]any{"reasoningEffort": "low"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	result := session.Run(context.Background(), Request{
		SystemPrompt: "system",
		Prompt:       "review this",
		MaxTokens:    123,
	})
	if result.Outcome != OutcomeCompleted || result.Content != `{"decision":"approved"}` {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.ParentSessionID != "session-1" || result.ParentTurnID != "turn-1" || result.Role != "review" {
		t.Fatalf("trace metadata missing: %+v", result)
	}
	if result.DurationMS < 0 || result.CompletedAt.IsZero() {
		t.Fatalf("duration metadata missing: %+v", result)
	}
	if len(client.requests) != 1 {
		t.Fatalf("expected one request, got %d", len(client.requests))
	}
	req := client.requests[0]
	if req.Model != "review-model" || req.MaxTokens != 123 || req.Effort != "low" {
		t.Fatalf("unexpected chat request: %+v", req)
	}
	if len(req.Tools) != 0 {
		t.Fatalf("review session must not send tools: %+v", req.Tools)
	}
	if len(req.Messages) != 2 || req.Messages[0].Role != "system" || req.Messages[1].Role != "user" {
		t.Fatalf("unexpected messages: %+v", req.Messages)
	}
	if got := req.ProviderOptions["reasoningEffort"]; got != "low" {
		t.Fatalf("provider options = %#v", req.ProviderOptions)
	}
}

func TestRunTimesOutFailClosed(t *testing.T) {
	client := &fakeReviewClient{delay: 100 * time.Millisecond}
	session, err := New(Config{Client: client, Model: "review-model", Timeout: 10 * time.Millisecond})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	result := session.Run(context.Background(), Request{Prompt: "review"})
	if result.Outcome != OutcomeTimedOut || result.ErrorKind != "timed_out" {
		t.Fatalf("expected timed_out result, got %+v", result)
	}
}

func TestRunProviderErrorFailsClosed(t *testing.T) {
	client := &fakeReviewClient{err: errors.New("upstream down")}
	session, err := New(Config{Client: client, Model: "review-model"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	result := session.Run(context.Background(), Request{Prompt: "review"})
	if result.Outcome != OutcomeFailed || result.ErrorKind != "provider_error" {
		t.Fatalf("expected provider_error result, got %+v", result)
	}
}
