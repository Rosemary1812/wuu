package tools

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/blueberrycongee/wuu/internal/agentthread"
)

const (
	directMCPToolLimit           = 4
	directMCPDescriptionMaxRunes = 600
	directMCPInputSchemaMaxBytes = 8 * 1024
)

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
	ToolKindMemory    ToolKind = "memory"
	ToolKindSkill     ToolKind = "skill"
	ToolKindGoal      ToolKind = "goal"
	ToolKindWorkflow  ToolKind = "workflow"
	ToolKindPlan      ToolKind = "plan"
	ToolKindContext   ToolKind = "context"
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
	// The bash-first surface collapses every legacy command entry
	// point into a single "bash" tool. The model never has to guess
	// between run_shell / run_test / start_process / git. The
	// legacy names are kept as internal / advanced implementations
	// for replay, progressive disclosure, and the bash result
	// post-processor; they stay registered in the toolkit so
	// LookupTool still finds them, but toolExposure returns Hidden
	// for every surface so they never appear in Definitions.
	if isAdvancedCommandToolHidden(name) {
		return ToolExposureHidden
	}
	if !t.extensionSurfacePolicy.allowsKind(classifyToolKind(name)) {
		return ToolExposureHidden
	}
	surface := t.activeCompiledSurface()
	if surface.ProfileName != "" {
		if !activeSurfaceAllowsKnownTool(surface, t.LookupTool(name)) {
			return ToolExposureHidden
		}
		if classifyToolKind(name) == ToolKindMCP {
			return ToolExposureDeferred
		}
	}
	if classifyToolKind(name) == ToolKindMCP {
		if t.shouldExposeMCPDirectly(name) {
			return ToolExposureDirect
		}
		return ToolExposureDeferred
	}
	if t.shouldDeferByDefault(name) {
		return ToolExposureDeferred
	}
	return ToolExposureDirect
}

// isAdvancedCommandToolHidden reports whether the given tool name
// belongs to the set of advanced / legacy command tools that the
// bash-first surface demotes to internal. The model-facing command
// surface is "bash" only; these names stay registered so the
// internal callers (tool_search, replay, the bash result
// post-processor that adds test summaries) can still reach them.
//
// The set covers the legacy run_shell, the run_test verifier, the
// five managed-process tools (start_process / list_processes /
// read_process_output / write_stdin / stop_process), and the
// structured git tool.
func isAdvancedCommandToolHidden(name string) bool {
	switch name {
	case "run_shell",
		"run_test",
		"git",
		"start_process",
		"list_processes",
		"read_process_output",
		"write_stdin",
		"stop_process":
		return true
	}
	return false
}

func (t *Toolkit) shouldExposeMCPDirectly(name string) bool {
	tools := t.mcpToolsForExposure()
	if len(tools) == 0 || len(tools) > directMCPToolLimit {
		return false
	}
	for _, tool := range tools {
		if tool.Name() != name {
			continue
		}
		return isDirectMCPToolCandidate(tool)
	}
	return false
}

func (t *Toolkit) mcpToolsForExposure() []Tool {
	all := t.allKnownTools()
	out := make([]Tool, 0, len(all))
	for _, tool := range all {
		if classifyToolKind(tool.Name()) == ToolKindMCP {
			out = append(out, tool)
		}
	}
	return out
}

func isDirectMCPToolCandidate(tool Tool) bool {
	def := tool.Definition()
	if strings.TrimSpace(def.Name) == "" {
		return false
	}
	if utf8.RuneCountInString(def.Description) > directMCPDescriptionMaxRunes {
		return false
	}
	if len(def.InputSchema) > 0 {
		raw, err := json.Marshal(def.InputSchema)
		if err != nil || len(raw) > directMCPInputSchemaMaxBytes {
			return false
		}
	}
	return true
}

func classifyToolKind(name string) ToolKind {
	switch name {
	case "read_file", "write_file", "list_files", "edit_file", "apply_patch", "checkpoint":
		return ToolKindFile
	case "grep", "glob", "ast_search", "semantic_search":
		return ToolKindSearch
	case "tool_search":
		return ToolKindDiscovery
	case "run_shell", "bash":
		return ToolKindShell
	case "run_test":
		return ToolKindTest
	case "git":
		return ToolKindGit
	case "web_search", "web_fetch":
		return ToolKindWeb
	case "read_memory", "write_memory", "session_memory":
		return ToolKindMemory
	case "load_skill":
		return ToolKindSkill
	case "create_goal", "get_goal", "update_goal":
		return ToolKindGoal
	case "list_workflows", "load_workflow", "save_workflow", "list_agent_profiles", "create_agent_profile", "start_workflow", "run_workflow", "create_workflow", "workflow_control", "workflow_status":
		return ToolKindWorkflow
	case "update_plan":
		return ToolKindPlan
	case "inception":
		return ToolKindContext
	case "spawn_agent", "helpme", "send_message", "followup_task", "wait_agent", "await_agents", "close_agent", "list_agents", "agent_report":
		return ToolKindAgent
	case "start_process", "list_processes", "stop_process", "read_process_output", "write_stdin", "report_listening_ports":
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
	case "schedule_cron", "cancel_cron", "list_cron", "run_workflow", "create_workflow":
		return true
	default:
		return false
	}
}

func (t *Toolkit) shouldDeferByDefault(name string) bool {
	if name == "agent_report" && t != nil && t.env != nil && strings.TrimSpace(t.env.AgentID) != "" && currentAgentPath(t.env) != agentthread.RootPath {
		return false
	}
	if isDeferredByDefault(name) {
		return true
	}
	if t != nil && t.activeCompiledSurface().ProfileName != "" && isProfileDeferredByDefault(name) {
		return true
	}
	return false
}

func isProfileDeferredByDefault(name string) bool {
	switch strings.TrimSpace(name) {
	case "ast_search",
		"semantic_search",
		"web_search",
		"web_fetch",
		"session_memory",
		"create_goal",
		"get_goal",
		"update_goal",
		"list_workflows",
		"load_workflow",
		"save_workflow",
		"list_agent_profiles",
		"create_agent_profile",
		"start_workflow",
		"workflow_control",
		"workflow_status",
		"spawn_agent",
		"helpme",
		"send_message",
		"followup_task",
		"wait_agent",
		"await_agents",
		"close_agent",
		"list_agents",
		"agent_report":
		return true
	default:
		return false
	}
}
