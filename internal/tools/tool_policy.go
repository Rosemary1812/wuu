package tools

import (
	"fmt"

	"github.com/blueberrycongee/wuu/internal/capability"
)

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
	ToolPolicyAutoClassify    ToolPolicyAction = "auto_classify"
)

type ToolPolicyProfile string

const (
	ToolPolicyProfileReadOnly   ToolPolicyProfile = "read_only"
	ToolPolicyProfileAgent      ToolPolicyProfile = "agent"
	ToolPolicyProfileAutoReview ToolPolicyProfile = "auto_review"
	ToolPolicyProfileFullAccess ToolPolicyProfile = "full_access"
)

type ToolApprovalPolicy string

const (
	ToolApprovalPolicyOnRequest ToolApprovalPolicy = "on_request"
	ToolApprovalPolicyNever     ToolApprovalPolicy = "never"
)

// ToolPolicy decides whether a tool call may run. The zero value is the local
// high-trust mode: every call is allowed unless an explicit override says
// otherwise.
type ToolPolicy struct {
	Profile        ToolPolicyProfile
	ApprovalPolicy ToolApprovalPolicy
	DefaultAction  ToolPolicyAction
	ToolActions    map[string]ToolPolicyAction
	KindActions    map[ToolKind]ToolPolicyAction
	RiskActions    map[ToolRisk]ToolPolicyAction
}

// ToolPolicyDecision is produced before each known tool call executes.
type ToolPolicyDecision struct {
	Action           ToolPolicyAction      `json:"action"`
	Risk             ToolRisk              `json:"risk"`
	Reason           string                `json:"reason,omitempty"`
	AutoModeDecision AutoModeDecision      `json:"auto_mode_decision,omitempty"`
	AutoModeReason   string                `json:"auto_mode_reason,omitempty"`
	Capability       capability.Capability `json:"capability,omitempty"`
	CapabilityObject string                `json:"capability_object,omitempty"`
	CapabilityAction string                `json:"capability_action,omitempty"`
	CapabilityRule   string                `json:"capability_rule,omitempty"`
}

func PolicyForProfile(profile ToolPolicyProfile) (ToolPolicy, bool) {
	switch profile {
	case "":
		return ToolPolicy{}, true
	case ToolPolicyProfileReadOnly:
		return ToolPolicy{
			Profile:        profile,
			ApprovalPolicy: ToolApprovalPolicyOnRequest,
			DefaultAction:  ToolPolicyAllow,
		}, true
	case ToolPolicyProfileAgent, ToolPolicyProfileAutoReview, ToolPolicyProfileFullAccess:
		policy := ToolPolicy{
			Profile:        profile,
			ApprovalPolicy: ToolApprovalPolicyOnRequest,
			DefaultAction:  ToolPolicyAllow,
		}
		if profile == ToolPolicyProfileFullAccess {
			policy.ApprovalPolicy = ToolApprovalPolicyNever
		}
		return policy, true
	default:
		return ToolPolicy{}, false
	}
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
		return toolPolicyBlockError{
			Kind:            "policy_denied",
			ToolName:        toolName,
			Action:          d.Action,
			Risk:            d.Risk,
			PolicyReason:    d.Reason,
			ModelNextAction: "choose a lower-risk tool or explain that policy blocks the requested action",
		}
	case ToolPolicyRequireApproval:
		return toolPolicyBlockError{
			Kind:            "approval_required",
			ToolName:        toolName,
			Action:          d.Action,
			Risk:            d.Risk,
			PolicyReason:    d.Reason,
			ModelNextAction: "ask the user for approval or choose a lower-risk alternative",
		}
	default:
		return nil
	}
}

func autoModeBlockError(toolName string, decision ToolPolicyDecision, kind string) error {
	reason := decision.AutoModeReason
	if reason == "" {
		reason = decision.Reason
	}
	return toolPolicyBlockError{
		Kind:            kind,
		ToolName:        toolName,
		Action:          decision.Action,
		Risk:            decision.Risk,
		PolicyReason:    reason,
		ModelNextAction: "ask the user for approval or choose a lower-risk alternative",
	}
}

type toolPolicyBlockError struct {
	Kind            string
	ToolName        string
	Action          ToolPolicyAction
	Risk            ToolRisk
	PolicyReason    string
	ModelNextAction string
}

func (e toolPolicyBlockError) Error() string {
	return fmt.Sprintf(
		"tool %q blocked by policy: error_kind=%s policy_action=%s risk=%s policy_reason=%q model_next_action=%q approval_options=[ask_user, choose_lower_risk_alternative, stop]",
		e.ToolName,
		e.Kind,
		e.Action,
		e.Risk,
		e.PolicyReason,
		e.ModelNextAction,
	)
}

func IsValidToolPolicyAction(action ToolPolicyAction) bool {
	switch action {
	case "", ToolPolicyAllow, ToolPolicyDeny, ToolPolicyRequireApproval, ToolPolicyAutoClassify:
		return true
	default:
		return false
	}
}

func classifyToolRisk(_ string, kind ToolKind, readOnly bool) ToolRisk {
	switch kind {
	case ToolKindShell, ToolKindGit:
		return ToolRiskHigh
	case ToolKindTest:
		return ToolRiskMedium
	case ToolKindFile:
		if readOnly {
			return ToolRiskLow
		}
		return ToolRiskHigh
	case ToolKindProcess, ToolKindSchedule, ToolKindAgent, ToolKindMCP:
		if readOnly {
			return ToolRiskMedium
		}
		return ToolRiskHigh
	case ToolKindWeb:
		return ToolRiskMedium
	case ToolKindMemory:
		if readOnly {
			return ToolRiskLow
		}
		return ToolRiskMedium
	case ToolKindGoal:
		return ToolRiskLow
	case ToolKindWorkflow:
		if readOnly {
			return ToolRiskLow
		}
		return ToolRiskHigh
	case ToolKindDiscovery, ToolKindSkill, ToolKindPlan, ToolKindContext, ToolKindSearch:
		return ToolRiskLow
	default:
		if readOnly {
			return ToolRiskLow
		}
		return ToolRiskHigh
	}
}
