package guardian

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/tools"
)

// DefaultReviewerTimeout caps each guardian LLM round-trip. Chosen to be
// long enough for slow models on big prompts but short enough that a stuck
// turn does not appear frozen to the user. The Codex reference uses 90s;
// we trim it because wuu runs interactively and a silent 90s wait feels
// broken to the user.
const DefaultReviewerTimeout = 30 * time.Second

// Reviewer is an LLM-driven implementation of tools.ToolApprovalReviewer.
// When wired into the toolkit it answers "should this tool call run?" by
// asking the configured model, given the recent transcript.
//
// All failure modes fail closed: parse errors, network errors, timeouts,
// and missing configuration all produce a Denied review tagged with the
// underlying reason. The reviewer does NOT return errors to the toolkit:
// every outcome is a verdict (approved or denied), so the toolkit's normal
// Denied handling bubbles the reason to the model without losing the
// Source / RiskLevel audit metadata.
type Reviewer struct {
	// Client is the LLM client used to make the review call. Required.
	Client providers.Client
	// Model is the model identifier passed to the provider. Required.
	Model string
	// Timeout caps each review call. Defaults to DefaultReviewerTimeout
	// when zero or negative.
	Timeout time.Duration
	// Cache stores session-wide approval decisions so repeated identical
	// tool calls do not re-query the LLM. Optional; nil disables caching.
	Cache *tools.ToolApprovalStore
	// Breaker tracks recent denials and signals when the host turn should
	// be interrupted. Optional; nil disables per-turn breakering.
	Breaker *RejectionCircuitBreaker
	// TurnID returns the current turn id for breaker per-turn isolation.
	// Optional; if nil or returns an empty string, the breaker operates
	// without turn scope.
	TurnID func() string
}

// ReviewToolApproval implements tools.ToolApprovalReviewer. It is safe to
// call from multiple goroutines, although in practice the toolkit only
// invokes it sequentially per turn.
func (r *Reviewer) ReviewToolApproval(ctx context.Context, req tools.ToolApprovalReviewRequest) (tools.ToolApprovalReview, error) {
	if r == nil || r.Client == nil {
		return denyReview("guardian reviewer is not configured"), nil
	}

	// 1. Cache hit: reuse prior decision for the same approval key.
	if r.Cache != nil {
		if cached, ok := r.Cache.IsApproved(req.ApprovalKey); ok {
			return cached, nil
		}
	}

	// 2. Build the prompt from the request + transcript context.
	transcript, _ := TranscriptFromContext(ctx)
	prompt, err := BuildPrompt(req, transcript)
	if err != nil {
		review := denyReview("guardian prompt build failed: " + err.Error())
		r.notifyBreaker(review.Decision)
		return review, nil
	}

	// 3. Apply timeout.
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultReviewerTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 4. Call the LLM. A 256-token cap is plenty: the prompt asks for a
	//    single-line JSON object so the response should fit comfortably.
	resp, err := r.Client.Chat(callCtx, providers.ChatRequest{
		Model:     strings.TrimSpace(r.Model),
		Messages:  []providers.ChatMessage{{Role: "user", Content: prompt}},
		MaxTokens: 256,
	})
	if err != nil {
		review := denyReview("guardian LLM call failed: " + err.Error())
		r.notifyBreaker(review.Decision)
		return review, nil
	}

	// 5. Parse the response (fail closed on any error).
	parsed, err := parseGuardianDecision(resp.Content)
	if err != nil {
		review := denyReview("guardian response parse failed: " + err.Error())
		r.notifyBreaker(review.Decision)
		return review, nil
	}

	// 6. Build the review value.
	review := tools.ToolApprovalReview{
		Decision:  parsed.Decision,
		Reason:    parsed.Rationale,
		RiskLevel: parsed.RiskLevel,
		Source:    tools.ApprovalSourceGuardian,
	}

	// 7. Notify the breaker.
	r.notifyBreaker(review.Decision)

	// 8. Cache approvals only. Denials are deliberately NOT cached so a
	//    later approval request for a similar tool call can still succeed
	//    after the user changes intent or after the model produces a more
	//    permissive verdict.
	if r.Cache != nil && req.ApprovalKey != "" && review.Decision == tools.ToolApprovalDecisionApproved {
		r.Cache.ApproveForSession(req.ApprovalKey, review)
	}
	return review, nil
}

func (r *Reviewer) notifyBreaker(decision tools.ToolApprovalDecision) {
	if r.Breaker == nil {
		return
	}
	turnID := ""
	if r.TurnID != nil {
		turnID = strings.TrimSpace(r.TurnID())
	}
	switch decision {
	case tools.ToolApprovalDecisionDenied:
		r.Breaker.RecordDenial(turnID)
	default:
		r.Breaker.RecordApproval(turnID)
	}
}

func denyReview(reason string) tools.ToolApprovalReview {
	return tools.ToolApprovalReview{
		Decision:  tools.ToolApprovalDecisionDenied,
		Reason:    reason,
		RiskLevel: tools.GuardianRiskHigh,
		Source:    tools.ApprovalSourceGuardian,
	}
}

// parsedGuardian is the structured shape we extract from the LLM response.
type parsedGuardian struct {
	Decision  tools.ToolApprovalDecision
	RiskLevel tools.GuardianRiskLevel
	Rationale string
}

// rawGuardianResponse is the JSON wire shape the prompt asks for.
type rawGuardianResponse struct {
	Decision  string `json:"decision"`
	RiskLevel string `json:"risk_level"`
	Rationale string `json:"rationale"`
}

// jsonCodeBlockRE matches ```json ... ``` blocks. Non-greedy to avoid
// eating across multiple code blocks.
var jsonCodeBlockRE = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{[\\s\\S]*?\\})\\s*```")

// jsonObjectKeyRE is a last-resort fallback that matches a single-level
// { ... "decision" ... } object. It does not handle nested braces; that
// is acceptable for the LLM output the prompt asks for (a flat object).
var jsonObjectKeyRE = regexp.MustCompile(`\{[^{}]*"decision"[^{}]*\}`)

// parseGuardianDecision extracts the JSON decision from the LLM response.
// It tries (in order): direct JSON parse, JSON-in-fenced-code-block, and a
// fallback regex over the last "decision"-bearing object. Any failure is
// reported so the caller can fail closed.
func parseGuardianDecision(text string) (parsedGuardian, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return parsedGuardian{}, errors.New("empty response from guardian LLM")
	}

	// 1. Direct JSON parse.
	if raw, err := decodeRawGuardian(text); err == nil {
		return normalizeRaw(raw)
	}

	// 2. JSON inside a fenced ```json ... ``` block.
	if m := jsonCodeBlockRE.FindStringSubmatch(text); len(m) >= 2 {
		if raw, err := decodeRawGuardian(m[1]); err == nil {
			return normalizeRaw(raw)
		}
	}

	// 3. Last "{ ... decision ... }" object (LLM wrote prose around it).
	if m := jsonObjectKeyRE.FindString(text); m != "" {
		if raw, err := decodeRawGuardian(m); err == nil {
			return normalizeRaw(raw)
		}
	}

	return parsedGuardian{}, fmt.Errorf("could not parse guardian decision from response: %s", truncateString(text, 100))
}

func decodeRawGuardian(s string) (rawGuardianResponse, error) {
	var raw rawGuardianResponse
	err := json.Unmarshal([]byte(s), &raw)
	return raw, err
}

// normalizeRaw validates and normalizes the parsed JSON into a parsedGuardian.
// Unknown / empty risk levels fall back to medium; missing rationale gets a
// placeholder so the model always sees a reason to learn from.
func normalizeRaw(raw rawGuardianResponse) (parsedGuardian, error) {
	decision := tools.ToolApprovalDecision(strings.ToLower(strings.TrimSpace(raw.Decision)))
	switch decision {
	case tools.ToolApprovalDecisionApproved, tools.ToolApprovalDecisionDenied:
		// ok
	default:
		return parsedGuardian{}, fmt.Errorf("invalid decision %q (expected approved or denied)", raw.Decision)
	}
	risk := tools.GuardianRiskLevel(strings.ToLower(strings.TrimSpace(raw.RiskLevel)))
	switch risk {
	case tools.GuardianRiskLow, tools.GuardianRiskMedium, tools.GuardianRiskHigh, tools.GuardianRiskCritical:
		// ok
	case "":
		risk = tools.GuardianRiskMedium
	default:
		risk = tools.GuardianRiskMedium
	}
	rationale := strings.TrimSpace(raw.Rationale)
	if rationale == "" {
		rationale = "guardian returned no rationale"
	}
	return parsedGuardian{
		Decision:  decision,
		RiskLevel: risk,
		Rationale: rationale,
	}, nil
}
