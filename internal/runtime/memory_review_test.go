package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/agent"
	memstore "github.com/blueberrycongee/wuu/internal/memory/store"
	"github.com/blueberrycongee/wuu/internal/providers"
)

type profileMemoryReviewFakeClient struct {
	responses []providers.ChatResponse
	requests  []providers.ChatRequest
}

func (c *profileMemoryReviewFakeClient) Chat(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	c.requests = append(c.requests, req)
	idx := len(c.requests) - 1
	if idx < len(c.responses) {
		return c.responses[idx], nil
	}
	return providers.ChatResponse{Content: "Nothing to save."}, nil
}

func TestProfileMemoryReviewScheduler_ShouldReviewHydratesFromHistory(t *testing.T) {
	provider := mustProfileMemoryProvider(t)
	scheduler := newProfileMemoryReviewScheduler(provider, 10)
	history := makeProfileMemoryReviewHistory(10)

	if !scheduler.shouldReview(history, agent.LoopResult{Content: "done"}) {
		t.Fatal("expected tenth user turn to trigger memory review")
	}
	if scheduler.shouldReview(history, agent.LoopResult{Content: "done"}) {
		t.Fatal("expected next turn in same cadence not to trigger immediately")
	}
}

func TestProfileMemoryReviewScheduler_ResetWhenTurnAlreadyWroteMemory(t *testing.T) {
	provider := mustProfileMemoryProvider(t)
	scheduler := newProfileMemoryReviewScheduler(provider, 1)
	history := makeProfileMemoryReviewHistory(1)
	result := agent.LoopResult{
		Content: "done",
		NewMessages: []providers.ChatMessage{
			{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call_1", Name: "write_memory"}}},
			{Role: "tool", Name: "write_memory", ToolCallID: "call_1", Content: `{"written":true}`},
		},
	}

	if scheduler.shouldReview(history, result) {
		t.Fatal("turn that already wrote memory should not trigger background review")
	}
	if scheduler.turnsSince != 0 {
		t.Fatalf("turnsSince = %d, want reset to 0", scheduler.turnsSince)
	}
}

func TestProfileMemoryReview_RunWritesMemoryWithMemoryOnlyTools(t *testing.T) {
	provider := mustProfileMemoryProvider(t)
	client := &profileMemoryReviewFakeClient{
		responses: []providers.ChatResponse{
			{
				ToolCalls: []providers.ToolCall{{
					ID:        "call_1",
					Name:      "write_memory",
					Arguments: `{"target":"user","content":"User prefers concise Chinese replies","source":"assistant"}`,
				}},
			},
			{Content: "Saved."},
		},
	}
	scheduler := newProfileMemoryReviewScheduler(provider, 1)
	err := scheduler.run(context.Background(), profileMemoryReviewJob{
		client:       client,
		model:        "test-model",
		systemPrompt: "# Persistent Memory",
		history: []providers.ChatMessage{
			{Role: "system", Content: "old system"},
			{Role: "user", Content: "以后都用简洁中文回复"},
			{Role: "assistant", Content: "好的。"},
		},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	entries, err := provider.Recall(context.Background(), memstore.RecallQuery{Limit: 10})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(entries) != 1 || entries[0].Content != "User prefers concise Chinese replies" {
		t.Fatalf("stored entries = %+v", entries)
	}
	if !hasProfileMemoryTag(entries[0].Tags, "target:user") {
		t.Fatalf("stored tags = %+v, want target:user", entries[0].Tags)
	}
	if len(client.requests) != 2 {
		t.Fatalf("chat calls = %d, want 2", len(client.requests))
	}
	toolNames := make(map[string]bool)
	for _, def := range client.requests[0].Tools {
		toolNames[def.Name] = true
	}
	if len(toolNames) != 2 || !toolNames["read_memory"] || !toolNames["write_memory"] {
		t.Fatalf("review tools = %+v, want only read_memory/write_memory", toolNames)
	}
	last := client.requests[0].Messages[len(client.requests[0].Messages)-1]
	if last.Role != "user" || !strings.Contains(last.Content, "Nothing to save") {
		t.Fatalf("missing review prompt in request: %+v", last)
	}
}

func TestBuildProfileMemoryReviewMessagesSkipsToolProtocolAndSyntheticUserMessages(t *testing.T) {
	messages := buildProfileMemoryReviewMessages("sys", []providers.ChatMessage{
		{Role: "system", Content: "old sys"},
		{Role: "user", Content: "[Hook context for read_file]: extra"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call_1", Name: "read_file"}}},
		{Role: "tool", Name: "read_file", ToolCallID: "call_1", Content: "file"},
		{Role: "user", Content: "Remember I prefer short answers"},
		{Role: "assistant", Content: "Noted."},
	})

	for _, msg := range messages {
		if msg.Role == "tool" {
			t.Fatalf("tool protocol message should not be included: %+v", messages)
		}
		if strings.Contains(msg.Content, "[Hook context") {
			t.Fatalf("synthetic hook context leaked into review: %+v", messages)
		}
	}
	if !strings.Contains(messages[len(messages)-1].Content, "profile memory should be updated") {
		t.Fatalf("review prompt missing: %+v", messages[len(messages)-1])
	}
}

func mustProfileMemoryProvider(t *testing.T) *memstore.FileProvider {
	t.Helper()
	provider, err := memstore.NewFileProvider(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileProvider: %v", err)
	}
	return provider
}

func makeProfileMemoryReviewHistory(userTurns int) []providers.ChatMessage {
	history := make([]providers.ChatMessage, 0, userTurns*2)
	for i := 0; i < userTurns; i++ {
		history = append(history,
			providers.ChatMessage{Role: "user", Content: "user turn"},
			providers.ChatMessage{Role: "assistant", Content: "assistant turn"},
		)
	}
	return history
}

func hasProfileMemoryTag(tags []string, want string) bool {
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}
