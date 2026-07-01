package appserver

import (
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestForkHistoryAtToolCallTargetKeepsWholeToolBatch(t *testing.T) {
	threadID := "thread-tool-batch"
	history := []providers.ChatMessage{
		{Role: "user", Content: "check alignment"},
		{
			Role:    "assistant",
			Content: "I will inspect both files.",
			Phase:   providers.MessagePhaseCommentary,
			ToolCalls: []providers.ToolCall{
				{ID: "call_read", Name: "read_file", Arguments: `{"path":"desktop/src/renderer/styles/sidebar.css"}`},
				{ID: "call_grep", Name: "bash", Arguments: `{"command":"grep -n composer desktop/src/renderer/styles/composer.css"}`},
			},
		},
		{Role: "tool", ToolCallID: "call_read", Name: "read_file", Content: "sidebar css"},
		{Role: "tool", ToolCallID: "call_grep", Name: "bash", Content: "composer css"},
		{
			Role:    "assistant",
			Content: "Use the same bottom padding.",
			Phase:   providers.MessagePhaseFinalAnswer,
		},
	}
	turns := turnsFromHistory(threadID, history, time.Unix(0, 0).UTC())
	if len(turns) != 1 {
		t.Fatalf("expected one turn, got %+v", turns)
	}
	targetItem := itemBySourceIDForForkTest(t, turns[0], "call_read")

	forked, err := forkHistoryAtTarget(history, threadID, turns, turns[0].ID, targetItem.ID)
	if err != nil {
		t.Fatalf("forkHistoryAtTarget returned error: %v", err)
	}

	visible := visibleMessagesForTest(forked)
	if len(visible) != 4 {
		t.Fatalf("expected fork to include user, assistant, and both tool results, got %+v", visible)
	}
	if visible[1].Role != "assistant" || len(visible[1].ToolCalls) != 2 {
		t.Fatalf("expected assistant tool-call batch, got %+v", visible[1])
	}
	if visible[2].Role != "tool" || visible[2].ToolCallID != "call_read" {
		t.Fatalf("expected first tool result, got %+v", visible[2])
	}
	if visible[3].Role != "tool" || visible[3].ToolCallID != "call_grep" {
		t.Fatalf("expected second tool result, got %+v", visible[3])
	}
}

func itemBySourceIDForForkTest(t *testing.T, turn Turn, sourceID string) ThreadItem {
	t.Helper()
	for _, item := range turn.Items {
		if item.SourceID == sourceID {
			return item
		}
	}
	t.Fatalf("item with source id %q not found in turn %+v", sourceID, turn)
	return ThreadItem{}
}
