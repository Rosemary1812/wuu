import {
  Archive,
  Bot,
  BotMessageSquare,
  ChevronRight,
  Clock,
  CornerDownRight,
  Download,
  FileText,
  FolderOpen,
  FolderPlus,
  LayoutGrid,
  List as ListIcon,
  MessageSquarePlus,
  MoreHorizontal,
  Pin,
  Plus,
  Search,
  Settings,
  ShieldCheck,
  Upload,
  Wrench,
} from "lucide-react";
import { type RefObject, useEffect, useRef, useState } from "react";
import type { DesktopProject, ParticipantProfile } from "../shared/protocol";
import type { AppState, ThreadSummary } from "./AppState";
import type { ConversationFixtureKind } from "./ConversationFixtures";
import { SCRATCH_PSEUDO_PROJECT_ID } from "./AppState";
import { PinnedThreadList, ProjectGroup, SectionRowIcon } from "./ThreadSidebar";

/**
 * Stable section identity keys for the new sidebar tree.
 *
 * - `SIDEBAR_SECTION_PINNED` is FIXED-position (always first, above the
 *   reorderable list). It is intentionally NOT in `sectionOrder`.
 * - `SIDEBAR_SECTION_AGENTS` is the fixed Agents section that follows the
 *   pinned block. It IS persisted in `sectionOrder` so the user can move
 *   it below projects later (drag-reorder is a separate follow-up task).
 *   Today the reconcile layer treats unknown/special keys defensively, but
 *   the canonical default is `[AGENTS, SCRATCH_PSEUDO_PROJECT_ID, ...projects]`.
 * - `SCRATCH_PSEUDO_PROJECT_ID` ("__wuu_scratch__") is the 对话
 *   pseudo-project entry. It participates in the reorderable list so the
 *   user can move 对话 below their projects.
 */
export const SIDEBAR_SECTION_PINNED = "__wuu_pinned__";
export const SIDEBAR_SECTION_AGENTS = "__wuu_agents__";

const SIDEBAR_SECTION_ORDER_KEY = "wuu.desktop.sidebarSectionOrder";

/**
 * Reconcile the persisted sidebar section order against the current
 * project list. Pure function so it is directly testable.
 *
 * Rules:
 *   1. Drop any stored key that is neither a real project id nor
 *      `SCRATCH_PSEUDO_PROJECT_ID` (unknown pseudo ids; the pinned section
 *      is fixed-position so it never appears in the stored list, and we
 *      also defensively strip `SIDEBAR_SECTION_PINNED` if a stale write
 *      included it). AGENTS is allowed through — it is part of the
 *      reorderable list.
 *   2. Append any newly-seen project ids at the END in `projectIDs` order,
 *      preserving the stored prefix for everything else.
 *   3. When no stored value is present, return the default:
 *      `[AGENTS, SCRATCH_PSEUDO_PROJECT_ID, ...projectIDs]`.
 */
export function reconcileSidebarSectionOrder(
  stored: string[] | undefined,
  projectIDs: string[],
): string[] {
  const knownIDs = new Set<string>([
    SCRATCH_PSEUDO_PROJECT_ID,
    SIDEBAR_SECTION_AGENTS,
    ...projectIDs,
  ]);
  const out: string[] = [];
  if (Array.isArray(stored)) {
    for (const key of stored) {
      if (typeof key !== "string" || key.length === 0) continue;
      if (key === SIDEBAR_SECTION_PINNED) continue;
      if (!knownIDs.has(key)) continue;
      if (out.includes(key)) continue;
      out.push(key);
    }
  }
  for (const id of projectIDs) {
    if (!out.includes(id)) {
      out.push(id);
    }
  }
  if (!out.includes(SCRATCH_PSEUDO_PROJECT_ID)) {
    // Insert scratch right after AGENTS (or at the head if AGENTS isn't
    // present). Mirrors the default below so the first reconcile keeps
    // scratch anchored at the top.
    const anchor = out.indexOf(SIDEBAR_SECTION_AGENTS);
    if (anchor === -1) {
      out.unshift(SCRATCH_PSEUDO_PROJECT_ID);
    } else {
      out.splice(anchor + 1, 0, SCRATCH_PSEUDO_PROJECT_ID);
    }
  }
  if (!out.includes(SIDEBAR_SECTION_AGENTS)) {
    out.unshift(SIDEBAR_SECTION_AGENTS);
  }
  return out;
}

export function AppSidebar({
  state,
  sidebarProjects,
  pinnedThreads,
  activeThreadID,
  activeParticipantID,
  participants,
  busyParticipantIDs,
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
  sectionOrder,
  onStartNewThread,
  onOpenSkillsTab,
  onToggleConversationSearch,
  onSeedConversationFixture,
  onSeedAgentTreeDemo,
  onOpenChipGallery,
  onOpenApprovalGallery,
  onSelectThread,
  onSelectParticipant,
  onCreateParticipant,
  onImportParticipants,
  onExportParticipants,
  onTogglePinned,
  onArchiveThread,
  onClearArchiveConfirm,
  onToggleProjectMenu,
  onCreateProject,
  onOpenProjectFolder,
  onToggleProjectCollapsed,
  onStartNewThreadForProject,
  onSelectProjectThread,
  onPointerEnter,
  onPointerLeave,
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
  // Set of participant IDs with at least one running run. Drives the busy
  // status dot in the roster. Derived in App.tsx by walking child agents
  // across all threads; participants not in the set render as online.
  busyParticipantIDs: Set<string>;
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
  // Order of reorderable sections. The pinned section is rendered first
  // (fixed position) and is NOT included. Each key maps to either
  // SCRATCH_PSEUDO_PROJECT_ID or a real project id. The Agents section
  // key may appear here too (it's persisted as part of the order list).
  sectionOrder: string[];
  onStartNewThread: () => void;
  onOpenSkillsTab: () => void;
  onToggleConversationSearch: () => void;
  onSeedConversationFixture: (kind: ConversationFixtureKind) => void;
  onSeedAgentTreeDemo: () => void;
  onOpenChipGallery: () => void;
  onOpenApprovalGallery: () => void;
  onSelectThread: (id: string) => void;
  onSelectParticipant: (participant: ParticipantProfile) => void;
  onCreateParticipant: () => void;
  onImportParticipants: (file: File) => void;
  onExportParticipants: () => void;
  onTogglePinned: (thread: ThreadSummary) => void;
  onArchiveThread: (thread: ThreadSummary) => void;
  onClearArchiveConfirm: (threadID: string) => void;
  onToggleProjectMenu: () => void;
  onCreateProject: () => void;
  onOpenProjectFolder: () => void;
  onToggleProjectCollapsed: (id: string) => void;
  onStartNewThreadForProject: (id: string) => void;
  onSelectProjectThread: (projectID: string, threadID: string) => void;
  onPointerEnter?: () => void;
  onPointerLeave?: () => void;
  onOpenSettings: () => void;
}): JSX.Element {
  const hasRuntimeContext = Boolean(state.activeContext);
  const fixturesEnabled = hasRuntimeContext && Boolean(state.initialized);
  const participantTemplateInputRef = useRef<HTMLInputElement>(null);
  const rosterMenuRef = useRef<HTMLDivElement>(null);
  const [rosterMenuOpen, setRosterMenuOpen] = useState(false);
  // Close the overflow menu on outside pointerdown or Escape so the same
  // input devices that open it (click / keyboard) can also dismiss it
  // without coupling to a parent click handler.
  useEffect(() => {
    if (!rosterMenuOpen) {
      return undefined;
    }
    const handlePointerDown = (event: PointerEvent): void => {
      if (!rosterMenuRef.current) {
        return;
      }
      if (event.target instanceof Node && rosterMenuRef.current.contains(event.target)) {
        return;
      }
      setRosterMenuOpen(false);
    };
    const handleKey = (event: KeyboardEvent): void => {
      if (event.key === "Escape") {
        setRosterMenuOpen(false);
      }
    };
    document.addEventListener("pointerdown", handlePointerDown);
    document.addEventListener("keydown", handleKey);
    return () => {
      document.removeEventListener("pointerdown", handlePointerDown);
      document.removeEventListener("keydown", handleKey);
    };
  }, [rosterMenuOpen]);
  // The scratch pseudo project is "active" when the runtime context is in
  // no-project mode (i.e. the user is viewing a scratch conversation).
  // Active state is passed into ProjectList so the row highlights even though
  // it has no DesktopProject entry in state.projects.
  const sidebarScratchPseudoActive = state.activeContext?.kind === "no_project";

  return (
    <aside
      className="sidebar"
      onPointerEnter={onPointerEnter}
      onPointerLeave={onPointerLeave}
    >
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
              <button
                className="nav-item dev-fixture-button"
                onClick={onOpenApprovalGallery}
              >
                <ShieldCheck className="icon" />
                <span>审批图鉴</span>
              </button>
            </div>
          ) : null}
        </nav>

        <div className="sidebar-main scrollbar-hidden">
          {pinnedThreads.length > 0 ? (
            <section className="pinned-thread-section" aria-label="置顶">
              <button
                className={`project-row pinned-row${collapsedProjectIDs.has(SIDEBAR_SECTION_PINNED) ? "" : " expanded"}`}
                type="button"
                aria-expanded={!collapsedProjectIDs.has(SIDEBAR_SECTION_PINNED)}
                aria-label={`${collapsedProjectIDs.has(SIDEBAR_SECTION_PINNED) ? "展开" : "收起"}置顶`}
                title={collapsedProjectIDs.has(SIDEBAR_SECTION_PINNED) ? "展开置顶" : "收起置顶"}
                onClick={() => onToggleProjectCollapsed(SIDEBAR_SECTION_PINNED)}
              >
                <SectionRowIcon
                  collapsed={collapsedProjectIDs.has(SIDEBAR_SECTION_PINNED)}
                  iconKind="pinned"
                  CollapsedIcon={Pin}
                  ExpandedIcon={Pin}
                />
                <span className="project-row-label">
                  <span className="project-row-name">置顶</span>
                  <ChevronRight className="project-row-chevron icon" aria-hidden="true" />
                </span>
              </button>
              {!collapsedProjectIDs.has(SIDEBAR_SECTION_PINNED) ? (
                <div className="thread-list-collapse">
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
                </div>
              ) : null}
            </section>
          ) : null}
          {sectionOrder.map((key) => {
            if (key === SIDEBAR_SECTION_AGENTS) {
              const collapsed = collapsedProjectIDs.has(SIDEBAR_SECTION_AGENTS);
              return (
                <section
                  key={SIDEBAR_SECTION_AGENTS}
                  className="participant-roster-section"
                  aria-label="Agents"
                  data-section-id={SIDEBAR_SECTION_AGENTS}
                >
                  <div className="participant-roster-header-group">
                    <button
                      className={`project-row agent-row${collapsed ? "" : " expanded"}`}
                      type="button"
                      aria-expanded={!collapsed}
                      aria-label={`${collapsed ? "展开" : "收起"} Agents`}
                      title={collapsed ? "展开 Agents" : "收起 Agents"}
                      onClick={() => onToggleProjectCollapsed(SIDEBAR_SECTION_AGENTS)}
                    >
                      <SectionRowIcon
                        collapsed={collapsed}
                        iconKind="agents"
                        CollapsedIcon={Bot}
                        ExpandedIcon={BotMessageSquare}
                      />
                      <span className="project-row-label">
                        <span className="project-row-name">Agents</span>
                        <ChevronRight className="project-row-chevron icon" aria-hidden="true" />
                      </span>
                    </button>
                    <div className="participant-roster-actions participant-roster-actions-header">
                      <input
                        ref={participantTemplateInputRef}
                        className="participant-roster-file-input"
                        type="file"
                        accept="application/json,.json"
                        tabIndex={-1}
                        onChange={(event) => {
                          const file = event.currentTarget.files?.[0];
                          event.currentTarget.value = "";
                          if (file) {
                            onImportParticipants(file);
                          }
                        }}
                      />
                      <div className="participant-roster-menu" ref={rosterMenuRef}>
                        <button
                          type="button"
                          className="participant-roster-add"
                          aria-label="团队模板操作"
                          aria-haspopup="menu"
                          aria-expanded={rosterMenuOpen}
                          title="团队模板操作"
                          disabled={!state.initialized}
                          onClick={() => setRosterMenuOpen((open) => !open)}
                        >
                          <MoreHorizontal aria-hidden="true" />
                        </button>
                        {rosterMenuOpen ? (
                          <div className="project-add-menu" role="menu">
                            <button
                              type="button"
                              role="menuitem"
                              disabled={!state.initialized}
                              onClick={() => {
                                setRosterMenuOpen(false);
                                participantTemplateInputRef.current?.click();
                              }}
                            >
                              <Upload aria-hidden="true" />
                              <span>导入团队模板</span>
                            </button>
                            <button
                              type="button"
                              role="menuitem"
                              disabled={!state.initialized || participants.length === 0}
                              onClick={() => {
                                setRosterMenuOpen(false);
                                onExportParticipants();
                              }}
                            >
                              <Download aria-hidden="true" />
                              <span>导出团队模板</span>
                            </button>
                          </div>
                        ) : null}
                      </div>
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
                  </div>
                  {!collapsed ? (
                    <div className="thread-list-collapse participant-roster-collapse">
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
                          participants.map((participant) => {
                            const isBusy = busyParticipantIDs.has(participant.id);
                            const status = isBusy ? "busy" : "online";
                            const avatar = (
                              <span className="participant-roster-avatar" aria-hidden="true">
                                {participant.avatar_image ? (
                                  <img
                                    className="participant-roster-avatar-image"
                                    src={participant.avatar_image}
                                    alt=""
                                    loading="lazy"
                                  />
                                ) : participant.avatar ? (
                                  participant.avatar
                                ) : (
                                  "•"
                                )}
                              </span>
                            );
                            return (
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
                                  data-status={status}
                                  title={isBusy ? "运行中" : "在线"}
                                />
                                {avatar}
                                <span className="participant-roster-copy">
                                  <span className="participant-roster-name">
                                    {participant.name}
                                  </span>
                                  <span className="participant-roster-meta">
                                    {participant.tagline || participant.role || "named"}
                                  </span>
                                </span>
                              </button>
                            );
                          })
                        )}
                      </div>
                    </div>
                  ) : null}
                </section>
              );
            }
            // For SCRATCH_PSEUDO_PROJECT_ID or any real project id, look up
            // the synthetic DesktopProject (App.tsx prepends the scratch
            // pseudo so `sidebarProjects` contains every key).
            const project = sidebarProjects.find((p) => p.id === key);
            if (!project) {
              return null;
            }
            // The 对话 scratch pseudo is surfaced with aria-label="项目" so
            // legacy single-wrapper selectors (and screen-reader users
            // scanning for "项目") still find it. Real projects expose the
            // more specific aria-label="项目 {name}" so per-project
            // automation can target them by id.
            const isScratchPseudo = project.id === SCRATCH_PSEUDO_PROJECT_ID;
            const sectionAriaLabel = isScratchPseudo
              ? "项目"
              : `项目 ${project.name}`;
            return (
              <section
                key={key}
                className="project-section"
                aria-label={sectionAriaLabel}
                data-section-id={key}
              >
                <ProjectGroup
                  project={project}
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
              </section>
            );
          })}
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
