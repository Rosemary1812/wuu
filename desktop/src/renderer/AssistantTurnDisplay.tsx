import { type JSX } from "react";
import type { ThreadItem, Turn } from "../shared/protocol";
import {
  agentHandoffChipDisplayItems,
  isAgentHandoffItem,
  type SubagentChipDisplay,
} from "./AgentHandoff";
import { streamFieldValue } from "./ThreadItemText";
import { readableToolActivityCommand } from "./ToolActivityHelpers";
import { isCancellationMessage } from "./UserFacingErrors";
import { translateCurrent } from "./i18n";

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
  /** A subagent wake notification rendered as a wait-tool-style timeline row. */
  subagentStatus?: SubagentChipDisplay;
  /** Origin turn when entries from several turns are merged into one
   *  group display (TurnGroupView). Undefined means "the shell's own
   *  turn" — single-turn displays never set it, so stream-text keys,
   *  fork targets and turn status all resolve to the owning turn either
   *  way. */
  turn?: Turn;
};

export type TurnEntryKind =
  | "commentary"
  | "answer"
  | "activity"
  | "process"
  | "process_group"
  | "subagent_status";

export type AssistantTurnDisplay = {
  entries: TurnEntry[];
  /** Every subagent notification collected across the turn. The shell
   *  surfaces them once in the fold header, next to the elapsed-time
   *  label: the wake event is the turn's cause, so it lives in the
   *  turn-level chrome instead of occupying a row of its own in the
   *  message stream. */
  subagentChips: SubagentChipDisplay[];
  /** True when the turn has at least one `answer`-position entry. The shell
   *  separately checks that its streamed text is non-empty before starting
   *  the process-to-answer handoff. */
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
  renderThreadItem?: (
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
  // Turn-level chip collection: every subagent notification in the turn
  // accumulates here and surfaces once in the fold header (see
  // AssistantTurnDisplay.subagentChips). Order is chronological.
  const collectedChips: SubagentChipDisplay[] = [];

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
    if (seenItemIDs.has(item.id)) continue;
    seenItemIDs.add(item.id);
    if (item.type === "user_message") {
      const chips = isAgentHandoffItem(item)
        ? agentHandoffChipDisplayItems(item)
        : [];
      if (chips.length > 0) {
        sawAssistantWork = true;
        collectedChips.push(...chips);
        for (let chipIndex = 0; chipIndex < chips.length; chipIndex += 1) {
          entries.push({
            key: `${item.id}-subagent-status-${chipIndex}`,
            item,
            position: "process",
            settled: true,
            streaming: false,
            kind: "subagent_status",
            subagentStatus: chips[chipIndex],
          });
        }
      }
      continue;
    }

    sawAssistantWork = true;

    // An agent_message with no visible text is only a transport placeholder.
    // Never turn it into a layout entry: providers can leave more than one
    // live placeholder between tool calls, and every empty entry would still
    // contribute the process list's vertical gap, producing a large blank
    // block before the next visible activity row. The in-progress turn keeps
    // the shell and timer mounted on its own; once text reaches the stream
    // store, the next render inserts the real entry.
    if (item.type === "agent_message") {
      const streaming = isProcessItemLive(item);
      const text = streamFieldValue(turn.id, item, "text");
      if (text.trim().length === 0) continue;
      const shouldDelayCursor = turnHasReasoning && !firstTextItemRendered;
      firstTextItemRendered = true;
      if (
        renderThreadItem &&
        !renderThreadItem(
          item,
          streaming,
          shouldDelayCursor ? true : undefined,
        )
      ) {
        continue;
      }
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
      const spawnBatch = item.name === "spawn_agent";
      let nextIndex = index + 1;
      while (
        nextIndex < turn.items.length &&
        (turn.items[nextIndex].type === "tool_call" ||
          turn.items[nextIndex].type === "collab_agent_tool_call") &&
        (turn.items[nextIndex].name === "spawn_agent") === spawnBatch
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

    if (shouldSuppressProcessErrorItem(turn, item)) {
      continue;
    }

    // reasoning, error, context_compaction, ...
    const streaming = isProcessItemLive(item);
    if (renderThreadItem && !renderThreadItem(item, streaming)) continue;
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
      missingReplyMessage = translateCurrent("turn.missingFinalAnswer");
    }
  }

  const latestProcessPreview = isInProgress
    ? latestInProgressProcessPreview(turn)
    : undefined;

  if (
    entries.length === 0 &&
    collectedChips.length === 0 &&
    !latestProcessPreview &&
    !isInProgress
  ) {
    return undefined;
  }

  return {
    entries: groupProcessEntries(entries),
    hasAnswer,
    subagentChips: collectedChips,
    missingReplyMessage,
    latestProcessPreview,
  };
}

function shouldSuppressProcessErrorItem(turn: Turn, item: ThreadItem): boolean {
  if (item.type !== "error") {
    return false;
  }
  if (turn.status === "failed") {
    return true;
  }
  // A stream error item only lands while a retryable attempt has just failed
  // terminally. If the turn is still running, the reconnect chip already
  // names the cause; if it completed, the retry recovered — in both cases a
  // settled error line would either pile up per attempt or stay behind as
  // stale noise after a successful recovery.
  if (turn.status === "in_progress" || turn.status === "completed") {
    return true;
  }
  // The turn-level interruption notice already explains user-initiated stops.
  // Rendering the matching cancellation error item here would show the same
  // stop twice in one turn.
  return (
    turn.status === "interrupted" &&
    isCancellationMessage((item.error ?? "").toLowerCase())
  );
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
    // A background subagent keeps one stable tool-call row so later wake
    // notifications can settle that exact row in place. Folding it into an
    // adjacent operation batch would erase the identity needed for pairing.
    if (entry.item.name === "spawn_agent") {
      return false;
    }
    return true;
  }
  return entry.item.type === "reasoning";
}

function latestInProgressProcessPreview(
  turn: Turn,
): TurnProcessPreview | undefined {
  for (let index = turn.items.length - 1; index >= 0; index--) {
    const preview = processPreviewForItem(turn.items[index]);
    if (preview) {
      return preview;
    }
  }
  return undefined;
}

function processPreviewForItem(
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
