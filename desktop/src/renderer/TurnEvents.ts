import type { ThreadItem, ThreadItemStatus, Turn } from "../shared/protocol";
import {
  isCancellationMessage,
  userFacingErrorForMessage,
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
  | "recovered_from_context_overflow";

export type TurnEventSource = "turn" | "item";

export type TurnEventDisplay =
  | {
      kind: TurnEventKind;
      source: TurnEventSource;
      presentation: "notice";
      notice: UserFacingErrorDisplay;
    }
  | {
      kind: "context_compacting" | "context_compacted" | "recovered_from_context_overflow";
      source: "item";
      presentation: "context_compaction";
      text?: string;
      status?: ThreadItemStatus;
    };

export function turnEventForTurn(
  turn: Turn,
  hasAssistantOutput: boolean,
): TurnEventDisplay | undefined {
  const rawMessage = turn.error?.message;
  const baseDisplay =
    turn.status === "interrupted"
      ? userFacingErrorForMessage("context canceled", "turn")
      : isCancellationMessage((rawMessage ?? "").toLowerCase())
        ? userFacingErrorForMessage(rawMessage, "turn")
        : turn.status === "failed"
          ? userFacingErrorForMessage(rawMessage, "turn")
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
  text: string | undefined,
  status: ThreadItemStatus | undefined,
): "context_compacting" | "context_compacted" | "recovered_from_context_overflow" {
  if (status === "in_progress") {
    return "context_compacting";
  }
  if (/recovered from context overflow/i.test(text ?? "")) {
    return "recovered_from_context_overflow";
  }
  return "context_compacted";
}
