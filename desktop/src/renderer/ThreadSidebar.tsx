import { Archive, Folder, FolderOpen, GitFork, MessageSquare, MessageSquarePlus, MessagesSquare, Pin } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import type { DesktopProject } from "../shared/protocol";
import { copyToClipboard, ThreadContextMenu } from "./ThreadContextMenu";
import { SidebarSection } from "./SidebarSection";
import { baseThreadTitle, threadShowsForkMarker } from "./ThreadTitles";
import { isThreadUnread, threadProjectPath, type ThreadSummary } from "./AppState";

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
  onClearArchiveConfirm,
  onRemoveProject,
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
  onClearArchiveConfirm: (threadID: string) => void;
  // Remove a real workspace from the sidebar. Absent for the 对话 scratch
  // pseudo project (and never wired for 群聊 / Agents, which are separate
  // sections), so those can never be removed.
  onRemoveProject?: (id: string) => void;
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
          isScratchPseudo || !onRemoveProject
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
    threadRunning(thread)
  );
}

function threadRunning(thread: ThreadSummary): boolean {
  return thread.status === "in_progress" || thread.turns.some((turn) => turn.status === "in_progress");
}

function projectThreadUnread(
  thread: ThreadSummary,
  activeID: string | undefined,
  pendingThreadID: string | undefined,
  lastViewedTurnByThreadID: Record<string, string>,
): boolean {
  return (
    !threadRunning(thread) &&
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
  onClearArchiveConfirm: (threadID: string) => void;
  // Fires when a row is hovered/unhovered so the conversation pane can
  // render a "what did this thread actually do" preview without the card
  // living inside the sidebar DOM (which would put it on top of the left
  // panel rather than in the message stream).
  onSidebarThreadHover?: (threadID: string | undefined) => void;
}): JSX.Element {
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number; thread: ThreadSummary } | null>(null);

  function handleContextMenu(
    targetThread: ThreadSummary,
    e: { clientX: number; clientY: number; preventDefault: () => void }
  ): void {
    e.preventDefault();
    setContextMenu({ x: e.clientX, y: e.clientY, thread: targetThread });
  }

  return (
    <>
      {threads.map((thread) => {
        const archiveConfirming = archiveConfirmThreadID === thread.id;
        const pendingSwitch = pendingThreadID === thread.id;
        const running = threadRunning(thread);
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
              onSelect: () => {
                const current =
                  contextMenu.thread.title?.trim() ||
                  contextMenu.thread.preview?.trim() ||
                  "";
                const next = window.prompt("新标题", current);
                if (next === null) return; // user cancelled
                const trimmed = next.trim();
                if (!trimmed || trimmed === current) return;
                // Fire-and-forget; the server will eventually send a
                // thread/updated notification that the renderer can use to
                // refresh the title. No need to manually refetch here.
                void window.wuu.renameThread(contextMenu.thread.id, trimmed);
              },
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
          ]}
          onClose={() => setContextMenu(null)}
        />
      ) : null}
    </>
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
