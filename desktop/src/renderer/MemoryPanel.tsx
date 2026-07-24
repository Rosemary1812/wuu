import { Loader2, RefreshCw, Send } from "lucide-react";
import {
  type FormEvent as ReactFormEvent,
  useEffect,
  useRef,
  useState,
} from "react";
import type {
  MemoryChangedFile,
  MemoryChangedFileAction,
  MemoryFileInfo,
  MemoryOverviewResult,
  MemoryReadResult,
} from "../shared/protocol";
import { ImagePreviewProvider } from "./ImagePreview";
import { RichContent } from "./RichContent";
import { useI18n } from "./i18n";

// 设置 → 记忆 面板（docs/plans/2026-07-04-memory-redesign.md §8.1）。
//
// 用户笔记本：打开时先请求 memory/overview（概览 agent 一次 LLM 调用），
// 等待期间渲染骨架屏；底部 chat 走 memory/chat 由管理 agent 修改真实索引
// 记忆，返回后回显回复与变更文件清单并自动重拉 overview；「查看原文」开关
// 直接渲染 memory/read 的 MEMORY.md 索引与文件清单（无 LLM，兜底与审计用）。
//
// 后端 M2 尚未落地时三个方法都会以 unknown method 拒绝——面板显示
// "记忆面板后端尚未就绪" 占位态，绝不崩溃、绝不停留在骨架屏。

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

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

// 后端没有 M2 时 appserver 以 `unknown method "memory/..."` 拒绝
// （internal/appserver/server.go）；Electron 的 invoke 会把这段文字包进
// 转发错误里，所以只匹配子串。"method not found" 兜住 JSON-RPC 习惯拼法。
function isMemoryBackendMissing(error: unknown): boolean {
  return /unknown method|method not found/i.test(errorMessage(error));
}

function timestampLabel(value: string | undefined, locale: string): string {
  if (!value) {
    return "";
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return value;
  }
  return parsed.toLocaleString(locale, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

type Translate = ReturnType<typeof useI18n>["t"];

function memoryFileTypeLabel(type: string, t: Translate): string {
  const labels: Record<string, string> = {
    user: t("memory.typeUser"),
    feedback: t("memory.typeFeedback"),
    reference: t("memory.typeReference"),
    lesson: t("memory.typeLesson"),
  };
  return labels[type] ?? (type || "—");
}

function memoryChangedFileActionLabel(
  action: MemoryChangedFileAction,
  t: Translate,
): string {
  const labels: Record<MemoryChangedFileAction, string> = {
    created: t("memory.actionCreated"),
    modified: t("memory.actionModified"),
    deleted: t("memory.actionDeleted"),
  };
  return labels[action] ?? action;
}

export function MemoryPanel(): JSX.Element {
  const { t } = useI18n();
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
  const forceOverviewRefreshRef = useRef(false);

  useEffect(() => {
    setChatEntries([]);
    setChatDraft("");
  }, []);

  useEffect(() => {
    let cancelled = false;
    const forceRefresh = forceOverviewRefreshRef.current;
    forceOverviewRefreshRef.current = false;
    setOverview({ status: "loading" });
    void window.wuu
      .getMemoryOverview({
        scope: "user",
        ...(forceRefresh ? { force_refresh: true } : {}),
      })
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
  }, [refreshNonce]);

  function refreshOverview(forceRefresh = false): void {
    forceOverviewRefreshRef.current = forceRefresh;
    setRefreshNonce((nonce) => nonce + 1);
  }

  useEffect(() => {
    if (!rawOpen) {
      return;
    }
    let cancelled = false;
    setRaw({ status: "loading" });
    void window.wuu
      .readMemoryRaw({ scope: "user" })
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
  }, [rawOpen, refreshNonce]);

  async function submitChat(
    event: ReactFormEvent<HTMLFormElement>,
  ): Promise<void> {
    event.preventDefault();
    const text = chatDraft.trim();
    if (!text || chatPending) {
      return;
    }
    const id = `chat-${chatSeqRef.current + 1}`;
    chatSeqRef.current += 1;
    setChatEntries((entries) => [
      ...entries,
      { id, role: "user", text },
      { id: `${id}-pending`, role: "assistant", text: "", pending: true },
    ]);
    setChatDraft("");
    setChatPending(true);
    try {
      const result = await window.wuu.sendMemoryChat({
        scope: "user",
        message: text,
      });
      setChatEntries((entries) =>
        entries.map((entry) =>
          entry.id === `${id}-pending`
            ? {
                ...entry,
                pending: false,
                text: result.reply_md,
                changedFiles: result.changed_files,
              }
            : entry,
        ),
      );
      refreshOverview(true);
    } catch (error) {
      const message = isMemoryBackendMissing(error)
        ? t("memory.messageNotApplied")
        : errorMessage(error);
      setChatEntries((entries) =>
        entries.map((entry) =>
          entry.id === `${id}-pending`
            ? { ...entry, pending: false, error: message }
            : entry,
        ),
      );
    } finally {
      setChatPending(false);
    }
  }

  const canSendChat = chatDraft.trim().length > 0 && !chatPending;

  return (
    <ImagePreviewProvider>
      <div className="settings-memory">
        <header className="settings-page-header settings-memory-header">
          <h1 className="settings-page-title">{t("memory.title")}</h1>
          <div className="settings-memory-actions">
            <button
              className={`settings-button ${rawOpen ? "settings-button-secondary" : "settings-button-ghost"}`}
              type="button"
              onClick={() => setRawOpen((open) => !open)}
              data-testid="memory-toggle-raw"
            >
              {rawOpen ? t("memory.closeSource") : t("memory.viewSource")}
            </button>
            <button
              className="settings-button settings-button-secondary"
              type="button"
              data-testid="memory-refresh-overview"
              disabled={rawOpen || overview.status === "loading"}
              onClick={() => refreshOverview(true)}
            >
              {overview.status === "loading" ? (
                <Loader2
                  className="icon settings-memory-spinner"
                  aria-hidden="true"
                />
              ) : (
                <RefreshCw className="icon" aria-hidden="true" />
              )}
              <span>{overview.status === "loading" ? t("memory.summarizing") : t("memory.summarizeAgain")}</span>
            </button>
          </div>
        </header>

        <section className="settings-card settings-memory-card">
          {rawOpen ? (
            <MemoryRawView raw={raw} />
          ) : (
            <MemoryOverviewView
              overview={overview}
              onRetry={() => refreshOverview(false)}
            />
          )}
        </section>

        <section className="settings-card settings-memory-chat-card">
          <div className="settings-memory-chat-log">
            {chatEntries.length === 0 ? (
              <div className="settings-memory-chat-hint">
                {t("memory.chatHint")}
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
              placeholder={t("memory.chatPlaceholder")}
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
              <span>{t("memory.send")}</span>
            </button>
          </form>
        </section>
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
  const { locale, t } = useI18n();
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
          {t("memory.retry")}
        </button>
      </div>
    );
  }
  const { result } = overview;
  if (!result.essay_md.trim()) {
    return <div className="settings-empty">{t("memory.emptyNotebook")}</div>;
  }
  return (
    <div className="settings-memory-overview">
      <RichContent text={result.essay_md} />
      <div className="settings-memory-meta">
        <span>{t("memory.generatedAt", { time: timestampLabel(result.generated_at, locale) })}</span>
        {result.cached ? (
          <span
            className="settings-memory-cached"
            title={t("memory.cacheLifetime")}
          >
            {t("memory.cached")}
          </span>
        ) : null}
      </div>
    </div>
  );
}

function MemoryRawView({ raw }: { raw: RawState }): JSX.Element {
  const { locale, t } = useI18n();
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
    <div className="settings-memory-raw">
      <div className="settings-memory-file-table-wrap">
        {result.files.length > 0 ? (
          <table className="settings-memory-file-table">
            <thead>
              <tr>
                <th>{t("memory.fileName")}</th>
                <th>{t("memory.fileDescription")}</th>
                <th>{t("memory.fileType")}</th>
                <th>{t("memory.fileTime")}</th>
              </tr>
            </thead>
            <tbody>
              {result.files.map((file: MemoryFileInfo) => (
                <tr key={file.name}>
                  <td>
                    <code>{file.name}</code>
                  </td>
                  <td>{file.description}</td>
                  <td>{memoryFileTypeLabel(file.type, t)}</td>
                  <td>{timestampLabel(file.mtime, locale)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <p className="settings-empty">{t("memory.noFiles")}</p>
        )}
      </div>
      {result.index_md.trim() ? (
        <div className="settings-memory-index">
          <RichContent text={result.index_md} />
        </div>
      ) : (
        <p className="settings-empty">{t("memory.emptyIndex")}</p>
      )}
    </div>
  );
}

function MemoryChatEntryView({ entry }: { entry: ChatEntry }): JSX.Element {
  const { t } = useI18n();
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
          <span>{t("memory.organizing")}</span>
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
              <summary>{t("memory.changedFiles", { count: entry.changedFiles.length })}</summary>
              <ul>
                {entry.changedFiles.map((file) => (
                  <li key={file.path}>
                    <code>{file.path}</code>
                    <span className="settings-memory-change-action">
                      {memoryChangedFileActionLabel(file.action, t)}
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
  const { t } = useI18n();
  const widths = ["42%", "100%", "94%", "78%", "88%", "63%"];
  return (
    <div
      className="settings-memory-skeleton"
      role="status"
      aria-label={t("memory.loading")}
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
  const { t } = useI18n();
  return (
    <div
      className="settings-empty settings-memory-unavailable"
      data-testid="memory-backend-missing"
    >
      {t("memory.upgradeCore")}
    </div>
  );
}
