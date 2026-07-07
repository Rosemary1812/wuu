import { Loader2, Send } from "lucide-react";
import {
  type FormEvent as ReactFormEvent,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import type {
  MemoryChangedFile,
  MemoryChangedFileAction,
  MemoryFileInfo,
  MemoryOverviewResult,
  MemoryReadResult,
  MemoryScope,
  ParticipantProfile,
} from "../shared/protocol";
import { ImagePreviewProvider } from "./ImagePreview";
import { RichContent } from "./RichContent";
import { SelectMenu } from "./SelectMenu";

// 设置 → 记忆 面板（docs/plans/2026-07-04-memory-redesign.md §8.1）。
//
// 两个子 Tab：「我」= 用户笔记本，「同事」= 选中 named agent 的身份笔记本。
// 打开或切换笔记本时先请求 memory/overview（概览 agent 一次 LLM 调用），
// 等待期间渲染骨架屏；底部 chat 走 memory/chat 由管理 agent 修改真实索引
// 记忆，返回后回显回复与变更文件清单并自动重拉 overview；「查看原文」开关
// 直接渲染 memory/read 的 MEMORY.md 索引与文件清单（无 LLM，兜底与审计用）。
//
// 后端 M2 尚未落地时三个方法都会以 unknown method 拒绝——面板显示
// "记忆面板后端尚未就绪" 占位态，绝不崩溃、绝不停留在骨架屏。

const BACKEND_NOT_READY_TEXT = "记忆面板后端尚未就绪";

type OverviewState =
  | { status: "loading" }
  | { status: "ready"; result: MemoryOverviewResult }
  | { status: "unavailable" }
  | { status: "error"; message: string };

type RawState =
  | { status: "loading" }
  | { status: "ready"; result: MemoryReadResult }
  | { status: "unavailable" }
  | { status: "error"; message: string };

type ChatEntry = {
  id: string;
  role: "user" | "assistant";
  text: string;
  pending?: boolean;
  error?: string;
  changedFiles?: MemoryChangedFile[];
};

const CHANGED_FILE_ACTION_LABELS: Record<MemoryChangedFileAction, string> = {
  created: "新增",
  modified: "更新",
  deleted: "删除",
};

const MEMORY_FILE_TYPE_LABELS: Record<string, string> = {
  user: "用户",
  feedback: "反馈",
  reference: "参考",
  lesson: "教训",
};

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

// 后端没有 M2 时 appserver 以 `unknown method "memory/..."` 拒绝
// （internal/appserver/server.go）；Electron 的 invoke 会把这段文字包进
// 转发错误里，所以只匹配子串。"method not found" 兜住 JSON-RPC 习惯拼法。
function isMemoryBackendMissing(error: unknown): boolean {
  return /unknown method|method not found/i.test(errorMessage(error));
}

function memoryParams(
  scope: MemoryScope,
  participantID: string,
): { scope: MemoryScope; participant_id?: string } {
  return scope === "participant"
    ? { scope, participant_id: participantID }
    : { scope };
}

function memoryFileTypeLabel(type: string): string {
  return MEMORY_FILE_TYPE_LABELS[type] ?? (type || "—");
}

function timestampLabel(value?: string): string {
  if (!value) {
    return "";
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return value;
  }
  return parsed.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function MemoryPanel({
  participants,
  focusParticipantID,
}: {
  participants: ParticipantProfile[];
  // 预选某位同事的笔记本（档案面板「在记忆面板中管理」跳转带过来）。
  focusParticipantID?: string;
}): JSX.Element {
  const [scope, setScope] = useState<MemoryScope>(
    focusParticipantID ? "participant" : "user",
  );
  const [selectedParticipantID, setSelectedParticipantID] = useState(
    focusParticipantID ?? "",
  );
  // 递增以强制重拉 overview（chat 成功后、点「重试」时）；原文视图打开
  // 时共用同一个 nonce，一并刷新。
  const [refreshNonce, setRefreshNonce] = useState(0);
  const [overview, setOverview] = useState<OverviewState>({ status: "loading" });
  const [rawOpen, setRawOpen] = useState(false);
  const [raw, setRaw] = useState<RawState>({ status: "loading" });
  const [chatEntries, setChatEntries] = useState<ChatEntry[]>([]);
  const [chatDraft, setChatDraft] = useState("");
  const [chatPending, setChatPending] = useState(false);
  const chatSeqRef = useRef(0);

  // 只列在职 named agent；participant/list 本身不返回已退休的。
  const namedAgents = useMemo(
    () => participants.filter((participant) => participant.kind === "named"),
    [participants],
  );
  const activeParticipantID =
    selectedParticipantID &&
    namedAgents.some((agent) => agent.id === selectedParticipantID)
      ? selectedParticipantID
      : namedAgents[0]?.id ?? "";
  // 当前笔记本的稳定键：tab / agent 切换时重置 chat。
  const notebookKey =
    scope === "user" ? "user" : `participant:${activeParticipantID}`;
  const notebookReady = scope === "user" || activeParticipantID.length > 0;

  useEffect(() => {
    setChatEntries([]);
    setChatDraft("");
  }, [notebookKey]);

  // 跳转焦点变化时切到对应同事的笔记本（初始值已在 useState 里消化，
  // 这里兜住同一面板实例被复用的情况）。
  useEffect(() => {
    if (focusParticipantID) {
      setScope("participant");
      setSelectedParticipantID(focusParticipantID);
    }
  }, [focusParticipantID]);

  useEffect(() => {
    if (!notebookReady) {
      return;
    }
    let cancelled = false;
    setOverview({ status: "loading" });
    void window.wuu
      .getMemoryOverview(memoryParams(scope, activeParticipantID))
      .then((result) => {
        if (!cancelled) {
          setOverview({ status: "ready", result });
        }
      })
      .catch((error: unknown) => {
        if (cancelled) {
          return;
        }
        setOverview(
          isMemoryBackendMissing(error)
            ? { status: "unavailable" }
            : { status: "error", message: errorMessage(error) },
        );
      });
    return () => {
      cancelled = true;
    };
  }, [scope, activeParticipantID, notebookReady, refreshNonce]);

  useEffect(() => {
    if (!rawOpen || !notebookReady) {
      return;
    }
    let cancelled = false;
    setRaw({ status: "loading" });
    void window.wuu
      .readMemoryRaw(memoryParams(scope, activeParticipantID))
      .then((result) => {
        if (!cancelled) {
          setRaw({ status: "ready", result });
        }
      })
      .catch((error: unknown) => {
        if (cancelled) {
          return;
        }
        setRaw(
          isMemoryBackendMissing(error)
            ? { status: "unavailable" }
            : { status: "error", message: errorMessage(error) },
        );
      });
    return () => {
      cancelled = true;
    };
  }, [rawOpen, scope, activeParticipantID, notebookReady, refreshNonce]);

  async function submitChat(
    event: ReactFormEvent<HTMLFormElement>,
  ): Promise<void> {
    event.preventDefault();
    const message = chatDraft.trim();
    if (!message || chatPending || !notebookReady) {
      return;
    }
    const seq = ++chatSeqRef.current;
    const replyID = `chat-${seq}-reply`;
    setChatDraft("");
    setChatEntries((entries) => [
      ...entries,
      { id: `chat-${seq}-user`, role: "user", text: message },
      { id: replyID, role: "assistant", text: "", pending: true },
    ]);
    setChatPending(true);
    try {
      const result = await window.wuu.sendMemoryChat({
        ...memoryParams(scope, activeParticipantID),
        message,
      });
      setChatEntries((entries) =>
        entries.map((entry) =>
          entry.id === replyID
            ? {
                id: replyID,
                role: "assistant" as const,
                text: result.reply_md,
                changedFiles: result.changed_files ?? [],
              }
            : entry,
        ),
      );
      // 契约 §8.1：chat 返回后自动重新拉取 overview。
      setRefreshNonce((nonce) => nonce + 1);
    } catch (error) {
      setChatEntries((entries) =>
        entries.map((entry) =>
          entry.id === replyID
            ? {
                id: replyID,
                role: "assistant" as const,
                text: "",
                error: isMemoryBackendMissing(error)
                  ? `${BACKEND_NOT_READY_TEXT}，这条消息未生效。`
                  : errorMessage(error),
              }
            : entry,
        ),
      );
    } finally {
      setChatPending(false);
    }
  }

  const canSendChat =
    notebookReady && chatDraft.trim().length > 0 && !chatPending;

  return (
    <ImagePreviewProvider>
      <div className="settings-memory" data-testid="settings-memory">
        <div className="settings-memory-toolbar">
          <div
            className="settings-memory-tabs"
            role="tablist"
            aria-label="记忆笔记本"
          >
            <button
              type="button"
              role="tab"
              aria-selected={scope === "user"}
              className={`settings-memory-tab${scope === "user" ? " active" : ""}`}
              onClick={() => setScope("user")}
            >
              我
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={scope === "participant"}
              className={`settings-memory-tab${scope === "participant" ? " active" : ""}`}
              onClick={() => setScope("participant")}
            >
              同事
            </button>
          </div>
          {scope === "participant" && namedAgents.length > 0 ? (
            <SelectMenu
              className="settings-memory-agent-select"
              triggerClassName="settings-select-trigger"
              ariaLabel="选择同事"
              dataTestid="memory-agent-select"
              value={activeParticipantID}
              onChange={(next) => setSelectedParticipantID(next)}
              options={namedAgents.map((agent) => ({
                value: agent.id,
                label: agent.name,
              }))}
            />
          ) : null}
          <div className="settings-memory-raw-toggle">
            <span className="settings-memory-raw-label">查看原文</span>
            <button
              className="settings-switch"
              type="button"
              role="switch"
              aria-checked={rawOpen}
              data-testid="memory-raw-toggle"
              onClick={() => setRawOpen((open) => !open)}
            >
              <span className="settings-switch-thumb" aria-hidden="true" />
              <span className="sr-only">
                {rawOpen ? "关闭原文视图" : "查看原文"}
              </span>
            </button>
          </div>
        </div>

        {!notebookReady ? (
          <div className="settings-card">
            <div className="settings-empty">
              还没有同事，先在侧边栏创建一位。
            </div>
          </div>
        ) : (
          <>
            <section className="settings-card settings-memory-card">
              {rawOpen ? (
                <MemoryRawView raw={raw} />
              ) : (
                <MemoryOverviewView
                  overview={overview}
                  onRetry={() => setRefreshNonce((nonce) => nonce + 1)}
                />
              )}
            </section>

            <section className="settings-card settings-memory-chat-card">
              <div className="settings-memory-chat-log">
                {chatEntries.length === 0 ? (
                  <div className="settings-memory-chat-hint">
                    想记住或忘掉什么，直接说。
                  </div>
                ) : (
                  chatEntries.map((entry) => (
                    <MemoryChatEntryView key={entry.id} entry={entry} />
                  ))
                )}
              </div>
              <form
                className="settings-memory-chat-composer"
                onSubmit={(event) => void submitChat(event)}
              >
                <input
                  className="settings-input settings-memory-chat-input"
                  data-testid="memory-chat-input"
                  value={chatDraft}
                  placeholder="例如：删掉关于旧项目路径的记忆"
                  onChange={(event) => setChatDraft(event.target.value)}
                  disabled={chatPending}
                />
                <button
                  className="settings-button settings-button-primary"
                  type="submit"
                  disabled={!canSendChat}
                >
                  {chatPending ? (
                    <Loader2
                      className="icon settings-memory-spinner"
                      aria-hidden="true"
                    />
                  ) : (
                    <Send className="icon" aria-hidden="true" />
                  )}
                  <span>发送</span>
                </button>
              </form>
            </section>
          </>
        )}
      </div>
    </ImagePreviewProvider>
  );
}

function MemoryOverviewView({
  overview,
  onRetry,
}: {
  overview: OverviewState;
  onRetry: () => void;
}): JSX.Element {
  if (overview.status === "loading") {
    return <MemorySkeleton />;
  }
  if (overview.status === "unavailable") {
    return <MemoryBackendMissingNotice />;
  }
  if (overview.status === "error") {
    return (
      <div className="settings-memory-state">
        <div className="settings-error" role="alert">
          {overview.message}
        </div>
        <button className="settings-button" type="button" onClick={onRetry}>
          重试
        </button>
      </div>
    );
  }
  const { result } = overview;
  if (!result.essay_md.trim()) {
    return <div className="settings-empty">这本笔记本还是空的。</div>;
  }
  return (
    <div className="settings-memory-overview">
      <RichContent text={result.essay_md} />
      <div className="settings-memory-meta">
        生成于 {timestampLabel(result.generated_at)}
        {result.cached ? " · 缓存" : ""}
      </div>
    </div>
  );
}

function MemoryRawView({ raw }: { raw: RawState }): JSX.Element {
  if (raw.status === "loading") {
    return <MemorySkeleton />;
  }
  if (raw.status === "unavailable") {
    return <MemoryBackendMissingNotice />;
  }
  if (raw.status === "error") {
    return (
      <div className="settings-error" role="alert">
        {raw.message}
      </div>
    );
  }
  const { result } = raw;
  return (
    <div className="settings-memory-raw" data-testid="memory-raw-view">
      {result.index_md.trim() ? (
        <RichContent text={result.index_md} />
      ) : (
        <div className="settings-empty">索引还是空的</div>
      )}
      {result.files.length > 0 ? (
        <div className="settings-memory-file-table-wrap">
          <table className="settings-memory-file-table">
            <thead>
              <tr>
                <th scope="col">名称</th>
                <th scope="col">描述</th>
                <th scope="col">类型</th>
                <th scope="col">时间</th>
              </tr>
            </thead>
            <tbody>
              {result.files.map((file: MemoryFileInfo) => (
                <tr key={file.name}>
                  <td>
                    <code>{file.name}</code>
                  </td>
                  <td>{file.description || "—"}</td>
                  <td>{memoryFileTypeLabel(file.type)}</td>
                  <td>{timestampLabel(file.mtime) || "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="settings-memory-meta">暂无记忆文件</div>
      )}
    </div>
  );
}

function MemoryChatEntryView({ entry }: { entry: ChatEntry }): JSX.Element {
  if (entry.role === "user") {
    return (
      <div className="settings-memory-chat-entry user">
        <div className="settings-memory-chat-bubble">{entry.text}</div>
      </div>
    );
  }
  return (
    <div className="settings-memory-chat-entry assistant">
      {entry.pending ? (
        <div className="settings-memory-chat-pending" role="status">
          <Loader2
            className="icon settings-memory-spinner"
            aria-hidden="true"
          />
          <span>整理中…</span>
        </div>
      ) : entry.error ? (
        <div className="settings-error" role="alert">
          {entry.error}
        </div>
      ) : (
        <div className="settings-memory-chat-reply">
          <RichContent text={entry.text} />
          {entry.changedFiles && entry.changedFiles.length > 0 ? (
            <details className="settings-memory-changes">
              <summary>变更了 {entry.changedFiles.length} 个记忆文件</summary>
              <ul>
                {entry.changedFiles.map((file) => (
                  <li key={file.path}>
                    <code>{file.path}</code>
                    <span className="settings-memory-change-action">
                      {CHANGED_FILE_ACTION_LABELS[file.action] ?? file.action}
                    </span>
                  </li>
                ))}
              </ul>
            </details>
          ) : null}
        </div>
      )}
    </div>
  );
}

// 简洁的灰条骨架：仓库里没有现成的 skeleton 组件，按契约自制一个，只用
// 主题 token（--ink-overlay-8），亮暗主题自动适配。
function MemorySkeleton(): JSX.Element {
  const widths = ["42%", "100%", "94%", "78%", "88%", "63%"];
  return (
    <div
      className="settings-memory-skeleton"
      role="status"
      aria-label="加载中"
      data-testid="memory-skeleton"
    >
      {widths.map((width, index) => (
        <span
          key={index}
          className="settings-memory-skeleton-bar"
          style={{ width }}
        />
      ))}
    </div>
  );
}

function MemoryBackendMissingNotice(): JSX.Element {
  return (
    <div
      className="settings-empty settings-memory-unavailable"
      data-testid="memory-backend-missing"
    >
      {BACKEND_NOT_READY_TEXT}，升级内核后可用。
    </div>
  );
}
