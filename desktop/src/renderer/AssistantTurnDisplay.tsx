import { type JSX } from "react";
import type { ThreadItem, Turn } from "../shared/protocol";
import { streamFieldValue } from "./ThreadItemText";
import { ToolActivityTimeline } from "./ToolActivity";

export type AssistantTurnDisplay = {
  /**
   * Front content: the "process" lane. Carries reasoning, tool calls,
   * context_compaction, and commentary in the order they appeared in
   * the turn. Commentary renders here as a normal text block because
   * the two are structurally distinct (front vs body).
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
   * Text of the latest commentary `agent_message` in the front content,
   * truncated to a single short line for the fold header preview.
   */
  latestCommentaryPreview?: string;
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
      const shouldDelayCursor = turnHasReasoning && !firstTextItemRendered;
      firstTextItemRendered = true;
      const rendered = renderThreadItem(
        item,
        streaming,
        shouldDelayCursor ? true : undefined,
      );
      if (!rendered) {
        continue;
      }
      if (isFinalAnswer) {
        finalAnswerItems.push({ item, streaming, element: rendered });
      } else {
        appendFrontEntry({
          key: item.id,
          element: rendered,
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
      });
    }
  }

  if (!sawAssistantWork) {
    return undefined;
  }
  if (frontEntries.length === 0 && finalAnswerItems.length === 0) {
    return undefined;
  }

  const finalCount = finalAnswerItems.length;
  const isInProgress = turn.status === "in_progress";
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
  const frontDefaultCollapsed = true;

  let latestCommentaryPreview: string | undefined;
  if (isInProgress) {
    let latestCommentaryItem: ThreadItem | undefined;
    for (const entry of frontEntries) {
      if (entry.key.endsWith("-activity")) {
        continue;
      }
      const item = turn.items.find((candidate) => candidate.id === entry.key);
      if (
        item &&
        item.type === "agent_message" &&
        item.phase === "commentary"
      ) {
        latestCommentaryItem = item;
      }
    }
    if (latestCommentaryItem) {
      const raw = streamFieldValue(turn.id, latestCommentaryItem, "text").trim();
      if (raw.length > 0) {
        const previewMax = 120;
        latestCommentaryPreview =
          raw.length > previewMax ? `${raw.slice(0, previewMax)}…` : raw;
      }
    }
  }

  return {
    frontEntries,
    finalAnswerItems,
    missingReplyMessage,
    showDivider,
    frontDefaultCollapsed,
    latestCommentaryPreview,
  };
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
