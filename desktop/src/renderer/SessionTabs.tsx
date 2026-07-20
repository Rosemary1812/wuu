import {
  closestCenter,
  DndContext,
  DragOverlay,
  PointerSensor,
  useSensor,
  useSensors,
  type DragCancelEvent,
  type DragEndEvent,
  type DragStartEvent,
} from "@dnd-kit/core";
import {
  horizontalListSortingStrategy,
  SortableContext,
  useSortable,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Plus, X } from "lucide-react";
import { type CSSProperties, type MouseEvent as ReactMouseEvent, useRef, useState } from "react";
import {
  isThreadRunning,
  isThreadUnread,
  sessionTabLabel,
  threadForTab,
  type AppState,
  type SessionTab,
} from "./AppState";
import {
  pendingComposerMessageCount,
  pendingComposerMessagesForThread,
  type PendingComposerMessagesByThread,
} from "./ComposerPendingMessages";
import { ThreadContextMenu, type ThreadContextMenuItem } from "./ThreadContextMenu";
import { handleTabListKeyDown, useTabCloseFocusRestoration } from "./TabKeyboardNavigation";
import { useStripEnterReady, useTabExitRetention } from "./TabMotion";
import { translateCurrent as translate, useI18n } from "./i18n";
import { ENABLE_COLLABORATION } from "./FeatureFlags";

const POP_OUT_DRAG_DISTANCE_PX = 54;

export function SessionTabStrip({
  state,
  pendingSwitchThreadID,
  pendingComposerMessagesByThread,
  canStartNewThread,
  onSelect,
  onClose,
  onCloseTabs,
  onPopOut,
  onNewThread,
  onReorder,
}: {
  state: AppState;
  pendingSwitchThreadID?: string;
  pendingComposerMessagesByThread: PendingComposerMessagesByThread;
  canStartNewThread: boolean;
  onSelect: (tabID: string) => void;
  onClose: (tabID: string) => void;
  onCloseTabs: (tabIDs: string[]) => void;
  onPopOut: (tabID: string) => void;
  onNewThread: () => void;
  onReorder: (activeID: string, overID: string) => void;
}): JSX.Element {
  const { t } = useI18n();
  const newTabButtonRef = useRef<HTMLButtonElement>(null);
  const { requestFocusRestoration, tabListRef } = useTabCloseFocusRestoration(
    state.activeSessionTabID,
    state.sessionTabs
      .filter((tab) => ENABLE_COLLABORATION || tab.kind !== "board")
      .map((tab) => tab.id),
    newTabButtonRef,
  );
  const [draggingTabID, setDraggingTabID] = useState<string | undefined>();
  const [draggingTabWidth, setDraggingTabWidth] = useState<number | undefined>();
  const enterReady = useStripEnterReady();
  const visibleTabs = ENABLE_COLLABORATION
    ? state.sessionTabs
    : state.sessionTabs.filter((tab) => tab.kind !== "board");
  const sessionTabEntries = useTabExitRetention(visibleTabs, (tab) => tab.id);
  const [tabContextMenu, setTabContextMenu] = useState<
    { tabID: string; x: number; y: number } | undefined
  >();
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
  );
  const draggingTab = draggingTabID
    ? state.sessionTabs.find((tab) => tab.id === draggingTabID)
    : undefined;

  function startDrag(event: DragStartEvent): void {
    setDraggingTabID(String(event.active.id));
    setDraggingTabWidth(event.active.rect.current.initial?.width);
  }

  function endDrag(event: DragEndEvent): void {
    const activeID = String(event.active.id);
    const overID = event.over ? String(event.over.id) : undefined;
    const activeTab = state.sessionTabs.find((tab) => tab.id === activeID);
    if (
      (activeTab?.kind === "thread" || activeTab?.kind === "draft") &&
      Math.abs(event.delta.y) >= POP_OUT_DRAG_DISTANCE_PX
    ) {
      onPopOut(activeID);
      finishDrag();
      return;
    }
    if (overID && activeID !== overID) {
      onReorder(activeID, overID);
    }
    finishDrag();
  }

  function cancelDrag(_event: DragCancelEvent): void {
    finishDrag();
  }

  function finishDrag(): void {
    setDraggingTabID(undefined);
    setDraggingTabWidth(undefined);
  }

  function handleTabContextMenu(tabID: string, event: ReactMouseEvent): void {
    // Prevent the browser's default context menu so the in-app menu is the
    // only thing the user sees. dnd-kit's PointerSensor already filters
    // non-primary mouse buttons, so right-click will not start a drag.
    event.preventDefault();
    event.stopPropagation();
    setTabContextMenu({ tabID, x: event.clientX, y: event.clientY });
  }

  const runningTabIDs = new Set<string>();
  for (const tab of state.sessionTabs) {
    if (
      tab.kind === "thread" &&
      isThreadRunning(threadForTab(state, tab.threadID))
    ) {
      runningTabIDs.add(tab.id);
    }
  }

  return (
    <div className="session-tab-strip" aria-label={t("tabs.openConversations")}>
      <div className="session-tab-list-shell">
        <DndContext
          sensors={sensors}
          collisionDetection={closestCenter}
          onDragStart={startDrag}
          onDragEnd={endDrag}
          onDragCancel={cancelDrag}
        >
          <SortableContext
            items={state.sessionTabs.map((tab) => tab.id)}
            strategy={horizontalListSortingStrategy}
          >
            <div
              ref={tabListRef}
              className="session-tab-scroll"
              role="tablist"
              aria-label={t("tabs.conversations")}
              data-enter-ready={enterReady ? "" : undefined}
              onKeyDown={handleTabListKeyDown}
            >
              {sessionTabEntries.map((entry) => {
                const tab = entry.tab;
                if (entry.closing) {
                  // Exit retention (TabMotion.ts): render an inert collapsing
                  // ghost so the neighbours slide over instead of jumping.
                  return (
                    <div
                      key={`closing-${tab.id}`}
                      className="session-tab closing"
                      aria-hidden="true"
                    >
                      <span className="session-tab-main">
                        <span className="session-tab-status" aria-hidden="true" />
                        <span className="session-tab-title">
                          {sessionTabLabel(tab, state)}
                        </span>
                      </span>
                    </div>
                  );
                }
                const active = tab.id === state.activeSessionTabID;
                const tabThread =
                  tab.kind === "thread"
                    ? threadForTab(state, tab.threadID)
                    : undefined;
                const running = isThreadRunning(tabThread);
                const pendingSwitch =
                  pendingSwitchThreadID !== undefined &&
                  tab.kind === "thread" &&
                  pendingSwitchThreadID === tab.threadID;
                const pendingCount =
                  tab.kind === "thread"
                    ? pendingComposerMessageCount(
                        pendingComposerMessagesForThread(
                          pendingComposerMessagesByThread,
                          tab.threadID,
                        ),
                      )
                    : 0;
                const unread =
                  !active &&
                  !running &&
                  !pendingSwitch &&
                  isThreadUnread(
                    tabThread,
                    tabThread ? state.lastViewedTurnByThreadID[tabThread.id] : undefined,
                  );
                const label = sessionTabLabel(tab, state);
                const closeLabel = tab.kind === "draft"
                  ? t("tabs.closeNewConversation")
                  : t("tabs.closeNamed", { name: label });
                return (
                  <SortableSessionTab
                    key={tab.id}
                    id={tab.id}
                    active={active}
                    running={running}
                    pendingSwitch={pendingSwitch}
                    pendingCount={pendingCount}
                    unread={unread}
                    label={label}
                    closeLabel={closeLabel}
                    draggable={
                      tab.kind === "thread" ||
                      tab.kind === "draft" ||
                      state.sessionTabs.length > 1
                    }
                    reorderable={state.sessionTabs.length > 1}
                    onSelect={() => onSelect(tab.id)}
                    onClose={() => {
                      requestFocusRestoration();
                      onClose(tab.id);
                    }}
                    onDoubleClick={() => {
                      requestFocusRestoration();
                      onClose(tab.id);
                    }}
                    onContextMenu={(event) => handleTabContextMenu(tab.id, event)}
                  />
                );
              })}
            </div>
          </SortableContext>
          <DragOverlay
            dropAnimation={{
              duration: 150,
              easing: "cubic-bezier(0.16, 1, 0.3, 1)",
            }}
          >
            {draggingTab ? (
              <SessionTabDragPreview
                active={draggingTab.id === state.activeSessionTabID}
                label={sessionTabLabel(draggingTab, state)}
                pendingCount={
                  draggingTab.kind === "thread"
                    ? pendingComposerMessageCount(
                        pendingComposerMessagesForThread(
                          pendingComposerMessagesByThread,
                          draggingTab.threadID,
                        ),
                      )
                    : 0
                }
                running={
                  draggingTab.kind === "thread"
                    ? isThreadRunning(threadForTab(state, draggingTab.threadID))
                    : false
                }
                unread={
                  draggingTab.id !== state.activeSessionTabID &&
                  draggingTab.kind === "thread" &&
                  !isThreadRunning(threadForTab(state, draggingTab.threadID)) &&
                  isThreadUnread(
                    threadForTab(state, draggingTab.threadID),
                    state.lastViewedTurnByThreadID[draggingTab.threadID],
                  )
                }
                width={draggingTabWidth}
              />
            ) : null}
          </DragOverlay>
        </DndContext>
      </div>
      <button
        ref={newTabButtonRef}
        className="icon-button workspace-panel-add session-tab-new"
        type="button"
        aria-label={t("tabs.newConversation")}
        title={t("tabs.newConversation")}
        disabled={!canStartNewThread}
        onClick={onNewThread}
      >
        <Plus className="icon-lg" />
      </button>
      {tabContextMenu ? (
        <ThreadContextMenu
          x={tabContextMenu.x}
          y={tabContextMenu.y}
          items={buildTabContextMenuItems({
            tabs: state.sessionTabs,
            runningTabIDs,
            rightClickedTabID: tabContextMenu.tabID,
            onClose,
            onCloseTabs,
            onPopOut,
          })}
          onClose={() => setTabContextMenu(undefined)}
        />
      ) : null}
    </div>
  );
}

type SortableSessionTabProps = {
  id: string;
  active: boolean;
  running: boolean;
  pendingSwitch: boolean;
  pendingCount: number;
  unread: boolean;
  label: string;
  closeLabel: string;
  draggable: boolean;
  reorderable: boolean;
  onSelect: () => void;
  onClose: () => void;
  onDoubleClick: () => void;
  onContextMenu: (event: ReactMouseEvent) => void;
};

function SortableSessionTab({
  id,
  active,
  running,
  pendingSwitch,
  pendingCount,
  unread,
  label,
  closeLabel,
  draggable,
  reorderable,
  onSelect,
  onClose,
  onDoubleClick,
  onContextMenu,
}: SortableSessionTabProps): JSX.Element {
  const { t } = useI18n();
  const {
    attributes,
    listeners,
    setActivatorNodeRef,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({
    id,
    disabled: !draggable,
  });
  const { role: _dragRole, ...dragAttributes } = attributes;
  const style: CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
  };
  const displayPendingCount = pendingCount > 9 ? "9+" : String(pendingCount);
  const pendingLabel = t(
    pendingCount === 1 ? "tabs.pendingInputOne" : "tabs.pendingInput",
    { count: pendingCount },
  );
  const tabTitle =
    pendingCount > 0 ? `${label} · ${pendingLabel}` : label;
  return (
    <div
      ref={setNodeRef}
      className={`session-tab${active ? " active" : ""}${running ? " running" : ""}${
        pendingSwitch ? " pending-switch" : ""
      }${unread ? " has-unread" : ""}${pendingCount > 0 ? " has-pending" : ""}${reorderable ? " can-reorder" : ""}${isDragging ? " dragging" : ""}`}
      style={style}
      aria-grabbed={isDragging || undefined}
      onContextMenu={onContextMenu}
    >
      <button
        ref={setActivatorNodeRef}
        className="session-tab-main"
        type="button"
        {...dragAttributes}
        {...listeners}
        role="tab"
        aria-selected={active}
        aria-busy={pendingSwitch}
        tabIndex={active ? 0 : -1}
        title={tabTitle}
        onClick={onSelect}
        onDoubleClick={onDoubleClick}
      >
        <span className="session-tab-status" aria-hidden="true" />
        <span className="session-tab-title">{label}</span>
        {pendingCount > 0 ? (
          <span
            className="session-tab-pending-count"
            aria-label={pendingLabel}
          >
            {displayPendingCount}
          </span>
        ) : null}
      </button>
      <button
        className="session-tab-close"
        type="button"
        draggable={false}
        aria-label={closeLabel}
        title={closeLabel}
        onClick={(event) => {
          event.stopPropagation();
          onClose();
        }}
      >
        <X className="icon-xs" />
      </button>
    </div>
  );
}

function SessionTabDragPreview({
  active,
  running,
  unread,
  label,
  pendingCount,
  width,
}: {
  active: boolean;
  running: boolean;
  unread: boolean;
  label: string;
  pendingCount: number;
  width?: number;
}): JSX.Element {
  const displayPendingCount = pendingCount > 9 ? "9+" : String(pendingCount);
  return (
    <div
      className={`session-tab session-tab-drag-overlay${active ? " active" : ""}${running ? " running" : ""}${unread ? " has-unread" : ""}${pendingCount > 0 ? " has-pending" : ""}`}
      style={width ? { width } : undefined}
    >
      <div className="session-tab-main">
        <span className="session-tab-status" aria-hidden="true" />
        <span className="session-tab-title">{label}</span>
        {pendingCount > 0 ? (
          <span className="session-tab-pending-count" aria-hidden="true">
            {displayPendingCount}
          </span>
        ) : null}
      </div>
      <div className="session-tab-close" aria-hidden="true">
        <X className="icon-xs" />
      </div>
    </div>
  );
}

function buildTabContextMenuItems({
  tabs,
  runningTabIDs,
  rightClickedTabID,
  onClose,
  onCloseTabs,
  onPopOut,
}: {
  tabs: SessionTab[];
  runningTabIDs: Set<string>;
  rightClickedTabID: string;
  onClose: (tabID: string) => void;
  onCloseTabs: (tabIDs: string[]) => void;
  onPopOut: (tabID: string) => void;
}): ThreadContextMenuItem[] {
  const allTabIDs = tabs.map((tab) => tab.id);
  const rightClickedTab = tabs.find((tab) => tab.id === rightClickedTabID);
  // Draft tabs have no thread and are never "running", so they count as
  // non-running here. Callers that want to preserve drafts should not pick
  // "关闭未运行的".
  const nonRunningTabIDs = allTabIDs.filter((id) => !runningTabIDs.has(id));
  return [
    {
      label: translate("tabs.openInNewWindow"),
      disabled:
        rightClickedTab?.kind !== "thread" && rightClickedTab?.kind !== "draft",
      onSelect: () => onPopOut(rightClickedTabID),
    },
    { separator: true },
    {
      label: translate("common.close"),
      onSelect: () => onClose(rightClickedTabID),
    },
    {
      label: translate("tabs.closeOthers"),
      disabled: allTabIDs.length <= 1,
      onSelect: () =>
        onCloseTabs(allTabIDs.filter((id) => id !== rightClickedTabID)),
    },
    { separator: true },
    {
      label: translate("tabs.closeNotRunning"),
      disabled: nonRunningTabIDs.length === 0,
      onSelect: () => onCloseTabs(nonRunningTabIDs),
    },
    {
      label: translate("tabs.closeAll"),
      disabled: allTabIDs.length <= 1,
      onSelect: () => onCloseTabs(allTabIDs),
    },
  ];
}
