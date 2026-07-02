export type UserFacingErrorCategory =
  | "cancelled"
  | "network"
  | "auth"
  | "provider"
  | "tool"
  | "local"
  | "internal";
export type UserFacingErrorTone = "neutral" | "warning" | "auth" | "error";
export type UserFacingErrorContext = "turn" | "tool" | "status";

import type { TurnError } from "../shared/protocol";

/**
 * Kinds map to renderer-side handlers. The data layer doesn't decide
 * what the action does — it just declares the intent. The UI layer
 * (TurnNotice / NoticeActions) maps each kind to a concrete handler.
 *
 * Keep this set small. Anything renderer-specific (a "Copy" button,
 * a "Submit feedback" link) belongs to a particular surface, not the
 * error model.
 */
export type UserFacingErrorActionKind =
  | "retry"
  | "switchModel"
  | "compactContext"
  | "reauth"
  | "openSettings"
  | "copyDebug"
  | "submitFeedback";

export type UserFacingErrorAction = {
  /** Stable identifier — used for analytics, tests, and keying. */
  kind: UserFacingErrorActionKind;
  /** Visible label. Short, sentence-case, no trailing punctuation. */
  label: string;
  /** Optional payload passed to the handler verbatim. */
  payload?: Record<string, unknown>;
  /** Secondary actions are visually de-emphasized in the notice. */
  variant?: "primary" | "secondary";
};

export type UserFacingErrorDisplay = {
  category: UserFacingErrorCategory;
  tone: UserFacingErrorTone;
  title: string;
  detail: string;
  /**
   * Concrete next-step the user can take. The renderer renders these
   * as inline text links beneath the notice copy. Absence (empty
   * array) is a valid signal that the user can't do anything useful
   * right now — the notice stays as text-only.
   */
  recommendedActions: UserFacingErrorAction[];
};

export function rawErrorMessage(error: unknown, fallback = ""): string {
  if (error instanceof Error) {
    return error.message || fallback;
  }
  if (typeof error === "string") {
    return error || fallback;
  }
  return fallback;
}

// Reason phrases for the HTTP status codes the classifier can extract from
// a provider error. The phrase is appended to the code ("401 unauthorized")
// so screenshots carry enough signal to identify the upstream side of the
// problem. Kept here so the test file does not need to be updated when the
// vocabulary grows.
const HTTP_REASON_PHRASES: Record<string, string> = {
  "400": "bad request",
  "401": "unauthorized",
  "403": "forbidden",
  "404": "not found",
  "408": "timeout",
  "413": "payload too large",
  "429": "rate limit",
  "500": "server error",
  "502": "bad gateway",
  "503": "unavailable",
  "504": "gateway timeout",
  "529": "overloaded",
};

const RESPONSE_COMPLETED_MISSING_TITLE = "stream_closed_before_response.completed";

function extractHttpCode(message: string): string | undefined {
  const match = message.match(/\b(400|401|403|404|408|413|429|500|502|503|504|529)\b/);
  return match?.[1];
}

function httpTitle(code: string): string {
  return HTTP_REASON_PHRASES[code] ? `${code} ${HTTP_REASON_PHRASES[code]}` : code;
}

function structuredStatusTitle(structured: TurnError | undefined): string | undefined {
  const statusCode = structured?.status_code;
  if (typeof statusCode !== "number" || !Number.isFinite(statusCode) || statusCode <= 0) {
    return undefined;
  }
  return httpTitle(String(Math.trunc(statusCode)));
}

/**
 * Pull a specific identifier out of the raw error so the chip is readable
 * at a glance and the screenshot carries enough signal to triage. Returns
 * undefined when no specific identifier can be pulled — the caller then
 * falls back to a category-level title.
 *
 * The list is intentionally short: anything renderer-specific belongs to
 * the turn notice, not the error model.
 */
function extractSpecificTitle(
  message: string,
  category: UserFacingErrorCategory,
): string | undefined {
  const lower = message.toLowerCase();
  switch (category) {
    case "cancelled":
      return undefined;
    case "network": {
      // OpenAI/Anthropic-style wrapped errors look like
      // "stream request failed: stream error (previous_response_not_found)".
      // The parenthesized token at the end of the message is the actual
      // provider error code, which is what the user (and support) need to
      // see in the chip and in a screenshot.
      if (isResponseCompletedMissingMessage(lower)) {
        return RESPONSE_COMPLETED_MISSING_TITLE;
      }
      const wrapped = message.match(/\(([^()]+)\)\s*$/);
      if (wrapped) return wrapped[1];
      const code = extractHttpCode(message);
      if (code) return httpTitle(code);
      if (lower.includes("rate limit") || lower.includes("too many requests")) return "rate limit";
      if (lower.includes("overloaded")) return "overloaded";
      if (lower.includes("timeout") || lower.includes("deadline exceeded")) return "timeout";
      if (lower.includes("connection refused")) return "connection refused";
      if (lower.includes("connection reset")) return "connection reset";
      if (lower.includes("connection dropped")) return "connection dropped";
      if (lower.includes("no such host")) return "host not found";
      if (lower.includes("dns")) return "dns error";
      if (lower.includes("eof")) return "eof";
      if (lower.includes("temporarily unavailable")) return "temporarily unavailable";
      return undefined;
    }
    case "auth": {
      const code = extractHttpCode(message);
      if (code) return httpTitle(code);
      if (lower.includes("api key")) return "invalid api key";
      if (lower.includes("oauth")) return "oauth failed";
      if (lower.includes("invalid token") || lower.includes("access token")) return "invalid token";
      if (lower.includes("permission denied")) return "permission denied";
      return undefined;
    }
    case "provider": {
      if (isResponseCompletedMissingMessage(lower)) {
        return RESPONSE_COMPLETED_MISSING_TITLE;
      }
      if (
        lower.includes("context_length_exceeded") ||
        lower.includes("context window") ||
        lower.includes("maximum context length")
      ) {
        return "context_length_exceeded";
      }
      if (lower.includes("too many tokens")) return "too many tokens";
      if (lower.includes("content policy") || lower.includes("content_policy")) return "content_policy";
      if (lower.includes("rate_limit") || lower.includes("rate limit")) return "rate_limit";
      if (lower.includes("model_not_found")) return "model_not_found";
      if (lower.includes("model returned")) return "model error";
      if (lower.includes("empty response") || lower.includes("empty answer")) return "empty response";
      if (lower.includes("response failed") || lower.includes("response error")) return "response failed";
      if (lower.includes("invalid_request_error")) return "invalid_request_error";
      return undefined;
    }
    case "tool": {
      const first = message.split(/[.\n]/)[0]?.trim();
      if (!first) return undefined;
      return first.length > 60 ? `${first.slice(0, 57)}…` : first;
    }
    case "local": {
      if (lower.includes("permission denied")) return "permission denied";
      if (lower.includes("enoent") || lower.includes("no such file")) return "file not found";
      if (lower.includes("not a directory")) return "not a directory";
      if (lower.includes("is a directory")) return "is a directory";
      if (
        lower.includes("outside the current workspace") ||
        lower.includes("outside the current git repository")
      ) {
        return "outside workspace";
      }
      if (lower.includes("command failed") || lower.includes("exit status")) return "command failed";
      return undefined;
    }
    case "internal":
    default:
      return undefined;
  }
}

export function userFacingErrorForMessage(
  input: string | TurnError | undefined,
  context: UserFacingErrorContext
): UserFacingErrorDisplay {
  // Accept either a raw string (legacy callers, including server-error
  // text and the composer status row) or a structured TurnError from the
  // Go core. The Go side's BuildTurnError is the authoritative source:
  // when its TurnError arrives, we use it as-is and only fall back to
  // message-substring matching when fields are missing (so an older app
  // server or a manual string still produces a sensible display).
  const structured: TurnError | undefined =
    typeof input === "object" && input !== null ? input : undefined;
  const message = (
    structured?.message ?? (typeof input === "string" ? input : "") ?? ""
  ).trim();

  // Category: prefer the wire value, fall back to the legacy classifier.
  // The Go side's 7 categories match UserFacingErrorCategory 1:1, so
  // the cast is safe.
  const category: UserFacingErrorCategory =
    (structured?.category as UserFacingErrorCategory | undefined) ??
    classifyUserFacingError(message, context);

  // Title: prefer the structured code, then keyword extraction from the
  // raw message, then the category's default Chinese label. The chip's
  // user-visible label is always the most specific signal available so
  // a screenshot carries enough info to triage.
  const specificTitle = extractSpecificTitle(message, category);
  const statusTitle = structuredStatusTitle(structured);
  const title =
    structured?.code?.trim() || statusTitle || specificTitle || defaultTitleForCategory(category);

  // Detail (hover): prefer the action's longer user-facing message
  // when the Go side provided one, fall back to the category's
  // Chinese default.
  const detail =
    structured?.action?.message?.trim() ||
    specificDetailForMessage(message, category) ||
    defaultDetailForCategory(category);

  // Actions: structured action's `reason` takes precedence, fall back
  // to the category-driven default (e.g. the auth case gets
  // openSettings, the tool/internal case gets copyDebug). When neither
  // is useful (cancelled) the array is empty.
  const recommendedActions = structured?.action
    ? actionsFromStructuredAction(structured, category, message)
    : defaultActionsForCategory(category, context, message);

  return {
    category,
    tone: toneForCategory(category),
    title,
    detail,
    recommendedActions,
  };
}

/**
 * Display for a turn that completed normally but never produced a
 * `final_answer` (only `commentary` items remain). Soft outcome-state
 * signal, not a failure — the model ran, it just talked instead of
 * answering. Routes through the same chip pipeline as cancelled /
 * failed turns so the user sees one consistent notice shape across
 * all "this turn ended in a non-answer state" outcomes.
 *
 * `category: "internal"` is a placeholder for the closed-union type;
 * tone / title / detail / actions are all set explicitly here so the
 * category-driven defaults never fire.
 */
export function userFacingErrorForMissingReply(): UserFacingErrorDisplay {
  return {
    category: "internal",
    tone: "warning",
    title: "无最终回答",
    detail: "这轮只保留了过程记录，没有生成最终回答。",
    recommendedActions: [],
  };
}

function toneForCategory(category: UserFacingErrorCategory): UserFacingErrorTone {
  switch (category) {
    case "cancelled":
      return "neutral";
    case "auth":
      return "auth";
    case "network":
    case "provider":
    case "tool":
    case "local":
    case "internal":
      return "error";
  }
}

function defaultTitleForCategory(category: UserFacingErrorCategory): string {
  switch (category) {
    case "cancelled":
      return "已停止";
    case "network":
      return "network error";
    case "auth":
      return "auth error";
    case "provider":
      return "provider error";
    case "tool":
      return "工具调用失败";
    case "local":
      return "本地操作失败";
    case "internal":
      return "wuu 内部错误";
  }
}

function defaultDetailForCategory(category: UserFacingErrorCategory): string {
  switch (category) {
    case "cancelled":
      return "这次请求已停止，可以继续发送消息。";
    case "network":
      return "没有完成这次请求。可以稍后再发，或检查当前 provider 状态。";
    case "auth":
      return "Provider 凭据或权限不足，请在 Settings → Providers 检查。";
    case "provider":
      return "可能是上下文超出窗口、模型限流或上游中断。";
    case "tool":
      return "某个工具没有完成。原始错误已留在调试信息中。";
    case "local":
      return "无法完成本地文件、命令或权限相关操作。";
    case "internal":
      return "没有完成这次请求。调试信息可用于排查。";
  }
}

function specificDetailForMessage(
  message: string,
  category: UserFacingErrorCategory,
): string | undefined {
  const normalized = message.toLowerCase();
  if (
    (category === "provider" || category === "network") &&
    isResponseCompletedMissingMessage(normalized)
  ) {
    return "Provider WS 流在 response.completed 前断开；这次回答可能不完整。";
  }
  return undefined;
}

// defaultActionsForCategory is the category-driven fallback used when
// the Go side did not send a structured action. Mirrors the previous
// behavior: auth/local open settings; everything else (except
// cancelled) gets a copy-debug fallback.
function defaultActionsForCategory(
  category: UserFacingErrorCategory,
  context: UserFacingErrorContext,
  message: string
): UserFacingErrorAction[] {
  switch (category) {
    case "cancelled":
      return [];
    case "auth":
      return [
        {
          kind: "openSettings",
          label: "查看 Provider 设置",
          variant: "primary",
          payload: { focus: "providers" },
        },
      ];
    case "local":
      return [
        {
          kind: "openSettings",
          label: "查看权限设置",
          variant: "primary",
          payload: { focus: "workspace" },
        },
      ];
    case "network":
    case "provider":
    case "tool":
    case "internal":
      return [copyDebugAction(category, context, message)];
  }
}

// actionsFromStructuredAction translates the Go side's stable
// `reason` taxonomy into renderer-side UserFacingErrorActionKind
// values. The reason values mirror opencode's opencode-ai/opencode
// `RetryReason` enum (reauth, compact, wait, retry, view_debug, etc.)
// — keeping the same vocabulary makes cross-tool telemetry easier.
function actionsFromStructuredAction(
  structured: TurnError,
  category: UserFacingErrorCategory,
  message: string
): UserFacingErrorAction[] {
  const action = structured.action;
  if (!action) {
    return [];
  }
  switch (action.reason) {
    case "reauth":
      return [
        {
          kind: "openSettings",
          label: action.label || "查看 Provider 设置",
          variant: "primary",
          payload: { focus: "providers" },
        },
      ];
    case "compact":
      return [
        {
          kind: "compactContext",
          label: action.label || "压缩上下文",
          variant: "primary",
        },
      ];
    case "wait":
    case "retry":
      return [
        {
          kind: "retry",
          label: action.label || "稍后重试",
          variant: "primary",
        },
      ];
    case "view_debug":
    case "copy_debug":
      return [
        {
          kind: "copyDebug",
          label: action.label || "复制调试信息",
          variant: "secondary",
          // Include the structured fields so the clipboard payload is
          // useful for triage: "what provider, what code, what status".
          payload: {
            category,
            context: "turn",
            message: structured.message,
            code: structured.code,
            provider: structured.provider,
            status_code: structured.status_code,
          },
        },
      ];
    case "open_settings":
      return [
        {
          kind: "openSettings",
          label: action.label || "查看权限设置",
          variant: "primary",
          payload: { focus: "workspace" },
        },
      ];
    default:
      return [];
  }
}

function copyDebugAction(
  category: UserFacingErrorCategory,
  context: UserFacingErrorContext,
  message: string,
): UserFacingErrorAction {
  return {
    kind: "copyDebug",
    label: "复制调试信息",
    variant: "secondary",
    payload: {
      category,
      context,
      message: truncateDebugMessage(message),
    },
  };
}

function truncateDebugMessage(message: string): string {
  const limit = 12_000;
  if (message.length <= limit) {
    return message;
  }
  return `${message.slice(0, limit)}…`;
}

function classifyUserFacingError(message: string, context: UserFacingErrorContext): UserFacingErrorCategory {
  const normalized = message.toLowerCase();
  if (isCancellationMessage(normalized)) {
    return "cancelled";
  }
  if (isLocalOperationError(normalized)) {
    return "local";
  }
  if (isAuthOrPermissionError(normalized)) {
    return "auth";
  }
  if (isProviderBusinessError(normalized)) {
    return "provider";
  }
  if (isNetworkOrUpstreamError(normalized)) {
    return "network";
  }
  return context === "tool" ? "tool" : "internal";
}

export function isCancellationMessage(message: string): boolean {
  return (
    message.includes("context canceled") ||
    message.includes("context cancelled") ||
    message.includes("user canceled") ||
    message.includes("user cancelled") ||
    message.includes("request canceled") ||
    message.includes("request cancelled") ||
    message.includes("operation was aborted") ||
    message.includes("aborterror")
  );
}

function isAuthOrPermissionError(message: string): boolean {
  return (
    /\b(401|403)\b/.test(message) ||
    message.includes("unauthorized") ||
    message.includes("unauthenticated") ||
    message.includes("forbidden") ||
    message.includes("permission denied") ||
    message.includes("api key") ||
    message.includes("access token") ||
    message.includes("invalid token") ||
    message.includes("oauth") ||
    message.includes("login required") ||
    message.includes("log in")
  );
}

function isNetworkOrUpstreamError(message: string): boolean {
  return (
    /\b(429|500|502|503|504|529)\b/.test(message) ||
    message.includes("network") ||
    message.includes("stream request failed") ||
    message.includes("request failed") ||
    message.includes("connection refused") ||
    message.includes("connection reset") ||
    message.includes("connection dropped") ||
    message.includes("no such host") ||
    message.includes("dial tcp") ||
    message.includes("dns") ||
    message.includes("timeout") ||
    message.includes("deadline exceeded") ||
    message.includes("temporarily unavailable") ||
    message.includes("overloaded") ||
    message.includes("too many requests") ||
    message.includes("rate limit") ||
    message.includes("eof")
  );
}

function isProviderBusinessError(message: string): boolean {
  return (
    isResponseCompletedMissingMessage(message) ||
    message.includes("context_length_exceeded") ||
    message.includes("context window") ||
    message.includes("maximum context length") ||
    message.includes("too many tokens") ||
    message.includes("empty response") ||
    message.includes("empty answer") ||
    message.includes("model returned") ||
    message.includes("provider") ||
    message.includes("response failed") ||
    message.includes("response error") ||
    message.includes("content policy") ||
    message.includes("invalid_request_error")
  );
}

function isResponseCompletedMissingMessage(message: string): boolean {
  return message.includes("before response.completed");
}

function isLocalOperationError(message: string): boolean {
  const hasLocalPermission =
    (message.includes("permission denied") || message.includes("operation not permitted")) &&
    (message.includes("file") ||
      message.includes("path") ||
      message.includes("directory") ||
      message.includes("command") ||
      message.includes("git") ||
      message.includes("gh") ||
      message.includes("eacces") ||
      message.includes("eperm"));
  return (
    hasLocalPermission ||
    message.includes("enoent") ||
    message.includes("eacces") ||
    message.includes("eperm") ||
    message.includes("no such file") ||
    message.includes("not a directory") ||
    message.includes("is a directory") ||
    message.includes("outside the current workspace") ||
    message.includes("outside the current git repository") ||
    message.includes("selected path") ||
    message.includes("git ") ||
    message.includes("github cli") ||
    message.includes("exit status") ||
    message.includes("command failed")
  );
}

export function statusMessageForError(error: unknown, fallback: string): string {
  // The composer status row renders a single-line label between two
  // dividers, so we return only the classified title. The full detail
  // is still rendered by the inline turn notice, which has room to
  // span more than one line when needed.
  const display = userFacingErrorForMessage(rawErrorMessage(error, fallback), "status");
  return display.title;
}

export function statusToneClass(status: string): string {
  const trimmed = status.trim();
  if (!trimmed || trimmed === "ready" || trimmed === "connecting" || trimmed === "opening" || trimmed.startsWith("正在")) {
    return "";
  }
  if (trimmed.includes("权限") || trimmed.includes("登录")) {
    return " auth";
  }
  if (
    trimmed.includes("失败") ||
    trimmed.includes("错误") ||
    trimmed.includes("不可用") ||
    trimmed.includes("内部") ||
    trimmed.includes("stream_closed") ||
    trimmed.includes("response.completed")
  ) {
    return " error";
  }
  return "";
}
