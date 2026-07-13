import { type CSSProperties, useCallback, useEffect, useRef, useState } from "react";
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
import { FileDiff, FileText, FolderOpen, Globe, Maximize2, Minimize2, PanelLeftOpen, Plus, ShieldCheck, Terminal, X } from "lucide-react";
import type { ActivitySession, GitStatusResult, RuntimeContext } from "../shared/protocol";
import { TurnFileDiffPanel } from "./TurnFileDiffPanel";
import { WorkspaceBrowserPanel } from "./WorkspaceBrowserPanel";
import { WorkspaceFilePreview, WorkspaceFileTree, type WorkspaceFileDirtyState } from "./WorkspaceFiles";
import { WorkspaceReviewPanel } from "./WorkspaceReviewPanels";
import { WorkspaceTerminalPanel } from "./WorkspaceTerminalPanel";
import type { WorkspaceFileViewTab, WorkspaceViewTab } from "./WorkspaceViewTabs";
import { handleTabListKeyDown, useTabCloseFocusRestoration } from "./TabKeyboardNavigation";

export type WorkspacePanelView = "files" | "review" | "terminal" | "browser";

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
  onDirtyFileTabsChange,
  onReorderTabs,
  onOpenFile,
  onClose,
  globalized,
  onToggleGlobalize,
  onOpenSidebar,
  canExitGlobalized = true,
  pendingBrowserURL,
  onBrowserURLConsumed,
  onBrowserURLChange,
  browserActivity,
  onBrowserActivityTakeover,
  onBrowserActivityRelease,
  onBrowserActivityStop,
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
  onDirtyFileTabsChange?: (dirty: boolean) => void;
  onReorderTabs: (activeID: string, overID: string) => void;
  onOpenFile: (path: string) => void;
  onClose: () => void;
  globalized: boolean;
  onToggleGlobalize: () => void;
  onOpenSidebar?: () => void;
  canExitGlobalized?: boolean;
  pendingBrowserURL?: string;
  onBrowserURLConsumed?: () => void;
  onBrowserURLChange?: (url: string) => void;
  browserActivity?: ActivitySession;
  onBrowserActivityTakeover?: () => void;
  onBrowserActivityRelease?: () => void;
  onBrowserActivityStop?: () => void;
}): JSX.Element {
  const activeTab = activeTabID ? tabs.find((tab) => tab.id === activeTabID) : undefined;
  const fileTabs = tabs.filter((tab): tab is WorkspaceFileViewTab => tab.kind === "file");
  const showingPicker = !activeTab;
  const [dirtyFileTabIDs, setDirtyFileTabIDs] = useState<Set<string>>(() => new Set());
  const [draggingTabID, setDraggingTabID] = useState<string | undefined>(undefined);
  const [draggingTabWidth, setDraggingTabWidth] = useState<number | undefined>(undefined);
  const tabSensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 6 } }));
  const draggingTab = draggingTabID ? tabs.find((tab) => tab.id === draggingTabID) : undefined;
  const addButtonRef = useRef<HTMLButtonElement>(null);
  const { requestFocusRestoration, tabListRef } = useTabCloseFocusRestoration(
    activeTabID,
    tabs.map((tab) => tab.id),
    addButtonRef,
  );

  useEffect(() => {
    onDirtyFileTabsChange?.(dirtyFileTabIDs.size > 0);
  }, [dirtyFileTabIDs, onDirtyFileTabsChange]);

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

  const updateFileDirtyState = useCallback((tabID: string, dirty: boolean): void => {
    setDirtyFileTabIDs((current) => {
      if (current.has(tabID) === dirty) {
        return current;
      }
      const next = new Set(current);
      if (dirty) {
        next.add(tabID);
      } else {
        next.delete(tabID);
      }
      return next;
    });
  }, []);

  function requestCloseTab(tab: WorkspaceViewTab): void {
    if (
      tab.kind === "file" &&
      dirtyFileTabIDs.has(tab.id) &&
      !window.confirm("此文件有未保存修改。关闭将丢失这些修改，仍要关闭吗？")
    ) {
      return;
    }
    setDirtyFileTabIDs((current) => {
      if (!current.has(tab.id)) {
        return current;
      }
      const next = new Set(current);
      next.delete(tab.id);
      return next;
    });
    requestFocusRestoration();
    onCloseTab(tab.id);
  }

  return (
    <aside
      className={`workspace-right-panel${activeTab ? " detail" : " tools"}${activeTab?.kind === "review" ? " review" : ""}${activeTab?.kind === "diff" ? " diff" : ""}${activeTab?.kind === "file" ? " file" : ""}`}
      aria-hidden={!open}
      inert={!open}
    >
      <div className="workspace-panel-tabbar">
        {globalized && onOpenSidebar ? (
          <button
            className="icon-button workspace-panel-sidebar"
            type="button"
            aria-label="打开导航侧栏"
            title="打开导航侧栏"
            disabled={!open}
            onClick={onOpenSidebar}
          >
            <PanelLeftOpen
              className="icon-lg"
              size={18}
              strokeWidth={1.67}
              viewBox="2 2 20 20"
            />
          </button>
        ) : null}
        <DndContext
          sensors={tabSensors}
          collisionDetection={closestCenter}
          modifiers={[restrictToHorizontalAxis]}
          onDragStart={startTabDrag}
          onDragEnd={endTabDrag}
          onDragCancel={cancelTabDrag}
        >
          <SortableContext items={tabs.map((tab) => tab.id)} strategy={horizontalListSortingStrategy}>
            <div
              ref={tabListRef}
              className="workspace-panel-tabs"
              role="tablist"
              aria-label="产物与工具"
              onKeyDown={handleTabListKeyDown}
            >
              {tabs.map((tab) => {
                const active = tab.id === activeTabID;
                return (
                  <SortableWorkspaceViewTab
                    key={tab.id}
                    tab={tab}
                    active={active}
                    dirty={dirtyFileTabIDs.has(tab.id)}
                    open={open}
                    reorderable={tabs.length > 1}
                    onSelect={() => onSelectTab(tab.id)}
                    onClose={() => requestCloseTab(tab)}
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
                dirty={dirtyFileTabIDs.has(draggingTab.id)}
                width={draggingTabWidth}
              />
            ) : null}
          </DragOverlay>
        </DndContext>
        <span className="workspace-panel-tabbar-spacer" />
        <button
          ref={addButtonRef}
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
          className={`icon-button workspace-panel-globalize${globalized ? " active" : ""}`}
          type="button"
          aria-label={
            globalized && !canExitGlobalized
              ? "窗口过窄，无法停靠右侧栏"
              : globalized
                ? "退出全面板"
                : "展开为全面板"
          }
          title={
            globalized && !canExitGlobalized
              ? "窗口过窄，无法停靠右侧栏"
              : globalized
                ? "退出全面板"
                : "展开为全面板"
          }
          aria-pressed={globalized}
          disabled={!open || (globalized && !canExitGlobalized)}
          onClick={onToggleGlobalize}
        >
          {globalized ? <Minimize2 className="icon" /> : <Maximize2 className="icon" />}
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
      {present || fileTabs.length > 0 ? (
        <>
          <div className={`workspace-panel-body${activeTab ? "" : " picker"}`}>
            {fileTabs.map((tab) => (
              <WorkspaceFileResource
                active={open && tab.id === activeTabID}
                key={tab.id}
                onDirtyChange={updateFileDirtyState}
                tab={tab}
              />
            ))}
            {activeTab?.kind === "file" ? null : !activeTab ? (
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
                activity={browserActivity}
                onActivityTakeover={onBrowserActivityTakeover}
                onActivityRelease={onBrowserActivityRelease}
                onActivityStop={onBrowserActivityStop}
              />
            ) : null}
          </div>
        </>
      ) : null}
    </aside>
  );
}

function WorkspaceFileResource({
  active,
  onDirtyChange,
  tab,
}: {
  active: boolean;
  onDirtyChange: (tabID: string, dirty: boolean) => void;
  tab: WorkspaceFileViewTab;
}): JSX.Element {
  const handleDirtyChange = useCallback(
    (state: WorkspaceFileDirtyState) => onDirtyChange(tab.id, state.dirty),
    [onDirtyChange, tab.id],
  );

  return (
    <div
      className={`workspace-file-resource${active ? " active" : ""}`}
      data-workspace-tab-id={tab.id}
      hidden={!active}
    >
      <WorkspaceFilePreview
        active={active}
        activeContext={tab.context}
        editorResourceID={tab.id}
        selectedFilePath={tab.path}
        onOpenRightPanel={() => {}}
        onDirtyChange={handleDirtyChange}
      />
    </div>
  );
}

function SortableWorkspaceViewTab({
  tab,
  active,
  dirty,
  open,
  reorderable,
  onSelect,
  onClose
}: {
  tab: WorkspaceViewTab;
  active: boolean;
  dirty: boolean;
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
      className={`workspace-tool-tab${active ? " active" : ""}${dirty ? " dirty" : ""}${reorderable ? " can-reorder" : ""}${
        isDragging ? " dragging" : ""
      }`}
      style={style}
      aria-grabbed={isDragging || undefined}
    >
      <button
        ref={setActivatorNodeRef}
        className="workspace-tool-tab-main"
        type="button"
        {...dragAttributes}
        {...listeners}
        role="tab"
        aria-selected={active}
        aria-label={dirty ? `${label}，有未保存修改` : label}
        tabIndex={active ? 0 : -1}
        title={tooltip}
        disabled={!open}
        onClick={onSelect}
      >
        <WorkspaceViewTabIcon tab={tab} className="icon" />
        <span>{label}</span>
        {dirty ? <span className="workspace-tab-dirty-indicator" aria-hidden="true" /> : null}
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
  dirty,
  width
}: {
  tab: WorkspaceViewTab;
  active: boolean;
  dirty: boolean;
  width?: number;
}): JSX.Element {
  const label = workspaceViewTabLabel(tab);
  return (
    <div className={`workspace-tool-tab workspace-tool-tab-drag-overlay${active ? " active" : ""}${dirty ? " dirty" : ""}`} style={width ? { width } : undefined}>
      <div className="workspace-tool-tab-main">
        <WorkspaceViewTabIcon tab={tab} className="icon" />
        <span>{label}</span>
        {dirty ? <span className="workspace-tab-dirty-indicator" aria-hidden="true" /> : null}
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
  return tab.kind === "diff" || tab.kind === "file" ? tab.title : workspaceToolFor(tab.kind).title;
}

function workspaceViewTabTooltip(tab: WorkspaceViewTab): string {
  return tab.kind === "diff" || tab.kind === "file" ? tab.path : workspaceToolFor(tab.kind).title;
}

function WorkspaceViewTabIcon({ tab, className }: { tab: WorkspaceViewTab; className?: string }): JSX.Element {
  if (tab.kind === "diff") {
    return <FileDiff className={className} />;
  }
  if (tab.kind === "file") {
    return <FileText className={className} />;
  }
  return <WorkspaceToolIcon view={tab.kind} className={className} />;
}
