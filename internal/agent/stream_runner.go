package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/compact"
	"github.com/blueberrycongee/wuu/internal/eventbus"
	"github.com/blueberrycongee/wuu/internal/provideroptions"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/stringutil"
)

// StreamCallback receives streaming events for live clients.
type StreamCallback func(event providers.StreamEvent)

const maxConsecutiveProactiveCompactFailures = 3

// StreamRunner manages one multi-step coding turn with streaming.
// It is a thin wrapper around RunToolLoop that supplies a streamStep
// adapter (Step → providers.StreamClient.StreamChat with reconnect),
// so the actual loop logic — step counting, finish handling,
// context-overflow auto-compact — comes from the same code as Runner.
type StreamRunner struct {
	Client       providers.StreamClient
	Tools        ToolExecutor
	Model        string
	APIModel     string
	SystemPrompt string
	// SystemPromptSections mirrors SystemPrompt as metadata only, allowing
	// request telemetry to explain the stable prompt without exposing text.
	SystemPromptSections []SystemPromptSectionInfo
	MaxSteps             int
	Temperature          float64
	OnEvent              StreamCallback

	// Bus, when non-nil, publishes all stream events to the event bus
	// in addition to OnEvent. This decouples the core agent loop from
	// UI consumers and enables headless / multi-client / remote use cases.
	Bus *eventbus.Bus

	// OnUsage, when non-nil, is invoked once per LLM round-trip with
	// the per-call token counts reported by the provider. This mirrors
	// the field of the same name on the non-streaming Runner so that
	// callers driving long-lived background runs (e.g. sub-agents) can
	// surface live token accumulation while the run is still going.
	OnUsage func(input, output int)
	// OnTokenUsage reports the full per-call token usage, including
	// prompt cache read/write counts.
	OnTokenUsage func(usage providers.TokenUsage)

	// ContextWindowOverride is the resolved provider/model context window.
	// Zero means the runtime does not know the limit and proactive compaction
	// stays disabled; reactive provider overflow recovery still runs.
	ContextWindowOverride int
	// MaxInputTokens lets callers pass a provider/model prompt limit
	// when it is smaller than the total context window.
	MaxInputTokens int
	// OutputReserveTokens lets callers pass a provider/model output limit
	// for compact threshold math without forcing a request max_tokens value.
	OutputReserveTokens int
	// CompactThresholdPct lets callers compact earlier than the default
	// usable-window calculation. Zero means auto.
	CompactThresholdPct float64
	// CompactKeepRecentTokens overrides the default recent raw-history budget
	// kept after compaction. Zero means use the default.
	CompactKeepRecentTokens int

	// DisableAutoCompact turns off the proactive fill-rate trigger.
	// The reactive context-overflow recovery still runs. Off by default.
	DisableAutoCompact bool

	// StreamingToolExecution, when true, starts executing read-only tools
	// during model streaming (before the full response arrives). Off by default
	// until stabilized.
	StreamingToolExecution bool

	// BeforeStep, when set, is called at the start of each model
	// round right before building the provider request. Any returned
	// messages are appended to history for that round.
	BeforeStep func() []providers.ChatMessage

	// BeforeRequestContext, when set, is called before each provider request.
	// Returned segments are assembled into that request but are not appended
	// to live or durable conversation history.
	BeforeRequestContext func() []ContextSegment
	// OnRequestContext receives metadata-only summaries of request-only model
	// context assembled before requests.
	OnRequestContext func(info RequestContextInfo)
	// OnCompactAttempt receives metadata-only compact attempt diagnostics.
	OnCompactAttempt func(info CompactAttemptInfo)

	// AfterTurn, when set, is invoked after a successful turn has
	// completed and usage state has been committed. It is best-effort:
	// implementations should return quickly and run slow work in the
	// background if needed.
	AfterTurn func(ctx context.Context, runner *StreamRunner, history []providers.ChatMessage, result LoopResult)

	// Effort controls reasoning depth. See ChatRequest.Effort.
	Effort string
	// Variant is the selected model-scoped provider option bundle.
	Variant string
	// ProviderOptions carries provider-specific model options selected by the
	// active model variant.
	ProviderOptions map[string]any
	// PromptCacheKey is a stable conversation-scoped cache key forwarded
	// to providers that support explicit prompt-cache routing.
	PromptCacheKey string

	// Stream reconnect policy. Zero values use Wuu defaults.
	StreamReconnectBudget   time.Duration // total time for reconnection (default: 0)
	StreamRetryInitialDelay time.Duration // backoff start (default: 1s)
	StreamRetryMaxDelay     time.Duration // backoff cap (default: 60s)

	usageMu           sync.Mutex
	conversationUsage *UsageTracker
	trackedHistoryLen int

	compactMu                sync.Mutex
	proactiveCompactFailures int

	sysPromptMu sync.RWMutex
}

// Run executes one prompt with streaming tool-use loop.
func (r *StreamRunner) Run(ctx context.Context, prompt string) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", errors.New("prompt is required")
	}
	var history []providers.ChatMessage
	sysPrompt, _ := r.systemPromptSnapshot()
	if strings.TrimSpace(sysPrompt) != "" {
		history = append(history, providers.ChatMessage{Role: "system", Content: sysPrompt})
	}
	history = append(history, providers.ChatMessage{Role: "user", Content: prompt})
	res, err := r.RunWithCallback(ctx, history, r.OnEvent)
	if err != nil {
		return "", err
	}
	return res.Content, nil
}

// RunWithCallback executes a conversation turn with a per-call event callback.
// It accepts the full message history and returns the loop result, including
// any new messages produced during this turn and whether history was rewritten
// by auto-compaction.
// UpdateSystemPrompt safely updates the system prompt at runtime.
// The new prompt takes effect on the next turn.
func (r *StreamRunner) UpdateSystemPrompt(newPrompt string) {
	r.UpdateSystemPromptWithSections(newPrompt, nil)
}

// UpdateSystemPromptWithSections safely updates the system prompt and its
// telemetry metadata at runtime. The new prompt takes effect on the next turn.
func (r *StreamRunner) UpdateSystemPromptWithSections(newPrompt string, sections []SystemPromptSectionInfo) {
	r.sysPromptMu.Lock()
	defer r.sysPromptMu.Unlock()
	r.SystemPrompt = newPrompt
	r.SystemPromptSections = cloneSystemPromptSections(sections)
}

func (r *StreamRunner) systemPromptSnapshot() (string, []SystemPromptSectionInfo) {
	r.sysPromptMu.RLock()
	defer r.sysPromptMu.RUnlock()
	return r.SystemPrompt, cloneSystemPromptSections(r.SystemPromptSections)
}

func (r *StreamRunner) RunWithCallback(ctx context.Context, history []providers.ChatMessage, onEvent StreamCallback) (LoopResult, error) {
	if r.Client == nil {
		return LoopResult{}, errors.New("client is required")
	}
	if strings.TrimSpace(r.Model) == "" {
		return LoopResult{}, errors.New("model is required")
	}
	requestModel := strings.TrimSpace(r.APIModel)
	if requestModel == "" {
		requestModel = r.Model
	}
	history = filterDurableHistory(history)
	runUsage, baseHistoryLen := r.prepareUsageTracker(history)

	// Wrap the caller's callback so events also flow to the bus, if wired.
	emitEvent := onEvent
	if r.Bus != nil {
		emitEvent = func(ev providers.StreamEvent) {
			if onEvent != nil {
				onEvent(ev)
			}
			r.Bus.Publish(eventbus.AdaptStreamEvent(ev))
		}
	}
	effectiveOnEvent := filterInternalContextStreamEvents(emitEvent)

	step := &streamStep{
		client:                  r.Client,
		onEvent:                 effectiveOnEvent,
		retry:                   r.streamReconnectCfg(),
		tools:                   r.Tools,
		enableStreamingToolExec: r.StreamingToolExecution,
	}

	compactContextTokens := r.ContextWindowOverride
	maxCtx := compactContextTokens
	if r.DisableAutoCompact || r.proactiveCompactCircuitOpen() {
		maxCtx = 0 // disables the proactive trigger inside RunToolLoop
	}
	_, systemPromptSections := r.systemPromptSnapshot()
	beforeStep := r.BeforeStep
	if beforeStep != nil {
		capturedBeforeStep := beforeStep
		beforeStep = func() []providers.ChatMessage {
			return filterDurableHistory(capturedBeforeStep())
		}
	}
	cfg := LoopConfig{
		Tools:                   r.Tools,
		Model:                   requestModel,
		Temperature:             r.Temperature,
		MaxSteps:                r.MaxSteps,
		MaxContextTokens:        maxCtx,
		MaxInputTokens:          r.MaxInputTokens,
		OutputReserveTokens:     r.OutputReserveTokens,
		CompactThresholdPct:     r.CompactThresholdPct,
		CompactKeepRecentTokens: r.CompactKeepRecentTokens,
		BeforeStep:              beforeStep,
		BeforeRequestContext:    r.BeforeRequestContext,
		SystemPromptSections:    systemPromptSections,
		OnRequestContext: func(info RequestContextInfo) {
			if r.OnRequestContext != nil {
				r.OnRequestContext(info)
			}
			if effectiveOnEvent == nil {
				return
			}
			effectiveOnEvent(providers.StreamEvent{
				Type:           providers.EventRequestContext,
				RequestContext: requestContextSummary(info),
			})
		},
		OnUsage:      r.OnUsage,
		OnTokenUsage: r.OnTokenUsage,
		OnMessage: func(msg providers.ChatMessage) {
			if effectiveOnEvent == nil || msg.Hidden || isNonDurableHistoryMessage(msg) || isInternalContextHistoryMessage(msg) {
				return
			}
			copyMsg := msg
			effectiveOnEvent(providers.StreamEvent{
				Type:    providers.EventMessage,
				Message: &copyMsg,
			})
		},
		Compact: func(ctx context.Context, messages []providers.ChatMessage) ([]providers.ChatMessage, error) {
			return compact.CompactWithBudget(ctx, messages, r.Client, requestModel, compact.Budget{
				ContextTokens:       compactContextTokens,
				InputTokens:         r.MaxInputTokens,
				OutputReserveTokens: r.OutputReserveTokens,
				KeepRecentTokens:    r.CompactKeepRecentTokens,
			})
		},
		// Forward each tool result through the streaming callback so
		// clients can render tool output live (the loop itself only
		// records the tool message into the history).
		OnToolResult: func(call providers.ToolCall, result string) {
			if effectiveOnEvent == nil || isInternalContextToolName(call.Name) {
				return
			}
			toolCall := enrichToolCallDisplay(r.Tools, providers.ToolCall{
				ID:                call.ID,
				ProviderItemID:    call.ProviderItemID,
				ProviderItemModel: call.ProviderItemModel,
				Name:              call.Name,
				Arguments:         call.Arguments,
				Kind:              call.Kind,
				Display:           call.Display,
			})
			effectiveOnEvent(providers.StreamEvent{
				Type:       providers.EventToolUseEnd,
				ToolCall:   &toolCall,
				ToolResult: truncateLog(result, 2000),
			})
			if update, ok := planUpdateEventFromToolResult(call, result); ok {
				effectiveOnEvent(providers.StreamEvent{
					Type:       providers.EventPlanUpdate,
					PlanUpdate: update,
				})
			}
		},
		PostToolRewrite: compact.RewriteHistoryFromInternalToolMessagesWithContext,
		// Surface auto-compact events as stream events. The loop fires
		// this for both the proactive and the reactive overflow path.
		OnCompact: func(info CompactInfo) {
			if effectiveOnEvent == nil {
				return
			}
			effectiveOnEvent(providers.StreamEvent{
				Type:          providers.EventCompact,
				Content:       formatCompactNotice(info),
				CompactReason: string(info.Reason),
			})
		},
		OnCompactAttempt: func(info CompactAttemptInfo) {
			r.recordCompactAttempt(info)
			if r.OnCompactAttempt != nil {
				r.OnCompactAttempt(info)
			}
			if effectiveOnEvent == nil {
				return
			}
			notice, ok := formatCompactAttemptNotice(info)
			if !ok {
				return
			}
			effectiveOnEvent(providers.StreamEvent{
				Type:          providers.EventCompact,
				Content:       notice,
				CompactReason: string(info.Reason),
			})
		},
		UsageTracker:    runUsage,
		Effort:          r.Effort,
		ProviderOptions: provideroptions.Clone(r.ProviderOptions),
		PromptCacheKey:  r.PromptCacheKey,
	}

	res, err := RunToolLoop(ctx, history, cfg, step)
	res.ContextTokens = runUsage.EstimateCurrent()
	res.NewMessages = filterDurableHistory(res.NewMessages)
	if err != nil {
		r.commitUsageTracker(runUsage, baseHistoryLen)
		return res, err
	}
	finalHistoryLen := baseHistoryLen + len(res.NewMessages)
	if res.HistoryRewritten {
		finalHistoryLen = len(res.NewMessages)
	}
	r.commitUsageTracker(runUsage, finalHistoryLen)
	if r.AfterTurn != nil {
		fullHistory := make([]providers.ChatMessage, 0, len(history)+len(res.NewMessages))
		fullHistory = append(fullHistory, history...)
		fullHistory = append(fullHistory, res.NewMessages...)
		fullHistory = filterDurableHistory(fullHistory)
		r.AfterTurn(ctx, r, fullHistory, res)
	}
	return res, nil
}

func planUpdateEventFromToolResult(call providers.ToolCall, result string) (*providers.PlanUpdate, bool) {
	if call.Name != "update_plan" {
		return nil, false
	}
	var status struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(result), &status); err != nil || status.Status != "updated" {
		return nil, false
	}
	var update providers.PlanUpdate
	if err := json.Unmarshal([]byte(call.Arguments), &update); err != nil || len(update.Plan) == 0 {
		return nil, false
	}
	return &update, true
}

func filterInternalContextStreamEvents(next StreamCallback) StreamCallback {
	if next == nil {
		return nil
	}
	var hiddenTool bool
	return func(ev providers.StreamEvent) {
		switch ev.Type {
		case providers.EventToolUseStart:
			if ev.ToolCall != nil && isInternalContextToolName(ev.ToolCall.Name) {
				hiddenTool = true
				return
			}
			hiddenTool = false
		case providers.EventToolUseDelta:
			if hiddenTool {
				return
			}
		case providers.EventToolUseEnd:
			if ev.ToolCall != nil && isInternalContextToolName(ev.ToolCall.Name) {
				hiddenTool = false
				return
			}
			if hiddenTool {
				hiddenTool = false
				return
			}
		case providers.EventMessage:
			if ev.Message != nil && isInternalContextHistoryMessage(*ev.Message) {
				return
			}
		}
		next(ev)
	}
}

func isInternalContextToolName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), compact.InceptionToolName)
}

func isInternalContextHistoryMessage(msg providers.ChatMessage) bool {
	return compact.IsInternalContextMessage(msg)
}

// prepareUsageTracker snapshots the runner's shared conversation
// usage state and advances it to the history passed for this turn.
// The returned tracker is run-local; callers must commit it
// explicitly after deciding which history actually persists.
func (r *StreamRunner) prepareUsageTracker(history []providers.ChatMessage) (*UsageTracker, int) {
	r.usageMu.Lock()
	defer r.usageMu.Unlock()

	if r.conversationUsage == nil {
		r.conversationUsage = NewUsageTracker()
	}

	tracker := r.conversationUsage.Clone()
	trackedLen := r.trackedHistoryLen
	if trackedLen < 0 {
		trackedLen = 0
	}
	if trackedLen > len(history) {
		tracker.Reset()
		trackedLen = 0
	}
	if trackedLen == 0 {
		tracker.RecordPendingMessages(history)
		return tracker, len(history)
	}
	if len(history) > trackedLen {
		tracker.RecordPendingMessages(history[trackedLen:])
		trackedLen = len(history)
	}
	return tracker, trackedLen
}

// commitUsageTracker publishes a run-local usage snapshot as the new
// shared baseline for future turns.
func (r *StreamRunner) commitUsageTracker(tracker *UsageTracker, historyLen int) {
	if tracker == nil {
		return
	}
	r.usageMu.Lock()
	defer r.usageMu.Unlock()
	r.conversationUsage = tracker.Clone()
	r.trackedHistoryLen = historyLen
}

func isNonDurableHistoryMessage(msg providers.ChatMessage) bool {
	return isTransientModelContextMessage(msg)
}

func (r *StreamRunner) proactiveCompactCircuitOpen() bool {
	r.compactMu.Lock()
	defer r.compactMu.Unlock()
	return r.proactiveCompactFailures >= maxConsecutiveProactiveCompactFailures
}

func (r *StreamRunner) recordCompactAttempt(info CompactAttemptInfo) {
	r.compactMu.Lock()
	defer r.compactMu.Unlock()

	if info.Status == CompactAttemptSucceeded {
		r.proactiveCompactFailures = 0
		return
	}
	if info.Reason == CompactReasonProactive && info.Status == CompactAttemptFailed {
		r.proactiveCompactFailures++
	}
}

func filterDurableHistory(msgs []providers.ChatMessage) []providers.ChatMessage {
	if len(msgs) == 0 {
		return nil
	}
	// In-place filter: transient messages are rare, so the write-back cost is
	// negligible and we avoid a heap allocation.
	n := 0
	for _, msg := range msgs {
		if !isNonDurableHistoryMessage(msg) {
			msgs[n] = msg
			n++
		}
	}
	return msgs[:n]
}

func requestContextSummary(info RequestContextInfo) *providers.RequestContextSummary {
	return &providers.RequestContextSummary{
		StepIndex:                info.StepIndex,
		TransientMessages:        info.TransientMessages,
		ContentBytes:             info.ContentBytes,
		BlockKinds:               append([]string(nil), info.BlockKinds...),
		BlockKindCounts:          cloneStringIntMap(info.BlockKindCounts),
		BlockKindBytes:           cloneStringIntMap(info.BlockKindBytes),
		SegmentLifecycleCounts:   cloneStringIntMap(info.SegmentLifecycleCounts),
		SegmentPlacementCounts:   cloneStringIntMap(info.SegmentPlacementCounts),
		SegmentCachePolicyCounts: cloneStringIntMap(info.SegmentCachePolicyCounts),
		MessageCount:             info.MessageCount,
		SystemMessages:           info.SystemMessages,
		HiddenMessages:           info.HiddenMessages,
		ToolCount:                info.ToolCount,
		StablePrefix:             info.StablePrefix,
		TurnPrefix:               info.TurnPrefix,
		DynamicBytes:             info.DynamicBytes,
		SystemBytes:              info.SystemBytes,
		StablePrefixBytes:        info.StablePrefixBytes,
		TurnPrefixBytes:          info.TurnPrefixBytes,
		MessageBytes:             info.MessageBytes,
		ToolSchemaBytes:          info.ToolSchemaBytes,
		LoadableToolCount:        info.LoadableToolCount,
		LoadableToolSchemaBytes:  info.LoadableToolSchemaBytes,
		LoadableToolSurfaceHash:  info.LoadableToolSurfaceHash,
		SystemHash:               info.SystemHash,
		StablePrefixHash:         info.StablePrefixHash,
		TurnPrefixHash:           info.TurnPrefixHash,
		ToolSurfaceHash:          info.ToolSurfaceHash,
		PromptCacheKey:           info.PromptCacheKey,
		InputTokens:              info.InputTokens,
		OutputTokens:             info.OutputTokens,
		CacheCreationTokens:      info.CacheCreationTokens,
		CacheReadTokens:          info.CacheReadTokens,
		SystemSections:           systemPromptSectionSummaries(info.SystemSections),
	}
}

func cloneStringIntMap(in map[string]int) map[string]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func systemPromptSectionSummaries(sections []SystemPromptSectionInfo) []providers.SystemPromptSectionSummary {
	if len(sections) == 0 {
		return nil
	}
	out := make([]providers.SystemPromptSectionSummary, 0, len(sections))
	for _, section := range sections {
		out = append(out, providers.SystemPromptSectionSummary{
			Key:    section.Key,
			Static: section.Static,
			Bytes:  section.Bytes,
			Hash:   section.Hash,
		})
	}
	return out
}

// streamStep adapts providers.StreamClient (with reconnect) to the
// transport-agnostic Step interface. One Execute call opens an SSE
// stream, consumes it to completion (with mid-attempt reconnect on
// early disconnects), accumulates the assistant content + tool calls
// + usage, and returns a normalized StepResult.
type streamStep struct {
	client  providers.StreamClient
	onEvent StreamCallback
	retry   streamReconnectConfig
	// Streaming tool execution: when set, read-only tools start
	// executing as soon as their arguments are fully received,
	// overlapping with continued model output.
	tools                   ToolExecutor
	enableStreamingToolExec bool
}

func (s *streamStep) Execute(ctx context.Context, req providers.ChatRequest) (StepResult, error) {
	var (
		contentBuf        strings.Builder
		thinkingBuf       strings.Builder
		reasoningBlocks   []providers.ReasoningBlock
		pendingTools      = map[int]*providers.ToolCall{}
		usage             *providers.TokenUsage
		stopReason        string
		finishReason      providers.FinishReason
		truncated         bool
		messagePhase      providers.MessagePhase
		providerItemID    string
		providerItemModel string
	)

	var toolRuntime *TurnToolRuntime
	if s.enableStreamingToolExec && s.tools != nil {
		toolRuntime = NewTurnToolRuntime(s.tools)
		toolRuntime.SetStepIndex(req.StepIndex)
	}

	// Wrap the event callback so streamed tool blocks enter the per-turn
	// runtime as soon as they arrive.
	origOnEvent := s.onEvent
	if toolRuntime != nil {
		s.onEvent = func(ev providers.StreamEvent) {
			toolRuntime.ObserveStreamEvent(ctx, ev)
			if origOnEvent != nil {
				origOnEvent(ev)
			}
		}
	}

	resetRuntime := func() {
		if toolRuntime != nil {
			toolRuntime.Cancel()
			toolRuntime = NewTurnToolRuntime(s.tools)
			toolRuntime.SetStepIndex(req.StepIndex)
		}
	}
	if err := s.runStreamWithReconnect(ctx, req, &contentBuf, &thinkingBuf, &reasoningBlocks, pendingTools, &messagePhase, &providerItemID, &providerItemModel, &usage, &stopReason, &finishReason, &truncated, resetRuntime); err != nil {
		if toolRuntime != nil {
			toolRuntime.Cancel()
		}
		s.onEvent = origOnEvent // restore
		return StepResult{}, fmt.Errorf("stream request failed: %w", err)
	}
	s.onEvent = origOnEvent // restore

	// Build ordered tool calls list from the pending map.
	toolCalls := make([]providers.ToolCall, 0, len(pendingTools))
	for i := 0; i < len(pendingTools); i++ {
		if tc, ok := pendingTools[i]; ok {
			toolCalls = append(toolCalls, *tc)
		}
	}

	if finishReason == "" {
		finishReason = providers.NormalizeFinishReason(stopReason, truncated, len(toolCalls) > 0)
	}

	// Non-streaming fallback: when the stream completed without producing any
	// visible content or tool calls AND the provider did not send any terminal
	// reason, the stream was likely broken by a proxy or compatibility issue.
	// A terminal length/max_tokens reason is a completed model response, not a
	// broken stream, even when the visible text is empty.
	if strings.TrimSpace(contentBuf.String()) == "" && len(toolCalls) == 0 && strings.TrimSpace(stopReason) == "" && finishReason == "" && !truncated {
		if toolRuntime != nil {
			toolRuntime.Cancel()
		}
		providers.DebugLogf("stream returned empty content with stop_reason=%q, attempting non-streaming fallback", stopReason)
		if s.onEvent != nil {
			s.onEvent(providers.StreamEvent{
				Type:    providers.EventReconnect,
				Content: "Empty stream response — trying non-streaming fallback...",
			})
		}
		resp, err := s.client.Chat(ctx, req)
		if err != nil {
			return StepResult{}, fmt.Errorf("non-streaming fallback failed: %w", err)
		}
		fbToolCalls := make([]providers.ToolCall, len(resp.ToolCalls))
		copy(fbToolCalls, resp.ToolCalls)
		enrichToolCallsDisplay(s.tools, fbToolCalls)
		// Emit the fallback content through the streaming callback so
		// live clients can render it.
		if s.onEvent != nil && strings.TrimSpace(resp.Content) != "" {
			s.onEvent(providers.StreamEvent{
				Type:    providers.EventContentDelta,
				Content: resp.Content,
				Phase:   resp.Phase,
			})
			s.onEvent(providers.StreamEvent{
				Type:         providers.EventDone,
				Usage:        resp.Usage,
				StopReason:   resp.StopReason,
				FinishReason: normalizedChatResponseFinish(resp),
				Truncated:    resp.Truncated,
			})
		}
		fbFinishReason := normalizedChatResponseFinish(resp)
		return StepResult{
			Content:           resp.Content,
			Phase:             resp.Phase,
			ProviderItemID:    resp.ProviderItemID,
			ProviderItemModel: resp.ProviderItemModel,
			ReasoningContent:  resp.ReasoningContent,
			ReasoningBlocks:   cloneReasoningBlocks(resp.ReasoningBlocks),
			ToolCalls:         fbToolCalls,
			Usage:             resp.Usage,
			FinishReason:      fbFinishReason,
			StopReason:        resp.StopReason,
			Truncated:         resp.Truncated,
		}, nil
	}
	if len(toolCalls) == 0 && toolRuntime != nil {
		toolRuntime.Cancel()
		toolRuntime = nil
	}

	return StepResult{
		Content:           contentBuf.String(),
		Phase:             messagePhase,
		ProviderItemID:    providerItemID,
		ProviderItemModel: providerItemModel,
		ReasoningContent:  thinkingBuf.String(),
		ReasoningBlocks:   cloneReasoningBlocks(reasoningBlocks),
		ToolCalls:         toolCalls,
		Usage:             usage,
		FinishReason:      finishReason,
		StopReason:        stopReason,
		Truncated:         truncated,
		ToolRuntime:       toolRuntime,
	}, nil
}

func normalizedChatResponseFinish(resp providers.ChatResponse) providers.FinishReason {
	if resp.FinishReason != "" {
		return resp.FinishReason
	}
	return providers.NormalizeFinishReason(resp.StopReason, resp.Truncated, len(resp.ToolCalls) > 0)
}

func truncateLog(s string, maxLen int) string {
	return stringutil.Truncate(s, maxLen, "...")
}

// formatCompactNotice produces the human-readable string surfaced via
// EventCompact. Kept short — it shows up as a single system line in
// the chat viewport.
func formatCompactNotice(info CompactInfo) string {
	verb := "Compacted"
	switch info.Reason {
	case CompactReasonOverflow:
		verb = "Recovered from context overflow — compacted"
	case CompactReasonHelpMe:
		verb = "HelpMe recovered and compacted"
	case CompactReasonInception:
		verb = "Inception rewrote"
	}
	if info.TokensBefore > 0 {
		return fmt.Sprintf("✦ %s history: %d → %d messages (was ~%s)",
			verb, info.MessagesBefore, info.MessagesAfter,
			formatTokenCount(info.TokensBefore))
	}
	return fmt.Sprintf("✦ %s history: %d → %d messages",
		verb, info.MessagesBefore, info.MessagesAfter)
}

func formatCompactAttemptNotice(info CompactAttemptInfo) (string, bool) {
	if info.Reason != CompactReasonProactive || info.Status != CompactAttemptFailed {
		return "", false
	}
	return "Context compaction failed; continuing without compacting history.", true
}

// formatTokenCount renders a token count in a compact form: 1234 →
// "1.2k", 12_345 → "12k", 1_234_567 → "1.2M".
func formatTokenCount(n int) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d tokens", n)
	case n < 10_000:
		return fmt.Sprintf("%.1fk tokens", float64(n)/1000)
	case n < 1_000_000:
		return fmt.Sprintf("%dk tokens", n/1000)
	default:
		return fmt.Sprintf("%.1fM tokens", float64(n)/1_000_000)
	}
}

func (s *streamStep) runStreamWithReconnect(
	ctx context.Context,
	req providers.ChatRequest,
	contentBuf *strings.Builder,
	thinkingBuf *strings.Builder,
	reasoningBlocks *[]providers.ReasoningBlock,
	pendingTools map[int]*providers.ToolCall,
	messagePhase *providers.MessagePhase,
	providerItemID *string,
	providerItemModel *string,
	usage **providers.TokenUsage,
	stopReason *string,
	finishReason *providers.FinishReason,
	truncated *bool,
	onAttemptStart func(),
) error {
	cfg := s.retry
	onEvent := s.onEvent
	attempt := 0
	var reconnectStart time.Time

	elapsed := func() time.Duration {
		if reconnectStart.IsZero() {
			return 0
		}
		return time.Since(reconnectStart)
	}

	emitLifecycle := func(phase providers.StreamLifecyclePhase, retryCount int, reason error, retryIn time.Duration) {
		if onEvent == nil {
			return
		}
		details := &providers.StreamLifecycle{
			Phase:      phase,
			Attempt:    retryCount + 1,
			RetryCount: retryCount,
			RetryIn:    retryIn,
			Elapsed:    elapsed(),
			Budget:     cfg.Budget,
		}
		if reason != nil {
			details.Reason = providers.StreamErrorSummary(reason)
		}
		onEvent(providers.StreamEvent{
			Type:      providers.EventLifecycle,
			Lifecycle: details,
		})
	}

	resetPartialOutput := func() {
		hadContent := contentBuf.Len() > 0
		hadThinking := thinkingBuf.Len() > 0
		if !hadContent && !hadThinking && len(*reasoningBlocks) == 0 {
			return
		}
		contentBuf.Reset()
		thinkingBuf.Reset()
		*reasoningBlocks = nil
		*messagePhase = ""
		*providerItemID = ""
		*providerItemModel = ""
		*usage = nil
		*stopReason = ""
		*finishReason = ""
		*truncated = false
		if onEvent == nil {
			return
		}
		if hadContent {
			onEvent(providers.StreamEvent{
				Type: providers.EventContentReplace,
			})
		}
		if hadThinking {
			onEvent(providers.StreamEvent{
				Type: providers.EventThinkingReplace,
			})
		}
	}

	reconnect := func(err error) (delay time.Duration, ok bool) {
		if reconnectStart.IsZero() {
			reconnectStart = time.Now()
		}
		el := elapsed()
		if !shouldRetryStreamError(err, el, cfg.Budget) {
			return 0, false
		}
		delay = streamRetryDelay(attempt, cfg.InitialDelay, cfg.MaxDelay)
		// Clamp delay so we don't overshoot the budget.
		if remaining := cfg.Budget - el; delay > remaining {
			delay = remaining
		}
		attempt++
		providers.DebugLogf("stream reconnecting (%d, %s/%s) in %s: %v",
			attempt, el.Round(time.Second), cfg.Budget, delay, err)
		// Log full error details for post-mortem analysis.
		var httpErr *providers.HTTPError
		if errors.As(err, &httpErr) {
			providers.DebugLogf("  HTTP %d body: %s", httpErr.StatusCode, httpErr.Body)
		}
		var streamErr *providers.StreamError
		if errors.As(err, &streamErr) {
			providers.DebugLogf("  stream error code=%s msg=%s", streamErr.Code, streamErr.Message)
		}
		emitLifecycle(providers.StreamPhaseReconnecting, attempt, err, delay)
		if onEvent != nil {
			onEvent(providers.StreamEvent{
				Type:    providers.EventReconnect,
				Content: fmt.Sprintf("Reconnecting... %s / %s", el.Round(time.Second), cfg.Budget),
			})
		}
		return delay, true
	}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// A broken attempt may have accumulated partial tool calls that
		// never received a matching EventToolUseEnd. Those incomplete
		// JSON arguments would leak into the final StepResult and later
		// cause mapMessage to fail when the message is replayed as
		// history. Clear the map on every fresh HTTP attempt.
		for k := range pendingTools {
			delete(pendingTools, k)
		}
		if onAttemptStart != nil {
			onAttemptStart()
		}

		emitLifecycle(providers.StreamPhaseConnecting, attempt, nil, 0)

		// Connection-stage timeouts (dial, TLS, response-header) are
		// already set on the HTTP transport by BuildStreamingHTTPClient.
		// Do NOT wrap ctx in a per-attempt WithTimeout here — the HTTP
		// request is created with the context passed to StreamChat, and
		// canceling that context kills resp.Body reads (especially on
		// HTTP/2), aborting the SSE stream immediately after connect.
		ch, err := s.client.StreamChat(ctx, req)
		if err != nil {
			if ctx.Err() == nil {
				if delay, ok := reconnect(err); ok {
					if waitErr := waitWithContext(ctx, delay); waitErr != nil {
						return waitErr
					}
					resetPartialOutput()
					continue
				}
			}
			emitLifecycle(providers.StreamPhaseFailed, attempt, err, 0)
			return err
		}
		// Successful connect resets the reconnect clock.
		reconnectStart = time.Time{}
		attempt = 0
		emitLifecycle(providers.StreamPhaseConnected, attempt, nil, 0)

		var (
			streamErr error
			sawDone   bool
		)

		for event := range ch {
			switch event.Type {
			case providers.EventContentDelta:
				if event.Phase != "" {
					*messagePhase = event.Phase
				}
				if strings.TrimSpace(event.ProviderItemID) != "" {
					*providerItemID = event.ProviderItemID
					*providerItemModel = req.Model
				}
				contentBuf.WriteString(event.Content)

			case providers.EventContentReplace:
				if event.Phase != "" {
					*messagePhase = event.Phase
				}
				if strings.TrimSpace(event.ProviderItemID) != "" {
					*providerItemID = event.ProviderItemID
					*providerItemModel = req.Model
				}
				contentBuf.Reset()
				contentBuf.WriteString(event.Content)

			case providers.EventThinkingDelta:
				thinkingBuf.WriteString(event.Content)

			case providers.EventThinkingDone:
				if event.ReasoningBlock != nil {
					*reasoningBlocks = append(*reasoningBlocks, *event.ReasoningBlock)
				}

			case providers.EventToolUseStart:
				if event.ToolCall != nil {
					if strings.TrimSpace(event.ToolCall.ProviderItemID) != "" {
						event.ToolCall.ProviderItemModel = req.Model
					}
					idx := len(pendingTools)
					toolCall := enrichToolCallDisplay(s.tools, providers.ToolCall{
						ID:                event.ToolCall.ID,
						ProviderItemID:    event.ToolCall.ProviderItemID,
						ProviderItemModel: event.ToolCall.ProviderItemModel,
						Name:              event.ToolCall.Name,
						Kind:              event.ToolCall.Kind,
					})
					pendingTools[idx] = &toolCall
					event.ToolCall = &toolCall
				}

			case providers.EventToolUseDelta:
				if len(pendingTools) > 0 {
					latest := pendingTools[len(pendingTools)-1]
					latest.Arguments += event.Content
				}

			case providers.EventToolUseEnd:
				if event.ToolCall != nil {
					if strings.TrimSpace(event.ToolCall.ProviderItemID) != "" {
						event.ToolCall.ProviderItemModel = req.Model
					}
					toolCall := enrichToolCallDisplay(s.tools, *event.ToolCall)
					for _, tc := range pendingTools {
						if tc.ID == toolCall.ID {
							if strings.TrimSpace(toolCall.ProviderItemID) != "" {
								tc.ProviderItemID = toolCall.ProviderItemID
								tc.ProviderItemModel = toolCall.ProviderItemModel
							}
							if strings.TrimSpace(toolCall.Name) != "" {
								tc.Name = toolCall.Name
							}
							if toolCall.Arguments != "" {
								tc.Arguments = toolCall.Arguments
							}
							if toolCall.Kind != "" {
								tc.Kind = toolCall.Kind
							}
							if toolCall.Display != nil {
								tc.Display = toolCall.Display
							}
							break
						}
					}
					event.ToolCall = &toolCall
				}

			case providers.EventUsage:
				if event.Usage != nil {
					*usage = event.Usage
				}

			case providers.EventProviderState:
				if event.ProviderState != nil {
					event.ProviderState.StepIndex = req.StepIndex
				}

			case providers.EventError:
				if event.Error != nil {
					streamErr = event.Error
				} else {
					streamErr = errors.New("stream error")
				}
				continue

			case providers.EventDone:
				sawDone = true
				if event.Usage != nil {
					*usage = event.Usage
				}
				if event.StopReason != "" {
					*stopReason = event.StopReason
				}
				if event.FinishReason != "" {
					*finishReason = event.FinishReason
				}
				if event.Truncated {
					*truncated = true
				}
			}

			if onEvent != nil {
				onEvent(event)
			}
		}

		if streamErr == nil && !sawDone {
			streamErr = providers.NewIncompleteStreamError("stream closed before done")
		}
		if streamErr == nil {
			return nil
		}

		// Retry on any retryable error while the parent context is still alive.
		// The retry replays the whole turn with a new provider request, regardless
		// of whether output was already seen.
		if ctx.Err() == nil {
			if delay, ok := reconnect(streamErr); ok {
				if waitErr := waitWithContext(ctx, delay); waitErr != nil {
					return waitErr
				}
				resetPartialOutput()
				continue
			}
		}

		emitLifecycle(providers.StreamPhaseFailed, attempt, streamErr, 0)
		if onEvent != nil && !providers.IsContextOverflow(streamErr) {
			onEvent(providers.StreamEvent{
				Type:  providers.EventError,
				Error: streamErr,
			})
		}
		return streamErr
	}
}

func enrichToolCallsDisplay(executor ToolExecutor, calls []providers.ToolCall) {
	for i := range calls {
		calls[i] = enrichToolCallDisplay(executor, calls[i])
	}
}

func enrichToolCallDisplay(executor ToolExecutor, call providers.ToolCall) providers.ToolCall {
	if call.Display != nil {
		return call
	}
	displayProvider, ok := executor.(ToolDisplayProvider)
	if !ok {
		return call
	}
	display, ok := displayProvider.ToolDisplay(call)
	if !ok || strings.TrimSpace(display.Text) == "" {
		return call
	}
	call.Display = &display
	return call
}

// streamReconnectConfig holds time-budget reconnection parameters.
type streamReconnectConfig struct {
	Budget       time.Duration
	InitialDelay time.Duration
	MaxDelay     time.Duration
}

const (
	defaultReconnectBudget = 0
	defaultReconnectDelay  = 1 * time.Second
	defaultReconnectMax    = 60 * time.Second
)

func (r *StreamRunner) streamReconnectCfg() streamReconnectConfig {
	cfg := streamReconnectConfig{
		Budget:       defaultReconnectBudget,
		InitialDelay: defaultReconnectDelay,
		MaxDelay:     defaultReconnectMax,
	}
	if r.StreamReconnectBudget > 0 {
		cfg.Budget = r.StreamReconnectBudget
	}
	if r.StreamRetryInitialDelay > 0 {
		cfg.InitialDelay = r.StreamRetryInitialDelay
	}
	if r.StreamRetryMaxDelay > 0 {
		cfg.MaxDelay = r.StreamRetryMaxDelay
	}
	if cfg.InitialDelay > cfg.MaxDelay {
		cfg.InitialDelay = cfg.MaxDelay
	}
	return cfg
}

func shouldRetryStreamError(err error, elapsed, budget time.Duration) bool {
	if elapsed >= budget {
		return false
	}
	return providers.IsRetryable(err)
}

func streamRetryDelay(attempt int, initial, maxDelay time.Duration) time.Duration {
	base := float64(initial) * math.Pow(2, float64(attempt))
	if base > float64(maxDelay) {
		base = float64(maxDelay)
	}
	// +/-25% jitter to avoid thundering herd.
	jitter := 0.25 * base * (2*rand.Float64() - 1)
	return time.Duration(base + jitter)
}

func waitWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
