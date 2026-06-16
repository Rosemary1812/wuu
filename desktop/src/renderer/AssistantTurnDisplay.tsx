import { type JSX } from "react";
import type { ThreadItem, Turn } from "../shared/protocol";
import { streamFieldValue } from "./ThreadItemText";
import { ToolActivityTimeline } from "./ToolActivity";
import { readableToolActivityCommand } from "./ToolActivityHelpers";

export type AssistantTurnDisplay = {
  /**
   * Front content: the process lane. Carries reasoning, tool calls,
   * context_compaction, pending assistant text, and commentary in the
   * order they appeared in the turn. The shell renders these entries as
   * process records, not as answer prose.
   */
  frontEntries: TurnProcessEntry[];
  /**
   * Body: the user-facing reply. Multiple final-answer text segments are
   * rendered in arrival order as one reply surface instead of becoming a
   * user-facing warning state.
   */
  finalAnswerItems: AssistantTurnAnswerItem[];
  /**
   * Present when a completed turn produced process text but no final
   * answer. This is a user-facing outcome, not an internal bug label.
   */
  missingReplyMessage?: string;
  /**
   * True when a completed turn has final-answer body text. The shell draws
   * a thin horizontal divider between the front and the body in that case.
   */
  showDivider: boolean;
  /**
   * Initial collapse state of the front content. The user can always
   * toggle the front afterwards, so this is purely a hint about the
   * default render.
   */
  frontDefaultCollapsed: boolean;
  /**
   * Latest process record shown in the fold header while the turn is
   * live. Usually pending/commentary text, but it can also be the latest
   * tool action when no newer text has arrived yet.
   */
  latestProcessPreview?: TurnProcessPreview;
};

export type AssistantTurnAnswerItem = {
  item: ThreadItem;
  streaming: boolean;
  element: JSX.Element;
  /**
   * True when the turn has a reasoning block that the model just finished
   * writing. The first text item waits a short beat so the reasoning cursor
   * can fully settle before its own cursor starts animating.
   */
  pendingCompanionReasoning?: boolean;
};

export type TurnProcessEntry = {
  key: string;
  element: JSX.Element;
  count?: number;
  kind: TurnProcessEntryKind;
};

export type TurnProcessEntryKind =
  | "pending"
  | "commentary"
  | "activity"
  | "process";

export type TurnProcessPreview = {
  text: string;
  kind: TurnProcessEntryKind;
};

export function buildAssistantTurnDisplay(
  turn: Turn,
  _actionableAgentMessageID: string | undefined,
  renderThreadItem: (
    item: ThreadItem,
    streaming: boolean,
    pendingCompanionReasoning?: boolean,
  ) => JSX.Element | null,
): AssistantTurnDisplay | undefined {
  const frontEntries: TurnProcessEntry[] = [];
  const finalAnswerItems: AssistantTurnAnswerItem[] = [];
  let sawAssistantWork = false;
  const turnHasReasoning = turn.items.some((item) => item.type === "reasoning");
  const isInProgress = turn.status === "in_progress";
  let firstTextItemRendered = false;

  function appendFrontEntry(entry: TurnProcessEntry | null): void {
    if (entry) {
      frontEntries.push(entry);
    }
  }

  for (let index = 0; index < turn.items.length; index++) {
    const item = turn.items[index];
    if (item.type === "user_message") {
      continue;
    }
    sawAssistantWork = true;

    if (item.type === "agent_message") {
      const streaming =
        turn.status === "in_progress" && item.status === "in_progress";
      const text = streamFieldValue(turn.id, item, "text");
      if (text.trim().length === 0 && !streaming) {
        continue;
      }
      const isFinalAnswer = item.phase === "final_answer";
      const isPendingText = isPendingAgentText(item, isInProgress);
      const shouldDelayCursor = turnHasReasoning && !firstTextItemRendered;
      firstTextItemRendered = true;
      const rendered = renderThreadItem(
        item,
        streaming,
        shouldDelayCursor ? true : undefined,
      );
      if (isFinalAnswer) {
        if (!rendered) {
          continue;
        }
        finalAnswerItems.push({ item, streaming, element: rendered });
      } else if (rendered) {
        appendFrontEntry({
          key: item.id,
          element: rendered,
          kind: isPendingText ? "pending" : "commentary",
        });
      }
      continue;
    }

    if (item.type === "tool_call" || item.type === "collab_agent_tool_call") {
      const group = [item];
      let nextIndex = index + 1;
      while (
        nextIndex < turn.items.length &&
        (turn.items[nextIndex].type === "tool_call" ||
          turn.items[nextIndex].type === "collab_agent_tool_call")
      ) {
        group.push(turn.items[nextIndex]);
        nextIndex++;
      }
      appendFrontEntry({
        key: `${item.id}-activity`,
        element: (
          <ToolActivityTimeline
            key={`${item.id}-activity`}
            items={group}
            collapseWhenIdle={agentMessageWithTextFollows(turn, nextIndex - 1)}
            revealItems={turn.status === "in_progress"}
          />
        ),
        count: group.length,
        kind: "activity",
      });
      index = nextIndex - 1;
      continue;
    }

    const rendered = renderThreadItem(
      item,
      turn.status === "in_progress" && item.status === "in_progress",
    );
    if (rendered) {
      appendFrontEntry({
        key: item.id,
        element: rendered,
        kind: "process",
      });
    }
  }

  if (!sawAssistantWork) {
    return undefined;
  }

  const finalCount = finalAnswerItems.length;
  const isCompleted = turn.status === "completed";

  let missingReplyMessage: string | undefined;
  if (isCompleted && finalAnswerItems.length === 0) {
    const hasCommentary = turn.items.some(
      (item) => item.type === "agent_message" && item.phase === "commentary",
    );
    if (hasCommentary) {
      missingReplyMessage = "这轮只保留了过程记录，没有生成最终回答。";
    }
  }

  const showDivider = isCompleted && finalCount > 0 && !missingReplyMessage;
  const frontDefaultCollapsed = finalCount > 0;

  const latestProcessPreview = isInProgress
    ? latestInProgressProcessPreview(turn)
    : undefined;

  if (
    frontEntries.length === 0 &&
    finalAnswerItems.length === 0 &&
    !latestProcessPreview
  ) {
    return undefined;
  }

  return {
    frontEntries,
    finalAnswerItems,
    missingReplyMessage,
    showDivider,
    frontDefaultCollapsed,
    latestProcessPreview,
  };
}

function latestInProgressProcessPreview(
  turn: Turn,
): TurnProcessPreview | undefined {
  for (let index = turn.items.length - 1; index >= 0; index--) {
    const preview = processPreviewForItem(turn, turn.items[index]);
    if (preview) {
      return preview;
    }
  }
  return undefined;
}

function processPreviewForItem(
  turn: Turn,
  item: ThreadItem,
): TurnProcessPreview | undefined {
  if (item.type === "agent_message") {
    if (
      item.phase !== "pending" &&
      item.phase !== "commentary" &&
      !isPendingAgentText(item, turn.status === "in_progress")
    ) {
      return undefined;
    }
    const text = compactProcessPreview(
      streamFieldValue(turn.id, item, "text"),
    );
    return text
      ? {
          text,
          kind:
            item.phase === "pending" ||
            isPendingAgentText(item, turn.status === "in_progress")
              ? "pending"
              : "commentary",
        }
      : undefined;
  }

  if (item.type === "tool_call" || item.type === "collab_agent_tool_call") {
    const text = compactProcessPreview(readableToolActivityCommand(item));
    return text ? { text, kind: "activity" } : undefined;
  }

  return undefined;
}

function isPendingAgentText(item: ThreadItem, turnInProgress: boolean): boolean {
  return (
    item.type === "agent_message" &&
    (item.phase === "pending" ||
      (!item.phase && turnInProgress && item.status === "in_progress"))
  );
}

function compactProcessPreview(raw: string | undefined): string | undefined {
  const text = raw?.replace(/\s+/g, " ").trim();
  if (!text) {
    return undefined;
  }
  const previewMax = 120;
  return text.length > previewMax ? `${text.slice(0, previewMax)}…` : text;
}

function agentMessageWithTextFollows(turn: Turn, itemIndex: number): boolean {
  for (let index = itemIndex + 1; index < turn.items.length; index++) {
    const item = turn.items[index];
    if (item.type === "user_message") {
      return false;
    }
    if (item.type !== "agent_message") {
      continue;
    }
    if (streamFieldValue(turn.id, item, "text").trim().length === 0) {
      continue;
    }
    return true;
  }
  return false;
}
