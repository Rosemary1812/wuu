package tools

import "strings"

// ToolKind groups tools by their primary product behavior. It is intentionally
// coarse so future telemetry can compare broad tool classes without depending
// on provider-specific names.
type ToolKind string

const (
	ToolKindFile      ToolKind = "file"
	ToolKindSearch    ToolKind = "search"
	ToolKindDiscovery ToolKind = "discovery"
	ToolKindShell     ToolKind = "shell"
	ToolKindTest      ToolKind = "test"
	ToolKindGit       ToolKind = "git"
	ToolKindWeb       ToolKind = "web"
	ToolKindSkill     ToolKind = "skill"
	ToolKindWorkflow  ToolKind = "workflow"
	ToolKindPlan      ToolKind = "plan"
	ToolKindAgent     ToolKind = "agent"
	ToolKindProcess   ToolKind = "process"
	ToolKindSchedule  ToolKind = "schedule"
	ToolKindMCP       ToolKind = "mcp"
	ToolKindUnknown   ToolKind = "unknown"
)

// ToolExposure describes whether a known tool is currently visible to the
// model. Hidden tools remain known to the runtime, but are not exposed in
// Definitions() and cannot be executed through this toolkit.
type ToolExposure string

const (
	ToolExposureDirect   ToolExposure = "direct"
	ToolExposureHidden   ToolExposure = "hidden"
	ToolExposureDeferred ToolExposure = "deferred"
)

// ToolInfo is lightweight metadata for inspection, policy, and telemetry.
type ToolInfo struct {
	Name            string       `json:"name"`
	Kind            ToolKind     `json:"kind"`
	Exposure        ToolExposure `json:"exposure"`
	Risk            ToolRisk     `json:"risk"`
	ReadOnly        bool         `json:"read_only"`
	ConcurrencySafe bool         `json:"concurrency_safe"`
	Destructive     bool         `json:"destructive"`
	Reason          string       `json:"reason,omitempty"`
}

// ToolInfo returns metadata for a known tool. Disabled tools still return
// metadata, but with hidden exposure. This is the default classification for
// the tool without call arguments; use ToolMetadata for call-specific behavior.
func (t *Toolkit) ToolInfo(name string) (ToolInfo, bool) {
	tool := t.LookupTool(name)
	if tool == nil {
		return ToolInfo{}, false
	}
	return buildToolInfo(tool, t.toolExposure(name)), true
}

// ToolInfos returns metadata for every known tool, including hidden tools.
func (t *Toolkit) ToolInfos() []ToolInfo {
	all := t.allKnownTools()
	out := make([]ToolInfo, 0, len(all))
	for _, tool := range all {
		out = append(out, buildToolInfo(tool, t.toolExposure(tool.Name())))
	}
	return out
}

func buildToolInfo(tool Tool, exposure ToolExposure) ToolInfo {
	return buildToolInfoForArgs(tool, exposure, "")
}

func buildToolInfoForArgs(tool Tool, exposure ToolExposure, argsJSON string) ToolInfo {
	kind := classifyToolKind(tool.Name())
	classification := classifyToolCall(tool, kind, argsJSON)
	return ToolInfo{
		Name:            tool.Name(),
		Kind:            kind,
		Exposure:        exposure,
		Risk:            classification.Risk,
		ReadOnly:        classification.ReadOnly,
		ConcurrencySafe: classification.ConcurrencySafe,
		Destructive:     classification.Destructive,
		Reason:          classification.Reason,
	}
}

func (t *Toolkit) toolExposure(name string) ToolExposure {
	if t.isToolDisabled(name) {
		return ToolExposureHidden
	}
	if t.isDeferredToolActive(name) {
		return ToolExposureDirect
	}
	if isDeferredByDefault(name) {
		return ToolExposureDeferred
	}
	return ToolExposureDirect
}

func classifyToolKind(name string) ToolKind {
	switch name {
	case "read_file", "write_file", "list_files", "edit_file", "apply_patch", "checkpoint":
		return ToolKindFile
	case "grep", "glob":
		return ToolKindSearch
	case "tool_search":
		return ToolKindDiscovery
	case "run_shell":
		return ToolKindShell
	case "run_test":
		return ToolKindTest
	case "git":
		return ToolKindGit
	case "web_search", "web_fetch":
		return ToolKindWeb
	case "load_skill":
		return ToolKindSkill
	case "list_workflows", "load_workflow", "save_workflow", "list_agent_profiles", "create_agent_profile", "start_workflow", "run_workflow", "create_workflow", "workflow_control", "workflow_status":
		return ToolKindWorkflow
	case "update_plan":
		return ToolKindPlan
	case "spawn_agent", "send_message", "followup_task", "wait_agent", "await_agents", "close_agent", "list_agents", "agent_report":
		return ToolKindAgent
	case "start_process", "list_processes", "stop_process", "read_process_output", "write_stdin":
		return ToolKindProcess
	case "schedule_cron", "cancel_cron", "list_cron":
		return ToolKindSchedule
	default:
		if strings.HasPrefix(name, "mcp_") {
			return ToolKindMCP
		}
		return ToolKindUnknown
	}
}

func isDeferredByDefault(name string) bool {
	if strings.HasPrefix(name, "mcp_") {
		return true
	}
	switch name {
	case "schedule_cron", "cancel_cron", "list_cron":
		return true
	default:
		return false
	}
}
