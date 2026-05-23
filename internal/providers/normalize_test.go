package providers

import (
	"strings"
	"testing"
)

func TestNormalizeMessages_empty(t *testing.T) {
	got := NormalizeMessages(nil)
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
	got = NormalizeMessages([]ChatMessage{})
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestValidateToolCalls_ok(t *testing.T) {
	if err := ValidateToolCalls([]ToolCall{
		{ID: "call_1", Name: "a"},
		{ID: "call_2", Name: "b"},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateToolCalls_rejectsMissingID(t *testing.T) {
	if err := ValidateToolCalls([]ToolCall{{ID: "", Name: "a"}}); err == nil {
		t.Fatal("expected missing id error")
	}
}

func TestValidateToolCalls_rejectsDuplicateID(t *testing.T) {
	if err := ValidateToolCalls([]ToolCall{
		{ID: "call_1", Name: "a"},
		{ID: "call_1", Name: "b"},
	}); err == nil {
		t.Fatal("expected duplicate id error")
	}
}

func TestNormalizeMessages_noToolCalls(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}
	got := NormalizeMessages(msgs)
	if len(got) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(got))
	}
}

func TestNormalizeMessages_orphanToolRemoved(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "call_1", Name: "read_file"}}},
		{Role: "tool", ToolCallID: "call_1", Content: "ok"},
		{Role: "tool", ToolCallID: "call_orphan", Content: "orphan"},
	}
	got := NormalizeMessages(msgs)
	if len(got) != 3 {
		t.Fatalf("expected 3 messages, got %d: %+v", len(got), roles(got))
	}
	if got[2].ToolCallID != "call_1" {
		t.Fatalf("expected tool call_1, got %s", got[2].ToolCallID)
	}
}

func TestNormalizeMessages_missingOutputInserted(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call_1", Name: "read_file"},
			{ID: "call_2", Name: "grep"},
		}},
		{Role: "tool", ToolCallID: "call_1", Content: "ok"},
	}
	got := NormalizeMessages(msgs)
	if len(got) != 4 {
		t.Fatalf("expected 4 messages, got %d: %+v", len(got), roles(got))
	}
	if got[2].ToolCallID != "call_1" {
		t.Fatalf("expected call_1 at index 2, got %s", got[2].ToolCallID)
	}
	if got[3].Role != "tool" || got[3].ToolCallID != "call_2" {
		t.Fatalf("expected synthetic tool call_2 at index 3, got %+v", got[3])
	}
	if got[3].Content != `{"error":"aborted"}` {
		t.Fatalf("expected aborted content, got %q", got[3].Content)
	}
}

func TestNormalizeMessages_multipleAssistants(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "call_a", Name: "a"}}},
		{Role: "tool", ToolCallID: "call_a", Content: "a_ok"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "call_b", Name: "b"}}},
		// missing tool for call_b
	}
	got := NormalizeMessages(msgs)
	if len(got) != 5 {
		t.Fatalf("expected 5 messages, got %d: %+v", len(got), roles(got))
	}
	if got[4].Role != "tool" || got[4].ToolCallID != "call_b" {
		t.Fatalf("expected synthetic tool call_b at index 4, got %+v", got[4])
	}
}

func TestNormalizeMessages_interleavedUserMovedAfterToolResults(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "call_1", Name: "a"}}},
		{Role: "user", Content: `{"type":"agent_result","agent_id":"worker-1","result":"done"}`},
		{Role: "tool", ToolCallID: "call_1", Content: "ok"},
	}
	got := NormalizeMessages(msgs)
	if len(got) != 4 {
		t.Fatalf("expected 4 messages, got %d: %+v", len(got), roles(got))
	}
	if got[1].Role != "assistant" || got[2].Role != "tool" || got[3].Role != "user" {
		t.Fatalf("expected assistant/tool/user ordering after repair, got %+v", got)
	}
	if got[2].ToolCallID != "call_1" || got[3].Content != `{"type":"agent_result","agent_id":"worker-1","result":"done"}` {
		t.Fatalf("unexpected repaired messages: %+v", got)
	}
}

func TestNormalizeMessages_allOutputsPresent(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call_1", Name: "a"},
			{ID: "call_2", Name: "b"},
		}},
		{Role: "tool", ToolCallID: "call_1", Content: "a"},
		{Role: "tool", ToolCallID: "call_2", Content: "b"},
	}
	got := NormalizeMessages(msgs)
	if len(got) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(got))
	}
}

func TestNormalizeMessages_noDuplicateSynthetic(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "call_1", Name: "a"}}},
		// missing tool
		{Role: "assistant", Content: "done"},
	}
	got := NormalizeMessages(msgs)
	// First assistant gets synthetic; second assistant has no calls.
	if len(got) != 4 {
		t.Fatalf("expected 4 messages, got %d: %+v", len(got), roles(got))
	}
	count := 0
	for _, m := range got {
		if m.Role == "tool" && m.ToolCallID == "call_1" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 synthetic tool for call_1, got %d", count)
	}
}

func TestNormalizeAndValidateMessages_repairsMissingOutput(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "call_1", Name: "read_file"},
		}},
	}
	got, err := NormalizeAndValidateMessages(msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected synthesized tool result, got %+v", got)
	}
	if got[2].Role != "tool" || got[2].ToolCallID != "call_1" {
		t.Fatalf("expected repaired tool result, got %+v", got[2])
	}
}

func TestNormalizeAndValidateMessages_repairsNonContiguousToolResult(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "call_1", Name: "a"}}},
		{Role: "user", Content: "mid"},
		{Role: "tool", ToolCallID: "call_1", Content: "ok"},
	}
	got, err := NormalizeAndValidateMessages(msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got[1].Role != "assistant" || got[2].Role != "tool" || got[3].Role != "user" {
		t.Fatalf("expected repaired ordering, got %+v", got)
	}
}

func TestNormalizeAndValidateMessages_asyncTimingScenarios(t *testing.T) {
	tests := []struct {
		name          string
		msgs          []ChatMessage
		want          string
		wantContentBy map[string]string
	}{
		{
			name: "parallel tool outputs finish out of order",
			msgs: []ChatMessage{
				{Role: "user", Content: "inspect"},
				{Role: "assistant", ToolCalls: []ToolCall{
					{ID: "call_1", Name: "read_file"},
					{ID: "call_2", Name: "grep"},
				}},
				{Role: "tool", ToolCallID: "call_2", Content: "grep ok"},
				{Role: "tool", ToolCallID: "call_1", Content: "read ok"},
			},
			want: "user,assistant,tool:call_1,tool:call_2",
		},
		{
			name: "worker message arrives between parallel tool outputs",
			msgs: []ChatMessage{
				{Role: "user", Content: "inspect"},
				{Role: "assistant", ToolCalls: []ToolCall{
					{ID: "call_1", Name: "read_file"},
					{ID: "call_2", Name: "grep"},
				}},
				{Role: "tool", ToolCallID: "call_1", Content: "read ok"},
				{Role: "user", Content: `{"type":"agent_result","agent_id":"worker-1","result":"done"}`},
				{Role: "tool", ToolCallID: "call_2", Content: "grep ok"},
			},
			want: "user,assistant,tool:call_1,tool:call_2,user",
		},
		{
			name: "worker message arrives before first tool output",
			msgs: []ChatMessage{
				{Role: "user", Content: "inspect"},
				{Role: "assistant", ToolCalls: []ToolCall{{ID: "call_1", Name: "spawn_agent"}}},
				{Role: "user", Content: `{"type":"agent_result","agent_id":"worker-1","result":"done"}`},
				{Role: "tool", ToolCallID: "call_1", Content: "worker ok"},
			},
			want: "user,assistant,tool:call_1,user",
		},
		{
			name: "orphan and retry outputs are dropped",
			msgs: []ChatMessage{
				{Role: "user", Content: "inspect"},
				{Role: "assistant", ToolCalls: []ToolCall{{ID: "call_1", Name: "read_file"}}},
				{Role: "tool", ToolCallID: "call_orphan", Content: "orphan"},
				{Role: "tool", ToolCallID: "call_1", Content: "first result"},
				{Role: "tool", ToolCallID: "call_1", Content: "retry result"},
				{Role: "user", Content: "after"},
			},
			want: "user,assistant,tool:call_1,user",
			wantContentBy: map[string]string{
				"call_1": "first result",
			},
		},
		{
			name: "missing earlier output is synthesized before later output",
			msgs: []ChatMessage{
				{Role: "user", Content: "inspect"},
				{Role: "assistant", ToolCalls: []ToolCall{
					{ID: "call_1", Name: "read_file"},
					{ID: "call_2", Name: "grep"},
				}},
				{Role: "tool", ToolCallID: "call_2", Content: "grep ok"},
				{Role: "user", Content: `{"type":"agent_result","agent_id":"worker-1","result":"done"}`},
			},
			want: "user,assistant,tool:call_1,tool:call_2,user",
			wantContentBy: map[string]string{
				"call_1": `{"error":"aborted"}`,
				"call_2": "grep ok",
			},
		},
		{
			name: "tool output is committed before assistant tool call",
			msgs: []ChatMessage{
				{Role: "user", Content: "inspect"},
				{Role: "tool", ToolCallID: "call_1", Content: "early output"},
				{Role: "assistant", ToolCalls: []ToolCall{{ID: "call_1", Name: "read_file"}}},
			},
			want: "user,assistant,tool:call_1",
			wantContentBy: map[string]string{
				"call_1": "early output",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeAndValidateMessages(tt.msgs)
			if err != nil {
				t.Fatalf("NormalizeAndValidateMessages: %v", err)
			}
			if err := ValidateMessageSequence(got); err != nil {
				t.Fatalf("ValidateMessageSequence: %v: %+v", err, got)
			}
			if gotOrder := roleToolOrder(got); gotOrder != tt.want {
				t.Fatalf("unexpected order: got %s want %s: %+v", gotOrder, tt.want, got)
			}
			for callID, want := range tt.wantContentBy {
				if got := toolContent(got, callID); got != want {
					t.Fatalf("tool %s content: got %q want %q", callID, got, want)
				}
			}
		})
	}
}

func TestValidateMessageSequence_ok(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "call_1", Name: "a"}}},
		{Role: "tool", ToolCallID: "call_1", Content: "ok"},
	}
	if err := ValidateMessageSequence(msgs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateMessageSequence_orphanTool(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
		{Role: "tool", ToolCallID: "call_orphan", Content: "x"},
	}
	if err := ValidateMessageSequence(msgs); err == nil {
		t.Fatalf("expected error for orphan tool")
	}
}

func TestValidateMessageSequence_toolAfterUser(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "call_1", Name: "a"}}},
		{Role: "user", Content: "mid"},
		{Role: "tool", ToolCallID: "call_1", Content: "ok"},
	}
	if err := ValidateMessageSequence(msgs); err == nil {
		t.Fatalf("expected error for tool after user")
	}
}

func TestValidateMessageSequence_rejectsAssistantToolCallMissingID(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "", Name: "a"}}},
	}
	if err := ValidateMessageSequence(msgs); err == nil {
		t.Fatal("expected error for assistant tool_call without id")
	}
}

func TestValidateMessageSequence_rejectsAssistantToolCallDuplicateIDAcrossTurns(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "call_1", Name: "a"}}},
		{Role: "tool", ToolCallID: "call_1", Content: "ok"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "call_1", Name: "b"}}},
	}
	if err := ValidateMessageSequence(msgs); err == nil {
		t.Fatal("expected error for duplicate tool_call id across turns")
	}
}

func roles(msgs []ChatMessage) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Role
	}
	return out
}

func roleToolOrder(msgs []ChatMessage) string {
	out := make([]string, 0, len(msgs))
	for _, msg := range msgs {
		if msg.Role == "tool" {
			out = append(out, "tool:"+msg.ToolCallID)
			continue
		}
		out = append(out, msg.Role)
	}
	return strings.Join(out, ",")
}

func toolContent(msgs []ChatMessage, callID string) string {
	for _, msg := range msgs {
		if msg.Role == "tool" && msg.ToolCallID == callID {
			return msg.Content
		}
	}
	return ""
}
