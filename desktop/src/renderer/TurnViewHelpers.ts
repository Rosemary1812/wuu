import type { Thread, ThreadItem, Turn } from "../shared/protocol";
import { isAgentHandoffItem } from "./AgentHandoff";
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

export type UserMessageAnchor = {
  turnID: string;
  itemID: string;
};

// Walks a thread's or a single turn's turns/items in order and returns
// the anchor of the earliest user_message. Sidebar rows pass a full
// Thread; the conversation turn rail passes a single Turn. For a
// Turn, the function treats it as a single-element sequence so the
// rail can scroll straight to that turn's first prompt.
// Returns undefined when there is no user_message yet (defensive
// against malformed fixtures); callers should treat that as "skip
// the scroll".
export function firstUserMessageAnchor(
  source: Thread | Turn | undefined,
): UserMessageAnchor | undefined {
  if (!source) {
    return undefined;
  }
  // Thread has `turns: Turn[]`; Turn has `items: ThreadItem[]`
  // directly. The `in` narrowing picks the right field without a
  // full type-guard.
  const turns: Turn[] =
    "turns" in source ? source.turns ?? [] : [source as Turn];
  for (const turn of turns) {
    for (const item of turn.items ?? []) {
      if (item.type === "user_message" && !isAgentHandoffItem(item)) {
        return { turnID: turn.id, itemID: item.id };
      }
    }
  }
  return undefined;
}

// Mirrors `firstUserMessageAnchor` but walks the same turns/items in
// reverse. Used by the fork picker to detect when the user clicked
// the "分叉" button on the latest user message (no choice dialog
// needed) versus any older user message (the picker asks whether to
// fork into a new worktree or stay local).
export function lastUserMessageAnchor(
  source: Thread | Turn | undefined,
): UserMessageAnchor | undefined {
  if (!source) {
    return undefined;
  }
  const turns: Turn[] =
    "turns" in source ? source.turns ?? [] : [source as Turn];
  for (let turnIndex = turns.length - 1; turnIndex >= 0; turnIndex -= 1) {
    const turn = turns[turnIndex];
    const items = turn.items ?? [];
    for (let itemIndex = items.length - 1; itemIndex >= 0; itemIndex -= 1) {
      const item = items[itemIndex];
      if (item.type === "user_message" && !isAgentHandoffItem(item)) {
        return { turnID: turn.id, itemID: item.id };
      }
    }
  }
  return undefined;
}

export type ThreadReplySnippet = {
  text: string;
  totalAgentMessages: number;
};

// Builds a short preview of the thread's agent replies for sidebar hover
// popovers. Skips agent_message items with no committed text (still-active
// streams that haven't produced visible output yet). Returns undefined when
// there is nothing readable to show — callers fall back to the thread's
// own `preview` field in that case.
export function threadReplySnippet(
  thread: Thread | undefined,
): ThreadReplySnippet | undefined {
  if (!thread) {
    return undefined;
  }
  let firstReply: string | undefined;
  let total = 0;
  for (const turn of thread.turns ?? []) {
    for (const item of turn.items ?? []) {
      if (item.type !== "agent_message") {
        continue;
      }
      const trimmed = (item.text ?? "").trim();
      if (!trimmed) {
        continue;
      }
      total += 1;
      if (firstReply === undefined) {
        firstReply = trimmed;
      }
    }
  }
  if (!firstReply) {
    return undefined;
  }
  return {
    text: firstReply,
    totalAgentMessages: total,
  };
}

// Per-turn variant of threadReplySnippet. The conversation turn rail shows
// one horizontal bar per turn on the left side of the message stream;
// hovering a bar must surface *that turn's* reply, not the whole thread's
// first reply. Same empty-text skipping rules as the thread-level helper.
export function turnReplySnippet(
  turn: Turn | undefined,
): ThreadReplySnippet | undefined {
  if (!turn) {
    return undefined;
  }
  let firstReply: string | undefined;
  let total = 0;
  for (const item of turn.items ?? []) {
    if (item.type !== "agent_message") {
      continue;
    }
    const trimmed = (item.text ?? "").trim();
    if (!trimmed) {
      continue;
    }
    total += 1;
    if (firstReply === undefined) {
      firstReply = trimmed;
    }
  }
  if (!firstReply) {
    return undefined;
  }
  return {
    text: firstReply,
    totalAgentMessages: total,
  };
}

// Per-turn variant for the first user message text. Used in the
// conversation turn rail's hover preview to show what the user
// asked for alongside the first agent reply. Same empty-text
// skipping rules as turnReplySnippet.
export function firstUserMessageText(
  turn: Turn | undefined,
): string | undefined {
  if (!turn) {
    return undefined;
  }
  for (const item of turn.items ?? []) {
    if (item.type !== "user_message") {
      continue;
    }
    // Gate first on the item-level signal so corrupted payload text never
    // reaches the trim path.
    if (isAgentHandoffItem(item)) {
      continue;
    }
    const trimmed = (item.text ?? "").trim();
    if (!trimmed) {
      continue;
    }
    return trimmed;
  }
  return undefined;
}

// Caps the preview body to ~140 characters on a single line. The Claude.ai
// reference card uses three short lines, but the wuu sidebar is narrow
// (326px) and the title already sits above the popover — a tighter single
// line keeps the popover visually compact and avoids duplicating context
// the title already conveys.
const THREAD_REPLY_PREVIEW_MAX_CHARS = 140;

export function truncateReplyPreview(text: string): string {
  const normalized = text.replace(/\s+/g, " ").trim();
  if (normalized.length <= THREAD_REPLY_PREVIEW_MAX_CHARS) {
    return normalized;
  }
  return `${normalized.slice(0, THREAD_REPLY_PREVIEW_MAX_CHARS - 1).trimEnd()}…`;
}

// Compact "X 分钟前" / "X 小时前" formatter for sidebar hover previews.
// Kept here (not in ThreadSidebar) because the preview now lives inside
// the conversation pane, not the sidebar DOM. The cadence mirrors the
// relative-time labels already used elsewhere in the sidebar; we do not
// pull in a date library because the input is always an ISO string from
// the server.
export function formatRelativeTime(iso: string | undefined): string {
  if (!iso) {
    return "";
  }
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) {
    return "";
  }
  const diffMs = Date.now() - then;
  const minutes = Math.round(diffMs / 60_000);
  if (minutes < 1) {
    return "刚刚";
  }
  if (minutes < 60) {
    return `${minutes} 分钟前`;
  }
  const hours = Math.round(minutes / 60);
  if (hours < 24) {
    return `${hours} 小时前`;
  }
  const days = Math.round(hours / 24);
  return `${days} 天前`;
}

const JUMP_HIGHLIGHT_CLASS = "user-message-jump-flash";
const JUMP_HIGHLIGHT_DURATION_MS = 1600;
// Try, then retry. The first attempt usually wins, but split conversations
// (and just-mounted threads) can take a frame or two to mount the anchor.
const JUMP_RETRY_DELAYS_MS: readonly number[] = [0, 80, 200];
// Padding above the target so the previous turn header stays in view
// and the message doesn't slam into the scroll container's top edge.
const JUMP_TOP_OFFSET_PX = 64;

function flashJumpTarget(node: HTMLElement): void {
  node.classList.remove(JUMP_HIGHLIGHT_CLASS);
  // Force reflow so re-adding the class restarts the animation even when
  // the user clicks two entries back-to-back.
  void node.offsetWidth;
  node.classList.add(JUMP_HIGHLIGHT_CLASS);
  window.setTimeout(() => {
    node.classList.remove(JUMP_HIGHLIGHT_CLASS);
  }, JUMP_HIGHLIGHT_DURATION_MS);
}

function findScrollContainer(start: HTMLElement): HTMLElement | null {
  // Walk up looking for the conversation pane's scroll surface. We probe
  // both the single-pane (.scroll-region) and split-pane containers so
  // the same helper works whether split mode is active or not.
  let parent: HTMLElement | null = start.parentElement;
  while (parent) {
    if (
      parent.classList.contains("scroll-region") ||
      parent.classList.contains("conversation-split-body")
    ) {
      return parent;
    }
    parent = parent.parentElement;
  }
  return null;
}

function scrollAnchorIntoContainer(
  node: HTMLElement,
  container: HTMLElement,
): void {
  const containerRect = container.getBoundingClientRect();
  const nodeRect = node.getBoundingClientRect();
  const currentOffset =
    nodeRect.top - containerRect.top + container.scrollTop;
  const targetTop = Math.max(
    0,
    Math.min(
      currentOffset - JUMP_TOP_OFFSET_PX,
      container.scrollHeight - container.clientHeight,
    ),
  );
  if (Math.abs(targetTop - container.scrollTop) < 2) {
    return;
  }
  const prefersReducedMotion =
    typeof window !== "undefined" &&
    window.matchMedia?.("(prefers-reduced-motion: reduce)").matches === true;
  container.scrollTo({
    top: targetTop,
    behavior: prefersReducedMotion ? "auto" : "smooth",
  });
}

function attemptJump(turnID: string, itemID: string): boolean {
  if (typeof document === "undefined") {
    return false;
  }
  const node = document.querySelector<HTMLElement>(
    userMessageAnchorSelector(turnID, itemID),
  );
  if (!node) {
    return false;
  }
  // The .turn ancestor has `content-visibility: auto`, which lets the
  // browser skip layout and paint while the turn is off-screen. The
  // anchor is still in the DOM tree, so querySelector finds it, but
  // getBoundingClientRect below would return the placeholder size from
  // `contain-intrinsic-size` instead of the real coordinates. Reading
  // any layout property on a skipped subtree forces the browser to
  // compute the real layout, so the subsequent scroll math sees the
  // actual position. Without this, the first click on a query whose
  // turn is above the current scroll viewport either scrolls to the
  // wrong offset or bails out (targetTop ≈ currentScrollTop), and the
  // user has to scroll up manually before the second click works.
  void node.offsetWidth;
  const container = findScrollContainer(node);
  if (!container) {
    // Fallback: still flash the target so the user gets feedback, even
    // if we couldn't locate the scroll container for an exact offset.
    flashJumpTarget(node);
    return true;
  }
  scrollAnchorIntoContainer(node, container);
  flashJumpTarget(node);
  return true;
}

/**
 * Scroll the user message at `turnID`/`itemID` into the visible area of
 * the conversation scroll surface. Adds a short highlight pulse to the
 * target so the jump is unmistakable. Safe to call before the anchor is
 * mounted — retries a few frames before giving up.
 */
export function scrollToUserMessage(turnID: string, itemID: string): void {
  if (typeof window === "undefined") {
    return;
  }
  let attemptIndex = 0;
  const tryOnce = (): void => {
    if (attemptJump(turnID, itemID)) {
      return;
    }
    const nextDelay = JUMP_RETRY_DELAYS_MS[attemptIndex + 1];
    if (nextDelay === undefined) {
      return;
    }
    attemptIndex += 1;
    window.setTimeout(tryOnce, nextDelay);
  };
  tryOnce();
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
      hasFinalText ||
      (latestItem.phase === "final_answer" &&
        debugStreamFieldLength(turn.id, latestItem, "text") > 0);
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
