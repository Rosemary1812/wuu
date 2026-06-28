package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/compact"
	"github.com/blueberrycongee/wuu/internal/modelprofile"
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

func TestActiveProfileInjectsHiddenInceptionAnchors(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetActiveProfile(modelprofile.Resolve("openai", "gpt-5-codex"), true)
	step := &inceptionLoopStep{results: []agent.StepResult{{Content: "done"}}}
	var visible []providers.ChatMessage

	if _, err := agent.RunToolLoop(context.Background(), []providers.ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "start"},
	}, agent.LoopConfig{
		Model: "m",
		Tools: kit,
		OnMessage: func(msg providers.ChatMessage) {
			visible = append(visible, msg)
		},
	}, step); err != nil {
		t.Fatal(err)
	}
	if len(step.calls) != 1 {
		t.Fatalf("expected one request, got %d", len(step.calls))
	}
	anchorIndex, ok := compact.FindContextAnchorIndex(step.calls[0].Messages, 0)
	if !ok {
		t.Fatalf("active profile request should inject Inception anchors: %+v", step.calls[0].Messages)
	}
	if !step.calls[0].Messages[anchorIndex].Hidden {
		t.Fatalf("inception anchor must be hidden: %+v", step.calls[0].Messages[anchorIndex])
	}
	for _, msg := range visible {
		if msg.Name == compact.ContextAnchorMessageName {
			t.Fatalf("hidden anchor leaked through OnMessage: %+v", visible)
		}
	}
}

func TestActiveProfileLoadsInceptionThroughToolSearchAndRewrites(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetAgentIdentity("root", agentthread.RootPath)
	kit.SetActiveProfile(modelprofile.Resolve("openai", "gpt-5-codex"), true)

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      compact.InceptionToolName,
		Arguments: `{"anchor_id":0,"summary":"state"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "deferred") {
		t.Fatalf("unloaded inception should require tool_search, got %v", err)
	}
	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "tool_search",
		Arguments: `{"query":"select:inception","limit":1}`,
	})
	if err != nil {
		t.Fatalf("tool_search: %v", err)
	}
	if !strings.Contains(resp, compact.InceptionToolName) {
		t.Fatalf("tool_search should load inception, got %s", resp)
	}

	step := &inceptionLoopStep{results: []agent.StepResult{
		{ToolCalls: []providers.ToolCall{{
			ID:        "call_inception",
			Name:      compact.InceptionToolName,
			Arguments: `{"anchor_id":0,"summary":"## Task state\nContinue with compact context.\n\n## External state\nNo rollback.\n\n## Verification state\nNot run.\n\n## Evidence pointers\n- active profile loaded inception\n\n## Next step\nContinue."}`,
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
		t.Fatal("expected loaded inception to rewrite history")
	}
	if len(step.calls) != 2 {
		t.Fatalf("expected follow-up request after rewrite, got %d", len(step.calls))
	}
	if _, ok := compact.FindContextAnchorIndex(step.calls[0].Messages, 0); !ok {
		t.Fatalf("first request missing context anchor: %+v", step.calls[0].Messages)
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
