import type { RefObject } from "react";
import type { DesktopProject, PlanUpdate } from "../shared/protocol";
import type { AppState } from "./AppState";
import {
  EnvironmentPanel,
  type BackgroundProcessItem,
  type EnvironmentPanelMenu,
  type EnvironmentPanelMotionState,
} from "./EnvironmentPanel";
import type { QueryHistoryEntry } from "./QueryHistoryPopover";
import { QueryHistoryPopover } from "./QueryHistoryPopover";

export function EnvironmentSideStack({
  visible,
  mounted,
  state,
  panelRef,
  closing,
  motionState,
  activeProject,
  planUpdate,
  backgroundProcesses,
  stoppingProcessIDs,
  activeMenu,
  running,
  pullRequestDisabledReason,
  queryHistoryDocked,
  queryHistory,
  onSetActiveMenu,
  onClose,
  onOpenProject,
  onSelectNoProject,
  onSelectBranch,
  onCreateBranch,
  onOpenReview,
  onOpenCommit,
  onOpenPullRequest,
  onStopBackgroundProcess,
  onOpenBackgroundPreview,
  onSelectQueryHistory,
}: {
  visible: boolean;
  mounted: boolean;
  state: AppState;
  panelRef: RefObject<HTMLDivElement | null>;
  closing: boolean;
  motionState: EnvironmentPanelMotionState;
  activeProject?: DesktopProject;
  planUpdate?: PlanUpdate;
  backgroundProcesses: BackgroundProcessItem[];
  stoppingProcessIDs: Set<string>;
  activeMenu: EnvironmentPanelMenu;
  running: boolean;
  pullRequestDisabledReason: string;
  queryHistoryDocked: boolean;
  queryHistory: QueryHistoryEntry[];
  onSetActiveMenu: (menu: EnvironmentPanelMenu) => void;
  onClose: () => void;
  onOpenProject: () => void;
  onSelectNoProject: () => void;
  onSelectBranch: (branch: string) => void;
  onCreateBranch: (branch: string) => Promise<void>;
  onOpenReview: () => void;
  onOpenCommit: () => void;
  onOpenPullRequest: () => void;
  onStopBackgroundProcess: (process: BackgroundProcessItem) => void;
  onOpenBackgroundPreview: (process: BackgroundProcessItem) => void;
  onSelectQueryHistory: (entry: QueryHistoryEntry) => void;
}): JSX.Element | null {
  if ((!visible && !mounted) || !state.initialized) {
    return null;
  }

  return (
    <div className="environment-side-stack">
      <EnvironmentPanel
        panelRef={panelRef}
        motionState={closing ? "closing" : motionState}
        initialized={state.initialized}
        gitStatus={state.gitStatus}
        activeContext={state.activeContext}
        activeProject={activeProject}
        planUpdate={planUpdate}
        backgroundProcesses={backgroundProcesses}
        stoppingProcessIDs={stoppingProcessIDs}
        activeMenu={activeMenu}
        running={running}
        pullRequestDisabledReason={pullRequestDisabledReason}
        onSetActiveMenu={onSetActiveMenu}
        onClose={onClose}
        onOpenProject={onOpenProject}
        onSelectNoProject={onSelectNoProject}
        onSelectBranch={onSelectBranch}
        onCreateBranch={onCreateBranch}
        onOpenReview={onOpenReview}
        onOpenCommit={onOpenCommit}
        onOpenPullRequest={onOpenPullRequest}
        onStopBackgroundProcess={onStopBackgroundProcess}
        onOpenBackgroundPreview={onOpenBackgroundPreview}
      />
      {queryHistoryDocked ? (
        <div className="query-history-environment-slot">
          <QueryHistoryPopover
            entries={queryHistory}
            onSelect={onSelectQueryHistory}
          />
        </div>
      ) : null}
    </div>
  );
}
