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

func TestPrepareMessagesForModelRequestAllowsDuplicateToolCallIDsAcrossTurns(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "call_1", Name: "read"}}},
		{Role: "tool", ToolCallID: "call_1", Content: "first"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "call_1", Name: "read"}}},
		{Role: "tool", ToolCallID: "call_1", Content: "second"},
	}

	for _, model := range []string{"gpt-test", "claude-opus-4.7"} {
		t.Run(model, func(t *testing.T) {
			got, err := PrepareMessagesForModelRequest(model, msgs)
			if err != nil {
				t.Fatalf("PrepareMessagesForModelRequest: %v", err)
			}
			if len(got) != len(msgs) {
				t.Fatalf("expected %d messages, got %d: %+v", len(msgs), len(got), got)
			}
			if got[2].Content != "first" || got[4].Content != "second" {
				t.Fatalf("tool results were not preserved in turn order: %+v", got)
			}
		})
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
	scrubbed := got[1].ToolCalls[0].ID
	if len(scrubbed) != 9 || !isMistralToolCallID(scrubbed) {
		t.Fatalf("scrubbed ID %q is not 9 alphanumerics", scrubbed)
	}
	if got[2].ToolCallID != scrubbed {
		t.Fatalf("tool result ID %q does not match call ID %q", got[2].ToolCallID, scrubbed)
	}
	if len(got) != 5 || got[3].Role != "assistant" || got[3].Content != "Done." || got[4].Role != "user" {
		t.Fatalf("expected assistant separator before user, got %+v", got)
	}
}

func TestScrubMistralToolCallIDAvoidsPrefixCollisions(t *testing.T) {
	// Moonshot/Kimi-style IDs share their first nine alphanumerics; prefix
	// truncation collapsed them into duplicates that failed tool-call history
	// validation on every retry.
	a := scrubMistralToolCallID("functions.read_file:0")
	b := scrubMistralToolCallID("functions.read_file:1")
	if a == b {
		t.Fatalf("distinct IDs collapsed to %q", a)
	}
	for _, id := range []string{a, b} {
		if len(id) != 9 || !isMistralToolCallID(id) {
			t.Fatalf("scrubbed ID %q is not 9 alphanumerics", id)
		}
	}
	// Deterministic: the same history scrubs to the same IDs on every request.
	if scrubMistralToolCallID("functions.read_file:0") != a {
		t.Fatal("scrub is not deterministic")
	}
	// Already-valid IDs pass through untouched.
	if scrubMistralToolCallID("abc123XYZ") != "abc123XYZ" {
		t.Fatal("valid 9-char ID should pass through")
	}
}
