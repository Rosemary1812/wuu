import {
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
  Image as ImageIcon,
  Laptop,
  MessageSquarePlus,
  Plus,
  Search,
  X
} from "lucide-react";
import { type FormEvent as ReactFormEvent, type RefObject, useState } from "react";
import type { DesktopProject, GitStatusResult, InitializeResult, PlanUpdate, RuntimeContext } from "../shared/protocol";
import type { ComposerImage, QueuedComposerMessage } from "./ComposerMessages";
import { shortCodexModelLabel } from "./RuntimeHelpers";

export type EnvironmentPanelMenu = "mode" | "branch" | "sources" | null;
export type EnvironmentPanelMotionState = "open" | "closing";

export type EnvironmentSourceItem = {
  id: string;
  icon: "project" | "temporary" | "file" | "image" | "queue" | "guide";
  title: string;
  detail: string;
};

export function buildEnvironmentSourceItems({
  activeContext,
  activeProject,
  selectedWorkspaceFile,
  composerImages,
  queuedMessages,
  guideMessages
}: {
  activeContext?: RuntimeContext;
  activeProject?: DesktopProject;
  selectedWorkspaceFile?: string;
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
    items.push({
      id: "queued-messages",
      icon: "queue",
      title: "排队消息",
      detail: imageCount > 0 ? `${queuedMessages.length} 条，${imageCount} 张图片` : `${queuedMessages.length} 条`
    });
  }
  return items;
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
  onOpenPullRequest
}: {
  panelRef: RefObject<HTMLDivElement>;
  motionState: EnvironmentPanelMotionState;
  initialized: InitializeResult;
  gitStatus?: GitStatusResult;
  activeContext?: RuntimeContext;
  activeProject?: DesktopProject;
  planUpdate?: PlanUpdate;
  sourceItems: EnvironmentSourceItem[];
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
}): JSX.Element {
  const diff = gitStatus?.diff ?? { files: 0, additions: 0, deletions: 0 };
  const hasChanges = Boolean(gitStatus?.is_repo && (gitStatus.dirty_count > 0 || diff.files > 0));
  const branchLabel = gitStatus?.is_repo ? gitStatus.branch ?? "detached" : "非 Git 仓库";
  const contextLabel =
    activeContext?.kind === "project" ? activeProject?.name ?? "当前项目" : activeContext ? "临时对话" : "未连接";
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
            <X size={16} />
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
          <FolderPlus size={18} />
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
          {gitStatus?.is_repo ? <ChevronRight size={17} /> : null}
        </button>

        <button
          className={`environment-row${activeMenu === "mode" ? " active" : ""}`}
          type="button"
          onClick={() => toggleMenu("mode")}
        >
          <Laptop size={18} />
          <strong>本地</strong>
          <span>{contextLabel}</span>
          <ChevronRight size={17} />
        </button>

        <button
          className={`environment-row${activeMenu === "branch" ? " active" : ""}`}
          type="button"
          disabled={!gitStatus?.is_repo || running}
          onClick={() => toggleMenu("branch")}
        >
          <GitBranch size={18} />
          <strong>{branchLabel}</strong>
          <span>{gitStatus?.dirty_count ? `未提交：${gitStatus.dirty_count} 个文件` : ""}</span>
          {gitStatus?.is_repo ? <ChevronRight size={17} /> : null}
        </button>

        <button
          className="environment-row"
          type="button"
          disabled={!hasChanges || running}
          onClick={onOpenCommit}
        >
          <CornerDownRight size={18} />
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
          <Github size={18} />
          <strong>{gitStatus?.pr_url ? "查看拉取请求" : "创建拉取请求"}</strong>
          <span>{gitStatus?.pr_url ? "已有 PR" : prDisabled ? pullRequestDisabledReason : "推送并创建 PR"}</span>
        </button>
      </div>

      <button
        className={`environment-footer-row${activeMenu === "sources" ? " active" : ""}`}
        type="button"
        onClick={() => toggleMenu("sources")}
      >
        <span>来源 {sourceItems.length}</span>
        <ChevronRight size={17} />
      </button>

      <div className="environment-runtime-summary">
        <span>{initialized.provider}</span>
        <span>{shortCodexModelLabel(initialized.model)}</span>
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

function EnvironmentPlanSection({ planUpdate }: { planUpdate: PlanUpdate }): JSX.Element {
  const total = planUpdate.plan.length;
  const completed = planUpdate.plan.filter((item) => item.status === "completed").length;

  return (
    <section className="environment-plan-section" aria-label="任务进度">
      <div className="environment-plan-meta">
        <span>{completed}/{total}</span>
      </div>
      {planUpdate.explanation ? <p className="environment-plan-explanation">{planUpdate.explanation}</p> : null}
      <ol className="environment-plan-list">
        {planUpdate.plan.map((item, index) => (
          <li className={`environment-plan-item ${item.status}`} key={`${index}-${item.step}`}>
            <span className="environment-plan-marker" aria-hidden="true">
              {item.status === "completed" ? <Check size={11} strokeWidth={3} /> : null}
            </span>
            <span>{item.step}</span>
          </li>
        ))}
      </ol>
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
          <Folder size={17} />
          <span>{activeProject.name}</span>
          <Check size={17} />
        </button>
      ) : null}
      <button role="menuitem" type="button" onClick={onOpenProject}>
        <FolderOpen size={17} />
        <span>打开本地项目</span>
      </button>
      <button role="menuitem" type="button" disabled={activeContext?.kind === "no_project"} onClick={onSelectNoProject}>
        <FolderX size={17} />
        <span>临时对话</span>
        {activeContext?.kind === "no_project" ? <Check size={17} /> : null}
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
        <Search size={16} />
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
              <GitBranch size={17} />
              <span>{branch}</span>
              {selected ? <Check size={17} /> : null}
            </button>
          );
        })}
      </div>
      <form className="environment-create-branch" onSubmit={(event) => void submitNewBranch(event)}>
        <input value={newBranch} placeholder="新分支名称" onChange={(event) => setNewBranch(event.target.value)} />
        <button type="submit" disabled={!newBranch.trim() || submitting}>
          <Plus size={16} />
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
    return <Folder size={17} />;
  }
  if (item.icon === "temporary") {
    return <FolderX size={17} />;
  }
  if (item.icon === "file") {
    return <FileText size={17} />;
  }
  if (item.icon === "image") {
    return <ImageIcon size={17} />;
  }
  if (item.icon === "guide") {
    return <CornerDownRight size={17} />;
  }
  return <MessageSquarePlus size={17} />;
}
