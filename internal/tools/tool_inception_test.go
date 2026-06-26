package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/compact"
	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestInceptionToolRewritesHistoryThroughLoop(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetAgentIdentity("root", agentthread.RootPath)
	step := &inceptionLoopStep{results: []agent.StepResult{
		{ToolCalls: []providers.ToolCall{{
			ID:        "call_inception",
			Name:      compact.InceptionToolName,
			Arguments: `{"anchor_id":0,"summary":"## Task state\nContinue from current files.\n\n## External state\nNo files or processes were rolled back.\n\n## Verification state\nFocused tests remain to run.\n\n## Evidence pointers\n- internal/tools/tool_inception.go\n\n## Next step\nRun the targeted tests."}`,
		}}},
		{Content: "continued"},
	}}

	res, err := agent.RunToolLoop(context.Background(), []providers.ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "start"},
	}, agent.LoopConfig{
		Model:           "m",
		Tools:           kit,
		PostToolRewrite: compact.RewriteHistoryFromInternalToolMessagesWithContext,
	}, step)
	if err != nil {
		t.Fatal(err)
	}
	if !res.HistoryRewritten {
		t.Fatal("expected Inception to rewrite history")
	}
	if err := providers.ValidateToolCallHistory(res.NewMessages); err != nil {
		t.Fatalf("rewritten history must stay provider-valid: %v\n%+v", err, res.NewMessages)
	}
	if len(step.calls) != 2 {
		t.Fatalf("expected follow-up request after rewrite, got %d", len(step.calls))
	}
	if _, ok := compact.FindContextAnchorIndex(step.calls[0].Messages, 0); !ok {
		t.Fatalf("first request missing automatic context anchor: %+v", step.calls[0].Messages)
	}
	var foundContinuation bool
	for _, msg := range step.calls[1].Messages {
		if msg.Role == "tool" || len(msg.ToolCalls) > 0 {
			t.Fatalf("second request should not retain old Inception tool chain: %+v", step.calls[1].Messages)
		}
		if msg.Name == compact.ContextContinuationName && strings.Contains(msg.Content, "External state") {
			foundContinuation = true
		}
	}
	if !foundContinuation {
		t.Fatalf("second request missing continuation summary: %+v", step.calls[1].Messages)
	}
}

func TestInceptionToolRejectsWorkerPath(t *testing.T) {
	tool := NewInceptionTool(&Env{AgentPath: "/root/worker"})
	_, err := tool.Execute(context.Background(), `{"anchor_id":0,"summary":"state"}`)
	if err == nil || !strings.Contains(err.Error(), "only available to the main agent") {
		t.Fatalf("expected main-agent guard, got %v", err)
	}
}

type inceptionLoopStep struct {
	results []agent.StepResult
	calls   []providers.ChatRequest
}

func (s *inceptionLoopStep) Execute(_ context.Context, req providers.ChatRequest) (agent.StepResult, error) {
	s.calls = append(s.calls, req)
	if len(s.results) == 0 {
		return agent.StepResult{}, nil
	}
	result := s.results[0]
	s.results = s.results[1:]
	return result, nil
}
