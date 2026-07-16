import type {
  ComponentProps,
  Dispatch,
  KeyboardEvent as ReactKeyboardEvent,
  MutableRefObject,
  PointerEvent as ReactPointerEvent,
  RefObject,
  SetStateAction,
} from "react";
import {
  Bug,
  Grid3X3,
  Info,
  ListChecks,
  Terminal,
} from "lucide-react";
import type {
  Agent,
  InputFile,
  InputImage,
  RuntimeContext,
  Thread,
  ThreadItem,
} from "../shared/protocol";
import {
  chatReaderCountForThread,
  emptyComposerDraft,
  isGroupThread,
  queryTextsForThread,
  threadForTab,
  turnStreamStatusForThread,
  type AppState,
  type ComposerDraftState,
  type ConversationPaneID,
  type OpenSubthreadPanel,
} from "./AppState";
import {
  CONVERSATION_SPLIT_MAX_PERCENT,
  CONVERSATION_SPLIT_MIN_PERCENT,
  SIDEBAR_MAX_WIDTH,
  SIDEBAR_MIN_WIDTH,
  THREAD_PANEL_MAX_WIDTH,
  THREAD_PANEL_MIN_WIDTH,
} from "./AppLayoutState";
import { ChipGalleryPanel } from "./ChipGalleryPanel";
import { ConversationSubthreadPanel } from "./ConversationSubthreadPanel";
import { EnvironmentSideStack } from "./EnvironmentSideStack";
import { ParticipantProfilePanel } from "./ParticipantProfilePanel";
import type { ParticipantPanelState } from "./ParticipantState";
import {
  Composer,
} from "./ComposerView";
import type { PendingComposerMessagesByThread } from "./ComposerPendingMessages";
import { ConversationSplitPane } from "./ConversationSplitPane";
import type { HistoryMessageEditState } from "./ConversationHistoryActions";
import { SessionTabStrip } from "./SessionTabs";
import { SettingsView } from "./SettingsView";
import { RunDebugPanel } from "./RunDebugPanel";
import { SidePanelToggleIcon } from "./SidePanelToggleIcon";
import { ViewSwitchLoading } from "./LoadingViews";
import type { TurnFileDiffSelection } from "./TurnFileDiffTypes";
import { useI18n } from "./i18n";

type ComposerProps = ComponentProps<typeof Composer>;
type RunDebugPanelProps = ComponentProps<typeof RunDebugPanel>;
type EnvironmentSideStackProps = ComponentProps<typeof EnvironmentSideStack>;
type ParticipantProfilePanelProps = ComponentProps<
  typeof ParticipantProfilePanel
>;
type SettingsViewProps = ComponentProps<typeof SettingsView>;

const noop = () => {};

export type SubthreadComposerRendererProps = {
  draft: ComposerDraftState;
  setDraft: Dispatch<SetStateAction<ComposerDraftState>>;
  readOnly: boolean;
  initialized: ComposerProps["initialized"];
  projects: ComposerProps["projects"];
  activeContext: ComposerProps["activeContext"];
  activeProject: ComposerProps["activeProject"];
  codexModels: ComposerProps["codexModels"];
  codexRuntimeRef: ComposerProps["codexRuntimeRef"];
  runtimeMenuRef: ComposerProps["menuRef"];
  accessMenuRef: ComposerProps["accessMenuRef"];
  queryHistorySessionID?: string;
  participants: NonNullable<ComposerProps["participants"]>;
  onPasteAttachmentFiles: (files: File[]) => void;
  onRemoveFile: (id: string) => void;
  onRemoveImage: (id: string) => void;
  onSend: () => void;
};

export function SubthreadComposerRenderer({
  draft,
  setDraft,
  readOnly,
  initialized,
  projects,
  activeContext,
  activeProject,
  codexModels,
  codexRuntimeRef,
  runtimeMenuRef,
  accessMenuRef,
  queryHistorySessionID,
  participants,
  onPasteAttachmentFiles,
  onRemoveFile,
  onRemoveImage,
  onSend,
}: SubthreadComposerRendererProps): JSX.Element {
  return (
    <Composer
      variant="dock"
      hideRuntimeControls
      prompt={draft.prompt}
      setPrompt={(value) =>
        setDraft((current) => ({ ...current, prompt: value }))
      }
      files={draft.files}
      images={draft.images}
      queuedMessages={[]}
      guideMessages={[]}
      running={false}
      runtimeControlsDisabled
      tokensPerSecond={0}
      status=""
      readOnly={readOnly}
      initialized={initialized}
      projects={projects}
      activeContext={activeContext}
      activeProject={activeProject}
      codexModels={codexModels}
      codexRuntimeMenu={null}
      codexRuntimeRef={codexRuntimeRef}
      menuOpen={false}
      accessMenuOpen={false}
      branchMenuOpen={false}
      menuRef={runtimeMenuRef}
      accessMenuRef={accessMenuRef}
      projectFilter=""
      setProjectFilter={noop}
      onToggleMenu={noop}
      onToggleAccessMenu={noop}
      onToggleBranchMenu={noop}
      onToggleCodexRuntimeMenu={noop}
      onSelectRuntimeModel={noop}
      onSelectRuntimeEffort={noop}
      onSelectPermissionMode={noop}
      onOpenSettings={noop}
      onOpenMemorySettings={noop}
      onOpenSkillsCatalog={noop}
      onSelectProject={noop}
      onSelectNoProject={noop}
      onSelectGitBranch={noop}
      onCreateProject={noop}
      onOpenProject={noop}
      onStartNewThread={noop}
      onOpenWorkspaceTool={noop}
      onOpenInstructions={noop}
      onPasteAttachmentFiles={onPasteAttachmentFiles}
      onRemoveFile={onRemoveFile}
      onRemoveImage={onRemoveImage}
      onRemoveQueuedMessage={noop}
      onRemoveGuideMessage={noop}
      onGuideQueuedMessage={noop}
      onEditQueuedMessage={noop}
      onEditGuideMessage={noop}
      onSend={onSend}
      onInterrupt={noop}
      queryHistorySessionID={queryHistorySessionID}
      queryHistory={[]}
      participants={participants}
    />
  );
}

export type ConversationSplitPaneRendererProps = {
  state: AppState;
  thread: Thread;
  pane: ConversationPaneID;
  splitComposerDrafts: Record<ConversationPaneID, ComposerDraftState>;
  splitPaneRefs: MutableRefObject<Record<ConversationPaneID, HTMLElement | null>>;
  viewSwitchPending: boolean;
  historyMessageEdit?: HistoryMessageEditState;
  onActivatePane: (pane: ConversationPaneID) => void;
  onClosePane: (pane: ConversationPaneID) => void;
  onConversationScroll: (node: HTMLElement) => void;
  onSetPrompt: (pane: ConversationPaneID, value: string) => void;
  onPasteAttachmentFiles: (
    pane: ConversationPaneID,
    files: File[],
  ) => void;
  onRemoveFile: (pane: ConversationPaneID, id: string) => void;
  onRemoveImage: (pane: ConversationPaneID, id: string) => void;
  onSend: (pane: ConversationPaneID) => void;
  onInterrupt: (pane: ConversationPaneID) => void;
  onForkMessage: (thread: Thread, turnID: string, itemID: string) => void;
  onOpenFile?: (thread: Thread, path: string) => void;
  onOpenAgent: (agent: Agent) => void;
  canEditThreadMessage: (thread: Thread) => boolean;
  onEditMessage: (
    thread: Thread,
    turnID: string,
    item: ThreadItem,
    pane: ConversationPaneID,
  ) => void;
  onCancelEditMessage: () => void;
  onSubmitEditMessage: (
    thread: Thread,
    turnID: string,
    item: ThreadItem,
    text: string,
    images: InputImage[],
    files: InputFile[],
    pane: ConversationPaneID,
  ) => void;
  onStreamFrame: () => void;
  onOpenFileDiff: (threadID: string, selection: TurnFileDiffSelection) => void;
};

export function ConversationSplitPaneRenderer({
  state,
  thread,
  pane,
  splitComposerDrafts,
  splitPaneRefs,
  viewSwitchPending,
  historyMessageEdit,
  onActivatePane,
  onClosePane,
  onConversationScroll,
  onSetPrompt,
  onPasteAttachmentFiles,
  onRemoveFile,
  onRemoveImage,
  onSend,
  onInterrupt,
  onForkMessage,
  onOpenFile,
  onOpenAgent,
  canEditThreadMessage,
  onEditMessage,
  onCancelEditMessage,
  onSubmitEditMessage,
  onStreamFrame,
  onOpenFileDiff,
}: ConversationSplitPaneRendererProps): JSX.Element {
  return (
    <ConversationSplitPane
      pane={pane}
      thread={thread}
      threads={state.threads}
      active={state.activePane === pane}
      activeContextCwd={state.activeContext?.cwd}
      appStatus={state.status}
      streamStatus={turnStreamStatusForThread(state, thread)}
      draft={splitComposerDrafts[pane] ?? emptyComposerDraft()}
      viewSwitchPending={viewSwitchPending}
      queryHistory={queryTextsForThread(thread)}
      editingMessage={
        historyMessageEdit?.threadID === thread.id
          ? historyMessageEdit
          : undefined
      }
      onActivate={() => onActivatePane(pane)}
      onClose={() => onClosePane(pane)}
      onBodyRef={(node) => {
        splitPaneRefs.current[pane] = node;
      }}
      onScroll={onConversationScroll}
      onSetPrompt={(value) => onSetPrompt(pane, value)}
      onPasteAttachmentFiles={(files) => onPasteAttachmentFiles(pane, files)}
      onRemoveFile={(id) => onRemoveFile(pane, id)}
      onRemoveImage={(id) => onRemoveImage(pane, id)}
      onSend={() => onSend(pane)}
      onInterrupt={() => onInterrupt(pane)}
      onForkMessage={(turnID, itemID) => onForkMessage(thread, turnID, itemID)}
      onOpenFile={(path) => onOpenFile?.(thread, path)}
      onOpenAgent={(agentID) => {
        const agent = thread.child_agents?.find(
          (candidate) => candidate.id === agentID,
        );
        if (agent) {
          onOpenAgent(agent);
        }
      }}
      onEditMessage={
        canEditThreadMessage(thread)
          ? (turnID, item) => onEditMessage(thread, turnID, item, pane)
          : undefined
      }
      onCancelEditMessage={onCancelEditMessage}
      onSubmitEditMessage={(turnID, item, text, images, files) =>
        onSubmitEditMessage(thread, turnID, item, text, images, files, pane)
      }
      onStreamFrame={onStreamFrame}
      onOpenFileDiff={(selection) => onOpenFileDiff(thread.id, selection)}
    />
  );
}

export type ConversationSplitLayoutRendererProps = Omit<
  ConversationSplitPaneRendererProps,
  "thread" | "pane"
> & {
  primaryThread: Thread;
  secondaryThread: Thread;
  splitLeftPercent: number;
  onSplitResizeStart: (event: ReactPointerEvent<HTMLDivElement>) => void;
  onSplitSeparatorDoubleClick: () => void;
  onSplitSeparatorKey: (event: ReactKeyboardEvent<HTMLDivElement>) => void;
};

export function ConversationSplitLayoutRenderer({
  primaryThread,
  secondaryThread,
  splitLeftPercent,
  onSplitResizeStart,
  onSplitSeparatorDoubleClick,
  onSplitSeparatorKey,
  ...paneProps
}: ConversationSplitLayoutRendererProps): JSX.Element {
  const { t } = useI18n();
  return (
    <div className="conversation-split">
      <ConversationSplitPaneRenderer
        {...paneProps}
        thread={primaryThread}
        pane="primary"
      />
      <div
        className="conversation-split-resizer"
        role="separator"
        aria-orientation="vertical"
        aria-label={t("shell.resizeSplit")}
        aria-valuemin={CONVERSATION_SPLIT_MIN_PERCENT}
        aria-valuemax={CONVERSATION_SPLIT_MAX_PERCENT}
        aria-valuenow={Math.round(splitLeftPercent)}
        tabIndex={0}
        onPointerDown={onSplitResizeStart}
        onDoubleClick={onSplitSeparatorDoubleClick}
        onKeyDown={onSplitSeparatorKey}
      />
      <ConversationSplitPaneRenderer
        {...paneProps}
        thread={secondaryThread}
        pane="secondary"
      />
    </div>
  );
}

export type ConversationTitleContentProps = {
  state: AppState;
  sessionTabsVisible: boolean;
  // Live busy set (computeBusyParticipantIDs) forwarded to the tab strip so
  // group-thread tabs spin off the member's actual turn lifecycle instead of
  // the server's stale members[].busy snapshot.
  busyParticipantIDs?: ReadonlySet<string>;
  pendingSwitchThreadID?: string;
  pendingComposerMessagesByThread: PendingComposerMessagesByThread;
  activeTitle: string;
  onSelectSessionTab: (tabID: string) => void;
  onCloseSessionTab: (tabID: string) => void;
  onCloseSessionTabs: (tabIDs: string[]) => void;
  onPopOutSessionTab: (tabID: string) => void;
  onStartNewThread: () => void;
  onReorderSessionTabs: (activeID: string, overID: string) => void;
};

export function ConversationTitleContent({
  state,
  sessionTabsVisible,
  busyParticipantIDs,
  pendingSwitchThreadID,
  pendingComposerMessagesByThread,
  activeTitle,
  onSelectSessionTab,
  onCloseSessionTab,
  onCloseSessionTabs,
  onPopOutSessionTab,
  onStartNewThread,
  onReorderSessionTabs,
}: ConversationTitleContentProps): JSX.Element {
  if (sessionTabsVisible) {
    return (
      <SessionTabStrip
        state={state}
        busyParticipantIDs={busyParticipantIDs}
        pendingSwitchThreadID={pendingSwitchThreadID}
        pendingComposerMessagesByThread={pendingComposerMessagesByThread}
        canStartNewThread={Boolean(state.activeContext)}
        onSelect={onSelectSessionTab}
        onClose={onCloseSessionTab}
        onCloseTabs={onCloseSessionTabs}
        onPopOut={onPopOutSessionTab}
        onNewThread={onStartNewThread}
        onReorder={onReorderSessionTabs}
      />
    );
  }
  return (
    <h1>{activeTitle}</h1>
  );
}

export type ConversationTitleActionsProps = {
  state: AppState;
  debugControlsVisible: boolean;
  enableLaunchPreview: boolean;
  previewingLaunch: boolean;
  onPinLaunchPreview: () => void;
  enablePlanPanelDebug: boolean;
  onSeedPlanPanelDebug: () => void;
  conversationGridVisible: boolean;
  onToggleConversationGrid: () => void;
  enableRunDebugPanel: boolean;
  runDebugRef: RefObject<HTMLDivElement | null>;
  runDebugOpen: boolean;
  onToggleRunDebug: () => void;
  runDebugPhase: RunDebugPanelProps["phase"];
  runDebugEvents: RunDebugPanelProps["events"];
  queuedMessages: RunDebugPanelProps["queuedMessages"];
  guideMessages: RunDebugPanelProps["guideMessages"];
  composerImages: RunDebugPanelProps["composerImages"];
  composerFiles: RunDebugPanelProps["composerFiles"];
  runDebugCopied: RunDebugPanelProps["copied"];
  onCopyRunDebug: () => void;
  onCloseRunDebug: () => void;
  chipGalleryOpen: boolean;
  onCloseChipGallery: () => void;
  poppedOutMode: boolean;
  activeThread?: Thread;
  onOpenTaskBoard: (thread: Thread) => void;
  environmentToggleRef: RefObject<HTMLButtonElement | null>;
  environmentPanelVisible: boolean;
  activeThreadIsGroup: boolean;
  onToggleEnvironmentPanel: () => void;
  rightPanelOpen: boolean;
  onToggleRightPanel: () => void;
};

export function ConversationTitleActions({
  state,
  debugControlsVisible,
  enableLaunchPreview,
  previewingLaunch,
  onPinLaunchPreview,
  enablePlanPanelDebug,
  onSeedPlanPanelDebug,
  conversationGridVisible,
  onToggleConversationGrid,
  enableRunDebugPanel,
  runDebugRef,
  runDebugOpen,
  onToggleRunDebug,
  runDebugPhase,
  runDebugEvents,
  queuedMessages,
  guideMessages,
  composerImages,
  composerFiles,
  runDebugCopied,
  onCopyRunDebug,
  onCloseRunDebug,
  chipGalleryOpen,
  onCloseChipGallery,
  poppedOutMode,
  activeThread,
  onOpenTaskBoard,
  environmentToggleRef,
  environmentPanelVisible,
  activeThreadIsGroup,
  onToggleEnvironmentPanel,
  rightPanelOpen,
  onToggleRightPanel,
}: ConversationTitleActionsProps): JSX.Element {
  const { t } = useI18n();
  const showTaskBoardAction =
    activeThreadIsGroup ||
    state.sessionTabs.some((tab) => {
      if (tab.kind === "board") {
        return true;
      }
      return (
        tab.kind === "thread" &&
        isGroupThread(threadForTab(state, tab.threadID) ?? {})
      );
    });
  const taskBoardAvailable = activeThreadIsGroup && Boolean(activeThread);

  return (
    <div className="title-actions">
      {debugControlsVisible && enableLaunchPreview ? (
        <button
          className="launch-preview-button"
          type="button"
          disabled={previewingLaunch}
          onClick={onPinLaunchPreview}
        >
          <Terminal className="icon" />
          <span>{t("shell.launchPreview")}</span>
        </button>
      ) : null}
      {debugControlsVisible && enablePlanPanelDebug ? (
        <button
          className="launch-preview-button plan-panel-debug-button"
          type="button"
          disabled={!state.activeContext || !state.initialized}
          onClick={onSeedPlanPanelDebug}
        >
          <ListChecks className="icon" />
          <span>{t("shell.planPanel")}</span>
        </button>
      ) : null}
      {debugControlsVisible ? (
        <button
          className={`launch-preview-button conversation-grid-button${conversationGridVisible ? " active" : ""}`}
          type="button"
          aria-label={t(
            conversationGridVisible ? "shell.hideGrid" : "shell.showGrid",
          )}
          aria-pressed={conversationGridVisible}
          title={t("shell.gridShortcut")}
          onClick={onToggleConversationGrid}
        >
          <Grid3X3 className="icon" />
          <span>{t("shell.grid")}</span>
        </button>
      ) : null}
      {debugControlsVisible && enableRunDebugPanel ? (
        <div className="run-debug-anchor" ref={runDebugRef}>
          <button
            className={`launch-preview-button run-debug-button${runDebugOpen ? " active" : ""}`}
            type="button"
            aria-label={t(
              runDebugOpen ? "shell.hideDebug" : "shell.showDebug",
            )}
            aria-expanded={runDebugOpen}
            onClick={onToggleRunDebug}
          >
            <Bug className="icon" />
            <span>{t("shell.debug")}</span>
          </button>
          {runDebugOpen ? (
            <RunDebugPanel
              state={state}
              phase={runDebugPhase}
              events={runDebugEvents}
              queuedMessages={queuedMessages}
              guideMessages={guideMessages}
              composerImages={composerImages}
              composerFiles={composerFiles}
              copied={runDebugCopied}
              onCopy={onCopyRunDebug}
              onClose={onCloseRunDebug}
            />
          ) : null}
        </div>
      ) : null}
      <ChipGalleryPanel open={chipGalleryOpen} onClose={onCloseChipGallery} />
      {poppedOutMode ? null : (
        <>
          {showTaskBoardAction ? (
            <button
              className="icon-button task-board-button"
              type="button"
              aria-label={t(
                taskBoardAvailable ? "shell.openTaskBoard" : "shell.noTaskBoard",
              )}
              title={t(
                taskBoardAvailable ? "shell.taskBoard" : "shell.noTaskBoard",
              )}
              disabled={!taskBoardAvailable}
              onClick={() => {
                if (activeThreadIsGroup && activeThread) {
                  onOpenTaskBoard(activeThread);
                }
              }}
            >
              <ListChecks className="icon-lg" size={18} viewBox="2 2 20 20" />
            </button>
          ) : null}
          <button
            ref={environmentToggleRef}
            className={`icon-button environment-toggle-button${environmentPanelVisible ? " active" : ""}`}
            type="button"
            aria-label={
              environmentPanelVisible
                ? activeThreadIsGroup
                  ? t("shell.hideGroupInfo")
                  : t("shell.hideEnvironmentInfo")
                : activeThreadIsGroup
                  ? t("shell.showGroupInfo")
                  : t("shell.showEnvironmentInfo")
            }
            aria-pressed={environmentPanelVisible}
            onClick={onToggleEnvironmentPanel}
          >
            <Info className="icon-lg" size={18} />
          </button>
          <button
            className="icon-button side-panel-toggle-button"
            type="button"
            aria-label={t(
              rightPanelOpen ? "shell.closeRightSidebar" : "shell.openRightSidebar",
            )}
            aria-pressed={rightPanelOpen}
            onClick={onToggleRightPanel}
          >
            <SidePanelToggleIcon side="right" open={rightPanelOpen} />
          </button>
        </>
      )}
    </div>
  );
}

export type ConversationSidePanelsProps = {
  state: AppState;
  environmentPanelVisible: boolean;
  environmentPanelMounted: boolean;
  environmentPanelRef: EnvironmentSideStackProps["panelRef"];
  environmentPanelClosing: boolean;
  environmentPanelMotionState: EnvironmentSideStackProps["motionState"];
  activePlanUpdate: EnvironmentSideStackProps["planUpdate"];
  environmentPanelMenu: EnvironmentSideStackProps["activeMenu"];
  anyThreadIsRunning: boolean;
  pullRequestDisabledReason: string;
  onSetEnvironmentPanelMenu: EnvironmentSideStackProps["onSetActiveMenu"];
  onCloseEnvironmentPanel: () => void;
  onSelectBranch: (branch: string) => void;
  onCreateBranch: EnvironmentSideStackProps["onCreateBranch"];
  onOpenReview: () => void;
  onOpenCommit: () => void;
  onOpenPullRequest: () => void;
  rightPanelFilePath?: string;
  onCloseFilePreview: () => void;
  activeThread?: Thread;
  archiveConfirmSubagentID?: string;
  onSelectChildAgent: (agent: Agent) => void;
  onToggleSubagentPinned: (agent: Agent) => void;
  onArchiveSubagent: (agent: Agent) => void;
  onClearSubagentArchiveConfirm: (id: string) => void;
  participants: NonNullable<EnvironmentSideStackProps["participants"]>;
  onAddThreadMember: (threadID: string, participantID: string) => void;
  onRemoveThreadMember: (threadID: string, participantID: string) => void;
  openSubthreadPanel?: OpenSubthreadPanel;
  onCloseSubthreadPanel: () => void;
  onResolveSubthread: ComponentProps<
    typeof ConversationSubthreadPanel
  >["onResolve"];
  onEscalateSubthread: ComponentProps<
    typeof ConversationSubthreadPanel
  >["onEscalate"];
  onReactSubthread: ComponentProps<
    typeof ConversationSubthreadPanel
  >["onReact"];
  poppedOutMode: boolean;
  activeContext?: RuntimeContext;
  onPopOutSubthread: (
    threadID: string,
    subthreadID: string,
    context: RuntimeContext,
  ) => void;
  subthreadComposer: Omit<
    SubthreadComposerRendererProps,
    "readOnly" | "queryHistorySessionID"
  >;
  resolveParticipantName: (id: string) => string;
  busyParticipantIDs: ComponentProps<
    typeof ConversationSubthreadPanel
  >["busyParticipantIDs"];
  chatReaderCount: number;
  debugControlsVisible: boolean;
  clampedThreadPanelWidth: number;
  onThreadPanelResizeStart: (event: ReactPointerEvent<HTMLDivElement>) => void;
  onThreadPanelReset: () => void;
  onThreadPanelSeparatorKey: (
    event: ReactKeyboardEvent<HTMLDivElement>,
  ) => void;
  participantPanel?: ParticipantPanelState;
  onCloseParticipantPanel: () => void;
  onSaveParticipant: ParticipantProfilePanelProps["onSave"];
  onFeedbackParticipant: ParticipantProfilePanelProps["onFeedback"];
  onOpenMemoryPanel: ParticipantProfilePanelProps["onOpenMemoryPanel"];
  onRetireParticipant: ParticipantProfilePanelProps["onRetire"];
  viewContextSwitchPending: boolean;
};

export function ConversationSidePanels({
  state,
  environmentPanelVisible,
  environmentPanelMounted,
  environmentPanelRef,
  environmentPanelClosing,
  environmentPanelMotionState,
  activePlanUpdate,
  environmentPanelMenu,
  anyThreadIsRunning,
  pullRequestDisabledReason,
  onSetEnvironmentPanelMenu,
  onCloseEnvironmentPanel,
  onSelectBranch,
  onCreateBranch,
  onOpenReview,
  onOpenCommit,
  onOpenPullRequest,
  rightPanelFilePath,
  onCloseFilePreview,
  activeThread,
  archiveConfirmSubagentID,
  onSelectChildAgent,
  onToggleSubagentPinned,
  onArchiveSubagent,
  onClearSubagentArchiveConfirm,
  participants,
  onAddThreadMember,
  onRemoveThreadMember,
  openSubthreadPanel,
  onCloseSubthreadPanel,
  onResolveSubthread,
  onEscalateSubthread,
  onReactSubthread,
  poppedOutMode,
  activeContext,
  onPopOutSubthread,
  subthreadComposer,
  resolveParticipantName,
  busyParticipantIDs,
  chatReaderCount,
  debugControlsVisible,
  clampedThreadPanelWidth,
  onThreadPanelResizeStart,
  onThreadPanelReset,
  onThreadPanelSeparatorKey,
  participantPanel,
  onCloseParticipantPanel,
  onSaveParticipant,
  onFeedbackParticipant,
  onOpenMemoryPanel,
  onRetireParticipant,
  viewContextSwitchPending,
}: ConversationSidePanelsProps): JSX.Element {
  const { t } = useI18n();
  return (
    <>
      <EnvironmentSideStack
        visible={environmentPanelVisible}
        mounted={environmentPanelMounted}
        state={state}
        panelRef={environmentPanelRef}
        closing={environmentPanelClosing}
        motionState={environmentPanelMotionState}
        planUpdate={activePlanUpdate}
        activeMenu={environmentPanelMenu}
        running={anyThreadIsRunning}
        pullRequestDisabledReason={pullRequestDisabledReason}
        onSetActiveMenu={onSetEnvironmentPanelMenu}
        onClose={onCloseEnvironmentPanel}
        onSelectBranch={onSelectBranch}
        onCreateBranch={onCreateBranch}
        onOpenReview={onOpenReview}
        onOpenCommit={onOpenCommit}
        onOpenPullRequest={onOpenPullRequest}
        rightPanelFilePath={rightPanelFilePath}
        onCloseFilePreview={onCloseFilePreview}
        subagentSessions={activeThread?.child_agents}
        archiveConfirmSubagentID={archiveConfirmSubagentID}
        onSelectSubagent={(agent) => onSelectChildAgent(agent as Agent)}
        onToggleSubagentPinned={(agent) =>
          onToggleSubagentPinned(agent as Agent)
        }
        onArchiveSubagent={(agent) => onArchiveSubagent(agent as Agent)}
        onClearSubagentArchiveConfirm={onClearSubagentArchiveConfirm}
        participants={participants}
        onAddThreadMember={onAddThreadMember}
        onRemoveThreadMember={onRemoveThreadMember}
      />

      {openSubthreadPanel ? (
        <ConversationSubthreadPanel
          threadID={openSubthreadPanel.threadID}
          cwd={activeThread?.cwd ?? activeContext?.cwd}
          subthread={openSubthreadPanel.subthread}
          loading={openSubthreadPanel.loading}
          error={openSubthreadPanel.error}
          onClose={onCloseSubthreadPanel}
          onResolve={onResolveSubthread}
          onEscalate={onEscalateSubthread}
          onReact={onReactSubthread}
          sourceItem={
            activeThread?.id === openSubthreadPanel.threadID
              ? activeThread.turns
                  .flatMap((turn) => turn.items)
                  .find(
                    (item) =>
                      item.id === openSubthreadPanel.subthread?.anchor_item_id ||
                      (Boolean(openSubthreadPanel.subthread?.parent_seq) &&
                        item.seq === openSubthreadPanel.subthread?.parent_seq),
                  )
              : undefined
          }
          onPopOut={
            !poppedOutMode && openSubthreadPanel.subthread && activeContext
              ? () =>
                  onPopOutSubthread(
                    openSubthreadPanel.threadID,
                    openSubthreadPanel.subthread!.id,
                    activeContext,
                  )
              : undefined
          }
          composer={
            <SubthreadComposerRenderer
              {...subthreadComposer}
              readOnly={openSubthreadPanel.subthread?.status === "resolved"}
              queryHistorySessionID={openSubthreadPanel.subthread?.id}
            />
          }
          resolveParticipantName={resolveParticipantName}
          busyParticipantIDs={busyParticipantIDs}
          readerCount={
            // A weak-isolation reply subthread routes only to its
            // participant subset, so that subset is the ring denominator.
            // Otherwise fall back to the parent thread's own reader count
            // (group members / DM peer), then the full roster.
            openSubthreadPanel.subthread?.participants?.length ||
            chatReaderCountForThread(
              activeThread?.id === openSubthreadPanel.threadID
                ? activeThread
                : undefined,
              chatReaderCount,
            )
          }
          showTechnicalTrace={debugControlsVisible}
        />
      ) : null}
      {openSubthreadPanel ? (
        <div
          className="thread-panel-resizer"
          role="separator"
          aria-label={t("shell.resizeThreadPanel")}
          aria-orientation="vertical"
          aria-valuemin={THREAD_PANEL_MIN_WIDTH}
          aria-valuemax={THREAD_PANEL_MAX_WIDTH}
          aria-valuenow={clampedThreadPanelWidth}
          tabIndex={0}
          onPointerDown={onThreadPanelResizeStart}
          onDoubleClick={onThreadPanelReset}
          onKeyDown={onThreadPanelSeparatorKey}
        />
      ) : null}

      {participantPanel ? (
        <ParticipantProfilePanel
          mode={participantPanel.mode}
          participant={participantPanel.participant}
          initialName={
            participantPanel.mode === "new"
              ? participantPanel.initialName
              : undefined
          }
          providers={state.initialized?.providers}
          loading={participantPanel.loading}
          error={participantPanel.error}
          saving={participantPanel.saving}
          feedbackSubmitting={participantPanel.feedbackSubmitting}
          feedbackReply={participantPanel.feedbackReply}
          retiring={participantPanel.retiring}
          archived={participantPanel.archived}
          onClose={onCloseParticipantPanel}
          onSave={onSaveParticipant}
          onFeedback={onFeedbackParticipant}
          onOpenMemoryPanel={onOpenMemoryPanel}
          onRetire={onRetireParticipant}
          forkedFromName={
            participantPanel.participant?.forked_from_id
              ? resolveParticipantName(participantPanel.participant.forked_from_id)
              : undefined
          }
        />
      ) : null}

      {viewContextSwitchPending ? <ViewSwitchLoading /> : null}
    </>
  );
}

export type SettingsShellRendererProps = Omit<
  SettingsViewProps,
  "sidebarMinWidth" | "sidebarMaxWidth"
>;

export function SettingsShellRenderer(
  props: SettingsShellRendererProps,
): JSX.Element {
  return (
    <SettingsView
      {...props}
      sidebarMinWidth={SIDEBAR_MIN_WIDTH}
      sidebarMaxWidth={SIDEBAR_MAX_WIDTH}
    />
  );
}
