package tools

import "fmt"

// ToolRisk is a coarse execution risk level used by policy, telemetry, and
// future approval/sandbox plumbing. It describes the expected blast radius of
// invoking a tool, not whether a specific call is correct.
type ToolRisk string

const (
	ToolRiskLow    ToolRisk = "low"
	ToolRiskMedium ToolRisk = "medium"
	ToolRiskHigh   ToolRisk = "high"
)

// ToolPolicyAction is the runtime decision for one tool call.
type ToolPolicyAction string

const (
	ToolPolicyAllow           ToolPolicyAction = "allow"
	ToolPolicyDeny            ToolPolicyAction = "deny"
	ToolPolicyRequireApproval ToolPolicyAction = "require_approval"
)

// ToolPolicy decides whether a tool call may run. The zero value is the local
// high-trust mode: every call is allowed unless an explicit override says
// otherwise.
type ToolPolicy struct {
	DefaultAction ToolPolicyAction
	ToolActions   map[string]ToolPolicyAction
	KindActions   map[ToolKind]ToolPolicyAction
	RiskActions   map[ToolRisk]ToolPolicyAction
}

// ToolPolicyDecision is produced before each known tool call executes.
type ToolPolicyDecision struct {
	Action ToolPolicyAction `json:"action"`
	Risk   ToolRisk         `json:"risk"`
	Reason string           `json:"reason,omitempty"`
}

func (p ToolPolicy) Decide(info ToolInfo) ToolPolicyDecision {
	action, reason := p.actionFor(info)
	if action == "" {
		action = ToolPolicyAllow
		reason = "default allow"
	}
	if !IsValidToolPolicyAction(action) {
		action = ToolPolicyAllow
		reason = "invalid policy action defaulted to allow"
	}
	return ToolPolicyDecision{
		Action: action,
		Risk:   info.Risk,
		Reason: reason,
	}
}

func (p ToolPolicy) actionFor(info ToolInfo) (ToolPolicyAction, string) {
	if action := p.ToolActions[info.Name]; action != "" {
		return action, "tool policy"
	}
	if action := p.KindActions[info.Kind]; action != "" {
		return action, "kind policy"
	}
	if action := p.RiskActions[info.Risk]; action != "" {
		return action, "risk policy"
	}
	if p.DefaultAction != "" {
		return p.DefaultAction, "default policy"
	}
	return "", ""
}

func (d ToolPolicyDecision) blockingError(toolName string) error {
	switch d.Action {
	case ToolPolicyDeny:
		return fmt.Errorf("tool %q denied by policy (%s risk)", toolName, d.Risk)
	case ToolPolicyRequireApproval:
		return fmt.Errorf("tool %q requires approval by policy (%s risk), but approval UI is not available yet", toolName, d.Risk)
	default:
		return nil
	}
}

func IsValidToolPolicyAction(action ToolPolicyAction) bool {
	switch action {
	case "", ToolPolicyAllow, ToolPolicyDeny, ToolPolicyRequireApproval:
		return true
	default:
		return false
	}
}

func classifyToolRisk(_ string, kind ToolKind, readOnly bool) ToolRisk {
	switch kind {
	case ToolKindShell, ToolKindGit:
		return ToolRiskHigh
	case ToolKindFile:
		if readOnly {
			return ToolRiskLow
		}
		return ToolRiskHigh
	case ToolKindProcess, ToolKindSchedule, ToolKindAgent, ToolKindBrowser, ToolKindMCP:
		if readOnly {
			return ToolRiskMedium
		}
		return ToolRiskHigh
	case ToolKindWeb, ToolKindUserInteraction:
		return ToolRiskMedium
	case ToolKindDiscovery, ToolKindSkill, ToolKindPlan, ToolKindSearch:
		return ToolRiskLow
	default:
		if readOnly {
			return ToolRiskLow
		}
		return ToolRiskHigh
	}
}
