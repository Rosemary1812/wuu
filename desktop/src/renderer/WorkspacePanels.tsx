import {
  type CSSProperties,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";
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
import {
  formatWorkspaceFileTarget,
  parseWorkspaceFileTarget,
  resolveWorkspaceFileTarget,
} from "./LinkTargets";
import { TurnFileDiffPanel } from "./TurnFileDiffPanel";
import { WorkspaceBrowserPanel } from "./WorkspaceBrowserPanel";
import {
  WorkspaceFilePreview,
  WorkspaceFileTree,
  WorkspacePanelEmpty,
  type WorkspaceFileDirtyState,
} from "./WorkspaceFiles";
import { WorkspaceReviewPanel } from "./WorkspaceReviewPanels";
import { WorkspaceTerminalPanel } from "./WorkspaceTerminalPanel";
import type { WorkspaceFileViewTab, WorkspaceViewTab } from "./WorkspaceViewTabs";
import { handleTabListKeyDown, useTabCloseFocusRestoration } from "./TabKeyboardNavigation";
import { useStripEnterReady, useTabExitRetention } from "./TabMotion";

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

export const WORKSPACE_FILE_TREE_DEFAULT_WIDTH = 320;
export const WORKSPACE_FILE_TREE_MIN_WIDTH = 180;
export const WORKSPACE_FILE_TREE_MAX_WIDTH = 480;
export const WORKSPACE_FILE_CONTENT_MIN_WIDTH = 240;
const WORKSPACE_FILE_TREE_WIDTH_STEP = 24;
const WORKSPACE_FILE_TREE_WIDTH_KEY = "wuu.desktop.fileTreeWidth";

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

export function clampWorkspaceFileTreeWidth(
  width: number,
  panelWidth = Number.POSITIVE_INFINITY,
): number {
  if (!Number.isFinite(panelWidth) || panelWidth <= 0) {
    return clamp(width, WORKSPACE_FILE_TREE_MIN_WIDTH, WORKSPACE_FILE_TREE_MAX_WIDTH);
  }
  const maxForPanel = Math.max(
    WORKSPACE_FILE_TREE_MIN_WIDTH,
    Math.min(WORKSPACE_FILE_TREE_MAX_WIDTH, panelWidth - WORKSPACE_FILE_CONTENT_MIN_WIDTH),
  );
  return clamp(width, WORKSPACE_FILE_TREE_MIN_WIDTH, maxForPanel);
}

function initialWorkspaceFileTreeWidth(): number {
  if (typeof window === "undefined") {
    return WORKSPACE_FILE_TREE_DEFAULT_WIDTH;
  }
  const stored = Number(window.localStorage.getItem(WORKSPACE_FILE_TREE_WIDTH_KEY));
  if (!Number.isFinite(stored) || stored <= 0) {
    return WORKSPACE_FILE_TREE_DEFAULT_WIDTH;
  }
  return clampWorkspaceFileTreeWidth(stored);
}

export function WorkspaceRightPanel({
  open,
  present,
  tabs,
  activeTabID,
  activeFileTabID,
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
  sheetPhase = "docked",
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
  activeFileTabID?: string;
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
  // Globalize-sheet phase from App's phase machine; drives the data-sheet
  // attribute that promotes the panel to a full-window sheet in CSS.
  sheetPhase?: "docked" | "arming" | "open" | "exiting" | "docking";
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
  const visibleTabs = tabs;
  const showingPicker = !activeTab;
  const [dirtyFileTabIDs, setDirtyFileTabIDs] = useState<Set<string>>(() => new Set());
  const enterReady = useStripEnterReady();
  const tabEntries = useTabExitRetention(visibleTabs, (tab) => tab.id);
  const [draggingTabID, setDraggingTabID] = useState<string | undefined>(undefined);
  const [draggingTabWidth, setDraggingTabWidth] = useState<number | undefined>(undefined);
  const [fileTreeWidth, setFileTreeWidth] = useState(initialWorkspaceFileTreeWidth);
  const fileTreePreferredWidthRef = useRef(fileTreeWidth);
  const [resizingFileSplit, setResizingFileSplit] = useState(false);
  const fileSplitRef = useRef<HTMLDivElement>(null);
  const fileSplitResizeRef = useRef<{ startX: number; startTreeWidth: number } | null>(null);
  const tabSensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 6 } }));
  const draggingTab = draggingTabID ? tabs.find((tab) => tab.id === draggingTabID) : undefined;
  const addButtonRef = useRef<HTMLButtonElement>(null);
  const { requestFocusRestoration, tabListRef } = useTabCloseFocusRestoration(
    activeTabID,
    visibleTabs.map((tab) => tab.id),
    addButtonRef,
  );

  useEffect(() => {
    onDirtyFileTabsChange?.(dirtyFileTabIDs.size > 0);
  }, [dirtyFileTabIDs, onDirtyFileTabsChange]);

  useEffect(() => {
    window.localStorage.setItem(
      WORKSPACE_FILE_TREE_WIDTH_KEY,
      String(fileTreePreferredWidthRef.current),
    );
  }, []);

  useEffect(() => {
    const split = fileSplitRef.current;
    if (!split || typeof ResizeObserver === "undefined") {
      return undefined;
    }
    const splitElement = split;

    function fitFileTreeToPanel(): void {
      const panelWidth = splitElement.getBoundingClientRect().width;
      if (panelWidth <= 0) {
        return;
      }
      setFileTreeWidth((current) => {
        const next = clampWorkspaceFileTreeWidth(
          fileTreePreferredWidthRef.current,
          panelWidth,
        );
        return next === current ? current : next;
      });
    }

    const observer = new ResizeObserver(fitFileTreeToPanel);
    observer.observe(splitElement);
    fitFileTreeToPanel();
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    const root = document.documentElement;
    root.classList.toggle("resizing-workspace-file-split", resizingFileSplit);
    if (!resizingFileSplit) {
      return () => root.classList.remove("resizing-workspace-file-split");
    }

    function handlePointerMove(event: PointerEvent): void {
      const session = fileSplitResizeRef.current;
      if (!session) {
        return;
      }
      const panelWidth = fileSplitRef.current?.getBoundingClientRect().width;
      setPreferredFileTreeWidth(
        session.startTreeWidth - (event.clientX - session.startX),
        panelWidth,
      );
    }

    function finishResize(): void {
      fileSplitResizeRef.current = null;
      setResizingFileSplit(false);
    }

    window.addEventListener("pointermove", handlePointerMove);
    window.addEventListener("pointerup", finishResize);
    window.addEventListener("pointercancel", finishResize);
    return () => {
      root.classList.remove("resizing-workspace-file-split");
      window.removeEventListener("pointermove", handlePointerMove);
      window.removeEventListener("pointerup", finishResize);
      window.removeEventListener("pointercancel", finishResize);
    };
  }, [resizingFileSplit]);

  function resizeFileTreeBy(delta: number): void {
    const panelWidth = fileSplitRef.current?.getBoundingClientRect().width;
    setPreferredFileTreeWidth(fileTreeWidth + delta, panelWidth);
  }

  function setPreferredFileTreeWidth(width: number, panelWidth?: number): void {
    const preferredWidth = clampWorkspaceFileTreeWidth(width);
    fileTreePreferredWidthRef.current = preferredWidth;
    window.localStorage.setItem(WORKSPACE_FILE_TREE_WIDTH_KEY, String(preferredWidth));
    setFileTreeWidth(clampWorkspaceFileTreeWidth(preferredWidth, panelWidth));
  }

  function startFileSplitResize(event: ReactPointerEvent<HTMLDivElement>): void {
    if (event.button !== 0) {
      return;
    }
    event.preventDefault();
    fileSplitResizeRef.current = { startX: event.clientX, startTreeWidth: fileTreeWidth };
    setResizingFileSplit(true);
  }

  function handleFileSplitKeyDown(event: ReactKeyboardEvent<HTMLDivElement>): void {
    if (event.key === "ArrowLeft") {
      event.preventDefault();
      resizeFileTreeBy(WORKSPACE_FILE_TREE_WIDTH_STEP);
    } else if (event.key === "ArrowRight") {
      event.preventDefault();
      resizeFileTreeBy(-WORKSPACE_FILE_TREE_WIDTH_STEP);
    } else if (event.key === "Home") {
      event.preventDefault();
      resizeFileTreeBy(WORKSPACE_FILE_TREE_MAX_WIDTH);
    } else if (event.key === "End") {
      event.preventDefault();
      resizeFileTreeBy(-WORKSPACE_FILE_TREE_MAX_WIDTH);
    }
  }

  function resetFileTreeWidth(): void {
    const panelWidth = fileSplitRef.current?.getBoundingClientRect().width;
    setPreferredFileTreeWidth(WORKSPACE_FILE_TREE_DEFAULT_WIDTH, panelWidth);
  }

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
      className={`workspace-right-panel${activeTab ? " detail" : " tools"}${activeTab?.kind === "review" ? " review" : ""}${activeTab?.kind === "diff" ? " diff" : ""}${activeTab?.kind === "files" || activeTab?.kind === "file" ? " files" : ""}`}
      data-sheet={
        sheetPhase === "exiting"
          ? "parked"
          : sheetPhase === "docked"
            ? undefined
            : sheetPhase
      }
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
          <SortableContext items={visibleTabs.map((tab) => tab.id)} strategy={horizontalListSortingStrategy}>
            <div
              ref={tabListRef}
              className="workspace-panel-tabs"
              role="tablist"
              aria-label="产物与工具"
              data-enter-ready={enterReady ? "" : undefined}
              onKeyDown={handleTabListKeyDown}
            >
              {tabEntries.map((entry) => {
                const tab = entry.tab;
                if (entry.closing) {
                  // Exit retention (TabMotion.ts): inert collapsing ghost so
                  // the neighbours slide over instead of jumping.
                  return (
                    <div
                      key={`closing-${tab.id}`}
                      className="workspace-tool-tab closing"
                      aria-hidden="true"
                    >
                      <span className="workspace-tool-tab-main">
                        <WorkspaceViewTabIcon tab={tab} className="icon" />
                        <span>{workspaceViewTabLabel(tab)}</span>
                      </span>
                    </div>
                  );
                }
                const active = tab.id === activeTabID;
                return (
                  <SortableWorkspaceViewTab
                    key={tab.id}
                    tab={tab}
                    active={active}
                    dirty={tab.kind === "file" && dirtyFileTabIDs.has(tab.id)}
                    open={open}
                    reorderable={visibleTabs.length > 1}
                    onSelect={() => onSelectTab(tab.id)}
                    onClose={() => requestCloseTab(tab)}
                    onDoubleClick={() => requestCloseTab(tab)}
                  />
                );
              })}
            </div>
          </SortableContext>
          {/* Portaled to <body>: the overlay is position:fixed and dnd-kit
            * places it in viewport coordinates, but .workspace-right-panel
            * has transform/will-change/contain — any of which makes it the
            * containing block for fixed descendants, so an in-panel overlay
            * renders offset by the panel's own position ("drifts" the moment
            * the drag starts). The session strip doesn't need this because
            * no ancestor of it is transformed. React portals keep context,
            * so DndContext still drives the overlay. */}
          {createPortal(
            <DragOverlay dropAnimation={{ duration: 150, easing: "cubic-bezier(0.16, 1, 0.3, 1)" }}>
              {draggingTab ? (
                <WorkspaceViewTabPreview
                  tab={draggingTab}
                  active={draggingTab.id === activeTabID}
                  dirty={dirtyFileTabIDs.has(draggingTab.id)}
                  width={draggingTabWidth}
                />
              ) : null}
            </DragOverlay>,
            document.body,
          )}
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
            <div
              className={`workspace-files-split${resizingFileSplit ? " resizing" : ""}`}
              hidden={activeTab?.kind !== "files" && activeTab?.kind !== "file"}
              ref={fileSplitRef}
              style={{ "--workspace-file-tree-width": `${fileTreeWidth}px` } as CSSProperties}
            >
              <section className="workspace-files-content" aria-label="文件内容">
                <div className="workspace-files-content-body">
                  {fileTabs.map((tab) => (
                    <WorkspaceFileResource
                      active={open && activeTab?.kind === "file" && tab.id === activeFileTabID}
                      key={tab.id}
                      onDirtyChange={updateFileDirtyState}
                      onOpenFile={onOpenFile}
                      tab={tab}
                    />
                  ))}
                  {activeTab?.kind === "files" ? (
                    <WorkspacePanelEmpty
                      title="选择文件"
                      description="从右侧文件树中选择要查看的文件。"
                      icon={<FileText size={24} />}
                    />
                  ) : null}
                </div>
              </section>
              <div
                className="workspace-files-resizer"
                role="separator"
                aria-label="调整文件内容和文件树宽度"
                aria-orientation="vertical"
                aria-valuemin={WORKSPACE_FILE_TREE_MIN_WIDTH}
                aria-valuemax={WORKSPACE_FILE_TREE_MAX_WIDTH}
                aria-valuenow={Math.round(fileTreeWidth)}
                tabIndex={0}
                onPointerDown={startFileSplitResize}
                onDoubleClick={resetFileTreeWidth}
                onKeyDown={handleFileSplitKeyDown}
              />
              <section className="workspace-files-tree" aria-label="文件树">
                <WorkspaceFileTree
                  activeContext={workspaceContext}
                  open={open && (activeTab?.kind === "files" || activeTab?.kind === "file")}
                  selectedFilePath={selectedFilePath}
                  onOpenFile={onOpenFile}
                />
              </section>
            </div>
            {activeTab?.kind === "files" || activeTab?.kind === "file" ? null : (
              <div
                className="workspace-panel-content-swap"
                key={activeTab?.id ?? "picker"}
              >
                {!activeTab ? (
                  <WorkspaceToolPicker tabs={tabs} onSelectTool={onOpenTool} />
                ) : activeTab.kind === "diff" ? (
                  <TurnFileDiffPanel
                    selection={activeTab.selection}
                    onClose={() => onCloseTab(activeTab.id)}
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
            )}
          </div>
        </>
      ) : null}
    </aside>
  );
}

function WorkspaceFileResource({
  active,
  onDirtyChange,
  onOpenFile,
  tab,
}: {
  active: boolean;
  onDirtyChange: (tabID: string, dirty: boolean) => void;
  onOpenFile: (path: string) => void;
  tab: WorkspaceFileViewTab;
}): JSX.Element {
  const handleDirtyChange = useCallback(
    (state: WorkspaceFileDirtyState) => onDirtyChange(tab.id, state.dirty),
    [onDirtyChange, tab.id],
  );
  const handleOpenFile = useCallback((reference: string) => {
    const target = parseWorkspaceFileTarget(reference);
    onOpenFile(
      target
        ? formatWorkspaceFileTarget(resolveWorkspaceFileTarget(tab.path, target))
        : reference,
    );
  }, [onOpenFile, tab.path]);

  return (
    <div
      className={`workspace-file-resource${active ? " active" : ""}`}
      data-workspace-tab-id={tab.id}
      hidden={!active}
    >
      <WorkspaceFilePreview
        active={active}
        activeContext={tab.context}
        anchor={tab.anchor}
        editorResourceID={tab.id}
        selection={tab.selection}
        selectedFilePath={tab.path}
        onOpenRightPanel={() => {}}
        onOpenFile={handleOpenFile}
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
  onClose,
  onDoubleClick
}: {
  tab: WorkspaceViewTab;
  active: boolean;
  dirty: boolean;
  open: boolean;
  reorderable: boolean;
  onSelect: () => void;
  onClose: () => void;
  // Parity with session tabs: double-click closes (through the dirty-file
  // confirm guard upstream).
  onDoubleClick: () => void;
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
        onDoubleClick={onDoubleClick}
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
