package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
)

// TestToolApprovalReview_JSONRoundtrip verifies that the new RiskLevel and
// Source fields survive a JSON encode/decode cycle and are omitted when
// empty, so the wire format stays backward-compatible with callers that
// only know about Decision/Reason.
func TestToolApprovalReview_JSONRoundtrip(t *testing.T) {
	cases := []struct {
		name     string
		review   ToolApprovalReview
		wantSubs []string // substrings that must appear in the marshalled payload
	}{
		{
			name: "approved with risk and source",
			review: ToolApprovalReview{
				Decision:                 ToolApprovalDecisionApproved,
				Reason:                   "looks fine",
				RiskLevel:                GuardianRiskLow,
				Source:                   ApprovalSourceGuardian,
				ReviewModel:              "review-model",
				ReviewRole:               "guardian",
				ReviewOutcome:            "completed",
				ReviewRequestFingerprint: strings.Repeat("a", 64),
				ReviewDurationMS:         123,
			},
			wantSubs: []string{
				`"decision":"approved"`,
				`"reason":"looks fine"`,
				`"risk_level":"low"`,
				`"source":"guardian"`,
				`"review_model":"review-model"`,
				`"review_role":"guardian"`,
				`"review_outcome":"completed"`,
				`"review_request_fingerprint":"` + strings.Repeat("a", 64) + `"`,
				`"review_duration_ms":123`,
			},
		},
		{
			name: "denied with risk only",
			review: ToolApprovalReview{
				Decision:  ToolApprovalDecisionDenied,
				RiskLevel: GuardianRiskHigh,
				Source:    ApprovalSourceAutoReview,
			},
			wantSubs: []string{
				`"decision":"denied"`,
				`"risk_level":"high"`,
				`"source":"auto_review"`,
			},
		},
		{
			name: "approved for session omits empty risk",
			review: ToolApprovalReview{
				Decision: ToolApprovalDecisionApprovedForSession,
				Reason:   "cached",
			},
			wantSubs: []string{
				`"decision":"approved_for_session"`,
				`"reason":"cached"`,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.review)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			for _, sub := range tc.wantSubs {
				if !strings.Contains(string(raw), sub) {
					t.Fatalf("payload missing %q: %s", sub, raw)
				}
			}
			// empty RiskLevel / Source must be omitted
			if tc.review.RiskLevel == "" && strings.Contains(string(raw), `"risk_level"`) {
				t.Fatalf("expected risk_level to be omitted, got %s", raw)
			}
			if tc.review.Source == "" && strings.Contains(string(raw), `"source"`) {
				t.Fatalf("expected source to be omitted, got %s", raw)
			}

			var got ToolApprovalReview
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got.Decision != tc.review.Decision {
				t.Fatalf("decision = %q, want %q", got.Decision, tc.review.Decision)
			}
			if got.RiskLevel != tc.review.RiskLevel {
				t.Fatalf("risk_level = %q, want %q", got.RiskLevel, tc.review.RiskLevel)
			}
			if got.Source != tc.review.Source {
				t.Fatalf("source = %q, want %q", got.Source, tc.review.Source)
			}
		})
	}
}

// TestNormalizeToolApprovalReview_PreservesNewFields verifies the existing
// normaliser does not strip the new fields when an unknown decision string
// is replaced with Denied.
func TestNormalizeToolApprovalReview_PreservesNewFields(t *testing.T) {
	got := normalizeToolApprovalReview(ToolApprovalReview{
		Decision:                 ToolApprovalDecision("bogus"),
		RiskLevel:                GuardianRiskCritical,
		Source:                   ApprovalSourceGuardian,
		ReviewModel:              "review-model",
		ReviewRole:               "guardian",
		ReviewOutcome:            "completed",
		ReviewRequestFingerprint: strings.Repeat("b", 64),
		ReviewDurationMS:         456,
	})
	if got.Decision != ToolApprovalDecisionDenied {
		t.Fatalf("decision = %q, want denied", got.Decision)
	}
	if got.RiskLevel != GuardianRiskCritical {
		t.Fatalf("risk_level = %q, want critical", got.RiskLevel)
	}
	if got.Source != ApprovalSourceGuardian {
		t.Fatalf("source = %q, want guardian", got.Source)
	}
	if got.ReviewModel != "review-model" || got.ReviewRole != "guardian" || got.ReviewOutcome != "completed" ||
		got.ReviewRequestFingerprint != strings.Repeat("b", 64) || got.ReviewDurationMS != 456 {
		t.Fatalf("review metadata not preserved: %+v", got)
	}
}

func TestToolkitCloneUsesFreshApprovalStore(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	call := providers.ToolCall{Name: "bash", Arguments: `{"command":"git push origin main"}`}
	key := toolApprovalKey(call)
	kit.ApprovalStore().ApproveForSession(key, ToolApprovalReview{Decision: ToolApprovalDecisionApprovedForSession})
	if _, ok := kit.ApprovalStore().IsApproved(key); !ok {
		t.Fatal("expected parent approval store to contain approved call")
	}

	clone, err := kit.CloneForRoot(root)
	if err != nil {
		t.Fatalf("CloneForRoot: %v", err)
	}
	if clone.ApprovalStore() == kit.ApprovalStore() {
		t.Fatal("clone must not share approval store with parent toolkit")
	}
	if _, ok := clone.ApprovalStore().IsApproved(key); ok {
		t.Fatal("clone approval store must not inherit parent session approvals")
	}
}
