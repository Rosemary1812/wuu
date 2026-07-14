package compact

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/contextbudget"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/stringutil"
)

// Compact can be the only recovery path after a provider context overflow.
// Large histories may require several summary requests, so the default must be
// long enough for recovery while still bounding a stuck compaction.
const defaultCompactTimeout = 20 * time.Minute
const (
	toolResultPruneProtectTokens = 40_000
	toolResultPruneMinimumTokens = 20_000
	// pruneProtectedTurns is the number of recent user-anchored turns
	// whose tool results are never pruned by the standalone prune pass.
	// Aligned with OpenCode V1's "if (turns < 2) continue" guard.
	pruneProtectedTurns = 2
)

var protectedToolResults = map[string]bool{
	"skill": true,
}

// recallableTools lists tools whose results are deterministic and can be
// re-obtained by re-calling the tool. Pruned results for these tools
// include a re-call hint in the placeholder so the model knows it can
// retrieve the current content by re-running the tool.
var recallableTools = map[string]bool{
	"read_file":  true,
	"grep":       true,
	"glob":       true,
	"list_files": true,
}

// maxCompactOutputChars caps the summarization output to approximately
// 20K tokens (~4 chars per token).
// Without this cap, the summary itself can consume a large portion of
// the context window, defeating the purpose of compaction.
const maxCompactOutputChars = 80_000
const (
	compactSummaryMaxTokens       = 4096
	compactSummaryInputMaxTokens  = 80_000
	compactSummaryInputMinTokens  = 4_000
	compactSummaryInputFraction   = 0.5
	compactPromptContentMaxChars  = 500
	compactPromptToolArgsMaxChars = 200
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
	return contextbudget.EstimateTokens(text)
}

// EstimateMessagesTokens estimates total tokens for a message list.
// Counts content, reasoning, tool calls (name + arguments + envelope),
// images, and per-message overhead. Slightly pessimistic so proactive
// compact fires before the hard overflow.
func EstimateMessagesTokens(messages []providers.ChatMessage) int {
	return contextbudget.EstimateMessagesTokens(messages)
}

// ShouldCompact returns true if messages exceed the threshold.
func ShouldCompact(messages []providers.ChatMessage, maxContextTokens int) bool {
	return contextbudget.ShouldCompact(messages, maxContextTokens)
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

// compactDefaultKeepRecentTokens is the recent raw history budget kept after
// compaction. It follows Pi's provider-neutral default; users can override it
// through agent.compact_keep_recent_tokens.
const compactDefaultKeepRecentTokens = 20_000

// compactTailAllFitFallbackTurns avoids no-op compaction when the configured
// tail budget is larger than the whole conversation. Normal budgeted selection
// can retain more turns; this only chooses a minimal recent tail when no budget
// boundary was reached.
const compactTailAllFitFallbackTurns = 2

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
	KeepRecentTokens    int
}

// CanCompactWithBudget reports whether CompactWithBudget can replace at least
// one real conversation message with a summary. It intentionally mirrors the
// production boundary selection so callers can skip no-op auto-compact passes
// before emitting user-facing state or paying for a summary request.
func CanCompactWithBudget(messages []providers.ChatMessage, model string, budget Budget) bool {
	_, ok := planCompaction(messages, model, budget)
	return ok
}

func CompactWithBudget(ctx context.Context, messages []providers.ChatMessage, client providers.Client, model string, budget Budget) ([]providers.ChatMessage, error) {
	plan, ok := planCompaction(messages, model, budget)
	if !ok {
		return messages, nil
	}
	ctx, cancel := withCompactTimeout(ctx)
	defer cancel()

	toSummarize := pruneOldToolResults(plan.conversation[:plan.keepStart])
	toKeep := plan.conversation[plan.keepStart:]

	summary, err := summarizeCompactHistory(ctx, client, model, budget, toSummarize, plan.previousSummary)
	if err != nil {
		return messages, err
	}
	if summary == "" {
		return messages, nil
	}

	summaryDiscoveredTools := providers.MergeLoadableToolDefinitions(
		plan.previousSummaryDiscoveredTools,
		providers.DiscoveredToolsFromMessages(plan.conversation[:plan.keepStart]),
	)
	compacted := providers.CloneChatMessages(plan.systemPrefix)
	compacted = append(compacted, providers.ChatMessage{
		Role:            "system",
		Content:         BuildSummaryContent(summary),
		DiscoveredTools: summaryDiscoveredTools,
	})
	compacted = append(compacted, providers.CloneChatMessages(toKeep)...)
	return compacted, nil
}

type compactionPlan struct {
	systemPrefix                   []providers.ChatMessage
	previousSummary                string
	previousSummaryDiscoveredTools []providers.LoadableToolDefinition
	conversation                   []providers.ChatMessage
	keepStart                      int
}

func planCompaction(messages []providers.ChatMessage, model string, budget Budget) (compactionPlan, bool) {
	if len(messages) <= 2 {
		return compactionPlan{}, false
	}
	systemPrefix, previousSummary, previousSummaryDiscoveredTools, conversation := splitLeadingSystemMessages(messages)
	if len(conversation) <= 2 {
		return compactionPlan{}, false
	}

	conversationForCompact := stripHistoricalImages(conversation)
	keepStart := compactKeepStart(conversationForCompact, compactTailBudgetForBudget(model, budget))
	if keepStart <= 0 || keepStart > len(conversationForCompact) {
		return compactionPlan{}, false
	}

	return compactionPlan{
		systemPrefix:                   systemPrefix,
		previousSummary:                previousSummary,
		previousSummaryDiscoveredTools: previousSummaryDiscoveredTools,
		conversation:                   conversationForCompact,
		keepStart:                      keepStart,
	}, true
}

func summarizeCompactHistory(ctx context.Context, client providers.Client, model string, budget Budget, messages []providers.ChatMessage, previousSummary string) (string, error) {
	if len(messages) == 0 {
		return strings.TrimSpace(previousSummary), nil
	}
	inputBudget := compactSummaryInputBudgetForBudget(model, budget)
	summary := strings.TrimSpace(previousSummary)
	remaining := messages
	for len(remaining) > 0 {
		n := compactSummaryChunkSize(remaining, summary, inputBudget)
		if n <= 0 {
			n = 1
		}
		chunk := remaining[:n]
		next, err := summarizeCompactChunk(ctx, client, model, chunk, summary)
		if err != nil {
			return "", err
		}
		summary = limitSummaryOutput(FormatSummary(next))
		remaining = remaining[n:]
	}
	return summary, nil
}

func limitSummaryOutput(summary string) string {
	if len(summary) <= maxCompactOutputChars {
		return summary
	}
	cut := maxCompactOutputChars
	for cut > 0 && summary[cut-1]&0xC0 == 0x80 {
		cut--
	}
	return summary[:cut]
}

func summarizeCompactChunk(ctx context.Context, client providers.Client, model string, messages []providers.ChatMessage, previousSummary string) (string, error) {
	toSummarize := messages
	for attempt := 0; ; attempt++ {
		summaryReq := compactSummaryRequest(model, buildSummaryPrompt(toSummarize, previousSummary))
		resp, err := summarizeCompact(ctx, client, summaryReq)
		if err != nil {
			// If the summary request itself overflowed the model's
			// context window, drop the oldest message from this chunk and try
			// again. The chunker prevents normal huge histories from reaching
			// this path; this remains a backstop for provider-specific counting.
			if providers.IsContextOverflow(err) && attempt < maxCompactRetries && len(toSummarize) > 1 {
				toSummarize = toSummarize[1:]
				continue
			}
			return "", fmt.Errorf("compact summary failed: %w", err)
		}
		return resp.Content, nil
	}
}

func compactSummaryRequest(model, prompt string) providers.ChatRequest {
	return providers.ChatRequest{
		Model:     model,
		Operation: providers.NewInferenceOperation(providers.InferenceOperationCompaction, providers.InferenceProfileContinuationCritical),
		Messages: []providers.ChatMessage{
			{Role: "system", Content: "You summarize coding-agent conversations for context compaction. Follow the user's required format exactly. Do not call tools."},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.3,
		MaxTokens:   compactSummaryMaxTokens,
		ProviderOptions: map[string]any{
			"textVerbosity": "low",
		},
	}
}

func summarizeCompact(ctx context.Context, client providers.Client, req providers.ChatRequest) (providers.ChatResponse, error) {
	return streamCompactSummary(ctx, providers.AdaptStreamClient(client), req)
}

func streamCompactSummary(ctx context.Context, client providers.StreamClient, req providers.ChatRequest) (providers.ChatResponse, error) {
	req.Operation = providers.EnsureInferenceOperation(req.Operation, providers.InferenceOperationCompaction, providers.InferenceProfileContinuationCritical)
	var err error
	req, err = providers.EnsureInferenceExecutionContext(ctx, req, providers.InferenceOperationCompaction, providers.InferenceProfileContinuationCritical)
	if err != nil {
		return providers.ChatResponse{}, err
	}
	reliableClient := providers.NewReliableStreamClient(client, nil)
	ch, err := reliableClient.StreamChat(ctx, req)
	if err != nil {
		return providers.ChatResponse{}, finishCompactFailure(req.Execution, err)
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
		case providers.EventLifecycle:
			if event.Lifecycle != nil && event.Lifecycle.Phase == providers.StreamPhaseReconnecting && event.Lifecycle.ResetPartial {
				content.Reset()
				usage = nil
				finishReason = ""
				stopReason = ""
				truncated = false
				done = false
			}
		case providers.EventError:
			if event.Error != nil {
				if resp, ok, ferr := recoverCompactStream(ctx, client, req, event.Error); ok {
					return resp, ferr
				}
				return providers.ChatResponse{}, finishCompactFailure(req.Execution, event.Error)
			}
			err := errors.New("compact summary stream error")
			return providers.ChatResponse{}, finishCompactFailure(req.Execution, err)
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
	if err := ctx.Err(); err != nil {
		return providers.ChatResponse{}, finishCompactFailure(req.Execution, err)
	}
	if !done {
		err := providers.NewIncompleteStreamError("compact summary stream closed before done")
		if resp, ok, ferr := recoverCompactStream(ctx, client, req, err); ok {
			return resp, ferr
		}
		return providers.ChatResponse{}, finishCompactFailure(req.Execution, err)
	}
	if finishReason == "" {
		finishReason = providers.NormalizeFinishReason(stopReason, truncated, false)
	}
	resp := providers.ChatResponse{
		Content:      content.String(),
		Usage:        usage,
		FinishReason: finishReason,
		StopReason:   stopReason,
		Truncated:    truncated,
	}
	if err := validateCompactResponse(resp); err != nil {
		return providers.ChatResponse{}, finishCompactFailure(req.Execution, err)
	}
	if err := req.Execution.Complete(providers.InferenceOutcomeSucceeded, providers.NormalizedFailure{}); err != nil {
		return providers.ChatResponse{}, err
	}
	return resp, nil
}

func recoverCompactStream(ctx context.Context, client providers.Client, req providers.ChatRequest, streamErr error) (providers.ChatResponse, bool, error) {
	if err := ctx.Err(); err != nil {
		return providers.ChatResponse{}, true, finishCompactFailure(req.Execution, err)
	}
	if !isIncompleteCompactStream(streamErr) {
		return providers.ChatResponse{}, false, nil
	}
	priorAttempt := req.Execution.LatestAttempt()
	nextAttempt, err := priorAttempt.PrepareRecoveryAttempt(ctx, providers.RecoveryPlan{
		Action: providers.RecoverySwitchTransport,
		Reason: "compaction stream incomplete",
	}, time.Time{})
	if err != nil {
		wrapped := fmt.Errorf("record compact fallback: %w", err)
		return providers.ChatResponse{}, true, finishCompactFailure(req.Execution, wrapped)
	}
	req.Attempt = nextAttempt
	resp, executed, err := providers.ExecuteChatAttempt(ctx, client, req, req.Operation.Kind, req.Operation.WorkloadProfile)
	if err != nil {
		execution := executed.Execution
		if execution == nil {
			execution = req.Execution
		}
		wrapped := fmt.Errorf("compact summary stream incomplete and chat fallback failed: %w", err)
		return providers.ChatResponse{}, true, finishCompactFailure(execution, wrapped)
	}
	if err := validateCompactResponse(resp); err != nil {
		return providers.ChatResponse{}, true, finishCompactFailure(executed.Execution, err)
	}
	if err := executed.Execution.Complete(providers.InferenceOutcomeSucceeded, providers.NormalizedFailure{}); err != nil {
		return providers.ChatResponse{}, true, err
	}
	return resp, true, nil
}

func finishCompactFailure(execution *providers.InferenceExecution, err error) error {
	if err == nil || execution == nil {
		return err
	}
	failure := providers.NormalizeFailure(err)
	outcome := providers.InferenceOutcomeFailed
	if failure.Category == providers.FailureCanceled || failure.Category == providers.FailureDeadline {
		outcome = providers.InferenceOutcomeCanceled
	}
	if journalErr := execution.Complete(outcome, failure); journalErr != nil {
		return errors.Join(err, journalErr)
	}
	return err
}

func validateCompactResponse(resp providers.ChatResponse) error {
	if strings.TrimSpace(resp.Content) == "" {
		return errors.New("compact summary was empty")
	}
	finish := resp.FinishReason
	if finish == "" {
		finish = providers.NormalizeFinishReason(resp.StopReason, resp.Truncated, len(resp.ToolCalls) > 0)
	}
	if resp.Truncated || finish == providers.FinishReasonLength {
		return errors.New("compact summary reached the output limit before completion")
	}
	return nil
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

func compactSummaryInputBudgetForBudget(model string, budget Budget) int {
	usable := compactUsableInputWindow(model, budget)
	if usable <= 0 {
		return compactSummaryInputMinTokens
	}
	inputBudget := int(float64(usable) * compactSummaryInputFraction)
	if inputBudget > compactSummaryInputMaxTokens {
		return compactSummaryInputMaxTokens
	}
	if inputBudget < compactSummaryInputMinTokens {
		return min(usable, compactSummaryInputMinTokens)
	}
	return inputBudget
}

func compactTailBudgetForBudget(model string, budget Budget) int {
	tailBudget := compactDefaultKeepRecentTokens
	if budget.KeepRecentTokens > 0 {
		tailBudget = budget.KeepRecentTokens
	}
	if usable := compactTailUsableWindow(model, budget); usable > 0 {
		maxTail := int(float64(usable) * compactSummaryInputFraction)
		if maxTail <= 0 {
			maxTail = usable
		}
		if tailBudget > maxTail {
			return maxTail
		}
	}
	return tailBudget
}

func compactTailUsableWindow(model string, budget Budget) int {
	usable := compactUsableInputWindow(model, budget)
	if usable > 0 {
		return usable
	}
	if budget.InputTokens > 0 {
		return budget.InputTokens
	}
	return budget.ContextTokens
}

func compactUsableInputWindow(model string, budget Budget) int {
	window := budget.ContextTokens
	if window <= 0 {
		return 0
	}
	outputReserve := budget.OutputReserveTokens
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

func compactSummaryChunkSize(messages []providers.ChatMessage, previousSummary string, inputBudget int) int {
	if len(messages) <= 1 {
		return len(messages)
	}
	if inputBudget <= 0 {
		inputBudget = compactSummaryInputMinTokens
	}
	total := EstimateTokens(buildSummaryPrompt(nil, previousSummary))
	keep := 0
	for keep < len(messages) {
		next := compactSummaryPromptMessageTokens(messages[keep])
		if keep > 0 && total+next > inputBudget {
			break
		}
		total += next
		keep++
	}
	if keep == 0 {
		return 1
	}
	return keep
}

func compactSummaryPromptMessageTokens(msg providers.ChatMessage) int {
	var b strings.Builder
	writeSummaryPromptMessage(&b, msg)
	return EstimateTokens(b.String())
}

type compactTurn struct {
	start int
	end   int
}

// compactKeepStart returns the index where the un-compacted tail should begin.
// It keeps recent user-anchored turns within the token budget instead of
// imposing a fixed turn count, matching Pi's compaction boundary shape. Long
// single-turn tool runs only have one user message at the front, so those fall
// back to a token-budgeted raw tail instead of refusing to compact. A return
// value equal to len(messages) means summarize everything and keep no raw tail;
// -1 means no compaction.
func compactKeepStart(messages []providers.ChatMessage, tailBudgetTokens int) int {
	if len(messages) <= 1 {
		return -1
	}
	if tailBudgetTokens <= 0 {
		tailBudgetTokens = compactDefaultKeepRecentTokens
	}

	turns := compactTurns(messages)
	if len(turns) == 0 {
		return compactTailStartOrSummaryAll(messages, compactFallbackTailStart(messages, tailBudgetTokens))
	}
	if len(turns) == 1 && turns[0].start == 0 {
		return compactTailStartOrSummaryAll(messages, compactFallbackTailStart(messages, tailBudgetTokens))
	}

	total := 0
	keepStart := -1
	for i := len(turns) - 1; i >= 0; i-- {
		turn := turns[i]
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
		if len(turns) > compactTailAllFitFallbackTurns {
			return adjustToolBoundary(messages, turns[len(turns)-compactTailAllFitFallbackTurns].start)
		}
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

// PruneToolResults is a standalone, non-LLM prune pass that truncates
// old tool-result content to save context before a provider request.
// It is non-destructive: the input slice is never modified, and a new
// slice with pruned content is returned only when pruning actually
// removes content; otherwise the original slice is returned as-is.
//
// Recent turns (the last pruneProtectedTurns user-anchored turns) are
// always preserved. Beyond that, the most recent tool-result tokens up
// to toolResultPruneProtectTokens are protected; older tool results are
// replaced with a truncated placeholder. Pruning is skipped entirely
// when the prunable amount is below toolResultPruneMinimumTokens.
//
// The original tool-result content remains in the live history slice
// for session durability and future archive retrieval; only the request
// projection is truncated.
func PruneToolResults(messages []providers.ChatMessage) []providers.ChatMessage {
	if len(messages) == 0 {
		return nil
	}

	total := 0
	prunedTokens := 0
	turnsSeen := 0
	var indexes []int
	for i := len(messages) - 1; i >= 0; i-- {
		// Count user messages as turn boundaries going backwards.
		if isPruneUserTurnBoundary(messages[i]) {
			turnsSeen++
		}
		// Skip tool results in the last pruneProtectedTurns turns.
		if turnsSeen < pruneProtectedTurns {
			continue
		}
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

func isPruneUserTurnBoundary(msg providers.ChatMessage) bool {
	return strings.EqualFold(msg.Role, "user") && !msg.Hidden && !IsInternalContextMessage(msg)
}

func summarizePrunedToolResult(msg providers.ChatMessage) string {
	name := strings.TrimSpace(msg.Name)
	if name == "" {
		name = "unknown tool"
	}
	label := toolCallLabel(msg.ToolCallID)
	if recallableTools[name] {
		return fmt.Sprintf("[Pruned %s result. Original: %d characters. Tool call ID: %s. Re-run %s to retrieve current content.]", name, len(msg.Content), label, name)
	}
	return fmt.Sprintf("[Pruned %s result. Original: %d characters. Tool call ID: %s. This output is from a completed operation and may not be reproducible.]", name, len(msg.Content), label)
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

## External State
- Files changed, commands/processes run, browser state, remote systems touched, and any external side effects that remain current

## Verification State
- Checks passed, failed, skipped, or still needed; include exact failure status without overstating confidence

## Key Decisions
- Product or engineering decisions already made and why

## Next Steps
- Concrete next actions in order

## Critical Context
- Exact errors, commands, logs, IDs, state, assumptions, and anything easy to lose

## Evidence Pointers
- Exact files, commands, logs, result ids, artifact paths, screenshots, or other evidence needed to resume

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
		writeSummaryPromptMessage(&b, msg)
	}
	return b.String()
}

func writeSummaryPromptMessage(b *strings.Builder, msg providers.ChatMessage) {
	fmt.Fprintf(b, "[%s]: %s\n", msg.Role, truncate(msg.Content, compactPromptContentMaxChars))
	for _, image := range msg.Images {
		mediaType := strings.TrimSpace(image.MediaType)
		if mediaType == "" {
			mediaType = "image"
		}
		fmt.Fprintf(b, "  [image omitted: %s, %d base64 characters]\n", mediaType, len(strings.TrimSpace(image.Data)))
	}
	for _, tc := range msg.ToolCalls {
		fmt.Fprintf(b, "  -> tool_call: %s(%s)\n", tc.Name, truncate(tc.Arguments, compactPromptToolArgsMaxChars))
	}
	if msg.ToolCallID != "" {
		fmt.Fprintf(b, "  (result for tool call %s)\n", msg.ToolCallID)
	}
	b.WriteString("\n")
}

func truncate(s string, maxLen int) string {
	return stringutil.Truncate(s, maxLen, "...")
}
