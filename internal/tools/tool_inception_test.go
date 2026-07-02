package tools

import (
	"context"
	"fmt"
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
			Arguments: structuredInceptionArgs(0),
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
		if msg.Name == compact.ContextContinuationName && strings.Contains(msg.Content, "## Progress") {
			foundContinuation = true
		}
	}
	if !foundContinuation {
		t.Fatalf("second request missing continuation summary: %+v", step.calls[1].Messages)
	}
	foundContinuation = false
	for _, msg := range res.NewMessages {
		if msg.Role == "tool" || len(msg.ToolCalls) > 0 {
			t.Fatalf("durable rewritten history should not retain old Inception tool chain: %+v", res.NewMessages)
		}
		if msg.Name == compact.ContextContinuationName &&
			msg.Hidden &&
			strings.Contains(msg.Content, compact.InceptionContinuationPrefix) &&
			strings.Contains(msg.Content, "Run the targeted tests.") {
			foundContinuation = true
		}
	}
	if !foundContinuation {
		t.Fatalf("durable rewritten history missing hidden Wuu context continuation: %+v", res.NewMessages)
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

	res, err := agent.RunToolLoop(context.Background(), []providers.ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "start"},
	}, agent.LoopConfig{
		Model: "m",
		Tools: kit,
		OnMessage: func(msg providers.ChatMessage) {
			visible = append(visible, msg)
		},
	}, step)
	if err != nil {
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
	var durableAnchor bool
	for _, msg := range res.NewMessages {
		if msg.Name == compact.ContextAnchorMessageName && msg.Hidden {
			durableAnchor = true
			break
		}
	}
	if !durableAnchor {
		t.Fatalf("inception anchor must be returned as hidden durable history: %+v", res.NewMessages)
	}
	for _, msg := range visible {
		if msg.Name == compact.ContextAnchorMessageName {
			t.Fatalf("hidden anchor leaked through OnMessage: %+v", visible)
		}
	}
}

func TestActiveProfileCanCallInceptionDirectlyAndRewrite(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetAgentIdentity("root", agentthread.RootPath)
	kit.SetActiveProfile(modelprofile.Resolve("openai", "gpt-5-codex"), true)

	step := &inceptionLoopStep{results: []agent.StepResult{
		{ToolCalls: []providers.ToolCall{{
			ID:        "call_inception",
			Name:      compact.InceptionToolName,
			Arguments: structuredInceptionArgs(0),
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

func TestInceptionToolDescriptionTeachesTriggers(t *testing.T) {
	desc := NewInceptionTool(&Env{AgentPath: agentthread.RootPath}).Definition().Description
	for _, want := range []string{
		"Compress the useful semantics",
		"<system>CHECKPOINT {id}</system>",
		"low-value suffix",
		"not a transcript",
		"delivered_to_user",
		"Omit raw logs",
		"does not roll back files",
	} {
		if !strings.Contains(desc, want) {
			t.Fatalf("inception description missing %q:\n%s", want, desc)
		}
	}
}

func TestInceptionToolDefinitionUsesStructuredSummary(t *testing.T) {
	def := NewInceptionTool(&Env{AgentPath: agentthread.RootPath}).Definition()
	schema := def.InputSchema
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("definition properties missing: %+v", schema)
	}
	summary, ok := properties["summary"].(map[string]any)
	if !ok {
		t.Fatalf("summary schema missing: %+v", properties)
	}
	if summary["type"] != "object" {
		t.Fatalf("summary must be a structured object, got %+v", summary)
	}
	summaryProperties, ok := summary["properties"].(map[string]any)
	if !ok {
		t.Fatalf("summary properties missing: %+v", summary)
	}
	for _, field := range []string{"delivered_to_user", "progress", "state", "next_action", "open_questions"} {
		if _, ok := summaryProperties[field]; !ok {
			t.Fatalf("structured summary missing field %q: %+v", field, summaryProperties)
		}
	}
}

func TestInceptionToolRejectsLegacyStringSummary(t *testing.T) {
	tool := NewInceptionTool(&Env{AgentPath: agentthread.RootPath})
	_, err := tool.Execute(context.Background(), `{"anchor_id":0,"summary":"legacy markdown"}`)
	if err == nil || !strings.Contains(err.Error(), "summary must be a structured object") {
		t.Fatalf("expected legacy string summary to be rejected, got %v", err)
	}
}

func TestInceptionToolRejectsEmptyStructuredSummaryState(t *testing.T) {
	tool := NewInceptionTool(&Env{AgentPath: agentthread.RootPath})
	_, err := tool.Execute(context.Background(), `{"anchor_id":0,"summary":{"delivered_to_user":"","progress":[],"state":[],"next_action":"","open_questions":[]}}`)
	if err == nil || !strings.Contains(err.Error(), "state or next_action") {
		t.Fatalf("expected empty state and next_action to be rejected, got %v", err)
	}
}

func TestInceptionToolAllowsWorkerPath(t *testing.T) {
	tool := NewInceptionTool(&Env{AgentPath: "/root/worker"})
	_, err := tool.Execute(context.Background(), structuredInceptionArgs(0))
	if err == nil || !strings.Contains(err.Error(), "parent history is unavailable") {
		t.Fatalf("expected worker path to pass agent guard and fail on missing history, got %v", err)
	}
}

func structuredInceptionArgs(anchorID int) string {
	return fmt.Sprintf(`{"anchor_id":%d,"summary":{"delivered_to_user":"Previous answer was already delivered to the user.","progress":["No files or processes were rolled back.","Focused tests remain to run."],"state":["Continue from current files.","Evidence pointer: internal/tools/tool_inception.go"],"next_action":"Run the targeted tests.","open_questions":[]}}`, anchorID)
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
