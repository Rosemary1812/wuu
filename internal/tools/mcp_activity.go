package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/blueberrycongee/wuu/internal/activity"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/toolresult"
)

type MCPActivityBinding struct {
	Kind     activity.Kind
	PluginID string
}

func (t *Toolkit) SetActivityRegistry(registry *activity.Registry) {
	if t == nil {
		return
	}
	t.activityRegistry = registry
}

func (t *Toolkit) SetMCPActivityBindings(bindings map[string]MCPActivityBinding) {
	if t == nil {
		return
	}
	t.mcpActivityBindings = cloneMCPActivityBindings(bindings)
}

func cloneMCPActivityBindings(bindings map[string]MCPActivityBinding) map[string]MCPActivityBinding {
	if len(bindings) == 0 {
		return nil
	}
	out := make(map[string]MCPActivityBinding, len(bindings))
	for server, binding := range bindings {
		server = strings.TrimSpace(server)
		binding.PluginID = strings.TrimSpace(binding.PluginID)
		if server != "" && binding.PluginID != "" && binding.Kind != "" {
			out[server] = binding
		}
	}
	return out
}

func (t *Toolkit) executeActivityBoundToolResult(ctx context.Context, call providers.ToolCall, tool Tool, serverName string) (toolresult.Result, error) {
	binding, bound := t.mcpActivityBindings[strings.TrimSpace(serverName)]
	if !bound {
		return t.executeKnownToolResult(ctx, call, tool)
	}
	if t.activityRegistry == nil {
		return toolresult.Result{}, errors.New("activity registry is unavailable")
	}
	threadID := ""
	workdir := ""
	if t.env != nil {
		threadID = strings.TrimSpace(t.env.SessionID)
		workdir = strings.TrimSpace(t.env.RootDir)
	}
	if threadID == "" {
		return toolresult.Result{}, errors.New("activity-bound MCP tool requires a thread context")
	}
	target := mcpActivityTarget(call.Arguments)
	session, lease, err := t.activityRegistry.Acquire(activity.StartOptions{
		Kind:     binding.Kind,
		ThreadID: threadID,
		Workdir:  workdir,
		PluginID: binding.PluginID,
		Target:   target,
	})
	if err != nil {
		return toolresult.Result{}, err
	}
	if err := t.activityRegistry.CheckControl(threadID, session.ID, lease.Token); err != nil {
		return toolresult.Result{}, err
	}
	if target != "" && target != session.Target {
		if updated, updateErr := t.activityRegistry.Update(threadID, session.ID, activity.UpdateOptions{Target: target}); updateErr == nil {
			session = updated
		}
	}

	result, callErr := t.executeKnownToolResult(ctx, call, tool)
	// Re-check the lease before publishing success. A takeover that happened
	// while the helper was running invalidates the result and prevents the
	// agent from chaining another action from stale UI state.
	if controlErr := t.activityRegistry.CheckControl(threadID, session.ID, lease.Token); controlErr != nil {
		return toolresult.Result{}, controlErr
	}
	state := activity.StateActive
	update := activity.UpdateOptions{State: state}
	if callErr != nil {
		update.State = activity.StateError
		update.Error = callErr.Error()
	}
	if updated, updateErr := t.activityRegistry.Update(threadID, session.ID, update); updateErr == nil {
		session = updated
	} else if callErr == nil {
		callErr = fmt.Errorf("update activity: %w", updateErr)
	}
	result.Activity = &toolresult.ActivityRef{
		ID:         session.ID,
		Kind:       string(session.Kind),
		State:      string(session.State),
		ThreadID:   session.ThreadID,
		PreviewURI: session.Preview,
	}
	return result, callErr
}

func mcpActivityTarget(arguments string) string {
	var values map[string]any
	if json.Unmarshal([]byte(arguments), &values) != nil {
		return ""
	}
	for _, key := range []string{"app", "bundle_id", "target"} {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
