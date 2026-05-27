package compact

import (
	"context"
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
const toolResultPruneThresholdChars = 400

// maxCompactOutputChars caps the summarization output to approximately
// 20K tokens (~4 chars per token). Aligned with Claude Code's
// MAX_OUTPUT_TOKENS_FOR_SUMMARY and Codex's COMPACT_USER_MESSAGE_MAX_TOKENS.
// Without this cap, the summary itself can consume a large portion of
// the context window, defeating the purpose of compaction.
const maxCompactOutputChars = 80_000

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

// imageTokenEstimate is the fixed token budget for an image content
// block. The real formula is (width×height)/750, but images are
// resized to fit within provider limits, and the actual count comes
// from the API response. 2000 is the conservative heuristic both
// Claude Code and Codex CLI use.
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
		// Images: fixed estimate per image block.
		total += len(msg.Images) * imageTokenEstimate
	}
	if hasTools {
		total += toolDefinitionOverhead
	}
	return total
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

// compactTailMaxTokens caps the recent raw history kept after compaction.
// Mature agents avoid a fixed "last N messages" tail: Codex keeps recent user
// messages under a token budget, and Claude Code budgets compaction headroom by
// tokens. 20K is the shared practical scale those systems use for compact
// summary/tail budgets.
const compactTailMaxTokens = 20_000

// compactTailContextFraction keeps the raw tail small relative to the target
// model window so the generated summary and post-compact context still have
// room. The tail selector also keeps complete user-anchored turns and tool
// chains, so this is a soft budget rather than a hard truncation point.
const compactTailContextFraction = 0.15

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
	if len(messages) <= 2 {
		return messages, nil // nothing to compact
	}
	ctx, cancel := withCompactTimeout(ctx)
	defer cancel()

	systemEnd := leadingSystemEnd(messages)
	systemPrefix := append([]providers.ChatMessage(nil), messages[:systemEnd]...)
	conversation := messages[systemEnd:]
	if len(conversation) <= 2 {
		return messages, nil
	}

	keepStart := compactKeepStart(conversation, compactTailBudget(model, maxContextTokens))
	if keepStart <= 0 {
		return messages, nil
	}

	toSummarize := pruneOldToolResults(conversation[:keepStart])
	toKeep := conversation[keepStart:]

	for attempt := 0; ; attempt++ {
		summaryInput := buildSummaryPrompt(toSummarize)
		summaryReq := providers.ChatRequest{
			Model: model,
			Messages: []providers.ChatMessage{
				{Role: "system", Content: "You summarize coding-agent conversations for context compaction. Follow the user's required format exactly. Do not call tools."},
				{Role: "user", Content: summaryInput},
			},
			Temperature: 0.3,
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

		compacted := append([]providers.ChatMessage(nil), systemPrefix...)
		compacted = append(compacted, providers.ChatMessage{Role: "system", Content: BuildSummaryContent(summary)})
		compacted = append(compacted, toKeep...)
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
	stopReason := ""
	truncated := false
	done := false
	for event := range ch {
		switch event.Type {
		case providers.EventContentDelta:
			content.WriteString(event.Content)
		case providers.EventError:
			if event.Error != nil {
				return providers.ChatResponse{}, event.Error
			}
			return providers.ChatResponse{}, fmt.Errorf("compact summary stream error")
		case providers.EventDone:
			done = true
			if event.Usage != nil {
				usage = event.Usage
			}
			stopReason = event.StopReason
			truncated = event.Truncated
		}
	}
	if !done {
		return providers.ChatResponse{}, providers.NewIncompleteStreamError("compact summary stream closed before done")
	}
	return providers.ChatResponse{
		Content:    content.String(),
		Usage:      usage,
		StopReason: stopReason,
		Truncated:  truncated,
	}, nil
}

func leadingSystemEnd(messages []providers.ChatMessage) int {
	i := 0
	for i < len(messages) && strings.EqualFold(messages[i].Role, "system") {
		i++
	}
	return i
}

// FormatSummary turns the model's compact response into the content that will
// be replayed later. The prompt asks for an <analysis> drafting block followed
// by <summary>; only the summary belongs in future model context.
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
	window := maxContextTokens
	if window <= 0 {
		window = providers.ContextWindowFor(model)
	}
	budget := int(float64(window) * compactTailContextFraction)
	if budget <= 0 || budget > compactTailMaxTokens {
		return compactTailMaxTokens
	}
	return budget
}

// compactKeepStart returns the index where the un-compacted tail should begin.
// It keeps the latest user-anchored turn, then expands backward by complete
// user turns while the tail remains under the token budget. Long single-turn
// tool runs only have one user message at the front, so those fall back to a
// token-budgeted raw tail instead of refusing to compact.
func compactKeepStart(messages []providers.ChatMessage, tailBudgetTokens int) int {
	if len(messages) <= 1 {
		return 0
	}
	if tailBudgetTokens <= 0 {
		tailBudgetTokens = compactTailMaxTokens
	}

	start := lastUserMessageIndex(messages)
	if start < 0 {
		start = compactFallbackTailStart(messages, tailBudgetTokens)
		return adjustToolBoundary(messages, start)
	}
	if start == 0 {
		start = compactFallbackTailStart(messages, tailBudgetTokens)
		return adjustToolBoundary(messages, start)
	}

	for {
		prev := previousUserMessageIndex(messages, start-1)
		if prev <= 0 {
			break
		}
		if EstimateMessagesTokens(messages[prev:]) > tailBudgetTokens {
			break
		}
		start = prev
	}
	return start
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

func previousUserMessageIndex(messages []providers.ChatMessage, before int) int {
	if before >= len(messages) {
		before = len(messages) - 1
	}
	for i := before; i >= 0; i-- {
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

func pruneOldToolResults(messages []providers.ChatMessage) []providers.ChatMessage {
	if len(messages) == 0 {
		return nil
	}

	pruned := make([]providers.ChatMessage, len(messages))
	copy(pruned, messages)

	for i := range pruned {
		if pruned[i].Role != "tool" {
			continue
		}
		if len(pruned[i].Content) < toolResultPruneThresholdChars {
			continue
		}
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
// The load-bearing requirements are:
//   - no tool calls at all
//   - the response must start with <analysis> and then <summary>
//   - the summary must preserve enough detail to continue the work
//     without access to the pre-compact conversation
const compactInstructionPrompt = `You are summarizing a coding-agent conversation to preserve context for continuing the work later.

CRITICAL: This summary will be the ONLY context available when the conversation resumes. Assume every previous message is about to be deleted. Be thorough — losing a detail here means the next agent will have to ask the user (or guess) to recover it.

CRITICAL: Respond with text only. Do NOT call any tools. Do NOT use read_file, grep, glob, run_shell, or any other tool. Tool calls will fail this task.

Your response must contain exactly two top-level blocks, in this order:
1. <analysis>...</analysis>
2. <summary>...</summary>

Use the <analysis> block to think through the conversation chronologically and make sure you did not miss anything load-bearing.

In the <summary> block, cover at least these sections:

## User Intent
- The user's exact request, constraints, preferences, and success criteria
- Any course corrections or explicit feedback from the user

## Technical Concepts
- Important technical concepts, design decisions, libraries, frameworks, and conventions

## Files and Code
- Files modified, with a one-line description of each change
- Files read or analyzed and why they mattered
- Important code snippets, function signatures, data shapes, and exact paths the next agent should inspect
- File paths and line numbers for code locations the next agent should jump to

## Errors and Fixes
- Errors encountered, what caused them, and how they were fixed or investigated
- Commands that failed and what the failure looked like
- Commands that worked and what they verified

## All User Messages
- Every user message that is not just a tool result, including short clarifications and corrections

## Unfinished Work
- Pending tasks, open questions, blockers, and assumptions that still matter

## Current Work
- What was being worked on immediately before this summary request
- How far along it is

## Next Step
- The next concrete step that should happen, written so another agent can continue directly

Tone: brief a teammate taking over mid-task. Include enough detail that they can continue without asking the user to repeat anything. No filler. No emojis.

--- Conversation to summarize ---

`

// buildSummaryPrompt is the inner formatting helper extracted so the
// retry loop above doesn't have to duplicate the string-builder code.
func buildSummaryPrompt(toSummarize []providers.ChatMessage) string {
	var b strings.Builder
	b.WriteString(compactInstructionPrompt)
	for _, msg := range toSummarize {
		fmt.Fprintf(&b, "[%s]: %s\n", msg.Role, truncate(msg.Content, 500))
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
