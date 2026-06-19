package providers

import "testing"

func TestApplyModelMessageCompatibilitySanitizesSurrogatesWithoutMutatingInput(t *testing.T) {
	highSurrogate := string([]rune{0xD800})
	lowSurrogate := string([]rune{0xDFFF})
	msgs := []ChatMessage{
		{
			Role:             "assistant",
			Content:          "bad " + highSurrogate + " text",
			ReasoningContent: "think " + lowSurrogate,
			ReasoningBlocks:  []ReasoningBlock{{Type: "thinking", Thinking: "block " + highSurrogate}},
		},
	}

	got := ApplyModelMessageCompatibility("gpt-test", msgs)
	if got[0].Content != "bad \uFFFD text" {
		t.Fatalf("content = %q", got[0].Content)
	}
	if got[0].ReasoningContent != "think \uFFFD" {
		t.Fatalf("reasoning content = %q", got[0].ReasoningContent)
	}
	if got[0].ReasoningBlocks[0].Thinking != "block \uFFFD" {
		t.Fatalf("reasoning block = %q", got[0].ReasoningBlocks[0].Thinking)
	}
	if msgs[0].Content != "bad "+highSurrogate+" text" {
		t.Fatalf("input mutated: %+v", msgs[0])
	}
}

func TestPrepareMessagesForModelRequestScrubsClaudeToolCallIDs(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "call.1/2", Name: "read"}}},
		{Role: "tool", ToolCallID: "call.1/2", Content: "ok"},
	}

	got, err := PrepareMessagesForModelRequest("claude-opus-4.7", msgs)
	if err != nil {
		t.Fatalf("PrepareMessagesForModelRequest: %v", err)
	}
	if got[1].ToolCalls[0].ID != "call_1_2" || got[2].ToolCallID != "call_1_2" {
		t.Fatalf("tool IDs not scrubbed: %+v", got)
	}
}

func TestPrepareMessagesForModelRequestScrubsMistralIDsAndSeparatesToolThenUser(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "call_123456789_extra", Name: "read"}}},
		{Role: "tool", ToolCallID: "call_123456789_extra", Content: "ok"},
		{Role: "user", Content: "next"},
	}

	got, err := PrepareMessagesForModelRequest("mistral-large-latest", msgs)
	if err != nil {
		t.Fatalf("PrepareMessagesForModelRequest: %v", err)
	}
	if got[1].ToolCalls[0].ID != "call12345" || got[2].ToolCallID != "call12345" {
		t.Fatalf("tool IDs not scrubbed: %+v", got)
	}
	if len(got) != 5 || got[3].Role != "assistant" || got[3].Content != "Done." || got[4].Role != "user" {
		t.Fatalf("expected assistant separator before user, got %+v", got)
	}
}
