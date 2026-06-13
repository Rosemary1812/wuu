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
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
  type RefObject,
  Fragment,
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
  ManagedProcess,
  PlanUpdate,
  ProjectListResult,
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
  inputFilesFromComposer,
  inputImagesFromComposer,
  isComposerImageFile,
  isPDFFile,
  type ComposerFile,
  type ComposerImage,
  type QueuedComposerMessage,
} from "./ComposerMessages";
import {
  Composer,
  FloatingMenuPortal,
  isInsideFloatingMenu,
  type CodexModelLoadState,
  type CodexRuntimeMenu,
  type ComposerVariant,
  type FloatingMenuOwner,
  type ToolPolicyProfile,
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
import { useConversationSearch } from "./ConversationSearchState";
import { AppSidebar } from "./AppSidebar";
import {
  EnvironmentPanel,
  backgroundProcessIsLive,
  backgroundProcessNeedsAttention,
  buildBackgroundProcessItems,
  buildEnvironmentSourceItems,
  type BackgroundProcessItem,
  type EnvironmentPanelMenu,
  type EnvironmentPanelMotionState,
} from "./EnvironmentPanel";
import {
  activeProjectID,
  activeSessionTab,
  activeThreadForState,
  activeThreadIDForState,
  activeTurnIDForThread,
  bindActiveSessionTabToThread,
  cloneSessionTabDraft,
  cloneComposerDraft,
  composerSubmissionDetail,
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
  notificationTargetsActiveThread,
  persistActiveSessionTabDraft,
  pinnedThreads,
  projectThreads,
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
import { CommitChangesDialog, PullRequestDialog } from "./GitDialogs";
import { DesignTokensPanel } from "./DesignTokensPanel";
import {
  EmptyConversationHome,
  RuntimeLoading,
  ViewSwitchLoading,
} from "./LoadingViews";
import {
  isCodexProvider,
  pullRequestUnavailableReason,
} from "./RuntimeHelpers";
import { SettingsView } from "./SettingsView";
import { SidePanelToggleIcon } from "./SidePanelToggleIcon";
import { SessionTabStrip } from "./SessionTabs";
import { SkillsCatalog } from "./SkillsCatalog";
import { StreamingMarkdown } from "./StreamingMarkdown";
import {
  RunDebugPanel,
  buildRunDebugSnapshot,
  debugStreamFieldLength,
  latestDebugItem,
  parseTurnTimestampMs,
  runDebugEventFromServerEvent,
  runDebugPhaseForState,
  type RunDebugEvent,
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
import {
  WorkspaceMainPanel,
  WorkspaceRightPanel,
  WorkspaceToolIcon,
  workspaceModeTitle,
  type WorkspacePanelView,
  type WorkspaceRightPanelView,
} from "./WorkspacePanels";
import { desktopApiErrorMessage } from "./WorkspaceReviewHelpers";

const VIEW_SWITCH_LOADING_DELAY_MS = 180;
const SIDEBAR_MOTION_MS = 280;
const RIGHT_PANEL_MOTION_MS = 280;
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


const SIDEBAR_DEFAULT_WIDTH = 326;
const SIDEBAR_MIN_WIDTH = 240;
const SIDEBAR_MAX_WIDTH = 520;
const SIDEBAR_STEP = 24;
const SIDEBAR_WIDTH_KEY = "wuu.desktop.sidebarWidth";
const SIDEBAR_COLLAPSED_KEY = "wuu.desktop.sidebarCollapsed";
const PROJECT_COLLAPSED_IDS_KEY = "wuu.desktop.collapsedProjectIDs";
const WORKSPACE_RIGHT_PANEL_DEFAULT_WIDTH = 360;
const WORKSPACE_RIGHT_PANEL_MIN_WIDTH = 300;
const WORKSPACE_RIGHT_PANEL_MAX_WIDTH = 860;
const WORKSPACE_RIGHT_PANEL_MAIN_MIN_WIDTH = 360;
const WORKSPACE_RIGHT_PANEL_STEP = 32;
const WORKSPACE_RIGHT_PANEL_WIDTH_KEY = "wuu.desktop.workspaceRightPanelWidth";
const DEBUG_CONTROLS_KEY = "wuu.desktop.debugControlsEnabled";
const CONVERSATION_AUTO_SCROLL_THRESHOLD_PX = 48;
const CONVERSATION_SCROLLBAR_HIDE_DELAY_MS = 700;
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

type SidebarResizeSession = {
  startX: number;
  startWidth: number;
  allowCollapse: boolean;
};

type RightPanelResizeSession = {
  startX: number;
  startWidth: number;
};

function initialSidebarWidth(): number {
  const stored = Number(window.localStorage.getItem(SIDEBAR_WIDTH_KEY));
  if (!Number.isFinite(stored)) {
    return SIDEBAR_DEFAULT_WIDTH;
  }
  return clamp(stored, SIDEBAR_MIN_WIDTH, SIDEBAR_MAX_WIDTH);
}

function initialSidebarCollapsed(): boolean {
  return window.localStorage.getItem(SIDEBAR_COLLAPSED_KEY) === "true";
}

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

function initialWorkspaceRightPanelWidth(): number {
  const stored = Number(
    window.localStorage.getItem(WORKSPACE_RIGHT_PANEL_WIDTH_KEY),
  );
  if (!Number.isFinite(stored)) {
    return WORKSPACE_RIGHT_PANEL_DEFAULT_WIDTH;
  }
  return clamp(
    stored,
    WORKSPACE_RIGHT_PANEL_MIN_WIDTH,
    WORKSPACE_RIGHT_PANEL_MAX_WIDTH,
  );
}

function initialDebugControlsEnabled(): boolean {
  if (!ENABLE_DEBUG_CONTROLS) {
    return false;
  }
  if (RENDERER_ENV?.VITE_ENABLE_RUN_DEBUG_PANEL === "true") {
    return true;
  }
  return window.localStorage.getItem(DEBUG_CONTROLS_KEY) === "true";
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

function clampWorkspaceRightPanelWidth(
  width: number,
  sidebarWidth: number,
): number {
  const maxForWindow =
    typeof window === "undefined"
      ? WORKSPACE_RIGHT_PANEL_MAX_WIDTH
      : window.innerWidth - sidebarWidth - WORKSPACE_RIGHT_PANEL_MAIN_MIN_WIDTH;
  const maxWidth = Math.max(
    WORKSPACE_RIGHT_PANEL_MIN_WIDTH,
    Math.min(WORKSPACE_RIGHT_PANEL_MAX_WIDTH, maxForWindow),
  );
  return clamp(width, WORKSPACE_RIGHT_PANEL_MIN_WIDTH, maxWidth);
}

export function App(): JSX.Element {
  const [state, setState] = useState<AppState>(initialState);
  const [prompt, setPrompt] = useState("");
  const [composerImages, setComposerImages] = useState<ComposerImage[]>([]);
  const [composerFiles, setComposerFiles] = useState<ComposerFile[]>([]);
  const [splitComposerDrafts, setSplitComposerDrafts] = useState<
    Record<ConversationPaneID, ComposerDraftState>
  >(initialSplitComposerDrafts);
  const [queuedMessages, setQueuedMessages] = useState<QueuedComposerMessage[]>(
    [],
  );
  const [guideMessages, setGuideMessages] = useState<QueuedComposerMessage[]>(
    [],
  );
  const [sidebarWidth, setSidebarWidth] = useState(initialSidebarWidth);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(
    initialSidebarCollapsed,
  );
  const [collapsedProjectIDs, setCollapsedProjectIDs] = useState<Set<string>>(
    initialCollapsedProjectIDs,
  );
  const [resizingSidebar, setResizingSidebar] = useState(false);
  const [sidebarAnimating, setSidebarAnimating] = useState(false);
  const [workspaceRightPanelWidth, setWorkspaceRightPanelWidth] = useState(
    initialWorkspaceRightPanelWidth,
  );
  const [resizingRightPanel, setResizingRightPanel] = useState(false);
  const [projectMenuOpen, setProjectMenuOpen] = useState(false);
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
  const [modeMenuOpen, setModeMenuOpen] = useState(false);
  const [branchMenuOpen, setBranchMenuOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [projectFilter, setProjectFilter] = useState("");
  const [launchPreviewPinned, setLaunchPreviewPinned] = useState(false);
  const [turnProgressPreviewOpen, setTurnProgressPreviewOpen] = useState(false);
  const [rightPanelOpen, setRightPanelOpen] = useState(false);
  const [rightPanelAnimating, setRightPanelAnimating] = useState(false);
  const [workspaceToolTabs, setWorkspaceToolTabs] = useState<
    WorkspacePanelView[]
  >([]);
  const [workspacePanelView, setWorkspacePanelView] =
    useState<WorkspacePanelView>("files");
  const [workspaceRightPanelView, setWorkspaceRightPanelView] =
    useState<WorkspaceRightPanelView>("tools");
  const [workspaceMode, setWorkspaceMode] = useState<
    WorkspacePanelView | undefined
  >(undefined);
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
  const [environmentDialog, setEnvironmentDialog] =
    useState<EnvironmentDialog>(null);
  const [managedProcesses, setManagedProcesses] = useState<ManagedProcess[]>(
    [],
  );
  const [stoppingProcessIDs, setStoppingProcessIDs] = useState<Set<string>>(
    () => new Set(),
  );
  const [runDebugOpen, setRunDebugOpen] = useState(false);
  const [conversationGridVisible, setConversationGridVisible] = useState(false);
  const [runDebugEvents, setRunDebugEvents] = useState<RunDebugEvent[]>([]);
  const [runDebugCopied, setRunDebugCopied] = useState(false);
  const [archiveConfirmThreadID, setArchiveConfirmThreadID] = useState<
    string | undefined
  >(undefined);
  const [pendingViewSwitch, setPendingViewSwitch] = useState<
    PendingViewSwitch | undefined
  >(undefined);
  const [debugControlsEnabled, setDebugControlsEnabled] = useState(
    initialDebugControlsEnabled,
  );
  const conversationScrollRef = useRef<HTMLDivElement | null>(null);
  const splitPaneRefs = useRef<Record<ConversationPaneID, HTMLElement | null>>({
    primary: null,
    secondary: null,
  });
  const conversationPaneRef = useRef<HTMLElement | null>(null);
  const queryHistoryRailRef = useRef<HTMLDivElement | null>(null);
  const [dockComposerNode, setDockComposerNode] = useState<HTMLElement | null>(
    null,
  );
  const dockComposerRef = useCallback((node: HTMLElement | null) => {
    setDockComposerNode(node);
  }, []);
  const dockComposerHeightRef = useRef(0);
  const conversationAutoFollowRef = useRef(true);
  const streamScrollFrameRef = useRef<number | undefined>(undefined);
  const projectCollapseTimersRef = useRef(new Map<string, number>());
  const sidebarMotionTimerRef = useRef<number | undefined>(undefined);
  const rightPanelMotionTimerRef = useRef<number | undefined>(undefined);
  const conversationScrollbarHideTimerRef = useRef<number | undefined>(
    undefined,
  );
  const [queryHistoryOpen, setQueryHistoryOpen] = useState(false);
  const queryHistoryCloseTimerRef = useRef<number | undefined>(undefined);
  const windowResizingRef = useRef(false);
  const environmentPanelHasRoomRef = useRef(environmentPanelHasRoom);
  const pendingEnvironmentPanelHasRoomRef = useRef<boolean | undefined>(
    undefined,
  );
  const resizeSessionRef = useRef<SidebarResizeSession | null>(null);
  const rightPanelResizeSessionRef = useRef<RightPanelResizeSession | null>(
    null,
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
  const runDebugRef = useRef<HTMLDivElement>(null);
  const appStateRef = useRef<AppState>(initialState);
  const queuedMessagesRef = useRef<QueuedComposerMessage[]>([]);
  const guideMessagesRef = useRef<QueuedComposerMessage[]>([]);
  const localDemoThreadsRef = useRef(new Map<string, Thread>());
  const viewSwitchRequestRef = useRef(0);
  const viewSwitchDelayTimerRef = useRef<number | undefined>(undefined);
  const runDebugEventIDRef = useRef(0);
  const runDebugDeltaSeenRef = useRef(new Set<string>());
  const draftSessionTabCounterRef = useRef(0);
  const effectiveSidebarWidth = sidebarCollapsed ? 0 : sidebarWidth;
  const clampedWorkspaceRightPanelWidth = clampWorkspaceRightPanelWidth(
    workspaceRightPanelWidth,
    effectiveSidebarWidth,
  );
  const debugControlsVisible = ENABLE_DEBUG_CONTROLS && debugControlsEnabled;
  const currentSessionTab = activeSessionTab(state);
  const activeWorkspaceFile =
    currentSessionTab?.kind === "file" &&
    sameRuntimeContext(currentSessionTab.context, state.activeContext)
      ? currentSessionTab.path
      : undefined;
  const activeThread = activeThreadForState(state);
  const activeThreadID = activeThread?.id;
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
      setModeMenuOpen(false);
      setBranchMenuOpen(false);
      setCodexRuntimeMenu(null);
    },
    onSelectThread: (threadID) => void selectThread(threadID),
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
      if (sidebarMotionTimerRef.current !== undefined) {
        window.clearTimeout(sidebarMotionTimerRef.current);
      }
      if (rightPanelMotionTimerRef.current !== undefined) {
        window.clearTimeout(rightPanelMotionTimerRef.current);
      }
      if (conversationScrollbarHideTimerRef.current !== undefined) {
        window.clearTimeout(conversationScrollbarHideTimerRef.current);
      }
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
      if (streamScrollFrameRef.current !== undefined) {
        window.cancelAnimationFrame(streamScrollFrameRef.current);
        streamScrollFrameRef.current = undefined;
      }
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
        (runtimeMenuOpen || modeMenuOpen || branchMenuOpen) &&
        !runtimeMenuRef.current?.contains(target) &&
        !isInsideFloatingMenu(target, "composer-runtime")
      ) {
        setRuntimeMenuOpen(false);
        setModeMenuOpen(false);
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
          setEnvironmentPanelOpen(false);
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
    modeMenuOpen,
    projectMenuOpen,
    runDebugOpen,
    runtimeMenuOpen,
  ]);

  useLayoutEffect(() => {
    conversationAutoFollowRef.current = true;
    scrollConversationToBottom({ force: true });
  }, [activeThreadID]);

  useEffect(() => {
    scheduleStreamScroll();
  }, [state.thread?.turns, state.secondaryThread?.turns]);

  useEffect(() => {
    if (ENABLE_DEBUG_CONTROLS) {
      window.localStorage.setItem(
        DEBUG_CONTROLS_KEY,
        String(debugControlsEnabled),
      );
    }
  }, [debugControlsEnabled]);

  useEffect(() => {
    if (debugControlsVisible) {
      return;
    }
    setConversationGridVisible(false);
    setLaunchPreviewPinned(false);
    setRunDebugOpen(false);
    setTurnProgressPreviewOpen(false);
  }, [debugControlsVisible]);

  useEffect(() => {
    if (!debugControlsVisible) {
      return;
    }

    const handleKeyDown = (event: KeyboardEvent): void => {
      if (
        event.key.toLowerCase() !== "g" ||
        event.metaKey ||
        event.ctrlKey ||
        event.altKey
      ) {
        return;
      }
      const target = event.target;
      if (
        target instanceof HTMLElement &&
        (target.isContentEditable ||
          target.tagName === "INPUT" ||
          target.tagName === "TEXTAREA" ||
          target.tagName === "SELECT")
      ) {
        return;
      }
      event.preventDefault();
      setConversationGridVisible((visible) => !visible);
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [debugControlsVisible]);

  useEffect(() => {
    window.localStorage.setItem(SIDEBAR_WIDTH_KEY, String(sidebarWidth));
    window.localStorage.setItem(
      SIDEBAR_COLLAPSED_KEY,
      String(sidebarCollapsed),
    );
  }, [sidebarWidth, sidebarCollapsed]);

  useEffect(() => {
    window.localStorage.setItem(
      PROJECT_COLLAPSED_IDS_KEY,
      JSON.stringify([...collapsedProjectIDs]),
    );
  }, [collapsedProjectIDs]);

  useEffect(() => {
    window.localStorage.setItem(
      WORKSPACE_RIGHT_PANEL_WIDTH_KEY,
      String(workspaceRightPanelWidth),
    );
  }, [workspaceRightPanelWidth]);

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

  useEffect(() => {
    if (!resizingSidebar) {
      return;
    }

    function handlePointerMove(event: PointerEvent): void {
      const session = resizeSessionRef.current;
      if (!session) {
        return;
      }
      const nextWidth = session.startWidth + event.clientX - session.startX;
      if (session.allowCollapse) {
        applySidebarWidth(nextWidth);
        return;
      }
      applySettingsSidebarWidth(nextWidth);
    }

    function handlePointerUp(): void {
      resizeSessionRef.current = null;
      setResizingSidebar(false);
    }

    window.addEventListener("pointermove", handlePointerMove);
    window.addEventListener("pointerup", handlePointerUp);
    window.addEventListener("pointercancel", handlePointerUp);
    return () => {
      window.removeEventListener("pointermove", handlePointerMove);
      window.removeEventListener("pointerup", handlePointerUp);
      window.removeEventListener("pointercancel", handlePointerUp);
    };
  }, [resizingSidebar]);

  useEffect(() => {
    if (!resizingRightPanel) {
      return;
    }

    function handlePointerMove(event: PointerEvent): void {
      const session = rightPanelResizeSessionRef.current;
      if (!session) {
        return;
      }
      setWorkspaceRightPanelWidth(
        clampWorkspaceRightPanelWidth(
          session.startWidth - (event.clientX - session.startX),
          effectiveSidebarWidth,
        ),
      );
    }

    function handlePointerUp(): void {
      rightPanelResizeSessionRef.current = null;
      setResizingRightPanel(false);
    }

    window.addEventListener("pointermove", handlePointerMove);
    window.addEventListener("pointerup", handlePointerUp);
    window.addEventListener("pointercancel", handlePointerUp);
    return () => {
      window.removeEventListener("pointermove", handlePointerMove);
      window.removeEventListener("pointerup", handlePointerUp);
      window.removeEventListener("pointercancel", handlePointerUp);
    };
  }, [effectiveSidebarWidth, resizingRightPanel]);

  useEffect(() => {
    function handleResize(): void {
      setWorkspaceRightPanelWidth((current) =>
        clampWorkspaceRightPanelWidth(current, effectiveSidebarWidth),
      );
    }

    window.addEventListener("resize", handleResize);
    return () => window.removeEventListener("resize", handleResize);
  }, [effectiveSidebarWidth]);

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
  const sidebarPinnedThreads = pinnedThreads(state.threads);
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
  const anyThreadIsRunning = isAnyThreadRunning(state) || viewSwitchPending;
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
  const environmentSourceItems = useMemo(
    () =>
      buildEnvironmentSourceItems({
        activeContext: state.activeContext,
        activeProject,
        selectedWorkspaceFile: activeWorkspaceFile,
        composerFiles,
        composerImages,
        queuedMessages,
        guideMessages,
      }),
    [
      activeProject,
      activeWorkspaceFile,
      composerFiles,
      composerImages,
      guideMessages,
      queuedMessages,
      state.activeContext,
    ],
  );
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

  useEffect(() => {
    if (queryHistoryDocked) {
      setQueryHistoryOpen(false);
    }
  }, [queryHistoryDocked]);

  useLayoutEffect(() => {
    const node = dockComposerNode;
    const pane = conversationPaneRef.current;
    const applyHeight = (nextHeight: number): void => {
      const nextValue = `${nextHeight}px`;
      if (
        dockComposerHeightRef.current === nextHeight &&
        pane?.style.getPropertyValue("--dock-composer-height") === nextValue
      ) {
        return;
      }
      dockComposerHeightRef.current = nextHeight;
      pane?.style.setProperty("--dock-composer-height", nextValue);
      if (nextHeight > 0 && conversationAutoFollowRef.current) {
        scrollConversationToBottom();
      }
    };

    if (!node) {
      applyHeight(0);
      return;
    }

    const updateHeight = (): void => {
      const nextHeight = Math.ceil(node.getBoundingClientRect().height);
      applyHeight(nextHeight);
    };

    updateHeight();
    const resizeObserver = new ResizeObserver(updateHeight);
    resizeObserver.observe(node);
    return () => resizeObserver.disconnect();
  }, [
    dockComposerNode,
    emptyConversation,
    previewingLaunch,
    showingWorkspaceMode,
    state.initialized,
  ]);

  function applySidebarWidth(nextWidth: number): void {
    if (nextWidth <= SIDEBAR_MIN_WIDTH) {
      if (!sidebarCollapsed && !resizingSidebar) {
        startSidebarMotion();
      }
      setProjectMenuOpen(false);
      setSidebarCollapsed(true);
      return;
    }
    if (sidebarCollapsed && !resizingSidebar) {
      startSidebarMotion();
    }
    setSidebarCollapsed(false);
    setSidebarWidth(clamp(nextWidth, SIDEBAR_MIN_WIDTH, SIDEBAR_MAX_WIDTH));
  }

  function applySettingsSidebarWidth(nextWidth: number): void {
    setSidebarWidth(clamp(nextWidth, SIDEBAR_MIN_WIDTH, SIDEBAR_MAX_WIDTH));
  }

  function startSidebarMotion(): void {
    if (sidebarMotionTimerRef.current !== undefined) {
      window.clearTimeout(sidebarMotionTimerRef.current);
    }
    setSidebarAnimating(true);
    sidebarMotionTimerRef.current = window.setTimeout(() => {
      sidebarMotionTimerRef.current = undefined;
      setSidebarAnimating(false);
    }, SIDEBAR_MOTION_MS);
  }

  function startRightPanelMotion(): void {
    if (rightPanelMotionTimerRef.current !== undefined) {
      window.clearTimeout(rightPanelMotionTimerRef.current);
    }
    setRightPanelAnimating(true);
    rightPanelMotionTimerRef.current = window.setTimeout(() => {
      rightPanelMotionTimerRef.current = undefined;
      setRightPanelAnimating(false);
    }, RIGHT_PANEL_MOTION_MS);
  }

  function setRightPanelOpenWithMotion(open: boolean): void {
    if (rightPanelOpen !== open) {
      startRightPanelMotion();
    }
    setRightPanelOpen(open);
  }

  function applyWorkspaceRightPanelWidth(nextWidth: number): void {
    setWorkspaceRightPanelWidth(
      clampWorkspaceRightPanelWidth(nextWidth, effectiveSidebarWidth),
    );
  }

  function scheduleStreamScroll(): void {
    if (!conversationAutoFollowRef.current) {
      return;
    }
    if (streamScrollFrameRef.current !== undefined) {
      return;
    }
    streamScrollFrameRef.current = window.requestAnimationFrame(() => {
      streamScrollFrameRef.current = undefined;
      scrollConversationToBottom();
    });
  }

  function handleConversationScroll(scrolledNode?: HTMLElement): void {
    const node = scrolledNode ?? conversationViewport();
    if (!node) {
      return;
    }
    showConversationScrollbar(node);
    conversationAutoFollowRef.current = isConversationNearBottom(node);
  }

  function scrollConversationToBottom(options: { force?: boolean } = {}): void {
    const node = conversationViewport();
    if (!node || (!options.force && !conversationAutoFollowRef.current)) {
      return;
    }
    node.scrollTop = node.scrollHeight;
    showConversationScrollbar(node);
    conversationAutoFollowRef.current = true;
  }

  function showConversationScrollbar(node: HTMLElement): void {
    if (
      node.classList.contains("empty-scroll-region") ||
      node.classList.contains("workspace-scroll-region") ||
      node.scrollHeight <= node.clientHeight
    ) {
      return;
    }
    node.classList.add("scrollbar-visible");
    if (conversationScrollbarHideTimerRef.current !== undefined) {
      window.clearTimeout(conversationScrollbarHideTimerRef.current);
    }
    conversationScrollbarHideTimerRef.current = window.setTimeout(() => {
      conversationScrollbarHideTimerRef.current = undefined;
      node.classList.remove("scrollbar-visible");
    }, CONVERSATION_SCROLLBAR_HIDE_DELAY_MS);
  }

  function conversationViewport(): HTMLElement | undefined {
    if (splitConversation) {
      return splitPaneRefs.current[state.activePane] ?? undefined;
    }
    return conversationScrollRef.current ?? undefined;
  }

  function isConversationNearBottom(node: HTMLElement): boolean {
    const distanceFromBottom = Math.max(
      0,
      node.scrollHeight - node.scrollTop - node.clientHeight,
    );
    return distanceFromBottom <= CONVERSATION_AUTO_SCROLL_THRESHOLD_PX;
  }

  function startSidebarResize(event: ReactPointerEvent<HTMLDivElement>): void {
    if (event.button !== 0) {
      return;
    }
    event.preventDefault();
    resizeSessionRef.current = {
      startX: event.clientX,
      startWidth: sidebarCollapsed ? 0 : sidebarWidth,
      allowCollapse: true,
    };
    setProjectMenuOpen(false);
    setResizingSidebar(true);
  }

  function startSettingsSidebarResize(
    event: ReactPointerEvent<HTMLDivElement>,
  ): void {
    if (event.button !== 0) {
      return;
    }
    event.preventDefault();
    resizeSessionRef.current = {
      startX: event.clientX,
      startWidth: sidebarWidth,
      allowCollapse: false,
    };
    setProjectMenuOpen(false);
    setResizingSidebar(true);
  }

  function startRightPanelResize(
    event: ReactPointerEvent<HTMLDivElement>,
  ): void {
    if (event.button !== 0 || !rightPanelOpen) {
      return;
    }
    event.preventDefault();
    rightPanelResizeSessionRef.current = {
      startX: event.clientX,
      startWidth: clampedWorkspaceRightPanelWidth,
    };
    setResizingRightPanel(true);
  }

  function handleRightPanelSeparatorKey(
    event: ReactKeyboardEvent<HTMLDivElement>,
  ): void {
    if (event.key === "ArrowLeft") {
      event.preventDefault();
      applyWorkspaceRightPanelWidth(
        workspaceRightPanelWidth + WORKSPACE_RIGHT_PANEL_STEP,
      );
    } else if (event.key === "ArrowRight") {
      event.preventDefault();
      applyWorkspaceRightPanelWidth(
        workspaceRightPanelWidth - WORKSPACE_RIGHT_PANEL_STEP,
      );
    } else if (event.key === "Home") {
      event.preventDefault();
      applyWorkspaceRightPanelWidth(WORKSPACE_RIGHT_PANEL_MAX_WIDTH);
    } else if (event.key === "End") {
      event.preventDefault();
      applyWorkspaceRightPanelWidth(WORKSPACE_RIGHT_PANEL_MIN_WIDTH);
    }
  }

  function resetWorkspaceRightPanelWidth(): void {
    applyWorkspaceRightPanelWidth(WORKSPACE_RIGHT_PANEL_DEFAULT_WIDTH);
  }

  function toggleSidebar(): void {
    setProjectMenuOpen(false);
    startSidebarMotion();
    setSidebarCollapsed((collapsed) => !collapsed);
    setSidebarWidth((width) =>
      width <= SIDEBAR_MIN_WIDTH ? SIDEBAR_DEFAULT_WIDTH : width,
    );
  }

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

  function ensureWorkspaceToolTab(view: WorkspacePanelView): void {
    setWorkspaceToolTabs((current) =>
      current.includes(view) ? current : [...current, view],
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
    clearPendingComposerMessages();
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

  function useSkillFromCatalog(name: string): void {
    if (!state.activeContext) {
      return;
    }
    draftSessionTabCounterRef.current += 1;
    const draft = {
      prompt: `/${name} `,
      images: [],
      files: [],
    };
    const tab = createDraftSessionTab(
      `draft:skill:${Date.now()}:${draftSessionTabCounterRef.current}`,
      state.activeContext,
      draft,
    );
    setArchiveConfirmThreadID(undefined);
    setWorkspaceMode(undefined);
    clearPendingComposerMessages();
    restorePrimaryComposerDraft(draft);
    setSplitComposerDrafts(initialSplitComposerDrafts());
    setState((current) => ({
      ...persistActiveSessionTabDraft(current, currentPrimaryComposerDraft()),
      thread: undefined,
      secondaryThread: undefined,
      activePane: "primary",
      sessionTabs: ensureSessionTab(current.sessionTabs, tab),
      activeSessionTabID: tab.id,
      allowThreadAutoActivation: false,
      running: false,
      status: "ready",
    }));
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
      nextTabs[
        Math.min(Math.max(closedIndex, 0), Math.max(nextTabs.length - 1, 0))
      ];
    if (fallback) {
      activateWorkspaceTool(fallback);
      return;
    }
    setWorkspaceRightPanelView("tools");
  }

  function reorderWorkspaceToolTabs(
    activeView: WorkspacePanelView,
    overView: WorkspacePanelView,
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

  function setQueuedMessagesNow(messages: QueuedComposerMessage[]): void {
    queuedMessagesRef.current = messages;
    setQueuedMessages(messages);
  }

  function setGuideMessagesNow(messages: QueuedComposerMessage[]): void {
    guideMessagesRef.current = messages;
    setGuideMessages(messages);
  }

  function clearPendingComposerMessages(): void {
    setQueuedMessagesNow([]);
    setGuideMessagesNow([]);
  }

  function removePendingComposerMessageByID(id: string): void {
    if (!id) {
      return;
    }
    const nextQueued = queuedMessagesRef.current.filter(
      (message) => message.id !== id,
    );
    const nextGuides = guideMessagesRef.current.filter(
      (message) => message.id !== id,
    );
    if (nextQueued.length !== queuedMessagesRef.current.length) {
      setQueuedMessagesNow(nextQueued);
    }
    if (nextGuides.length !== guideMessagesRef.current.length) {
      setGuideMessagesNow(nextGuides);
    }
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
    if (event.message.method === "turn/started") {
      const queueID = stringValue(params, "queue_id");
      if (queueID) {
        removePendingComposerMessageByID(queueID);
      }
      return;
    }
    if (event.message.method === "turn/dequeued") {
      const queueID = stringValue(params, "queue_id");
      if (queueID) {
        removePendingComposerMessageByID(queueID);
      }
      return;
    }
    if (event.message.method === "item/completed") {
      const item = threadItemFromRecord(recordValue(params, "item"));
      if (item?.type === "user_message" && item.source_id) {
        removePendingComposerMessageByID(item.source_id);
      }
    }
  }

  function enqueueComposerMessage(message: QueuedComposerMessage): void {
    const next = [...queuedMessagesRef.current, message];
    setQueuedMessagesNow(next);
    setState((current) => ({
      ...current,
      status: `已排队 ${next.length} 条`,
    }));
  }

  async function removeQueuedMessage(id: string): Promise<void> {
    setQueuedMessagesNow(
      queuedMessagesRef.current.filter((message) => message.id !== id),
    );
    const targetThread = activeThreadForState(appStateRef.current);
    if (!targetThread) {
      return;
    }
    try {
      await window.wuu.dequeueTurn(targetThread.id, id);
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "取消排队失败",
      }));
    }
  }

  async function removeGuideMessage(id: string): Promise<void> {
    setGuideMessagesNow(
      guideMessagesRef.current.filter((message) => message.id !== id),
    );
    const targetThread = activeThreadForState(appStateRef.current);
    if (!targetThread) {
      return;
    }
    try {
      await window.wuu.unsteerTurn(targetThread.id, id);
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "取消引导失败",
      }));
    }
  }

  async function guideQueuedMessage(id: string): Promise<void> {
    const queuedIndex = queuedMessagesRef.current.findIndex(
      (message) => message.id === id,
    );
    if (queuedIndex < 0) {
      return;
    }
    const message = queuedMessagesRef.current[queuedIndex];
    const remainingQueued = [
      ...queuedMessagesRef.current.slice(0, queuedIndex),
      ...queuedMessagesRef.current.slice(queuedIndex + 1),
    ];
    const currentState = appStateRef.current;
    if (!isStateActiveThreadRunning(currentState)) {
      setQueuedMessagesNow(remainingQueued);
      void sendComposerMessage(message);
      return;
    }
    const targetThread = activeThreadForState(currentState);
    if (!targetThread) {
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
      const files = inputFilesFromComposer(message.files);
      await window.wuu.steerTurn(
        targetThread.id,
        turnID,
        message.text.trim(),
        inputImagesFromComposer(message.images),
        message.id,
        files,
      );
      setQueuedMessagesNow(remainingQueued);
      setGuideMessagesNow([...guideMessagesRef.current, message]);
      setState((current) => ({
        ...current,
        status: "已引导当前任务",
      }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "引导失败",
      }));
    }
  }

  function handleSidebarSeparatorKey(
    event: ReactKeyboardEvent<HTMLDivElement>,
  ): void {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      toggleSidebar();
      return;
    }
    if (event.key === "ArrowLeft") {
      event.preventDefault();
      if (sidebarCollapsed) {
        return;
      }
      applySidebarWidth(sidebarWidth - SIDEBAR_STEP);
      return;
    }
    if (event.key === "ArrowRight") {
      event.preventDefault();
      if (sidebarCollapsed) {
        startSidebarMotion();
        setSidebarCollapsed(false);
        setSidebarWidth(SIDEBAR_DEFAULT_WIDTH);
        return;
      }
      applySidebarWidth(sidebarWidth + SIDEBAR_STEP);
    }
  }

  function handleSettingsSidebarSeparatorKey(
    event: ReactKeyboardEvent<HTMLDivElement>,
  ): void {
    if (event.key === "ArrowLeft") {
      event.preventDefault();
      applySettingsSidebarWidth(sidebarWidth - SIDEBAR_STEP);
      return;
    }
    if (event.key === "ArrowRight") {
      event.preventDefault();
      applySettingsSidebarWidth(sidebarWidth + SIDEBAR_STEP);
      return;
    }
    if (event.key === "Home") {
      event.preventDefault();
      applySettingsSidebarWidth(SIDEBAR_MIN_WIDTH);
      return;
    }
    if (event.key === "End") {
      event.preventDefault();
      applySettingsSidebarWidth(SIDEBAR_MAX_WIDTH);
    }
  }

  function resetSettingsSidebarWidth(): void {
    applySettingsSidebarWidth(SIDEBAR_DEFAULT_WIDTH);
  }

  function renderComposer(variant: ComposerVariant): JSX.Element {
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
          (!activeThreadReadOnly && activeThreadIsRunning) || viewSwitchPending
        }
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
        modeMenuOpen={modeMenuOpen}
        branchMenuOpen={branchMenuOpen}
        menuRef={runtimeMenuRef}
        accessMenuRef={accessMenuRef}
        projectFilter={projectFilter}
        setProjectFilter={setProjectFilter}
        onToggleMenu={() => {
          setAccessMenuOpen(false);
          setModeMenuOpen(false);
          setBranchMenuOpen(false);
          setCodexRuntimeMenu(null);
          setRuntimeMenuOpen((open) => !open);
        }}
        onToggleAccessMenu={() => {
          setRuntimeMenuOpen(false);
          setModeMenuOpen(false);
          setBranchMenuOpen(false);
          setCodexRuntimeMenu(null);
          setAccessMenuOpen((open) => !open);
        }}
        onToggleModeMenu={() => {
          setRuntimeMenuOpen(false);
          setAccessMenuOpen(false);
          setBranchMenuOpen(false);
          setCodexRuntimeMenu(null);
          setModeMenuOpen((open) => !open);
        }}
        onToggleBranchMenu={() => {
          setRuntimeMenuOpen(false);
          setAccessMenuOpen(false);
          setModeMenuOpen(false);
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
        onSelectToolPolicyProfile={(profile) =>
          void selectToolPolicyProfile(profile)
        }
        onOpenSettings={() => {
          closeProjectMenus();
          setSettingsOpen(true);
        }}
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
        onClearQueuedMessages={clearPendingComposerMessages}
        onSend={() => void sendPrompt()}
        onInterrupt={() => void interrupt()}
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
        viewSwitchPending={viewSwitchPending}
        queryHistory={queryTextsForThread(thread)}
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
        canStartNewThread={Boolean(state.activeContext)}
        onSelect={(tabID) => void selectSessionTab(tabID)}
        onClose={(tabID) => void closeSessionTab(tabID)}
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
            <WorkspaceToolIcon view={workspaceMode} size={18} />
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
    setModeMenuOpen(false);
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
        clearPendingComposerMessages();
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
      clearPendingComposerMessages();
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
        clearPendingComposerMessages();
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
      clearPendingComposerMessages();
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
      clearPendingComposerMessages();
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
      clearPendingComposerMessages();
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
      clearPendingComposerMessages();
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
      clearPendingComposerMessages();
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
      clearPendingComposerMessages();
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
    setEnvironmentPanelOpen(false);
    setEnvironmentPanelDismissed(true);
    setEnvironmentPanelMenu(null);
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
    setEnvironmentPanelOpen(!visible);
    setEnvironmentPanelDismissed(visible);
    if (visible) {
      setEnvironmentPanelMenu(null);
    } else {
      setRuntimeMenuOpen(false);
      setAccessMenuOpen(false);
      setModeMenuOpen(false);
      setBranchMenuOpen(false);
      setCodexRuntimeMenu(null);
    }
  }

  function openBackgroundProcessPanel(): void {
    setEnvironmentPanelOpen(true);
    setEnvironmentPanelDismissed(false);
    setEnvironmentPanelMenu(null);
    setRunDebugOpen(false);
    setRuntimeMenuOpen(false);
    setAccessMenuOpen(false);
    setModeMenuOpen(false);
    setBranchMenuOpen(false);
    setCodexRuntimeMenu(null);
  }

  function appendRunDebugEvent(entry: Omit<RunDebugEvent, "id" | "at">): void {
    const next: RunDebugEvent = {
      ...entry,
      id: ++runDebugEventIDRef.current,
      at: Date.now(),
    };
    setRunDebugEvents((current) => [...current, next].slice(-80));
  }

  function resetRunDebugEvents(entry: Omit<RunDebugEvent, "id" | "at">): void {
    runDebugDeltaSeenRef.current.clear();
    const next: RunDebugEvent = {
      ...entry,
      id: ++runDebugEventIDRef.current,
      at: Date.now(),
    };
    setRunDebugEvents([next]);
  }

  function recordRunDebugEvent(event: ServerEvent): void {
    const entry = runDebugEventFromServerEvent(
      event,
      runDebugDeltaSeenRef.current,
    );
    if (entry) {
      appendRunDebugEvent(entry);
    }
  }

  async function copyRunDebugInfo(): Promise<void> {
    const snapshot = buildRunDebugSnapshot({
      state,
      events: runDebugEvents,
      queuedMessages,
      guideMessages,
      composerImages,
      composerFiles,
    });
    try {
      await navigator.clipboard.writeText(snapshot);
      setRunDebugCopied(true);
      window.setTimeout(() => setRunDebugCopied(false), 1200);
    } catch (error) {
      appendRunDebugEvent({
        source: "client",
        method: "debug/copy",
        detail: error instanceof Error ? error.message : "复制失败",
        tone: "error",
      });
    }
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
        clearPendingComposerMessages();
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
        clearPendingComposerMessages();
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
        clearPendingComposerMessages();
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
      clearPendingComposerMessages();
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
    clearPendingComposerMessages();
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
    clearPendingComposerMessages();
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
    clearPendingComposerMessages();
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
    clearPendingComposerMessages();
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
      clearPendingComposerMessages();
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
      clearPendingComposerMessages();
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
      clearPendingComposerMessages();
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
    clearPendingComposerMessages();
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
      conversationAutoFollowRef.current = true;
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
    clearPendingComposerMessages();
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
      await window.wuu.queueTurn(targetThread.id, text, images, message.id, files);
      enqueueComposerMessage(message);
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
    conversationAutoFollowRef.current = true;
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
      const result = await window.wuu.startTurn(thread.id, text, images, files);
      setState((current) =>
        updateThreadByID(
          setThreadForPane(current, targetPane, thread),
          thread.id,
          (currentThread) => upsertTurn(currentThread, result.turn),
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
      appStateRef.current = {
        ...appStateRef.current,
        running: false,
        status: errorMessage,
      };
      setState((current) => ({
        ...current,
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
    conversationAutoFollowRef.current = true;
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
    try {
      const result = await window.wuu.startTurn(targetThread.id, text, images, files);
      setState((current) =>
        updateThreadByID(
          { ...current, activePane: pane },
          targetThread.id,
          (thread) => upsertTurn(thread, result.turn),
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
      appStateRef.current = {
        ...appStateRef.current,
        activePane: pane,
        running: false,
        status: errorMessage,
      };
      setState((current) => ({
        ...current,
        activePane: pane,
        running: false,
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
    toolPolicyProfile?: string,
  ): Promise<void> {
    const nextProvider = provider.trim();
    const nextModel = model.trim();
    const nextEffort = effort === undefined ? undefined : effort.trim();
    const nextVariant = variant === undefined ? undefined : variant.trim();
    const nextToolPolicyProfile =
      toolPolicyProfile === undefined ? undefined : toolPolicyProfile.trim();
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
    const currentToolPolicyProfile =
      state.initialized?.tool_policy?.profile ?? "";
    const currentToolPolicy = state.initialized?.tool_policy;
    const currentToolPolicyHasOverrides = Boolean(
      currentToolPolicy?.default_action ||
        Object.keys(currentToolPolicy?.tools ?? {}).length > 0 ||
        Object.keys(currentToolPolicy?.kinds ?? {}).length > 0 ||
        Object.keys(currentToolPolicy?.risks ?? {}).length > 0,
    );
    const toolPolicyChanged =
      nextToolPolicyProfile !== undefined &&
      (nextToolPolicyProfile !== currentToolPolicyProfile ||
        currentToolPolicyHasOverrides);
    if (
      !nextProvider ||
      !nextModel ||
      !state.initialized ||
      anyThreadIsRunning ||
      (nextProvider === state.initialized.provider &&
        nextModel === state.initialized.model &&
        (nextEffort === undefined ||
          nextEffort === (state.initialized.effort ?? "")) &&
        (nextVariant === undefined ||
          nextVariant === (state.initialized.variant ?? "")) &&
        !connectionChanged &&
        !toolPolicyChanged)
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
        nextToolPolicyProfile,
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
              providers: updated.providers ?? current.initialized.providers,
            }
          : current.initialized;
        const updateThreadModel = (thread: Thread): Thread => ({
          ...thread,
          model_provider: updated.provider,
          model: updated.model,
        });
        const thread = current.thread
          ? updateThreadModel(current.thread)
          : current.thread;
        return {
          ...current,
          initialized,
          thread,
          threads: current.threads.map(updateThreadModel),
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

  function toggleCodexRuntimeMenu(menu: Exclude<CodexRuntimeMenu, null>): void {
    if (!state.initialized || anyThreadIsRunning) {
      return;
    }
    setRuntimeMenuOpen(false);
    setAccessMenuOpen(false);
    setModeMenuOpen(false);
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
    if (!state.initialized || anyThreadIsRunning) {
      return;
    }
    await updateRuntimeSettings(provider, model, undefined, undefined, variant);
    setCodexRuntimeMenu(null);
  }

  async function selectRuntimeEffort(nextVariant: string): Promise<void> {
    if (!state.initialized || anyThreadIsRunning) {
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

  async function selectToolPolicyProfile(
    profile: ToolPolicyProfile,
  ): Promise<void> {
    if (!state.initialized || anyThreadIsRunning) {
      return;
    }
    await updateRuntimeSettings(
      state.initialized.provider,
      state.initialized.model,
      undefined,
      undefined,
      undefined,
      profile,
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
        setSettingsOpen(true);
        // payload.focus can be "providers" | "workspace" — the
        // settings view will read this in a follow-up patch.
        return;
      }
      case "copyDebug": {
        // Snapshot whatever the user can see right now. Doesn't
        // include a turn ID because the action is generic; if a
        // turn-scoped variant is needed, pass turn.id via payload.
        const snapshot = JSON.stringify(
          {
            kind: "wuu-notice-debug",
            category: action.payload,
            at: new Date().toISOString(),
            status: appStateRef.current.status,
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

  if (settingsOpen) {
    return (
      <SettingsView
        initialized={state.initialized}
        running={anyThreadIsRunning}
        showDebugControlsSetting={ENABLE_DEBUG_CONTROL_SETTING}
        debugControlsEnabled={debugControlsEnabled}
        sidebarWidth={sidebarWidth}
        sidebarMinWidth={SIDEBAR_MIN_WIDTH}
        sidebarMaxWidth={SIDEBAR_MAX_WIDTH}
        resizingSidebar={resizingSidebar}
        onBack={() => setSettingsOpen(false)}
        onSave={updateRuntimeSettings}
        onDebugControlsChange={setDebugControlsEnabled}
        onSidebarResizeStart={startSettingsSidebarResize}
        onSidebarSeparatorKey={handleSettingsSidebarSeparatorKey}
        onSidebarSeparatorDoubleClick={resetSettingsSidebarWidth}
      />
    );
  }

  const environmentPanelNode =
    (environmentPanelVisible || environmentPanelMounted) &&
    state.initialized ? (
      <div className="environment-side-stack">
        <EnvironmentPanel
          panelRef={environmentPanelRef}
          motionState={
            environmentPanelClosing ? "closing" : environmentPanelMotionState
          }
          initialized={state.initialized}
          gitStatus={state.gitStatus}
          activeContext={state.activeContext}
          activeProject={activeProject}
          planUpdate={activePlanUpdate}
          sourceItems={environmentSourceItems}
          backgroundProcesses={backgroundProcesses}
          stoppingProcessIDs={stoppingProcessIDs}
          activeMenu={environmentPanelMenu}
          running={anyThreadIsRunning}
          pullRequestDisabledReason={pullRequestDisabledReason}
          onSetActiveMenu={setEnvironmentPanelMenu}
          onClose={() => {
            setEnvironmentPanelOpen(false);
            setEnvironmentPanelDismissed(true);
            setEnvironmentPanelMenu(null);
          }}
          onOpenProject={() => void chooseProjectFolder()}
          onSelectNoProject={() => void useNoProject(false)}
          onSelectBranch={(branch) => void checkoutBranch(branch)}
          onCreateBranch={(branch) => createAndCheckoutBranch(branch)}
          onOpenReview={() => {
            setWorkspacePanelView("review");
            setWorkspaceRightPanelView("review");
            setWorkspaceMode(undefined);
            setRightPanelOpenWithMotion(true);
            setEnvironmentPanelOpen(false);
            setEnvironmentPanelDismissed(true);
            setEnvironmentPanelMenu(null);
          }}
          onOpenCommit={() => setEnvironmentDialog("commit")}
          onOpenPullRequest={() => setEnvironmentDialog("pull-request")}
          onStopBackgroundProcess={(process) => void stopBackgroundProcess(process)}
          onOpenBackgroundPreview={openBackgroundProcessPreview}
        />
        {queryHistoryDocked ? (
          <div className="query-history-environment-slot">
            <QueryHistoryPopover
              entries={pastQueries}
              onSelect={handleQueryHistorySelect}
            />
          </div>
        ) : null}
      </div>
    ) : null;
  return (
    <div className={shellClassName} style={shellStyle}>
      <AppSidebar
        state={state}
        pinnedThreads={sidebarPinnedThreads}
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
        onSelectThread={(id) => void selectThread(id)}
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
                <Terminal size={15} />
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
                <Film size={15} />
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
                <ListChecks size={15} />
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
                <Grid3X3 size={15} />
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
                    setEnvironmentPanelOpen(false);
                    setEnvironmentPanelMenu(null);
                    setRunDebugOpen((open) => !open);
                  }}
                >
                  <Bug size={15} />
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
                    onCopy={() => void copyRunDebugInfo()}
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
                  <AlertCircle size={14} />
                ) : (
                  <Terminal size={14} />
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
              <Info size={18} />
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

        {debugControlsVisible &&
        ENABLE_TURN_PROGRESS_EXPERIMENT &&
        turnProgressPreviewOpen ? (
          <TurnProgressPreviewOverlay
            onClose={() => setTurnProgressPreviewOpen(false)}
          />
        ) : null}

        {environmentPanelNode}

        {pendingViewSwitch?.visible ? <ViewSwitchLoading /> : null}

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
                onUseSkill={useSkillFromCatalog}
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
              <div className="conversation-width">
                {conversationGridVisible ? <ConversationGridGuides /> : null}
                {turns.map((turn) => (
                  <Fragment key={turn.id}>
                    <TurnView
                      turn={turn}
                      cwd={activeThread?.cwd ?? state.activeContext?.cwd}
                      latestAgentMessageID={latestAgentMessageID}
                      onStreamFrame={scheduleStreamScroll}
                      onForkMessage={
                        activeThread
                          ? (turnID, itemID) =>
                              void forkThreadFromMessage(
                                activeThread,
                                turnID,
                                itemID,
                              )
                          : undefined
                      }
                      onNoticeAction={handleNoticeAction}
                    />
                  </Fragment>
                ))}
              </div>
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
