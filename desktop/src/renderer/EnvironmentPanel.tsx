import {
  Activity,
  AlertCircle,
  Check,
  ChevronRight,
  CornerDownRight,
  FileText,
  FileX,
  FolderPlus,
  Github,
  GitBranch,
  Globe,
  Plus,
  Search,
  Square,
  Terminal,
  X
} from "lucide-react";
import { type FormEvent as ReactFormEvent, type RefObject, useEffect, useState } from "react";
import type {
  GitStatusResult,
  InitializeResult,
  ManagedProcess,
  PlanUpdate,
  Thread,
  WorkspaceFileReadResult
} from "../shared/protocol";
import { desktopApiErrorMessage, formatBytes } from "./WorkspaceReviewHelpers";

export type EnvironmentPanelMenu = "branch" | "file" | null;
export type EnvironmentPanelMotionState = "open" | "closing";

export type BackgroundProcessItem = {
  id: string;
  ownerID?: string;
  command: string;
  cwd?: string;
  lifecycle?: string;
  status: string;
  previewURLs?: string[];
  primaryPreviewURL?: string;
  startedAt?: string;
  updatedAt?: string;
  lastError?: string;
};

type JsonRecord = Record<string, unknown>;
export function buildBackgroundProcessItems(
  thread?: Thread,
  managedProcesses: ManagedProcess[] = []
): BackgroundProcessItem[] {
  const byID = new Map<string, BackgroundProcessItem>();
  if (thread) {
    for (const turn of thread.turns) {
      for (const item of turn.items) {
        if (item.type !== "tool_call" && item.type !== "collab_agent_tool_call") {
          continue;
        }
        const capability = item.display?.capability?.trim();
        if (capability === "command.background" && item.status === "in_progress") {
          const args = parseJsonRecord(item.arguments);
          const command = stringValue(args, "command");
          if (command) {
            byID.set(item.id, {
              id: item.id,
              command,
              cwd: stringValue(args, "cwd"),
              lifecycle: stringValue(args, "lifecycle") || "session",
              status: "starting"
            });
          }
        }
        for (const process of processItemsFromToolResult(item.result)) {
          byID.delete(item.id);
          byID.set(process.id, process);
        }
      }
    }
  }
  for (const process of managedProcesses) {
    byID.set(process.id, processItemFromManagedProcess(process));
  }
  return [...byID.values()].filter(backgroundProcessShouldDisplay).sort(compareBackgroundProcesses);
}

export function backgroundProcessIsLive(process: BackgroundProcessItem): boolean {
  return process.status === "starting" || process.status === "running" || process.status === "stopping";
}

export function backgroundProcessNeedsAttention(process: BackgroundProcessItem): boolean {
  return process.status === "failed";
}

function backgroundProcessShouldDisplay(process: BackgroundProcessItem): boolean {
  return backgroundProcessIsLive(process) || backgroundProcessNeedsAttention(process);
}

export function EnvironmentPanel({
  panelRef,
  motionState,
  initialized,
  gitStatus,
  planUpdate,
  backgroundProcesses,
  stoppingProcessIDs,
  activeMenu,
  running,
  pullRequestDisabledReason,
  onSetActiveMenu,
  onClose,
  onSelectBranch,
  onCreateBranch,
  onOpenReview,
  onOpenCommit,
  onOpenPullRequest,
  onStopBackgroundProcess,
  onOpenBackgroundPreview,
  rightPanelFilePath,
  onCloseFilePreview
}: {
  panelRef: RefObject<HTMLDivElement | null>;
  motionState: EnvironmentPanelMotionState;
  initialized: InitializeResult;
  gitStatus?: GitStatusResult;
  planUpdate?: PlanUpdate;
  backgroundProcesses: BackgroundProcessItem[];
  stoppingProcessIDs: Set<string>;
  activeMenu: EnvironmentPanelMenu;
  running: boolean;
  pullRequestDisabledReason: string;
  onSetActiveMenu: (menu: EnvironmentPanelMenu) => void;
  onClose: () => void;
  onSelectBranch: (branch: string) => void;
  onCreateBranch: (branch: string) => Promise<void>;
  onOpenReview: () => void;
  onOpenCommit: () => void;
  onOpenPullRequest: () => void;
  onStopBackgroundProcess: (process: BackgroundProcessItem) => void;
  onOpenBackgroundPreview: (process: BackgroundProcessItem) => void;
  /**
   * Absolute path to a workspace file the right panel should preview. When
   * present together with `activeMenu === "file"`, the panel swaps its
   * default environment body for a file viewer that reads the file via
   * `window.wuu.readWorkspaceFile`.
   */
  rightPanelFilePath?: string;
  /**
   * Invoked when the user closes the file preview from inside the panel.
   * Falls back to `onClose` (which dismisses the whole panel) when the
   * caller does not provide a file-specific closer.
   */
  onCloseFilePreview?: () => void;
}): JSX.Element {
  if (activeMenu === "file" && rightPanelFilePath) {
    return (
      <EnvironmentFilePreview
        filePath={rightPanelFilePath}
        onClose={onCloseFilePreview ?? onClose}
        panelRef={panelRef}
        motionState={motionState}
      />
    );
  }

  const diff = gitStatus?.diff ?? { files: 0, additions: 0, deletions: 0 };
  const hasChanges = Boolean(gitStatus?.is_repo && (gitStatus.dirty_count > 0 || diff.files > 0));
  const branchLabel = gitStatus?.is_repo ? gitStatus.branch ?? "detached" : "非 Git 仓库";
  const prDisabled = Boolean(pullRequestDisabledReason && !gitStatus?.pr_url);
  const planTotal = planUpdate?.plan.length ?? 0;
  const planCompleted = planUpdate?.plan.filter((item) => item.status === "completed").length ?? 0;

  function toggleMenu(menu: Exclude<EnvironmentPanelMenu, null>): void {
    onSetActiveMenu(activeMenu === menu ? null : menu);
  }

  return (
    <aside
      className={`environment-panel ${motionState}`}
      ref={panelRef}
      aria-label={planUpdate ? "进度与环境信息" : "环境信息"}
      aria-hidden={motionState === "closing" ? true : undefined}
    >
      <div className="environment-panel-header">
        <div className="environment-panel-title">
          {planUpdate ? (
            <>
              <h2>进度</h2>
              <span
                className="environment-panel-counter"
                aria-label={`已完成 ${planCompleted} 项，共 ${planTotal} 项`}
              >
                {planCompleted}/{planTotal}
              </span>
            </>
          ) : (
            <h2>环境信息</h2>
          )}
        </div>
        <div className="environment-panel-actions">
          <button className="icon-button" type="button" aria-label="关闭环境信息" onClick={onClose}>
            <X className="icon" />
          </button>
        </div>
      </div>

      {planUpdate ? <EnvironmentPlanSection planUpdate={planUpdate} /> : null}

      {planUpdate ? <div className="environment-section-heading">环境信息</div> : null}

      <div className="environment-panel-body">
        <button
          className="environment-row environment-change-row"
          type="button"
          disabled={!gitStatus?.is_repo}
          onClick={onOpenReview}
        >
          <FolderPlus className="icon-lg" />
          <strong>变更</strong>
          <span className="environment-row-meta">
            {gitStatus?.is_repo ? `${diff.files} 个文件` : "非 Git"}
            {gitStatus?.is_repo && diff.files > 0 ? (
              <span className="environment-diff">
                <span className="additions">+{diff.additions.toLocaleString()}</span>
                <span className="deletions">-{diff.deletions.toLocaleString()}</span>
              </span>
            ) : null}
          </span>
          {gitStatus?.is_repo ? <ChevronRight className="icon" /> : null}
        </button>

        <button
          className={`environment-row${activeMenu === "branch" ? " active" : ""}`}
          type="button"
          disabled={!gitStatus?.is_repo || running}
          onClick={() => toggleMenu("branch")}
        >
          <GitBranch className="icon-lg" />
          <strong>{branchLabel}</strong>
          <span>{gitStatus?.dirty_count ? `未提交：${gitStatus.dirty_count} 个文件` : ""}</span>
          {gitStatus?.is_repo ? <ChevronRight className="icon" /> : null}
        </button>

        <button
          className="environment-row"
          type="button"
          disabled={!hasChanges || running}
          onClick={onOpenCommit}
        >
          <CornerDownRight className="icon-lg" />
          <strong>提交</strong>
          <span>{hasChanges ? "提交当前更改" : "工作区干净"}</span>
        </button>

        <button
          className="environment-row"
          type="button"
          disabled={prDisabled || running}
          title={prDisabled ? pullRequestDisabledReason : undefined}
          onClick={onOpenPullRequest}
        >
          <Github className="icon-lg" />
          <strong>{gitStatus?.pr_url ? "查看拉取请求" : "创建拉取请求"}</strong>
          <span>{gitStatus?.pr_url ? "已有 PR" : prDisabled ? pullRequestDisabledReason : "推送并创建 PR"}</span>
        </button>

        {backgroundProcesses.length > 0 ? (
          <EnvironmentBackgroundProcesses
            processes={backgroundProcesses.slice(0, 5)}
            stoppingProcessIDs={stoppingProcessIDs}
            onStopProcess={onStopBackgroundProcess}
            onOpenPreview={onOpenBackgroundPreview}
          />
        ) : null}
      </div>

      {activeMenu === "branch" && gitStatus?.is_repo ? (
        <EnvironmentBranchMenu
          gitStatus={gitStatus}
          onSelectBranch={onSelectBranch}
          onCreateBranch={onCreateBranch}
        />
      ) : null}
    </aside>
  );
}

function EnvironmentBackgroundProcesses({
  processes,
  stoppingProcessIDs,
  onStopProcess,
  onOpenPreview
}: {
  processes: BackgroundProcessItem[];
  stoppingProcessIDs: Set<string>;
  onStopProcess: (process: BackgroundProcessItem) => void;
  onOpenPreview: (process: BackgroundProcessItem) => void;
}): JSX.Element {
  const activeCount = processes.filter(backgroundProcessIsLive).length;
  const failedCount = processes.filter(backgroundProcessNeedsAttention).length;
  const countLabel =
    activeCount > 0
      ? `${activeCount} 个活跃`
      : failedCount > 0
        ? `${failedCount} 个失败`
        : `${processes.length} 个任务`;
  return (
    <section className="environment-process-section" aria-label="后台任务">
      <div className="environment-process-heading">
        <span>
          <Activity className="icon" />
          后台任务
        </span>
        <span>{countLabel}</span>
      </div>
      <div className="environment-process-list">
        {processes.map((process) => (
          <div className="environment-process-row" key={process.id}>
            <Terminal className="icon" />
            <div>
              <strong title={process.command}>{process.command}</strong>
              <span>{processDetail(process)}</span>
            </div>
            <span className={`environment-process-status ${processStatusTone(process.status)}`}>
              {processStatusLabel(process.status)}
            </span>
            {process.primaryPreviewURL ? (
              <button
                className="environment-process-action"
                type="button"
                aria-label={`打开预览 ${process.primaryPreviewURL}`}
                title={process.primaryPreviewURL}
                onClick={() => onOpenPreview(process)}
              >
                <Globe className="icon-xs" />
              </button>
            ) : null}
            {processCanStop(process) ? (
              <button
                className="environment-process-action"
                type="button"
                aria-label={`停止 ${process.command}`}
                disabled={stoppingProcessIDs.has(process.id)}
                title="停止后台任务"
                onClick={() => onStopProcess(process)}
              >
                <Square className="icon-xs" />
              </button>
            ) : null}
          </div>
        ))}
      </div>
    </section>
  );
}

function processItemsFromToolResult(result: string | undefined): BackgroundProcessItem[] {
  const record = parseJsonRecord(result);
  if (!record) {
    return [];
  }
  const action = stringValue(record, "action");
  if (action === "list_background") {
    const processes = Array.isArray(record.processes) ? record.processes : [];
    return processes.flatMap((item) => {
      const process = asRecord(item);
      return process ? processItemFromRecord(process) : [];
    });
  }
  if (
    action === "read_background" ||
    action === "write_background"
  ) {
    const process = asRecord(record.process);
    return process ? processItemFromRecord(process) : [];
  }
  if (
    action === "start_background" ||
    action === "stop_background"
  ) {
    return processItemFromRecord(record);
  }
  const process = asRecord(record.process);
  if (process) {
    return processItemFromRecord(process);
  }
  return [];
}

function processItemFromRecord(record: JsonRecord): BackgroundProcessItem[] {
  const id = stringValue(record, "id");
  if (!id) {
    return [];
  }
  return [
    {
      id,
      ownerID: stringValue(record, "owner_id"),
      command: stringValue(record, "command") || id,
      cwd: stringValue(record, "cwd"),
      lifecycle: stringValue(record, "lifecycle"),
      status: stringValue(record, "status") || "running",
      previewURLs: stringArrayValue(record, "preview_urls"),
      primaryPreviewURL: stringValue(record, "primary_preview_url"),
      startedAt: stringValue(record, "started_at"),
      updatedAt: stringValue(record, "updated_at"),
      lastError: stringValue(record, "last_error")
    }
  ];
}

function processItemFromManagedProcess(process: ManagedProcess): BackgroundProcessItem {
  return {
    id: process.id,
    ownerID: process.owner_id,
    command: process.command || process.id,
    cwd: process.cwd,
    lifecycle: process.lifecycle,
    status: process.status || "running",
    previewURLs: process.preview_urls,
    primaryPreviewURL: process.primary_preview_url,
    startedAt: process.started_at,
    updatedAt: process.updated_at,
    lastError: process.last_error
  };
}

function processCanStop(process: BackgroundProcessItem): boolean {
  return process.id.startsWith("proc-") && (process.status === "starting" || process.status === "running");
}

function compareBackgroundProcesses(a: BackgroundProcessItem, b: BackgroundProcessItem): number {
  const status = processStatusRank(a.status) - processStatusRank(b.status);
  if (status !== 0) {
    return status;
  }
  return processTimestamp(b) - processTimestamp(a);
}

function processTimestamp(process: BackgroundProcessItem): number {
  const value = Date.parse(process.updatedAt || process.startedAt || "");
  return Number.isFinite(value) ? value : 0;
}

function processStatusRank(status: string): number {
  switch (processStatusTone(status)) {
    case "running":
      return 0;
    case "stopping":
      return 1;
    case "failed":
      return 2;
    case "stopped":
      return 3;
    default:
      return 4;
  }
}

function processStatusTone(status: string): "running" | "stopping" | "failed" | "stopped" {
  switch (status) {
    case "starting":
    case "running":
      return "running";
    case "stopping":
      return "stopping";
    case "failed":
      return "failed";
    case "stopped":
    default:
      return "stopped";
  }
}

function processStatusLabel(status: string): string {
  switch (status) {
    case "starting":
      return "启动中";
    case "running":
      return "运行中";
    case "stopping":
      return "停止中";
    case "failed":
      return "失败";
    case "stopped":
      return "已停止";
    default:
      return status || "未知";
  }
}

function processDetail(process: BackgroundProcessItem): string {
  if (process.status === "failed" && process.lastError) {
    return process.lastError;
  }
  const lifecycle =
    process.lifecycle === "managed" ? "需手动清理" : process.lifecycle === "session" ? "随会话清理" : "";
  const parts = [process.cwd, lifecycle].filter(Boolean);
  return parts.length > 0 ? parts.join(" · ") : process.id;
}

function parseJsonRecord(value: string | undefined): JsonRecord | undefined {
  if (!value) {
    return undefined;
  }
  try {
    return asRecord(JSON.parse(value));
  } catch {
    return undefined;
  }
}

function asRecord(value: unknown): JsonRecord | undefined {
  return typeof value === "object" && value !== null && !Array.isArray(value) ? (value as JsonRecord) : undefined;
}

function stringValue(record: JsonRecord | undefined, key: string): string {
  const value = record?.[key];
  return typeof value === "string" ? value : "";
}

function stringArrayValue(record: JsonRecord | undefined, key: string): string[] | undefined {
  const value = record?.[key];
  if (!Array.isArray(value)) {
    return undefined;
  }
  const items = value.filter((item): item is string => typeof item === "string" && item.length > 0);
  return items.length > 0 ? items : undefined;
}

function EnvironmentPlanSection({ planUpdate }: { planUpdate: PlanUpdate }): JSX.Element {
  return (
    <section className="environment-plan-section" aria-label="任务进度">
      <div className="environment-plan-scroll">
        {planUpdate.explanation ? <p className="environment-plan-explanation">{planUpdate.explanation}</p> : null}
        <ol className="environment-plan-list">
          {planUpdate.plan.map((item, index) => (
            <li className={`environment-plan-item ${item.status}`} key={`${index}-${item.step}`}>
              <span className="environment-plan-marker" aria-hidden="true">
                {item.status === "completed" ? <Check className="icon-xs" strokeWidth={3} /> : null}
              </span>
              <span>{item.step}</span>
            </li>
          ))}
        </ol>
      </div>
    </section>
  );
}

function EnvironmentBranchMenu({
  gitStatus,
  onSelectBranch,
  onCreateBranch
}: {
  gitStatus: GitStatusResult;
  onSelectBranch: (branch: string) => void;
  onCreateBranch: (branch: string) => Promise<void>;
}): JSX.Element {
  const [query, setQuery] = useState("");
  const [newBranch, setNewBranch] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const normalizedQuery = query.trim().toLocaleLowerCase();
  const branches = (gitStatus.branches ?? []).filter((branch) =>
    normalizedQuery ? branch.toLocaleLowerCase().includes(normalizedQuery) : true
  );

  async function submitNewBranch(event: ReactFormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    const branch = newBranch.trim();
    if (!branch || submitting) {
      return;
    }
    setSubmitting(true);
    setError("");
    try {
      await onCreateBranch(branch);
      setNewBranch("");
    } catch (createError) {
      setError(createError instanceof Error ? createError.message : "无法创建分支");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="environment-side-menu branch" role="menu">
      <label className="environment-search">
        <Search className="icon" />
        <input value={query} placeholder="搜索分支" onChange={(event) => setQuery(event.target.value)} />
      </label>
      {gitStatus.dirty_count > 0 ? (
        <div className="environment-side-note">未提交更改会跟随分支切换；如果会覆盖本地内容，Git 会拒绝。</div>
      ) : null}
      <div className="environment-branch-list">
        {branches.length === 0 ? <div className="environment-empty">没有匹配分支</div> : null}
        {branches.map((branch) => {
          const selected = branch === gitStatus.branch;
          return (
            <button key={branch} role="menuitem" type="button" disabled={selected} onClick={() => onSelectBranch(branch)}>
              <GitBranch className="icon" />
              <span>{branch}</span>
              {selected ? <Check className="icon" /> : null}
            </button>
          );
        })}
      </div>
      <form className="environment-create-branch" onSubmit={(event) => void submitNewBranch(event)}>
        <input value={newBranch} placeholder="新分支名称" onChange={(event) => setNewBranch(event.target.value)} />
        <button type="submit" disabled={!newBranch.trim() || submitting}>
          <Plus className="icon" />
        </button>
      </form>
      {error ? <div className="environment-side-error">{error}</div> : null}
    </div>
  );
}

/**
 * Right-panel body that replaces the default environment panel when
 * `activeMenu === "file"` and a `rightPanelFilePath` is supplied. Reads the
 * file via `window.wuu.readWorkspaceFile` and renders loading / error /
 * binary / text-file states. The header close button falls back to
 * `onClose` when the caller does not provide a file-specific closer.
 */
function EnvironmentFilePreview({
  filePath,
  onClose,
  panelRef,
  motionState
}: {
  filePath: string;
  onClose: () => void;
  panelRef: RefObject<HTMLDivElement | null>;
  motionState: EnvironmentPanelMotionState;
}): JSX.Element {
  const [file, setFile] = useState<WorkspaceFileReadResult | undefined>(undefined);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | undefined>(undefined);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(undefined);
    setFile(undefined);
    void window.wuu
      .readWorkspaceFile(filePath)
      .then((result) => {
        if (!cancelled) {
          setFile(result);
        }
      })
      .catch((e) => {
        if (!cancelled) {
          setError(desktopApiErrorMessage(e, "打开文件失败"));
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [filePath]);

  let body: JSX.Element;
  if (loading) {
    body = (
      <div className="environment-panel-body">
        <div className="environment-row environment-file-row">
          <FileText aria-hidden="true" className="icon-lg" />
          <strong>正在打开</strong>
          <span>{filePath}</span>
        </div>
      </div>
    );
  } else if (error) {
    body = (
      <div className="environment-panel-body">
        <div className="environment-row environment-file-row">
          <AlertCircle aria-hidden="true" className="icon-lg" />
          <strong>打开失败</strong>
          <span>{error}</span>
          <span className="environment-row-meta">{filePath}</span>
        </div>
      </div>
    );
  } else if (!file) {
    body = (
      <div className="environment-panel-body">
        <div className="environment-row environment-file-row">
          <FileText aria-hidden="true" className="icon-lg" />
          <strong>没有内容</strong>
          <span>{filePath}</span>
        </div>
      </div>
    );
  } else if (file.binary) {
    body = (
      <div className="environment-panel-body">
        <div className="environment-row environment-file-row">
          <FileX aria-hidden="true" className="icon-lg" />
          <strong>无法预览</strong>
          <span>{file.path} 是二进制文件</span>
        </div>
      </div>
    );
  } else {
    body = (
      <article className="workspace-file-preview">
        <header className="workspace-file-preview-header">
          <div>
            <strong>{file.path}</strong>
            <span>
              {formatBytes(file.size_bytes)}
              {file.truncated ? " · 仅显示前 512 KB" : ""}
            </span>
          </div>
          <button
            type="button"
            className="icon-button"
            onClick={onClose}
            aria-label="返回环境信息"
            title="返回"
          >
            <ChevronRight aria-hidden="true" className="icon" />
          </button>
        </header>
        <div className="workspace-file-code-scroll">
          <pre className="workspace-file-code">
            <code>{file.text}</code>
          </pre>
        </div>
      </article>
    );
  }

  return (
    <aside
      ref={panelRef}
      className={`environment-panel ${motionState}`}
      aria-label="文件预览"
      aria-hidden={motionState === "closing" ? true : undefined}
    >
      <div className="environment-panel-header">
        <h2>文件</h2>
        <div className="environment-panel-actions">
          <button
            className="icon-button"
            type="button"
            aria-label="返回环境信息"
            title="返回"
            onClick={onClose}
          >
            <X aria-hidden="true" className="icon" />
          </button>
        </div>
      </div>
      {body}
    </aside>
  );
}
