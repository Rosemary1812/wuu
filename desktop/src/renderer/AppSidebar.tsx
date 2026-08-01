import {
  AlarmClock,
  Archive,
  ChevronRight,
  ClipboardList,
  Clock,
  CornerDownRight,
  FileText,
  Folder,
  FolderOpen,
  FolderPlus,
  Hash,
  LayoutGrid,
  List as ListIcon,
  MessageSquare,
  MessageSquarePlus,
  MessagesSquare,
  Pin,
  Plus,
  Search,
  Settings,
  UsersRound,
  Wrench,
} from "lucide-react";
import {
  type CSSProperties,
  type HTMLAttributes,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
  type RefObject,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import {
  closestCenter,
  DndContext,
  DragOverlay,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragStartEvent,
} from "@dnd-kit/core";
import { restrictToVerticalAxis } from "@dnd-kit/modifiers";
import {
  arrayMove,
  SortableContext,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import type { ChannelRoom, DesktopProject } from "../shared/protocol";
import type { AppState, ThreadSummary } from "./AppState";
import { formatChannelUnreadCount } from "./ChannelView";
import type { ConversationFixtureKind } from "./ConversationFixtures";
import { SCRATCH_PSEUDO_PROJECT_ID } from "./AppState";
import { PinnedThreadList, ProjectGroup } from "./ThreadSidebar";
import { SidebarSection, SidebarSectionDragHandleContext } from "./SidebarSection";
import { useI18n } from "./i18n";

/**
 * Stable section identity keys for the new sidebar tree.
 *
 * - `SIDEBAR_SECTION_PINNED` is FIXED-position (always first, above the
 *   reorderable list). It is intentionally NOT in `sectionOrder`.
 * - `SCRATCH_PSEUDO_PROJECT_ID` ("__wuu_scratch__") is the 对话
 *   pseudo-project entry. It belongs to the workspace functional group and
 *   can be reordered with real projects.
 */
export const SIDEBAR_SECTION_PINNED = "__wuu_pinned__";

/**
 * Fixed-position 协作 (group chat) section. Like the pinned section it is
 * intentionally NOT part of the reorderable `sectionOrder` — it always sits
 * between 置顶 and the workspace group so the sidebar anatomy stays
 * predictable regardless of how the user reorders projects.
 */
export const SIDEBAR_SECTION_COLLAB = "__wuu_collab__";

/**
 * Reconcile the persisted sidebar section order against the current
 * project list. Pure function so it is directly testable.
 *
 * Rules:
 *   1. Drop any stored key that is neither a real project id nor
 *      `SCRATCH_PSEUDO_PROJECT_ID` (including stale fixed or legacy section
 *      ids).
 *   2. Append newly-seen project ids in `projectIDs` order.
 *   3. Ensure the scratch entry is present while preserving workspace order.
 */
export function reconcileSidebarSectionOrder(
  stored: string[] | undefined,
  projectIDs: string[],
): string[] {
  const knownIDs = new Set<string>([
    SCRATCH_PSEUDO_PROJECT_ID,
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
    out.unshift(SCRATCH_PSEUDO_PROJECT_ID);
  }
  return out;
}

/**
 * Pure reorder helper: returns the order with `activeId` moved to the
 * position of `overId`. Pure so the drag-end logic is testable without
 * mounting the DOM or stubbing dnd-kit. Behavior contract:
 *   - overId is null / empty / equal to activeId → returns the input
 *     unchanged. dnd-kit occasionally emits "no over" when the pointer
 *     releases outside any droppable; we treat that as a no-op.
 *   - Either id absent from the order → returns the input unchanged.
 *     This preserves the persisted order when something exotic happens
 *     (e.g. the active section was removed from the reconcile result
 *     while the drag was in flight).
 */
export function reorderSidebarSections(
  order: string[],
  activeId: string,
  overId: string | null | undefined,
): string[] {
  if (!overId || activeId === overId) return order;
  const from = order.indexOf(activeId);
  const to = order.indexOf(overId);
  if (from === -1 || to === -1) return order;
  return arrayMove(order, from, to);
}

// Context that lets SidebarSection — a shared component used by both
// the non-sortable pinned section and the sortable Agents / 对话 / 项目
// sections — pick up the dnd-kit activator listeners when it's inside a
// SortableSection. Default value is null so non-sortable callsites
// (the pinned section) fall through to the no-drag-handle path and the
// header still works as a plain toggle.
//
// The context itself lives in SidebarSection so the consumer side
// (SidebarSection) and provider side (SortableSection, below) share a
// single import without a circular type dependency. SidebarSection
// also exports the hook SidebarSection uses to read the handle.

type SectionHeaderInfo = {
  label: string;
  iconKind: string;
  CollapsedIcon: React.ComponentType<{ className?: string }>;
  ExpandedIcon: React.ComponentType<{ className?: string }>;
};

function SectionDragPreview({
  info,
}: {
  info: SectionHeaderInfo;
}): JSX.Element {
  // Header-only preview rendered inside <DragOverlay>. Mirrors the
  // SidebarSection anatomy (icon pair + label + chevron) so the floating
  // preview reads as the same row the user grabbed. No toggle button —
  // DragOverlay should be inert while dragging.
  return (
    <div className="sidebar-section-drag-overlay">
      <div className="sidebar-section-row project-row expanded">
        <span className="project-row-icon">
          <info.CollapsedIcon
            className="icon-lg project-row-icon-state collapsed"
            data-project-icon-kind={info.iconKind}
            data-project-icon-state="collapsed"
            aria-hidden="true"
          />
          <info.ExpandedIcon
            className="icon-lg project-row-icon-state expanded"
            data-project-icon-kind={info.iconKind}
            aria-hidden="true"
          />
        </span>
        <span className="project-row-label">
          <span className="project-row-name">{info.label}</span>
          <ChevronRight
            className="project-row-chevron icon"
            aria-hidden="true"
          />
        </span>
      </div>
    </div>
  );
}

function SortableSection({
  id,
  className,
  ariaLabel,
  headerInfo,
  registerHeaderInfo,
  children,
}: {
  id: string;
  className: string;
  ariaLabel: string;
  headerInfo: SectionHeaderInfo;
  // Called on every render with the current id → info mapping so the
  // DragOverlay can render the right header while a drag is in flight.
  // Kept as a callback rather than a side-effect of mounting because
  // sectionOrder is reconciled on every project-list change and stale
  // ids must drop out of the lookup cleanly.
  registerHeaderInfo: (id: string, info: SectionHeaderInfo | null) => void;
  children: ReactNode;
}): JSX.Element {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id });
  // Depend on the individual fields (not the object) so the parent's
  // per-render headerInfo literal doesn't re-run the effect every render.
  const { label, iconKind, CollapsedIcon, ExpandedIcon } = headerInfo;
  useEffect(() => {
    registerHeaderInfo(id, { label, iconKind, CollapsedIcon, ExpandedIcon });
    return () => {
      registerHeaderInfo(id, null);
    };
  }, [id, label, iconKind, CollapsedIcon, ExpandedIcon, registerHeaderInfo]);
  const style: CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
  };
  const dragHandleProps: HTMLAttributes<HTMLButtonElement> = {
    ...attributes,
    ...listeners,
  };
  return (
    <section
      ref={setNodeRef}
      className={className}
      aria-label={ariaLabel}
      data-section-id={id}
      style={style}
    >
      <SidebarSectionDragHandleContext.Provider value={{ dragHandleProps, isDragging }}>
        {children}
      </SidebarSectionDragHandleContext.Provider>
    </section>
  );
}

export function AppSidebar({
  state,
  sidebarProjects,
  activeProjectID,
  pinnedThreads,
  activeThreadID,
  pendingThreadID,
  pendingProjectID,
  collapsedSidebarSectionIDs,
  expandedSidebarSectionIDs,
  loadingProjectThreadIDs,
  projectThreadsByProjectID,
  projectMenuOpen,
  projectMenuRef,
  searchOpen,
  debugFixturesVisible,
  sectionOrder,
  onStartNewThread,
  onOpenAutomationsTab,
  onOpenSkillsTab,
  groupChatEnabled = false,
  channelRooms = [],
  activeChannelRoomID,
  activeChannelSection = null,
  onSelectChannelRoom,
  onOpenChannelAgents,
  onOpenChannelTasks,
  onOpenChannels,
  onCreateChannelRoom,
  onToggleConversationSearch,
  onSeedConversationFixture,
  onSeedAgentTreeDemo,
  onOpenChipGallery,
  onSelectThread,
  onTogglePinned,
  onArchiveThread,
  onDeleteThread,
  onRenameThread,
  onToggleProjectMenu,
  onCreateProject,
  onOpenProjectFolder,
  onToggleSidebarSectionCollapsed,
  onSelectProjectWorkspace,
  onStartNewThreadForProject,
  onSelectProjectThread,
  onRemoveProject,
  onRelocateProject,
  onReorderSections,
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
  activeProjectID?: string;
  pinnedThreads: ThreadSummary[];
  activeThreadID?: string;
  pendingThreadID?: string;
  pendingProjectID?: string;
  collapsedSidebarSectionIDs: Set<string>;
  expandedSidebarSectionIDs: Set<string>;
  loadingProjectThreadIDs?: ReadonlySet<string>;
  projectThreadsByProjectID: Record<string, ThreadSummary[]>;
  projectMenuOpen: boolean;
  projectMenuRef: RefObject<HTMLDivElement | null>;
  searchOpen: boolean;
  debugFixturesVisible: boolean;
  // Order of reorderable sections. The pinned section is rendered first
  // (fixed position) and is NOT included. Each key maps to either
  // SCRATCH_PSEUDO_PROJECT_ID or a real project id.
  sectionOrder: string[];
  onStartNewThread: () => void;
  onOpenAutomationsTab: () => void;
  onOpenSkillsTab: () => void;
  groupChatEnabled?: boolean;
  // Unified 协作 section: the room list (with per-room unread counts) is
  // polled at the App level and passed down so the sidebar and the channel
  // canvas never disagree about what needs attention.
  channelRooms?: ChannelRoom[];
  activeChannelRoomID?: string;
  // Which channel canvas is on screen, or null when channels are closed —
  // drives the Agents / 任务 entry row highlights.
  activeChannelSection?: "rooms" | "agents" | "tasks" | null;
  onSelectChannelRoom?: (roomID: string) => void;
  onOpenChannelAgents?: () => void;
  onOpenChannelTasks?: () => void;
  onOpenChannels?: () => void;
  onCreateChannelRoom?: () => void;
  onToggleConversationSearch: () => void;
  onSeedConversationFixture: (kind: ConversationFixtureKind) => void;
  onSeedAgentTreeDemo: () => void;
  onOpenChipGallery: () => void;
  onSelectThread: (id: string) => void;
  onTogglePinned: (thread: ThreadSummary) => void;
  onArchiveThread: (thread: ThreadSummary) => void;
  onDeleteThread: (thread: ThreadSummary) => void;
  onRenameThread: (thread: ThreadSummary, title: string) => void;
  onToggleProjectMenu: () => void;
  onCreateProject: () => void;
  onOpenProjectFolder: () => void;
  onToggleSidebarSectionCollapsed: (id: string) => void;
  onSelectProjectWorkspace?: (id: string) => void;
  onStartNewThreadForProject: (id: string) => void;
  onSelectProjectThread: (projectID: string, threadID: string) => void;
  onRemoveProject: (id: string) => void;
  onRelocateProject: (id: string) => void;
  // Fires when the user drops a reorderable sidebar section in a new
  // position. The next array is the FULL sectionOrder with the moved
  // entry swapped into place. App.tsx persists this via the same
  // sidebarSectionOrder effect that already exists. Optional so test
  // harnesses and other consumers can render the sidebar without
  // wiring persistence — production callers (App.tsx) always provide it.
  onReorderSections?: (nextOrder: string[]) => void;
  onPointerEnter?: () => void;
  onPointerLeave?: (event: ReactPointerEvent<HTMLElement>) => void;
  onOpenSettings: () => void;
}): JSX.Element {
  const { t } = useI18n();
  const hasRuntimeContext = Boolean(state.activeContext);
  const fixturesEnabled = hasRuntimeContext && Boolean(state.initialized);
  // The scratch pseudo project is "active" when the runtime context is in
  // no-project mode (i.e. the user is viewing a scratch conversation).
  // Active state is passed into ProjectList so the row highlights even though
  // it has no DesktopProject entry in state.projects.
  const sidebarScratchPseudoActive = state.activeContext?.kind === "no_project";

  // Drag-and-drop reorder wiring for the reorderable sections. The 6px
  // activation distance lets plain clicks on the header (collapse toggle)
  // and on threads inside the section pass through without triggering a
  // drag — matches SessionTabs so the two surfaces share a feel.
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
  );
  const [draggingSectionID, setDraggingSectionID] = useState<string | undefined>();
  function handleDragStart(event: DragStartEvent): void {
    setDraggingSectionID(String(event.active.id));
  }
  function handleDragEnd(event: DragEndEvent): void {
    const activeID = String(event.active.id);
    const overID = event.over ? String(event.over.id) : undefined;
    const next = reorderSidebarSections(sectionOrder, activeID, overID);
    if (next !== sectionOrder) {
      onReorderSections?.(next);
    }
    setDraggingSectionID(undefined);
  }
  function handleDragCancel(): void {
    setDraggingSectionID(undefined);
  }
  // Each SortableSection calls back into this registry on every render
  // with its id → header info. The DragOverlay reads from this map to
  // render the right header for the currently dragged section. Using a
  // ref (not state) avoids an extra render when a child mounts and
  // keeps the lookup cheap during high-frequency drag pointer moves.
  const sectionHeaderInfoByIDRef = useRef<Map<string, SectionHeaderInfo>>(new Map());
  const registerSectionHeaderInfo = useCallback(
    (id: string, info: SectionHeaderInfo | null): void => {
      if (info === null) {
        sectionHeaderInfoByIDRef.current.delete(id);
      } else {
        sectionHeaderInfoByIDRef.current.set(id, info);
      }
    },
    [],
  );
  const draggingSectionInfo = draggingSectionID
    ? sectionHeaderInfoByIDRef.current.get(draggingSectionID)
    : undefined;
  const pinnedRows = pinnedThreads;
  const pinnedCollapsed = collapsedSidebarSectionIDs.has(
    SIDEBAR_SECTION_PINNED,
  );
  const collabCollapsed = collapsedSidebarSectionIDs.has(
    SIDEBAR_SECTION_COLLAB,
  );
  const collabUnreadTotal = channelRooms.reduce(
    (sum, room) => sum + (room.unread_count ?? 0),
    0,
  );
  const functionalGroups = ([{
    id: "workspace" as const,
    label: t("sidebar.workspace"),
    sectionIDs: sectionOrder,
  }] satisfies Array<{
    id: "workspace";
    label: string;
    sectionIDs: string[];
  }>).filter((group) => group.sectionIDs.length > 0);

  return (
    <aside
      className="sidebar"
      onPointerEnter={onPointerEnter}
      onPointerLeave={onPointerLeave}
    >
      <div className="sidebar-content">
        <div className="traffic-spacer" />
        {/*
          * Brand placeholder at the top of the sidebar visible area.
          * 等真正的 logo / wordmark / lockup 设计落地后整体替换。
          * 见 desktop/DESIGN.md 的"克制"原则：用最少的署名时刻承载识别度。
          */}
        <div className="sidebar-brand">
          <span className="sidebar-brand-wordmark">wuu</span>
        </div>
        <nav className="primary-nav" aria-label={t("sidebar.mainNavigation")}>
          <button
            className="nav-item"
            onClick={onStartNewThread}
            disabled={!hasRuntimeContext}
          >
            <MessageSquarePlus className="icon-lg" />
            <span>{t("sidebar.newConversation")}</span>
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
            <span>{t("sidebar.searchConversations")}</span>
          </button>
          <button
            className="nav-item"
            onClick={onOpenAutomationsTab}
            disabled={!hasRuntimeContext}
          >
            <AlarmClock className="icon-lg" />
            <span>{t("automations.title")}</span>
          </button>
          <button
            className="nav-item"
            onClick={onOpenSkillsTab}
            disabled={!hasRuntimeContext}
          >
            <Wrench className="icon-lg" />
            <span>{t("skills.sectionSkills")}</span>
          </button>
          {debugFixturesVisible ? (
            <div className="dev-fixture-nav" aria-label={t("sidebar.devFixtures.label")}>
              <div className="dev-fixture-label">{t("sidebar.devFixtures.title")}</div>
              <button
                className="nav-item dev-fixture-button"
                onClick={() => onSeedConversationFixture("long")}
                disabled={!fixturesEnabled}
              >
                <FileText className="icon" />
                <span>{t("sidebar.devFixtures.longConversation")}</span>
              </button>
              <button
                className="nav-item dev-fixture-button"
                onClick={() => onSeedConversationFixture("rich")}
                disabled={!fixturesEnabled}
              >
                <ListIcon className="icon" />
                <span>{t("sidebar.devFixtures.richContent")}</span>
              </button>
              <button
                className="nav-item dev-fixture-button"
                onClick={() => onSeedConversationFixture("running")}
                disabled={!fixturesEnabled}
              >
                <Clock className="icon" />
                <span>{t("sidebar.devFixtures.running")}</span>
              </button>
              <button
                className="nav-item dev-fixture-button"
                onClick={() => onSeedConversationFixture("compact")}
                disabled={!fixturesEnabled}
              >
                <Archive className="icon" />
                <span>{t("sidebar.devFixtures.compaction")}</span>
              </button>
              <button
                className="nav-item dev-fixture-button"
                onClick={onSeedAgentTreeDemo}
                disabled={!fixturesEnabled}
              >
                <CornerDownRight className="icon" />
                <span>{t("sidebar.devFixtures.subtasks")}</span>
              </button>
              <button
                className="nav-item dev-fixture-button"
                onClick={onOpenChipGallery}
              >
                <LayoutGrid className="icon" />
                <span>{t("sidebar.devFixtures.chipGallery")}</span>
              </button>
            </div>
          ) : null}
        </nav>

        <div className="sidebar-main scrollbar-hidden">
          {pinnedRows.length > 0 ? (
            <section
              className="sidebar-functional-group pinned-functional-group"
              aria-label={t("sidebar.pinned")}
            >
              <div className="sidebar-functional-group-body">
                <div className="pinned-thread-section">
                  <SidebarSection
                    expanded={!pinnedCollapsed}
                    iconKind="pinned"
                    CollapsedIcon={Pin}
                    ExpandedIcon={Pin}
                    label={t("sidebar.pinned")}
                    ariaLabel={t(pinnedCollapsed ? "sidebar.expandSection" : "sidebar.collapseSection", { section: t("sidebar.pinned") })}
                    title={t(pinnedCollapsed ? "sidebar.expandSection" : "sidebar.collapseSection", { section: t("sidebar.pinned") })}
                    onToggle={() =>
                      onToggleSidebarSectionCollapsed(SIDEBAR_SECTION_PINNED)
                    }
                  >
                    <PinnedThreadList
                      threads={pinnedRows}
                      activeID={activeThreadID}
                      pendingThreadID={pendingThreadID}
                      
                      lastViewedTurnByThreadID={state.lastViewedTurnByThreadID}
                      onSelect={onSelectThread}
                      onTogglePinned={onTogglePinned}
                      onArchive={onArchiveThread}
                      onDelete={onDeleteThread}
                      onRename={onRenameThread}
                      
                    />
                  </SidebarSection>
                </div>
              </div>
            </section>
          ) : null}
          {groupChatEnabled ? (
            <section
              className="sidebar-functional-group collab-functional-group"
              aria-label={t("sidebar.collaboration")}
            >
              <div className="sidebar-functional-group-body">
                <div className="collab-section">
                  <SidebarSection
                    expanded={!collabCollapsed}
                    iconKind="collab"
                    CollapsedIcon={Hash}
                    ExpandedIcon={Hash}
                    label={t("sidebar.collaboration")}
                    ariaLabel={t(collabCollapsed ? "sidebar.expandSection" : "sidebar.collapseSection", { section: t("sidebar.collaboration") })}
                    title={t(collabCollapsed ? "sidebar.expandSection" : "sidebar.collapseSection", { section: t("sidebar.collaboration") })}
                    unread={collabCollapsed && collabUnreadTotal > 0}
                    onToggle={() =>
                      onToggleSidebarSectionCollapsed(SIDEBAR_SECTION_COLLAB)
                    }
                    newItemButton={
                      <button
                        className="sidebar-row-icon-button project-row-new-thread"
                        type="button"
                        aria-label={t("channels.newRoom")}
                        title={t("channels.newRoom")}
                        onClick={onCreateChannelRoom ?? onOpenChannels}
                      >
                        <Plus className="icon" />
                      </button>
                    }
                    emptyNote={t("sidebar.collaborationEmpty")}
                  >
                    <div className="sidebar-collab-list">
                      {channelRooms.map((room) => {
                        const isActiveRoom = room.id === activeChannelRoomID;
                        const unread = room.unread_count ?? 0;
                        return (
                          <button
                            key={room.id}
                            className={`thread-row sidebar-room-row${isActiveRoom ? " active" : ""}`}
                            type="button"
                            aria-current={isActiveRoom ? "page" : undefined}
                            onClick={() => onSelectChannelRoom?.(room.id)}
                          >
                            <Hash className="icon sidebar-room-icon" aria-hidden="true" />
                            <span className="sidebar-room-name">{room.name}</span>
                            {!isActiveRoom && unread > 0 ? (
                              <span
                                className="channel-room-unread"
                                aria-label={t("channels.unreadMessages", { count: unread })}
                              >
                                {formatChannelUnreadCount(unread)}
                              </span>
                            ) : null}
                          </button>
                        );
                      })}
                      <button
                        className={`thread-row sidebar-room-row sidebar-collab-entry${activeChannelSection === "agents" ? " active" : ""}`}
                        type="button"
                        aria-current={activeChannelSection === "agents" ? "page" : undefined}
                        onClick={onOpenChannelAgents}
                      >
                        <UsersRound className="icon sidebar-room-icon" aria-hidden="true" />
                        <span className="sidebar-room-name">{t("channels.agents")}</span>
                      </button>
                      <button
                        className={`thread-row sidebar-room-row sidebar-collab-entry${activeChannelSection === "tasks" ? " active" : ""}`}
                        type="button"
                        aria-current={activeChannelSection === "tasks" ? "page" : undefined}
                        onClick={onOpenChannelTasks}
                      >
                        <ClipboardList className="icon sidebar-room-icon" aria-hidden="true" />
                        <span className="sidebar-room-name">{t("channels.tasks")}</span>
                      </button>
                    </div>
                  </SidebarSection>
                </div>
              </div>
            </section>
          ) : null}
          {sectionOrder.length > 0 ? (
            <DndContext
              sensors={sensors}
              collisionDetection={closestCenter}
              modifiers={[restrictToVerticalAxis]}
              onDragStart={handleDragStart}
              onDragEnd={handleDragEnd}
              onDragCancel={handleDragCancel}
            >
              <SortableContext
                items={sectionOrder}
                strategy={verticalListSortingStrategy}
              >
                {functionalGroups.map((group) => (
                  <section
                    key={group.id}
                    className="sidebar-functional-group"
                    aria-label={group.label}
                  >
                    <div className="sidebar-functional-heading">
                      <span className="sidebar-functional-heading-label">
                        {group.label}
                      </span>
                      {group.id === "workspace" ? (
                        <div
                          className="sidebar-add-workspace"
                          ref={projectMenuRef}
                        >
                          <button
                            className="sidebar-functional-action"
                            type="button"
                            aria-label={t("sidebar.addWorkspace")}
                            title={t("sidebar.addWorkspace")}
                            aria-haspopup="menu"
                            aria-expanded={projectMenuOpen}
                            onClick={onToggleProjectMenu}
                          >
                            <Plus aria-hidden="true" />
                          </button>
                          {projectMenuOpen ? (
                            <div className="project-add-menu" role="menu">
                              <button role="menuitem" onClick={onCreateProject}>
                                <FolderPlus className="icon-xl" />
                                <span>{t("sidebar.newBlankProject")}</span>
                              </button>
                              <button role="menuitem" onClick={onOpenProjectFolder}>
                                <FolderOpen className="icon-xl" />
                                <span>{t("sidebar.useExistingFolder")}</span>
                              </button>
                            </div>
                          ) : null}
                        </div>
                      ) : null}
                    </div>
                    <div className="sidebar-functional-group-body">
                      {group.sectionIDs.map((key) => {
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
              ? t("sidebar.project")
              : t("sidebar.projectNamed", { name: project.name });
            return (
              <SortableSection
                key={key}
                id={key}
                className="project-section"
                ariaLabel={sectionAriaLabel}
                headerInfo={{
                  label: isScratchPseudo ? t("sidebar.conversations") : project.name,
                  iconKind: isScratchPseudo ? "conversation" : "project",
                  CollapsedIcon: isScratchPseudo ? MessageSquare : Folder,
                  ExpandedIcon: isScratchPseudo ? MessagesSquare : FolderOpen,
                }}
                registerHeaderInfo={registerSectionHeaderInfo}
              >
                <ProjectGroup
                  project={project}
                  activeID={activeProjectID ?? state.activeProjectId}
                  pendingProjectID={pendingProjectID}
                  expandedSidebarSectionIDs={expandedSidebarSectionIDs}
                  loadingProjectThreadIDs={loadingProjectThreadIDs}
                  threadsByProjectID={projectThreadsByProjectID}
                  activeThreadID={activeThreadID}
                  pendingThreadID={pendingThreadID}
                  
                  lastViewedTurnByThreadID={state.lastViewedTurnByThreadID}
                  scratchPseudoProjectID={SCRATCH_PSEUDO_PROJECT_ID}
                  scratchPseudoActive={sidebarScratchPseudoActive}
                  onToggleSidebarSectionCollapsed={
                    onToggleSidebarSectionCollapsed
                  }
                  onSelectProjectWorkspace={onSelectProjectWorkspace}
                  onStartNewThread={onStartNewThreadForProject}
                  onSelectThread={onSelectProjectThread}
                  onToggleThreadPinned={onTogglePinned}
                  onArchiveThread={onArchiveThread}
                  onDeleteThread={onDeleteThread}
                  onRenameThread={onRenameThread}
                  
                  onRemoveProject={onRemoveProject}
                  onRelocateProject={onRelocateProject}
                />
              </SortableSection>
            );
                      })}
                    </div>
                  </section>
                ))}
              </SortableContext>
              <DragOverlay
                dropAnimation={{
                  duration: 150,
                  easing: "cubic-bezier(0.16, 1, 0.3, 1)",
                }}
              >
                {draggingSectionInfo ? (
                  <SectionDragPreview info={draggingSectionInfo} />
                ) : null}
              </DragOverlay>
            </DndContext>
          ) : null}
        </div>
        <div className="sidebar-settings">
          <button
            className="sidebar-settings-button"
            type="button"
            disabled={!state.initialized}
            onClick={onOpenSettings}
          >
            <Settings className="icon-lg" />
            <span>{t("sidebar.settings")}</span>
          </button>
        </div>
      </div>
    </aside>
  );
}
