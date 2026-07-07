import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { Reply, SmilePlus } from "lucide-react";
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
} from "./AppState";
import type { QueuedComposerMessage } from "./ComposerMessages";
import { DefaultAvatarMark } from "./DefaultAvatar";
import { reactionArt } from "./MessageReactionArt";
import { EnvelopeNotice } from "./EnvelopeNotice";
import { MessageReactions } from "./MessageReactions";
import {
  REACTION_EMOJI,
  REACTION_KEYS,
  REACTION_LABEL,
  reactionGlyph,
  ringModel,
  type MessageMarksView,
} from "./MessageMarks";
import { ReadReceiptRing } from "./ReadReceiptRing";
import { RichContent } from "./RichContent";
import { TaskCardItem } from "./ThreadItemView";

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
  pendingMessages = [],
  busyParticipantIDs,
  marksBySeq,
  readerCount = 0,
  resolveParticipantName,
  subthreadsByAnchor,
  onOpenSubthread,
  onReact,
}: {
  turns: ReadonlyArray<Pick<Turn, "id" | "items">>;
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
  /**
   * Reply subthreads (群中群) anchored on this thread's messages, keyed by
   * anchor_item_id (== the anchored ThreadItem.id). A message with an entry
   * gets a reply affordance under its bubble: a "N 条回复" badge for a plain
   * discussion reply, or the shared task 活动卡/result 摘要 once the reply has
   * been (人点击)升级为 task。Absent = subthreads not loaded / not a group thread.
   */
  subthreadsByAnchor?: ReadonlyMap<string, ConversationSubthread>;
  /**
   * Open the reply/subthread panel for a message. Wired to the same
   * create-or-find-by-anchor path the agent-brain transcript uses. Optional:
   * when absent the reply badge / task card renders read-only (无点击入口)。
   */
  onOpenSubthread?: (item: ThreadItem) => void;
  /**
   * Stamp an emoji reaction on a message (人点击, one-click from the bubble's
   * hover toolbar). Wired to the message/react RPC. Optional: when absent the
   * 贴表情 reaction row is omitted from the toolbar.
   */
  onReact?: (item: ThreadItem, reaction: string) => void;
}): JSX.Element {
  const rows = useMemo(() => chatMessagesFromTurns(turns), [turns]);
  // 贴表情 and 回复 are both one-click affordances on each bubble's hover toolbar
  // (ChatBubbleToolbar) — no right-click menu, no hoisted popup state. A bubble
  // inside a cth reply panel receives onReact but not onOpenSubthread, so its
  // toolbar shows only the reaction row: "一层不嵌套" — no reply on a message
  // already inside a cth — is enforced at the view level (the reply-panel caller
  // never wires the reply handler).
  const containerRef = useRef<HTMLDivElement | null>(null);
  const topSentinelRef = useRef<HTMLDivElement | null>(null);
  const rowCount = rows.length + pendingMessages.length;

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
    // rowCount changes whenever a real chat row is added or removed; that is
    // the only signal that should trigger auto-follow.
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
        <ChatRow
          key={row.id}
          row={row}
          busyParticipantIDs={busyParticipantIDs}
          marks={
            row.kind !== "envelope" && row.item.seq
              ? marksBySeq?.get(row.item.seq)
              : undefined
          }
          readerCount={readerCount}
          resolveParticipantName={resolveParticipantName}
          subthread={
            row.kind === "user" || row.kind === "participant"
              ? subthreadsByAnchor?.get(row.item.id)
              : undefined
          }
          onOpenSubthread={onOpenSubthread}
          onReact={onReact}
        />
      ))}
      {pendingMessages.map((message) => (
        <PendingChatRow key={`pending-${message.id}`} message={message} />
      ))}
    </div>
  );
}

/**
 * Hover toolbar floating above a chat bubble (Slack/Discord 式). 贴表情 is a
 * single trigger that opens the sticker picker panel (the mascot faces are
 * near-identical at toolbar size, so picking happens in a panel that shows
 * each sticker large with its label — 微信式); 回复 is a one-click button
 * next to it and the sole entry into a reply subthread on the main stream.
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
      aria-label="消息操作"
      data-testid="chat-bubble-toolbar"
    >
      {onReact ? (
        <button
          type="button"
          className="chat-bubble-toolbar-btn chat-bubble-toolbar-react"
          aria-label="贴表情"
          title="贴表情"
          aria-haspopup="true"
          aria-expanded={pickerOpen}
          onClick={() => setPickerOpen((open) => !open)}
        >
          <SmilePlus size={15} aria-hidden="true" />
        </button>
      ) : null}
      {onReact && pickerOpen ? (
        <div className="chat-reaction-picker" role="group" aria-label="选择表情">
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
                {REACTION_LABEL[key] ?? key}
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
          aria-label="回复"
          title="回复"
          onClick={() => onReply(item)}
        >
          <Reply size={14} aria-hidden="true" />
          <span>回复</span>
        </button>
      ) : null}
    </div>
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
}: {
  message: QueuedComposerMessage;
}): JSX.Element {
  const attachmentHint = [
    message.images.length > 0 ? `${message.images.length} 张图片` : "",
    message.files.length > 0 ? `${message.files.length} 个文件` : "",
  ]
    .filter(Boolean)
    .join(" · ");
  return (
    <div className="chat-row chat-row--user chat-row--pending">
      <div className="chat-bubble-group">
        <div className="chat-bubble chat-bubble--user chat-bubble--pending">
          {message.text.trim() ? <RichContent text={message.text} /> : null}
          {attachmentHint ? (
            <div className="chat-pending-attachments">{attachmentHint}</div>
          ) : null}
        </div>
        <div className="chat-pending-hint">发送中…</div>
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
    return "⌂ 个人";
  }
  if (meta.kind === "workspace") {
    const label = meta.name?.trim() || meta.root?.trim() || "工作区";
    return `⬒ ${label}`;
  }
  return "⬒ 全部工作区";
}

function ChatRow({
  row,
  busyParticipantIDs,
  marks,
  readerCount = 0,
  resolveParticipantName,
  subthread,
  onOpenSubthread,
  onReact,
}: {
  row: ChatMessageRow;
  busyParticipantIDs?: ReadonlySet<string>;
  marks?: MessageMarksView;
  readerCount?: number;
  resolveParticipantName?: (id: string) => string;
  subthread?: ConversationSubthread;
  onOpenSubthread?: (item: ThreadItem) => void;
  /** Stamp a one-click reaction from the bubble's hover toolbar. Absent = no reactions. */
  onReact?: (item: ThreadItem, reaction: string) => void;
}): JSX.Element {
  if (row.kind === "task") {
    // task_card 折叠卡:复用 agent-brain 转录里的 TaskCardItem(进行中显示活动
    // 状态,完成后显示 result 摘要)。「查看过程」仅在 task 绑了 subthread 时可点。
    return (
      <div className="chat-row chat-row--task">
        <TaskCardItem
          item={row.item}
          onOpenSubthread={
            row.item.task?.subthread_id
              ? () => onOpenSubthread?.(row.item)
              : undefined
          }
        />
      </div>
    );
  }
  if (row.kind === "focus") {
    const meta = row.item.focus_meta;
    const label = meta ? focusDividerLabel(meta) : "⬒ 全部工作区";
    return (
      <div className="chat-row chat-row--focus">
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
      <div className="chat-row chat-row--envelope">
        <EnvelopeNotice meta={meta} text={text} />
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
          />
        </div>
      </div>
    );
  }
  // participant
  const postKind = row.item.post_kind ?? "result";
  const participant = row.item.participant;
  const name = participant?.name?.trim() || "参与者";
  const participantStatus = participant?.id
    ? busyParticipantIDs?.has(participant.id)
      ? "busy"
      : "online"
    : undefined;
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
      <ChatAvatar participant={participant} status={participantStatus} />
      <div className="chat-bubble-group">
        <div className="chat-sender-name">{name}</div>
        <div className="chat-bubble">
          {row.item.text ? <RichContent text={row.item.text} /> : null}
          <ChatBubbleToolbar
            item={row.item}
            onReact={onReact}
            onReply={onOpenSubthread}
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
        />
      </div>
    </div>
  );
}

/**
 * Reply affordance hanging under a chat bubble (群中群折叠). Nothing renders
 * unless the message actually anchors a reply subthread. A plain discussion
 * reply shows a "N 条回复" badge (封顶 99 → 99+); once the reply is
 * (人点击)升级为 task, the same slot shows the shared task 活动卡/result 摘要
 * via TaskCardItem — 进行中显示活动状态,完成后显示 result 摘要。Both open the
 * reply panel on click via the message's anchor when onOpenSubthread is wired.
 */
function ReplyAffordance({
  subthread,
  item,
  onOpenSubthread,
}: {
  subthread?: ConversationSubthread;
  item: ThreadItem;
  onOpenSubthread?: (item: ThreadItem) => void;
}): JSX.Element | null {
  if (!subthread) {
    // No reply thread yet: nothing hangs under the bubble. Starting a reply is
    // the hover toolbar's 回复 button (ChatBubbleToolbar) — one entry, no
    // divergence. This slot only ever shows an *existing* subthread's 折叠卡.
    return null;
  }
  const open = onOpenSubthread ? () => onOpenSubthread(item) : undefined;
  if (subthread.task) {
    // 升级为 task 后的活动卡/摘要:把 subthread.task 包成一个合成 task_card item
    // 复用 TaskCardItem 呈现,与 agent-brain 转录里的 task 卡完全一致。
    const taskItem: ThreadItem = {
      id: subthread.anchor_item_id,
      type: "task_card",
      task: subthread.task,
      participant: subthread.task.participant,
    };
    return (
      <div className="chat-reply-task">
        <TaskCardItem item={taskItem} onOpenSubthread={open} />
      </div>
    );
  }
  const label = `${replyCountBadge(subthread.reply_count)} 条回复`;
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
  const avatarImage = participant?.avatar_image?.trim();
  const name = participant?.name?.trim() || "参与者";
  const statusLabel =
    status === "busy" ? "正在响应" : status === "online" ? "在线" : "";
  return (
    <div
      className="chat-avatar"
      role={status ? "img" : undefined}
      aria-label={
        status ? `${name} ${status === "busy" ? "正在响应" : "在线"}` : undefined
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
