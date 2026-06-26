package providers

import (
	"context"
	"strings"
	"time"
)

// ToolDefinition describes a callable tool exposed to the model.
type ToolDefinition struct {
	Name        string
	Description string
	InputSchema map[string]any
	// DeferLoading marks a provider-native loadable tool declaration. It is a
	// wire-format hint, not an execution policy; runtimes still decide whether a
	// tool is hidden, deferred, or directly callable.
	DeferLoading bool
	// CacheStable marks tools that belong to the stable prompt-cache
	// prefix. Providers that support tool-level cache markers can place
	// a breakpoint at the end of the contiguous stable prefix.
	CacheStable bool
}

// LoadableToolDefinition is the provider-neutral shape for a tool schema that
// can be returned by a discovery flow before it is part of the ordinary tool
// list. Providers can adapt it into native shapes such as Responses
// tool_search_output.tools or Anthropic tool_reference expansion.
type LoadableToolDefinition struct {
	Type         string         `json:"type"`
	Name         string         `json:"name"`
	Description  string         `json:"description,omitempty"`
	InputSchema  map[string]any `json:"input_schema,omitempty"`
	DeferLoading bool           `json:"defer_loading,omitempty"`
}

func LoadableToolFromDefinition(def ToolDefinition) LoadableToolDefinition {
	return LoadableToolDefinition{
		Type:         "function",
		Name:         def.Name,
		Description:  def.Description,
		InputSchema:  cloneLoadableToolSchema(def.InputSchema),
		DeferLoading: true,
	}
}

// ToolCallDisplay carries a user-facing summary for a tool invocation.
type ToolCallDisplay struct {
	Kind       string `json:"kind,omitempty"`
	Text       string `json:"text,omitempty"`
	Capability string `json:"capability,omitempty"`
}

// ToolCallKind distinguishes ordinary function calls from provider-native
// tool discovery calls that use different wire-format input/output items.
type ToolCallKind string

const (
	ToolCallKindFunction   ToolCallKind = "function"
	ToolCallKindToolSearch ToolCallKind = "tool_search"
)

func NormalizeToolCallKind(kind string) ToolCallKind {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case string(ToolCallKindToolSearch):
		return ToolCallKindToolSearch
	case string(ToolCallKindFunction):
		return ToolCallKindFunction
	default:
		return ""
	}
}

// FinishReason is Wuu's provider-neutral reason for why a model response ended.
// StopReason keeps the raw provider string for diagnostics; FinishReason is the
// stable semantic value used by the agent loop and app-server.
type FinishReason string

const (
	FinishReasonStop          FinishReason = "stop"
	FinishReasonLength        FinishReason = "length"
	FinishReasonToolCalls     FinishReason = "tool_calls"
	FinishReasonContentFilter FinishReason = "content_filter"
	FinishReasonError         FinishReason = "error"
	FinishReasonUnknown       FinishReason = "unknown"
)

func NormalizeFinishReason(stopReason string, truncated bool, hasToolCalls bool) FinishReason {
	reason := strings.ToLower(strings.TrimSpace(stopReason))
	if truncated {
		return FinishReasonLength
	}
	switch reason {
	case "stop", "end_turn", "stop_sequence", "pause_turn", "completed":
		if hasToolCalls {
			return FinishReasonToolCalls
		}
		return FinishReasonStop
	case "length", "max_tokens", "max_output_tokens":
		return FinishReasonLength
	case "tool_calls", "tool_use", "function_call":
		return FinishReasonToolCalls
	case "content_filter", "refusal":
		return FinishReasonContentFilter
	case "error", "failed":
		return FinishReasonError
	case "":
		if hasToolCalls {
			return FinishReasonToolCalls
		}
		return ""
	default:
		if hasToolCalls {
			return FinishReasonToolCalls
		}
		return FinishReasonUnknown
	}
}

// ToolCall is a model requested tool execution.
type ToolCall struct {
	ID                string           `json:"id,omitempty"`
	ProviderItemID    string           `json:"provider_item_id,omitempty"`
	ProviderItemModel string           `json:"provider_item_model,omitempty"`
	Name              string           `json:"name,omitempty"`
	Arguments         string           `json:"arguments,omitempty"`
	Kind              ToolCallKind     `json:"kind,omitempty"`
	Display           *ToolCallDisplay `json:"display,omitempty"`
}

// InputImage carries one user-provided image in base64 form.
type InputImage struct {
	MediaType string
	Data      string
}

// InputFile carries one user-provided file attachment in base64 form.
// Images are still represented by InputImage for backward compatibility;
// non-image media such as PDFs use InputFile.
type InputFile struct {
	MediaType string
	Data      string
	Filename  string
}

// ReasoningBlock preserves provider-native thinking payloads that must be
// replayed verbatim on follow-up requests for some APIs.
type ReasoningBlock struct {
	Type      string
	Thinking  string
	Signature string
	Data      string
}

// MessagePhase classifies assistant text when a provider exposes the same
// structured phase signal Codex uses. Empty means unknown.
type MessagePhase string

const (
	MessagePhaseCommentary  MessagePhase = "commentary"
	MessagePhaseFinalAnswer MessagePhase = "final_answer"
)

// NormalizeMessagePhase returns a known phase value or empty for unsupported
// provider strings.
func NormalizeMessagePhase(phase string) MessagePhase {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case string(MessagePhaseCommentary):
		return MessagePhaseCommentary
	case string(MessagePhaseFinalAnswer):
		return MessagePhaseFinalAnswer
	default:
		return ""
	}
}

// ChatMessage is a generic multi-provider chat message.
type ChatMessage struct {
	Role           string
	Name           string
	ClientID       string
	Content        string
	DisplayContent string `json:"display_content,omitempty"`
	Phase          MessagePhase
	// ProviderItemID preserves provider-native assistant output item identity,
	// such as OpenAI Responses msg_* ids, for same-model replay.
	ProviderItemID    string
	ProviderItemModel string
	// Steered marks user input that was injected into an already-running turn.
	// Providers ignore this; app-server history uses it to restore turn items.
	Steered bool
	// ReasoningContent stores provider-emitted hidden reasoning that
	// must be replayed in follow-up assistant tool-call messages for
	// providers like Kimi when thinking mode is enabled.
	ReasoningContent string
	ReasoningBlocks  []ReasoningBlock
	Images           []InputImage
	Files            []InputFile
	ToolCallID       string
	ToolResultKind   ToolCallKind
	ToolCalls        []ToolCall
	FinishReason     FinishReason
	StopReason       string
	Truncated        bool
	// DiscoveredTools carries provider-native deferred tool state across
	// compact/resume/fork boundaries without adding full schemas to prompt text.
	DiscoveredTools []LoadableToolDefinition
}

// CacheHint carries provider-agnostic prompt-cache guidance.
//
// The goal is not to model every provider's caching surface area.
// wuu only needs a small set of cross-provider signals:
//   - PromptCacheKey: a stable conversation-scoped cache key for
//     OpenAI-compatible APIs that expose one.
//   - StableSystem/StablePrefixMessages: which request prefix is
//     stable enough to mark as cache-eligible on providers like
//     Anthropic.
//   - HasCompactSummary: whether the stable prefix starts with a
//     compacted conversation summary, so providers can bias cache
//     anchors toward that rewritten history root.
//
// Providers are free to ignore hints they don't support.
type CacheHint struct {
	// PromptCacheKey is a stable key for providers exposing an explicit
	// prompt cache key (for example promptCacheKey / prompt_cache_key).
	PromptCacheKey string
	// StableSystem marks the system prompt as part of the cacheable
	// stable prefix. Ignored when the request has no system message.
	StableSystem bool
	// StablePrefixMessages is the number of leading non-system entries
	// in ChatRequest.Messages that belong to the stable prefix.
	// Providers that merge, split, or lift messages must map this
	// source-message count onto their final wire-format block boundary.
	StablePrefixMessages int
	// HasCompactSummary reports that the leading system prompt contains
	// a compacted conversation summary. This lets providers prefer a
	// cache anchor close to the rewritten history root without needing
	// a heavier session-parts model.
	HasCompactSummary bool
}

// ChatRequest is the normalized request payload for providers.
type ChatRequest struct {
	Model       string
	Messages    []ChatMessage
	Tools       []ToolDefinition
	Temperature float64
	CacheHint   *CacheHint
	// MaxTokens caps the model's output length. Zero means the provider
	// should use its own default. If the provider hits this cap, the
	// response completes with FinishReason=length.
	MaxTokens int
	// Effort controls how much reasoning the model performs before
	// responding. Empty string means "use API default". Valid values
	// are provider-specific:
	//   Anthropic: "low", "medium", "high", "max"
	//   OpenAI:    "low", "medium", "high"
	// This maps to each provider's reasoning-depth control.
	Effort string
	// ProviderOptions carries model/provider-specific options selected through
	// model variants. Keys follow OpenCode's AI SDK option names where
	// possible; provider clients translate them to wire-format fields.
	ProviderOptions map[string]any
}

// ChatResponse is the normalized response from providers.
type ChatResponse struct {
	Content           string
	Phase             MessagePhase
	ProviderItemID    string
	ProviderItemModel string
	ReasoningContent  string
	ReasoningBlocks   []ReasoningBlock
	ToolCalls         []ToolCall
	Usage             *TokenUsage // optional; nil when the provider didn't return usage
	// StopReason is the raw provider stop signal, normalized to lowercase.
	// Common values: "stop" / "end_turn" (natural finish), "length" /
	// "max_tokens" (output truncation), "tool_calls" / "tool_use".
	StopReason string
	// FinishReason is Wuu's provider-neutral stop semantic. It preserves
	// OpenCode-style behavior where max_tokens/max_output_tokens are a
	// completed response with finish_reason=length, not an interrupted turn.
	FinishReason FinishReason
	// Truncated is true when the model hit its output token cap mid-response.
	// Callers can surface this as "output reached the limit" without treating
	// the turn as a user interruption or transport failure.
	Truncated bool
}

// Client sends chat requests to an LLM provider.
type Client interface {
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
}

// StreamEventType classifies events emitted during streaming.
type StreamEventType string

const (
	EventContentDelta    StreamEventType = "content_delta"
	EventContentReplace  StreamEventType = "content_replace"
	EventThinkingDelta   StreamEventType = "thinking_delta"
	EventThinkingReplace StreamEventType = "thinking_replace"
	EventThinkingDone    StreamEventType = "thinking_done"
	EventToolUseStart    StreamEventType = "tool_use_start"
	EventToolUseDelta    StreamEventType = "tool_use_delta"
	EventToolUseEnd      StreamEventType = "tool_use_end"
	EventPlanUpdate      StreamEventType = "plan_update"
	EventRequestContext  StreamEventType = "request_context"
	EventUsage           StreamEventType = "usage"
	EventMessage         StreamEventType = "message"
	EventLifecycle       StreamEventType = "lifecycle"
	EventReconnect       StreamEventType = "reconnect"
	EventCompact         StreamEventType = "compact"
	EventDone            StreamEventType = "done"
	EventError           StreamEventType = "error"
)

// StreamLifecyclePhase is the structured state of the live response transport.
type StreamLifecyclePhase string

const (
	StreamPhaseConnecting   StreamLifecyclePhase = "connecting"
	StreamPhaseConnected    StreamLifecyclePhase = "connected"
	StreamPhaseReconnecting StreamLifecyclePhase = "reconnecting"
	StreamPhaseFailed       StreamLifecyclePhase = "failed"
)

// StreamLifecycle carries retry metadata for one streaming connection attempt.
// Attempt is 1-based and includes the initial connect.
type StreamLifecycle struct {
	Phase       StreamLifecyclePhase
	Attempt     int
	MaxAttempts int
	RetryCount  int
	MaxRetries  int
	RetryIn     time.Duration
	Elapsed     time.Duration // time since first reconnect attempt
	Budget      time.Duration // total reconnection time budget
	Reason      string
}

// TokenUsage reports token consumption for a single API call. Cache
// fields are populated when the provider supports prompt caching:
// Anthropic returns them on every messages response; OpenAI returns
// `cached_tokens` under prompt_tokens_details on supporting models.
//
// IMPORTANT: cached tokens still occupy the model's context window —
// they are read out of cache and packed into the prompt, the only
// difference is the per-token price. Auto-compact's fill-rate
// calculation must include them, otherwise providers using prompt
// caching look like they're using almost no context and the trigger
// fires far too late.
type TokenUsage struct {
	InputTokens  int
	OutputTokens int
	// CacheCreationTokens are the tokens that were written into the
	// provider's prompt cache as a side effect of this call (Anthropic
	// only; OpenAI doesn't expose this separately).
	CacheCreationTokens int
	// CacheReadTokens are the tokens served out of the prompt cache
	// for this call. Reported by both Anthropic
	// (cache_read_input_tokens) and OpenAI
	// (prompt_tokens_details.cached_tokens) on models that support
	// prompt caching.
	CacheReadTokens int
	// CacheCreationUnknown is true when the provider's response did not
	// meaningfully report CacheCreationTokens. Some anthropic-compatible
	// backends (notably minimax's api.minimaxi.com) omit
	// cache_creation_input_tokens from their response entirely while
	// still serving cache_read_input_tokens, so the CacheCreationTokens
	// value above stays at zero and reads as "wuu didn't create any
	// cache" in UI even when the provider caches implicitly. Downstream
	// code should render this as N/A instead of a literal zero so users
	// don't chase a non-existent regression. The flag is stamped by the
	// provider client based on endpoint detection; the default value
	// (false) is correct for native Anthropic endpoints where a zero
	// CacheCreationTokens means "no cache written this turn".
	CacheCreationUnknown bool
}

// PlanStep is one item in an update_plan snapshot.
type PlanStep struct {
	Step   string `json:"step"`
	Status string `json:"status"`
}

// PlanUpdate carries the latest update_plan snapshot as a stream event.
type PlanUpdate struct {
	Explanation string     `json:"explanation,omitempty"`
	Plan        []PlanStep `json:"plan"`
}

// RequestContextSummary carries metadata-only context compilation details for
// one provider request. It intentionally excludes raw prompt text.
type RequestContextSummary struct {
	StepIndex         int      `json:"step_index"`
	TransientMessages int      `json:"transient_messages,omitempty"`
	ContentBytes      int      `json:"content_bytes,omitempty"`
	BlockKinds        []string `json:"block_kinds,omitempty"`
}

// TotalContextTokens returns the number of tokens this call actually
// consumed against the model's context window. Equals InputTokens +
// CacheReadTokens + OutputTokens. CacheCreationTokens are NOT
// included because the cache_creation count reported by Anthropic is
// already a subset of InputTokens — adding it would double-count.
func (u TokenUsage) TotalContextTokens() int {
	if u == (TokenUsage{}) {
		return 0
	}
	return u.InputTokens + u.CacheReadTokens + u.OutputTokens
}

// StreamEvent is a single event from a streaming chat response.
type StreamEvent struct {
	Type           StreamEventType
	Content        string
	Phase          MessagePhase
	ProviderItemID string
	Message        *ChatMessage
	ReasoningBlock *ReasoningBlock
	ToolCall       *ToolCall
	ToolResult     string
	PlanUpdate     *PlanUpdate
	RequestContext *RequestContextSummary
	Lifecycle      *StreamLifecycle
	Error          error
	Usage          *TokenUsage
	// StopReason / FinishReason / Truncated are populated on the terminal
	// EventDone when the provider reports them. StopReason is raw-ish provider
	// detail; FinishReason is the normalized semantic.
	StopReason   string
	FinishReason FinishReason
	Truncated    bool
}

// StreamClient extends Client with streaming support.
type StreamClient interface {
	Client
	StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error)
}
