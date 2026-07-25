/// <reference path="../shared/jsx-compat.d.ts" />

import {
  type CSSProperties,
  type RefObject,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import type {
  ActivitySession,
  Agent,
  DesktopProject,
  InitializeResult,
  InputFile,
  InputImage,
  PopOutInitResult,
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
  failOptimisticCompactTurn,
  inputFilesFromComposer,
  inputImagesFromComposer,
  replaceOptimisticTurn,
  type QueuedComposerMessage,
} from "./ComposerMessages";
import {
  greetingFor,
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
} from "./ComposerView";
import {
  QueryHistoryPopover,
  type QueryHistoryEntry,
} from "./QueryHistoryPopover";
import { QueryHistoryRail } from "./QueryHistoryRail";
import { ConversationSearchOverlay } from "./ConversationSearchOverlay";
import { useConversationScrollState } from "./ConversationScrollState";
import { useConversationSearch } from "./ConversationSearchState";
import {
  SideThreadPanel,
  type SideThreadPanelHandle,
} from "./SideThreadPanel";
import { SideThreadComposer } from "./SideThreadComposer";
import { ConversationForkDialog } from "./ConversationForkDialog";
import type { TurnFileDiffSelection } from "./TurnFileDiffTypes";
import {
  AppSidebar,
} from "./AppSidebar";
import { ChannelView, type ChannelSection } from "./ChannelView";
import {
  type EnvironmentPanelMenu,
  type EnvironmentPanelMotionState,
} from "./EnvironmentPanel";
import { createEnvironmentActions } from "./EnvironmentActions";
import { useGitActionBusy } from "./GitActionBusy";
import {
  activePlanUpdateForThread,
  activeSessionTab,
  activeThreadForState,
  activeThreadIDForState,
  activeTurnForThread,
  latestContextUsageForThread,
  activeTurnIDForThread,
  activeTurnTokenSpeedSnapshot,
  flushPendingStreamingTokenSamples,
  recordPendingStreamingTokenSample,
  type PendingStreamingTokenSamples,
  bindActiveSessionTabToThread,
  cloneSessionTabDraft,
  composerSubmissionDetail,
  conversationPaneThreadsByID,
  createDraftSessionTab,
  emptyComposerDraft,
  ensureSessionTab,
  handleStreamingNotification,
  initialSplitComposerDrafts,
  initialState,
  isAnyThreadRunning,
  isStateActiveThreadRunning,
  isThreadRunning,
  isThreadUnread,
  latestPlanUpdateForThread,
  markThreadTurnsViewed,
  pinnedThreadSummaries,
  queryTextForUserItem,
  SCRATCH_PSEUDO_PROJECT_ID,
  scratchThreadSummaries,
  queryTextsForThread,
  reduceServerEvent,
  resolveComposerRunningAction,
  requireThread,
  runtimeContextKey,
  sameRuntimeContext,
  serverEventShouldRefreshGit,
  serverEventTargetsActiveContext,
  sessionTabForLoadedRuntime,
  setThreadForPane,
  sortThreads,
  summarizeThreadsForSidebar,
  threadBelongsToProject,
  threadForTab,
  threadForPane,
  threadSessionTabID,
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
} from "./AppState";
import {
  RIGHT_PANEL_MOTION_MS,
  SIDEBAR_DRAWER_EXIT_MS,
  SIDEBAR_MAX_WIDTH,
  SIDEBAR_MIN_WIDTH,
  SIDEBAR_MOTION_MS,
  WORKSPACE_RIGHT_PANEL_MAX_WIDTH,
  WORKSPACE_RIGHT_PANEL_MIN_WIDTH,
  useAppLayoutState,
} from "./AppLayoutState";
import { CommitChangesDialog, PullRequestDialog } from "./GitDialogs";
import { motionDurationMs } from "./motion";
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
import { deriveActiveSessionHints } from "./activeSessionHint";
import { pullRequestUnavailableReason } from "./RuntimeHelpers";
import type { SettingsPage } from "./SettingsView";
import { ENABLE_GROUP_CHAT, ENABLE_ULTRA_MODE } from "./FeatureFlags";
import { ArchiveTip } from "./ArchiveTip";
import { TopNotice } from "./TopNotice";
import { CircleAlert } from "lucide-react";
import type { ComposerGoalSummary } from "../shared/protocol";
import { useSettingsRuntimeState } from "./SettingsRuntimeState";
import { SidePanelToggleIcon } from "./SidePanelToggleIcon";
import { JumpToLatestPill } from "./JumpToLatestPill";
import { SkillsCatalog } from "./SkillsCatalog";
import { runDebugPhaseForState } from "./RunDebugPanel";
import { useBrowserVisibility } from "./BrowserVisibility";
import { useSideThreadController } from "./SideThreadController";
import {
  rawErrorMessage,
  statusMessageForError,
} from "./UserFacingErrors";
import { scrollToUserMessage, TurnView } from "./TurnView";
import { ConversationTurnRail } from "./ConversationTurnRail";
import {
  WorkspaceRightPanel,
} from "./WorkspacePanels";
import { WorkspaceDocumentTurnDock } from "./WorkspaceDocumentTurnDock";
import type { WorkspaceTerminalRunRequest } from "./WorkspaceTerminalPanel";
import { useWorkspaceToolState } from "./WorkspaceToolState";
import type { WorkspaceViewTab } from "./WorkspaceViewTabs";
import { desktopApiErrorMessage } from "./WorkspaceReviewHelpers";
import { ImagePreviewProvider } from "./ImagePreview";
import { WINDOW_RESIZING_CLASS } from "./WindowResizeState";
import { useComposerDraftState } from "./ComposerDraftState";
import { useComposerPendingState } from "./ComposerPendingState";
import { useSidebarDrawerState } from "./SidebarDrawerState";
import {
  mergeSidebarThreadSnapshots,
  threadsForDesktopProject,
  useSidebarProjectState,
} from "./SidebarProjectState";
import { useViewSwitchState } from "./ViewSwitchState";
import {
  activitiesForThread,
  clearActivitiesForWorkdir,
  emptyActivitySessions,
  mergeActivityList,
  reduceActivitySessionEvent,
} from "./ActivitySessions";
import {
  loadPopOutRuntime,
  loadRuntime,
  selectRuntimeContext,
} from "./RuntimeLoadState";
import { createProjectRuntimeActions } from "./ProjectRuntimeActions";
import { createWorkspaceActions } from "./WorkspaceActions";
import { createSessionTabActions } from "./SessionTabActions";
import { createThreadActivationActions } from "./ThreadActivationActions";
import { createThreadMutationActions } from "./ThreadMutationActions";
import { createRuntimeSettingsActions } from "./RuntimeSettingsActions";
import { createConversationDemoPaneActions } from "./ConversationDemoPaneActions";
import {
  createConversationHistoryActions,
  type HistoryMessageEditState,
  type PendingForkState,
} from "./ConversationHistoryActions";
import { localizedText, resolveLocalizedText, useI18n } from "./i18n";
import { CachedConversationPanes } from "./CachedConversationPanes";
import {
  ConversationSidePanels,
  ConversationSplitLayoutRenderer,
  ConversationTitleActions,
  ConversationTitleContent,
  SettingsShellRenderer,
} from "./ConversationShellRenderers";
import {
  runtimeViewForConversation,
  runtimeViewForSession,
} from "./SessionRuntimeState";
export { SIDEBAR_DRAWER_HOVER_OPEN_DELAY_MS } from "./SidebarDrawerState";

const ENVIRONMENT_PANEL_MOTION_MS = motionDurationMs(
  "--environment-panel-motion-duration",
  260,
);
const WORKSPACE_SHEET_EXIT_MS = motionDurationMs("--sheet-exit-duration", 220);
// Globalized-sheet phases: docked (grid child) → arming (promoted to a
// full-window fixed sheet, teleported over its dock slot for one frame) →
// open (slid to cover the window) → exiting (sliding back to park) →
// docking (teleported back into the grid for one frame, no transition) →
// docked. Transitions retarget mid-flight, so rapid toggles stay continuous.
type WorkspaceSheetPhase = "docked" | "arming" | "open" | "exiting" | "docking";
const ENVIRONMENT_PANEL_WIDTH_PX = 328;
const ENVIRONMENT_PANEL_WIDTH_CSS = `${ENVIRONMENT_PANEL_WIDTH_PX}px`;
// Cap on the number of bars rendered in the always-visible rail. The
// rail is a thin at-a-glance index; if there are more queries than fit,
// we collapse the tail into a single bar.
const QUERY_HISTORY_RAIL_MAX_BARS = 20;
// Keep only the active conversation pane mounted. Hidden panes used to retain
// full TurnView DOM trees, making long sessions heavier after each tab switch.
const CACHED_THREAD_PANE_LIMIT = 1;
type EnvironmentDialog = "commit" | "pull-request" | null;
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

type MainComposerFocusRequest = {
  target: ComposerVariant;
  origin: Element | null;
  interactionVersion: number;
};

export function App(): JSX.Element {
  const { locale, t, formatNumber } = useI18n();
  const [popOutInit] = useState<PopOutInitResult | null>(() => readPopOutInit());
  const poppedOutMode = Boolean(popOutInit?.kind && popOutInit.context);
  const [state, setState] = useState<AppState>(initialState);
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
    attachComposerAttachmentFiles,
    removeComposerImage,
    removeComposerFile,
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
  const [activitySessions, setActivitySessions] = useState(emptyActivitySessions);
  const [projectMenuOpen, setProjectMenuOpen] = useState(false);
  const closeProjectMenu = useCallback(() => setProjectMenuOpen(false), []);
  const appShellRef = useRef<HTMLDivElement>(null);
  const settingsShellRef = useRef<HTMLDivElement>(null);
  const [mainComposerFocusRequest, setMainComposerFocusRequest] =
    useState<MainComposerFocusRequest | null>(null);
  const userInteractionVersionRef = useRef(0);
  const {
    sidebarWidth,
    sidebarCollapsed,
    resizingSidebar,
    sidebarAnimating,
    clampedWorkspaceRightPanelWidth,
    resizingRightPanel,
    rightPanelOpen,
    rightPanelAnimating,
    effectiveSidebarWidth,
    workspaceRightPanelAutoGlobalized,
    workspaceRightPanelDockableWithoutSidebar,
    setRightPanelOpenWithMotion,
    animateRightPanelLayout,
    animateSidebarLayout,
    startSidebarResize,
    startRightPanelResize,
    handleRightPanelSeparatorKey,
    resetWorkspaceRightPanelWidth,
    toggleSidebar,
    handleSidebarSeparatorKey,
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
  const [rightPanelManualGlobalized, setRightPanelManualGlobalized] =
    useState(false);
  const rightPanelAutoGlobalized =
    rightPanelOpen && workspaceRightPanelAutoGlobalized;
  const rightPanelGlobalized =
    rightPanelOpen &&
    (rightPanelManualGlobalized || rightPanelAutoGlobalized);
  const [workspaceSheetPhase, setWorkspaceSheetPhase] =
    useState<WorkspaceSheetPhase>(rightPanelGlobalized ? "open" : "docked");
  useLayoutEffect(() => {
    if (rightPanelGlobalized) {
      if (workspaceSheetPhase === "open" || workspaceSheetPhase === "arming") {
        return undefined;
      }
      setWorkspaceSheetPhase("arming");
      return undefined;
    }
    if (workspaceSheetPhase === "docked") {
      return undefined;
    }
    if (workspaceSheetPhase === "arming") {
      // Interrupted before the slide even started: demote instantly.
      setWorkspaceSheetPhase("docked");
      return undefined;
    }
    if (workspaceSheetPhase === "docking") {
      return undefined;
    }
    setWorkspaceSheetPhase("exiting");
    const timer = window.setTimeout(
      () => setWorkspaceSheetPhase("docking"),
      WORKSPACE_SHEET_EXIT_MS,
    );
    return () => window.clearTimeout(timer);
  }, [rightPanelGlobalized, workspaceSheetPhase]);
  useEffect(() => {
    if (workspaceSheetPhase !== "arming" && workspaceSheetPhase !== "docking") {
      return undefined;
    }
    // Double rAF: arming lets the parked transform commit before retargeting
    // to open (so the enter transition has a start value); docking lets the
    // grid snap commit transition-free before the data attribute clears.
    const next = workspaceSheetPhase === "arming" ? "open" : "docked";
    const from = workspaceSheetPhase;
    const raf = requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        setWorkspaceSheetPhase((current) => (current === from ? next : current));
      });
    });
    return () => cancelAnimationFrame(raf);
  }, [workspaceSheetPhase]);
  const sidebarDrawerMode = sidebarCollapsed || rightPanelGlobalized;
  const {
    sidebarDrawerPhase,
    sidebarHoverZoneRef,
    cancelSidebarDrawerOpen,
    openSidebarDrawer,
    openSidebarDrawerNow,
    scheduleSidebarDrawerOpen,
    closeSidebarDrawer,
    scheduleSidebarDrawerCloseFromPointerLeave,
  } = useSidebarDrawerState({
    appShellRef,
    sidebarCollapsed,
    resizingSidebar,
    activeSessionTabID: state.activeSessionTabID,
    motionMs: SIDEBAR_DRAWER_EXIT_MS,
    dockingMotionMs: SIDEBAR_MOTION_MS,
  });
  const sidebarDrawerVisible =
    sidebarDrawerPhase === "open" ||
    (rightPanelGlobalized && !sidebarCollapsed);
  const {
    collapsedSidebarSectionIDs,
    expandedSidebarSectionIDs,
    loadingProjectThreadIDs,
    projectThreadsByProjectID,
    cachedScratchThreads,
    sidebarSectionOrder,
    setSidebarSectionOrder,
    cacheSidebarThreads,
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
  });
  const [runtimeMenuOpen, setRuntimeMenuOpen] = useState(false);
  const [accessMenuOpen, setAccessMenuOpen] = useState(false);
  const [codexRuntimeMenu, setCodexRuntimeMenu] =
    useState<CodexRuntimeMenu>(null);
  const [codexModels, setCodexModels] = useState<CodexModelLoadState>({
    loading: false,
    error: "",
    models: [],
  });
  const [branchMenuOpen, setBranchMenuOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [channelsOpen, setChannelsOpen] = useState(false);
  const [channelSection, setChannelSection] = useState<ChannelSection>("rooms");
  const [channelMentionCount, setChannelMentionCount] = useState(0);
  const previousChannelMentionCount = useRef<number | null>(null);
  const [settingsInitialPage, setSettingsInitialPage] =
    useState<SettingsPage>("providers");
  const {
    settingsUsage,
    codexPets,
    codexPetsLoading,
    codexPetsError,
    refreshCodexPets,
    updateCodexPets,
  } = useSettingsRuntimeState({ settingsOpen });

  useEffect(() => {
    if (!ENABLE_GROUP_CHAT || !window.wuu || !state.initialized) {
      setChannelMentionCount(0);
      return;
    }
    let active = true;
    const refresh = async (): Promise<void> => {
      try {
        if (channelsOpen) {
          await window.wuu.ackChannelHumanMentions();
          if (active) {
            previousChannelMentionCount.current = 0;
            setChannelMentionCount(0);
          }
          return;
        }
        const result = await window.wuu.getChannelHumanMentionStatus();
        if (active) {
          previousChannelMentionCount.current = result.count;
          setChannelMentionCount(result.count);
        }
      } catch (reason) {
        console.warn("channel mention refresh failed", reason);
      }
    };
    void refresh();
    const timer = window.setInterval(() => void refresh(), 2_000);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [channelsOpen, state.initialized, t]);
  const [projectFilter, setProjectFilter] = useState("");
  const [launchPreviewPinned, setLaunchPreviewPinned] = useState(false);
  const {
    workspaceViewTabs,
    workspaceActiveViewTabID,
    workspaceActiveFileTabID,
    openWorkspaceTool,
    openWorkspaceDiffTab,
    openWorkspaceFileTab,
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
  const [terminalRunRequest, setTerminalRunRequest] =
    useState<WorkspaceTerminalRunRequest | undefined>(undefined);
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
  const [focusedWorkspaceContext, setFocusedWorkspaceContext] =
    useState<RuntimeContext | undefined>(undefined);
  useEffect(() => {
    if (!rightPanelGlobalized) {
      setFocusedWorkspaceContext(undefined);
    }
  }, [rightPanelGlobalized]);
  // Manual focus is user intent; automatic focus is derived independently
  // from current layout capacity above. Closing the workspace clears only the
  // manual request, while resizing can freely enter/leave automatic focus.
  useEffect(() => {
    if (!rightPanelOpen) {
      setRightPanelManualGlobalized(false);
    }
  }, [rightPanelOpen]);
  const toggleWorkspacePanelGlobalized = useCallback((): void => {
    animateRightPanelLayout();
    // The under-stage sidebar column collapses/restores while the sheet
    // covers it; give that structural change its shared motion too.
    animateSidebarLayout();
    if (!rightPanelGlobalized) {
      setRightPanelManualGlobalized(true);
      return;
    }
    setRightPanelManualGlobalized(false);
    if (
      rightPanelAutoGlobalized &&
      workspaceRightPanelDockableWithoutSidebar &&
      !sidebarCollapsed
    ) {
      toggleSidebar();
    }
  }, [
    animateRightPanelLayout,
    animateSidebarLayout,
    rightPanelAutoGlobalized,
    rightPanelGlobalized,
    sidebarCollapsed,
    toggleSidebar,
    workspaceRightPanelDockableWithoutSidebar,
  ]);
  const revealConversationFromFocusedWorkspace = useCallback((): void => {
    setChannelsOpen(false);
    if (!rightPanelGlobalized) {
      return;
    }
    closeSidebarDrawer();
    setRightPanelManualGlobalized(false);
    if (!rightPanelAutoGlobalized) {
      return;
    }
    if (!workspaceRightPanelDockableWithoutSidebar) {
      setRightPanelOpenWithMotion(false);
      return;
    }
    if (!sidebarCollapsed) {
      toggleSidebar();
    }
  }, [
    closeSidebarDrawer,
    rightPanelAutoGlobalized,
    rightPanelGlobalized,
    setRightPanelOpenWithMotion,
    sidebarCollapsed,
    toggleSidebar,
    workspaceRightPanelDockableWithoutSidebar,
  ]);
  const [environmentDialog, setEnvironmentDialog] =
    useState<EnvironmentDialog | null>(null);
  const [contextCompositionEntries, setContextCompositionEntries] = useState<
    ContextCompositionEntry[]
  >([]);
  const [instructionFilesEntries, setInstructionFilesEntries] = useState<
    InstructionFilesEntry[]
  >([]);
  const [archiveTip, setArchiveTip] = useState<{
    threadID: string;
    threadTitle: string;
    errorMessage?: string;
  } | null>(null);
  // Archive is now a single-click action (the previous two-step "click again
  // to confirm" pattern was too easy to misfire). Success and failure feedback
  // lives in `archiveTip` above; the underlying IPC still goes through
  // `window.wuu.archiveThread(id, true)`.
  const dismissArchiveTip = useCallback(() => {
    setArchiveTip(null);
  }, []);
  const [checkoutErrorTip, setCheckoutErrorTip] = useState<string | null>(null);
  const dismissCheckoutErrorTip = useCallback(() => {
    setCheckoutErrorTip(null);
  }, []);
  // Mirrors `archiveConfirmSubagentID` (legacy confirm state) for the
  // info-panel subagent rows. The state lives in App rather than the panel
  // so the "press again to confirm" survives the panel being toggled off
  // and on, and so a single archive button click in either surface is
  // consistent.
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
  const environmentToggleRef = useRef<HTMLButtonElement>(null);
  const environmentPanelRef = useRef<HTMLDivElement>(null);
  const appStateRef = useRef<AppState>(initialState);
  const workspaceHasDirtyFilesRef = useRef(false);
  const lastFocusOutsideWorkspaceRef = useRef<HTMLElement | null>(null);
  const previousWorkspaceFocusModeRef = useRef({
    fullPanel: false,
    open: false,
  });
  const {
    pendingComposerMessagesByThread,
    pendingComposerMessagesForThread: pendingComposerMessagesForActiveThread,
    updateThreadPendingComposerMessages,
    clearThreadPendingComposerMessages,
    removePendingComposerMessageByID,
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
    restoreComposerDraftForThread: (threadID, draft) => {
      if (activeThreadIDForState(appStateRef.current) === threadID) {
        restorePrimaryComposerDraft(draft);
        return;
      }
      setState((current) => ({
        ...current,
        sessionTabs: current.sessionTabs.map((tab) =>
          tab.kind === "thread" && tab.threadID === threadID
            ? {
                ...tab,
                prompt: draft.prompt,
                images: draft.images.map((image) => ({ ...image })),
                files: draft.files.map((file) => ({ ...file })),
              }
            : tab,
        ),
      }));
    },
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
  const currentSessionTab = activeSessionTab(state);

  useEffect(() => {
    const handleBeforeUnload = (event: BeforeUnloadEvent): void => {
      if (!workspaceHasDirtyFilesRef.current) {
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

  useEffect(() => {
    const handleFocusIn = (event: FocusEvent): void => {
      const target = event.target;
      const workspacePanel = appShellRef.current?.querySelector(".workspace-right-panel");
      if (target instanceof HTMLElement && !workspacePanel?.contains(target)) {
        lastFocusOutsideWorkspaceRef.current = target;
      }
    };
    document.addEventListener("focusin", handleFocusIn);
    return () => document.removeEventListener("focusin", handleFocusIn);
  }, []);

  useLayoutEffect(() => {
    const fullPanel = rightPanelOpen && rightPanelGlobalized;
    const previous = previousWorkspaceFocusModeRef.current;
    previousWorkspaceFocusModeRef.current = { fullPanel, open: rightPanelOpen };

    appShellRef.current
      ?.querySelector<HTMLElement>(".sidebar")
      ?.toggleAttribute(
        "inert",
        fullPanel && !sidebarDrawerVisible,
      );

    if (fullPanel && !previous.fullPanel) {
      appShellRef.current
        ?.querySelector<HTMLButtonElement>(
          '.workspace-right-panel [role="tab"][aria-selected="true"]',
        )
        ?.focus();
      return;
    }
    if ((previous.fullPanel && !fullPanel) || (previous.open && !rightPanelOpen)) {
      const previousFocus = lastFocusOutsideWorkspaceRef.current;
      if (previousFocus?.isConnected && !previousFocus.closest("[inert]")) {
        previousFocus.focus();
        return;
      }
      appShellRef.current
        ?.querySelector<HTMLButtonElement>(
          '.session-tab-main[aria-selected="true"]',
        )
        ?.focus();
    }
  }, [rightPanelGlobalized, rightPanelOpen, sidebarDrawerVisible]);

  // Workspace panel (file tree / file preview / terminal / review) root: follows the
  // active thread's own cwd when it differs from state.activeContext — the
  // main remaining case is a worktree-fork thread, whose cwd is a git
  // worktree directory distinct from the project root activeContext stays
  // pinned to.
  const conversationWorkspaceContext = useMemo(
    () => workspacePanelContext(state.activeContext, state.thread),
    [state.activeContext, state.thread],
  );
  const workspaceContext =
    rightPanelGlobalized && focusedWorkspaceContext
      ? focusedWorkspaceContext
      : conversationWorkspaceContext;
  const activeWorkspaceViewTab = workspaceActiveViewTabID
    ? workspaceViewTabs.find((tab) => tab.id === workspaceActiveViewTabID)
    : undefined;
  const workspaceProjectSelectionEnabled =
    rightPanelGlobalized &&
    (activeWorkspaceViewTab?.kind === "files" ||
      activeWorkspaceViewTab?.kind === "file");
  const activeWorkspaceFileTab = workspaceActiveFileTabID
    ? workspaceViewTabs.find((tab) => tab.id === workspaceActiveFileTabID)
    : undefined;
  const activeWorkspaceFileTabID =
    activeWorkspaceFileTab?.kind === "file" &&
    sameRuntimeContext(activeWorkspaceFileTab.context, workspaceContext)
      ? activeWorkspaceFileTab.id
      : undefined;
  const activeWorkspaceFile =
    activeWorkspaceFileTab?.kind === "file" && activeWorkspaceFileTabID
      ? activeWorkspaceFileTab.path
      : undefined;
  const activeThread = activeThreadForState(state);
  const activeThreadID = activeThread?.id;
  const activeTabKind = activeSessionTab(state)?.kind;
  const environmentContext = workspacePanelContext(state.activeContext, activeThread);
  const sideThread = useSideThreadController({
    activeThreadId: activeThreadID,
    activeContext: state.activeContext,
  });
  const sideThreadPanelRef = useRef<SideThreadPanelHandle>(null);
  const activeTurn = activeTurnForThread(activeThread);
  useEffect(() => {
    const syncVisibleCUAThread = () => {
      (window.wuu as typeof window.wuu & { setActiveCUAThread?: (threadID?: string) => void })
        .setActiveCUAThread?.(activeThreadID);
    };
    syncVisibleCUAThread();
    window.addEventListener("focus", syncVisibleCUAThread);
    return () => window.removeEventListener("focus", syncVisibleCUAThread);
  }, [activeThreadID]);
  const activeBrowserActivity = useMemo(
    () =>
      activitiesForThread(activitySessions, state.activeContext?.cwd, activeThreadID)
        .filter((activity) => activity.kind === "browser" && activity.state !== "stopped")
        .at(-1),
    [activitySessions, activeThreadID, state.activeContext?.cwd],
  );

  useEffect(() => {
    const workdir = state.activeContext?.cwd;
    if (!workdir || !activeThreadID || typeof window.wuu.listActivities !== "function") {
      return undefined;
    }
    let cancelled = false;
    void window.wuu.listActivities(activeThreadID).then((result) => {
      if (!cancelled) {
        setActivitySessions((current) =>
          mergeActivityList(current, workdir, activeThreadID, result.activities ?? []),
        );
      }
    }).catch(() => {
      // Live notifications still populate the panel when the initial list
      // races app-server startup; ordinary app status handles transport errors.
    });
    return () => {
      cancelled = true;
    };
  }, [activeThreadID, state.activeContext?.cwd]);

  function mergeActivityResponse(activity: ActivitySession): void {
    setActivitySessions((current) =>
      mergeActivityList(current, activity.workdir, activity.thread_id, [activity]),
    );
  }

  async function takeoverBrowserActivity(): Promise<void> {
    if (!activeBrowserActivity) {
      return;
    }
    try {
      const result = await window.wuu.takeoverActivity(
        activeBrowserActivity.thread_id,
        activeBrowserActivity.id,
      );
      mergeActivityResponse(result.activity);
    } catch (error) {
      setState((current) => ({
        ...current,
        status: desktopApiErrorMessage(error, t("app.browserTakeoverFailed")),
      }));
    }
  }

  async function releaseBrowserActivity(): Promise<void> {
    if (!activeBrowserActivity) {
      return;
    }
    try {
      const result = await window.wuu.releaseActivity(
        activeBrowserActivity.thread_id,
        activeBrowserActivity.id,
      );
      mergeActivityResponse(result.activity);
    } catch (error) {
      setState((current) => ({
        ...current,
        status: desktopApiErrorMessage(error, t("app.browserReleaseFailed")),
      }));
    }
  }

  async function stopBrowserActivity(): Promise<void> {
    if (!activeBrowserActivity) {
      return;
    }
    try {
      const result = await window.wuu.stopActivity(
        activeBrowserActivity.thread_id,
        activeBrowserActivity.id,
      );
      mergeActivityResponse(result.activity);
    } catch (error) {
      setState((current) => ({
        ...current,
        status: desktopApiErrorMessage(error, t("app.browserStopFailed")),
      }));
    }
  }
  const sessionRuntime = useMemo(
    () => runtimeViewForSession(state.initialized, activeThread),
    [state.initialized, activeThread],
  );
  const visibleConversationRuntime = useMemo(
    () => runtimeViewForConversation(state.initialized, activeThread, activeTurn),
    [state.initialized, activeThread, activeTurn],
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
    // Use the state snapshot that produced this render. appStateRef is synced
    // by a later effect, so reading it here can observe the previous thread
    // snapshot and permanently miss a materialized queued user_message.
    reconcilePendingComposerMessagesForState(state);
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
    selectConversationSearchResult,
    handleConversationSearchKeyDown,
    setConversationSearchQuery,
    clearConversationSearchQuery,
    setConversationSearchSelectedIndex,
  } = useConversationSearch({
    activeContext: state.activeContext,
    getAppState: () => appStateRef.current,
    cacheThreads: cacheSidebarThreads,
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
  // A visible agent browser view is a main-owned WebContentsView that floats
  // above the DOM; any full-window overlay would occlude it, so we tell main to
  // hide the view while one is open. This covers the common full-window
  // surfaces: settings (which replaces the whole app), the commit / PR
  // dialogs, the fork dialog, and the conversation search overlay. TODO: other
  // ad-hoc modals/portals (e.g. participant panels) are not yet
  // enumerated here; extend this predicate as more full-window overlays land.
  const browserOverlaySuppressed =
    settingsOpen ||
    environmentDialog !== null ||
    Boolean(pendingFork) ||
    conversationSearch.open;
  useBrowserVisibility({
    activeThreadID,
    activeBrowserActivity,
    overlaySuppressed: browserOverlaySuppressed,
    onOpenBrowser: () => openWorkspaceTool("browser"),
    onInvalidateWorkdir: (workdir) =>
      setActivitySessions((current) =>
        clearActivitiesForWorkdir(current, workdir),
      ),
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
      ? t("app.worktreeRequiresGit")
      : undefined;
  const splitConversation = Boolean(state.thread && state.secondaryThread);

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
    const markUserInteraction = (): void => {
      userInteractionVersionRef.current += 1;
    };
    document.addEventListener("pointerdown", markUserInteraction, true);
    document.addEventListener("keydown", markUserInteraction, true);
    return () => {
      document.removeEventListener("pointerdown", markUserInteraction, true);
      document.removeEventListener("keydown", markUserInteraction, true);
    };
  }, []);

  useEffect(() => {
    void refreshGoalSummary();
  }, [refreshGoalSummary]);

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
    // Deltas arrive at tens per second; a setState per delta would re-render
    // the whole App tree just to advance the token-speed gauge. Accumulate
    // the estimates here and fold them into state at most every 250ms.
    const pendingTokenSamples: PendingStreamingTokenSamples = new Map();
    let tokenSampleFlushTimer: number | undefined;
    const flushTokenSamples = () => {
      tokenSampleFlushTimer = undefined;
      if (!mounted || pendingTokenSamples.size === 0) {
        return;
      }
      const batch = new Map(pendingTokenSamples);
      pendingTokenSamples.clear();
      setState((current) =>
        flushPendingStreamingTokenSamples(current, batch, Date.now()),
      );
    };
    const off = window.wuu.onServerEvent((event) => {
      if (!mounted) {
        return;
      }
      setActivitySessions((current) => reduceActivitySessionEvent(current, event));
      if (!serverEventTargetsActiveContext(event, appStateRef.current)) {
        return;
      }
      recordRunDebugEvent(event);
      const handling = handleStreamingNotification(event, appStateRef.current);
      if (handling === "stream" || handling === "stream-state") {
        // The first visible delta still needs to mount and reveal the live
        // surface. Subsequent text commits call onStreamFrame after the
        // throttled StreamTextStore publication; scrolling here as well would
        // schedule two layout passes for every provider delta, including
        // passes before the DOM contains the new text.
        if (handling === "stream-state") {
          scheduleStreamScroll();
        }
        if (
          event.kind === "notification" &&
          serverEventCarriesModelOutputDelta(event) &&
          recordPendingStreamingTokenSample(
            pendingTokenSamples,
            event.message.params as Record<string, unknown> | undefined,
          ) &&
          tokenSampleFlushTimer === undefined
        ) {
          tokenSampleFlushTimer = window.setTimeout(flushTokenSamples, 250);
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
          status: error instanceof Error ? error.message : t("runtime.startFailed"),
        }));
      }
    })();

    return () => {
      mounted = false;
      off();
      if (tokenSampleFlushTimer !== undefined) {
        window.clearTimeout(tokenSampleFlushTimer);
        tokenSampleFlushTimer = undefined;
      }
      if (gitRefreshTimerRef.current !== undefined) {
        window.clearTimeout(gitRefreshTimerRef.current);
        gitRefreshTimerRef.current = undefined;
      }
    };
  }, [popOutInit]);

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
  ]);

  useEffect(() => {
    scheduleGitStatusRefresh(0);
  }, [
    state.activeContext?.kind,
    environmentContext?.cwd,
    state.activeProjectId,
    activeThreadID,
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
  const activeTitle = showingSkillsCatalog
    ? t("skills.title")
    : resolveLocalizedText(activeThread?.preview ?? "") ||
      t("tabs.newConversation");
  const popOutWindowTitle =
    popOutInit?.kind === "thread"
      ? activeThread?.title?.trim() ||
        resolveLocalizedText(activeThread?.preview?.trim() ?? "") ||
        t("tabs.newConversation")
      : t("tabs.newConversation");
  useEffect(() => {
    if (!poppedOutMode) {
      return;
    }
    document.title = `wuu · ${popOutWindowTitle}`;
  }, [poppedOutMode, popOutWindowTitle]);
  const currentHour = useCurrentHour();
  const greetingContext: GreetingContext =
    state.activeContext?.kind === "project"
      ? {
          kind: "project",
          projectName: activeProject?.name ?? t("greeting.projectFallback"),
        }
      : { kind: "wuu" };
  const emptyThreadTitle = greetingFor(currentHour, greetingContext);
  const [pendingNewThreadTurn, setPendingNewThreadTurn] = useState<{
    sessionTabID: string;
    turn: Turn;
  }>();
  const activePendingNewThreadTurn =
    pendingNewThreadTurn?.sessionTabID === state.activeSessionTabID
      ? pendingNewThreadTurn.turn
      : undefined;
  const turns = activeThread?.turns ?? [];
  const activeContextCompositionEntries = activeThreadID
    ? contextCompositionEntries.filter((entry) => entry.threadID === activeThreadID)
    : [];
  const emptyConversation =
    !showingSkillsCatalog &&
    !activePendingNewThreadTurn &&
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
  const [mainConversationScrolledAway, setMainConversationScrolledAway] =
    useState(false);
  const mainConversationDockVisible =
    Boolean(state.initialized) &&
    !previewingLaunch &&
    !emptyConversation &&
    !splitConversation &&
    !showingSkillsCatalog &&
    !rightPanelGlobalized;

  useEffect(() => {
    // Diff tabs are scoped to the thread whose turn they came from: they
    // don't make sense to keep browsing once we've navigated away from
    // that thread (or away from the conversation view entirely), so prune
    // them eagerly instead of leaving stale diffs sitting in the tab strip.
    const isStaleDiffTab = (tab: WorkspaceViewTab): boolean =>
      tab.kind === "diff" &&
      (!activeThreadID ||
        tab.threadID !== activeThreadID ||
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
    enableConversationAutoFollow,
    disableConversationAutoFollow,
    captureConversationScrollPosition,
    restoreConversationScrollPosition,
  } = useConversationScrollState({
    activeThreadID,
    activePane: state.activePane,
    splitConversation,
    primaryTurns: state.thread?.turns,
    secondaryTurns: state.secondaryThread?.turns,
    emptyConversation,
    previewingLaunch,
    initialized: Boolean(state.initialized),
  });
  const conversationRailScrollContainer = useCallback((): HTMLElement | null => {
    if (splitConversation) {
      return splitPaneRefs.current[state.activePane] ?? null;
    }
    return conversationScrollRef.current;
  }, [conversationScrollRef, splitConversation, splitPaneRefs, state.activePane]);
  const focusMainComposer = useCallback(
    (
      target: ComposerVariant,
      origin: Element | null,
      interactionVersion: number,
    ): boolean => {
      const composer = conversationPaneRef.current?.querySelector<HTMLElement>(
        `[data-main-conversation-composer="${target}"]`,
      );
      const textarea = composer?.querySelector<HTMLTextAreaElement>("textarea");
      if (!textarea || textarea.disabled) {
        return false;
      }

      const activeElement = document.activeElement;
      if (
        userInteractionVersionRef.current === interactionVersion &&
        (activeElement === document.body || activeElement === origin)
      ) {
        textarea.focus();
      }
      return true;
    },
    [conversationPaneRef],
  );
  const requestMainComposerFocus = useCallback(
    (
      target: ComposerVariant,
      origin: Element | null = document.activeElement,
      interactionVersion: number = userInteractionVersionRef.current,
    ): MainComposerFocusRequest => {
      const request = {
        target,
        origin,
        interactionVersion,
      };
      setMainComposerFocusRequest(request);
      return request;
    },
    [],
  );
  const cancelMainComposerFocusRequest = useCallback(
    (request: MainComposerFocusRequest): void => {
      setMainComposerFocusRequest((current) =>
        current === request ? null : current,
      );
    },
    [],
  );

  useLayoutEffect(() => {
    if (!mainComposerFocusRequest) {
      return;
    }
    if (
      !focusMainComposer(
        mainComposerFocusRequest.target,
        mainComposerFocusRequest.origin,
        mainComposerFocusRequest.interactionVersion,
      )
    ) {
      return;
    }
    setMainComposerFocusRequest((current) =>
      current === mainComposerFocusRequest ? null : current,
    );
  }, [
    emptyConversation,
    focusMainComposer,
    mainComposerFocusRequest,
    mainConversationDockVisible,
    state.activeSessionTabID,
  ]);
  const handleTurnCollapseComplete = useCallback(() => {
    scheduleStreamScroll();
  }, [scheduleStreamScroll]);

  // Captures the user's scroll position when they open the inline edit on a
  // historical message so cancel can put them back exactly where they were
  // instead of dropping them at the latest content via the resize observer.
  const preEditScrollSnapshotRef = useRef<
    | {
        scrollTop: number;
        autoFollow: boolean;
      }
    | undefined
  >(undefined);
  const rememberConversationScrollForEdit = useCallback((): void => {
    preEditScrollSnapshotRef.current = captureConversationScrollPosition();
  }, [captureConversationScrollPosition]);
  const restoreConversationScrollForEdit = useCallback((): void => {
    const snapshot = preEditScrollSnapshotRef.current;
    preEditScrollSnapshotRef.current = undefined;
    if (!snapshot) {
      return;
    }
    restoreConversationScrollPosition(snapshot);
  }, [restoreConversationScrollPosition]);
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
  const handleCachedPaneOpenFileDiff = useStableCallback(
    (thread: Thread, selection: TurnFileDiffSelection) => {
      openTurnFileDiffPanel(thread.id, selection);
    },
  );
  const handleOpenTurnRuns = useStableCallback((thread: Thread, turnID: string): void => {
    setTerminalRunRequest((current) => ({
      threadID: thread.id,
      turnID,
      requestID: (current?.requestID ?? 0) + 1,
    }));
    openWorkspaceTool("terminal");
  });
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
    openWorkspaceFileTab({ context, path });
  });
  const openWorkspaceFileForThread = useStableCallback((thread: Thread, path: string): void => {
    const context = workspacePanelContext(appStateRef.current.activeContext, thread);
    if (!context) {
      return;
    }
    openWorkspaceFileTab({ context, path });
  });
  const rememberWorkspaceDirtyFiles = useStableCallback((dirty: boolean): void => {
    workspaceHasDirtyFilesRef.current = dirty;
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
      [state.activeProjectId]: mergeSidebarThreadSnapshots(
        projectThreadsByProjectID[state.activeProjectId],
        threadsForDesktopProject(state.threads, activeProject),
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
      next[projectID] = summarizeThreadsForSidebar(threads);
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
  // The scratch pseudo project lives at the top of the sidebar tree. It is
  // a synthetic DesktopProject (id = SCRATCH_PSEUDO_PROJECT_ID) whose
  // threads are the scratch conversations pulled out of
  // sidebarThreadSummaries above. path is intentionally "" — ThreadSidebar
  // special-cases the scratch pseudo id and skips its cwd-path filter.
  const scratchPseudoProject = useMemo<DesktopProject>(
    () => ({
      id: SCRATCH_PSEUDO_PROJECT_ID,
      name: t("sidebar.conversations"),
      path: "",
      created_at: new Date(0).toISOString(),
      updated_at: new Date(0).toISOString(),
    }),
    [t],
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
  const activeThreadCanSteer = Boolean(activeTurnIDForThread(activeThread));
  const activeThreadStreamStatus = turnStreamStatusForThread(state, activeThread);
  const anyThreadIsRunning = isAnyThreadRunning(state) || viewContextSwitchPending;
  const runningThreadKey = useMemo(() => {
    const running = new Set<string>();
    for (const thread of [state.thread, state.secondaryThread, ...state.threads]) {
      if (thread?.cwd && isThreadRunning(thread)) {
        running.add(`${thread.id}\0${thread.cwd}`);
      }
    }
    if (state.running && activeThread?.cwd) {
      running.add(`${activeThread.id}\0${activeThread.cwd}`);
    }
    return [...running].sort().join("\x01");
  }, [
    activeThread?.cwd,
    activeThread?.id,
    state.running,
    state.secondaryThread,
    state.thread,
    state.threads,
  ]);
  const activeWorkingTreeBusy = useGitActionBusy(
    environmentContext,
    runningThreadKey,
  );
  // The Environment panel's git actions (branch switch / commit / PR) mutate the
  // active session's working tree, so they are gated on that tree being busy —
  // not on any thread anywhere running. A worktree-fork thread running in its
  // own cwd, or a thread in another project, no longer blocks them.
  const environmentGitBusy = activeWorkingTreeBusy || viewContextSwitchPending;
  // The desktop pet lives in its own always-on-top window owned by the main
  // process; the renderer only feeds it the session runtime so its sprite
  // state tracks what the app is doing.
  useEffect(() => {
    const api = window.wuu as Partial<typeof window.wuu>;
    if (typeof api.updateCodexPetRuntime !== "function") {
      return;
    }
    void api
      .updateCodexPetRuntime({
        running: anyThreadIsRunning,
        status: resolveLocalizedText(state.status),
      })
      .catch(() => undefined);
  }, [anyThreadIsRunning, locale, state.status]);
  // The pet bubble is a lightweight hint of the most relevant session.
  // Re-derive whenever the thread state changes and push the result to
  // the main process, which keeps the always-on-top pet window in sync.
  // See ./activeSessionHint for the priority logic.
  // `unreadThreadIDs` lets an idle thread outrank a plain idle one when its
  // latest completed turn has not been viewed yet, so a finished-but-unread
  // conversation still surfaces in the bubble.
  const unreadThreadIDs = useMemo(() => {
    const ids = new Set<string>();
    for (const thread of [state.thread, state.secondaryThread, ...state.threads]) {
      if (!thread?.id) continue;
      if (
        isThreadUnread(
          thread,
          state.lastViewedTurnByThreadID[thread.id],
        )
      ) {
        ids.add(thread.id);
      }
    }
    return ids;
  }, [
    state.thread,
    state.secondaryThread,
    state.threads,
    state.lastViewedTurnByThreadID,
  ]);
  useEffect(() => {
    const api = window.wuu as Partial<typeof window.wuu>;
    if (typeof api.updateCodexPetHints !== "function") return;
    const hints = deriveActiveSessionHints({
      thread: state.thread ?? undefined,
      secondaryThread: state.secondaryThread ?? undefined,
      threads: state.threads,
      unreadThreadIDs,
    });
    void api.updateCodexPetHints(hints).catch(() => undefined);
  }, [
    state.thread,
    state.secondaryThread,
    state.threads,
    unreadThreadIDs,
  ]);
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
  const sideThreadPanelVisible = Boolean(activeThreadID && sideThread.entry?.open);
  useEffect(() => {
    if (!sideThreadPanelVisible) {
      return undefined;
    }
    const frame = window.requestAnimationFrame(() => {
      sideThreadPanelRef.current?.focusComposer();
    });
    return () => window.cancelAnimationFrame(frame);
  }, [sideThreadPanelVisible]);
  // The environment panel floats inside the conversation pane, so it can
  // coexist with the docked workspace right panel. Only the globalized
  // (full-window sheet) right panel blocks it, because that mode makes the
  // entire conversation pane inert.
  const environmentPanelCanShow = Boolean(
    state.initialized &&
    !poppedOutMode &&
    !previewingLaunch &&
    !rightPanelGlobalized &&
    !sideThreadPanelVisible,
  );
  const environmentPanelTargetVisible =
    environmentPanelCanShow &&
    (environmentPanelOpen ||
      (environmentPanelHasRoom &&
        !environmentPanelDismissed &&
        !emptyConversation));
  const environmentPanelVisible = environmentPanelTargetVisible;
  const environmentPanelMotionState: EnvironmentPanelMotionState =
    environmentPanelVisible ? "open" : "closing";
  const sessionTabsVisible = Boolean(
    state.initialized && !previewingLaunch && !poppedOutMode,
  );
  const sidebarVisible = !poppedOutMode;

  useEffect(() => {
    if (sideThread.entry?.open && environmentPanelOpen) {
      sideThread.close();
    }
  }, [environmentPanelOpen, sideThread.close, sideThread.entry?.open]);

  const shellClassName = `app-shell${poppedOutMode ? " popped-out-shell" : ""}${sidebarDrawerMode ? " sidebar-collapsed" : ""}${
    sidebarDrawerMode && sidebarDrawerVisible ? " sidebar-drawer-open" : ""
  }${
    sidebarDrawerMode &&
    !(!sidebarCollapsed && rightPanelGlobalized) &&
    sidebarDrawerPhase === "closing"
      ? " sidebar-drawer-closing"
      : ""
  }${
    !sidebarDrawerMode && sidebarDrawerPhase === "docking"
      ? " sidebar-drawer-docking"
      : ""
  }${
    sidebarAnimating ? " sidebar-animating" : ""
  }${rightPanelAnimating ? " right-panel-animating" : ""}${resizingSidebar ? " resizing-sidebar" : ""}${
    resizingRightPanel ? " resizing-right-panel" : ""
  }${rightPanelOpen ? " right-panel-open" : ""}${rightPanelGlobalized && rightPanelOpen ? " right-panel-globalized" : ""}${resizingSplit ? " resizing-split" : ""}`;
  const shellStyle = {
    "--sidebar-width": `${effectiveSidebarWidth}px`,
    "--sidebar-open-width": `${sidebarWidth}px`,
    "--workspace-right-panel-width": `${clampedWorkspaceRightPanelWidth}px`,
    "--side-thread-width": `${sideThread.width}px`,
    "--conversation-split-left": `${splitLeftPercent}%`,
    "--environment-panel-width": ENVIRONMENT_PANEL_WIDTH_CSS,
    "--environment-panel-reserved-width": "372px",
    "--environment-panel-edge-gap": "18px",
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

  function openSideThreadPanel(): void {
    if (!activeThreadID) {
      return;
    }
    if (!sideThread.entry?.open) {
      setEnvironmentPanelOpen(false);
      setEnvironmentPanelDismissed(true);
      setEnvironmentPanelMenu(null);
      sideThread.open();
    }
  }

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
    const streamStatus = activeThreadStreamStatus;
    return (
      <Composer
        variant={variant}
        mainConversation
        topAccessory={undefined}
        containerRef={variant === "dock" ? dockComposerRef : undefined}
        prompt={prompt}
        setPrompt={setPrompt}
        files={composerFiles}
        images={composerImages}
        queuedMessages={queuedMessages}
        guideMessages={guideMessages}
        running={
          (!activeThreadReadOnly && activeThreadIsRunning) ||
          viewContextSwitchPending
        }
        ultraEnabled={ENABLE_ULTRA_MODE && Boolean(state.initialized?.ultra)}
        onToggleUltra={
          ENABLE_ULTRA_MODE
            ? (enabled) => {
                void updateUltraMode(enabled).catch(() => undefined);
              }
            : undefined
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
              ? t("app.childTaskRunning")
              : t("app.childTaskReadOnly")
            : streamStatus?.text ?? state.status
        }
        statusLiveProgress={
          activeThreadReadOnly
            ? false
            : streamStatus?.liveProgress
        }
        readOnly={activeThreadReadOnly}
        initialized={visibleConversationRuntime}
        gitStatus={state.gitStatus}
        projects={state.projects}
        activeContext={state.activeContext}
        activeProject={activeProject}
        compactDisabledReason={
          !activeThread ? t("app.openConversationFirst") : undefined
        }
        sideThreadDisabledReason={
          !activeThread ? t("app.sendMessageFirst") : undefined
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
        onStartNewThread={startNewThreadWithComposerFocus}
        onOpenSideThread={openSideThreadPanel}
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
        onSend={(promptOverride) => void sendPrompt("queue", promptOverride)}
        onSteer={
          activeThreadIsRunning && activeThread && activeThreadCanSteer
            ? (promptOverride) => void sendPrompt("steer", promptOverride)
            : undefined
        }
        onQueue={
          activeThreadIsRunning && activeThread
            ? () => void sendPrompt("queue")
            : undefined
        }
        onInterrupt={() => void interrupt()}
        goalSummary={goalSummary}
        onEditGoal={editGoalText}
        onPauseGoal={pauseCurrentGoal}
        onResumeGoal={resumeCurrentGoal}
        onClearGoal={clearCurrentGoal}
        queryHistorySessionID={activeThread?.id}
        queryHistory={queryTextsForThread(activeThread)}
      />
    );
  }

  function handleEmptyStateHint(action: EmptyStateHintAction): void {
    if (action.kind === "openSettings") {
      closeProjectMenus();
      setSettingsInitialPage("providers");
      setSettingsOpen(true);
    }
  }

  function openArchiveSettings(): void {
    // Used by both the sidebar entry (when one is added later) and the
    // archive-tip toast: always jump the Settings shell to the Archive page,
    // even if Settings was already open on a different tab.
    setProjectMenuOpen(false);
    setRuntimeMenuOpen(false);
    setCodexRuntimeMenu(null);
    setSettingsInitialPage("archive");
    setSettingsOpen(true);
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
    scheduleGitStatusRefresh,
    createAndCheckoutBranch,
    commitEnvironmentChanges,
    createEnvironmentPullRequest,
    toggleEnvironmentPanel,
    openEnvironmentPanel,
    closeEnvironmentPanel,
  } = createEnvironmentActions({
    getAppState: () => appStateRef.current,
    getEnvironmentRoot: () =>
      workspacePanelContext(
        appStateRef.current.activeContext,
        activeThreadForState(appStateRef.current),
      )?.cwd,
    setAppState: setState,
    getAnyThreadIsRunning: () => environmentGitBusy,
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
    restorePrimaryComposerDraft,
    clearPrimaryComposerDraft: () =>
      restorePrimaryComposerDraft(emptyComposerDraft()),
    restoreLoadedRuntimeComposerDraft,
    nextDraftSessionTab,
    closeProjectMenus,
    
    beginViewSwitch,
    finishViewSwitch,
    cancelViewSwitch,
    loadRuntime,
  });

  const {
    selectThread,
    selectProjectThread,
    activateThread,
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
    
    beginViewSwitch,
    beginInstantThreadSwitch,
    finishViewSwitch,
    cancelViewSwitch,
    isCurrentViewSwitchRequest,
    loadRuntime,
    selectRuntimeContext,
  });

  // The pet bubble click sends a `wuu:codex-pet-jump` event from main;
  // bring the conversation forward and switch to the target thread.
  // Placed here (after `activateThread` is destructured) to avoid TDZ.
  useEffect(() => {
    const api = window.wuu as Partial<typeof window.wuu>;
    if (typeof api.onCodexPetJumpRequest !== "function") return;
    return api.onCodexPetJumpRequest((event) => {
      revealConversationFromFocusedWorkspace();
      void activateThread(event.thread_id);
    });
  }, [activateThread, revealConversationFromFocusedWorkspace]);

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
    beginViewSwitch,
    beginInstantThreadSwitch,
    finishViewSwitch,
    cancelViewSwitch,
    loadRuntime,
    selectRuntimeContext,
  });

  function focusHeroAfter(
    action: Promise<void | boolean>,
    origin: Element | null,
    matchesDestination: (state: AppState) => boolean,
  ): void {
    const interactionVersion = userInteractionVersionRef.current;
    void action.then((succeeded) => {
      if (succeeded === false) {
        return;
      }
      window.requestAnimationFrame(() => {
        const current = appStateRef.current;
        if (
          !current.thread &&
          !current.secondaryThread &&
          activeSessionTab(current)?.kind === "draft" &&
          matchesDestination(current)
        ) {
          // Queue the focus through the layout effect instead of making a
          // one-shot attempt in this frame. React may not have committed the
          // destination hero composer yet, so the request must survive until
          // that element is mounted. Preserve the interaction version from
          // the original click so later user input still cancels the handoff.
          requestMainComposerFocus("hero", origin, interactionVersion);
        }
      });
    });
  }

  function startNewThreadWithComposerFocus(): void {
    const origin = document.activeElement;
    const context = appStateRef.current.activeContext;
    focusHeroAfter(
      startNewThread(),
      origin,
      (current) => sameRuntimeContext(current.activeContext, context),
    );
  }

  function trySkillFromCatalog(skill: { name: string }): void {
    const origin = document.activeElement;
    const context = appStateRef.current.activeContext;
    if (!context) {
      return;
    }
    revealConversationFromFocusedWorkspace();
    void startNewThread().then(() => {
      setComposerImages([]);
      setComposerFiles([]);
      setPrompt(`/${skill.name} `);
      requestMainComposerFocus("hero", origin);
    });
  }

  function startNewThreadForProjectWithComposerFocus(id: string): void {
    const origin = document.activeElement;
    focusHeroAfter(
      id === SCRATCH_PSEUDO_PROJECT_ID
        ? useNoProject(true)
        : startNewThreadForProject(id),
      origin,
      (current) =>
        id === SCRATCH_PSEUDO_PROJECT_ID
          ? current.activeContext?.kind === "no_project"
          : current.activeContext?.kind === "project" &&
            current.activeProjectId === id,
    );
  }

  const {
    toggleThreadPinned,
    renameThread,
    archiveThread,
    unarchiveThread,
    deleteThread,
    toggleSubagentPinned,
    archiveSubagent,
  } = createThreadMutationActions({
    getAppState: () => appStateRef.current,
    setAppState: setState,
    getActiveThreadID: () => activeThreadID,
    getArchiveConfirmSubagentID: () => archiveConfirmSubagentID,
    
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
    updateUltraMode,
    updateAdvancedSettings,
    updateGeneralSettings,
    removeProvider,
    toggleCodexRuntimeMenu,
    selectRuntimeModel,
    selectRuntimeEffort,
    selectPermissionMode,
    interrupt,
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
    clearThreadPendingComposerMessages,
  });

  const {
    openSkillsTab,
    dismissContextCompositionEntry,
    dismissInstructionFilesEntry,
    openInstructions,
    openContextComposition,
    openMemorySettings,
  } = createWorkspaceActions({
    getAppState: () => appStateRef.current,
    setAppState: setState,
    getActiveTitle: () => activeTitle,
    getPrimaryComposerDraft: currentPrimaryComposerDraft,
    setSplitComposerDrafts,
    setPrompt,
    setComposerImages,
    setComposerFiles,
    
    cancelViewSwitch,
    setContextCompositionEntries,
    setInstructionFilesEntries,
    scheduleStreamScroll,
    closeProjectMenus,
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
    disableConversationAutoFollow,
    enableConversationAutoFollow,
    rememberConversationScrollForEdit,
    restoreConversationScrollForEdit,
    threadHasPendingComposerMessages,
    sendComposerMessageToThread,
    worktreeForkNonGitReason: t("app.worktreeRequiresGit"),
  });

  async function sendPrompt(
    runningAction: "queue" | "steer" = "queue",
    promptOverride?: string,
  ): Promise<void> {
    if (viewSwitchPending) {
      return;
    }
    const draftMessage = createComposerMessage(
      promptOverride ?? prompt,
      composerImages,
      composerFiles,
    );
    const message =
      draftMessage && activeWorkspaceFile
        ? { ...draftMessage, activeDocument: { path: activeWorkspaceFile } }
        : draftMessage;
    const currentState = appStateRef.current;
    const targetThread = activeThreadForState(currentState);
    if (targetThread?.read_only) {
      setState((current) => ({
        ...current,
        status: localizedText("app.childTaskReadOnly"),
      }));
      return;
    }
    if (!message || !currentState.activeContext || !currentState.initialized) {
      return;
    }
    let focusRequest: MainComposerFocusRequest | undefined;
    if (emptyConversation) {
      const activeElement = document.activeElement;
      if (
        activeElement === document.body ||
        activeElement?.closest("[data-main-conversation-composer]")
      ) {
        focusRequest = requestMainComposerFocus("dock", activeElement);
      }
    }
    setPrompt("");
    setComposerImages([]);
    setComposerFiles([]);
    if (isStateActiveThreadRunning(currentState)) {
      const resolvedAction = resolveComposerRunningAction(runningAction, targetThread);
      const sent = resolvedAction === "steer"
        ? await steerComposerMessage(message, targetThread)
        : await queueComposerMessage(message, targetThread);
      if (!sent) {
        setPrompt(message.text);
        setComposerImages(message.images);
        setComposerFiles(message.files);
      }
      return;
    }
    const sent = await sendComposerMessage(message, true);
    if (!sent && focusRequest) {
      cancelMainComposerFocusRequest(focusRequest);
      requestMainComposerFocus(
        "hero",
        focusRequest.origin,
        focusRequest.interactionVersion,
      );
    }
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
      setState((current) => ({
        ...current,
        status: localizedText("app.openConversationFirst"),
      }));
      return;
    }
    if (targetThread.read_only) {
      setState((current) => ({
        ...current,
        status: localizedText("app.childTaskReadOnly"),
      }));
      return;
    }
    if (isStateActiveThreadRunning(currentState)) {
      setState((current) => ({
        ...current,
        status: localizedText("app.currentTaskRunning"),
      }));
      return;
    }

    enableConversationAutoFollow();
    resetRunDebugEvents({
      source: "client",
      method: "client/compact",
      detail: t("app.compactionStarting"),
      tone: "running",
      threadID: targetThread.id,
    });
    appStateRef.current = {
      ...currentState,
      running: true,
      status: localizedText("app.compactingContext"),
    };
    setState((current) => ({
      ...current,
      running: true,
      status: localizedText("app.compactingContext"),
    }));

    const optimisticTurn = createOptimisticCompactTurn(Date.now());
    const optimisticTurnID = optimisticTurn.id;
    appStateRef.current = updateThreadByID(
      appStateRef.current,
      targetThread.id,
      (thread) => upsertTurn(thread, optimisticTurn),
      { running: true, status: localizedText("app.compactingContext") },
    );
    setState((current) =>
      updateThreadByID(
        current,
        targetThread.id,
        (thread) => upsertTurn(thread, optimisticTurn),
        { running: true, status: localizedText("app.compactingContext") },
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
        { running: true, status: localizedText("app.compactingContext") },
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
          { running: true, status: localizedText("app.compactingContext") },
        ),
      );
      appendRunDebugEvent({
        source: "client",
        method: "thread/compact/start response",
        detail: t("app.compactionAccepted"),
        tone: "running",
        threadID: targetThread.id,
        turnID: result.turn.id,
      });
    } catch (error) {
      const rawMessage = rawErrorMessage(error, t("composer.compactFailed"));
      const errorMessage = statusMessageForError(rawMessage, t("composer.compactFailed"));
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
    enqueueComposerMessage(targetThread.id, message);
    try {
      const encodedImages = await awaitComposerImages(message.images);
      const images = inputImagesFromComposer(encodedImages);
      const result = await window.wuu.queueTurn(
        targetThread.id,
        text,
        images,
        message.id,
        files,
        targetThread.permission_mode || currentState.initialized.permissions?.mode,
        message.activeDocument,
      );
      updateThreadPendingComposerMessages(targetThread.id, (previous) => ({
        ...previous,
        queued: previous.queued.map((candidate) =>
          candidate.id === message.id
            ? {
                ...candidate,
                id: result.queued.id || message.id,
                images: encodedImages,
              }
            : candidate,
        ),
      }));
      return true;
    } catch (error) {
      removePendingComposerMessageByID(targetThread.id, message.id, "queue");
      setState((current) => ({
        ...current,
        status:
          error instanceof Error ? error.message : t("app.queueFailed"),
      }));
      return false;
    }
  }

  async function steerComposerMessage(
    message: QueuedComposerMessage,
    targetThread = activeThreadForState(appStateRef.current),
  ): Promise<boolean> {
    const currentState = appStateRef.current;
    const text = message.text.trim();
    const files = inputFilesFromComposer(message.files);
    const turnID = targetThread ? activeTurnIDForThread(targetThread) : undefined;
    if (
      (!text && message.images.length === 0 && files.length === 0) ||
      !targetThread ||
      targetThread.read_only ||
      !turnID ||
      !currentState.activeContext ||
      !currentState.initialized ||
      viewSwitchPending
    ) {
      return false;
    }
    updateThreadPendingComposerMessages(targetThread.id, (previous) => ({
      ...previous,
      guides: [...previous.guides, { ...message, origin: "steer" }],
    }));
    try {
      const encodedImages = await awaitComposerImages(message.images);
      await window.wuu.steerTurn(
        targetThread.id,
        turnID,
        text,
        inputImagesFromComposer(encodedImages),
        message.id,
        files,
        message.activeDocument,
      );
      updateThreadPendingComposerMessages(targetThread.id, (previous) => ({
        ...previous,
        guides: previous.guides.map((candidate) =>
          candidate.id === message.id
            ? { ...candidate, images: encodedImages }
            : candidate,
        ),
      }));
      return true;
    } catch (error) {
      removePendingComposerMessageByID(targetThread.id, message.id, "guide");
      setState((current) => ({
        ...current,
        status:
          error instanceof Error ? error.message : t("composer.guideFailed"),
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
      isStateActiveThreadRunning(currentState)
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
      status: localizedText("app.sendingRequest"),
    };
    setState((current) => ({
      ...current,
      running: true,
      status: localizedText("app.sendingRequest"),
    }));
    let optimisticTurnID: string | undefined;
    let optimisticThreadID: string | undefined;
    // Render a tab-scoped optimistic turn before a new thread exists. Once
    // thread/start returns, the same turn moves into normal thread state.
    const optimisticTurn = createOptimisticTurn(message, sendClickedAtMs);
    if (!targetThread && currentState.activeSessionTabID) {
      setPendingNewThreadTurn({
        sessionTabID: currentState.activeSessionTabID,
        turn: optimisticTurn,
      });
    }
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
      setPendingNewThreadTurn((current) =>
        current?.turn.id === optimisticTurn.id ? undefined : current,
      );
      const encodedImages = await awaitComposerImages(message.images);
      const images = inputImagesFromComposer(encodedImages);
      const result = await window.wuu.startTurn(
        thread.id,
        text,
        images,
        files,
        thread.permission_mode || currentState.initialized.permissions?.mode,
        message.activeDocument,
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
        detail: t("app.turnAccepted"),
        tone: "running",
        threadID: thread.id,
        turnID: result.turn.id,
      });
    } catch (error) {
      const rawMessage = rawErrorMessage(error, t("composer.sendFailed"));
      const errorMessage = statusMessageForError(rawMessage, t("composer.sendFailed"));
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
      setPendingNewThreadTurn((current) =>
        current?.turn.id === optimisticTurn.id ? undefined : current,
      );
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
      setState((current) => ({
        ...current,
        status: localizedText("app.childTaskReadOnly"),
      }));
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
    if (isThreadRunning(targetThread)) {
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
      isThreadRunning(targetThread)
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
      status: localizedText("app.sendingRequest"),
    };
    setState((current) => ({
      ...current,
      activePane: pane,
      running: true,
      status: localizedText("app.sendingRequest"),
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
        targetThread.permission_mode || currentState.initialized.permissions?.mode,
        message.activeDocument,
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
        detail: t("app.turnAccepted"),
        tone: "running",
        threadID: targetThread.id,
        turnID: result.turn.id,
      });
    } catch (error) {
      const rawMessage = rawErrorMessage(error, t("composer.sendFailed"));
      const errorMessage = statusMessageForError(rawMessage, t("composer.sendFailed"));
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
      isThreadRunning(targetThread)
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
        status: localizedText("app.sendingRequest"),
      };
      setState((current) => ({
        ...current,
        running: true,
        status: localizedText("app.sendingRequest"),
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
        targetThread.permission_mode || currentState.initialized.permissions?.mode,
        message.activeDocument,
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
          detail: t("app.turnAccepted"),
          tone: "running",
          threadID: targetThread.id,
          turnID: result.turn.id,
        });
      }
    } catch (error) {
      const rawMessage = rawErrorMessage(error, t("composer.sendFailed"));
      const errorMessage = statusMessageForError(rawMessage, t("composer.sendFailed"));
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

  const archiveTipNode = archiveTip ? (
    <ArchiveTip
      threadTitle={archiveTip.threadTitle}
      errorMessage={archiveTip.errorMessage}
      onViewArchive={() => {
        dismissArchiveTip();
        openArchiveSettings();
      }}
      onDismiss={dismissArchiveTip}
    />
  ) : null;

  const checkoutErrorTipNode = checkoutErrorTip ? (
    <TopNotice
      message={checkoutErrorTip}
      icon={CircleAlert}
      onDismiss={dismissCheckoutErrorTip}
      isError
      dismissAriaLabel={t("common.closeNotice")}
    />
  ) : null;

  if (settingsOpen) {
    return (
      <>
        {archiveTipNode}
        {checkoutErrorTipNode}
        <SettingsShellRenderer
          initialized={sessionRuntime}
          initialPage={settingsInitialPage}
          running={viewContextSwitchPending}
          runningProviderNames={runningProviderNames}
          usage={settingsUsage}
          codexPets={codexPets}
          codexPetsLoading={codexPetsLoading}
          codexPetsError={codexPetsError}
          showDebugControlsSetting={ENABLE_DEBUG_CONTROL_SETTING}
          debugControlsEnabled={debugControlsEnabled}
          sidebarWidth={sidebarWidth}
          resizingSidebar={resizingSidebar}
          shellRef={settingsShellRef}
          // The settings rail reuses the main sidebar's state and handlers
          // wholesale — same persisted width, same collapse flag, same
          // drag-to-collapse resize session, same toggle motion — so both
          // shells behave identically. The drawer controller runs
          // independently inside the settings shell (its own ref + phase)
          // and uses the active conversation as its sync key, mirroring how
          // the main view resets on session-tab swap.
          sidebarCollapsed={sidebarCollapsed}
          sidebarAnimating={sidebarAnimating}
          onToggleSidebar={toggleSidebar}
          sidebarMotionMs={SIDEBAR_MOTION_MS}
          activeSessionTabID={activeThreadID ?? ""}
          onBack={() => {
            setSettingsOpen(false);
          }}
          onSave={updateRuntimeSettings}
          onRemoveProvider={removeProvider}
          onAdvancedSave={updateAdvancedSettings}
          onGeneralSave={updateGeneralSettings}
          onCodexPetsRefresh={refreshCodexPets}
          onCodexPetsUpdate={updateCodexPets}
          onDebugControlsChange={setDebugControlsEnabled}
          onSidebarResizeStart={startSidebarResize}
          onSidebarSeparatorKey={handleSidebarSeparatorKey}
          archivedThreads={state.threads
            .filter((thread) => thread.archived)
            .map((thread) => {
              const project = state.projects.find((candidate) =>
                threadBelongsToProject(thread, candidate),
              );
              return {
                ...thread,
                archive_project_id: project?.id ?? "",
                archive_project_name: project?.name ?? t("appState.noProject"),
              };
            })}
          onUnarchiveThread={(thread) => void unarchiveThread(thread)}
        />
      </>
    );
  }

  return (
    <>
      {archiveTipNode}
      {checkoutErrorTipNode}
      <ImagePreviewProvider>
        <div ref={appShellRef} className={shellClassName} style={shellStyle}>
          {!poppedOutMode ? (
            <>
          <div
            ref={sidebarHoverZoneRef}
            className="sidebar-hover-zone"
            aria-hidden="true"
            onPointerEnter={scheduleSidebarDrawerOpen}
            onPointerLeave={cancelSidebarDrawerOpen}
          />
          {rightPanelGlobalized ? (
            <div className="globalized-sidebar-toggle-region">
              <button
                className="icon-button side-panel-toggle-button sidebar-toggle-button globalized-sidebar-toggle"
                type="button"
                aria-label={t(
                  sidebarCollapsed
                    ? "app.expandLeftSidebar"
                    : "app.collapseLeftSidebar",
                )}
                aria-pressed={!sidebarCollapsed}
                onClick={toggleSidebar}
                onPointerEnter={scheduleSidebarDrawerOpen}
                onPointerLeave={(event) =>
                  scheduleSidebarDrawerCloseFromPointerLeave(event.nativeEvent)
                }
              >
                <SidePanelToggleIcon side="left" open={!sidebarCollapsed} />
              </button>
            </div>
          ) : null}
          <AppSidebar
            state={state}
            sidebarProjects={sidebarProjects}
            activeProjectID={
              workspaceProjectSelectionEnabled && workspaceContext?.kind === "project"
                ? workspaceContext.project_id
                : undefined
            }
            pinnedThreads={sidebarPinnedThreads}
            activeThreadID={activeThreadID}
            pendingThreadID={visiblePendingThreadID}
            pendingProjectID={visiblePendingProjectID}
            collapsedSidebarSectionIDs={collapsedSidebarSectionIDs}
            expandedSidebarSectionIDs={expandedSidebarSectionIDs}
            loadingProjectThreadIDs={loadingProjectThreadIDs}
            projectThreadsByProjectID={sidebarThreadsByProjectID}
            projectMenuOpen={projectMenuOpen}
            projectMenuRef={projectMenuRef}
            searchOpen={conversationSearch.open}
            debugFixturesVisible={
              debugControlsVisible && ENABLE_CONVERSATION_FIXTURES
            }
            sectionOrder={sidebarSectionOrder}
            onStartNewThread={() => {
              revealConversationFromFocusedWorkspace();
              startNewThreadWithComposerFocus();
            }}
            onOpenSkillsTab={() => {
              setChannelsOpen(false);
              openSkillsTab();
            }}
            groupChatEnabled={ENABLE_GROUP_CHAT}
            channelsOpen={channelsOpen}
            channelMentionCount={channelMentionCount}
            onOpenChannels={() => {
              setProjectMenuOpen(false);
              setRuntimeMenuOpen(false);
              setCodexRuntimeMenu(null);
              setEnvironmentPanelOpen(false);
              setRightPanelOpenWithMotion(false);
              setChannelsOpen(true);
              setChannelMentionCount(0);
              closeSidebarDrawer();
            }}
            onToggleConversationSearch={toggleConversationSearch}
            onSeedConversationFixture={seedConversationFixture}
            onSeedAgentTreeDemo={seedAgentTreeDemo}
            onOpenChipGallery={() => setChipGalleryOpen(true)}
            onSelectThread={(id) => {
              revealConversationFromFocusedWorkspace();
              void activateThread(id);
            }}
            onTogglePinned={(thread) => void toggleThreadPinned(thread)}
            onArchiveThread={(thread) => {
              const archivedTitle =
                thread.title?.trim() || t("app.thisConversation");
              void archiveThread(thread).then((outcome) => {
                setArchiveTip({
                  threadID: thread.id,
                  threadTitle: archivedTitle,
                  errorMessage: outcome.ok ? undefined : outcome.error,
                });
              });
            }}
            onDeleteThread={(thread) => void deleteThread(thread)}
            onRenameThread={(thread, title) => void renameThread(thread, title)}
            onToggleProjectMenu={() => setProjectMenuOpen((open) => !open)}
            onCreateProject={() => void createBlankProject()}
            onOpenProjectFolder={() => void chooseProjectFolder()}
            onToggleSidebarSectionCollapsed={toggleSidebarSectionCollapsed}
            onSelectProjectWorkspace={
              workspaceProjectSelectionEnabled
                ? (id) => {
                    const project = state.projects.find((item) => item.id === id);
                    if (!project || project.missing) {
                      return;
                    }
                    setFocusedWorkspaceContext({
                      kind: "project",
                      project_id: project.id,
                      cwd: project.path,
                    });
                    openWorkspaceTool("files");
                  }
                : undefined
            }
            onStartNewThreadForProject={(id) => {
              revealConversationFromFocusedWorkspace();
              startNewThreadForProjectWithComposerFocus(id);
            }}
            onSelectProjectThread={(projectID, threadID) => {
              revealConversationFromFocusedWorkspace();
              void selectProjectThread(projectID, threadID);
            }}
            onRemoveProject={(id) => void removeProject(id)}
            onRelocateProject={(id) => void relocateProject(id)}
            onReorderSections={setSidebarSectionOrder}
            onPointerEnter={openSidebarDrawer}
            onPointerLeave={(event) =>
              scheduleSidebarDrawerCloseFromPointerLeave(event.nativeEvent)
            }
            onOpenSettings={() => {
              setProjectMenuOpen(false);
              setRuntimeMenuOpen(false);
              setCodexRuntimeMenu(null);
              setSettingsInitialPage("providers");
              setSettingsOpen(true);
            }}
          />

          {sidebarDrawerMode ? null : (
            <div
              className="sidebar-resizer"
              inert={rightPanelOpen && rightPanelGlobalized}
              role="separator"
              aria-label={t("app.resizeSidebar")}
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
        onQueryChange={setConversationSearchQuery}
        onClearQuery={clearConversationSearchQuery}
        onKeyDown={handleConversationSearchKeyDown}
        onSelectIndex={setConversationSearchSelectedIndex}
        onSelectResult={selectConversationSearchResult}
      />
            </>
          ) : null}

      <main
        inert={rightPanelOpen && rightPanelGlobalized}
        className={`conversation-pane${environmentPanelVisible ? " environment-panel-visible" : ""}${
          environmentPanelReserved ? " environment-panel-reserved" : ""
        }${
          sideThreadPanelVisible ? " side-thread-panel-visible" : ""
        }${sessionTabsVisible ? " session-tabs-visible" : ""}${
          conversationGridVisible ? " conversation-grid-visible" : ""
        }`}
        ref={conversationPaneRef}
      >
        {ENABLE_GROUP_CHAT && channelsOpen ? (
          <>
            <header className="titlebar">
              <div className="title-block channel-title-block">
                {sidebarVisible ? (
                  <button
                    className="icon-button side-panel-toggle-button sidebar-toggle-button"
                    type="button"
                    aria-label={t(
                      sidebarCollapsed
                        ? "app.expandLeftSidebar"
                        : "app.collapseLeftSidebar",
                    )}
                    aria-pressed={!sidebarCollapsed}
                    onClick={toggleSidebar}
                    onPointerEnter={scheduleSidebarDrawerOpen}
                    onPointerLeave={(event) =>
                      scheduleSidebarDrawerCloseFromPointerLeave(event.nativeEvent)
                    }
                  >
                    <SidePanelToggleIcon side="left" open={!sidebarCollapsed} />
                  </button>
                ) : null}
                <nav className="channel-mode-tabs" aria-label={t("channels.title")}>
                  {(["rooms", "agents", "tasks"] as const).map((mode) => (
                    <button
                      className={channelSection === mode ? "active" : ""}
                      type="button"
                      key={mode}
                      aria-current={channelSection === mode ? "page" : undefined}
                      onClick={() => setChannelSection(mode)}
                    >
                      {t(mode === "rooms" ? "channels.rooms" : mode === "agents" ? "channels.agents" : "channels.tasks")}
                    </button>
                  ))}
                </nav>
              </div>
            </header>
            <ChannelView initialized={sessionRuntime ?? state.initialized} section={channelSection} onSectionChange={setChannelSection} />
          </>
        ) : (
          <>
        <header className="titlebar">
          <div className="title-block">
            {sidebarVisible ? (
              <button
                className="icon-button side-panel-toggle-button sidebar-toggle-button"
                type="button"
                aria-label={t(
                  sidebarCollapsed
                    ? "app.expandLeftSidebar"
                    : "app.collapseLeftSidebar",
                )}
                aria-pressed={!sidebarCollapsed}
                onClick={toggleSidebar}
                onPointerEnter={scheduleSidebarDrawerOpen}
                onPointerLeave={(event) =>
                  scheduleSidebarDrawerCloseFromPointerLeave(event.nativeEvent)
                }
              >
                <SidePanelToggleIcon side="left" open={!sidebarCollapsed} />
              </button>
            ) : null}
            <ConversationTitleContent
              state={state}
              crossWorkspaceThreads={sidebarThreads}
              sessionTabsVisible={sessionTabsVisible}
              pendingSwitchThreadID={visiblePendingThreadID}
              pendingComposerMessagesByThread={pendingComposerMessagesByThread}
              activeTitle={activeTitle}
              onSelectSessionTab={(tabID) => void selectSessionTab(tabID)}
              onCloseSessionTab={(tabID) => void closeSessionTab(tabID)}
              onCloseSessionTabs={(tabIDs) => void closeSessionTabs(tabIDs)}
              onPopOutSessionTab={(tabID) => void popOutSessionTab(tabID)}
              onStartNewThread={startNewThreadWithComposerFocus}
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
            environmentToggleRef={environmentToggleRef}
            environmentPanelVisible={environmentPanelVisible}
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
          environmentGitBusy={environmentGitBusy}
          pullRequestDisabledReason={pullRequestDisabledReason}
          onSetEnvironmentPanelMenu={setEnvironmentPanelMenu}
          onCloseEnvironmentPanel={() =>
            closeEnvironmentPanel({ dismissed: true })
          }
          onSelectBranch={async (branch) => {
            try {
              await checkoutBranch(branch);
            } catch (error) {
              setCheckoutErrorTip(
                error instanceof Error
                  ? error.message
                  : t("git.checkoutFailed"),
              );
            }
          }}
          onCreateBranch={(branch) => createAndCheckoutBranch(branch)}
          onOpenReview={() => {
            openWorkspaceTool("review");
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
          viewContextSwitchPending={viewContextSwitchPending}
        />

        {sideThreadPanelVisible && activeThreadID && sideThread.entry ? (
          <SideThreadPanel
            ref={sideThreadPanelRef}
            entry={sideThread.entry}
            mainThreadId={activeThreadID}
            width={sideThread.width}
            cwd={activeThread?.cwd ?? state.activeContext?.cwd}
            onOpenFile={openWorkspaceFile}
            composer={
              <SideThreadComposer
                draft={sideThread.entry.draft}
                running={sideThread.entry.streaming}
                disabledReason={sideThread.sendDisabledReason}
                queryHistorySessionID={
                  sideThread.entry.summary?.side_thread_id ?? `side:${activeThreadID}`
                }
                queryHistory={sideThread.entry.messages
                  .filter((message) => message.role === "user")
                  .map((message) => message.text)}
                onChangeDraft={sideThread.setDraft}
                onSend={sideThread.sendMessage}
                onInterrupt={sideThread.interrupt}
                onReset={sideThread.reset}
              />
            }
            onClose={sideThread.close}
            onResizeStart={sideThread.startResize}
            onChangeDraft={sideThread.setDraft}
          />
        ) : null}

        {state.initialized && !previewingLaunch ? (
          <div
            className={`scroll-region${emptyConversation ? " empty-scroll-region" : ""}${
              splitConversation ? " split-scroll-region" : ""
            }${showingSkillsCatalog ? " skills-scroll-region" : ""}`}
            onScroll={(event) => handleConversationScroll(event.currentTarget)}
            ref={conversationScrollRef}
          >
            <div ref={scrollContentRef} className="scroll-region-content">
              {showingSkillsCatalog ? (
              <SkillsCatalog
                activeContext={state.activeContext}
                extensionInventory={state.initialized?.extension_inventory}
                onTrySkill={trySkillFromCatalog}
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
                    onOpenFile={openWorkspaceFileForThread}
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
                    onOpenFileDiff={openTurnFileDiffPanel}
                  />
                ) : activePendingNewThreadTurn ? (
                  <div className="conversation-width session-flow">
                    <TurnView
                      turn={activePendingNewThreadTurn}
                      cwd={state.activeContext?.cwd}
                      onStreamFrame={scheduleStreamScroll}
                      isLatestTurn
                    />
                  </div>
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
                {rightPanelGlobalized && activeWorkspaceFileTabID
                  ? <div />
                  : renderComposer("hero")}
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
                onOpenFile={openWorkspaceFileForThread}
                onOpenAgent={handleCachedPaneOpenAgent}
                onEditMessage={handleCachedPaneEditMessage}
                onCancelEditMessage={handleCachedPaneCancelEditMessage}
                onSubmitEditMessage={handleCachedPaneSubmitEditMessage}
                turnStreamStatus={state.turnStreamStatus}
                onOpenFileDiff={handleCachedPaneOpenFileDiff}
                onOpenTurnRuns={handleOpenTurnRuns}
              />
            )}
              </>
            )}
            </div>
            {mainConversationDockVisible ? (
              <JumpToLatestPill
                containerRef={conversationScrollRef}
                bottomAnchor={dockComposerNode}
                onScrolledAwayChange={setMainConversationScrolledAway}
              />
            ) : null}
          </div>
        ) : (
          <RuntimeLoading
            status={resolveLocalizedText(state.status)}
            pinned={previewingLaunch}
            onExitPreview={() => setLaunchPreviewPinned(false)}
          />
        )}

        {mainConversationDockVisible ? renderComposer("dock") : null}

        {mainConversationDockVisible && activePlanVisible && !mainConversationScrolledAway ? (
          <div
            className="jump-to-latest-cluster"
            aria-label={t("app.currentPositionAndProgress")}
          >
            {activePlanVisible ? (
              <div
                className="jump-to-latest-progress"
                aria-label={t("app.planProgressLabel", {
                  completed: formatNumber(activePlanCompleted),
                  total: formatNumber(activePlanTotal),
                })}
              >
                {t("app.progressFraction", {
                  completed: formatNumber(activePlanCompleted),
                  total: formatNumber(activePlanTotal),
                })}
                {activePlanDetailItems.length > 0 ? (
                  <span className="jump-to-latest-progress-detail" aria-hidden="true">
                    {activePlanDetailItems.map((item) => (
                      <span className={`jump-to-latest-progress-step ${item.status}`} key={item.step}>
                        {t(
                          item.status === "in_progress"
                            ? "app.planInProgress"
                            : "app.planNext",
                          { step: item.step },
                        )}
                      </span>
                    ))}
                  </span>
                ) : null}
              </div>
            ) : null}
          </div>
        ) : null}
          </>
        )}
      </main>

      {!poppedOutMode && (rightPanelOpen || rightPanelAnimating) ? (
        <div
          className="workspace-right-panel-resizer"
          inert={rightPanelGlobalized}
          role="separator"
          aria-label={t("app.resizeRightSidebar")}
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
          prewarm={Boolean(state.initialized)}
          tabs={workspaceViewTabs}
          activeTabID={workspaceActiveViewTabID}
          activeFileTabID={activeWorkspaceFileTabID}
          activeContext={state.activeContext}
          workspaceContext={workspaceContext}
          terminalThread={activeThread}
          terminalRunRequest={terminalRunRequest}
          gitStatus={state.gitStatus}
          selectedFilePath={activeWorkspaceFile}
          onSelectTab={focusWorkspaceViewTab}
          onOpenTool={openWorkspaceTool}
          onShowTools={showWorkspaceToolPicker}
          onCloseTab={closeWorkspaceViewTab}
          onDirtyFileTabsChange={rememberWorkspaceDirtyFiles}
          onReorderTabs={reorderWorkspaceViewTabs}
          onOpenFile={openWorkspaceFile}
          onClose={() => setRightPanelOpenWithMotion(false)}
          globalized={rightPanelGlobalized}
          sheetPhase={workspaceSheetPhase}
          onToggleGlobalize={toggleWorkspacePanelGlobalized}
          canExitGlobalized={
            !rightPanelAutoGlobalized ||
            workspaceRightPanelDockableWithoutSidebar
          }
          browserActivity={activeBrowserActivity}
          onBrowserActivityTakeover={() => void takeoverBrowserActivity()}
          onBrowserActivityRelease={() => void releaseBrowserActivity()}
          onBrowserActivityStop={() => void stopBrowserActivity()}
          focusedComposer={
            rightPanelGlobalized && activeWorkspaceFileTabID
              ? (
                  <WorkspaceDocumentTurnDock
                    key={activeThreadID ?? state.activeSessionTabID}
                    cwd={activeThread?.cwd ?? state.activeContext?.cwd}
                    onOpenFile={openWorkspaceFile}
                    turns={
                      activePendingNewThreadTurn
                        ? [...turns, activePendingNewThreadTurn]
                        : turns
                    }
                  >
                    {renderComposer("document")}
                  </WorkspaceDocumentTurnDock>
                )
              : undefined
          }
          fileRefreshKey={
            activeThreadIsRunning ? "running" : activeThread?.updated_at
          }
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
      </div>
    </ImagePreviewProvider>
    </>
  );
}
