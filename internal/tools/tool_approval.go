package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
)

type ToolApprovalDecision string

const (
	ToolApprovalDecisionApproved           ToolApprovalDecision = "approved"
	ToolApprovalDecisionApprovedForSession ToolApprovalDecision = "approved_for_session"
	ToolApprovalDecisionDenied             ToolApprovalDecision = "denied"
)

// GuardianRiskLevel lets an automatic reviewer communicate how dangerous it
// considered the action it just decided on. It mirrors Codex's
// GuardianRiskLevel taxonomy (low/medium/high/critical) so audit logs and the
// UI can surface this signal consistently.
type GuardianRiskLevel string

const (
	GuardianRiskLow      GuardianRiskLevel = "low"
	GuardianRiskMedium   GuardianRiskLevel = "medium"
	GuardianRiskHigh     GuardianRiskLevel = "high"
	GuardianRiskCritical GuardianRiskLevel = "critical"
)

// ApprovalSource tags who produced a given approval verdict so audit logs and
// downstream UI can distinguish user-driven decisions from automatic ones.
const (
	ApprovalSourceUser       = "user"
	ApprovalSourceAutoReview = "auto_review"
	ApprovalSourceGuardian   = "guardian"
)

type ToolApprovalReviewRequest struct {
	ID                   string           `json:"id"`
	ToolName             string           `json:"tool_name"`
	CallID               string           `json:"call_id,omitempty"`
	Kind                 ToolKind         `json:"kind"`
	Risk                 ToolRisk         `json:"risk"`
	PolicyAction         ToolPolicyAction `json:"policy_action"`
	PolicyReason         string           `json:"policy_reason,omitempty"`
	ClassificationReason string           `json:"classification_reason,omitempty"`
	ReadOnly             bool             `json:"read_only"`
	Destructive          bool             `json:"destructive"`
	CreatedAt            time.Time        `json:"created_at"`
	Revision             string           `json:"revision,omitempty"`
	ArgumentsSHA256      string           `json:"arguments_sha256,omitempty"`
	ArgumentsPreview     string           `json:"arguments_preview,omitempty"`
	ApprovalRef          string           `json:"approval_ref,omitempty"`
	ApprovalKey          string           `json:"approval_key,omitempty"`
	ModelNextAction      string           `json:"model_next_action"`
}

type ToolApprovalReview struct {
	Decision                 ToolApprovalDecision `json:"decision"`
	Reason                   string               `json:"reason,omitempty"`
	RiskLevel                GuardianRiskLevel    `json:"risk_level,omitempty"`
	Source                   string               `json:"source,omitempty"`
	ReviewModel              string               `json:"review_model,omitempty"`
	ReviewRole               string               `json:"review_role,omitempty"`
	ReviewOutcome            string               `json:"review_outcome,omitempty"`
	ReviewRequestFingerprint string               `json:"review_request_fingerprint,omitempty"`
	ReviewDurationMS         int64                `json:"review_duration_ms,omitempty"`
}

type ToolApprovalReviewer interface {
	ReviewToolApproval(ctx context.Context, request ToolApprovalReviewRequest) (ToolApprovalReview, error)
}

type ToolApprovalReviewerFunc func(ctx context.Context, request ToolApprovalReviewRequest) (ToolApprovalReview, error)

func (f ToolApprovalReviewerFunc) ReviewToolApproval(ctx context.Context, request ToolApprovalReviewRequest) (ToolApprovalReview, error) {
	return f(ctx, request)
}

type ToolApprovalStore struct {
	mu       sync.RWMutex
	approved map[string]ToolApprovalReview
}

func NewToolApprovalStore() *ToolApprovalStore {
	return &ToolApprovalStore{approved: make(map[string]ToolApprovalReview)}
}

func (s *ToolApprovalStore) IsApproved(key string) (ToolApprovalReview, bool) {
	key = strings.TrimSpace(key)
	if s == nil || key == "" {
		return ToolApprovalReview{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	review, ok := s.approved[key]
	return review, ok
}

func (s *ToolApprovalStore) ApproveForSession(key string, review ToolApprovalReview) {
	key = strings.TrimSpace(key)
	if s == nil || key == "" {
		return
	}
	if review.Decision == "" {
		review.Decision = ToolApprovalDecisionApprovedForSession
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.approved == nil {
		s.approved = make(map[string]ToolApprovalReview)
	}
	s.approved[key] = review
}

func normalizeToolApprovalReview(review ToolApprovalReview) ToolApprovalReview {
	review.Reason = strings.TrimSpace(review.Reason)
	switch review.Decision {
	case ToolApprovalDecisionApproved, ToolApprovalDecisionApprovedForSession, ToolApprovalDecisionDenied:
		return review
	default:
		// Preserve audit metadata even when the
		// reviewer returned an unrecognised decision string: downstream
		// logs and the UI still need to know who made the call and at
		// what risk level so the model can be steered accordingly.
		review.Decision = ToolApprovalDecisionDenied
		review.Reason = "approval reviewer returned an invalid decision"
		return ToolApprovalReview{
			Decision:                 review.Decision,
			Reason:                   review.Reason,
			RiskLevel:                review.RiskLevel,
			Source:                   review.Source,
			ReviewModel:              review.ReviewModel,
			ReviewRole:               review.ReviewRole,
			ReviewOutcome:            review.ReviewOutcome,
			ReviewRequestFingerprint: review.ReviewRequestFingerprint,
			ReviewDurationMS:         review.ReviewDurationMS,
		}
	}
}

var errToolApprovalReviewerUnavailable = errors.New("tool approval reviewer is unavailable")

func (t *Toolkit) requestToolApproval(
	ctx context.Context,
	call providers.ToolCall,
	info ToolInfo,
	decision ToolPolicyDecision,
	createdAt time.Time,
	revision string,
	approvalRef string,
) (ToolApprovalReview, error) {
	key := toolApprovalKey(call)
	if review, ok := t.approvalStore.IsApproved(key); ok {
		if review.Decision == "" {
			review.Decision = ToolApprovalDecisionApprovedForSession
		}
		return review, nil
	}
	if t == nil || t.approvalReviewer == nil {
		return ToolApprovalReview{}, errToolApprovalReviewerUnavailable
	}
	request := ToolApprovalReviewRequest{
		ID:                   approvalRequestID(call, createdAt),
		ToolName:             call.Name,
		CallID:               call.ID,
		Kind:                 info.Kind,
		Risk:                 info.Risk,
		PolicyAction:         decision.Action,
		PolicyReason:         decision.Reason,
		ClassificationReason: info.Reason,
		ReadOnly:             info.ReadOnly,
		Destructive:          info.Destructive,
		CreatedAt:            createdAt.UTC(),
		Revision:             revision,
		ArgumentsSHA256:      toolArgumentsSHA256(call.Arguments),
		ArgumentsPreview:     approvalArgumentsPreview(call.Arguments),
		ApprovalRef:          approvalRef,
		ApprovalKey:          key,
		ModelNextAction:      "ask the user for approval or choose a lower-risk alternative",
	}
	review, err := t.approvalReviewer.ReviewToolApproval(ctx, request)
	if err != nil {
		return ToolApprovalReview{
			Decision: ToolApprovalDecisionDenied,
			Reason:   strings.TrimSpace(err.Error()),
		}, toolApprovalReviewError{ToolName: call.Name, Cause: err}
	}
	review = normalizeToolApprovalReview(review)
	switch review.Decision {
	case ToolApprovalDecisionApproved:
		return review, nil
	case ToolApprovalDecisionApprovedForSession:
		t.approvalStore.ApproveForSession(key, review)
		return review, nil
	case ToolApprovalDecisionDenied:
		return review, toolApprovalDeniedError{
			ToolName: call.Name,
			Reason:   review.Reason,
		}
	default:
		return review, toolApprovalDeniedError{
			ToolName: call.Name,
			Reason:   review.Reason,
		}
	}
}

type toolApprovalDeniedError struct {
	ToolName string
	Reason   string
}

func (e toolApprovalDeniedError) Error() string {
	reason := strings.TrimSpace(e.Reason)
	if reason == "" {
		reason = "approval denied"
	}
	return fmt.Sprintf(
		"tool %q blocked by approval decision: error_kind=approval_denied policy_action=%s policy_reason=%q model_next_action=%q approval_options=[choose_lower_risk_alternative, stop]",
		e.ToolName,
		ToolPolicyRequireApproval,
		reason,
		"choose a lower-risk alternative or stop",
	)
}

type toolApprovalReviewError struct {
	ToolName string
	Cause    error
}

func (e toolApprovalReviewError) Error() string {
	reason := ""
	if e.Cause != nil {
		reason = strings.TrimSpace(e.Cause.Error())
	}
	if reason == "" {
		reason = "approval reviewer failed"
	}
	return fmt.Sprintf(
		"tool %q blocked by approval reviewer: error_kind=approval_reviewer_error policy_action=%s policy_reason=%q model_next_action=%q approval_options=[ask_user, choose_lower_risk_alternative, stop]",
		e.ToolName,
		ToolPolicyRequireApproval,
		reason,
		"ask the user for approval or choose a lower-risk alternative",
	)
}

func (e toolApprovalReviewError) Unwrap() error {
	return e.Cause
}

func toolApprovalKey(call providers.ToolCall) string {
	name := strings.TrimSpace(call.Name)
	hash := toolArgumentsSHA256(call.Arguments)
	if name == "" {
		return hash
	}
	if hash == "" {
		return name
	}
	return name + ":" + hash
}

func approvalArgumentsPreview(arguments string) string {
	argsPreview := redactToolOutput(strings.TrimSpace(arguments))
	if len(argsPreview) > 1200 {
		argsPreview = argsPreview[:1200] + "\n...[truncated]"
	}
	return argsPreview
}
