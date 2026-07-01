package appserver

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/providers"
)

const responseCompletedMissingCode = "stream_closed_before_response.completed"

// BuildTurnError inspects the raw error from a failed turn and produces a
// structured TurnError the front-end can render directly. Mirrors
// opencode's retryable() pattern: typed-error matching first, JSON body
// parse as a second pass, message-substring matching as the final
// fallback so we never lose information for an unrecognized provider.
//
// The function is best-effort: every field is optional and the caller
// (turn_handlers.go) still ships the raw `Message` so the front-end
// always has a fallback to show.
func BuildTurnError(err error, provider string) TurnError {
	if err == nil {
		return TurnError{Message: "", Provider: provider}
	}

	message := err.Error()
	lowerMessage := strings.ToLower(message)
	out := TurnError{
		Message:  message,
		Provider: provider,
	}

	// 1. Typed-error extraction. HTTPError and StreamError are the
	// canonical types produced by the providers package; pulling fields
	// from them avoids any string matching.
	if httpErr := asHTTPError(err); httpErr != nil {
		out.StatusCode = httpErr.StatusCode
		if code := extractCodeFromBody(httpErr.Body); code != "" {
			out.Code = code
		}
		if out.Category == "" {
			out.Category = categoryFromHTTPError(httpErr)
		}
	}
	if streamErr := asStreamError(err); streamErr != nil {
		if streamErr.Code != "" && out.Code == "" {
			out.Code = streamErr.Code
		}
		if out.Category == "" {
			out.Category = categoryFromStreamError(streamErr)
		}
	}

	if isResponseCompletedMissingMessage(lowerMessage) {
		if out.Code == "" {
			out.Code = responseCompletedMissingCode
		}
		if out.Category == "" {
			out.Category = "provider"
		}
	}

	// 2. If the typed checks did not pick a category, run the agent
	// classifier (which also walks typed errors and message substrings
	// in the agentcontrol package). This is the belt-and-suspenders
	// pass for wrapped / re-formatted errors.
	if out.Category == "" {
		out.Category = categoryFromClassifier(agentcontrol.ClassifyError(err))
	}

	// 3. Last-resort message-substring scan. We only get here for
	// untyped errors that the agent classifier also missed.
	if out.Category == "" {
		out.Category = categoryFromMessage(message)
	}

	// 4. If we still do not have a code, look for the parens-wrapped
	// OpenAI/Anthropic-style "stream error (CODE)" pattern that
	// wuu's Go core emits when wrapping provider stream failures.
	if out.Code == "" {
		out.Code = extractCodeFromMessage(message)
	}

	// 5. Action: the front-end renders this as a button beneath the
	// chip. Empty when the category does not have a meaningful next
	// step (cancelled) or when the message does not call for one.
	out.Action = buildAction(out.Category, out.Code, message)

	return out
}

// asHTTPError unwraps to *providers.HTTPError. Returns nil if not
// matchable. errors.As walks the wrap chain.
func asHTTPError(err error) *providers.HTTPError {
	var httpErr *providers.HTTPError
	if errors.As(err, &httpErr) {
		return httpErr
	}
	return nil
}

// asStreamError unwraps to *providers.StreamError.
func asStreamError(err error) *providers.StreamError {
	var streamErr *providers.StreamError
	if errors.As(err, &streamErr) {
		return streamErr
	}
	return nil
}

func categoryFromHTTPError(httpErr *providers.HTTPError) string {
	if httpErr.ContextOverflow {
		return "provider"
	}
	// 413 Payload Too Large is almost always a context-overflow
	// signal for chat-completions endpoints. Treat it as
	// provider-side so the chip surfaces the code and the
	// recommended action (compact or wait) matches the situation.
	if httpErr.StatusCode == 413 {
		return "provider"
	}
	switch httpErr.StatusCode {
	case 401, 403:
		return "auth"
	case 429, 529:
		// 429 is rate limit, 529 is Anthropic "overloaded". Both fall
		// under the provider category; the front-end distinguishes
		// them via the code/status fields.
		return "provider"
	case 408, 500, 502, 503, 504:
		return "network"
	}
	return ""
}

func categoryFromStreamError(streamErr *providers.StreamError) string {
	if streamErr.ContextOverflow {
		return "provider"
	}
	if streamErr.Auth {
		return "auth"
	}
	// Recognize rate-limit and overload error codes from the
	// stream-error payload directly. The StreamError.Retryable flag
	// is set separately by the providers layer; some flows (notably
	// Anthropic and OpenAI) emit rate_limit_error / overloaded_error
	// before the retry classification runs, so detect these from the
	// code without depending on Retryable.
	code := strings.ToLower(strings.TrimSpace(streamErr.Code))
	if code == "429" || code == "529" || code == "1305" ||
		code == "rate_limit_error" || code == "overloaded_error" {
		return "provider"
	}
	if streamErr.Retryable {
		return "provider"
	}
	return ""
}

func categoryFromClassifier(class agentcontrol.ErrorClass) string {
	switch class {
	case agentcontrol.ErrorClassCancelled:
		return "cancelled"
	case agentcontrol.ErrorClassAuth:
		return "auth"
	case agentcontrol.ErrorClassContextOverflow:
		return "provider"
	case agentcontrol.ErrorClassRetryable:
		return "network"
	case agentcontrol.ErrorClassFatal:
		return ""
	}
	return ""
}

// categoryFromMessage is the last-resort scan for unrecognized errors.
// It deliberately mirrors the front-end's UserFacingErrors classifier
// so a front-end that has not yet received the structured fields (or
// one running against an older Go core) still classifies consistently.
func categoryFromMessage(message string) string {
	lower := strings.ToLower(message)
	switch {
	case isCancellationMessage(lower):
		return "cancelled"
	// Local comes BEFORE auth: a "permission denied: file /etc/hosts"
	// string has both "permission denied" and "file" tokens, and the
	// local check is the more specific match (auth + file → local).
	// The reverse order lets auth steal the local cases.
	case isLocalOperationMessage(lower):
		return "local"
	case isAuthOrPermissionMessage(lower):
		return "auth"
	case isResponseCompletedMissingMessage(lower):
		return "provider"
	case isProviderBusinessMessage(lower):
		return "provider"
	case isNetworkOrUpstreamMessage(lower):
		return "network"
	}
	return "internal"
}

func isCancellationMessage(lower string) bool {
	return strings.Contains(lower, "context canceled") ||
		strings.Contains(lower, "context cancelled") ||
		strings.Contains(lower, "user canceled") ||
		strings.Contains(lower, "user cancelled") ||
		strings.Contains(lower, "request canceled") ||
		strings.Contains(lower, "request cancelled") ||
		strings.Contains(lower, "operation was aborted") ||
		strings.Contains(lower, "aborterror")
}

func isAuthOrPermissionMessage(lower string) bool {
	return strings.Contains(lower, "401") || strings.Contains(lower, "403") ||
		strings.Contains(lower, "unauthorized") ||
		strings.Contains(lower, "unauthenticated") ||
		strings.Contains(lower, "forbidden") ||
		strings.Contains(lower, "permission denied") ||
		strings.Contains(lower, "api key") ||
		strings.Contains(lower, "access token") ||
		strings.Contains(lower, "invalid token") ||
		strings.Contains(lower, "oauth") ||
		strings.Contains(lower, "login required") ||
		strings.Contains(lower, "log in")
}

func isProviderBusinessMessage(lower string) bool {
	return strings.Contains(lower, "context_length_exceeded") ||
		strings.Contains(lower, "context window") ||
		strings.Contains(lower, "maximum context length") ||
		strings.Contains(lower, "too many tokens") ||
		strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "rate_limit") ||
		strings.Contains(lower, "overloaded") ||
		strings.Contains(lower, "content policy") ||
		strings.Contains(lower, "content_policy") ||
		strings.Contains(lower, "empty response") ||
		strings.Contains(lower, "empty answer") ||
		strings.Contains(lower, "model returned") ||
		strings.Contains(lower, "response failed") ||
		strings.Contains(lower, "response error") ||
		strings.Contains(lower, "invalid_request_error") ||
		strings.Contains(lower, "previous_response_not_found")
}

func isLocalOperationMessage(lower string) bool {
	return (strings.Contains(lower, "permission denied") || strings.Contains(lower, "operation not permitted")) &&
		(strings.Contains(lower, "file") || strings.Contains(lower, "path") ||
			strings.Contains(lower, "directory") || strings.Contains(lower, "command") ||
			strings.Contains(lower, "git ") || strings.Contains(lower, "gh ") ||
			strings.Contains(lower, "eacces") || strings.Contains(lower, "eperm")) ||
		strings.Contains(lower, "enoent") || strings.Contains(lower, "eacces") ||
		strings.Contains(lower, "eperm") || strings.Contains(lower, "no such file") ||
		strings.Contains(lower, "not a directory") || strings.Contains(lower, "is a directory") ||
		strings.Contains(lower, "outside the current workspace") ||
		strings.Contains(lower, "outside the current git repository") ||
		strings.Contains(lower, "selected path") || strings.Contains(lower, "git ") ||
		strings.Contains(lower, "github cli") || strings.Contains(lower, "exit status") ||
		strings.Contains(lower, "command failed")
}

func isNetworkOrUpstreamMessage(lower string) bool {
	return strings.Contains(lower, "network") ||
		strings.Contains(lower, "stream request failed") ||
		strings.Contains(lower, "request failed") ||
		strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "connection reset") ||
		strings.Contains(lower, "connection dropped") ||
		strings.Contains(lower, "no such host") ||
		strings.Contains(lower, "dial tcp") ||
		strings.Contains(lower, "dns") ||
		strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "deadline exceeded") ||
		strings.Contains(lower, "temporarily unavailable") ||
		strings.Contains(lower, "overloaded") ||
		strings.Contains(lower, "too many requests") ||
		strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "eof")
}

// extractCodeFromBody parses the JSON response body from a provider HTTP
// error and pulls the most useful code field. Covers the common shapes:
//
//	OpenAI:   {"error": {"code": "insufficient_quota", "message": "..."}}
//	Anthropic: {"error": {"type": "rate_limit_error", "message": "..."}}
//	Gemini:   {"error": {"code": 7, "status": "RESOURCE_EXHAUSTED", "message": "..."}}
//	Azure:    {"code": "previous_response_not_found", "error": "..."}
//
// Returns the empty string if the body is empty, unparseable, or has
// no recognizable code field.
func extractCodeFromBody(body string) string {
	if strings.TrimSpace(body) == "" {
		return ""
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return ""
	}
	// Top-level "code" (Azure: string, Gemini: number).
	if c, ok := parsed["code"].(string); ok && strings.TrimSpace(c) != "" {
		return strings.TrimSpace(c)
	}
	if n, ok := parsed["code"].(float64); ok && n > 0 {
		return strconv.FormatInt(int64(n), 10)
	}
	if e, ok := parsed["error"].(map[string]any); ok {
		// Gemini-style "error.status" string (e.g. "RESOURCE_EXHAUSTED").
		// Gemini pairs this with a numeric code; the status string is
		// more informative in a screenshot, so check it first.
		if s, ok := e["status"].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
		// Nested "error.type" (Anthropic).
		if t, ok := e["type"].(string); ok && strings.TrimSpace(t) != "" {
			return strings.TrimSpace(t)
		}
		// Nested "error.code" (OpenAI: string, Gemini: number).
		if c, ok := e["code"].(string); ok && strings.TrimSpace(c) != "" {
			return strings.TrimSpace(c)
		}
		if n, ok := e["code"].(float64); ok && n > 0 {
			return strconv.FormatInt(int64(n), 10)
		}
	}
	return ""
}

// extractCodeFromMessage pulls the parens-wrapped code out of
// wuu-style "stream request failed: stream error (CODE)" wrapping.
// Mirrors the front-end's extractSpecificTitle logic.
func extractCodeFromMessage(message string) string {
	lower := strings.ToLower(message)
	if isResponseCompletedMissingMessage(lower) {
		return responseCompletedMissingCode
	}
	if !strings.Contains(lower, "stream error (") {
		return ""
	}
	idx := strings.LastIndex(message, "(")
	end := strings.LastIndex(message, ")")
	if idx < 0 || end < 0 || end <= idx {
		return ""
	}
	inner := strings.TrimSpace(message[idx+1 : end])
	if inner == "" || strings.ContainsAny(inner, " \t\n") {
		return ""
	}
	return inner
}

// buildAction picks the user-facing next-step button for the chip. The
// action is the authoritative source — the front-end does not
// re-derive it. The front-end can decide whether to render the action
// as a button below the chip, in a kebab menu, or anywhere else.
func buildAction(category string, code, message string) *TurnErrorAction {
	switch category {
	case "cancelled":
		return nil
	case "auth":
		return &TurnErrorAction{
			Reason:  "reauth",
			Title:   "Provider 凭据或权限不足",
			Message: "请在 Settings → Providers 检查当前 Provider 的 API 密钥和权限。",
			Label:   "查看 Provider 设置",
		}
	case "provider":
		return buildProviderAction(code, message)
	case "network":
		return &TurnErrorAction{
			Reason:  "retry",
			Title:   "网络问题",
			Message: "无法完成这次请求。可以稍后再发，或检查网络连接。",
			Label:   "稍后重试",
		}
	case "tool":
		return &TurnErrorAction{
			Reason:  "view_debug",
			Title:   "工具调用失败",
			Message: "某个工具没有完成。原始错误已留在调试信息中。",
			Label:   "复制调试信息",
		}
	case "local":
		return &TurnErrorAction{
			Reason:  "open_settings",
			Title:   "本地操作失败",
			Message: "无法完成本地文件、命令或权限相关操作。",
			Label:   "查看权限设置",
		}
	case "internal":
		return &TurnErrorAction{
			Reason:  "copy_debug",
			Title:   "wuu 内部错误",
			Message: "没有完成这次请求。调试信息可用于排查。",
			Label:   "复制调试信息",
		}
	}
	return nil
}

// buildProviderAction specializes the action for the most common
// provider-side sub-cases. The chip's visible title is the specific
// code (e.g. "insufficient_quota"); the action's title is the
// user-facing summary.
func buildProviderAction(code, message string) *TurnErrorAction {
	lower := strings.ToLower(code + " " + message)
	if isResponseCompletedMissingMessage(lower) {
		return &TurnErrorAction{
			Reason:  "view_debug",
			Title:   "部分回答未完成",
			Message: "Provider WS 流在 response.completed 前断开；这次回答可能不完整。",
			Label:   "复制调试信息",
		}
	}
	if isRateLimitPattern(lower) {
		return &TurnErrorAction{
			Reason:  "wait",
			Title:   "Provider 限流或配额用尽",
			Message: "Provider 限流或配额已用完。稍后重试，或检查 Provider 账户的额度。",
			Label:   "稍后重试",
		}
	}
	if isOverloadPattern(lower) {
		return &TurnErrorAction{
			Reason:  "wait",
			Title:   "Provider 繁忙",
			Message: "Provider 当前过载，稍后重试。",
			Label:   "稍后重试",
		}
	}
	if isContextOverflowPattern(lower) {
		return &TurnErrorAction{
			Reason:  "compact",
			Title:   "上下文超出窗口",
			Message: "请求超出当前模型的上下文窗口。压缩上下文后重试。",
			Label:   "压缩上下文",
		}
	}
	return &TurnErrorAction{
		Reason:  "view_debug",
		Title:   "模型没有完成请求",
		Message: "可能是上下文超出窗口、限流或上游中断。",
		Label:   "复制调试信息",
	}
}

func isRateLimitPattern(lower string) bool {
	return strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "rate_limit") ||
		strings.Contains(lower, "too many requests") ||
		strings.Contains(lower, "insufficient_quota") ||
		strings.Contains(lower, "quota exceeded") ||
		strings.Contains(lower, "usage_limit") ||
		// Gemini uses "RESOURCE_EXHAUSTED" alongside its numeric code;
		// semantically it is a quota/rate-limit signal (the resource
		// the user paid for is gone) and the chip should suggest
		// waiting or upgrading rather than retrying blindly.
		strings.Contains(lower, "resource_exhausted") ||
		strings.Contains(lower, "resource has been exhausted") ||
		strings.Contains(lower, "访问量过大") ||
		strings.Contains(lower, "稍后再试") ||
		strings.Contains(lower, "freeusagelimit") ||
		strings.Contains(lower, "gousagelimit")
}

func isOverloadPattern(lower string) bool {
	return strings.Contains(lower, "overloaded") ||
		strings.Contains(lower, "overloaded_error") ||
		strings.Contains(lower, "temporarily unavailable") ||
		strings.Contains(lower, "529")
}

func isContextOverflowPattern(lower string) bool {
	return strings.Contains(lower, "context_length_exceeded") ||
		strings.Contains(lower, "context window") ||
		strings.Contains(lower, "maximum context length") ||
		strings.Contains(lower, "prompt is too long") ||
		strings.Contains(lower, "request_too_large") ||
		strings.Contains(lower, "request too large") ||
		strings.Contains(lower, "request buffer limit") ||
		strings.Contains(lower, "input is too long") ||
		strings.Contains(lower, "model_context_window_exceeded") ||
		strings.Contains(lower, "too many tokens")
}

func isResponseCompletedMissingMessage(lower string) bool {
	return strings.Contains(lower, "before response.completed")
}
