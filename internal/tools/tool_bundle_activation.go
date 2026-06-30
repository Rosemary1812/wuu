package tools

import (
	"strings"

	"github.com/blueberrycongee/wuu/internal/providers"
)

var subagentManagementTools = []string{
	"send_message",
	"followup_task",
	"await_agents",
	"close_agent",
	"list_agents",
}

func (t *Toolkit) activateToolBundlesAfterSuccess(toolName string) []providers.LoadableToolDefinition {
	switch strings.TrimSpace(toolName) {
	case "spawn_agent", "helpme":
		return t.activateSubagentManagementTools()
	}
	return nil
}

func (t *Toolkit) refreshStateActivatedToolBundles() {
	if t == nil || t.env == nil || t.env.AgentControl == nil {
		return
	}
	if len(t.env.AgentControl.ListFrom(currentAgentPath(t.env), "")) == 0 {
		return
	}
	t.activateSubagentManagementTools()
}

func (t *Toolkit) activateSubagentManagementTools() []providers.LoadableToolDefinition {
	if t == nil {
		return nil
	}
	t.markDeferredToolsLoaded(subagentManagementTools...)
	return t.loadableToolDefinitions(subagentManagementTools...)
}

func isSubagentManagementTool(name string) bool {
	for _, toolName := range subagentManagementTools {
		if name == toolName {
			return true
		}
	}
	return false
}

func (t *Toolkit) recordDiscoveredToolsForCall(call providers.ToolCall, tools []providers.LoadableToolDefinition) {
	if t == nil || len(tools) == 0 {
		return
	}
	key := discoveredToolCallKey(call)
	if key == "" {
		return
	}
	t.discoveryMu.Lock()
	defer t.discoveryMu.Unlock()
	if t.discoveredToolsByCall == nil {
		t.discoveredToolsByCall = make(map[string][]providers.LoadableToolDefinition)
	}
	t.discoveredToolsByCall[key] = providers.CloneLoadableToolDefinitions(tools)
}

func (t *Toolkit) DiscoveredTools(call providers.ToolCall) []providers.LoadableToolDefinition {
	if t == nil {
		return nil
	}
	key := discoveredToolCallKey(call)
	if key == "" {
		return nil
	}
	t.discoveryMu.Lock()
	defer t.discoveryMu.Unlock()
	tools := providers.CloneLoadableToolDefinitions(t.discoveredToolsByCall[key])
	delete(t.discoveredToolsByCall, key)
	return tools
}

func discoveredToolCallKey(call providers.ToolCall) string {
	if id := strings.TrimSpace(call.ID); id != "" {
		return id
	}
	name := strings.TrimSpace(call.Name)
	if name == "" {
		return ""
	}
	return name + "\x00" + strings.TrimSpace(call.Arguments)
}

func (t *Toolkit) loadableToolDefinitions(names ...string) []providers.LoadableToolDefinition {
	if t == nil || t.registry == nil || len(names) == 0 {
		return nil
	}
	out := make([]providers.LoadableToolDefinition, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || t.isToolDisabled(name) {
			continue
		}
		tool := t.registry.Lookup(name)
		if tool == nil {
			continue
		}
		if t.toolExposure(name) != ToolExposureDeferred {
			continue
		}
		out = append(out, providers.LoadableToolFromDefinition(tool.Definition()))
	}
	return out
}
