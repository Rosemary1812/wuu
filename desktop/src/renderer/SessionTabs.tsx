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
import { type CSSProperties, type MouseEvent as ReactMouseEvent, useState } from "react";
import {
  isThreadRunning,
  isThreadUnread,
  overlayMemberBusy,
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

const POP_OUT_DRAG_DISTANCE_PX = 54;

export function SessionTabStrip({
  state,
  busyParticipantIDs,
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
  // Live busy set from computeBusyParticipantIDs. Server-sent
  // members[].busy is a pull-time snapshot that is never pushed on busy
  // flips, so group-thread tabs overlay it with this set — the same
  // source the sidebar rows and roster dots use — to keep the tab
  // spinner bound to the member's actual turn lifecycle. Optional so
  // test harnesses can omit it.
  busyParticipantIDs?: ReadonlySet<string>;
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
  const [draggingTabID, setDraggingTabID] = useState<string | undefined>();
  const [draggingTabWidth, setDraggingTabWidth] = useState<number | undefined>();
  const [tabContextMenu, setTabContextMenu] = useState<
    { tabID: string; x: number; y: number } | undefined
  >();
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
  );
  const draggingTab = draggingTabID
    ? state.sessionTabs.find((tab) => tab.id === draggingTabID)
    : undefined;

  function liveThreadForTab(threadID: string) {
    const thread = threadForTab(state, threadID);
    if (!thread || !busyParticipantIDs) {
      return thread;
    }
    return overlayMemberBusy(thread, busyParticipantIDs);
  }

  function startDrag(event: DragStartEvent): void {
    setDraggingTabID(String(event.active.id));
    setDraggingTabWidth(event.active.rect.current.initial?.width);
  }

  function endDrag(event: DragEndEvent): void {
    const activeID = String(event.active.id);
    const overID = event.over ? String(event.over.id) : undefined;
    if (Math.abs(event.delta.y) >= POP_OUT_DRAG_DISTANCE_PX) {
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
      isThreadRunning(liveThreadForTab(tab.threadID))
    ) {
      runningTabIDs.add(tab.id);
    }
  }

  return (
    <div className="session-tab-strip" aria-label="已打开的工作对象">
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
            <div className="session-tab-scroll">
              {state.sessionTabs.map((tab) => {
                const active = tab.id === state.activeSessionTabID;
                const tabThread =
                  tab.kind === "thread"
                    ? liveThreadForTab(tab.threadID)
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
                    tabThread
                      ? state.lastViewedMessageSeqByThreadID[tabThread.id]
                      : undefined,
                  );
                const label = sessionTabLabel(tab, state);
                const closeLabel =
                  tab.kind === "draft" ? "关闭新对话" : `关闭 ${label}`;
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
                    onClose={() => onClose(tab.id)}
                    onDoubleClick={() => onClose(tab.id)}
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
                    ? isThreadRunning(liveThreadForTab(draggingTab.threadID))
                    : false
                }
                unread={
                  draggingTab.id !== state.activeSessionTabID &&
                  draggingTab.kind === "thread" &&
                  !isThreadRunning(liveThreadForTab(draggingTab.threadID)) &&
                  isThreadUnread(
                    liveThreadForTab(draggingTab.threadID),
                    state.lastViewedTurnByThreadID[draggingTab.threadID],
                    state.lastViewedMessageSeqByThreadID[draggingTab.threadID],
                  )
                }
                width={draggingTabWidth}
              />
            ) : null}
          </DragOverlay>
        </DndContext>
      </div>
      <button
        className="icon-button workspace-panel-add session-tab-new"
        type="button"
        aria-label="新建对话"
        title="新建对话"
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
  const style: CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
  };
  const displayPendingCount = pendingCount > 9 ? "9+" : String(pendingCount);
  const tabTitle =
    pendingCount > 0 ? `${label} · ${pendingCount} 条待处理输入` : label;
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
        aria-current={active ? "page" : undefined}
        aria-busy={pendingSwitch}
        title={tabTitle}
        onClick={onSelect}
        onDoubleClick={onDoubleClick}
        {...attributes}
        {...listeners}
      >
        <span className="session-tab-status" aria-hidden="true" />
        <span className="session-tab-title">{label}</span>
        {pendingCount > 0 ? (
          <span
            className="session-tab-pending-count"
            aria-label={`${pendingCount} 条待处理输入`}
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
      label: "在新窗口打开",
      disabled:
        rightClickedTab?.kind !== "thread" && rightClickedTab?.kind !== "draft",
      onSelect: () => onPopOut(rightClickedTabID),
    },
    { separator: true },
    {
      label: "关闭",
      onSelect: () => onClose(rightClickedTabID),
    },
    {
      label: "关闭其他",
      disabled: allTabIDs.length <= 1,
      onSelect: () =>
        onCloseTabs(allTabIDs.filter((id) => id !== rightClickedTabID)),
    },
    { separator: true },
    {
      label: "关闭未运行的",
      disabled: nonRunningTabIDs.length === 0,
      onSelect: () => onCloseTabs(nonRunningTabIDs),
    },
    {
      label: "关闭全部",
      disabled: allTabIDs.length <= 1,
      onSelect: () => onCloseTabs(allTabIDs),
    },
  ];
}
