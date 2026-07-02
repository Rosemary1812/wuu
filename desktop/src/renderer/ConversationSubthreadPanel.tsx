import { CheckCircle2, Circle, Loader2, X } from "lucide-react";
import type { ConversationSubthread, ThreadItem } from "../shared/protocol";
import { ConversationTurnList } from "./ConversationTurnList";
import { TurnView, latestAgentMessageItemID } from "./TurnView";
import type { UserFacingErrorAction } from "./UserFacingErrors";

export function ConversationSubthreadPanel({
  subthread,
  loading,
  error,
  cwd,
  onClose,
  onResolve,
  onOpenFile,
  onOpenAgent,
  onNoticeAction,
}: {
  subthread?: ConversationSubthread;
  loading?: boolean;
  error?: string;
  cwd?: string;
  onClose: () => void;
  onResolve: (resolved: boolean) => void;
  onOpenFile?: (path: string) => void;
  onOpenAgent?: (agentID: string) => void;
  onNoticeAction: (action: UserFacingErrorAction) => void;
}): JSX.Element {
  const turns = subthread?.turns ?? [];
  const latestMessageID = latestAgentMessageItemID(turns);
  const resolved = subthread?.status === "resolved";

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
          <ConversationTurnList
            threadID={subthread?.id ?? "subthread"}
            turns={turns}
            renderTurn={(turn) => (
              <TurnView
                turn={turn}
                cwd={cwd}
                onOpenFile={onOpenFile}
                onOpenAgent={onOpenAgent}
                latestAgentMessageID={latestMessageID}
                onStreamFrame={() => undefined}
                onNoticeAction={onNoticeAction}
                onOpenSubthread={(_item: ThreadItem) => undefined}
                isLatestTurn={turns[turns.length - 1]?.id === turn.id}
              />
            )}
          />
        )}
      </div>
    </aside>
  );
}
