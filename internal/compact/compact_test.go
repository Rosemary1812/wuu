package compact

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestEstimateTokens_English(t *testing.T) {
	// ~4 chars per token for English text.
	text := "Hello world, this is a test sentence for token estimation."
	tokens := EstimateTokens(text)
	// 58 chars / 4 = 14, +1 = 15
	if tokens < 10 || tokens > 25 {
		t.Fatalf("English token estimate out of range: got %d for %d chars", tokens, len(text))
	}
}

func TestEstimateTokens_CJK(t *testing.T) {
	// ~2 chars per token for CJK text.
	text := "你好世界这是一个测试"
	tokens := EstimateTokens(text)
	// 10 CJK chars / 2 = 5, +1 = 6
	if tokens < 4 || tokens > 10 {
		t.Fatalf("CJK token estimate out of range: got %d for %q", tokens, text)
	}
}

func TestEstimateTokens_Mixed(t *testing.T) {
	text := "Hello 你好 world 世界"
	tokens := EstimateTokens(text)
	// Should be somewhere between pure English and pure CJK estimates.
	if tokens < 3 || tokens > 15 {
		t.Fatalf("mixed token estimate out of range: got %d", tokens)
	}
}

func TestEstimateTokens_Empty(t *testing.T) {
	if got := EstimateTokens(""); got != 0 {
		t.Fatalf("expected 0 for empty string, got %d", got)
	}
}

// TestEstimateTokens_InvalidUTF8 pins down behavior on malformed bytes so
// the single-pass implementation stays consistent with the previous
// two-pass one (which relied on utf8.RuneCountInString). A for-range loop
// yields one RuneError per invalid sequence, same as RuneCountInString.
func TestEstimateTokens_InvalidUTF8(t *testing.T) {
	// Two ASCII runes around one invalid byte: 'a' + 0xFF + 'b'.
	// RuneError is non-CJK, so total=3 runes, cjk=0, tokens = 3/4 + 1 = 1.
	text := "a\xffb"
	if got, want := EstimateTokens(text), 1; got != want {
		t.Fatalf("invalid utf8 tokens: got %d, want %d", got, want)
	}
}

func TestShouldCompact_UnderThreshold(t *testing.T) {
	messages := []providers.ChatMessage{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	}
	// With a large max context, should not compact.
	if ShouldCompact(messages, 100000) {
		t.Fatal("expected ShouldCompact=false for small messages with large context")
	}
}

func TestShouldCompact_OverThreshold(t *testing.T) {
	// Create messages that exceed 80% of a small threshold.
	messages := []providers.ChatMessage{
		{Role: "user", Content: "This is a fairly long message that should push us over the threshold when the max context is small."},
		{Role: "assistant", Content: "This is another fairly long response that adds more tokens to the conversation history."},
	}
	// With a very small max context (e.g., 10 tokens), should compact.
	if !ShouldCompact(messages, 10) {
		t.Fatal("expected ShouldCompact=true for large messages with small context")
	}
}

func TestShouldCompact_ZeroThreshold(t *testing.T) {
	messages := []providers.ChatMessage{{Role: "user", Content: "hi"}}
	if ShouldCompact(messages, 0) {
		t.Fatal("expected ShouldCompact=false for zero threshold")
	}
}

type mockCompactClient struct {
	response    string
	lastRequest providers.ChatRequest
}

func (m *mockCompactClient) Chat(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	m.lastRequest = req
	return providers.ChatResponse{Content: m.response}, nil
}

// flakyOverflowClient returns context-overflow on the first N calls
// then a real summary. Used to exercise Compact's defensive trimming.
type flakyOverflowClient struct {
	failsRemaining int
	finalSummary   string
	calls          int
}

func (f *flakyOverflowClient) Chat(_ context.Context, _ providers.ChatRequest) (providers.ChatResponse, error) {
	f.calls++
	if f.failsRemaining > 0 {
		f.failsRemaining--
		return providers.ChatResponse{}, &providers.HTTPError{
			StatusCode:      400,
			Body:            "context_length_exceeded",
			ContextOverflow: true,
		}
	}
	return providers.ChatResponse{Content: f.finalSummary}, nil
}

func TestCompactInstructionPrompt_EnforcesNoToolsAndFormat(t *testing.T) {
	for _, want := range []string{
		"ONLY context available when the conversation resumes",
		"Do NOT call any tools",
		"Do NOT use read_file, grep, glob, run_shell",
		"<analysis>",
		"<summary>",
		"exactly two top-level blocks",
	} {
		if !strings.Contains(compactInstructionPrompt, want) {
			t.Errorf("compactInstructionPrompt missing %q", want)
		}
	}
}

func TestCompactInstructionPrompt_CoversHandoffSections(t *testing.T) {
	for _, want := range []string{
		"## User Intent",
		"## Technical Concepts",
		"## Files and Code",
		"## Errors and Fixes",
		"## All User Messages",
		"## Unfinished Work",
		"## Current Work",
		"## Next Step",
	} {
		if !strings.Contains(compactInstructionPrompt, want) {
			t.Errorf("compactInstructionPrompt missing section %q", want)
		}
	}
}

func TestFormatSummary_StripsAnalysisAndExtractsSummary(t *testing.T) {
	raw := "<analysis>\nprivate reasoning\n</analysis>\n\n<summary>\n## Current Work\nContinue implementation.\n</summary>"
	got := FormatSummary(raw)
	if strings.Contains(got, "private reasoning") || strings.Contains(got, "<analysis>") || strings.Contains(got, "<summary>") {
		t.Fatalf("expected only cleaned summary, got %q", got)
	}
	if got != "## Current Work\nContinue implementation." {
		t.Fatalf("unexpected cleaned summary: %q", got)
	}
}

func TestBuildSummaryContent_UsesStableConversationSummaryPrefix(t *testing.T) {
	content := BuildSummaryContent("Older turns were compacted.")
	if !IsConversationSummaryContent(content) {
		t.Fatalf("expected compact summary content, got %q", content)
	}
	if !strings.Contains(content, "This session is being continued") {
		t.Fatalf("expected continuation handoff text, got %q", content)
	}
	if !strings.Contains(content, "Summary:\nOlder turns were compacted.") {
		t.Fatalf("expected formatted summary body, got %q", content)
	}
}

func TestCompact_DefensiveTrimOnOverflow(t *testing.T) {
	// 8 messages, summary request overflows twice then succeeds.
	// The final compact result should still contain the summary +
	// a recent raw tail.
	large := strings.Repeat("x", 120000)
	messages := []providers.ChatMessage{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "first reply"},
		{Role: "user", Content: "second"},
		{Role: "assistant", Content: "second reply"},
		{Role: "user", Content: "third"},
		{Role: "assistant", Content: large},
		{Role: "user", Content: "fourth"},
		{Role: "assistant", Content: "fourth reply"},
	}

	client := &flakyOverflowClient{
		failsRemaining: 2,
		finalSummary:   "summary of older turns",
	}
	result, err := Compact(context.Background(), messages, client, "gpt-4")
	if err != nil {
		t.Fatalf("Compact returned error: %v", err)
	}
	if client.calls != 3 {
		t.Fatalf("expected 3 client calls (2 fails + 1 success), got %d", client.calls)
	}
	if len(result) < 3 {
		t.Fatalf("expected summary + kept tail messages, got %d", len(result))
	}
	if result[0].Role != "system" {
		t.Fatalf("expected system summary first, got %s", result[0].Role)
	}
}

func TestCompact_LongSingleUserTurnFallsBackToRecentTail(t *testing.T) {
	largeToolOutput := strings.Repeat("tool output ", 1000)
	messages := []providers.ChatMessage{
		{Role: "user", Content: "debug the failing workbench request"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{
			{ID: "call_1", Name: "run_shell", Arguments: `{"command":"rg failing"}`},
		}},
		{Role: "tool", Name: "run_shell", ToolCallID: "call_1", Content: largeToolOutput},
		{Role: "assistant", Content: "I found the first clue."},
		{Role: "assistant", ToolCalls: []providers.ToolCall{
			{ID: "call_2", Name: "run_shell", Arguments: `{"command":"sed -n 1,200p file.go"}`},
		}},
		{Role: "tool", Name: "run_shell", ToolCallID: "call_2", Content: largeToolOutput},
		{Role: "assistant", Content: "I will keep checking the runtime path."},
	}

	client := &mockCompactClient{response: "single-turn investigation summary"}
	result, err := CompactWithContextWindow(context.Background(), messages, client, "test-model", 1000)
	if err != nil {
		t.Fatalf("CompactWithContextWindow: %v", err)
	}
	if len(result) >= len(messages) {
		t.Fatalf("expected single-user tool run to compact, got %d messages from %d", len(result), len(messages))
	}
	if result[0].Role != "system" || !IsConversationSummaryContent(result[0].Content) {
		t.Fatalf("expected compact summary first, got %#v", result[0])
	}
	if err := providers.ValidateMessageSequence(result); err != nil {
		t.Fatalf("compacted history has invalid tool sequence: %v\n%#v", err, result)
	}
}

func TestCompact_PreservesLeadingSystemPrompt(t *testing.T) {
	messages := []providers.ChatMessage{
		{Role: "system", Content: "You are wuu."},
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "first reply"},
		{Role: "user", Content: "second"},
		{Role: "assistant", Content: "second reply"},
		{Role: "user", Content: "third"},
		{Role: "assistant", Content: "third reply"},
		{Role: "user", Content: "fourth"},
		{Role: "assistant", Content: "fourth reply"},
	}

	client := &mockCompactClient{response: "<analysis>draft</analysis><summary>summary of older turns</summary>"}
	result, err := Compact(context.Background(), messages, client, "test")
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if len(result) < 6 {
		t.Fatalf("expected system prompt + summary + kept tail messages, got %d", len(result))
	}
	if result[0].Role != "system" || result[0].Content != "You are wuu." {
		t.Fatalf("expected original system prompt preserved first, got %#v", result[0])
	}
	if result[1].Role != "system" || !IsConversationSummaryContent(result[1].Content) {
		t.Fatalf("expected compact summary after system prompt, got %#v", result[1])
	}
	if strings.Contains(result[1].Content, "draft") || strings.Contains(result[1].Content, "<analysis>") {
		t.Fatalf("analysis leaked into compact summary: %q", result[1].Content)
	}
}

func TestCompact_DefensiveTrimGivesUpAfterMaxRetries(t *testing.T) {
	// Always overflows. Compact should bail after maxCompactRetries
	// attempts and propagate the error to the caller.
	large := strings.Repeat("x", 120000)
	messages := []providers.ChatMessage{
		{Role: "user", Content: "a"},
		{Role: "assistant", Content: "b"},
		{Role: "user", Content: "c"},
		{Role: "assistant", Content: "d"},
		{Role: "user", Content: "e"},
		{Role: "assistant", Content: large},
		{Role: "user", Content: "g"},
		{Role: "assistant", Content: "h"},
	}
	client := &flakyOverflowClient{failsRemaining: 100} // never succeeds

	_, err := Compact(context.Background(), messages, client, "gpt-4")
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if client.calls != maxCompactRetries+1 {
		t.Fatalf("expected %d attempts, got %d", maxCompactRetries+1, client.calls)
	}
}

func TestCompact_IncludesToolCallsInSummary(t *testing.T) {
	messages := []providers.ChatMessage{
		{Role: "user", Content: "Read main.go"},
		{Role: "assistant", Content: "Sure.", ToolCalls: []providers.ToolCall{
			{ID: "c1", Name: "read_file", Arguments: `{"path":"main.go"}`},
		}},
		{Role: "tool", Name: "read_file", ToolCallID: "c1", Content: "package main"},
		{Role: "assistant", Content: "Here is main.go content."},
		{Role: "user", Content: "Now fix the bug."},
		{Role: "assistant", Content: "Fixed."},
		{Role: "user", Content: "Thanks."},
		{Role: "assistant", Content: "You're welcome."},
	}

	client := &mockCompactClient{response: "User asked to read main.go, assistant used read_file tool, then fixed a bug."}
	result, err := Compact(context.Background(), messages, client, "test")
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if len(result) >= len(messages) {
		t.Fatalf("expected compacted result to be shorter, got %d vs %d", len(result), len(messages))
	}
	if result[0].Role != "system" {
		t.Fatalf("expected system summary, got %s", result[0].Role)
	}
}

func TestCompact_DoesNotLeaveDanglingToolResults(t *testing.T) {
	messages := []providers.ChatMessage{
		{Role: "user", Content: "older question"},
		{Role: "assistant", Content: "older answer"},
		{Role: "user", Content: "run both reads"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{
			{ID: "c1", Name: "read_file", Arguments: `{"path":"README.md"}`},
			{ID: "c2", Name: "read_file", Arguments: `{"path":"README_zh.md"}`},
		}},
		{Role: "tool", Name: "read_file", ToolCallID: "c1", Content: "english"},
		{Role: "tool", Name: "read_file", ToolCallID: "c2", Content: "chinese"},
		{Role: "assistant", Content: "done"},
	}

	client := &mockCompactClient{response: "summary of older turns"}
	result, err := Compact(context.Background(), messages, client, "test")
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if err := providers.ValidateMessageSequence(result); err != nil {
		t.Fatalf("compacted history has invalid tool sequence: %v\n%#v", err, result)
	}
	if len(result) < 6 {
		t.Fatalf("expected summary + intact current tool turn, got %d messages", len(result))
	}
	if result[2].Role != "assistant" || len(result[2].ToolCalls) != 2 {
		t.Fatalf("expected assistant tool_call turn preserved, got %+v", result[2])
	}
	if result[3].Role != "tool" || result[4].Role != "tool" {
		t.Fatalf("expected tool results preserved after assistant tool_call, got %+v %+v", result[3], result[4])
	}
}

func TestCompactKeepStart_UsesTokenBudgetForRecentUserTurns(t *testing.T) {
	large := strings.Repeat("x", 2000)
	messages := []providers.ChatMessage{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "first reply"},
		{Role: "user", Content: "second"},
		{Role: "assistant", Content: large},
		{Role: "user", Content: "third"},
		{Role: "assistant", Content: "third reply"},
	}

	start := compactKeepStart(messages, 100)
	if start != 4 {
		t.Fatalf("expected only the latest user turn to fit budget, start=%d", start)
	}
}

func TestCompactKeepStart_ExpandsRecentTurnsWithinBudget(t *testing.T) {
	messages := []providers.ChatMessage{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "first reply"},
		{Role: "user", Content: "second"},
		{Role: "assistant", Content: "second reply"},
		{Role: "user", Content: "third"},
		{Role: "assistant", Content: "third reply"},
	}

	start := compactKeepStart(messages, 1000)
	if start != 2 {
		t.Fatalf("expected budget to keep recent turns while leaving oldest summarized, start=%d", start)
	}
}

type ctxAwareCompactClient struct{}

func (c *ctxAwareCompactClient) Chat(ctx context.Context, _ providers.ChatRequest) (providers.ChatResponse, error) {
	<-ctx.Done()
	return providers.ChatResponse{}, ctx.Err()
}

func TestCompact_UsesInternalTimeout(t *testing.T) {
	t.Setenv("WUU_COMPACT_TIMEOUT_MS", "20")

	messages := []providers.ChatMessage{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "first reply"},
		{Role: "user", Content: "second"},
		{Role: "assistant", Content: "second reply"},
		{Role: "user", Content: "third"},
		{Role: "assistant", Content: "third reply"},
	}

	start := time.Now()
	_, err := Compact(context.Background(), messages, &ctxAwareCompactClient{}, "test")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Fatalf("expected internal compact timeout to stop quickly, took %s", elapsed)
	}
}

func TestCompact_PrunesOldLargeToolResultsBeforeSummary(t *testing.T) {
	large := strings.Repeat("x", toolResultPruneThresholdChars+50)
	messages := []providers.ChatMessage{
		{Role: "user", Content: "inspect logs"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "c1", Name: "read_file", Arguments: `{"path":"build.log"}`}}},
		{Role: "tool", Name: "read_file", ToolCallID: "c1", Content: large},
		{Role: "assistant", Content: "I checked the log."},
		{Role: "user", Content: "continue"},
		{Role: "assistant", Content: "working"},
		{Role: "user", Content: "status?"},
		{Role: "assistant", Content: "done"},
	}

	client := &mockCompactClient{response: "summary"}
	_, err := Compact(context.Background(), messages, client, "test")
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}

	if len(client.lastRequest.Messages) < 2 || client.lastRequest.Messages[0].Role != "system" {
		t.Fatalf("expected compact summary request to include system instructions, got %+v", client.lastRequest.Messages)
	}
	summaryInput := client.lastRequest.Messages[len(client.lastRequest.Messages)-1].Content
	if strings.Contains(summaryInput, large) {
		t.Fatal("expected old large tool result to be pruned from summary input")
	}
	if !strings.Contains(summaryInput, "[Old read_file result omitted during compact to save context.") {
		t.Fatalf("expected placeholder in summary input, got: %s", summaryInput)
	}
}

func TestCompact_DoesNotPruneRecentTailToolResults(t *testing.T) {
	large := strings.Repeat("y", toolResultPruneThresholdChars+50)
	messages := []providers.ChatMessage{
		{Role: "user", Content: "older question"},
		{Role: "assistant", Content: "older answer"},
		{Role: "user", Content: "run tool"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "c2", Name: "read_file", Arguments: `{"path":"recent.log"}`}}},
		{Role: "tool", Name: "read_file", ToolCallID: "c2", Content: large},
		{Role: "assistant", Content: "recent analysis"},
		{Role: "user", Content: "what changed?"},
		{Role: "assistant", Content: "here's the update"},
	}

	client := &mockCompactClient{response: "summary"}
	result, err := Compact(context.Background(), messages, client, "test")
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}

	if len(result) < 4 {
		t.Fatalf("expected compacted tail to be preserved, got %d messages", len(result))
	}
	found := false
	for _, msg := range result {
		if msg.Role == "tool" && msg.ToolCallID == "c2" {
			found = true
			if msg.Content != large {
				t.Fatal("expected recent tail tool result to remain unchanged")
			}
		}
	}
	if !found {
		t.Fatal("expected recent tail tool result to be preserved in compacted output")
	}
}

func TestPruneOldToolResults_PreservesToolCallPairing(t *testing.T) {
	large := strings.Repeat("z", toolResultPruneThresholdChars+50)
	messages := []providers.ChatMessage{
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "c1", Name: "grep", Arguments: `{"pattern":"TODO"}`}}},
		{Role: "tool", Name: "grep", ToolCallID: "c1", Content: large},
		{Role: "assistant", Content: "done"},
	}

	pruned := pruneOldToolResults(messages)
	if len(pruned) != len(messages) {
		t.Fatalf("expected message count to stay the same, got %d vs %d", len(pruned), len(messages))
	}
	if len(pruned[0].ToolCalls) != 1 || pruned[0].ToolCalls[0].ID != "c1" {
		t.Fatalf("expected assistant tool call metadata preserved, got %+v", pruned[0].ToolCalls)
	}
	if pruned[1].Role != "tool" || pruned[1].ToolCallID != "c1" {
		t.Fatalf("expected tool pairing preserved, got %+v", pruned[1])
	}
	if pruned[1].Content == "" || strings.Contains(pruned[1].Content, large) {
		t.Fatalf("expected tool result content replaced with placeholder, got %q", pruned[1].Content)
	}
}
