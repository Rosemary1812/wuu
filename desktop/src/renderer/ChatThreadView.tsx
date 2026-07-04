import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import type { ParticipantSummary, Turn } from "../shared/protocol";
import { chatMessagesFromTurns, type ChatMessageRow } from "./AppState";
import { EnvelopeNotice } from "./EnvelopeNotice";
import { RichContent } from "./RichContent";

// Distance (px) from the bottom of the scroll container within which the
// view still counts as "at the bottom" and should auto-follow new rows.
const AUTO_FOLLOW_THRESHOLD_PX = 120;

// Chat-view windowing (mirrors a WeChat/Slack-style opening: land on the
// latest messages, reveal older ones a batch at a time as the reader
// scrolls up). Exported so tests can assert against the exact thresholds
// instead of duplicating the magic numbers.
export const INITIAL_CHAT_WINDOW_ROWS = 80;
export const CHAT_WINDOW_ROW_BATCH = 80;

/**
 * Walk up from `start` to find the nearest scrollable ancestor — the
 * first element whose computed `overflow-y` is `auto`/`scroll` and whose
 * content actually overflows (`scrollHeight > clientHeight`). The chat
 * view's own `.chat-thread` container sets `overflow-y: auto` but is
 * never height-constrained (it grows with its content); the real scroll
 * surface is an ancestor — `.scroll-region` in the single-pane layout,
 * a split-pane body in split mode. Returns null when nothing scrolls,
 * which is always the case in jsdom (no layout, so every scrollHeight/
 * clientHeight reads as 0) — callers must treat that as "nothing to do"
 * rather than an error.
 */
export function findScrollParent(start: Element | null): HTMLElement | null {
  let node = start?.parentElement ?? null;
  while (node) {
    if (node instanceof HTMLElement) {
      const overflowY = window.getComputedStyle(node).overflowY;
      if (
        (overflowY === "auto" || overflowY === "scroll") &&
        node.scrollHeight > node.clientHeight
      ) {
        return node;
      }
    }
    node = node.parentElement;
  }
  return null;
}

/**
 * Chat-style message stream for DM and group threads
 * (chat-style-threads-design.md §2, §4). Renders exactly the whitelist
 * produced by chatMessagesFromTurns — user messages, envelope meta rows,
 * and tool-posted participant messages — never the agent's working
 * transcript (thinking, tool calls, plans, final-answer prose).
 *
 * The DM thread doubles as the resident agent's "brain" (every group
 * envelope turn is recorded into it too), so history can grow into the
 * thousands of rows. The backend already ships the full history to the
 * renderer on thread/resume, so this view windows the render instead:
 * it opens on the most recent `INITIAL_CHAT_WINDOW_ROWS` rows — like
 * opening a WeChat/Slack conversation lands on the latest messages —
 * and reveals another `CHAT_WINDOW_ROW_BATCH` rows each time the reader
 * scrolls up to the top of the currently-rendered window, until
 * everything is revealed. Nothing is ever dropped, only rendered later.
 * The window only grows at the bottom: a newly arriving message never
 * pushes an already-rendered older message back out of view — see the
 * render-time `hiddenOlderCount` adjustment below.
 *
 * Callers should mount one instance per thread (for example via
 * `key={threadID}`) so switching threads starts a fresh window instead
 * of carrying over the previous thread's reveal state.
 */
export function ChatThreadView({
  turns,
  typingParticipants,
}: {
  turns: ReadonlyArray<Pick<Turn, "id" | "items">>;
  typingParticipants: ReadonlyArray<ParticipantSummary>;
}): JSX.Element {
  const rows = useMemo(() => chatMessagesFromTurns(turns), [turns]);
  const containerRef = useRef<HTMLDivElement | null>(null);
  const topSentinelRef = useRef<HTMLDivElement | null>(null);
  const rowCount = rows.length + typingParticipants.length;

  // Count of the oldest rows currently withheld from the DOM. 0 means the
  // whole history is rendered (either it was never longer than the
  // initial window, or the reader has scrolled all the way up).
  const [hiddenOlderCount, setHiddenOlderCount] = useState(0);
  // Tracks the previous rows.length purely to detect *transitions*
  // (0 -> N on first resume payload, N -> M on later growth/shrink) so
  // the window can be (re)sized exactly once per transition. Adjusting
  // state during render (rather than in an effect) avoids a flash of
  // the full unwindowed history before the window snaps down.
  const [observedRowsLength, setObservedRowsLength] = useState<number | null>(
    null,
  );
  if (observedRowsLength === null) {
    if (rows.length > 0) {
      // First time this mount sees a non-empty rows array — thread/resume
      // is async, so this can happen well after mount, not just on it.
      setObservedRowsLength(rows.length);
      setHiddenOlderCount(Math.max(0, rows.length - INITIAL_CHAT_WINDOW_ROWS));
    }
    // Still nothing to show (resume pending) — leave the window at 0
    // until rows arrive.
  } else if (rows.length !== observedRowsLength) {
    setObservedRowsLength(rows.length);
    if (hiddenOlderCount >= rows.length && rows.length > 0) {
      // rows shrank below (or to exactly) the hidden count — the history
      // was reset or edited out from under the window, and keeping (or
      // merely clamping) the old hidden count would leave the visible
      // slice empty: a blank chat that only self-heals if the sentinel
      // happens to intersect on a later frame. Treat the shrink as a
      // history reset and reopen the window on the latest content, the
      // same way a fresh mount does.
      setHiddenOlderCount(Math.max(0, rows.length - INITIAL_CHAT_WINDOW_ROWS));
    }
    // rows grew: hiddenOlderCount is intentionally left untouched so the
    // new rows simply appear after the existing window instead of
    // sliding it forward.
  }

  // Manual scroll-position compensation for revealing older rows. The
  // reveal inserts content above the current viewport while the reader
  // is typically already at (or very near) scrollTop 0 — the one
  // position where the browser's native scroll anchoring does *not*
  // kick in — so without this the viewport would jump down visually
  // even though nothing the reader was looking at moved. `revealOlder`
  // captures the scroll parent's metrics synchronously before the state
  // update; the layout effect below applies the compensating delta
  // after the newly revealed rows are in the DOM but before the browser
  // paints.
  const pendingScrollAdjustRef = useRef<{
    scrollParent: HTMLElement;
    scrollHeight: number;
    scrollTop: number;
  } | null>(null);

  const revealOlder = useCallback(() => {
    const scrollParent = findScrollParent(containerRef.current);
    pendingScrollAdjustRef.current = scrollParent
      ? {
          scrollParent,
          scrollHeight: scrollParent.scrollHeight,
          scrollTop: scrollParent.scrollTop,
        }
      : null;
    setHiddenOlderCount((prev) => Math.max(0, prev - CHAT_WINDOW_ROW_BATCH));
  }, []);

  useLayoutEffect(() => {
    const pending = pendingScrollAdjustRef.current;
    if (!pending) {
      return;
    }
    pendingScrollAdjustRef.current = null;
    const { scrollParent, scrollHeight, scrollTop } = pending;
    scrollParent.scrollTop = scrollTop + (scrollParent.scrollHeight - scrollHeight);
  }, [hiddenOlderCount]);

  // Observe the sentinel above the windowed rows; when it scrolls into
  // view the reader has reached the top of what is currently rendered,
  // so reveal the next batch. Default root (the layout viewport) covers
  // both the single-pane `.scroll-region` and split-pane bodies without
  // this view needing to know which one it is inside.
  useEffect(() => {
    if (hiddenOlderCount <= 0) {
      return;
    }
    const node = topSentinelRef.current;
    if (!node || typeof IntersectionObserver === "undefined") {
      return;
    }
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) {
        revealOlder();
      }
    });
    observer.observe(node);
    return () => observer.disconnect();
  }, [hiddenOlderCount, revealOlder]);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) {
      return;
    }
    const distanceFromBottom =
      container.scrollHeight - container.scrollTop - container.clientHeight;
    if (distanceFromBottom <= AUTO_FOLLOW_THRESHOLD_PX) {
      container.scrollTop = container.scrollHeight;
    }
    // rowCount changes whenever a message or typing indicator is added or
    // removed; that is the only signal that should trigger auto-follow.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rowCount]);

  const visibleRows = hiddenOlderCount > 0 ? rows.slice(hiddenOlderCount) : rows;

  return (
    <div className="chat-thread" ref={containerRef}>
      {hiddenOlderCount > 0 ? (
        <div
          className="chat-window-sentinel"
          ref={topSentinelRef}
          aria-hidden="true"
        />
      ) : null}
      {visibleRows.map((row) => (
        <ChatRow key={row.id} row={row} />
      ))}
      {typingParticipants.map((participant) => (
        <ChatTypingRow key={`typing:${participant.id}`} participant={participant} />
      ))}
    </div>
  );
}

// focusDividerLabel renders the fixed glyph + name convention the
// desktop design settled on for the workspace-focus divider: a house
// glyph for the resident's personal space, a generic workspace glyph
// for everything else (both the "all workspaces" catch-all and any one
// named project), differentiated only by the trailing label.
function focusDividerLabel(meta: NonNullable<ChatMessageRow["item"]["focus_meta"]>): string {
  if (meta.kind === "home") {
    return "⌂ 个人";
  }
  if (meta.kind === "workspace") {
    const label = meta.name?.trim() || meta.root?.trim() || "工作区";
    return `⬒ ${label}`;
  }
  return "⬒ 全部工作区";
}

function ChatRow({ row }: { row: ChatMessageRow }): JSX.Element {
  if (row.kind === "focus") {
    const meta = row.item.focus_meta;
    const label = meta ? focusDividerLabel(meta) : "⬒ 全部工作区";
    return (
      <div className="chat-row chat-row--focus">
        <div className="chat-focus-divider" role="separator" aria-label={label}>
          <span className="chat-focus-divider-label">{label}</span>
        </div>
      </div>
    );
  }
  if (row.kind === "envelope") {
    return (
      <div className="chat-row chat-row--envelope">
        <EnvelopeNotice meta={row.item.envelope_meta ?? []} text={row.item.text ?? ""} />
      </div>
    );
  }
  if (row.kind === "user") {
    return (
      <div className="chat-row chat-row--user">
        <div className="chat-bubble-group">
          <div className="chat-bubble chat-bubble--user">
            {row.item.text ? (
              <RichContent text={row.item.text} />
            ) : null}
          </div>
        </div>
      </div>
    );
  }
  // participant
  const postKind = row.item.post_kind ?? "result";
  const participant = row.item.participant;
  const name = participant?.name?.trim() || "参与者";
  if (postKind === "decline") {
    const text = (row.item.text ?? "").trim();
    return (
      <div className="chat-row chat-row--decline">
        <div className="chat-decline-line">
          {name} 认为无需回应{text ? `：${text}` : ""}
        </div>
      </div>
    );
  }
  return (
    <div className="chat-row chat-row--participant">
      <ChatAvatar participant={participant} />
      <div className="chat-bubble-group">
        <div className="chat-sender-name">{name}</div>
        <div className="chat-bubble">
          {row.item.text ? <RichContent text={row.item.text} /> : null}
        </div>
      </div>
    </div>
  );
}

function ChatAvatar({
  participant,
}: {
  participant: ParticipantSummary | undefined;
}): JSX.Element {
  const avatarImage = participant?.avatar_image?.trim();
  const name = participant?.name?.trim() || "参与者";
  return (
    <div className="chat-avatar" aria-hidden="true">
      {avatarImage ? <img src={avatarImage} alt="" /> : name.charAt(0)}
    </div>
  );
}

function ChatTypingRow({
  participant,
}: {
  participant: ParticipantSummary;
}): JSX.Element {
  const name = participant.name.trim() || "参与者";
  return (
    <div className="chat-row chat-row--participant chat-typing-row" aria-label={`${name} 正在输入`}>
      <ChatAvatar participant={participant} />
      <div className="chat-bubble-group">
        <div className="chat-sender-name">{name}</div>
        <div className="chat-bubble chat-typing-bubble">
          <span className="chat-typing-dot" />
          <span className="chat-typing-dot" />
          <span className="chat-typing-dot" />
        </div>
      </div>
    </div>
  );
}
