import {
  Archive,
  ChevronRight,
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
  UserRound,
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
import type {
  DesktopProject,
  ParticipantProfile,
  ParticipantSaveParams,
  ProviderSummary,
} from "../shared/protocol";
import type { AppState, ThreadSummary } from "./AppState";
import type { ConversationFixtureKind } from "./ConversationFixtures";
import { isGroupThread, SCRATCH_PSEUDO_PROJECT_ID } from "./AppState";
import { PinnedThreadList, ProjectGroup } from "./ThreadSidebar";
import { baseThreadTitle } from "./ThreadTitles";
import { SidebarSection, SidebarSectionDragHandleContext } from "./SidebarSection";
import { ThreadContextMenu } from "./ThreadContextMenu";
import { SidebarNameDialog } from "./SidebarNameDialog";
import { NewParticipantDialog } from "./NewParticipantDialog";
import { ENABLE_COLLABORATION } from "./FeatureFlags";

/**
 * Stable section identity keys for the new sidebar tree.
 *
 * - `SIDEBAR_SECTION_PINNED` is FIXED-position (always first, above the
 *   reorderable list). It is intentionally NOT in `sectionOrder`.
 * - `SIDEBAR_SECTION_AGENTS` and `SIDEBAR_SECTION_GROUP` belong to the fixed
 *   collaboration functional group. Their relative order is customizable.
 * - `SCRATCH_PSEUDO_PROJECT_ID` ("__wuu_scratch__") is the 对话
 *   pseudo-project entry. It belongs to the workspace functional group and
 *   can be reordered with real projects.
 */
export const SIDEBAR_SECTION_PINNED = "__wuu_pinned__";
export const SIDEBAR_SECTION_AGENTS = "__wuu_agents__";
export const SIDEBAR_SECTION_GROUP = "__wuu_group__";

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
 *      included it). AGENTS and GROUP are allowed through — both are
 *      part of the reorderable list
 *      (sidebar-groups-andy-workspaces.md §2).
 *   2. Append newly-seen project ids in `projectIDs` order.
 *   3. Normalize legacy flat orders into fixed functional groups while
 *      preserving relative order inside collaboration and workspace.
 */
export function reconcileSidebarSectionOrder(
  stored: string[] | undefined,
  projectIDs: string[],
  collaborationEnabled = ENABLE_COLLABORATION,
): string[] {
  const knownIDs = new Set<string>([
    SCRATCH_PSEUDO_PROJECT_ID,
    ...projectIDs,
  ]);
  if (collaborationEnabled) {
    knownIDs.add(SIDEBAR_SECTION_AGENTS);
    knownIDs.add(SIDEBAR_SECTION_GROUP);
  }
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
  if (collaborationEnabled) {
    if (!out.includes(SIDEBAR_SECTION_AGENTS)) {
      out.unshift(SIDEBAR_SECTION_AGENTS);
    }
    if (!out.includes(SIDEBAR_SECTION_GROUP)) {
      // Migration for orders persisted before 群聊 joined the reorderable
      // list: seed it at the head, matching its previous fixed spot right
      // below 置顶. A stored GROUP position is preserved above.
      out.unshift(SIDEBAR_SECTION_GROUP);
    }
  }
  const collaboration = collaborationEnabled
    ? out.filter(
        (id) => id === SIDEBAR_SECTION_GROUP || id === SIDEBAR_SECTION_AGENTS,
      )
    : [];
  const workspaces = out.filter(
    (id) => id !== SIDEBAR_SECTION_GROUP && id !== SIDEBAR_SECTION_AGENTS,
  );
  return [...collaboration, ...workspaces];
}

type SidebarFunctionalGroupID = "collaboration" | "workspace";

function functionalGroupForSectionID(
  sectionID: string,
): SidebarFunctionalGroupID | undefined {
  if (sectionID === SIDEBAR_SECTION_PINNED) return undefined;
  if (
    sectionID === SIDEBAR_SECTION_GROUP ||
    sectionID === SIDEBAR_SECTION_AGENTS
  ) {
    return "collaboration";
  }
  return "workspace";
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
  const activeGroup = functionalGroupForSectionID(activeId);
  if (activeGroup !== functionalGroupForSectionID(overId)) return order;
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
  pinnedThreads,
  groupThreads,
  activeThreadID,
  activeDMParticipantID,
  dmThreadByParticipantID,
  unreadDMParticipantIDs,
  participants,
  busyParticipantIDs,
  pendingThreadID,
  pendingProjectID,
  collapsedSidebarSectionIDs,
  expandedSidebarSectionIDs,
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
  onSelectThread,
  onSelectParticipant,
  onSaveParticipant,
  onCreateGroupThread,
  onTogglePinned,
  onArchiveThread,
  onDeleteThread,
  onRenameThread,
  onToggleProjectMenu,
  onCreateProject,
  onOpenProjectFolder,
  onToggleSidebarSectionCollapsed,
  onStartNewThreadForProject,
  onSelectProjectThread,
  onRemoveProject,
  onRelocateProject,
  onReorderSections,
  onPointerEnter,
  onPointerLeave,
  onOpenSettings,
  providers,
}: {
  state: AppState;
  // The sidebar renders scratch conversations through the same ProjectList
  // path as real projects, so App.tsx prepends a synthetic DesktopProject
  // (id = SCRATCH_PSEUDO_PROJECT_ID) into this array. The original
  // state.projects list is unchanged; sidebarProjects is what the sidebar
  // actually shows.
  sidebarProjects: DesktopProject[];
  pinnedThreads: ThreadSummary[];
  // Group (chat-style) threads for the sidebar's 群聊 section, between
  // 置顶 and 对话 (chat-style-threads-design.md §1, §3). Empty when the
  // roster has no named agents yet (the backend auto-creates the # all
  // channel once one exists) — the section then renders the inert
  // "# all" placeholder instead.
  groupThreads: ThreadSummary[];
  activeThreadID?: string;
  // The active thread's dm_participant_id (when the active thread is a
  // DM with a named participant). Drives the highlighted row in the agent
  // roster; collapses to undefined for any non-DM thread.
  activeDMParticipantID?: string;
  // Most-recent non-archived DM thread per participant id, derived in
  // App.tsx. Drives the context menu's pin/unpin DM affordance and any
  // future per-row DM metadata.
  dmThreadByParticipantID: Map<string, ThreadSummary>;
  // Participants whose DM thread holds an unseen actual chat message.
  // Drives the `.has-unread` indicator on the roster row so the user can
  // spot new messages at a glance without opening the conversation.
  unreadDMParticipantIDs: Set<string>;
  participants: ParticipantProfile[];
  // Set of participant IDs with at least one running run. Drives the busy
  // status dot in the roster. Derived in App.tsx by walking child agents
  // across all threads; participants not in the set render as online.
  busyParticipantIDs: ReadonlySet<string>;
  pendingThreadID?: string;
  pendingProjectID?: string;
  collapsedSidebarSectionIDs: Set<string>;
  expandedSidebarSectionIDs: Set<string>;
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
  onSelectThread: (id: string) => void;
  // Fires when the user clicks an agent row. The current product flow
  // routes this to opening (or creating) the DM conversation with that
  // named participant; profile editing lives in the local floating dialog.
  onSelectParticipant: (participant: ParticipantProfile) => void;
  // Fires when the user clicks the Agents section's + button (or the
  // Saves create and edit submissions from NewParticipantDialog. App.tsx
  // upserts the returned participant without opening the right-side profile
  // panel. Optional so test harnesses can render the sidebar without wiring
  // participant persistence.
  onSaveParticipant?: (params: ParticipantSaveParams) => Promise<ParticipantProfile>;
  // Provider summaries from the runtime, used to populate the model
  // SelectMenu in the NewParticipantDialog. Optional so the dialog can
  // still render with just the "跟随全局" fallback.
  providers?: ProviderSummary[];
  // Fires when the user submits a name in the 群聊 section's inline
  // creator. App.tsx starts the group thread via
  // startThread({ group: true, title }) and selects it. Optional so test
  // harnesses can render the sidebar without wiring thread creation.
  onCreateGroupThread?: (title: string) => void;
  onImportParticipants: (file: File) => void;
  onExportParticipants: () => void;
  onTogglePinned: (thread: ThreadSummary) => void;
  onArchiveThread: (thread: ThreadSummary) => void;
  onDeleteThread: (thread: ThreadSummary) => void;
  onRenameThread: (thread: ThreadSummary, title: string) => void;
  onToggleProjectMenu: () => void;
  onCreateProject: () => void;
  onOpenProjectFolder: () => void;
  onToggleSidebarSectionCollapsed: (id: string) => void;
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
  const hasRuntimeContext = Boolean(state.activeContext);
  const fixturesEnabled = hasRuntimeContext && Boolean(state.initialized);
  // Floating create-group dialog for the 群聊 section: the + button opens a
  // shared overlay (same shell as the rename-conversation dialog) that
  // submits on Enter and dismisses on Escape / overlay click. Controlled
  // title state keeps the input value available to both the form and the
  // commit handler.
  const [creatingGroupThread, setCreatingGroupThread] = useState(false);
  const [groupNameDraft, setGroupNameDraft] = useState("");
  // Floating agent dialog shared by create and edit. The + button starts an
  // empty form; the row context menu seeds the same form from the participant.
  const [creatingParticipant, setCreatingParticipant] = useState(false);
  const [editingParticipant, setEditingParticipant] =
    useState<ParticipantProfile>();
  // Right-click menu on an individual agent row. The menu hosts the
  // profile-editing entry plus a pin/unpin DM affordance that drives off
  // the latest DM thread for that participant. The roster header's own
  // overflow menu is unchanged; only the per-row context menu is new.
  const [rosterContextMenu, setRosterContextMenu] = useState<{
    x: number;
    y: number;
    participant: ParticipantProfile;
  } | null>(null);
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
  const pinnedRows: ThreadSummary[] = pinnedThreads.map((thread) =>
    isGroupThread(thread)
      ? {
          ...thread,
          title: `#${baseThreadTitle(thread, pinnedThreads)}`,
        }
      : thread,
  );
  const pinnedCollapsed = collapsedSidebarSectionIDs.has(
    SIDEBAR_SECTION_PINNED,
  );
  const functionalGroups = ([
    ...(ENABLE_COLLABORATION
      ? [
          {
            id: "collaboration" as const,
            label: "协作",
            sectionIDs: sectionOrder.filter(
              (id) =>
                id === SIDEBAR_SECTION_GROUP || id === SIDEBAR_SECTION_AGENTS,
            ),
          },
        ]
      : []),
    {
      id: "workspace" as const,
      label: "工作区",
      sectionIDs: sectionOrder.filter(
        (id) =>
          !ENABLE_COLLABORATION ||
          (id !== SIDEBAR_SECTION_GROUP && id !== SIDEBAR_SECTION_AGENTS),
      ),
    },
  ] satisfies Array<{
    id: SidebarFunctionalGroupID;
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
          <button
            className="nav-item"
            onClick={onOpenSkillsTab}
            disabled={!hasRuntimeContext}
          >
            <Wrench className="icon-lg" />
            <span>Skills</span>
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
          {pinnedRows.length > 0 ? (
            <section
              className="sidebar-functional-group pinned-functional-group"
              aria-label="置顶"
            >
              <div className="sidebar-functional-group-body">
                <div className="pinned-thread-section">
                  <SidebarSection
                    expanded={!pinnedCollapsed}
                    iconKind="pinned"
                    CollapsedIcon={Pin}
                    ExpandedIcon={Pin}
                    label="置顶"
                    ariaLabel={`${pinnedCollapsed ? "展开" : "收起"}置顶`}
                    title={pinnedCollapsed ? "展开置顶" : "收起置顶"}
                    onToggle={() =>
                      onToggleSidebarSectionCollapsed(SIDEBAR_SECTION_PINNED)
                    }
                  >
                    <PinnedThreadList
                      threads={pinnedRows}
                      activeID={activeThreadID}
                      pendingThreadID={pendingThreadID}
                      
                      lastViewedTurnByThreadID={state.lastViewedTurnByThreadID}
                      lastViewedMessageSeqByThreadID={state.lastViewedMessageSeqByThreadID}
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
                            aria-label="添加工作区"
                            title="添加工作区"
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
                                <span>新建空白项目</span>
                              </button>
                              <button role="menuitem" onClick={onOpenProjectFolder}>
                                <FolderOpen className="icon-xl" />
                                <span>使用现有文件夹</span>
                              </button>
                            </div>
                          ) : null}
                        </div>
                      ) : null}
                    </div>
                    <div className="sidebar-functional-group-body">
                      {group.sectionIDs.map((key) => {
            if (key === SIDEBAR_SECTION_GROUP) {
              // 群聊 — first-class reorderable section
              // (sidebar-groups-andy-workspaces.md §2). The backend
              // guarantees the # all channel exists once the roster is
              // non-empty, so an empty list just shows the empty note —
              // no placeholder row.
              const groupCollapsed = collapsedSidebarSectionIDs.has(
                SIDEBAR_SECTION_GROUP,
              );
              const groupRows: ThreadSummary[] = groupThreads.map((thread) => ({
                ...thread,
                title: `#${baseThreadTitle(thread, groupThreads)}`,
              }));
              const openGroupThreadDialog = (): void => {
                setGroupNameDraft("");
                setCreatingGroupThread(true);
              };
              const groupActions = (
                <div className="sidebar-section-actions group-thread-actions">
                  <button
                    type="button"
                    className="participant-roster-add"
                    aria-label="新建群聊"
                    title="新建群聊"
                    disabled={!state.initialized}
                    onClick={openGroupThreadDialog}
                  >
                    <Plus aria-hidden="true" />
                  </button>
                </div>
              );
              return (
                <SortableSection
                  key={SIDEBAR_SECTION_GROUP}
                  id={SIDEBAR_SECTION_GROUP}
                  className="group-thread-section"
                  ariaLabel="群聊"
                  headerInfo={{
                    label: "群聊",
                    iconKind: "group",
                    CollapsedIcon: Hash,
                    ExpandedIcon: Hash,
                  }}
                  registerHeaderInfo={registerSectionHeaderInfo}
                >
                  <SidebarSection
                    expanded={!groupCollapsed}
                    iconKind="group"
                    CollapsedIcon={Hash}
                    ExpandedIcon={Hash}
                    label="群聊"
                    ariaLabel={`${groupCollapsed ? "展开" : "收起"}群聊`}
                    title={groupCollapsed ? "展开群聊" : "收起群聊"}
                    onToggle={() =>
                      onToggleSidebarSectionCollapsed(SIDEBAR_SECTION_GROUP)
                    }
                    actions={groupActions}
                  >
                    {groupRows.length > 0 ? (
                      <PinnedThreadList
                        threads={groupRows}
                        activeID={activeThreadID}
                        pendingThreadID={pendingThreadID}
                        
                        lastViewedTurnByThreadID={state.lastViewedTurnByThreadID}
                        lastViewedMessageSeqByThreadID={state.lastViewedMessageSeqByThreadID}
                        onSelect={onSelectThread}
                        onTogglePinned={onTogglePinned}
                        onArchive={onArchiveThread}
                        onDelete={onDeleteThread}
                        onRename={onRenameThread}
                        
                      />
                    ) : (
                      <div className="sidebar-section-empty-list">
                        <button
                          type="button"
                          className="sidebar-section-empty-note sidebar-section-empty-note-action"
                          disabled={!state.initialized}
                          onClick={openGroupThreadDialog}
                        >
                          添加群聊
                        </button>
                      </div>
                    )}
                  </SidebarSection>
                </SortableSection>
              );
            }
            if (key === SIDEBAR_SECTION_AGENTS) {
              const collapsed = collapsedSidebarSectionIDs.has(
                SIDEBAR_SECTION_AGENTS,
              );
              // Pinning MOVES a conversation under 置顶 — same semantics as
              // the 对话 and 群聊 lists. A participant whose DM is pinned is
              // represented by its pinned thread row, so its roster row is
              // hidden here; unpinning brings it back.
              const visibleParticipants = participants.filter(
                (participant) =>
                  !dmThreadByParticipantID.get(participant.id)?.pinned,
              );
              const rosterMenu = rosterContextMenu ? (
                <ThreadContextMenu
                  x={rosterContextMenu.x}
                  y={rosterContextMenu.y}
                  items={[
                    {
                      label: "编辑设定",
                      onSelect: () =>
                        setEditingParticipant(rosterContextMenu.participant),
                    },
                    {
                      label: (() => {
                        const dm = dmThreadByParticipantID.get(
                          rosterContextMenu.participant.id,
                        );
                        return dm?.pinned ? "取消置顶 DM" : "置顶 DM";
                      })(),
                      disabled: !dmThreadByParticipantID.has(
                        rosterContextMenu.participant.id,
                      ),
                      onSelect: () => {
                        const dm = dmThreadByParticipantID.get(
                          rosterContextMenu.participant.id,
                        );
                        if (!dm) return;
                        onTogglePinned(dm);
                      },
                    },
                  ]}
                  onClose={() => setRosterContextMenu(null)}
                />
              ) : null;
              const rosterActions = (
                <div className="sidebar-section-actions participant-roster-actions">
                  <button
                    type="button"
                    className="participant-roster-add"
                    aria-label="新建 Agent"
                    title="新建 Agent"
                    disabled={!state.initialized}
                    onClick={() => {
                      setCreatingParticipant(true);
                    }}
                  >
                    <Plus aria-hidden="true" />
                  </button>
                </div>
              );
              return (
                <SortableSection
                  key={SIDEBAR_SECTION_AGENTS}
                  id={SIDEBAR_SECTION_AGENTS}
                  className="participant-roster-section"
                  ariaLabel="Agents"
                  headerInfo={{
                    label: "Agents",
                    iconKind: "agents",
                    CollapsedIcon: UserRound,
                    ExpandedIcon: UsersRound,
                  }}
                  registerHeaderInfo={registerSectionHeaderInfo}
                >
                  <SidebarSection
                    expanded={!collapsed}
                    iconKind="agents"
                    CollapsedIcon={UserRound}
                    ExpandedIcon={UsersRound}
                    label="Agents"
                    ariaLabel={`${collapsed ? "展开" : "收起"} Agents`}
                    title={collapsed ? "展开 Agents" : "收起 Agents"}
                    onToggle={() =>
                      onToggleSidebarSectionCollapsed(SIDEBAR_SECTION_AGENTS)
                    }
                    actions={rosterActions}
                  >
                    <div className="participant-roster-list">
                      {participants.length === 0 ? (
                        <button
                          type="button"
                          className="sidebar-section-empty-note sidebar-section-empty-note-action"
                          disabled={!state.initialized}
                          onClick={() => {
                            setCreatingParticipant(true);
                          }}
                        >
                          添加 Agent
                        </button>
                      ) : (
                        visibleParticipants.map((participant) => {
                          const isBusy = busyParticipantIDs.has(participant.id);
                          const isActiveDM =
                            activeDMParticipantID === participant.id;
                          const isUnread = unreadDMParticipantIDs.has(participant.id);
                          const status = isBusy ? "busy" : "online";
                          const dm = dmThreadByParticipantID.get(participant.id);
                          // No placeholder glyph when the participant has no
                          // uploaded avatar: the name flows right after the
                          // status dot, and the avatar column only exists
                          // when there is an image to fill it.
                          const avatar = participant.avatar_image ? (
                            <span className="participant-roster-avatar" aria-hidden="true">
                              <img
                                className="participant-roster-avatar-image"
                                src={participant.avatar_image}
                                alt=""
                                loading="lazy"
                              />
                            </span>
                          ) : null;
                          const className = [
                            "participant-roster-row",
                            "sidebar-session-row",
                            isActiveDM ? "active" : "",
                            isUnread ? "has-unread" : "",
                          ]
                            .filter(Boolean)
                            .join(" ");
                          return (
                            <div
                              key={participant.id}
                              className={className}
                              onContextMenu={(event) => {
                                event.preventDefault();
                                setRosterContextMenu({
                                  x: event.clientX,
                                  y: event.clientY,
                                  participant,
                                });
                              }}
                            >
                              <button
                                type="button"
                                className="participant-roster-main"
                                aria-busy={isBusy}
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
                                </span>
                              </button>
                              {dm ? (
                                <div
                                  className="thread-row-actions"
                                  aria-label="DM 操作"
                                >
                                  <button
                                    className={`sidebar-row-icon-button thread-row-action ${dm.pinned ? "active" : ""}`}
                                    type="button"
                                    aria-label={dm.pinned ? "取消置顶" : "置顶"}
                                    title={dm.pinned ? "取消置顶" : "置顶"}
                                    onClick={() => onTogglePinned(dm)}
                                  >
                                    <Pin className="icon-sm" />
                                  </button>
                                  <button
                                    className="sidebar-row-icon-button thread-row-action archive"
                                    type="button"
                                    aria-label="归档"
                                    title="归档"
                                    onClick={() => onArchiveThread(dm)}
                                  >
                                    <Archive className="icon-sm" />
                                  </button>
                                </div>
                              ) : null}
                            </div>
                          );
                        })
                      )}
                    </div>
                  </SidebarSection>
                  {rosterMenu}
                </SortableSection>
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
              <SortableSection
                key={key}
                id={key}
                className="project-section"
                ariaLabel={sectionAriaLabel}
                headerInfo={{
                  label: isScratchPseudo ? "对话" : project.name,
                  iconKind: isScratchPseudo ? "conversation" : "project",
                  CollapsedIcon: isScratchPseudo ? MessageSquare : Folder,
                  ExpandedIcon: isScratchPseudo ? MessagesSquare : FolderOpen,
                }}
                registerHeaderInfo={registerSectionHeaderInfo}
              >
                <ProjectGroup
                  project={project}
                  activeID={state.activeProjectId}
                  pendingProjectID={pendingProjectID}
                  expandedSidebarSectionIDs={expandedSidebarSectionIDs}
                  threadsByProjectID={projectThreadsByProjectID}
                  activeThreadID={activeThreadID}
                  pendingThreadID={pendingThreadID}
                  
                  lastViewedTurnByThreadID={state.lastViewedTurnByThreadID}
                  lastViewedMessageSeqByThreadID={state.lastViewedMessageSeqByThreadID}
                  scratchPseudoProjectID={SCRATCH_PSEUDO_PROJECT_ID}
                  scratchPseudoActive={sidebarScratchPseudoActive}
                  onToggleSidebarSectionCollapsed={
                    onToggleSidebarSectionCollapsed
                  }
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
            <span>设置</span>
          </button>
        </div>
      </div>
      <SidebarNameDialog
        open={creatingGroupThread}
        title={groupNameDraft}
        onTitleChange={setGroupNameDraft}
        onSubmit={() => {
          const nextTitle = groupNameDraft.trim();
          if (nextTitle === "") {
            return;
          }
          setCreatingGroupThread(false);
          setGroupNameDraft("");
          onCreateGroupThread?.(nextTitle);
        }}
        onClose={() => {
          setCreatingGroupThread(false);
          setGroupNameDraft("");
        }}
        dialogTitle="新建群聊"
        dialogTitleId="group-thread-create-title"
        fieldLabel="群聊名称"
        fieldAriaLabel="群聊名称"
        placeholder="群聊名称"
        icon={Hash}
        submitLabel="创建"
        cancelLabel="取消"
      />
      <NewParticipantDialog
        open={creatingParticipant || Boolean(editingParticipant)}
        participant={editingParticipant}
        providers={providers}
        onSubmit={async (params) => {
          // The dialog owns its own saving/error UI; the sidebar is just the
          // messenger. App.tsx's onSaveParticipant handler is responsible
          // for calling saveParticipant, updating the participants list, and
          // returning the saved participant so the dialog can close on
          // success. Errors thrown here surface inline inside the dialog.
          if (!onSaveParticipant) {
            throw new Error("onSaveParticipant handler is not wired");
          }
          return onSaveParticipant(params);
        }}
        onCreated={() => {
          // No-op: closing is driven by the dialog itself on success. Kept
          // for symmetry with the prop signature and to give App.tsx a
          // hook for future side effects (e.g. focus a thread).
        }}
        onClose={() => {
          setCreatingParticipant(false);
          setEditingParticipant(undefined);
        }}
      />
    </aside>
  );
}
