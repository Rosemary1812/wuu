package agent

import (
	"context"
	"errors"
	"strings"

	"github.com/blueberrycongee/wuu/internal/compact"
	"github.com/blueberrycongee/wuu/internal/providers"
)

// ToolExecutor executes model-requested tool calls.
type ToolExecutor interface {
	Definitions() []providers.ToolDefinition
	Execute(ctx context.Context, call providers.ToolCall) (string, error)
}

// ToolMetadata describes a tool's scheduling and policy characteristics.
type ToolMetadata struct {
	ReadOnly        bool
	ConcurrencySafe bool
	Destructive     bool
	Risk            string
	Reason          string
}

// ToolMetadataProvider is an optional interface a ToolExecutor can
// implement to expose per-tool metadata (read-only, concurrency-safe).
// The agent loop uses this to partition tool calls: read-only tools run
// concurrently, write tools run serially.
type ToolMetadataProvider interface {
	ToolMetadata(call providers.ToolCall) (ToolMetadata, bool)
}

// ToolDisplayProvider is an optional interface a ToolExecutor can implement
// to provide user-facing labels for tool calls. UI clients should treat this
// as display metadata only; it is not sent to model providers.
type ToolDisplayProvider interface {
	ToolDisplay(call providers.ToolCall) (providers.ToolCallDisplay, bool)
}

// ToolContextProvider is an optional interface a ToolExecutor can
// implement to return additional context alongside tool results.
// Hook systems use this to inject context into the conversation after
// PostToolUse hooks run.
type ToolContextProvider interface {
	// LastAdditionalContext returns the additional context string
	// from the most recent Execute call, if any. Callers should
	// check this after each Execute and inject non-empty values
	// as system messages.
	LastAdditionalContext() string
}

// Runner manages one multi-step coding turn. It is a thin wrapper
// around RunToolLoop that always executes through the streaming Step
// path; unary clients are adapted underneath via AdaptStreamClient.
type Runner struct {
	Client       providers.Client
	Tools        ToolExecutor
	Model        string
	SystemPrompt string
	MaxSteps     int
	Temperature  float64
	// ContextWindowOverride pins the context window for this run
	// instead of consulting the known-model registry. Zero disables
	// proactive compaction when the model is unknown.
	ContextWindowOverride int
	// MaxInputTokens pins the prompt/input budget when lower than the
	// model's total context window.
	MaxInputTokens int
	// OutputReserveTokens pins the output budget used for compact threshold
	// math without forcing a request max_tokens value.
	OutputReserveTokens int
}

// RunResult is the structured outcome of a Runner.RunWithUsage call.
type RunResult struct {
	Content             string
	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int
	FinishReason        providers.FinishReason
	StopReason          string
	Truncated           bool
}

// Run executes one prompt with optional tool-use loop.
func (r *Runner) Run(ctx context.Context, prompt string) (string, error) {
	res, err := r.RunWithUsage(ctx, prompt, nil)
	if err != nil {
		return "", err
	}
	return res.Content, nil
}

// RunWithUsage is like Run but reports per-call token usage to the
// optional onUsage callback (called once per LLM round-trip) and
// returns cumulative totals in the result.
func (r *Runner) RunWithUsage(ctx context.Context, prompt string, onUsage func(input, output int)) (RunResult, error) {
	if r.Client == nil {
		return RunResult{}, errors.New("client is required")
	}
	if strings.TrimSpace(r.Model) == "" {
		return RunResult{}, errors.New("model is required")
	}
	if strings.TrimSpace(prompt) == "" {
		return RunResult{}, errors.New("prompt is required")
	}

	// Build the initial conversation: optional system prompt + the
	// user's request. The shared loop takes it from there.
	history := make([]providers.ChatMessage, 0, 2)
	if strings.TrimSpace(r.SystemPrompt) != "" {
		history = append(history, providers.ChatMessage{Role: "system", Content: r.SystemPrompt})
	}
	history = append(history, providers.ChatMessage{Role: "user", Content: prompt})

	maxCtx := r.ContextWindowOverride
	if maxCtx <= 0 {
		if window, ok := providers.KnownContextWindowFor(r.Model); ok {
			maxCtx = window
		}
	}
	cfg := LoopConfig{
		Tools:               r.Tools,
		Model:               r.Model,
		Temperature:         r.Temperature,
		MaxSteps:            r.MaxSteps,
		MaxContextTokens:    maxCtx,
		MaxInputTokens:      r.MaxInputTokens,
		OutputReserveTokens: r.OutputReserveTokens,
		OnUsage:             onUsage,
		Compact: func(ctx context.Context, messages []providers.ChatMessage) ([]providers.ChatMessage, error) {
			return compact.CompactWithBudget(ctx, messages, r.Client, r.Model, compact.Budget{
				ContextTokens:       maxCtx,
				InputTokens:         r.MaxInputTokens,
				OutputReserveTokens: r.OutputReserveTokens,
			})
		},
	}

	res, err := RunToolLoop(ctx, history, cfg, &streamStep{client: providers.AdaptStreamClient(r.Client)})
	return RunResult{
		Content:             res.Content,
		InputTokens:         res.InputTokens,
		OutputTokens:        res.OutputTokens,
		CacheCreationTokens: res.CacheCreationTokens,
		CacheReadTokens:     res.CacheReadTokens,
		FinishReason:        res.FinishReason,
		StopReason:          res.StopReason,
		Truncated:           res.Truncated,
	}, err
}
