import { useEffect, useRef, type RefObject } from "react";
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
import { environmentPanelScaleForWidth } from "./EnvironmentPanelScale";
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

function useEnvironmentPanelScale(
  stackRef: RefObject<HTMLDivElement | null>,
  enabled: boolean,
): void {
  useEffect(() => {
    const stack = stackRef.current;
    const container = stack?.parentElement;
    if (!enabled || !stack || !container) {
      return;
    }

    const applyScale = (width: number): void => {
      const scale = environmentPanelScaleForWidth(width);
      stack.style.setProperty(
        "--environment-panel-scale",
        String(scale),
      );
    };
    applyScale(container.clientWidth);

    if (typeof ResizeObserver === "undefined") {
      const handleResize = (): void => applyScale(container.clientWidth);
      window.addEventListener("resize", handleResize);
      return () => window.removeEventListener("resize", handleResize);
    }

    const observer = new ResizeObserver((entries) => {
      applyScale(entries[0]?.contentRect.width ?? container.clientWidth);
    });
    observer.observe(container);
    return () => observer.disconnect();
  }, [enabled, stackRef]);
}

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
  onAddThreadMember?: (
    threadID: string,
    participantID: string,
  ) => Promise<void> | void;
  onRemoveThreadMember?: (
    threadID: string,
    participantID: string,
  ) => Promise<void> | void;
}): JSX.Element | null {
  const stackRef = useRef<HTMLDivElement>(null);
  const shouldRender = (visible || mounted) && Boolean(state.initialized);
  const thread = state.initialized ? activeThreadForState(state) : undefined;
  const groupThread = Boolean(thread && isGroupThread(thread));
  useEnvironmentPanelScale(stackRef, shouldRender && !groupThread);

  if (!shouldRender || !state.initialized) {
    return null;
  }

  if (thread && isGroupThread(thread)) {
    return (
      <div className="environment-side-stack">
        <GroupInfoPanel
          panelRef={panelRef}
          motionState={closing ? "closing" : motionState}
          thread={thread}
          members={thread.members ?? []}
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
    <div
      className="environment-side-stack environment-info-side-stack"
      ref={stackRef}
    >
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
