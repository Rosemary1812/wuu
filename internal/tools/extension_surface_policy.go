package tools

import "fmt"

// ExtensionSurfacePolicy controls tools backed by extension surfaces such as
// MCP, skills, and workflows. The zero value preserves the existing main
// session behavior: all extension-backed tools are available.
type ExtensionSurfacePolicy struct {
	Enforce        bool
	AllowMCP       bool
	AllowSkills    bool
	AllowWorkflows bool
}

// RestrictedExtensionSurfacePolicy disables extension-backed tools. It is the
// default shape for reviewer-like runtimes where extensions must opt in
// explicitly instead of entering through deferred discovery.
func RestrictedExtensionSurfacePolicy() ExtensionSurfacePolicy {
	return ExtensionSurfacePolicy{Enforce: true}
}

func (p ExtensionSurfacePolicy) Check(info ToolInfo) error {
	if p.allowsKind(info.Kind) {
		return nil
	}
	return extensionSurfaceError{
		ToolName:        info.Name,
		Kind:            info.Kind,
		Surface:         extensionSurfaceForKind(info.Kind),
		ModelNextAction: "use built-in observation or planning tools, or report that this extension surface is disabled",
	}
}

func (p ExtensionSurfacePolicy) allowsKind(kind ToolKind) bool {
	if !p.Enforce {
		return true
	}
	switch kind {
	case ToolKindMCP:
		return p.AllowMCP
	case ToolKindSkill:
		return p.AllowSkills
	case ToolKindWorkflow:
		return p.AllowWorkflows
	default:
		return true
	}
}

func extensionSurfaceForKind(kind ToolKind) string {
	switch kind {
	case ToolKindMCP:
		return "mcp"
	case ToolKindSkill:
		return "skills"
	case ToolKindWorkflow:
		return "workflows"
	default:
		return "unknown"
	}
}

type extensionSurfaceError struct {
	ToolName        string
	Kind            ToolKind
	Surface         string
	ModelNextAction string
}

func (e extensionSurfaceError) Error() string {
	return fmt.Sprintf(
		"tool %q blocked by extension surface policy: error_kind=extension_surface_denied surface=%s kind=%s model_next_action=%q",
		e.ToolName,
		e.Surface,
		e.Kind,
		e.ModelNextAction,
	)
}

type ExtensionToolSurfaceStates struct {
	MCP       ExtensionToolSurfaceState `json:"mcp"`
	Skills    ExtensionToolSurfaceState `json:"skills"`
	Workflows ExtensionToolSurfaceState `json:"workflows"`
}

type ExtensionToolSurfaceState struct {
	Allowed      bool `json:"allowed"`
	KnownTools   int  `json:"known_tools"`
	VisibleTools int  `json:"visible_tools"`
}

// ExtensionToolSurfaceStates summarizes extension-backed tools known to this
// toolkit after policy filtering. It is intended for UI/status reporting.
func (t *Toolkit) ExtensionToolSurfaceStates() ExtensionToolSurfaceStates {
	var states ExtensionToolSurfaceStates
	if t == nil {
		return states
	}
	states.MCP.Allowed = t.extensionSurfacePolicy.allowsKind(ToolKindMCP)
	states.Skills.Allowed = t.extensionSurfacePolicy.allowsKind(ToolKindSkill)
	states.Workflows.Allowed = t.extensionSurfacePolicy.allowsKind(ToolKindWorkflow)

	for _, tool := range t.allKnownTools() {
		state := stateForExtensionKind(&states, classifyToolKind(tool.Name()))
		if state == nil {
			continue
		}
		state.KnownTools++
		if t.toolExposure(tool.Name()) == ToolExposureDirect {
			state.VisibleTools++
		}
	}
	return states
}

func stateForExtensionKind(states *ExtensionToolSurfaceStates, kind ToolKind) *ExtensionToolSurfaceState {
	if states == nil {
		return nil
	}
	switch kind {
	case ToolKindMCP:
		return &states.MCP
	case ToolKindSkill:
		return &states.Skills
	case ToolKindWorkflow:
		return &states.Workflows
	default:
		return nil
	}
}
