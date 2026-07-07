import { useEffect, useState } from "react";
import type { ConversationSubthread } from "../shared/protocol";
import { DefaultAvatarMark } from "./DefaultAvatar";
import { desktopApiErrorMessage } from "./WorkspaceReviewHelpers";

/**
 * TaskBoardView 是一个群线程的任务看板 tab(kind: "board"):列出该群全部
 * task/review 状态的子线程——包括无锚点消息的 standalone 任务,它们在聊天
 * 视图里没有任何挂靠点,这里是人能看到它们的唯一入口(task rail GUI 断点一)。
 * 待验收(review)置顶并标出:task_review: human 模式下那是在等人点「完成
 * Task」的队列(断点二);每行渲染认领人(断点三)。看板是观测面,不是操作面:
 * 状态流转仍走 agent 工具与 thread 面板,点行即跳回群聊 tab 打开该 thread。
 */

type TaskBoardViewProps = {
  threadID: string;
  /** 群名,仅用于标题行;缺省用 threadID 兜底。 */
  title?: string;
  /** 变化即触发重载——App 在收到 thread/subUpdated 通知时递增。 */
  refreshToken: number;
  resolveParticipantName?: (id: string) => string | undefined;
  onOpenTask: (subthreadID: string) => void;
};

type BoardData = {
  review: ConversationSubthread[];
  active: ConversationSubthread[];
};

function splitBoard(subthreads: ConversationSubthread[]): BoardData {
  const review: ConversationSubthread[] = [];
  const active: ConversationSubthread[] = [];
  for (const sub of subthreads) {
    if (sub.status === "review") {
      review.push(sub);
    } else if (sub.status === "task") {
      active.push(sub);
    }
  }
  const byCreatedAt = (a: ConversationSubthread, b: ConversationSubthread) =>
    Date.parse(a.created_at) - Date.parse(b.created_at);
  review.sort(byCreatedAt);
  active.sort(byCreatedAt);
  return { review, active };
}

export function TaskBoardView({
  threadID,
  title,
  refreshToken,
  resolveParticipantName,
  onOpenTask,
}: TaskBoardViewProps): JSX.Element {
  const [board, setBoard] = useState<BoardData | undefined>();
  const [error, setError] = useState<string | undefined>();

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const result = await window.wuu.listConversationSubthreads(threadID);
        if (cancelled) {
          return;
        }
        setBoard(splitBoard(result.subthreads ?? []));
        setError(undefined);
      } catch (err) {
        if (cancelled) {
          return;
        }
        setError(desktopApiErrorMessage(err, "无法加载任务列表"));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [threadID, refreshToken]);

  const ownerName = (sub: ConversationSubthread): string | undefined => {
    const id = sub.owner_participant_id?.trim();
    if (!id) {
      return undefined;
    }
    return resolveParticipantName?.(id) ?? id;
  };

  const renderRow = (sub: ConversationSubthread): JSX.Element => {
    const owner = ownerName(sub);
    return (
      <li key={sub.id}>
        <button
          type="button"
          className="task-board-row"
          onClick={() => onOpenTask(sub.id)}
        >
          <span className="task-board-row-owner">
            {sub.owner_participant_id ? (
              <DefaultAvatarMark seed={sub.owner_participant_id} />
            ) : (
              <span className="task-board-row-unclaimed-mark" aria-hidden="true" />
            )}
          </span>
          <span className="task-board-row-main">
            <span className="task-board-row-title">
              {sub.title?.trim() || "未命名任务"}
            </span>
            <span className="task-board-row-meta">
              {sub.status === "review"
                ? `待验收 · ${owner ?? "无人认领"}`
                : owner
                  ? `${owner} 认领`
                  : "无人认领"}
            </span>
          </span>
          {sub.status === "review" ? (
            <span className="task-board-row-chip task-board-row-chip--review">
              待验收
            </span>
          ) : null}
        </button>
      </li>
    );
  };

  const empty = board && board.review.length === 0 && board.active.length === 0;

  return (
    <div className="task-board" aria-label="任务看板">
      <header className="task-board-header">
        <h2>{title?.trim() || threadID}</h2>
        {board ? (
          <span className="task-board-header-meta">
            {board.review.length > 0
              ? `${board.review.length} 待验收 · ${board.active.length} 进行中`
              : `${board.active.length} 进行中`}
          </span>
        ) : null}
      </header>
      {error ? <div className="task-board-error">{error}</div> : null}
      {empty ? (
        <div className="task-board-empty">
          板上没有任务。让群里的 agent 拆活,或在消息的 thread 里「升级为
          Task」,任务就会出现在这里。
        </div>
      ) : null}
      {board && board.review.length > 0 ? (
        <section className="task-board-section">
          <h3>待验收</h3>
          <ul className="task-board-list">{board.review.map(renderRow)}</ul>
        </section>
      ) : null}
      {board && board.active.length > 0 ? (
        <section className="task-board-section">
          <h3>进行中</h3>
          <ul className="task-board-list">{board.active.map(renderRow)}</ul>
        </section>
      ) : null}
    </div>
  );
}
