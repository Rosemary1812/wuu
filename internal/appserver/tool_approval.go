package appserver

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/guardian"
	"github.com/blueberrycongee/wuu/internal/modelroles"
	"github.com/blueberrycongee/wuu/internal/providerfactory"
	"github.com/blueberrycongee/wuu/internal/reviewsession"
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

// installToolApprovalReviewer wires the toolkit's reviewer based on the
// runtime permission profile:
//
//   - ApprovalsReviewer == auto_review: try to construct an LLM-driven
//     guardian.Reviewer from the current provider config. On any
//     construction failure (bad config, missing API key, etc) fail closed:
//     approval requests are denied instead of falling back to local rules.
//   - everything else: keep the legacy IPC path that asks the user via
//     the desktop app-server.
func (s *Server) installToolApprovalReviewer(kit *tools.Toolkit) {
	if s == nil || kit == nil {
		return
	}
	if s.rt != nil && s.rt.Permissions.ApprovalsReviewer == config.ApprovalsReviewerAutoReview {
		if reviewer, ok := s.buildGuardianReviewer(kit); ok {
			kit.SetToolApprovalReviewer(reviewer)
			return
		}
		reason := "auto_review guardian unavailable; approval blocked"
		s.logGuardianFallback(reason)
		kit.SetToolApprovalReviewer(unavailableGuardianReviewer(reason))
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

// buildGuardianReviewer constructs a guardian.Reviewer wired to the
// runtime's configured provider. Returns (nil, false) whenever any step
// fails — bad runtime, missing config, unresolvable provider name, or a
// BuildClient error — so the caller can fail closed without needing to
// inspect each failure mode.
//
// BuildClient is the same synchronous factory used by the runtime when
// constructing the main agent's StreamRunner client (see
// internal/providerfactory/factory.go), so the guardian reuses the
// resolved auth, base URL, and provider-specific wiring the user
// already configured.
func (s *Server) buildGuardianReviewer(kit *tools.Toolkit) (*guardian.Reviewer, bool) {
	if s == nil || s.rt == nil || kit == nil {
		return nil, false
	}
	cfg, _, err := config.LoadFrom(s.rt.RootDir, os.Getenv("HOME"))
	if err != nil {
		return nil, false
	}
	selection := s.rt.ModelRoles.Review
	if strings.TrimSpace(selection.Provider) == "" || strings.TrimSpace(selection.Model) == "" {
		providerCfg, resolvedName, err := cfg.ResolveProvider(s.rt.ProviderName)
		if err != nil {
			return nil, false
		}
		roles, resolveErr := modelroles.Resolve(cfg, modelroles.ResolveOptions{
			ProviderName:   resolvedName,
			ProviderConfig: providerCfg,
			Model:          providerCfg.Model,
		})
		if resolveErr != nil {
			return nil, false
		}
		selection = roles.Review
	}
	client, err := providerfactory.BuildClient(selection.RuleProviderConfig, selection.Provider)
	if err != nil || client == nil {
		return nil, false
	}
	session, err := reviewsession.New(reviewsession.Config{
		Client:          client,
		Model:           firstNonEmpty(selection.APIModel, selection.Model),
		Role:            "guardian",
		Timeout:         guardian.DefaultReviewerTimeout,
		MaxTokens:       256,
		Effort:          selection.LegacyEffort,
		ProviderOptions: selection.ProviderOptions,
	})
	if err != nil {
		return nil, false
	}
	return &guardian.Reviewer{
		Session: session,
		Cache:   kit.ApprovalStore(),
		Breaker: s.guardianBreaker,
	}, true
}

func unavailableGuardianReviewer(reason string) tools.ToolApprovalReviewer {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "auto_review guardian unavailable; approval blocked"
	}
	return tools.ToolApprovalReviewerFunc(func(context.Context, tools.ToolApprovalReviewRequest) (tools.ToolApprovalReview, error) {
		return tools.ToolApprovalReview{
			Decision:  tools.ToolApprovalDecisionDenied,
			Reason:    reason,
			RiskLevel: tools.GuardianRiskHigh,
			Source:    tools.ApprovalSourceGuardian,
		}, nil
	})
}

// logGuardianFallback records the auto-review failure reason on the Server so it
// can be surfaced by the desktop layer without us polluting the
// JSON-RPC out stream with diagnostic log lines. Concurrent-safe.
func (s *Server) logGuardianFallback(msg string) {
	if s == nil || strings.TrimSpace(msg) == "" {
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.lastFallback = msg
}
