import { type JSX } from "react";
import type { ThreadItem, Turn } from "../shared/protocol";
import { streamFieldValue } from "./ThreadItemText";
import { readableToolActivityCommand } from "./ToolActivityHelpers";

/**
 * The single, ordered list of items that make up an assistant turn.
 *
 * The shell renders this list top-to-bottom inside two visual groups
 * (`process` fold + `answer` body) but the data itself is one
 * chronological sequence — the old `frontEntries` / `finalAnswerItems`
 * split let the same item drift between arrays as `phase` flipped,
 * which is exactly the transition race that produced the "commentary
 * appeared in the final slot" bug.
 *
 * `settled` is derived from `item.status`, not from `phase`:
 *   - `settled = item.status !== "in_progress"`
 *   - the back end marks the item complete when the entire message
 *     (including all deltas) has been committed, so `settled` flips
 *     atomically the moment streaming is over.
 *
 * `position` only describes which visual group the entry belongs to,
 * not whether it is "active" — that's `settled`.
 */
export type TurnEntry = {
  /** Stable React key. Distinct from `item.id` only when several items
   *  share an id (e.g. consecutive tool calls collapsed into one group). */
  key: string;
  item: ThreadItem;
  items?: ThreadItem[];
  position: "process" | "answer";
  settled: boolean;
  streaming: boolean;
  kind: TurnEntryKind;
  count?: number;
};

export type TurnEntryKind =
  | "commentary"
  | "answer"
  | "activity"
  | "process"
  | "process_group";

export type AssistantTurnDisplay = {
  entries: TurnEntry[];
  /** True when the turn has at least one `answer`-position entry. The shell
   *  uses this to collapse earlier process records once answer text begins. */
  hasAnswer: boolean;
  /** Present when a completed turn produced process text but no final
   *  answer. This is a user-facing outcome, not an internal bug label. */
  missingReplyMessage?: string;
  /** Latest process record shown in the fold header while the turn is
   *  live. Usually commentary text, but it can also be the latest tool
   *  action when no newer text has arrived yet. */
  latestProcessPreview?: TurnProcessPreview;
};

export type TurnProcessPreview = {
  text: string;
  kind: TurnEntryKind;
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
  const entries: TurnEntry[] = [];
  // Defensive dedupe. The back-end's `upsertItemLocked` already de-dupes
  // by item id, but a partial resync could in principle leave the list
  // with two of the same id; we never want to render an item twice.
  const seenItemIDs = new Set<string>();
  let sawAssistantWork = false;
  let hasAnswer = false;
  const isInProgress = turn.status === "in_progress";
  const turnHasReasoning = turn.items.some((item) => item.type === "reasoning");
  let firstTextItemRendered = false;

  function isProcessItemLive(item: ThreadItem): boolean {
    return isInProgress && item.status === "in_progress";
  }

  function entryPosition(item: ThreadItem): "process" | "answer" {
    // Per the message-display policy: only a confirmed `final_answer`
    // moves into the answer region. Empty / unparseable phase is
    // treated as `unknown` (rule 5) and stays in process so it never
    // collapses the process fold mid-stream (rule 7). The back-end
    // settles the phase after streaming, so unknown items either
    // upgrade to final_answer (which will re-enter here on the next
    // render) or stay as commentary.
    if (item.type === "agent_message" && item.phase === "final_answer") {
      return "answer";
    }
    return "process";
  }

  function entryKind(item: ThreadItem): TurnEntryKind {
    if (item.type === "agent_message") {
      if (item.phase === "final_answer") return "answer";
      // commentary covers both explicit `phase: "commentary"` items
      // and `phase`-less in-flight agent text (rule 6: unknown is
      // surfaced inline alongside commentary).
      return "commentary";
    }
    if (item.type === "tool_call" || item.type === "collab_agent_tool_call") {
      return "activity";
    }
    return "process";
  }

  for (let index = 0; index < turn.items.length; index++) {
    const item = turn.items[index];
    if (item.type === "user_message") continue;
    if (seenItemIDs.has(item.id)) continue;
    seenItemIDs.add(item.id);

    sawAssistantWork = true;

    // Empty agent_message items carry no value once the turn is no longer
    // in flight; drop them so the process fold doesn't show ghost rows.
    if (item.type === "agent_message") {
      const streaming = isProcessItemLive(item);
      const text = streamFieldValue(turn.id, item, "text");
      if (text.trim().length === 0 && !streaming) continue;
      const shouldDelayCursor = turnHasReasoning && !firstTextItemRendered;
      firstTextItemRendered = true;
      const rendered = renderThreadItem(
        item,
        streaming,
        shouldDelayCursor ? true : undefined,
      );
      if (!rendered) continue;
      const position = entryPosition(item);
      if (position === "answer") hasAnswer = true;
      entries.push({
        key: item.id,
        item,
        position,
        settled: !streaming,
        streaming,
        kind: entryKind(item),
      });
      continue;
    }

    if (item.type === "tool_call" || item.type === "collab_agent_tool_call") {
      const group: ThreadItem[] = [item];
      let nextIndex = index + 1;
      while (
        nextIndex < turn.items.length &&
        (turn.items[nextIndex].type === "tool_call" ||
          turn.items[nextIndex].type === "collab_agent_tool_call")
      ) {
        group.push(turn.items[nextIndex]);
        nextIndex++;
      }
      const allSettled = group.every((g) => g.status !== "in_progress");
      entries.push({
        key: `${item.id}-activity`,
        item,
        items: group,
        position: "process",
        settled: allSettled,
        streaming: isInProgress && !allSettled,
        kind: "activity",
        count: group.length,
      });
      index = nextIndex - 1;
      continue;
    }

    // reasoning, error, context_compaction, ...
    const streaming = isProcessItemLive(item);
    const rendered = renderThreadItem(item, streaming);
    if (!rendered) continue;
    entries.push({
      key: item.id,
      item,
      position: "process",
      settled: !streaming,
      streaming,
      kind: entryKind(item),
    });
  }

  // For an in_progress turn we always return a display so TurnView keeps
  // the AssistantTurnShell (process row + live elapsed timer) mounted
  // even when the turn has no items yet — both the optimistic placeholder
  // before the server's first item and the brief window between the real
  // turn arriving and the first agent_message delta would otherwise
  // flicker through an undefined display.
  if (!sawAssistantWork && !isInProgress) {
    return undefined;
  }

  const isCompleted = turn.status === "completed";
  let missingReplyMessage: string | undefined;
  if (isCompleted && !hasAnswer) {
    const hasCommentary = turn.items.some(
      (item) => item.type === "agent_message" && item.phase === "commentary",
    );
    if (hasCommentary) {
      missingReplyMessage = "这轮只保留了过程记录，没有生成最终回答。";
    }
  }

  const latestProcessPreview = isInProgress
    ? latestInProgressProcessPreview(turn)
    : undefined;

  if (entries.length === 0 && !latestProcessPreview && !isInProgress) {
    return undefined;
  }

  return {
    entries: groupProcessEntries(entries),
    hasAnswer,
    missingReplyMessage,
    latestProcessPreview,
  };
}

function groupProcessEntries(entries: TurnEntry[]): TurnEntry[] {
  const grouped: TurnEntry[] = [];
  let pending: TurnEntry[] = [];

  const flushPending = (): void => {
    if (pending.length === 0) {
      return;
    }
    const items = pending.flatMap((entry) => entry.items ?? [entry.item]);
    const hasActivity = pending.some((entry) => entry.kind === "activity");
    const shouldGroup =
      items.length > 1 && (hasActivity || pending.length > 1);
    if (!shouldGroup) {
      grouped.push(...pending);
      pending = [];
      return;
    }
    const first = pending[0];
    grouped.push({
      key: first.key,
      item: first.item,
      items,
      position: "process",
      settled: pending.every((entry) => entry.settled),
      streaming: pending.some((entry) => entry.streaming),
      kind: "process_group",
      count: items.length,
    });
    pending = [];
  };

  for (const entry of entries) {
    if (isProcessGroupCandidate(entry)) {
      pending.push(entry);
      continue;
    }
    flushPending();
    grouped.push(entry);
  }
  flushPending();
  return grouped;
}

function isProcessGroupCandidate(entry: TurnEntry): boolean {
  if (entry.position !== "process") {
    return false;
  }
  if (entry.kind === "activity") {
    return true;
  }
  return entry.item.type === "reasoning";
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
    if (item.phase !== "commentary") {
      return undefined;
    }
    // The fold-header preview is a committed snapshot, not a live
    // mirror of the streaming store. The body's StreamingMarkdown is
    // the single live surface; if the preview also advanced, the user
    // would see two surfaces incrementing in lockstep, which reads as
    // a duplicated or racing stream rather than one typing assistant.
    // Read item.text (last settled value) so the preview either shows
    // a previously committed topic or stays empty until the stream
    // settles and item.text is populated.
    const text = compactProcessPreview(item.text ?? "");
    return text
      ? {
          text,
          kind: "commentary",
        }
      : undefined;
  }

  if (item.type === "tool_call" || item.type === "collab_agent_tool_call") {
    const text = compactProcessPreview(readableToolActivityCommand(item));
    return text ? { text, kind: "activity" } : undefined;
  }

  return undefined;
}

function compactProcessPreview(raw: string | undefined): string | undefined {
  const text = raw?.replace(/\s+/g, " ").trim();
  if (!text) {
    return undefined;
  }
  const previewMax = 120;
  return text.length > previewMax ? `${text.slice(0, previewMax)}…` : text;
}
