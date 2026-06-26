import { Archive, ChevronRight, CornerDownRight, Folder, FolderOpen, MessageSquarePlus, Pin } from "lucide-react";
import { Fragment, useRef, useState } from "react";
import type { Agent, DesktopProject, Thread } from "../shared/protocol";
import { copyToClipboard, ThreadContextMenu } from "./ThreadContextMenu";
import {
  agentLabel,
  agentNestedLabel,
  agentStatusLabel,
  agentStatusTone,
  agentTooltip,
  sortChildAgents
} from "./ThreadAgents";
import { threadDisplayTitle } from "./ThreadTitles";
import { isThreadUnread } from "./AppState";

function projectThreads(threads: Thread[]): Thread[] {
  return threads.filter((thread) => !thread.pinned);
}

const PROJECT_THREAD_INITIAL_VISIBLE_COUNT = 8;
const PROJECT_THREAD_VISIBLE_INCREMENT = 10;

export function ProjectList({
  projects,
  activeID,
  pendingProjectID,
  collapsedProjectIDs,
  collapsingProjectIDs,
  threads,
  activeThreadID,
  pendingThreadID,
  archiveConfirmThreadID,
  lastViewedTurnByThreadID,
  onSelectProject,
  onToggleProjectCollapsed,
  onStartNewThread,
  onSelectThread,
  onSelectChildAgent,
  onToggleThreadPinned,
  onArchiveThread,
  onClearArchiveConfirm
}: {
  projects: DesktopProject[];
  activeID?: string;
  pendingProjectID?: string;
  collapsedProjectIDs: Set<string>;
  collapsingProjectIDs: Set<string>;
  threads: Thread[];
  activeThreadID?: string;
  pendingThreadID?: string;
  archiveConfirmThreadID?: string;
  lastViewedTurnByThreadID: Record<string, string>;
  onSelectProject: (id: string) => void;
  onToggleProjectCollapsed: (id: string) => void;
  onStartNewThread: (id: string) => void;
  onSelectThread: (id: string) => void;
  onSelectChildAgent: (agent: Agent) => void;
  onToggleThreadPinned: (thread: Thread) => void;
  onArchiveThread: (thread: Thread) => void;
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
        const activeProject = project.id === activeID;
        const collapsed = collapsedProjectIDs.has(project.id);
        const collapsing = collapsingProjectIDs.has(project.id);
        const expanded = activeProject && !collapsed && !collapsing;
        const threadListMounted = activeProject && (!collapsed || collapsing);
        const projectRowClassName = `project-row ${activeProject ? "active" : ""}${expanded ? " expanded" : ""}${
          pendingProject ? " pending-switch" : ""
        }`;
        return (
          <div key={project.id} className="project-group">
            <button
              className={projectRowClassName}
              aria-label={activeProject ? `${expanded ? "收起" : "展开"} ${project.name} 的会话` : `打开 ${project.name}`}
              aria-current={activeProject ? "page" : undefined}
              aria-expanded={activeProject ? expanded : undefined}
              aria-busy={pendingProject}
              title={activeProject ? (expanded ? "收起会话" : "展开会话") : "打开项目"}
              onClick={() => (activeProject ? onToggleProjectCollapsed(project.id) : onSelectProject(project.id))}
            >
              <ChevronRight className="project-row-chevron icon" aria-hidden="true" />
              {expanded ? <FolderOpen className="icon-lg" /> : <Folder className="icon-lg" />}
              <span>{project.name}</span>
              {pendingProject ? <span className="project-row-loading" aria-hidden="true" /> : null}
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
                <ThreadList
                  threads={threads}
                  activeID={activeThreadID}
                  pendingThreadID={pendingThreadID}
                  archiveConfirmThreadID={archiveConfirmThreadID}
                  lastViewedTurnByThreadID={lastViewedTurnByThreadID}
                  visibleCount={visibleThreadCountForProject(project.id)}
                  onSelect={onSelectThread}
                  onSelectChildAgent={onSelectChildAgent}
                  onTogglePinned={onToggleThreadPinned}
                  onArchive={onArchiveThread}
                  onClearArchiveConfirm={onClearArchiveConfirm}
                  onShowMore={() => showMoreProjectThreads(project.id)}
                  onCollapse={() => collapseProjectThreads(project.id)}
                />
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
  onSelectChildAgent,
  onTogglePinned,
  onArchive,
  onClearArchiveConfirm,
  onSidebarThreadHover,
  onShowMore,
  onCollapse
}: {
  threads: Thread[];
  activeID?: string;
  pendingThreadID?: string;
  archiveConfirmThreadID?: string;
  lastViewedTurnByThreadID: Record<string, string>;
  visibleCount: number;
  onSelect: (id: string) => void;
  onSelectChildAgent: (agent: Agent) => void;
  onTogglePinned: (thread: Thread) => void;
  onArchive: (thread: Thread) => void;
  onClearArchiveConfirm: (threadID: string) => void;
  // Forwarded so ThreadRows can report which row is currently hovered; the
  // conversation pane owns the preview render so the card lives in the
  // message stream, not the sidebar DOM.
  onSidebarThreadHover?: (threadID: string | undefined) => void;
  onShowMore: () => void;
  onCollapse: () => void;
}): JSX.Element {
  const visibleThreads = projectThreads(threads);
  const limitedThreads = limitedProjectThreads(visibleThreads, visibleCount, activeID, pendingThreadID);
  const hiddenCount = visibleThreads.length - limitedThreads.length;
  const showMoreCount = Math.min(PROJECT_THREAD_VISIBLE_INCREMENT, hiddenCount);
  const expanded = visibleCount > PROJECT_THREAD_INITIAL_VISIBLE_COUNT;
  const showFooter = hiddenCount > 0 || expanded;
  return (
    <div className="thread-list">
      <ThreadRows
        threads={limitedThreads}
        activeID={activeID}
        pendingThreadID={pendingThreadID}
        archiveConfirmThreadID={archiveConfirmThreadID}
        lastViewedTurnByThreadID={lastViewedTurnByThreadID}
        onSelect={onSelect}
        onSelectChildAgent={onSelectChildAgent}
        onTogglePinned={onTogglePinned}
        onArchive={onArchive}
        onClearArchiveConfirm={onClearArchiveConfirm}
      />
      {showFooter ? (
        <div className="thread-list-footer">
          {hiddenCount > 0 ? (
            <button className="thread-list-more" type="button" onClick={onShowMore}>
              <span>{hiddenCount > PROJECT_THREAD_VISIBLE_INCREMENT ? `再显示 ${showMoreCount} 条` : `显示剩余 ${hiddenCount} 条`}</span>
              <span className="thread-list-more-count">剩余 {hiddenCount} 条</span>
            </button>
          ) : null}
          {expanded ? (
            <button
              className="thread-list-collapse-btn"
              type="button"
              onClick={onCollapse}
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
  threads: Thread[],
  visibleCount: number,
  activeID: string | undefined,
  pendingThreadID: string | undefined
): Thread[] {
  const visibleIDs = new Set(threads.slice(0, Math.max(0, visibleCount)).map((thread) => thread.id));
  return threads.filter((thread) => {
    if (visibleIDs.has(thread.id) || importantThreadVisible(thread, activeID, pendingThreadID)) {
      return true;
    }
    return false;
  });
}

function importantThreadVisible(
  thread: Thread,
  activeID: string | undefined,
  pendingThreadID: string | undefined
): boolean {
  if (thread.id === activeID || thread.id === pendingThreadID || threadRunning(thread)) {
    return true;
  }
  return (thread.child_agents ?? []).some(
    (agent) => agent.id === activeID || agent.id === pendingThreadID
  );
}

function threadRunning(thread: Thread): boolean {
  return thread.status === "in_progress" || thread.turns.some((turn) => turn.status === "in_progress");
}

export function PinnedThreadList({
  threads,
  activeID,
  pendingThreadID,
  archiveConfirmThreadID,
  lastViewedTurnByThreadID,
  onSelect,
  onSelectChildAgent,
  onTogglePinned,
  onArchive,
  onClearArchiveConfirm
}: {
  threads: Thread[];
  activeID?: string;
  pendingThreadID?: string;
  archiveConfirmThreadID?: string;
  lastViewedTurnByThreadID: Record<string, string>;
  onSelect: (id: string) => void;
  onSelectChildAgent: (agent: Agent) => void;
  onTogglePinned: (thread: Thread) => void;
  onArchive: (thread: Thread) => void;
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
        onSelectChildAgent={onSelectChildAgent}
        onTogglePinned={onTogglePinned}
        onArchive={onArchive}
        onClearArchiveConfirm={onClearArchiveConfirm}
      />
    </div>
  );
}

export function ScratchThreadSection({
  threads,
  activeID,
  pendingThreadID,
  archiveConfirmThreadID,
  lastViewedTurnByThreadID,
  onSelect,
  onSelectChildAgent,
  onToggleThreadPinned,
  onArchiveThread,
  onClearArchiveConfirm,
  onCreateScratchThread
}: {
  threads: Thread[];
  activeID?: string;
  pendingThreadID?: string;
  archiveConfirmThreadID?: string;
  lastViewedTurnByThreadID: Record<string, string>;
  onSelect: (id: string) => void;
  onSelectChildAgent: (agent: Agent) => void;
  onToggleThreadPinned: (thread: Thread) => void;
  onArchiveThread: (thread: Thread) => void;
  onClearArchiveConfirm: (threadID: string) => void;
  onCreateScratchThread: () => void;
}): JSX.Element {
  const [visibleCount, setVisibleCount] = useState(PROJECT_THREAD_INITIAL_VISIBLE_COUNT);

  function showMoreScratchThreads(): void {
    setVisibleCount((current) => current + PROJECT_THREAD_VISIBLE_INCREMENT);
  }

  function collapseScratchThreads(): void {
    setVisibleCount(PROJECT_THREAD_INITIAL_VISIBLE_COUNT);
  }

  return (
    <section className="scratch-thread-section" aria-label="对话">
      <div className="sidebar-section-header scratch-thread-header">
        <span className="section-label scratch-thread-label">对话</span>
        <button
          className="project-add-button"
          type="button"
          aria-label="新建对话"
          title="新建对话"
          onClick={onCreateScratchThread}
        >
          <MessageSquarePlus className="icon-xl" />
        </button>
      </div>
      {threads.length === 0 ? (
        <div className="scratch-thread-empty-note">还没有对话</div>
      ) : (
        <div className="scratch-thread-list">
          <ThreadList
            threads={threads}
            activeID={activeID}
            pendingThreadID={pendingThreadID}
            archiveConfirmThreadID={archiveConfirmThreadID}
            lastViewedTurnByThreadID={lastViewedTurnByThreadID}
            visibleCount={visibleCount}
            onSelect={onSelect}
            onSelectChildAgent={onSelectChildAgent}
            onTogglePinned={onToggleThreadPinned}
            onArchive={onArchiveThread}
            onClearArchiveConfirm={onClearArchiveConfirm}
            onShowMore={showMoreScratchThreads}
            onCollapse={collapseScratchThreads}
          />
        </div>
      )}
    </section>
  );
}

function ThreadRows({
  threads,
  activeID,
  pendingThreadID,
  archiveConfirmThreadID,
  lastViewedTurnByThreadID,
  onSelect,
  onSelectChildAgent,
  onTogglePinned,
  onArchive,
  onClearArchiveConfirm,
  onSidebarThreadHover,
}: {
  threads: Thread[];
  activeID?: string;
  pendingThreadID?: string;
  archiveConfirmThreadID?: string;
  lastViewedTurnByThreadID: Record<string, string>;
  onSelect: (id: string) => void;
  onSelectChildAgent: (agent: Agent) => void;
  onTogglePinned: (thread: Thread) => void;
  onArchive: (thread: Thread) => void;
  onClearArchiveConfirm: (threadID: string) => void;
  // Fires when a row is hovered/unhovered so the conversation pane can
  // render a "what did this thread actually do" preview without the card
  // living inside the sidebar DOM (which would put it on top of the left
  // panel rather than in the message stream).
  onSidebarThreadHover?: (threadID: string | undefined) => void;
}): JSX.Element {
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number; thread: Thread } | null>(null);

  function handleContextMenu(
    targetThread: Thread,
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
          <Fragment key={thread.id}>
            <div
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
                {pendingSwitch ? <span className="thread-row-loading" aria-hidden="true" /> : null}
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
            {thread.child_agents?.length ? (
              <ThreadChildAgentRows
                agents={thread.child_agents}
                activeID={activeID}
                pendingThreadID={pendingThreadID}
                onSelect={onSelectChildAgent}
                onContextMenu={(e) => handleContextMenu(thread, e)}
              />
            ) : null}
          </Fragment>
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

function ThreadChildAgentRows({
  agents,
  activeID,
  pendingThreadID,
  onSelect,
  onContextMenu
}: {
  agents: Agent[];
  activeID?: string;
  pendingThreadID?: string;
  onSelect: (agent: Agent) => void;
  onContextMenu: (e: { clientX: number; clientY: number; preventDefault: () => void }) => void;
}): JSX.Element {
  return (
    <div className="thread-child-agent-list" aria-label="子任务">
      {sortChildAgents(agents).map((agent) => {
        const status = agentStatusTone(agent.status);
        const label = agentLabel(agent);
        const nestedLabel = agentNestedLabel(agent);
        const active = activeID === agent.id;
        const pendingSwitch = pendingThreadID === agent.id;
        return (
          <button
            key={agent.id}
            className={`thread-child-agent-row ${status}${active ? " active" : ""}${
              pendingSwitch ? " pending-switch" : ""
            }`}
            type="button"
            aria-current={active ? "page" : undefined}
            aria-busy={pendingSwitch}
            aria-label={`${label}，${agentStatusLabel(agent.status)}`}
            title={agentTooltip(agent)}
            onClick={() => onSelect(agent)}
            onContextMenu={onContextMenu}
          >
            <CornerDownRight className="thread-child-agent-branch icon-xs" />
            <span className="thread-child-agent-name">{label}</span>
            {nestedLabel ? <span className="thread-child-agent-nested">{nestedLabel}</span> : null}
            {pendingSwitch ? (
              <span className="thread-row-loading thread-child-agent-loading" aria-hidden="true" />
            ) : (
              <span className="thread-child-agent-status">{agentStatusLabel(agent.status)}</span>
            )}
          </button>
        );
      })}
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
