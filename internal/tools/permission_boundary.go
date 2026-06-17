package tools

import (
	"fmt"
	"strings"
)

const (
	PermissionProfileReadOnly         = "read_only"
	PermissionProfileWorkspaceWrite   = "workspace_write"
	PermissionProfileDangerFullAccess = "danger_full_access"
)

// PermissionBoundary is the runtime hard boundary below tool policy and
// approval. Tool policy answers "should this call be allowed or reviewed?";
// the boundary answers "is this runtime physically allowed to do this at all?".
type PermissionBoundary struct {
	Profile string
}

func PermissionBoundaryForProfile(profile string) PermissionBoundary {
	return PermissionBoundary{Profile: normalizePermissionBoundaryProfile(profile)}
}

func (b PermissionBoundary) IsZero() bool {
	return strings.TrimSpace(b.Profile) == ""
}

func (b PermissionBoundary) Check(info ToolInfo) error {
	profile := normalizePermissionBoundaryProfile(b.Profile)
	if profile == "" {
		return nil
	}
	switch profile {
	case PermissionProfileReadOnly:
		if readOnlyProfileBlocks(info) {
			return permissionBoundaryError{
				Profile:         profile,
				ToolName:        info.Name,
				Kind:            info.Kind,
				Reason:          "read_only profile blocks workspace, process, extension, schedule, agent, workflow, and memory mutation",
				ModelNextAction: "use observation or internal planning tools, or explain that the runtime is read-only",
			}
		}
	case PermissionProfileWorkspaceWrite:
		if info.Destructive {
			return permissionBoundaryError{
				Profile:         profile,
				ToolName:        info.Name,
				Kind:            info.Kind,
				Reason:          "workspace_write profile blocks destructive runtime actions",
				ModelNextAction: "use a non-destructive workspace edit or ask the user to switch to full access",
			}
		}
	case PermissionProfileDangerFullAccess:
		return nil
	default:
		return permissionBoundaryError{
			Profile:         profile,
			ToolName:        info.Name,
			Kind:            info.Kind,
			Reason:          "unknown permission profile",
			ModelNextAction: "stop and report the invalid permission profile",
		}
	}
	return nil
}

func readOnlyProfileBlocks(info ToolInfo) bool {
	if info.ReadOnly {
		return false
	}
	switch info.Kind {
	case ToolKindFile, ToolKindShell, ToolKindTest, ToolKindGit, ToolKindMemory, ToolKindWorkflow, ToolKindAgent, ToolKindProcess, ToolKindSchedule, ToolKindMCP:
		return true
	default:
		return false
	}
}

func normalizePermissionBoundaryProfile(profile string) string {
	switch strings.TrimSpace(profile) {
	case "", PermissionProfileReadOnly, PermissionProfileWorkspaceWrite, PermissionProfileDangerFullAccess:
		return strings.TrimSpace(profile)
	default:
		return strings.TrimSpace(profile)
	}
}

type permissionBoundaryError struct {
	Profile         string
	ToolName        string
	Kind            ToolKind
	Reason          string
	ModelNextAction string
}

func (e permissionBoundaryError) Error() string {
	return fmt.Sprintf(
		"tool %q blocked by permission boundary: error_kind=permission_boundary_denied profile=%s kind=%s reason=%q model_next_action=%q",
		e.ToolName,
		e.Profile,
		e.Kind,
		e.Reason,
		e.ModelNextAction,
	)
}
