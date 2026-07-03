import {
  Archive,
  ChevronRight,
  Clock,
  CornerDownRight,
  Download,
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
  MoreHorizontal,
  Pin,
  Plus,
  Search,
  Settings,
  ShieldCheck,
  Upload,
  UserRound,
  UsersRound,
  Wrench,
} from "lucide-react";
import {
  type CSSProperties,
  type HTMLAttributes,
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
import type { DesktopProject, ParticipantProfile } from "../shared/protocol";
import type { AppState, ThreadSummary } from "./AppState";
import type { ConversationFixtureKind } from "./ConversationFixtures";
import { SCRATCH_PSEUDO_PROJECT_ID } from "./AppState";
import { PinnedThreadList, ProjectGroup, ThreadRowTitle } from "./ThreadSidebar";
import { baseThreadTitle } from "./ThreadTitles";
import { SidebarSection, SidebarSectionDragHandleContext } from "./SidebarSection";
import { ThreadContextMenu } from "./ThreadContextMenu";

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
// Fixed-position (like SIDEBAR_SECTION_PINNED), rendered right after 置顶
// and before the reorderable Agents / 对话 / 项目 list
// (chat-style-threads-design.md §1). Not part of sectionOrder.
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
  onEditParticipant,
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
  pinnedThreads: ThreadSummary[];
  // Group (chat-style) threads for the sidebar's 群聊 section, between
  // 置顶 and 对话 (chat-style-threads-design.md §1, §3). Always empty
  // until the backend serializes Thread.group — the section then
  // renders the inert "# all" placeholder instead.
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
  // Participants whose DM thread has an unseen completed turn. Drives the
  // `.has-unread` indicator on the roster row so the user can spot new
  // messages at a glance without opening the conversation.
  unreadDMParticipantIDs: Set<string>;
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
  // Fires when the user clicks an agent row. The current product flow
  // routes this to opening (or creating) the DM conversation with that
  // named participant; profile editing lives behind the right-click menu
  // (see onEditParticipant).
  onSelectParticipant: (participant: ParticipantProfile) => void;
  // Fires when the user picks the "编辑设定" entry in the agent row's
  // right-click menu. Opens the profile editing side panel.
  onEditParticipant: (participant: ParticipantProfile) => void;
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
  // Fires when the user drops a reorderable sidebar section in a new
  // position. The next array is the FULL sectionOrder with the moved
  // entry swapped into place. App.tsx persists this via the same
  // sidebarSectionOrder effect that already exists. Optional so test
  // harnesses and other consumers can render the sidebar without
  // wiring persistence — production callers (App.tsx) always provide it.
  onReorderSections?: (nextOrder: string[]) => void;
  onPointerEnter?: () => void;
  onPointerLeave?: () => void;
  onOpenSettings: () => void;
}): JSX.Element {
  const hasRuntimeContext = Boolean(state.activeContext);
  const fixturesEnabled = hasRuntimeContext && Boolean(state.initialized);
  const participantTemplateInputRef = useRef<HTMLInputElement>(null);
  const rosterMenuRef = useRef<HTMLDivElement>(null);
  const [rosterMenuOpen, setRosterMenuOpen] = useState(false);
  // Right-click menu on an individual agent row. The menu hosts the
  // profile-editing entry plus a pin/unpin DM affordance that drives off
  // the latest DM thread for that participant. The roster header's own
  // overflow menu is unchanged; only the per-row context menu is new.
  const [rosterContextMenu, setRosterContextMenu] = useState<{
    x: number;
    y: number;
    participant: ParticipantProfile;
  } | null>(null);
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
          {(() => {
            // 置顶 is always visible — even when there are no pinned
            // threads — so the section reads as a stable container
            // alongside Agents / 对话 / projects. An empty body
            // surfaces a muted 还没有会话 placeholder row so the
            // collapse animation has real height to animate.
            const pinnedCollapsed = collapsedProjectIDs.has(
              SIDEBAR_SECTION_PINNED,
            );
            return (
              <section className="pinned-thread-section" aria-label="置顶">
                <SidebarSection
                  expanded={!pinnedCollapsed}
                  iconKind="pinned"
                  CollapsedIcon={Pin}
                  ExpandedIcon={Pin}
                  label="置顶"
                  ariaLabel={`${pinnedCollapsed ? "展开" : "收起"}置顶`}
                  title={pinnedCollapsed ? "展开置顶" : "收起置顶"}
                  onToggle={() =>
                    onToggleProjectCollapsed(SIDEBAR_SECTION_PINNED)
                  }
                  emptyNote="还没有会话"
                >
                  {pinnedThreads.length === 0 ? null : (
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
                  )}
                </SidebarSection>
              </section>
            );
          })()}
          {(() => {
            // 群聊 sits between 置顶 and 对话 (chat-style-threads-
            // design.md §1). Fixed position like 置顶 — group threads
            // are data-gated on the backend, so today the section is
            // always visible but its body is the inert "# all"
            // placeholder until Thread.group starts arriving.
            const groupCollapsed = collapsedProjectIDs.has(
              SIDEBAR_SECTION_GROUP,
            );
            const groupRows: ThreadSummary[] = groupThreads.map((thread) => ({
              ...thread,
              title: `#${baseThreadTitle(thread, groupThreads)}`,
            }));
            return (
              <section className="group-thread-section" aria-label="群聊">
                <SidebarSection
                  expanded={!groupCollapsed}
                  iconKind="group"
                  CollapsedIcon={Hash}
                  ExpandedIcon={Hash}
                  label="群聊"
                  ariaLabel={`${groupCollapsed ? "展开" : "收起"}群聊`}
                  title={groupCollapsed ? "展开群聊" : "收起群聊"}
                  onToggle={() =>
                    onToggleProjectCollapsed(SIDEBAR_SECTION_GROUP)
                  }
                  emptyNote="还没有群聊"
                >
                  {groupRows.length === 0 ? (
                    <div className="thread-list">
                      <div className="thread-row group-thread-row-placeholder disabled">
                        <button
                          className="thread-row-main"
                          type="button"
                          disabled
                          title="等待后端支持群聊"
                        >
                          <ThreadRowTitle title="# all" />
                        </button>
                      </div>
                    </div>
                  ) : (
                    <PinnedThreadList
                      threads={groupRows}
                      activeID={activeThreadID}
                      pendingThreadID={pendingThreadID}
                      archiveConfirmThreadID={archiveConfirmThreadID}
                      lastViewedTurnByThreadID={state.lastViewedTurnByThreadID}
                      onSelect={onSelectThread}
                      onTogglePinned={onTogglePinned}
                      onArchive={onArchiveThread}
                      onClearArchiveConfirm={onClearArchiveConfirm}
                    />
                  )}
                </SidebarSection>
              </section>
            );
          })()}
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
                {sectionOrder.map((key) => {
            if (key === SIDEBAR_SECTION_AGENTS) {
              const collapsed = collapsedProjectIDs.has(SIDEBAR_SECTION_AGENTS);
              const rosterMenu = rosterContextMenu ? (
                <ThreadContextMenu
                  x={rosterContextMenu.x}
                  y={rosterContextMenu.y}
                  items={[
                    {
                      label: "编辑设定",
                      onSelect: () =>
                        onEditParticipant(rosterContextMenu.participant),
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
                      onToggleProjectCollapsed(SIDEBAR_SECTION_AGENTS)
                    }
                    actions={rosterActions}
                  >
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
                          const isActiveDM =
                            activeDMParticipantID === participant.id;
                          const isUnread = unreadDMParticipantIDs.has(participant.id);
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
                          const className = [
                            "participant-roster-row",
                            isActiveDM ? "active" : "",
                            isUnread ? "has-unread" : "",
                          ]
                            .filter(Boolean)
                            .join(" ");
                          return (
                            <button
                              key={participant.id}
                              type="button"
                              className={className}
                              onClick={() => onSelectParticipant(participant)}
                              onContextMenu={(event) => {
                                event.preventDefault();
                                setRosterContextMenu({
                                  x: event.clientX,
                                  y: event.clientY,
                                  participant,
                                });
                              }}
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
              </SortableSection>
            );
          })}
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
    </aside>
  );
}
