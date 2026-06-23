package config

import (
	"fmt"
	"strings"
)

const (
	PermissionModeReadOnly   = "read_only"
	PermissionModeAgent      = "agent"
	PermissionModeAutoReview = "auto_review"
	PermissionModeFullAccess = "full_access"

	PermissionProfileReadOnly         = "read_only"
	PermissionProfileWorkspaceWrite   = "workspace_write"
	PermissionProfileDangerFullAccess = "danger_full_access"

	ApprovalPolicyOnRequest = "on_request"
	ApprovalPolicyNever     = "never"

	ApprovalsReviewerUser       = "user"
	ApprovalsReviewerAutoReview = "auto_review"
)

type ResolvedPermissions struct {
	Mode              string `json:"mode,omitempty"`
	PermissionProfile string `json:"permission_profile,omitempty"`
	ApprovalPolicy    string `json:"approval_policy,omitempty"`
	ApprovalsReviewer string `json:"approvals_reviewer,omitempty"`
}

func ResolveAgentPermissions(agent AgentConfig) ResolvedPermissions {
	mode := normalizePermissionMode(agent.PermissionMode)
	if mode == "" {
		mode = inferPermissionMode(agent)
	}
	resolved, ok := PermissionPresetForMode(mode)
	if !ok {
		resolved, _ = PermissionPresetForMode(PermissionModeAgent)
	}
	if profile := normalizePermissionProfile(agent.PermissionProfile); profile != "" {
		resolved.PermissionProfile = profile
	}
	if policy := normalizeApprovalPolicy(agent.ApprovalPolicy); policy != "" {
		resolved.ApprovalPolicy = policy
	}
	if reviewer := normalizeApprovalsReviewer(agent.ApprovalsReviewer); reviewer != "" {
		resolved.ApprovalsReviewer = reviewer
	}
	resolved.Mode = inferResolvedPermissionMode(resolved, mode)
	return resolved
}

func PermissionPresetForMode(mode string) (ResolvedPermissions, bool) {
	switch normalizePermissionMode(mode) {
	case PermissionModeReadOnly:
		return ResolvedPermissions{
			Mode:              PermissionModeReadOnly,
			PermissionProfile: PermissionProfileReadOnly,
			ApprovalPolicy:    ApprovalPolicyOnRequest,
			ApprovalsReviewer: ApprovalsReviewerUser,
		}, true
	case "", PermissionModeAgent:
		return ResolvedPermissions{
			Mode:              PermissionModeAgent,
			PermissionProfile: PermissionProfileWorkspaceWrite,
			ApprovalPolicy:    ApprovalPolicyOnRequest,
			ApprovalsReviewer: ApprovalsReviewerUser,
		}, true
	case PermissionModeAutoReview:
		return ResolvedPermissions{
			Mode:              PermissionModeAutoReview,
			PermissionProfile: PermissionProfileWorkspaceWrite,
			ApprovalPolicy:    ApprovalPolicyOnRequest,
			ApprovalsReviewer: ApprovalsReviewerAutoReview,
		}, true
	case PermissionModeFullAccess:
		return ResolvedPermissions{
			Mode:              PermissionModeFullAccess,
			PermissionProfile: PermissionProfileDangerFullAccess,
			ApprovalPolicy:    ApprovalPolicyNever,
			ApprovalsReviewer: ApprovalsReviewerUser,
		}, true
	default:
		return ResolvedPermissions{}, false
	}
}

func ResolvePermissionModePreset(mode string) (ResolvedPermissions, error) {
	permissions, ok := PermissionPresetForMode(mode)
	if !ok {
		return ResolvedPermissions{}, fmt.Errorf("invalid permission mode %q", strings.TrimSpace(mode))
	}
	return permissions, nil
}

func ApplyPermissionModePreset(agent *AgentConfig, mode string) (ResolvedPermissions, error) {
	if agent == nil {
		return ResolvedPermissions{}, fmt.Errorf("agent config is required")
	}
	permissions, err := ResolvePermissionModePreset(mode)
	if err != nil {
		return ResolvedPermissions{}, err
	}
	agent.PermissionMode = permissions.Mode
	agent.PermissionProfile = permissions.PermissionProfile
	agent.ApprovalPolicy = permissions.ApprovalPolicy
	agent.ApprovalsReviewer = permissions.ApprovalsReviewer
	agent.ToolPolicy = ToolPolicyConfig{}
	return permissions, nil
}

func ToolPolicyProfileForPermissionMode(mode string) string {
	switch normalizePermissionMode(mode) {
	case PermissionModeReadOnly:
		return PermissionModeReadOnly
	case PermissionModeFullAccess:
		return PermissionModeFullAccess
	case PermissionModeAutoReview:
		return PermissionModeAutoReview
	case "", PermissionModeAgent:
		return PermissionModeAgent
	default:
		return ""
	}
}

func normalizePermissionMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case PermissionModeReadOnly:
		return PermissionModeReadOnly
	case "":
		return ""
	case PermissionModeAgent:
		return PermissionModeAgent
	case PermissionModeAutoReview:
		return PermissionModeAutoReview
	case PermissionModeFullAccess:
		return PermissionModeFullAccess
	default:
		return strings.TrimSpace(mode)
	}
}

func normalizePermissionProfile(profile string) string {
	switch strings.TrimSpace(profile) {
	case "", PermissionProfileReadOnly, PermissionProfileWorkspaceWrite, PermissionProfileDangerFullAccess:
		return strings.TrimSpace(profile)
	default:
		return strings.TrimSpace(profile)
	}
}

func normalizeApprovalPolicy(policy string) string {
	switch strings.TrimSpace(policy) {
	case "", ApprovalPolicyOnRequest, ApprovalPolicyNever:
		return strings.TrimSpace(policy)
	default:
		return strings.TrimSpace(policy)
	}
}

func normalizeApprovalsReviewer(reviewer string) string {
	switch strings.TrimSpace(reviewer) {
	case "", ApprovalsReviewerUser, ApprovalsReviewerAutoReview:
		return strings.TrimSpace(reviewer)
	default:
		return strings.TrimSpace(reviewer)
	}
}

func inferPermissionMode(agent AgentConfig) string {
	resolved := ResolvedPermissions{
		PermissionProfile: normalizePermissionProfile(agent.PermissionProfile),
		ApprovalPolicy:    normalizeApprovalPolicy(agent.ApprovalPolicy),
		ApprovalsReviewer: normalizeApprovalsReviewer(agent.ApprovalsReviewer),
	}
	if mode := inferResolvedPermissionMode(resolved, ""); mode != "" {
		return mode
	}
	return PermissionModeAgent
}

func inferResolvedPermissionMode(resolved ResolvedPermissions, fallback string) string {
	switch {
	case resolved.PermissionProfile == PermissionProfileReadOnly &&
		resolved.ApprovalPolicy == ApprovalPolicyOnRequest &&
		resolved.ApprovalsReviewer == ApprovalsReviewerUser:
		return PermissionModeReadOnly
	case resolved.PermissionProfile == PermissionProfileWorkspaceWrite &&
		resolved.ApprovalPolicy == ApprovalPolicyOnRequest &&
		resolved.ApprovalsReviewer == ApprovalsReviewerAutoReview:
		return PermissionModeAutoReview
	case resolved.PermissionProfile == PermissionProfileWorkspaceWrite &&
		resolved.ApprovalPolicy == ApprovalPolicyOnRequest &&
		resolved.ApprovalsReviewer == ApprovalsReviewerUser:
		return PermissionModeAgent
	case resolved.PermissionProfile == PermissionProfileDangerFullAccess &&
		resolved.ApprovalPolicy == ApprovalPolicyNever:
		return PermissionModeFullAccess
	default:
		return normalizePermissionMode(fallback)
	}
}
