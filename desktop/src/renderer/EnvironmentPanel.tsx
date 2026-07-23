import {
  AlertCircle,
  Archive,
  Check,
  ChevronRight,
  CornerDownRight,
  FileText,
  FileX,
  FolderPlus,
  Github,
  GitBranch,
  Pin,
  Plus,
  Search,
  X
} from "lucide-react";
import { type FormEvent as ReactFormEvent, type RefObject, useEffect, useState } from "react";
import type {
  Agent,
  GitStatusResult,
  InitializeResult,
  PlanUpdate,
  WorkspaceFileReadResult
} from "../shared/protocol";
import { desktopApiErrorMessage, formatBytes } from "./WorkspaceReviewHelpers";
import { agentStatusLabel, sortChildAgents } from "./ThreadAgents";
import { ParticipantChip } from "./ParticipantChip";
import { type SubagentRowSummary } from "./EnvironmentSideStack";
import { agentNameForSubagentID } from "./agentNames";
import { useI18n } from "./i18n";

export type EnvironmentPanelMenu = "branch" | "file" | null;
export type EnvironmentPanelMotionState = "open" | "closing";

const MAX_VISIBLE_ENVIRONMENT_SUBAGENTS = 5;

export function EnvironmentPanel({
  panelRef,
  motionState,
  gitStatus,
  planUpdate,
  activeMenu,
  running,
  pullRequestDisabledReason,
  onSetActiveMenu,
  onClose,
  onSelectBranch,
  onCreateBranch,
  onOpenReview,
  onOpenCommit,
  onOpenPullRequest,
  rightPanelFilePath,
  onCloseFilePreview,
  subagentSessions,
  archiveConfirmSubagentID,
  onSelectSubagent,
  onToggleSubagentPinned,
  onArchiveSubagent,
  onClearSubagentArchiveConfirm
}: {
  panelRef: RefObject<HTMLDivElement | null>;
  motionState: EnvironmentPanelMotionState;
  initialized: InitializeResult;
  gitStatus?: GitStatusResult;
  planUpdate?: PlanUpdate;
  activeMenu: EnvironmentPanelMenu;
  running: boolean;
  pullRequestDisabledReason: string;
  onSetActiveMenu: (menu: EnvironmentPanelMenu) => void;
  onClose: () => void;
  onSelectBranch: (branch: string) => void;
  onCreateBranch: (branch: string) => Promise<void>;
  onOpenReview: () => void;
  onOpenCommit: () => void;
  onOpenPullRequest: () => void;
  /**
   * Absolute path to a workspace file the right panel should preview. When
   * present together with `activeMenu === "file"`, the panel swaps its
   * default environment body for a file viewer that reads the file via
   * `window.wuu.readWorkspaceFile`.
   */
  rightPanelFilePath?: string;
  /**
   * Invoked when the user closes the file preview from inside the panel.
   * Falls back to `onClose` (which dismisses the whole panel) when the
   * caller does not provide a file-specific closer.
   */
  onCloseFilePreview?: () => void;
  /**
   * Subagent sessions owned by the active main session. When the array is
   * undefined or empty the "子任务" section is hidden entirely so the panel
   * stays quiet for sessions that never spawned one.
   */
  subagentSessions?: SubagentRowSummary[];
  /**
   * ID of the subagent currently sitting in the "press again to confirm"
   * archive state. Mirrors `archiveConfirmThreadID` in the sidebar so the
   * UX stays consistent between the two surfaces.
   */
  archiveConfirmSubagentID?: string;
  onSelectSubagent?: (agent: SubagentRowSummary) => void;
  onToggleSubagentPinned?: (agent: SubagentRowSummary) => void;
  onArchiveSubagent?: (agent: SubagentRowSummary) => void;
  onClearSubagentArchiveConfirm?: (agentID: string) => void;
}): JSX.Element {
  const { t, formatNumber } = useI18n();
  if (activeMenu === "file" && rightPanelFilePath) {
    return (
      <EnvironmentFilePreview
        filePath={rightPanelFilePath}
        onClose={onCloseFilePreview ?? onClose}
        panelRef={panelRef}
        motionState={motionState}
      />
    );
  }

  const diff = gitStatus?.diff ?? { files: 0, additions: 0, deletions: 0 };
  const hasChanges = Boolean(gitStatus?.is_repo && (gitStatus.dirty_count > 0 || diff.files > 0));
  const branchLabel = gitStatus?.is_repo
    ? gitStatus.branch ?? "detached"
    : t("environment.notGitRepository");
  const prDisabled = Boolean(pullRequestDisabledReason && !gitStatus?.pr_url);

  function toggleMenu(menu: Exclude<EnvironmentPanelMenu, null>): void {
    onSetActiveMenu(activeMenu === menu ? null : menu);
  }

  return (
    <aside
      className={`environment-panel ${motionState}`}
      ref={panelRef}
      aria-label={t(planUpdate ? "environment.progressAndInfo" : "environment.info")}
      aria-hidden={motionState === "closing" ? true : undefined}
    >
      <div className="environment-panel-header floating">
        <div className="environment-panel-actions">
          <button className="icon-button" type="button" aria-label={t("environment.closeInfo")} onClick={onClose}>
            <X className="icon" />
          </button>
        </div>
      </div>

      {planUpdate ? <EnvironmentPlanSection planUpdate={planUpdate} /> : null}

      <div className="environment-panel-body">
        <button
          className="environment-row environment-change-row"
          type="button"
          disabled={!gitStatus?.is_repo}
          onClick={onOpenReview}
        >
          <FolderPlus className="icon-lg" />
          <strong>{t("environment.changes")}</strong>
          <span className="environment-row-meta">
            {gitStatus?.is_repo
              ? hasChanges
                ? (
                  <>
                    {t(diff.files === 1 ? "environment.fileCountOne" : "environment.fileCount", {
                      count: formatNumber(diff.files),
                    })}
                    <span className="environment-diff">
                      <span className="additions">+{formatNumber(diff.additions)}</span>
                      <span className="deletions">-{formatNumber(diff.deletions)}</span>
                    </span>
                  </>
                )
                : null
              : t("environment.notGit")}
          </span>
          {gitStatus?.is_repo ? <ChevronRight className="icon" /> : null}
        </button>

        <button
          className={`environment-row${activeMenu === "branch" ? " active" : ""}`}
          type="button"
          disabled={!gitStatus?.is_repo || running}
          onClick={() => toggleMenu("branch")}
        >
          <GitBranch className="icon-lg" />
          <strong>{branchLabel}</strong>
          {gitStatus?.is_repo ? <ChevronRight className="icon" /> : null}
        </button>

        <button
          className="environment-row"
          type="button"
          disabled={!hasChanges || running}
          onClick={onOpenCommit}
        >
          <CornerDownRight className="icon-lg" />
          <strong>{t("environment.commit")}</strong>
          <span>{hasChanges ? t("environment.commitChanges") : ""}</span>
        </button>

        <button
          className="environment-row"
          type="button"
          disabled={prDisabled || running}
          title={prDisabled ? pullRequestDisabledReason : undefined}
          onClick={onOpenPullRequest}
        >
          <Github className="icon-lg" />
          <strong>{t(gitStatus?.pr_url ? "environment.viewPR" : "environment.createPR")}</strong>
          <span>{gitStatus?.pr_url ? t("environment.existingPR") : prDisabled ? pullRequestDisabledReason : t("environment.pushAndCreatePR")}</span>
        </button>

        {subagentSessions && subagentSessions.length > 0 ? (
          <EnvironmentSubagents
            agents={subagentSessions}
            archiveConfirmID={archiveConfirmSubagentID}
            onSelect={onSelectSubagent}
            onTogglePinned={onToggleSubagentPinned}
            onArchive={onArchiveSubagent}
            onClearArchiveConfirm={onClearSubagentArchiveConfirm}
          />
        ) : null}
      </div>

      {activeMenu === "branch" && gitStatus?.is_repo ? (
        <EnvironmentBranchMenu
          gitStatus={gitStatus}
          onSelectBranch={onSelectBranch}
          onCreateBranch={onCreateBranch}
        />
      ) : null}
    </aside>
  );
}

/**
 * EnvironmentSubagents renders the active session's subagent sessions as
 * horizontal rows in the info panel. The section is only shown when at
 * least one subagent exists; otherwise the entire block disappears so
 * the panel stays uncluttered for plain sessions.
 *
 * The visual is deliberately a near-clone of the sidebar's `thread-row`
 * pattern: a single-row card with a status indicator, a title, and
 * hover-revealed Pin / Archive actions. The right column is also where
 * the nested-children counter appears for agents that have spawned their
 * own subagents.
 */
function EnvironmentSubagents({
  agents,
  archiveConfirmID,
  onSelect,
  onTogglePinned,
  onArchive,
  onClearArchiveConfirm,
}: {
  agents: SubagentRowSummary[];
  archiveConfirmID?: string;
  onSelect?: (agent: SubagentRowSummary) => void;
  onTogglePinned?: (agent: SubagentRowSummary) => void;
  onArchive?: (agent: SubagentRowSummary) => void;
  onClearArchiveConfirm?: (agentID: string) => void;
}): JSX.Element {
  const { t } = useI18n();
  // We treat the summary as a quasi-`Agent` for the existing sort helper
  // — it sorts by started_at then by agent_path which keeps the visible
  // order deterministic and matches the sidebar's historical behaviour.
  // The double cast is necessary because `SubagentRowSummary` is a strict
  // subset of `Agent` and `sortChildAgents` is generic over the full type.
  const ordered: SubagentRowSummary[] = sortChildAgents(
    agents as unknown as Agent[],
  );
  const hiddenCount = Math.max(
    0,
    ordered.length - MAX_VISIBLE_ENVIRONMENT_SUBAGENTS,
  );
  const visibleAgents = ordered.slice(-MAX_VISIBLE_ENVIRONMENT_SUBAGENTS);
  return (
    <div
      role="group"
      aria-label={t("environment.subtasks")}
      className="environment-subagent-group"
    >
      {visibleAgents.map((agent) => {
          const archiveConfirming = archiveConfirmID === agent.id;
          const nestedTotal = agent.nested_count ?? 0;
          const nestedRunning = agent.nested_running_count ?? 0;
          const nestedLabel =
            nestedTotal <= 0
              ? null
              : nestedRunning > 0
                ? `${nestedRunning}/${nestedTotal}`
                : `+${nestedTotal}`;
          return (
            <div
              key={agent.id}
              className={`subagent-row ${agentStatusToneToClass(agent.status)}${
                agent.archived ? " archived" : ""
              }${archiveConfirming ? " archive-confirm" : ""}`}
              onMouseLeave={() => onClearArchiveConfirm?.(agent.id)}
            >
              <button
                type="button"
                className="subagent-row-main"
                onClick={() => onSelect?.(agent)}
                aria-label={t("environment.openSubtask", {
                  name: agentLabelFromSummary(agent),
                })}
                title={agentTooltipFromSummary(agent)}
              >
                <span className="subagent-row-title">
                  <ParticipantChip
                    fallbackType=""
                    fallbackTaskName={agentLabelFromSummary(agent)}
                    size="sm"
                  />
                </span>
                <span className="subagent-row-status">
                  {agentStatusLabel(agent.status)}
                </span>
                {nestedLabel ? (
                  <span className="subagent-row-nested">{nestedLabel}</span>
                ) : null}
              </button>
              <div
                className="subagent-row-actions"
                aria-label={t("environment.subtaskActions")}
              >
                <button
                  className={`sidebar-row-icon-button subagent-row-action ${agent.pinned ? "active" : ""}`}
                  type="button"
                  aria-label={t(agent.pinned ? "sidebar.unpin" : "sidebar.pin")}
                  title={t(agent.pinned ? "sidebar.unpin" : "sidebar.pin")}
                  onClick={() => onTogglePinned?.(agent)}
                >
                  <Pin className="icon-sm" />
                </button>
                <button
                  className={`sidebar-row-icon-button subagent-row-action archive ${archiveConfirming ? "confirm" : ""}`}
                  type="button"
                  aria-label={t(
                    archiveConfirming
                      ? "environment.confirmArchive"
                      : "sidebar.archiveAction",
                  )}
                  title={t(
                    archiveConfirming
                      ? "environment.clickAgainArchive"
                      : "sidebar.archiveAction",
                  )}
                  onClick={() => onArchive?.(agent)}
                >
                  <Archive className="icon-sm" />
                </button>
              </div>
            </div>
          );
        })}
      {hiddenCount > 0 ? (
        <div className="environment-subagent-overflow-note" role="note">
          {t("environment.earlierSubtasksHidden", { count: hiddenCount })}
        </div>
      ) : null}
    </div>
  );
}

/** Keep the panel-specific visual class stable across agent states. */
function agentStatusToneToClass(status: string | undefined): string {
  switch (status) {
    case "running":
      return "running";
    case "completed":
      return "completed";
    case "failed":
      return "failed";
    case "cancelled":
      return "cancelled";
    default:
      return "pending";
  }
}

function agentLabelFromSummary(agent: SubagentRowSummary): string {
  return agentNameForSubagentID(agent.id).displayName;
}

function agentTooltipFromSummary(agent: SubagentRowSummary): string {
  const { displayName, secondaryName, source } = agentNameForSubagentID(agent.id);
  const pieces: string[] = [displayName];
  if (secondaryName) {
    pieces.push(secondaryName);
  }
  pieces.push(source, agentStatusLabel(agent.status));
  const type = agent.type?.trim();
  if (type && type !== displayName) {
    pieces.push(type);
  }
  const path = agent.agent_path?.trim();
  if (path) {
    pieces.push(path);
  }
  const task = agent.task_name?.trim() || agent.description?.trim();
  if (task) {
    pieces.push(task);
  }
  return pieces.join(" · ");
}

function EnvironmentPlanSection({ planUpdate }: { planUpdate: PlanUpdate }): JSX.Element {
  const { t } = useI18n();
  return (
    <section className="environment-plan-section" aria-label={t("environment.taskProgress")}>
      <div className="environment-plan-scroll">
        <ol className="environment-plan-list">
          {planUpdate.plan.map((item, index) => (
            <li className={`environment-plan-item ${item.status}`} key={`${index}-${item.step}`}>
              <span className="environment-plan-marker" aria-hidden="true">
                {item.status === "completed" ? <Check className="icon-xs" strokeWidth={3} /> : null}
              </span>
              <span>{item.step}</span>
            </li>
          ))}
        </ol>
      </div>
    </section>
  );
}

function EnvironmentBranchMenu({
  gitStatus,
  onSelectBranch,
  onCreateBranch
}: {
  gitStatus: GitStatusResult;
  onSelectBranch: (branch: string) => void;
  onCreateBranch: (branch: string) => Promise<void>;
}): JSX.Element {
  const { t } = useI18n();
  const [query, setQuery] = useState("");
  const [newBranch, setNewBranch] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const normalizedQuery = query.trim().toLocaleLowerCase();
  const branches = (gitStatus.branches ?? []).filter((branch) =>
    normalizedQuery ? branch.toLocaleLowerCase().includes(normalizedQuery) : true
  );

  async function submitNewBranch(event: ReactFormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    const branch = newBranch.trim();
    if (!branch || submitting) {
      return;
    }
    setSubmitting(true);
    setError("");
    try {
      await onCreateBranch(branch);
      setNewBranch("");
    } catch (createError) {
      setError(
        createError instanceof Error
          ? createError.message
          : t("environment.createBranchFailed"),
      );
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="environment-side-menu branch" role="menu">
      <label className="environment-search">
        <Search className="icon" />
        <input value={query} placeholder={t("environment.searchBranches")} onChange={(event) => setQuery(event.target.value)} />
      </label>
      {gitStatus.dirty_count > 0 ? (
        <div className="environment-side-note">{t("environment.dirtyBranchNote")}</div>
      ) : null}
      <div className="environment-branch-list">
        {branches.length === 0 ? <div className="environment-empty">{t("environment.noMatchingBranches")}</div> : null}
        {branches.map((branch) => {
          const selected = branch === gitStatus.branch;
          return (
            <button key={branch} role="menuitem" type="button" disabled={selected} onClick={() => onSelectBranch(branch)}>
              <GitBranch className="icon" />
              <span>{branch}</span>
              {selected ? <Check className="icon" /> : null}
            </button>
          );
        })}
      </div>
      <form className="environment-create-branch" onSubmit={(event) => void submitNewBranch(event)}>
        <input value={newBranch} placeholder={t("environment.newBranchName")} onChange={(event) => setNewBranch(event.target.value)} />
        <button type="submit" disabled={!newBranch.trim() || submitting}>
          <Plus className="icon" />
        </button>
      </form>
      {error ? <div className="environment-side-error">{error}</div> : null}
    </div>
  );
}

/**
 * Right-panel body that replaces the default environment panel when
 * `activeMenu === "file"` and a `rightPanelFilePath` is supplied. Reads the
 * file via `window.wuu.readWorkspaceFile` and renders loading / error /
 * binary / text-file states. The header close button falls back to
 * `onClose` when the caller does not provide a file-specific closer.
 */
function EnvironmentFilePreview({
  filePath,
  onClose,
  panelRef,
  motionState
}: {
  filePath: string;
  onClose: () => void;
  panelRef: RefObject<HTMLDivElement | null>;
  motionState: EnvironmentPanelMotionState;
}): JSX.Element {
  const { locale, t } = useI18n();
  const [file, setFile] = useState<WorkspaceFileReadResult | undefined>(undefined);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | undefined>(undefined);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(undefined);
    setFile(undefined);
    void window.wuu
      .readWorkspaceFile(filePath)
      .then((result) => {
        if (!cancelled) {
          setFile(result);
        }
      })
      .catch((e) => {
        if (!cancelled) {
          setError(desktopApiErrorMessage(e, t("environment.openFileFailed")));
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [filePath, locale]);

  let body: JSX.Element;
  if (loading) {
    body = (
      <div className="environment-panel-body">
        <div className="environment-row environment-file-row">
          <FileText aria-hidden="true" className="icon-lg" />
          <strong>{t("environment.opening")}</strong>
          <span>{filePath}</span>
        </div>
      </div>
    );
  } else if (error) {
    body = (
      <div className="environment-panel-body">
        <div className="environment-row environment-file-row">
          <AlertCircle aria-hidden="true" className="icon-lg" />
          <strong>{t("environment.openFailed")}</strong>
          <span>{error}</span>
          <span className="environment-row-meta">{filePath}</span>
        </div>
      </div>
    );
  } else if (!file) {
    body = (
      <div className="environment-panel-body">
        <div className="environment-row environment-file-row">
          <FileText aria-hidden="true" className="icon-lg" />
          <strong>{t("environment.noContent")}</strong>
          <span>{filePath}</span>
        </div>
      </div>
    );
  } else if (file.binary) {
    body = (
      <div className="environment-panel-body">
        <div className="environment-row environment-file-row">
          <FileX aria-hidden="true" className="icon-lg" />
          <strong>{t("environment.cannotPreview")}</strong>
          <span>{t("environment.binaryFile", { path: file.path })}</span>
        </div>
      </div>
    );
  } else {
    body = (
      <article className="workspace-file-preview">
        <header className="workspace-file-preview-header">
          <div>
            <strong>{file.path}</strong>
            <span>
              {formatBytes(file.size_bytes)}
              {file.truncated ? t("environment.truncated512") : ""}
            </span>
          </div>
          <button
            type="button"
            className="icon-button"
            onClick={onClose}
            aria-label={t("environment.backToInfo")}
            title={t("common.back")}
          >
            <ChevronRight aria-hidden="true" className="icon" />
          </button>
        </header>
        <div className="workspace-file-code-scroll">
          <pre className="workspace-file-code">
            <code>{file.text}</code>
          </pre>
        </div>
      </article>
    );
  }

  return (
    <aside
      ref={panelRef}
      className={`environment-panel ${motionState}`}
      aria-label={t("environment.filePreview")}
      aria-hidden={motionState === "closing" ? true : undefined}
    >
      <div className="environment-panel-header">
        <h2>{t("environment.file")}</h2>
        <div className="environment-panel-actions">
          <button
            className="icon-button"
            type="button"
            aria-label={t("environment.backToInfo")}
            title={t("common.back")}
            onClick={onClose}
          >
            <X aria-hidden="true" className="icon" />
          </button>
        </div>
      </div>
      {body}
    </aside>
  );
}
