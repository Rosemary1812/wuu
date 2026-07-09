import {
  type KeyboardEvent as ReactKeyboardEvent,
  useEffect,
  useRef,
  useState,
} from "react";
import {
  CheckCircle2,
  ChevronDown,
  Circle,
  CircleCheckBig,
  ListChecks,
  Loader2,
  SquareArrowOutUpRight,
  X,
} from "lucide-react";
import type {
  ConversationSubthread,
  ParticipantSummary,
  TaskEventView,
  TaskPieceView,
  ThreadItem,
} from "../shared/protocol";
import { ChatThreadViewContainer } from "./ChatThreadViewContainer";
import { JumpToLatestPill } from "./JumpToLatestPill";
import { SelectMenu } from "./SelectMenu";

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
 * The footer is the `composer` slot: the host passes in the SAME full
 * conversation composer the main dock uses (附件/截图/命令菜单/盾牌), so a reply
 * has the exact same send affordances as the main stream — not a stripped
 * one-line shell. It posts the human's messages back into the cth
 * (message/postSubthread → thread_id=cth participant_message). The header
 * carries the human-click "升级为 Task" gate. Once escalated, the same slot
 * offers a human-click "完成 Task" gate: the human writes a one-line conclusion
 * that bubbles back to the main stream (全员可见) and resolves the cth, flipping
 * its main-stream task 活动卡 to a result 摘要卡.
 *
 * Once escalated it also renders the PROGRESS LAYER (plan §T11): a compact node
 * board (one row per plan piece with its assignee, a Status-derived state badge,
 * and a relative activity/progress hint) plus a collapsible "轨迹" timeline that
 * lazy-loads the task's trace on expand. The "疑似失联 / 进展慢" cue on a node is
 * a DISPLAY-ONLY relative judgement computed here in the renderer — never a
 * backend state and never a fixed lease (§4.7).
 */

// Compact relative-time label ("刚刚" / "N秒前" / "N分钟前" / "N小时前" / "N天前")
// for the node board's activity/progress hints and the trace timeline.
function relativeTimeShort(iso: string | undefined, nowMs = Date.now()): string {
  if (!iso) {
    return "";
  }
  const atMs = Date.parse(iso);
  if (Number.isNaN(atMs)) {
    return "";
  }
  const elapsed = Math.max(0, nowMs - atMs);
  if (elapsed < 10_000) {
    return "刚刚";
  }
  if (elapsed < 60_000) {
    return `${Math.floor(elapsed / 1000)}秒前`;
  }
  if (elapsed < 60 * 60_000) {
    return `${Math.floor(elapsed / 60_000)}分钟前`;
  }
  if (elapsed < 24 * 60 * 60_000) {
    return `${Math.floor(elapsed / (60 * 60_000))}小时前`;
  }
  return `${Math.floor(elapsed / (24 * 60 * 60_000))}天前`;
}

// The node state badge label + CSS modifier, keyed on the backend-derived
// display state (piece.state; falls back to the raw status). done -> completed
// upstream, so the panel never depends on the internal status vocabulary.
const NODE_STATE_META: Record<string, { label: string; cls: string }> = {
  completed: { label: "完成", cls: "done" },
  done: { label: "完成", cls: "done" },
  active: { label: "进行中", cls: "active" },
  pending: { label: "待命", cls: "pending" },
  blocked: { label: "阻塞", cls: "blocked" },
  failed: { label: "失败", cls: "failed" },
  retrying: { label: "重试中", cls: "retrying" },
};

function nodeStateMeta(
  state: string | undefined,
  status: string | undefined,
): { label: string; cls: string } {
  const key = (state || status || "").trim();
  return NODE_STATE_META[key] ?? { label: key || "未知", cls: "pending" };
}

// A DISPLAY-ONLY, relative liveness cue (plan §T9 / red line §4.7): computed
// here in the renderer by comparing the node's two liveness timestamps to each
// other and to now. It is NEVER a backend state and NEVER a fixed lease — only
// a soft hint for a node that is currently running (active / retrying). No
// observable action for a relatively long stretch reads as 疑似失联; activity
// that keeps landing while progress lags well behind reads as 进展慢.
function softLivenessCue(
  piece: TaskPieceView,
  nowMs = Date.now(),
): string | undefined {
  const state = (piece.state || piece.status || "").trim();
  if (state !== "active" && state !== "retrying") {
    return undefined;
  }
  const activityMs = piece.last_activity_at
    ? Date.parse(piece.last_activity_at)
    : NaN;
  if (Number.isNaN(activityMs)) {
    return undefined;
  }
  const sinceActivity = Math.max(0, nowMs - activityMs);
  if (sinceActivity > 120_000) {
    return "疑似失联";
  }
  const progressMs = piece.last_progress_at
    ? Date.parse(piece.last_progress_at)
    : NaN;
  const sinceProgress = Number.isNaN(progressMs)
    ? Number.POSITIVE_INFINITY
    : Math.max(0, nowMs - progressMs);
  if (sinceProgress > 60_000 && sinceProgress > sinceActivity * 3) {
    return "进展慢";
  }
  return undefined;
}

// Short human labels for the trace event kinds shown in the "轨迹" timeline. An
// unknown kind falls through to its raw name (forward-compatible).
const TRACE_KIND_LABEL: Record<string, string> = {
  task_created: "任务创建",
  workflow_planned: "编排计划",
  node_started: "节点开始",
  commentary: "说明",
  tool_call: "工具调用",
  tool_result: "工具结果",
  node_progress: "进展",
  handoff_created: "交接",
  node_succeeded: "节点完成",
  node_failed: "节点失败",
  retrying: "重试",
  blocked: "阻塞",
  lead_invoked: "唤醒 lead",
  task_completed: "任务完成",
};

function traceKindLabel(kind: string): string {
  return TRACE_KIND_LABEL[kind] ?? kind;
}

export function ConversationSubthreadPanel({
  threadID,
  cwd,
  subthread,
  loading,
  error,
  onClose,
  onResolve,
  onEscalate,
  onBubble,
  onReact,
  onPopOut,
  leadCandidates,
  composer,
  resolveParticipantName,
  busyParticipantIDs,
  readerCount,
}: {
  /** Parent group thread id — cth messages carry their seq in this thread's
   *  history, so read receipts / reactions resolve against it. */
  threadID?: string;
  cwd?: string;
  subthread?: ConversationSubthread;
  loading?: boolean;
  error?: string;
  onClose: () => void;
  onResolve: (resolved: boolean) => void;
  /** Promote this reply to a task (人点击), granting编排权 to the picked named
   *  member (task lead). Absent while no subthread is loaded. */
  onEscalate?: (leadParticipantId: string) => void;
  /** Finalize the task by bubbling a one-line conclusion to the main stream and
   *  resolving the cth (人点击). Only meaningful once escalated. */
  onBubble?: (summary: string) => void;
  /** Stamp an emoji reaction on a cth message (贴 emoji, right-click). */
  onReact?: (item: ThreadItem, reaction: string) => void;
  /** Lift this reply subthread into its own window. Absent while no subthread is
   *  loaded, or inside the popped-out window itself (already detached). */
  onPopOut?: () => void;
  /** The group's named members — the candidate pool for the human's "指定 Task
   *  lead" pick when 升级为 Task. When empty/undefined the escalate gate falls
   *  back to escalating without an explicit lead. */
  leadCandidates?: ParticipantSummary[];
  /** The reused full conversation composer (host-provided slot). Rendered where
   *  the old stripped footer sat; absent while no subthread is loaded or once
   *  the cth is resolved. */
  composer?: JSX.Element;
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
  // Inline "完成 Task" conclusion form, revealed by the header gate.
  const [finalizing, setFinalizing] = useState(false);
  const [summary, setSummary] = useState("");
  // The panel's own scroll container (.conversation-subthread-body): a long
  // task/reply thread gets the same jump-to-latest pill as the main stream,
  // scoped to this panel's scroll (issue #5 Fix 2).
  const bodyScrollRef = useRef<HTMLDivElement | null>(null);
  // Inline "升级为 Task" lead picker, revealed by the header gate. The human
  // must pick a named member to hold编排权 (task lead) before escalating.
  const candidates = leadCandidates ?? [];
  const [escalating, setEscalating] = useState(false);
  const [selectedLeadID, setSelectedLeadID] = useState("");
  // The progress layer (plan §T11): the plan node board is prop-driven (from
  // subthread.plan), but the "轨迹" trace timeline is lazy — it fetches the
  // task's events only when the human expands it, and resets whenever the panel
  // switches to a different subthread.
  const plan = subthread?.plan ?? [];
  const [traceOpen, setTraceOpen] = useState(false);
  const [traceEvents, setTraceEvents] = useState<TaskEventView[] | undefined>(
    undefined,
  );
  const [traceLoading, setTraceLoading] = useState(false);
  const [traceError, setTraceError] = useState<string | undefined>(undefined);
  const subthreadID = subthread?.id;
  useEffect(() => {
    setTraceOpen(false);
    setTraceEvents(undefined);
    setTraceLoading(false);
    setTraceError(undefined);
  }, [subthreadID]);

  // Toggle the trace timeline; fetch once, on first expand, via the taskEvents
  // RPC (window.wuu). Null-safe: a missing api (e.g. a pop-out shell that never
  // wired it) just leaves the timeline empty rather than throwing.
  async function loadTrace(): Promise<void> {
    const next = !traceOpen;
    setTraceOpen(next);
    if (!next || traceEvents !== undefined || traceLoading || !subthread) {
      return;
    }
    const api = window.wuu?.taskEvents;
    if (typeof api !== "function") {
      setTraceEvents([]);
      return;
    }
    setTraceLoading(true);
    setTraceError(undefined);
    try {
      const parentID = threadID ?? subthread.thread_id ?? subthread.id;
      const result = await api(parentID, subthread.id);
      setTraceEvents(result?.events ?? []);
    } catch (err) {
      setTraceError(err instanceof Error ? err.message : String(err));
      setTraceEvents([]);
    } finally {
      setTraceLoading(false);
    }
  }

  // The header escalate gate: with named candidates it opens the inline lead
  // picker (pre-selecting the first member so 人升级 always has a valid pick);
  // with none it escalates without an explicit lead (backend allows empty).
  function beginEscalate(): void {
    if (!onEscalate) {
      return;
    }
    if (candidates.length === 0) {
      onEscalate("");
      return;
    }
    setSelectedLeadID((prev) =>
      prev && candidates.some((member) => member.id === prev)
        ? prev
        : candidates[0]!.id,
    );
    setEscalating(true);
  }

  function submitEscalate(): void {
    if (!onEscalate) {
      return;
    }
    const leadID = selectedLeadID || candidates[0]?.id || "";
    if (!leadID) {
      return;
    }
    onEscalate(leadID);
    setEscalating(false);
  }

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
              aria-expanded={escalating}
              onClick={beginEscalate}
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
          {subthread && onPopOut ? (
            <button
              type="button"
              className="icon-button conversation-subthread-icon"
              aria-label="弹出独立窗口"
              title="弹出独立窗口"
              onClick={onPopOut}
            >
              <SquareArrowOutUpRight aria-hidden="true" />
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
      <div className="conversation-subthread-body" ref={bodyScrollRef}>
        {subthread &&
        !alreadyTask &&
        !resolved &&
        escalating &&
        onEscalate &&
        candidates.length > 0 ? (
          <div className="conversation-subthread-escalate-form">
            <label
              className="conversation-subthread-lead-label"
              htmlFor="conversation-subthread-lead-select"
            >
              指定 Task lead
            </label>
            <SelectMenu
              id="conversation-subthread-lead-select"
              ariaLabel="Task lead"
              value={selectedLeadID}
              onChange={(next) => setSelectedLeadID(next)}
              options={candidates.map((member) => ({
                value: member.id,
                label: member.name || member.id,
              }))}
            />
            <button
              type="button"
              className="conversation-subthread-escalate conversation-subthread-escalate-submit"
              disabled={selectedLeadID.trim() === ""}
              onClick={submitEscalate}
            >
              <ListChecks aria-hidden="true" />
              确认升级
            </button>
          </div>
        ) : null}
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
        {subthread && alreadyTask && plan.length > 0 ? (
          <section className="conversation-subthread-board" aria-label="Task 进展">
            {plan.map((piece) => {
              const meta = nodeStateMeta(piece.state, piece.status);
              const cue = softLivenessCue(piece);
              const progressHint = piece.last_progress_at
                ? `进展 ${relativeTimeShort(piece.last_progress_at)}`
                : piece.last_activity_at
                  ? `活动 ${relativeTimeShort(piece.last_activity_at)}`
                  : "";
              const assigneeName = piece.assignee
                ? resolveParticipantName
                  ? resolveParticipantName(piece.assignee)
                  : piece.assignee
                : "";
              return (
                <div className="conversation-subthread-node" key={piece.id}>
                  <div className="conversation-subthread-node-head">
                    <span className="conversation-subthread-node-title">
                      {piece.title || piece.id}
                    </span>
                    <span
                      className={`conversation-subthread-node-state is-${meta.cls}`}
                    >
                      {meta.label}
                    </span>
                  </div>
                  {assigneeName || progressHint || cue ? (
                    <div className="conversation-subthread-node-meta">
                      {assigneeName ? (
                        <span className="conversation-subthread-node-assignee">
                          {assigneeName}
                        </span>
                      ) : null}
                      {progressHint ? (
                        <span className="conversation-subthread-node-time">
                          {progressHint}
                        </span>
                      ) : null}
                      {cue ? (
                        <span className="conversation-subthread-node-cue">
                          {cue}
                        </span>
                      ) : null}
                    </div>
                  ) : null}
                </div>
              );
            })}
          </section>
        ) : null}
        {subthread && alreadyTask ? (
          <section className="conversation-subthread-trace">
            <button
              type="button"
              className="conversation-subthread-trace-toggle"
              aria-expanded={traceOpen}
              onClick={() => {
                void loadTrace();
              }}
            >
              <ChevronDown
                aria-hidden="true"
                className={traceOpen ? "is-open" : undefined}
              />
              轨迹
            </button>
            {traceOpen ? (
              <div className="conversation-subthread-trace-body">
                {traceLoading ? (
                  <div className="conversation-subthread-trace-state">
                    加载轨迹…
                  </div>
                ) : traceError ? (
                  <div className="conversation-subthread-trace-state error">
                    {traceError}
                  </div>
                ) : (traceEvents?.length ?? 0) === 0 ? (
                  <div className="conversation-subthread-trace-state">
                    暂无轨迹
                  </div>
                ) : (
                  <ol className="conversation-subthread-trace-list">
                    {traceEvents!.map((ev) => (
                      <li
                        className="conversation-subthread-trace-item"
                        key={ev.seq}
                      >
                        <span className="conversation-subthread-trace-kind">
                          {traceKindLabel(ev.kind)}
                        </span>
                        {ev.summary ? (
                          <span className="conversation-subthread-trace-summary">
                            {ev.summary}
                          </span>
                        ) : null}
                        <span className="conversation-subthread-trace-time">
                          {relativeTimeShort(ev.at)}
                        </span>
                      </li>
                    ))}
                  </ol>
                )}
              </div>
            ) : null}
          </section>
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
            cwd={cwd}
            resolveParticipantName={resolveParticipantName}
            busyParticipantIDs={busyParticipantIDs}
            readerCount={readerCount}
            onReact={onReact}
          />
        )}
        <JumpToLatestPill containerRef={bodyScrollRef} />
      </div>
      {subthread && !resolved && composer ? (
        <div className="conversation-subthread-composer">{composer}</div>
      ) : null}
    </aside>
  );
}
