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
import { restrictToHorizontalAxis } from "@dnd-kit/modifiers";
import {
  horizontalListSortingStrategy,
  SortableContext,
  useSortable,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Plus, X } from "lucide-react";
import { type CSSProperties, useState } from "react";
import {
  isThreadRunning,
  sessionTabLabel,
  threadForTab,
  type AppState,
} from "./AppState";
import {
  pendingComposerMessageCount,
  pendingComposerMessagesForThread,
  type PendingComposerMessagesByThread,
} from "./ComposerPendingMessages";

export function SessionTabStrip({
  state,
  pendingSwitchThreadID,
  pendingComposerMessagesByThread,
  canStartNewThread,
  onSelect,
  onClose,
  onNewThread,
  onReorder,
}: {
  state: AppState;
  pendingSwitchThreadID?: string;
  pendingComposerMessagesByThread: PendingComposerMessagesByThread;
  canStartNewThread: boolean;
  onSelect: (tabID: string) => void;
  onClose: (tabID: string) => void;
  onNewThread: () => void;
  onReorder: (activeID: string, overID: string) => void;
}): JSX.Element {
  const [draggingTabID, setDraggingTabID] = useState<string | undefined>();
  const [draggingTabWidth, setDraggingTabWidth] = useState<number | undefined>();
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

  return (
    <div className="session-tab-strip" aria-label="已打开的工作对象">
      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        modifiers={[restrictToHorizontalAxis]}
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
                  label={label}
                  closeLabel={closeLabel}
                  reorderable={state.sessionTabs.length > 1}
                  onSelect={() => onSelect(tab.id)}
                  onClose={() => onClose(tab.id)}
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
              width={draggingTabWidth}
            />
          ) : null}
        </DragOverlay>
      </DndContext>
      <button
        className="icon-button workspace-panel-add session-tab-new"
        type="button"
        aria-label="新建对话"
        title="新建对话"
        disabled={!canStartNewThread}
        onClick={onNewThread}
      >
        <Plus size={19} />
      </button>
    </div>
  );
}

type SortableSessionTabProps = {
  id: string;
  active: boolean;
  running: boolean;
  pendingSwitch: boolean;
  pendingCount: number;
  label: string;
  closeLabel: string;
  reorderable: boolean;
  onSelect: () => void;
  onClose: () => void;
};

function SortableSessionTab({
  id,
  active,
  running,
  pendingSwitch,
  pendingCount,
  label,
  closeLabel,
  reorderable,
  onSelect,
  onClose,
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
    disabled: !reorderable,
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
      }${pendingCount > 0 ? " has-pending" : ""}${reorderable ? " can-reorder" : ""}${isDragging ? " dragging" : ""}`}
      style={style}
      aria-grabbed={isDragging || undefined}
    >
      <button
        ref={setActivatorNodeRef}
        className="session-tab-main"
        type="button"
        aria-current={active ? "page" : undefined}
        aria-busy={pendingSwitch}
        title={tabTitle}
        onClick={onSelect}
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
        <X size={13} />
      </button>
    </div>
  );
}

function SessionTabDragPreview({
  active,
  running,
  label,
  pendingCount,
  width,
}: {
  active: boolean;
  running: boolean;
  label: string;
  pendingCount: number;
  width?: number;
}): JSX.Element {
  const displayPendingCount = pendingCount > 9 ? "9+" : String(pendingCount);
  return (
    <div
      className={`session-tab session-tab-drag-overlay${active ? " active" : ""}${running ? " running" : ""}${pendingCount > 0 ? " has-pending" : ""}`}
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
        <X size={13} />
      </div>
    </div>
  );
}
