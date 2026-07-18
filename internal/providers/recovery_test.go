package providers

import (
	"errors"
	"testing"
	"time"
)

func TestPlanRecoveryHTTPStatusSemantics(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		action RecoveryActionKind
	}{
		{name: "gateway timeout", err: &HTTPError{StatusCode: 504, Body: "gateway timeout"}, action: RecoveryReplaySame},
		{name: "temporary openai compatible 404", err: &HTTPError{ProviderFamily: "openai", StatusCode: 404, Body: `{"error":"route warming"}`}, action: RecoveryReplaySame},
		{name: "terminal openai model 404", err: &HTTPError{ProviderFamily: "openai", StatusCode: 404, Body: `{"code":"model_not_found"}`}, action: RecoveryStop},
		{name: "anthropic 404", err: &HTTPError{ProviderFamily: "anthropic", StatusCode: 404, Body: "not found"}, action: RecoveryStop},
		{name: "temporary rate limit", err: &HTTPError{StatusCode: 429, Body: "retry later", RetryAfter: 3 * time.Second}, action: RecoveryWaitThenReplay},
		{name: "terminal quota", err: &HTTPError{StatusCode: 429, Body: "insufficient_quota"}, action: RecoveryStop},
		{name: "terminal 403 quota", err: &HTTPError{StatusCode: 403, Body: "The usage limit has been reached", AuthRefreshable: true}, action: RecoveryStop},
		{name: "terminal 403 permission", err: &HTTPError{StatusCode: 403, Body: "forbidden"}, action: RecoveryStop},
		{name: "billing service 503", err: &HTTPError{StatusCode: 503, Body: "billing service temporarily unavailable"}, action: RecoveryReplaySame},
		{name: "context transform", err: &HTTPError{StatusCode: 413, Body: "context_length_exceeded", ContextOverflow: true}, action: RecoveryTransformPayload},
		{name: "body too large", err: &HTTPError{StatusCode: 413, Body: "nginx client_max_body_size"}, action: RecoveryStop},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			failure := NormalizeFailure(test.err)
			plan := PlanRecovery(failure)
			if plan.Action != test.action {
				t.Fatalf("failure/plan = %+v / %+v, want %s", failure, plan, test.action)
			}
		})
	}
}

func TestPlanRecoveryStopsLocalBackpressureAndUnsafeReplay(t *testing.T) {
	backpressure := PlanRecovery(NormalizeFailure(&LocalBackpressureError{Component: "responses websocket"}))
	if backpressure.Action != RecoveryStop {
		t.Fatalf("backpressure plan = %+v", backpressure)
	}
	unsafe := PlanRecovery(NormalizeFailure(&ReplayBlockedError{Cause: errors.New("EOF"), Reason: errors.New("tool started")}))
	if unsafe.Action != RecoveryBlockUnsafe {
		t.Fatalf("unsafe replay plan = %+v", unsafe)
	}
}
