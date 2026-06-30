package tools

import "strings"

var subagentManagementTools = []string{
	"send_message",
	"followup_task",
	"await_agents",
	"close_agent",
	"list_agents",
}

func (t *Toolkit) activateToolBundlesAfterSuccess(toolName string) {
	switch strings.TrimSpace(toolName) {
	case "spawn_agent":
		t.activateSubagentManagementTools()
	}
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

func (t *Toolkit) activateSubagentManagementTools() {
	if t == nil {
		return
	}
	t.markDeferredToolsLoaded(subagentManagementTools...)
}

func isSubagentManagementTool(name string) bool {
	for _, toolName := range subagentManagementTools {
		if name == toolName {
			return true
		}
	}
	return false
}
