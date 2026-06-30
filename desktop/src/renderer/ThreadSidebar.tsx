import { Archive, ChevronRight, Folder, FolderOpen, MessageSquarePlus, MessagesSquare, Pin } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import type { DesktopProject } from "../shared/protocol";
import { copyToClipboard, ThreadContextMenu } from "./ThreadContextMenu";
import { threadDisplayTitle } from "./ThreadTitles";
import { isThreadUnread, threadProjectPath, type ThreadSummary } from "./AppState";

function threadsForProjectPath(
  threads: ThreadSummary[],
  projectPath: string,
): ThreadSummary[] {
  return threads.filter((thread) =>
    sameSidebarPath(threadProjectPath(thread), projectPath),
  );
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
  const [visibleThreadCountsByProjectID, setVisibleThreadCountsByProjectID] = useState<Record<string, number>>({});

  function visibleThreadCountForProject(projectID: string): number {
    return visibleThreadCountsByProjectID[projectID] ?? PROJECT_THREAD_INITIAL_VISIBLE_COUNT;
  }

  function showMoreProjectThreads(projectID: string): void {
    setVisibleThreadCountsByProjectID((current) => ({
      ...current,
      [projectID]: (current[projectID] ?? PROJECT_THREAD_INITIAL_VISIBLE_COUNT) + PROJECT_THREAD_VISIBLE_INCREMENT
    }));
  }

  function collapseProjectThreads(projectID: string): void {
    setVisibleThreadCountsByProjectID((current) => {
      if (!(projectID in current)) return current;
      const next = { ...current };
      delete next[projectID];
      return next;
    });
  }

  return (
    <div className="projects">
      {projects.map((project) => {
        const pendingProject = pendingProjectID === project.id;
        const isScratchPseudo = project.id === scratchPseudoProjectID;
        const activeProject = isScratchPseudo
          ? scratchPseudoActive
          : project.id === activeID;
        const collapsed = collapsedProjectIDs.has(project.id);
        const collapsing = collapsingProjectIDs.has(project.id);
        const expanded =
          (expandedProjectIDs.has(project.id) || (activeProject && !collapsed)) &&
          !collapsing;
        const threadListMounted = expanded || collapsing;
        // The scratch pseudo project trusts the threadsByProjectID entry
        // directly: App.tsx already filtered scratch threads. Real
        // projects still go through the cwd-path filter so stale entries
        // can't leak into the wrong group.
        const projectThreads = isScratchPseudo
          ? threadsByProjectID[project.id] ?? []
          : threadsForProjectPath(
              threadsByProjectID[project.id] ?? [],
              project.path,
            );
        const projectHasUnread = projectThreads.some((thread) =>
          projectThreadUnread(
            thread,
            activeThreadID,
            pendingThreadID,
            lastViewedTurnByThreadID,
          ),
        );
        const projectRowClassName = `project-row ${activeProject ? "active" : ""}${expanded ? " expanded" : ""}${
          pendingProject ? " pending-switch" : ""
        }${projectHasUnread ? " has-unread" : ""}${isScratchPseudo ? " scratch-pseudo" : ""}`;
        return (
          <div key={project.id} className="project-group">
            <button
              className={projectRowClassName}
              aria-label={`${expanded ? "收起" : "展开"} ${project.name} 的会话${
                projectHasUnread ? "，有未读会话" : ""
              }`}
              aria-current={activeProject ? "page" : undefined}
              aria-expanded={expanded}
              aria-busy={pendingProject}
              title={expanded ? "收起会话" : "展开会话"}
              onClick={() => onToggleProjectCollapsed(project.id)}
            >
              {isScratchPseudo ? (
                <MessagesSquare className="icon-lg" aria-hidden="true" />
              ) : expanded ? (
                <FolderOpen className="icon-lg" />
              ) : (
                <Folder className="icon-lg" />
              )}
              <span className="project-row-label">
                <span className="project-row-name">{project.name}</span>
                <ChevronRight className="project-row-chevron icon" aria-hidden="true" />
              </span>
              {pendingProject ? <span className="project-row-loading" aria-hidden="true" /> : null}
              {projectHasUnread && !pendingProject ? (
                <span className="project-row-unread" aria-hidden="true" />
              ) : null}
            </button>
            <button
              className="sidebar-row-icon-button project-row-new-thread"
              type="button"
              aria-label={`在 ${project.name} 中新建会话`}
              title="新建会话"
              onClick={() => onStartNewThread(project.id)}
            >
              <MessageSquarePlus className="icon" />
            </button>
            {threadListMounted ? (
              <div className={`thread-list-collapse${collapsing ? " closing" : ""}`} aria-hidden={collapsing || undefined}>
                {projectThreads.length === 0 ? (
                  // Empty projects would otherwise render a 0-height .thread-list,
                  // which makes grid-template-rows animate 0 → 0 (invisible) and
                  // leaves the user with only a margin/opacity tail. Rendering
                  // a small note gives the wrapper real content to collapse, so
                  // the height animation matches what non-empty projects get.
                  <div className="project-thread-empty-note">还没有会话</div>
                ) : (
                  <ThreadList
                    threads={projectThreads}
                    activeID={activeThreadID}
                    pendingThreadID={pendingThreadID}
                    archiveConfirmThreadID={archiveConfirmThreadID}
                    lastViewedTurnByThreadID={lastViewedTurnByThreadID}
                    visibleCount={visibleThreadCountForProject(project.id)}
                    onSelect={(threadID) => onSelectThread(project.id, threadID)}
                    onTogglePinned={onToggleThreadPinned}
                    onArchive={onArchiveThread}
                    onClearArchiveConfirm={onClearArchiveConfirm}
                    onShowMore={() => showMoreProjectThreads(project.id)}
                    onCollapse={() => collapseProjectThreads(project.id)}
                  />
                )}
              </div>
            ) : null}
          </div>
        );
      })}
    </div>
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
        const title = threadDisplayTitle(thread, threads);
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
                aria-label={`${title}，${running ? "响应中" : "已完成"}`}
                onClick={() => onSelect(thread.id)}
              >
                <ThreadRowTitle title={title} />
              </button>
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
