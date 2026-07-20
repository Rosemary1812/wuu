import {
  Check,
  CheckCircle2,
  ChevronRight,
  FileText,
  Loader2,
  RotateCcw,
  Send,
  X,
  XCircle,
} from "lucide-react";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type JSX,
  type MouseEvent,
  type RefObject,
} from "react";
import type {
  KanbanArtifact,
  KanbanRun,
  KanbanRunKind,
  KanbanTask,
  KanbanTaskStatus,
  ParticipantProfile,
} from "../shared/protocol";
import { DefaultAvatarMark } from "./DefaultAvatar";
import { useI18n } from "./i18n";

type I18nT = ReturnType<typeof useI18n>["t"];

const COLUMNS: {
  status: KanbanTaskStatus;
  labelKey: Parameters<I18nT>[0];
}[] = [
  { status: "draft", labelKey: "kanban.column.draft" },
  { status: "ready", labelKey: "kanban.column.ready" },
  { status: "running", labelKey: "kanban.column.running" },
  { status: "review", labelKey: "kanban.column.review" },
  { status: "done", labelKey: "kanban.column.done" },
];

const COLUMN_WIDTH = 248;

export type KanbanBoardViewProps = {
  sessionId: string;
  refreshToken?: number;
  onOpenSourceThread?: (threadId: string) => void;
};

type LoadingState =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "ready" };

type BoardData = {
  tasks: KanbanTask[];
  participants: Map<string, ParticipantProfile>;
  childCounts: Map<string, number>;
};

export function KanbanBoardView({
  sessionId,
  refreshToken,
  onOpenSourceThread,
}: KanbanBoardViewProps): JSX.Element {
  const { t } = useI18n();
  const [phase, setPhase] = useState<LoadingState>({ kind: "loading" });
  const [data, setData] = useState<BoardData>({
    tasks: [],
    participants: new Map(),
    childCounts: new Map(),
  });
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null);
  const selectedTask = useMemo(
    () => data.tasks.find((task) => task.id === selectedTaskId) ?? null,
    [data.tasks, selectedTaskId],
  );

  const load = useCallback(async () => {
    setPhase({ kind: "loading" });
    try {
      const [tasks, participantResult] = await Promise.all([
        window.wuu.kanbanListTasks(sessionId),
        window.wuu.listParticipants(),
      ]);
      const participants = new Map<string, ParticipantProfile>();
      for (const participant of participantResult.participants) {
        participants.set(participant.id, participant);
      }
      const childCounts = new Map<string, number>();
      for (const task of tasks) {
        if (task.parent_id) {
          childCounts.set(task.parent_id, (childCounts.get(task.parent_id) ?? 0) + 1);
        }
      }
      setData({ tasks, participants, childCounts });
      setPhase({ kind: "ready" });
    } catch (error) {
      setPhase({
        kind: "error",
        message: error instanceof Error ? error.message : String(error),
      });
    }
  }, [sessionId]);

  useEffect(() => {
    let cancelled = false;
    void load().then(() => {
      if (cancelled) return;
    });
    return () => {
      cancelled = true;
    };
  }, [load, refreshToken]);

  const refresh = useCallback(() => {
    void load();
  }, [load]);

  const tasksByColumn = useMemo(() => {
    const map = new Map<KanbanTaskStatus, KanbanTask[]>();
    for (const column of COLUMNS) {
      map.set(column.status, []);
    }
    for (const task of data.tasks) {
      const list = map.get(task.status);
      if (list) {
        list.push(task);
      }
    }
    for (const list of map.values()) {
      list.sort((a, b) => a.sort_index - b.sort_index || a.created_at - b.created_at);
    }
    return map;
  }, [data.tasks]);

  if (phase.kind === "loading") {
    return (
      <div className="kanban-board kanban-board-loading" role="status" aria-live="polite">
        <Loader2 className="kanban-spinner" aria-hidden="true" />
        <span>{t("kanban.loading")}</span>
      </div>
    );
  }

  if (phase.kind === "error") {
    return (
      <div className="kanban-board kanban-board-error" role="alert">
        <XCircle aria-hidden="true" />
        <span>{t("kanban.error", { message: phase.message })}</span>
      </div>
    );
  }

  return (
    <div className="kanban-board">
      <div className="kanban-board-scroll" role="region" aria-label={t("kanban.title")}>
        <div className="kanban-board-columns" style={{ minWidth: `${COLUMN_WIDTH * COLUMNS.length}px` }}>
          {COLUMNS.map((column) => (
            <KanbanColumn
              key={column.status}
              status={column.status}
              labelKey={column.labelKey}
              tasks={tasksByColumn.get(column.status) ?? []}
              participants={data.participants}
              childCounts={data.childCounts}
              onSelectTask={setSelectedTaskId}
              onRefresh={refresh}
            />
          ))}
        </div>
      </div>
      {selectedTask ? (
        <KanbanTaskDrawer
          task={selectedTask}
          participants={data.participants}
          onClose={() => setSelectedTaskId(null)}
          onOpenSourceThread={onOpenSourceThread}
          onRefresh={refresh}
        />
      ) : null}
    </div>
  );
}

function KanbanColumn({
  status,
  labelKey,
  tasks,
  participants,
  childCounts,
  onSelectTask,
  onRefresh,
}: {
  status: KanbanTaskStatus;
  labelKey: Parameters<I18nT>[0];
  tasks: KanbanTask[];
  participants: Map<string, ParticipantProfile>;
  childCounts: Map<string, number>;
  onSelectTask: (taskId: string) => void;
  onRefresh: () => void;
}): JSX.Element {
  const { t } = useI18n();
  return (
    <div className={`kanban-column kanban-column-${status}`} style={{ width: `${COLUMN_WIDTH}px` }}>
      <div className="kanban-column-header">
        <h3 className="kanban-column-title">{t(labelKey)}</h3>
        <span className="kanban-column-count">{t("kanban.column.count", { count: tasks.length })}</span>
      </div>
      <div className="kanban-column-cards">
        {tasks.length === 0 ? (
          <div className="kanban-column-empty">{t("kanban.column.empty")}</div>
        ) : (
          tasks.map((task) => (
            <KanbanCard
              key={task.id}
              task={task}
              participants={participants}
              childCount={childCounts.get(task.id) ?? 0}
              onClick={() => onSelectTask(task.id)}
              onRefresh={onRefresh}
            />
          ))
        )}
      </div>
    </div>
  );
}

function KanbanCard({
  task,
  participants,
  childCount,
  onClick,
  onRefresh,
}: {
  task: KanbanTask;
  participants: Map<string, ParticipantProfile>;
  childCount: number;
  onClick: () => void;
  onRefresh: () => void;
}): JSX.Element {
  const { t } = useI18n();
  const latestRun = task.latest_run;
  const target = latestRun ? participants.get(latestRun.target_id) : undefined;

  return (
    <div
      className={`kanban-card kanban-card-${task.status}`}
      role="button"
      tabIndex={0}
      onClick={onClick}
      onKeyDown={(event) => {
        if (
          event.target === event.currentTarget &&
          (event.key === "Enter" || event.key === " ")
        ) {
          event.preventDefault();
          onClick();
        }
      }}
      aria-label={t("kanban.card.label", { title: task.title })}
    >
      <div className="kanban-card-header">
        <span className="kanban-card-title">{task.title}</span>
      </div>
      <div className="kanban-card-meta">
        {task.status === "draft" ? (
          <span className="kanban-status-chip kanban-status-chip-draft">
            {t("kanban.status.draft")}
          </span>
        ) : null}
        {latestRun ? (
          <span className={`kanban-status-chip kanban-status-chip-${latestRun.status}`}>
            {runStatusLabel(t, latestRun.status)}
          </span>
        ) : null}
        {target ? (
          <span className="kanban-card-target">
            <span className="kanban-card-avatar">
              <DefaultAvatarMark seed={target.id} kind={target.kind} />
            </span>
            <span className="kanban-card-target-name">{target.name}</span>
          </span>
        ) : null}
        {childCount > 0 ? (
          <span className="kanban-card-subtask-count">
            {t(
              childCount === 1 ? "kanban.subtaskCountOne" : "kanban.subtaskCount",
              { count: childCount },
            )}
          </span>
        ) : null}
      </div>
      <KanbanCardActions task={task} onRefresh={onRefresh} />
    </div>
  );
}

function KanbanCardActions({
  task,
  onRefresh,
}: {
  task: KanbanTask;
  onRefresh: () => void;
}): JSX.Element | null {
  const { t } = useI18n();
  const [dispatchOpen, setDispatchOpen] = useState(false);
  const popoverRef = useRef<HTMLDivElement | null>(null);
  const buttonRef = useRef<HTMLButtonElement | null>(null);

  useEffect(() => {
    function handleClickOutside(event: Event) {
      const target = event.target as Node;
      if (
        popoverRef.current &&
        !popoverRef.current.contains(target) &&
        !buttonRef.current?.contains(target)
      ) {
        setDispatchOpen(false);
      }
    }
    if (dispatchOpen) {
      document.addEventListener("mousedown", handleClickOutside);
      return () => document.removeEventListener("mousedown", handleClickOutside);
    }
  }, [dispatchOpen]);

  const handleTransition = async (status: KanbanTaskStatus) => {
    try {
      await window.wuu.kanbanTransitionTask(task.id, status);
      onRefresh();
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error("kanban transition failed", error);
    }
  };

  const handleDispatch = async (
    targetId: string,
    kind: KanbanRunKind,
  ) => {
    try {
      await window.wuu.kanbanDispatchRun({
        thread_id: task.source_thread_id ?? task.session_id,
        task_id: task.id,
        target_id: targetId,
        kind,
      });
      setDispatchOpen(false);
      onRefresh();
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error("kanban dispatch failed", error);
    }
  };

  switch (task.status) {
    case "draft":
      return (
        <div className="kanban-card-actions">
          <button
            className="kanban-action-button kanban-action-button-primary"
            type="button"
            onClick={(event) => {
              event.stopPropagation();
              void handleTransition("ready");
            }}
          >
            <Check size={12} />
            {t("kanban.action.confirm")}
          </button>
          <button
            className="kanban-action-button"
            type="button"
            onClick={(event) => {
              event.stopPropagation();
              void handleTransition("cancelled");
            }}
          >
            <X size={12} />
            {t("kanban.action.cancel")}
          </button>
        </div>
      );
    case "ready":
      return (
        <div className="kanban-card-actions">
          <div className="kanban-dispatch-anchor">
            <button
              ref={buttonRef}
              className="kanban-action-button kanban-action-button-primary"
              type="button"
              onClick={(event) => {
                event.stopPropagation();
                setDispatchOpen((open) => !open);
              }}
            >
              <Send size={12} />
              {t("kanban.action.dispatch")}
            </button>
            {dispatchOpen ? (
              <KanbanDispatchPopover
                popoverRef={popoverRef}
                task={task}
                defaultKind="execute"
                onDispatch={handleDispatch}
                onClose={() => setDispatchOpen(false)}
              />
            ) : null}
          </div>
        </div>
      );
    case "review":
      return (
        <div className="kanban-card-actions">
          <button
            className="kanban-action-button kanban-action-button-primary"
            type="button"
            onClick={(event) => {
              event.stopPropagation();
              void handleTransition("done");
            }}
          >
            <CheckCircle2 size={12} />
            {t("kanban.action.accept")}
          </button>
          <div className="kanban-dispatch-anchor">
            <button
              ref={buttonRef}
              className="kanban-action-button"
              type="button"
              onClick={(event) => {
                event.stopPropagation();
                setDispatchOpen((open) => !open);
              }}
            >
              <RotateCcw size={12} />
              {t("kanban.action.reject")}
            </button>
            {dispatchOpen ? (
              <KanbanDispatchPopover
                popoverRef={popoverRef}
                task={task}
                defaultKind="execute"
                onDispatch={handleDispatch}
                onClose={() => setDispatchOpen(false)}
              />
            ) : null}
          </div>
        </div>
      );
    case "running":
      return (
        <div className="kanban-card-actions">
          <span className="kanban-card-running-chip">
            <Loader2 className="kanban-running-spinner" size={12} />
            {t("kanban.status.running")}
          </span>
        </div>
      );
    default:
      return null;
  }
}

const KanbanDispatchPopover = ({
  task,
  defaultKind,
  onDispatch,
  onClose,
  popoverRef,
}: {
  task: KanbanTask;
  defaultKind: KanbanRunKind;
  onDispatch: (targetId: string, kind: KanbanRunKind) => void;
  onClose: () => void;
  popoverRef?: RefObject<HTMLDivElement | null>;
}) => {
  const { t } = useI18n();
  const [participants, setParticipants] = useState<ParticipantProfile[]>([]);
  const [selectedId, setSelectedId] = useState<string>("");
  const [kind, setKind] = useState<KanbanRunKind>(defaultKind);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    window.wuu
      .listParticipants()
      .then((result) => {
        setParticipants(result.participants);
        setSelectedId(
          task.latest_run?.target_id ?? result.participants[0]?.id ?? "",
        );
      })
      .finally(() => setLoading(false));
  }, [task.latest_run?.target_id]);

  const handleSubmit = (event: MouseEvent<HTMLButtonElement>) => {
    event.stopPropagation();
    if (selectedId) {
      onDispatch(selectedId, kind);
    }
  };

  return (
    <div className="kanban-dispatch-popover" ref={popoverRef} onClick={(e) => e.stopPropagation()}>
      <div className="kanban-dispatch-popover-row">
        <label className="kanban-dispatch-label">{t("kanban.dispatch.target")}</label>
        {loading ? (
          <Loader2 className="kanban-spinner" size={14} />
        ) : (
          <select
            className="kanban-dispatch-select"
            value={selectedId}
            onChange={(e) => setSelectedId(e.target.value)}
            onClick={(e) => e.stopPropagation()}
          >
            {participants.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        )}
      </div>
      <div className="kanban-dispatch-popover-row">
        <label className="kanban-dispatch-label">{t("kanban.dispatch.kind")}</label>
        <div className="kanban-dispatch-kind">
          <button
            className={`kanban-dispatch-kind-button${kind === "execute" ? " active" : ""}`}
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              setKind("execute");
            }}
          >
            {t("kanban.kind.execute")}
          </button>
          <button
            className={`kanban-dispatch-kind-button${kind === "review" ? " active" : ""}`}
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              setKind("review");
            }}
          >
            {t("kanban.kind.review")}
          </button>
        </div>
      </div>
      <div className="kanban-dispatch-popover-actions">
        <button className="kanban-dispatch-cancel" type="button" onClick={onClose}>
          {t("common.cancel")}
        </button>
        <button
          className="kanban-dispatch-confirm"
          type="button"
          disabled={!selectedId}
          onClick={handleSubmit}
        >
          {t("kanban.action.dispatch")}
        </button>
      </div>
    </div>
  );
};

function KanbanTaskDrawer({
  task,
  participants,
  onClose,
  onOpenSourceThread,
  onRefresh,
}: {
  task: KanbanTask;
  participants: Map<string, ParticipantProfile>;
  onClose: () => void;
  onOpenSourceThread?: (threadId: string) => void;
  onRefresh: () => void;
}): JSX.Element {
  const { t } = useI18n();
  const [runs, setRuns] = useState<KanbanRun[] | null>(null);
  const [artifacts, setArtifacts] = useState<KanbanArtifact[] | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    Promise.all([window.wuu.kanbanListRuns(task.id), window.wuu.kanbanListArtifacts(task.id)])
      .then(([runResult, artifactResult]) => {
        if (cancelled) return;
        setRuns(runResult);
        setArtifacts(artifactResult);
      })
      .catch((err) => {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [task.id]);

  return (
    <div className="kanban-drawer-overlay" onClick={onClose}>
      <div
        className="kanban-drawer"
        role="dialog"
        aria-modal="true"
        aria-label={t("kanban.drawer.title")}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="kanban-drawer-header">
          <h3 className="kanban-drawer-title">{task.title}</h3>
          <button className="kanban-drawer-close" type="button" onClick={onClose} aria-label={t("common.close")}>
            <X size={16} />
          </button>
        </div>
        <div className="kanban-drawer-body">
          <StatusLine task={task} t={t} />
          {task.brief ? (
            <div className="kanban-drawer-section">
              <h4>{t("kanban.drawer.brief")}</h4>
              <p className="kanban-drawer-brief">{task.brief}</p>
            </div>
          ) : null}
          {task.source_thread_id && onOpenSourceThread ? (
            <div className="kanban-drawer-section">
              <button
                className="kanban-link-button"
                type="button"
                onClick={() => {
                  onOpenSourceThread(task.source_thread_id!);
                  onClose();
                }}
              >
                {t("kanban.drawer.openSourceThread")}
                <ChevronRight size={14} />
              </button>
            </div>
          ) : null}
          <div className="kanban-drawer-section">
            <h4>{t("kanban.drawer.runs")}</h4>
            {loading ? (
              <div className="kanban-drawer-loading">
                <Loader2 className="kanban-spinner" size={14} />
              </div>
            ) : error ? (
              <div className="kanban-drawer-error">{error}</div>
            ) : runs && runs.length > 0 ? (
              <ul className="kanban-drawer-list">
                {runs.map((run) => (
                  <KanbanRunItem key={run.id} run={run} participants={participants} t={t} />
                ))}
              </ul>
            ) : (
              <div className="kanban-drawer-empty">{t("kanban.drawer.noRuns")}</div>
            )}
          </div>
          <div className="kanban-drawer-section">
            <h4>{t("kanban.drawer.artifacts")}</h4>
            {loading ? (
              <div className="kanban-drawer-loading">
                <Loader2 className="kanban-spinner" size={14} />
              </div>
            ) : error ? (
              <div className="kanban-drawer-error">{error}</div>
            ) : artifacts && artifacts.length > 0 ? (
              <ul className="kanban-drawer-list">
                {artifacts.map((artifact) => (
                  <KanbanArtifactItem key={artifact.id} artifact={artifact} t={t} />
                ))}
              </ul>
            ) : (
              <div className="kanban-drawer-empty">{t("kanban.drawer.noArtifacts")}</div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

function StatusLine({
  task,
  t,
}: {
  task: KanbanTask;
  t: I18nT;
}): JSX.Element {
  return (
    <div className="kanban-drawer-status-line">
      <span className={`kanban-status-chip kanban-status-chip-${task.status}`}>
        {taskStatusLabel(t, task.status)}
      </span>
      <span className="kanban-drawer-timestamp">
        {t("kanban.drawer.updated", { time: formatTimestamp(task.updated_at) })}
      </span>
    </div>
  );
}

function KanbanRunItem({
  run,
  participants,
  t,
}: {
  run: KanbanRun;
  participants: Map<string, ParticipantProfile>;
  t: I18nT;
}): JSX.Element {
  const target = participants.get(run.target_id);
  return (
    <li className="kanban-drawer-run">
      <div className="kanban-drawer-run-header">
        <span className={`kanban-status-chip kanban-status-chip-${run.status}`}>
          {runStatusLabel(t, run.status)}
        </span>
        <span className="kanban-drawer-run-kind">{t("kanban.kind.label", { kind: run.kind })}</span>
      </div>
      {target ? (
        <div className="kanban-drawer-run-target">
          <span className="kanban-card-avatar">
            <DefaultAvatarMark seed={target.id} kind={target.kind} />
          </span>
          <span>{target.name}</span>
        </div>
      ) : null}
      {run.summary ? <p className="kanban-drawer-run-summary">{run.summary}</p> : null}
      {run.error_message ? (
        <p className="kanban-drawer-run-error">{run.error_message}</p>
      ) : null}
      <div className="kanban-drawer-run-times">
        {run.created_at ? (
          <span>{t("kanban.run.created", { time: formatTimestamp(run.created_at) })}</span>
        ) : null}
        {run.finished_at ? (
          <span>{t("kanban.run.finished", { time: formatTimestamp(run.finished_at) })}</span>
        ) : null}
      </div>
    </li>
  );
}

function KanbanArtifactItem({
  artifact,
  t,
}: {
  artifact: KanbanArtifact;
  t: I18nT;
}): JSX.Element {
  return (
    <li className="kanban-drawer-artifact">
      <FileText size={14} aria-hidden="true" />
      <div className="kanban-drawer-artifact-main">
        <span className="kanban-drawer-artifact-name">
          {artifact.display_name || artifact.path}
        </span>
        <span className="kanban-drawer-artifact-size">
          {t("kanban.artifact.size", { size: formatBytes(artifact.size_bytes) })}
        </span>
      </div>
    </li>
  );
}

function taskStatusLabel(
  t: I18nT,
  status: KanbanTaskStatus,
): string {
  switch (status) {
    case "draft":
      return t("kanban.status.draft");
    case "ready":
      return t("kanban.status.ready");
    case "running":
      return t("kanban.status.running");
    case "review":
      return t("kanban.status.review");
    case "done":
      return t("kanban.status.done");
    case "cancelled":
      return t("kanban.status.cancelled");
    default:
      return String(status);
  }
}

function runStatusLabel(
  t: I18nT,
  status: KanbanRun["status"],
): string {
  switch (status) {
    case "queued":
      return t("kanban.runStatus.queued");
    case "running":
      return t("kanban.runStatus.running");
    case "succeeded":
      return t("kanban.runStatus.succeeded");
    case "failed":
      return t("kanban.runStatus.failed");
    case "interrupted":
      return t("kanban.runStatus.interrupted");
    default:
      return String(status);
  }
}

function formatTimestamp(ts: number): string {
  return new Date(ts).toLocaleString();
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

export type {
  KanbanTask,
  KanbanTaskStatus,
  KanbanRun,
  KanbanArtifact,
  KanbanRunKind,
  ParticipantProfile,
};
