/// <reference path="../shared/jsx-compat.d.ts" />

import {
  AlertCircle,
  Bug,
  ChevronRight,
  Film,
  Folder,
  FolderX,
  GitBranch,
  Grid3X3,
  Image as ImageIcon,
  Info,
  Laptop,
  ListChecks,
  MoreHorizontal,
  Pencil,
  Pin,
  Plus,
  Send,
  Terminal,
  Trash2,
  Wrench,
  X,
} from "lucide-react";
import {
  type CSSProperties,
  type RefObject,
  memo,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { arrayMove } from "@dnd-kit/sortable";
import type {
  Agent,
  DesktopProject,
  GitCommitResult,
  GitPullRequestResult,
  GitStatusResult,
  InitializeResult,
  InputFile,
  InputImage,
  ManagedProcess,
  PendingToolApproval,
  PlanUpdate,
  ProjectListResult,
  RuntimeAdvancedSettingsUpdate,
  RuntimeConnectionUpdate,
  RuntimeContext,
  ServerEvent,
  Thread,
  ThreadItem,
  Turn,
} from "../shared/protocol";
import {
  composerFileFromFile,
  composerImageFromFile,
  createComposerMessage,
  createOptimisticTurn,
  dropOptimisticTurn,
  earlierStartedAt,
  inputFilesFromComposer,
  inputImagesFromComposer,
  isComposerImageFile,
  isPDFFile,
  replaceOptimisticTurn,
  type ComposerFile,
  type ComposerImage,
  type QueuedComposerMessage,
} from "./ComposerMessages";
import {
  emptyThreadPendingComposerMessages,
  findPendingComposerMessage,
  pendingComposerMessagesForThread,
  threadPendingComposerMessagesIsEmpty,
  type PendingComposerMessagesByThread,
  type ThreadPendingComposerMessages,
} from "./ComposerPendingMessages";
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
import {
  createAgentTreeDemo,
  createConversationFixture,
  type ConversationFixtureKind,
} from "./ConversationFixtures";
import { ConversationSearchOverlay } from "./ConversationSearchOverlay";
import { ConversationSplitPane } from "./ConversationSplitPane";
import { useConversationScrollState } from "./ConversationScrollState";
import { useConversationSearch } from "./ConversationSearchState";
import { AppSidebar } from "./AppSidebar";
import {
  backgroundProcessIsLive,
  backgroundProcessNeedsAttention,
  buildBackgroundProcessItems,
  type BackgroundProcessItem,
  type EnvironmentPanelMenu,
  type EnvironmentPanelMotionState,
} from "./EnvironmentPanel";
import { EnvironmentSideStack } from "./EnvironmentSideStack";
import {
  activeProjectID,
  activeSessionTab,
  activeThreadForState,
  activeThreadIDForState,
  activeTurnIDForThread,
  activeTurnTokenSpeedSnapshot,
  appendStreamingTokenSample,
  bindActiveSessionTabToThread,
  cloneSessionTabDraft,
  cloneComposerDraft,
  composerSubmissionDetail,
  conversationPaneThreadsByID,
  createDraftSessionTab,
  createFileSessionTab,
  createSkillsSessionTab,
  createThreadSessionTab,
  emptyComposerDraft,
  ensureSessionTab,
  fileNameFromPath,
  handleStreamingNotification,
  initialSplitComposerDrafts,
  initialState,
  isAnyThreadRunning,
  isDirectChildAgent,
  isStateActiveThreadRunning,
  isThread,
  isThreadRunning,
  latestPlanUpdateForThread,
  mergeListedThreads,
  markThreadTurnsViewed,
  notificationTargetsActiveThread,
  persistActiveSessionTabDraft,
  pinnedThreads,
  projectThreads,
  scratchThreads,
  queryTextsForThread,
  reduceServerEvent,
  removeSessionTab,
  requireThread,
  runtimeContextKey,
  sameRuntimeContext,
  serverEventMayAffectProcesses,
  serverEventShouldRefreshGit,
  serverEventTargetsActiveContext,
  sessionTabForLoadedRuntime,
  sessionTabDraftForThread,
  setThreadForPane,
  sortThreads,
  threadForTab,
  threadForPane,
  threadItemFromRecord,
  threadFromRecord,
  threadIDFromParams,
  threadSessionTabID,
  turnFromRecord,
  updateThreadByID,
  upsertManagedProcess,
  upsertThread,
  upsertTurn,
  withLoadedRuntimeSessionTab,
  type AppState,
  type ComposerDraftState,
  type ConversationPaneID,
  type SessionTab,
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
import { DesignTokensPanel } from "./DesignTokensPanel";
import { useAppDebugState } from "./AppDebugState";
import {
  EmptyConversationHome,
  RuntimeLoading,
  ViewSwitchLoading,
} from "./LoadingViews";
import {
  isCodexProvider,
  pullRequestUnavailableReason,
} from "./RuntimeHelpers";
import { SettingsView, type SettingsPage } from "./SettingsView";
import type { ComposerGoalSummary, SettingsUsageRange, SettingsUsageResponse } from "../shared/protocol";
import { SidePanelToggleIcon } from "./SidePanelToggleIcon";
import { SessionTabStrip } from "./SessionTabs";
import { SkillsCatalog } from "./SkillsCatalog";
import { StreamingMarkdown } from "./StreamingMarkdown";
import {
  RunDebugPanel,
  runDebugPhaseForState,
} from "./RunDebugPanel";
import { useThreadBrowserPreview } from "./ThreadBrowserPreview";
import { threadDisplayTitle } from "./ThreadTitles";
import {
  isRecord,
  numberValue,
  recordValue,
  stringValue,
} from "./ToolActivity";
import {
  TurnProgressCampaignScene,
  TurnProgressPreviewOverlay,
  turnProgressCampaign,
} from "./TurnProgress";
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
  TurnView,
  latestAgentMessageItemID,
  scrollToUserMessage,
} from "./TurnView";
import { ConversationTurnRail } from "./ConversationTurnRail";
import {
  WorkspaceMainPanel,
  WorkspaceRightPanel,
  WorkspaceToolIcon,
  workspaceModeTitle,
  type WorkspacePanelView,
} from "./WorkspacePanels";
import { useWorkspaceToolState } from "./WorkspaceToolState";
import { desktopApiErrorMessage } from "./WorkspaceReviewHelpers";
import { ImagePreviewProvider } from "./ImagePreview";

const VIEW_SWITCH_LOADING_DELAY_MS = 180;
const PROJECT_THREAD_COLLAPSE_MS = 190;
const ENVIRONMENT_PANEL_MOTION_MS = 260;
const ENVIRONMENT_PANEL_WIDTH_PX = 328;
const ENVIRONMENT_PANEL_WIDTH_CSS = `${ENVIRONMENT_PANEL_WIDTH_PX}px`;
const CONVERSATION_GRID_COLUMNS = 12;
// Cap on the number of bars rendered in the always-visible rail. The
// rail is a thin at-a-glance index; if there are more queries than fit,
// we collapse the tail into a single bar.
const QUERY_HISTORY_RAIL_MAX_BARS = 20;
type EnvironmentDialog = "commit" | "pull-request" | null;
type PendingViewSwitchKind = "thread" | "project" | "runtime";

type PendingViewSwitch = {
  kind: PendingViewSwitchKind;
  targetID: string;
  visible: boolean;
};

type TurnProgressContent = {
  label: string;
  detail?: string;
};

const PROJECT_COLLAPSED_IDS_KEY = "wuu.desktop.collapsedProjectIDs";
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
const ENABLE_TURN_PROGRESS_EXPERIMENT = false;

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

type HistoryMessageEditState = {
  threadID: string;
  turnID: string;
  itemID: string;
  pane?: ConversationPaneID;
  submitting: boolean;
};

function initialCollapsedProjectIDs(): Set<string> {
  try {
    const stored = window.localStorage.getItem(PROJECT_COLLAPSED_IDS_KEY);
    const parsed: unknown = stored ? JSON.parse(stored) : [];
    if (!Array.isArray(parsed)) {
      return new Set();
    }
    return new Set(
      parsed.filter(
        (id): id is string => typeof id === "string" && id.length > 0,
      ),
    );
  } catch {
    return new Set();
  }
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


export function App(): JSX.Element {
  const [state, setState] = useState<AppState>(initialState);
  const [prompt, setPrompt] = useState("");
  const [composerImages, setComposerImages] = useState<ComposerImage[]>([]);
  const [composerFiles, setComposerFiles] = useState<ComposerFile[]>([]);
  const [goalSummary, setGoalSummary] = useState<ComposerGoalSummary | null>(
    null
  );
  const [splitComposerDrafts, setSplitComposerDrafts] = useState<
    Record<ConversationPaneID, ComposerDraftState>
  >(initialSplitComposerDrafts);
  const [historyMessageEdit, setHistoryMessageEdit] =
    useState<HistoryMessageEditState | undefined>(undefined);
  const [pendingComposerMessagesByThread, setPendingComposerMessagesByThread] =
    useState<PendingComposerMessagesByThread>({});
  const [projectMenuOpen, setProjectMenuOpen] = useState(false);
  const closeProjectMenu = useCallback(() => setProjectMenuOpen(false), []);
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
    setRightPanelOpenWithMotion,
    startSidebarResize,
    startSettingsSidebarResize,
    startRightPanelResize,
    handleRightPanelSeparatorKey,
    resetWorkspaceRightPanelWidth,
    toggleSidebar,
    handleSidebarSeparatorKey,
    handleSettingsSidebarSeparatorKey,
    resetSettingsSidebarWidth,
  } = useAppLayoutState({ onCloseProjectMenu: closeProjectMenu });
  const [collapsedProjectIDs, setCollapsedProjectIDs] = useState<Set<string>>(
    initialCollapsedProjectIDs,
  );
  const [collapsingProjectIDs, setCollapsingProjectIDs] = useState<Set<string>>(
    () => new Set(),
  );
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
  const [settingsInitialPage, setSettingsInitialPage] =
    useState<SettingsPage>("providers");
  const [projectFilter, setProjectFilter] = useState("");
  const [launchPreviewPinned, setLaunchPreviewPinned] = useState(false);
  const [turnProgressPreviewOpen, setTurnProgressPreviewOpen] = useState(false);
  const {
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
  const [environmentDialog, setEnvironmentDialog] =
    useState<EnvironmentDialog | null>(null);
  const [managedProcesses, setManagedProcesses] = useState<ManagedProcess[]>(
    [],
  );
  const [stoppingProcessIDs, setStoppingProcessIDs] = useState<Set<string>>(
    () => new Set(),
  );
  const [archiveConfirmThreadID, setArchiveConfirmThreadID] = useState<
    string | undefined
  >(undefined);
  const [pendingViewSwitch, setPendingViewSwitch] = useState<
    PendingViewSwitch | undefined
  >(undefined);
  const hideDebugControls = useCallback(() => {
    setLaunchPreviewPinned(false);
    setTurnProgressPreviewOpen(false);
  }, []);
  const {
    debugControlsEnabled,
    setDebugControlsEnabled,
    debugControlsVisible,
    runDebugOpen,
    setRunDebugOpen,
    conversationGridVisible,
    setConversationGridVisible,
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
  const projectCollapseTimersRef = useRef(new Map<string, number>());
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
  const managedProcessRefreshTimerRef = useRef<number | undefined>(undefined);
  const managedProcessRefreshInFlightRef = useRef(false);
  const managedProcessRefreshQueuedRef = useRef(false);
  const projectMenuRef = useRef<HTMLDivElement>(null);
  const runtimeMenuRef = useRef<HTMLDivElement>(null);
  const accessMenuRef = useRef<HTMLDivElement>(null);
  const codexRuntimeRef = useRef<HTMLDivElement>(null);
  const environmentToggleRef = useRef<HTMLButtonElement>(null);
  const environmentPanelRef = useRef<HTMLDivElement>(null);
  const appStateRef = useRef<AppState>(initialState);
  const pendingComposerMessagesByThreadRef =
    useRef<PendingComposerMessagesByThread>({});
  const localDemoThreadsRef = useRef(new Map<string, Thread>());
  const viewSwitchRequestRef = useRef(0);
  const viewSwitchDelayTimerRef = useRef<number | undefined>(undefined);
  const draftSessionTabCounterRef = useRef(0);
  const currentSessionTab = activeSessionTab(state);
  const activeWorkspaceFile =
    currentSessionTab?.kind === "file" &&
    sameRuntimeContext(currentSessionTab.context, state.activeContext)
      ? currentSessionTab.path
      : undefined;
  const activeThread = activeThreadForState(state);
  const activeThreadID = activeThread?.id;
  // Per-thread keep-alive for the main conversation pane. We render a
  // pane per open session tab (with display:none for inactive ones) so
  // switching tabs no longer unmounts and remounts the entire <TurnView>
  // tree — that unmount was the visible flash, and it also reset
  // scroll, interrupted streaming, and re-ran the ConversationScrollState
  // useLayoutEffect on every switch.
  //
  // Crucially we derive the cache synchronously from state.sessionTabs
  // and state.thread via useMemo, not via useState + useEffect. The
  // async effect path rendered once with the new activeThreadID but
  // the stale cache (no pane for the new thread) and then a second
  // time with the cache updated — the "two flickers" the user saw.
  // Computing the cache from state in the same render closes that
  // empty frame.
  const CACHED_THREAD_PANE_LIMIT = 10;
  const cachedThreadPaneIDs = useMemo(() => {
    const activeID = state.thread?.id;
    const sessionThreadIDs = state.sessionTabs
      .filter(
        (tab): tab is Extract<SessionTab, { kind: "thread" }> =>
          tab.kind === "thread",
      )
      .map((tab) => tab.threadID);
    const others = sessionThreadIDs.filter((id) => id !== activeID);
    const ordered = activeID ? [activeID, ...others] : others;
    return ordered.slice(0, CACHED_THREAD_PANE_LIMIT);
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
  const activePendingComposerMessages = pendingComposerMessagesForThread(
    pendingComposerMessagesByThread,
    activeThreadID,
  );
  const queuedMessages = activePendingComposerMessages.queued;
  const guideMessages = activePendingComposerMessages.guides;
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
  const cancelCurrentGoal = useCallback(async () => {
    if (!goalSummary) {
      return;
    }
    const threadID = goalSummary.thread_id ?? activeThreadID;
    if (!threadID) {
      return;
    }
    await window.wuu.cancelGoal(goalSummary.id, threadID);
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
  const [usageRange, setUsageRange] = useState<SettingsUsageRange>("all");
  const [settingsUsage, setSettingsUsage] = useState<SettingsUsageResponse | undefined>(
    undefined,
  );
  useEffect(() => {
    if (!settingsOpen) {
      setSettingsUsage(undefined);
      return;
    }
    let cancelled = false;
    void window.wuu
      .getSettingsUsage(usageRange)
      .then((response) => {
        if (cancelled) {
          return;
        }
        setSettingsUsage(response);
      })
      .catch(() => {
        if (cancelled) {
          return;
        }
        setSettingsUsage(undefined);
      });
    return () => {
      cancelled = true;
    };
  }, [settingsOpen, usageRange]);
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
    },
    onSelectThread: (threadID) => void activateThread(threadID),
  });
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
  const activeContextKey = state.activeContext
    ? runtimeContextKey(state.activeContext)
    : "";
  const backgroundProcesses = useMemo(
    () => buildBackgroundProcessItems(activeThread, managedProcesses),
    [activeThread, managedProcesses],
  );
  const liveBackgroundProcesses = backgroundProcesses.filter(backgroundProcessIsLive);
  const failedBackgroundProcesses = backgroundProcesses.filter(backgroundProcessNeedsAttention);
  const backgroundProcessCapsuleVisible =
    liveBackgroundProcesses.length > 0 || failedBackgroundProcesses.length > 0;
  const backgroundProcessCapsuleLabel =
    failedBackgroundProcesses.length > 0
      ? `后台 ${failedBackgroundProcesses.length} 失败`
      : liveBackgroundProcesses.length === 1
        ? liveBackgroundProcesses[0].command
        : `后台 ${liveBackgroundProcesses.length}`;
  const backgroundProcessCapsuleTone =
    failedBackgroundProcesses.length > 0 ? "failed" : "running";
  const backgroundProcessCapsuleTitle =
    failedBackgroundProcesses.length > 0
      ? "后台任务失败"
      : liveBackgroundProcesses.some((process) => process.lifecycle === "managed")
        ? "后台任务运行中，包含需手动清理任务"
        : "后台任务运行中";
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
    return () => {
      for (const timer of projectCollapseTimersRef.current.values()) {
        window.clearTimeout(timer);
      }
      projectCollapseTimersRef.current.clear();
      clearViewSwitchDelay();
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
      root.classList.toggle("window-resizing", nextResizing);
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
        document.documentElement.classList.contains("window-resizing")
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
      if (!serverEventTargetsActiveContext(event, appStateRef.current)) {
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
      if (handling === "skip") {
        return;
      }
      if (serverEventShouldRefreshGit(event)) {
        scheduleGitStatusRefresh(600);
      }
      if (serverEventMayAffectProcesses(event)) {
        scheduleManagedProcessRefresh(500);
      }
      syncPendingComposerMessagesFromServerEvent(event);
      setState((current) => reduceServerEvent(current, event));
    });

    void (async () => {
      try {
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
      if (managedProcessRefreshTimerRef.current !== undefined) {
        window.clearTimeout(managedProcessRefreshTimerRef.current);
        managedProcessRefreshTimerRef.current = undefined;
      }
    };
  }, []);

  useEffect(() => {
    if (!state.initialized || !state.activeContext) {
      setManagedProcesses([]);
      return;
    }
    setManagedProcesses([]);
    scheduleManagedProcessRefresh(0);
  }, [state.initialized, activeContextKey]);

  useEffect(() => {
    if (liveBackgroundProcesses.length === 0) {
      return;
    }
    const id = window.setInterval(() => {
      void refreshManagedProcesses();
    }, 3000);
    return () => window.clearInterval(id);
  }, [liveBackgroundProcesses.length, activeContextKey]);

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
    window.localStorage.setItem(
      PROJECT_COLLAPSED_IDS_KEY,
      JSON.stringify([...collapsedProjectIDs]),
    );
  }, [collapsedProjectIDs]);

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
  const activeTitle = showingSkillsCatalog
    ? "Skills"
    : workspaceMode
      ? workspaceModeTitle(workspaceMode)
      : activeThread?.preview || "新对话";
  const emptyThreadTitle =
    state.activeContext?.kind === "project"
      ? `我们应该在 ${activeProject?.name ?? "这个项目"} 中构建什么？`
      : "我们应该在 wuu 中构建什么？";
  const turns = activeThread?.turns ?? [];
  const latestAgentMessageID = latestAgentMessageItemID(turns);
  const emptyConversation = !showingSkillsCatalog && turns.length === 0;

  // Past user queries for the input-box hover popover. We collect them
  // in turn order, oldest first, so the popover mirrors the order in
  // which the user asked them. Empty / handoff / image-only items are
  // skipped — they have nothing to show in a quick-jump list.
  const pastQueries = useMemo<QueryHistoryEntry[]>(() => {
    const entries: QueryHistoryEntry[] = [];
    for (const turn of turns) {
      for (const item of turn.items) {
        if (item.type !== "user_message") {
          continue;
        }
        const text = (item.text ?? "").trim();
        if (text.length === 0) {
          continue;
        }
        entries.push({ turnID: turn.id, itemID: item.id, text });
      }
    }
    return entries;
  }, [turns]);
  const showingWorkspaceMode =
    state.initialized && !previewingLaunch && workspaceMode !== undefined;
  const {
    conversationScrollRef,
    splitPaneRefs,
    conversationPaneRef,
    dockComposerRef,
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
  const sidebarPinnedThreads = pinnedThreads(state.threads);
  const sidebarScratchThreads = scratchThreads(state.threads, state.projects);
  const visiblePendingThreadID =
    pendingViewSwitch?.visible && pendingViewSwitch.kind === "thread"
      ? pendingViewSwitch.targetID
      : undefined;
  const visiblePendingProjectID =
    pendingViewSwitch?.visible && pendingViewSwitch.kind === "project"
      ? pendingViewSwitch.targetID
      : undefined;
  const activeThreadReadOnly = Boolean(activeThread?.read_only);
  const activeThreadIsRunning = isStateActiveThreadRunning(state);
  const viewSwitchPending = pendingViewSwitch !== undefined;
  // A "context" switch means a project or runtime-context change that
  // genuinely has to load remote resources. Only those should trip the
  // full-screen loading shimmer, the composer "running" lock, or the
  // environment side stack's "anything is in flight" indicator. Thread
  // switches resolve from local data and stay instant.
  const viewContextSwitchPending =
    pendingViewSwitch?.visible === true &&
    pendingViewSwitch.kind !== "thread";
  const anyThreadIsRunning = isAnyThreadRunning(state) || viewContextSwitchPending;
  const environmentPanelCanShow = Boolean(
    state.initialized &&
    !previewingLaunch &&
    !showingWorkspaceMode &&
    !rightPanelOpen,
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
  const queryHistoryDocked = Boolean(
    environmentPanelReserved &&
      environmentPanelVisible &&
      !activeThreadReadOnly &&
      pastQueries.length > 0 &&
      !showingWorkspaceMode &&
      !showingSkillsCatalog,
  );
  const sessionTabsVisible = Boolean(state.initialized && !previewingLaunch);
  const shellClassName = `app-shell${sidebarCollapsed ? " sidebar-collapsed" : ""}${
    sidebarAnimating ? " sidebar-animating" : ""
  }${rightPanelAnimating ? " right-panel-animating" : ""}${resizingSidebar ? " resizing-sidebar" : ""}${
    resizingRightPanel ? " resizing-right-panel" : ""
  }${rightPanelOpen ? " right-panel-open" : ""}`;
  const shellStyle = {
    "--sidebar-width": `${effectiveSidebarWidth}px`,
    "--sidebar-open-width": `${sidebarWidth}px`,
    "--sidebar-motion-duration": `${SIDEBAR_MOTION_MS}ms`,
    "--workspace-panel-motion-duration": `${RIGHT_PANEL_MOTION_MS}ms`,
    "--workspace-right-panel-width": `${clampedWorkspaceRightPanelWidth}px`,
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

  const handleOpenFileInRightPanel = useCallback(
    (path: string): void => {
      setRightPanelFilePath(path);
      setEnvironmentPanelMenu("file");
      setRightPanelOpenWithMotion(true);
    },
    [setRightPanelOpenWithMotion],
  );

  const handleCloseFilePreview = useCallback((): void => {
    setRightPanelFilePath(undefined);
    setEnvironmentPanelMenu(null);
  }, []);

  useEffect(() => {
    if (queryHistoryDocked) {
      setQueryHistoryOpen(false);
    }
  }, [queryHistoryDocked]);

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

  function clearProjectCollapseTimer(projectID: string): void {
    const timer = projectCollapseTimersRef.current.get(projectID);
    if (timer === undefined) {
      return;
    }
    window.clearTimeout(timer);
    projectCollapseTimersRef.current.delete(projectID);
  }

  function toggleProjectCollapsed(projectID: string): void {
    if (
      collapsedProjectIDs.has(projectID) ||
      collapsingProjectIDs.has(projectID)
    ) {
      clearProjectCollapseTimer(projectID);
      setCollapsedProjectIDs((current) => {
        if (!current.has(projectID)) {
          return current;
        }
        const next = new Set(current);
        next.delete(projectID);
        return next;
      });
      setCollapsingProjectIDs((current) => {
        if (!current.has(projectID)) {
          return current;
        }
        const next = new Set(current);
        next.delete(projectID);
        return next;
      });
      return;
    }

    setCollapsingProjectIDs((current) =>
      current.has(projectID) ? current : new Set(current).add(projectID),
    );
    clearProjectCollapseTimer(projectID);
    const timer = window.setTimeout(() => {
      projectCollapseTimersRef.current.delete(projectID);
      setCollapsedProjectIDs((current) =>
        current.has(projectID) ? current : new Set(current).add(projectID),
      );
      setCollapsingProjectIDs((current) => {
        if (!current.has(projectID)) {
          return current;
        }
        const next = new Set(current);
        next.delete(projectID);
        return next;
      });
    }, PROJECT_THREAD_COLLAPSE_MS);
    projectCollapseTimersRef.current.set(projectID, timer);
  }

  function openWorkspaceFile(path: string): void {
    const context = appStateRef.current.activeContext;
    if (!context) {
      return;
    }
    const fileTab = createFileSessionTab(context, path);
    const outgoingDraft = currentPrimaryComposerDraft();
    ensureWorkspaceToolTab("files");
    setWorkspacePanelView("files");
    setWorkspaceRightPanelView("files");
    setRightPanelOpenWithMotion(true);
    setWorkspaceMode("files");
    setState((current) => ({
      ...persistActiveSessionTabDraft(current, outgoingDraft),
      sessionTabs: ensureSessionTab(current.sessionTabs, fileTab),
      activeSessionTabID: fileTab.id,
    }));
  }

  function openSkillsTab(): void {
    if (!state.activeContext) {
      return;
    }
    const tab = createSkillsSessionTab(state.activeContext);
    setArchiveConfirmThreadID(undefined);
    setWorkspaceMode(undefined);
    setSplitComposerDrafts(initialSplitComposerDrafts());
    setState((current) => ({
      ...persistActiveSessionTabDraft(current, currentPrimaryComposerDraft()),
      secondaryThread: undefined,
      activePane: "primary",
      sessionTabs: ensureSessionTab(current.sessionTabs, tab),
      activeSessionTabID: tab.id,
      allowThreadAutoActivation: false,
      running: false,
      status: "ready",
    }));
  }

  async function buildComposerAttachments(files: File[]): Promise<{
    images: ComposerImage[];
    files: ComposerFile[];
  }> {
    const imageFiles = files.filter(isComposerImageFile);
    const pdfFiles = files.filter(isPDFFile);
    return {
      images: await Promise.all(
        imageFiles.map((file) => composerImageFromFile(file)),
      ),
      files: await Promise.all(
        pdfFiles.map((file) => composerFileFromFile(file)),
      ),
    };
  }

  async function attachComposerAttachmentFiles(files: File[]): Promise<void> {
    if (files.length === 0) {
      return;
    }
    try {
      const attachments = await buildComposerAttachments(files);
      if (attachments.images.length === 0 && attachments.files.length === 0) {
        setState((current) => ({
          ...current,
          status: "仅支持图片和 PDF",
        }));
        return;
      }
      setComposerImages((current) => [...current, ...attachments.images]);
      setComposerFiles((current) => [...current, ...attachments.files]);
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "附件添加失败",
      }));
    }
  }

  function removeComposerImage(id: string): void {
    setComposerImages((current) => current.filter((image) => image.id !== id));
  }

  function removeComposerFile(id: string): void {
    setComposerFiles((current) => current.filter((file) => file.id !== id));
  }

  function updateSplitComposerDraft(
    pane: ConversationPaneID,
    update: (draft: ComposerDraftState) => ComposerDraftState,
  ): void {
    setSplitComposerDrafts((current) => {
      const draft = current[pane] ?? emptyComposerDraft();
      return {
        ...current,
        [pane]: update(draft),
      };
    });
  }

  function setSplitComposerPrompt(
    pane: ConversationPaneID,
    value: string,
  ): void {
    updateSplitComposerDraft(pane, (draft) => ({ ...draft, prompt: value }));
  }

  async function attachSplitComposerAttachmentFiles(
    pane: ConversationPaneID,
    files: File[],
  ): Promise<void> {
    if (files.length === 0) {
      return;
    }
    try {
      const attachments = await buildComposerAttachments(files);
      if (attachments.images.length === 0 && attachments.files.length === 0) {
        setState((current) => ({
          ...current,
          status: "仅支持图片和 PDF",
        }));
        return;
      }
      updateSplitComposerDraft(pane, (draft) => ({
        ...draft,
        images: [...draft.images, ...attachments.images],
        files: [...draft.files, ...attachments.files],
      }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "附件添加失败",
      }));
    }
  }

  function removeSplitComposerImage(
    pane: ConversationPaneID,
    id: string,
  ): void {
    updateSplitComposerDraft(pane, (draft) => ({
      ...draft,
      images: draft.images.filter((image) => image.id !== id),
    }));
  }

  function removeSplitComposerFile(pane: ConversationPaneID, id: string): void {
    updateSplitComposerDraft(pane, (draft) => ({
      ...draft,
      files: draft.files.filter((file) => file.id !== id),
    }));
  }

  function moveSplitDraftToGlobalComposer(pane: ConversationPaneID): void {
    const draft = splitComposerDrafts[pane] ?? emptyComposerDraft();
    setPrompt(draft.prompt);
    setComposerImages(draft.images.map((image) => ({ ...image })));
    setComposerFiles(draft.files.map((file) => ({ ...file })));
    setSplitComposerDrafts(initialSplitComposerDrafts());
  }

  function setPendingComposerMessagesByThreadNow(
    messagesByThread: PendingComposerMessagesByThread,
  ): void {
    pendingComposerMessagesByThreadRef.current = messagesByThread;
    setPendingComposerMessagesByThread(messagesByThread);
  }

  function updateThreadPendingComposerMessages(
    threadID: string,
    update: (
      previous: ThreadPendingComposerMessages,
    ) => ThreadPendingComposerMessages,
  ): void {
    const previousByThread = pendingComposerMessagesByThreadRef.current;
    const previous =
      previousByThread[threadID] ?? emptyThreadPendingComposerMessages();
    const nextForThread = update(previous);
    const nextByThread = { ...previousByThread };
    if (threadPendingComposerMessagesIsEmpty(nextForThread)) {
      delete nextByThread[threadID];
    } else {
      nextByThread[threadID] = nextForThread;
    }
    setPendingComposerMessagesByThreadNow(nextByThread);
  }

  function clearThreadPendingComposerMessages(threadID: string): void {
    const previousByThread = pendingComposerMessagesByThreadRef.current;
    if (!previousByThread[threadID]) {
      return;
    }
    const nextByThread = { ...previousByThread };
    delete nextByThread[threadID];
    setPendingComposerMessagesByThreadNow(nextByThread);
  }

  function removePendingComposerMessageByID(
    threadID: string | undefined,
    id: string,
  ): void {
    if (!threadID || !id) {
      return;
    }
    updateThreadPendingComposerMessages(threadID, (previous) => ({
      queued: previous.queued.filter((message) => message.id !== id),
      guides: previous.guides.filter((message) => message.id !== id),
    }));
  }

  function syncPendingComposerMessagesFromServerEvent(event: ServerEvent): void {
    if (event.kind !== "notification") {
      return;
    }
    const params = isRecord(event.message.params)
      ? event.message.params
      : undefined;
    if (!params) {
      return;
    }
    const threadID = stringValue(params, "thread_id");
    if (event.message.method === "turn/started") {
      const queueID = stringValue(params, "queue_id");
      if (queueID) {
        removePendingComposerMessageByID(threadID, queueID);
      }
      return;
    }
    if (event.message.method === "turn/dequeued") {
      const queueID = stringValue(params, "queue_id");
      if (queueID) {
        removePendingComposerMessageByID(threadID, queueID);
      }
      return;
    }
    if (event.message.method === "item/completed") {
      const item = threadItemFromRecord(recordValue(params, "item"));
      if (item?.type === "user_message" && item.source_id) {
        removePendingComposerMessageByID(threadID, item.source_id);
      }
    }
  }

  function enqueueComposerMessage(
    threadID: string,
    message: QueuedComposerMessage,
  ): void {
    updateThreadPendingComposerMessages(threadID, (previous) => ({
      ...previous,
      queued: [...previous.queued, message],
    }));
  }

  async function removeQueuedMessage(id: string): Promise<boolean> {
    const target = findPendingComposerMessage(
      pendingComposerMessagesByThreadRef.current,
      id,
      "queue",
      activeThreadIDForState(appStateRef.current),
    );
    if (!target) {
      return false;
    }
    updateThreadPendingComposerMessages(target.threadID, (previous) => ({
      ...previous,
      queued: previous.queued.filter((message) => message.id !== id),
    }));
    try {
      const result = await window.wuu.dequeueTurn(target.threadID, id);
      if (!result.ok) {
        setState((current) => ({
          ...current,
          status: "排队消息已被处理，无法取消",
        }));
        return false;
      }
      return true;
    } catch (error) {
      updateThreadPendingComposerMessages(target.threadID, (previous) => {
        if (previous.queued.some((message) => message.id === id)) {
          return previous;
        }
        const insertAt = Math.min(target.index, previous.queued.length);
        return {
          ...previous,
          queued: [
            ...previous.queued.slice(0, insertAt),
            target.message,
            ...previous.queued.slice(insertAt),
          ],
        };
      });
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "取消排队失败",
      }));
      return false;
    }
  }

  async function removeGuideMessage(id: string): Promise<boolean> {
    const target = findPendingComposerMessage(
      pendingComposerMessagesByThreadRef.current,
      id,
      "guide",
      activeThreadIDForState(appStateRef.current),
    );
    if (!target) {
      return false;
    }
    updateThreadPendingComposerMessages(target.threadID, (previous) => ({
      ...previous,
      guides: previous.guides.filter((message) => message.id !== id),
    }));
    try {
      const result = await window.wuu.unsteerTurn(target.threadID, id);
      if (!result.ok) {
        setState((current) => ({
          ...current,
          status: "引导消息已被处理，无法取消",
        }));
        return false;
      }
      return true;
    } catch (error) {
      updateThreadPendingComposerMessages(target.threadID, (previous) => {
        if (previous.guides.some((message) => message.id === id)) {
          return previous;
        }
        const insertAt = Math.min(target.index, previous.guides.length);
        return {
          ...previous,
          guides: [
            ...previous.guides.slice(0, insertAt),
            target.message,
            ...previous.guides.slice(insertAt),
          ],
        };
      });
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "取消引导失败",
      }));
      return false;
    }
  }

  function restorePendingComposerMessage(message: QueuedComposerMessage): void {
    setPrompt(message.text);
    setComposerImages(message.images.map((image) => ({ ...image })));
    setComposerFiles(message.files.map((file) => ({ ...file })));
  }

  function canRestorePendingComposerMessage(): boolean {
    if (
      prompt.trim().length === 0 &&
      composerImages.length === 0 &&
      composerFiles.length === 0
    ) {
      return true;
    }
    setState((current) => ({
      ...current,
      status: "先发送或清空当前输入，再编辑排队消息",
    }));
    return false;
  }

  async function editQueuedMessage(id: string): Promise<void> {
    const target = findPendingComposerMessage(
      pendingComposerMessagesByThreadRef.current,
      id,
      "queue",
      activeThreadIDForState(appStateRef.current),
    );
    if (!target || !canRestorePendingComposerMessage()) {
      return;
    }
    if (await removeQueuedMessage(id)) {
      restorePendingComposerMessage(target.message);
    }
  }

  async function editGuideMessage(id: string): Promise<void> {
    const target = findPendingComposerMessage(
      pendingComposerMessagesByThreadRef.current,
      id,
      "guide",
      activeThreadIDForState(appStateRef.current),
    );
    if (!target || !canRestorePendingComposerMessage()) {
      return;
    }
    if (await removeGuideMessage(id)) {
      restorePendingComposerMessage(target.message);
    }
  }

  async function guideQueuedMessage(id: string): Promise<void> {
    const target = findPendingComposerMessage(
      pendingComposerMessagesByThreadRef.current,
      id,
      "queue",
      activeThreadIDForState(appStateRef.current),
    );
    if (!target) {
      return;
    }
    const currentState = appStateRef.current;
    const targetThread = threadForTab(currentState, target.threadID);
    if (!targetThread) {
      return;
    }
    if (!isThreadRunning(targetThread)) {
      updateThreadPendingComposerMessages(target.threadID, (previous) => ({
        ...previous,
        queued: previous.queued.filter((message) => message.id !== id),
      }));
      void sendComposerMessageToThread(target.message, targetThread);
      return;
    }
    const turnID = activeTurnIDForThread(targetThread);
    if (!turnID) {
      setState((current) => ({
        ...current,
        status: "没有可引导的任务",
      }));
      return;
    }
    try {
      const files = inputFilesFromComposer(target.message.files);
      await window.wuu.steerTurn(
        targetThread.id,
        turnID,
        target.message.text.trim(),
        inputImagesFromComposer(target.message.images),
        target.message.id,
        files,
      );
      updateThreadPendingComposerMessages(target.threadID, (previous) => ({
        queued: previous.queued.filter((message) => message.id !== id),
        guides: [...previous.guides, target.message],
      }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "引导失败",
      }));
    }
  }

  function renderComposer(variant: ComposerVariant): JSX.Element {
    const tokenSpeed = activeTurnTokenSpeedSnapshot(
      state,
      activeThread ? activeTurnIDForThread(activeThread) : undefined,
    );
    return (
      <Composer
        variant={variant}
        containerRef={variant === "dock" ? dockComposerRef : undefined}
        prompt={prompt}
        setPrompt={setPrompt}
        files={composerFiles}
        images={composerImages}
        queuedMessages={queuedMessages}
        guideMessages={guideMessages}
        running={
          (!activeThreadReadOnly && activeThreadIsRunning) || viewContextSwitchPending
        }
        runtimeControlsDisabled={viewContextSwitchPending}
        tokensPerSecond={tokenSpeed.tokensPerSecond}
        tokenSpeedSampledAt={tokenSpeed.sampledAt}
        tokenSpeedSource={tokenSpeed.source}
        status={
          activeThreadReadOnly
            ? activeThreadIsRunning
              ? "子任务运行中"
              : "子任务会话只读"
            : state.status
        }
        readOnly={activeThreadReadOnly}
        initialized={state.initialized}
        gitStatus={state.gitStatus}
        projects={state.projects}
        activeContext={state.activeContext}
        activeProject={activeProject}
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
        onOpenSkillsCatalog={openSkillsTab}
        onSelectProject={(id) => void selectProjectForNewThread(id)}
        onSelectNoProject={() => void useNoProject(false)}
        onSelectGitBranch={(branch) => void checkoutBranch(branch)}
        onCreateProject={() => void createBlankProject()}
        onOpenProject={() => void chooseProjectFolder()}
        onStartNewThread={() => void startNewThread()}
        onOpenWorkspaceTool={openWorkspaceTool}
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
        onCancelGoal={cancelCurrentGoal}
        onClearGoal={clearCurrentGoal}
        queryHistorySessionID={activeThread?.id}
        queryHistory={queryTextsForThread(activeThread)}
      />
    );
  }

  function renderConversationSplitPane(
    thread: Thread,
    pane: ConversationPaneID,
  ): JSX.Element {
    return (
      <ConversationSplitPane
        pane={pane}
        thread={thread}
        threads={state.threads}
        active={state.activePane === pane}
        activeContextCwd={state.activeContext?.cwd}
        appStatus={state.status}
        draft={splitComposerDrafts[pane] ?? emptyComposerDraft()}
        viewSwitchPending={viewContextSwitchPending}
        queryHistory={queryTextsForThread(thread)}
        editingMessage={
          historyMessageEdit?.threadID === thread.id
            ? historyMessageEdit
            : undefined
        }
        onActivate={() => activateConversationPane(pane)}
        onClose={() => closeConversationPane(pane)}
        onBodyRef={(node) => {
          splitPaneRefs.current[pane] = node;
        }}
        onScroll={handleConversationScroll}
        onSetPrompt={(value) => setSplitComposerPrompt(pane, value)}
        onPasteAttachmentFiles={(files) =>
          void attachSplitComposerAttachmentFiles(pane, files)
        }
        onRemoveFile={(id) => removeSplitComposerFile(pane, id)}
        onRemoveImage={(id) => removeSplitComposerImage(pane, id)}
        onSend={() => void sendPromptForPane(pane)}
        onInterrupt={() => void interruptPane(pane)}
        onForkMessage={(turnID, itemID) =>
          void forkThreadFromMessage(thread, turnID, itemID)
        }
        onEditMessage={
          canShowHistoryEditButton(thread)
            ? (turnID, item) =>
                startEditingThreadMessageFromHistory(thread, turnID, item, pane)
            : undefined
        }
        onCancelEditMessage={cancelEditingThreadMessage}
        onSubmitEditMessage={(turnID, item, text, images, files) =>
          void submitEditedThreadMessageFromHistory(thread, turnID, item, text, images, files, pane)
        }
        onStreamFrame={scheduleStreamScroll}
        onNoticeAction={handleNoticeAction}
      />
    );
  }

  function renderSessionTabs(): JSX.Element {
    return (
      <SessionTabStrip
        state={state}
        pendingSwitchThreadID={visiblePendingThreadID}
        pendingComposerMessagesByThread={pendingComposerMessagesByThread}
        canStartNewThread={Boolean(state.activeContext)}
        onSelect={(tabID) => void selectSessionTab(tabID)}
        onClose={(tabID) => void closeSessionTab(tabID)}
        onCloseTabs={(tabIDs) => void closeSessionTabs(tabIDs)}
        onNewThread={() => void startNewThread()}
        onReorder={reorderSessionTabs}
      />
    );
  }

  function renderTitleContent(): JSX.Element {
    if (sessionTabsVisible) {
      return renderSessionTabs();
    }
    return (
      <>
        {workspaceMode ? (
          <span className="workspace-title-icon" aria-hidden="true">
            <WorkspaceToolIcon view={workspaceMode} className="icon-lg" />
          </span>
        ) : null}
        <h1>{activeTitle}</h1>
      </>
    );
  }

  function reorderSessionTabs(activeID: string, overID: string): void {
    setState((current) => {
      const sourceIndex = current.sessionTabs.findIndex(
        (tab) => tab.id === activeID,
      );
      const targetIndex = current.sessionTabs.findIndex(
        (tab) => tab.id === overID,
      );
      if (sourceIndex < 0 || targetIndex < 0) {
        return current;
      }
      return {
        ...current,
        sessionTabs: arrayMove(current.sessionTabs, sourceIndex, targetIndex),
      };
    });
  }

  function clearViewSwitchDelay(): void {
    if (viewSwitchDelayTimerRef.current === undefined) {
      return;
    }
    window.clearTimeout(viewSwitchDelayTimerRef.current);
    viewSwitchDelayTimerRef.current = undefined;
  }

  function beginViewSwitch(
    kind: PendingViewSwitchKind,
    targetID: string,
  ): number {
    const requestID = viewSwitchRequestRef.current + 1;
    viewSwitchRequestRef.current = requestID;
    clearViewSwitchDelay();
    // Thread switches resolve from in-memory thread data plus a single
    // resume round-trip. The IPC completes in tens of milliseconds and
    // the user already has the thread loaded, so the switch should feel
    // instant. Showing a delayed loading shimmer here flashes the
    // conversation area, pollutes the composer's "running" indicator,
    // and adds an "in-flight" guard window for a transition that the
    // user perceives as a local tab change — so we mark the switch
    // visible from the start. Project and runtime-context switches
    // genuinely need to load remote resources, so they keep the
    // deferred-visible shimmer.
    if (kind === "thread") {
      setPendingViewSwitch({ kind, targetID, visible: true });
      return requestID;
    }
    setPendingViewSwitch({ kind, targetID, visible: false });
    viewSwitchDelayTimerRef.current = window.setTimeout(() => {
      viewSwitchDelayTimerRef.current = undefined;
      if (viewSwitchRequestRef.current !== requestID) {
        return;
      }
      setPendingViewSwitch((current) =>
        current?.kind === kind && current.targetID === targetID
          ? { ...current, visible: true }
          : current,
      );
    }, VIEW_SWITCH_LOADING_DELAY_MS);
    return requestID;
  }

  function finishViewSwitch(requestID: number): boolean {
    if (viewSwitchRequestRef.current !== requestID) {
      return false;
    }
    clearViewSwitchDelay();
    setPendingViewSwitch(undefined);
    return true;
  }

  function cancelViewSwitch(): void {
    viewSwitchRequestRef.current += 1;
    clearViewSwitchDelay();
    setPendingViewSwitch(undefined);
  }

  async function loadRuntime(
    projectState: ProjectListResult,
    options: { resumeLatestThread?: boolean } = {},
  ): Promise<Partial<AppState>> {
    if (!projectState.active_context) {
      return emptyRuntimeState(projectState);
    }
    const resumeLatestThread = options.resumeLatestThread ?? true;
    const initialized = await window.wuu.initialize();
    const listed = await window.wuu.listThreads();
    const listedThreads = sortThreads(listed.threads);
    const defaultThread = resumeLatestThread
      ? (listedThreads.find((candidate) => !candidate.pinned) ??
        listedThreads[0])
      : undefined;
    const thread = defaultThread
      ? requireThread(
          await window.wuu.resumeThread(defaultThread.id),
          "resume did not return a thread",
        )
      : undefined;
    return {
      initialized,
      projects: projectState.projects,
      activeContext: projectState.active_context,
      activeProjectId: activeProjectID(projectState.active_context),
      gitStatus: undefined,
      thread,
      secondaryThread: undefined,
      activePane: "primary",
      allowThreadAutoActivation: Boolean(thread),
      threads: thread ? upsertThread(listedThreads, thread) : listedThreads,
      running: isThreadRunning(thread),
      status: "ready",
    };
  }

  function emptyRuntimeState(
    projectState: ProjectListResult,
  ): Partial<AppState> {
    return {
      initialized: undefined,
      projects: projectState.projects,
      activeContext: undefined,
      activeProjectId: undefined,
      gitStatus: undefined,
      thread: undefined,
      secondaryThread: undefined,
      activePane: "primary",
      allowThreadAutoActivation: false,
      threads: [],
      running: false,
      status: "no-runtime",
    };
  }

  async function selectRuntimeContext(
    context: RuntimeContext,
  ): Promise<ProjectListResult> {
    if (context.kind === "project") {
      return window.wuu.selectProject(context.project_id);
    }
    return window.wuu.selectNoProject(false, context.cwd);
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

  async function openProject(projectId: string): Promise<void> {
    const currentState = appStateRef.current;
    if (
      projectId === currentState.activeProjectId &&
      currentState.activeContext?.kind === "project"
    ) {
      closeProjectMenus();
      setArchiveConfirmThreadID(undefined);
      setWorkspaceMode(undefined);
      if (currentState.thread || currentState.secondaryThread) {
        const nextTab = nextDraftSessionTab(currentState.activeContext);
        setPrompt("");
        setComposerImages([]);
        setComposerFiles([]);
        setState((current) => ({
          ...persistActiveSessionTabDraft(
            current,
            currentPrimaryComposerDraft(),
          ),
          thread: undefined,
          secondaryThread: undefined,
          activePane: "primary",
          sessionTabs: ensureSessionTab(current.sessionTabs, nextTab),
          activeSessionTabID: nextTab.id,
          allowThreadAutoActivation: false,
          running: false,
          status: "ready",
        }));
      }
      return;
    }
    const requestID = beginViewSwitch("project", projectId);
    closeProjectMenus();
    setWorkspaceMode(undefined);
    const outgoingDraft = currentPrimaryComposerDraft();
    try {
      const projectState = await window.wuu.selectProject(projectId);
      const loadedState = await loadRuntime(projectState, {
        resumeLatestThread: false,
      });
      if (!finishViewSwitch(requestID)) {
        return;
      }
      restoreLoadedRuntimeComposerDraft(loadedState);
      setState((current) => {
        const next = withLoadedRuntimeSessionTab(
          persistActiveSessionTabDraft(current, outgoingDraft),
          loadedState,
        );
        return {
          ...next,
          thread: undefined,
          secondaryThread: undefined,
          activePane: "primary",
          allowThreadAutoActivation: false,
          running: false,
          status: "ready",
        };
      });
    } catch (error) {
      if (!finishViewSwitch(requestID)) {
        return;
      }
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "open project failed",
      }));
    }
  }

  async function selectProjectForNewThread(projectId: string): Promise<void> {
    const currentState = appStateRef.current;
    if (
      projectId === currentState.activeProjectId &&
      currentState.activeContext?.kind === "project"
    ) {
      closeProjectMenus();
      setArchiveConfirmThreadID(undefined);
      setWorkspaceMode(undefined);
      if (currentState.thread || currentState.secondaryThread) {
        const nextTab = nextDraftSessionTab(currentState.activeContext);
        setPrompt("");
        setComposerImages([]);
        setComposerFiles([]);
        setState((current) => ({
          ...persistActiveSessionTabDraft(
            current,
            currentPrimaryComposerDraft(),
          ),
          thread: undefined,
          secondaryThread: undefined,
          activePane: "primary",
          sessionTabs: ensureSessionTab(current.sessionTabs, nextTab),
          activeSessionTabID: nextTab.id,
          allowThreadAutoActivation: false,
          running: false,
          status: "ready",
        }));
      }
      return;
    }
    if (isAnyThreadRunning(currentState)) {
      closeProjectMenus();
      setState((current) => ({
        ...current,
        status: "任务运行中，暂不能切换项目",
      }));
      return;
    }
    const requestID = beginViewSwitch("project", projectId);
    closeProjectMenus();
    setArchiveConfirmThreadID(undefined);
    setWorkspaceMode(undefined);
    const outgoingDraft = currentPrimaryComposerDraft();
    try {
      const projectState = await window.wuu.selectProject(projectId);
      const loadedState = await loadRuntime(projectState, {
        resumeLatestThread: false,
      });
      if (!finishViewSwitch(requestID)) {
        return;
      }
      restoreLoadedRuntimeComposerDraft(loadedState);
      setState((current) => {
        const next = withLoadedRuntimeSessionTab(
          persistActiveSessionTabDraft(current, outgoingDraft),
          loadedState,
        );
        return {
          ...next,
          thread: undefined,
          secondaryThread: undefined,
          activePane: "primary",
          allowThreadAutoActivation: false,
          running: false,
          status: "ready",
        };
      });
    } catch (error) {
      if (!finishViewSwitch(requestID)) {
        return;
      }
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "open project failed",
      }));
    }
  }

  async function startNewThreadForProject(projectId: string): Promise<void> {
    cancelViewSwitch();
    closeProjectMenus();
    setArchiveConfirmThreadID(undefined);
    setWorkspaceMode(undefined);
    if (
      projectId === state.activeProjectId &&
      state.activeContext?.kind === "project"
    ) {
      setPrompt("");
      setComposerImages([]);
      setComposerFiles([]);
      const nextTab =
        activeSessionTab(state)?.kind === "draft" &&
        !prompt.trim() &&
        composerImages.length === 0 &&
        composerFiles.length === 0
          ? activeSessionTab(state)
          : nextDraftSessionTab(state.activeContext);
      if (!nextTab) {
        return;
      }
      setState((current) => ({
        ...persistActiveSessionTabDraft(current, currentPrimaryComposerDraft()),
        thread: undefined,
        secondaryThread: undefined,
        activePane: "primary",
        sessionTabs: ensureSessionTab(current.sessionTabs, nextTab),
        activeSessionTabID: nextTab.id,
        allowThreadAutoActivation: false,
        running: false,
        status: "ready",
      }));
      return;
    }
    const requestID = beginViewSwitch("project", projectId);
    const outgoingDraft = currentPrimaryComposerDraft();
    try {
      const projectState = await window.wuu.selectProject(projectId);
      const loadedState = await loadRuntime(projectState, {
        resumeLatestThread: false,
      });
      if (!finishViewSwitch(requestID)) {
        return;
      }
      if (!loadedState.activeContext) {
        return;
      }
      setPrompt("");
      setComposerImages([]);
      setComposerFiles([]);
      const nextTab = nextDraftSessionTab(loadedState.activeContext);
      setState((current) => {
        const withDraft = persistActiveSessionTabDraft(current, outgoingDraft);
        return {
          ...withDraft,
          ...loadedState,
          thread: undefined,
          secondaryThread: undefined,
          activePane: "primary",
          sessionTabs: ensureSessionTab(withDraft.sessionTabs, nextTab),
          activeSessionTabID: nextTab.id,
          allowThreadAutoActivation: false,
          running: false,
          status: "ready",
        };
      });
    } catch (error) {
      if (!finishViewSwitch(requestID)) {
        return;
      }
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "open project failed",
      }));
    }
  }

  async function createBlankProject(): Promise<void> {
    const requestID = beginViewSwitch("runtime", "create-project");
    closeProjectMenus();
    const outgoingDraft = currentPrimaryComposerDraft();
    try {
      const projectState = await window.wuu.createBlankProject();
      if (
        sameRuntimeContext(projectState.active_context, state.activeContext)
      ) {
        if (!finishViewSwitch(requestID)) {
          return;
        }
        setState((current) => ({
          ...current,
          projects: projectState.projects,
        }));
        return;
      }
      const loadedState = await loadRuntime(projectState);
      if (!finishViewSwitch(requestID)) {
        return;
      }
      restoreLoadedRuntimeComposerDraft(loadedState);
      setState((current) =>
        withLoadedRuntimeSessionTab(
          persistActiveSessionTabDraft(current, outgoingDraft),
          loadedState,
        ),
      );
    } catch (error) {
      if (!finishViewSwitch(requestID)) {
        return;
      }
      setState((current) => ({
        ...current,
        status:
          error instanceof Error ? error.message : "create project failed",
      }));
    }
  }

  async function chooseProjectFolder(): Promise<void> {
    const requestID = beginViewSwitch("runtime", "choose-project");
    closeProjectMenus();
    const outgoingDraft = currentPrimaryComposerDraft();
    try {
      const projectState = await window.wuu.chooseProjectFolder();
      if (
        sameRuntimeContext(projectState.active_context, state.activeContext)
      ) {
        if (!finishViewSwitch(requestID)) {
          return;
        }
        setState((current) => ({
          ...current,
          projects: projectState.projects,
        }));
        return;
      }
      const loadedState = await loadRuntime(projectState);
      if (!finishViewSwitch(requestID)) {
        return;
      }
      restoreLoadedRuntimeComposerDraft(loadedState);
      setState((current) =>
        withLoadedRuntimeSessionTab(
          persistActiveSessionTabDraft(current, outgoingDraft),
          loadedState,
        ),
      );
    } catch (error) {
      if (!finishViewSwitch(requestID)) {
        return;
      }
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "open folder failed",
      }));
    }
  }

  async function useNoProject(fresh: boolean): Promise<void> {
    if (!fresh && state.activeContext?.kind === "no_project") {
      closeProjectMenus();
      return;
    }
    const requestID = beginViewSwitch(
      "runtime",
      fresh ? "no-project:fresh" : "no-project",
    );
    closeProjectMenus();
    const outgoingDraft = currentPrimaryComposerDraft();
    try {
      const projectState = await window.wuu.selectNoProject(fresh);
      const loadedState = await loadRuntime(projectState);
      if (!finishViewSwitch(requestID)) {
        return;
      }
      restoreLoadedRuntimeComposerDraft(loadedState);
      setState((current) =>
        withLoadedRuntimeSessionTab(
          persistActiveSessionTabDraft(current, outgoingDraft),
          loadedState,
        ),
      );
    } catch (error) {
      if (!finishViewSwitch(requestID)) {
        return;
      }
      setState((current) => ({
        ...current,
        status:
          error instanceof Error ? error.message : "open no-project failed",
      }));
    }
  }

  async function checkoutBranch(branch: string): Promise<void> {
    if (!branch || anyThreadIsRunning) {
      return;
    }
    closeProjectMenus();
    try {
      const gitStatus = await window.wuu.checkoutGitBranch(branch);
      setState((current) => ({
        ...current,
        gitStatus,
        status: current.status === "ready" ? "ready" : current.status,
      }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status:
          error instanceof Error ? error.message : "checkout branch failed",
      }));
    }
  }

  async function refreshGitStatus(): Promise<void> {
    const context = appStateRef.current.activeContext;
    if (!context) {
      return;
    }
    if (gitRefreshInFlightRef.current) {
      gitRefreshQueuedRef.current = true;
      return;
    }
    gitRefreshInFlightRef.current = true;
    try {
      const gitStatus = await window.wuu.gitStatus();
      if (!sameRuntimeContext(appStateRef.current.activeContext, context)) {
        return;
      }
      setState((current) => ({
        ...current,
        gitStatus,
        status: current.status === "ready" ? "ready" : current.status,
      }));
    } catch (error) {
      if (!sameRuntimeContext(appStateRef.current.activeContext, context)) {
        return;
      }
      setState((current) => ({
        ...current,
        status:
          error instanceof Error ? error.message : "refresh git status failed",
      }));
    } finally {
      gitRefreshInFlightRef.current = false;
      if (gitRefreshQueuedRef.current) {
        gitRefreshQueuedRef.current = false;
        scheduleGitStatusRefresh(150);
      }
    }
  }

  function scheduleGitStatusRefresh(delayMs: number): void {
    if (!appStateRef.current.activeContext) {
      return;
    }
    if (gitRefreshTimerRef.current !== undefined) {
      window.clearTimeout(gitRefreshTimerRef.current);
    }
    gitRefreshTimerRef.current = window.setTimeout(() => {
      gitRefreshTimerRef.current = undefined;
      void refreshGitStatus();
    }, delayMs);
  }

  async function refreshManagedProcesses(): Promise<void> {
    const context = appStateRef.current.activeContext;
    if (!context || !appStateRef.current.initialized) {
      return;
    }
    if (managedProcessRefreshInFlightRef.current) {
      managedProcessRefreshQueuedRef.current = true;
      return;
    }
    managedProcessRefreshInFlightRef.current = true;
    try {
      const result = await window.wuu.listManagedProcesses();
      if (!sameRuntimeContext(appStateRef.current.activeContext, context)) {
        return;
      }
      setManagedProcesses(result.processes ?? []);
    } catch {
      if (sameRuntimeContext(appStateRef.current.activeContext, context)) {
        setManagedProcesses([]);
      }
    } finally {
      managedProcessRefreshInFlightRef.current = false;
      if (managedProcessRefreshQueuedRef.current) {
        managedProcessRefreshQueuedRef.current = false;
        scheduleManagedProcessRefresh(200);
      }
    }
  }

  function scheduleManagedProcessRefresh(delayMs: number): void {
    if (!appStateRef.current.activeContext || !appStateRef.current.initialized) {
      return;
    }
    if (managedProcessRefreshTimerRef.current !== undefined) {
      window.clearTimeout(managedProcessRefreshTimerRef.current);
    }
    managedProcessRefreshTimerRef.current = window.setTimeout(() => {
      managedProcessRefreshTimerRef.current = undefined;
      void refreshManagedProcesses();
    }, delayMs);
  }

  async function stopBackgroundProcess(process: BackgroundProcessItem): Promise<void> {
    if (!process.id.startsWith("proc-") || stoppingProcessIDs.has(process.id)) {
      return;
    }
    setStoppingProcessIDs((current) => {
      const next = new Set(current);
      next.add(process.id);
      return next;
    });
    try {
      const result = await window.wuu.stopManagedProcess(process.id);
      setManagedProcesses((current) => upsertManagedProcess(current, result.process));
      scheduleManagedProcessRefresh(300);
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "stop process failed",
      }));
    } finally {
      setStoppingProcessIDs((current) => {
        const next = new Set(current);
        next.delete(process.id);
        return next;
      });
    }
  }

  function openBackgroundProcessPreview(process: BackgroundProcessItem): void {
    const target = process.primaryPreviewURL || process.previewURLs?.[0];
    if (!target) {
      return;
    }
    openBrowserURL(target);
    closeEnvironmentPanel({ dismissed: true });
  }

  async function createAndCheckoutBranch(branch: string): Promise<void> {
    if (!branch || anyThreadIsRunning) {
      return;
    }
    try {
      const result = await window.wuu.createCheckoutGitBranch(branch);
      setState((current) => ({
        ...current,
        gitStatus: result.status,
        status: current.status === "ready" ? "ready" : current.status,
      }));
      setEnvironmentPanelMenu(null);
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "create branch failed",
      }));
      throw error;
    }
  }

  async function commitEnvironmentChanges(params: {
    message: string;
    includeUnstaged: boolean;
  }): Promise<GitCommitResult> {
    const result = await window.wuu.commitGitChanges({
      message: params.message,
      include_unstaged: params.includeUnstaged,
    });
    setState((current) => ({
      ...current,
      gitStatus: result.status,
      status: `已提交 ${result.commit}`,
    }));
    return result;
  }

  async function createEnvironmentPullRequest(params: {
    title: string;
    body: string;
    draft: boolean;
  }): Promise<GitPullRequestResult> {
    const result = await window.wuu.createPullRequest({
      title: params.title,
      body: params.body,
      draft: params.draft,
    });
    setState((current) => ({
      ...current,
      gitStatus: result.status,
      status: result.already_exists ? "已有拉取请求" : "已创建拉取请求",
    }));
    return result;
  }

  function toggleEnvironmentPanel(): void {
    const visible = environmentPanelVisible;
    if (visible) {
      closeEnvironmentPanel({ dismissed: true });
      return;
    }
    setEnvironmentPanelOpen(true);
    setEnvironmentPanelDismissed(false);
    setRuntimeMenuOpen(false);
    setAccessMenuOpen(false);
    setBranchMenuOpen(false);
    setCodexRuntimeMenu(null);
  }

  function closeEnvironmentPanel({
    dismissed = false,
  }: { dismissed?: boolean } = {}): void {
    restoreEnvironmentPanelFocus();
    setEnvironmentPanelOpen(false);
    if (dismissed) {
      setEnvironmentPanelDismissed(true);
    }
    setEnvironmentPanelMenu(null);
  }

  function restoreEnvironmentPanelFocus(): void {
    const activeElement = document.activeElement;
    if (
      activeElement instanceof HTMLElement &&
      environmentPanelRef.current?.contains(activeElement)
    ) {
      environmentToggleRef.current?.focus({ preventScroll: true });
    }
  }

  function openBackgroundProcessPanel(): void {
    setEnvironmentPanelOpen(true);
    setEnvironmentPanelDismissed(false);
    setEnvironmentPanelMenu(null);
    setRunDebugOpen(false);
    setRuntimeMenuOpen(false);
    setAccessMenuOpen(false);
    setBranchMenuOpen(false);
    setCodexRuntimeMenu(null);
  }

  function currentPrimaryComposerDraft(): ComposerDraftState {
    return {
      prompt,
      images: composerImages.map((image) => ({ ...image })),
      files: composerFiles.map((file) => ({ ...file })),
    };
  }

  function restorePrimaryComposerDraft(draft: ComposerDraftState): void {
    setPrompt(draft.prompt);
    setComposerImages(draft.images.map((image) => ({ ...image })));
    setComposerFiles(draft.files.map((file) => ({ ...file })));
  }

  function threadHasPendingComposerMessages(threadID: string): boolean {
    return !threadPendingComposerMessagesIsEmpty(
      pendingComposerMessagesForThread(
        pendingComposerMessagesByThreadRef.current,
        threadID,
      ),
    );
  }

  function canShowHistoryEditButton(thread: Thread): boolean {
    return (
      !thread.read_only &&
      !isThreadRunning(thread) &&
      !localDemoThreadsRef.current.has(thread.id) &&
      threadPendingComposerMessagesIsEmpty(
        pendingComposerMessagesForThread(
          pendingComposerMessagesByThread,
          thread.id,
        ),
      )
    );
  }

  function restoreSessionTabComposerDraft(tab: SessionTab): void {
    restorePrimaryComposerDraft(cloneSessionTabDraft(tab));
    setSplitComposerDrafts(initialSplitComposerDrafts());
  }

  function restoreLoadedRuntimeComposerDraft(
    loadedState: Partial<AppState>,
  ): void {
    const context = loadedState.activeContext;
    if (!context) {
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

  async function selectSessionTab(tabID: string): Promise<void> {
    const currentState = appStateRef.current;
    if (tabID === currentState.activeSessionTabID) {
      return;
    }
    const tab = currentState.sessionTabs.find((item) => item.id === tabID);
    if (!tab) {
      return;
    }
    setArchiveConfirmThreadID(undefined);
    const sameContext = sameRuntimeContext(
      tab.context,
      currentState.activeContext,
    );
    if (tab.kind === "file") {
      const outgoingDraft = currentPrimaryComposerDraft();
      const requestID = sameContext
        ? undefined
        : beginViewSwitch("runtime", runtimeContextKey(tab.context));
      try {
        const loadedState = sameContext
          ? undefined
          : await loadRuntime(await selectRuntimeContext(tab.context), {
              resumeLatestThread: false,
            });
        if (requestID !== undefined && !finishViewSwitch(requestID)) {
          return;
        }
        if (requestID === undefined) {
          cancelViewSwitch();
        }
        setSplitComposerDrafts(initialSplitComposerDrafts());
        setWorkspaceMode("files");
        setState((current) => {
          const withDraft = persistActiveSessionTabDraft(
            current,
            outgoingDraft,
          );
          const next = loadedState
            ? { ...withDraft, ...loadedState }
            : withDraft;
          return {
            ...next,
            secondaryThread: undefined,
            activePane: "primary",
            sessionTabs: ensureSessionTab(next.sessionTabs, tab),
            activeSessionTabID: tab.id,
            allowThreadAutoActivation: false,
            running: false,
            status: "ready",
          };
        });
      } catch (error) {
        if (requestID !== undefined && !finishViewSwitch(requestID)) {
          return;
        }
        setState((current) => ({
          ...current,
          status: error instanceof Error ? error.message : "load failed",
        }));
      }
      return;
    }
    if (tab.kind === "skills") {
      const outgoingDraft = currentPrimaryComposerDraft();
      const requestID = sameContext
        ? undefined
        : beginViewSwitch("runtime", runtimeContextKey(tab.context));
      try {
        const loadedState = sameContext
          ? undefined
          : await loadRuntime(await selectRuntimeContext(tab.context), {
              resumeLatestThread: false,
            });
        if (requestID !== undefined && !finishViewSwitch(requestID)) {
          return;
        }
        if (requestID === undefined) {
          cancelViewSwitch();
        }
        setSplitComposerDrafts(initialSplitComposerDrafts());
        setWorkspaceMode(undefined);
        setState((current) => {
          const withDraft = persistActiveSessionTabDraft(
            current,
            outgoingDraft,
          );
          const next = loadedState
            ? { ...withDraft, ...loadedState }
            : withDraft;
          return {
            ...next,
            secondaryThread: undefined,
            activePane: "primary",
            sessionTabs: ensureSessionTab(next.sessionTabs, tab),
            activeSessionTabID: tab.id,
            allowThreadAutoActivation: false,
            running: false,
            status: "ready",
          };
        });
      } catch (error) {
        if (requestID !== undefined && !finishViewSwitch(requestID)) {
          return;
        }
        setState((current) => ({
          ...current,
          status: error instanceof Error ? error.message : "load failed",
        }));
      }
      return;
    }
    setWorkspaceMode(undefined);
    if (tab.kind === "draft") {
      const outgoingDraft = currentPrimaryComposerDraft();
      const requestID = sameContext
        ? undefined
        : beginViewSwitch("runtime", runtimeContextKey(tab.context));
      try {
        const loadedState = sameContext
          ? undefined
          : await loadRuntime(await selectRuntimeContext(tab.context), {
              resumeLatestThread: false,
            });
        if (requestID !== undefined && !finishViewSwitch(requestID)) {
          return;
        }
        if (requestID === undefined) {
          cancelViewSwitch();
        }
        restoreSessionTabComposerDraft(tab);
        setState((current) => {
          const withDraft = persistActiveSessionTabDraft(
            current,
            outgoingDraft,
          );
          const next = loadedState
            ? { ...withDraft, ...loadedState }
            : withDraft;
          return {
            ...next,
            thread: undefined,
            secondaryThread: undefined,
            activePane: "primary",
            sessionTabs: ensureSessionTab(next.sessionTabs, tab),
            activeSessionTabID: tab.id,
            allowThreadAutoActivation: false,
            running: false,
            status: "ready",
          };
        });
      } catch (error) {
        if (requestID !== undefined && !finishViewSwitch(requestID)) {
          return;
        }
        setState((current) => ({
          ...current,
          status: error instanceof Error ? error.message : "load failed",
        }));
      }
      return;
    }
    if (sameContext) {
      await selectThread(tab.threadID);
      return;
    }
    const outgoingDraft = currentPrimaryComposerDraft();
    const targetDraft = cloneSessionTabDraft(tab);
    const requestID = beginViewSwitch("thread", tab.threadID);
    try {
      const projectState = await selectRuntimeContext(tab.context);
      const loadedState = await loadRuntime(projectState, {
        resumeLatestThread: false,
      });
      const thread = requireThread(
        await window.wuu.resumeThread(tab.threadID),
        "resume did not return a thread",
      );
      if (!finishViewSwitch(requestID)) {
        return;
      }
      restorePrimaryComposerDraft(targetDraft);
      setSplitComposerDrafts(initialSplitComposerDrafts());
      setState((current) => {
        const withDraft = persistActiveSessionTabDraft(current, outgoingDraft);
        const next = { ...withDraft, ...loadedState };
        return {
          ...next,
          thread,
          secondaryThread: undefined,
          activePane: "primary",
          allowThreadAutoActivation: true,
          sessionTabs: ensureSessionTab(
            next.sessionTabs,
            createThreadSessionTab(thread, tab.context, targetDraft),
          ),
          activeSessionTabID: threadSessionTabID(thread.id),
          threads: upsertThread(next.threads, thread),
          running: isThreadRunning(thread),
          status: "ready",
        };
      });
    } catch (error) {
      if (!finishViewSwitch(requestID)) {
        return;
      }
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "load failed",
      }));
    }
  }

  async function closeSessionTab(tabID: string): Promise<void> {
    const currentState = appStateRef.current;
    const tabIndex = currentState.sessionTabs.findIndex(
      (tab) => tab.id === tabID,
    );
    if (tabIndex < 0) {
      return;
    }
    const closingActive = currentState.activeSessionTabID === tabID;
    const nextTabs = currentState.sessionTabs.filter((tab) => tab.id !== tabID);
    const closedTab = currentState.sessionTabs[tabIndex];
    if (!closingActive) {
      setState((current) => ({
        ...current,
        sessionTabs: current.sessionTabs.filter((tab) => tab.id !== tabID),
      }));
      return;
    }

    const fallbackTab =
      nextTabs[Math.min(tabIndex, Math.max(nextTabs.length - 1, 0))] ??
      nextDraftSessionTab(closedTab.context);
    const tabsWithFallback = nextTabs.length > 0 ? nextTabs : [fallbackTab];
    setArchiveConfirmThreadID(undefined);
    if (fallbackTab.kind === "file") {
      const sameContext = sameRuntimeContext(
        fallbackTab.context,
        currentState.activeContext,
      );
      const requestID = sameContext
        ? undefined
        : beginViewSwitch("runtime", runtimeContextKey(fallbackTab.context));
      try {
        const loadedState = sameContext
          ? undefined
          : await loadRuntime(await selectRuntimeContext(fallbackTab.context), {
              resumeLatestThread: false,
            });
        if (requestID !== undefined && !finishViewSwitch(requestID)) {
          return;
        }
        if (requestID === undefined) {
          cancelViewSwitch();
        }
        setSplitComposerDrafts(initialSplitComposerDrafts());
        setWorkspaceMode("files");
        setState((current) => {
          const next = loadedState ? { ...current, ...loadedState } : current;
          return {
            ...next,
            sessionTabs: tabsWithFallback,
            activeSessionTabID: fallbackTab.id,
            secondaryThread: undefined,
            activePane: "primary",
            allowThreadAutoActivation: false,
            running: false,
            status: "ready",
          };
        });
      } catch (error) {
        if (requestID !== undefined && !finishViewSwitch(requestID)) {
          return;
        }
        setState((current) => ({
          ...current,
          status: error instanceof Error ? error.message : "load failed",
        }));
      }
      return;
    }
    if (fallbackTab.kind === "skills") {
      const sameContext = sameRuntimeContext(
        fallbackTab.context,
        currentState.activeContext,
      );
      const requestID = sameContext
        ? undefined
        : beginViewSwitch("runtime", runtimeContextKey(fallbackTab.context));
      try {
        const loadedState = sameContext
          ? undefined
          : await loadRuntime(await selectRuntimeContext(fallbackTab.context), {
              resumeLatestThread: false,
            });
        if (requestID !== undefined && !finishViewSwitch(requestID)) {
          return;
        }
        if (requestID === undefined) {
          cancelViewSwitch();
        }
        setSplitComposerDrafts(initialSplitComposerDrafts());
        setWorkspaceMode(undefined);
        setState((current) => {
          const next = loadedState ? { ...current, ...loadedState } : current;
          return {
            ...next,
            sessionTabs: tabsWithFallback,
            activeSessionTabID: fallbackTab.id,
            secondaryThread: undefined,
            activePane: "primary",
            allowThreadAutoActivation: false,
            running: false,
            status: "ready",
          };
        });
      } catch (error) {
        if (requestID !== undefined && !finishViewSwitch(requestID)) {
          return;
        }
        setState((current) => ({
          ...current,
          status: error instanceof Error ? error.message : "load failed",
        }));
      }
      return;
    }
    setWorkspaceMode(undefined);
    if (fallbackTab.kind === "draft") {
      const sameContext = sameRuntimeContext(
        fallbackTab.context,
        currentState.activeContext,
      );
      const requestID = sameContext
        ? undefined
        : beginViewSwitch("runtime", runtimeContextKey(fallbackTab.context));
      try {
        const loadedState = sameContext
          ? undefined
          : await loadRuntime(await selectRuntimeContext(fallbackTab.context), {
              resumeLatestThread: false,
            });
        if (requestID !== undefined && !finishViewSwitch(requestID)) {
          return;
        }
        if (requestID === undefined) {
          cancelViewSwitch();
        }
        restoreSessionTabComposerDraft(fallbackTab);
        setState((current) => {
          const next = loadedState ? { ...current, ...loadedState } : current;
          return {
            ...next,
            sessionTabs: tabsWithFallback,
            activeSessionTabID: fallbackTab.id,
            thread: undefined,
            secondaryThread: undefined,
            activePane: "primary",
            allowThreadAutoActivation: false,
            running: false,
            status: "ready",
          };
        });
      } catch (error) {
        if (requestID !== undefined && !finishViewSwitch(requestID)) {
          return;
        }
        setState((current) => ({
          ...current,
          status: error instanceof Error ? error.message : "load failed",
        }));
      }
      return;
    }

    const restoredDraft = cloneSessionTabDraft(fallbackTab);
    const sameContext = sameRuntimeContext(
      fallbackTab.context,
      currentState.activeContext,
    );
    const requestID = beginViewSwitch("thread", fallbackTab.threadID);
    try {
      const loadedState = sameContext
        ? undefined
        : await loadRuntime(await selectRuntimeContext(fallbackTab.context), {
            resumeLatestThread: false,
          });
      const thread = requireThread(
        await window.wuu.resumeThread(fallbackTab.threadID),
        "resume did not return a thread",
      );
      if (!finishViewSwitch(requestID)) {
        return;
      }
      restorePrimaryComposerDraft(restoredDraft);
      setSplitComposerDrafts(initialSplitComposerDrafts());
      setState((current) => {
        const next = loadedState ? { ...current, ...loadedState } : current;
        return {
          ...next,
          thread,
          secondaryThread: undefined,
          activePane: "primary",
          allowThreadAutoActivation: true,
          sessionTabs: ensureSessionTab(
            tabsWithFallback,
            createThreadSessionTab(thread, fallbackTab.context, restoredDraft),
          ),
          activeSessionTabID: threadSessionTabID(thread.id),
          threads: upsertThread(next.threads, thread),
          running: isThreadRunning(thread),
          status: "ready",
        };
      });
    } catch (error) {
      if (!finishViewSwitch(requestID)) {
        return;
      }
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "load failed",
      }));
    }
  }

  // closeSessionTabs closes a batch of tabs through the existing per-tab
  // closeSessionTab path. The active tab is closed last so its fallback
  // logic can pick a still-open sibling; if every sibling is also being
  // closed, closeSessionTab will fall back to a fresh draft.
  async function closeSessionTabs(tabIDs: string[]): Promise<void> {
    if (tabIDs.length === 0) {
      return;
    }
    const activeID = appStateRef.current.activeSessionTabID;
    const orderedIDs = tabIDs.includes(activeID)
      ? [...tabIDs.filter((id) => id !== activeID), activeID]
      : tabIDs;
    for (const tabID of orderedIDs) {
      await closeSessionTab(tabID);
    }
  }

  async function startNewThread(): Promise<void> {
    if (!state.activeContext) {
      return;
    }
    cancelViewSwitch();
    setArchiveConfirmThreadID(undefined);
    setWorkspaceMode(undefined);
    setPrompt("");
    setComposerImages([]);
    setComposerFiles([]);
    if (
      state.activeContext.kind === "no_project" &&
      (state.thread || state.secondaryThread)
    ) {
      await useNoProject(true);
      return;
    }
    const nextTab =
      activeSessionTab(state)?.kind === "draft" &&
      !prompt.trim() &&
      composerImages.length === 0 &&
      composerFiles.length === 0
        ? activeSessionTab(state)
        : nextDraftSessionTab(state.activeContext);
    if (!nextTab) {
      return;
    }
    setState((current) => ({
      ...persistActiveSessionTabDraft(current, currentPrimaryComposerDraft()),
      thread: undefined,
      secondaryThread: undefined,
      activePane: "primary",
      sessionTabs: ensureSessionTab(current.sessionTabs, nextTab),
      activeSessionTabID: nextTab.id,
      allowThreadAutoActivation: false,
      running: false,
      status: "ready",
    }));
  }

  function seedAgentTreeDemo(): void {
    if (!state.activeContext || !state.initialized) {
      return;
    }
    const activeContext = state.activeContext;
    cancelViewSwitch();
    setArchiveConfirmThreadID(undefined);
    setWorkspaceMode(undefined);
    setPrompt("");
    setComposerImages([]);
    setComposerFiles([]);
    const demo = createAgentTreeDemo(activeContext.cwd, state.initialized);
    const demoThreads = [demo.parent, ...demo.children];
    localDemoThreadsRef.current = new Map([
      ...localDemoThreadsRef.current,
      ...demoThreads.map((thread): [string, Thread] => [thread.id, thread]),
    ]);
    setState((current) => ({
      ...current,
      thread: demo.parent,
      secondaryThread: undefined,
      activePane: "primary",
      allowThreadAutoActivation: true,
      sessionTabs: ensureSessionTab(
        current.sessionTabs,
        createThreadSessionTab(demo.parent, activeContext),
      ),
      activeSessionTabID: threadSessionTabID(demo.parent.id),
      threads: upsertThread(current.threads, demo.parent),
      running: false,
      status: "ready",
    }));
  }

  function seedConversationFixture(kind: ConversationFixtureKind): void {
    if (!state.activeContext || !state.initialized) {
      return;
    }
    const activeContext = state.activeContext;
    cancelViewSwitch();
    setArchiveConfirmThreadID(undefined);
    setWorkspaceMode(undefined);
    setPrompt("");
    setComposerImages([]);
    setComposerFiles([]);
    const thread = createConversationFixture(
      kind,
      activeContext.cwd,
      state.initialized,
    );
    localDemoThreadsRef.current = new Map([
      ...localDemoThreadsRef.current,
      [thread.id, thread],
    ]);
    setState((current) => ({
      ...current,
      thread,
      secondaryThread: undefined,
      activePane: "primary",
      allowThreadAutoActivation: true,
      sessionTabs: ensureSessionTab(
        current.sessionTabs,
        createThreadSessionTab(thread, activeContext),
      ),
      activeSessionTabID: threadSessionTabID(thread.id),
      threads: upsertThread(current.threads, thread),
      running: isThreadRunning(thread),
      status: "ready",
    }));
  }

  function seedPlanPanelDebug(): void {
    if (!state.activeContext || !state.initialized) {
      return;
    }
    seedConversationFixture("plan");
    setRunDebugOpen(false);
    setEnvironmentPanelOpen(true);
    setEnvironmentPanelDismissed(false);
    setEnvironmentPanelMenu(null);
  }

  async function selectThread(threadId: string): Promise<void> {
    if (!state.activeContext) {
      return;
    }
    const activeContext = state.activeContext;
    if (threadId === activeThreadID) {
      if (pendingViewSwitch) {
        cancelViewSwitch();
      }
      return;
    }
    if (
      pendingViewSwitch?.kind === "thread" &&
      pendingViewSwitch.targetID === threadId
    ) {
      return;
    }
    setArchiveConfirmThreadID(undefined);
    setWorkspaceMode(undefined);
    const outgoingDraft = currentPrimaryComposerDraft();
    const targetDraft = sessionTabDraftForThread(state, threadId);
    const demoThread = localDemoThreadsRef.current.get(threadId);
    if (demoThread) {
      cancelViewSwitch();
      restorePrimaryComposerDraft(targetDraft);
      setSplitComposerDrafts(initialSplitComposerDrafts());
      setState((current) => {
        const withDraft = persistActiveSessionTabDraft(current, outgoingDraft);
        return {
          ...withDraft,
          thread: demoThread,
          secondaryThread: undefined,
          activePane: "primary",
          allowThreadAutoActivation: true,
          sessionTabs: ensureSessionTab(
            withDraft.sessionTabs,
            createThreadSessionTab(demoThread, activeContext, targetDraft),
          ),
          activeSessionTabID: threadSessionTabID(demoThread.id),
          threads: upsertThread(current.threads, demoThread),
          running: isThreadRunning(demoThread),
          status: "ready",
        };
      });
      return;
    }
    const sourceContext = state.activeContext;
    const requestID = beginViewSwitch("thread", threadId);
    try {
      const thread = requireThread(
        await window.wuu.resumeThread(threadId),
        "resume did not return a thread",
      );
      if (
        !finishViewSwitch(requestID) ||
        !sameRuntimeContext(appStateRef.current.activeContext, sourceContext)
      ) {
        return;
      }
      restorePrimaryComposerDraft(targetDraft);
      setSplitComposerDrafts(initialSplitComposerDrafts());
      setState((current) => {
        const withDraft = persistActiveSessionTabDraft(current, outgoingDraft);
        return {
          ...withDraft,
          thread,
          secondaryThread: undefined,
          activePane: "primary",
          allowThreadAutoActivation: true,
          sessionTabs: ensureSessionTab(
            withDraft.sessionTabs,
            createThreadSessionTab(thread, sourceContext, targetDraft),
          ),
          activeSessionTabID: threadSessionTabID(thread.id),
          threads: upsertThread(current.threads, thread),
          running: isThreadRunning(thread),
          status: "ready",
        };
      });
    } catch (error) {
      if (
        !finishViewSwitch(requestID) ||
        !sameRuntimeContext(appStateRef.current.activeContext, sourceContext)
      ) {
        return;
      }
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "load failed",
      }));
    }
  }

  async function activateThread(threadID: string): Promise<void> {
    await selectThread(threadID);
  }

  async function selectChildAgent(agent: Agent): Promise<void> {
    if (!state.activeContext) {
      return;
    }
    if (agent.id === activeThreadID) {
      if (pendingViewSwitch) {
        cancelViewSwitch();
      }
      return;
    }
    if (
      pendingViewSwitch?.kind === "thread" &&
      pendingViewSwitch.targetID === agent.id
    ) {
      return;
    }
    setArchiveConfirmThreadID(undefined);
    setWorkspaceMode(undefined);
    const outgoingDraft = currentPrimaryComposerDraft();
    const targetDraft = sessionTabDraftForThread(state, agent.id);
    const sourceContext = state.activeContext;
    const requestID = beginViewSwitch("thread", agent.id);
    try {
      const thread =
        localDemoThreadsRef.current.get(agent.id) ??
        requireThread(
          await window.wuu.resumeThread(agent.id),
          "resume did not return a child agent thread",
        );
      if (
        !finishViewSwitch(requestID) ||
        !sameRuntimeContext(appStateRef.current.activeContext, sourceContext)
      ) {
        return;
      }
      restorePrimaryComposerDraft(targetDraft);
      setSplitComposerDrafts(initialSplitComposerDrafts());
      setState((current) => {
        const withDraft = persistActiveSessionTabDraft(current, outgoingDraft);
        return {
          ...withDraft,
          thread,
          secondaryThread: undefined,
          activePane: "primary",
          allowThreadAutoActivation: true,
          sessionTabs: ensureSessionTab(
            withDraft.sessionTabs,
            createThreadSessionTab(thread, sourceContext, targetDraft),
          ),
          activeSessionTabID: threadSessionTabID(thread.id),
          threads: upsertThread(current.threads, thread),
          running: isThreadRunning(thread),
          status: "ready",
        };
      });
    } catch (error) {
      if (
        !finishViewSwitch(requestID) ||
        !sameRuntimeContext(appStateRef.current.activeContext, sourceContext)
      ) {
        return;
      }
      setState((current) => ({
        ...current,
        status:
          error instanceof Error ? error.message : "load child agent failed",
      }));
    }
  }

  function activateConversationPane(pane: ConversationPaneID): void {
    setState((current) => {
      if (pane === "secondary" && !current.secondaryThread) {
        return current;
      }
      const thread =
        pane === "secondary" ? current.secondaryThread : current.thread;
      return {
        ...current,
        activePane: pane,
        activeSessionTabID: thread
          ? threadSessionTabID(thread.id)
          : current.activeSessionTabID,
        running: isThreadRunning(thread),
      };
    });
  }

  function closeConversationPane(pane: ConversationPaneID): void {
    moveSplitDraftToGlobalComposer(
      pane === "secondary" ? "primary" : "secondary",
    );
    setState((current) => {
      if (pane === "secondary") {
        return {
          ...current,
          secondaryThread: undefined,
          activePane: "primary",
          activeSessionTabID: current.thread
            ? threadSessionTabID(current.thread.id)
            : current.activeSessionTabID,
          running: isThreadRunning(current.thread),
          status: "ready",
        };
      }
      if (current.secondaryThread) {
        return {
          ...current,
          thread: current.secondaryThread,
          secondaryThread: undefined,
          activePane: "primary",
          activeSessionTabID: threadSessionTabID(current.secondaryThread.id),
          running: isThreadRunning(current.secondaryThread),
          status: "ready",
        };
      }
      if (!current.activeContext) {
        return current;
      }
      const nextTab = createDraftSessionTab(
        `draft:closed:${Date.now()}`,
        current.activeContext,
      );
      return {
        ...current,
        thread: undefined,
        activePane: "primary",
        sessionTabs: ensureSessionTab(current.sessionTabs, nextTab),
        activeSessionTabID: nextTab.id,
        running: false,
        status: "ready",
      };
    });
  }

  async function forkThreadFromMessage(
    sourceThread: Thread,
    turnID: string,
    itemID: string,
  ): Promise<void> {
    if (!state.activeContext || sourceThread.read_only) {
      return;
    }
    const activeContext = state.activeContext;
    if (localDemoThreadsRef.current.has(sourceThread.id)) {
      setState((current) => ({ ...current, status: "示例会话不能分叉" }));
      return;
    }
    setArchiveConfirmThreadID(undefined);
    setState((current) => ({ ...current, status: "正在分叉会话" }));
    try {
      const fork = requireThread(
        await window.wuu.forkThread(sourceThread.id, turnID, itemID),
        "thread/fork did not return a thread",
      );
      enableConversationAutoFollow();
      const currentState = appStateRef.current;
      const sourcePane =
        currentState.secondaryThread?.id === sourceThread.id
          ? "secondary"
          : "primary";
      const currentSplitConversation = Boolean(
        currentState.thread && currentState.secondaryThread && !workspaceMode,
      );
      const sourceDraft = currentSplitConversation
        ? cloneComposerDraft(
            splitComposerDrafts[sourcePane] ?? emptyComposerDraft(),
          )
        : {
            prompt,
            images: composerImages.map((image) => ({ ...image })),
            files: composerFiles.map((file) => ({ ...file })),
          };
      setPrompt("");
      setComposerImages([]);
      setComposerFiles([]);
      setSplitComposerDrafts({
        primary: sourceDraft,
        secondary: emptyComposerDraft(),
      });
      setState((current) => {
        const source =
          current.secondaryThread?.id === sourceThread.id
            ? current.secondaryThread
            : current.thread?.id === sourceThread.id
              ? current.thread
              : sourceThread;
        const forkTab = createThreadSessionTab(fork, activeContext);
        return {
          ...current,
          thread: source,
          secondaryThread: fork,
          activePane: "secondary",
          allowThreadAutoActivation: true,
          sessionTabs: ensureSessionTab(
            ensureSessionTab(
              current.sessionTabs,
              createThreadSessionTab(source, activeContext, sourceDraft),
            ),
            forkTab,
          ),
          activeSessionTabID: forkTab.id,
          threads: upsertThread(upsertThread(current.threads, source), fork),
          running: isThreadRunning(fork),
          status: "ready",
        };
      });
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "fork failed",
      }));
    }
  }

  function startEditingThreadMessageFromHistory(
    sourceThread: Thread,
    turnID: string,
    item: ThreadItem,
    pane?: ConversationPaneID,
  ): void {
    if (!state.activeContext || sourceThread.read_only) {
      return;
    }
    if (localDemoThreadsRef.current.has(sourceThread.id)) {
      setState((current) => ({ ...current, status: "示例会话不能编辑历史" }));
      return;
    }
    if (isThreadRunning(sourceThread)) {
      setState((current) => ({ ...current, status: "等待当前回复结束后再编辑历史" }));
      return;
    }
    if (threadHasPendingComposerMessages(sourceThread.id)) {
      setState((current) => ({ ...current, status: "先处理待发送消息，再编辑历史" }));
      return;
    }
    setArchiveConfirmThreadID(undefined);
    setHistoryMessageEdit({
      threadID: sourceThread.id,
      turnID,
      itemID: item.id,
      pane,
      submitting: false,
    });
  }

  function cancelEditingThreadMessage(): void {
    setHistoryMessageEdit(undefined);
  }

  async function submitEditedThreadMessageFromHistory(
    sourceThread: Thread,
    turnID: string,
    item: ThreadItem,
    text: string,
    images: InputImage[],
    files: InputFile[],
    pane?: ConversationPaneID,
  ): Promise<void> {
    if (!state.activeContext || sourceThread.read_only) {
      return;
    }
    // The editor lets users remove, add, and reorder attachments, so we
    // can't trust `item.images` / `item.files` anymore — thread the
    // post-edit arrays through. IDs are only needed as React keys in the
    // composer; the protocol strips them via `inputImagesFromComposer`
    // before they hit the wire.
    const idSalt = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
    const composerImages: ComposerImage[] = images.map((image, index) => ({
      id: `edit-attach-${index}-${idSalt}`,
      media_type: image.media_type,
      data: image.data
    }));
    const composerFiles: ComposerFile[] = files.map((file, index) => ({
      id: `edit-file-${index}-${idSalt}`,
      media_type: file.media_type,
      data: file.data,
      filename: file.filename
    }));
    const message = createComposerMessage(text, composerImages, composerFiles);
    if (!message) {
      setState((current) => ({ ...current, status: "编辑内容不能为空" }));
      return;
    }
    if (isThreadRunning(sourceThread)) {
      setState((current) => ({ ...current, status: "等待当前回复结束后再编辑历史" }));
      return;
    }
    if (threadHasPendingComposerMessages(sourceThread.id)) {
      setState((current) => ({ ...current, status: "先处理待发送消息，再编辑历史" }));
      return;
    }

    setHistoryMessageEdit((current) =>
      current?.threadID === sourceThread.id &&
      current.turnID === turnID &&
      current.itemID === item.id
        ? { ...current, submitting: true }
        : current,
    );
    setArchiveConfirmThreadID(undefined);
    setState((current) => ({ ...current, status: "正在发送编辑后的消息" }));
    try {
      const result = await window.wuu.editThreadMessage(
        sourceThread.id,
        turnID,
        item.id,
      );
      const thread = requireThread(
        { thread: result.thread },
        "thread/edit-message did not return a thread",
      );
      enableConversationAutoFollow();
      const targetPane = pane ?? appStateRef.current.activePane;
      setHistoryMessageEdit(undefined);
      appStateRef.current = updateThreadByID(
        { ...appStateRef.current, activePane: targetPane },
        thread.id,
        (currentThread) => ({
          ...thread,
          child_agents: thread.child_agents ?? currentThread.child_agents,
        }),
        { status: "正在发送请求" },
      );
      setState((current) =>
        updateThreadByID(
          { ...current, activePane: targetPane },
          thread.id,
          (currentThread) => ({
            ...thread,
            child_agents: thread.child_agents ?? currentThread.child_agents,
          }),
          { status: "正在发送请求" },
        ),
      );
      const sent = await sendComposerMessageToThread(message, thread);
      if (sent) {
        // `editThreadMessage` truncated the thread at the edited user_message
        // (length = `thread.turns.length`, the original edit index), and
        // `sendComposerMessageToThread` appends the new turn at the end. Make
        // sure the new turn sits at the original edit index so the right-side
        // "historical query" list reflects the edit in place (e.g. A,B,C
        // → edit C to D → [A,B,D]; A,B,C,D → edit C to E → [A,B,E]).
        const editIndex = thread.turns.length;
        const reordered = (latest: Thread): Thread => {
          if (latest.turns.length <= editIndex) return latest;
          const newTurn = latest.turns[latest.turns.length - 1];
          if (latest.turns[editIndex] === newTurn) return latest;
          const turns = latest.turns.slice(0, editIndex);
          turns.push(newTurn);
          return { ...latest, turns };
        };
        appStateRef.current = updateThreadByID(
          { ...appStateRef.current, activePane: targetPane },
          thread.id,
          reordered,
          {},
        );
        setState((current) =>
          updateThreadByID(
            { ...current, activePane: targetPane },
            thread.id,
            reordered,
            {},
          ),
        );
      }
      if (!sent) {
        if (pane === undefined) {
          restorePrimaryComposerDraft({
            prompt: message.text,
            images: message.images.map((image) => ({ ...image })),
            files: message.files.map((file) => ({ ...file })),
          });
        } else {
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
    } catch (error) {
      setHistoryMessageEdit((current) =>
        current?.threadID === sourceThread.id &&
        current.turnID === turnID &&
        current.itemID === item.id
          ? { ...current, submitting: false }
          : current,
      );
      setState((current) => ({
        ...current,
        status: desktopApiErrorMessage(error, "编辑历史消息失败"),
      }));
    }
  }

  async function toggleThreadPinned(thread: Thread): Promise<void> {
    if (!state.activeContext) {
      return;
    }
    setArchiveConfirmThreadID(undefined);
    if (localDemoThreadsRef.current.has(thread.id)) {
      const nextThread = { ...thread, pinned: !thread.pinned };
      localDemoThreadsRef.current = new Map([
        ...localDemoThreadsRef.current,
        [thread.id, nextThread],
      ]);
      setState((current) => ({
        ...current,
        thread: current.thread?.id === thread.id ? nextThread : current.thread,
        secondaryThread:
          current.secondaryThread?.id === thread.id
            ? nextThread
            : current.secondaryThread,
        threads: upsertThread(current.threads, nextThread),
        status: current.status === "ready" ? "ready" : current.status,
      }));
      return;
    }
    try {
      const result = await window.wuu.pinThread(thread.id, !thread.pinned);
      setState((current) => ({
        ...current,
        thread:
          current.thread?.id === thread.id ? result.thread : current.thread,
        secondaryThread:
          current.secondaryThread?.id === thread.id
            ? result.thread
            : current.secondaryThread,
        threads: upsertThread(current.threads, result.thread),
        status: current.status === "ready" ? "ready" : current.status,
      }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "pin thread failed",
      }));
    }
  }

  async function archiveThread(thread: Thread): Promise<void> {
    const isLocalDemoThread = localDemoThreadsRef.current.has(thread.id);
    if (
      !state.activeContext ||
      (!isLocalDemoThread && isThreadRunning(thread))
    ) {
      return;
    }
    if (archiveConfirmThreadID !== thread.id) {
      setArchiveConfirmThreadID(thread.id);
      return;
    }
    clearThreadPendingComposerMessages(thread.id);
    const archivedActiveThread = thread.id === activeThreadID;
    const fallbackDraft = archivedActiveThread
      ? nextDraftSessionTab(state.activeContext)
      : undefined;
    if (archivedActiveThread) {
      setPrompt("");
      setComposerImages([]);
      setComposerFiles([]);
      setSplitComposerDrafts(initialSplitComposerDrafts());
    }
    if (isLocalDemoThread) {
      localDemoThreadsRef.current = new Map(
        [...localDemoThreadsRef.current].filter(
          ([threadID]) => threadID !== thread.id,
        ),
      );
      setArchiveConfirmThreadID(undefined);
      setState((current) => {
        const nextTabs = removeSessionTab(
          current.sessionTabs,
          threadSessionTabID(thread.id),
        );
        return {
          ...current,
          thread: current.thread?.id === thread.id ? undefined : current.thread,
          secondaryThread:
            current.secondaryThread?.id === thread.id
              ? undefined
              : current.secondaryThread,
          activePane:
            current.activePane === "secondary" &&
            current.secondaryThread?.id === thread.id
              ? "primary"
              : current.activePane,
          sessionTabs: fallbackDraft
            ? ensureSessionTab(nextTabs, fallbackDraft)
            : nextTabs,
          activeSessionTabID:
            current.activeSessionTabID === threadSessionTabID(thread.id) &&
            fallbackDraft
              ? fallbackDraft.id
              : current.activeSessionTabID,
          threads: current.threads.filter(
            (candidate) => candidate.id !== thread.id,
          ),
          running:
            activeThreadIDForState(current) === thread.id
              ? false
              : current.running,
          status: "ready",
        };
      });
      return;
    }
    try {
      const result = await window.wuu.archiveThread(thread.id, true);
      setArchiveConfirmThreadID(undefined);
      setState((current) => {
        const nextTabs = removeSessionTab(
          current.sessionTabs,
          threadSessionTabID(thread.id),
        );
        return {
          ...current,
          thread: current.thread?.id === thread.id ? undefined : current.thread,
          secondaryThread:
            current.secondaryThread?.id === thread.id
              ? undefined
              : current.secondaryThread,
          activePane:
            current.activePane === "secondary" &&
            current.secondaryThread?.id === thread.id
              ? "primary"
              : current.activePane,
          sessionTabs: fallbackDraft
            ? ensureSessionTab(nextTabs, fallbackDraft)
            : nextTabs,
          activeSessionTabID:
            current.activeSessionTabID === threadSessionTabID(thread.id) &&
            fallbackDraft
              ? fallbackDraft.id
              : current.activeSessionTabID,
          threads: current.threads.filter(
            (candidate) => candidate.id !== result.thread.id,
          ),
          running:
            activeThreadIDForState(current) === thread.id
              ? false
              : current.running,
          status: "ready",
        };
      });
    } catch (error) {
      setArchiveConfirmThreadID(undefined);
      setState((current) => ({
        ...current,
        status:
          error instanceof Error ? error.message : "archive thread failed",
      }));
    }
  }

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
    setPrompt("");
    setComposerImages([]);
    setComposerFiles([]);
    if (isStateActiveThreadRunning(currentState)) {
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

  async function queueComposerMessage(
    message: QueuedComposerMessage,
    targetThread = activeThreadForState(appStateRef.current),
  ): Promise<boolean> {
    const currentState = appStateRef.current;
    const text = message.text.trim();
    const images = inputImagesFromComposer(message.images);
    const files = inputFilesFromComposer(message.files);
    if (
      (!text && images.length === 0 && files.length === 0) ||
      !targetThread ||
      targetThread.read_only ||
      !currentState.activeContext ||
      !currentState.initialized ||
      viewSwitchPending
    ) {
      return false;
    }
    try {
      const result = await window.wuu.queueTurn(
        targetThread.id,
        text,
        images,
        message.id,
        files,
      );
      enqueueComposerMessage(targetThread.id, {
        ...message,
        id: result.queued.id || message.id,
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
    const currentState = appStateRef.current;
    const targetThread = activeThreadForState(currentState);
    const targetPane: ConversationPaneID =
      currentState.activePane === "secondary" && currentState.secondaryThread
        ? "secondary"
        : "primary";
    const text = message.text.trim();
    const images = inputImagesFromComposer(message.images);
    const files = inputFilesFromComposer(message.files);
    if (
      (!text && images.length === 0 && files.length === 0) ||
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
      detail: composerSubmissionDetail(images.length, files.length),
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
      const optimisticTurn = createOptimisticTurn(message, Date.now());
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
      const result = await window.wuu.startTurn(thread.id, text, images, files);
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
    const images = inputImagesFromComposer(message.images);
    const files = inputFilesFromComposer(message.files);
    if (
      (!text && images.length === 0 && files.length === 0) ||
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
      detail: composerSubmissionDetail(images.length, files.length),
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
      const result = await window.wuu.startTurn(targetThread.id, text, images, files);
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
    const images = inputImagesFromComposer(message.images);
    const files = inputFilesFromComposer(message.files);
    if (
      (!text && images.length === 0 && files.length === 0) ||
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
        detail: composerSubmissionDetail(images.length, files.length),
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
      const result = await window.wuu.startTurn(targetThread.id, text, images, files);
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

  async function updateRuntimeSettings(
    provider: string,
    model: string,
    effort?: string,
    connection?: RuntimeConnectionUpdate,
    variant?: string,
    permissionMode?: string,
  ): Promise<void> {
    const nextProvider = provider.trim();
    const nextModel = model.trim();
    const nextEffort = effort === undefined ? undefined : effort.trim();
    const nextVariant = variant === undefined ? undefined : variant.trim();
    const nextPermissionMode =
      permissionMode === undefined ? undefined : permissionMode.trim();
    const nextConnection =
      connection === undefined
        ? undefined
        : {
            ...(connection.base_url === undefined
              ? {}
              : { base_url: connection.base_url.trim() }),
            ...(connection.api_key === undefined
              ? {}
              : { api_key: connection.api_key.trim() }),
            ...(connection.create_provider ? { create_provider: true } : {}),
          };
    const currentProvider = state.initialized?.providers?.find(
      (item) => item.name === nextProvider,
    );
    const connectionChanged =
      Boolean(nextConnection?.create_provider) ||
      Boolean(nextConnection?.api_key) ||
      (nextConnection?.base_url !== undefined &&
        nextConnection.base_url !== (currentProvider?.base_url ?? ""));
    const currentPermissionMode =
      state.initialized?.permissions?.mode ?? "";
    const currentToolPolicy = state.initialized?.tool_policy;
    const currentToolPolicyHasOverrides = Boolean(
      currentToolPolicy?.default_action ||
        Object.keys(currentToolPolicy?.tools ?? {}).length > 0 ||
        Object.keys(currentToolPolicy?.kinds ?? {}).length > 0 ||
        Object.keys(currentToolPolicy?.risks ?? {}).length > 0,
    );
    const permissionModeChanged =
      nextPermissionMode !== undefined &&
      (nextPermissionMode !== currentPermissionMode ||
        currentToolPolicyHasOverrides);
    if (
      !nextProvider ||
      !nextModel ||
      !state.initialized ||
      (nextProvider === state.initialized.provider &&
        nextModel === state.initialized.model &&
        (nextEffort === undefined ||
          nextEffort === (state.initialized.effort ?? "")) &&
        (nextVariant === undefined ||
          nextVariant === (state.initialized.variant ?? "")) &&
        !connectionChanged &&
        !permissionModeChanged)
    ) {
      return;
    }
    try {
      const updated = await window.wuu.updateRuntimeSettings(
        nextProvider,
        nextModel,
        nextEffort,
        nextConnection,
        nextVariant,
        nextPermissionMode,
      );
      setState((current) => {
        const initialized = current.initialized
          ? {
              ...current.initialized,
              provider: updated.provider,
              model: updated.model,
              effort: updated.effort ?? "",
              variant: updated.variant ?? "",
              tool_policy: updated.tool_policy ?? current.initialized.tool_policy,
              permissions: updated.permissions ?? current.initialized.permissions,
              extension_trust: updated.extension_trust ?? current.initialized.extension_trust,
              providers: updated.providers ?? current.initialized.providers,
              advanced_settings: updated.advanced_settings ?? current.initialized.advanced_settings,
            }
          : current.initialized;
        return {
          ...current,
          initialized,
          status: current.status === "ready" ? current.status : "ready",
        };
      });
    } catch (error) {
      setState((current) => ({
        ...current,
        status:
          error instanceof Error
            ? error.message
            : "update runtime settings failed",
      }));
      throw error;
    }
  }

  async function updateAdvancedSettings(
    settings: RuntimeAdvancedSettingsUpdate,
  ): Promise<void> {
    if (!state.initialized || viewContextSwitchPending) {
      return;
    }
    try {
      const updated = await window.wuu.updateAdvancedSettings(settings);
      setState((current) => {
        const initialized = current.initialized
          ? {
              ...current.initialized,
              advanced_settings: updated.advanced_settings,
              providers: updated.providers ?? current.initialized.providers,
            }
          : current.initialized;
        return {
          ...current,
          initialized,
          status: current.status === "ready" ? current.status : "ready",
        };
      });
    } catch (error) {
      setState((current) => ({
        ...current,
        status:
          error instanceof Error
            ? error.message
            : "update advanced settings failed",
      }));
      throw error;
    }
  }

  function toggleCodexRuntimeMenu(menu: Exclude<CodexRuntimeMenu, null>): void {
    if (!state.initialized || viewContextSwitchPending) {
      return;
    }
    setRuntimeMenuOpen(false);
    setAccessMenuOpen(false);
    setBranchMenuOpen(false);
    setCodexRuntimeMenu((current) => (current === menu ? null : menu));
    if (isCodexProvider(state.initialized)) {
      void loadCodexModelsForProvider(state.initialized.provider);
    }
  }

  async function loadCodexModelsForProvider(provider: string): Promise<void> {
    if (!provider) {
      return;
    }
    if (
      codexModels.provider === provider &&
      (codexModels.loading || codexModels.models.length > 0)
    ) {
      return;
    }
    setCodexModels({ provider, loading: true, error: "", models: [] });
    try {
      const result = await window.wuu.loadCodexModels(provider);
      setCodexModels({
        provider: result.provider,
        loading: false,
        error: "",
        models: result.models,
      });
      setState((current) => {
        if (
          !current.initialized ||
          current.initialized.provider !== result.provider
        ) {
          return current;
        }
        return {
          ...current,
          initialized: {
            ...current.initialized,
            model: result.model,
            effort: result.effort ?? "",
            variant: result.variant ?? "",
          },
        };
      });
    } catch (error) {
      setCodexModels({
        provider,
        loading: false,
        error: error instanceof Error ? error.message : "无法加载 Codex 模型",
        models: [],
      });
    }
  }

  async function selectRuntimeModel(
    provider: string,
    model: string,
    variant?: string,
  ): Promise<void> {
    if (!state.initialized || viewContextSwitchPending) {
      return;
    }
    await updateRuntimeSettings(provider, model, undefined, undefined, variant);
    setCodexRuntimeMenu(null);
  }

  async function selectRuntimeEffort(nextVariant: string): Promise<void> {
    if (!state.initialized || viewContextSwitchPending) {
      return;
    }
    await updateRuntimeSettings(
      state.initialized.provider,
      state.initialized.model,
      undefined,
      undefined,
      nextVariant,
    );
    setCodexRuntimeMenu(null);
  }

  async function selectPermissionMode(
    mode: PermissionMode,
  ): Promise<void> {
    if (!state.initialized || viewContextSwitchPending) {
      return;
    }
    await updateRuntimeSettings(
      state.initialized.provider,
      state.initialized.model,
      undefined,
      undefined,
      undefined,
      mode,
    );
    setAccessMenuOpen(false);
  }

  async function interrupt(): Promise<void> {
    const thread = activeThreadForState(appStateRef.current);
    if (!thread) {
      return;
    }
    await window.wuu.interruptTurn(thread.id);
  }

  /**
   * Dispatch for UserFacingError recommended actions. The data layer
   * (UserFacingErrors.ts) only declares what an action IS — this is
   * where we decide what each one DOES. New action kinds go here; the
   * data layer does not need to know.
   *
   * Only actions with an existing, real UI path should be emitted by
   * UserFacingErrors.ts. Future actions still route through this table
   * when their corresponding UI surfaces land.
   */
  function handleNoticeAction(action: UserFacingErrorAction): void {
    switch (action.kind) {
      case "openSettings": {
        setSettingsInitialPage(settingsPageFromNoticeFocus(action.payload?.focus));
        setSettingsOpen(true);
        return;
      }
      case "copyDebug": {
        const current = appStateRef.current;
        const thread = activeThreadForState(current);
        const snapshot = JSON.stringify(
          {
            kind: "wuu-notice-debug",
            notice: action.payload ?? {},
            at: new Date().toISOString(),
            status: current.status,
            running: current.running,
            thread_id: thread?.id,
            provider: thread?.model_provider,
            model: thread?.model,
          },
          null,
          2,
        );
        void navigator.clipboard.writeText(snapshot).catch(() => {
          /* clipboard unavailable — best-effort */
        });
        return;
      }
      case "retry":
      case "switchModel":
      case "compactContext":
      case "reauth":
      case "submitFeedback":
        return;
      default:
        return;
    }
  }

  async function interruptPane(pane: ConversationPaneID): Promise<void> {
    const thread = threadForPane(appStateRef.current, pane);
    if (!thread) {
      return;
    }
    await window.wuu.interruptTurn(thread.id);
  }

  async function resolveToolApproval(
    approval: PendingToolApproval,
    decision: "approved" | "approved_for_session" | "denied",
  ): Promise<void> {
    await window.wuu.respondToServerRequest(approval.server_request_id, {
      decision,
      reason:
        decision === "denied"
          ? "user denied the requested tool call"
          : "user approved the requested tool call",
    });
    setState((current) =>
      current.pendingToolApproval?.server_request_id === approval.server_request_id
        ? {
            ...current,
            pendingToolApproval: undefined,
            status: current.running ? current.status : "ready",
          }
        : current,
    );
  }

  if (settingsOpen) {
    return (
      <SettingsView
        initialized={state.initialized}
        initialPage={settingsInitialPage}
        running={viewContextSwitchPending}
        usage={settingsUsage}
        usageRange={usageRange}
        setUsageRange={setUsageRange}
        showDebugControlsSetting={ENABLE_DEBUG_CONTROL_SETTING}
        debugControlsEnabled={debugControlsEnabled}
        sidebarWidth={sidebarWidth}
        sidebarMinWidth={SIDEBAR_MIN_WIDTH}
        sidebarMaxWidth={SIDEBAR_MAX_WIDTH}
        resizingSidebar={resizingSidebar}
        onBack={() => setSettingsOpen(false)}
        onSave={updateRuntimeSettings}
        onAdvancedSave={updateAdvancedSettings}
        onDebugControlsChange={setDebugControlsEnabled}
        onSidebarResizeStart={startSettingsSidebarResize}
        onSidebarSeparatorKey={handleSettingsSidebarSeparatorKey}
        onSidebarSeparatorDoubleClick={resetSettingsSidebarWidth}
      />
    );
  }

  return (
    <ImagePreviewProvider>
      <div className={shellClassName} style={shellStyle}>
      <AppSidebar
        state={state}
        pinnedThreads={sidebarPinnedThreads}
        scratchThreads={sidebarScratchThreads}
        onCreateScratchThread={() => void useNoProject(true)}
        activeThreadID={activeThreadID}
        pendingThreadID={visiblePendingThreadID}
        pendingProjectID={visiblePendingProjectID}
        archiveConfirmThreadID={archiveConfirmThreadID}
        collapsedProjectIDs={collapsedProjectIDs}
        collapsingProjectIDs={collapsingProjectIDs}
        projectMenuOpen={projectMenuOpen}
        projectMenuRef={projectMenuRef}
        searchOpen={conversationSearch.open}
        debugFixturesVisible={
          debugControlsVisible && ENABLE_CONVERSATION_FIXTURES
        }
        onStartNewThread={() => void startNewThread()}
        onOpenSkillsTab={openSkillsTab}
        onToggleConversationSearch={toggleConversationSearch}
        onSeedConversationFixture={seedConversationFixture}
        onSeedAgentTreeDemo={seedAgentTreeDemo}
        onSelectThread={(id) => void activateThread(id)}
        onSelectChildAgent={(agent) => void selectChildAgent(agent)}
        onTogglePinned={(thread) => void toggleThreadPinned(thread)}
        onArchiveThread={(thread) => void archiveThread(thread)}
        onClearArchiveConfirm={(id) =>
          setArchiveConfirmThreadID((current) =>
            current === id ? undefined : current,
          )
        }
        onToggleProjectMenu={() => setProjectMenuOpen((open) => !open)}
        onCreateProject={() => void createBlankProject()}
        onOpenProjectFolder={() => void chooseProjectFolder()}
        onOpenProject={(id) => void openProject(id)}
        onToggleProjectCollapsed={toggleProjectCollapsed}
        onStartNewThreadForProject={(id) => void startNewThreadForProject(id)}
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

      {state.pendingToolApproval ? (
        <ToolApprovalDialog
          approval={state.pendingToolApproval}
          onApprove={() =>
            void resolveToolApproval(state.pendingToolApproval!, "approved")
          }
          onApproveForSession={() =>
            void resolveToolApproval(state.pendingToolApproval!, "approved_for_session")
          }
          onDeny={() =>
            void resolveToolApproval(state.pendingToolApproval!, "denied")
          }
        />
      ) : null}

      <main
        className={`conversation-pane${environmentPanelVisible ? " environment-panel-visible" : ""}${
          environmentPanelReserved ? " environment-panel-reserved" : ""
        }${sessionTabsVisible ? " session-tabs-visible" : ""}${
          conversationGridVisible ? " conversation-grid-visible" : ""
        }`}
        ref={conversationPaneRef}
      >
        <header className="titlebar">
          <div className="title-block">
            <button
              className="icon-button side-panel-toggle-button sidebar-toggle-button"
              type="button"
              aria-label={sidebarCollapsed ? "展开左侧栏" : "收起左侧栏"}
              aria-pressed={!sidebarCollapsed}
              onClick={toggleSidebar}
            >
              <SidePanelToggleIcon side="left" open={!sidebarCollapsed} />
            </button>
            {renderTitleContent()}
          </div>
          <div className="title-actions">
            {debugControlsVisible && ENABLE_LAUNCH_PREVIEW ? (
              <button
                className="launch-preview-button"
                type="button"
                disabled={previewingLaunch}
                onClick={() => setLaunchPreviewPinned(true)}
              >
                <Terminal className="icon" />
                <span>启动动画</span>
              </button>
            ) : null}
            {debugControlsVisible && ENABLE_TURN_PROGRESS_EXPERIMENT ? (
              <button
                className={`launch-preview-button turn-progress-preview-button${turnProgressPreviewOpen ? " active" : ""}`}
                type="button"
                aria-pressed={turnProgressPreviewOpen}
                onClick={() => setTurnProgressPreviewOpen(true)}
              >
                <Film className="icon" />
                <span>完整预览</span>
              </button>
            ) : null}
            {debugControlsVisible && ENABLE_PLAN_PANEL_DEBUG ? (
              <button
                className="launch-preview-button plan-panel-debug-button"
                type="button"
                disabled={!state.activeContext || !state.initialized}
                onClick={seedPlanPanelDebug}
              >
                <ListChecks className="icon" />
                <span>计划面板</span>
              </button>
            ) : null}
            {debugControlsVisible ? (
              <button
                className={`launch-preview-button conversation-grid-button${conversationGridVisible ? " active" : ""}`}
                type="button"
                aria-label={conversationGridVisible ? "隐藏对话网格" : "显示对话网格"}
                aria-pressed={conversationGridVisible}
                title="按 G 切换对话网格"
                onClick={() => setConversationGridVisible((visible) => !visible)}
              >
                <Grid3X3 className="icon" />
                <span>网格</span>
              </button>
            ) : null}
            {debugControlsVisible && ENABLE_RUN_DEBUG_PANEL ? (
              <div className="run-debug-anchor" ref={runDebugRef}>
                <button
                  className={`launch-preview-button run-debug-button${runDebugOpen ? " active" : ""}`}
                  type="button"
                  aria-label={runDebugOpen ? "隐藏调试信息" : "显示调试信息"}
                  aria-expanded={runDebugOpen}
                  onClick={() => {
                    closeEnvironmentPanel();
                    setRunDebugOpen((open) => !open);
                  }}
                >
                  <Bug className="icon" />
                  <span>调试</span>
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
                    onCopy={() =>
                      void copyRunDebugInfo({
                        state,
                        queuedMessages,
                        guideMessages,
                        composerImages,
                        composerFiles,
                      })
                    }
                    onClose={() => setRunDebugOpen(false)}
                  />
                ) : null}
              </div>
            ) : null}
            {backgroundProcessCapsuleVisible ? (
              <button
                className={`background-process-capsule ${backgroundProcessCapsuleTone}${
                  environmentPanelVisible ? " active" : ""
                }`}
                type="button"
                aria-label={backgroundProcessCapsuleTitle}
                title={backgroundProcessCapsuleTitle}
                onClick={openBackgroundProcessPanel}
              >
                {backgroundProcessCapsuleTone === "failed" ? (
                  <AlertCircle className="icon-sm" />
                ) : (
                  <Terminal className="icon-sm" />
                )}
                <span>{backgroundProcessCapsuleLabel}</span>
              </button>
            ) : null}
            <button
              ref={environmentToggleRef}
              className={`icon-button environment-toggle-button${environmentPanelVisible ? " active" : ""}`}
              type="button"
              aria-label={
                environmentPanelVisible ? "隐藏环境信息" : "显示环境信息"
              }
              aria-pressed={environmentPanelVisible}
              onClick={toggleEnvironmentPanel}
            >
              <Info className="icon-lg" />
            </button>
            <button
              className="icon-button side-panel-toggle-button"
              type="button"
              aria-label={rightPanelOpen ? "关闭右侧栏" : "打开右侧栏"}
              aria-pressed={rightPanelOpen}
              onClick={toggleRightPanel}
            >
              <SidePanelToggleIcon side="right" open={rightPanelOpen} />
            </button>
          </div>
        </header>

        <ConversationTurnRail
          turns={turns}
          activeTurnID={turns[turns.length - 1]?.id}
          scrollContainerRef={conversationScrollRef}
          getScrollContainer={conversationRailScrollContainer}
          onWheelScrollAway={disableConversationAutoFollow}
          onSelectQueryHistory={handleQueryHistorySelect}
        />

        {debugControlsVisible &&
        ENABLE_TURN_PROGRESS_EXPERIMENT &&
        turnProgressPreviewOpen ? (
          <TurnProgressPreviewOverlay
            onClose={() => setTurnProgressPreviewOpen(false)}
          />
        ) : null}

        <EnvironmentSideStack
          visible={environmentPanelVisible}
          mounted={environmentPanelMounted}
          state={state}
          panelRef={environmentPanelRef}
          closing={environmentPanelClosing}
          motionState={environmentPanelMotionState}
          activeProject={activeProject}
          planUpdate={activePlanUpdate}
          backgroundProcesses={backgroundProcesses}
          stoppingProcessIDs={stoppingProcessIDs}
          activeMenu={environmentPanelMenu}
          running={anyThreadIsRunning}
          pullRequestDisabledReason={pullRequestDisabledReason}
          queryHistoryDocked={queryHistoryDocked}
          queryHistory={pastQueries}
          onSetActiveMenu={setEnvironmentPanelMenu}
          onClose={() => closeEnvironmentPanel({ dismissed: true })}
          onOpenProject={() => void chooseProjectFolder()}
          onSelectNoProject={() => void useNoProject(false)}
          onSelectBranch={(branch) => void checkoutBranch(branch)}
          onCreateBranch={(branch) => createAndCheckoutBranch(branch)}
          onOpenReview={() => {
            setWorkspacePanelView("review");
            setWorkspaceRightPanelView("review");
            setWorkspaceMode(undefined);
            setRightPanelOpenWithMotion(true);
            closeEnvironmentPanel({ dismissed: true });
          }}
          onOpenCommit={() => setEnvironmentDialog("commit")}
          onOpenPullRequest={() => setEnvironmentDialog("pull-request")}
          onStopBackgroundProcess={(process) => void stopBackgroundProcess(process)}
          onOpenBackgroundPreview={openBackgroundProcessPreview}
          onSelectQueryHistory={handleQueryHistorySelect}
          rightPanelFilePath={rightPanelFilePath}
          onCloseFilePreview={handleCloseFilePreview}
        />

        {viewContextSwitchPending ? <ViewSwitchLoading /> : null}

        {state.initialized && !previewingLaunch ? (
          <div
            className={`scroll-region${emptyConversation && !showingWorkspaceMode ? " empty-scroll-region" : ""}${
              showingWorkspaceMode ? " workspace-scroll-region" : ""
            }${splitConversation ? " split-scroll-region" : ""}${showingSkillsCatalog ? " skills-scroll-region" : ""}`}
            onScroll={(event) => handleConversationScroll(event.currentTarget)}
            ref={conversationScrollRef}
          >
            {showingSkillsCatalog ? (
              <SkillsCatalog
                activeContext={state.activeContext}
              />
            ) : workspaceMode ? (
              <WorkspaceMainPanel
                view={workspaceMode}
                activeContext={state.activeContext}
                gitStatus={state.gitStatus}
                selectedFilePath={activeWorkspaceFile}
                onOpenRightPanel={() => {
                  ensureWorkspaceToolTab(workspaceMode);
                  activateWorkspaceTool(workspaceMode);
                  setRightPanelOpenWithMotion(true);
                }}
              />
            ) : (
              <>
                {!queryHistoryDocked && !activeThreadReadOnly ? (
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
                  <div className="conversation-split">
                    {renderConversationSplitPane(state.thread, "primary")}
                    {renderConversationSplitPane(
                      state.secondaryThread,
                      "secondary",
                    )}
                  </div>
                ) : emptyConversation ? (
              <EmptyConversationHome title={emptyThreadTitle}>
                {renderComposer("hero")}
              </EmptyConversationHome>
            ) : (
              <CachedConversationPanes
                threadIDs={cachedThreadPaneIDs}
                threadsByID={cachedConversationThreadsByID}
                activeThreadID={activeThreadID}
                activeContextCwd={state.activeContext?.cwd}
                conversationGridVisible={conversationGridVisible}
                historyMessageEdit={historyMessageEdit}
                onStreamFrame={scheduleStreamScroll}
                onCollapseComplete={handleTurnCollapseComplete}
                canEditThreadMessage={canEditCachedThreadMessage}
                onForkMessage={handleCachedPaneForkMessage}
                onEditMessage={handleCachedPaneEditMessage}
                onCancelEditMessage={handleCachedPaneCancelEditMessage}
                onSubmitEditMessage={handleCachedPaneSubmitEditMessage}
                onNoticeAction={handleCachedPaneNoticeAction}
                onOpenFile={handleOpenFileInRightPanel}
              />
            )}
              </>
            )}
          </div>
        ) : (
          <RuntimeLoading
            status={state.status}
            pinned={previewingLaunch}
            onExitPreview={() => setLaunchPreviewPinned(false)}
          />
        )}

        {state.initialized &&
        !previewingLaunch &&
        !emptyConversation &&
        !showingWorkspaceMode &&
        !splitConversation &&
        !showingSkillsCatalog
          ? renderComposer("dock")
          : null}

        {state.initialized &&
        !previewingLaunch &&
        !emptyConversation &&
        !showingWorkspaceMode &&
        !splitConversation &&
        !showingSkillsCatalog &&
        userScrolledAway ? (
          <button
            type="button"
            className="jump-to-latest-pill"
            aria-label="跳到最新"
            onClick={() => scrollConversationToBottom({ force: true })}
          >
            <svg
              width="14"
              height="14"
              viewBox="0 0 14 14"
              fill="none"
              aria-hidden="true"
            >
              <path
                d="M7 1V11M7 11L3 7M7 11L11 7"
                stroke="currentColor"
                strokeWidth="1.5"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            </svg>
            <span>跳到最新</span>
          </button>
        ) : null}
      </main>

      {rightPanelOpen || rightPanelAnimating ? (
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
      <WorkspaceRightPanel
        open={rightPanelOpen}
        present={rightPanelOpen || rightPanelAnimating}
        view={workspaceRightPanelView}
        openTabs={workspaceToolTabs}
        activeContext={state.activeContext}
        gitStatus={state.gitStatus}
        selectedFilePath={activeWorkspaceFile}
        onSelectView={openWorkspaceTool}
        onShowTools={showWorkspaceToolPicker}
        onCloseTab={closeWorkspaceToolTab}
        onReorderTabs={reorderWorkspaceToolTabs}
        onOpenFile={openWorkspaceFile}
        onClose={() => setRightPanelOpenWithMotion(false)}
        pendingBrowserURL={pendingBrowserURL}
        onBrowserURLConsumed={consumePendingBrowserURL}
        onBrowserURLChange={rememberBrowserURLForActiveThread}
      />
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
      {debugControlsVisible ? <DesignTokensPanel /> : null}
      {queryHistoryOpen &&
      !queryHistoryDocked &&
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
  );
}

type CachedConversationPanesProps = {
  threadIDs: string[];
  threadsByID: ReadonlyMap<string, Thread>;
  activeThreadID?: string;
  activeContextCwd?: string;
  conversationGridVisible: boolean;
  historyMessageEdit?: HistoryMessageEditState;
  onStreamFrame: () => void;
  onCollapseComplete: () => void;
  canEditThreadMessage: (thread: Thread) => boolean;
  onForkMessage: (thread: Thread, turnID: string, itemID: string) => void;
  onEditMessage: (thread: Thread, turnID: string, item: ThreadItem) => void;
  onCancelEditMessage: () => void;
  onSubmitEditMessage: (
    thread: Thread,
    turnID: string,
    item: ThreadItem,
    text: string,
    images: InputImage[],
    files: InputFile[],
  ) => void;
  onNoticeAction: (action: UserFacingErrorAction) => void;
  /**
   * Forwarded to `<TurnView>` so file-path chips inside any message
   * can request the right-side panel to open the referenced file.
   */
  onOpenFile?: (path: string) => void;
};

const CachedConversationPanes = memo(function CachedConversationPanes({
  threadIDs,
  threadsByID,
  activeThreadID,
  activeContextCwd,
  conversationGridVisible,
  historyMessageEdit,
  onStreamFrame,
  onCollapseComplete,
  canEditThreadMessage,
  onForkMessage,
  onEditMessage,
  onCancelEditMessage,
  onSubmitEditMessage,
  onNoticeAction,
  onOpenFile,
}: CachedConversationPanesProps): JSX.Element {
  return (
    <div className="cached-conversation-panes">
      {threadIDs.map((threadID) => {
        const thread = threadsByID.get(threadID);
        if (!thread) return null;
        const isActive = threadID === activeThreadID;
        const threadTurns = thread.turns ?? [];
        const threadLatestAgentMessageID = latestAgentMessageItemID(threadTurns);
        return (
          <div
            key={threadID}
            className="cached-conversation-pane"
            data-active={isActive}
            style={isActive ? undefined : { display: "none" }}
          >
            <div className="conversation-width session-flow">
              {isActive && conversationGridVisible ? (
                <ConversationGridGuides />
              ) : null}
              {threadTurns.map((turn) => (
                <TurnView
                  key={turn.id}
                  turn={turn}
                  cwd={thread.cwd ?? activeContextCwd}
                  latestAgentMessageID={threadLatestAgentMessageID}
                  onStreamFrame={onStreamFrame}
                  onCollapseComplete={onCollapseComplete}
                  onForkMessage={(turnID, itemID) =>
                    onForkMessage(thread, turnID, itemID)
                  }
                  onEditMessage={
                    canEditThreadMessage(thread)
                      ? (turnID, item) => onEditMessage(thread, turnID, item)
                      : undefined
                  }
                  editingMessage={
                    historyMessageEdit?.threadID === thread.id
                      ? historyMessageEdit
                      : undefined
                  }
                  onCancelEditMessage={onCancelEditMessage}
                  onSubmitEditMessage={(turnID, item, text, images, files) =>
                    onSubmitEditMessage(
                      thread,
                      turnID,
                      item,
                      text,
                      images,
                      files,
                    )
                  }
                  onNoticeAction={onNoticeAction}
                  onOpenFile={onOpenFile}
                />
              ))}
            </div>
          </div>
        );
      })}
    </div>
  );
});

function settingsPageFromNoticeFocus(focus: unknown): SettingsPage {
  if (focus === "providers") {
    return "providers";
  }
  return "general";
}

function ToolApprovalDialog({
  approval,
  onApprove,
  onApproveForSession,
  onDeny,
}: {
  approval: PendingToolApproval;
  onApprove: () => void;
  onApproveForSession: () => void;
  onDeny: () => void;
}): JSX.Element {
  const preview = approval.arguments_preview?.trim();
  const capability = approval.capability?.trim() || approval.tool_name;
  const capabilityAction = approval.capability_action?.trim();
  const capabilityObject = approval.capability_object?.trim();
  const capabilityLine = [capability, capabilityAction].filter(Boolean).join(" · ");
  const rule = approval.capability_rule?.trim() || approval.permission_rule?.trim();
  return (
    <div className="modal-backdrop environment-modal-backdrop">
      <section className="environment-dialog" role="dialog" aria-modal="true" aria-label="操作审批">
        <div className="environment-dialog-header">
          <span className="environment-dialog-icon">
            <AlertCircle className="icon-lg" />
          </span>
          <div>
            <h2>审批操作</h2>
            <p>{capabilityLine || approval.tool_name}</p>
          </div>
        </div>
        <div className="environment-dialog-summary">
          <strong>{approval.risk ? `风险：${approval.risk}` : "需要确认"}</strong>
          <span>{approval.policy_reason || approval.classification_reason || "这个操作需要人工审批后才能继续。"}</span>
        </div>
        {capabilityObject ? (
          <div className="environment-dialog-summary">
            <strong>对象</strong>
            <span>{capabilityObject}</span>
          </div>
        ) : null}
        {rule ? (
          <div className="environment-dialog-summary">
            <strong>规则</strong>
            <span>{rule}</span>
          </div>
        ) : null}
        {preview ? <pre className="environment-dialog-error">{preview}</pre> : null}
        <div className="environment-dialog-footer">
          <button type="button" onClick={onDeny}>
            拒绝
          </button>
          <button type="button" onClick={onApproveForSession}>
            本会话批准
          </button>
          <button className="primary-button" type="button" onClick={onApprove}>
            批准一次
          </button>
        </div>
      </section>
    </div>
  );
}

function ConversationGridGuides(): JSX.Element {
  return (
    <div className="conversation-grid-guides" aria-hidden="true">
      <div className="conversation-grid-cols">
        {Array.from({ length: CONVERSATION_GRID_COLUMNS }, (_, index) => (
          <div className="conversation-grid-col" key={index}>
            <span>{index + 1}</span>
          </div>
        ))}
      </div>
      <div className="conversation-grid-rows" />
    </div>
  );
}
