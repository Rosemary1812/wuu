package config

import "strings"

const (
	PermissionModeReadOnly     = "read_only"
	PermissionModeDefault      = "default"
	PermissionModeApproveForMe = "approve_for_me"
	PermissionModeFullAccess   = "full_access"

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
		resolved, _ = PermissionPresetForMode(PermissionModeDefault)
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
	case "", PermissionModeDefault:
		return ResolvedPermissions{
			Mode:              PermissionModeDefault,
			PermissionProfile: PermissionProfileWorkspaceWrite,
			ApprovalPolicy:    ApprovalPolicyOnRequest,
			ApprovalsReviewer: ApprovalsReviewerUser,
		}, true
	case PermissionModeApproveForMe:
		return ResolvedPermissions{
			Mode:              PermissionModeApproveForMe,
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

func LegacyToolPolicyProfileForPermissionMode(mode string) string {
	switch normalizePermissionMode(mode) {
	case PermissionModeReadOnly:
		return "safe"
	case PermissionModeFullAccess:
		return "autonomous"
	case PermissionModeDefault, PermissionModeApproveForMe:
		return "auto"
	default:
		return ""
	}
}

func PermissionModeForLegacyToolPolicyProfile(profile string) string {
	switch strings.TrimSpace(profile) {
	case "safe", "enterprise_restricted":
		return PermissionModeReadOnly
	case "autonomous":
		return PermissionModeFullAccess
	case "balanced", "auto":
		return PermissionModeDefault
	default:
		return ""
	}
}

func normalizePermissionMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case PermissionModeReadOnly, "read-only", "readonly":
		return PermissionModeReadOnly
	case "":
		return ""
	case PermissionModeDefault, "auto":
		return PermissionModeDefault
	case PermissionModeApproveForMe, "approve-for-me", "auto_review":
		return PermissionModeApproveForMe
	case PermissionModeFullAccess, "full-access", "danger_full_access", "autonomous":
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
	if mode := PermissionModeForLegacyToolPolicyProfile(agent.ToolPolicy.Profile); mode != "" {
		return mode
	}
	return PermissionModeDefault
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
		return PermissionModeApproveForMe
	case resolved.PermissionProfile == PermissionProfileWorkspaceWrite &&
		resolved.ApprovalPolicy == ApprovalPolicyOnRequest &&
		resolved.ApprovalsReviewer == ApprovalsReviewerUser:
		return PermissionModeDefault
	case resolved.PermissionProfile == PermissionProfileDangerFullAccess &&
		resolved.ApprovalPolicy == ApprovalPolicyNever:
		return PermissionModeFullAccess
	default:
		return normalizePermissionMode(fallback)
	}
}
