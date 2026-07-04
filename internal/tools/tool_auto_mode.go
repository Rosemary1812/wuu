package tools

import (
	"context"
	"strings"
)

type AutoModeDecision string

const (
	AutoModeDecisionAllow AutoModeDecision = "allow"
	AutoModeDecisionDeny  AutoModeDecision = "deny"
)

type AutoModeClassifyRequest struct {
	ToolName      string
	CallID        string
	ArgumentsJSON string
	WorkspaceRoot string
	Info          ToolInfo
}

type AutoModeClassifyResult struct {
	Decision AutoModeDecision
	Reason   string
}

type AutoModeClassifier interface {
	Classify(ctx context.Context, request AutoModeClassifyRequest) (AutoModeClassifyResult, error)
}

type DefaultAutoModeClassifier struct{}

func (DefaultAutoModeClassifier) Classify(_ context.Context, request AutoModeClassifyRequest) (AutoModeClassifyResult, error) {
	info := request.Info
	if strings.TrimSpace(info.Name) == "" {
		return autoModeDeny("missing tool metadata"), nil
	}
	if info.ReadOnly && !info.Destructive {
		return autoModeAllow("read-only tool call"), nil
	}
	if info.Destructive {
		return autoModeDeny(autoModeReason(info, "destructive tool call")), nil
	}

	switch info.Kind {
	case ToolKindFile:
		switch info.Name {
		case "write_file", "edit_file", "apply_patch":
			return autoModeAllow("workspace file edit"), nil
		}
	case ToolKindTest:
		if info.Risk == ToolRiskMedium {
			return autoModeAllow("local verification command"), nil
		}
	case ToolKindShell:
		if info.Risk == ToolRiskMedium && info.Reason == "local verification command" {
			return autoModeAllow("local verification command"), nil
		}
	case ToolKindGit:
		if info.ReadOnly {
			return autoModeAllow("restricted read-only git command"), nil
		}
	case ToolKindWeb:
		if info.ReadOnly {
			return autoModeAllow("read-only web lookup"), nil
		}
	case ToolKindWorkflow, ToolKindProcess, ToolKindSchedule, ToolKindAgent, ToolKindMCP:
		if info.ReadOnly {
			return autoModeAllow("read-only runtime operation"), nil
		}
	}

	return autoModeDeny(autoModeReason(info, "tool call needs user approval in auto mode")), nil
}

func normalizeAutoModeResult(result AutoModeClassifyResult) AutoModeClassifyResult {
	result.Reason = strings.TrimSpace(result.Reason)
	switch result.Decision {
	case AutoModeDecisionAllow:
		if result.Reason == "" {
			result.Reason = "auto classifier allowed the tool call"
		}
		return result
	case AutoModeDecisionDeny:
		if result.Reason == "" {
			result.Reason = "auto classifier denied the tool call"
		}
		return result
	default:
		return autoModeDeny("auto classifier returned an invalid decision")
	}
}

func autoModeAllow(reason string) AutoModeClassifyResult {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "auto classifier allowed the tool call"
	}
	return AutoModeClassifyResult{Decision: AutoModeDecisionAllow, Reason: reason}
}

func autoModeDeny(reason string) AutoModeClassifyResult {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "auto classifier denied the tool call"
	}
	return AutoModeClassifyResult{Decision: AutoModeDecisionDeny, Reason: reason}
}

func autoModeReason(info ToolInfo, fallback string) string {
	if reason := strings.TrimSpace(info.Reason); reason != "" {
		return reason
	}
	return fallback
}
