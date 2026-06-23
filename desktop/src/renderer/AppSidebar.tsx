import {
  Archive,
  Clock,
  CornerDownRight,
  FileText,
  FolderOpen,
  FolderPlus,
  List as ListIcon,
  MessageSquarePlus,
  Search,
  Settings,
  Wrench,
} from "lucide-react";
import { useEffect, useRef, type RefObject } from "react";
import type { Agent, Thread } from "../shared/protocol";
import type { AppState } from "./AppState";
import type { ConversationFixtureKind } from "./ConversationFixtures";
import { PinnedThreadList, ProjectList, ScratchThreadSection } from "./ThreadSidebar";

// Matches ConversationScrollState.CONVERSATION_SCROLLBAR_HIDE_DELAY_MS so
// the sidebar scrollbar feels identical to the main conversation pane.
const SIDEBAR_SCROLLBAR_HIDE_DELAY_MS = 700;

export function AppSidebar({
  state,
  pinnedThreads,
  scratchThreads,
  activeThreadID,
  pendingThreadID,
  pendingProjectID,
  archiveConfirmThreadID,
  collapsedProjectIDs,
  collapsingProjectIDs,
  projectMenuOpen,
  projectMenuRef,
  searchOpen,
  debugFixturesVisible,
  onStartNewThread,
  onOpenSkillsTab,
  onToggleConversationSearch,
  onSeedConversationFixture,
  onSeedAgentTreeDemo,
  onSelectThread,
  onSelectChildAgent,
  onTogglePinned,
  onArchiveThread,
  onClearArchiveConfirm,
  onToggleProjectMenu,
  onCreateProject,
  onOpenProjectFolder,
  onOpenProject,
  onToggleProjectCollapsed,
  onStartNewThreadForProject,
  onCreateScratchThread,
  onOpenSettings,
}: {
  state: AppState;
  pinnedThreads: Thread[];
  scratchThreads: Thread[];
  activeThreadID?: string;
  pendingThreadID?: string;
  pendingProjectID?: string;
  archiveConfirmThreadID?: string;
  collapsedProjectIDs: Set<string>;
  collapsingProjectIDs: Set<string>;
  projectMenuOpen: boolean;
  projectMenuRef: RefObject<HTMLDivElement | null>;
  searchOpen: boolean;
  debugFixturesVisible: boolean;
  onStartNewThread: () => void;
  onOpenSkillsTab: () => void;
  onToggleConversationSearch: () => void;
  onSeedConversationFixture: (kind: ConversationFixtureKind) => void;
  onSeedAgentTreeDemo: () => void;
  onSelectThread: (id: string) => void;
  onSelectChildAgent: (agent: Agent) => void;
  onTogglePinned: (thread: Thread) => void;
  onArchiveThread: (thread: Thread) => void;
  onClearArchiveConfirm: (threadID: string) => void;
  onToggleProjectMenu: () => void;
  onCreateProject: () => void;
  onOpenProjectFolder: () => void;
  onOpenProject: (id: string) => void;
  onToggleProjectCollapsed: (id: string) => void;
  onStartNewThreadForProject: (id: string) => void;
  onCreateScratchThread: () => void;
  onOpenSettings: () => void;
}): JSX.Element {
  const hasRuntimeContext = Boolean(state.activeContext);
  const fixturesEnabled = hasRuntimeContext && Boolean(state.initialized);

  /*
   * Fade the project-list scrollbar in while the user is actively scrolling
   * and out after 700ms of idle, matching the main conversation pane. The
   * scrollHeight check mirrors ConversationScrollState so a list that fits
   * inside the sidebar never paints a phantom scrollbar.
   */
  const projectListRef = useRef<HTMLDivElement | null>(null);
  const sidebarScrollbarHideTimerRef = useRef<number | undefined>(undefined);
  useEffect(() => {
    const node = projectListRef.current;
    if (!node) {
      return;
    }
    function showScrollbar(scrollNode: HTMLElement): void {
      if (scrollNode.scrollHeight <= scrollNode.clientHeight) {
        return;
      }
      scrollNode.classList.add("scrollbar-visible");
      if (sidebarScrollbarHideTimerRef.current !== undefined) {
        window.clearTimeout(sidebarScrollbarHideTimerRef.current);
      }
      sidebarScrollbarHideTimerRef.current = window.setTimeout(() => {
        sidebarScrollbarHideTimerRef.current = undefined;
        scrollNode.classList.remove("scrollbar-visible");
      }, SIDEBAR_SCROLLBAR_HIDE_DELAY_MS);
    }
    node.addEventListener("scroll", () => showScrollbar(node), { passive: true });
    return () => {
      node.removeEventListener("scroll", () => showScrollbar(node));
      if (sidebarScrollbarHideTimerRef.current !== undefined) {
        window.clearTimeout(sidebarScrollbarHideTimerRef.current);
        sidebarScrollbarHideTimerRef.current = undefined;
      }
    };
  }, []);

  return (
    <aside className="sidebar">
      <div className="sidebar-content">
        <div className="traffic-spacer" />
        <nav className="primary-nav" aria-label="主导航">
          <button
            className="nav-item"
            onClick={onStartNewThread}
            disabled={!hasRuntimeContext}
          >
            <MessageSquarePlus className="icon-lg" />
            <span>新对话</span>
          </button>
          <button
            className="nav-item"
            onClick={onOpenSkillsTab}
            disabled={!hasRuntimeContext}
          >
            <Wrench className="icon-lg" />
            <span>Skills</span>
          </button>
          <button
            className="nav-item conversation-search-trigger"
            type="button"
            aria-haspopup="dialog"
            aria-expanded={searchOpen}
            onClick={onToggleConversationSearch}
            disabled={!hasRuntimeContext}
          >
            <Search className="icon-lg" />
            <span>搜索会话</span>
          </button>
          {debugFixturesVisible ? (
            <div className="dev-fixture-nav" aria-label="开发调试会话">
              <div className="dev-fixture-label">开发样例</div>
              <button
                className="nav-item dev-fixture-button"
                onClick={() => onSeedConversationFixture("long")}
                disabled={!fixturesEnabled}
              >
                <FileText className="icon" />
                <span>长对话</span>
              </button>
              <button
                className="nav-item dev-fixture-button"
                onClick={() => onSeedConversationFixture("rich")}
                disabled={!fixturesEnabled}
              >
                <ListIcon className="icon" />
                <span>富内容</span>
              </button>
              <button
                className="nav-item dev-fixture-button"
                onClick={() => onSeedConversationFixture("running")}
                disabled={!fixturesEnabled}
              >
                <Clock className="icon" />
                <span>运行中</span>
              </button>
              <button
                className="nav-item dev-fixture-button"
                onClick={() => onSeedConversationFixture("compact")}
                disabled={!fixturesEnabled}
              >
                <Archive className="icon" />
                <span>上下文压缩</span>
              </button>
              <button
                className="nav-item dev-fixture-button"
                onClick={onSeedAgentTreeDemo}
                disabled={!fixturesEnabled}
              >
                <CornerDownRight className="icon" />
                <span>子任务</span>
              </button>
            </div>
          ) : null}
        </nav>

        {pinnedThreads.length > 0 ? (
          <section className="pinned-thread-section" aria-label="置顶">
            <div className="section-label pinned-thread-label">置顶</div>
            <PinnedThreadList
              threads={pinnedThreads}
              activeID={activeThreadID}
              pendingThreadID={pendingThreadID}
              archiveConfirmThreadID={archiveConfirmThreadID}
              lastViewedTurnByThreadID={state.lastViewedTurnByThreadID}
              onSelect={onSelectThread}
              onSelectChildAgent={onSelectChildAgent}
              onTogglePinned={onTogglePinned}
              onArchive={onArchiveThread}
              onClearArchiveConfirm={onClearArchiveConfirm}
            />
          </section>
        ) : null}

        <ScratchThreadSection
          threads={scratchThreads}
          activeID={activeThreadID}
          pendingThreadID={pendingThreadID}
          archiveConfirmThreadID={archiveConfirmThreadID}
          lastViewedTurnByThreadID={state.lastViewedTurnByThreadID}
          onSelect={onSelectThread}
          onSelectChildAgent={onSelectChildAgent}
          onToggleThreadPinned={onTogglePinned}
          onArchiveThread={onArchiveThread}
          onClearArchiveConfirm={onClearArchiveConfirm}
          onCreateScratchThread={onCreateScratchThread}
        />

        <section className="project-section" aria-label="项目">
          <div className="sidebar-section-header project-section-header" ref={projectMenuRef}>
            <div className="section-label">项目</div>
            <button
              className="project-add-button"
              aria-label="添加项目"
              aria-haspopup="menu"
              aria-expanded={projectMenuOpen}
              onClick={onToggleProjectMenu}
            >
              <FolderPlus className="icon-xl" />
            </button>
            {projectMenuOpen ? (
              <div className="project-add-menu" role="menu">
                <button role="menuitem" onClick={onCreateProject}>
                  <FolderPlus className="icon-xl" />
                  <span>新建空白项目</span>
                </button>
                <button role="menuitem" onClick={onOpenProjectFolder}>
                  <FolderOpen className="icon-xl" />
                  <span>使用现有文件夹</span>
                </button>
              </div>
            ) : null}
          </div>
          <div className="project-list" ref={projectListRef}>
            {state.projects.length === 0 ? (
              <div className="project-empty-note">还没有项目</div>
            ) : null}
            <ProjectList
              projects={state.projects}
              activeID={state.activeProjectId}
              pendingProjectID={pendingProjectID}
              collapsedProjectIDs={collapsedProjectIDs}
              collapsingProjectIDs={collapsingProjectIDs}
              threads={state.threads}
              activeThreadID={activeThreadID}
              pendingThreadID={pendingThreadID}
              archiveConfirmThreadID={archiveConfirmThreadID}
              lastViewedTurnByThreadID={state.lastViewedTurnByThreadID}
              onSelectProject={onOpenProject}
              onToggleProjectCollapsed={onToggleProjectCollapsed}
              onStartNewThread={onStartNewThreadForProject}
              onSelectThread={onSelectThread}
              onSelectChildAgent={onSelectChildAgent}
              onToggleThreadPinned={onTogglePinned}
              onArchiveThread={onArchiveThread}
              onClearArchiveConfirm={onClearArchiveConfirm}
            />
          </div>
        </section>
        <div className="sidebar-settings">
          <button
            className="settings-button"
            type="button"
            disabled={!state.initialized}
            onClick={onOpenSettings}
          >
            <Settings className="icon-lg" />
            <span>设置</span>
          </button>
        </div>
      </div>
    </aside>
  );
}
