/// <reference path="../shared/jsx-compat.d.ts" />

import {
  ChevronRight,
  Folder,
  FolderX,
  GitBranch,
  Image as ImageIcon,
  Laptop,
  MoreHorizontal,
  Pencil,
  Pin,
  Plus,
  Send,
  Trash2,
  Wrench,
  X,
} from "lucide-react";
import {
  type CSSProperties,
  type RefObject,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import type {
  Agent,
  ConversationSubthread,
  SubthreadUpdatedNotification,
  DesktopProject,
  GitStatusResult,
  InitializeResult,
  InputFile,
  InputImage,
  PlanUpdate,
  PopOutInitResult,
  PermissionSummary,
  RuntimeContext,
  ServerEvent,
  Thread,
  ThreadItem,
  Turn,
} from "../shared/protocol";
import {
  awaitComposerImages,
  createComposerMessage,
  createOptimisticCompactTurn,
  createOptimisticTurn,
  dropOptimisticTurn,
  earlierStartedAt,
  failOptimisticCompactTurn,
  inputFilesFromComposer,
  inputImagesFromComposer,
  replaceOptimisticTurn,
  type ComposerFile,
  type ComposerImage,
  type QueuedComposerMessage,
} from "./ComposerMessages";
import {
  greetingFor,
  resolveGreetingContext,
  useCurrentHour,
  type GreetingContext,
} from "./greetings";
import {
  Composer,
  FloatingMenuPortal,
  isInsideFloatingMenu,
  type CodexModelLoadState,
  type CodexRuntimeMenu,
  type ComposerVariant,
  type FloatingMenuOwner,
  type PermissionMode,
} from "./ComposerView";
import {
  QueryHistoryPopover,
  type QueryHistoryEntry,
} from "./QueryHistoryPopover";
import { QueryHistoryRail } from "./QueryHistoryRail";
import { ConversationSearchOverlay } from "./ConversationSearchOverlay";
import { useConversationScrollState } from "./ConversationScrollState";
import { useConversationSearch } from "./ConversationSearchState";
import { useConversationSubthreadState } from "./ConversationSubthreadState";
import { CodexPetLayer } from "./CodexPetLayer";
import { useThreadMarkList } from "./useThreadMarks";
import { useParticipantState } from "./ParticipantState";
import { ConversationForkDialog } from "./ConversationForkDialog";
import type { TurnFileDiffSelection } from "./TurnFileDiffTypes";
import {
  AppSidebar,
} from "./AppSidebar";
import {
  type EnvironmentPanelMenu,
  type EnvironmentPanelMotionState,
} from "./EnvironmentPanel";
import { createEnvironmentActions } from "./EnvironmentActions";
import {
  activePlanUpdateForThread,
  activeSessionTab,
  activeThreadForState,
  activeThreadIDForState,
  latestContextUsageForThread,
  activeTurnIDForThread,
  activeTurnTokenSpeedSnapshot,
  applyLoadedRuntimeWithDraftCarry,
  appendStreamingTokenSample,
  bindActiveSessionTabToThread,
  computeBusyParticipantIDs,
  chatFocusValueForThread,
  cloneSessionTabDraft,
  composerDraftHasContent,
  composerSubmissionDetail,
  conversationPaneThreadsByID,
  createDraftSessionTab,
  createFileSessionTab,
  createThreadSessionTab,
  emptyComposerDraft,
  ensureSessionTab,
  fileNameFromPath,
  findDMThread,
  focusWorkspaceSendValue,
  handleStreamingNotification,
  initialSplitComposerDrafts,
  initialState,
  isAnyThreadRunning,
  isDirectChildAgent,
  isDMThread,
  groupThreadSummaries,
  isGroupThread,
  isStateActiveThreadRunning,
  isThread,
  isThreadRunning,
  isThreadUnread,
  latestPlanUpdateForThread,
  mergeListedThreads,
  markThreadTurnsViewed,
  mentionedParticipantIDsFromText,
  notificationTargetsActiveThread,
  persistActiveSessionTabDraft,
  pinnedThreadSummaries,
  queryTextForUserItem,
  SCRATCH_PSEUDO_PROJECT_ID,
  scratchThreadSummaries,
  queryTextsForThread,
  reduceServerEvent,
  requireThread,
  runtimeContextKey,
  sameRuntimeContext,
  serverEventShouldRefreshGit,
  serverEventTargetsActiveContext,
  serverEventTargetsGlobalThread,
  shouldResetToNoProjectForNewThread,
  sessionTabForLoadedRuntime,
  sessionTabLabel,
  setThreadForPane,
  sortThreads,
  summarizeThreadsForSidebar,
  threadForTab,
  threadForPane,
  threadFromRecord,
  threadIDFromParams,
  threadSessionTabID,
  turnFromRecord,
  turnStreamStatusForThread,
  updateThreadByID,
  upsertThread,
  upsertTurn,
  withLoadedRuntimeSessionTab,
  workspacePanelContext,
  type AppState,
  type ComposerDraftState,
  type ConversationPaneID,
  type SessionTab,
  type ThreadSummary,
  type TurnStreamStatus,
} from "./AppState";
import {
  RIGHT_PANEL_MOTION_MS,
  SIDEBAR_MAX_WIDTH,
  SIDEBAR_MIN_WIDTH,
  SIDEBAR_MOTION_MS,
  WORKSPACE_RIGHT_PANEL_MAX_WIDTH,
  WORKSPACE_RIGHT_PANEL_MIN_WIDTH,
  useAppLayoutState,
} from "./AppLayoutState";
import { CommitChangesDialog, PullRequestDialog } from "./GitDialogs";
import type { ContextCompositionEntry } from "./ContextCompositionCard";
import type { InstructionFilesEntry } from "./InstructionFilesCard";
import { DesignTokensPanel } from "./DesignTokensPanel";
import { useAppDebugState } from "./AppDebugState";
import {
  EmptyStateHints,
  type EmptyStateHintAction,
} from "./EmptyStateHints";
import {
  EmptyConversationHome,
  RuntimeLoading,
} from "./LoadingViews";
import { pullRequestUnavailableReason } from "./RuntimeHelpers";
import type { SettingsPage } from "./SettingsView";
import type { ComposerGoalSummary } from "../shared/protocol";
import { useSettingsRuntimeState } from "./SettingsRuntimeState";
import { SidePanelToggleIcon } from "./SidePanelToggleIcon";
import { JumpToLatestPill } from "./JumpToLatestPill";
import { SkillsCatalog } from "./SkillsCatalog";
import { TaskBoardView } from "./TaskBoardView";
import { StreamingMarkdown } from "./StreamingMarkdown";
import { runDebugPhaseForState } from "./RunDebugPanel";
import { useThreadBrowserPreview } from "./ThreadBrowserPreview";
import { threadDisplayTitle } from "./ThreadTitles";
import {
  isRecord,
  numberValue,
} from "./ToolActivity";
import {
  mergeTurnItemsInOrder,
  orderedTurnItems,
  upsertTurnItemInOrder,
} from "./TurnOrdering";
import { sortChildAgents } from "./ThreadAgents";
import {
  rawErrorMessage,
  statusMessageForError,
  statusToneClass,
  type UserFacingErrorAction,
} from "./UserFacingErrors";
import {
  latestAgentMessageItemID,
  scrollToUserMessage,
} from "./TurnView";
import { ConversationTurnRail } from "./ConversationTurnRail";
import {
  WorkspaceMainPanel,
  WorkspaceRightPanel,
  workspaceModeTitle,
  type WorkspacePanelView,
} from "./WorkspacePanels";
import type { WorkspaceFileDirtyState } from "./WorkspaceFiles";
import { useWorkspaceToolState } from "./WorkspaceToolState";
import type { WorkspaceViewTab } from "./WorkspaceViewTabs";
import { desktopApiErrorMessage } from "./WorkspaceReviewHelpers";
import { ImagePreviewProvider } from "./ImagePreview";
import { WINDOW_RESIZING_CLASS } from "./WindowResizeState";
import { useComposerDraftState } from "./ComposerDraftState";
import {
  useComposerPendingState,
  type QueuedMessageEditTarget,
} from "./ComposerPendingState";
import { useSidebarDrawerState } from "./SidebarDrawerState";
import {
  threadsForDesktopProject,
  useSidebarProjectState,
} from "./SidebarProjectState";
import { useViewSwitchState } from "./ViewSwitchState";
import {
  loadPopOutRuntime,
  loadRuntime,
  selectRuntimeContext,
} from "./RuntimeLoadState";
import { createProjectRuntimeActions } from "./ProjectRuntimeActions";
import { createSessionTabActions } from "./SessionTabActions";
import { createThreadActivationActions } from "./ThreadActivationActions";
import { createThreadMutationActions } from "./ThreadMutationActions";
import { createRuntimeSettingsActions } from "./RuntimeSettingsActions";
import { createCollaborationActions } from "./CollaborationActions";
import { createConversationDemoPaneActions } from "./ConversationDemoPaneActions";
import {
  createConversationHistoryActions,
  type HistoryMessageEditState,
  type PendingForkState,
} from "./ConversationHistoryActions";
import { CachedConversationPanes } from "./CachedConversationPanes";
import {
  ConversationSidePanels,
  ConversationSplitLayoutRenderer,
  ConversationTitleActions,
  ConversationTitleContent,
  SettingsShellRenderer,
} from "./ConversationShellRenderers";
export { SIDEBAR_DRAWER_HOVER_OPEN_DELAY_MS } from "./SidebarDrawerState";

function permissionSummaryForMode(mode: PermissionMode): PermissionSummary {
  return { mode };
}

function initializedForSelectedPermissionMode(
  initialized: InitializeResult | undefined,
  mode: PermissionMode | undefined,
): InitializeResult | undefined {
  if (!initialized || mode === undefined) {
    return initialized;
  }
  return {
    ...initialized,
    permissions: permissionSummaryForMode(mode),
  };
}

const SIDEBAR_SESSION_SECTION_COLLAPSE_MS = 190;
const ENVIRONMENT_PANEL_MOTION_MS = 260;
const ENVIRONMENT_PANEL_WIDTH_PX = 328;
const ENVIRONMENT_PANEL_WIDTH_CSS = `${ENVIRONMENT_PANEL_WIDTH_PX}px`;
const WORKTREE_FORK_NON_GIT_REASON =
  "当前工作目录不是 git 仓库，不能创建 git worktree";
// Cap on the number of bars rendered in the always-visible rail. The
// rail is a thin at-a-glance index; if there are more queries than fit,
// we collapse the tail into a single bar.
const QUERY_HISTORY_RAIL_MAX_BARS = 20;
// Keep only the active conversation pane mounted. Hidden panes used to retain
// full TurnView DOM trees, making long sessions heavier after each tab switch.
const CACHED_THREAD_PANE_LIMIT = 1;
type EnvironmentDialog = "commit" | "pull-request" | null;
type TurnProgressContent = {
  label: string;
  detail?: string;
};

const RENDERER_ENV = (
  import.meta as ImportMeta & {
    env?: { DEV?: boolean; VITE_ENABLE_RUN_DEBUG_PANEL?: string };
  }
).env;
const ENABLE_DEBUG_CONTROL_SETTING = Boolean(RENDERER_ENV?.DEV);
const ENABLE_DEBUG_CONTROLS = Boolean(
  RENDERER_ENV?.DEV || RENDERER_ENV?.VITE_ENABLE_RUN_DEBUG_PANEL === "true",
);
const ENABLE_LAUNCH_PREVIEW = Boolean(RENDERER_ENV?.DEV);
const ENABLE_RUN_DEBUG_PANEL = Boolean(
  RENDERER_ENV?.DEV || RENDERER_ENV?.VITE_ENABLE_RUN_DEBUG_PANEL === "true",
);
const ENABLE_CONVERSATION_FIXTURES = Boolean(RENDERER_ENV?.DEV);
const ENABLE_PLAN_PANEL_DEBUG = Boolean(RENDERER_ENV?.DEV);

function useStableCallback<T extends (...args: any[]) => any>(callback: T): T {
  const callbackRef = useRef(callback);
  useLayoutEffect(() => {
    callbackRef.current = callback;
  });
  return useCallback(
    ((...args: Parameters<T>): ReturnType<T> => callbackRef.current(...args)) as T,
    [],
  );
}

function normalizeWorkspaceFileSwitchPath(path: string, root?: string): string {
  const normalizedPath = path.trim().replace(/\\/g, "/").replace(/\/+$/, "");
  const normalizedRoot = root?.trim().replace(/\\/g, "/").replace(/\/+$/, "");
  if (normalizedRoot && normalizedPath.startsWith(`${normalizedRoot}/`)) {
    return normalizedPath.slice(normalizedRoot.length + 1);
  }
  return normalizedPath.replace(/^\/+/, "");
}

function serverEventCarriesModelOutputDelta(event: ServerEvent): boolean {
  if (event.kind !== "notification") {
    return false;
  }
  switch (event.message.method) {
    case "item/agentMessage/delta":
    case "item/reasoning/delta":
    case "item/toolCall/delta":
      return true;
    default:
      return false;
  }
}

function readPopOutInit(): PopOutInitResult | null {
  try {
    const init = window.wuu.popOutInit();
    return init.kind && init.context ? init : null;
  } catch {
    return null;
  }
}

export function App(): JSX.Element {
  const [popOutInit] = useState<PopOutInitResult | null>(() => readPopOutInit());
  const poppedOutMode = Boolean(popOutInit?.kind && popOutInit.context);
  const [state, setState] = useState<AppState>(initialState);
  // Bumped each time the empty-state hint chip should refocus the hero
  // composer. A counter (not a boolean) so re-clicking the same chip
  // still fires the focus effect on the next render.
  const [heroComposerFocusTick, setHeroComposerFocusTick] = useState(0);
  const [goalSummary, setGoalSummary] = useState<ComposerGoalSummary | null>(
    null
  );
  const {
    prompt,
    setPrompt,
    composerImages,
    setComposerImages,
    composerFiles,
    setComposerFiles,
    splitComposerDrafts,
    setSplitComposerDrafts,
    subthreadComposerDraft,
    setSubthreadComposerDraft,
    attachComposerAttachmentFiles,
    removeComposerImage,
    removeComposerFile,
    attachSubthreadComposerAttachmentFiles,
    removeSubthreadComposerImage,
    removeSubthreadComposerFile,
    updateSplitComposerDraft,
    setSplitComposerPrompt,
    attachSplitComposerAttachmentFiles,
    removeSplitComposerImage,
    removeSplitComposerFile,
    moveSplitDraftToGlobalComposer,
    currentPrimaryComposerDraft,
    restorePrimaryComposerDraft,
  } = useComposerDraftState({
    setStatus: (status) =>
      setState((current) => ({
        ...current,
        status,
      })),
  });
  const [historyMessageEdit, setHistoryMessageEdit] =
    useState<HistoryMessageEditState | undefined>(undefined);
  const [projectMenuOpen, setProjectMenuOpen] = useState(false);
  const closeProjectMenu = useCallback(() => setProjectMenuOpen(false), []);
  const appShellRef = useRef<HTMLDivElement>(null);
  const settingsShellRef = useRef<HTMLDivElement>(null);
  const {
    sidebarWidth,
    settingsSidebarWidth,
    sidebarCollapsed,
    resizingSidebar,
    sidebarAnimating,
    clampedWorkspaceRightPanelWidth,
    resizingRightPanel,
    rightPanelOpen,
    rightPanelAnimating,
    effectiveSidebarWidth,
    setRightPanelOpenWithMotion,
    startSidebarResize,
    startSettingsSidebarResize,
    startRightPanelResize,
    handleRightPanelSeparatorKey,
    resetWorkspaceRightPanelWidth,
    clampedThreadPanelWidth,
    resizingThreadPanel,
    startThreadPanelResize,
    handleThreadPanelSeparatorKey,
    resetThreadPanelWidth,
    toggleSidebar,
    handleSidebarSeparatorKey,
    handleSettingsSidebarSeparatorKey,
    resetSettingsSidebarWidth,
    splitLeftPercent,
    resizingSplit,
    startSplitResize,
    handleSplitSeparatorKey,
    resetSplitPercent,
  } = useAppLayoutState({
    layoutRootRef: appShellRef,
    settingsLayoutRootRef: settingsShellRef,
    onCloseProjectMenu: closeProjectMenu,
  });
  const {
    sidebarDrawerPhase,
    sidebarHoverZoneRef,
    cancelSidebarDrawerOpen,
    openSidebarDrawer,
    scheduleSidebarDrawerOpen,
    closeSidebarDrawer,
  } = useSidebarDrawerState({
    appShellRef,
    sidebarCollapsed,
    resizingSidebar,
    activeSessionTabID: state.activeSessionTabID,
    motionMs: SIDEBAR_MOTION_MS,
  });
  const {
    collapsedSidebarSectionIDs,
    expandedSidebarSectionIDs,
    collapsingSidebarSectionIDs,
    projectThreadsByProjectID,
    cachedScratchThreads,
    sidebarSectionOrder,
    setSidebarSectionOrder,
    updateCachedSidebarThread,
    removeCachedSidebarThread,
    toggleSidebarSectionCollapsed,
  } = useSidebarProjectState({
    projects: state.projects,
    threads: state.threads,
    activeContext: state.activeContext,
    activeProjectID: state.activeProjectId,
    setStatus: (status) =>
      setState((current) => ({
        ...current,
        status,
      })),
    collapseMs: SIDEBAR_SESSION_SECTION_COLLAPSE_MS,
  });
  const [runtimeMenuOpen, setRuntimeMenuOpen] = useState(false);
  const [accessMenuOpen, setAccessMenuOpen] = useState(false);
  const [selectedPermissionMode, setSelectedPermissionMode] =
    useState<PermissionMode | undefined>(undefined);
  // Per-thread chat-style "work focus" chip selections made this session.
  // Keyed by thread ID; absence means the chip was never touched, so the
  // composer echoes Thread.focus_workspace and turn/start carries no
  // focus_workspace param (see focusWorkspaceSendValue in AppState.ts).
  const [chatFocusOverrides, setChatFocusOverrides] = useState<
    Record<string, string>
  >({});
  const [codexRuntimeMenu, setCodexRuntimeMenu] =
    useState<CodexRuntimeMenu>(null);
  const [codexModels, setCodexModels] = useState<CodexModelLoadState>({
    loading: false,
    error: "",
    models: [],
  });
  const [branchMenuOpen, setBranchMenuOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [settingsInitialPage, setSettingsInitialPage] =
    useState<SettingsPage>("providers");
  // 设置 → 记忆 打开时预选的同事笔记本（档案面板「在记忆面板中管理」）。
  const [settingsMemoryFocusID, setSettingsMemoryFocusID] = useState<
    string | undefined
  >(undefined);
  const {
    usageRange,
    setUsageRange,
    settingsUsage,
    codexPets,
    codexPetsLoading,
    codexPetsError,
    refreshCodexPets,
    updateCodexPets,
  } = useSettingsRuntimeState({ settingsOpen });
  const [projectFilter, setProjectFilter] = useState("");
  const [launchPreviewPinned, setLaunchPreviewPinned] = useState(false);
  const {
    workspaceViewTabs,
    workspaceActiveViewTabID,
    workspacePanelView,
    workspaceMode,
    setWorkspaceMode,
    ensureWorkspaceToolTab,
    activateWorkspaceTool,
    openWorkspaceTool,
    openWorkspaceDiffTab,
    showWorkspaceToolPicker,
    focusWorkspaceViewTab,
    closeWorkspaceViewTab,
    closeWorkspaceViewTabsWhere,
    reorderWorkspaceViewTabs,
    toggleRightPanel,
  } = useWorkspaceToolState({
    rightPanelOpen,
    setRightPanelOpenWithMotion,
  });
  const [environmentPanelOpen, setEnvironmentPanelOpen] = useState(false);
  const [environmentPanelDismissed, setEnvironmentPanelDismissed] =
    useState(false);
  const [environmentPanelHasRoom, setEnvironmentPanelHasRoom] = useState(() =>
    typeof window === "undefined"
      ? false
      : window.matchMedia("(min-width: 1320px) and (min-height: 680px)")
          .matches,
  );
  const [environmentPanelMounted, setEnvironmentPanelMounted] = useState(false);
  const [environmentPanelClosing, setEnvironmentPanelClosing] = useState(false);
  const [environmentPanelReserved, setEnvironmentPanelReserved] =
    useState(false);
  const [environmentPanelMenu, setEnvironmentPanelMenu] =
    useState<EnvironmentPanelMenu>(null);
  const [rightPanelFilePath, setRightPanelFilePath] = useState<
    string | undefined
  >(undefined);
  // In-window "globalize" toggle: collapse sidebar + conversation so the user
  // can dedicate the window to browsing the right panel. Reset on close.
  const [rightPanelGlobalized, setRightPanelGlobalized] = useState(false);
  useEffect(() => {
    if (!rightPanelOpen) {
      setRightPanelGlobalized(false);
    }
  }, [rightPanelOpen]);
  const [environmentDialog, setEnvironmentDialog] =
    useState<EnvironmentDialog | null>(null);
  const [contextCompositionEntries, setContextCompositionEntries] = useState<
    ContextCompositionEntry[]
  >([]);
  const [instructionFilesEntries, setInstructionFilesEntries] = useState<
    InstructionFilesEntry[]
  >([]);
  // Reply subthreads (群中群) for the active chat thread, keyed by
  // anchor_item_id, feeding the chat view's reply badges / task 活动卡. Loaded
  // per active chat thread (see effect below); non-active panes never need it.
  const [chatSubthreads, setChatSubthreads] = useState<{
    threadID: string;
    // byAnchor 服务消息行的 reply 徽标;standalone task 没有锚点,由任务
    // 看板 tab 自己拉取列表展示。
    byAnchor: Map<string, ConversationSubthread>;
  } | null>(null);
  // Bump on every thread/subUpdated notification: an open task-board tab
  // reloads on this tick (its thread may not be the active thread, so the
  // chatSubthreadsNonce path alone would miss it).
  const [boardRefreshTick, setBoardRefreshTick] = useState(0);
  const {
    participants,
    setParticipants,
    participantPanel,
    setParticipantPanel,
    refreshParticipants,
    openParticipantProfile: openParticipantProfilePanel,
    handleNewParticipantCreate,
    handleParticipantSave,
    handleParticipantFeedback,
    handleParticipantRetire,
    exportParticipantTemplate,
    importParticipantTemplate,
  } = useParticipantState({
    initialized: Boolean(state.initialized),
    setStatus: (status) =>
      setState((current) => ({
        ...current,
        status,
      })),
  });
  const [archiveConfirmThreadID, setArchiveConfirmThreadID] = useState<
    string | undefined
  >(undefined);
  // Mirrors `archiveConfirmThreadID` for the info-panel subagent rows.
  // The state lives in App rather than the panel so the "press again to
  // confirm" survives the panel being toggled off and on, and so a
  // single archive button click in either surface is consistent.
  const [archiveConfirmSubagentID, setArchiveConfirmSubagentID] = useState<
    string | undefined
  >(undefined);
  // When the user clicks "分叉" on a non-latest user message, the fork
  // picker dialog asks whether to stay local or fork into a new worktree.
  // Holding the source thread snapshot in state lets the dialog callback
  // resolve the same data the user clicked, regardless of subsequent
  // thread updates.
  const [pendingFork, setPendingFork] =
    useState<PendingForkState | undefined>(undefined);
  const {
    pendingViewSwitch,
    visiblePendingThreadID,
    visiblePendingProjectID,
    viewSwitchPending,
    viewContextSwitchPending,
    beginViewSwitch,
    beginInstantThreadSwitch,
    finishViewSwitch,
    cancelViewSwitch,
    isCurrentViewSwitchRequest,
  } = useViewSwitchState();
  const hideDebugControls = useCallback(() => {
    setLaunchPreviewPinned(false);
  }, []);
  const {
    debugControlsEnabled,
    setDebugControlsEnabled,
    debugControlsVisible,
    runDebugOpen,
    setRunDebugOpen,
    conversationGridVisible,
    setConversationGridVisible,
    chipGalleryOpen,
    setChipGalleryOpen,
    runDebugEvents,
    runDebugCopied,
    runDebugRef,
    appendRunDebugEvent,
    resetRunDebugEvents,
    recordRunDebugEvent,
    copyRunDebugInfo,
  } = useAppDebugState({
    enabled: ENABLE_DEBUG_CONTROLS,
    forced: RENDERER_ENV?.VITE_ENABLE_RUN_DEBUG_PANEL === "true",
    onHideDebugControls: hideDebugControls,
  });
  const queryHistoryRailRef = useRef<HTMLDivElement | null>(null);
  const [queryHistoryOpen, setQueryHistoryOpen] = useState(false);
  const queryHistoryCloseTimerRef = useRef<number | undefined>(undefined);
  const windowResizingRef = useRef(false);
  const environmentPanelHasRoomRef = useRef(environmentPanelHasRoom);
  const pendingEnvironmentPanelHasRoomRef = useRef<boolean | undefined>(
    undefined,
  );
  const gitRefreshTimerRef = useRef<number | undefined>(undefined);
  const gitRefreshInFlightRef = useRef(false);
  const gitRefreshQueuedRef = useRef(false);
  const projectMenuRef = useRef<HTMLDivElement>(null);
  const runtimeMenuRef = useRef<HTMLDivElement>(null);
  const accessMenuRef = useRef<HTMLDivElement>(null);
  const codexRuntimeRef = useRef<HTMLDivElement>(null);
  // The split reply panel mounts a second full composer alongside the dock
  // composer, so its permission (盾牌) menu needs its own anchor + open state —
  // sharing the dock's would misplace the floating menu and cross-toggle it.
  const subthreadAccessMenuRef = useRef<HTMLDivElement>(null);
  const [subthreadAccessMenuOpen, setSubthreadAccessMenuOpen] = useState(false);
  const environmentToggleRef = useRef<HTMLButtonElement>(null);
  const environmentPanelRef = useRef<HTMLDivElement>(null);
  const appStateRef = useRef<AppState>(initialState);
  const workspaceEditorDirtyRef = useRef<WorkspaceFileDirtyState>({ dirty: false });
  const {
    pendingComposerMessagesByThread,
    queuedMessageEditTargetRef,
    setQueuedMessageEditTargetNow,
    pendingComposerMessagesForThread: pendingComposerMessagesForActiveThread,
    updateThreadPendingComposerMessages,
    clearThreadPendingComposerMessages,
    syncPendingComposerMessagesFromServerEvent,
    reconcilePendingComposerMessagesForState,
    enqueueComposerMessage,
    removeQueuedMessage,
    removeGuideMessage,
    editQueuedMessage,
    editGuideMessage,
    guideQueuedMessage,
    threadHasPendingComposerMessages,
  } = useComposerPendingState({
    getAppState: () => appStateRef.current,
    getPrimaryComposerDraft: currentPrimaryComposerDraft,
    restorePrimaryComposerDraft,
    setStatus: (status) =>
      setState((current) => ({
        ...current,
        status,
      })),
    sendComposerMessageToThread,
  });
  const localDemoThreadsRef = useRef(new Map<string, Thread>());
  const cachedThreadPaneHistoryRef = useRef<string[]>([]);
  const draftSessionTabCounterRef = useRef(0);
  const poppingOutTabIDsRef = useRef(new Set<string>());
  const poppingOutSubthreadIDsRef = useRef(new Set<string>());
  // Synchronous in-flight guard for openParticipantDM. A rapid double-click
  // on the same agent row otherwise fires two startThread calls and creates
  // duplicate DM threads; the ref is checked and set before any await so
  // the second invocation short-circuits immediately, and cleared in the
  // finally block of openParticipantDM regardless of the resolution path.
  const openingDMParticipantIDRef = useRef<string | undefined>(undefined);
  const currentSessionTab = activeSessionTab(state);

  useEffect(() => {
    const handleBeforeUnload = (event: BeforeUnloadEvent): void => {
      if (!workspaceEditorDirtyRef.current.dirty) {
        return;
      }
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", handleBeforeUnload);
    return () => {
      window.removeEventListener("beforeunload", handleBeforeUnload);
    };
  }, []);

  // Workspace panel (file tree / file preview / terminal) root: follows the
  // active thread's own cwd when it differs from state.activeContext — the
  // main remaining case is a worktree-fork thread, whose cwd is a git
  // worktree directory distinct from the project root activeContext stays
  // pinned to. The diff/review panel intentionally keeps using
  // state.activeContext directly (see workspacePanelContext's doc comment).
  const workspaceContext = useMemo(
    () => workspacePanelContext(state.activeContext, state.thread),
    [state.activeContext, state.thread],
  );
  const activeWorkspaceFile =
    currentSessionTab?.kind === "file" &&
    sameRuntimeContext(currentSessionTab.context, workspaceContext)
      ? currentSessionTab.path
      : undefined;
  const activeThread = activeThreadForState(state);
  const activeThreadID = activeThread?.id;
  const {
    openSubthreadPanel,
    setOpenSubthreadPanel,
    chatSubthreadsNonce,
    subthreadLeadCandidates,
    handleSubthreadUpdatedNotification,
    openConversationSubthreadByID,
    openConversationSubthread,
    resolveOpenConversationSubthread,
    sendOpenConversationSubthreadMessage,
    escalateOpenConversationSubthread,
    bubbleOpenConversationSubthread,
    reactToOpenConversationSubthreadMessage,
  } = useConversationSubthreadState({
    activeThreadID,
    threads: [state.thread, state.secondaryThread, ...state.threads],
    subthreadComposerDraft,
    setSubthreadComposerDraft,
    onOpenSubthreadPanel: () => {
      setEnvironmentPanelOpen(false);
      setEnvironmentPanelDismissed(true);
      setParticipantPanel(undefined);
    },
  });
  const activeThreadIsGroup = Boolean(activeThread && isGroupThread(activeThread));
  // Chat-style threads (DM + group) follow chat send semantics (issue #10):
  // the composer never surfaces the worker-thread queue strip or the stop
  // button; a send always reads as "message sent" in the transcript.
  const activeThreadIsChatStyle = Boolean(
    activeThread && (isDMThread(activeThread) || isGroupThread(activeThread)),
  );
  const activeThreadMarks = useThreadMarkList(
    activeThreadID,
    activeThreadIsChatStyle,
  );
  // Reload trigger for subthread badges: new main-stream messages can anchor
  // new replies. (cth reply traffic itself is short-circuited off the main
  // turns, so this misses live reply-count growth — acceptable until a
  // subthread-scoped notification lands; opening a reply bumps the nonce.)
  const activeThreadTurnCount = activeThread?.turns?.length ?? 0;
  const composerInitialized = useMemo(
    () =>
      initializedForSelectedPermissionMode(
        state.initialized,
        selectedPermissionMode,
      ),
    [state.initialized, selectedPermissionMode],
  );
  // Per-thread keep-alive for the main conversation pane. We keep the active
  // thread and a small recency buffer mounted so switching back does not
  // unmount/remount the entire <TurnView> tree. Keeping every open tab mounted
  // makes long sessions progressively heavier because each hidden pane still
  // retains its full React subtree.
  //
  // Crucially we derive the cache synchronously from state.sessionTabs
  // and state.thread via useMemo, not via useState + useEffect. The
  // async effect path rendered once with the new activeThreadID but
  // the stale cache (no pane for the new thread) and then a second
  // time with the cache updated — the "two flickers" the user saw.
  // Computing the cache from state in the same render closes that
  // empty frame.
  const cachedThreadPaneIDs = useMemo(() => {
    const activeID = state.thread?.id;
    const openThreadIDs = new Set(
      state.sessionTabs
        .filter(
          (tab): tab is Extract<SessionTab, { kind: "thread" }> =>
            tab.kind === "thread",
        )
        .map((tab) => tab.threadID),
    );
    if (activeID) {
      openThreadIDs.add(activeID);
    }
    const recentIDs = cachedThreadPaneHistoryRef.current.filter(
      (id) => openThreadIDs.has(id) && id !== activeID,
    );
    const next = [
      ...(activeID ? [activeID] : []),
      ...recentIDs,
    ].slice(0, CACHED_THREAD_PANE_LIMIT);
    cachedThreadPaneHistoryRef.current = next;
    return next;
  }, [state.thread?.id, state.sessionTabs]);
  const cachedConversationThreadsByID = useMemo(
    () =>
      conversationPaneThreadsByID(
        state.threads,
        state.thread,
        state.secondaryThread,
      ),
    [state.threads, state.thread, state.secondaryThread],
  );
  const openTurnFileDiffPanel = useStableCallback(
    (threadID: string, selection: TurnFileDiffSelection) => {
      openWorkspaceDiffTab({ threadID, path: selection.path, selection });
      setRightPanelOpenWithMotion(true);
      closeEnvironmentPanel({ dismissed: true });
    },
  );
  const activePendingComposerMessages = pendingComposerMessagesForActiveThread(
    activeThreadID,
  );
  const queuedMessages = activePendingComposerMessages.queued;
  const guideMessages = activePendingComposerMessages.guides;
  // Self-healing reconciliation for pending composer messages: once a queued
  // or guide send materializes as a real user_message turn item, drop it from
  // the composer queue strip / chat "发送中…" bubble even if the live
  // turn/started (or item/completed) removal notification was missed — e.g. it
  // got gated out of the renderer because the thread was backgrounded when the
  // event arrived (serverEventTargetsActiveContext filter). Keying off the
  // authoritative thread turns means a message that already went out can never
  // stay stuck as "排队中".
  useEffect(() => {
    reconcilePendingComposerMessagesForState(appStateRef.current);
  }, [
    pendingComposerMessagesByThread,
    state.thread,
    state.secondaryThread,
    state.threads,
  ]);
  const refreshGoalSummary = useCallback(
    async (threadID = activeThreadID) => {
      if (!threadID) {
        setGoalSummary(null);
        return;
      }
      try {
        const summary = await window.wuu.getActiveGoalSummary(threadID);
        if (activeThreadIDForState(appStateRef.current) === threadID) {
          setGoalSummary(summary);
        }
      } catch {
        if (activeThreadIDForState(appStateRef.current) === threadID) {
          setGoalSummary(null);
        }
      }
    },
    [activeThreadID],
  );
  const editGoalText = useCallback(
    async (nextText: string) => {
      if (!goalSummary) {
        return;
      }
      const threadID = goalSummary.thread_id ?? activeThreadID;
      if (!threadID) {
        return;
      }
      await window.wuu.updateGoalText(goalSummary.id, nextText, threadID);
      await refreshGoalSummary(threadID);
    },
    [activeThreadID, goalSummary, refreshGoalSummary],
  );
  const pauseCurrentGoal = useCallback(async () => {
    if (!goalSummary) {
      return;
    }
    const threadID = goalSummary.thread_id ?? activeThreadID;
    if (!threadID) {
      return;
    }
    await window.wuu.pauseGoal(goalSummary.id, threadID);
    await refreshGoalSummary(threadID);
  }, [activeThreadID, goalSummary, refreshGoalSummary]);
  const resumeCurrentGoal = useCallback(async () => {
    if (!goalSummary) {
      return;
    }
    const threadID = goalSummary.thread_id ?? activeThreadID;
    if (!threadID) {
      return;
    }
    await window.wuu.resumeGoal(goalSummary.id, threadID);
    await refreshGoalSummary(threadID);
  }, [activeThreadID, goalSummary, refreshGoalSummary]);
  const clearCurrentGoal = useCallback(async () => {
    if (!goalSummary) {
      return;
    }
    const threadID = goalSummary.thread_id ?? activeThreadID;
    if (!threadID) {
      return;
    }
    await window.wuu.clearGoal(goalSummary.id, threadID);
    await refreshGoalSummary(threadID);
  }, [activeThreadID, goalSummary, refreshGoalSummary]);
  const {
    conversationSearch,
    conversationSearchResults,
    conversationSearchRef,
    conversationSearchInputRef,
    toggleConversationSearch,
    closeConversationSearch,
    refreshConversationSearchThreads,
    selectConversationSearchResult,
    handleConversationSearchKeyDown,
    setConversationSearchQuery,
    clearConversationSearchQuery,
    setConversationSearchSelectedIndex,
  } = useConversationSearch({
    activeContext: state.activeContext,
    getAppState: () => appStateRef.current,
    setAppState: setState,
    onOpen: () => {
      setProjectMenuOpen(false);
      setRuntimeMenuOpen(false);
      setAccessMenuOpen(false);
      setBranchMenuOpen(false);
      setCodexRuntimeMenu(null);
      setEnvironmentDialog(null);
      setPendingFork(undefined);
    },
    onSelectThread: (threadID) => void activateThread(threadID),
  });

  // Cmd/Ctrl+P toggles the conversation search overlay. Mirrors the
  // "Quick Open / Go to file" convention from VS Code, Sublime, and
  // JetBrains — semantically "navigate to a thing by name" rather than
  // Cmd+F's "find text in current view". preventDefault stops the
  // browser's Print dialog. Works from anywhere in the app, including
  // while typing in the chat composer.
  useEffect(() => {
    function onKeyDown(event: KeyboardEvent): void {
      if (
        (event.metaKey || event.ctrlKey) &&
        !event.shiftKey &&
        !event.altKey &&
        event.key.toLowerCase() === "p"
      ) {
        event.preventDefault();
        toggleConversationSearch();
      }
    }
    window.addEventListener("keydown", onKeyDown);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
    };
  }, [toggleConversationSearch]);

  function openEnvironmentDialog(dialog: EnvironmentDialog): void {
    closeConversationSearch({ immediate: true });
    setPendingFork(undefined);
    setEnvironmentDialog(dialog);
  }
  const {
    pendingBrowserURL,
    consumePendingBrowserURL,
    openBrowserURL,
    rememberBrowserURLForActiveThread,
  } = useThreadBrowserPreview({
    activeThread,
    activeThreadID,
    onOpenBrowser: () => openWorkspaceTool("browser"),
  });
  const activePlanUpdate = latestPlanUpdateForThread(activeThread);
  // Distinct from `activePlanUpdate` above: the floating "jump to latest /
  // progress" pill cluster only tracks a plan while its turn is still
  // running (see `activePlanUpdateForThread`), whereas the environment
  // side panel keeps showing the most recent plan — running or completed —
  // as a persistent checklist once the user opens it.
  const activePlanPillUpdate = activePlanUpdateForThread(activeThread);
  const activeContextKey = state.activeContext
    ? runtimeContextKey(state.activeContext)
    : "";
  const activePlanTotal = activePlanPillUpdate?.plan.length ?? 0;
  const activePlanCompleted =
    activePlanPillUpdate?.plan.filter((item) => item.status === "completed").length ?? 0;
  // Hide the pill once the active plan is fully done; it will reappear when
  // the next plan arrives with pending work (or more steps are appended).
  const activePlanVisible = Boolean(
    activePlanPillUpdate &&
      activePlanTotal > 0 &&
      activePlanCompleted < activePlanTotal,
  );
  const activePlanCurrentItem = activePlanPillUpdate?.plan.find(
    (item) => item.status === "in_progress",
  );
  const activePlanNextItem = activePlanPillUpdate?.plan.find(
    (item) => item.status === "pending",
  );
  const activePlanDetailItems = [activePlanCurrentItem, activePlanNextItem].flatMap(
    (item, index, items) =>
      item && items.findIndex((other) => other === item) === index ? [item] : [],
  );
  const forkWorktreeDisabledReason =
    state.gitStatus?.is_repo === false
      ? WORKTREE_FORK_NON_GIT_REASON
      : undefined;
  const splitConversation = Boolean(
    state.thread && state.secondaryThread && !workspaceMode,
  );

  // Past-query popover control. The rail beside the scrollbar is the hover
  // target; we close on a short delay so the user can travel from the rail
  // into the floating list without it snapping shut.
  function openQueryHistory(): void {
    if (activeThreadReadOnly || pastQueries.length === 0) {
      return;
    }
    cancelQueryHistoryClose();
    setQueryHistoryOpen(true);
  }

  function scheduleQueryHistoryClose(): void {
    cancelQueryHistoryClose();
    queryHistoryCloseTimerRef.current = window.setTimeout(() => {
      queryHistoryCloseTimerRef.current = undefined;
      setQueryHistoryOpen(false);
    }, 200);
  }

  function cancelQueryHistoryClose(): void {
    if (queryHistoryCloseTimerRef.current !== undefined) {
      window.clearTimeout(queryHistoryCloseTimerRef.current);
      queryHistoryCloseTimerRef.current = undefined;
    }
  }

  function handleQueryHistorySelect(entry: QueryHistoryEntry): void {
    cancelQueryHistoryClose();
    setQueryHistoryOpen(false);
    // Stop auto-follow before we jump — otherwise the next stream tick
    // would drag the scroll position back to the bottom and undo the
    // jump before the user even registers it happened.
    disableConversationAutoFollow();
    scrollToUserMessage(entry.turnID, entry.itemID);
  }

  useEffect(() => {
    return () => {
      cancelQueryHistoryClose();
    };
  }, []);

  useEffect(() => {
    const root = document.documentElement;
    let resizeEndTimer: number | undefined;
    let resizing = false;

    function setResizeState(nextResizing: boolean): void {
      if (resizing === nextResizing) {
        return;
      }
      resizing = nextResizing;
      windowResizingRef.current = nextResizing;
      root.classList.toggle(WINDOW_RESIZING_CLASS, nextResizing);
      if (
        !nextResizing &&
        pendingEnvironmentPanelHasRoomRef.current !== undefined
      ) {
        const pendingHasRoom = pendingEnvironmentPanelHasRoomRef.current;
        pendingEnvironmentPanelHasRoomRef.current = undefined;
        if (environmentPanelHasRoomRef.current !== pendingHasRoom) {
          environmentPanelHasRoomRef.current = pendingHasRoom;
          setEnvironmentPanelHasRoom(pendingHasRoom);
        }
      }
    }

    function scheduleResizeEnd(delay = 140): void {
      if (resizeEndTimer !== undefined) {
        window.clearTimeout(resizeEndTimer);
      }
      resizeEndTimer = window.setTimeout(() => {
        resizeEndTimer = undefined;
        setResizeState(false);
      }, delay);
    }

    function handleWindowResize(): void {
      setResizeState(true);
      scheduleResizeEnd();
    }

    const offWindowResizeState = window.wuu.onWindowResizeState(
      ({ resizing: nextResizing }) => {
        if (nextResizing) {
          setResizeState(true);
          scheduleResizeEnd();
          return;
        }
        scheduleResizeEnd(40);
      },
    );

    window.addEventListener("resize", handleWindowResize);
    return () => {
      offWindowResizeState();
      window.removeEventListener("resize", handleWindowResize);
      if (resizeEndTimer !== undefined) {
        window.clearTimeout(resizeEndTimer);
      }
      windowResizingRef.current = false;
      pendingEnvironmentPanelHasRoomRef.current = undefined;
      setResizeState(false);
    };
  }, []);

  useEffect(() => {
    const query = window.matchMedia(
      "(min-width: 1320px) and (min-height: 680px)",
    );
    const update = (): void => {
      const nextHasRoom = query.matches;
      if (
        windowResizingRef.current ||
        document.documentElement.classList.contains(WINDOW_RESIZING_CLASS)
      ) {
        pendingEnvironmentPanelHasRoomRef.current = nextHasRoom;
        return;
      }
      pendingEnvironmentPanelHasRoomRef.current = undefined;
      environmentPanelHasRoomRef.current = nextHasRoom;
      setEnvironmentPanelHasRoom(nextHasRoom);
    };
    update();
    query.addEventListener("change", update);
    return () => query.removeEventListener("change", update);
  }, []);

  useEffect(() => {
    appStateRef.current = state;
  }, [state]);

  useEffect(() => {
    void refreshGoalSummary();
  }, [refreshGoalSummary]);

  useEffect(() => {
    if (heroComposerFocusTick === 0) {
      return;
    }
    // The hero composer only exists while the empty conversation view
    // is on screen. Query inside the empty-home section so we never
    // accidentally grab the dock composer's textarea.
    const root = document.querySelector(".empty-home");
    const textarea = root?.querySelector(".hero-composer-wrap textarea");
    if (textarea instanceof HTMLTextAreaElement) {
      textarea.focus();
    }
  }, [heroComposerFocusTick]);

  useEffect(() => {
    const off = window.wuu.onServerEvent((event) => {
      if (event.kind !== "notification") {
        return;
      }
      const method = event.message.method;
      // Refresh the composer goal banner whenever a turn or thread
      // lifecycle event lands. The backend filters terminal goals, so
      // the summary just collapses to null after a clean completion.
      if (
        method === "turn/started" ||
        method === "turn/completed" ||
        method === "turn/error" ||
        method === "turn/interrupted" ||
        method === "thread/started" ||
        method === "thread/resumed" ||
        method === "thread/updated"
      ) {
        void refreshGoalSummary();
      }
    });
    return off;
  }, [refreshGoalSummary]);

  useEffect(() => {
    let mounted = true;
    const off = window.wuu.onServerEvent((event) => {
      if (!mounted) {
        return;
      }
      if (
        event.kind === "notification" &&
        event.message.method === "participant/updated"
      ) {
        void refreshParticipants().catch((error) => {
          setState((current) => ({
            ...current,
            status: desktopApiErrorMessage(error, "无法刷新 Agents"),
          }));
        });
      }
      // Reply-subthread (cth) traffic never appends a main-stream turn, so the
      // split reply panel and the reply-count badge only stay live via this
      // subthread-scoped notification. Handled BEFORE the active/global gate
      // because it carries the PARENT group thread id and must patch an open
      // panel / bump the badge regardless of which context started the run.
      if (
        event.kind === "notification" &&
        event.message.method === "thread/subUpdated"
      ) {
        const note = event.message
          .params as unknown as SubthreadUpdatedNotification;
        handleSubthreadUpdatedNotification(
          note,
          activeThreadIDForState(appStateRef.current),
        );
        // An open task-board tab may target a non-active thread; its list
        // reloads on this unconditional tick (cheap: only a mounted board
        // subscribes to it).
        if (note?.thread_id) {
          setBoardRefreshTick((tick) => tick + 1);
        }
      }
      // Workspace-scoped events (project sessions/files/terminals) stay bound
      // to the active context, but global-collaboration threads (DM/group) run
      // under whichever app-server client started them and must pass through so
      // the roster's busy/unread stays live across project switches (issue #9).
      if (
        !serverEventTargetsActiveContext(event, appStateRef.current) &&
        !serverEventTargetsGlobalThread(event, appStateRef.current)
      ) {
        return;
      }
      recordRunDebugEvent(event);
      const handling = handleStreamingNotification(event, appStateRef.current);
      if (handling === "stream" || handling === "stream-state") {
        scheduleStreamScroll();
        if (
          event.kind === "notification" &&
          serverEventCarriesModelOutputDelta(event)
        ) {
          setState((current) =>
            appendStreamingTokenSample(
              current,
              event.message.params as Record<string, unknown> | undefined,
              Date.now(),
            ),
          );
        }
      }
      if (handling === "stream") {
        return;
      }
      if (handling === "background-stream") {
        return;
      }
      if (handling === "skip") {
        return;
      }
      if (serverEventShouldRefreshGit(event)) {
        scheduleGitStatusRefresh(600);
      }
      syncPendingComposerMessagesFromServerEvent(event);
      setState((current) => reduceServerEvent(current, event));
    });

    void (async () => {
      try {
        if (popOutInit?.kind && popOutInit.context) {
          const loadedState = await loadPopOutRuntime(popOutInit);
          if (!mounted) {
            return;
          }
          setState((current) => ({ ...current, ...loadedState }));
          // A subthread window resumes its PARENT thread for context/participants
          // (loadPopOutRuntime, above) and then opens the reply panel over it —
          // the SAME ConversationSubthreadPanel + composer the in-window split
          // uses, so the popped cth renders identically.
          if (
            popOutInit.kind === "subthread" &&
            popOutInit.threadID &&
            popOutInit.subthreadID
          ) {
            openConversationSubthreadByID(
              popOutInit.threadID,
              popOutInit.subthreadID,
            );
          }
          return;
        }
        const listedProjects = await window.wuu.listProjects();
        const runtimeState = listedProjects.active_context
          ? listedProjects
          : await window.wuu.selectNoProject(false);
        const loadedState = await loadRuntime(runtimeState);
        if (!mounted) {
          return;
        }
        setState((current) =>
          withLoadedRuntimeSessionTab(current, loadedState),
        );
      } catch (error) {
        if (!mounted) {
          return;
        }
        setState((current) => ({
          ...current,
          status: error instanceof Error ? error.message : "failed to start",
        }));
      }
    })();

    return () => {
      mounted = false;
      off();
      if (gitRefreshTimerRef.current !== undefined) {
        window.clearTimeout(gitRefreshTimerRef.current);
        gitRefreshTimerRef.current = undefined;
      }
    };
  }, [handleSubthreadUpdatedNotification, popOutInit, refreshParticipants]);

  useEffect(() => {
    if (!state.initialized || !state.activeContext) {
      setParticipants([]);
      setParticipantPanel(undefined);
      return;
    }
    void refreshParticipants().catch((error) => {
      setParticipantPanel((current) =>
        current
          ? {
              ...current,
              loading: false,
              error: desktopApiErrorMessage(error, "无法加载 Agents"),
            }
          : current,
      );
    });
  }, [state.initialized, activeContextKey, refreshParticipants]);

  useEffect(() => {
    function handlePointerDown(event: PointerEvent): void {
      const target = event.target;
      if (!(target instanceof Node)) {
        return;
      }
      if (projectMenuOpen && !projectMenuRef.current?.contains(target)) {
        setProjectMenuOpen(false);
      }
      if (
        (runtimeMenuOpen || branchMenuOpen) &&
        !runtimeMenuRef.current?.contains(target) &&
        !isInsideFloatingMenu(target, "composer-runtime")
      ) {
        setRuntimeMenuOpen(false);
        setBranchMenuOpen(false);
      }
      if (
        accessMenuOpen &&
        !accessMenuRef.current?.contains(target) &&
        !isInsideFloatingMenu(target, "composer-access")
      ) {
        setAccessMenuOpen(false);
      }
      if (
        subthreadAccessMenuOpen &&
        !subthreadAccessMenuRef.current?.contains(target) &&
        !isInsideFloatingMenu(target, "composer-access")
      ) {
        setSubthreadAccessMenuOpen(false);
      }
      if (
        codexRuntimeMenu &&
        !codexRuntimeRef.current?.contains(target) &&
        !isInsideFloatingMenu(target, "codex-runtime")
      ) {
        setCodexRuntimeMenu(null);
      }
      const environmentPanelClickOutside =
        !environmentToggleRef.current?.contains(target) &&
        !environmentPanelRef.current?.contains(target);
      if (environmentPanelClickOutside) {
        if (environmentPanelMenu) {
          setEnvironmentPanelMenu(null);
        }
        if (environmentPanelOpen && !environmentPanelHasRoom) {
          closeEnvironmentPanel();
        }
      }
      if (runDebugOpen && !runDebugRef.current?.contains(target)) {
        setRunDebugOpen(false);
      }
    }

    window.addEventListener("pointerdown", handlePointerDown);
    return () => window.removeEventListener("pointerdown", handlePointerDown);
  }, [
    accessMenuOpen,
    branchMenuOpen,
    codexRuntimeMenu,
    environmentPanelHasRoom,
    environmentPanelMenu,
    environmentPanelOpen,
    projectMenuOpen,
    runDebugOpen,
    runtimeMenuOpen,
    subthreadAccessMenuOpen,
  ]);

  useEffect(() => {
    scheduleGitStatusRefresh(0);
  }, [
    state.activeContext?.kind,
    state.activeContext?.cwd,
    state.activeProjectId,
  ]);

  useEffect(() => {
    function handleFocus(): void {
      scheduleGitStatusRefresh(0);
    }

    window.addEventListener("focus", handleFocus);
    return () => window.removeEventListener("focus", handleFocus);
  }, []);

  const activeProject = useMemo(
    () =>
      state.projects.find((project) => project.id === state.activeProjectId),
    [state.activeProjectId, state.projects],
  );
  const previewingLaunch =
    debugControlsVisible && ENABLE_LAUNCH_PREVIEW && launchPreviewPinned;
  const showingSkillsCatalog = Boolean(
    state.initialized &&
    !previewingLaunch &&
    currentSessionTab?.kind === "skills",
  );
  const boardSessionTab =
    state.initialized &&
    !previewingLaunch &&
    currentSessionTab?.kind === "board"
      ? currentSessionTab
      : undefined;
  const showingTaskBoard = Boolean(boardSessionTab);
  const activeTitle = showingSkillsCatalog
    ? "Skills"
    : boardSessionTab
      ? sessionTabLabel(boardSessionTab, state)
      : workspaceMode
        ? workspaceModeTitle(workspaceMode)
        : activeThread?.preview || "新对话";
  const currentHour = useCurrentHour();
  const greetingContext: GreetingContext = resolveGreetingContext({
    activeThread,
    participants,
    activeContextKind: state.activeContext?.kind,
    activeProjectName: activeProject?.name,
  });
  const emptyThreadTitle = greetingFor(currentHour, greetingContext);
  const turns = activeThread?.turns ?? [];
  const latestAgentMessageID = latestAgentMessageItemID(turns);
  const activeContextCompositionEntries = activeThreadID
    ? contextCompositionEntries.filter((entry) => entry.threadID === activeThreadID)
    : [];
  const emptyConversation =
    !showingSkillsCatalog &&
    !showingTaskBoard &&
    turns.length === 0 &&
    activeContextCompositionEntries.length === 0;

  // Past user queries for the input-box hover popover. We collect them
  // in turn order, oldest first, so the popover mirrors the order in
  // which the user asked them. Empty / handoff / image-only items are
  // skipped — they have nothing to show in a quick-jump list.
  const pastQueries = useMemo<QueryHistoryEntry[]>(() => {
    const entries: QueryHistoryEntry[] = [];
    for (const turn of turns) {
      for (const item of turn.items) {
        const text = queryTextForUserItem(item);
        if (!text) {
          continue;
        }
        entries.push({ turnID: turn.id, itemID: item.id, text });
      }
    }
    return entries;
  }, [turns]);
  const showingWorkspaceMode =
    state.initialized && !previewingLaunch && workspaceMode !== undefined;
  const mainConversationDockVisible =
    Boolean(state.initialized) &&
    !previewingLaunch &&
    !emptyConversation &&
    !showingWorkspaceMode &&
    !splitConversation &&
    !showingSkillsCatalog &&
    !showingTaskBoard;

  useEffect(() => {
    // Diff tabs are scoped to the thread whose turn they came from: they
    // don't make sense to keep browsing once we've navigated away from
    // that thread (or away from the conversation view entirely), so prune
    // them eagerly instead of leaving stale diffs sitting in the tab strip.
    const isStaleDiffTab = (tab: WorkspaceViewTab): boolean =>
      tab.kind === "diff" &&
      (!activeThreadID ||
        tab.threadID !== activeThreadID ||
        showingWorkspaceMode ||
        showingSkillsCatalog ||
        emptyConversation);
    if (!workspaceViewTabs.some(isStaleDiffTab)) {
      return;
    }
    const activeTab = workspaceViewTabs.find((tab) => tab.id === workspaceActiveViewTabID);
    const closingActiveDiffTab = Boolean(activeTab && isStaleDiffTab(activeTab));
    closeWorkspaceViewTabsWhere(isStaleDiffTab);
    if (closingActiveDiffTab) {
      setRightPanelOpenWithMotion(false);
    }
  }, [
    activeThreadID,
    closeWorkspaceViewTabsWhere,
    emptyConversation,
    showingSkillsCatalog,
    showingWorkspaceMode,
    workspaceActiveViewTabID,
    workspaceViewTabs,
  ]);

  const {
    conversationScrollRef,
    scrollContentRef,
    splitPaneRefs,
    conversationPaneRef,
    dockComposerRef,
    dockComposerNode,
    scheduleStreamScroll,
    handleConversationScroll,
    scrollConversationToBottom,
    enableConversationAutoFollow,
    disableConversationAutoFollow,
    userScrolledAway
  } = useConversationScrollState({
    activeThreadID,
    activePane: state.activePane,
    splitConversation,
    primaryTurns: state.thread?.turns,
    secondaryTurns: state.secondaryThread?.turns,
    emptyConversation,
    previewingLaunch,
    showingWorkspaceMode: Boolean(showingWorkspaceMode),
    initialized: Boolean(state.initialized),
  });
  const conversationRailScrollContainer = useCallback((): HTMLElement | null => {
    if (splitConversation) {
      return splitPaneRefs.current[state.activePane] ?? null;
    }
    return conversationScrollRef.current;
  }, [conversationScrollRef, splitConversation, splitPaneRefs, state.activePane]);
  const handleTurnCollapseComplete = useCallback(() => {
    scheduleStreamScroll();
  }, [scheduleStreamScroll]);
  const canEditCachedThreadMessage = useStableCallback((thread: Thread) =>
    canShowHistoryEditButton(thread),
  );
  const handleCachedPaneForkMessage = useStableCallback(
    (thread: Thread, turnID: string, itemID: string) => {
      void forkThreadFromMessage(thread, turnID, itemID);
    },
  );
  const handleCachedPaneEditMessage = useStableCallback(
    (thread: Thread, turnID: string, item: ThreadItem) => {
      startEditingThreadMessageFromHistory(thread, turnID, item);
    },
  );
  const handleCachedPaneCancelEditMessage = useStableCallback(() => {
    cancelEditingThreadMessage();
  });
  const handleCachedPaneSubmitEditMessage = useStableCallback(
    (
      thread: Thread,
      turnID: string,
      item: ThreadItem,
      text: string,
      images: InputImage[],
      files: InputFile[],
    ) => {
      void submitEditedThreadMessageFromHistory(
        thread,
        turnID,
        item,
        text,
        images,
        files,
      );
    },
  );
  const handleCachedPaneNoticeAction = useStableCallback(
    (action: UserFacingErrorAction) => {
      handleNoticeAction(action);
    },
  );
  // Stable identities for every remaining CachedConversationPanes
  // callback prop. The component is React.memo'd; a single freshly
  // created arrow prop defeats the bailout and re-renders the full
  // cached turn lists on EVERY App state change — that full re-render
  // is the sidebar click lag (collapse a section → conversation pane
  // re-renders for nothing).
  const handleCachedPaneDismissContextComposition = useStableCallback(
    (id: string) => {
      dismissContextCompositionEntry(id);
    },
  );
  const handleCachedPaneDismissInstructions = useStableCallback((id: string) => {
    dismissInstructionFilesEntry(id);
  });
  const handleCachedPaneOpenAgent = useStableCallback((agent: Agent) => {
    void selectChildAgent(agent);
  });
  const handleCachedPaneOpenSubthread = useStableCallback(
    (thread: Thread, item: ThreadItem) => {
      openConversationSubthread(thread, item);
    },
  );
  const handleCachedPaneReact = useStableCallback(
    (thread: Thread, item: ThreadItem, reaction: string) => {
      // Reactions address a message by its seq; skip items that never got one
      // (e.g. a not-yet-persisted live snapshot). The chip lands on the bubble
      // via the message/mark notification the RPC triggers, so no optimistic
      // patch is needed here.
      const seq = item.seq;
      if (typeof seq !== "number" || seq < 0) {
        return;
      }
      void window.wuu.reactToMessage(thread.id, seq, reaction).catch((error) => {
        console.error("react to message failed", error);
      });
    },
  );
  const handleCachedPaneOpenFileDiff = useStableCallback(
    (thread: Thread, selection: TurnFileDiffSelection) => {
      openTurnFileDiffPanel(thread.id, selection);
    },
  );
  const rememberWorkspaceFileDirtyState = useStableCallback(
    (dirtyState: WorkspaceFileDirtyState) => {
      workspaceEditorDirtyRef.current = dirtyState;
    },
  );
  const confirmWorkspaceFileSwitch = useStableCallback(
    (context: RuntimeContext, nextPath: string): boolean => {
      const dirtyState = workspaceEditorDirtyRef.current;
      if (!dirtyState.dirty) {
        return true;
      }
      const nextRelativePath = normalizeWorkspaceFileSwitchPath(nextPath, context.cwd);
      if (dirtyState.root === context.cwd && dirtyState.path === nextRelativePath) {
        return true;
      }
      return window.confirm("当前文件有未保存修改，切换文件会丢失这些修改。仍要打开其他文件吗？");
    },
  );
  const openWorkspaceFile = useStableCallback((path: string): void => {
    // Stamp the same derived context the workspace panel's file tree/preview
    // are rooted at (workspacePanelContext), not the raw activeContext — for
    // a worktree-fork thread these differ, and activeWorkspaceFile's match
    // above must be comparing against the same context or the tab silently
    // stops highlighting/previewing once opened.
    const context = workspacePanelContext(
      appStateRef.current.activeContext,
      appStateRef.current.thread,
    );
    if (!context) {
      return;
    }
    if (!confirmWorkspaceFileSwitch(context, path)) {
      return;
    }
    const fileTab = createFileSessionTab(context, path);
    const outgoingDraft = currentPrimaryComposerDraft();
    openWorkspaceTool("files");
    setWorkspaceMode("files");
    setState((current) => ({
      ...persistActiveSessionTabDraft(current, outgoingDraft),
      sessionTabs: ensureSessionTab(current.sessionTabs, fileTab),
      activeSessionTabID: fileTab.id,
    }));
  });
  const sidebarProjectThreadsByProjectID = useMemo(() => {
    if (state.activeContext?.kind !== "project" || !state.activeProjectId) {
      return projectThreadsByProjectID;
    }
    const activeProject = state.projects.find(
      (project) => project.id === state.activeProjectId,
    );
    if (!activeProject) {
      return projectThreadsByProjectID;
    }
    return {
      ...projectThreadsByProjectID,
      [state.activeProjectId]: threadsForDesktopProject(
        state.threads,
        activeProject,
      ),
    };
  }, [
    projectThreadsByProjectID,
    state.activeContext?.kind,
    state.activeProjectId,
    state.projects,
    state.threads,
  ]);
  const sidebarThreads = useMemo(() => {
    const byID = new Map<string, Thread>();
    for (const thread of cachedScratchThreads) {
      byID.set(thread.id, thread);
    }
    for (const threads of Object.values(sidebarProjectThreadsByProjectID)) {
      for (const thread of threads) {
        byID.set(thread.id, thread);
      }
    }
    for (const thread of state.threads) {
      byID.set(thread.id, thread);
    }
    return sortThreads([...byID.values()]);
  }, [cachedScratchThreads, sidebarProjectThreadsByProjectID, state.threads]);
  const sidebarProjectThreadSummariesByProjectID = useMemo(() => {
    const next: Record<string, ThreadSummary[]> = {};
    for (const [projectID, threads] of Object.entries(
      sidebarProjectThreadsByProjectID,
    )) {
      // DM conversations live under the agent roster and group threads
      // under 群聊, never under the project tree — even when their cwd
      // happens to match a project root (group threads inherit the
      // runtime root as cwd). Both are hidden from the 对话 scratch
      // group already (scratchThreadSummaries) and from every project's
      // thread list here. Pinned ones continue to surface under 置顶
      // because that list reads from sidebarThreadSummaries directly,
      // bypassing this filter.
      next[projectID] = summarizeThreadsForSidebar(
        threads.filter(
          (thread) => !isDMThread(thread) && !isGroupThread(thread),
        ),
      );
    }
    return next;
  }, [sidebarProjectThreadsByProjectID]);
  const sidebarThreadSummaries = useMemo(
    () => summarizeThreadsForSidebar(sidebarThreads),
    [sidebarThreads],
  );
  const sidebarPinnedThreads = useMemo(
    () => pinnedThreadSummaries(sidebarThreadSummaries),
    [sidebarThreadSummaries],
  );
  const sidebarScratchThreads = useMemo(
    () => scratchThreadSummaries(sidebarThreadSummaries, state.projects),
    [sidebarThreadSummaries, state.projects],
  );
  // Group threads (chat-style-threads-design.md §3) live in the 群聊
  // section. groupThreadSummaries applies the shared pin/archive
  // semantics: pinned groups move under 置顶 (no duplicate here) and
  // archived groups leave the sidebar entirely.
  const sidebarGroupThreads = useMemo(
    () => groupThreadSummaries(sidebarThreadSummaries),
    [sidebarThreadSummaries],
  );
  // The scratch pseudo project lives at the top of the sidebar tree. It is
  // a synthetic DesktopProject (id = SCRATCH_PSEUDO_PROJECT_ID) whose
  // threads are the scratch conversations pulled out of
  // sidebarThreadSummaries above. path is intentionally "" — ThreadSidebar
  // special-cases the scratch pseudo id and skips its cwd-path filter.
  const scratchPseudoProject = useMemo<DesktopProject>(
    () => ({
      id: SCRATCH_PSEUDO_PROJECT_ID,
      name: "对话",
      path: "",
      created_at: new Date(0).toISOString(),
      updated_at: new Date(0).toISOString(),
    }),
    [],
  );
  const sidebarProjects = useMemo<DesktopProject[]>(
    () => [scratchPseudoProject, ...state.projects],
    [scratchPseudoProject, state.projects],
  );
  const sidebarThreadsByProjectID = useMemo(
    () => ({
      [SCRATCH_PSEUDO_PROJECT_ID]: sidebarScratchThreads,
      ...sidebarProjectThreadSummariesByProjectID,
    }),
    [sidebarScratchThreads, sidebarProjectThreadSummariesByProjectID],
  );
  const activeThreadReadOnly = Boolean(activeThread?.read_only);
  const activeThreadIsRunning = isStateActiveThreadRunning(state);
  const anyThreadIsRunning = isAnyThreadRunning(state) || viewContextSwitchPending;
  const runningProviderNames = useMemo(() => {
    const names = new Set<string>();
    for (const thread of [state.thread, state.secondaryThread, ...state.threads]) {
      const provider = thread?.model_provider.trim();
      if (provider && isThreadRunning(thread)) {
        names.add(provider);
      }
    }
    return Array.from(names);
  }, [state.thread, state.secondaryThread, state.threads]);
  // Aggregate participant IDs that are currently busy. Resident DM running
  // state is the baseline; active chat read receipts add participants that
  // are explicitly marked `seen: in_progress` for the visible message. Running
  // child agents dispatched inside some thread still do NOT light their
  // dispatcher's dot (ISSUE-12). See computeBusyParticipantIDs for the full
  // rationale. Named participants not in the set render as online. This drives
  // the sidebar roster and chat-style message avatars.
  const busyParticipantIDs = useMemo(
    () =>
      computeBusyParticipantIDs({
        threads: state.threads,
        marks: activeThreadMarks,
      }),
    [activeThreadMarks, state.threads],
  );
  // Chat read receipts + reactions render participant ids; resolve them to
  // names for the ring/chip hovers. chatReaderCount is the ring denominator —
  // the named roster (exact for #all; a slight overestimate for sub-groups,
  // whose ring may then not reach 100%). Both feed ChatThreadViewContainer.
  const resolveParticipantName = useMemo(() => {
    const byID = new Map<string, string>();
    for (const participant of participants) {
      if (participant.id) {
        byID.set(participant.id, participant.name?.trim() || participant.id);
      }
    }
    // The human has no roster row (rosters list only named agents), but its
    // emoji reactions are attributed to the stable "human" identity by
    // message/react; resolve it to "你" so the reaction chip hover reads right.
    return (id: string): string =>
      byID.get(id) ?? (id === "human" ? "你" : id);
  }, [participants]);
  const chatReaderCount = participants.length;
  // The active thread's dm_participant_id (when set) drives the highlight
  // in the agent roster. When the active thread is a DM the matching
  // participant row renders as active; for non-DM threads the highlight
  // collapses so no row is highlighted.
  const activeDMParticipantID = useMemo(() => {
    const id = state.thread?.dm_participant_id;
    return typeof id === "string" && id.length > 0 ? id : undefined;
  }, [state.thread?.dm_participant_id]);
  // Per-participant DM lookup so the roster row can drive a context menu
  // (pin/unpin DM, edit profile) without the sidebar having to refetch.
  // Walk the participant list explicitly so the sidebar knows which
  // participants have a DM thread even if state.threads hasn't been
  // refreshed yet (the picker in AppState.ts picks the latest non-archived
  // match for the given id). Values are summarized so the sidebar only
  // sees the cheap ThreadSummary shape it already expects.
  const dmThreadByParticipantID = useMemo(() => {
    const map = new Map<string, ThreadSummary>();
    for (const participant of participants) {
      const dmThread = findDMThread(state.threads, participant.id);
      if (dmThread) {
        map.set(participant.id, dmThread as unknown as ThreadSummary);
      }
    }
    return map;
  }, [participants, state.threads]);
  // Participants whose DM thread has a turn that the user has not yet
  // seen. Mirrors the `.has-unread` treatment used by thread rows so the
  // roster row gives the same visual cue without re-implementing the
  // helper.
  const unreadDMParticipantIDs = useMemo(() => {
    const ids = new Set<string>();
    for (const [participantID, thread] of dmThreadByParticipantID) {
      if (isThreadUnread(thread, state.lastViewedTurnByThreadID[thread.id])) {
        ids.add(participantID);
      }
    }
    return ids;
  }, [dmThreadByParticipantID, state.lastViewedTurnByThreadID]);
  const environmentPanelCanShow = Boolean(
    state.initialized &&
    !poppedOutMode &&
    !previewingLaunch &&
    !showingWorkspaceMode &&
    !rightPanelOpen &&
    !openSubthreadPanel &&
    !participantPanel,
  );
  const environmentPanelTargetVisible =
    environmentPanelCanShow &&
    (environmentPanelOpen ||
      (environmentPanelHasRoom &&
        !environmentPanelDismissed &&
        !emptyConversation));
  const environmentPanelVisible = environmentPanelTargetVisible;
  const subthreadPanelVisible = Boolean(openSubthreadPanel);
  const participantPanelVisible = Boolean(participantPanel);
  const environmentPanelMotionState: EnvironmentPanelMotionState =
    environmentPanelVisible ? "open" : "closing";
  const sessionTabsVisible = Boolean(
    state.initialized && !previewingLaunch && !poppedOutMode,
  );
  const sidebarVisible = !poppedOutMode;
  const shellClassName = `app-shell${poppedOutMode ? " popped-out-shell" : ""}${sidebarCollapsed ? " sidebar-collapsed" : ""}${
    sidebarCollapsed && sidebarDrawerPhase === "open" ? " sidebar-drawer-open" : ""
  }${
    sidebarCollapsed && sidebarDrawerPhase === "closing"
      ? " sidebar-drawer-closing"
      : ""
  }${
    sidebarAnimating ? " sidebar-animating" : ""
  }${rightPanelAnimating ? " right-panel-animating" : ""}${resizingSidebar ? " resizing-sidebar" : ""}${
    resizingRightPanel ? " resizing-right-panel" : ""
  }${rightPanelOpen ? " right-panel-open" : ""}${rightPanelGlobalized && rightPanelOpen ? " right-panel-globalized" : ""}${resizingSplit ? " resizing-split" : ""}${
    resizingThreadPanel ? " resizing-thread-panel" : ""
  }`;
  const shellStyle = {
    "--sidebar-width": `${effectiveSidebarWidth}px`,
    "--sidebar-open-width": `${sidebarWidth}px`,
    "--sidebar-motion-duration": `${SIDEBAR_MOTION_MS}ms`,
    "--workspace-panel-motion-duration": `${RIGHT_PANEL_MOTION_MS}ms`,
    "--workspace-right-panel-width": `${clampedWorkspaceRightPanelWidth}px`,
    "--thread-panel-width": `${clampedThreadPanelWidth}px`,
    "--conversation-split-left": `${splitLeftPercent}%`,
    "--environment-panel-width": ENVIRONMENT_PANEL_WIDTH_CSS,
    "--environment-panel-reserved-width": "372px",
    "--environment-panel-edge-gap": "18px",
    "--environment-panel-motion-duration": `${ENVIRONMENT_PANEL_MOTION_MS}ms`,
  } as CSSProperties;
  const pullRequestDisabledReason = pullRequestUnavailableReason(
    state.gitStatus,
  );
  const runDebugPhase = runDebugPhaseForState(state);

  useLayoutEffect(() => {
    if (environmentPanelVisible) {
      setEnvironmentPanelMounted(true);
      setEnvironmentPanelClosing(false);
      setEnvironmentPanelReserved(environmentPanelHasRoom);
      scheduleGitStatusRefresh(0);
      return;
    }
    if (!environmentPanelMounted) {
      setEnvironmentPanelReserved(false);
      return;
    }

    setEnvironmentPanelClosing(true);
    const timer = window.setTimeout(() => {
      setEnvironmentPanelMounted(false);
      setEnvironmentPanelClosing(false);
      setEnvironmentPanelReserved(false);
    }, ENVIRONMENT_PANEL_MOTION_MS);
    return () => window.clearTimeout(timer);
  }, [
    environmentPanelHasRoom,
    environmentPanelMounted,
    environmentPanelVisible,
  ]);

  useEffect(() => {
    if (!environmentPanelVisible && environmentPanelMenu) {
      setEnvironmentPanelMenu(null);
    }
  }, [environmentPanelMenu, environmentPanelVisible]);

  // Load the active chat thread's reply subthreads for the chat view's reply
  // badges / task 活动卡. Best-effort and decorative: any failure leaves the
  // last-known map untouched and never disrupts the message stream.
  useEffect(() => {
    if (!activeThreadID || !activeThreadIsChatStyle) {
      setChatSubthreads(null);
      return;
    }
    const listSub = window.wuu?.listConversationSubthreads;
    if (typeof listSub !== "function") {
      return;
    }
    let cancelled = false;
    const threadID = activeThreadID;
    void (async () => {
      try {
        const result = await listSub(threadID);
        if (cancelled) {
          return;
        }
        const byAnchor = new Map<string, ConversationSubthread>();
        for (const sub of result.subthreads ?? []) {
          if (sub.anchor_item_id) {
            byAnchor.set(sub.anchor_item_id, sub);
          }
        }
        setChatSubthreads({ threadID, byAnchor });
      } catch {
        // Decorative badges — keep the previous map rather than clearing it.
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [
    activeThreadID,
    activeThreadIsChatStyle,
    activeThreadTurnCount,
    chatSubthreadsNonce,
  ]);

  const activeChatSubthreadsByAnchor =
    chatSubthreads && chatSubthreads.threadID === activeThreadID
      ? chatSubthreads.byAnchor
      : undefined;

  const handleCloseFilePreview = useCallback((): void => {
    setRightPanelFilePath(undefined);
    setEnvironmentPanelMenu(null);
  }, []);

  // Mark the active thread's latest completed turn as viewed so the sidebar
  // and session tab strip stop showing the "has-unread" dot. This effect is
  // the single source of truth for advancing `lastViewedTurnByThreadID`; any
  // state change that re-renders the conversation (tab switch, new turn for
  // the active thread) reaches here. Running threads are skipped so a
  // mid-stream turn does not get pinned as the viewed ID.
  useEffect(() => {
    const tab = state.sessionTabs.find(
      (candidate) => candidate.id === state.activeSessionTabID,
    );
    if (tab?.kind !== "thread") return;
    const thread = threadForTab(state, tab.threadID);
    if (!thread) return;
    if (isThreadRunning(thread)) return;
    setState((current) => {
      const next = markThreadTurnsViewed(current, thread.id);
      return next === current ? current : next;
    });
  }, [state.activeSessionTabID, state.thread, state.threads]);

  function renderComposer(variant: ComposerVariant): JSX.Element {
    const tokenSpeed = activeTurnTokenSpeedSnapshot(
      state,
      activeThread ? activeTurnIDForThread(activeThread) : undefined,
    );
    // Drives the composer context meter. Existing threads use the latest
    // known usage; a brand-new session falls back to the current runtime
    // window so the meter can render at 0% before the first turn.
    const contextUsage = latestContextUsageForThread(state, activeThread, {
      model: state.initialized?.model,
      contextWindowTokens:
        state.initialized?.advanced_settings?.context_window_tokens,
    });
    const streamStatus = turnStreamStatusForThread(state, activeThread);
    return (
      <Composer
        variant={variant}
        containerRef={variant === "dock" ? dockComposerRef : undefined}
        prompt={prompt}
        setPrompt={setPrompt}
        files={composerFiles}
        images={composerImages}
        // Chat-style threads (DM/group) never surface the queue strip —
        // pending sends render as chat bubbles in ChatThreadView instead —
        // and the send button stays a send button while the agent replies
        // (chat semantics, issue #10). Work threads keep the queue UI.
        queuedMessages={activeThreadIsChatStyle ? [] : queuedMessages}
        guideMessages={guideMessages}
        running={
          activeThreadIsChatStyle
            ? viewContextSwitchPending
            : (!activeThreadReadOnly && activeThreadIsRunning) ||
              viewContextSwitchPending
        }
        runtimeControlsDisabled={
          (!activeThreadReadOnly && activeThreadIsRunning) ||
          viewContextSwitchPending
        }
        tokensPerSecond={tokenSpeed.tokensPerSecond}
        tokenSpeedSampledAt={tokenSpeed.sampledAt}
        tokenSpeedSource={tokenSpeed.source}
        contextUsage={contextUsage}
        status={
          activeThreadReadOnly
            ? activeThreadIsRunning
              ? "子任务运行中"
              : "子任务会话只读"
            : streamStatus?.text ?? state.status
        }
        statusLiveProgress={
          activeThreadReadOnly
            ? false
            : streamStatus?.liveProgress
        }
        readOnly={activeThreadReadOnly}
        initialized={composerInitialized}
        gitStatus={state.gitStatus}
        projects={state.projects}
        activeContext={state.activeContext}
        activeProject={activeProject}
        compactDisabledReason={
          !activeThread
            ? "先打开一个对话"
            : activeThreadIsGroup
              ? "群聊暂不支持上下文压缩"
              : undefined
        }
        codexModels={codexModels}
        codexRuntimeMenu={codexRuntimeMenu}
        codexRuntimeRef={codexRuntimeRef}
        menuOpen={runtimeMenuOpen}
        accessMenuOpen={accessMenuOpen}
        branchMenuOpen={branchMenuOpen}
        menuRef={runtimeMenuRef}
        accessMenuRef={accessMenuRef}
        projectFilter={projectFilter}
        setProjectFilter={setProjectFilter}
        onToggleMenu={() => {
          setAccessMenuOpen(false);
          setBranchMenuOpen(false);
          setCodexRuntimeMenu(null);
          setRuntimeMenuOpen((open) => !open);
        }}
        onToggleAccessMenu={() => {
          setRuntimeMenuOpen(false);
          setBranchMenuOpen(false);
          setCodexRuntimeMenu(null);
          setAccessMenuOpen((open) => !open);
        }}
        onToggleBranchMenu={() => {
          setRuntimeMenuOpen(false);
          setAccessMenuOpen(false);
          setCodexRuntimeMenu(null);
          setBranchMenuOpen((open) => !open);
        }}
        onToggleCodexRuntimeMenu={toggleCodexRuntimeMenu}
        onSelectRuntimeModel={(provider, model, variant) =>
          void selectRuntimeModel(provider, model, variant)
        }
        onSelectRuntimeEffort={(nextVariant) =>
          void selectRuntimeEffort(nextVariant)
        }
        onSelectPermissionMode={(mode) =>
          void selectPermissionMode(mode)
        }
        onOpenSettings={() => {
          closeProjectMenus();
          setSettingsInitialPage("providers");
          setSettingsOpen(true);
        }}
        onOpenMemorySettings={() => openMemorySettings()}
        onOpenSkillsCatalog={openSkillsTab}
        onSelectProject={(id) => void selectProjectForNewThread(id)}
        onSelectNoProject={() => void useNoProject(false)}
        onSelectGitBranch={(branch) => void checkoutBranch(branch)}
        onCreateProject={() => void createBlankProject()}
        onOpenProject={() => void chooseProjectFolder()}
        onStartNewThread={() => void startNewThread({ resetToNoProject: true })}
        onOpenWorkspaceTool={openWorkspaceTool}
        onOpenContextComposition={openContextComposition}
        onCompactContext={() => void compactActiveThread()}
        onOpenInstructions={openInstructions}
        onPasteAttachmentFiles={(files) => void attachComposerAttachmentFiles(files)}
        onRemoveFile={removeComposerFile}
        onRemoveImage={removeComposerImage}
        onRemoveQueuedMessage={removeQueuedMessage}
        onRemoveGuideMessage={removeGuideMessage}
        onGuideQueuedMessage={(id) => void guideQueuedMessage(id)}
        onEditQueuedMessage={(id) => void editQueuedMessage(id)}
        onEditGuideMessage={(id) => void editGuideMessage(id)}
        onSend={() => void sendPrompt()}
        onInterrupt={() => void interrupt()}
        goalSummary={goalSummary}
        onEditGoal={editGoalText}
        onPauseGoal={pauseCurrentGoal}
        onResumeGoal={resumeCurrentGoal}
        onClearGoal={clearCurrentGoal}
        queryHistorySessionID={activeThread?.id}
        queryHistory={queryTextsForThread(activeThread)}
        participants={participants}
        chatFocusValue={
          activeThread && (isDMThread(activeThread) || isGroupThread(activeThread))
            ? chatFocusValueForThread(
                activeThread,
                chatFocusOverrides,
                state.projects,
              )
            : undefined
        }
        onSelectChatFocus={(value) => {
          const threadID = activeThread?.id;
          if (!threadID) {
            return;
          }
          setChatFocusOverrides((current) => ({
            ...current,
            [threadID]: value,
          }));
        }}
        groupMembers={
          activeThreadIsGroup ? (activeThread?.members ?? []) : undefined
        }
        onOpenGroupInfo={openEnvironmentPanel}
      />
    );
  }

  // Lift the open reply subthread (cth) into its own window. threadID is the
  // PARENT group thread (the cth's home, needed for runtime routing); the new
  // window renders the cth via the SAME ConversationSubthreadPanel + composer
  // the in-window split uses. On success the in-window panel closes so the cth
  // lives in exactly one place.
  async function popOutSubthread(
    threadID: string,
    subthreadID: string,
    context: RuntimeContext,
  ): Promise<void> {
    if (poppingOutSubthreadIDsRef.current.has(subthreadID)) {
      return;
    }
    poppingOutSubthreadIDsRef.current.add(subthreadID);
    try {
      await window.wuu.popOutSession({
        kind: "subthread",
        threadID,
        subthreadID,
        context,
      });
      setOpenSubthreadPanel((current) =>
        current?.subthread?.id === subthreadID ? undefined : current,
      );
    } catch (error) {
      setState((current) => ({
        ...current,
        status:
          error instanceof Error ? error.message : "open detached window failed",
      }));
    } finally {
      poppingOutSubthreadIDsRef.current.delete(subthreadID);
    }
  }

  function handleEmptyStateHint(action: EmptyStateHintAction): void {
    if (action.kind === "openSettings") {
      closeProjectMenus();
      setSettingsInitialPage("providers");
      setSettingsOpen(true);
    }
  }

  function closeProjectMenus(): void {
    setProjectMenuOpen(false);
    setRuntimeMenuOpen(false);
    setAccessMenuOpen(false);
    setCodexRuntimeMenu(null);
    setBranchMenuOpen(false);
    setEnvironmentPanelMenu(null);
    setSettingsOpen(false);
    setProjectFilter("");
  }

  const {
    checkoutBranch,
    refreshGitStatus,
    scheduleGitStatusRefresh,
    createAndCheckoutBranch,
    commitEnvironmentChanges,
    createEnvironmentPullRequest,
    toggleEnvironmentPanel,
    openEnvironmentPanel,
    closeEnvironmentPanel,
  } = createEnvironmentActions({
    getAppState: () => appStateRef.current,
    setAppState: setState,
    getAnyThreadIsRunning: () => anyThreadIsRunning,
    closeProjectMenus,
    setEnvironmentPanelOpen,
    setEnvironmentPanelDismissed,
    setEnvironmentPanelMenu,
    closeRuntimeMenus: () => {
      setRuntimeMenuOpen(false);
      setAccessMenuOpen(false);
      setBranchMenuOpen(false);
      setCodexRuntimeMenu(null);
    },
    getEnvironmentPanelVisible: () => environmentPanelVisible,
    environmentPanelContainsActiveElement: () => {
      const activeElement = document.activeElement;
      return (
        activeElement instanceof HTMLElement &&
        environmentPanelRef.current?.contains(activeElement) === true
      );
    },
    focusEnvironmentToggle: () =>
      environmentToggleRef.current?.focus({ preventScroll: true }),
    gitRefreshTimerRef,
    gitRefreshInFlightRef,
    gitRefreshQueuedRef,
  });

  function canShowHistoryEditButton(thread: Thread): boolean {
    return (
      !thread.read_only &&
      !isThreadRunning(thread) &&
      !localDemoThreadsRef.current.has(thread.id) &&
      !threadHasPendingComposerMessages(thread.id)
    );
  }

  function restoreSessionTabComposerDraft(tab: SessionTab): void {
    restorePrimaryComposerDraft(cloneSessionTabDraft(tab));
    setSplitComposerDrafts(initialSplitComposerDrafts());
  }

  function restoreLoadedRuntimeComposerDraft(
    loadedState: Partial<AppState>,
    carryDraft?: ComposerDraftState,
  ): void {
    const context = loadedState.activeContext;
    if (!context) {
      return;
    }
    // When a draft is being carried across the switch (see
    // applyLoadedRuntimeWithDraftCarry), the composer should keep showing
    // exactly what the user had typed rather than whatever the target
    // context's own tab already held.
    if (carryDraft) {
      restorePrimaryComposerDraft(carryDraft);
      setSplitComposerDrafts(initialSplitComposerDrafts());
      return;
    }
    restoreSessionTabComposerDraft(
      sessionTabForLoadedRuntime(
        appStateRef.current.sessionTabs,
        context,
        loadedState.thread,
      ),
    );
  }

  function nextDraftSessionTab(context: RuntimeContext): SessionTab {
    draftSessionTabCounterRef.current += 1;
    return createDraftSessionTab(
      `draft:${Date.now()}:${draftSessionTabCounterRef.current}`,
      context,
    );
  }

  const {
    openProject,
    selectProjectForNewThread,
    startNewThreadForProject,
    createBlankProject,
    chooseProjectFolder,
    removeProject,
    relocateProject,
    useNoProject,
  } = createProjectRuntimeActions({
    getAppState: () => appStateRef.current,
    setAppState: setState,
    getPrimaryComposerDraft: currentPrimaryComposerDraft,
    clearPrimaryComposerDraft: () =>
      restorePrimaryComposerDraft(emptyComposerDraft()),
    restoreLoadedRuntimeComposerDraft,
    nextDraftSessionTab,
    closeProjectMenus,
    setArchiveConfirmThreadID,
    setWorkspaceMode,
    beginViewSwitch,
    finishViewSwitch,
    cancelViewSwitch,
    loadRuntime,
  });

  const {
    selectThread,
    selectProjectThread,
    activateThread,
    selectProjectChildAgent,
    selectChildAgent,
  } = createThreadActivationActions({
    getAppState: () => appStateRef.current,
    setAppState: setState,
    getActiveThreadID: () => activeThreadID,
    getPendingViewSwitch: () => pendingViewSwitch,
    getPrimaryComposerDraft: currentPrimaryComposerDraft,
    restorePrimaryComposerDraft,
    resetSplitComposerDrafts: () =>
      setSplitComposerDrafts(initialSplitComposerDrafts()),
    getLocalDemoThread: (threadID) => localDemoThreadsRef.current.get(threadID),
    getSidebarThreads: () => sidebarThreads,
    getSidebarProjectThreadsByProjectID: () =>
      sidebarProjectThreadsByProjectID,
    setArchiveConfirmThreadID,
    setWorkspaceMode,
    beginViewSwitch,
    beginInstantThreadSwitch,
    finishViewSwitch,
    cancelViewSwitch,
    isCurrentViewSwitchRequest,
    loadRuntime,
    selectRuntimeContext,
  });

  const {
    selectSessionTab,
    closeSessionTab,
    closeSessionTabs,
    startNewThread,
    reorderSessionTabs,
    popOutSessionTab,
  } = createSessionTabActions({
    getAppState: () => appStateRef.current,
    setAppState: setState,
    getPrimaryComposerDraft: currentPrimaryComposerDraft,
    restorePrimaryComposerDraft,
    clearPrimaryComposerDraft: () =>
      restorePrimaryComposerDraft(emptyComposerDraft()),
    resetSplitComposerDrafts: () =>
      setSplitComposerDrafts(initialSplitComposerDrafts()),
    nextDraftSessionTab,
    selectThread,
    useNoProject,
    setArchiveConfirmThreadID,
    setWorkspaceMode,
    poppingOutTabIDsRef,
    beginViewSwitch,
    finishViewSwitch,
    cancelViewSwitch,
    loadRuntime,
    selectRuntimeContext,
  });

  const {
    toggleThreadPinned,
    renameThread,
    removeThreadMemberByID,
    addThreadMemberByID,
    archiveThread,
    deleteThread,
    toggleSubagentPinned,
    archiveSubagent,
  } = createThreadMutationActions({
    getAppState: () => appStateRef.current,
    setAppState: setState,
    getActiveThreadID: () => activeThreadID,
    getArchiveConfirmThreadID: () => archiveConfirmThreadID,
    getArchiveConfirmSubagentID: () => archiveConfirmSubagentID,
    setArchiveConfirmThreadID,
    setArchiveConfirmSubagentID,
    localDemoThreadsRef,
    nextDraftSessionTab,
    clearPrimaryComposerDraft: () =>
      restorePrimaryComposerDraft(emptyComposerDraft()),
    resetSplitComposerDrafts: () =>
      setSplitComposerDrafts(initialSplitComposerDrafts()),
    updateCachedSidebarThread,
    removeCachedSidebarThread,
    clearThreadPendingComposerMessages,
  });

  const {
    updateRuntimeSettings,
    updateAdvancedSettings,
    updateGeneralSettings,
    removeProvider,
    toggleCodexRuntimeMenu,
    selectRuntimeModel,
    selectRuntimeEffort,
    selectPermissionMode,
    interrupt,
    handleNoticeAction,
    interruptPane,
  } = createRuntimeSettingsActions({
    getAppState: () => appStateRef.current,
    setAppState: setState,
    getViewContextSwitchPending: () => viewContextSwitchPending,
    getCodexModels: () => codexModels,
    setCodexModels,
    setRuntimeMenuOpen,
    setAccessMenuOpen,
    setBranchMenuOpen,
    setCodexRuntimeMenu,
    setSelectedPermissionMode,
    setSettingsInitialPage,
    setSettingsOpen,
    clearThreadPendingComposerMessages,
    writeClipboardText: (text) => navigator.clipboard.writeText(text),
  });

  const {
    openSkillsTab,
    dismissContextCompositionEntry,
    dismissInstructionFilesEntry,
    openInstructions,
    openContextComposition,
    openParticipantProfile,
    openParticipantDM,
    createGroupThread,
    openTaskBoardTab,
    openTaskFromBoard,
    openMemorySettings,
  } = createCollaborationActions({
    getAppState: () => appStateRef.current,
    setAppState: setState,
    getActiveTitle: () => activeTitle,
    getPrimaryComposerDraft: currentPrimaryComposerDraft,
    setSplitComposerDrafts,
    setPrompt,
    setComposerImages,
    setComposerFiles,
    setArchiveConfirmThreadID,
    setWorkspaceMode,
    cancelViewSwitch,
    activateThread,
    selectThread,
    selectSessionTab,
    closeConversationSearch,
    closeEnvironmentPanel,
    setOpenSubthreadPanel,
    setRightPanelOpenWithMotion,
    openParticipantProfilePanel,
    setContextCompositionEntries,
    setInstructionFilesEntries,
    scheduleStreamScroll,
    openingDMParticipantIDRef,
    openConversationSubthreadByID,
    closeProjectMenus,
    setSettingsMemoryFocusID,
    setSettingsInitialPage,
    setSettingsOpen,
  });

  const {
    seedAgentTreeDemo,
    seedConversationFixture,
    seedPlanPanelDebug,
    activateConversationPane,
    closeConversationPane,
  } = createConversationDemoPaneActions({
    getAppState: () => appStateRef.current,
    setAppState: setState,
    localDemoThreadsRef,
    cancelViewSwitch,
    setArchiveConfirmThreadID,
    setWorkspaceMode,
    setPrompt,
    setComposerImages,
    setComposerFiles,
    setSplitComposerDrafts,
    moveSplitDraftToGlobalComposer,
    setRunDebugOpen,
    setEnvironmentPanelOpen,
    setEnvironmentPanelDismissed,
    setEnvironmentPanelMenu,
  });

  const {
    choosePendingFork,
    forkThreadFromMessage,
    startEditingThreadMessageFromHistory,
    cancelEditingThreadMessage,
    submitEditedThreadMessageFromHistory,
  } = createConversationHistoryActions({
    appStateRef,
    setAppState: setState,
    localDemoThreadsRef,
    getPendingFork: () => pendingFork,
    setPendingFork,
    setHistoryMessageEdit,
    setArchiveConfirmThreadID,
    getWorkspaceMode: () => workspaceMode,
    getPrompt: () => prompt,
    getComposerImages: () => composerImages,
    getComposerFiles: () => composerFiles,
    getSplitComposerDrafts: () => splitComposerDrafts,
    setPrompt,
    setComposerImages,
    setComposerFiles,
    setSplitComposerDrafts,
    restorePrimaryComposerDraft,
    closeConversationSearch,
    clearEnvironmentDialog: () => setEnvironmentDialog(null),
    scheduleGitStatusRefresh,
    enableConversationAutoFollow,
    threadHasPendingComposerMessages,
    sendComposerMessageToThread,
    worktreeForkNonGitReason: WORKTREE_FORK_NON_GIT_REASON,
  });

  async function sendPrompt(): Promise<void> {
    if (viewSwitchPending) {
      return;
    }
    const message = createComposerMessage(prompt, composerImages, composerFiles);
    const currentState = appStateRef.current;
    const targetThread = activeThreadForState(currentState);
    if (targetThread?.read_only) {
      setState((current) => ({ ...current, status: "子任务会话只读" }));
      return;
    }
    if (!message || !currentState.activeContext || !currentState.initialized) {
      return;
    }
    const queuedEditTarget = queuedMessageEditTargetRef.current;
    if (queuedEditTarget) {
      await updateQueuedComposerMessage(message, queuedEditTarget);
      return;
    }
    setPrompt("");
    setComposerImages([]);
    setComposerFiles([]);
    // DM threads intentionally share this exact path: a resident named
    // agent's DM is a normal multi-turn thread (turn/start), not a
    // spawn-per-message shell. See docs/plans/2026-07-03-resident-named-agents.md §7.1.
    //
    // Chat send semantics (issue #10):
    // - Group threads never queue. The server records every group send as a
    //   completed turn with no provider call (and rejects turn/queue for
    //   groups outright), so a busy-looking state must not divert the
    //   message into the queue path — send straight away.
    // - DM threads still reuse turn/queue's reliable delivery while the
    //   resident is mid-turn, but the pending message renders as a chat
    //   bubble in ChatThreadView instead of the composer queue strip.
    if (
      isStateActiveThreadRunning(currentState) &&
      !(targetThread && isGroupThread(targetThread))
    ) {
      const queued = await queueComposerMessage(message, targetThread);
      if (!queued) {
        setPrompt(message.text);
        setComposerImages(message.images);
        setComposerFiles(message.files);
      }
      return;
    }
    await sendComposerMessage(message, true);
  }

  async function compactActiveThread(): Promise<void> {
    if (viewSwitchPending) {
      return;
    }
    const currentState = appStateRef.current;
    const targetThread = activeThreadForState(currentState);
    if (!currentState.activeContext || !currentState.initialized) {
      return;
    }
    if (!targetThread) {
      setState((current) => ({ ...current, status: "先打开一个对话" }));
      return;
    }
    if (targetThread.read_only) {
      setState((current) => ({ ...current, status: "子任务会话只读" }));
      return;
    }
    if (isGroupThread(targetThread)) {
      setState((current) => ({
        ...current,
        status: "群聊没有可压缩的模型上下文",
      }));
      return;
    }
    if (isStateActiveThreadRunning(currentState)) {
      setState((current) => ({ ...current, status: "当前任务运行中" }));
      return;
    }

    enableConversationAutoFollow();
    resetRunDebugEvents({
      source: "client",
      method: "client/compact",
      detail: "开始压缩上下文",
      tone: "running",
      threadID: targetThread.id,
    });
    appStateRef.current = {
      ...currentState,
      running: true,
      status: "正在压缩上下文",
    };
    setState((current) => ({
      ...current,
      running: true,
      status: "正在压缩上下文",
    }));

    const optimisticTurn = createOptimisticCompactTurn(Date.now());
    const optimisticTurnID = optimisticTurn.id;
    appStateRef.current = updateThreadByID(
      appStateRef.current,
      targetThread.id,
      (thread) => upsertTurn(thread, optimisticTurn),
      { running: true, status: "正在压缩上下文" },
    );
    setState((current) =>
      updateThreadByID(
        current,
        targetThread.id,
        (thread) => upsertTurn(thread, optimisticTurn),
        { running: true, status: "正在压缩上下文" },
      ),
    );

    try {
      const result = await window.wuu.compactThread(targetThread.id);
      appStateRef.current = updateThreadByID(
        appStateRef.current,
        targetThread.id,
        (thread) =>
          replaceOptimisticTurn(
            thread,
            optimisticTurnID,
            result.turn,
            upsertTurn,
          ),
        { running: true, status: "正在压缩上下文" },
      );
      setState((current) =>
        updateThreadByID(
          current,
          targetThread.id,
          (thread) =>
            replaceOptimisticTurn(
              thread,
              optimisticTurnID,
              result.turn,
              upsertTurn,
            ),
          { running: true, status: "正在压缩上下文" },
        ),
      );
      appendRunDebugEvent({
        source: "client",
        method: "thread/compact/start response",
        detail: "服务端已接受压缩请求",
        tone: "running",
        threadID: targetThread.id,
        turnID: result.turn.id,
      });
    } catch (error) {
      const rawMessage = rawErrorMessage(error, "compact failed");
      const errorMessage = statusMessageForError(rawMessage, "compact failed");
      appendRunDebugEvent({
        source: "client",
        method: "thread/compact/start failed",
        detail: rawMessage,
        tone: "error",
        threadID: targetThread.id,
      });
      const failedTurn = failOptimisticCompactTurn(
        optimisticTurn,
        rawMessage,
        Date.now(),
      );
      appStateRef.current = {
        ...updateThreadByID(
          appStateRef.current,
          targetThread.id,
          (thread) =>
            replaceOptimisticTurn(
              thread,
              optimisticTurnID,
              failedTurn,
              upsertTurn,
            ),
        ),
        running: false,
        status: errorMessage,
      };
      setState((current) =>
        updateThreadByID(
          current,
          targetThread.id,
          (thread) =>
            replaceOptimisticTurn(
              thread,
              optimisticTurnID,
              failedTurn,
              upsertTurn,
            ),
          { running: false, status: errorMessage },
        ),
      );
    }
  }

  async function updateQueuedComposerMessage(
    message: QueuedComposerMessage,
    target: QueuedMessageEditTarget,
  ): Promise<boolean> {
    const currentState = appStateRef.current;
    const targetThread = threadForTab(currentState, target.threadID);
    const text = message.text.trim();
    const imageCount = message.images.length;
    const files = inputFilesFromComposer(message.files);
    if (
      (!text && imageCount === 0 && files.length === 0) ||
      !targetThread ||
      targetThread.read_only ||
      !currentState.activeContext ||
      !currentState.initialized ||
      viewSwitchPending
    ) {
      return false;
    }
    try {
      const encodedImages = await awaitComposerImages(message.images);
      const images = inputImagesFromComposer(encodedImages);
      const result = await window.wuu.updateQueuedTurn(
        target.threadID,
        target.queueID,
        text,
        images,
        files,
      );
      if (!result.ok) {
        setQueuedMessageEditTargetNow(undefined);
        setState((current) => ({
          ...current,
          status: "排队消息已开始处理，无法保存编辑",
        }));
        return false;
      }
      const updatedMessage: QueuedComposerMessage = {
        ...message,
        id: result.queued.id || target.queueID,
        images: encodedImages,
      };
      updateThreadPendingComposerMessages(target.threadID, (previous) => ({
        ...previous,
        queued: previous.queued.map((queued) =>
          queued.id === target.queueID ? updatedMessage : queued,
        ),
      }));
      setQueuedMessageEditTargetNow(undefined);
      setPrompt("");
      setComposerImages([]);
      setComposerFiles([]);
      setState((current) => ({
        ...current,
        status: "已更新排队消息",
      }));
      return true;
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "保存排队编辑失败",
      }));
      return false;
    }
  }

  async function queueComposerMessage(
    message: QueuedComposerMessage,
    targetThread = activeThreadForState(appStateRef.current),
  ): Promise<boolean> {
    const currentState = appStateRef.current;
    const text = message.text.trim();
    const imageCount = message.images.length;
    const files = inputFilesFromComposer(message.files);
    if (
      (!text && imageCount === 0 && files.length === 0) ||
      !targetThread ||
      targetThread.read_only ||
      !currentState.activeContext ||
      !currentState.initialized ||
      viewSwitchPending
    ) {
      return false;
    }
    try {
      const encodedImages = await awaitComposerImages(message.images);
      const images = inputImagesFromComposer(encodedImages);
      const result = await window.wuu.queueTurn(
        targetThread.id,
        text,
        images,
        message.id,
        files,
        selectedPermissionMode,
      );
      enqueueComposerMessage(targetThread.id, {
        ...message,
        id: result.queued.id || message.id,
        images: encodedImages,
      });
      return true;
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "排队失败",
      }));
      return false;
    }
  }

  async function sendComposerMessage(
    message: QueuedComposerMessage,
    restoreDraftOnError = false,
  ): Promise<boolean> {
    // Captured before any await: on a brand-new conversation the thread
    // itself is created over IPC first, and the optimistic turn's live
    // timer must count from the user's click, not from when that
    // round-trip finishes.
    const sendClickedAtMs = Date.now();
    const currentState = appStateRef.current;
    const targetThread = activeThreadForState(currentState);
    const targetPane: ConversationPaneID =
      currentState.activePane === "secondary" && currentState.secondaryThread
        ? "secondary"
        : "primary";
    const text = message.text.trim();
    const imageCount = message.images.length;
    const files = inputFilesFromComposer(message.files);
    if (
      (!text && imageCount === 0 && files.length === 0) ||
      !currentState.activeContext ||
      !currentState.initialized ||
      targetThread?.read_only ||
      viewSwitchPending ||
      // Group threads accept concurrent sends: each turn/start lands a
      // completed chat turn server-side, so a transient running state
      // (a send round-trip still in flight) must not block the next
      // message (issue #10).
      (isStateActiveThreadRunning(currentState) &&
        !(targetThread && isGroupThread(targetThread)))
    ) {
      return false;
    }
    const activeContext = currentState.activeContext;
    enableConversationAutoFollow();
    resetRunDebugEvents({
      source: "client",
      method: "client/send",
      detail: composerSubmissionDetail(imageCount, files.length),
      tone: "running",
      threadID: targetThread?.id,
    });
    appStateRef.current = {
      ...currentState,
      running: true,
      status: "正在发送请求",
    };
    setState((current) => ({
      ...current,
      running: true,
      status: "正在发送请求",
    }));
    let optimisticTurnID: string | undefined;
    let optimisticThreadID: string | undefined;
    try {
      const thread =
        targetThread ??
        requireThread(
          await window.wuu.startThread(),
          "thread/start did not return a thread",
        );
      appStateRef.current = {
        ...setThreadForPane(appStateRef.current, targetPane, thread),
        activePane: targetPane,
        allowThreadAutoActivation: true,
        sessionTabs:
          targetPane === "primary"
            ? bindActiveSessionTabToThread(
                appStateRef.current.sessionTabs,
                appStateRef.current.activeSessionTabID,
                thread,
                activeContext,
              )
            : appStateRef.current.sessionTabs,
        activeSessionTabID:
          targetPane === "primary"
            ? threadSessionTabID(thread.id)
            : appStateRef.current.activeSessionTabID,
        threads: upsertThread(appStateRef.current.threads, thread),
      };
      setState((current) => ({
        ...setThreadForPane(current, targetPane, thread),
        activePane: targetPane,
        allowThreadAutoActivation: true,
        sessionTabs:
          targetPane === "primary"
            ? bindActiveSessionTabToThread(
                current.sessionTabs,
                current.activeSessionTabID,
                thread,
                activeContext,
              )
            : current.sessionTabs,
        activeSessionTabID:
          targetPane === "primary"
            ? threadSessionTabID(thread.id)
            : current.activeSessionTabID,
        threads: upsertThread(current.threads, thread),
      }));
      // Insert an optimistic in_progress turn before the IPC round-trip so
      // the live "正在回复/处理" timer starts at the user's click moment
      // instead of waiting for the server's first turn notification. The
      // placeholder is replaced (or dropped on error) once the real turn
      // arrives or the request fails.
      const optimisticTurn = createOptimisticTurn(message, sendClickedAtMs);
      optimisticTurnID = optimisticTurn.id;
      optimisticThreadID = thread.id;
      appStateRef.current = updateThreadByID(
        appStateRef.current,
        thread.id,
        (currentThread) => upsertTurn(currentThread, optimisticTurn),
      );
      setState((current) =>
        updateThreadByID(
          current,
          thread.id,
          (currentThread) => upsertTurn(currentThread, optimisticTurn),
        ),
      );
      const encodedImages = await awaitComposerImages(message.images);
      const images = inputImagesFromComposer(encodedImages);
      const result = await window.wuu.startTurn(
        thread.id,
        text,
        images,
        files,
        selectedPermissionMode,
        mentionedParticipantIDsFromText(text, participants),
        // Only defined when the chat-focus chip changed this session and
        // differs from the thread's last-known focus_workspace; plain
        // sends carry nothing extra.
        focusWorkspaceSendValue(thread, chatFocusOverrides[thread.id]),
      );
      setState((current) =>
        updateThreadByID(
          setThreadForPane(current, targetPane, thread),
          thread.id,
          (currentThread) =>
            replaceOptimisticTurn(
              currentThread,
              optimisticTurnID ?? result.turn.id,
              result.turn,
              upsertTurn,
            ),
        ),
      );
      appendRunDebugEvent({
        source: "client",
        method: "turn/start response",
        detail: "服务端已接受本轮请求",
        tone: "running",
        threadID: thread.id,
        turnID: result.turn.id,
      });
    } catch (error) {
      const rawMessage = rawErrorMessage(error, "send failed");
      const errorMessage = statusMessageForError(rawMessage, "send failed");
      appendRunDebugEvent({
        source: "client",
        method: "turn/start failed",
        detail: rawMessage,
        tone: "error",
        threadID: targetThread?.id,
      });
      const droppedState =
        optimisticTurnID && optimisticThreadID
          ? updateThreadByID(
              appStateRef.current,
              optimisticThreadID,
              (currentThread) =>
                dropOptimisticTurn(currentThread, optimisticTurnID),
            )
          : appStateRef.current;
      appStateRef.current = {
        ...droppedState,
        running: false,
        status: errorMessage,
      };
      setState((current) => ({
        ...(optimisticTurnID && optimisticThreadID
          ? updateThreadByID(
              current,
              optimisticThreadID,
              (currentThread) =>
                dropOptimisticTurn(currentThread, optimisticTurnID),
            )
          : current),
        running: false,
        status: errorMessage,
      }));
      if (restoreDraftOnError) {
        setPrompt(message.text);
        setComposerImages(message.images);
        setComposerFiles(message.files);
      }
      return false;
    }
    return true;
  }

  async function sendPromptForPane(pane: ConversationPaneID): Promise<void> {
    if (viewSwitchPending) {
      return;
    }
    const draft = splitComposerDrafts[pane] ?? emptyComposerDraft();
    const message = createComposerMessage(draft.prompt, draft.images, draft.files);
    const currentState = appStateRef.current;
    const targetThread = threadForPane(currentState, pane);
    if (targetThread?.read_only) {
      setState((current) => ({ ...current, status: "子任务会话只读" }));
      return;
    }
    if (
      !message ||
      !targetThread ||
      !currentState.activeContext ||
      !currentState.initialized
    ) {
      return;
    }
    // Group threads never queue — every send lands as a completed chat
    // turn server-side (issue #10); see sendPrompt for the full rationale.
    if (isThreadRunning(targetThread) && !isGroupThread(targetThread)) {
      const queued = await queueComposerMessage(message, targetThread);
      if (queued) {
        setSplitComposerDrafts((current) => ({
          ...current,
          [pane]: emptyComposerDraft(),
        }));
        setState((current) => ({
          ...current,
          activePane: pane,
        }));
      }
      return;
    }
    setSplitComposerDrafts((current) => ({
      ...current,
      [pane]: emptyComposerDraft(),
    }));
    const sent = await sendComposerMessageToPane(message, pane);
    if (!sent) {
      setSplitComposerDrafts((current) => ({
        ...current,
        [pane]: {
          prompt: message.text,
          images: message.images.map((image) => ({ ...image })),
          files: message.files.map((file) => ({ ...file })),
        },
      }));
    }
  }

  async function sendComposerMessageToPane(
    message: QueuedComposerMessage,
    pane: ConversationPaneID,
  ): Promise<boolean> {
    const currentState = appStateRef.current;
    const targetThread = threadForPane(currentState, pane);
    const text = message.text.trim();
    const imageCount = message.images.length;
    const files = inputFilesFromComposer(message.files);
    if (
      (!text && imageCount === 0 && files.length === 0) ||
      !targetThread ||
      targetThread.read_only ||
      !currentState.activeContext ||
      !currentState.initialized ||
      viewSwitchPending ||
      (isThreadRunning(targetThread) && !isGroupThread(targetThread))
    ) {
      return false;
    }
    enableConversationAutoFollow();
    resetRunDebugEvents({
      source: "client",
      method: "client/send",
      detail: composerSubmissionDetail(imageCount, files.length),
      tone: "running",
      threadID: targetThread.id,
    });
    appStateRef.current = {
      ...currentState,
      activePane: pane,
      running: true,
      status: "正在发送请求",
    };
    setState((current) => ({
      ...current,
      activePane: pane,
      running: true,
      status: "正在发送请求",
    }));
    let optimisticTurnID: string | undefined;
    try {
      // Insert an optimistic in_progress turn before the IPC round-trip
      // so the live "正在回复/处理" timer starts at the user's click
      // moment instead of waiting for the server's first turn
      // notification. The placeholder is replaced (or dropped on error)
      // once the real turn arrives or the request fails.
      const optimisticTurn = createOptimisticTurn(message, Date.now());
      optimisticTurnID = optimisticTurn.id;
      appStateRef.current = updateThreadByID(
        appStateRef.current,
        targetThread.id,
        (thread) => upsertTurn(thread, optimisticTurn),
      );
      setState((current) =>
        updateThreadByID(
          current,
          targetThread.id,
          (thread) => upsertTurn(thread, optimisticTurn),
        ),
      );
      const encodedImages = await awaitComposerImages(message.images);
      const images = inputImagesFromComposer(encodedImages);
      const result = await window.wuu.startTurn(
        targetThread.id,
        text,
        images,
        files,
        selectedPermissionMode,
        mentionedParticipantIDsFromText(text, participants),
        focusWorkspaceSendValue(
          targetThread,
          chatFocusOverrides[targetThread.id],
        ),
      );
      setState((current) =>
        updateThreadByID(
          { ...current, activePane: pane },
          targetThread.id,
          (thread) =>
            replaceOptimisticTurn(
              thread,
              optimisticTurnID ?? result.turn.id,
              result.turn,
              upsertTurn,
            ),
        ),
      );
      appendRunDebugEvent({
        source: "client",
        method: "turn/start response",
        detail: "服务端已接受本轮请求",
        tone: "running",
        threadID: targetThread.id,
        turnID: result.turn.id,
      });
    } catch (error) {
      const rawMessage = rawErrorMessage(error, "send failed");
      const errorMessage = statusMessageForError(rawMessage, "send failed");
      appendRunDebugEvent({
        source: "client",
        method: "turn/start failed",
        detail: rawMessage,
        tone: "error",
        threadID: targetThread.id,
      });
      const droppedState = optimisticTurnID
        ? updateThreadByID(
            appStateRef.current,
            targetThread.id,
            (thread) => dropOptimisticTurn(thread, optimisticTurnID),
          )
        : appStateRef.current;
      appStateRef.current = {
        ...droppedState,
        activePane: pane,
        running: false,
        status: errorMessage,
      };
      setState((current) => ({
        ...(optimisticTurnID
          ? updateThreadByID(
              current,
              targetThread.id,
              (thread) => dropOptimisticTurn(thread, optimisticTurnID),
            )
          : current),
        activePane: pane,
        running: false,
        status: errorMessage,
      }));
      return false;
    }
    return true;
  }

  async function sendComposerMessageToThread(
    message: QueuedComposerMessage,
    targetThread: Thread,
  ): Promise<boolean> {
    const currentState = appStateRef.current;
    const text = message.text.trim();
    const imageCount = message.images.length;
    const files = inputFilesFromComposer(message.files);
    if (
      (!text && imageCount === 0 && files.length === 0) ||
      targetThread.read_only ||
      !currentState.activeContext ||
      !currentState.initialized ||
      viewSwitchPending ||
      (isThreadRunning(targetThread) && !isGroupThread(targetThread))
    ) {
      return false;
    }
    const targetIsActive = activeThreadIDForState(currentState) === targetThread.id;
    if (targetIsActive) {
      enableConversationAutoFollow();
      resetRunDebugEvents({
        source: "client",
        method: "client/send",
        detail: composerSubmissionDetail(imageCount, files.length),
        tone: "running",
        threadID: targetThread.id,
      });
      appStateRef.current = {
        ...currentState,
        running: true,
        status: "正在发送请求",
      };
      setState((current) => ({
        ...current,
        running: true,
        status: "正在发送请求",
      }));
    }
    let optimisticTurnID: string | undefined;
    try {
      // Insert an optimistic in_progress turn before the IPC round-trip
      // so the live "正在回复/处理" timer starts at the user's click
      // moment instead of waiting for the server's first turn
      // notification. The placeholder is replaced (or dropped on error)
      // once the real turn arrives or the request fails.
      const optimisticTurn = createOptimisticTurn(message, Date.now());
      optimisticTurnID = optimisticTurn.id;
      appStateRef.current = updateThreadByID(
        appStateRef.current,
        targetThread.id,
        (thread) => upsertTurn(thread, optimisticTurn),
      );
      setState((current) =>
        updateThreadByID(
          current,
          targetThread.id,
          (thread) => upsertTurn(thread, optimisticTurn),
        ),
      );
      const encodedImages = await awaitComposerImages(message.images);
      const images = inputImagesFromComposer(encodedImages);
      const result = await window.wuu.startTurn(
        targetThread.id,
        text,
        images,
        files,
        selectedPermissionMode,
        mentionedParticipantIDsFromText(text, participants),
        focusWorkspaceSendValue(
          targetThread,
          chatFocusOverrides[targetThread.id],
        ),
      );
      setState((current) =>
        updateThreadByID(
          current,
          targetThread.id,
          (thread) =>
            replaceOptimisticTurn(
              thread,
              optimisticTurnID ?? result.turn.id,
              result.turn,
              upsertTurn,
            ),
          targetIsActive ? { running: true } : {},
        ),
      );
      if (targetIsActive) {
        appendRunDebugEvent({
          source: "client",
          method: "turn/start response",
          detail: "服务端已接受本轮请求",
          tone: "running",
          threadID: targetThread.id,
          turnID: result.turn.id,
        });
      }
    } catch (error) {
      const rawMessage = rawErrorMessage(error, "send failed");
      const errorMessage = statusMessageForError(rawMessage, "send failed");
      if (targetIsActive) {
        appendRunDebugEvent({
          source: "client",
          method: "turn/start failed",
          detail: rawMessage,
          tone: "error",
          threadID: targetThread.id,
        });
      }
      const droppedState = optimisticTurnID
        ? updateThreadByID(
            appStateRef.current,
            targetThread.id,
            (thread) => dropOptimisticTurn(thread, optimisticTurnID),
          )
        : appStateRef.current;
      appStateRef.current = {
        ...droppedState,
        running: targetIsActive ? false : appStateRef.current.running,
        status: errorMessage,
      };
      setState((current) => ({
        ...(optimisticTurnID
          ? updateThreadByID(
              current,
              targetThread.id,
              (thread) => dropOptimisticTurn(thread, optimisticTurnID),
            )
          : current),
        running: targetIsActive ? false : current.running,
        status: errorMessage,
      }));
      return false;
    }
    return true;
  }

  if (settingsOpen) {
    return (
      <SettingsShellRenderer
        initialized={state.initialized}
        initialPage={settingsInitialPage}
        memoryFocusParticipantID={settingsMemoryFocusID}
        running={viewContextSwitchPending}
        runningProviderNames={runningProviderNames}
        participants={participants}
        usage={settingsUsage}
        usageRange={usageRange}
        setUsageRange={setUsageRange}
        codexPets={codexPets}
        codexPetsLoading={codexPetsLoading}
        codexPetsError={codexPetsError}
        showDebugControlsSetting={ENABLE_DEBUG_CONTROL_SETTING}
        debugControlsEnabled={debugControlsEnabled}
        sidebarWidth={settingsSidebarWidth}
        resizingSidebar={resizingSidebar}
        shellRef={settingsShellRef}
        onBack={() => {
          setSettingsOpen(false);
          setSettingsMemoryFocusID(undefined);
        }}
        onSave={updateRuntimeSettings}
        onRemoveProvider={removeProvider}
        onAdvancedSave={updateAdvancedSettings}
        onGeneralSave={updateGeneralSettings}
        onCodexPetsRefresh={refreshCodexPets}
        onCodexPetsUpdate={updateCodexPets}
        onDebugControlsChange={setDebugControlsEnabled}
        onSidebarResizeStart={startSettingsSidebarResize}
        onSidebarSeparatorKey={handleSettingsSidebarSeparatorKey}
        onSidebarSeparatorDoubleClick={resetSettingsSidebarWidth}
      />
    );
  }

  return (
    <ImagePreviewProvider>
      <div ref={appShellRef} className={shellClassName} style={shellStyle}>
      {sidebarVisible ? (
        <>
          <div
            ref={sidebarHoverZoneRef}
            className="sidebar-hover-zone"
            aria-hidden="true"
            onPointerEnter={scheduleSidebarDrawerOpen}
            onPointerLeave={cancelSidebarDrawerOpen}
          />
          <AppSidebar
            state={state}
            sidebarProjects={sidebarProjects}
            pinnedThreads={sidebarPinnedThreads}
            groupThreads={sidebarGroupThreads}
            activeThreadID={activeThreadID}
            activeDMParticipantID={activeDMParticipantID}
            dmThreadByParticipantID={dmThreadByParticipantID}
            unreadDMParticipantIDs={unreadDMParticipantIDs}
            participants={participants}
            busyParticipantIDs={busyParticipantIDs}
            pendingThreadID={visiblePendingThreadID}
            pendingProjectID={visiblePendingProjectID}
            archiveConfirmThreadID={archiveConfirmThreadID}
            collapsedSidebarSectionIDs={collapsedSidebarSectionIDs}
            expandedSidebarSectionIDs={expandedSidebarSectionIDs}
            collapsingSidebarSectionIDs={collapsingSidebarSectionIDs}
            projectThreadsByProjectID={sidebarThreadsByProjectID}
            projectMenuOpen={projectMenuOpen}
            projectMenuRef={projectMenuRef}
            searchOpen={conversationSearch.open}
            debugFixturesVisible={
              debugControlsVisible && ENABLE_CONVERSATION_FIXTURES
            }
            sectionOrder={sidebarSectionOrder}
            onStartNewThread={() => void startNewThread({ resetToNoProject: true })}
            onOpenSkillsTab={openSkillsTab}
            onToggleConversationSearch={toggleConversationSearch}
            onSeedConversationFixture={seedConversationFixture}
            onSeedAgentTreeDemo={seedAgentTreeDemo}
            onOpenChipGallery={() => setChipGalleryOpen(true)}
            onSelectThread={(id) => void activateThread(id)}
            onSelectParticipant={(participant) => void openParticipantDM(participant)}
            onEditParticipant={openParticipantProfile}
            onCreateParticipant={handleNewParticipantCreate}
            providers={state.initialized?.providers}
            onCreateGroupThread={(title) => void createGroupThread(title)}
            onImportParticipants={importParticipantTemplate}
            onExportParticipants={exportParticipantTemplate}
            onTogglePinned={(thread) => void toggleThreadPinned(thread)}
            onArchiveThread={(thread) => void archiveThread(thread)}
            onDeleteThread={(thread) => void deleteThread(thread)}
            onRenameThread={(thread, title) => void renameThread(thread, title)}
            onClearArchiveConfirm={(id) =>
              setArchiveConfirmThreadID((current) =>
                current === id ? undefined : current,
              )
            }
            onToggleProjectMenu={() => setProjectMenuOpen((open) => !open)}
            onCreateProject={() => void createBlankProject()}
            onOpenProjectFolder={() => void chooseProjectFolder()}
            onToggleSidebarSectionCollapsed={toggleSidebarSectionCollapsed}
            onStartNewThreadForProject={(id) => {
              if (id === SCRATCH_PSEUDO_PROJECT_ID) {
                void useNoProject(true);
              } else {
                void startNewThreadForProject(id);
              }
            }}
            onSelectProjectThread={(projectID, threadID) =>
              void selectProjectThread(projectID, threadID)
            }
            onRemoveProject={(id) => void removeProject(id)}
            onRelocateProject={(id) => void relocateProject(id)}
            onReorderSections={setSidebarSectionOrder}
            onPointerEnter={openSidebarDrawer}
            onPointerLeave={closeSidebarDrawer}
            onOpenSettings={() => {
              setProjectMenuOpen(false);
              setRuntimeMenuOpen(false);
              setCodexRuntimeMenu(null);
              setSettingsInitialPage("providers");
              setSettingsOpen(true);
            }}
          />

          {sidebarCollapsed ? null : (
            <div
              className="sidebar-resizer"
              role="separator"
              aria-label="调整侧边栏宽度"
              aria-orientation="vertical"
              aria-valuemin={SIDEBAR_MIN_WIDTH}
              aria-valuemax={SIDEBAR_MAX_WIDTH}
              aria-valuenow={sidebarWidth}
              tabIndex={0}
              onPointerDown={startSidebarResize}
              onDoubleClick={toggleSidebar}
              onKeyDown={handleSidebarSeparatorKey}
            />
          )}
        </>
      ) : null}

      <ConversationSearchOverlay
        state={conversationSearch}
        results={conversationSearchResults}
        threads={state.threads}
        projects={state.projects}
        activeThreadID={activeThreadID}
        pendingThreadID={visiblePendingThreadID}
        dialogRef={conversationSearchRef}
        inputRef={conversationSearchInputRef}
        onClose={closeConversationSearch}
        onRefresh={() => void refreshConversationSearchThreads()}
        onQueryChange={setConversationSearchQuery}
        onClearQuery={clearConversationSearchQuery}
        onKeyDown={handleConversationSearchKeyDown}
        onSelectIndex={setConversationSearchSelectedIndex}
        onSelectResult={selectConversationSearchResult}
      />

      <main
        className={`conversation-pane${environmentPanelVisible ? " environment-panel-visible" : ""}${
          environmentPanelReserved || participantPanelVisible ? " environment-panel-reserved" : ""
        }${
          subthreadPanelVisible ? " subthread-panel-visible" : ""
        }${
          participantPanelVisible ? " participant-panel-visible" : ""
        }${sessionTabsVisible ? " session-tabs-visible" : ""}${
          conversationGridVisible ? " conversation-grid-visible" : ""
        }`}
        ref={conversationPaneRef}
      >
        <header className="titlebar">
          <div className="title-block">
            {sidebarVisible ? (
              <button
                className="icon-button side-panel-toggle-button sidebar-toggle-button"
                type="button"
                aria-label={sidebarCollapsed ? "展开左侧栏" : "收起左侧栏"}
                aria-pressed={!sidebarCollapsed}
                onClick={toggleSidebar}
              >
                <SidePanelToggleIcon side="left" open={!sidebarCollapsed} />
              </button>
            ) : null}
            <ConversationTitleContent
              state={state}
              sessionTabsVisible={sessionTabsVisible}
              pendingSwitchThreadID={visiblePendingThreadID}
              pendingComposerMessagesByThread={pendingComposerMessagesByThread}
              workspaceMode={workspaceMode}
              activeTitle={activeTitle}
              onSelectSessionTab={(tabID) => void selectSessionTab(tabID)}
              onCloseSessionTab={(tabID) => void closeSessionTab(tabID)}
              onCloseSessionTabs={(tabIDs) => void closeSessionTabs(tabIDs)}
              onPopOutSessionTab={(tabID) => void popOutSessionTab(tabID)}
              onStartNewThread={() => void startNewThread()}
              onReorderSessionTabs={reorderSessionTabs}
            />
          </div>
          <ConversationTitleActions
            state={state}
            debugControlsVisible={debugControlsVisible}
            enableLaunchPreview={ENABLE_LAUNCH_PREVIEW}
            previewingLaunch={previewingLaunch}
            onPinLaunchPreview={() => setLaunchPreviewPinned(true)}
            enablePlanPanelDebug={ENABLE_PLAN_PANEL_DEBUG}
            onSeedPlanPanelDebug={seedPlanPanelDebug}
            conversationGridVisible={conversationGridVisible}
            onToggleConversationGrid={() =>
              setConversationGridVisible((visible) => !visible)
            }
            enableRunDebugPanel={ENABLE_RUN_DEBUG_PANEL}
            runDebugRef={runDebugRef}
            runDebugOpen={runDebugOpen}
            onToggleRunDebug={() => {
              closeEnvironmentPanel();
              setRunDebugOpen((open) => !open);
            }}
            runDebugPhase={runDebugPhase}
            runDebugEvents={runDebugEvents}
            queuedMessages={queuedMessages}
            guideMessages={guideMessages}
            composerImages={composerImages}
            composerFiles={composerFiles}
            runDebugCopied={runDebugCopied}
            onCopyRunDebug={() =>
              void copyRunDebugInfo({
                state,
                queuedMessages,
                guideMessages,
                composerImages,
                composerFiles,
              })
            }
            onCloseRunDebug={() => setRunDebugOpen(false)}
            chipGalleryOpen={chipGalleryOpen}
            onCloseChipGallery={() => setChipGalleryOpen(false)}
            poppedOutMode={poppedOutMode}
            activeThreadIsChatStyle={activeThreadIsChatStyle}
            activeThread={activeThread}
            onOpenTaskBoard={openTaskBoardTab}
            environmentToggleRef={environmentToggleRef}
            environmentPanelVisible={environmentPanelVisible}
            activeThreadIsGroup={activeThreadIsGroup}
            onToggleEnvironmentPanel={toggleEnvironmentPanel}
            rightPanelOpen={rightPanelOpen}
            onToggleRightPanel={toggleRightPanel}
          />
        </header>

        <ConversationTurnRail
          turns={turns}
          activeTurnID={turns[turns.length - 1]?.id}
          scrollContainerRef={conversationScrollRef}
          getScrollContainer={conversationRailScrollContainer}
          onWheelScrollAway={disableConversationAutoFollow}
          onDragScrollAway={disableConversationAutoFollow}
          onSelectQueryHistory={handleQueryHistorySelect}
        />

        <ConversationSidePanels
          state={state}
          environmentPanelVisible={environmentPanelVisible}
          environmentPanelMounted={environmentPanelMounted}
          environmentPanelRef={environmentPanelRef}
          environmentPanelClosing={environmentPanelClosing}
          environmentPanelMotionState={environmentPanelMotionState}
          activePlanUpdate={activePlanUpdate}
          environmentPanelMenu={environmentPanelMenu}
          anyThreadIsRunning={anyThreadIsRunning}
          pullRequestDisabledReason={pullRequestDisabledReason}
          onSetEnvironmentPanelMenu={setEnvironmentPanelMenu}
          onCloseEnvironmentPanel={() =>
            closeEnvironmentPanel({ dismissed: true })
          }
          onSelectBranch={(branch) => void checkoutBranch(branch)}
          onCreateBranch={(branch) => createAndCheckoutBranch(branch)}
          onOpenReview={() => {
            openWorkspaceTool("review");
            setWorkspaceMode(undefined);
            closeEnvironmentPanel({ dismissed: true });
          }}
          onOpenCommit={() => openEnvironmentDialog("commit")}
          onOpenPullRequest={() => openEnvironmentDialog("pull-request")}
          rightPanelFilePath={rightPanelFilePath}
          onCloseFilePreview={handleCloseFilePreview}
          activeThread={activeThread}
          archiveConfirmSubagentID={archiveConfirmSubagentID}
          onSelectChildAgent={(agent) => void selectChildAgent(agent)}
          onToggleSubagentPinned={(agent) =>
            void toggleSubagentPinned(agent)
          }
          onArchiveSubagent={(agent) => void archiveSubagent(agent)}
          onClearSubagentArchiveConfirm={(id) =>
            setArchiveConfirmSubagentID((current) =>
              current === id ? undefined : current,
            )
          }
          participants={participants}
          onAddThreadMember={(threadID, participantID) =>
            addThreadMemberByID(threadID, participantID)
          }
          onRemoveThreadMember={(threadID, participantID) =>
            removeThreadMemberByID(threadID, participantID)
          }
          openSubthreadPanel={openSubthreadPanel}
          onCloseSubthreadPanel={() => setOpenSubthreadPanel(undefined)}
          onResolveSubthread={resolveOpenConversationSubthread}
          onEscalateSubthread={escalateOpenConversationSubthread}
          onBubbleSubthread={bubbleOpenConversationSubthread}
          onReactSubthread={reactToOpenConversationSubthreadMessage}
          poppedOutMode={poppedOutMode}
          activeContext={state.activeContext}
          onPopOutSubthread={(threadID, subthreadID, context) =>
            void popOutSubthread(threadID, subthreadID, context)
          }
          subthreadLeadCandidates={subthreadLeadCandidates}
          subthreadComposer={{
            draft: subthreadComposerDraft,
            setDraft: setSubthreadComposerDraft,
            initialized: composerInitialized,
            projects: state.projects,
            activeContext: state.activeContext,
            activeProject,
            codexModels,
            codexRuntimeRef,
            runtimeMenuRef,
            accessMenuRef: subthreadAccessMenuRef,
            participants,
            onPasteAttachmentFiles: (files) =>
              void attachSubthreadComposerAttachmentFiles(files),
            onRemoveFile: removeSubthreadComposerFile,
            onRemoveImage: removeSubthreadComposerImage,
            onSend: () => void sendOpenConversationSubthreadMessage(),
          }}
          resolveParticipantName={resolveParticipantName}
          busyParticipantIDs={busyParticipantIDs}
          chatReaderCount={chatReaderCount}
          clampedThreadPanelWidth={clampedThreadPanelWidth}
          onThreadPanelResizeStart={startThreadPanelResize}
          onThreadPanelReset={resetThreadPanelWidth}
          onThreadPanelSeparatorKey={handleThreadPanelSeparatorKey}
          participantPanel={participantPanel}
          onCloseParticipantPanel={() => setParticipantPanel(undefined)}
          onSaveParticipant={handleParticipantSave}
          onFeedbackParticipant={handleParticipantFeedback}
          onOpenMemoryPanel={(participantID) =>
            openMemorySettings(participantID)
          }
          onRetireParticipant={handleParticipantRetire}
          viewContextSwitchPending={viewContextSwitchPending}
        />

        {state.initialized && !previewingLaunch ? (
          <div
            className={`scroll-region${emptyConversation && !showingWorkspaceMode ? " empty-scroll-region" : ""}${
              showingWorkspaceMode ? " workspace-scroll-region" : ""
            }${splitConversation ? " split-scroll-region" : ""}${showingSkillsCatalog ? " skills-scroll-region" : ""}${showingTaskBoard ? " task-board-scroll-region" : ""}`}
            onScroll={(event) => handleConversationScroll(event.currentTarget)}
            ref={conversationScrollRef}
          >
            <div ref={scrollContentRef} className="scroll-region-content">
              {showingSkillsCatalog ? (
              <SkillsCatalog
                activeContext={state.activeContext}
              />
            ) : boardSessionTab ? (
              <TaskBoardView
                threadID={boardSessionTab.threadID}
                title={sessionTabLabel(boardSessionTab, state)}
                refreshToken={boardRefreshTick}
                resolveParticipantName={resolveParticipantName}
                onOpenTask={(subthreadID) =>
                  void openTaskFromBoard(boardSessionTab.threadID, subthreadID)
                }
              />
            ) : workspaceMode ? (
              <WorkspaceMainPanel
                view={workspaceMode}
                activeContext={state.activeContext}
                workspaceContext={workspaceContext}
                gitStatus={state.gitStatus}
                selectedFilePath={activeWorkspaceFile}
                onFileDirtyChange={rememberWorkspaceFileDirtyState}
                onOpenRightPanel={() => {
                  ensureWorkspaceToolTab(workspaceMode);
                  activateWorkspaceTool(workspaceMode);
                  setRightPanelOpenWithMotion(true);
                }}
              />
            ) : (
              <>
                {!activeThreadReadOnly ? (
                  <QueryHistoryRail
                    entries={pastQueries}
                    maxBars={QUERY_HISTORY_RAIL_MAX_BARS}
                    active={queryHistoryOpen}
                    railRef={queryHistoryRailRef}
                    onHoverStart={openQueryHistory}
                    onHoverEnd={scheduleQueryHistoryClose}
                  />
                ) : null}
                {splitConversation && state.thread && state.secondaryThread ? (
                  <ConversationSplitLayoutRenderer
                    state={state}
                    primaryThread={state.thread}
                    secondaryThread={state.secondaryThread}
                    splitLeftPercent={splitLeftPercent}
                    splitComposerDrafts={splitComposerDrafts}
                    splitPaneRefs={splitPaneRefs}
                    viewSwitchPending={viewContextSwitchPending}
                    historyMessageEdit={historyMessageEdit}
                    onSplitResizeStart={startSplitResize}
                    onSplitSeparatorDoubleClick={resetSplitPercent}
                    onSplitSeparatorKey={handleSplitSeparatorKey}
                    onActivatePane={activateConversationPane}
                    onClosePane={closeConversationPane}
                    onConversationScroll={handleConversationScroll}
                    onSetPrompt={setSplitComposerPrompt}
                    onPasteAttachmentFiles={(pane, files) =>
                      void attachSplitComposerAttachmentFiles(pane, files)
                    }
                    onRemoveFile={removeSplitComposerFile}
                    onRemoveImage={removeSplitComposerImage}
                    onSend={(pane) => void sendPromptForPane(pane)}
                    onInterrupt={(pane) => void interruptPane(pane)}
                    onForkMessage={(thread, turnID, itemID) =>
                      void forkThreadFromMessage(thread, turnID, itemID)
                    }
                    onOpenFile={openWorkspaceFile}
                    onOpenAgent={(agent) => void selectChildAgent(agent)}
                    canEditThreadMessage={canShowHistoryEditButton}
                    onEditMessage={startEditingThreadMessageFromHistory}
                    onCancelEditMessage={cancelEditingThreadMessage}
                    onSubmitEditMessage={(
                      thread,
                      turnID,
                      item,
                      text,
                      images,
                      files,
                      pane,
                    ) =>
                      void submitEditedThreadMessageFromHistory(
                        thread,
                        turnID,
                        item,
                        text,
                        images,
                        files,
                        pane,
                      )
                    }
                    onStreamFrame={scheduleStreamScroll}
                    onNoticeAction={handleNoticeAction}
                    onOpenFileDiff={openTurnFileDiffPanel}
                  />
                ) : emptyConversation ? (
              <EmptyConversationHome
                title={emptyThreadTitle}
                belowTitle={
                  <EmptyStateHints
                    providers={state.initialized?.providers}
                    onSelect={handleEmptyStateHint}
                  />
                }
              >
                {renderComposer("hero")}
              </EmptyConversationHome>
            ) : (
              <CachedConversationPanes
                threadIDs={cachedThreadPaneIDs}
                threadsByID={cachedConversationThreadsByID}
                activeThreadID={activeThreadID}
                activeContextCwd={state.activeContext?.cwd}
                conversationGridVisible={conversationGridVisible}
                contextCompositionEntries={contextCompositionEntries}
                instructionFilesEntries={instructionFilesEntries}
                historyMessageEdit={historyMessageEdit}
                onStreamFrame={scheduleStreamScroll}
                onCollapseComplete={handleTurnCollapseComplete}
                onDismissContextComposition={
                  handleCachedPaneDismissContextComposition
                }
                onDismissInstructions={handleCachedPaneDismissInstructions}
                canEditThreadMessage={canEditCachedThreadMessage}
                onForkMessage={handleCachedPaneForkMessage}
                onOpenFile={openWorkspaceFile}
                onOpenAgent={handleCachedPaneOpenAgent}
                onOpenSubthread={handleCachedPaneOpenSubthread}
                onReact={handleCachedPaneReact}
                onEditMessage={handleCachedPaneEditMessage}
                onCancelEditMessage={handleCachedPaneCancelEditMessage}
                onSubmitEditMessage={handleCachedPaneSubmitEditMessage}
                onNoticeAction={handleCachedPaneNoticeAction}
                busyParticipantIDs={busyParticipantIDs}
                activeThreadMarks={activeThreadMarks}
                resolveParticipantName={resolveParticipantName}
                chatReaderCount={chatReaderCount}
                subthreadsByAnchor={activeChatSubthreadsByAnchor}
                subthreadsThreadID={activeThreadID}
                pendingChatMessagesByThread={pendingComposerMessagesByThread}
                turnStreamStatus={state.turnStreamStatus}
                onOpenFileDiff={handleCachedPaneOpenFileDiff}
              />
            )}
              </>
            )}
            </div>
            {mainConversationDockVisible ? (
              <JumpToLatestPill
                containerRef={conversationScrollRef}
                bottomAnchor={dockComposerNode}
              />
            ) : null}
          </div>
        ) : (
          <RuntimeLoading
            status={state.status}
            pinned={previewingLaunch}
            onExitPreview={() => setLaunchPreviewPinned(false)}
          />
        )}

        {mainConversationDockVisible ? renderComposer("dock") : null}

        {mainConversationDockVisible && activePlanVisible ? (
          <div
            className="jump-to-latest-cluster"
            aria-label="当前位置与进度"
          >
            {activePlanVisible ? (
              <div
                className="jump-to-latest-progress"
                aria-label={`当前计划已完成 ${activePlanCompleted} 项，共 ${activePlanTotal} 项`}
              >
                进度 {activePlanCompleted}/{activePlanTotal}
                {activePlanDetailItems.length > 0 ? (
                  <span className="jump-to-latest-progress-detail" aria-hidden="true">
                    {activePlanDetailItems.map((item) => (
                      <span className={`jump-to-latest-progress-step ${item.status}`} key={item.step}>
                        {item.status === "in_progress" ? "进行中" : "下一步"}：{item.step}
                      </span>
                    ))}
                  </span>
                ) : null}
              </div>
            ) : null}
          </div>
        ) : null}
      </main>

      {!poppedOutMode && (rightPanelOpen || rightPanelAnimating) ? (
        <div
          className="workspace-right-panel-resizer"
          role="separator"
          aria-label="调整右侧栏宽度"
          aria-orientation="vertical"
          aria-valuemin={WORKSPACE_RIGHT_PANEL_MIN_WIDTH}
          aria-valuemax={WORKSPACE_RIGHT_PANEL_MAX_WIDTH}
          aria-valuenow={clampedWorkspaceRightPanelWidth}
          tabIndex={0}
          onPointerDown={startRightPanelResize}
          onDoubleClick={resetWorkspaceRightPanelWidth}
          onKeyDown={handleRightPanelSeparatorKey}
        />
      ) : null}
      {poppedOutMode ? null : (
        <WorkspaceRightPanel
          open={rightPanelOpen}
          present={rightPanelOpen || rightPanelAnimating}
          tabs={workspaceViewTabs}
          activeTabID={workspaceActiveViewTabID}
          activeContext={state.activeContext}
          workspaceContext={workspaceContext}
          gitStatus={state.gitStatus}
          selectedFilePath={activeWorkspaceFile}
          onSelectTab={focusWorkspaceViewTab}
          onOpenTool={openWorkspaceTool}
          onShowTools={showWorkspaceToolPicker}
          onCloseTab={closeWorkspaceViewTab}
          onReorderTabs={reorderWorkspaceViewTabs}
          onOpenFile={openWorkspaceFile}
          onClose={() => setRightPanelOpenWithMotion(false)}
          globalized={rightPanelGlobalized}
          onToggleGlobalize={() => setRightPanelGlobalized((g) => !g)}
          pendingBrowserURL={pendingBrowserURL}
          onBrowserURLConsumed={consumePendingBrowserURL}
          onBrowserURLChange={rememberBrowserURLForActiveThread}
        />
      )}
      {environmentDialog === "commit" ? (
        <CommitChangesDialog
          gitStatus={state.gitStatus}
          branch={state.gitStatus?.branch}
          onCancel={() => setEnvironmentDialog(null)}
          onCommit={commitEnvironmentChanges}
        />
      ) : null}
      {environmentDialog === "pull-request" ? (
        <PullRequestDialog
          gitStatus={state.gitStatus}
          disabledReason={pullRequestDisabledReason}
          onCancel={() => setEnvironmentDialog(null)}
          onCreate={createEnvironmentPullRequest}
        />
      ) : null}
      {pendingFork ? (
        <ConversationForkDialog
          worktreeDisabledReason={forkWorktreeDisabledReason}
          onCancel={() => setPendingFork(undefined)}
          onChoose={choosePendingFork}
        />
      ) : null}
      {debugControlsVisible ? <DesignTokensPanel /> : null}
      {queryHistoryOpen &&
      !activeThreadReadOnly &&
      pastQueries.length > 0 ? (
        <FloatingMenuPortal
          anchorRef={queryHistoryRailRef}
          owner="composer-query-history"
          placement="middle"
          align="right"
          crossAxisOffset={-8}
          width={ENVIRONMENT_PANEL_WIDTH_PX}
        >
          <div
            onMouseEnter={cancelQueryHistoryClose}
            onMouseLeave={scheduleQueryHistoryClose}
            style={{
              width: `min(${ENVIRONMENT_PANEL_WIDTH_CSS}, calc(100vw - 32px))`,
            }}
          >
            <QueryHistoryPopover
              entries={pastQueries}
              onSelect={handleQueryHistorySelect}
            />
          </div>
        </FloatingMenuPortal>
      ) : null}
      <CodexPetLayer
        snapshot={codexPets}
        running={anyThreadIsRunning}
        status={state.status}
      />
      </div>
    </ImagePreviewProvider>
  );
}
