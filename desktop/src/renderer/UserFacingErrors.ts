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
  /**
   * Raw machine identifier (provider error code, wrapped stream token,
   * untranslated tool message) kept next to the Chinese title so a
   * screenshot still carries the triage signal. Rendered muted — same
   * neutral family as the divider lines, not the tone color.
   */
  code?: string;
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
// a provider error. The numeric code stays in the title ("401 未授权") so
// screenshots carry enough signal to identify the upstream side of the
// problem, while the phrase itself reads in Chinese like the rest of the
// notice vocabulary.
const HTTP_REASON_PHRASES: Record<string, string> = {
  "400": "请求无效",
  "401": "未授权",
  "403": "无访问权限",
  "404": "资源不存在",
  "408": "请求超时",
  "413": "请求体过大",
  "429": "触发限流",
  "500": "服务器错误",
  "502": "网关错误",
  "503": "服务不可用",
  "504": "网关超时",
  "529": "上游过载",
};

const RESPONSE_COMPLETED_MISSING_CODE = "stream_closed_before_response.completed";
const RESPONSE_COMPLETED_MISSING_TITLE = "回答未完整返回";

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

type SpecificDisplay = {
  /** Readable Chinese title, when the message maps to a known situation. */
  title?: string;
  /**
   * Raw machine identifier worth keeping next to the title (provider
   * error code, untranslated tool message). Only set when the identifier
   * itself carries triage signal that the Chinese title alone loses.
   */
  code?: string;
};

/**
 * Pull a specific situation out of the raw error so the chip is readable
 * at a glance. Known keywords map to a Chinese title; identifiers we can't
 * translate reliably (arbitrary provider codes, raw tool messages) are
 * surfaced through `code` instead, and the caller falls back to the
 * category-level title.
 *
 * The list is intentionally short: anything renderer-specific belongs to
 * the turn notice, not the error model.
 */
function extractSpecificDisplay(
  message: string,
  category: UserFacingErrorCategory,
): SpecificDisplay {
  const lower = message.toLowerCase();
  switch (category) {
    case "cancelled":
      return {};
    case "network": {
      if (isResponseCompletedMissingMessage(lower)) {
        return { title: RESPONSE_COMPLETED_MISSING_TITLE, code: RESPONSE_COMPLETED_MISSING_CODE };
      }
      // OpenAI/Anthropic-style wrapped errors look like
      // "stream request failed: stream error (previous_response_not_found)".
      // The parenthesized token at the end of the message is the actual
      // provider error code — an arbitrary identifier we can't translate,
      // so it rides along as the muted code.
      const wrapped = message.match(/\(([^()]+)\)\s*$/);
      if (wrapped) return { code: wrapped[1] };
      const code = extractHttpCode(message);
      if (code) return { title: httpTitle(code) };
      if (lower.includes("rate limit") || lower.includes("too many requests")) return { title: "触发限流" };
      if (lower.includes("overloaded")) return { title: "上游过载" };
      if (lower.includes("timeout") || lower.includes("deadline exceeded")) return { title: "请求超时" };
      if (lower.includes("connection refused")) return { title: "连接被拒绝" };
      if (lower.includes("connection reset")) return { title: "连接被重置" };
      if (lower.includes("connection dropped")) return { title: "连接已断开" };
      if (lower.includes("no such host")) return { title: "无法解析主机" };
      if (lower.includes("dns")) return { title: "DNS 解析失败" };
      if (lower.includes("eof")) return { title: "连接意外中断" };
      if (lower.includes("temporarily unavailable")) return { title: "服务暂时不可用" };
      return {};
    }
    case "auth": {
      const code = extractHttpCode(message);
      if (code) return { title: httpTitle(code) };
      if (lower.includes("api key")) return { title: "API Key 无效" };
      if (lower.includes("oauth")) return { title: "OAuth 登录失败" };
      if (lower.includes("invalid token") || lower.includes("access token")) return { title: "凭据已失效" };
      if (lower.includes("permission denied")) return { title: "没有访问权限" };
      return {};
    }
    case "provider": {
      if (isResponseCompletedMissingMessage(lower)) {
        return { title: RESPONSE_COMPLETED_MISSING_TITLE, code: RESPONSE_COMPLETED_MISSING_CODE };
      }
      if (
        lower.includes("context_length_exceeded") ||
        lower.includes("context window") ||
        lower.includes("maximum context length")
      ) {
        return { title: "上下文超出窗口" };
      }
      if (lower.includes("too many tokens")) return { title: "tokens 超出限制" };
      if (lower.includes("content policy") || lower.includes("content_policy")) return { title: "触发内容安全策略" };
      if (lower.includes("rate_limit") || lower.includes("rate limit")) return { title: "触发限流" };
      if (lower.includes("model_not_found")) return { title: "模型不存在" };
      if (lower.includes("model returned")) return { title: "模型返回错误" };
      if (lower.includes("empty response") || lower.includes("empty answer")) return { title: "模型返回为空" };
      if (lower.includes("response failed") || lower.includes("response error")) return { title: "响应失败" };
      if (lower.includes("invalid_request_error")) return { title: "请求参数无效" };
      return {};
    }
    case "tool": {
      const first = message.split(/[.\n]/)[0]?.trim();
      if (!first) return {};
      const clipped = first.length > 60 ? `${first.slice(0, 57)}…` : first;
      // A tool error that is already Chinese reads fine as the title; an
      // English one is an arbitrary message we can't translate, so it
      // becomes the muted code next to the category title.
      if (/[一-鿿]/.test(clipped)) return { title: clipped };
      return { code: clipped };
    }
    case "local": {
      if (lower.includes("permission denied")) return { title: "权限不足" };
      if (lower.includes("enoent") || lower.includes("no such file")) return { title: "文件不存在" };
      if (lower.includes("not a directory")) return { title: "路径不是目录" };
      if (lower.includes("is a directory")) return { title: "目标是目录" };
      if (
        lower.includes("outside the current workspace") ||
        lower.includes("outside the current git repository")
      ) {
        return { title: "超出工作区范围" };
      }
      if (lower.includes("command failed") || lower.includes("exit status")) return { title: "命令执行失败" };
      return {};
    }
    case "internal":
    default:
      return {};
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

  // Title: always a Chinese label — keyword extraction from the raw
  // message (or from the structured code, when the code itself is a
  // known identifier), then the HTTP status phrase, then the category
  // default. The raw machine identifier is not lost: it rides along in
  // `code` and renders muted next to the title, so a screenshot still
  // carries enough info to triage.
  const structuredCode = structured?.code?.trim() || undefined;
  const specific = extractSpecificDisplay(message, category);
  const specificFromCode = structuredCode
    ? extractSpecificDisplay(structuredCode, category)
    : {};
  const statusTitle = structuredStatusTitle(structured);
  const title =
    specific.title ||
    specificFromCode.title ||
    statusTitle ||
    defaultTitleForCategory(category);
  const code = dedupedCode(structuredCode ?? specific.code, title);

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
    code,
    detail,
    recommendedActions,
  };
}

// A code that repeats what the title already says (same string, or an
// HTTP-phrase code whose status number is already in the title) is noise
// next to the Chinese label — drop it.
function dedupedCode(code: string | undefined, title: string): string | undefined {
  if (!code) return undefined;
  if (title.includes(code)) return undefined;
  const httpCode = extractHttpCode(code);
  if (httpCode && title.includes(httpCode)) return undefined;
  return code;
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
      return "网络异常";
    case "auth":
      return "认证失败";
    case "provider":
      return "模型服务异常";
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
  // dividers, so we return the classified title, with the machine code
  // appended when one exists so the row keeps its triage signal. The
  // full detail is still rendered by the inline turn notice.
  const display = userFacingErrorForMessage(rawErrorMessage(error, fallback), "status");
  return display.code ? `${display.title} (${display.code})` : display.title;
}

// Keyword lists mirror the title vocabulary produced above (specific
// titles, HTTP phrases, category defaults) — extend them together.
const AUTH_STATUS_KEYWORDS = ["权限", "登录", "认证", "凭据", "未授权", "API Key", "OAuth"];
const ERROR_STATUS_KEYWORDS = [
  "失败",
  "错误",
  "异常",
  "不可用",
  "内部",
  "超时",
  "限流",
  "中断",
  "断开",
  "拒绝",
  "重置",
  "过载",
  "过大",
  "无效",
  "不存在",
  "为空",
  "超出",
  "无法解析",
  "内容安全",
  "未完整",
  "stream_closed",
  "response.completed",
];

export function statusToneClass(status: string): string {
  const trimmed = status.trim();
  if (!trimmed || trimmed === "ready" || trimmed === "connecting" || trimmed === "opening" || trimmed.startsWith("正在")) {
    return "";
  }
  if (AUTH_STATUS_KEYWORDS.some((keyword) => trimmed.includes(keyword))) {
    return " auth";
  }
  if (ERROR_STATUS_KEYWORDS.some((keyword) => trimmed.includes(keyword))) {
    return " error";
  }
  return "";
}
