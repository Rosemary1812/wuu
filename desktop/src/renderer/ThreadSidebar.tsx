import { Archive, Folder, FolderOpen, GitFork, MessageSquare, MessageSquarePlus, MessagesSquare, Pin } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import type { DesktopProject } from "../shared/protocol";
import { copyToClipboard, ThreadContextMenu } from "./ThreadContextMenu";
import { SidebarSection } from "./SidebarSection";
import { baseThreadTitle, threadShowsForkMarker } from "./ThreadTitles";
import { isThreadRunning, isThreadUnread, threadProjectPath, type ThreadSummary } from "./AppState";

function threadsForProjectPath(
  threads: ThreadSummary[],
  projectPath: string,
): ThreadSummary[] {
  return threads.filter((thread) =>
    sameSidebarPath(threadProjectPath(thread), projectPath),
  );
}

function unpinnedThreads(threads: ThreadSummary[]): ThreadSummary[] {
  return threads.filter((thread) => !thread.pinned);
}

function sameSidebarPath(left: string, right: string): boolean {
  return cleanSidebarPath(left) === cleanSidebarPath(right);
}

function cleanSidebarPath(path: string): string {
  const trimmed = path.trim();
  const withoutTrailingSlash = trimmed.replace(/\/+$/, "");
  return withoutTrailingSlash || trimmed;
}

const PROJECT_THREAD_INITIAL_VISIBLE_COUNT = 8;
const PROJECT_THREAD_VISIBLE_INCREMENT = 10;

export function ProjectList({
  projects,
  activeID,
  pendingProjectID,
  collapsedProjectIDs,
  expandedProjectIDs,
  collapsingProjectIDs,
  threadsByProjectID,
  activeThreadID,
  pendingThreadID,
  archiveConfirmThreadID,
  lastViewedTurnByThreadID,
  scratchPseudoProjectID,
  scratchPseudoActive,
  onToggleProjectCollapsed,
  onStartNewThread,
  onSelectThread,
  onToggleThreadPinned,
  onArchiveThread,
  onDeleteThread,
  onRenameThread,
  onClearArchiveConfirm
}: {
  projects: DesktopProject[];
  activeID?: string;
  pendingProjectID?: string;
  collapsedProjectIDs: Set<string>;
  expandedProjectIDs: Set<string>;
  collapsingProjectIDs: Set<string>;
  threadsByProjectID: Record<string, ThreadSummary[]>;
  activeThreadID?: string;
  pendingThreadID?: string;
  archiveConfirmThreadID?: string;
  lastViewedTurnByThreadID: Record<string, string>;
  // The scratch pseudo project lives at the top of the sidebar tree and
  // groups all no-project (scratch) conversations under one collapsible
  // header, just like a real project. App.tsx injects a synthetic
  // DesktopProject with this id; AppSidebar passes it down so the row can
  // render a chat-bubble icon instead of a folder and so the row's
  // "active" highlight can be driven by the runtime context kind
  // (no_project), which is not represented in DesktopProject itself.
  scratchPseudoProjectID: string;
  scratchPseudoActive: boolean;
  onToggleProjectCollapsed: (id: string) => void;
  onStartNewThread: (id: string) => void;
  onSelectThread: (projectID: string, threadID: string) => void;
  onToggleThreadPinned: (thread: ThreadSummary) => void;
  onArchiveThread: (thread: ThreadSummary) => void;
  onDeleteThread: (thread: ThreadSummary) => void;
  onRenameThread?: (thread: ThreadSummary, title: string) => void;
  onClearArchiveConfirm: (threadID: string) => void;
}): JSX.Element {
  return (
    <div className="projects">
      {projects.map((project) => (
        <ProjectGroup
          key={project.id}
          project={project}
          activeID={activeID}
          pendingProjectID={pendingProjectID}
          collapsedProjectIDs={collapsedProjectIDs}
          expandedProjectIDs={expandedProjectIDs}
          collapsingProjectIDs={collapsingProjectIDs}
          threadsByProjectID={threadsByProjectID}
          activeThreadID={activeThreadID}
          pendingThreadID={pendingThreadID}
          archiveConfirmThreadID={archiveConfirmThreadID}
          lastViewedTurnByThreadID={lastViewedTurnByThreadID}
          scratchPseudoProjectID={scratchPseudoProjectID}
          scratchPseudoActive={scratchPseudoActive}
          onToggleProjectCollapsed={onToggleProjectCollapsed}
          onStartNewThread={onStartNewThread}
          onSelectThread={onSelectThread}
          onToggleThreadPinned={onToggleThreadPinned}
          onArchiveThread={onArchiveThread}
          onDeleteThread={onDeleteThread}
          onRenameThread={onRenameThread}
          onClearArchiveConfirm={onClearArchiveConfirm}
        />
      ))}
    </div>
  );
}

/**
 * Single project (or scratch pseudo) collapsible group. AppSidebar renders
 * one of these per reorderable section key. Visual/behavioral parity with
 * the legacy `ProjectList` map: the same `project-row` anatomy, the same
 * `thread-list-collapse` unfurl animation, the same unread/active classes.
 */
export function ProjectGroup({
  project,
  activeID,
  pendingProjectID,
  collapsedProjectIDs,
  expandedProjectIDs,
  collapsingProjectIDs,
  threadsByProjectID,
  activeThreadID,
  pendingThreadID,
  archiveConfirmThreadID,
  lastViewedTurnByThreadID,
  scratchPseudoProjectID,
  scratchPseudoActive,
  onToggleProjectCollapsed,
  onStartNewThread,
  onSelectThread,
  onToggleThreadPinned,
  onArchiveThread,
  onDeleteThread,
  onRenameThread,
  onClearArchiveConfirm,
  onRemoveProject,
  onRelocateProject,
}: {
  project: DesktopProject;
  activeID?: string;
  pendingProjectID?: string;
  collapsedProjectIDs: ReadonlySet<string>;
  expandedProjectIDs: ReadonlySet<string>;
  collapsingProjectIDs: ReadonlySet<string>;
  threadsByProjectID: Record<string, ThreadSummary[]>;
  activeThreadID?: string;
  pendingThreadID?: string;
  archiveConfirmThreadID?: string;
  lastViewedTurnByThreadID: Record<string, string>;
  scratchPseudoProjectID: string;
  scratchPseudoActive: boolean;
  onToggleProjectCollapsed: (id: string) => void;
  onStartNewThread: (id: string) => void;
  onSelectThread: (projectID: string, threadID: string) => void;
  onToggleThreadPinned: (thread: ThreadSummary) => void;
  onArchiveThread: (thread: ThreadSummary) => void;
  onDeleteThread: (thread: ThreadSummary) => void;
  onRenameThread?: (thread: ThreadSummary, title: string) => void;
  onClearArchiveConfirm: (threadID: string) => void;
  // Remove a real workspace from the sidebar. Absent for the 对话 scratch
  // pseudo project (and never wired for 群聊 / Agents, which are separate
  // sections), so those can never be removed.
  onRemoveProject?: (id: string) => void;
  // Point a real workspace at a new folder (keeping its stable id, so its
  // state and history reconnect). The remedy for a moved/deleted directory.
  onRelocateProject?: (id: string) => void;
}): JSX.Element {
  const [visibleThreadCount, setVisibleThreadCount] = useState<number>(
    PROJECT_THREAD_INITIAL_VISIBLE_COUNT,
  );
  const [contextMenu, setContextMenu] = useState<{
    x: number;
    y: number;
  } | null>(null);

  function showMoreProjectThreads(): void {
    setVisibleThreadCount(
      (current) => current + PROJECT_THREAD_VISIBLE_INCREMENT,
    );
  }

  function collapseProjectThreads(): void {
    setVisibleThreadCount(PROJECT_THREAD_INITIAL_VISIBLE_COUNT);
  }

  const pendingProject = pendingProjectID === project.id;
  const isScratchPseudo = project.id === scratchPseudoProjectID;
  // A real workspace whose directory was moved away or deleted. Its "新建会话"
  // affordance is disabled so no session can be created in a cwd that is gone.
  const isMissing = !isScratchPseudo && project.missing === true;
  const activeProject = isScratchPseudo
    ? scratchPseudoActive
    : project.id === activeID;
  const collapsed = collapsedProjectIDs.has(project.id);
  const collapsing = collapsingProjectIDs.has(project.id);
  const expanded =
    (expandedProjectIDs.has(project.id) || (activeProject && !collapsed)) &&
    !collapsing;
  // The scratch pseudo project trusts the threadsByProjectID entry
  // directly: App.tsx already filtered scratch threads. Real
  // projects still go through the cwd-path filter so stale entries
  // can't leak into the wrong group.
  const projectThreads = unpinnedThreads(
    isScratchPseudo
      ? threadsByProjectID[project.id] ?? []
      : threadsForProjectPath(
          threadsByProjectID[project.id] ?? [],
          project.path,
        ),
  );
  const projectHasUnread = projectThreads.some((thread) =>
    projectThreadUnread(
      thread,
      activeThreadID,
      pendingThreadID,
      lastViewedTurnByThreadID,
    ),
  );
  const CollapsedIcon = isScratchPseudo ? MessageSquare : Folder;
  const ExpandedIcon = isScratchPseudo ? MessagesSquare : FolderOpen;
  return (
    <div
      className={`project-group${isMissing ? " project-group-missing" : ""}`}
      data-section-id={project.id}
      data-missing={isMissing || undefined}
    >
      <SidebarSection
        expanded={expanded}
        iconKind={isScratchPseudo ? "conversation" : "project"}
        CollapsedIcon={CollapsedIcon}
        ExpandedIcon={ExpandedIcon}
        label={project.name}
        ariaLabel={`${expanded ? "收起" : "展开"} ${project.name} 的会话${
          projectHasUnread ? "，有未读会话" : ""
        }`}
        title={expanded ? "收起会话" : "展开会话"}
        active={activeProject}
        pending={pendingProject}
        unread={projectHasUnread}
        loading={pendingProject}
        onToggle={() => onToggleProjectCollapsed(project.id)}
        onContextMenu={
          isScratchPseudo || (!onRemoveProject && !onRelocateProject)
            ? undefined
            : (event) => {
                event.preventDefault();
                setContextMenu({ x: event.clientX, y: event.clientY });
              }
        }
        newItemButton={
          <button
            className="sidebar-row-icon-button project-row-new-thread"
            type="button"
            aria-label={`在 ${project.name} 中新建会话`}
            title={isMissing ? "工作区目录已不存在，无法新建会话" : "新建会话"}
            disabled={isMissing}
            onClick={() => onStartNewThread(project.id)}
          >
            <MessageSquarePlus className="icon" />
          </button>
        }
        emptyNote="还没有会话"
      >
        {projectThreads.length === 0 ? null : (
          <ThreadList
            threads={projectThreads}
            activeID={activeThreadID}
            pendingThreadID={pendingThreadID}
            archiveConfirmThreadID={archiveConfirmThreadID}
            lastViewedTurnByThreadID={lastViewedTurnByThreadID}
            visibleCount={visibleThreadCount}
            onSelect={(threadID) => onSelectThread(project.id, threadID)}
            onTogglePinned={onToggleThreadPinned}
            onArchive={onArchiveThread}
            onDelete={onDeleteThread}
            onRename={onRenameThread}
            onClearArchiveConfirm={onClearArchiveConfirm}
            onShowMore={showMoreProjectThreads}
            onCollapse={collapseProjectThreads}
          />
        )}
      </SidebarSection>
      {contextMenu ? (
        <ThreadContextMenu
          x={contextMenu.x}
          y={contextMenu.y}
          items={[
            {
              label: "重新定位…",
              onSelect: () => onRelocateProject?.(project.id),
            },
            {
              label: "移除工作区",
              onSelect: () => onRemoveProject?.(project.id),
            },
          ]}
          onClose={() => setContextMenu(null)}
        />
      ) : null}
    </div>
  );
}

/**
 * Renders the dual collapsed/expanded icon pair that lives inside every
 * section header (project, 对话/chat scratch, pinned, agents). The pair is
 * always rendered together; CSS crossfades between them via
 * `.project-row-icon-state` rules in sidebar.css. We render both icons
 * unconditionally rather than swapping them so the cross-fade transition stays
 * smooth without a mount/unmount flicker.
 *
 * Every pair follows the same single → many / closed → open metaphor:
 * Agents uses UserRound → UsersRound, 对话 uses MessageSquare →
 * MessagesSquare, projects use Folder → FolderOpen; the pinned section
 * keeps one Pin glyph and rotates the expanded state -45deg via CSS.
 */
export function SectionRowIcon({
  collapsed,
  iconKind,
  CollapsedIcon,
  ExpandedIcon,
}: {
  collapsed: boolean;
  iconKind: string;
  CollapsedIcon: React.ComponentType<{ className?: string }>;
  ExpandedIcon: React.ComponentType<{ className?: string }>;
}): JSX.Element {
  return (
    <span
      className={`project-row-icon${collapsed ? "" : " expanded"}`}
      aria-hidden="true"
    >
      <CollapsedIcon
        className="icon-lg project-row-icon-state collapsed"
        data-project-icon-kind={iconKind}
        data-project-icon-state="collapsed"
      />
      <ExpandedIcon
        className="icon-lg project-row-icon-state expanded"
        data-project-icon-kind={iconKind}
        data-project-icon-state="expanded"
      />
    </span>
  );
}

function ThreadList({
  threads,
  activeID,
  pendingThreadID,
  archiveConfirmThreadID,
  lastViewedTurnByThreadID,
  visibleCount,
  onSelect,
  onTogglePinned,
  onArchive,
  onDelete,
  onRename,
  onClearArchiveConfirm,
  onSidebarThreadHover,
  onShowMore,
  onCollapse
}: {
  threads: ThreadSummary[];
  activeID?: string;
  pendingThreadID?: string;
  archiveConfirmThreadID?: string;
  lastViewedTurnByThreadID: Record<string, string>;
  visibleCount: number;
  onSelect: (id: string) => void;
  onTogglePinned: (thread: ThreadSummary) => void;
  onArchive: (thread: ThreadSummary) => void;
  onDelete: (thread: ThreadSummary) => void;
  onRename?: (thread: ThreadSummary, title: string) => void;
  onClearArchiveConfirm: (threadID: string) => void;
  // Forwarded so ThreadRows can report which row is currently hovered; the
  // conversation pane owns the preview render so the card lives in the
  // message stream, not the sidebar DOM.
  onSidebarThreadHover?: (threadID: string | undefined) => void;
  onShowMore: () => void;
  onCollapse: () => void;
}): JSX.Element {
  const [stickyVisibleThreadIDs, setStickyVisibleThreadIDs] = useState<
    Set<string>
  >(() => new Set());
  const visibleThreads = threads;
  useEffect(() => {
    const validIDs = new Set(visibleThreads.map((thread) => thread.id));
    setStickyVisibleThreadIDs((current) => {
      const next = new Set<string>();
      for (const id of current) {
        if (validIDs.has(id)) {
          next.add(id);
        }
      }
      for (const thread of visibleThreads) {
        if (importantThreadVisible(thread, activeID, pendingThreadID)) {
          next.add(thread.id);
        }
      }
      return sameStringSet(current, next) ? current : next;
    });
  }, [activeID, pendingThreadID, visibleThreads]);
  const limitedThreads = limitedProjectThreads(
    visibleThreads,
    visibleCount,
    activeID,
    pendingThreadID,
    stickyVisibleThreadIDs,
  );
  const hiddenCount = visibleThreads.length - limitedThreads.length;
  const expanded = visibleCount > PROJECT_THREAD_INITIAL_VISIBLE_COUNT;
  const showFooter = hiddenCount > 0 || expanded;

  function collapseVisibleThreads(): void {
    setStickyVisibleThreadIDs(new Set());
    onCollapse();
  }

  return (
    <div className="thread-list">
      <ThreadRows
        threads={limitedThreads}
        activeID={activeID}
        pendingThreadID={pendingThreadID}
        archiveConfirmThreadID={archiveConfirmThreadID}
        lastViewedTurnByThreadID={lastViewedTurnByThreadID}
        onSelect={onSelect}
        onTogglePinned={onTogglePinned}
        onArchive={onArchive}
        onDelete={onDelete}
        onRename={onRename}
        onClearArchiveConfirm={onClearArchiveConfirm}
      />
      {showFooter ? (
        <div className="thread-list-footer">
          {hiddenCount > 0 ? (
            <button className="thread-list-more" type="button" onClick={onShowMore}>
              展开
            </button>
          ) : null}
          {expanded ? (
            <button
              className="thread-list-collapse-btn"
              type="button"
              onClick={collapseVisibleThreads}
              aria-label="收起已展开的会话"
              title="收起"
            >
              收起
            </button>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

function limitedProjectThreads(
  threads: ThreadSummary[],
  visibleCount: number,
  activeID: string | undefined,
  pendingThreadID: string | undefined,
  stickyVisibleThreadIDs: ReadonlySet<string> = new Set(),
): ThreadSummary[] {
  const visibleIDs = new Set(threads.slice(0, Math.max(0, visibleCount)).map((thread) => thread.id));
  return threads.filter((thread) => {
    if (
      visibleIDs.has(thread.id) ||
      stickyVisibleThreadIDs.has(thread.id) ||
      importantThreadVisible(thread, activeID, pendingThreadID)
    ) {
      return true;
    }
    return false;
  });
}

function sameStringSet(left: ReadonlySet<string>, right: ReadonlySet<string>): boolean {
  if (left.size !== right.size) {
    return false;
  }
  for (const value of left) {
    if (!right.has(value)) {
      return false;
    }
  }
  return true;
}

function importantThreadVisible(
  thread: ThreadSummary,
  activeID: string | undefined,
  pendingThreadID: string | undefined
): boolean {
  return (
    thread.id === activeID ||
    thread.id === pendingThreadID ||
    isThreadRunning(thread)
  );
}

function projectThreadUnread(
  thread: ThreadSummary,
  activeID: string | undefined,
  pendingThreadID: string | undefined,
  lastViewedTurnByThreadID: Record<string, string>,
): boolean {
  return (
    !isThreadRunning(thread) &&
    thread.id !== activeID &&
    thread.id !== pendingThreadID &&
    isThreadUnread(thread, lastViewedTurnByThreadID[thread.id])
  );
}

function ThreadRows({
  threads,
  activeID,
  pendingThreadID,
  archiveConfirmThreadID,
  lastViewedTurnByThreadID,
  onSelect,
  onTogglePinned,
  onArchive,
  onDelete,
  onRename,
  onClearArchiveConfirm,
  onSidebarThreadHover,
}: {
  threads: ThreadSummary[];
  activeID?: string;
  pendingThreadID?: string;
  archiveConfirmThreadID?: string;
  lastViewedTurnByThreadID: Record<string, string>;
  onSelect: (id: string) => void;
  onTogglePinned: (thread: ThreadSummary) => void;
  onArchive: (thread: ThreadSummary) => void;
  onDelete: (thread: ThreadSummary) => void;
  onRename?: (thread: ThreadSummary, title: string) => void;
  onClearArchiveConfirm: (threadID: string) => void;
  // Fires when a row is hovered/unhovered so the conversation pane can
  // render a "what did this thread actually do" preview without the card
  // living inside the sidebar DOM (which would put it on top of the left
  // panel rather than in the message stream).
  onSidebarThreadHover?: (threadID: string | undefined) => void;
}): JSX.Element {
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number; thread: ThreadSummary } | null>(null);
  const [renameDialog, setRenameDialog] = useState<{
    thread: ThreadSummary;
    initialTitle: string;
  } | null>(null);
  const [renameTitle, setRenameTitle] = useState("");

  function handleContextMenu(
    targetThread: ThreadSummary,
    e: { clientX: number; clientY: number; preventDefault: () => void }
  ): void {
    e.preventDefault();
    setContextMenu({ x: e.clientX, y: e.clientY, thread: targetThread });
  }

  function editableThreadTitle(thread: ThreadSummary): string {
    return thread.title?.trim() || thread.preview?.trim() || "";
  }

  function openRenameDialog(thread: ThreadSummary): void {
    if (!onRename) {
      return;
    }
    const title = editableThreadTitle(thread);
    setContextMenu(null);
    setRenameTitle(title);
    setRenameDialog({ thread, initialTitle: title });
  }

  function closeRenameDialog(): void {
    setRenameDialog(null);
    setRenameTitle("");
  }

  function submitRenameDialog(): void {
    if (!renameDialog) {
      return;
    }
    const trimmed = renameTitle.trim();
    if (trimmed && trimmed !== renameDialog.initialTitle.trim()) {
      onRename?.(renameDialog.thread, trimmed);
    }
    closeRenameDialog();
  }

  return (
    <>
      {threads.map((thread) => {
        const archiveConfirming = archiveConfirmThreadID === thread.id;
        const pendingSwitch = pendingThreadID === thread.id;
        const running = isThreadRunning(thread);
        const title = baseThreadTitle(thread, threads);
        const forkMarker = threadShowsForkMarker(thread, threads);
        const unread =
          !running &&
          !pendingSwitch &&
          thread.id !== activeID &&
          isThreadUnread(thread, lastViewedTurnByThreadID[thread.id]);
        return (
          <div
            key={thread.id}
            className={`thread-row ${thread.id === activeID ? "active" : ""}${running ? " running" : ""}${
              pendingSwitch ? " pending-switch" : ""
            }${unread ? " has-unread" : ""}`}
            aria-current={thread.id === activeID ? "page" : undefined}
            onMouseLeave={() => onClearArchiveConfirm(thread.id)}
            onContextMenu={(e) => handleContextMenu(thread, e)}
          >
              {running ? (
                <span className="thread-row-spinner" aria-hidden="true" />
              ) : null}
              <button
                className="thread-row-main"
                type="button"
                aria-busy={pendingSwitch || running}
                aria-label={`${title}${forkMarker ? "，分叉自其他会话" : ""}，${running ? "响应中" : "已完成"}`}
                onClick={() => onSelect(thread.id)}
                onDoubleClick={() => openRenameDialog(thread)}
              >
                <ThreadRowTitle title={title} />
              </button>
              {forkMarker ? (
                <GitFork
                  className="icon-sm thread-row-fork-icon"
                  aria-hidden="true"
                />
              ) : null}
              <div className="thread-row-actions" aria-label="对话操作">
                <button
                  className={`sidebar-row-icon-button thread-row-action ${thread.pinned ? "active" : ""}`}
                  type="button"
                  aria-label={thread.pinned ? "取消置顶" : "置顶"}
                  title={thread.pinned ? "取消置顶" : "置顶"}
                  onClick={() => onTogglePinned(thread)}
                >
                  <Pin className="icon-sm" />
                </button>
                <button
                  className={`sidebar-row-icon-button thread-row-action archive ${archiveConfirming ? "confirm" : ""}`}
                  type="button"
                  aria-label={archiveConfirming ? "确认归档" : "归档"}
                  title={archiveConfirming ? "再次点击归档" : "归档"}
                  onClick={() => onArchive(thread)}
                >
                  <Archive className="icon-sm" />
                </button>
              </div>
          </div>
        );
      })}
      {contextMenu ? (
        <ThreadContextMenu
          x={contextMenu.x}
          y={contextMenu.y}
          items={[
            {
              label: contextMenu.thread.pinned ? "取消置顶" : "置顶",
              onSelect: () => onTogglePinned(contextMenu.thread),
            },
            {
              label: "重命名对话",
              disabled: !onRename,
              onSelect: () => openRenameDialog(contextMenu.thread),
            },
            {
              label: "复制工作目录",
              onSelect: () => {
                void copyToClipboard(contextMenu.thread.cwd);
              },
            },
            {
              label: "在 Finder 中显示",
              onSelect: () => {
                void window.wuu.revealSession(contextMenu.thread.id);
              },
            },
            {
              label: "复制会话 ID",
              onSelect: () => {
                void copyToClipboard(contextMenu.thread.id);
              },
            },
            { separator: true },
            {
              label: "删除对话",
              // A running thread cannot be deleted (the server also rejects
              // it); disable the entry so the confirm dialog never promises
              // a deletion that will fail.
              disabled: isThreadRunning(contextMenu.thread),
              onSelect: () => {
                if (!window.confirm("删除后不可恢复，对话与工件将被清除")) {
                  return;
                }
                onDelete(contextMenu.thread);
              },
            },
          ]}
          onClose={() => setContextMenu(null)}
        />
      ) : null}
      {renameDialog ? (
        <ThreadRenameDialog
          title={renameTitle}
          onTitleChange={setRenameTitle}
          onClose={closeRenameDialog}
          onSubmit={submitRenameDialog}
        />
      ) : null}
    </>
  );
}

function ThreadRenameDialog({
  title,
  onTitleChange,
  onClose,
  onSubmit,
}: {
  title: string;
  onTitleChange: (title: string) => void;
  onClose: () => void;
  onSubmit: () => void;
}): JSX.Element {
  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent): void {
      if (event.key !== "Escape") {
        return;
      }
      event.preventDefault();
      onClose();
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [onClose]);

  return createPortal(
    <div
      className="conversation-search-overlay thread-rename-overlay"
      onPointerDown={(event) => {
        if (event.target === event.currentTarget) {
          onClose();
        }
      }}
    >
      <form
        className="conversation-search-dialog thread-rename-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="thread-rename-title"
        onSubmit={(event) => {
          event.preventDefault();
          onSubmit();
        }}
      >
        <div className="thread-rename-header">
          <span className="thread-rename-icon" aria-hidden="true">
            <MessageSquare className="icon-lg" />
          </span>
          <h2 id="thread-rename-title" className="thread-rename-title">
            重命名对话
          </h2>
        </div>
        <label className="thread-rename-field">
          <span className="thread-rename-label">会话标题</span>
          <input
            className="thread-rename-input"
            value={title}
            aria-label="会话标题"
            placeholder="会话标题"
            autoFocus
            onChange={(event) => onTitleChange(event.currentTarget.value)}
            onFocus={(event) => event.currentTarget.select()}
          />
        </label>
        <div className="thread-rename-actions">
          <button type="button" onClick={onClose}>
            取消
          </button>
          <button type="submit" disabled={title.trim().length === 0}>
            保存
          </button>
        </div>
      </form>
    </div>,
    document.body,
  );
}

export function PinnedThreadList({
  threads,
  activeID,
  pendingThreadID,
  archiveConfirmThreadID,
  lastViewedTurnByThreadID,
  onSelect,
  onTogglePinned,
  onArchive,
  onDelete,
  onRename,
  onClearArchiveConfirm,
}: {
  threads: ThreadSummary[];
  activeID?: string;
  pendingThreadID?: string;
  archiveConfirmThreadID?: string;
  lastViewedTurnByThreadID: Record<string, string>;
  onSelect: (id: string) => void;
  onTogglePinned: (thread: ThreadSummary) => void;
  onArchive: (thread: ThreadSummary) => void;
  onDelete: (thread: ThreadSummary) => void;
  onRename?: (thread: ThreadSummary, title: string) => void;
  onClearArchiveConfirm: (threadID: string) => void;
}): JSX.Element {
  return (
    <div className="pinned-thread-list">
      <ThreadRows
        threads={threads}
        activeID={activeID}
        pendingThreadID={pendingThreadID}
        archiveConfirmThreadID={archiveConfirmThreadID}
        lastViewedTurnByThreadID={lastViewedTurnByThreadID}
        onSelect={onSelect}
        onTogglePinned={onTogglePinned}
        onArchive={onArchive}
        onDelete={onDelete}
        onRename={onRename}
        onClearArchiveConfirm={onClearArchiveConfirm}
      />
    </div>
  );
}

/**
 * ThreadRowTitle renders the sidebar title with a soft crossfade whenever the
 * displayed text changes (typically when the LLM-generated title replaces the
 * fallback preview after the first turn completes).
 *
 * Design intent: the streaming-state visual and the post-stable visual should
 * not switch abruptly. A pure DOM-text swap reads as a flicker because the
 * fallback (first user query, often long) and the final title (short,
 * grammar-normalized) are visually very different. We crossfade between them
 * with a key remount so the user perceives a settle, not a snap.
 *
 * The first appearance of a title does not animate — only swaps after the
 * component has been mounted with prior content. This avoids the entire
 * sidebar fading in on project switch / cold boot, which would itself feel
 * like a loading state.
 */
export function ThreadRowTitle({ title }: { title: string }): JSX.Element {
  const previousTitleRef = useRef(title);
  const swapCountRef = useRef(0);
  if (previousTitleRef.current !== title) {
    previousTitleRef.current = title;
    swapCountRef.current += 1;
  }
  const hasSwapped = swapCountRef.current > 0;
  return (
    <span
      className="thread-row-title"
      data-title-swap={hasSwapped ? swapCountRef.current : undefined}
      key={swapCountRef.current}
    >
      {title}
    </span>
  );
}
