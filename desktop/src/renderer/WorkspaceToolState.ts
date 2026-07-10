import type { RuntimeContext } from "../shared/protocol";
import type { WorkspacePanelView } from "./WorkspacePanels";
import type { TurnFileDiffSelection } from "./TurnFileDiffTypes";
import {
  useWorkspaceViewTabs,
  workspaceDiffViewTab,
  workspaceFileViewTab,
  workspaceToolViewTab,
  type WorkspaceViewTab,
} from "./WorkspaceViewTabs";

export function useWorkspaceToolState({
  rightPanelOpen,
  setRightPanelOpenWithMotion
}: {
  rightPanelOpen: boolean;
  setRightPanelOpenWithMotion: (open: boolean) => void;
}): {
  // Unified right-panel tab strip: the four singleton tools plus zero or
  // more per-file diff tabs. See WorkspaceViewTabs.ts.
  workspaceViewTabs: WorkspaceViewTab[];
  workspaceActiveViewTabID: string | undefined;
  ensureWorkspaceToolTab: (view: WorkspacePanelView) => void;
  activateWorkspaceTool: (view: WorkspacePanelView) => void;
  openWorkspaceTool: (view: WorkspacePanelView) => void;
  openWorkspaceDiffTab: (input: { threadID: string; path: string; selection: TurnFileDiffSelection }) => void;
  openWorkspaceFileTab: (input: { context: RuntimeContext; path: string }) => void;
  showWorkspaceToolPicker: () => void;
  focusWorkspaceViewTab: (id: string | undefined) => void;
  closeWorkspaceViewTab: (id: string) => void;
  closeWorkspaceViewTabsWhere: (predicate: (tab: WorkspaceViewTab) => boolean) => void;
  reorderWorkspaceViewTabs: (activeID: string, overID: string) => void;
  toggleRightPanel: () => void;
} {
  const {
    tabs: workspaceViewTabs,
    activeTabID: workspaceActiveViewTabID,
    openTab,
    focusTab,
    closeTab,
    closeTabsWhere,
    reorderTabs,
  } = useWorkspaceViewTabs();

  function ensureWorkspaceToolTab(view: WorkspacePanelView): void {
    if (!workspaceViewTabs.some((tab) => tab.id === view)) {
      openTab(workspaceToolViewTab(view));
    }
  }

  function activateWorkspaceTool(view: WorkspacePanelView): void {
    openTab(workspaceToolViewTab(view));
  }

  function openWorkspaceTool(view: WorkspacePanelView): void {
    activateWorkspaceTool(view);
    setRightPanelOpenWithMotion(true);
  }

  function openWorkspaceDiffTab(input: { threadID: string; path: string; selection: TurnFileDiffSelection }): void {
    openTab(workspaceDiffViewTab(input));
  }

  function openWorkspaceFileTab(input: { context: RuntimeContext; path: string }): void {
    openTab(workspaceFileViewTab(input));
    setRightPanelOpenWithMotion(true);
  }

  function showWorkspaceToolPicker(): void {
    focusTab(undefined);
    setRightPanelOpenWithMotion(true);
  }

  function toggleRightPanel(): void {
    if (rightPanelOpen) {
      setRightPanelOpenWithMotion(false);
      return;
    }
    if (workspaceActiveViewTabID === undefined && workspaceViewTabs.length > 0) {
      focusTab(workspaceViewTabs[workspaceViewTabs.length - 1].id);
    }
    setRightPanelOpenWithMotion(true);
  }

  return {
    workspaceViewTabs,
    workspaceActiveViewTabID,
    ensureWorkspaceToolTab,
    activateWorkspaceTool,
    openWorkspaceTool,
    openWorkspaceDiffTab,
    openWorkspaceFileTab,
    showWorkspaceToolPicker,
    focusWorkspaceViewTab: focusTab,
    closeWorkspaceViewTab: closeTab,
    closeWorkspaceViewTabsWhere: closeTabsWhere,
    reorderWorkspaceViewTabs: reorderTabs,
    toggleRightPanel
  };
}
