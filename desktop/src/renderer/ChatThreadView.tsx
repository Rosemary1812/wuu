import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";
import { ChevronDown, ChevronUp, Reply, SmilePlus, X } from "lucide-react";
import type {
  ConversationSubthread,
  EnvelopeMeta,
  FocusMeta,
  ParticipantSummary,
  ThreadItem,
  Turn,
} from "../shared/protocol";
import {
  chatMessagesFromTurns,
  replyCountBadge,
  type ChatMessageRow,
  type TurnStreamStatus,
} from "./AppState";
import { selectionIntersectsNode } from "./AutoFollowScroll";
import type { QueuedComposerMessage } from "./ComposerMessages";
import { DefaultAvatarMark } from "./DefaultAvatar";
import { reactionArt } from "./MessageReactionArt";
import { EnvelopeNotice } from "./EnvelopeNotice";
import {
  collapsedLongTextPreview,
  useLongTextCollapse,
} from "./LongTextCollapse";
import { MessageFileList, MessageImageGrid } from "./MessageActions";
import { MessageReactions } from "./MessageReactions";
import {
  REACTION_EMOJI,
  REACTION_KEYS,
  reactionLabel,
  reactionGlyph,
  ringModel,
  type MessageMarksView,
} from "./MessageMarks";
import { ReadReceiptRing } from "./ReadReceiptRing";
import { RichContent } from "./RichContent";
import {
  StreamReconnectNotice,
  SystemEventDivider,
  TurnEventNotice,
} from "./TurnNotice";
import type { TurnEventDisplay } from "./TurnEvents";
import { translateCurrent as translate, useI18n } from "./i18n";

// Distance (px) from the bottom of the scroll container within which the
// view still counts as "at the bottom" and should auto-follow new rows.
const AUTO_FOLLOW_THRESHOLD_PX = 120;

// Chat-view windowing (mirrors a WeChat/Slack-style opening: land on the
// latest messages, reveal older ones a batch at a time as the reader
// scrolls up). Exported so tests can assert against the exact thresholds
// instead of duplicating the magic numbers.
export const INITIAL_CHAT_WINDOW_ROWS = 80;
export const CHAT_WINDOW_ROW_BATCH = 80;

function taskStateLabel(state: string | undefined): string {
  const key = state?.trim() ?? "";
  switch (key) {
    case "planning": return translate("chat.task.planning");
    case "executing":
    case "running": return translate("chat.task.executing");
    case "awaiting_lead": return translate("chat.task.awaitingLead");
    case "blocked": return translate("chat.task.blocked");
    case "needs_human": return translate("chat.task.needsHuman");
    case "paused": return translate("chat.task.paused");
    case "completed": return translate("chat.task.completed");
    case "failed": return translate("chat.task.failed");
    default: return key || translate("chat.task.ready");
  }
}

/**
 * Walk up from `start` to find the nearest scrollable ancestor — the
 * first element whose computed `overflow-y` is `auto`/`scroll` and whose
 * content actually overflows (`scrollHeight > clientHeight`). The chat
 * view's own `.chat-thread` container is deliberately not a scroll
 * container (it grows with its content); the real scroll
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
  cwd,
  pendingMessages = [],
  busyParticipantIDs,
  marksBySeq,
  readerCount = 0,
  resolveParticipantName,
  threadOwnerCandidates = [],
  subthreadsByAnchor,
  isActive = true,
  streamStatus,
  turnEvents = [],
  onOpenSubthread,
  onReact,
}: {
  turns: ReadonlyArray<Pick<Turn, "id" | "items">>;
  cwd?: string;
  /**
   * Messages the user has sent that have not yet landed in the thread's
   * turn history — turn/queue entries awaiting drain while the agent is
   * mid-turn. Chat send semantics (issue #10): they render as normal user
   * bubbles with a subtle "发送中" hint instead of a queue strip, and are
   * removed by the existing reconciliation (turn/started with queue_id /
   * item/completed with source_id) once the real turn arrives.
   */
  pendingMessages?: ReadonlyArray<QueuedComposerMessage>;
  busyParticipantIDs?: ReadonlySet<string>;
  /**
   * Aggregated read receipts + reactions for this thread, keyed by message
   * seq. Fed from thread/marks on load and patched by message/mark
   * notifications (2026-07-04-read-receipts-and-reactions.md). Absent = the
   * feature is off / not a chat thread, so no rings or chips render.
   */
  marksBySeq?: ReadonlyMap<number, MessageMarksView>;
  /** How many members a message is broadcast to — the ring's denominator. */
  readerCount?: number;
  resolveParticipantName?: (id: string) => string;
  /** Named group members eligible to own a Thread started from a human post. */
  threadOwnerCandidates?: ReadonlyArray<ParticipantSummary>;
  /**
   * Reply subthreads (群中群) anchored on this thread's messages, keyed by
   * anchor_item_id (== the anchored ThreadItem.id). A message with an entry
   * gets a reply affordance under its bubble: a "N 条回复" badge for a plain
   * discussion reply, or the shared task 活动卡/result 摘要 once the reply has
   * been (人点击)升级为 task。Absent = subthreads not loaded / not a group thread.
   */
  subthreadsByAnchor?: ReadonlyMap<string, ConversationSubthread>;
  isActive?: boolean;
  streamStatus?: TurnStreamStatus;
  turnEvents?: ReadonlyArray<{ turnID: string; event: TurnEventDisplay }>;
  /**
   * Open the reply/subthread panel for a message. Wired to the same
   * create-or-find-by-anchor path the agent-brain transcript uses. Optional:
   * when absent the reply badge / task card renders read-only (无点击入口)。
   */
  onOpenSubthread?: (
    item: ThreadItem,
    threadOwnerParticipantID?: string,
    existingSubthreadID?: string,
  ) => void;
  /**
   * Stamp an emoji reaction on a message (人点击, one-click from the bubble's
   * hover toolbar). Wired to the message/react RPC. Optional: when absent the
   * 贴表情 reaction row is omitted from the toolbar.
   */
  onReact?: (item: ThreadItem, reaction: string) => void;
}): JSX.Element {
  const rows = useMemo(() => {
    const messageRows = chatMessagesFromTurns(turns);
    if (turnEvents.length === 0) {
      return messageRows;
    }
    const eventsByTurn = new Map(turnEvents.map(({ turnID, event }) => [turnID, event]));
    const merged: Array<
      | ChatMessageRow
      | {
          kind: "turn-event";
          id: string;
          turnID: string;
          event: TurnEventDisplay;
          count?: number;
        }
    > = [];
    let messageIndex = 0;
    for (const turn of turns) {
      while (messageRows[messageIndex]?.turnID === turn.id) {
        merged.push(messageRows[messageIndex]!);
        messageIndex += 1;
      }
      const event = eventsByTurn.get(turn.id);
      if (event) {
        const previous = merged[merged.length - 1];
        if (
          previous?.kind === "turn-event" &&
          JSON.stringify(previous.event) === JSON.stringify(event)
        ) {
          previous.count = (previous.count ?? 1) + 1;
        } else {
          merged.push({
            kind: "turn-event",
            id: `${turn.id}:turn-event`,
            turnID: turn.id,
            event,
          });
        }
      }
    }
    merged.push(...messageRows.slice(messageIndex));
    return merged;
  }, [turnEvents, turns]);
  // 贴表情 and 开 Thread are one-click affordances on each group bubble's toolbar
  // (ChatBubbleToolbar) — no right-click menu, no hoisted popup state. A bubble
  // inside a cth reply panel receives onReact but not onOpenSubthread, so its
  // toolbar shows only the reaction row: "一层不嵌套" — no Thread on a message
  // already inside a cth — is enforced at the view level (the reply-panel caller
  // never wires the reply handler).
  const containerRef = useRef<HTMLDivElement | null>(null);
  const topSentinelRef = useRef<HTMLDivElement | null>(null);
  const autoFollowRef = useRef(true);
  const selectionPausedAutoFollowRef = useRef(false);
  const autoFollowParentRef = useRef<HTMLElement | null>(null);
  const autoFollowListenerRef = useRef<(() => void) | null>(null);
  const [ownerSelectionItem, setOwnerSelectionItem] = useState<ThreadItem>();
  const namedOwnerCandidates = useMemo(
    () =>
      threadOwnerCandidates.filter(
        (member) => member.kind === "named" && member.id.trim() !== "",
      ),
    [threadOwnerCandidates],
  );
  const requestOpenSubthread = useCallback(
    (item: ThreadItem, suggestedOwnerID?: string): void => {
      if (!onOpenSubthread) {
        return;
      }
      const existing =
        subthreadsByAnchor?.get(item.id) ??
        (item.seq ? subthreadsByAnchor?.get(`seq:${item.seq}`) : undefined);
      const ownerID =
        existing?.thread_owner_participant_id?.trim() ||
        suggestedOwnerID?.trim() ||
        "";
      if (existing || item.task?.subthread_id) {
        onOpenSubthread(item, ownerID || undefined, existing?.id);
        return;
      }
      if (ownerID) {
        onOpenSubthread(item, ownerID);
        return;
      }
      if (namedOwnerCandidates.length === 1) {
        onOpenSubthread(item, namedOwnerCandidates[0]!.id);
        return;
      }
      setOwnerSelectionItem(item);
    },
    [namedOwnerCandidates, onOpenSubthread, subthreadsByAnchor],
  );
  const closeOwnerSelection = useCallback(() => {
    setOwnerSelectionItem(undefined);
  }, []);
  const autoFollowVersion = [
    ...rows.map((row) =>
      row.kind === "turn-event"
        ? `${row.id}:${row.event.kind}:${row.event.presentation}`
        : row.id,
    ),
    ...pendingMessages.map((message) => `pending:${message.id}`),
    streamStatus?.liveProgress ? `stream:${streamStatus.text}` : "",
  ].join("\u0000");

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

  useLayoutEffect(() => {
    const detachAutoFollow = (): void => {
      if (autoFollowParentRef.current && autoFollowListenerRef.current) {
        autoFollowParentRef.current.removeEventListener(
          "scroll",
          autoFollowListenerRef.current,
        );
      }
      autoFollowParentRef.current = null;
      autoFollowListenerRef.current = null;
    };
    if (!isActive) {
      detachAutoFollow();
      selectionPausedAutoFollowRef.current = false;
      return;
    }
    const scrollParent = findScrollParent(containerRef.current);
    if (!scrollParent) {
      detachAutoFollow();
      return;
    }
    if (autoFollowParentRef.current !== scrollParent) {
      detachAutoFollow();
      const updateAutoFollow = (): void => {
        if (selectionIntersectsNode(document.getSelection(), scrollParent)) {
          selectionPausedAutoFollowRef.current = true;
          autoFollowRef.current = false;
          return;
        }
        selectionPausedAutoFollowRef.current = false;
        autoFollowRef.current =
          scrollParent.scrollHeight - scrollParent.scrollTop - scrollParent.clientHeight <=
          AUTO_FOLLOW_THRESHOLD_PX;
      };
      scrollParent.addEventListener("scroll", updateAutoFollow, { passive: true });
      autoFollowParentRef.current = scrollParent;
      autoFollowListenerRef.current = updateAutoFollow;
    }
    if (selectionIntersectsNode(document.getSelection(), scrollParent)) {
      selectionPausedAutoFollowRef.current = true;
      autoFollowRef.current = false;
    }
    if (autoFollowRef.current) {
      scrollParent.scrollTop = scrollParent.scrollHeight;
    }
    if (!selectionPausedAutoFollowRef.current) {
      autoFollowListenerRef.current?.();
    }
    // Identity/state changes, rather than only row count, also cover replacing
    // a reconnect notice with a terminal event in the same render slot.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [autoFollowVersion, isActive]);

  useEffect(
    () => () => {
      if (autoFollowParentRef.current && autoFollowListenerRef.current) {
        autoFollowParentRef.current.removeEventListener(
          "scroll",
          autoFollowListenerRef.current,
        );
      }
    },
    [],
  );

  const visibleRows = hiddenOlderCount > 0 ? rows.slice(hiddenOlderCount) : rows;
  const firstVisibleBubbleRowID = visibleRows.find(
    (row) =>
      row.kind === "user" ||
      (row.kind === "participant" && (row.item.post_kind ?? "result") !== "decline"),
  )?.id;

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
        row.kind === "turn-event" ? (
          <div key={row.id} className="chat-row chat-row--system chat-row--turn-event">
            <TurnEventNotice
              event={
                row.count && row.count > 1 && row.event.presentation === "notice"
                  ? {
                      ...row.event,
                      notice: {
                        ...row.event.notice,
                        title: `${row.event.notice.title} ×${row.count}`,
                      },
                    }
                  : row.event
              }
            />
          </div>
        ) : (
          <ChatRow
            key={row.id}
            isTopBubble={row.id === firstVisibleBubbleRowID}
            row={row}
            cwd={cwd}
            busyParticipantIDs={busyParticipantIDs}
            marks={
              row.kind !== "envelope" && typeof row.item.seq === "number"
                ? marksBySeq?.get(row.item.seq)
                : undefined
            }
            readerCount={readerCount}
            resolveParticipantName={resolveParticipantName}
            subthread={
              row.kind === "user" || row.kind === "participant"
                ? subthreadsByAnchor?.get(row.item.id) ??
                  (row.item.seq
                    ? subthreadsByAnchor?.get(`seq:${row.item.seq}`)
                    : undefined)
                : undefined
            }
            onOpenSubthread={
              onOpenSubthread ? requestOpenSubthread : undefined
            }
            onReact={onReact}
          />
        )
      ))}
      {pendingMessages.map((message) => (
        <PendingChatRow key={`pending-${message.id}`} message={message} cwd={cwd} />
      ))}
      {streamStatus?.liveProgress ? (
        <div className="chat-row chat-row--system chat-row--reconnecting">
          <StreamReconnectNotice text={streamStatus.text} />
        </div>
      ) : null}
      {ownerSelectionItem ? (
        <ThreadOwnerDialog
          candidates={namedOwnerCandidates}
          onClose={closeOwnerSelection}
          onSelect={(ownerID) => {
            const item = ownerSelectionItem;
            setOwnerSelectionItem(undefined);
            onOpenSubthread?.(item, ownerID);
          }}
        />
      ) : null}
    </div>
  );
}

/**
 * Hover toolbar floating above a chat bubble (Slack/Discord 式). 贴表情 is a
 * single trigger that opens the sticker picker panel (the mascot faces are
 * near-identical at toolbar size, so picking happens in a panel that shows
 * each sticker large with its label — 微信式); 开 Thread is a one-click button
 * next to it and the sole entry into a subthread on the group stream.
 * There is deliberately no ⋯ overflow: the two affordances are the whole
 * toolbar. Renders nothing when neither reaction nor reply is wired (a
 * read-only reuse of this view, e.g. inside a cth reply panel that omits
 * onOpenSubthread — 一层不嵌套).
 *
 * The reveal is CSS-driven off `.chat-bubble:hover/:focus-within` (chat.css);
 * this component only renders the buttons, always present in the DOM so
 * keyboard focus can reach them and the reveal is purely presentational.
 * While the picker is open the toolbar pins itself visible (.picker-open).
 */
function ChatBubbleToolbar({
  item,
  onReact,
  onReply,
}: {
  item: ThreadItem;
  onReact?: (item: ThreadItem, reaction: string) => void;
  onReply?: (item: ThreadItem) => void;
}): JSX.Element | null {
  const { t } = useI18n();
  const [pickerOpen, setPickerOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!pickerOpen) {
      return;
    }
    const onPointerDown = (event: PointerEvent): void => {
      if (!rootRef.current?.contains(event.target as Node)) {
        setPickerOpen(false);
      }
    };
    const onKeyDown = (event: KeyboardEvent): void => {
      if (event.key === "Escape") {
        setPickerOpen(false);
      }
    };
    document.addEventListener("pointerdown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [pickerOpen]);

  if (!onReact && !onReply) {
    return null;
  }
  return (
    <div
      ref={rootRef}
      className={`chat-bubble-toolbar${pickerOpen ? " picker-open" : ""}`}
      role="toolbar"
      aria-label={t("chat.messageActions")}
      data-testid="chat-bubble-toolbar"
    >
      {onReact ? (
        <button
          type="button"
          className="chat-bubble-toolbar-btn chat-bubble-toolbar-react"
          aria-label={t("chat.addReaction")}
          title={t("chat.addReaction")}
          aria-haspopup="true"
          aria-expanded={pickerOpen}
          onClick={() => setPickerOpen((open) => !open)}
        >
          <SmilePlus size={15} aria-hidden="true" />
        </button>
      ) : null}
      {onReact && pickerOpen ? (
        <div className="chat-reaction-picker" role="group" aria-label={t("chat.chooseReaction")}>
          {REACTION_KEYS.map((key) => (
            <button
              key={key}
              type="button"
              className="chat-reaction-picker-option"
              data-reaction={key}
              onClick={() => {
                onReact(item, key);
                setPickerOpen(false);
              }}
            >
              {reactionArt(key) ? (
                <img src={reactionArt(key)} alt="" draggable={false} />
              ) : (
                <span className="chat-reaction-picker-glyph" aria-hidden="true">
                  {REACTION_EMOJI[key] ?? reactionGlyph(key)}
                </span>
              )}
              <span className="chat-reaction-picker-label">
                {reactionLabel(key)}
              </span>
            </button>
          ))}
        </div>
      ) : null}
      {onReact && onReply ? (
        <span className="chat-bubble-toolbar-divider" aria-hidden="true" />
      ) : null}
      {onReply ? (
        <button
          type="button"
          className="chat-bubble-toolbar-btn chat-bubble-toolbar-reply"
          aria-label={t("chat.openThread")}
          title={t("chat.openThread")}
          onClick={() => onReply(item)}
        >
          <Reply size={14} aria-hidden="true" />
          <span>{t("chat.openThread")}</span>
        </button>
      ) : null}
    </div>
  );
}

function ThreadOwnerDialog({
  candidates,
  onSelect,
  onClose,
}: {
  candidates: ReadonlyArray<ParticipantSummary>;
  onSelect: (participantID: string) => void;
  onClose: () => void;
}): JSX.Element {
  const { t } = useI18n();
  const dialogRef = useRef<HTMLElement | null>(null);
  const firstOptionRef = useRef<HTMLButtonElement | null>(null);
  const closeButtonRef = useRef<HTMLButtonElement | null>(null);
  useEffect(() => {
    const previouslyFocused =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : undefined;
    (firstOptionRef.current ?? closeButtonRef.current)?.focus();
    const onKeyDown = (event: KeyboardEvent): void => {
      if (event.key === "Escape") {
        event.preventDefault();
        onClose();
        return;
      }
      if (event.key !== "Tab") {
        return;
      }
      const focusable = Array.from(
        dialogRef.current?.querySelectorAll<HTMLButtonElement>(
          "button:not(:disabled)",
        ) ?? [],
      );
      if (focusable.length === 0) {
        event.preventDefault();
        return;
      }
      const first = focusable[0]!;
      const last = focusable[focusable.length - 1]!;
      if (!dialogRef.current?.contains(document.activeElement)) {
        event.preventDefault();
        first.focus();
      } else if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("keydown", onKeyDown);
      previouslyFocused?.focus();
    };
  }, [onClose]);

  return createPortal(
    <div
      className="thread-owner-dialog-backdrop"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) {
          onClose();
        }
      }}
    >
      <section
        ref={dialogRef}
        className="thread-owner-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="thread-owner-dialog-title"
      >
        <header>
          <div>
            <h2 id="thread-owner-dialog-title">{t("chat.chooseThreadOwner")}</h2>
            <p>{t("chat.threadOwnerDescription")}</p>
          </div>
          <button
            ref={closeButtonRef}
            type="button"
            aria-label={t("chat.closeOwnerSelection")}
            onClick={onClose}
          >
            <X size={16} aria-hidden="true" />
          </button>
        </header>
        {candidates.length > 0 ? (
          <div className="thread-owner-dialog-options">
            {candidates.map((candidate) => (
              <button
                key={candidate.id}
                ref={candidate === candidates[0] ? firstOptionRef : undefined}
                type="button"
                onClick={() => onSelect(candidate.id)}
              >
                <DefaultAvatarMark seed={candidate.id} kind={candidate.kind} />
                <span>
                  <strong>{candidate.name || candidate.id}</strong>
                  {candidate.role ? <small>{candidate.role}</small> : null}
                </span>
              </button>
            ))}
          </div>
        ) : (
          <p className="thread-owner-dialog-empty">
            {t("chat.noOwnerCandidates")}
          </p>
        )}
      </section>
    </div>,
    document.body,
  );
}

/**
 * A user message that is sent from the composer but not yet part of the
 * turn history (delivery in flight while the agent is mid-turn). Renders
 * as a regular outgoing bubble so the chat never exposes queue mechanics;
 * only the dimmed style + "发送中" hint distinguish it until the real
 * user_message item replaces it.
 */
function PendingChatRow({
  message,
  cwd,
}: {
  message: QueuedComposerMessage;
  cwd?: string;
}): JSX.Element {
  const { t } = useI18n();
  return (
    <div className="chat-row chat-row--user chat-row--pending">
      <div className="chat-bubble-group">
        <div className="chat-bubble chat-bubble--user chat-bubble--pending">
          {message.images.length ? (
            <MessageImageGrid images={message.images} />
          ) : null}
          {message.files.length ? <MessageFileList files={message.files} /> : null}
          {message.text.trim() ? <RichContent text={message.text} cwd={cwd} /> : null}
        </div>
        <div className="chat-pending-hint">
          {message.held ? t("chat.held") : t("chat.sending")}
        </div>
      </div>
    </div>
  );
}

// focusDividerLabel renders the fixed glyph + name convention the
// desktop design settled on for the workspace-focus divider: a house
// glyph for the resident's personal space, a generic workspace glyph
// for everything else (both the "all workspaces" catch-all and any one
// named project), differentiated only by the trailing label.
function focusDividerLabel(meta: FocusMeta): string {
  if (meta.kind === "home") {
    return translate("runtime.personalChip");
  }
  if (meta.kind === "workspace") {
    const label = meta.name?.trim() || meta.root?.trim() || translate("chat.workspace");
    return `⬒ ${label}`;
  }
  return translate("chat.allWorkspacesDivider");
}

function ChatRow({
  isTopBubble,
  row,
  cwd,
  busyParticipantIDs,
  marks,
  readerCount = 0,
  resolveParticipantName,
  subthread,
  onOpenSubthread,
  onReact,
}: {
  isTopBubble?: boolean;
  row: ChatMessageRow;
  cwd?: string;
  busyParticipantIDs?: ReadonlySet<string>;
  marks?: MessageMarksView;
  readerCount?: number;
  resolveParticipantName?: (id: string) => string;
  subthread?: ConversationSubthread;
  onOpenSubthread?: (item: ThreadItem, suggestedOwnerID?: string) => void;
  /** Stamp a one-click reaction from the bubble's hover toolbar. Absent = no reactions. */
  onReact?: (item: ThreadItem, reaction: string) => void;
}): JSX.Element {
  const { t } = useI18n();
  const topBubbleClass = isTopBubble ? " chat-row--top-bubble" : "";
  // Long-text collapse state for the chat-style bubble — the same
  // threshold + preview estimator as `ThreadItemView`'s
  // `user-message-long-card`, so an 8 KB agent-notification dump that
  // lands in the chat stream folds to ~14 lines by default with a
  // "显示更多" toggle. System / focus / envelope rows don't carry a
  // message body, so the hook runs with an empty string —
  // `isCollapsibleLongText` bails out cheaply on that path. The hook
  // sits above every early return so its call order is stable across
  // renders regardless of which `row.kind` branch the user sees.
  const messageText =
    row.kind === "user" || row.kind === "participant"
      ? row.item.text ?? ""
      : "";
  const { collapsible, expanded, toggleExpanded } =
    useLongTextCollapse(messageText);
  const longCardClass = collapsible
    ? ` chat-bubble--long-card ${expanded ? "expanded" : "collapsed"}`
    : "";
  if (row.kind === "system") {
    const text = row.count && row.count > 1 ? `${row.text} ×${row.count}` : row.text;
    return (
      <div className={`chat-row chat-row--system${topBubbleClass}`}>
        <SystemEventDivider
          text={text}
          className="chat-system-divider"
        />
      </div>
    );
  }
  if (row.kind === "focus") {
    const meta = row.item.focus_meta;
    const label = meta ? focusDividerLabel(meta) : t("chat.allWorkspacesDivider");
    return (
      <div className={`chat-row chat-row--focus${topBubbleClass}`}>
        <div
          className="chat-inline-divider chat-focus-divider"
          role="separator"
          aria-label={label}
        >
          <span className="chat-inline-divider-label chat-focus-divider-label">
            {label}
          </span>
        </div>
      </div>
    );
  }
  if (row.kind === "envelope") {
    const meta = envelopeRowMeta(row);
    const text = envelopeRowText(row);
    return (
      <div className={`chat-row chat-row--envelope${topBubbleClass}`}>
        <EnvelopeNotice meta={meta} text={text} />
      </div>
    );
  }
  if (row.kind === "user") {
    return (
      <div className={`chat-row chat-row--user${topBubbleClass}`}>
        <div className="chat-bubble-group">
          <div className={`chat-bubble chat-bubble--user${longCardClass}`}>
            {row.item.images?.length ? (
              <MessageImageGrid images={row.item.images} />
            ) : null}
            {row.item.files?.length ? (
              <MessageFileList files={row.item.files} />
            ) : null}
            {collapsible ? (
              expanded ? (
                <RichContent text={messageText} cwd={cwd} />
              ) : (
                <div className="chat-bubble-raw-query">
                  {collapsedLongTextPreview(messageText)}
                </div>
              )
            ) : row.item.text ? (
              <RichContent text={row.item.text} cwd={cwd} />
            ) : null}
            {collapsible ? (
              <button
                type="button"
                className="chat-bubble-expand-toggle"
                aria-expanded={expanded}
                onClick={toggleExpanded}
              >
                <span>{expanded ? t("common.collapse") : t("common.showMore")}</span>
                {expanded ? (
                  <ChevronUp aria-hidden="true" />
                ) : (
                  <ChevronDown aria-hidden="true" />
                )}
              </button>
            ) : null}
            <ChatBubbleToolbar
              item={row.item}
              onReact={onReact}
              onReply={onOpenSubthread}
            />
          </div>
          {marks ? (
            <div className="chat-bubble-marks chat-bubble-marks--user">
              <MessageReactions
                reactions={marks.reactions}
                resolveName={resolveParticipantName}
              />
              <ReadReceiptRing
                ring={ringModel(marks.seen, readerCount)}
                seen={marks.seen}
                resolveName={resolveParticipantName}
              />
            </div>
          ) : null}
          <ReplyAffordance
            subthread={subthread}
            item={row.item}
            onOpenSubthread={onOpenSubthread}
            resolveParticipantName={resolveParticipantName}
          />
        </div>
      </div>
    );
  }
  // participant
  const postKind = row.item.post_kind ?? "result";
  const participant = row.item.participant;
  const name = participant?.name?.trim() || t("chat.participant");
  const participantStatus = participant?.id
    ? busyParticipantIDs?.has(participant.id)
      ? "busy"
      : "online"
    : undefined;
  if (postKind === "decline") {
    const text = (row.item.text ?? "").trim();
    return (
      <div className={`chat-row chat-row--decline${topBubbleClass}`}>
        <div className="chat-decline-line">
          {text
            ? t("chat.declinedWithReason", { name, reason: text })
            : t("chat.declined", { name })}
        </div>
      </div>
    );
  }
  return (
    <div className={`chat-row chat-row--participant${topBubbleClass}`}>
      <ChatAvatar participant={participant} status={participantStatus} />
      <div className="chat-bubble-group">
        <div className="chat-sender-name">{name}</div>
        <div className={`chat-bubble${longCardClass}`}>
          {collapsible ? (
            expanded ? (
              <RichContent text={messageText} cwd={cwd} />
            ) : (
              <div className="chat-bubble-raw-query">
                {collapsedLongTextPreview(messageText)}
              </div>
            )
          ) : row.item.text ? (
            <RichContent text={row.item.text} cwd={cwd} />
          ) : null}
          {collapsible ? (
            <button
              type="button"
              className="chat-bubble-expand-toggle"
              aria-expanded={expanded}
              onClick={toggleExpanded}
            >
              <span>{expanded ? t("common.collapse") : t("common.showMore")}</span>
              {expanded ? (
                <ChevronUp aria-hidden="true" />
              ) : (
                <ChevronDown aria-hidden="true" />
              )}
            </button>
          ) : null}
          <ChatBubbleToolbar
            item={row.item}
            onReact={onReact}
            onReply={
              onOpenSubthread
                ? (item) =>
                    onOpenSubthread(
                      item,
                      participant?.kind === "named" ? participant.id : undefined,
                    )
                : undefined
            }
          />
        </div>
        {marks && marks.reactions.length > 0 ? (
          <div className="chat-bubble-marks">
            <MessageReactions
              reactions={marks.reactions}
              resolveName={resolveParticipantName}
            />
          </div>
        ) : null}
        <ReplyAffordance
          subthread={subthread}
          item={row.item}
          onOpenSubthread={onOpenSubthread}
          resolveParticipantName={resolveParticipantName}
        />
      </div>
    </div>
  );
}

/**
 * Reply affordance hanging under a chat bubble (群中群折叠). Nothing renders
 * unless the message actually anchors a reply subthread. A plain discussion
 * reply shows a "N 条回复" badge (封顶 99 → 99+); once the reply is
 * (人点击)升级为 Task, the same slot shows a compact summary from that same
 * subthread workflow projection. Both open the Thread panel on click via the
 * message's anchor when onOpenSubthread is wired.
 */
function ReplyAffordance({
  subthread,
  item,
  onOpenSubthread,
  resolveParticipantName,
}: {
  subthread?: ConversationSubthread;
  item: ThreadItem;
  onOpenSubthread?: (item: ThreadItem, suggestedOwnerID?: string) => void;
  resolveParticipantName?: (id: string) => string;
}): JSX.Element | null {
  const { t } = useI18n();
  if (!subthread) {
    // No Thread yet: nothing hangs under the bubble. Starting one is the hover
    // toolbar's 开 Thread button — one entry, no divergence. This slot only
    // ever shows an existing Thread's folded summary.
    return null;
  }
  const open = onOpenSubthread ? () => onOpenSubthread(item) : undefined;
  if (subthread.task || subthread.status === "task" || subthread.exec_state) {
    const state = subthread.status === "resolved"
      ? t("chat.task.completed")
      : taskStateLabel(subthread.exec_state);
    const ownerID = subthread.thread_owner_participant_id?.trim() ?? "";
    const owner = ownerID
      ? resolveParticipantName?.(ownerID) || ownerID
      : t("chat.leadPending");
    const content = (
      <>
        <span className="chat-thread-summary-state">{state}</span>
        <span className="chat-thread-summary-title">
          {subthread.title?.trim() || subthread.task?.name?.trim() || "Task"}
        </span>
        <span className="chat-thread-summary-owner">Lead · {owner}</span>
      </>
    );
    return (
      <div className="chat-reply-task">
        {open ? (
          <button type="button" className="chat-thread-summary" onClick={open}>
            {content}
          </button>
        ) : (
          <div className="chat-thread-summary">{content}</div>
        )}
      </div>
    );
  }
  const count = subthread.reply_count ?? 0;
  const label = t(count === 1 ? "chat.replyCountOne" : "chat.replyCount", {
    count: replyCountBadge(count),
  });
  if (!open) {
    return <span className="chat-reply-badge">{label}</span>;
  }
  return (
    <button
      type="button"
      className="chat-reply-badge chat-reply-badge--button"
      onClick={open}
    >
      {label}
    </button>
  );
}

function envelopeRowMeta(
  row: Extract<ChatMessageRow, { kind: "envelope" }>,
): EnvelopeMeta {
  return row.items.flatMap((item) => item.envelope_meta ?? []);
}

function envelopeRowText(
  row: Extract<ChatMessageRow, { kind: "envelope" }>,
): string {
  return row.items
    .map((item) => item.text ?? "")
    .filter((text) => text.trim() !== "")
    .join("\n\n");
}

function ChatAvatar({
  participant,
  status,
}: {
  participant: ParticipantSummary | undefined;
  status?: "online" | "busy";
}): JSX.Element {
  const { t } = useI18n();
  const avatarImage = participant?.avatar_image?.trim();
  const name = participant?.name?.trim() || t("chat.participant");
  const statusLabel =
    status === "busy" ? t("chat.responding") : status === "online" ? t("chat.online") : "";
  return (
    <div
      className="chat-avatar"
      role={status ? "img" : undefined}
      aria-label={
        status ? t("chat.participantStatus", { name, status: statusLabel }) : undefined
      }
      aria-hidden={status ? undefined : true}
    >
      <span className="chat-avatar-face" aria-hidden="true">
        {avatarImage ? (
          <img src={avatarImage} alt="" />
        ) : (
          <DefaultAvatarMark seed={participant?.id || name} kind={participant?.kind} />
        )}
      </span>
      {status ? (
        <>
          <span className="chat-avatar-status" data-status={status} />
          <span className="chat-avatar-status-card" role="tooltip">
            <span className="chat-avatar-status-card-name">{name}</span>
            <span
              className="chat-avatar-status-card-state"
              data-status={status}
            >
              {statusLabel}
            </span>
          </span>
        </>
      ) : null}
    </div>
  );
}
