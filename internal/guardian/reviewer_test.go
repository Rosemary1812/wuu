package guardian

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/tools"
)

// mockProvider is a hand-rolled stand-in for providers.Client. We avoid
// generating mocks here because the production reviewer only consumes the
// Chat method; a 30-line struct is clearer than a generated mock.
type mockProvider struct {
	response providers.ChatResponse
	err      error
	calls    int
	received providers.ChatRequest
}

func (m *mockProvider) Chat(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	m.calls++
	m.received = req
	return m.response, m.err
}

func newReq(approvalKey string) tools.ToolApprovalReviewRequest {
	return tools.ToolApprovalReviewRequest{
		ToolName:         "run_shell",
		Kind:             tools.ToolKindShell,
		Risk:             tools.ToolRiskHigh,
		ApprovalKey:      approvalKey,
		ArgumentsPreview: `{"command":"ls"}`,
	}
}

func TestReviewer_NilReceiverFailsClosed(t *testing.T) {
	//nolint:staticcheck // explicit nil-receiver test
	var r *Reviewer
	review, err := r.ReviewToolApproval(context.Background(), newReq("k"))
	if err != nil {
		t.Fatalf("expected nil err (fail closed via Denied review), got %v", err)
	}
	if review.Decision != tools.ToolApprovalDecisionDenied {
		t.Fatalf("decision = %q, want denied", review.Decision)
	}
	if review.Source != tools.ApprovalSourceGuardian {
		t.Fatalf("source = %q, want guardian", review.Source)
	}
}

func TestReviewer_NilClientFailsClosed(t *testing.T) {
	r := &Reviewer{}
	review, err := r.ReviewToolApproval(context.Background(), newReq("k"))
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if review.Decision != tools.ToolApprovalDecisionDenied {
		t.Fatalf("decision = %q, want denied", review.Decision)
	}
	if !strings.Contains(review.Reason, "not configured") {
		t.Fatalf("reason should mention not-configured: %q", review.Reason)
	}
}

func TestReviewer_CacheHitSkipsLLM(t *testing.T) {
	provider := &mockProvider{
		response: providers.ChatResponse{Content: `{"decision":"denied","risk_level":"high","rationale":"x"}`},
	}
	cache := tools.NewToolApprovalStore()
	cache.ApproveForSession("k", tools.ToolApprovalReview{
		Decision: tools.ToolApprovalDecisionApproved,
		Reason:   "cached ok",
		Source:   tools.ApprovalSourceGuardian,
	})
	r := &Reviewer{Client: provider, Model: "m", Cache: cache}
	review, err := r.ReviewToolApproval(context.Background(), newReq("k"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if review.Decision != tools.ToolApprovalDecisionApproved {
		t.Fatalf("expected cached approval, got %q", review.Decision)
	}
	if review.Reason != "cached ok" {
		t.Fatalf("expected cached reason, got %q", review.Reason)
	}
	if provider.calls != 0 {
		t.Fatalf("LLM should not have been called, got %d calls", provider.calls)
	}
}

func TestReviewer_DirectJSONApproved(t *testing.T) {
	provider := &mockProvider{
		response: providers.ChatResponse{
			Content: `{"decision":"approved","risk_level":"low","rationale":"looks safe"}`,
		},
	}
	r := &Reviewer{Client: provider, Model: "m"}
	review, err := r.ReviewToolApproval(context.Background(), newReq("k"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if review.Decision != tools.ToolApprovalDecisionApproved {
		t.Fatalf("decision = %q, want approved", review.Decision)
	}
	if review.RiskLevel != tools.GuardianRiskLow {
		t.Fatalf("risk_level = %q, want low", review.RiskLevel)
	}
	if review.Reason != "looks safe" {
		t.Fatalf("reason = %q", review.Reason)
	}
	if review.Source != tools.ApprovalSourceGuardian {
		t.Fatalf("source = %q, want guardian", review.Source)
	}
}

func TestReviewer_DirectJSONDenied(t *testing.T) {
	provider := &mockProvider{
		response: providers.ChatResponse{
			Content: `{"decision":"denied","risk_level":"high","rationale":"destructive command"}`,
		},
	}
	r := &Reviewer{Client: provider, Model: "m"}
	review, err := r.ReviewToolApproval(context.Background(), newReq("k"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if review.Decision != tools.ToolApprovalDecisionDenied {
		t.Fatalf("decision = %q, want denied", review.Decision)
	}
	if review.RiskLevel != tools.GuardianRiskHigh {
		t.Fatalf("risk_level = %q, want high", review.RiskLevel)
	}
}

func TestReviewer_CodeBlockJSON(t *testing.T) {
	provider := &mockProvider{
		response: providers.ChatResponse{
			Content: "Sure, here's my analysis:\n\n```json\n{\"decision\":\"approved\",\"risk_level\":\"medium\",\"rationale\":\"ok\"}\n```\n",
		},
	}
	r := &Reviewer{Client: provider, Model: "m"}
	review, err := r.ReviewToolApproval(context.Background(), newReq("k"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if review.Decision != tools.ToolApprovalDecisionApproved {
		t.Fatalf("decision = %q, want approved", review.Decision)
	}
	if review.RiskLevel != tools.GuardianRiskMedium {
		t.Fatalf("risk_level = %q, want medium", review.RiskLevel)
	}
}

func TestReviewer_ObjectRegexJSON(t *testing.T) {
	provider := &mockProvider{
		response: providers.ChatResponse{
			Content: `I think... {"decision":"approved","risk_level":"low","rationale":"fine"} ... hope that helps`,
		},
	}
	r := &Reviewer{Client: provider, Model: "m"}
	review, err := r.ReviewToolApproval(context.Background(), newReq("k"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if review.Decision != tools.ToolApprovalDecisionApproved {
		t.Fatalf("decision = %q, want approved", review.Decision)
	}
}

func TestReviewer_InvalidDecisionFailsClosed(t *testing.T) {
	provider := &mockProvider{
		response: providers.ChatResponse{
			Content: `{"decision":"maybe","risk_level":"low","rationale":"unsure"}`,
		},
	}
	r := &Reviewer{Client: provider, Model: "m"}
	review, err := r.ReviewToolApproval(context.Background(), newReq("k"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if review.Decision != tools.ToolApprovalDecisionDenied {
		t.Fatalf("decision = %q, want denied (fail closed)", review.Decision)
	}
	if !strings.Contains(review.Reason, "invalid decision") {
		t.Fatalf("reason should mention invalid decision: %q", review.Reason)
	}
}

func TestReviewer_EmptyResponseFailsClosed(t *testing.T) {
	provider := &mockProvider{response: providers.ChatResponse{Content: ""}}
	r := &Reviewer{Client: provider, Model: "m"}
	review, err := r.ReviewToolApproval(context.Background(), newReq("k"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if review.Decision != tools.ToolApprovalDecisionDenied {
		t.Fatalf("decision = %q, want denied", review.Decision)
	}
	if !strings.Contains(review.Reason, "empty response") {
		t.Fatalf("reason should mention empty response: %q", review.Reason)
	}
}

func TestReviewer_UnknownRiskDefaultsToMedium(t *testing.T) {
	provider := &mockProvider{
		response: providers.ChatResponse{
			Content: `{"decision":"approved","risk_level":"unknown","rationale":"x"}`,
		},
	}
	r := &Reviewer{Client: provider, Model: "m"}
	review, err := r.ReviewToolApproval(context.Background(), newReq("k"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if review.RiskLevel != tools.GuardianRiskMedium {
		t.Fatalf("risk_level = %q, want medium", review.RiskLevel)
	}
}

func TestReviewer_MissingRationaleDefaulted(t *testing.T) {
	provider := &mockProvider{
		response: providers.ChatResponse{
			Content: `{"decision":"approved","risk_level":"low"}`,
		},
	}
	r := &Reviewer{Client: provider, Model: "m"}
	review, err := r.ReviewToolApproval(context.Background(), newReq("k"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(review.Reason, "no rationale") {
		t.Fatalf("expected placeholder rationale, got %q", review.Reason)
	}
}

func TestReviewer_ProviderErrorFailsClosed(t *testing.T) {
	provider := &mockProvider{err: errors.New("network down")}
	r := &Reviewer{Client: provider, Model: "m"}
	review, err := r.ReviewToolApproval(context.Background(), newReq("k"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if review.Decision != tools.ToolApprovalDecisionDenied {
		t.Fatalf("decision = %q, want denied (fail closed)", review.Decision)
	}
	if !strings.Contains(review.Reason, "network down") {
		t.Fatalf("reason should surface provider error: %q", review.Reason)
	}
}

func TestReviewer_ApprovalCachedDenialNotCached(t *testing.T) {
	approvedProvider := &mockProvider{response: providers.ChatResponse{
		Content: `{"decision":"approved","risk_level":"low","rationale":"ok"}`,
	}}
	deniedProvider := &mockProvider{response: providers.ChatResponse{
		Content: `{"decision":"denied","risk_level":"high","rationale":"no"}`,
	}}
	cache := tools.NewToolApprovalStore()

	r1 := &Reviewer{Client: approvedProvider, Model: "m", Cache: cache}
	if _, err := r1.ReviewToolApproval(context.Background(), newReq("approved-key")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := cache.IsApproved("approved-key"); !ok {
		t.Fatal("expected approval to be cached")
	}

	r2 := &Reviewer{Client: deniedProvider, Model: "m", Cache: cache}
	if _, err := r2.ReviewToolApproval(context.Background(), newReq("denied-key")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := cache.IsApproved("denied-key"); ok {
		t.Fatal("denial should NOT be cached")
	}
}

func TestReviewer_BreakerNotifiedOnDeny(t *testing.T) {
	provider := &mockProvider{response: providers.ChatResponse{
		Content: `{"decision":"denied","risk_level":"high","rationale":"no"}`,
	}}
	breaker := NewRejectionCircuitBreaker()
	r := &Reviewer{Client: provider, Model: "m", Breaker: breaker, TurnID: func() string { return "turn-x" }}
	if _, err := r.ReviewToolApproval(context.Background(), newReq("k")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// One denial already recorded; two more should trip on consecutive.
	breaker.RecordDenial("turn-x")
	if got := breaker.RecordDenial("turn-x"); got != BreakerInterruptTurn {
		t.Fatalf("expected breaker to trip after 3 total denials, got %v", got)
	}
}

func TestReviewer_BreakerNotifiedOnApprove(t *testing.T) {
	provider := &mockProvider{response: providers.ChatResponse{
		Content: `{"decision":"approved","risk_level":"low","rationale":"ok"}`,
	}}
	breaker := NewRejectionCircuitBreaker()
	r := &Reviewer{Client: provider, Model: "m", Breaker: breaker, TurnID: func() string { return "turn-x" }}
	if _, err := r.ReviewToolApproval(context.Background(), newReq("k")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The approval should have reset the consecutive counter, so 3 fresh
	// denials should be required to trip.
	breaker.RecordDenial("turn-x")
	breaker.RecordDenial("turn-x")
	if got := breaker.RecordDenial("turn-x"); got != BreakerInterruptTurn {
		t.Fatalf("expected breaker to trip after 3 fresh denials, got %v", got)
	}
}

func TestReviewer_NilBreakerDoesNotPanic(t *testing.T) {
	provider := &mockProvider{response: providers.ChatResponse{
		Content: `{"decision":"approved","risk_level":"low","rationale":"ok"}`,
	}}
	r := &Reviewer{Client: provider, Model: "m"} // no breaker
	if _, err := r.ReviewToolApproval(context.Background(), newReq("k")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReviewer_TranscriptThreadedIntoPrompt(t *testing.T) {
	provider := &mockProvider{response: providers.ChatResponse{
		Content: `{"decision":"approved","risk_level":"low","rationale":"ok"}`,
	}}
	r := &Reviewer{Client: provider, Model: "m"}
	ctx := WithTranscript(context.Background(), Transcript{Entries: []TranscriptEntry{
		{Role: TranscriptRoleUser, Content: "please run tests"},
	}})
	if _, err := r.ReviewToolApproval(ctx, newReq("k")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(provider.received.Messages[0].Content, "please run tests") {
		t.Fatalf("transcript not threaded into prompt:\n%s", provider.received.Messages[0].Content)
	}
}

func TestReviewer_TimeoutDefaultsApplied(t *testing.T) {
	provider := &mockProvider{response: providers.ChatResponse{
		Content: `{"decision":"approved","risk_level":"low","rationale":"ok"}`,
	}}
	r := &Reviewer{Client: provider, Model: "m"} // Timeout zero → default
	if _, err := r.ReviewToolApproval(context.Background(), newReq("k")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Timeout != 0 {
		t.Fatal("test precondition: Timeout must be zero")
	}
	if DefaultReviewerTimeout <= 0 {
		t.Fatal("DefaultReviewerTimeout must be positive")
	}
}

func TestReviewer_ProviderErrorStillNotifiesBreaker(t *testing.T) {
	provider := &mockProvider{err: errors.New("boom")}
	breaker := NewRejectionCircuitBreaker()
	r := &Reviewer{Client: provider, Model: "m", Breaker: breaker, TurnID: func() string { return "turn-y" }}
	if _, err := r.ReviewToolApproval(context.Background(), newReq("k")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 1 prior denial via the error path; two more should trip.
	breaker.RecordDenial("turn-y")
	if got := breaker.RecordDenial("turn-y"); got != BreakerInterruptTurn {
		t.Fatalf("expected breaker to trip after provider-error denial, got %v", got)
	}
}

func TestReviewer_MissingModelStillCalls(t *testing.T) {
	// Provider implementations usually fall back to a default model when
	// Model is empty. We just verify the reviewer does not panic or
	// short-circuit when Model is blank.
	provider := &mockProvider{response: providers.ChatResponse{
		Content: `{"decision":"approved","risk_level":"low","rationale":"ok"}`,
	}}
	r := &Reviewer{Client: provider} // Model empty
	if _, err := r.ReviewToolApproval(context.Background(), newReq("k")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("expected 1 LLM call, got %d", provider.calls)
	}
}
