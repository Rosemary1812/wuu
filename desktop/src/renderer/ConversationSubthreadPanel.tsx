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
  ThreadItem,
} from "../shared/protocol";
import { ChatThreadViewContainer } from "./ChatThreadViewContainer";
import { formatCurrentNumber, translateCurrent as t, useI18n } from "./i18n";
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
    return t("time.justNow");
  }
  if (elapsed < 60_000) {
    const seconds = Math.floor(elapsed / 1000);
    return t(seconds === 1 ? "time.secondAgo" : "time.secondsAgo", {
      count: formatCurrentNumber(seconds),
    });
  }
  if (elapsed < 60 * 60_000) {
    const minutes = Math.floor(elapsed / 60_000);
    return t(minutes === 1 ? "time.minuteAgo" : "time.minutesAgo", {
      count: formatCurrentNumber(minutes),
    });
  }
  if (elapsed < 24 * 60 * 60_000) {
    const hours = Math.floor(elapsed / (60 * 60_000));
    return t(hours === 1 ? "time.hourAgo" : "time.hoursAgo", {
      count: formatCurrentNumber(hours),
    });
  }
  const days = Math.floor(elapsed / (24 * 60 * 60_000));
  return t(days === 1 ? "time.dayAgo" : "time.daysAgo", {
    count: formatCurrentNumber(days),
  });
}

// The node state badge label + CSS modifier, keyed on the backend-derived
// display state (piece.state; falls back to the raw status). done -> completed
// upstream, so the panel never depends on the internal status vocabulary.
const NODE_STATE_META: Record<string, { labelKey: Parameters<typeof t>[0]; cls: string }> = {
  completed: { labelKey: "subthread.node.completed", cls: "done" },
  done: { labelKey: "subthread.node.completed", cls: "done" },
  active: { labelKey: "subthread.node.active", cls: "active" },
  pending: { labelKey: "subthread.node.pending", cls: "pending" },
  blocked: { labelKey: "subthread.node.blocked", cls: "blocked" },
  failed: { labelKey: "subthread.node.failed", cls: "failed" },
  retrying: { labelKey: "subthread.node.retrying", cls: "retrying" },
  cancelled: { labelKey: "subthread.node.cancelled", cls: "cancelled" },
};

function nodeStateMeta(
  state: string | undefined,
  status: string | undefined,
): { label: string; cls: string } {
  const key = (state || status || "").trim();
  const meta = NODE_STATE_META[key];
  return meta
    ? { label: t(meta.labelKey), cls: meta.cls }
    : { label: key || t("subthread.node.unknown"), cls: "pending" };
}

const TASK_STATE_LABEL: Record<string, Parameters<typeof t>[0]> = {
  planning: "subthread.task.planning",
  executing: "subthread.task.executing",
  running: "subthread.task.executing",
  awaiting_lead: "subthread.task.awaitingLead",
  blocked: "subthread.task.blocked",
  needs_human: "subthread.task.needsHuman",
  paused: "subthread.task.paused",
  completed: "subthread.task.completed",
  failed: "subthread.task.failed",
};

function taskStateLabel(state: string | undefined): string {
  const key = state?.trim() ?? "";
  const labelKey = TASK_STATE_LABEL[key];
  return labelKey ? t(labelKey) : key || t("subthread.task.ready");
}

// Short human labels for the trace event kinds shown in the "轨迹" timeline. An
// unknown kind falls through to its raw name (forward-compatible).
const TRACE_KIND_LABEL: Record<string, Parameters<typeof t>[0]> = {
  task_created: "subthread.trace.taskCreated",
  workflow_planned: "subthread.trace.workflowPlanned",
  workflow_revised: "subthread.trace.workflowRevised",
  node_started: "subthread.trace.nodeStarted",
  commentary: "subthread.trace.commentary",
  tool_call: "subthread.trace.toolCall",
  tool_result: "subthread.trace.toolResult",
  node_progress: "subthread.trace.nodeProgress",
  handoff_created: "subthread.trace.handoffCreated",
  node_succeeded: "subthread.trace.nodeSucceeded",
  node_failed: "subthread.trace.nodeFailed",
  node_cancelled: "subthread.trace.nodeCancelled",
  retrying: "subthread.trace.retrying",
  blocked: "subthread.trace.blocked",
  lead_invoked: "subthread.trace.leadInvoked",
  task_completed: "subthread.trace.taskCompleted",
};

function traceKindLabel(kind: string): string {
  const labelKey = TRACE_KIND_LABEL[kind];
  return labelKey ? t(labelKey) : kind;
}

function taskAttentionText(state: string | undefined): string {
  switch (state?.trim()) {
    case "needs_human":
      return t("subthread.attention.needsHuman");
    case "awaiting_lead":
      return t("subthread.attention.awaitingLead");
    case "blocked":
      return t("subthread.attention.blocked");
    case "failed":
      return t("subthread.attention.failed");
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
  useI18n();
  const turns = subthread?.turns ?? [];
  const resolved = subthread?.status === "resolved";
  const alreadyTask = subthread?.status === "task" || Boolean(subthread?.task);
  const ownerID = subthread?.thread_owner_participant_id?.trim() ?? "";
  const ownerName = ownerID
    ? resolveParticipantName?.(ownerID) || ownerID
    : t("subthread.ownerPending");
  const phaseLabel = resolved
    ? t("subthread.phase.completed")
    : alreadyTask
      ? taskStateLabel(subthread?.exec_state)
      : t("subthread.phase.converging");
  const sourceText = sourceItem?.text?.trim() ?? "";
  const threadTitle =
    subthread?.title?.trim() ||
    sourceText.split("\n")[0]?.slice(0, 48) ||
    "Thread";
  const sourceAuthor = sourceItem
    ? sourceItem.type === "user_message"
      ? t("subthread.you")
      : sourceItem.participant?.name?.trim() || t("subthread.groupMember")
    : t("subthread.sourceMessage");
  // The panel's own scroll container (.conversation-subthread-body): a long
  // task/reply thread gets the same jump-to-latest pill as the main stream,
  // scoped to this panel's scroll (issue #5 Fix 2).
  const bodyScrollRef = useRef<HTMLDivElement | null>(null);
  // A callback ref triggers the render that gives JumpToLatestPill its live
  // composer anchor; reading ref.current during the first render would stay null.
  const [composerAnchorNode, setComposerAnchorNode] =
    useState<HTMLDivElement | null>(null);
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
    <aside className="conversation-subthread-panel" aria-label={t("subthread.label")}>
      <header className="conversation-subthread-header">
        <div className="conversation-subthread-title-group">
          <h2>{threadTitle}</h2>
          {subthread ? (
            <span className="conversation-subthread-meta">
              {phaseLabel} · {t(
                subthread.reply_count === 1
                  ? "chat.replyCountOne"
                  : "chat.replyCount",
                { count: formatCurrentNumber(subthread.reply_count) },
              )}
            </span>
          ) : null}
        </div>
        <div className="conversation-subthread-actions">
          {subthread && !alreadyTask && !resolved && onEscalate ? (
            <button
              type="button"
              className="conversation-subthread-escalate"
              disabled={!ownerID}
              title={
                ownerID
                  ? t("subthread.promoteOwnerTitle")
                  : t("subthread.promoteNeedsOwnerTitle")
              }
              onClick={onEscalate}
            >
              <ListChecks aria-hidden="true" />
              {t("subthread.promote")}
            </button>
          ) : null}
          {subthread && !alreadyTask && !resolved ? (
            <button
              type="button"
              className="icon-button conversation-subthread-icon"
              aria-label={t("subthread.resolve")}
              title={t("subthread.resolve")}
              onClick={() => onResolve(true)}
            >
              <Circle aria-hidden="true" />
            </button>
          ) : null}
          {subthread && onPopOut ? (
            <button
              type="button"
              className="icon-button conversation-subthread-icon"
              aria-label={t("subthread.popOut")}
              title={t("subthread.popOut")}
              onClick={onPopOut}
            >
              <SquareArrowOutUpRight aria-hidden="true" />
            </button>
          ) : null}
          <button
            type="button"
            className="icon-button conversation-subthread-icon"
            aria-label={t("common.close")}
            title={t("common.close")}
            onClick={onClose}
          >
            <X aria-hidden="true" />
          </button>
        </div>
      </header>
      <div className="conversation-subthread-body" ref={bodyScrollRef}>
        {subthread ? (
          <section
            className="conversation-subthread-overview"
            aria-label={t("subthread.overview")}
          >
            <div className="conversation-subthread-overview-meta">
              <span className="conversation-subthread-phase">{phaseLabel}</span>
              <span className="conversation-subthread-owner">
                {alreadyTask ? "Lead" : "Owner"} · {ownerName}
              </span>
            </div>
            <div className="conversation-subthread-source">
              <span>{sourceAuthor}</span>
              {sourceText ? <p>{sourceText}</p> : <p>{t("subthread.originalMessage")}</p>}
            </div>
          </section>
        ) : null}
        {subthread && alreadyTask && attentionText ? (
          <div className="conversation-subthread-attention" role="status">
            {attentionText}
          </div>
        ) : null}
        {subthread && alreadyTask && plan.length > 0 ? (
          <section
            className="conversation-subthread-board"
            aria-label={t("subthread.taskProgress")}
          >
            {plan.map((piece) => {
              const meta = nodeStateMeta(piece.state, piece.status);
              const progressHint = piece.last_progress_at
                ? t("subthread.progressAt", {
                    time: relativeTimeShort(piece.last_progress_at),
                  })
                : piece.last_activity_at
                  ? t("subthread.activityAt", {
                      time: relativeTimeShort(piece.last_activity_at),
                    })
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
                ? t("subthread.currentAttempt", {
                    count: formatCurrentNumber(piece.attempts ?? 1),
                  })
                : (piece.attempts ?? 0) > 0
                  ? t(
                      piece.attempts === 1
                        ? "subthread.attemptedOne"
                        : "subthread.attempted",
                      { count: formatCurrentNumber(piece.attempts ?? 0) },
                    )
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
                      {t("subthread.waitingFor", {
                        names: unresolvedDependencies.join(
                          t("subthread.dependencySeparator"),
                        ),
                      })}
                    </div>
                  ) : null}
                  {piece.failure_reason ? (
                    <div className="conversation-subthread-node-detail is-error">
                      {t("subthread.failureReason", {
                        reason: piece.failure_reason,
                      })}
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
              {t("subthread.trace.title")}
            </button>
            {traceOpen ? (
              <div className="conversation-subthread-trace-body">
                {traceLoading ? (
                  <div className="conversation-subthread-trace-state">
                    {t("subthread.trace.loading")}
                  </div>
                ) : traceError ? (
                  <div className="conversation-subthread-trace-state error">
                    {traceError}
                  </div>
                ) : (traceEvents?.length ?? 0) === 0 ? (
                  <div className="conversation-subthread-trace-state">
                    {t("subthread.trace.empty")}
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
            <span>{t("common.loading")}</span>
          </div>
        ) : error ? (
          <div className="conversation-subthread-state error" role="alert">
            {error}
          </div>
        ) : turns.length === 0 ? (
          <div className="conversation-subthread-state">
            {t("subthread.noReplies")}
          </div>
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
        <JumpToLatestPill
          containerRef={bodyScrollRef}
          bottomAnchor={composerAnchorNode}
        />
      </div>
      {subthread && !resolved && composer ? (
        <div
          className="conversation-subthread-composer"
          ref={setComposerAnchorNode}
        >
          {composer}
        </div>
      ) : null}
    </aside>
  );
}
