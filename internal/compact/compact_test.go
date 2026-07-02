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

type chunkRecordingCompactClient struct {
	prompts []string
}

func (c *chunkRecordingCompactClient) Chat(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	if len(req.Messages) >= 2 {
		c.prompts = append(c.prompts, req.Messages[1].Content)
	}
	return providers.ChatResponse{Content: "summary chunk"}, nil
}

type partialStreamCompactClient struct {
	events      []providers.StreamEvent
	lastRequest providers.ChatRequest
}

func (p *partialStreamCompactClient) Chat(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	p.lastRequest = req
	return providers.ChatResponse{Content: "fallback summary"}, nil
}

func (p *partialStreamCompactClient) StreamChat(ctx context.Context, req providers.ChatRequest) (<-chan providers.StreamEvent, error) {
	p.lastRequest = req
	ch := make(chan providers.StreamEvent, len(p.events))
	go func() {
		defer close(ch)
		for _, ev := range p.events {
			select {
			case <-ctx.Done():
				return
			case ch <- ev:
			}
		}
	}()
	return ch, nil
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
		"used to resume after older messages are removed",
		"without asking the user to repeat context",
		"Do not call tools",
		"request tool use",
		"markdown summary only",
		"Do not include an analysis block",
	} {
		if !strings.Contains(compactInstructionPrompt, want) {
			t.Errorf("compactInstructionPrompt missing %q", want)
		}
	}
	for _, banned := range []string{"read_file", "grep", "glob", "bash", "run_shell", "run_test", "start_process"} {
		if strings.Contains(compactInstructionPrompt, banned) {
			t.Fatalf("compactInstructionPrompt must not name unavailable tool path %q:\n%s", banned, compactInstructionPrompt)
		}
	}
	if strings.Contains(compactInstructionPrompt, "ONLY context available when the conversation resumes") {
		t.Fatal("compactInstructionPrompt should avoid overly dramatic context wording")
	}
	if strings.Contains(compactInstructionPrompt, "<analysis>") {
		t.Fatal("compact prompt should not ask for an analysis block")
	}
}

func TestCompactInstructionPrompt_CoversHandoffSections(t *testing.T) {
	for _, want := range []string{
		"## Goal",
		"## Constraints & Preferences",
		"## Progress",
		"## External State",
		"## Verification State",
		"## Key Decisions",
		"## Next Steps",
		"## Critical Context",
		"## Evidence Pointers",
		"## Relevant Files",
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
	if got := summaryBodyFromContent(content); got != "Older turns were compacted." {
		t.Fatalf("expected summary body extraction, got %q", got)
	}
}

func TestCompact_PreservesDiscoveredToolsOnSummaryMessage(t *testing.T) {
	messages := []providers.ChatMessage{
		{Role: "user", Content: "find docs"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{
			ID:        "search_1",
			Name:      "tool_search",
			Kind:      providers.ToolCallKindToolSearch,
			Arguments: `{"query":"docs"}`,
		}}},
		{
			Role:           "tool",
			Name:           "tool_search",
			ToolCallID:     "search_1",
			ToolResultKind: providers.ToolCallKindToolSearch,
			Content:        `{"loadable_tools":[{"type":"function","name":"mcp_docs_search","description":"Search docs","input_schema":{"type":"object","properties":{"query":{"type":"string"}}}}]}`,
		},
		{Role: "assistant", Content: "Use mcp_docs_search for docs."},
		{Role: "user", Content: "now inspect the config"},
		{Role: "assistant", Content: "config inspected"},
		{Role: "user", Content: "what remains?"},
		{Role: "assistant", Content: "one follow-up remains"},
	}

	client := &mockCompactClient{response: "docs search tool was discovered"}
	result, err := Compact(context.Background(), messages, client, "test")
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if len(result) < 3 || result[0].Role != "system" {
		t.Fatalf("expected compacted summary plus recent tail, got %+v", result)
	}
	tools := result[0].DiscoveredTools
	if len(tools) != 1 || tools[0].Name != "mcp_docs_search" || tools[0].InputSchema["type"] != "object" {
		t.Fatalf("expected discovered tool metadata on compact summary, got %+v", tools)
	}
	if strings.Contains(result[0].Content, "input_schema") || strings.Contains(result[0].Content, "mcp_docs_search") {
		t.Fatalf("summary text should not embed tool schema metadata, got %q", result[0].Content)
	}
	for _, msg := range result[1:] {
		if msg.Role == "tool" && msg.ToolCallID == "search_1" {
			t.Fatalf("old tool_search result should have been summarized, got tail %+v", result)
		}
	}
}

func TestCompact_CarriesPreviousSummaryDiscoveredTools(t *testing.T) {
	messages := []providers.ChatMessage{
		{
			Role:    "system",
			Content: BuildSummaryContent("older summary"),
			DiscoveredTools: []providers.LoadableToolDefinition{{
				Type:        "function",
				Name:        "mcp_docs_search",
				InputSchema: map[string]any{"type": "object"},
			}},
		},
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "first reply"},
		{Role: "user", Content: "second"},
		{Role: "assistant", Content: "second reply"},
	}

	client := &mockCompactClient{response: "new summary"}
	result, err := Compact(context.Background(), messages, client, "test")
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if len(result) != 1 || result[0].Role != "system" {
		t.Fatalf("expected compacted summary, got %+v", result)
	}
	if tools := result[0].DiscoveredTools; len(tools) != 1 || tools[0].Name != "mcp_docs_search" {
		t.Fatalf("expected previous summary discovered tools to carry forward, got %+v", tools)
	}
}

func TestCompact_SetsSummaryOutputControls(t *testing.T) {
	messages := []providers.ChatMessage{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "first reply"},
		{Role: "user", Content: "second"},
		{Role: "assistant", Content: "second reply"},
		{Role: "user", Content: "third"},
		{Role: "assistant", Content: "third reply"},
	}

	client := &mockCompactClient{response: "summary of older turns"}
	_, err := Compact(context.Background(), messages, client, "gpt-5.5")
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if client.lastRequest.MaxTokens != compactSummaryMaxTokens {
		t.Fatalf("expected compact MaxTokens=%d, got %d", compactSummaryMaxTokens, client.lastRequest.MaxTokens)
	}
	if got := client.lastRequest.ProviderOptions["textVerbosity"]; got != "low" {
		t.Fatalf("expected low text verbosity, got %#v", got)
	}
}

func TestCompact_UsesPartialStreamSummaryWhenDoneIsMissing(t *testing.T) {
	partial := strings.Repeat("usable compact summary detail ", 12)
	messages := []providers.ChatMessage{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "first reply"},
		{Role: "user", Content: "second"},
		{Role: "assistant", Content: "second reply"},
		{Role: "user", Content: "third"},
		{Role: "assistant", Content: "third reply"},
	}
	client := &partialStreamCompactClient{events: []providers.StreamEvent{
		{Type: providers.EventContentDelta, Content: partial},
	}}

	result, err := Compact(context.Background(), messages, client, "gpt-5.5")
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if len(result) == 0 || !IsConversationSummaryContent(result[0].Content) {
		t.Fatalf("expected compacted summary first, got %#v", result)
	}
	if !strings.Contains(result[0].Content, strings.TrimSpace(partial)) {
		t.Fatalf("expected partial summary to be used, got %q", result[0].Content)
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

func TestCompact_ChunksSummaryInputForHugeHistory(t *testing.T) {
	var messages []providers.ChatMessage
	for i := 0; i < 70; i++ {
		messages = append(messages,
			providers.ChatMessage{Role: "user", Content: "investigate long running issue " + strings.Repeat("u", 900)},
			providers.ChatMessage{Role: "assistant", Content: "analysis result " + strings.Repeat("a", 900)},
		)
	}

	client := &chunkRecordingCompactClient{}
	budget := Budget{ContextTokens: 20_000, OutputReserveTokens: 4_000, KeepRecentTokens: 2_000}
	result, err := CompactWithBudget(context.Background(), messages, client, "gpt-4", budget)
	if err != nil {
		t.Fatalf("CompactWithBudget: %v", err)
	}
	if len(client.prompts) < 2 {
		t.Fatalf("expected huge summary input to be chunked, got %d compact request(s)", len(client.prompts))
	}
	inputBudget := compactSummaryInputBudgetForBudget("gpt-4", budget)
	for i, prompt := range client.prompts {
		if got := EstimateTokens(prompt); got > inputBudget {
			t.Fatalf("chunk %d prompt estimate = %d, want <= %d", i, got, inputBudget)
		}
	}
	if !strings.Contains(client.prompts[1], "Previous anchored summary") || !strings.Contains(client.prompts[1], "summary chunk") {
		t.Fatalf("second chunk should carry anchored summary, got %q", client.prompts[1])
	}
	if len(result) == 0 || !IsConversationSummaryContent(result[0].Content) || !strings.Contains(result[0].Content, "summary chunk") {
		t.Fatalf("expected compacted summary in result, got %+v", result)
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
	result, err := CompactWithBudget(context.Background(), messages, client, "test-model", Budget{
		ContextTokens:    1_000,
		KeepRecentTokens: 1_000,
	})
	if err != nil {
		t.Fatalf("CompactWithContextWindow: %v", err)
	}
	if len(result) >= len(messages) {
		t.Fatalf("expected single-user tool run to compact, got %d messages from %d", len(result), len(messages))
	}
	if result[0].Role != "system" || !IsConversationSummaryContent(result[0].Content) {
		t.Fatalf("expected compact summary first, got %#v", result[0])
	}
	if err := providers.ValidateToolCallHistory(result); err != nil {
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

func TestCompact_ReplacesPreviousSummaryInsteadOfStacking(t *testing.T) {
	messages := []providers.ChatMessage{
		{Role: "system", Content: "You are wuu."},
		{Role: "system", Content: BuildSummaryContent("old anchored summary")},
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "first reply"},
		{Role: "user", Content: "second"},
		{Role: "assistant", Content: "second reply"},
		{Role: "user", Content: "third"},
		{Role: "assistant", Content: "third reply"},
	}

	client := &mockCompactClient{response: "updated anchored summary"}
	result, err := Compact(context.Background(), messages, client, "test")
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}

	summaryCount := 0
	for _, msg := range result {
		if msg.Role == "system" && IsConversationSummaryContent(msg.Content) {
			summaryCount++
			if strings.Contains(msg.Content, "old anchored summary") {
				t.Fatalf("old summary should be replaced, got %q", msg.Content)
			}
		}
	}
	if summaryCount != 1 {
		t.Fatalf("expected exactly one compact summary, got %d in %+v", summaryCount, result)
	}
	if result[0].Content != "You are wuu." {
		t.Fatalf("expected base system prompt preserved first, got %+v", result[0])
	}
	if len(client.lastRequest.Messages) < 2 {
		t.Fatalf("expected compact request, got %+v", client.lastRequest.Messages)
	}
	prompt := client.lastRequest.Messages[1].Content
	if !strings.Contains(prompt, "Previous anchored summary") || !strings.Contains(prompt, "old anchored summary") {
		t.Fatalf("expected previous summary in compact prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "Return one complete replacement summary") {
		t.Fatalf("expected replacement-summary instruction, got %q", prompt)
	}
}

func TestCompact_RepeatedCompactionKeepsRecentTailAndAnchorsPreviousSummary(t *testing.T) {
	firstMessages := []providers.ChatMessage{
		{Role: "user", Content: "one"},
		{Role: "assistant", Content: "one reply"},
		{Role: "user", Content: "two"},
		{Role: "assistant", Content: "two reply"},
		{Role: "user", Content: "three"},
		{Role: "assistant", Content: "three reply"},
	}
	firstClient := &mockCompactClient{response: "summary one"}

	firstResult, err := Compact(context.Background(), firstMessages, firstClient, "test")
	if err != nil {
		t.Fatalf("first Compact: %v", err)
	}
	if len(firstResult) != 5 {
		t.Fatalf("expected first compact to keep summary plus two recent turns, got %+v", firstResult)
	}
	if !IsConversationSummaryContent(firstResult[0].Content) || !strings.Contains(firstResult[0].Content, "summary one") {
		t.Fatalf("expected first compact summary, got %+v", firstResult[0])
	}

	secondMessages := append([]providers.ChatMessage{}, firstResult...)
	secondMessages = append(secondMessages,
		providers.ChatMessage{Role: "user", Content: "four"},
		providers.ChatMessage{Role: "assistant", Content: "four reply"},
	)
	secondClient := &mockCompactClient{response: "summary two"}

	secondResult, err := Compact(context.Background(), secondMessages, secondClient, "test")
	if err != nil {
		t.Fatalf("second Compact: %v", err)
	}

	if len(secondClient.lastRequest.Messages) < 2 {
		t.Fatalf("expected compact request, got %+v", secondClient.lastRequest.Messages)
	}
	prompt := secondClient.lastRequest.Messages[1].Content
	if !strings.Contains(prompt, "Previous anchored summary") || strings.Count(prompt, "summary one") != 1 {
		t.Fatalf("expected previous summary to anchor replacement prompt once, got %q", prompt)
	}
	for _, want := range []string{"[user]: two", "[assistant]: two reply"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected second compact prompt to include old retained head %q, got %q", want, prompt)
		}
	}
	for _, notWant := range []string{"[user]: three", "[assistant]: three reply", "[user]: four", "[assistant]: four reply"} {
		if strings.Contains(prompt, notWant) {
			t.Fatalf("expected recent tail %q to stay out of summary input, got %q", notWant, prompt)
		}
	}

	summaryCount := 0
	for _, msg := range secondResult {
		if msg.Role == "system" && IsConversationSummaryContent(msg.Content) {
			summaryCount++
			if !strings.Contains(msg.Content, "summary two") || strings.Contains(msg.Content, "summary one") {
				t.Fatalf("expected replacement summary only, got %q", msg.Content)
			}
		}
	}
	if summaryCount != 1 {
		t.Fatalf("expected exactly one compact summary, got %d in %+v", summaryCount, secondResult)
	}
	for _, want := range []string{"three", "three reply", "four", "four reply"} {
		found := false
		for _, msg := range secondResult {
			if msg.Content == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected recent tail message %q to remain raw, got %+v", want, secondResult)
		}
	}
	for _, notWant := range []string{"two", "two reply"} {
		for _, msg := range secondResult {
			if msg.Content == notWant {
				t.Fatalf("expected older retained head %q to be summarized, got %+v", notWant, secondResult)
			}
		}
	}
}

func TestCompactTailBudget_UsesPiStyleDefaultAndOverride(t *testing.T) {
	if got, want := compactTailBudget("gpt-4o", 128_000), compactDefaultKeepRecentTokens; got != want {
		t.Fatalf("expected default keep-recent budget %d, got %d", want, got)
	}
	if got, want := compactTailBudgetForBudget("test-model", Budget{ContextTokens: 20_000, OutputReserveTokens: 4_000, KeepRecentTokens: 5_000}), 5_000; got != want {
		t.Fatalf("expected configured keep-recent budget %d, got %d", want, got)
	}
	if got, want := compactTailBudgetForBudget("test-model", Budget{ContextTokens: 1_000, KeepRecentTokens: 5_000}), 500; got != want {
		t.Fatalf("expected small-window keep-recent budget cap %d, got %d", want, got)
	}
	if got, want := compactUsableInputWindow("gpt-5.5", Budget{ContextTokens: 1_000_000, InputTokens: 272_000, OutputReserveTokens: 128_000}), 252_000; got != want {
		t.Fatalf("input-limited usable window = %d, want %d", got, want)
	}
	if got, want := compactUsableInputWindow("gpt-5.5", Budget{ContextTokens: 1_000_000, OutputReserveTokens: 128_000}), 872_000; got != want {
		t.Fatalf("context usable window = %d, want %d", got, want)
	}
}

func TestCompact_SummarizesAllWhenRecentTailWouldBeWholeHistory(t *testing.T) {
	messages := []providers.ChatMessage{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "first reply"},
		{Role: "user", Content: "second"},
		{Role: "assistant", Content: "second reply"},
	}

	client := &mockCompactClient{response: "summary of all turns"}
	result, err := Compact(context.Background(), messages, client, "test")
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if len(result) != 1 || result[0].Role != "system" || !IsConversationSummaryContent(result[0].Content) {
		t.Fatalf("expected summary-only compacted history, got %+v", result)
	}
	if strings.Contains(result[0].Content, "first reply") {
		t.Fatalf("expected raw history to be summarized, got %q", result[0].Content)
	}
}

func TestCanCompactWithBudgetSkipsFreshPrompt(t *testing.T) {
	messages := []providers.ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: strings.Repeat("fresh prompt ", 1000)},
	}
	if CanCompactWithBudget(messages, "test", Budget{ContextTokens: 1000}) {
		t.Fatal("fresh system+user prompt has no older history to compact")
	}
}

func TestCanCompactWithBudgetDetectsOlderHistory(t *testing.T) {
	messages := []providers.ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "first reply"},
		{Role: "user", Content: "second"},
	}
	if !CanCompactWithBudget(messages, "test", Budget{ContextTokens: 1000}) {
		t.Fatal("expected older conversation turns to be compactable")
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
		{Role: "user", Content: "before current tool run"},
		{Role: "assistant", Content: "ready"},
		{Role: "user", Content: "run both reads"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{
			{ID: "c1", Name: "read_file", Arguments: `{"path":"README.md"}`},
			{ID: "c2", Name: "read_file", Arguments: `{"path":"README_zh.md"}`},
		}},
		{Role: "tool", Name: "read_file", ToolCallID: "c1", Content: "english"},
		{Role: "tool", Name: "read_file", ToolCallID: "c2", Content: "chinese"},
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "what changed?"},
		{Role: "assistant", Content: "summarized changes"},
	}

	client := &mockCompactClient{response: "summary of older turns"}
	result, err := Compact(context.Background(), messages, client, "test")
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if err := providers.ValidateToolCallHistory(result); err != nil {
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
	large := strings.Repeat("x", 5000)
	messages := []providers.ChatMessage{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: large},
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

func TestCompactKeepStart_UsesFullBudgetAcrossMoreThanTwoUserTurns(t *testing.T) {
	large := strings.Repeat("x", 5000)
	messages := []providers.ChatMessage{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: large},
		{Role: "user", Content: "second"},
		{Role: "assistant", Content: "second reply"},
		{Role: "user", Content: "third"},
		{Role: "assistant", Content: "third reply"},
		{Role: "user", Content: "fourth"},
		{Role: "assistant", Content: "fourth reply"},
	}

	start := compactKeepStart(messages, 1000)
	if start != 2 {
		t.Fatalf("expected token budget to keep more than two recent turns, start=%d", start)
	}
}

type ctxAwareCompactClient struct{}

func (c *ctxAwareCompactClient) Chat(ctx context.Context, _ providers.ChatRequest) (providers.ChatResponse, error) {
	<-ctx.Done()
	return providers.ChatResponse{}, ctx.Err()
}

func TestCompact_DefaultTimeoutLeavesRoomForLargeRecovery(t *testing.T) {
	t.Setenv("WUU_COMPACT_TIMEOUT_MS", "")

	if got, want := compactTimeout(), 20*time.Minute; got != want {
		t.Fatalf("compactTimeout() = %s, want %s", got, want)
	}
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
	large := largePrunableToolOutput("x")
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
	if !strings.Contains(summaryInput, "[Pruned read_file result.") {
		t.Fatalf("expected placeholder in summary input, got: %s", summaryInput)
	}
}

func TestCompact_DoesNotPruneRecentTailToolResults(t *testing.T) {
	large := strings.Repeat("y", 2_000)
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

func TestCompact_StripsHistoricalImagesFromKeptTail(t *testing.T) {
	imageData := strings.Repeat("a", 1200)
	messages := []providers.ChatMessage{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "first reply"},
		{Role: "user", Content: "second screenshot", Images: []providers.InputImage{{MediaType: "image/png", Data: imageData}}},
		{Role: "assistant", Content: "second reply"},
		{Role: "user", Content: "third screenshot", Images: []providers.InputImage{{MediaType: "image/jpeg", Data: "latest"}}},
		{Role: "assistant", Content: "third reply"},
	}

	client := &mockCompactClient{response: "summary"}
	result, err := CompactWithContextWindow(context.Background(), messages, client, "test", 100_000)
	if err != nil {
		t.Fatalf("CompactWithContextWindow: %v", err)
	}

	var historical providers.ChatMessage
	var latest providers.ChatMessage
	foundHistorical := false
	foundLatest := false
	for _, msg := range result {
		switch msg.Content {
		case "second screenshot\n\n[Image attachment omitted from compacted history: image/png, 1200 base64 characters.]":
			historical = msg
			foundHistorical = true
		case "third screenshot":
			latest = msg
			foundLatest = true
		}
	}
	if !foundHistorical {
		t.Fatalf("expected historical image omission note in compacted result: %+v", result)
	}
	if !foundLatest {
		t.Fatalf("expected latest user message in compacted result: %+v", result)
	}
	if len(historical.Images) != 0 {
		t.Fatalf("expected historical image stripped, got %+v", historical.Images)
	}
	if len(latest.Images) != 1 || latest.Images[0].Data != "latest" {
		t.Fatalf("expected latest user image preserved, got %+v", latest.Images)
	}
}

func TestCompact_StripsHistoricalFilesFromKeptTail(t *testing.T) {
	fileData := strings.Repeat("b", 1400)
	messages := []providers.ChatMessage{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "first reply"},
		{Role: "user", Content: "second brief", Files: []providers.InputFile{{MediaType: "application/pdf", Data: fileData, Filename: "brief.pdf"}}},
		{Role: "assistant", Content: "second reply"},
		{Role: "user", Content: "latest brief", Files: []providers.InputFile{{MediaType: "application/pdf", Data: "latest", Filename: "latest.pdf"}}},
		{Role: "assistant", Content: "latest reply"},
	}

	client := &mockCompactClient{response: "summary"}
	result, err := CompactWithContextWindow(context.Background(), messages, client, "test", 100_000)
	if err != nil {
		t.Fatalf("CompactWithContextWindow: %v", err)
	}

	var historical providers.ChatMessage
	var latest providers.ChatMessage
	foundHistorical := false
	foundLatest := false
	for _, msg := range result {
		switch msg.Content {
		case "second brief\n\n[File attachment omitted from compacted history: application/pdf, brief.pdf, 1400 base64 characters.]":
			historical = msg
			foundHistorical = true
		case "latest brief":
			latest = msg
			foundLatest = true
		}
	}
	if !foundHistorical {
		t.Fatalf("expected historical file omission note in compacted result: %+v", result)
	}
	if !foundLatest {
		t.Fatalf("expected latest user message in compacted result: %+v", result)
	}
	if len(historical.Files) != 0 {
		t.Fatalf("expected historical file stripped, got %+v", historical.Files)
	}
	if len(latest.Files) != 1 || latest.Files[0].Data != "latest" {
		t.Fatalf("expected latest user file preserved, got %+v", latest.Files)
	}
}

func TestBuildSummaryPromptMentionsImagesWithoutData(t *testing.T) {
	imageData := strings.Repeat("z", 80)
	prompt := buildSummaryPrompt([]providers.ChatMessage{
		{Role: "user", Content: "see screenshot", Images: []providers.InputImage{{MediaType: "image/png", Data: imageData}}},
	}, "")

	if strings.Contains(prompt, imageData) {
		t.Fatal("summary prompt must not include raw image data")
	}
	if !strings.Contains(prompt, "[image omitted: image/png, 80 base64 characters]") {
		t.Fatalf("expected image omission note, got %q", prompt)
	}
}

func TestPruneOldToolResults_PreservesToolCallPairing(t *testing.T) {
	large := largePrunableToolOutput("z")
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

func TestPruneOldToolResults_ProtectsSkillTool(t *testing.T) {
	large := largePrunableToolOutput("s")
	messages := []providers.ChatMessage{
		{Role: "tool", Name: "skill", ToolCallID: "skill-call", Content: large},
	}

	pruned := pruneOldToolResults(messages)
	if pruned[0].Content != large {
		t.Fatal("skill tool output should be protected from pruning")
	}
}

func largePrunableToolOutput(char string) string {
	return strings.Repeat(char, (toolResultPruneProtectTokens+toolResultPruneMinimumTokens+1_000)*4)
}

func TestPruneToolResults_EmptyInput(t *testing.T) {
	if got := PruneToolResults(nil); got != nil {
		t.Fatalf("expected nil for empty input, got %v", got)
	}
}

func TestPruneToolResults_TurnProtection(t *testing.T) {
	large := largePrunableToolOutput("z")
	messages := []providers.ChatMessage{
		// Turn 1 (old, eligible for pruning)
		{Role: "user", Content: "old question"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "c1", Name: "bash", Arguments: "{}"}}},
		{Role: "tool", Name: "bash", ToolCallID: "c1", Content: large},
		// Turn 2 (within last 2 turns, protected)
		{Role: "user", Content: "recent question"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "c2", Name: "bash", Arguments: "{}"}}},
		{Role: "tool", Name: "bash", ToolCallID: "c2", Content: large},
		// Turn 3 (most recent, protected)
		{Role: "user", Content: "latest question"},
		{Role: "assistant", Content: "done"},
	}

	pruned := PruneToolResults(messages)

	if pruned[2].Content == large {
		t.Fatal("expected old tool result to be pruned")
	}
	if !strings.Contains(pruned[2].Content, "[Pruned bash result") {
		t.Fatalf("expected pruned placeholder, got %q", pruned[2].Content)
	}
	if pruned[5].Content != large {
		t.Fatal("expected recent tool result to be preserved (turn protection)")
	}
}

func TestPruneToolResults_DoesNotCountHiddenContextAnchorAsUserTurn(t *testing.T) {
	large := largePrunableToolOutput("a")
	messages := []providers.ChatMessage{
		// Visible turn 1: old enough to prune.
		{Role: "user", Content: "old question"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "c1", Name: "bash", Arguments: "{}"}}},
		{Role: "tool", Name: "bash", ToolCallID: "c1", Content: large},
		{Role: "assistant", Content: "old answer"},
		// Visible turn 2: should remain protected.
		{Role: "user", Content: "recent question"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "c2", Name: "bash", Arguments: "{}"}}},
		{Role: "tool", Name: "bash", ToolCallID: "c2", Content: large},
		{Role: "assistant", Content: "recent answer"},
		// Visible turn 3: latest user request, followed by an internal anchor.
		{Role: "user", Content: "latest question"},
		BuildContextAnchorMessage(0),
	}

	pruned := PruneToolResults(messages)

	if pruned[2].Content == large {
		t.Fatal("expected old visible turn tool result to be pruned")
	}
	if pruned[6].Content != large {
		t.Fatal("hidden context anchor must not consume a protected visible user turn")
	}
}

func TestPruneToolResults_ProtectsSkillTool(t *testing.T) {
	large := largePrunableToolOutput("s")
	messages := []providers.ChatMessage{
		{Role: "user", Content: "q1"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "c1", Name: "skill", Arguments: "{}"}}},
		{Role: "tool", Name: "skill", ToolCallID: "c1", Content: large},
		{Role: "user", Content: "q2"},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "q3"},
		{Role: "assistant", Content: "done"},
	}

	pruned := PruneToolResults(messages)
	if pruned[2].Content != large {
		t.Fatal("skill tool output should be protected from pruning")
	}
}

func TestPruneToolResults_RecallableAndNonRecallablePlaceholders(t *testing.T) {
	large := largePrunableToolOutput("x")
	messages := []providers.ChatMessage{
		// Old turn 1: read_file (recallable)
		{Role: "user", Content: "q1"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "rf1", Name: "read_file", Arguments: "{}"}}},
		{Role: "tool", Name: "read_file", ToolCallID: "rf1", Content: large},
		// Old turn 2: bash (non-recallable)
		{Role: "user", Content: "q2"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "bs1", Name: "bash", Arguments: "{}"}}},
		{Role: "tool", Name: "bash", ToolCallID: "bs1", Content: large},
		// Recent turns (protected)
		{Role: "user", Content: "q3"},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "q4"},
		{Role: "assistant", Content: "done"},
	}

	pruned := PruneToolResults(messages)

	if !strings.Contains(pruned[2].Content, "Re-run read_file") {
		t.Fatalf("expected recallable placeholder for read_file, got %q", pruned[2].Content)
	}
	if !strings.Contains(pruned[5].Content, "may not be reproducible") {
		t.Fatalf("expected non-recallable placeholder for bash, got %q", pruned[5].Content)
	}
	// Non-destructive: originals unchanged.
	if messages[2].Content != large {
		t.Fatal("original read_file result should be unchanged (non-destructive)")
	}
	if messages[5].Content != large {
		t.Fatal("original bash result should be unchanged (non-destructive)")
	}
}

func TestPruneToolResults_BelowMinimumNotPruned(t *testing.T) {
	// Recent old result (processed first going backwards) is under the 40K
	// protect threshold; older result pushes total over 40K but the prunable
	// amount is under the 20K minimum, so nothing is pruned.
	recentOld := strings.Repeat("b", 150_000) // ~37.5K tokens, under 40K protect
	farOld := strings.Repeat("a", 50_000)     // ~12.5K tokens, prunable < 20K minimum

	messages := []providers.ChatMessage{
		{Role: "user", Content: "q1"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "c1", Name: "bash", Arguments: "{}"}}},
		{Role: "tool", Name: "bash", ToolCallID: "c1", Content: farOld},
		{Role: "user", Content: "q2"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "c2", Name: "bash", Arguments: "{}"}}},
		{Role: "tool", Name: "bash", ToolCallID: "c2", Content: recentOld},
		{Role: "user", Content: "q3"},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "q4"},
		{Role: "assistant", Content: "done"},
	}

	pruned := PruneToolResults(messages)
	if pruned[2].Content != farOld {
		t.Fatal("tool result with prunable amount below minimum should not be pruned")
	}
	if pruned[5].Content != recentOld {
		t.Fatal("tool result under protect threshold should not be pruned")
	}
}
