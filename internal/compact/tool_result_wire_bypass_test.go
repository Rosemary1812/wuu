package compact

import (
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/toolresult"
)

// These tests characterize the CURRENT behavior of the request-time
// tool-result prune once it reaches the real provider request projection.
//
// They intentionally document a known defect: PruneToolResults rewrites only
// ChatMessage.Content, but providers.PrepareMessagesForModelRequest later
// regenerates Content from the untouched rich ToolResult, so the placeholder
// is discarded before the request is lowered to the wire. Any change that
// makes the request-time prune actually shrink the wire request for results
// carrying a rich ToolResult MUST update these expectations — that is the
// point: they pin the observable wire behavior so a fix cannot pass silently.

const bypassSentinel = "WIRE_BYPASS_SENTINEL_GREP_MATCH_LINE"

// oversizedGrepText returns text large enough to exceed the prune protect
// threshold (toolResultPruneProtectTokens = 40_000 tokens ≈ 160_000 ASCII
// chars) so a single old grep result is pruned rather than protected.
func oversizedGrepText() string {
	line := bypassSentinel + " 0123456789 abcdefghijklmnopqrstuvwxyz\n"
	// ~55 chars/line * 4000 lines ≈ 220_000 chars ≈ 55_000 estimated tokens.
	body := strings.Repeat(line, 4000)
	if got := EstimateTokens(body); got <= toolResultPruneProtectTokens {
		panic("test fixture too small to trigger prune")
	}
	return body
}

// findToolByCallID locates the tool message for a given tool-call id after any
// projection-time reordering (observation messages, separators).
func findToolByCallID(msgs []providers.ChatMessage, callID string) (providers.ChatMessage, bool) {
	for _, m := range msgs {
		if strings.EqualFold(m.Role, "tool") && m.ToolCallID == callID {
			return m, true
		}
	}
	return providers.ChatMessage{}, false
}

func TestPruneToolResults_BypassedByRichProjection_Characterization(t *testing.T) {
	bigText := oversizedGrepText()
	bigResult := toolresult.FromText(bigText)

	messages := []providers.ChatMessage{
		{Role: "user", Content: "u1"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call-old", Name: "grep"}}},
		{Role: "tool", ToolCallID: "call-old", Name: "grep", ToolResult: &bigResult},
		{Role: "user", Content: "u2"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call-recent", Name: "grep"}}},
		{Role: "tool", ToolCallID: "call-recent", Name: "grep", Content: "small recent", ToolResult: ptrResult(toolresult.FromText("small recent"))},
		{Role: "user", Content: "u3"},
	}
	// The live-history Content of the old tool result mirrors the rich result,
	// as tool_runtime.toolResultMessage constructs it.
	messages[2].Content = bigText

	pruned := PruneToolResults(messages)

	// Step 1: the prune DOES rewrite Content in the returned slice.
	prunedOld, ok := findToolByCallID(pruned, "call-old")
	if !ok {
		t.Fatalf("old tool message missing after prune")
	}
	if !strings.HasPrefix(prunedOld.Content, "[Pruned grep result.") {
		t.Fatalf("expected pruned placeholder in Content, got prefix %q", head(prunedOld.Content, 60))
	}
	if strings.Contains(prunedOld.Content, bypassSentinel) {
		t.Fatalf("prune should have removed the sentinel from Content")
	}
	// The rich ToolResult is deliberately left intact by the prune.
	if prunedOld.ToolResult == nil || !strings.Contains(prunedOld.ToolResult.TextProjection(), bypassSentinel) {
		t.Fatalf("prune must not touch ToolResult; sentinel should remain in the rich result")
	}

	// Step 2: the real provider projection restores the full result on the
	// wire, defeating the prune. This is the documented bypass.
	prepared, err := providers.PrepareMessagesForModelRequest("gpt-5", pruned)
	if err != nil {
		t.Fatalf("PrepareMessagesForModelRequest: %v", err)
	}
	wireOld, ok := findToolByCallID(prepared, "call-old")
	if !ok {
		t.Fatalf("old tool message missing after wire preparation")
	}
	if !strings.Contains(wireOld.Content, bypassSentinel) {
		t.Fatalf("BYPASS CHARACTERIZATION FAILED: expected the full result restored on the wire, "+
			"but Content no longer contains the sentinel. If a prune fix landed, update this test. "+
			"Got prefix: %q", head(wireOld.Content, 80))
	}
	if strings.HasPrefix(wireOld.Content, "[Pruned grep result.") {
		t.Fatalf("unexpected: placeholder survived to the wire (prune fix may have landed)")
	}
}

// TestPruneToolResults_LegacyNilResult_PlaceholderReachesWire is the boundary
// case: when a tool message has no rich ToolResult (older persisted sessions),
// the projection cannot regenerate Content, so the pruned placeholder does
// survive to the wire. This isolates the bypass to messages carrying a rich
// ToolResult.
func TestPruneToolResults_LegacyNilResult_PlaceholderReachesWire(t *testing.T) {
	bigText := oversizedGrepText()

	messages := []providers.ChatMessage{
		{Role: "user", Content: "u1"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call-old", Name: "grep"}}},
		{Role: "tool", ToolCallID: "call-old", Name: "grep", Content: bigText}, // ToolResult == nil
		{Role: "user", Content: "u2"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call-recent", Name: "grep"}}},
		{Role: "tool", ToolCallID: "call-recent", Name: "grep", Content: "small recent"},
		{Role: "user", Content: "u3"},
	}

	pruned := PruneToolResults(messages)
	prepared, err := providers.PrepareMessagesForModelRequest("gpt-5", pruned)
	if err != nil {
		t.Fatalf("PrepareMessagesForModelRequest: %v", err)
	}
	wireOld, ok := findToolByCallID(prepared, "call-old")
	if !ok {
		t.Fatalf("old tool message missing after wire preparation")
	}
	if !strings.HasPrefix(wireOld.Content, "[Pruned grep result.") {
		t.Fatalf("expected pruned placeholder to survive on the wire for a nil ToolResult, got prefix %q", head(wireOld.Content, 80))
	}
	if strings.Contains(wireOld.Content, bypassSentinel) {
		t.Fatalf("nil-ToolResult message should not restore the full result")
	}
}

func ptrResult(r toolresult.Result) *toolresult.Result { return &r }

func head(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
