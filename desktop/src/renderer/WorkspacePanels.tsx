import { type CSSProperties, useState } from "react";
import {
  closestCenter,
  DndContext,
  DragOverlay,
  PointerSensor,
  useSensor,
  useSensors,
  type DragCancelEvent,
  type DragEndEvent,
  type DragStartEvent
} from "@dnd-kit/core";
import { restrictToHorizontalAxis } from "@dnd-kit/modifiers";
import { horizontalListSortingStrategy, SortableContext, useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Activity, FolderOpen, Globe, Plus, ShieldCheck, Terminal, X } from "lucide-react";
import { OverlayScrollbarsComponent } from "overlayscrollbars-react";
import type { GitStatusResult, RuntimeContext } from "../shared/protocol";
import { OVERLAY_SCROLLBAR_OPTIONS } from "./ScrollbarOptions";
import { WorkspaceBrowserPanel } from "./WorkspaceBrowserPanel";
import { WorkspaceFilePreview, WorkspaceFileTree } from "./WorkspaceFiles";
import { WorkspaceGoalPanel } from "./WorkspaceGoalPanel";
import { WorkspaceDiffReview, WorkspaceReviewPanel } from "./WorkspaceReviewPanels";
import { WorkspaceTerminalPanel } from "./WorkspaceTerminalPanel";

export type WorkspacePanelView = "files" | "review" | "terminal" | "browser" | "goals";
export type WorkspaceRightPanelView = "tools" | WorkspacePanelView;

export function WorkspaceMainPanel({
  view,
  activeContext,
  threadId,
  gitStatus,
  selectedFilePath,
  onOpenRightPanel
}: {
  view: WorkspacePanelView;
  activeContext?: RuntimeContext;
  threadId?: string;
  gitStatus?: GitStatusResult;
  selectedFilePath?: string;
  onOpenRightPanel: () => void;
}): JSX.Element | null {
  if (view === "files") {
    return (
      <WorkspaceFilePreview
        activeContext={activeContext}
        selectedFilePath={selectedFilePath}
        onOpenRightPanel={onOpenRightPanel}
      />
    );
  }

  if (view === "review") {
    return <WorkspaceDiffReview activeContext={activeContext} gitStatus={gitStatus} />;
  }

  if (view === "goals") {
    return <WorkspaceGoalPanel activeContext={activeContext} threadId={threadId} open />;
  }

  return null;
}

const WORKSPACE_TOOL_ITEMS: Array<{
  id: WorkspacePanelView;
  title: string;
  subtitle: string;
}> = [
  { id: "files", title: "文件", subtitle: "浏览项目文件" },
  { id: "review", title: "审查", subtitle: "查看代码更改" },
  { id: "terminal", title: "终端", subtitle: "运行 shell 命令" },
  { id: "browser", title: "浏览器", subtitle: "在右侧栏里调试前端" },
  { id: "goals", title: "Goal", subtitle: "查看目标、workflow 和 agent 状态" }
];

export function WorkspaceRightPanel({
  open,
  present,
  view,
  openTabs,
  activeContext,
  threadId,
  gitStatus,
  selectedFilePath,
  onSelectView,
  onShowTools,
  onCloseTab,
  onReorderTabs,
  onOpenFile,
  onClose,
  pendingBrowserURL,
  onBrowserURLConsumed,
  onBrowserURLChange
}: {
  open: boolean;
  present: boolean;
  view: WorkspaceRightPanelView;
  openTabs: WorkspacePanelView[];
  activeContext?: RuntimeContext;
  threadId?: string;
  gitStatus?: GitStatusResult;
  selectedFilePath?: string;
  onSelectView: (view: WorkspacePanelView) => void;
  onShowTools: () => void;
  onCloseTab: (view: WorkspacePanelView) => void;
  onReorderTabs: (activeView: WorkspacePanelView, overView: WorkspacePanelView) => void;
  onOpenFile: (path: string) => void;
  onClose: () => void;
  pendingBrowserURL?: string;
  onBrowserURLConsumed?: () => void;
  onBrowserURLChange?: (url: string) => void;
}): JSX.Element {
  const detailView = view === "tools" ? undefined : view;
  const [draggingTab, setDraggingTab] = useState<WorkspacePanelView | undefined>(undefined);
  const [draggingTabWidth, setDraggingTabWidth] = useState<number | undefined>(undefined);
  const tabSensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 6 } }));
  const draggingTool = draggingTab ? workspaceToolFor(draggingTab) : undefined;

  function startTabDrag(event: DragStartEvent): void {
    const activeView = workspacePanelViewFromSortableID(event.active.id);
    if (!activeView) {
      return;
    }
    setDraggingTab(activeView);
    setDraggingTabWidth(event.active.rect.current.initial?.width);
  }

  function endTabDrag(event: DragEndEvent): void {
    const activeView = workspacePanelViewFromSortableID(event.active.id);
    const overView = event.over ? workspacePanelViewFromSortableID(event.over.id) : undefined;
    if (activeView && overView && activeView !== overView) {
      onReorderTabs(activeView, overView);
    }
    finishTabDrag();
  }

  function cancelTabDrag(_event: DragCancelEvent): void {
    finishTabDrag();
  }

  function finishTabDrag(): void {
    setDraggingTab(undefined);
    setDraggingTabWidth(undefined);
  }

  return (
    <aside
      className={`workspace-right-panel${detailView ? " detail" : " tools"}${detailView === "review" ? " review" : ""}`}
      aria-hidden={!open}
    >
      <div className="workspace-panel-tabbar">
        <DndContext
          sensors={tabSensors}
          collisionDetection={closestCenter}
          modifiers={[restrictToHorizontalAxis]}
          onDragStart={startTabDrag}
          onDragEnd={endTabDrag}
          onDragCancel={cancelTabDrag}
        >
          <SortableContext items={openTabs} strategy={horizontalListSortingStrategy}>
            <div className="workspace-panel-tabs" role="tablist" aria-label="右侧栏工具">
              {openTabs.map((item) => {
                const tool = workspaceToolFor(item);
                const active = item === detailView;
                return (
                  <SortableWorkspaceToolTab
                    key={item}
                    view={item}
                    title={tool.title}
                    active={active}
                    open={open}
                    reorderable={openTabs.length > 1}
                    onSelect={() => onSelectView(item)}
                    onClose={() => onCloseTab(item)}
                  />
                );
              })}
            </div>
          </SortableContext>
          <DragOverlay dropAnimation={{ duration: 150, easing: "cubic-bezier(0.16, 1, 0.3, 1)" }}>
            {draggingTab && draggingTool ? (
              <WorkspaceToolTabPreview
                view={draggingTab}
                title={draggingTool.title}
                active={draggingTab === detailView}
                width={draggingTabWidth}
              />
            ) : null}
          </DragOverlay>
        </DndContext>
        <span className="workspace-panel-tabbar-spacer" />
        <button
          className={`icon-button workspace-panel-add${view === "tools" ? " active" : ""}`}
          type="button"
          aria-label="选择工具"
          aria-pressed={view === "tools"}
          disabled={!open}
          onClick={onShowTools}
        >
          <Plus className="icon-lg" />
        </button>
        <button
          className="icon-button workspace-panel-close"
          type="button"
          aria-label="关闭右侧栏"
          disabled={!open}
          onClick={onClose}
        >
          <X className="icon" />
        </button>
      </div>
      {present ? (
        <>
          <div className={`workspace-panel-body${view === "tools" ? " picker" : ""}`}>
            {view === "tools" ? (
              <WorkspaceToolPicker openTabs={openTabs} onSelectTool={onSelectView} />
            ) : view === "files" ? (
              <WorkspaceFileTree
                activeContext={activeContext}
                open={open}
                selectedFilePath={selectedFilePath}
                onOpenFile={onOpenFile}
              />
            ) : view === "review" ? (
              <WorkspaceReviewPanel gitStatus={gitStatus} />
            ) : view === "terminal" ? (
              <WorkspaceTerminalPanel activeContext={activeContext} />
            ) : view === "browser" ? (
              <WorkspaceBrowserPanel
                activeContext={activeContext}
                pendingBrowserURL={pendingBrowserURL}
                onBrowserURLConsumed={onBrowserURLConsumed}
                onCurrentURLChange={onBrowserURLChange}
              />
            ) : view === "goals" ? (
              <WorkspaceGoalPanel activeContext={activeContext} threadId={threadId} open={open} />
            ) : null}
          </div>
        </>
      ) : null}
    </aside>
  );
}

function SortableWorkspaceToolTab({
  view,
  title,
  active,
  open,
  reorderable,
  onSelect,
  onClose
}: {
  view: WorkspacePanelView;
  title: string;
  active: boolean;
  open: boolean;
  reorderable: boolean;
  onSelect: () => void;
  onClose: () => void;
}): JSX.Element {
  const { attributes, listeners, setActivatorNodeRef, setNodeRef, transform, transition, isDragging } = useSortable({
    id: view,
    disabled: !reorderable
  });
  const { role: _dragRole, ...dragAttributes } = attributes;
  const style: CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition
  };
  return (
    <div
      ref={setNodeRef}
      className={`workspace-tool-tab${active ? " active" : ""}${reorderable ? " can-reorder" : ""}${
        isDragging ? " dragging" : ""
      }`}
      style={style}
      aria-grabbed={isDragging || undefined}
    >
      <button
        ref={setActivatorNodeRef}
        className="workspace-tool-tab-main"
        type="button"
        role="tab"
        aria-selected={active}
        title={title}
        disabled={!open}
        onClick={onSelect}
        {...dragAttributes}
        {...listeners}
      >
        <WorkspaceToolIcon view={view} className="icon" />
        <span>{title}</span>
      </button>
      <button
        className="workspace-tool-tab-close"
        type="button"
        draggable={false}
        aria-label={`关闭${title}`}
        disabled={!open}
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

function WorkspaceToolTabPreview({
  view,
  title,
  active,
  width
}: {
  view: WorkspacePanelView;
  title: string;
  active: boolean;
  width?: number;
}): JSX.Element {
  return (
    <div className={`workspace-tool-tab workspace-tool-tab-drag-overlay${active ? " active" : ""}`} style={width ? { width } : undefined}>
      <div className="workspace-tool-tab-main">
        <WorkspaceToolIcon view={view} className="icon" />
        <span>{title}</span>
      </div>
      <div className="workspace-tool-tab-close" aria-hidden="true">
        <X className="icon-xs" />
      </div>
    </div>
  );
}

function WorkspaceToolPicker({
  openTabs,
  onSelectTool
}: {
  openTabs: WorkspacePanelView[];
  onSelectTool: (view: WorkspacePanelView) => void;
}): JSX.Element {
  return (
    <div className="workspace-tool-menu" aria-label="工作区工具">
      {WORKSPACE_TOOL_ITEMS.map((item) => (
        <button
          key={item.id}
          className={`workspace-tool-menu-item${openTabs.includes(item.id) ? " active" : ""}`}
          type="button"
          onClick={() => onSelectTool(item.id)}
        >
          <span className="workspace-tool-menu-icon" aria-hidden="true">
            <WorkspaceToolIcon view={item.id} className="icon-xl" />
          </span>
          <span className="workspace-tool-menu-copy">
            <strong>{item.title}</strong>
            <span>{item.subtitle}</span>
          </span>
        </button>
      ))}
    </div>
  );
}

export function WorkspaceBottomPanel({
  open,
  selectedView,
  onSelectTool,
  onClose
}: {
  open: boolean;
  selectedView: WorkspacePanelView;
  onSelectTool: (view: WorkspacePanelView) => void;
  onClose: () => void;
}): JSX.Element {
  return (
    <section className="workspace-bottom-panel" aria-hidden={!open}>
      <div className="workspace-bottom-header">
        <div className="workspace-bottom-title">工具</div>
        <button
          className="icon-button workspace-panel-close"
          type="button"
          aria-label="关闭底部栏"
          disabled={!open}
          onClick={onClose}
        >
          <X className="icon" />
        </button>
      </div>
      {open ? (
        <OverlayScrollbarsComponent
          className="workspace-tool-grid"
          aria-label="工作区工具"
          data-overlayscrollbars-initialize
          defer
          options={OVERLAY_SCROLLBAR_OPTIONS}
        >
          {WORKSPACE_TOOL_ITEMS.map((item) => (
            <button
              key={item.id}
              className={`workspace-tool-card${item.id === selectedView ? " active" : ""}`}
              type="button"
              onClick={() => onSelectTool(item.id)}
            >
              <WorkspaceToolIcon view={item.id} className="workspace-tool-card-icon" />
              <strong>{item.title}</strong>
              <span>{item.subtitle}</span>
            </button>
          ))}
        </OverlayScrollbarsComponent>
      ) : null}
    </section>
  );
}

export function WorkspaceToolIcon({ view, className }: { view: WorkspacePanelView; className?: string }): JSX.Element {
  switch (view) {
    case "files":
      return <FolderOpen className={className} />;
    case "review":
      return <ShieldCheck className={className} />;
    case "terminal":
      return <Terminal className={className} />;
    case "browser":
      return <Globe className={className} />;
    case "goals":
      return <Activity className={className} />;
  }
}

function workspaceToolFor(view: WorkspacePanelView): (typeof WORKSPACE_TOOL_ITEMS)[number] {
  return WORKSPACE_TOOL_ITEMS.find((item) => item.id === view) ?? WORKSPACE_TOOL_ITEMS[0];
}

function workspacePanelViewFromSortableID(id: unknown): WorkspacePanelView | undefined {
  const value = typeof id === "string" ? id : String(id);
  return WORKSPACE_TOOL_ITEMS.some((item) => item.id === value) ? (value as WorkspacePanelView) : undefined;
}

export function workspaceModeTitle(view: WorkspacePanelView): string {
  return view === "files" ? "打开文件" : workspaceToolFor(view).title;
}
