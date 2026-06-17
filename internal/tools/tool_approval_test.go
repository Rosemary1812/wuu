package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
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
				Decision:  ToolApprovalDecisionApproved,
				Reason:    "looks fine",
				RiskLevel: GuardianRiskLow,
				Source:    ApprovalSourceGuardian,
			},
			wantSubs: []string{
				`"decision":"approved"`,
				`"reason":"looks fine"`,
				`"risk_level":"low"`,
				`"source":"guardian"`,
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

// TestDefaultAutoApprovalReviewer_TagsSource verifies that the legacy rule
// engine marks its verdicts with ApprovalSourceRule so audit logs can
// distinguish rule-driven decisions from LLM-driven ones.
func TestDefaultAutoApprovalReviewer_TagsSource(t *testing.T) {
	reviewer := DefaultAutoApprovalReviewer{}

	cases := []struct {
		name    string
		request ToolApprovalReviewRequest
		source  string
	}{
		{
			name:    "destructive denies with rule source",
			request: ToolApprovalReviewRequest{Destructive: true, Kind: ToolKindShell, Risk: ToolRiskHigh},
			source:  ApprovalSourceRule,
		},
		{
			name:    "read-only approves with rule source",
			request: ToolApprovalReviewRequest{ReadOnly: true, Kind: ToolKindFile, Risk: ToolRiskLow},
			source:  ApprovalSourceRule,
		},
		{
			name:    "non-destructive file write approves with rule source",
			request: ToolApprovalReviewRequest{Kind: ToolKindFile, Risk: ToolRiskMedium},
			source:  ApprovalSourceRule,
		},
		{
			name:    "high-risk shell denies with rule source",
			request: ToolApprovalReviewRequest{Kind: ToolKindShell, Risk: ToolRiskHigh},
			source:  ApprovalSourceRule,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := reviewer.ReviewToolApproval(context.Background(), tc.request)
			if err != nil {
				t.Fatalf("review: %v", err)
			}
			if got.Source != tc.source {
				t.Fatalf("source = %q, want %q", got.Source, tc.source)
			}
		})
	}
}

// TestNormalizeToolApprovalReview_PreservesNewFields verifies the existing
// normaliser does not strip the new fields when an unknown decision string
// is replaced with Denied.
func TestNormalizeToolApprovalReview_PreservesNewFields(t *testing.T) {
	got := normalizeToolApprovalReview(ToolApprovalReview{
		Decision:  ToolApprovalDecision("bogus"),
		RiskLevel: GuardianRiskCritical,
		Source:    ApprovalSourceGuardian,
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
}
