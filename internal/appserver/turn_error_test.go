package appserver

import (
	"errors"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
)

// TestBuildTurnError_Nil covers the no-error path. BuildTurnError
// must not panic on nil and must still surface the provider so the
// front-end can show "Provider: openai" even when the body is empty.
func TestBuildTurnError_Nil(t *testing.T) {
	out := BuildTurnError(nil, "openai")
	if out.Message != "" {
		t.Errorf("expected empty message, got %q", out.Message)
	}
	if out.Provider != "openai" {
		t.Errorf("expected provider=openai, got %q", out.Provider)
	}
	if out.Action != nil {
		t.Errorf("expected nil action on nil error, got %+v", out.Action)
	}
}

// TestBuildTurnError_HTTP401_Auth covers the OpenAI "invalid API key"
// path: HTTP 401 with no parseable code in the body, classified as
// auth with a reauth action that points the user at the Provider
// settings page.
func TestBuildTurnError_HTTP401_Auth(t *testing.T) {
	err := &providers.HTTPError{
		StatusCode: 401,
		Body:       `{"error": {"message": "Incorrect API key provided."}}`,
	}
	out := BuildTurnError(err, "openai")
	if out.Category != string("auth") {
		t.Errorf("expected category=auth, got %q", out.Category)
	}
	if out.StatusCode != 401 {
		t.Errorf("expected status_code=401, got %d", out.StatusCode)
	}
	if out.Provider != "openai" {
		t.Errorf("expected provider=openai, got %q", out.Provider)
	}
	if out.Action == nil || out.Action.Reason != "reauth" {
		t.Errorf("expected reauth action, got %+v", out.Action)
	}
	if out.Action != nil && out.Action.Label == "" {
		t.Errorf("reauth action must have a non-empty label")
	}
}

// TestBuildTurnError_HTTP429_OpenAIQuota covers the OpenAI
// insufficient_quota path: HTTP 429 with code=insufficient_quota
// in the body, classified as provider with a "wait" action.
func TestBuildTurnError_HTTP429_OpenAIQuota(t *testing.T) {
	err := &providers.HTTPError{
		StatusCode: 429,
		Body:       `{"error": {"code": "insufficient_quota", "message": "You exceeded your current quota."}}`,
	}
	out := BuildTurnError(err, "openai")
	if out.Category != string("provider") {
		t.Errorf("expected category=provider, got %q", out.Category)
	}
	if out.Code != "insufficient_quota" {
		t.Errorf("expected code=insufficient_quota, got %q", out.Code)
	}
	if out.Action == nil || out.Action.Reason != "wait" {
		t.Errorf("expected wait action, got %+v", out.Action)
	}
}

// TestBuildTurnError_HTTPContextOverflow covers the typed context
// overflow path: the HTTPError has ContextOverflow=true so the
// category is provider and the action is "compact" regardless of
// the HTTP status code.
func TestBuildTurnError_HTTPContextOverflow(t *testing.T) {
	err := &providers.HTTPError{
		StatusCode:      400,
		ContextOverflow: true,
		Body:            `{"error": {"code": "context_length_exceeded", "message": "input too long"}}`,
	}
	out := BuildTurnError(err, "anthropic")
	if out.Category != string("provider") {
		t.Errorf("expected category=provider, got %q", out.Category)
	}
	if out.Code != "context_length_exceeded" {
		t.Errorf("expected code=context_length_exceeded, got %q", out.Code)
	}
	if out.Action == nil || out.Action.Reason != "compact" {
		t.Errorf("expected compact action, got %+v", out.Action)
	}
}

// TestBuildTurnError_StreamAnthropicRateLimit covers the Anthropic
// stream-error path: Code=rate_limit_error sets the category to
// provider and the code field carries the wire-level error name.
func TestBuildTurnError_StreamAnthropicRateLimit(t *testing.T) {
	err := &providers.StreamError{
		Code:    "rate_limit_error",
		Message: "Rate limit exceeded.",
	}
	out := BuildTurnError(err, "anthropic")
	if out.Category != string("provider") {
		t.Errorf("expected category=provider, got %q", out.Category)
	}
	if out.Code != "rate_limit_error" {
		t.Errorf("expected code=rate_limit_error, got %q", out.Code)
	}
}

// TestBuildTurnError_StreamAuth covers the typed auth path via
// StreamError.Auth = true.
func TestBuildTurnError_StreamAuth(t *testing.T) {
	err := &providers.StreamError{
		Code:    "authentication_error",
		Message: "Invalid API key.",
		Auth:    true,
	}
	out := BuildTurnError(err, "anthropic")
	if out.Category != string("auth") {
		t.Errorf("expected category=auth, got %q", out.Category)
	}
	if out.Code != "authentication_error" {
		t.Errorf("expected code=authentication_error, got %q", out.Code)
	}
}

// TestBuildTurnError_StreamContextOverflow covers typed
// StreamError.ContextOverflow — the chip should still surface the
// compact action.
func TestBuildTurnError_StreamContextOverflow(t *testing.T) {
	err := &providers.StreamError{
		Code:            "context_length_exceeded",
		Message:         "input too long",
		ContextOverflow: true,
	}
	out := BuildTurnError(err, "openai")
	if out.Category != string("provider") {
		t.Errorf("expected category=provider, got %q", out.Category)
	}
	if out.Action == nil || out.Action.Reason != "compact" {
		t.Errorf("expected compact action, got %+v", out.Action)
	}
}

// TestBuildTurnError_OverloadedAnthropic covers the Anthropic
// 529 "overloaded" path. The body is the stream-error wrapper from
// wuu's Go core ("stream request failed: stream error
// (overloaded_error)"), and the code extraction pulls "overloaded_error"
// out of the parens.
func TestBuildTurnError_OverloadedAnthropic(t *testing.T) {
	err := errors.New("stream request failed: stream error (overloaded_error)")
	out := BuildTurnError(err, "anthropic")
	if out.Code != "overloaded_error" {
		t.Errorf("expected code=overloaded_error, got %q", out.Code)
	}
	if out.Category != string("provider") {
		t.Errorf("expected category=provider, got %q", out.Category)
	}
	if out.Action == nil || out.Action.Reason != "wait" {
		t.Errorf("expected wait action for overload, got %+v", out.Action)
	}
}

// TestBuildTurnError_Cancellation covers the user-stop path. A plain
// "context canceled" error has no action because the user already
// knows they stopped the request.
func TestBuildTurnError_Cancellation(t *testing.T) {
	err := errors.New("context canceled")
	out := BuildTurnError(err, "openai")
	if out.Category != string("cancelled") {
		t.Errorf("expected category=cancelled, got %q", out.Category)
	}
	if out.Action != nil {
		t.Errorf("expected nil action for cancellation, got %+v", out.Action)
	}
}

// TestBuildTurnError_InternalFallback covers the unrecognized-error
// path. The message has no category-specific token so the classifier
// falls back to "internal" with a copy_debug action.
func TestBuildTurnError_InternalFallback(t *testing.T) {
	err := errors.New("panic: nil pointer dereference")
	out := BuildTurnError(err, "")
	if out.Category != string("internal") {
		t.Errorf("expected category=internal, got %q", out.Category)
	}
	if out.Action == nil || out.Action.Reason != "copy_debug" {
		t.Errorf("expected copy_debug action, got %+v", out.Action)
	}
}

// TestBuildTurnError_LocalPermissionDenied covers the local
// permissions path. The "permission denied: file" combination
// triggers isLocalOperationError.
func TestBuildTurnError_LocalPermissionDenied(t *testing.T) {
	err := errors.New("permission denied: file /etc/hosts")
	out := BuildTurnError(err, "")
	if out.Category != string("local") {
		t.Errorf("expected category=local, got %q", out.Category)
	}
	if out.Action == nil || out.Action.Reason != "open_settings" {
		t.Errorf("expected open_settings action, got %+v", out.Action)
	}
}

// TestBuildTurnError_HTTPNetwork5xx covers a 503 with no parseable
// body. The category is network and the action is "retry".
func TestBuildTurnError_HTTPNetwork5xx(t *testing.T) {
	err := &providers.HTTPError{StatusCode: 503, Body: "Service Unavailable"}
	out := BuildTurnError(err, "openai")
	if out.Category != string("network") {
		t.Errorf("expected category=network, got %q", out.Category)
	}
	if out.Action == nil || out.Action.Reason != "retry" {
		t.Errorf("expected retry action, got %+v", out.Action)
	}
}

// TestBuildTurnError_GeminiResourceExhausted covers a Gemini-style
// response where the code is in "error.code" (number) and the
// status is a string like "RESOURCE_EXHAUSTED". extractCodeFromBody
// pulls the string from error.type.
func TestBuildTurnError_GeminiResourceExhausted(t *testing.T) {
	err := &providers.HTTPError{
		StatusCode: 429,
		Body:       `{"error": {"code": 7, "message": "Resource has been exhausted", "status": "RESOURCE_EXHAUSTED"}}`,
	}
	out := BuildTurnError(err, "gemini")
	if out.Code != "RESOURCE_EXHAUSTED" {
		t.Errorf("expected code=RESOURCE_EXHAUSTED, got %q", out.Code)
	}
	if out.Action == nil || out.Action.Reason != "wait" {
		t.Errorf("expected wait action, got %+v", out.Action)
	}
}

// TestBuildTurnError_AnthropicRequestTooLarge covers the
// "request_too_large" Anthropic 413 body, where the code is in
// error.type.
func TestBuildTurnError_AnthropicRequestTooLarge(t *testing.T) {
	err := &providers.HTTPError{
		StatusCode: 413,
		Body:       `{"error": {"type": "request_too_large", "message": "Request exceeds the maximum size"}}`,
	}
	out := BuildTurnError(err, "anthropic")
	if out.Code != "request_too_large" {
		t.Errorf("expected code=request_too_large, got %q", out.Code)
	}
	// request_too_large is a context-overflow shape; the action
	// should suggest compact.
	if out.Action == nil || out.Action.Reason != "compact" {
		t.Errorf("expected compact action for request_too_large, got %+v", out.Action)
	}
}
