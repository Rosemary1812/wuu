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
import { FileDiff, FolderOpen, Globe, Plus, ShieldCheck, Terminal, X } from "lucide-react";
import type { GitStatusResult, RuntimeContext } from "../shared/protocol";
import { TurnFileDiffPanel } from "./TurnFileDiffPanel";
import { WorkspaceBrowserPanel } from "./WorkspaceBrowserPanel";
import { WorkspaceFilePreview, WorkspaceFileTree } from "./WorkspaceFiles";
import { WorkspaceDiffReview, WorkspaceReviewPanel } from "./WorkspaceReviewPanels";
import { WorkspaceTerminalPanel } from "./WorkspaceTerminalPanel";
import type { WorkspaceViewTab } from "./WorkspaceViewTabs";

export type WorkspacePanelView = "files" | "review" | "terminal" | "browser";

export function WorkspaceMainPanel({
  view,
  activeContext,
  workspaceContext,
  gitStatus,
  selectedFilePath,
  onOpenRightPanel
}: {
  view: WorkspacePanelView;
  // activeContext is the pinned project/no_project context — used only by
  // the diff/review view (see below). workspaceContext follows the active
  // thread's own cwd (e.g. a worktree fork) when it differs from
  // activeContext; see workspacePanelContext in AppState.ts.
  activeContext?: RuntimeContext;
  workspaceContext?: RuntimeContext;
  gitStatus?: GitStatusResult;
  selectedFilePath?: string;
  onOpenRightPanel: () => void;
}): JSX.Element | null {
  if (view === "files") {
    return (
      <WorkspaceFilePreview
        activeContext={workspaceContext}
        selectedFilePath={selectedFilePath}
        onOpenRightPanel={onOpenRightPanel}
      />
    );
  }

  if (view === "review") {
    // Deliberately activeContext, not workspaceContext: gitStatus is fetched
    // from the app-server bound to activeContext's own workdir, so feeding
    // this view the thread's cwd would silently show diff data for the
    // wrong directory.
    return <WorkspaceDiffReview activeContext={activeContext} gitStatus={gitStatus} />;
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
  { id: "browser", title: "浏览器", subtitle: "在右侧栏里调试前端" }
];

export function WorkspaceRightPanel({
  open,
  present,
  tabs,
  activeTabID,
  activeContext,
  workspaceContext,
  gitStatus,
  selectedFilePath,
  onSelectTab,
  onOpenTool,
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
  tabs: WorkspaceViewTab[];
  activeTabID: string | undefined;
  // activeContext is the pinned project/no_project context — used for the
  // browser tab (its "current project" hint text, not a filesystem root).
  // workspaceContext follows the active thread's own cwd (e.g. a worktree
  // fork) when it differs from activeContext, and roots the file tree and
  // terminal; see workspacePanelContext in AppState.ts.
  activeContext?: RuntimeContext;
  workspaceContext?: RuntimeContext;
  gitStatus?: GitStatusResult;
  selectedFilePath?: string;
  onSelectTab: (id: string) => void;
  onOpenTool: (view: WorkspacePanelView) => void;
  onShowTools: () => void;
  onCloseTab: (id: string) => void;
  onReorderTabs: (activeID: string, overID: string) => void;
  onOpenFile: (path: string) => void;
  onClose: () => void;
  pendingBrowserURL?: string;
  onBrowserURLConsumed?: () => void;
  onBrowserURLChange?: (url: string) => void;
}): JSX.Element {
  const activeTab = activeTabID ? tabs.find((tab) => tab.id === activeTabID) : undefined;
  const showingPicker = !activeTab;
  const [draggingTabID, setDraggingTabID] = useState<string | undefined>(undefined);
  const [draggingTabWidth, setDraggingTabWidth] = useState<number | undefined>(undefined);
  const tabSensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 6 } }));
  const draggingTab = draggingTabID ? tabs.find((tab) => tab.id === draggingTabID) : undefined;

  function startTabDrag(event: DragStartEvent): void {
    setDraggingTabID(String(event.active.id));
    setDraggingTabWidth(event.active.rect.current.initial?.width);
  }

  function endTabDrag(event: DragEndEvent): void {
    const activeID = String(event.active.id);
    const overID = event.over ? String(event.over.id) : undefined;
    if (overID && activeID !== overID) {
      onReorderTabs(activeID, overID);
    }
    finishTabDrag();
  }

  function cancelTabDrag(_event: DragCancelEvent): void {
    finishTabDrag();
  }

  function finishTabDrag(): void {
    setDraggingTabID(undefined);
    setDraggingTabWidth(undefined);
  }

  return (
    <aside
      className={`workspace-right-panel${activeTab ? " detail" : " tools"}${activeTab?.kind === "review" ? " review" : ""}${activeTab?.kind === "diff" ? " diff" : ""}`}
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
          <SortableContext items={tabs.map((tab) => tab.id)} strategy={horizontalListSortingStrategy}>
            <div className="workspace-panel-tabs" role="tablist" aria-label="右侧栏工具">
              {tabs.map((tab) => {
                const active = tab.id === activeTabID;
                return (
                  <SortableWorkspaceViewTab
                    key={tab.id}
                    tab={tab}
                    active={active}
                    open={open}
                    reorderable={tabs.length > 1}
                    onSelect={() => onSelectTab(tab.id)}
                    onClose={() => onCloseTab(tab.id)}
                  />
                );
              })}
            </div>
          </SortableContext>
          <DragOverlay dropAnimation={{ duration: 150, easing: "cubic-bezier(0.16, 1, 0.3, 1)" }}>
            {draggingTab ? (
              <WorkspaceViewTabPreview
                tab={draggingTab}
                active={draggingTab.id === activeTabID}
                width={draggingTabWidth}
              />
            ) : null}
          </DragOverlay>
        </DndContext>
        <span className="workspace-panel-tabbar-spacer" />
        <button
          className={`icon-button workspace-panel-add${showingPicker ? " active" : ""}`}
          type="button"
          aria-label="选择工具"
          aria-pressed={showingPicker}
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
          <div className={`workspace-panel-body${activeTab ? "" : " picker"}`}>
            {!activeTab ? (
              <WorkspaceToolPicker tabs={tabs} onSelectTool={onOpenTool} />
            ) : activeTab.kind === "diff" ? (
              <TurnFileDiffPanel
                selection={activeTab.selection}
                onClose={() => onCloseTab(activeTab.id)}
              />
            ) : activeTab.kind === "files" ? (
              <WorkspaceFileTree
                activeContext={workspaceContext}
                open={open}
                selectedFilePath={selectedFilePath}
                onOpenFile={onOpenFile}
              />
            ) : activeTab.kind === "review" ? (
              <WorkspaceReviewPanel gitStatus={gitStatus} />
            ) : activeTab.kind === "terminal" ? (
              <WorkspaceTerminalPanel activeContext={workspaceContext} />
            ) : activeTab.kind === "browser" ? (
              <WorkspaceBrowserPanel
                open={open}
                activeContext={activeContext}
                pendingBrowserURL={pendingBrowserURL}
                onBrowserURLConsumed={onBrowserURLConsumed}
                onCurrentURLChange={onBrowserURLChange}
              />
            ) : null}
          </div>
        </>
      ) : null}
    </aside>
  );
}

function SortableWorkspaceViewTab({
  tab,
  active,
  open,
  reorderable,
  onSelect,
  onClose
}: {
  tab: WorkspaceViewTab;
  active: boolean;
  open: boolean;
  reorderable: boolean;
  onSelect: () => void;
  onClose: () => void;
}): JSX.Element {
  const { attributes, listeners, setActivatorNodeRef, setNodeRef, transform, transition, isDragging } = useSortable({
    id: tab.id,
    disabled: !reorderable
  });
  const { role: _dragRole, ...dragAttributes } = attributes;
  const style: CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition
  };
  const label = workspaceViewTabLabel(tab);
  const tooltip = workspaceViewTabTooltip(tab);
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
        title={tooltip}
        disabled={!open}
        onClick={onSelect}
        {...dragAttributes}
        {...listeners}
      >
        <WorkspaceViewTabIcon tab={tab} className="icon" />
        <span>{label}</span>
      </button>
      <button
        className="workspace-tool-tab-close"
        type="button"
        draggable={false}
        aria-label={`关闭${label}`}
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

function WorkspaceViewTabPreview({
  tab,
  active,
  width
}: {
  tab: WorkspaceViewTab;
  active: boolean;
  width?: number;
}): JSX.Element {
  const label = workspaceViewTabLabel(tab);
  return (
    <div className={`workspace-tool-tab workspace-tool-tab-drag-overlay${active ? " active" : ""}`} style={width ? { width } : undefined}>
      <div className="workspace-tool-tab-main">
        <WorkspaceViewTabIcon tab={tab} className="icon" />
        <span>{label}</span>
      </div>
      <div className="workspace-tool-tab-close" aria-hidden="true">
        <X className="icon-xs" />
      </div>
    </div>
  );
}

function WorkspaceToolPicker({
  tabs,
  onSelectTool
}: {
  tabs: WorkspaceViewTab[];
  onSelectTool: (view: WorkspacePanelView) => void;
}): JSX.Element {
  return (
    <div className="workspace-tool-menu" aria-label="工作区工具">
      {WORKSPACE_TOOL_ITEMS.map((item) => (
        <button
          key={item.id}
          className={`workspace-tool-menu-item${tabs.some((tab) => tab.kind === item.id) ? " active" : ""}`}
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
        <div
          className="workspace-tool-grid"
          aria-label="工作区工具"
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
        </div>
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
  }
}

function workspaceToolFor(view: WorkspacePanelView): (typeof WORKSPACE_TOOL_ITEMS)[number] {
  return WORKSPACE_TOOL_ITEMS.find((item) => item.id === view) ?? WORKSPACE_TOOL_ITEMS[0];
}

function workspaceViewTabLabel(tab: WorkspaceViewTab): string {
  return tab.kind === "diff" ? tab.title : workspaceToolFor(tab.kind).title;
}

function workspaceViewTabTooltip(tab: WorkspaceViewTab): string {
  return tab.kind === "diff" ? tab.path : workspaceToolFor(tab.kind).title;
}

function WorkspaceViewTabIcon({ tab, className }: { tab: WorkspaceViewTab; className?: string }): JSX.Element {
  if (tab.kind === "diff") {
    return <FileDiff className={className} />;
  }
  return <WorkspaceToolIcon view={tab.kind} className={className} />;
}

export function workspaceModeTitle(view: WorkspacePanelView): string {
  return view === "files" ? "打开文件" : workspaceToolFor(view).title;
}
