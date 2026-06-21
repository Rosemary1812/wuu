package compact

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/stringutil"
)

const defaultCompactTimeout = 60 * time.Second
const (
	toolResultPruneProtectTokens = 40_000
	toolResultPruneMinimumTokens = 20_000
)

var protectedToolResults = map[string]bool{
	"skill": true,
}

// maxCompactOutputChars caps the summarization output to approximately
// 20K tokens (~4 chars per token). Aligned with Claude Code's
// MAX_OUTPUT_TOKENS_FOR_SUMMARY and Codex's COMPACT_USER_MESSAGE_MAX_TOKENS.
// Without this cap, the summary itself can consume a large portion of
// the context window, defeating the purpose of compaction.
const maxCompactOutputChars = 80_000
const (
	compactSummaryMaxTokens      = 4096
	compactPartialMinOutputChars = 200
)

const (
	// ConversationSummaryPrefix marks the synthetic summary installed after
	// compacting older conversation turns. Kept stable for persisted sessions
	// and cache-hint detection.
	ConversationSummaryPrefix = "[Conversation summary]"
	summarySectionHeader      = "Summary:"
	summaryContinuationNote   = "This session is being continued from a previous conversation that ran out of context. The summary below covers the earlier portion of the conversation."
)

func compactTimeout() time.Duration {
	if v := os.Getenv("WUU_COMPACT_TIMEOUT_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return time.Duration(n) * time.Millisecond
		}
	}
	return defaultCompactTimeout
}

func withCompactTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := compactTimeout()
	if timeout <= 0 {
		return ctx, func() {}
	}
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > 0 && remaining <= timeout {
			return ctx, func() {}
		}
	}
	return context.WithTimeout(ctx, timeout)
}

// EstimateTokens provides a rough token count estimate.
// English: ~4 chars per token. CJK: ~2 chars per token.
//
// Counts total runes and CJK runes in a single pass. Invalid UTF-8
// sequences yield utf8.RuneError (1 rune each) from the range loop,
// matching the behavior of utf8.RuneCountInString, so the count is
// identical to the previous two-pass implementation.
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}

	var cjkCount, totalChars int
	for _, r := range text {
		totalChars++
		if isCJK(r) {
			cjkCount++
		}
	}

	nonCJK := totalChars - cjkCount
	return (nonCJK / 4) + (cjkCount / 2) + 1
}

// EstimateJSONTokens estimates tokens for JSON content. JSON is
// denser than prose because single-character structural tokens
// ({, }, :, ,, ") each consume one token. Aligned with Claude Code's
// file-type-aware bytesPerToken=2 for JSON.
func EstimateJSONTokens(text string) int {
	if text == "" {
		return 0
	}
	return utf8.RuneCountInString(text)/2 + 1
}

// imageTokenEstimate is the minimum token budget for an image content
// block. We also account for large inline base64 payloads so resumed
// sessions with old screenshots compact before the request body itself
// becomes the dominant context risk.
const imageTokenEstimate = 2000

// toolDefinitionOverhead is the approximate token cost the API adds
// for tool definitions in the request (schema preamble, JSON wrapping).
// Claude Code documents this as ~500 tokens when tools are present.
const toolDefinitionOverhead = 500

// EstimateMessagesTokens estimates total tokens for a message list.
// Counts content, reasoning, tool calls (name + arguments + envelope),
// images, and per-message overhead. Slightly pessimistic so proactive
// compact fires before the hard overflow.
func EstimateMessagesTokens(messages []providers.ChatMessage) int {
	total := 0
	hasTools := false
	for _, msg := range messages {
		// Tool call arguments are JSON — use denser ratio.
		total += EstimateTokens(msg.Content)
		total += EstimateTokens(msg.ReasoningContent)
		total += 4 // per-message overhead (role, separators)
		for _, tc := range msg.ToolCalls {
			hasTools = true
			total += EstimateTokens(tc.Name)
			total += EstimateJSONTokens(tc.Arguments)
			total += 8 // tool call envelope (id, type, JSON wrapping)
		}
		for _, image := range msg.Images {
			total += estimateImageTokens(image)
		}
		for _, file := range msg.Files {
			total += estimateFileTokens(file)
		}
	}
	if hasTools {
		total += toolDefinitionOverhead
	}
	return total
}

func estimateImageTokens(image providers.InputImage) int {
	dataLen := len(strings.TrimSpace(image.Data))
	if dataLen == 0 {
		return 0
	}
	payloadEstimate := dataLen / 4
	if payloadEstimate > imageTokenEstimate {
		return payloadEstimate
	}
	return imageTokenEstimate
}

func estimateFileTokens(file providers.InputFile) int {
	dataLen := len(strings.TrimSpace(file.Data))
	if dataLen == 0 {
		return 0
	}
	payloadEstimate := dataLen / 4
	if payloadEstimate > imageTokenEstimate {
		return payloadEstimate
	}
	return imageTokenEstimate
}

// ShouldCompact returns true if messages exceed the threshold.
func ShouldCompact(messages []providers.ChatMessage, maxContextTokens int) bool {
	if maxContextTokens <= 0 {
		return false
	}
	estimated := EstimateMessagesTokens(messages)
	threshold := int(float64(maxContextTokens) * 0.8)
	return estimated > threshold
}

// maxCompactRetries caps how many times Compact will defensively trim
// the oldest message and re-issue the summarization request after
// hitting a context-overflow on the compact request itself. Aligned
// with Codex CLI's safeguard.
const maxCompactRetries = 3

// compactReservedMaxTokens reserves output headroom before deciding how much
// raw recent context can survive compaction. This mirrors OpenCode's overflow
// guard, which keeps up to 20K tokens unavailable for retained input.
const compactReservedMaxTokens = 20_000

// compactTailMaxTokens caps the recent raw history kept after compaction.
// OpenCode keeps only a small recent tail after the anchored summary; its
// default upper bound is 8K tokens.
const compactTailMaxTokens = 8_000

// compactTailMinTokens mirrors OpenCode's lower bound for the recent raw
// context kept after the anchored summary. Even small models need enough fresh
// context to preserve the immediate working state.
const compactTailMinTokens = 2_000

// compactTailDefaultTurns mirrors OpenCode's default tail_turns setting.
const compactTailDefaultTurns = 2

// compactTailContextFraction keeps the raw tail small relative to the target
// model window so the generated summary and post-compact context still have
// room. OpenCode's default is roughly 25% of the usable input window, capped at
// 8K. The tail selector also keeps complete user-anchored turns and tool chains,
// so this is a soft budget rather than a hard truncation point.
const compactTailContextFraction = 0.25

// Compact compresses older messages into a summary. It finds an
// appropriate boundary near the end of the conversation, summarizes
// everything before it through the provided client's normal Chat path,
// and returns the compacted message list. Provider-specific remote
// compaction endpoints are intentionally not part of this flow; wuu owns
// the prompt, output format, and history replacement.
//
// Defensive trimming: if the summarization request itself overflows
// the model's context window (because the conversation being
// compacted is itself enormous), Compact drops the oldest entry from
// the to-be-summarized slice and retries up to maxCompactRetries
// times. This prevents the "compact → overflow → compact again →
// overflow again" deadlock the simple form is vulnerable to.
func Compact(ctx context.Context, messages []providers.ChatMessage, client providers.Client, model string) ([]providers.ChatMessage, error) {
	return CompactWithContextWindow(ctx, messages, client, model, 0)
}

func CompactWithContextWindow(ctx context.Context, messages []providers.ChatMessage, client providers.Client, model string, maxContextTokens int) ([]providers.ChatMessage, error) {
	return CompactWithBudget(ctx, messages, client, model, Budget{ContextTokens: maxContextTokens})
}

type Budget struct {
	ContextTokens       int
	InputTokens         int
	OutputReserveTokens int
}

func CompactWithBudget(ctx context.Context, messages []providers.ChatMessage, client providers.Client, model string, budget Budget) ([]providers.ChatMessage, error) {
	if len(messages) <= 2 {
		return messages, nil // nothing to compact
	}
	ctx, cancel := withCompactTimeout(ctx)
	defer cancel()

	systemPrefix, previousSummary, previousSummaryDiscoveredTools, conversation := splitLeadingSystemMessages(messages)
	if len(conversation) <= 2 {
		return messages, nil
	}

	conversationForCompact := stripHistoricalImages(conversation)
	keepStart := compactKeepStart(conversationForCompact, compactTailBudgetForBudget(model, budget))
	if keepStart < 0 || keepStart > len(conversationForCompact) {
		return messages, nil
	}

	toSummarize := pruneOldToolResults(conversationForCompact[:keepStart])
	toKeep := conversationForCompact[keepStart:]

	for attempt := 0; ; attempt++ {
		summaryInput := buildSummaryPrompt(toSummarize, previousSummary)
		summaryReq := providers.ChatRequest{
			Model: model,
			Messages: []providers.ChatMessage{
				{Role: "system", Content: "You summarize coding-agent conversations for context compaction. Follow the user's required format exactly. Do not call tools."},
				{Role: "user", Content: summaryInput},
			},
			Temperature: 0.3,
			MaxTokens:   compactSummaryMaxTokens,
			ProviderOptions: map[string]any{
				"textVerbosity": "low",
			},
		}

		resp, err := summarizeCompact(ctx, client, summaryReq)
		if err != nil {
			// If the summary request itself overflowed the model's
			// context window, drop the oldest message from the slice
			// being summarized and try again. This is the "compact-
			// of-compact" backstop borrowed from Codex CLI.
			if providers.IsContextOverflow(err) && attempt < maxCompactRetries && len(toSummarize) > 1 {
				toSummarize = toSummarize[1:]
				continue
			}
			return messages, fmt.Errorf("compact summary failed: %w", err)
		}

		summary := FormatSummary(resp.Content)
		if len(summary) > maxCompactOutputChars {
			cut := maxCompactOutputChars
			for cut > 0 && summary[cut-1]&0xC0 == 0x80 {
				cut--
			}
			summary = summary[:cut]
		}
		if summary == "" {
			return messages, nil
		}

		summaryDiscoveredTools := providers.MergeLoadableToolDefinitions(
			previousSummaryDiscoveredTools,
			providers.DiscoveredToolsFromMessages(conversationForCompact[:keepStart]),
		)
		compacted := providers.CloneChatMessages(systemPrefix)
		compacted = append(compacted, providers.ChatMessage{
			Role:            "system",
			Content:         BuildSummaryContent(summary),
			DiscoveredTools: summaryDiscoveredTools,
		})
		compacted = append(compacted, providers.CloneChatMessages(toKeep)...)
		return compacted, nil
	}
}

func summarizeCompact(ctx context.Context, client providers.Client, req providers.ChatRequest) (providers.ChatResponse, error) {
	if streamClient, ok := client.(providers.StreamClient); ok {
		return streamCompactSummary(ctx, streamClient, req)
	}
	return client.Chat(ctx, req)
}

func streamCompactSummary(ctx context.Context, client providers.StreamClient, req providers.ChatRequest) (providers.ChatResponse, error) {
	ch, err := client.StreamChat(ctx, req)
	if err != nil {
		return providers.ChatResponse{}, err
	}

	var content strings.Builder
	var usage *providers.TokenUsage
	var finishReason providers.FinishReason
	stopReason := ""
	truncated := false
	done := false
	for event := range ch {
		switch event.Type {
		case providers.EventContentDelta:
			content.WriteString(event.Content)
		case providers.EventError:
			if event.Error != nil {
				if resp, ok, ferr := recoverCompactStream(ctx, client, req, content.String(), event.Error); ok {
					return resp, ferr
				}
				return providers.ChatResponse{}, event.Error
			}
			return providers.ChatResponse{}, fmt.Errorf("compact summary stream error")
		case providers.EventDone:
			done = true
			if event.Usage != nil {
				usage = event.Usage
			}
			finishReason = event.FinishReason
			stopReason = event.StopReason
			truncated = event.Truncated
		}
	}
	if !done {
		err := providers.NewIncompleteStreamError("compact summary stream closed before done")
		if resp, ok, ferr := recoverCompactStream(ctx, client, req, content.String(), err); ok {
			return resp, ferr
		}
		return providers.ChatResponse{}, err
	}
	if finishReason == "" {
		finishReason = providers.NormalizeFinishReason(stopReason, truncated, false)
	}
	return providers.ChatResponse{
		Content:      content.String(),
		Usage:        usage,
		FinishReason: finishReason,
		StopReason:   stopReason,
		Truncated:    truncated,
	}, nil
}

func recoverCompactStream(ctx context.Context, client providers.Client, req providers.ChatRequest, partial string, streamErr error) (providers.ChatResponse, bool, error) {
	if partial = strings.TrimSpace(partial); len(partial) >= compactPartialMinOutputChars {
		return providers.ChatResponse{
			Content:      partial,
			FinishReason: providers.FinishReasonLength,
			Truncated:    true,
		}, true, nil
	}
	if !isIncompleteCompactStream(streamErr) {
		return providers.ChatResponse{}, false, nil
	}
	resp, err := client.Chat(ctx, req)
	if err != nil {
		return providers.ChatResponse{}, true, fmt.Errorf("compact summary stream incomplete and chat fallback failed: %w", err)
	}
	return resp, true, nil
}

func isIncompleteCompactStream(err error) bool {
	var streamErr *providers.StreamError
	if !errors.As(err, &streamErr) {
		return false
	}
	if streamErr.ContextOverflow || streamErr.Auth {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(streamErr.Message))
	return strings.Contains(msg, "before done") ||
		strings.Contains(msg, "before [done]") ||
		strings.Contains(msg, "before message_stop") ||
		strings.Contains(msg, "before completion") ||
		strings.Contains(msg, "before response.completed")
}

func splitLeadingSystemMessages(messages []providers.ChatMessage) ([]providers.ChatMessage, string, []providers.LoadableToolDefinition, []providers.ChatMessage) {
	i := 0
	systemPrefix := make([]providers.ChatMessage, 0)
	previousSummary := ""
	var previousSummaryDiscoveredTools []providers.LoadableToolDefinition
	for i < len(messages) && strings.EqualFold(messages[i].Role, "system") {
		msg := messages[i]
		if IsConversationSummaryContent(msg.Content) {
			previousSummary = summaryBodyFromContent(msg.Content)
			previousSummaryDiscoveredTools = providers.MergeLoadableToolDefinitions(previousSummaryDiscoveredTools, msg.DiscoveredTools)
			i++
			continue
		}
		systemPrefix = append(systemPrefix, msg)
		i++
	}
	return systemPrefix, previousSummary, previousSummaryDiscoveredTools, messages[i:]
}

// FormatSummary turns the model's compact response into the content that will
// be replayed later. The current prompt asks for markdown only, but this still
// strips legacy XML wrappers from older compaction prompts and tests.
func FormatSummary(raw string) string {
	summary := strings.TrimSpace(raw)
	summary = stripXMLBlock(summary, "analysis")
	if extracted, ok := extractXMLBlock(summary, "summary"); ok {
		summary = extracted
	}
	summary = strings.TrimSpace(summary)
	summary = strings.ReplaceAll(summary, "\r\n", "\n")
	summary = collapseBlankLines(summary)
	return summary
}

// BuildSummaryContent wraps a cleaned summary in the stable persisted handoff
// format used by load/resume and cache-hint detection.
func BuildSummaryContent(summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return ConversationSummaryPrefix
	}
	return fmt.Sprintf("%s\n%s\n\n%s\n%s", ConversationSummaryPrefix, summaryContinuationNote, summarySectionHeader, summary)
}

// IsConversationSummaryContent reports whether content is a persisted compact
// summary. It accepts both the current handoff format and the older bare
// "[Conversation summary]" format for existing sessions.
func IsConversationSummaryContent(content string) bool {
	return strings.HasPrefix(strings.TrimSpace(content), ConversationSummaryPrefix)
}

func summaryBodyFromContent(content string) string {
	text := strings.TrimSpace(content)
	if !IsConversationSummaryContent(text) {
		return ""
	}
	text = strings.TrimSpace(strings.TrimPrefix(text, ConversationSummaryPrefix))
	text = strings.TrimSpace(strings.TrimPrefix(text, summaryContinuationNote))
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, summarySectionHeader)
	return strings.TrimSpace(text)
}

func stripXMLBlock(text, tag string) string {
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	start := strings.Index(text, open)
	if start < 0 {
		return text
	}
	end := strings.Index(text[start+len(open):], close)
	if end < 0 {
		return text
	}
	end += start + len(open)
	return strings.TrimSpace(text[:start] + text[end+len(close):])
}

func extractXMLBlock(text, tag string) (string, bool) {
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	start := strings.Index(text, open)
	if start < 0 {
		return "", false
	}
	start += len(open)
	end := strings.Index(text[start:], close)
	if end < 0 {
		return "", false
	}
	return strings.TrimSpace(text[start : start+end]), true
}

func collapseBlankLines(text string) string {
	var b strings.Builder
	blank := false
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			if blank {
				continue
			}
			blank = true
			b.WriteByte('\n')
			continue
		}
		blank = false
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

func compactTailBudget(model string, maxContextTokens int) int {
	return compactTailBudgetForBudget(model, Budget{ContextTokens: maxContextTokens})
}

func compactTailBudgetForBudget(model string, budget Budget) int {
	usable := compactUsableInputWindow(model, budget)
	tailBudget := int(float64(usable) * compactTailContextFraction)
	if tailBudget > compactTailMaxTokens {
		return compactTailMaxTokens
	}
	if tailBudget < compactTailMinTokens {
		return compactTailMinTokens
	}
	return tailBudget
}

func compactUsableWindow(model string, window int) int {
	return compactUsableInputWindow(model, Budget{ContextTokens: window})
}

func compactUsableInputWindow(model string, budget Budget) int {
	window := budget.ContextTokens
	if window <= 0 {
		window = providers.ContextWindowFor(model)
	}
	if window <= 0 {
		return 0
	}
	outputReserve := budget.OutputReserveTokens
	if outputReserve <= 0 {
		outputReserve = providers.MaxOutputTokensFor(model)
	}
	if budget.InputTokens > 0 {
		reserved := outputReserve
		if reserved > compactReservedMaxTokens {
			reserved = compactReservedMaxTokens
		}
		if reserved <= 0 {
			return budget.InputTokens
		}
		return max(0, budget.InputTokens-reserved)
	}
	if outputReserve <= 0 {
		return window
	}
	return max(0, window-outputReserve)
}

type compactTurn struct {
	start int
	end   int
}

// compactKeepStart returns the index where the un-compacted tail should begin.
// It keeps up to the latest two user-anchored turns within the token budget,
// matching OpenCode's default tail selection. Long single-turn tool runs only
// have one user message at the front, so those fall back to a token-budgeted
// raw tail instead of refusing to compact. A return value equal to len(messages)
// means summarize everything and keep no raw tail; -1 means no compaction.
func compactKeepStart(messages []providers.ChatMessage, tailBudgetTokens int) int {
	if len(messages) <= 1 {
		return -1
	}
	if tailBudgetTokens <= 0 {
		tailBudgetTokens = compactTailMaxTokens
	}

	turns := compactTurns(messages)
	if len(turns) == 0 {
		return compactTailStartOrSummaryAll(messages, compactFallbackTailStart(messages, tailBudgetTokens))
	}
	if len(turns) == 1 && turns[0].start == 0 {
		return compactTailStartOrSummaryAll(messages, compactFallbackTailStart(messages, tailBudgetTokens))
	}

	recentStart := len(turns) - compactTailDefaultTurns
	if recentStart < 0 {
		recentStart = 0
	}
	recent := turns[recentStart:]

	total := 0
	keepStart := -1
	for i := len(recent) - 1; i >= 0; i-- {
		turn := recent[i]
		size := EstimateMessagesTokens(messages[turn.start:turn.end])
		if total+size <= tailBudgetTokens {
			total += size
			keepStart = turn.start
			continue
		}

		remaining := tailBudgetTokens - total
		if split, ok := compactSplitTurnStart(messages, turn, remaining); ok {
			keepStart = split
		}
		break
	}
	if keepStart <= 0 {
		return len(messages)
	}
	return adjustToolBoundary(messages, keepStart)
}

func compactTailStartOrSummaryAll(messages []providers.ChatMessage, start int) int {
	start = adjustToolBoundary(messages, start)
	if start <= 0 {
		return len(messages)
	}
	return start
}

func compactTurns(messages []providers.ChatMessage) []compactTurn {
	turns := make([]compactTurn, 0)
	for i, msg := range messages {
		if !strings.EqualFold(msg.Role, "user") {
			continue
		}
		turns = append(turns, compactTurn{
			start: i,
			end:   len(messages),
		})
	}
	for i := 0; i < len(turns)-1; i++ {
		turns[i].end = turns[i+1].start
	}
	return turns
}

func compactSplitTurnStart(messages []providers.ChatMessage, turn compactTurn, tailBudgetTokens int) (int, bool) {
	if tailBudgetTokens <= 0 || turn.end-turn.start <= 1 {
		return 0, false
	}
	for start := turn.start + 1; start < turn.end; start++ {
		if EstimateMessagesTokens(messages[start:turn.end]) > tailBudgetTokens {
			continue
		}
		return adjustToolBoundary(messages, start), true
	}
	return 0, false
}

func compactFallbackTailStart(messages []providers.ChatMessage, tailBudgetTokens int) int {
	start := len(messages) - 1
	for candidate := start - 1; candidate > 0; candidate-- {
		if EstimateMessagesTokens(messages[candidate:]) > tailBudgetTokens {
			break
		}
		start = candidate
	}
	return start
}

func lastUserMessageIndex(messages []providers.ChatMessage) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(messages[i].Role, "user") {
			return i
		}
	}
	return -1
}

func adjustToolBoundary(messages []providers.ChatMessage, start int) int {
	if start <= 0 || start >= len(messages) || !strings.EqualFold(messages[start].Role, "tool") {
		return start
	}

	// Boundary landed inside a tool-result block. Shift left to include every
	// contiguous tool result and the assistant tool_calls turn that started it.
	for start > 0 && strings.EqualFold(messages[start-1].Role, "tool") {
		start--
	}
	if start > 0 && strings.EqualFold(messages[start-1].Role, "assistant") && len(messages[start-1].ToolCalls) > 0 {
		start--
	}
	return start
}

func stripHistoricalImages(messages []providers.ChatMessage) []providers.ChatMessage {
	if len(messages) == 0 {
		return nil
	}
	latestUser := lastUserMessageIndex(messages)
	out := make([]providers.ChatMessage, len(messages))
	copy(out, messages)
	for i := range out {
		if len(out[i].Images) == 0 && len(out[i].Files) == 0 {
			continue
		}
		if i == latestUser && strings.EqualFold(out[i].Role, "user") {
			continue
		}
		out[i].Content = appendAttachmentOmissionNote(out[i].Content, out[i].Images, out[i].Files)
		out[i].Images = nil
		out[i].Files = nil
	}
	return out
}

func appendImageOmissionNote(content string, images []providers.InputImage) string {
	return appendAttachmentOmissionNote(content, images, nil)
}

func appendAttachmentOmissionNote(content string, images []providers.InputImage, files []providers.InputFile) string {
	if len(images) == 0 && len(files) == 0 {
		return content
	}
	var b strings.Builder
	b.WriteString(strings.TrimSpace(content))
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	for i, image := range images {
		if i > 0 {
			b.WriteByte('\n')
		}
		mediaType := strings.TrimSpace(image.MediaType)
		if mediaType == "" {
			mediaType = "image"
		}
		fmt.Fprintf(&b, "[Image attachment omitted from compacted history: %s, %d base64 characters.]", mediaType, len(strings.TrimSpace(image.Data)))
	}
	for i, file := range files {
		if len(images) > 0 || i > 0 {
			b.WriteByte('\n')
		}
		mediaType := strings.TrimSpace(file.MediaType)
		if mediaType == "" {
			mediaType = "file"
		}
		name := strings.TrimSpace(file.Filename)
		if name == "" {
			name = "file"
		}
		fmt.Fprintf(&b, "[File attachment omitted from compacted history: %s, %s, %d base64 characters.]", mediaType, name, len(strings.TrimSpace(file.Data)))
	}
	return b.String()
}

func pruneOldToolResults(messages []providers.ChatMessage) []providers.ChatMessage {
	if len(messages) == 0 {
		return nil
	}

	total := 0
	prunedTokens := 0
	var indexes []int
	for i := len(messages) - 1; i >= 0; i-- {
		if !strings.EqualFold(messages[i].Role, "tool") {
			continue
		}
		if protectedToolResults[strings.TrimSpace(messages[i].Name)] {
			continue
		}
		size := EstimateTokens(messages[i].Content)
		if size <= 0 {
			continue
		}
		total += size
		if total <= toolResultPruneProtectTokens {
			continue
		}
		prunedTokens += size
		indexes = append(indexes, i)
	}
	if prunedTokens <= toolResultPruneMinimumTokens {
		return messages
	}

	pruned := make([]providers.ChatMessage, len(messages))
	copy(pruned, messages)
	for _, i := range indexes {
		pruned[i].Content = summarizePrunedToolResult(pruned[i])
	}

	return pruned
}

func summarizePrunedToolResult(msg providers.ChatMessage) string {
	name := strings.TrimSpace(msg.Name)
	if name == "" {
		name = "unknown tool"
	}
	return fmt.Sprintf("[Old %s result omitted during compact to save context. Original output was %d characters. Tool call ID: %s]", name, len(msg.Content), toolCallLabel(msg.ToolCallID))
}

func toolCallLabel(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "unknown"
	}
	return id
}

// compactInstructionPrompt is the framing wuu wraps every
// summarization request in. It keeps the existing single-user-message
// compact flow, but tightens the handoff discipline so the generated
// summary can safely serve as the only continuation context.
//
// The load-bearing requirements are: no tool calls, no hidden reasoning, and
// enough concrete state for the next turn to continue without the deleted
// history. Keep this prompt concise: it runs only after the normal request is
// near or over the context limit.
const compactInstructionPrompt = `You are summarizing a coding-agent conversation to preserve context for continuing the work later.

This summary is used to resume after older messages are removed. Include enough detail that the next agent can continue without asking the user to repeat context or guessing missing state.

Respond with text only. Do not call tools, request tool use, or name a tool as part of your own process. Tool calls will fail this task.

Respond with a markdown summary only. Do not include an analysis block, hidden reasoning, XML tags, preamble, or outro. Keep the summary terse but complete enough to continue the task.

Cover these sections:

## Goal
- Current user goal and success criteria

## Constraints & Preferences
- User instructions, project rules, style constraints, and important non-goals

## Progress
- Done
- In progress
- Blocked

## Key Decisions
- Product or engineering decisions already made and why

## Next Steps
- Concrete next actions in order

## Critical Context
- Exact errors, commands, logs, IDs, state, assumptions, and anything easy to lose

## Relevant Files
- Exact paths and why each matters

Tone: brief a teammate taking over mid-task. Include enough detail that they can continue without asking the user to repeat anything. No filler. No emojis.
`

// buildSummaryPrompt is the inner formatting helper extracted so the
// retry loop above doesn't have to duplicate the string-builder code.
func buildSummaryPrompt(toSummarize []providers.ChatMessage, previousSummary string) string {
	var b strings.Builder
	b.WriteString(compactInstructionPrompt)
	previousSummary = strings.TrimSpace(previousSummary)
	if previousSummary != "" {
		b.WriteString("\n--- Previous anchored summary ---\n\n")
		b.WriteString(previousSummary)
		b.WriteString("\n\nUpdate the anchored summary above using the new conversation below. Preserve details that are still true, remove stale details, and merge new facts. Return one complete replacement summary with the required sections.\n\n")
	}
	b.WriteString("--- Conversation to summarize ---\n\n")
	for _, msg := range toSummarize {
		fmt.Fprintf(&b, "[%s]: %s\n", msg.Role, truncate(msg.Content, 500))
		for _, image := range msg.Images {
			mediaType := strings.TrimSpace(image.MediaType)
			if mediaType == "" {
				mediaType = "image"
			}
			fmt.Fprintf(&b, "  [image omitted: %s, %d base64 characters]\n", mediaType, len(strings.TrimSpace(image.Data)))
		}
		for _, tc := range msg.ToolCalls {
			fmt.Fprintf(&b, "  -> tool_call: %s(%s)\n", tc.Name, truncate(tc.Arguments, 200))
		}
		if msg.ToolCallID != "" {
			fmt.Fprintf(&b, "  (result for tool call %s)\n", msg.ToolCallID)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || // CJK Unified Ideographs
		(r >= 0x3400 && r <= 0x4DBF) || // CJK Extension A
		(r >= 0x3000 && r <= 0x303F) || // CJK Symbols
		(r >= 0x3040 && r <= 0x309F) || // Hiragana
		(r >= 0x30A0 && r <= 0x30FF) || // Katakana
		(r >= 0xAC00 && r <= 0xD7AF) // Hangul
}

func truncate(s string, maxLen int) string {
	return stringutil.Truncate(s, maxLen, "...")
}
