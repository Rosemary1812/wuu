import { useState } from "react";
import type { WorkspacePanelView } from "./WorkspacePanels";
import type { TurnFileDiffSelection } from "./TurnFileDiffTypes";
import {
  useWorkspaceViewTabs,
  workspaceDiffViewTab,
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
  workspacePanelView: WorkspacePanelView;
  setWorkspacePanelView: (view: WorkspacePanelView) => void;
  workspaceMode: WorkspacePanelView | undefined;
  setWorkspaceMode: (view: WorkspacePanelView | undefined) => void;
  ensureWorkspaceToolTab: (view: WorkspacePanelView) => void;
  activateWorkspaceTool: (view: WorkspacePanelView) => void;
  openWorkspaceTool: (view: WorkspacePanelView) => void;
  openWorkspaceDiffTab: (input: { threadID: string; path: string; selection: TurnFileDiffSelection }) => void;
  showWorkspaceToolPicker: () => void;
  focusWorkspaceViewTab: (id: string | undefined) => void;
  closeWorkspaceViewTab: (id: string) => void;
  closeWorkspaceViewTabsWhere: (predicate: (tab: WorkspaceViewTab) => boolean) => void;
  reorderWorkspaceViewTabs: (activeID: string, overID: string) => void;
  toggleRightPanel: () => void;
} {
  const [workspacePanelView, setWorkspacePanelView] = useState<WorkspacePanelView>("files");
  const [workspaceMode, setWorkspaceMode] = useState<WorkspacePanelView | undefined>(undefined);
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
    setWorkspacePanelView(view);
    openTab(workspaceToolViewTab(view));
  }

  function openWorkspaceTool(view: WorkspacePanelView): void {
    activateWorkspaceTool(view);
    setRightPanelOpenWithMotion(true);
  }

  function openWorkspaceDiffTab(input: { threadID: string; path: string; selection: TurnFileDiffSelection }): void {
    openTab(workspaceDiffViewTab(input));
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
    workspacePanelView,
    setWorkspacePanelView,
    workspaceMode,
    setWorkspaceMode,
    ensureWorkspaceToolTab,
    activateWorkspaceTool,
    openWorkspaceTool,
    openWorkspaceDiffTab,
    showWorkspaceToolPicker,
    focusWorkspaceViewTab: focusTab,
    closeWorkspaceViewTab: closeTab,
    closeWorkspaceViewTabsWhere: closeTabsWhere,
    reorderWorkspaceViewTabs: reorderTabs,
    toggleRightPanel
  };
}
