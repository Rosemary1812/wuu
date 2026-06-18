// Package agent: Step is the transport-agnostic abstraction the
// shared tool-use loop drives. Both Runner and StreamRunner execute
// through Step (Runner adapts providers.Client to StreamClient first),
// so the actual loop logic — step counting, tool execution,
// truncation recovery, context-overflow auto-compact — lives in
// exactly one place. See loop.go.
package agent

import (
	"context"

	"github.com/blueberrycongee/wuu/internal/providers"
)

// StepResult is the outcome of one model round-trip, normalized so
// the loop doesn't care whether the response came from a one-shot
// Chat or a fully-consumed SSE stream.
type StepResult struct {
	// Content is the assistant's text for this round (concatenation
	// of all content deltas in the streaming case).
	Content string
	// Phase is the provider-supplied assistant message phase when
	// available. Empty means unknown and callers may infer from tool use.
	Phase providers.MessagePhase
	// ReasoningContent is provider-emitted hidden reasoning for this
	// round. Some OpenAI-compatible providers require replaying it
	// verbatim on follow-up assistant tool-call messages.
	ReasoningContent string
	// ReasoningBlocks preserves provider-native thinking payloads that
	// cannot safely be reconstructed from ReasoningContent text alone.
	ReasoningBlocks []providers.ReasoningBlock
	// ToolCalls is the ordered list of tool invocations the model
	// requested in this round, fully assembled (arguments included).
	ToolCalls []providers.ToolCall
	// Truncated is true when the provider signalled an output-token
	// cap (Anthropic stop_reason=max_tokens, OpenAI finish_reason=
	// length). The loop uses this to drive the "continue" recovery.
	Truncated bool
	// StopReason is the lowercase normalized stop signal. Surfaced
	// for diagnostics; the loop's behavior is driven by Truncated.
	StopReason string
	// Usage is the per-round token consumption when the provider
	// reports it. nil is allowed.
	Usage *providers.TokenUsage
	// ToolRuntime owns any tool calls that were started while the provider
	// was still streaming. The loop uses it to wait for those same runs
	// instead of issuing duplicate executions.
	ToolRuntime *TurnToolRuntime
}

// Step performs exactly one model round-trip and returns the result
// in normalized form. Implementations encapsulate the transport
// (one-shot vs streaming) and any per-round live-rendering side
// effects; the loop above doesn't observe those.
type Step interface {
	Execute(ctx context.Context, req providers.ChatRequest) (StepResult, error)
}

// CompactFn compresses a conversation that has overflowed the model's
// context window. The loop calls it once when the underlying step
// returns a context-overflow error, then re-issues the same step.
//
// Implementations are expected to wrap whatever provider-side
// summarization they need; the loop is intentionally agnostic.
type CompactFn func(ctx context.Context, messages []providers.ChatMessage) ([]providers.ChatMessage, error)

// CompactReason classifies why the loop ran a compact pass.
type CompactReason string

const (
	// CompactReasonProactive means the loop hit its proactive usable-window
	// threshold and ran a compact preemptively to avoid overflow.
	CompactReasonProactive CompactReason = "proactive"
	// CompactReasonOverflow means a step.Execute returned a
	// context-overflow error and the loop ran compact reactively as
	// the recovery path.
	CompactReasonOverflow CompactReason = "overflow"
)

// CompactInfo describes a compact pass that just ran. Surfaced via
// LoopConfig.OnCompact so interactive clients can let the user know
// what just happened.
type CompactInfo struct {
	Reason         CompactReason
	TokensBefore   int
	MessagesBefore int
	MessagesAfter  int
}

// RequestContextInfo summarizes transient request-only context injected into
// a provider call. It intentionally excludes raw prompt text.
type RequestContextInfo struct {
	StepIndex         int
	TransientMessages int
	ContentBytes      int
	BlockKinds        []string
}

// LoopConfig bundles every knob the shared loop needs. All callbacks
// are optional. Tools is required if the model is allowed to call any.
type LoopConfig struct {
	// Tools is the executor used to run model-requested tool calls.
	// May be nil if the caller knows the model has no tools available.
	Tools ToolExecutor
	// Model is the model identifier passed through to the provider.
	Model string
	// Temperature is the sampling temperature; 0 means provider default.
	Temperature float64
	// MaxSteps caps the number of model round-trips per Run. Zero
	// means unlimited (aligned with Claude Code's default), positive
	// values act as a runaway safety net.
	MaxSteps int
	// Compact is invoked when the loop wants to summarize the older
	// conversation (proactive fill-rate trigger or reactive
	// context-overflow recovery). nil disables both auto-compact
	// paths; the overflow error is propagated to the caller as-is.
	Compact CompactFn
	// MaxContextTokens is the model's context window. When non-zero,
	// the loop tracks usage from response.usage plus local estimates
	// and proactively triggers a compact pass once the conversation
	// exceeds the configured fraction or the output-reserved usable
	// input window. Zero disables proactive compact (the reactive
	// overflow path still works).
	MaxContextTokens int
	// MaxInputTokens is the provider's prompt/input limit when it is
	// lower than the full context window. Some APIs advertise a large
	// total context window but reserve a large output budget server-side;
	// proactive compact must respect the smaller input side.
	MaxInputTokens int
	// OutputReserveTokens is the model output limit used for context
	// budgeting. It does not force the request's max_tokens; DefaultMaxTokens
	// remains the request cap.
	OutputReserveTokens int
	// CompactThresholdPct overrides the default OpenCode-style usable-window
	// trigger with a fraction of the configured input/context window. Zero
	// means use the default usable-window calculation.
	CompactThresholdPct float64
	// BeforeStep, when set, is called at the start of each model
	// round. Any returned messages are appended to the live history
	// before the next provider request is built. This is used by
	// sub-agent follow-up messaging: send_message queues
	// user-role messages that are injected on the next round.
	BeforeStep func() []providers.ChatMessage
	// BeforeRequest, when set, is called after live-history updates and
	// right before each provider request. Returned messages are sent to
	// the model for this request only; they are not appended to live
	// history and are not persisted. Use this for dynamic runtime facts
	// such as environment or child-agent status reminders.
	BeforeRequest func() []providers.ChatMessage
	// OnRequestContext receives a metadata-only summary of request-only
	// context injected by BeforeRequest.
	OnRequestContext func(info RequestContextInfo)
	// OnUsage is invoked once per LLM round-trip with the per-call
	// token counts when the provider reports them. The loop also
	// accumulates totals into LoopResult.
	OnUsage func(input, output int)
	// OnTokenUsage is invoked once per LLM round-trip with the full
	// provider token usage, including prompt cache read/write counts.
	OnTokenUsage func(usage providers.TokenUsage)
	// OnMessage is invoked whenever the loop appends a semantic chat
	// message to its live history. Streaming callers use it to persist
	// assistant/tool/internal follow-up messages incrementally instead of
	// waiting for the whole turn to finish.
	OnMessage func(msg providers.ChatMessage)
	// OnToolResult is invoked after each tool execution with the
	// (call, JSON result) pair. Used by streaming callers to feed
	// live tool-result rendering into clients.
	OnToolResult func(call providers.ToolCall, result string)
	// OnCompact is invoked once per compact pass (proactive or
	// reactive). Optional; clients can use it to render a status line.
	OnCompact func(info CompactInfo)
	// UsageTracker, when non-nil, is the caller-owned conversation
	// usage state to reuse across runs. This lets the loop make the
	// same compact decision before the first request of a new turn
	// that it would make mid-run after receiving fresh usage.
	UsageTracker *UsageTracker

	// DefaultMaxTokens is the output token cap sent on every request.
	// Zero means the provider's default (e.g. 16 384 for Anthropic).
	// Aligned with Claude Code's initial max_tokens.
	DefaultMaxTokens int
	// EscalatedMaxTokens is the output token cap used after the first
	// truncation recovery. Zero defaults to 65 536. Aligned with
	// Claude Code's "start low, escalate on truncation" strategy.
	EscalatedMaxTokens int
	// Effort controls reasoning depth. Empty = API default. Valid:
	//   Anthropic: "low", "medium", "high", "max"
	//   OpenAI:    "low", "medium", "high"
	// Aligned with Claude Code's /effort and Codex's reasoning_effort.
	Effort string
	// ProviderOptions are provider-specific model options selected by the
	// active model variant. They are forwarded to ChatRequest.
	ProviderOptions map[string]any
	// PromptCacheKey, when set, overrides the content-derived fallback
	// key in CacheHint for providers with explicit prompt-cache routing.
	PromptCacheKey string
}

// LoopResult is what RunToolLoop returns on success.
type LoopResult struct {
	// Content is the model's final assistant message after any
	// truncation-recovery rounds have been concatenated.
	Content string
	// NewMessages is the slice of messages produced during this run
	// (assistant turns + tool result turns) in order. When a compact
	// pass rewrote the live history mid-run, this becomes the full
	// replacement history snapshot and HistoryRewritten is true.
	NewMessages []providers.ChatMessage
	// HistoryRewritten reports whether a compact pass replaced the
	// live history slice mid-run. Callers that persist conversations
	// should replace stored history instead of append-only extending
	// it when this is true.
	HistoryRewritten bool
	// InputTokens / OutputTokens are the cumulative usage across
	// every round in this run, including any compact + recovery
	// rounds. Zero when the provider doesn't report usage.
	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int
}
