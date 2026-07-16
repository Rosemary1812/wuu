import { useEffect, useState } from "react";
import type { ConversationSubthread, TaskPieceView } from "../shared/protocol";
import { DefaultAvatarMark } from "./DefaultAvatar";
import { desktopApiErrorMessage } from "./WorkspaceReviewHelpers";
import { translateCurrent, useI18n } from "./i18n";
import type { TranslationKey } from "./i18n/resources/zh-CN";

type TaskBoardViewProps = {
  threadID: string;
  title?: string;
  refreshToken: number;
  resolveParticipantName?: (id: string) => string | undefined;
  onOpenTask: (subthreadID: string) => void;
};

type BoardData = {
  attention: ConversationSubthread[];
  running: ConversationSubthread[];
  completed: ConversationSubthread[];
};

type BoardSnapshot = {
  requestKey: string;
  data: BoardData;
};

type BoardLoadState = {
  requestKey: string;
  loading: boolean;
  error?: string;
};

const EXEC_STATE_LABEL: Record<string, TranslationKey> = {
  planning: "taskBoard.state.planning",
  executing: "taskBoard.state.executing",
  running: "taskBoard.state.executing",
  awaiting_lead: "taskBoard.state.awaitingLead",
  blocked: "taskBoard.state.blocked",
  needs_human: "taskBoard.state.needsHuman",
  paused: "taskBoard.state.paused",
  completed: "taskBoard.state.completed",
  failed: "taskBoard.state.failed",
};

function executionLabel(subthread: ConversationSubthread): string {
  if (subthread.status === "resolved") {
    return translateCurrent("taskBoard.state.completed");
  }
  const key = subthread.exec_state?.trim() ?? "";
  return EXEC_STATE_LABEL[key]
    ? translateCurrent(EXEC_STATE_LABEL[key])
    : key || translateCurrent("taskBoard.state.ready");
}

function isEscalatedTask(subthread: ConversationSubthread): boolean {
  return (
    subthread.status === "task" ||
    Boolean(
      subthread.task ||
        subthread.escalated_by ||
        subthread.lead_participant_id ||
        subthread.exec_state,
    )
  );
}

function isCompletedTask(subthread: ConversationSubthread): boolean {
  return (
    subthread.status === "resolved" || subthread.exec_state === "completed"
  );
}

function taskNeedsAttention(subthread: ConversationSubthread): boolean {
  return ["awaiting_lead", "blocked", "needs_human", "failed"].includes(
    subthread.exec_state?.trim() ?? "",
  );
}

function splitBoard(subthreads: ConversationSubthread[]): BoardData {
  const attention: ConversationSubthread[] = [];
  const running: ConversationSubthread[] = [];
  const completed: ConversationSubthread[] = [];
  for (const subthread of subthreads) {
    if (!isEscalatedTask(subthread)) {
      continue;
    }
    if (isCompletedTask(subthread)) {
      completed.push(subthread);
    } else if (taskNeedsAttention(subthread)) {
      attention.push(subthread);
    } else {
      running.push(subthread);
    }
  }
  const newestFirst = (a: ConversationSubthread, b: ConversationSubthread) =>
    Date.parse(b.created_at) - Date.parse(a.created_at);
  attention.sort(newestFirst);
  running.sort(newestFirst);
  completed.sort(newestFirst);
  return { attention, running, completed };
}

function isCompletedPiece(piece: TaskPieceView): boolean {
  const state = (piece.state || piece.status || "").trim();
  return state === "completed" || state === "done" || state === "succeeded";
}

function isActivePiece(piece: TaskPieceView): boolean {
  const state = (piece.state || piece.status || "").trim();
  return (
    state === "active" ||
    state === "running" ||
    state === "retrying"
  );
}

export function TaskBoardView({
  threadID,
  title,
  refreshToken,
  resolveParticipantName,
  onOpenTask,
}: TaskBoardViewProps): JSX.Element {
  const { locale, t, formatNumber } = useI18n();
  const requestKey = `${threadID}:${refreshToken}`;
  const [snapshot, setSnapshot] = useState<BoardSnapshot>();
  const [loadState, setLoadState] = useState<BoardLoadState>({
    requestKey,
    loading: true,
  });
  const board = snapshot?.requestKey === requestKey ? snapshot.data : undefined;
  const loading =
    loadState.requestKey !== requestKey || loadState.loading;
  const error =
    loadState.requestKey === requestKey ? loadState.error : undefined;

  useEffect(() => {
    let cancelled = false;
    setSnapshot(undefined);
    setLoadState({ requestKey, loading: true });
    void (async () => {
      try {
        const result = await window.wuu.listConversationSubthreads(threadID);
        if (!cancelled) {
          setSnapshot({
            requestKey,
            data: splitBoard(result.subthreads ?? []),
          });
          setLoadState({ requestKey, loading: false });
        }
      } catch (err) {
        if (!cancelled) {
          setLoadState({
            requestKey,
            loading: false,
            error: desktopApiErrorMessage(err, translateCurrent("taskBoard.loadFailed")),
          });
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [requestKey, threadID, locale]);

  const participantName = (id: string): string =>
    resolveParticipantName?.(id) ?? id;

  const renderRow = (subthread: ConversationSubthread): JSX.Element => {
    const leadID =
      subthread.lead_participant_id?.trim() ||
      subthread.thread_owner_participant_id?.trim() ||
      "";
    const lead = leadID ? participantName(leadID) : t("taskBoard.leadMissing");
    const plan = subthread.plan ?? [];
    const cancelledCount = plan.filter(
      (piece) => (piece.state || piece.status || "").trim() === "cancelled",
    ).length;
    const livePlan = plan.filter(
      (piece) => (piece.state || piece.status || "").trim() !== "cancelled",
    );
    const completedCount = livePlan.filter(isCompletedPiece).length;
    const activeWorkers = [
      ...new Set(
        plan
          .filter(isActivePiece)
          .flatMap((piece) => (piece.assignee ? [participantName(piece.assignee)] : [])),
      ),
    ];
    return (
      <li key={subthread.id}>
        <button
          type="button"
          className={`task-board-row${taskNeedsAttention(subthread) ? " is-attention" : ""}`}
          onClick={() => onOpenTask(subthread.id)}
        >
          {leadID ? (
            <span className="task-board-row-lead-avatar">
              <DefaultAvatarMark seed={leadID} />
            </span>
          ) : null}
          <span className="task-board-row-main">
            <span className="task-board-row-heading">
              <span className="task-board-row-title">
                {subthread.title?.trim() || t("taskBoard.untitled")}
              </span>
              <span className="task-board-row-state">
                {executionLabel(subthread)}
              </span>
            </span>
            <span className="task-board-row-meta">
              <span>{t("taskBoard.lead", { name: lead })}</span>
              {livePlan.length > 0 ? (
                <span>
                  {t("taskBoard.planProgress", {
                    completed: formatNumber(completedCount),
                    total: formatNumber(livePlan.length),
                  })}
                </span>
              ) : null}
              {cancelledCount > 0 ? (
                <span>{t(cancelledCount === 1 ? "taskBoard.cancelledOne" : "taskBoard.cancelled", { count: formatNumber(cancelledCount) })}</span>
              ) : null}
              {activeWorkers.length > 0 ? (
                <span>{t("taskBoard.executingWorkers", { names: activeWorkers.join(t("taskBoard.nameSeparator")) })}</span>
              ) : null}
            </span>
          </span>
        </button>
      </li>
    );
  };

  const empty =
    board &&
    board.attention.length === 0 &&
    board.running.length === 0 &&
    board.completed.length === 0;

  return (
    <div className="task-board" aria-label={t("taskBoard.label")}>
      <header className="task-board-header">
        <h2>{title?.trim() || threadID}</h2>
        {board ? (
          <span className="task-board-header-meta">
            {t("taskBoard.summary", {
              attention: formatNumber(board.attention.length),
              running: formatNumber(board.running.length),
              completed: formatNumber(board.completed.length),
            })}
          </span>
        ) : null}
      </header>
      {loading ? (
        <div className="task-board-loading" role="status">{t("taskBoard.loading")}</div>
      ) : null}
      {error ? (
        <div className="task-board-error" role="alert">{error}</div>
      ) : null}
      {empty ? (
        <div className="task-board-empty">
          {t("taskBoard.empty")}
        </div>
      ) : null}
      {board && board.attention.length > 0 ? (
        <section className="task-board-section is-attention">
          <h3>{t("taskBoard.section.attention")}</h3>
          <ul className="task-board-list">{board.attention.map(renderRow)}</ul>
        </section>
      ) : null}
      {board && board.running.length > 0 ? (
        <section className="task-board-section">
          <h3>{t("taskBoard.section.running")}</h3>
          <ul className="task-board-list">{board.running.map(renderRow)}</ul>
        </section>
      ) : null}
      {board && board.completed.length > 0 ? (
        <section className="task-board-section">
          <h3>{t("taskBoard.section.completed")}</h3>
          <ul className="task-board-list">{board.completed.map(renderRow)}</ul>
        </section>
      ) : null}
    </div>
  );
}
