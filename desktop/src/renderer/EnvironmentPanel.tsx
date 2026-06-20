import {
  Activity,
  Check,
  ChevronRight,
  CornerDownRight,
  FileText,
  Folder,
  FolderOpen,
  FolderPlus,
  FolderX,
  Github,
  GitBranch,
  Globe,
  Image as ImageIcon,
  Laptop,
  MessageSquarePlus,
  Plus,
  Search,
  Square,
  Terminal,
  X
} from "lucide-react";
import { type FormEvent as ReactFormEvent, type RefObject, useState } from "react";
import type {
  DesktopProject,
  GitStatusResult,
  InitializeResult,
  ManagedProcess,
  PlanUpdate,
  RuntimeContext,
  Thread
} from "../shared/protocol";
import type { ComposerFile, ComposerImage, QueuedComposerMessage } from "./ComposerMessages";
import { shortCodexModelLabel } from "./RuntimeHelpers";

export type EnvironmentPanelMenu = "mode" | "branch" | "sources" | null;
export type EnvironmentPanelMotionState = "open" | "closing";

export type EnvironmentSourceItem = {
  id: string;
  icon: "project" | "temporary" | "file" | "image" | "queue" | "guide";
  title: string;
  detail: string;
};

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

export function buildEnvironmentSourceItems({
  activeContext,
  activeProject,
  selectedWorkspaceFile,
  composerFiles,
  composerImages,
  queuedMessages,
  guideMessages
}: {
  activeContext?: RuntimeContext;
  activeProject?: DesktopProject;
  selectedWorkspaceFile?: string;
  composerFiles: ComposerFile[];
  composerImages: ComposerImage[];
  queuedMessages: QueuedComposerMessage[];
  guideMessages: QueuedComposerMessage[];
}): EnvironmentSourceItem[] {
  const items: EnvironmentSourceItem[] = [];
  if (activeContext?.kind === "project") {
    items.push({
      id: "project",
      icon: "project",
      title: activeProject?.name ?? "当前项目",
      detail: activeContext.cwd
    });
  } else if (activeContext?.kind === "no_project") {
    items.push({
      id: "temporary",
      icon: "temporary",
      title: "临时工作区",
      detail: activeContext.cwd
    });
  }
  if (selectedWorkspaceFile) {
    items.push({
      id: "selected-file",
      icon: "file",
      title: "当前文件",
      detail: selectedWorkspaceFile
    });
  }
  if (composerImages.length > 0) {
    items.push({
      id: "composer-images",
      icon: "image",
      title: "输入图片",
      detail: `${composerImages.length} 张`
    });
  }
  if (composerFiles.length > 0) {
    items.push({
      id: "composer-files",
      icon: "file",
      title: "输入文件",
      detail: `${composerFiles.length} 个`
    });
  }
  if (guideMessages.length > 0) {
    items.push({
      id: "guide-messages",
      icon: "guide",
      title: "下轮引导",
      detail: `${guideMessages.length} 条`
    });
  }
  if (queuedMessages.length > 0) {
    const imageCount = queuedMessages.reduce((count, message) => count + message.images.length, 0);
    const fileCount = queuedMessages.reduce((count, message) => count + message.files.length, 0);
    const detail = [
      `${queuedMessages.length} 条`,
      imageCount > 0 ? `${imageCount} 张图片` : "",
      fileCount > 0 ? `${fileCount} 个文件` : ""
    ]
      .filter(Boolean)
      .join("，");
    items.push({
      id: "queued-messages",
      icon: "queue",
      title: "排队消息",
      detail
    });
  }
  return items;
}

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
        if ((item.name === "start_process" || capability === "command.background") && item.status === "in_progress") {
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
        for (const process of processItemsFromToolResult(item.name, item.result)) {
          byID.delete(item.id);
          byID.set(process.id, process);
        }
      }
    }
  }
  for (const process of managedProcesses) {
    byID.set(process.id, processItemFromManagedProcess(process));
  }
  return [...byID.values()].sort(compareBackgroundProcesses);
}

export function backgroundProcessIsLive(process: BackgroundProcessItem): boolean {
  return process.status === "starting" || process.status === "running" || process.status === "stopping";
}

export function backgroundProcessNeedsAttention(process: BackgroundProcessItem): boolean {
  return process.status === "failed";
}

export function EnvironmentPanel({
  panelRef,
  motionState,
  initialized,
  gitStatus,
  activeContext,
  activeProject,
  planUpdate,
  sourceItems,
  backgroundProcesses,
  stoppingProcessIDs,
  activeMenu,
  running,
  pullRequestDisabledReason,
  onSetActiveMenu,
  onClose,
  onOpenProject,
  onSelectNoProject,
  onSelectBranch,
  onCreateBranch,
  onOpenReview,
  onOpenCommit,
  onOpenPullRequest,
  onStopBackgroundProcess,
  onOpenBackgroundPreview
}: {
  panelRef: RefObject<HTMLDivElement | null>;
  motionState: EnvironmentPanelMotionState;
  initialized: InitializeResult;
  gitStatus?: GitStatusResult;
  activeContext?: RuntimeContext;
  activeProject?: DesktopProject;
  planUpdate?: PlanUpdate;
  sourceItems: EnvironmentSourceItem[];
  backgroundProcesses: BackgroundProcessItem[];
  stoppingProcessIDs: Set<string>;
  activeMenu: EnvironmentPanelMenu;
  running: boolean;
  pullRequestDisabledReason: string;
  onSetActiveMenu: (menu: EnvironmentPanelMenu) => void;
  onClose: () => void;
  onOpenProject: () => void;
  onSelectNoProject: () => void;
  onSelectBranch: (branch: string) => void;
  onCreateBranch: (branch: string) => Promise<void>;
  onOpenReview: () => void;
  onOpenCommit: () => void;
  onOpenPullRequest: () => void;
  onStopBackgroundProcess: (process: BackgroundProcessItem) => void;
  onOpenBackgroundPreview: (process: BackgroundProcessItem) => void;
}): JSX.Element {
  const diff = gitStatus?.diff ?? { files: 0, additions: 0, deletions: 0 };
  const hasChanges = Boolean(gitStatus?.is_repo && (gitStatus.dirty_count > 0 || diff.files > 0));
  const branchLabel = gitStatus?.is_repo ? gitStatus.branch ?? "detached" : "非 Git 仓库";
  const contextLabel =
    activeContext?.kind === "project" ? activeProject?.name ?? "当前项目" : activeContext ? "临时对话" : "未连接";
  const profileLabel = initialized.model_profile?.profile_name || initialized.tool_surface?.profile_name || initialized.provider;
  const surfaceLabel = initialized.tool_surface?.edit_primitive
    ? `${initialized.tool_surface.edit_primitive}${initialized.tool_surface.bash_first ? " · bash" : ""}`
    : shortCodexModelLabel(initialized.model);
  const prDisabled = Boolean(pullRequestDisabledReason && !gitStatus?.pr_url);

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
        <h2>{planUpdate ? "进度" : "环境信息"}</h2>
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
          className={`environment-row${activeMenu === "mode" ? " active" : ""}`}
          type="button"
          onClick={() => toggleMenu("mode")}
        >
          <Laptop className="icon-lg" />
          <strong>本地</strong>
          <span>{contextLabel}</span>
          <ChevronRight className="icon" />
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

      <button
        className={`environment-footer-row${activeMenu === "sources" ? " active" : ""}`}
        type="button"
        onClick={() => toggleMenu("sources")}
      >
        <span>来源 {sourceItems.length}</span>
        <ChevronRight className="icon" />
      </button>

      <div className="environment-runtime-summary">
        <span>{profileLabel}</span>
        <span>{surfaceLabel}</span>
      </div>

      {activeMenu === "mode" ? (
        <EnvironmentModeMenu
          activeContext={activeContext}
          activeProject={activeProject}
          onOpenProject={onOpenProject}
          onSelectNoProject={onSelectNoProject}
        />
      ) : null}
      {activeMenu === "branch" && gitStatus?.is_repo ? (
        <EnvironmentBranchMenu
          gitStatus={gitStatus}
          onSelectBranch={onSelectBranch}
          onCreateBranch={onCreateBranch}
        />
      ) : null}
      {activeMenu === "sources" ? <EnvironmentSourcesMenu items={sourceItems} /> : null}
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
  return (
    <section className="environment-process-section" aria-label="后台任务">
      <div className="environment-process-heading">
        <span>
          <Activity className="icon" />
          后台任务
        </span>
        <span>{activeCount > 0 ? `${activeCount} 个活跃` : `${processes.length} 个最近任务`}</span>
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

function processItemsFromToolResult(name: string | undefined, result: string | undefined): BackgroundProcessItem[] {
  const record = parseJsonRecord(result);
  if (!record) {
    return [];
  }
  const action = stringValue(record, "action");
  if (name === "list_processes" || action === "list_processes" || action === "list_background") {
    const processes = Array.isArray(record.processes) ? record.processes : [];
    return processes.flatMap((item) => {
      const process = asRecord(item);
      return process ? processItemFromRecord(process) : [];
    });
  }
  if (
    name === "read_process_output" ||
    name === "write_stdin" ||
    action === "read_process_output" ||
    action === "read_background" ||
    action === "write_stdin"
  ) {
    const process = asRecord(record.process);
    return process ? processItemFromRecord(process) : [];
  }
  if (
    name === "start_process" ||
    name === "stop_process" ||
    action === "start_process" ||
    action === "start_background" ||
    action === "stop_process" ||
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
  const total = planUpdate.plan.length;
  const completed = planUpdate.plan.filter((item) => item.status === "completed").length;

  return (
    <section className="environment-plan-section" aria-label="任务进度">
      <div className="environment-plan-scroll">
        <div className="environment-plan-meta">
          <span>{completed}/{total}</span>
        </div>
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

function EnvironmentModeMenu({
  activeContext,
  activeProject,
  onOpenProject,
  onSelectNoProject
}: {
  activeContext?: RuntimeContext;
  activeProject?: DesktopProject;
  onOpenProject: () => void;
  onSelectNoProject: () => void;
}): JSX.Element {
  return (
    <div className="environment-side-menu mode" role="menu">
      <div className="environment-side-label">继续使用</div>
      {activeProject ? (
        <button role="menuitem" type="button" disabled>
          <Folder className="icon" />
          <span>{activeProject.name}</span>
          <Check className="icon" />
        </button>
      ) : null}
      <button role="menuitem" type="button" onClick={onOpenProject}>
        <FolderOpen className="icon" />
        <span>打开本地项目</span>
      </button>
      <button role="menuitem" type="button" disabled={activeContext?.kind === "no_project"} onClick={onSelectNoProject}>
        <FolderX className="icon" />
        <span>临时对话</span>
        {activeContext?.kind === "no_project" ? <Check className="icon" /> : null}
      </button>
    </div>
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

function EnvironmentSourcesMenu({ items }: { items: EnvironmentSourceItem[] }): JSX.Element {
  return (
    <div className="environment-side-menu sources" role="menu">
      <div className="environment-side-label">当前上下文</div>
      {items.length === 0 ? <div className="environment-empty">没有额外来源</div> : null}
      {items.map((item) => (
        <div className="environment-source-item" key={item.id}>
          <EnvironmentSourceIcon item={item} />
          <div>
            <strong>{item.title}</strong>
            <span>{item.detail}</span>
          </div>
        </div>
      ))}
    </div>
  );
}

function EnvironmentSourceIcon({ item }: { item: EnvironmentSourceItem }): JSX.Element {
  if (item.icon === "project") {
    return <Folder className="icon" />;
  }
  if (item.icon === "temporary") {
    return <FolderX className="icon" />;
  }
  if (item.icon === "file") {
    return <FileText className="icon" />;
  }
  if (item.icon === "image") {
    return <ImageIcon className="icon" />;
  }
  if (item.icon === "guide") {
    return <CornerDownRight className="icon" />;
  }
  return <MessageSquarePlus className="icon" />;
}
