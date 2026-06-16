package appserver

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/tools"
)

const MethodToolApprovalRequest = "tool/approval/request"

type ToolApprovalRequest struct {
	ID                   string `json:"id"`
	ToolName             string `json:"tool_name"`
	CallID               string `json:"call_id,omitempty"`
	Kind                 string `json:"kind"`
	Risk                 string `json:"risk"`
	PolicyAction         string `json:"policy_action"`
	PolicyReason         string `json:"policy_reason,omitempty"`
	ClassificationReason string `json:"classification_reason,omitempty"`
	ReadOnly             bool   `json:"read_only"`
	Destructive          bool   `json:"destructive"`
	Revision             string `json:"revision,omitempty"`
	ArgumentsSHA256      string `json:"arguments_sha256,omitempty"`
	ArgumentsPreview     string `json:"arguments_preview,omitempty"`
	ApprovalRef          string `json:"approval_ref,omitempty"`
	ModelNextAction      string `json:"model_next_action,omitempty"`
}

type ToolApprovalResponse struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
}

func (s *Server) installToolApprovalReviewer(kit *tools.Toolkit) {
	if s == nil || kit == nil {
		return
	}
	if s.rt != nil && s.rt.Permissions.ApprovalsReviewer == config.ApprovalsReviewerAutoReview {
		kit.SetToolApprovalReviewer(tools.DefaultAutoApprovalReviewer{})
		return
	}
	kit.SetToolApprovalReviewer(tools.ToolApprovalReviewerFunc(func(ctx context.Context, request tools.ToolApprovalReviewRequest) (tools.ToolApprovalReview, error) {
		raw, err := s.requestClient(ctx, MethodToolApprovalRequest, ToolApprovalRequest{
			ID:                   request.ID,
			ToolName:             request.ToolName,
			CallID:               request.CallID,
			Kind:                 string(request.Kind),
			Risk:                 string(request.Risk),
			PolicyAction:         string(request.PolicyAction),
			PolicyReason:         request.PolicyReason,
			ClassificationReason: request.ClassificationReason,
			ReadOnly:             request.ReadOnly,
			Destructive:          request.Destructive,
			Revision:             request.Revision,
			ArgumentsSHA256:      request.ArgumentsSHA256,
			ArgumentsPreview:     request.ArgumentsPreview,
			ApprovalRef:          request.ApprovalRef,
			ModelNextAction:      request.ModelNextAction,
		})
		if err != nil {
			return tools.ToolApprovalReview{}, err
		}
		var response ToolApprovalResponse
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &response); err != nil {
				return tools.ToolApprovalReview{}, err
			}
		}
		return tools.ToolApprovalReview{
			Decision: tools.ToolApprovalDecision(strings.TrimSpace(response.Decision)),
			Reason:   strings.TrimSpace(response.Reason),
		}, nil
	}))
}
