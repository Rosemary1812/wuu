import type { ThreadItem, Turn } from "../shared/protocol";
import {
  messageFlowFinalTextIndex,
  messageFlowStatusLabel,
} from "./message-flow-display";
import { debugStreamFieldLength, latestDebugItem } from "./RunDebugPanel";
import { streamFieldValue } from "./ThreadItemText";

type TurnProgressContent = {
  label: string;
  detail?: string;
};

// Anchor IDs used by the input-box query history popover to scroll
// back to a past user message. Kept as plain DOM ids (no hash routing
// involvement) so document.getElementById / scrollIntoView stay cheap.
export function turnAnchorID(turnID: string): string {
  return `turn-${turnID}`;
}

export function userMessageAnchorID(turnID: string, itemID: string): string {
  return `user-msg-${turnID}-${itemID}`;
}

function userMessageAnchorSelector(turnID: string, itemID: string): string {
  return `#${userMessageAnchorID(turnID, itemID)}`;
}

export function scrollToUserMessage(turnID: string, itemID: string): void {
  if (typeof document === "undefined") {
    return;
  }
  const node = document.querySelector<HTMLElement>(
    userMessageAnchorSelector(turnID, itemID),
  );
  if (!node) {
    return;
  }
  node.scrollIntoView({ behavior: "smooth", block: "start" });
}

export function turnHasAssistantOutput(turn: Turn): boolean {
  return turn.items.some((item) => {
    if (item.type !== "agent_message") {
      return false;
    }
    return streamFieldValue(turn.id, item, "text").trim().length > 0;
  });
}

export function turnProgressContent(
  turn: Turn,
  elapsedMs: number,
  hasFinalText: boolean,
): TurnProgressContent {
  if (turn.status === "interrupted") {
    return { label: "已停止", detail: "这次请求已停止" };
  }
  if (turn.status !== "in_progress") {
    return {
      label: messageFlowStatusLabel({
        done: true,
        failed: turn.status === "failed",
        hasFinalText,
        locale: "zh",
      }),
    };
  }

  const runningTool = turn.items.find(
    (item) =>
      (item.type === "tool_call" || item.type === "collab_agent_tool_call") &&
      (item.status ?? "in_progress") === "in_progress",
  );
  if (runningTool) {
    return {
      label: messageFlowStatusLabel({
        done: false,
        failed: false,
        hasFinalText,
        locale: "zh",
      }),
    };
  }

  const latestItem = latestDebugItem(turn);
  if (!latestItem) {
    return {
      label: messageFlowStatusLabel({
        done: false,
        failed: false,
        hasFinalText,
        locale: "zh",
      }),
      detail: waitingDetail(elapsedMs, "已收到请求，正在等待模型回应"),
    };
  }
  if (latestItem.type === "agent_message") {
    const hasText =
      hasFinalText || debugStreamFieldLength(turn.id, latestItem, "text") > 0;
    return {
      label: messageFlowStatusLabel({
        done: false,
        failed: false,
        hasFinalText: hasText,
        locale: "zh",
      }),
      detail: hasText ? undefined : waitingDetail(elapsedMs, "正在组织回答"),
    };
  }
  if (latestItem.type === "reasoning") {
    return {
      label: messageFlowStatusLabel({
        done: false,
        failed: false,
        hasFinalText,
        locale: "zh",
      }),
      detail: waitingDetail(elapsedMs, "正在组织回答"),
    };
  }
  if (
    latestItem.type === "tool_call" ||
    latestItem.type === "collab_agent_tool_call"
  ) {
    return {
      label: messageFlowStatusLabel({
        done: false,
        failed: false,
        hasFinalText,
        locale: "zh",
      }),
    };
  }
  if (latestItem.type === "context_compaction") {
    return {
      label: messageFlowStatusLabel({
        done: false,
        failed: false,
        hasFinalText,
        locale: "zh",
      }),
    };
  }
  if (latestItem.type === "error") {
    return {
      label: messageFlowStatusLabel({
        done: false,
        failed: false,
        hasFinalText,
        locale: "zh",
      }),
    };
  }

  return {
    label: messageFlowStatusLabel({
      done: false,
      failed: false,
      hasFinalText,
      locale: "zh",
    }),
    detail: waitingDetail(elapsedMs, "请求正在处理中"),
  };
}

function waitingDetail(elapsedMs: number, defaultDetail: string): string {
  if (elapsedMs >= 30_000) {
    return "这个请求比平常更久，仍在等待响应";
  }
  if (elapsedMs >= 8_000) {
    return "请求已开始，正在继续处理";
  }
  return defaultDetail;
}

export function latestAgentMessageItemID(turns: Turn[]): string | undefined {
  for (let turnIndex = turns.length - 1; turnIndex >= 0; turnIndex--) {
    const itemID = latestAgentMessageItemIDForTurn(turns[turnIndex]);
    if (itemID) {
      return itemID;
    }
  }
  return undefined;
}

function latestAgentMessageItemIDForTurn(turn: Turn): string | undefined {
  for (let itemIndex = turn.items.length - 1; itemIndex >= 0; itemIndex--) {
    const item = turn.items[itemIndex];
    if (item.type === "agent_message") {
      return item.id;
    }
  }
  return undefined;
}

export function messageFlowAgentMessageItemID(
  turn: Turn,
): string | undefined {
  const explicitFinalID = explicitFinalAgentMessageItemID(turn);
  if (explicitFinalID) {
    return explicitFinalID;
  }

  const finalIndex = messageFlowFinalTextIndex(turn.items, (item) => {
    if (item.type === "agent_message") {
      return streamFieldValue(turn.id, item, "text").trim().length > 0
        ? "text"
        : "ignore";
    }
    if (
      item.type === "reasoning" ||
      item.type === "tool_call" ||
      item.type === "collab_agent_tool_call" ||
      item.type === "context_compaction"
    ) {
      return "process";
    }
    return "ignore";
  });

  return finalIndex >= 0 ? turn.items[finalIndex]?.id : undefined;
}

function explicitFinalAgentMessageItemID(turn: Turn): string | undefined {
  for (let itemIndex = turn.items.length - 1; itemIndex >= 0; itemIndex--) {
    const item = turn.items[itemIndex];
    if (item.type !== "agent_message" || item.phase !== "final_answer") {
      continue;
    }
    if (streamFieldValue(turn.id, item, "text").trim().length > 0) {
      return item.id;
    }
  }
  return undefined;
}
