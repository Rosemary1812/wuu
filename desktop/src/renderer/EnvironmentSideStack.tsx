import type { RefObject } from "react";
import type {
  Agent,
  ParticipantProfile,
  PlanUpdate,
} from "../shared/protocol";
import { activeThreadForState, isGroupThread, type AppState } from "./AppState";
import {
  EnvironmentPanel,
  type EnvironmentPanelMenu,
  type EnvironmentPanelMotionState,
} from "./EnvironmentPanel";
import { GroupInfoPanel } from "./GroupInfoPanel";

export type SubagentRowSummary = Pick<
  Agent,
  | "id"
  | "type"
  | "task_name"
  | "agent_path"
  | "description"
  | "status"
  | "pinned"
  | "archived"
  | "started_at"
  | "completed_at"
  | "nested_count"
  | "nested_running_count"
  | "participant"
>;

export function EnvironmentSideStack({
  visible,
  mounted,
  state,
  panelRef,
  closing,
  motionState,
  planUpdate,
  activeMenu,
  running,
  pullRequestDisabledReason,
  rightPanelFilePath,
  onCloseFilePreview,
  onSetActiveMenu,
  onClose,
  onSelectBranch,
  onCreateBranch,
  onOpenReview,
  onOpenCommit,
  onOpenPullRequest,
  subagentSessions,
  archiveConfirmSubagentID,
  onSelectSubagent,
  onToggleSubagentPinned,
  onArchiveSubagent,
  onClearSubagentArchiveConfirm,
  participants = [],
  busyParticipantIDs,
  onAddThreadMember,
  onRemoveThreadMember,
}: {
  visible: boolean;
  mounted: boolean;
  state: AppState;
  panelRef: RefObject<HTMLDivElement | null>;
  closing: boolean;
  motionState: EnvironmentPanelMotionState;
  planUpdate?: PlanUpdate;
  activeMenu: EnvironmentPanelMenu;
  running: boolean;
  pullRequestDisabledReason: string;
  /**
   * Absolute path of the file the right panel should preview. When set
   * together with `activeMenu === "file"`, the panel swaps to a file
   * viewer; `onCloseFilePreview` returns it to the default environment view.
   */
  rightPanelFilePath?: string;
  onCloseFilePreview?: () => void;
  onSetActiveMenu: (menu: EnvironmentPanelMenu) => void;
  onClose: () => void;
  onSelectBranch: (branch: string) => void;
  onCreateBranch: (branch: string) => Promise<void>;
  onOpenReview: () => void;
  onOpenCommit: () => void;
  onOpenPullRequest: () => void;
  /**
   * Subagent sessions owned by the active thread. Rendered in the
   * environment panel as a "子任务" section. When undefined/empty the
   * section is hidden, matching the user intent that no row appears unless
   * the main session actually has subagents.
   */
  subagentSessions?: SubagentRowSummary[];
  /**
   * ID of the subagent currently in "press again to confirm" archive
   * state. Mirrors `archiveConfirmThreadID` for the sidebar's session
   * rows so the UX stays consistent between the two surfaces.
   */
  archiveConfirmSubagentID?: string;
  onSelectSubagent?: (agent: SubagentRowSummary) => void;
  onToggleSubagentPinned?: (agent: SubagentRowSummary) => void;
  onArchiveSubagent?: (agent: SubagentRowSummary) => void;
  onClearSubagentArchiveConfirm?: (agentID: string) => void;
  participants?: ParticipantProfile[];
  busyParticipantIDs?: ReadonlySet<string>;
  onAddThreadMember?: (
    threadID: string,
    participantID: string,
  ) => Promise<void> | void;
  onRemoveThreadMember?: (
    threadID: string,
    participantID: string,
  ) => Promise<void> | void;
}): JSX.Element | null {
  if ((!visible && !mounted) || !state.initialized) {
    return null;
  }

  const thread = activeThreadForState(state);
  if (thread && isGroupThread(thread)) {
    return (
      <div className="environment-side-stack">
        <GroupInfoPanel
          panelRef={panelRef}
          motionState={closing ? "closing" : motionState}
          thread={thread}
          members={thread.members ?? []}
          busyParticipantIDs={busyParticipantIDs}
          participants={participants}
          onClose={onClose}
          onAddMember={
            onAddThreadMember
              ? (participantID) => onAddThreadMember(thread.id, participantID)
              : undefined
          }
          onRemoveMember={
            onRemoveThreadMember
              ? (participantID) =>
                  onRemoveThreadMember(thread.id, participantID)
              : undefined
          }
        />
      </div>
    );
  }

  return (
    <div className="environment-side-stack">
      <EnvironmentPanel
        panelRef={panelRef}
        motionState={closing ? "closing" : motionState}
        initialized={state.initialized}
        gitStatus={state.gitStatus}
        planUpdate={planUpdate}
        activeMenu={activeMenu}
        running={running}
        pullRequestDisabledReason={pullRequestDisabledReason}
        rightPanelFilePath={rightPanelFilePath}
        onCloseFilePreview={onCloseFilePreview}
        onSetActiveMenu={onSetActiveMenu}
        onClose={onClose}
        onSelectBranch={onSelectBranch}
        onCreateBranch={onCreateBranch}
        onOpenReview={onOpenReview}
        onOpenCommit={onOpenCommit}
        onOpenPullRequest={onOpenPullRequest}
        subagentSessions={subagentSessions}
        archiveConfirmSubagentID={archiveConfirmSubagentID}
        onSelectSubagent={onSelectSubagent}
        onToggleSubagentPinned={onToggleSubagentPinned}
        onArchiveSubagent={onArchiveSubagent}
        onClearSubagentArchiveConfirm={onClearSubagentArchiveConfirm}
      />
    </div>
  );
}
