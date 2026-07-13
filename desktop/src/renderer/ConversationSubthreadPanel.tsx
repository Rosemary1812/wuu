import { useEffect, useRef, useState } from "react";
import {
  ChevronDown,
  Circle,
  ListChecks,
  Loader2,
  SquareArrowOutUpRight,
  X,
} from "lucide-react";
import type {
  ConversationSubthread,
  TaskEventView,
  TaskPieceView,
  ThreadItem,
} from "../shared/protocol";
import { ChatThreadViewContainer } from "./ChatThreadViewContainer";
import { JumpToLatestPill } from "./JumpToLatestPill";

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
 * carries the human-click "升级为 Task" gate. Promotion keeps the same cth and
 * derives its Task lead from the Thread owner; the desktop never asks the human
 * to pick a second owner or to finish the lead's work on its behalf.
 *
 * Once escalated it also renders the PROGRESS LAYER (plan §T11): a compact node
 * board (one row per plan piece with its assignee, a Status-derived state badge,
 * and a relative activity/progress hint). Raw execution trace is development
 * diagnostics and only appears when the shared debug-controls switch is on.
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
  cancelled: { label: "已取消", cls: "cancelled" },
};

function nodeStateMeta(
  state: string | undefined,
  status: string | undefined,
): { label: string; cls: string } {
  const key = (state || status || "").trim();
  return NODE_STATE_META[key] ?? { label: key || "未知", cls: "pending" };
}

const TASK_STATE_LABEL: Record<string, string> = {
  planning: "规划中",
  executing: "执行中",
  running: "执行中",
  awaiting_lead: "等待 Lead 验收",
  blocked: "受阻",
  needs_human: "需要你处理",
  paused: "已暂停",
  completed: "已完成",
  failed: "失败",
};

function taskStateLabel(state: string | undefined): string {
  const key = state?.trim() ?? "";
  return TASK_STATE_LABEL[key] ?? (key || "准备中");
}

// Short human labels for the trace event kinds shown in the "轨迹" timeline. An
// unknown kind falls through to its raw name (forward-compatible).
const TRACE_KIND_LABEL: Record<string, string> = {
  task_created: "任务创建",
  workflow_planned: "编排计划",
  workflow_revised: "调整编排",
  node_started: "节点开始",
  commentary: "说明",
  tool_call: "工具调用",
  tool_result: "工具结果",
  node_progress: "进展",
  handoff_created: "交接",
  node_succeeded: "节点完成",
  node_failed: "节点失败",
  node_cancelled: "取消节点",
  retrying: "重试",
  blocked: "阻塞",
  lead_invoked: "唤醒 lead",
  task_completed: "任务完成",
};

function traceKindLabel(kind: string): string {
  return TRACE_KIND_LABEL[kind] ?? kind;
}

function taskAttentionText(state: string | undefined): string {
  switch (state?.trim()) {
    case "needs_human":
      return "等待你的决定";
    case "awaiting_lead":
      return "Lead 正在验收 worker 结果";
    case "blocked":
      return "Lead 需要调整编排后继续";
    case "failed":
      return "Lead 需要处理失败节点";
    default:
      return "";
  }
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
  onReact,
  onPopOut,
  sourceItem,
  composer,
  resolveParticipantName,
  busyParticipantIDs,
  readerCount,
  showTechnicalTrace = false,
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
  /** Promote this Thread to a Task. The Thread owner becomes Task lead. */
  onEscalate?: () => void;
  /** Stamp an emoji reaction on a cth message (贴 emoji, right-click). */
  onReact?: (item: ThreadItem, reaction: string) => void;
  /** Lift this reply subthread into its own window. Absent while no subthread is
   *  loaded, or inside the popped-out window itself (already detached). */
  onPopOut?: () => void;
  /** Main-stream message this Thread converges on. */
  sourceItem?: ThreadItem;
  /** The reused full conversation composer (host-provided slot). Rendered where
   *  the old stripped footer sat; absent while no subthread is loaded or once
   *  the cth is resolved. */
  composer?: JSX.Element;
  resolveParticipantName?: (id: string) => string;
  busyParticipantIDs?: ReadonlySet<string>;
  readerCount?: number;
  /** Exposes raw task events for development diagnostics only. */
  showTechnicalTrace?: boolean;
}): JSX.Element {
  const turns = subthread?.turns ?? [];
  const resolved = subthread?.status === "resolved";
  const alreadyTask = subthread?.status === "task" || Boolean(subthread?.task);
  const ownerID = subthread?.thread_owner_participant_id?.trim() ?? "";
  const ownerName = ownerID
    ? resolveParticipantName?.(ownerID) || ownerID
    : "Owner 待同步";
  const phaseLabel = resolved
    ? "已完成"
    : alreadyTask
      ? taskStateLabel(subthread?.exec_state)
      : "收敛中";
  const sourceText = sourceItem?.text?.trim() ?? "";
  const threadTitle =
    subthread?.title?.trim() ||
    sourceText.split("\n")[0]?.slice(0, 48) ||
    "Thread";
  const sourceAuthor = sourceItem
    ? sourceItem.type === "user_message"
      ? "你"
      : sourceItem.participant?.name?.trim() || "群聊成员"
    : "来源消息";
  // The panel's own scroll container (.conversation-subthread-body): a long
  // task/reply thread gets the same jump-to-latest pill as the main stream,
  // scoped to this panel's scroll (issue #5 Fix 2).
  const bodyScrollRef = useRef<HTMLDivElement | null>(null);
  // The progress layer (plan §T11): the plan node board is prop-driven (from
  // subthread.plan), but the "轨迹" trace timeline is lazy — it fetches the
  // task's events only when the human expands it, and resets whenever the panel
  // switches to a different subthread.
  const plan = subthread?.plan ?? [];
  const planByID = new Map(plan.map((piece) => [piece.id, piece]));
  const attentionText = taskAttentionText(subthread?.exec_state);
  const [traceOpen, setTraceOpen] = useState(false);
  const [traceEvents, setTraceEvents] = useState<TaskEventView[] | undefined>(
    undefined,
  );
  const [traceLoading, setTraceLoading] = useState(false);
  const [traceError, setTraceError] = useState<string | undefined>(undefined);
  const subthreadID = subthread?.id;
  const traceRequestVersionRef = useRef(0);
  const traceSubthreadIDRef = useRef(subthreadID);
  if (traceSubthreadIDRef.current !== subthreadID) {
    traceSubthreadIDRef.current = subthreadID;
    traceRequestVersionRef.current += 1;
  }
  useEffect(() => {
    traceRequestVersionRef.current += 1;
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
    const requestVersion = ++traceRequestVersionRef.current;
    setTraceOpen(next);
    if (!next) {
      setTraceLoading(false);
      return;
    }
    if (traceEvents !== undefined || traceLoading || !subthread) {
      return;
    }
    const api = window.wuu?.taskEvents;
    if (typeof api !== "function") {
      if (
        traceRequestVersionRef.current === requestVersion &&
        traceSubthreadIDRef.current === subthread.id
      ) {
        setTraceEvents([]);
      }
      return;
    }
    setTraceLoading(true);
    setTraceError(undefined);
    try {
      const parentID = threadID ?? subthread.thread_id ?? subthread.id;
      const result = await api(parentID, subthread.id);
      if (
        traceRequestVersionRef.current === requestVersion &&
        traceSubthreadIDRef.current === subthread.id
      ) {
        setTraceEvents(result?.events ?? []);
      }
    } catch (err) {
      if (
        traceRequestVersionRef.current === requestVersion &&
        traceSubthreadIDRef.current === subthread.id
      ) {
        setTraceError(err instanceof Error ? err.message : String(err));
        setTraceEvents([]);
      }
    } finally {
      if (
        traceRequestVersionRef.current === requestVersion &&
        traceSubthreadIDRef.current === subthread.id
      ) {
        setTraceLoading(false);
      }
    }
  }

  return (
    <aside className="conversation-subthread-panel" aria-label="Thread">
      <header className="conversation-subthread-header">
        <div className="conversation-subthread-title-group">
          <h2>{threadTitle}</h2>
          {subthread ? (
            <span className="conversation-subthread-meta">
              {phaseLabel} · {subthread.reply_count} 条回复
            </span>
          ) : null}
        </div>
        <div className="conversation-subthread-actions">
          {subthread && !alreadyTask && !resolved && onEscalate ? (
            <button
              type="button"
              className="conversation-subthread-escalate"
              disabled={!ownerID}
              title={ownerID ? "Owner 将成为 Task Lead" : "Thread 需要 Owner"}
              onClick={onEscalate}
            >
              <ListChecks aria-hidden="true" />
              升级为 Task
            </button>
          ) : null}
          {subthread && !alreadyTask && !resolved ? (
            <button
              type="button"
              className="icon-button conversation-subthread-icon"
              aria-label="标记已解决"
              title="标记已解决"
              onClick={() => onResolve(true)}
            >
              <Circle aria-hidden="true" />
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
        {subthread ? (
          <section className="conversation-subthread-overview" aria-label="Thread 概览">
            <div className="conversation-subthread-overview-meta">
              <span className="conversation-subthread-phase">{phaseLabel}</span>
              <span className="conversation-subthread-owner">
                {alreadyTask ? "Lead" : "Owner"} · {ownerName}
              </span>
            </div>
            <div className="conversation-subthread-source">
              <span>{sourceAuthor}</span>
              {sourceText ? <p>{sourceText}</p> : <p>来自群聊中的原消息</p>}
            </div>
          </section>
        ) : null}
        {subthread && alreadyTask && attentionText ? (
          <div className="conversation-subthread-attention" role="status">
            {attentionText}
          </div>
        ) : null}
        {subthread && alreadyTask && plan.length > 0 ? (
          <section className="conversation-subthread-board" aria-label="Task 进展">
            {plan.map((piece) => {
              const meta = nodeStateMeta(piece.state, piece.status);
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
              const unresolvedDependencies = (piece.depends_on ?? []).filter(
                (id) => {
                  const dependency = planByID.get(id);
                  const state = (
                    dependency?.state ||
                    dependency?.status ||
                    ""
                  ).trim();
                  return state !== "done" && state !== "completed" && state !== "cancelled";
                },
              );
              const attemptHint = piece.current_attempt_id
                ? `第 ${piece.attempts ?? 1} 次尝试`
                : (piece.attempts ?? 0) > 0
                  ? `已尝试 ${piece.attempts} 次`
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
                  {assigneeName || attemptHint || progressHint ? (
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
                      {attemptHint ? (
                        <span className="conversation-subthread-node-attempt">
                          {attemptHint}
                        </span>
                      ) : null}
                    </div>
                  ) : null}
                  {unresolvedDependencies.length ? (
                    <div className="conversation-subthread-node-detail">
                      等待：{unresolvedDependencies.join("、")}
                    </div>
                  ) : null}
                  {piece.failure_reason ? (
                    <div className="conversation-subthread-node-detail is-error">
                      原因：{piece.failure_reason}
                    </div>
                  ) : null}
                </div>
              );
            })}
          </section>
        ) : null}
        {subthread && alreadyTask && showTechnicalTrace ? (
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
