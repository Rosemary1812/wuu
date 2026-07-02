import {
  Archive,
  Clock,
  CornerDownRight,
  FileText,
  FolderOpen,
  FolderPlus,
  LayoutGrid,
  List as ListIcon,
  MessageSquarePlus,
  Plus,
  Search,
  Settings,
  Wrench,
} from "lucide-react";
import type { RefObject } from "react";
import type { DesktopProject, ParticipantProfile } from "../shared/protocol";
import type { AppState, ThreadSummary } from "./AppState";
import type { ConversationFixtureKind } from "./ConversationFixtures";
import { SCRATCH_PSEUDO_PROJECT_ID } from "./AppState";
import { PinnedThreadList, ProjectList } from "./ThreadSidebar";

export function AppSidebar({
  state,
  sidebarProjects,
  pinnedThreads,
  activeThreadID,
  activeParticipantID,
  participants,
  pendingThreadID,
  pendingProjectID,
  archiveConfirmThreadID,
  collapsedProjectIDs,
  expandedProjectIDs,
  collapsingProjectIDs,
  projectThreadsByProjectID,
  projectMenuOpen,
  projectMenuRef,
  searchOpen,
  debugFixturesVisible,
  onStartNewThread,
  onOpenSkillsTab,
  onToggleConversationSearch,
  onSeedConversationFixture,
  onSeedAgentTreeDemo,
  onOpenChipGallery,
  onSelectThread,
  onSelectParticipant,
  onCreateParticipant,
  onTogglePinned,
  onArchiveThread,
  onClearArchiveConfirm,
  onToggleProjectMenu,
  onCreateProject,
  onOpenProjectFolder,
  onToggleProjectCollapsed,
  onStartNewThreadForProject,
  onSelectProjectThread,
  onOpenSettings,
}: {
  state: AppState;
  // The sidebar renders scratch conversations through the same ProjectList
  // path as real projects, so App.tsx prepends a synthetic DesktopProject
  // (id = SCRATCH_PSEUDO_PROJECT_ID) into this array. The original
  // state.projects list is unchanged; sidebarProjects is what the sidebar
  // actually shows.
  sidebarProjects: DesktopProject[];
  pinnedThreads: ThreadSummary[];
  activeThreadID?: string;
  activeParticipantID?: string;
  participants: ParticipantProfile[];
  pendingThreadID?: string;
  pendingProjectID?: string;
  archiveConfirmThreadID?: string;
  collapsedProjectIDs: Set<string>;
  expandedProjectIDs: Set<string>;
  collapsingProjectIDs: Set<string>;
  projectThreadsByProjectID: Record<string, ThreadSummary[]>;
  projectMenuOpen: boolean;
  projectMenuRef: RefObject<HTMLDivElement | null>;
  searchOpen: boolean;
  debugFixturesVisible: boolean;
  onStartNewThread: () => void;
  onOpenSkillsTab: () => void;
  onToggleConversationSearch: () => void;
  onSeedConversationFixture: (kind: ConversationFixtureKind) => void;
  onSeedAgentTreeDemo: () => void;
  onOpenChipGallery: () => void;
  onSelectThread: (id: string) => void;
  onSelectParticipant: (participant: ParticipantProfile) => void;
  onCreateParticipant: () => void;
  onTogglePinned: (thread: ThreadSummary) => void;
  onArchiveThread: (thread: ThreadSummary) => void;
  onClearArchiveConfirm: (threadID: string) => void;
  onToggleProjectMenu: () => void;
  onCreateProject: () => void;
  onOpenProjectFolder: () => void;
  onToggleProjectCollapsed: (id: string) => void;
  onStartNewThreadForProject: (id: string) => void;
  onSelectProjectThread: (projectID: string, threadID: string) => void;
  onOpenSettings: () => void;
}): JSX.Element {
  const hasRuntimeContext = Boolean(state.activeContext);
  const fixturesEnabled = hasRuntimeContext && Boolean(state.initialized);
  // The scratch pseudo project is "active" when the runtime context is in
  // no-project mode (i.e. the user is viewing a scratch conversation).
  // Active state is passed into ProjectList so the row highlights even though
  // it has no DesktopProject entry in state.projects.
  const sidebarScratchPseudoActive = state.activeContext?.kind === "no_project";

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
          <div className="sidebar-add-workspace" ref={projectMenuRef}>
            <button
              className="nav-item"
              type="button"
              aria-label="添加工作区"
              aria-haspopup="menu"
              aria-expanded={projectMenuOpen}
              onClick={onToggleProjectMenu}
            >
              <FolderPlus className="icon-lg" />
              <span>添加工作区</span>
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
              <button
                className="nav-item dev-fixture-button"
                onClick={onOpenChipGallery}
              >
                <LayoutGrid className="icon" />
                <span>Chip 图鉴</span>
              </button>
            </div>
          ) : null}
        </nav>

        <div className="sidebar-main scrollbar-hidden">
          <section className="participant-roster-section" aria-label="Agents">
            <div className="section-label participant-roster-label">
              <span>Agents</span>
              <button
                type="button"
                className="participant-roster-add"
                aria-label="新建 Agent"
                title="新建 Agent"
                disabled={!state.initialized}
                onClick={onCreateParticipant}
              >
                <Plus aria-hidden="true" />
              </button>
            </div>
            <div className="participant-roster-list">
              {participants.length === 0 ? (
                <button
                  type="button"
                  className="participant-roster-row empty"
                  disabled={!state.initialized}
                  onClick={onCreateParticipant}
                >
                  <span
                    className="participant-roster-status"
                    data-status="offline"
                  />
                  <span className="participant-roster-name">添加 Agent</span>
                  <span className="participant-roster-meta">常驻身份</span>
                </button>
              ) : (
                participants.map((participant) => (
                  <button
                    key={participant.id}
                    type="button"
                    className={`participant-roster-row${
                      activeParticipantID === participant.id ? " active" : ""
                    }`}
                    onClick={() => onSelectParticipant(participant)}
                  >
                    <span
                      className="participant-roster-status"
                      data-status="online"
                    />
                    <span className="participant-roster-avatar" aria-hidden="true">
                      {participant.avatar || "•"}
                    </span>
                    <span className="participant-roster-copy">
                      <span className="participant-roster-name">
                        {participant.name}
                      </span>
                      <span className="participant-roster-meta">
                        {participant.tagline || participant.role || "named"}
                      </span>
                    </span>
                  </button>
                ))
              )}
            </div>
          </section>
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
                onTogglePinned={onTogglePinned}
                onArchive={onArchiveThread}
                onClearArchiveConfirm={onClearArchiveConfirm}
              />
            </section>
          ) : null}
          <section className="project-section" aria-label="项目">
            <div className="project-list">
              {sidebarProjects.length === 0 ? (
                <div className="project-empty-note">还没有项目</div>
              ) : null}
              <ProjectList
                projects={sidebarProjects}
                activeID={state.activeProjectId}
                pendingProjectID={pendingProjectID}
                collapsedProjectIDs={collapsedProjectIDs}
                expandedProjectIDs={expandedProjectIDs}
                collapsingProjectIDs={collapsingProjectIDs}
                threadsByProjectID={projectThreadsByProjectID}
                activeThreadID={activeThreadID}
                pendingThreadID={pendingThreadID}
                archiveConfirmThreadID={archiveConfirmThreadID}
                lastViewedTurnByThreadID={state.lastViewedTurnByThreadID}
                scratchPseudoProjectID={SCRATCH_PSEUDO_PROJECT_ID}
                scratchPseudoActive={sidebarScratchPseudoActive}
                onToggleProjectCollapsed={onToggleProjectCollapsed}
                onStartNewThread={onStartNewThreadForProject}
                onSelectThread={onSelectProjectThread}
                onToggleThreadPinned={onTogglePinned}
                onArchiveThread={onArchiveThread}
                onClearArchiveConfirm={onClearArchiveConfirm}
              />
            </div>
          </section>
        </div>
        <div className="sidebar-settings">
          <button
            className="sidebar-settings-button"
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
