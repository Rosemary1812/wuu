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

export type UserFacingErrorDisplay = {
  category: UserFacingErrorCategory;
  tone: UserFacingErrorTone;
  title: string;
  detail: string;
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

export function userFacingErrorForMessage(
  rawMessage: string | undefined,
  context: UserFacingErrorContext
): UserFacingErrorDisplay {
  const message = (rawMessage ?? "").trim();
  const category = classifyUserFacingError(message, context);
  switch (category) {
    case "cancelled":
      return { category, tone: "neutral", title: "已停止", detail: "这次请求已停止，可以继续发送消息。" };
    case "network":
      return { category, tone: "error", title: "连接暂时不可用", detail: "没有完成这次请求。请稍后重试。" };
    case "auth":
      return {
        category,
        tone: "auth",
        title: "需要重新登录或检查权限",
        detail: "当前凭据或权限不足，处理没有完成。"
      };
    case "provider":
      return {
        category,
        tone: "error",
        title: "模型没有完成请求",
        detail: "可以调整输入、切换模型，或稍后重试。"
      };
    case "tool":
      return {
        category,
        tone: "error",
        title: "工具调用失败",
        detail: "某个工具没有完成。原始错误已留在调试信息中。"
      };
    case "local":
      return {
        category,
        tone: "error",
        title: "本地操作失败",
        detail: "无法完成本地文件、命令或权限相关操作。"
      };
    case "internal":
    default:
      return {
        category: "internal",
        tone: "error",
        title: "wuu 遇到内部错误",
        detail: "请重试；如果反复出现，可以复制调试信息排查。"
      };
  }
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
  if (isNetworkOrUpstreamError(normalized)) {
    return "network";
  }
  if (isProviderBusinessError(normalized)) {
    return "provider";
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
  const display = userFacingErrorForMessage(rawErrorMessage(error, fallback), "status");
  return `${display.title}。${display.detail}`;
}

export function statusToneClass(status: string): string {
  const trimmed = status.trim();
  if (!trimmed || trimmed === "ready" || trimmed === "connecting" || trimmed === "opening" || trimmed.startsWith("正在")) {
    return "";
  }
  if (trimmed.includes("权限") || trimmed.includes("登录")) {
    return " auth";
  }
  if (trimmed.includes("失败") || trimmed.includes("错误") || trimmed.includes("不可用") || trimmed.includes("内部")) {
    return " error";
  }
  return "";
}
