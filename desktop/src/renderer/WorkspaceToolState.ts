import { useState } from "react";
import { arrayMove } from "@dnd-kit/sortable";
import type {
  WorkspacePanelView,
  WorkspaceRightPanelView
} from "./WorkspacePanels";

export function useWorkspaceToolState({
  rightPanelOpen,
  setRightPanelOpenWithMotion
}: {
  rightPanelOpen: boolean;
  setRightPanelOpenWithMotion: (open: boolean) => void;
}): {
  workspaceToolTabs: WorkspacePanelView[];
  workspacePanelView: WorkspacePanelView;
  setWorkspacePanelView: (view: WorkspacePanelView) => void;
  workspaceRightPanelView: WorkspaceRightPanelView;
  setWorkspaceRightPanelView: (view: WorkspaceRightPanelView) => void;
  workspaceMode: WorkspacePanelView | undefined;
  setWorkspaceMode: (view: WorkspacePanelView | undefined) => void;
  ensureWorkspaceToolTab: (view: WorkspacePanelView) => void;
  activateWorkspaceTool: (view: WorkspacePanelView) => void;
  openWorkspaceTool: (view: WorkspacePanelView) => void;
  showWorkspaceToolPicker: () => void;
  closeWorkspaceToolTab: (view: WorkspacePanelView) => void;
  reorderWorkspaceToolTabs: (activeView: WorkspacePanelView, overView: WorkspacePanelView) => void;
  toggleRightPanel: () => void;
} {
  const [workspaceToolTabs, setWorkspaceToolTabs] = useState<WorkspacePanelView[]>([]);
  const [workspacePanelView, setWorkspacePanelView] = useState<WorkspacePanelView>("files");
  const [workspaceRightPanelView, setWorkspaceRightPanelView] =
    useState<WorkspaceRightPanelView>("tools");
  const [workspaceMode, setWorkspaceMode] = useState<WorkspacePanelView | undefined>(undefined);

  function ensureWorkspaceToolTab(view: WorkspacePanelView): void {
    setWorkspaceToolTabs((current) =>
      current.includes(view) ? current : [...current, view]
    );
  }

  function activateWorkspaceTool(view: WorkspacePanelView): void {
    setWorkspacePanelView(view);
    setWorkspaceRightPanelView(view);
  }

  function openWorkspaceTool(view: WorkspacePanelView): void {
    ensureWorkspaceToolTab(view);
    activateWorkspaceTool(view);
    setRightPanelOpenWithMotion(true);
  }

  function showWorkspaceToolPicker(): void {
    setWorkspaceRightPanelView("tools");
    setRightPanelOpenWithMotion(true);
  }

  function closeWorkspaceToolTab(view: WorkspacePanelView): void {
    const nextTabs = workspaceToolTabs.filter((item) => item !== view);
    setWorkspaceToolTabs(nextTabs);

    if (workspaceRightPanelView !== view) {
      return;
    }

    const closedIndex = workspaceToolTabs.indexOf(view);
    const fallback =
      nextTabs[Math.min(Math.max(closedIndex, 0), Math.max(nextTabs.length - 1, 0))];
    if (fallback) {
      activateWorkspaceTool(fallback);
      return;
    }
    setWorkspaceRightPanelView("tools");
  }

  function reorderWorkspaceToolTabs(
    activeView: WorkspacePanelView,
    overView: WorkspacePanelView
  ): void {
    if (activeView === overView) {
      return;
    }
    setWorkspaceToolTabs((current) => {
      const sourceIndex = current.indexOf(activeView);
      const targetIndex = current.indexOf(overView);
      if (sourceIndex < 0 || targetIndex < 0) {
        return current;
      }
      return arrayMove(current, sourceIndex, targetIndex);
    });
  }

  function toggleRightPanel(): void {
    if (rightPanelOpen) {
      setRightPanelOpenWithMotion(false);
      return;
    }
    if (workspaceRightPanelView === "tools" && workspaceToolTabs.length > 0) {
      activateWorkspaceTool(workspaceToolTabs[workspaceToolTabs.length - 1]);
    }
    setRightPanelOpenWithMotion(true);
  }

  return {
    workspaceToolTabs,
    workspacePanelView,
    setWorkspacePanelView,
    workspaceRightPanelView,
    setWorkspaceRightPanelView,
    workspaceMode,
    setWorkspaceMode,
    ensureWorkspaceToolTab,
    activateWorkspaceTool,
    openWorkspaceTool,
    showWorkspaceToolPicker,
    closeWorkspaceToolTab,
    reorderWorkspaceToolTabs,
    toggleRightPanel
  };
}
