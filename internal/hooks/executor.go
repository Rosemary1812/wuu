package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/providers"
)

// ToolExecutor matches agent.ToolExecutor so HookedExecutor can decorate
// the real Toolkit without importing the agent package.
type ToolExecutor interface {
	Definitions() []providers.ToolDefinition
	Execute(ctx context.Context, call providers.ToolCall) (string, error)
}

// HookedExecutor decorates a ToolExecutor with PreToolUse / PostToolUse /
// PostToolUseFailure hooks.  It implements ToolExecutor and can be used as
// a drop-in replacement wherever a Toolkit is expected.
//
// It also implements agent.ToolContextProvider: after each Execute call,
// LastAdditionalContext() returns any additional_context from PostToolUse
// hooks. The agent loop projects this into the next provider request as
// request-only context so the model sees hook-provided context without
// adding it to durable conversation history.
type HookedExecutor struct {
	inner             ToolExecutor
	dispatcher        *Dispatcher
	sessionID         string
	cwd               string
	lastAdditionalCtx string // populated by PostToolUse hooks
}

// NewHookedExecutor wraps inner with hook dispatch.
func NewHookedExecutor(inner ToolExecutor, d *Dispatcher, sessionID, cwd string) *HookedExecutor {
	return &HookedExecutor{
		inner:      inner,
		dispatcher: d,
		sessionID:  sessionID,
		cwd:        cwd,
	}
}

// Definitions delegates to the inner executor.
func (h *HookedExecutor) Definitions() []providers.ToolDefinition {
	return h.inner.Definitions()
}

// SupportsTool forwards tool-surface checks through the hook layer. This keeps
// deferred tool support visible to the agent loop even when hooks decorate the
// real Toolkit.
func (h *HookedExecutor) SupportsTool(name string) bool {
	if h == nil || h.inner == nil {
		return false
	}
	if sp, ok := h.inner.(agent.ToolSupportProvider); ok {
		return sp.SupportsTool(name)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, def := range h.inner.Definitions() {
		if strings.EqualFold(strings.TrimSpace(def.Name), name) {
			return true
		}
	}
	return false
}

// ToolMetadata forwards to the inner executor if it implements
// agent.ToolMetadataProvider, so the loop's concurrency partitioning
// works through the hook layer.
func (h *HookedExecutor) ToolMetadata(call providers.ToolCall) (agent.ToolMetadata, bool) {
	if mp, ok := h.inner.(agent.ToolMetadataProvider); ok {
		return mp.ToolMetadata(call)
	}
	return agent.ToolMetadata{}, false
}

// DiscoveredTools forwards provider-native deferred tool discovery through the
// hook layer. Without this, successful tools can activate deferred schemas in
// the inner Toolkit while the agent loop cannot attach them to the tool result.
func (h *HookedExecutor) DiscoveredTools(call providers.ToolCall) []providers.LoadableToolDefinition {
	if h == nil || h.inner == nil {
		return nil
	}
	if dp, ok := h.inner.(agent.ToolDiscoveryProvider); ok {
		return providers.CloneLoadableToolDefinitions(dp.DiscoveredTools(call))
	}
	return nil
}

// Execute fires PreToolUse hooks, delegates to the inner executor, then
// fires PostToolUse (on success) or PostToolUseFailure (on error).
//
// If a PreToolUse hook blocks, the inner executor is never called.
// If a PreToolUse hook returns UpdatedInput, the tool call arguments are
// replaced before delegation.
//
// PostToolUse and PostToolUseFailure hooks are fire-and-forget: their
// errors are not propagated so they cannot mask the real tool outcome.
func (h *HookedExecutor) Execute(ctx context.Context, call providers.ToolCall) (string, error) {
	h.lastAdditionalCtx = "" // reset for this call
	input := &Input{
		SessionID: h.sessionID,
		CWD:       h.cwd,
		ToolName:  call.Name,
		ToolInput: json.RawMessage(call.Arguments),
	}

	// PreToolUse
	out, err := h.dispatcher.Dispatch(ctx, PreToolUse, input)
	if err != nil {
		return "", fmt.Errorf("hook blocked %s: %w", call.Name, err)
	}
	if len(out.UpdatedInput) > 0 {
		call.Arguments = string(out.UpdatedInput)
	}

	// Delegate to real tool.
	result, execErr := h.inner.Execute(ctx, call)

	// PostToolUse / PostToolUseFailure
	if execErr != nil {
		failInput := &Input{
			SessionID: h.sessionID,
			CWD:       h.cwd,
			ToolName:  call.Name,
			ToolInput: json.RawMessage(call.Arguments),
			Error:     execErr.Error(),
		}
		_, _ = h.dispatcher.Dispatch(ctx, PostToolUseFailure, failInput)
		return result, execErr
	}

	postInput := &Input{
		SessionID:    h.sessionID,
		CWD:          h.cwd,
		ToolName:     call.Name,
		ToolInput:    json.RawMessage(call.Arguments),
		ToolResponse: result,
	}
	postOut, _ := h.dispatcher.Dispatch(ctx, PostToolUse, postInput)
	if postOut != nil && postOut.Context != "" {
		h.lastAdditionalCtx = postOut.Context
	}

	return result, nil
}

// LastAdditionalContext returns the additional context from the most
// recent PostToolUse hook execution, if any. The value is cleared on
// each Execute call so it can only be consumed once. Implements
// agent.ToolContextProvider.
func (h *HookedExecutor) LastAdditionalContext() string {
	return h.lastAdditionalCtx
}
