package runtime

import (
	"context"
	"testing"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/providers"
)

type sideThreadRuntimeTools struct{}

func (sideThreadRuntimeTools) Definitions() []providers.ToolDefinition {
	return []providers.ToolDefinition{{Name: "write_file"}}
}

func (sideThreadRuntimeTools) Execute(context.Context, providers.ToolCall) (string, error) {
	return "", nil
}

func TestNewSideThreadRunnerRemovesMutableMainThreadSurface(t *testing.T) {
	called := false
	base := &agent.StreamRunner{
		Client:               providers.AdaptStreamClient(&staticClient{}),
		Tools:                sideThreadRuntimeTools{},
		Model:                "test-model",
		SystemPrompt:         "main prompt",
		MaxSteps:             20,
		BeforeStep:           func() []providers.ChatMessage { called = true; return nil },
		BeforeRequestContext: func() []agent.ContextSegment { called = true; return nil },
		BeforeRequest:        func(context.Context, *providers.ChatRequest) error { called = true; return nil },
		AfterTurn:            func(context.Context, *agent.StreamRunner, []providers.ChatMessage, agent.LoopResult) { called = true },
		OnEvent:              func(providers.StreamEvent) { called = true },
	}
	runner, err := (&Session{StreamRunner: base}).NewSideThreadRunner("side-actual-id")
	if err != nil {
		t.Fatalf("NewSideThreadRunner: %v", err)
	}
	if runner.Tools != nil || runner.BeforeStep != nil || runner.BeforeRequestContext != nil || runner.BeforeRequest != nil || runner.AfterTurn != nil || runner.OnEvent != nil {
		t.Fatalf("side runner retained mutable callbacks: %+v", runner)
	}
	if runner.MaxSteps != 1 {
		t.Fatalf("MaxSteps=%d want 1", runner.MaxSteps)
	}
	if runner.PromptCacheKey != "side-thread:side-actual-id" {
		t.Fatalf("PromptCacheKey=%q", runner.PromptCacheKey)
	}
	if called {
		t.Fatal("constructing side runner invoked a main-thread callback")
	}
}

type staticClient struct{}

func (*staticClient) Chat(context.Context, providers.ChatRequest) (providers.ChatResponse, error) {
	return providers.ChatResponse{Content: "ok"}, nil
}
