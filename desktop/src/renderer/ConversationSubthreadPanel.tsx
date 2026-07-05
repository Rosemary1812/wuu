import {
  type KeyboardEvent as ReactKeyboardEvent,
  useState,
} from "react";
import {
  CheckCircle2,
  Circle,
  CircleCheckBig,
  ListChecks,
  Loader2,
  Send,
  X,
} from "lucide-react";
import type { ConversationSubthread, ThreadItem } from "../shared/protocol";
import { ChatThreadViewContainer } from "./ChatThreadViewContainer";

/**
 * The split reply panel (群中群) for a message's reply subthread (cth). It renders
 * the cth's message stream through the SAME full conversation view the main chat
 * uses (ChatThreadView via its container) — not a stripped-down transcript — so a
 * reply reads exactly like the main thread, just scoped to its participant subset.
 *
 * Sitting side-by-side with the main conversation (absolute right column, see
 * subthreads.css) it is the "左右分屏" surface: main stream on the left, this reply
 * on the right. It deliberately does NOT pass onOpenSubthread to the inner view —
 * that omission is how 一层不嵌套 is enforced at the UI level (a message already
 * inside a cth offers no further reply entry).
 *
 * A footer composer posts the human's messages back into the cth
 * (message/postSubthread → thread_id=cth participant_message), and the header
 * carries the human-click "升级为 Task" gate. Once escalated, the same slot
 * offers a human-click "完成 Task" gate: the human writes a one-line conclusion
 * that bubbles back to the main stream (全员可见) and resolves the cth, flipping
 * its main-stream task 活动卡 to a result 摘要卡.
 */
export function ConversationSubthreadPanel({
  threadID,
  subthread,
  loading,
  error,
  onClose,
  onResolve,
  onEscalate,
  onBubble,
  onSend,
  onReact,
  resolveParticipantName,
  busyParticipantIDs,
  readerCount,
}: {
  /** Parent group thread id — cth messages carry their seq in this thread's
   *  history, so read receipts / reactions resolve against it. */
  threadID?: string;
  subthread?: ConversationSubthread;
  loading?: boolean;
  error?: string;
  onClose: () => void;
  onResolve: (resolved: boolean) => void;
  /** Promote this reply to a task (人点击). Absent while no subthread is loaded. */
  onEscalate?: () => void;
  /** Finalize the task by bubbling a one-line conclusion to the main stream and
   *  resolving the cth (人点击). Only meaningful once escalated. */
  onBubble?: (summary: string) => void;
  /** Post a human message into the cth. */
  onSend?: (text: string) => void;
  /** Stamp an emoji reaction on a cth message (贴 emoji, right-click). */
  onReact?: (item: ThreadItem, reaction: string) => void;
  resolveParticipantName?: (id: string) => string;
  busyParticipantIDs?: ReadonlySet<string>;
  readerCount?: number;
}): JSX.Element {
  const turns = subthread?.turns ?? [];
  const resolved = subthread?.status === "resolved";
  // A reply already promoted to a task carries a task_card; the escalate gate is
  // then spent (execution folds into the same cth) so the button drops away and
  // the "完成 Task" gate takes its place.
  const alreadyTask = Boolean(subthread?.task);
  const canFinalize = Boolean(subthread) && alreadyTask && !resolved && Boolean(onBubble);
  const [draft, setDraft] = useState("");
  // Inline "完成 Task" conclusion form, revealed by the header gate.
  const [finalizing, setFinalizing] = useState(false);
  const [summary, setSummary] = useState("");

  function submitSummary(): void {
    const text = summary.trim();
    if (!text || !onBubble) {
      return;
    }
    onBubble(text);
    setSummary("");
    setFinalizing(false);
  }

  function onSummaryKeyDown(event: ReactKeyboardEvent<HTMLTextAreaElement>): void {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      submitSummary();
    } else if (event.key === "Escape") {
      event.preventDefault();
      setFinalizing(false);
    }
  }

  function submitDraft(): void {
    const text = draft.trim();
    if (!text || !onSend) {
      return;
    }
    onSend(text);
    setDraft("");
  }

  function onComposerKeyDown(event: ReactKeyboardEvent<HTMLTextAreaElement>): void {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      submitDraft();
    }
  }

  return (
    <aside className="conversation-subthread-panel" aria-label="Thread">
      <header className="conversation-subthread-header">
        <div className="conversation-subthread-title-group">
          <h2>{subthread?.title || "Thread"}</h2>
          {subthread ? (
            <span className="conversation-subthread-meta">
              {subthread.reply_count} 条回复
            </span>
          ) : null}
        </div>
        <div className="conversation-subthread-actions">
          {subthread && !alreadyTask && !resolved && onEscalate ? (
            <button
              type="button"
              className="conversation-subthread-escalate"
              onClick={onEscalate}
            >
              <ListChecks aria-hidden="true" />
              升级为 Task
            </button>
          ) : null}
          {canFinalize ? (
            <button
              type="button"
              className="conversation-subthread-escalate conversation-subthread-finalize-toggle"
              aria-expanded={finalizing}
              onClick={() => setFinalizing((open) => !open)}
            >
              <CircleCheckBig aria-hidden="true" />
              完成 Task
            </button>
          ) : null}
          {subthread ? (
            <button
              type="button"
              className="icon-button conversation-subthread-icon"
              aria-label={resolved ? "重新打开" : "标记已解决"}
              title={resolved ? "重新打开" : "标记已解决"}
              onClick={() => onResolve(!resolved)}
            >
              {resolved ? <CheckCircle2 aria-hidden="true" /> : <Circle aria-hidden="true" />}
            </button>
          ) : null}
          <button
            type="button"
            className="icon-button conversation-subthread-icon"
            aria-label="关闭"
            title="关闭"
            onClick={onClose}
          >
            <X aria-hidden="true" />
          </button>
        </div>
      </header>
      <div className="conversation-subthread-body">
        {canFinalize && finalizing ? (
          <div className="conversation-subthread-finalize">
            <textarea
              className="conversation-subthread-input"
              value={summary}
              placeholder="一句话结论,冒泡回主流…"
              aria-label="Task 结论"
              rows={2}
              autoFocus
              onChange={(event) => setSummary(event.target.value)}
              onKeyDown={onSummaryKeyDown}
            />
            <button
              type="button"
              className="conversation-subthread-escalate conversation-subthread-finalize-submit"
              disabled={summary.trim() === ""}
              onClick={submitSummary}
            >
              <CircleCheckBig aria-hidden="true" />
              冒泡并完成
            </button>
          </div>
        ) : null}
        {loading ? (
          <div className="conversation-subthread-state" role="status">
            <Loader2 aria-hidden="true" />
            <span>加载中</span>
          </div>
        ) : error ? (
          <div className="conversation-subthread-state error" role="alert">
            {error}
          </div>
        ) : turns.length === 0 ? (
          <div className="conversation-subthread-state">暂无回复</div>
        ) : (
          <ChatThreadViewContainer
            key={subthread?.id ?? "subthread"}
            threadID={threadID ?? subthread?.thread_id ?? subthread?.id ?? "subthread"}
            turns={turns}
            resolveParticipantName={resolveParticipantName}
            busyParticipantIDs={busyParticipantIDs}
            readerCount={readerCount}
            onReact={onReact}
          />
        )}
      </div>
      {subthread && !resolved && onSend ? (
        <footer className="conversation-subthread-composer">
          <textarea
            className="conversation-subthread-input"
            value={draft}
            placeholder="回复这条线程…"
            aria-label="回复这条线程"
            rows={1}
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={onComposerKeyDown}
          />
          <button
            type="button"
            className="icon-button conversation-subthread-send"
            aria-label="发送"
            title="发送"
            disabled={draft.trim() === ""}
            onClick={submitDraft}
          >
            <Send aria-hidden="true" />
          </button>
        </footer>
      ) : null}
    </aside>
  );
}
