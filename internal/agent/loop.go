package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/blueberrycongee/wuu/internal/compact"
	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/provideroptions"
	"github.com/blueberrycongee/wuu/internal/providers"
)

// EmptyAnswerError is returned when the model completes a turn without
// producing any text content or tool calls. StopReason carries the
// provider's finish signal (e.g. "stop", "end_turn") when one was
// received, or "" when the stream ended without a normal stop — the
// latter usually indicates a proxy/compatibility issue rather than
// intentional model behaviour.
type EmptyAnswerError struct {
	StopReason string
}

func (e *EmptyAnswerError) Error() string {
	if e.StopReason != "" {
		return fmt.Sprintf("model returned empty answer (stop_reason=%s)", e.StopReason)
	}
	return "model returned empty answer"
}

// IsEmptyAnswer reports whether err (or any error in its chain) is an
// EmptyAnswerError. Callers use this to distinguish empty-content
// failures from other fatal errors.
func IsEmptyAnswer(err error) bool {
	var target *EmptyAnswerError
	return errors.As(err, &target)
}

// RunToolLoop drives the shared multi-step tool-use loop both Runner
// and StreamRunner depend on. It is transport-agnostic: callers
// supply a Step that knows how to perform one model round-trip
// (Chat for Runner, SSE consumption for StreamRunner) and a
// LoopConfig describing the rest.
//
// Behavior:
//   - Loops up to cfg.MaxSteps rounds (0 = unlimited).
//   - On context-overflow errors from the step, calls cfg.Compact
//     once and re-issues the step. Subsequent overflows propagate.
//   - Output truncation is treated as a completed model response with
//     FinishReason=length. The caller/UI can surface that reason without
//     classifying the turn as a user interruption or transport failure.
//   - Executes any tool calls the model requested, recording results
//     as tool messages and (if configured) emitting them through
//     OnToolResult so callers can render them live.
//   - Returns the final assistant message + the slice of new messages
//     produced during this run + cumulative token usage.
//
// The history slice is treated as immutable; callers can reuse it.
func RunToolLoop(
	ctx context.Context,
	history []providers.ChatMessage,
	cfg LoopConfig,
	step Step,
) (LoopResult, error) {
	if step == nil {
		return LoopResult{}, errors.New("agent: step is required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return LoopResult{}, errors.New("agent: model is required")
	}

	messages := make([]providers.ChatMessage, len(history))
	copy(messages, history)
	// Transient context is request-only across runs, but append-only inside
	// a run so provider continuation deltas can match prior request prefixes.
	messages = filterTransientModelContextHistory(messages)
	startLen := len(messages)

	currentMaxTokens := cfg.DefaultMaxTokens // 0 = provider default

	var (
		totalIn, totalOut, totalCacheCreation, totalCacheRead int
		// Reactive auto-compact (overflow recovery) runs at most once
		// per Run; if a single compaction isn't enough, surfacing the
		// error is more honest than silently looping. Proactive compact
		// is limited to the first model round of a Run so tool results
		// produced mid-turn cannot rewrite the conversation out from
		// under the current task.
		overflowCompacted bool
		historyRewritten  bool
		// Tracks current context fill so we can decide whether to
		// proactively compact before the next round. Uses
		// response.usage as ground truth + delta estimation for
		// messages added since the last successful response.
		usage = cfg.UsageTracker
		// Request-only context produced by tool execution in the current
		// run. It is consumed by the next provider request and never enters
		// live or durable history.
		postToolContextSegments []ContextSegment
	)
	if usage == nil {
		usage = NewUsageTracker()
		// Without caller-owned cross-turn state, seed this run from a
		// local estimate so resumed long sessions can compact before
		// the first provider request.
		usage.RecordPendingMessages(messages)
	}
	threshold := proactiveCompactThreshold(cfg)
	appendMessage := func(msg providers.ChatMessage) {
		messages = append(messages, msg)
		if cfg.OnMessage != nil && !msg.Hidden {
			cfg.OnMessage(msg)
		}
	}

	for stepIdx := 0; cfg.MaxSteps == 0 || stepIdx < cfg.MaxSteps; stepIdx++ {
		if cfg.BeforeStep != nil {
			injected := cfg.BeforeStep()
			if len(injected) > 0 {
				for _, msg := range injected {
					appendMessage(msg)
				}
				usage.RecordPendingMessages(injected)
			}
		}
		if stepIdx == 0 && cfg.Compact != nil && threshold > 0 && usage.EstimateCurrent() >= threshold && canProactivelyCompact(messages, cfg) {
			before := usage.EstimateCurrent()
			msgsBefore := len(messages)
			compacted, cerr := cfg.Compact(ctx, messages)
			switch {
			case cerr != nil:
				emitCompactAttempt(cfg, CompactAttemptInfo{
					Reason:         CompactReasonProactive,
					Status:         CompactAttemptFailed,
					TokensBefore:   before,
					MessagesBefore: msgsBefore,
					Error:          cerr.Error(),
				})
			case compactChanged(messages, compacted):
				messages = compacted
				historyRewritten = true
				usage.Reset()
				usage.RecordPendingMessages(messages)
				emitCompactAttempt(cfg, CompactAttemptInfo{
					Reason:         CompactReasonProactive,
					Status:         CompactAttemptSucceeded,
					TokensBefore:   before,
					MessagesBefore: msgsBefore,
					MessagesAfter:  len(messages),
				})
				if cfg.OnCompact != nil {
					cfg.OnCompact(CompactInfo{
						Reason:         CompactReasonProactive,
						TokensBefore:   before,
						MessagesBefore: msgsBefore,
						MessagesAfter:  len(messages),
					})
				}
			default:
				emitCompactAttempt(cfg, CompactAttemptInfo{
					Reason:         CompactReasonProactive,
					Status:         CompactAttemptUnchanged,
					TokensBefore:   before,
					MessagesBefore: msgsBefore,
					MessagesAfter:  len(compacted),
				})
			}
		}
		if repaired, changed, nerr := repairLiveToolCallHistory(messages); nerr != nil {
			return LoopResult{
				NewMessages:         newMessagesForReturn(messages, startLen, historyRewritten),
				HistoryRewritten:    historyRewritten,
				InputTokens:         totalIn,
				OutputTokens:        totalOut,
				CacheCreationTokens: totalCacheCreation,
				CacheReadTokens:     totalCacheRead,
			}, nerr
		} else if changed {
			messages = repaired
			historyRewritten = true
			usage.Reset()
			usage.RecordPendingMessages(messages)
		}
		if toolSurfaceSupports(cfg.Tools, compact.InceptionToolName) {
			anchor := compact.BuildContextAnchorMessage(compact.NextContextAnchorID(messages))
			appendMessage(anchor)
			usage.RecordPendingMessages([]providers.ChatMessage{anchor})
		}
		requestSegments := requestContextSegments(cfg.BeforeRequestContext)
		if len(postToolContextSegments) > 0 {
			requestSegments = append(requestSegments, postToolContextSegments...)
			postToolContextSegments = nil
		}
		// Non-destructive tool-result prune: replace old tool results
		// with compact placeholders in the request only. The live
		// history retains full content for durability and future
		// retrieval.
		messagesForRequest := messages
		if cfg.ToolPrune {
			messagesForRequest = compact.PruneToolResults(messages)
		}
		assembly := assembleModelRequest(messagesForRequest, requestSegments)
		requestMessages := assembly.Messages
		cacheHint := buildCacheHint(requestMessages)
		applyPromptCacheKeyOverride(&cacheHint, cfg.PromptCacheKey)
		req := providers.ChatRequest{
			Model:                       cfg.Model,
			Messages:                    requestMessages,
			Temperature:                 cfg.Temperature,
			MaxTokens:                   currentMaxTokens,
			CacheHint:                   cacheHint,
			StepIndex:                   stepIdx,
			Effort:                      cfg.Effort,
			ProviderOptions:             provideroptions.Clone(cfg.ProviderOptions),
			NativeDeferredToolDiscovery: cfg.NativeDeferredToolDiscovery,
		}
		if cfg.Tools != nil {
			req.Tools = cfg.Tools.Definitions()
		}
		if cfg.OnRequestContext != nil {
			cfg.OnRequestContext(requestContextInfo(stepIdx, assembly, req.Tools, cacheHint, cfg.SystemPromptSections))
		}

		result, err := step.Execute(ctx, req)
		if err != nil {
			// Context window exceeded — try a one-shot compaction of
			// older history and re-issue. Provider-agnostic; the
			// CompactFn carries whatever client/model knowledge it
			// needs. This is the reactive backstop for the case
			// where our proactive estimate undercounted.
			if cfg.Compact != nil && providers.IsContextOverflow(err) && !overflowCompacted {
				overflowCompacted = true // gate first; never retry twice
				before := usage.EstimateCurrent()
				msgsBefore := len(messages)
				if compacted, cerr := cfg.Compact(ctx, messages); cerr == nil {
					if compactChanged(messages, compacted) {
						messages = compacted
						historyRewritten = true
						usage.Reset()
						usage.RecordPendingMessages(messages)
						emitCompactAttempt(cfg, CompactAttemptInfo{
							Reason:         CompactReasonOverflow,
							Status:         CompactAttemptSucceeded,
							TokensBefore:   before,
							MessagesBefore: msgsBefore,
							MessagesAfter:  len(messages),
						})
						if cfg.OnCompact != nil {
							cfg.OnCompact(CompactInfo{
								Reason:         CompactReasonOverflow,
								TokensBefore:   before,
								MessagesBefore: msgsBefore,
								MessagesAfter:  len(messages),
							})
						}
						continue
					}
					emitCompactAttempt(cfg, CompactAttemptInfo{
						Reason:         CompactReasonOverflow,
						Status:         CompactAttemptUnchanged,
						TokensBefore:   before,
						MessagesBefore: msgsBefore,
						MessagesAfter:  len(compacted),
					})
				} else {
					emitCompactAttempt(cfg, CompactAttemptInfo{
						Reason:         CompactReasonOverflow,
						Status:         CompactAttemptFailed,
						TokensBefore:   before,
						MessagesBefore: msgsBefore,
						Error:          cerr.Error(),
					})
				}
			}
			return LoopResult{
				NewMessages:         newMessagesForReturn(messages, startLen, historyRewritten),
				HistoryRewritten:    historyRewritten,
				InputTokens:         totalIn,
				OutputTokens:        totalOut,
				CacheCreationTokens: totalCacheCreation,
				CacheReadTokens:     totalCacheRead,
			}, err
		}

		if result.Usage != nil {
			totalIn += result.Usage.InputTokens
			totalOut += result.Usage.OutputTokens
			totalCacheCreation += result.Usage.CacheCreationTokens
			totalCacheRead += result.Usage.CacheReadTokens
			if cfg.OnUsage != nil {
				cfg.OnUsage(result.Usage.InputTokens, result.Usage.OutputTokens)
			}
			if cfg.OnTokenUsage != nil {
				cfg.OnTokenUsage(*result.Usage)
			}
			// Fold the precise per-call usage into the tracker, excluding
			// request-only context that will not persist into future history.
			usage.RecordResponseExcludingMessages(result.Usage, assembly.RequestOnlyMessages)
		}
		if err := providers.ValidateAssistantToolCalls(result.ToolCalls); err != nil {
			return LoopResult{
				NewMessages:         newMessagesForReturn(messages, startLen, historyRewritten),
				HistoryRewritten:    historyRewritten,
				InputTokens:         totalIn,
				OutputTokens:        totalOut,
				CacheCreationTokens: totalCacheCreation,
				CacheReadTokens:     totalCacheRead,
			}, fmt.Errorf("provider returned invalid tool_calls: %w", err)
		}

		finishReason := result.FinishReason
		if finishReason == "" {
			finishReason = providers.NormalizeFinishReason(result.StopReason, result.Truncated, len(result.ToolCalls) > 0)
		}

		assistant := providers.ChatMessage{
			Role:              "assistant",
			Content:           result.Content,
			Phase:             result.Phase,
			ProviderItemID:    result.ProviderItemID,
			ProviderItemModel: result.ProviderItemModel,
			ReasoningContent:  result.ReasoningContent,
			ReasoningBlocks:   cloneReasoningBlocks(result.ReasoningBlocks),
			ToolCalls:         result.ToolCalls,
			FinishReason:      finishReason,
			StopReason:        result.StopReason,
			Truncated:         result.Truncated,
		}
		if shouldPersistAssistantMessage(assistant) {
			appendMessage(assistant)
		}

		// No tool calls → model is done. Return content plus finish metadata.
		if len(result.ToolCalls) == 0 {
			finalContent := result.Content
			if strings.TrimSpace(finalContent) == "" {
				if isLegitimateEmptyCompletion(finishReason, result.StopReason) {
					return LoopResult{
						Content:             "",
						NewMessages:         newMessagesForReturn(messages, startLen, historyRewritten),
						HistoryRewritten:    historyRewritten,
						InputTokens:         totalIn,
						OutputTokens:        totalOut,
						CacheCreationTokens: totalCacheCreation,
						CacheReadTokens:     totalCacheRead,
						FinishReason:        finishReason,
						StopReason:          result.StopReason,
						Truncated:           result.Truncated,
					}, nil
				}
				return LoopResult{
					NewMessages:         newMessagesForReturn(messages, startLen, historyRewritten),
					HistoryRewritten:    historyRewritten,
					InputTokens:         totalIn,
					OutputTokens:        totalOut,
					CacheCreationTokens: totalCacheCreation,
					CacheReadTokens:     totalCacheRead,
				}, &EmptyAnswerError{StopReason: result.StopReason}
			}
			return LoopResult{
				Content:             finalContent,
				NewMessages:         newMessagesForReturn(messages, startLen, historyRewritten),
				HistoryRewritten:    historyRewritten,
				InputTokens:         totalIn,
				OutputTokens:        totalOut,
				CacheCreationTokens: totalCacheCreation,
				CacheReadTokens:     totalCacheRead,
				FinishReason:        finishReason,
				StopReason:          result.StopReason,
				Truncated:           result.Truncated,
			}, nil
		}

		if cfg.Tools == nil {
			return LoopResult{
				NewMessages:         newMessagesForReturn(messages, startLen, historyRewritten),
				HistoryRewritten:    historyRewritten,
				InputTokens:         totalIn,
				OutputTokens:        totalOut,
				CacheCreationTokens: totalCacheCreation,
				CacheReadTokens:     totalCacheRead,
			}, errors.New("model requested tools but none are configured")
		}

		// Execute tool calls. Read-only tools that are concurrency-safe run in
		// parallel; write tools run serially.
		//
		// The tool's execution context carries the current `messages`
		// slice (via withHistory) so tools like spawn_agent can fork
		// from the parent agent's current history.
		toolCtx := withHistory(ctx, messages)
		toolRuntime := result.ToolRuntime
		if toolRuntime == nil {
			toolRuntime = NewTurnToolRuntime(cfg.Tools)
		}
		toolRuntime.SetStepIndex(stepIdx)
		orderedToolMessages := toolRuntime.ExecuteFinalCalls(toolCtx, result.ToolCalls, cfg.OnToolResult)
		postToolContextSegments = append(postToolContextSegments, toolRuntime.TakeRequestContextSegments()...)
		enforceAggregateResultBudget(orderedToolMessages)
		for _, toolMsg := range orderedToolMessages {
			appendMessage(toolMsg)
		}
		usage.RecordPendingMessages(orderedToolMessages)
		if cfg.PostToolRewrite != nil {
			before := usage.EstimateCurrent()
			msgsBefore := len(messages)
			reason := postToolRewriteCompactReason(orderedToolMessages)
			rewritten, changed, rerr := cfg.PostToolRewrite(ctx, providers.CloneChatMessages(messages), providers.CloneChatMessages(orderedToolMessages))
			if rerr != nil {
				return LoopResult{
					NewMessages:         newMessagesForReturn(messages, startLen, historyRewritten),
					HistoryRewritten:    historyRewritten,
					InputTokens:         totalIn,
					OutputTokens:        totalOut,
					CacheCreationTokens: totalCacheCreation,
					CacheReadTokens:     totalCacheRead,
				}, rerr
			}
			if changed && compactChanged(messages, rewritten) {
				attempt := CompactAttemptInfo{
					Reason:         reason,
					Status:         CompactAttemptSucceeded,
					TokensBefore:   before,
					MessagesBefore: msgsBefore,
					MessagesAfter:  len(rewritten),
				}
				if reason == CompactReasonInception {
					if stats, ok, serr := compact.InceptionRewriteStatsFromToolMessages(messages, orderedToolMessages); serr == nil && ok {
						attempt.AnchorID = intPtr(stats.AnchorID)
						attempt.MessagesRemoved = stats.MessagesRemoved
						attempt.PreservedUserMessages = stats.PreservedUserMessages
						attempt.PreservedUserMessageBytes = stats.PreservedUserMessageBytes
						attempt.PreservedUserSuffixStartIndex = stats.PreservedUserSuffixStartIndex
						attempt.SummaryBytes = stats.SummaryBytes
					}
				}
				messages = rewritten
				historyRewritten = true
				usage.Reset()
				usage.RecordPendingMessages(messages)
				emitCompactAttempt(cfg, attempt)
				if cfg.OnCompact != nil {
					cfg.OnCompact(CompactInfo{
						Reason:         reason,
						TokensBefore:   before,
						MessagesBefore: msgsBefore,
						MessagesAfter:  len(messages),
					})
				}
			}
		}
	}

	return LoopResult{
		NewMessages:         newMessagesForReturn(messages, startLen, historyRewritten),
		HistoryRewritten:    historyRewritten,
		InputTokens:         totalIn,
		OutputTokens:        totalOut,
		CacheCreationTokens: totalCacheCreation,
		CacheReadTokens:     totalCacheRead,
	}, fmt.Errorf("max steps exceeded (%d)", cfg.MaxSteps)
}

func emitCompactAttempt(cfg LoopConfig, info CompactAttemptInfo) {
	if cfg.OnCompactAttempt != nil {
		cfg.OnCompactAttempt(info)
	}
}

func intPtr(v int) *int {
	return &v
}

func toolDefinitionsContain(executor ToolExecutor, name string) bool {
	if executor == nil || strings.TrimSpace(name) == "" {
		return false
	}
	for _, def := range executor.Definitions() {
		if strings.EqualFold(strings.TrimSpace(def.Name), name) {
			return true
		}
	}
	return false
}

func toolSurfaceSupports(executor ToolExecutor, name string) bool {
	if executor == nil || strings.TrimSpace(name) == "" {
		return false
	}
	if support, ok := executor.(ToolSupportProvider); ok {
		return support.SupportsTool(name)
	}
	return toolDefinitionsContain(executor, name)
}

func postToolRewriteCompactReason(toolMessages []providers.ChatMessage) CompactReason {
	for _, msg := range toolMessages {
		if strings.EqualFold(strings.TrimSpace(msg.Name), compact.InceptionToolName) {
			return CompactReasonInception
		}
	}
	return CompactReasonHelpMe
}

// proactiveCompactThreshold returns the absolute token count at which
// the loop should run a proactive compact pass, or 0 if proactive
// compact is disabled (caller didn't supply a window).
func proactiveCompactThreshold(cfg LoopConfig) int {
	if cfg.MaxContextTokens <= 0 && cfg.MaxInputTokens <= 0 {
		return 0
	}
	if cfg.CompactThresholdPct > 0 && cfg.CompactThresholdPct < 1 {
		return proactiveCompactPercentThreshold(cfg, cfg.CompactThresholdPct)
	}
	return proactiveCompactUsableWindow(cfg)
}

func proactiveCompactUsableWindow(cfg LoopConfig) int {
	maxOutput := compactMaxOutputTokens(cfg)
	if cfg.MaxInputTokens > 0 {
		reserve := maxOutput
		if reserve > 20_000 {
			reserve = 20_000
		}
		return max(0, cfg.MaxInputTokens-reserve)
	}
	if cfg.MaxContextTokens <= 0 {
		return 0
	}
	return max(0, cfg.MaxContextTokens-maxOutput)
}

func proactiveCompactPercentThreshold(cfg LoopConfig, pct float64) int {
	baseWindow := cfg.MaxContextTokens
	if baseWindow <= 0 || (cfg.MaxInputTokens > 0 && cfg.MaxInputTokens < baseWindow) {
		baseWindow = cfg.MaxInputTokens
	}
	threshold := int(float64(baseWindow) * pct)
	if cfg.MaxInputTokens > 0 {
		inputThreshold := int(float64(cfg.MaxInputTokens) * pct)
		if inputThreshold > 0 && inputThreshold < threshold {
			threshold = inputThreshold
		}
	}
	if cfg.MaxContextTokens > 0 {
		outputReserved := cfg.MaxContextTokens - compactMaxOutputTokens(cfg)
		if outputReserved > 0 && outputReserved < threshold {
			threshold = outputReserved
		}
	}
	return threshold
}

func compactMaxOutputTokens(cfg LoopConfig) int {
	reserve := cfg.OutputReserveTokens
	if reserve <= 0 {
		reserve = cfg.DefaultMaxTokens
	}
	if reserve <= 0 {
		reserve = providers.MaxOutputTokensFor(cfg.Model)
	}
	return reserve
}

func canProactivelyCompact(messages []providers.ChatMessage, cfg LoopConfig) bool {
	return compact.CanCompactWithBudget(messages, cfg.Model, compact.Budget{
		ContextTokens:       cfg.MaxContextTokens,
		InputTokens:         cfg.MaxInputTokens,
		OutputReserveTokens: cfg.OutputReserveTokens,
		KeepRecentTokens:    cfg.CompactKeepRecentTokens,
	})
}

func compactChanged(before, after []providers.ChatMessage) bool {
	if len(before) != len(after) {
		return true
	}
	return !reflect.DeepEqual(before, after)
}

// copyMessages returns an independent copy of msgs so callers can
// safely retain it after the loop's working slice is reused.
func copyMessages(msgs []providers.ChatMessage) []providers.ChatMessage {
	return providers.CloneChatMessages(msgs)
}

func repairLiveToolCallHistory(messages []providers.ChatMessage) ([]providers.ChatMessage, bool, error) {
	repaired, err := providers.RepairAndValidateToolCallHistory(messages)
	if err != nil {
		return nil, false, err
	}
	return repaired, !reflect.DeepEqual(repaired, messages), nil
}

func newMessagesForReturn(messages []providers.ChatMessage, startLen int, historyRewritten bool) []providers.ChatMessage {
	if historyRewritten {
		return copyMessages(filterTransientModelContextHistory(messages))
	}
	if startLen < 0 {
		startLen = 0
	}
	if startLen > len(messages) {
		startLen = len(messages)
	}
	return copyMessages(filterTransientModelContextHistory(messages[startLen:]))
}

func filterTransientModelContextHistory(msgs []providers.ChatMessage) []providers.ChatMessage {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]providers.ChatMessage, 0, len(msgs))
	for _, msg := range msgs {
		if isTransientModelContextMessage(msg) {
			continue
		}
		out = append(out, msg)
	}
	return out
}

func isTransientModelContextMessage(msg providers.ChatMessage) bool {
	if compact.IsInternalContextMessage(msg) {
		return false
	}
	name := strings.TrimSpace(msg.Name)
	switch name {
	case wuucontext.TaskContractMessageName,
		wuucontext.GoalContinuationMessageName:
		return true
	}
	if wuucontext.IsSystemReminder(name, "") {
		return true
	}
	if isLegacyHookContextMessage(msg) {
		return true
	}
	if !msg.Hidden {
		return false
	}
	return wuucontext.IsSystemReminder("", msg.Content) ||
		wuucontext.IsGoalContinuation("", msg.Content)
}

func isLegacyHookContextMessage(msg providers.ChatMessage) bool {
	return strings.EqualFold(strings.TrimSpace(msg.Role), "user") &&
		strings.HasPrefix(strings.TrimSpace(msg.Content), "[Hook context for ")
}

func cloneReasoningBlocks(blocks []providers.ReasoningBlock) []providers.ReasoningBlock {
	if len(blocks) == 0 {
		return nil
	}
	out := make([]providers.ReasoningBlock, len(blocks))
	copy(out, blocks)
	return out
}

func shouldPersistAssistantMessage(msg providers.ChatMessage) bool {
	if strings.TrimSpace(msg.Content) != "" {
		return true
	}
	if strings.TrimSpace(msg.ReasoningContent) != "" {
		return true
	}
	if len(msg.ReasoningBlocks) > 0 {
		return true
	}
	return len(msg.ToolCalls) > 0
}

func isLegitimateEmptyCompletion(finishReason providers.FinishReason, stopReason string) bool {
	switch finishReason {
	case providers.FinishReasonLength:
		return true
	}
	switch strings.TrimSpace(strings.ToLower(stopReason)) {
	case "end_turn":
		return true
	default:
		return false
	}
}

// errorJSON marshals an error into the JSON payload tool callers see
// when their tool execution fails. Centralized here so both runners
// produce identical error shapes.
func errorJSON(err error) string {
	message := "tool execution failed"
	if err != nil {
		message = err.Error()
	}
	payload := map[string]any{
		"ok":               false,
		"error":            message,
		"next_suggestions": toolErrorNextSuggestions(message),
	}
	if kind := extractToolErrorKind(message); kind != "" {
		payload["error_kind"] = kind
	}
	b, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return `{"error":"tool execution failed"}`
	}
	return string(b)
}

func extractToolErrorKind(message string) string {
	const marker = "error_kind="
	idx := strings.Index(message, marker)
	if idx < 0 {
		return ""
	}
	rest := strings.TrimLeft(message[idx+len(marker):], ` "'`)
	if rest == "" {
		return ""
	}
	end := 0
	for end < len(rest) {
		ch := rest[end]
		if ch == '"' || ch == '\'' || ch == ',' || ch == ';' || ch == ')' || ch == ']' || ch == '}' || ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			break
		}
		end++
	}
	return strings.TrimSpace(rest[:end])
}

func toolErrorNextSuggestions(message string) []string {
	kind := extractToolErrorKind(message)
	switch kind {
	case "approval_required":
		return []string{"ask the user for approval or choose a lower-risk alternative"}
	case "policy_denied":
		return []string{"choose a lower-risk tool or explain that policy blocks the requested action"}
	}
	if strings.Contains(message, "safe_retry=") {
		return []string{"follow safe_retry after refreshing the relevant file, command, or workspace evidence"}
	}
	if strings.Contains(message, "model_next_action=") {
		return []string{"follow model_next_action from the error payload"}
	}
	return []string{"inspect the error, correct the inputs or choose a safer tool, then retry only if the next step is clear"}
}

// historyContextKey is the unexported key under which RunToolLoop
// threads the current parent-agent message slice into a tool's
// execution context. Tools that support history inheritance can read
// it; everyone else can ignore it. Using an unexported zero-sized
// struct as the key guarantees no collisions with other ctx values.
type historyContextKey struct{}

// withHistory attaches a snapshot of the parent agent's current
// message history to ctx so a tool can later retrieve it via
// HistoryFromContext. The slice is shared by reference — tools must
// treat it as read-only and copy if they need to retain it past the
// Execute call.
func withHistory(ctx context.Context, history []providers.ChatMessage) context.Context {
	return context.WithValue(ctx, historyContextKey{}, history)
}

// HistoryFromContext returns the parent agent's current message
// history if RunToolLoop attached one. Returns nil otherwise.
//
// Tools that read this should copy the slice if they need it past
// the Execute call: it points at the live messages slice that
// RunToolLoop is mutating.
func HistoryFromContext(ctx context.Context) []providers.ChatMessage {
	h, _ := ctx.Value(historyContextKey{}).([]providers.ChatMessage)
	return h
}

// ── Concurrency partitioning ───────────────────────────────────────
//
// consecutive read-only tools run in parallel (up to maxToolConcurrency),
// write tools run serially. This preserves ordering semantics while
// getting maximum throughput for common patterns like multiple
// concurrent reads.

const maxToolConcurrency = 10

// toolBatch groups consecutive tool calls that share a concurrency
// mode. concurrent=true means every call in the batch can run in
// parallel.
type toolBatch struct {
	calls      []providers.ToolCall
	concurrent bool
}

// partitionToolCalls groups consecutive tool calls into batches based
// on concurrency safety. If the ToolExecutor implements
// ToolMetadataProvider, we use per-tool metadata; otherwise all tools
// are treated as serial (backwards compatible).
func partitionToolCalls(executor ToolExecutor, calls []providers.ToolCall) []toolBatch {
	if len(calls) == 0 {
		return nil
	}
	// Single call — no partitioning needed.
	if len(calls) == 1 {
		return []toolBatch{{calls: calls, concurrent: false}}
	}

	mp, hasMetadata := executor.(ToolMetadataProvider)
	if !hasMetadata {
		// No metadata provider — run everything serially.
		return []toolBatch{{calls: calls, concurrent: false}}
	}

	var batches []toolBatch
	var currentCalls []providers.ToolCall
	currentConcurrent := false

	for i, call := range calls {
		meta, ok := mp.ToolMetadata(call)
		canConcur := ok && meta.ConcurrencySafe

		if i == 0 {
			currentConcurrent = canConcur
			currentCalls = []providers.ToolCall{call}
			continue
		}

		if canConcur == currentConcurrent {
			currentCalls = append(currentCalls, call)
		} else {
			batches = append(batches, toolBatch{
				calls:      currentCalls,
				concurrent: currentConcurrent,
			})
			currentCalls = []providers.ToolCall{call}
			currentConcurrent = canConcur
		}
	}
	batches = append(batches, toolBatch{
		calls:      currentCalls,
		concurrent: currentConcurrent,
	})

	return batches
}

// maxAggregateResultChars caps the total content of all tool-role messages in
// a single batch. Prevents N parallel tools x 50K each from bloating the prompt.
const maxAggregateResultChars = 200_000

// enforceAggregateResultBudget trims tool messages in-place so their
// total content stays within the aggregate budget. Trims the largest
// results first to minimize information loss.
func enforceAggregateResultBudget(msgs []providers.ChatMessage) {
	total := 0
	for _, m := range msgs {
		if m.Role == "tool" {
			total += len(m.Content)
		}
	}
	if total <= maxAggregateResultChars {
		return
	}
	// Trim the longest tool results first.
	for total > maxAggregateResultChars {
		maxIdx, maxLen := -1, 0
		for i, m := range msgs {
			if m.Role == "tool" && len(m.Content) > maxLen {
				maxIdx = i
				maxLen = len(m.Content)
			}
		}
		if maxIdx < 0 || maxLen <= 200 {
			break
		}
		excess := total - maxAggregateResultChars
		newLen := maxLen - excess
		if newLen < 200 {
			newLen = 200
		}
		msgs[maxIdx].Content = msgs[maxIdx].Content[:newLen] +
			fmt.Sprintf("\n[trimmed: original %d chars, aggregate budget %d]", maxLen, maxAggregateResultChars)
		total = total - maxLen + len(msgs[maxIdx].Content)
	}
}

func systemReminderBlockKinds(content string) []string {
	var kinds []string
	seen := make(map[string]struct{})
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "[") {
			continue
		}
		end := strings.Index(line, "]")
		if end <= 1 {
			continue
		}
		kind := strings.TrimSpace(line[1:end])
		if kind == "" {
			continue
		}
		if _, ok := seen[kind]; ok {
			continue
		}
		seen[kind] = struct{}{}
		kinds = append(kinds, kind)
	}
	return kinds
}
