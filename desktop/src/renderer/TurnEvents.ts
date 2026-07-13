import type { ThreadItem, ThreadItemStatus, Turn } from "../shared/protocol";
import {
  isCancellationMessage,
  userFacingErrorForMessage,
  userFacingErrorForMissingReply,
  type UserFacingErrorDisplay,
} from "./UserFacingErrors";

export type TurnEventKind =
  | "user_stopped"
  | "network_lost"
  | "auth_required"
  | "provider_failed"
  | "tool_failed"
  | "local_failed"
  | "internal_error"
  | "context_compacting"
  | "context_compacted"
  | "context_compaction_failed"
  | "missing_final_answer";

export type TurnEventSource = "turn" | "item";

export type TurnEventDisplay =
  | {
      kind: TurnEventKind;
      source: TurnEventSource;
      presentation: "notice";
      notice: UserFacingErrorDisplay;
    }
  | {
      kind:
        | "context_compacting"
        | "context_compacted"
        | "context_compaction_failed";
      source: "item";
      presentation: "context_compaction";
      text?: string;
      reason?: string;
      status?: ThreadItemStatus;
    };

export function turnEventForTurn(
  turn: Turn,
  hasAssistantOutput: boolean,
  hasMissingReply: boolean = false,
): TurnEventDisplay | undefined {
  // Soft outcome: turn completed but only produced commentary, no
  // `final_answer`. Rendered as a warning system event (not an error) and
  // checked first so a completed turn with commentary but no answer
  // does not get re-routed to the cancelled / failed branches below.
  // The flag is computed by the caller from `AssistantTurnDisplay`,
  // which has the full item list and is the only place that already
  // does this classification.
  if (hasMissingReply) {
    return {
      kind: "missing_final_answer",
      source: "turn",
      presentation: "notice",
      notice: userFacingErrorForMissingReply(),
    };
  }
  if (turn.kind === "compact" && hasContextCompactionOutcome(turn)) {
    return undefined;
  }
  const rawMessage = turn.error?.message || latestTurnItemError(turn);
  const baseDisplay =
    turn.status === "interrupted"
      ? userFacingErrorForMessage("context canceled", "turn")
      : isCancellationMessage((rawMessage ?? "").toLowerCase())
        ? userFacingErrorForMessage(rawMessage, "turn")
        // For a failed turn, prefer the structured `turn.error` populated
        // by the Go core's BuildTurnError so the renderer can derive one
        // accurate user-facing label while keeping diagnostics internal.
        // The legacy string fallback in userFacingErrorForMessage still
        // works for older app-servers that only send the message.
        : turn.status === "failed"
          ? userFacingErrorForMessage(turn.error, "turn")
          : undefined;
  if (!baseDisplay) {
    return undefined;
  }

  const notice =
    baseDisplay.category === "cancelled"
      ? {
          ...baseDisplay,
          title: hasAssistantOutput ? "回复已中断" : "已停止",
          detail: hasAssistantOutput
            ? "已保留已生成内容，可以继续发送消息。"
            : "这次请求已停止，没有生成回复内容。",
        }
      : {
          ...baseDisplay,
          detail: hasAssistantOutput
            ? `${baseDisplay.detail} 已保留已生成内容。`
            : baseDisplay.detail,
        };

  return {
    kind: turnEventKindForNotice(notice),
    source: "turn",
    presentation: "notice",
    notice,
  };
}

export function turnEventForItem(item: ThreadItem): TurnEventDisplay | undefined {
  if (item.type === "context_compaction") {
    return {
      kind: contextCompactionKind(item.text, item.status),
      source: "item",
      presentation: "context_compaction",
      text: item.text,
      reason: item.reason,
      status: item.status,
    };
  }
  if (item.type === "error") {
    const notice = userFacingErrorForMessage(item.error ?? "", "turn");
    return {
      kind: turnEventKindForNotice(notice),
      source: "item",
      presentation: "notice",
      notice,
    };
  }
  return undefined;
}

function latestTurnItemError(turn: Turn): string | undefined {
  for (let i = turn.items.length - 1; i >= 0; i--) {
    const item = turn.items[i];
    if (item.type === "error") {
      const error = item.error?.trim();
      if (error) {
        return error;
      }
    }
  }
  return undefined;
}

function turnEventKindForNotice(display: UserFacingErrorDisplay): TurnEventKind {
  switch (display.category) {
    case "cancelled":
      return "user_stopped";
    case "network":
      return "network_lost";
    case "auth":
      return "auth_required";
    case "provider":
      return "provider_failed";
    case "tool":
      return "tool_failed";
    case "local":
      return "local_failed";
    case "internal":
    default:
      return "internal_error";
  }
}

function contextCompactionKind(
  _text: string | undefined,
  status: ThreadItemStatus | undefined,
): "context_compacting" | "context_compacted" | "context_compaction_failed" {
  if (status === "in_progress") {
    return "context_compacting";
  }
  if (status === "failed") {
    return "context_compaction_failed";
  }
  return "context_compacted";
}

function hasContextCompactionOutcome(turn: Turn): boolean {
  return turn.items.some(
    (item) =>
      item.type === "context_compaction" &&
      (item.status === "completed" || item.status === "failed"),
  );
}
