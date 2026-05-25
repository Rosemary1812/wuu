import {
  AlertCircle,
  Archive,
  Brain,
  Bug,
  Check,
  ChevronDown,
  ChevronRight,
  Copy,
  CornerDownRight,
  Clock,
  FileText,
  Film,
  Folder,
  FolderX,
  FolderOpen,
  FolderPlus,
  GitBranch,
  GitFork,
  Image as ImageIcon,
  Info,
  Laptop,
  ListChecks,
  List as ListIcon,
  MessageSquarePlus,
  MoreHorizontal,
  PanelBottomOpen,
  Pencil,
  Pin,
  Plus,
  RefreshCw,
  Search,
  Send,
  Settings,
  ShieldCheck,
  Square,
  Terminal,
  ThumbsDown,
  ThumbsUp,
  Trash2,
  Wrench,
  X
} from "lucide-react";
import {
  type CSSProperties,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
  type RefObject,
  type ReactNode,
  Fragment,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState
} from "react";
import type {
  Agent,
  AppServerNotification,
  AskUserQuestion,
  AskUserResponse,
  CodexModelSummary,
  DesktopProject,
  GitCommitResult,
  GitPullRequestResult,
  GitStatusResult,
  InputImage,
  InitializeResult,
  PlanUpdate,
  ProjectListResult,
  RuntimeContext,
  ServerEvent,
  Thread,
  ThreadItem,
  Turn
} from "../shared/protocol";
import {
  AnsweredAskUserMessage,
  AskUserMessage,
  type AnsweredAskRequestState,
  type AskRequestState
} from "./AskUserMessages";
import {
  composerImageFromFile,
  createComposerMessage,
  imageSource,
  inputImagesFromComposer,
  mergeGuideMessages,
  type ComposerImage,
  type QueuedComposerMessage
} from "./ComposerMessages";
import {
  Composer,
  SplitPaneComposer,
  isInsideFloatingMenu,
  type CodexModelLoadState,
  type CodexRuntimeMenu,
  type ComposerVariant,
  type FloatingMenuOwner
} from "./ComposerView";
import { createAgentTreeDemo, createConversationFixture, type ConversationFixtureKind } from "./ConversationFixtures";
import {
  EnvironmentPanel,
  buildEnvironmentSourceItems,
  type EnvironmentPanelMenu,
  type EnvironmentPanelMotionState
} from "./EnvironmentPanel";
import { CommitChangesDialog, PullRequestDialog } from "./GitDialogs";
import { RichContent } from "./RichContent";
import { EmptyConversationHome, RuntimeLoading, ViewSwitchLoading } from "./LoadingViews";
import {
  isCodexProvider,
  normalizedEffortForModel,
  pullRequestUnavailableReason
} from "./RuntimeHelpers";
import { SettingsView } from "./SettingsView";
import { StreamingMarkdown } from "./StreamingMarkdown";
import { streamTextKey, streamTextStore, type StreamTextField } from "./StreamText";
import {
  ToolActivityRow,
  isRecord,
  numberValue,
  readableToolName,
  recordValue,
  stringValue,
  type JsonRecord
} from "./ToolActivity";
import { sortChildAgents } from "./ThreadAgents";
import { PinnedThreadList, ProjectList } from "./ThreadSidebar";
import {
  isCancellationMessage,
  rawErrorMessage,
  statusMessageForError,
  statusToneClass,
  userFacingErrorForMessage,
  type UserFacingErrorDisplay
} from "./UserFacingErrors";
import {
  WorkspaceBottomPanel,
  WorkspaceMainPanel,
  WorkspaceRightPanel,
  WorkspaceToolIcon,
  workspaceModeTitle,
  type WorkspacePanelView,
  type WorkspaceRightPanelView
} from "./WorkspacePanels";
import { desktopApiErrorMessage } from "./WorkspaceReviewHelpers";

const VIEW_SWITCH_LOADING_DELAY_MS = 180;
const SIDEBAR_MOTION_MS = 280;
const RIGHT_PANEL_MOTION_MS = 280;
const PROJECT_THREAD_COLLAPSE_MS = 190;
const ENVIRONMENT_PANEL_MOTION_MS = 260;

type EnvironmentDialog = "commit" | "pull-request" | null;
type PendingViewSwitchKind = "thread" | "project" | "runtime";

type PendingViewSwitch = {
  kind: PendingViewSwitchKind;
  targetID: string;
  visible: boolean;
};
type ConversationPaneID = "primary" | "secondary";
type RunDebugEventSource = "client" | "server";
type RunDebugEventTone = "info" | "running" | "success" | "warning" | "error";

function SidePanelToggleIcon({
  side,
  open,
  size = 20
}: {
  side: "left" | "right";
  open: boolean;
  size?: number;
}): JSX.Element {
  const paneX = side === "left" ? 4 : 10.2;
  const dividerX = side === "left" ? 8.5 : 9.5;
  return (
    <svg
      className="side-panel-toggle-icon"
      data-open={open}
      width={size}
      height={size}
      viewBox="0 0 18 18"
      aria-hidden="true"
      focusable="false"
    >
      <rect className="side-panel-toggle-frame" x="2.65" y="3.05" width="12.7" height="11.9" rx="2.4" />
      <path className="side-panel-toggle-divider" d={`M${dividerX} 3.65v10.7`} />
      <rect className="side-panel-toggle-pane" x={paneX} y="4.75" width="3.8" height="8.5" rx="1.1" />
    </svg>
  );
}

type RunDebugPhaseTone = "idle" | "running" | "success" | "warning" | "error";

type ComposerDraftState = {
  prompt: string;
  images: ComposerImage[];
};

function emptyComposerDraft(): ComposerDraftState {
  return { prompt: "", images: [] };
}

function initialSplitComposerDrafts(): Record<ConversationPaneID, ComposerDraftState> {
  return {
    primary: emptyComposerDraft(),
    secondary: emptyComposerDraft()
  };
}

function cloneComposerDraft(draft: ComposerDraftState): ComposerDraftState {
  return {
    prompt: draft.prompt,
    images: draft.images.map((image) => ({ ...image }))
  };
}

type RunDebugEvent = {
  id: number;
  at: number;
  source: RunDebugEventSource;
  method: string;
  detail: string;
  tone: RunDebugEventTone;
  threadID?: string;
  turnID?: string;
  itemID?: string;
};

type RunDebugPhase = {
  label: string;
  detail: string;
  tone: RunDebugPhaseTone;
  turn?: Turn;
  activeItem?: ThreadItem;
};

type TurnProgressContent = {
  label: string;
  detail?: string;
};

type TurnProgressEra =
  | "sticks"
  | "swords"
  | "fortress"
  | "cannon"
  | "factory"
  | "armor"
  | "rockets"
  | "orbit"
  | "galaxy";

type TurnProgressCampaign = {
  currentEra: TurnProgressEra;
  nextEra: TurnProgressEra;
  currentLayer: "a" | "b";
  transitionProgress: number;
  variant: number;
};

const TURN_PROGRESS_ERA_MS = 4 * 60 * 1000;
const TURN_PROGRESS_TRANSITION_MS = 30 * 1000;
const TURN_PROGRESS_PREVIEW_MS = 72 * 1000;
const TURN_PROGRESS_ERAS: TurnProgressEra[] = [
  "sticks",
  "swords",
  "fortress",
  "cannon",
  "factory",
  "armor",
  "rockets",
  "orbit",
  "galaxy"
];
const TURN_PROGRESS_PREVIEW_SPEED = (TURN_PROGRESS_ERA_MS * TURN_PROGRESS_ERAS.length) / TURN_PROGRESS_PREVIEW_MS;
const TURN_PROGRESS_CAMPAIGN_MS = TURN_PROGRESS_ERA_MS * TURN_PROGRESS_ERAS.length;
const TURN_PROGRESS_ERA_LABELS: Record<TurnProgressEra, string> = {
  sticks: "木棍",
  swords: "刀剑",
  fortress: "城堡",
  cannon: "火炮",
  factory: "工厂",
  armor: "装甲",
  rockets: "火箭",
  orbit: "轨道",
  galaxy: "星际"
};

type AppState = {
  initialized?: InitializeResult;
  projects: DesktopProject[];
  activeContext?: RuntimeContext;
  activeProjectId?: string;
  gitStatus?: GitStatusResult;
  thread?: Thread;
  secondaryThread?: Thread;
  activePane: ConversationPaneID;
  allowThreadAutoActivation: boolean;
  threads: Thread[];
  running: boolean;
  status: string;
  askRequests: AskRequestState[];
  answeredAskRequests: AnsweredAskRequestState[];
};

const initialState: AppState = {
  projects: [],
  activePane: "primary",
  allowThreadAutoActivation: false,
  threads: [],
  running: false,
  status: "connecting",
  askRequests: [],
  answeredAskRequests: []
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
const SWISS_STYLE_KEY = "wuu.desktop.swissInternationalStyle";
const DEBUG_CONTROLS_KEY = "wuu.desktop.debugControlsEnabled";
const CONVERSATION_AUTO_SCROLL_THRESHOLD_PX = 48;
const CONVERSATION_SCROLLBAR_HIDE_DELAY_MS = 700;
const RENDERER_ENV = (
  import.meta as ImportMeta & { env?: { DEV?: boolean; VITE_ENABLE_RUN_DEBUG_PANEL?: string } }
).env;
const ENABLE_DEBUG_CONTROL_SETTING = Boolean(RENDERER_ENV?.DEV);
const ENABLE_DEBUG_CONTROLS = Boolean(RENDERER_ENV?.DEV || RENDERER_ENV?.VITE_ENABLE_RUN_DEBUG_PANEL === "true");
const ENABLE_LAUNCH_PREVIEW = Boolean(RENDERER_ENV?.DEV);
const ENABLE_RUN_DEBUG_PANEL = Boolean(RENDERER_ENV?.DEV || RENDERER_ENV?.VITE_ENABLE_RUN_DEBUG_PANEL === "true");
const ENABLE_SWISS_STYLE_TOGGLE = Boolean(RENDERER_ENV?.DEV);
const ENABLE_CONVERSATION_FIXTURES = Boolean(RENDERER_ENV?.DEV);
const ENABLE_PLAN_PANEL_DEBUG = Boolean(RENDERER_ENV?.DEV);
const ENABLE_TURN_PROGRESS_EXPERIMENT = false;

type SidebarResizeSession = {
  startX: number;
  startWidth: number;
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
    return new Set(parsed.filter((id): id is string => typeof id === "string" && id.length > 0));
  } catch {
    return new Set();
  }
}

function initialWorkspaceRightPanelWidth(): number {
  const stored = Number(window.localStorage.getItem(WORKSPACE_RIGHT_PANEL_WIDTH_KEY));
  if (!Number.isFinite(stored)) {
    return WORKSPACE_RIGHT_PANEL_DEFAULT_WIDTH;
  }
  return clamp(stored, WORKSPACE_RIGHT_PANEL_MIN_WIDTH, WORKSPACE_RIGHT_PANEL_MAX_WIDTH);
}

function initialSwissStyleEnabled(): boolean {
  return ENABLE_SWISS_STYLE_TOGGLE && window.localStorage.getItem(SWISS_STYLE_KEY) === "true";
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

function clampWorkspaceRightPanelWidth(width: number, sidebarWidth: number): number {
  const maxForWindow =
    typeof window === "undefined"
      ? WORKSPACE_RIGHT_PANEL_MAX_WIDTH
      : window.innerWidth - sidebarWidth - WORKSPACE_RIGHT_PANEL_MAIN_MIN_WIDTH;
  const maxWidth = Math.max(
    WORKSPACE_RIGHT_PANEL_MIN_WIDTH,
    Math.min(WORKSPACE_RIGHT_PANEL_MAX_WIDTH, maxForWindow)
  );
  return clamp(width, WORKSPACE_RIGHT_PANEL_MIN_WIDTH, maxWidth);
}

export function App(): JSX.Element {
  const [state, setState] = useState<AppState>(initialState);
  const [prompt, setPrompt] = useState("");
  const [composerImages, setComposerImages] = useState<ComposerImage[]>([]);
  const [splitComposerDrafts, setSplitComposerDrafts] = useState<Record<ConversationPaneID, ComposerDraftState>>(
    initialSplitComposerDrafts
  );
  const [queuedMessages, setQueuedMessages] = useState<QueuedComposerMessage[]>([]);
  const [guideMessages, setGuideMessages] = useState<QueuedComposerMessage[]>([]);
  const [sidebarWidth, setSidebarWidth] = useState(initialSidebarWidth);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(initialSidebarCollapsed);
  const [collapsedProjectIDs, setCollapsedProjectIDs] = useState<Set<string>>(initialCollapsedProjectIDs);
  const [resizingSidebar, setResizingSidebar] = useState(false);
  const [sidebarAnimating, setSidebarAnimating] = useState(false);
  const [workspaceRightPanelWidth, setWorkspaceRightPanelWidth] = useState(initialWorkspaceRightPanelWidth);
  const [resizingRightPanel, setResizingRightPanel] = useState(false);
  const [projectMenuOpen, setProjectMenuOpen] = useState(false);
  const [collapsingProjectIDs, setCollapsingProjectIDs] = useState<Set<string>>(() => new Set());
  const [runtimeMenuOpen, setRuntimeMenuOpen] = useState(false);
  const [accessMenuOpen, setAccessMenuOpen] = useState(false);
  const [codexRuntimeMenu, setCodexRuntimeMenu] = useState<CodexRuntimeMenu>(null);
  const [codexModels, setCodexModels] = useState<CodexModelLoadState>({ loading: false, error: "", models: [] });
  const [modeMenuOpen, setModeMenuOpen] = useState(false);
  const [branchMenuOpen, setBranchMenuOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [projectFilter, setProjectFilter] = useState("");
  const [launchPreviewPinned, setLaunchPreviewPinned] = useState(false);
  const [turnProgressPreviewOpen, setTurnProgressPreviewOpen] = useState(false);
  const [rightPanelOpen, setRightPanelOpen] = useState(false);
  const [rightPanelAnimating, setRightPanelAnimating] = useState(false);
  const [bottomPanelOpen, setBottomPanelOpen] = useState(false);
  const [workspaceToolTabs, setWorkspaceToolTabs] = useState<WorkspacePanelView[]>([]);
  const [workspacePanelView, setWorkspacePanelView] = useState<WorkspacePanelView>("files");
  const [workspaceRightPanelView, setWorkspaceRightPanelView] = useState<WorkspaceRightPanelView>("tools");
  const [workspaceMode, setWorkspaceMode] = useState<WorkspacePanelView | undefined>(undefined);
  const [selectedWorkspaceFile, setSelectedWorkspaceFile] = useState<string | undefined>(undefined);
  const [environmentPanelOpen, setEnvironmentPanelOpen] = useState(false);
  const [environmentPanelDismissed, setEnvironmentPanelDismissed] = useState(false);
  const [environmentPanelHasRoom, setEnvironmentPanelHasRoom] = useState(() =>
    typeof window === "undefined" ? false : window.matchMedia("(min-width: 1320px) and (min-height: 680px)").matches
  );
  const [environmentPanelMounted, setEnvironmentPanelMounted] = useState(false);
  const [environmentPanelClosing, setEnvironmentPanelClosing] = useState(false);
  const [environmentPanelReserved, setEnvironmentPanelReserved] = useState(false);
  const [environmentPanelMenu, setEnvironmentPanelMenu] = useState<EnvironmentPanelMenu>(null);
  const [environmentDialog, setEnvironmentDialog] = useState<EnvironmentDialog>(null);
  const [runDebugOpen, setRunDebugOpen] = useState(false);
  const [runDebugEvents, setRunDebugEvents] = useState<RunDebugEvent[]>([]);
  const [runDebugCopied, setRunDebugCopied] = useState(false);
  const [archiveConfirmThreadID, setArchiveConfirmThreadID] = useState<string | undefined>(undefined);
  const [pendingViewSwitch, setPendingViewSwitch] = useState<PendingViewSwitch | undefined>(undefined);
  const [debugControlsEnabled, setDebugControlsEnabled] = useState(initialDebugControlsEnabled);
  const [swissStyleEnabled, setSwissStyleEnabled] = useState(initialSwissStyleEnabled);
  const conversationScrollRef = useRef<HTMLDivElement | null>(null);
  const splitPaneRefs = useRef<Record<ConversationPaneID, HTMLElement | null>>({ primary: null, secondary: null });
  const conversationPaneRef = useRef<HTMLElement | null>(null);
  const dockComposerRef = useRef<HTMLElement>(null);
  const dockComposerHeightRef = useRef(0);
  const conversationAutoFollowRef = useRef(true);
  const streamScrollFrameRef = useRef<number | undefined>(undefined);
  const projectCollapseTimersRef = useRef(new Map<string, number>());
  const sidebarMotionTimerRef = useRef<number | undefined>(undefined);
  const rightPanelMotionTimerRef = useRef<number | undefined>(undefined);
  const conversationScrollbarHideTimerRef = useRef<number | undefined>(undefined);
  const windowResizingRef = useRef(false);
  const environmentPanelHasRoomRef = useRef(environmentPanelHasRoom);
  const pendingEnvironmentPanelHasRoomRef = useRef<boolean | undefined>(undefined);
  const resizeSessionRef = useRef<SidebarResizeSession | null>(null);
  const rightPanelResizeSessionRef = useRef<RightPanelResizeSession | null>(null);
  const gitRefreshTimerRef = useRef<number | undefined>(undefined);
  const gitRefreshInFlightRef = useRef(false);
  const gitRefreshQueuedRef = useRef(false);
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
  const drainingQueueRef = useRef(false);
  const queueDrainPausedRef = useRef(false);
  const viewSwitchRequestRef = useRef(0);
  const viewSwitchDelayTimerRef = useRef<number | undefined>(undefined);
  const runDebugEventIDRef = useRef(0);
  const runDebugDeltaSeenRef = useRef(new Set<string>());
  const effectiveSidebarWidth = sidebarCollapsed ? 0 : sidebarWidth;
  const clampedWorkspaceRightPanelWidth = clampWorkspaceRightPanelWidth(workspaceRightPanelWidth, effectiveSidebarWidth);
  const debugControlsVisible = ENABLE_DEBUG_CONTROLS && debugControlsEnabled;
  const activeThread = activeThreadForState(state);
  const activeThreadID = activeThread?.id;
  const activePlanUpdate = latestPlanUpdateForThread(activeThread);
  const splitConversation = Boolean(state.thread && state.secondaryThread && !workspaceMode);

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
      if (!nextResizing && pendingEnvironmentPanelHasRoomRef.current !== undefined) {
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

    const offWindowResizeState = window.wuu.onWindowResizeState(({ resizing: nextResizing }) => {
      if (nextResizing) {
        setResizeState(true);
        scheduleResizeEnd();
        return;
      }
      scheduleResizeEnd(40);
    });

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
    const query = window.matchMedia("(min-width: 1320px) and (min-height: 680px)");
    const update = (): void => {
      const nextHasRoom = query.matches;
      if (windowResizingRef.current || document.documentElement.classList.contains("window-resizing")) {
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
    if (!isStateActiveThreadRunning(state)) {
      void drainQueuedMessages();
    }
  }, [
    state.activeContext?.cwd,
    state.initialized?.model,
    state.initialized?.provider,
    state.running,
    state.activePane,
    state.thread?.id,
    state.thread?.status,
    state.thread?.turns,
    state.secondaryThread?.id,
    state.secondaryThread?.status,
    state.secondaryThread?.turns
  ]);

  useEffect(() => {
    let mounted = true;
    const off = window.wuu.onServerEvent((event) => {
      if (!mounted) {
        return;
      }
      recordRunDebugEvent(event);
      const handling = handleStreamingNotification(event, appStateRef.current);
      if (handling === "stream") {
        scheduleStreamScroll();
        return;
      }
      if (handling === "skip") {
        return;
      }
      if (serverEventShouldRefreshGit(event)) {
        scheduleGitStatusRefresh(600);
      }
      setState((current) => reduceServerEvent(current, event));
    });

    void (async () => {
      try {
        const listedProjects = await window.wuu.listProjects();
        const runtimeState = listedProjects.active_context ? listedProjects : await window.wuu.selectNoProject(false);
        const loadedState = await loadRuntime(runtimeState);
        if (!mounted) {
          return;
        }
        setState((current) => ({
          ...current,
          ...loadedState
        }));
      } catch (error) {
        if (!mounted) {
          return;
        }
        setState((current) => ({
          ...current,
          status: error instanceof Error ? error.message : "failed to start"
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
    };
  }, []);

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
        !environmentToggleRef.current?.contains(target) && !environmentPanelRef.current?.contains(target);
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
    runtimeMenuOpen
  ]);

  useLayoutEffect(() => {
    conversationAutoFollowRef.current = true;
    scrollConversationToBottom({ force: true });
  }, [activeThreadID]);

  useEffect(() => {
    scheduleStreamScroll();
  }, [state.thread?.turns, state.secondaryThread?.turns]);

  useEffect(() => {
    if (!visibleAskRequestForThread(state.askRequests, activeThreadID)) {
      return;
    }
    setSettingsOpen(false);
    setWorkspaceMode(undefined);
    conversationAutoFollowRef.current = true;
    window.requestAnimationFrame(() => scrollConversationToBottom({ force: true }));
  }, [activeThreadID, state.askRequests]);

  useEffect(() => {
    const enabled = debugControlsVisible && ENABLE_SWISS_STYLE_TOGGLE && swissStyleEnabled;
    document.documentElement.classList.toggle("swiss-international", enabled);
    if (ENABLE_SWISS_STYLE_TOGGLE) {
      window.localStorage.setItem(SWISS_STYLE_KEY, String(swissStyleEnabled));
    }
    return () => {
      document.documentElement.classList.remove("swiss-international");
    };
  }, [debugControlsVisible, swissStyleEnabled]);

  useEffect(() => {
    if (ENABLE_DEBUG_CONTROLS) {
      window.localStorage.setItem(DEBUG_CONTROLS_KEY, String(debugControlsEnabled));
    }
  }, [debugControlsEnabled]);

  useEffect(() => {
    if (debugControlsVisible) {
      return;
    }
    setLaunchPreviewPinned(false);
    setRunDebugOpen(false);
    setTurnProgressPreviewOpen(false);
  }, [debugControlsVisible]);

  useEffect(() => {
    window.localStorage.setItem(SIDEBAR_WIDTH_KEY, String(sidebarWidth));
    window.localStorage.setItem(SIDEBAR_COLLAPSED_KEY, String(sidebarCollapsed));
  }, [sidebarWidth, sidebarCollapsed]);

  useEffect(() => {
    window.localStorage.setItem(PROJECT_COLLAPSED_IDS_KEY, JSON.stringify([...collapsedProjectIDs]));
  }, [collapsedProjectIDs]);

  useEffect(() => {
    window.localStorage.setItem(WORKSPACE_RIGHT_PANEL_WIDTH_KEY, String(workspaceRightPanelWidth));
  }, [workspaceRightPanelWidth]);

  useEffect(() => {
    setSelectedWorkspaceFile(undefined);
  }, [state.activeContext?.cwd]);

  useEffect(() => {
    scheduleGitStatusRefresh(0);
  }, [state.activeContext?.cwd]);

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
      applySidebarWidth(session.startWidth + event.clientX - session.startX);
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
        clampWorkspaceRightPanelWidth(session.startWidth - (event.clientX - session.startX), effectiveSidebarWidth)
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
      setWorkspaceRightPanelWidth((current) => clampWorkspaceRightPanelWidth(current, effectiveSidebarWidth));
    }

    window.addEventListener("resize", handleResize);
    return () => window.removeEventListener("resize", handleResize);
  }, [effectiveSidebarWidth]);

  const activeProject = useMemo(
    () => state.projects.find((project) => project.id === state.activeProjectId),
    [state.activeProjectId, state.projects]
  );
  const activeTitle = workspaceMode ? workspaceModeTitle(workspaceMode) : activeThread?.preview || "新对话";
  const emptyThreadTitle =
    state.activeContext?.kind === "project"
      ? `我们应该在 ${activeProject?.name ?? "这个项目"} 中构建什么？`
      : "我们应该在 wuu 中构建什么？";
  const turns = activeThread?.turns ?? [];
  const latestAgentMessageID = latestAgentMessageItemID(turns);
  const visibleAskRequest = visibleAskRequestForThread(state.askRequests, activeThreadID);
  const visibleAnsweredAskRequests = visibleAnsweredAskRequestsForThread(state.answeredAskRequests, activeThreadID);
  const answeredAskRequestsWithoutVisibleTurn = visibleAnsweredAskRequests.filter(
    (request) => !request.turnID || !turns.some((turn) => turn.id === request.turnID)
  );
  const emptyConversation = turns.length === 0 && !visibleAskRequest && visibleAnsweredAskRequests.length === 0;
  const previewingLaunch = debugControlsVisible && ENABLE_LAUNCH_PREVIEW && launchPreviewPinned;
  const showingWorkspaceMode = state.initialized && !previewingLaunch && workspaceMode !== undefined;
  const sidebarPinnedThreads = pinnedThreads(state.threads);
  const visiblePendingThreadID =
    pendingViewSwitch?.visible && pendingViewSwitch.kind === "thread" ? pendingViewSwitch.targetID : undefined;
  const visiblePendingProjectID =
    pendingViewSwitch?.visible && pendingViewSwitch.kind === "project" ? pendingViewSwitch.targetID : undefined;
  const activeThreadReadOnly = Boolean(activeThread?.read_only);
  const activeThreadIsRunning = !activeThreadReadOnly && isStateActiveThreadRunning(state);
  const viewSwitchPending = pendingViewSwitch !== undefined;
  const pendingAskThreadIDs = pendingAskThreadIDsForRequests(state.askRequests);
  const anyThreadIsRunning = isAnyThreadRunning(state) || viewSwitchPending;
  const environmentPanelCanShow = Boolean(state.initialized && !previewingLaunch && !showingWorkspaceMode && !rightPanelOpen);
  const environmentPanelTargetVisible =
    environmentPanelCanShow &&
    (environmentPanelOpen || (environmentPanelHasRoom && !environmentPanelDismissed && !emptyConversation));
  const environmentPanelVisible = environmentPanelTargetVisible;
  const environmentPanelMotionState: EnvironmentPanelMotionState = environmentPanelVisible ? "open" : "closing";
  const shellClassName = `app-shell${sidebarCollapsed ? " sidebar-collapsed" : ""}${
    sidebarAnimating ? " sidebar-animating" : ""
  }${rightPanelAnimating ? " right-panel-animating" : ""}${resizingSidebar ? " resizing-sidebar" : ""}${
    resizingRightPanel ? " resizing-right-panel" : ""
  }${rightPanelOpen ? " right-panel-open" : ""}${bottomPanelOpen ? " bottom-panel-open" : ""}`;
  const shellStyle = {
    "--sidebar-width": `${effectiveSidebarWidth}px`,
    "--sidebar-open-width": `${sidebarWidth}px`,
    "--sidebar-motion-duration": `${SIDEBAR_MOTION_MS}ms`,
    "--workspace-panel-motion-duration": `${RIGHT_PANEL_MOTION_MS}ms`,
    "--workspace-right-panel-width": `${clampedWorkspaceRightPanelWidth}px`,
    "--environment-panel-width": "328px",
    "--environment-panel-reserved-width": "372px",
    "--environment-panel-edge-gap": "18px",
    "--environment-panel-motion-duration": `${ENVIRONMENT_PANEL_MOTION_MS}ms`,
    "--workspace-bottom-panel-height": "238px"
  } as CSSProperties;
  const environmentSourceItems = useMemo(
    () =>
      buildEnvironmentSourceItems({
        activeContext: state.activeContext,
        activeProject,
        selectedWorkspaceFile,
        composerImages,
        queuedMessages,
        guideMessages
      }),
    [activeProject, composerImages, guideMessages, queuedMessages, selectedWorkspaceFile, state.activeContext]
  );
  const pullRequestDisabledReason = pullRequestUnavailableReason(state.gitStatus);
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
  }, [environmentPanelHasRoom, environmentPanelMounted, environmentPanelVisible]);

  useEffect(() => {
    if (!environmentPanelVisible && environmentPanelMenu) {
      setEnvironmentPanelMenu(null);
    }
  }, [environmentPanelMenu, environmentPanelVisible]);

  useEffect(() => {
    if (visibleAnsweredAskRequests.length === 0) {
      return;
    }
    conversationAutoFollowRef.current = true;
    window.requestAnimationFrame(() => scrollConversationToBottom({ force: true }));
  }, [visibleAnsweredAskRequests.length]);

  useLayoutEffect(() => {
    const node = dockComposerRef.current;
    const pane = conversationPaneRef.current;
    const applyHeight = (nextHeight: number): void => {
      if (dockComposerHeightRef.current === nextHeight) {
        return;
      }
      dockComposerHeightRef.current = nextHeight;
      pane?.style.setProperty("--dock-composer-height", `${nextHeight}px`);
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
  }, [emptyConversation, previewingLaunch, showingWorkspaceMode, state.initialized]);

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
    setWorkspaceRightPanelWidth(clampWorkspaceRightPanelWidth(nextWidth, effectiveSidebarWidth));
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
    const distanceFromBottom = Math.max(0, node.scrollHeight - node.scrollTop - node.clientHeight);
    return distanceFromBottom <= CONVERSATION_AUTO_SCROLL_THRESHOLD_PX;
  }

  function startSidebarResize(event: ReactPointerEvent<HTMLDivElement>): void {
    if (event.button !== 0) {
      return;
    }
    event.preventDefault();
    resizeSessionRef.current = {
      startX: event.clientX,
      startWidth: sidebarCollapsed ? 0 : sidebarWidth
    };
    setProjectMenuOpen(false);
    setResizingSidebar(true);
  }

  function startRightPanelResize(event: ReactPointerEvent<HTMLDivElement>): void {
    if (event.button !== 0 || !rightPanelOpen) {
      return;
    }
    event.preventDefault();
    rightPanelResizeSessionRef.current = {
      startX: event.clientX,
      startWidth: clampedWorkspaceRightPanelWidth
    };
    setResizingRightPanel(true);
  }

  function handleRightPanelSeparatorKey(event: ReactKeyboardEvent<HTMLDivElement>): void {
    if (event.key === "ArrowLeft") {
      event.preventDefault();
      applyWorkspaceRightPanelWidth(workspaceRightPanelWidth + WORKSPACE_RIGHT_PANEL_STEP);
    } else if (event.key === "ArrowRight") {
      event.preventDefault();
      applyWorkspaceRightPanelWidth(workspaceRightPanelWidth - WORKSPACE_RIGHT_PANEL_STEP);
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
    setSidebarWidth((width) => (width <= SIDEBAR_MIN_WIDTH ? SIDEBAR_DEFAULT_WIDTH : width));
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
    if (collapsedProjectIDs.has(projectID) || collapsingProjectIDs.has(projectID)) {
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

    setCollapsingProjectIDs((current) => (current.has(projectID) ? current : new Set(current).add(projectID)));
    clearProjectCollapseTimer(projectID);
    const timer = window.setTimeout(() => {
      projectCollapseTimersRef.current.delete(projectID);
      setCollapsedProjectIDs((current) => (current.has(projectID) ? current : new Set(current).add(projectID)));
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
    setWorkspaceToolTabs((current) => (current.includes(view) ? current : [...current, view]));
  }

  function activateWorkspaceTool(view: WorkspacePanelView): void {
    setWorkspacePanelView(view);
    setWorkspaceRightPanelView(view);
    setWorkspaceMode(view === "files" ? "files" : undefined);
  }

  function openWorkspaceTool(view: WorkspacePanelView): void {
    ensureWorkspaceToolTab(view);
    activateWorkspaceTool(view);
    setRightPanelOpenWithMotion(true);
  }

  function openWorkspaceFile(path: string): void {
    ensureWorkspaceToolTab("files");
    activateWorkspaceTool("files");
    setRightPanelOpenWithMotion(true);
    setSelectedWorkspaceFile((current) => (current === path ? current : path));
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
    const fallback = nextTabs[Math.min(Math.max(closedIndex, 0), Math.max(nextTabs.length - 1, 0))];
    if (fallback) {
      activateWorkspaceTool(fallback);
      return;
    }
    setWorkspaceRightPanelView("tools");
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

  async function attachComposerImageFiles(files: File[]): Promise<void> {
    if (files.length === 0) {
      return;
    }
    try {
      const images = await Promise.all(files.map((file) => composerImageFromFile(file)));
      setComposerImages((current) => [...current, ...images]);
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "图片粘贴失败"
      }));
    }
  }

  function removeComposerImage(id: string): void {
    setComposerImages((current) => current.filter((image) => image.id !== id));
  }

  function updateSplitComposerDraft(
    pane: ConversationPaneID,
    update: (draft: ComposerDraftState) => ComposerDraftState
  ): void {
    setSplitComposerDrafts((current) => {
      const draft = current[pane] ?? emptyComposerDraft();
      return {
        ...current,
        [pane]: update(draft)
      };
    });
  }

  function setSplitComposerPrompt(pane: ConversationPaneID, value: string): void {
    updateSplitComposerDraft(pane, (draft) => ({ ...draft, prompt: value }));
  }

  async function attachSplitComposerImageFiles(pane: ConversationPaneID, files: File[]): Promise<void> {
    if (files.length === 0) {
      return;
    }
    try {
      const images = await Promise.all(files.map((file) => composerImageFromFile(file)));
      updateSplitComposerDraft(pane, (draft) => ({ ...draft, images: [...draft.images, ...images] }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "图片粘贴失败"
      }));
    }
  }

  function removeSplitComposerImage(pane: ConversationPaneID, id: string): void {
    updateSplitComposerDraft(pane, (draft) => ({
      ...draft,
      images: draft.images.filter((image) => image.id !== id)
    }));
  }

  function moveSplitDraftToGlobalComposer(pane: ConversationPaneID): void {
    const draft = splitComposerDrafts[pane] ?? emptyComposerDraft();
    setPrompt(draft.prompt);
    setComposerImages(draft.images.map((image) => ({ ...image })));
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
    queueDrainPausedRef.current = false;
    setQueuedMessagesNow([]);
    setGuideMessagesNow([]);
  }

  function enqueueComposerMessage(message: QueuedComposerMessage): void {
    queueDrainPausedRef.current = false;
    const next = [...queuedMessagesRef.current, message];
    setQueuedMessagesNow(next);
    setState((current) => ({
      ...current,
      status: `已排队 ${next.length} 条`
    }));
  }

  function removeQueuedMessage(id: string): void {
    queueDrainPausedRef.current = false;
    setQueuedMessagesNow(queuedMessagesRef.current.filter((message) => message.id !== id));
    void drainQueuedMessages();
  }

  function removeGuideMessage(id: string): void {
    queueDrainPausedRef.current = false;
    setGuideMessagesNow(guideMessagesRef.current.filter((message) => message.id !== id));
    void drainQueuedMessages();
  }

  async function guideQueuedMessage(id: string): Promise<void> {
    const queuedIndex = queuedMessagesRef.current.findIndex((message) => message.id === id);
    if (queuedIndex < 0) {
      return;
    }
    const message = queuedMessagesRef.current[queuedIndex];
    const remainingQueued = [
      ...queuedMessagesRef.current.slice(0, queuedIndex),
      ...queuedMessagesRef.current.slice(queuedIndex + 1)
    ];
    queueDrainPausedRef.current = false;
    setQueuedMessagesNow(remainingQueued);
    setGuideMessagesNow([...guideMessagesRef.current, message]);
    setState((current) => ({
      ...current,
      status: "引导已加入"
    }));

    const currentState = appStateRef.current;
    if (!isStateActiveThreadRunning(currentState)) {
      void drainQueuedMessages();
      return;
    }
    const targetThread = activeThreadForState(currentState);
    if (!targetThread) {
      return;
    }
    try {
      await window.wuu.interruptTurn(targetThread.id);
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "interrupt failed"
      }));
    }
  }

  function handleSidebarSeparatorKey(event: ReactKeyboardEvent<HTMLDivElement>): void {
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

  function renderComposer(variant: ComposerVariant): JSX.Element {
    return (
      <Composer
        variant={variant}
        containerRef={variant === "dock" ? dockComposerRef : undefined}
        prompt={prompt}
        setPrompt={setPrompt}
        images={composerImages}
        queuedMessages={queuedMessages}
        guideMessages={guideMessages}
        running={activeThreadIsRunning || viewSwitchPending}
        status={activeThreadReadOnly ? "子任务会话只读" : state.status}
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
        onSelectCodexModel={(nextModel) => void selectCodexModel(nextModel)}
        onSelectCodexEffort={(nextEffort) => void selectCodexEffort(nextEffort)}
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
        onPasteImageFiles={(files) => void attachComposerImageFiles(files)}
        onRemoveImage={removeComposerImage}
        onRemoveQueuedMessage={removeQueuedMessage}
        onRemoveGuideMessage={removeGuideMessage}
        onGuideQueuedMessage={(id) => void guideQueuedMessage(id)}
        onClearQueuedMessages={clearPendingComposerMessages}
        onSend={() => void sendPrompt()}
        onInterrupt={() => void interrupt()}
      />
    );
  }

  function renderThreadConversation(thread: Thread, pane: ConversationPaneID): JSX.Element {
    const paneTurns = thread.turns ?? [];
    const paneLatestAgentMessageID = latestAgentMessageItemID(paneTurns);
    const paneAskRequest = visibleAskRequestForThread(state.askRequests, thread.id);
    const paneAnsweredAskRequests = visibleAnsweredAskRequestsForThread(state.answeredAskRequests, thread.id);
    const paneAnsweredWithoutVisibleTurn = paneAnsweredAskRequests.filter(
      (request) => !request.turnID || !paneTurns.some((turn) => turn.id === request.turnID)
    );
    const active = state.activePane === pane;
    const closeLabel = pane === "secondary" ? "关闭右侧对话" : "关闭左侧对话";
    const draft = splitComposerDrafts[pane] ?? emptyComposerDraft();
    const paneRunning = isThreadRunning(thread);
    const paneReadOnly = Boolean(thread.read_only);
    const paneStatus = paneReadOnly ? "子任务会话只读" : paneRunning ? "运行中" : active && state.status !== "ready" ? state.status : "";
    return (
      <section
        className={`conversation-split-pane${active ? " active" : ""}`}
        aria-label={pane === "secondary" ? "分叉对话" : "源对话"}
        onPointerDown={() => activateConversationPane(pane)}
      >
        <div className="conversation-split-header">
          <div className="conversation-split-title">
            <span>{pane === "secondary" ? "分叉" : "源会话"}</span>
            <strong>{thread.preview || "新对话"}</strong>
          </div>
          <button className="icon-button conversation-split-close" type="button" aria-label={closeLabel} title={closeLabel} onClick={() => closeConversationPane(pane)}>
            <X size={16} />
          </button>
        </div>
        <div
          ref={(node) => {
            splitPaneRefs.current[pane] = node;
          }}
          className="conversation-split-body"
          onScroll={(event) => handleConversationScroll(event.currentTarget)}
        >
          <div className="conversation-width conversation-split-width">
            {paneTurns.map((turn) => (
              <Fragment key={turn.id}>
                <TurnView
                  turn={turn}
                  cwd={thread.cwd ?? state.activeContext?.cwd}
                  latestAgentMessageID={paneLatestAgentMessageID}
                  onStreamFrame={scheduleStreamScroll}
                  onForkMessage={(turnID, itemID) => void forkThreadFromMessage(thread, turnID, itemID)}
                />
                {paneAnsweredAskRequests
                  .filter((request) => request.turnID === turn.id)
                  .map((request) => (
                    <AnsweredAskUserMessage key={`answered-${request.id}`} request={request} />
                  ))}
              </Fragment>
            ))}
            {paneAnsweredWithoutVisibleTurn.map((request) => (
              <AnsweredAskUserMessage key={`answered-${request.id}`} request={request} />
            ))}
            {paneAskRequest ? (
              <AskUserMessage
                key={paneAskRequest.id}
                request={paneAskRequest}
                onCancel={(request) => respondToAskRequest(request, { answers: {}, cancelled: true })}
                onSubmit={(request, answers) => respondToAskRequest(request, { answers })}
              />
            ) : null}
          </div>
        </div>
        <SplitPaneComposer
          prompt={draft.prompt}
          setPrompt={(value) => setSplitComposerPrompt(pane, value)}
          images={draft.images}
          running={paneRunning || viewSwitchPending}
          readOnly={paneReadOnly}
          status={paneStatus}
          onPasteImageFiles={(files) => void attachSplitComposerImageFiles(pane, files)}
          onRemoveImage={(id) => removeSplitComposerImage(pane, id)}
          onSend={() => void sendPromptForPane(pane)}
          onInterrupt={() => void interruptPane(pane)}
        />
      </section>
    );
  }

  function clearViewSwitchDelay(): void {
    if (viewSwitchDelayTimerRef.current === undefined) {
      return;
    }
    window.clearTimeout(viewSwitchDelayTimerRef.current);
    viewSwitchDelayTimerRef.current = undefined;
  }

  function beginViewSwitch(kind: PendingViewSwitchKind, targetID: string): number {
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
        current?.kind === kind && current.targetID === targetID ? { ...current, visible: true } : current
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
    options: { resumeLatestThread?: boolean } = {}
  ): Promise<Partial<AppState>> {
    if (!projectState.active_context) {
      return emptyRuntimeState(projectState);
    }
    const resumeLatestThread = options.resumeLatestThread ?? true;
    const [initialized, gitStatus] = await Promise.all([window.wuu.initialize(), window.wuu.gitStatus()]);
    const listed = await window.wuu.listThreads();
    const listedThreads = sortThreads(listed.threads);
    const defaultThread = resumeLatestThread
      ? listedThreads.find((candidate) => !candidate.pinned) ?? listedThreads[0]
      : undefined;
    const thread = defaultThread
      ? requireThread(await window.wuu.resumeThread(defaultThread.id), "resume did not return a thread")
      : undefined;
    return {
      initialized,
      projects: projectState.projects,
      activeContext: projectState.active_context,
      activeProjectId: activeProjectID(projectState.active_context),
      gitStatus,
      thread,
      secondaryThread: undefined,
      activePane: "primary",
      allowThreadAutoActivation: Boolean(thread),
      threads: thread ? upsertThread(listedThreads, thread) : listedThreads,
      running: isThreadRunning(thread),
      status: "ready",
      askRequests: [],
      answeredAskRequests: []
    };
  }

  function emptyRuntimeState(projectState: ProjectListResult): Partial<AppState> {
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
      askRequests: [],
      answeredAskRequests: []
    };
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
    if (projectId === currentState.activeProjectId && currentState.activeContext?.kind === "project") {
      closeProjectMenus();
      setArchiveConfirmThreadID(undefined);
      if (currentState.thread || currentState.secondaryThread) {
        clearPendingComposerMessages();
        setState((current) => ({
          ...current,
          thread: undefined,
          secondaryThread: undefined,
          activePane: "primary",
          allowThreadAutoActivation: false,
          running: false,
          status: "ready"
        }));
      }
      return;
    }
    const requestID = beginViewSwitch("project", projectId);
    closeProjectMenus();
    try {
      const projectState = await window.wuu.selectProject(projectId);
      const loadedState = await loadRuntime(projectState, { resumeLatestThread: false });
      if (!finishViewSwitch(requestID)) {
        return;
      }
      clearPendingComposerMessages();
      setState((current) => ({
        ...current,
        ...loadedState,
        thread: undefined,
        secondaryThread: undefined,
        activePane: "primary",
        allowThreadAutoActivation: false,
        running: false,
        status: "ready"
      }));
    } catch (error) {
      if (!finishViewSwitch(requestID)) {
        return;
      }
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "open project failed"
      }));
    }
  }

  async function selectProjectForNewThread(projectId: string): Promise<void> {
    const currentState = appStateRef.current;
    if (projectId === currentState.activeProjectId && currentState.activeContext?.kind === "project") {
      closeProjectMenus();
      setArchiveConfirmThreadID(undefined);
      if (currentState.thread || currentState.secondaryThread) {
        clearPendingComposerMessages();
        setState((current) => ({
          ...current,
          thread: undefined,
          secondaryThread: undefined,
          activePane: "primary",
          allowThreadAutoActivation: false,
          running: false,
          status: "ready"
        }));
      }
      return;
    }
    if (isAnyThreadRunning(currentState)) {
      closeProjectMenus();
      setState((current) => ({
        ...current,
        status: "任务运行中，暂不能切换项目"
      }));
      return;
    }
    const requestID = beginViewSwitch("project", projectId);
    closeProjectMenus();
    setArchiveConfirmThreadID(undefined);
    try {
      const projectState = await window.wuu.selectProject(projectId);
      const loadedState = await loadRuntime(projectState, { resumeLatestThread: false });
      if (!finishViewSwitch(requestID)) {
        return;
      }
      clearPendingComposerMessages();
      setState((current) => ({
        ...current,
        ...loadedState,
        thread: undefined,
        secondaryThread: undefined,
        activePane: "primary",
        allowThreadAutoActivation: false,
        running: false,
        status: "ready"
      }));
    } catch (error) {
      if (!finishViewSwitch(requestID)) {
        return;
      }
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "open project failed"
      }));
    }
  }

  async function startNewThreadForProject(projectId: string): Promise<void> {
    cancelViewSwitch();
    closeProjectMenus();
    setArchiveConfirmThreadID(undefined);
    if (projectId === state.activeProjectId && state.activeContext?.kind === "project") {
      setPrompt("");
      setComposerImages([]);
      clearPendingComposerMessages();
      setState((current) => ({
        ...current,
        thread: undefined,
        secondaryThread: undefined,
        activePane: "primary",
        allowThreadAutoActivation: false,
        running: false,
        status: "ready"
      }));
      return;
    }
    const requestID = beginViewSwitch("project", projectId);
    try {
      const projectState = await window.wuu.selectProject(projectId);
      const loadedState = await loadRuntime(projectState, { resumeLatestThread: false });
      if (!finishViewSwitch(requestID)) {
        return;
      }
      setPrompt("");
      setComposerImages([]);
      clearPendingComposerMessages();
      setState((current) => ({ ...current, ...loadedState }));
    } catch (error) {
      if (!finishViewSwitch(requestID)) {
        return;
      }
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "open project failed"
      }));
    }
  }

  async function createBlankProject(): Promise<void> {
    const requestID = beginViewSwitch("runtime", "create-project");
    closeProjectMenus();
    try {
      const projectState = await window.wuu.createBlankProject();
      if (sameRuntimeContext(projectState.active_context, state.activeContext)) {
        if (!finishViewSwitch(requestID)) {
          return;
        }
        setState((current) => ({ ...current, projects: projectState.projects }));
        return;
      }
      const loadedState = await loadRuntime(projectState);
      if (!finishViewSwitch(requestID)) {
        return;
      }
      clearPendingComposerMessages();
      setState((current) => ({ ...current, ...loadedState }));
    } catch (error) {
      if (!finishViewSwitch(requestID)) {
        return;
      }
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "create project failed"
      }));
    }
  }

  async function chooseProjectFolder(): Promise<void> {
    const requestID = beginViewSwitch("runtime", "choose-project");
    closeProjectMenus();
    try {
      const projectState = await window.wuu.chooseProjectFolder();
      if (sameRuntimeContext(projectState.active_context, state.activeContext)) {
        if (!finishViewSwitch(requestID)) {
          return;
        }
        setState((current) => ({ ...current, projects: projectState.projects }));
        return;
      }
      const loadedState = await loadRuntime(projectState);
      if (!finishViewSwitch(requestID)) {
        return;
      }
      clearPendingComposerMessages();
      setState((current) => ({ ...current, ...loadedState }));
    } catch (error) {
      if (!finishViewSwitch(requestID)) {
        return;
      }
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "open folder failed"
      }));
    }
  }

  async function useNoProject(fresh: boolean): Promise<void> {
    if (!fresh && state.activeContext?.kind === "no_project") {
      closeProjectMenus();
      return;
    }
    const requestID = beginViewSwitch("runtime", fresh ? "no-project:fresh" : "no-project");
    closeProjectMenus();
    try {
      const projectState = await window.wuu.selectNoProject(fresh);
      const loadedState = await loadRuntime(projectState);
      if (!finishViewSwitch(requestID)) {
        return;
      }
      clearPendingComposerMessages();
      setState((current) => ({ ...current, ...loadedState }));
    } catch (error) {
      if (!finishViewSwitch(requestID)) {
        return;
      }
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "open no-project failed"
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
        status: current.status === "ready" ? "ready" : current.status
      }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "checkout branch failed"
      }));
    }
  }

  async function refreshGitStatus(): Promise<void> {
    if (!appStateRef.current.activeContext) {
      return;
    }
    if (gitRefreshInFlightRef.current) {
      gitRefreshQueuedRef.current = true;
      return;
    }
    gitRefreshInFlightRef.current = true;
    try {
      const gitStatus = await window.wuu.gitStatus();
      if (!appStateRef.current.activeContext) {
        return;
      }
      setState((current) => ({
        ...current,
        gitStatus,
        status: current.status === "ready" ? "ready" : current.status
      }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "refresh git status failed"
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

  async function createAndCheckoutBranch(branch: string): Promise<void> {
    if (!branch || anyThreadIsRunning) {
      return;
    }
    try {
      const result = await window.wuu.createCheckoutGitBranch(branch);
      setState((current) => ({
        ...current,
        gitStatus: result.status,
        status: current.status === "ready" ? "ready" : current.status
      }));
      setEnvironmentPanelMenu(null);
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "create branch failed"
      }));
      throw error;
    }
  }

  async function commitEnvironmentChanges(params: { message: string; includeUnstaged: boolean }): Promise<GitCommitResult> {
    const result = await window.wuu.commitGitChanges({
      message: params.message,
      include_unstaged: params.includeUnstaged
    });
    setState((current) => ({
      ...current,
      gitStatus: result.status,
      status: `已提交 ${result.commit}`
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
      draft: params.draft
    });
    setState((current) => ({
      ...current,
      gitStatus: result.status,
      status: result.already_exists ? "已有拉取请求" : "已创建拉取请求"
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

  function appendRunDebugEvent(entry: Omit<RunDebugEvent, "id" | "at">): void {
    const next: RunDebugEvent = {
      ...entry,
      id: ++runDebugEventIDRef.current,
      at: Date.now()
    };
    setRunDebugEvents((current) => [...current, next].slice(-80));
  }

  function resetRunDebugEvents(entry: Omit<RunDebugEvent, "id" | "at">): void {
    runDebugDeltaSeenRef.current.clear();
    const next: RunDebugEvent = {
      ...entry,
      id: ++runDebugEventIDRef.current,
      at: Date.now()
    };
    setRunDebugEvents([next]);
  }

  function recordRunDebugEvent(event: ServerEvent): void {
    const entry = runDebugEventFromServerEvent(event, runDebugDeltaSeenRef.current);
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
      composerImages
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
        tone: "error"
      });
    }
  }

  async function startNewThread(): Promise<void> {
    if (!state.activeContext) {
      return;
    }
    cancelViewSwitch();
    setArchiveConfirmThreadID(undefined);
    setPrompt("");
    setComposerImages([]);
    clearPendingComposerMessages();
    if (state.activeContext.kind === "no_project" && (state.thread || state.secondaryThread)) {
      await useNoProject(true);
      return;
    }
    setState((current) => ({
      ...current,
      thread: undefined,
      secondaryThread: undefined,
      activePane: "primary",
      allowThreadAutoActivation: false,
      running: false,
      status: "ready"
    }));
  }

  function seedAgentTreeDemo(): void {
    if (!state.activeContext || !state.initialized) {
      return;
    }
    cancelViewSwitch();
    setArchiveConfirmThreadID(undefined);
    setPrompt("");
    setComposerImages([]);
    clearPendingComposerMessages();
    const demo = createAgentTreeDemo(state.activeContext.cwd, state.initialized);
    const demoThreads = [demo.parent, ...demo.children];
    localDemoThreadsRef.current = new Map([
      ...localDemoThreadsRef.current,
      ...demoThreads.map((thread): [string, Thread] => [thread.id, thread])
    ]);
    setState((current) => ({
      ...current,
      thread: demo.parent,
      secondaryThread: undefined,
      activePane: "primary",
      allowThreadAutoActivation: true,
      threads: upsertThread(current.threads, demo.parent),
      running: false,
      status: "ready",
      askRequests: [],
      answeredAskRequests: []
    }));
  }

  function seedConversationFixture(kind: ConversationFixtureKind): void {
    if (!state.activeContext || !state.initialized) {
      return;
    }
    cancelViewSwitch();
    setArchiveConfirmThreadID(undefined);
    setPrompt("");
    setComposerImages([]);
    clearPendingComposerMessages();
    const thread = createConversationFixture(kind, state.activeContext.cwd, state.initialized);
    localDemoThreadsRef.current = new Map([...localDemoThreadsRef.current, [thread.id, thread]]);
    setState((current) => ({
      ...current,
      thread,
      secondaryThread: undefined,
      activePane: "primary",
      allowThreadAutoActivation: true,
      threads: upsertThread(current.threads, thread),
      running: isThreadRunning(thread),
      status: "ready",
      askRequests: [],
      answeredAskRequests: []
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
    if (threadId === activeThreadID) {
      if (pendingViewSwitch) {
        cancelViewSwitch();
      }
      return;
    }
    if (pendingViewSwitch?.kind === "thread" && pendingViewSwitch.targetID === threadId) {
      return;
    }
    setArchiveConfirmThreadID(undefined);
    const demoThread = localDemoThreadsRef.current.get(threadId);
    if (demoThread) {
      cancelViewSwitch();
      clearPendingComposerMessages();
      setState((current) => ({
        ...current,
        thread: demoThread,
        secondaryThread: undefined,
        activePane: "primary",
        allowThreadAutoActivation: true,
        threads: upsertThread(current.threads, demoThread),
        running: isThreadRunning(demoThread),
        status: "ready",
        askRequests: [],
        answeredAskRequests: []
      }));
      return;
    }
    const sourceContext = state.activeContext;
    const requestID = beginViewSwitch("thread", threadId);
    try {
      const thread = requireThread(await window.wuu.resumeThread(threadId), "resume did not return a thread");
      if (!finishViewSwitch(requestID) || !sameRuntimeContext(appStateRef.current.activeContext, sourceContext)) {
        return;
      }
      clearPendingComposerMessages();
      setState((current) => ({
        ...current,
        thread,
        secondaryThread: undefined,
        activePane: "primary",
        allowThreadAutoActivation: true,
        threads: upsertThread(current.threads, thread),
        running: isThreadRunning(thread),
        status: "ready"
      }));
    } catch (error) {
      if (!finishViewSwitch(requestID) || !sameRuntimeContext(appStateRef.current.activeContext, sourceContext)) {
        return;
      }
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "load failed"
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
    if (pendingViewSwitch?.kind === "thread" && pendingViewSwitch.targetID === agent.id) {
      return;
    }
    setArchiveConfirmThreadID(undefined);
    const sourceContext = state.activeContext;
    const requestID = beginViewSwitch("thread", agent.id);
    try {
      const thread =
        localDemoThreadsRef.current.get(agent.id) ??
        requireThread(await window.wuu.resumeThread(agent.id), "resume did not return a child agent thread");
      if (!finishViewSwitch(requestID) || !sameRuntimeContext(appStateRef.current.activeContext, sourceContext)) {
        return;
      }
      setPrompt("");
      setComposerImages([]);
      clearPendingComposerMessages();
      setState((current) => ({
        ...current,
        thread,
        secondaryThread: undefined,
        activePane: "primary",
        allowThreadAutoActivation: true,
        threads: upsertThread(current.threads, thread),
        running: false,
        status: "ready"
      }));
    } catch (error) {
      if (!finishViewSwitch(requestID) || !sameRuntimeContext(appStateRef.current.activeContext, sourceContext)) {
        return;
      }
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "load child agent failed"
      }));
    }
  }

  function activateConversationPane(pane: ConversationPaneID): void {
    setState((current) => {
      if (pane === "secondary" && !current.secondaryThread) {
        return current;
      }
      const thread = pane === "secondary" ? current.secondaryThread : current.thread;
      return { ...current, activePane: pane, running: isThreadRunning(thread) };
    });
  }

  function closeConversationPane(pane: ConversationPaneID): void {
    clearPendingComposerMessages();
    moveSplitDraftToGlobalComposer(pane === "secondary" ? "primary" : "secondary");
    setState((current) => {
      if (pane === "secondary") {
        return {
          ...current,
          secondaryThread: undefined,
          activePane: "primary",
          running: isThreadRunning(current.thread),
          status: "ready"
        };
      }
      if (current.secondaryThread) {
        return {
          ...current,
          thread: current.secondaryThread,
          secondaryThread: undefined,
          activePane: "primary",
          running: isThreadRunning(current.secondaryThread),
          status: "ready"
        };
      }
      return {
        ...current,
        thread: undefined,
        activePane: "primary",
        running: false,
        status: "ready"
      };
    });
  }

  async function forkThreadFromMessage(sourceThread: Thread, turnID: string, itemID: string): Promise<void> {
    if (!state.activeContext || sourceThread.read_only) {
      return;
    }
    if (localDemoThreadsRef.current.has(sourceThread.id)) {
      setState((current) => ({ ...current, status: "示例会话不能分叉" }));
      return;
    }
    setArchiveConfirmThreadID(undefined);
    setState((current) => ({ ...current, status: "正在分叉会话" }));
    try {
      const fork = requireThread(
        await window.wuu.forkThread(sourceThread.id, turnID, itemID),
        "thread/fork did not return a thread"
      );
      conversationAutoFollowRef.current = true;
      const currentState = appStateRef.current;
      const sourcePane = currentState.secondaryThread?.id === sourceThread.id ? "secondary" : "primary";
      const sourceDraft = splitConversation
        ? cloneComposerDraft(splitComposerDrafts[sourcePane] ?? emptyComposerDraft())
        : { prompt, images: composerImages.map((image) => ({ ...image })) };
      setPrompt("");
      setComposerImages([]);
      setSplitComposerDrafts({
        primary: sourceDraft,
        secondary: emptyComposerDraft()
      });
      setState((current) => {
        const source =
          current.secondaryThread?.id === sourceThread.id
            ? current.secondaryThread
            : current.thread?.id === sourceThread.id
              ? current.thread
              : sourceThread;
        return {
          ...current,
          thread: source,
          secondaryThread: fork,
          activePane: "secondary",
          allowThreadAutoActivation: true,
          threads: upsertThread(upsertThread(current.threads, source), fork),
          running: isThreadRunning(fork),
          status: "ready"
        };
      });
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "fork failed"
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
      localDemoThreadsRef.current = new Map([...localDemoThreadsRef.current, [thread.id, nextThread]]);
      setState((current) => ({
        ...current,
        thread: current.thread?.id === thread.id ? nextThread : current.thread,
        secondaryThread: current.secondaryThread?.id === thread.id ? nextThread : current.secondaryThread,
        threads: upsertThread(current.threads, nextThread),
        status: current.status === "ready" ? "ready" : current.status
      }));
      return;
    }
    try {
      const result = await window.wuu.pinThread(thread.id, !thread.pinned);
      setState((current) => ({
        ...current,
        thread: current.thread?.id === thread.id ? result.thread : current.thread,
        secondaryThread: current.secondaryThread?.id === thread.id ? result.thread : current.secondaryThread,
        threads: upsertThread(current.threads, result.thread),
        status: current.status === "ready" ? "ready" : current.status
      }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "pin thread failed"
      }));
    }
  }

  async function archiveThread(thread: Thread): Promise<void> {
    const isLocalDemoThread = localDemoThreadsRef.current.has(thread.id);
    if (!state.activeContext || (!isLocalDemoThread && isThreadRunning(thread))) {
      return;
    }
    if (archiveConfirmThreadID !== thread.id) {
      setArchiveConfirmThreadID(thread.id);
      return;
    }
    clearPendingComposerMessages();
    if (isLocalDemoThread) {
      localDemoThreadsRef.current = new Map(
        [...localDemoThreadsRef.current].filter(([threadID]) => threadID !== thread.id)
      );
      setArchiveConfirmThreadID(undefined);
      setState((current) => ({
        ...current,
        thread: current.thread?.id === thread.id ? undefined : current.thread,
        secondaryThread: current.secondaryThread?.id === thread.id ? undefined : current.secondaryThread,
        activePane: current.activePane === "secondary" && current.secondaryThread?.id === thread.id ? "primary" : current.activePane,
        threads: current.threads.filter((candidate) => candidate.id !== thread.id),
        running: activeThreadIDForState(current) === thread.id ? false : current.running,
        status: "ready"
      }));
      return;
    }
    try {
      const result = await window.wuu.archiveThread(thread.id, true);
      setArchiveConfirmThreadID(undefined);
      setState((current) => ({
        ...current,
        thread: current.thread?.id === thread.id ? undefined : current.thread,
        secondaryThread: current.secondaryThread?.id === thread.id ? undefined : current.secondaryThread,
        activePane: current.activePane === "secondary" && current.secondaryThread?.id === thread.id ? "primary" : current.activePane,
        threads: current.threads.filter((candidate) => candidate.id !== result.thread.id),
        running: activeThreadIDForState(current) === thread.id ? false : current.running,
        status: "ready"
      }));
    } catch (error) {
      setArchiveConfirmThreadID(undefined);
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "archive thread failed"
      }));
    }
  }

  async function sendPrompt(): Promise<void> {
    if (viewSwitchPending) {
      return;
    }
    const message = createComposerMessage(prompt, composerImages);
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
    if (isStateActiveThreadRunning(currentState)) {
      enqueueComposerMessage(message);
      return;
    }
    await sendComposerMessage(message, true);
  }

  async function sendComposerMessage(message: QueuedComposerMessage, restoreDraftOnError = false): Promise<boolean> {
    const currentState = appStateRef.current;
    const targetThread = activeThreadForState(currentState);
    const targetPane: ConversationPaneID =
      currentState.activePane === "secondary" && currentState.secondaryThread ? "secondary" : "primary";
    const text = message.text.trim();
    const images = inputImagesFromComposer(message.images);
    if (
      (!text && images.length === 0) ||
      !currentState.activeContext ||
      !currentState.initialized ||
      targetThread?.read_only ||
      viewSwitchPending ||
      isStateActiveThreadRunning(currentState)
    ) {
      return false;
    }
    conversationAutoFollowRef.current = true;
    resetRunDebugEvents({
      source: "client",
      method: "client/send",
      detail: images.length > 0 ? `已提交输入，包含 ${images.length} 张图片` : "已提交输入",
      tone: "running",
      threadID: targetThread?.id
    });
    appStateRef.current = { ...currentState, running: true, status: "正在发送请求" };
    setState((current) => ({ ...current, running: true, status: "正在发送请求" }));
    try {
      const thread =
        targetThread ?? requireThread(await window.wuu.startThread(), "thread/start did not return a thread");
      appStateRef.current = {
        ...setThreadForPane(appStateRef.current, targetPane, thread),
        activePane: targetPane,
        allowThreadAutoActivation: true,
        threads: upsertThread(appStateRef.current.threads, thread)
      };
      setState((current) => ({
        ...setThreadForPane(current, targetPane, thread),
        activePane: targetPane,
        allowThreadAutoActivation: true,
        threads: upsertThread(current.threads, thread)
      }));
      const result = await window.wuu.startTurn(thread.id, text, images);
      setState((current) => updateThreadByID(setThreadForPane(current, targetPane, thread), thread.id, (currentThread) =>
        upsertTurn(currentThread, result.turn)
      ));
      appendRunDebugEvent({
        source: "client",
        method: "turn/start response",
        detail: "服务端已接受本轮请求",
        tone: "running",
        threadID: thread.id,
        turnID: result.turn.id
      });
    } catch (error) {
      const rawMessage = rawErrorMessage(error, "send failed");
      const errorMessage = statusMessageForError(rawMessage, "send failed");
      appendRunDebugEvent({
        source: "client",
        method: "turn/start failed",
        detail: rawMessage,
        tone: "error",
        threadID: targetThread?.id
      });
      appStateRef.current = { ...appStateRef.current, running: false, status: errorMessage };
      setState((current) => ({
        ...current,
        running: false,
        status: errorMessage
      }));
      if (restoreDraftOnError) {
        setPrompt(message.text);
        setComposerImages(message.images);
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
    const message = createComposerMessage(draft.prompt, draft.images);
    const currentState = appStateRef.current;
    const targetThread = threadForPane(currentState, pane);
    if (targetThread?.read_only) {
      setState((current) => ({ ...current, status: "子任务会话只读" }));
      return;
    }
    if (!message || !targetThread || !currentState.activeContext || !currentState.initialized) {
      return;
    }
    if (isThreadRunning(targetThread)) {
      setState((current) => ({ ...current, activePane: pane, status: "该分支正在运行" }));
      return;
    }
    setSplitComposerDrafts((current) => ({
      ...current,
      [pane]: emptyComposerDraft()
    }));
    const sent = await sendComposerMessageToPane(message, pane);
    if (!sent) {
      setSplitComposerDrafts((current) => ({
        ...current,
        [pane]: {
          prompt: message.text,
          images: message.images.map((image) => ({ ...image }))
        }
      }));
    }
  }

  async function sendComposerMessageToPane(message: QueuedComposerMessage, pane: ConversationPaneID): Promise<boolean> {
    const currentState = appStateRef.current;
    const targetThread = threadForPane(currentState, pane);
    const text = message.text.trim();
    const images = inputImagesFromComposer(message.images);
    if (
      (!text && images.length === 0) ||
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
      detail: images.length > 0 ? `已提交输入，包含 ${images.length} 张图片` : "已提交输入",
      tone: "running",
      threadID: targetThread.id
    });
    appStateRef.current = { ...currentState, activePane: pane, running: true, status: "正在发送请求" };
    setState((current) => ({ ...current, activePane: pane, running: true, status: "正在发送请求" }));
    try {
      const result = await window.wuu.startTurn(targetThread.id, text, images);
      setState((current) =>
        updateThreadByID({ ...current, activePane: pane }, targetThread.id, (thread) => upsertTurn(thread, result.turn))
      );
      appendRunDebugEvent({
        source: "client",
        method: "turn/start response",
        detail: "服务端已接受本轮请求",
        tone: "running",
        threadID: targetThread.id,
        turnID: result.turn.id
      });
    } catch (error) {
      const rawMessage = rawErrorMessage(error, "send failed");
      const errorMessage = statusMessageForError(rawMessage, "send failed");
      appendRunDebugEvent({
        source: "client",
        method: "turn/start failed",
        detail: rawMessage,
        tone: "error",
        threadID: targetThread.id
      });
      appStateRef.current = { ...appStateRef.current, activePane: pane, running: false, status: errorMessage };
      setState((current) => ({
        ...current,
        activePane: pane,
        running: false,
        status: errorMessage
      }));
      return false;
    }
    return true;
  }

  async function drainQueuedMessages(): Promise<void> {
    if (drainingQueueRef.current || queueDrainPausedRef.current) {
      return;
    }
    const currentState = appStateRef.current;
    if (isAnyThreadRunning(currentState) || !currentState.activeContext || !currentState.initialized) {
      return;
    }

    const guidesToSend = guideMessagesRef.current;
    let message: QueuedComposerMessage | undefined;
    let restoreGuides: QueuedComposerMessage[] = [];
    let restoreQueued: QueuedComposerMessage | undefined;

    if (guidesToSend.length > 0) {
      restoreGuides = guidesToSend;
      message = mergeGuideMessages(guidesToSend);
      setGuideMessagesNow([]);
    } else if (queuedMessagesRef.current.length > 0) {
      const [nextMessage, ...remainingMessages] = queuedMessagesRef.current;
      message = nextMessage;
      restoreQueued = nextMessage;
      setQueuedMessagesNow(remainingMessages);
    }

    if (!message) {
      return;
    }

    drainingQueueRef.current = true;
    const sent = await sendComposerMessage(message);
    drainingQueueRef.current = false;
    if (!sent) {
      queueDrainPausedRef.current = true;
      if (restoreGuides.length > 0) {
        setGuideMessagesNow([...restoreGuides, ...guideMessagesRef.current]);
      } else if (restoreQueued) {
        setQueuedMessagesNow([restoreQueued, ...queuedMessagesRef.current]);
      }
      setState((current) => ({
        ...current,
        status: "队列暂停"
      }));
    }
  }

  async function updateRuntimeSettings(provider: string, model: string, effort?: string): Promise<void> {
    const nextProvider = provider.trim();
    const nextModel = model.trim();
    const nextEffort = effort === undefined ? undefined : effort.trim();
    if (
      !nextProvider ||
      !nextModel ||
      !state.initialized ||
      anyThreadIsRunning ||
      (nextProvider === state.initialized.provider &&
        nextModel === state.initialized.model &&
        (nextEffort === undefined || nextEffort === (state.initialized.effort ?? "")))
    ) {
      return;
    }
    try {
      const updated = await window.wuu.updateRuntimeSettings(nextProvider, nextModel, nextEffort);
      setState((current) => {
        const initialized = current.initialized
          ? {
              ...current.initialized,
              provider: updated.provider,
              model: updated.model,
              effort: updated.effort ?? "",
              providers: updated.providers ?? current.initialized.providers
            }
          : current.initialized;
        const updateThreadModel = (thread: Thread): Thread => ({
          ...thread,
          model_provider: updated.provider,
          model: updated.model
        });
        const thread = current.thread ? updateThreadModel(current.thread) : current.thread;
        return {
          ...current,
          initialized,
          thread,
          threads: current.threads.map(updateThreadModel),
          status: current.status === "ready" ? current.status : "ready"
        };
      });
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "update runtime settings failed"
      }));
      throw error;
    }
  }

  function toggleCodexRuntimeMenu(menu: Exclude<CodexRuntimeMenu, null>): void {
    if (!state.initialized || anyThreadIsRunning || !isCodexProvider(state.initialized)) {
      return;
    }
    setRuntimeMenuOpen(false);
    setAccessMenuOpen(false);
    setModeMenuOpen(false);
    setBranchMenuOpen(false);
    setCodexRuntimeMenu((current) => (current === menu ? null : menu));
    void loadCodexModelsForProvider(state.initialized.provider);
  }

  async function loadCodexModelsForProvider(provider: string): Promise<void> {
    if (!provider) {
      return;
    }
    if (codexModels.provider === provider && (codexModels.loading || codexModels.models.length > 0)) {
      return;
    }
    setCodexModels({ provider, loading: true, error: "", models: [] });
    try {
      const result = await window.wuu.loadCodexModels(provider);
      setCodexModels({
        provider: result.provider,
        loading: false,
        error: "",
        models: result.models
      });
      setState((current) => {
        if (!current.initialized || current.initialized.provider !== result.provider) {
          return current;
        }
        return {
          ...current,
          initialized: {
            ...current.initialized,
            model: result.model,
            effort: result.effort ?? ""
          }
        };
      });
    } catch (error) {
      setCodexModels({
        provider,
        loading: false,
        error: error instanceof Error ? error.message : "无法加载 Codex 模型",
        models: []
      });
    }
  }

  async function selectCodexModel(nextModel: CodexModelSummary): Promise<void> {
    if (!state.initialized || anyThreadIsRunning) {
      return;
    }
    const nextEffort = normalizedEffortForModel(state.initialized.effort ?? "", nextModel);
    await updateRuntimeSettings(state.initialized.provider, nextModel.slug, nextEffort);
    setCodexRuntimeMenu(null);
  }

  async function selectCodexEffort(nextEffort: string): Promise<void> {
    if (!state.initialized || anyThreadIsRunning) {
      return;
    }
    await updateRuntimeSettings(state.initialized.provider, state.initialized.model, nextEffort);
    setCodexRuntimeMenu(null);
  }

  async function interrupt(): Promise<void> {
    const thread = activeThreadForState(appStateRef.current);
    if (!thread) {
      return;
    }
    await window.wuu.interruptTurn(thread.id);
  }

  async function interruptPane(pane: ConversationPaneID): Promise<void> {
    const thread = threadForPane(appStateRef.current, pane);
    if (!thread) {
      return;
    }
    await window.wuu.interruptTurn(thread.id);
  }

  async function respondToAskRequest(request: AskRequestState, response: AskUserResponse): Promise<void> {
    try {
      await window.wuu.respondToServerRequest(request.id, response);
      const currentThread = activeThreadForState(appStateRef.current);
      const answeredRequest: AnsweredAskRequestState = {
        ...request,
        threadID: request.threadID ?? currentThread?.id,
        turnID: activeDebugTurn(currentThread)?.id,
        answers: response.answers ?? {},
        cancelled: response.cancelled === true
      };
      setState((current) => ({
        ...current,
        askRequests: removeAskRequest(current.askRequests, request.id),
        answeredAskRequests: upsertAnsweredAskRequest(current.answeredAskRequests, answeredRequest)
      }));
    } catch (error) {
      setState((current) => ({
        ...current,
        askRequests: upsertAskRequest(current.askRequests, request),
        status: desktopApiErrorMessage(error, "提交选择失败")
      }));
    }
  }

  if (settingsOpen) {
    return (
      <SettingsView
        initialized={state.initialized}
        running={anyThreadIsRunning}
        showDebugControlsSetting={ENABLE_DEBUG_CONTROL_SETTING}
        debugControlsEnabled={debugControlsEnabled}
        onBack={() => setSettingsOpen(false)}
        onSave={updateRuntimeSettings}
        onDebugControlsChange={setDebugControlsEnabled}
      />
    );
  }

  const environmentPanelNode =
    (environmentPanelVisible || environmentPanelMounted) && state.initialized ? (
      <EnvironmentPanel
        panelRef={environmentPanelRef}
        motionState={environmentPanelClosing ? "closing" : environmentPanelMotionState}
        initialized={state.initialized}
        gitStatus={state.gitStatus}
        activeContext={state.activeContext}
        activeProject={activeProject}
        planUpdate={activePlanUpdate}
        sourceItems={environmentSourceItems}
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
      />
    ) : null;

  return (
    <div className={shellClassName} style={shellStyle}>
      <aside className="sidebar">
        <div className="sidebar-content">
          <div className="traffic-spacer" />
          <nav className="primary-nav" aria-label="主导航">
            <button className="nav-item" onClick={() => void startNewThread()} disabled={!state.activeContext}>
              <MessageSquarePlus size={18} />
              <span>新对话</span>
            </button>
            {debugControlsVisible && ENABLE_CONVERSATION_FIXTURES ? (
              <div className="dev-fixture-nav" aria-label="开发调试会话">
                <div className="dev-fixture-label">开发样例</div>
                <button
                  className="nav-item dev-fixture-button"
                  onClick={() => seedConversationFixture("long")}
                  disabled={!state.activeContext || !state.initialized}
                >
                  <FileText size={17} />
                  <span>长对话</span>
                </button>
                <button
                  className="nav-item dev-fixture-button"
                  onClick={() => seedConversationFixture("rich")}
                  disabled={!state.activeContext || !state.initialized}
                >
                  <ListIcon size={17} />
                  <span>富内容</span>
                </button>
                <button
                  className="nav-item dev-fixture-button"
                  onClick={() => seedConversationFixture("running")}
                  disabled={!state.activeContext || !state.initialized}
                >
                  <Clock size={17} />
                  <span>运行中</span>
                </button>
                <button
                  className="nav-item dev-fixture-button"
                  onClick={() => seedConversationFixture("compact")}
                  disabled={!state.activeContext || !state.initialized}
                >
                  <Archive size={17} />
                  <span>上下文压缩</span>
                </button>
                <button
                  className="nav-item dev-fixture-button"
                  onClick={seedAgentTreeDemo}
                  disabled={!state.activeContext || !state.initialized}
                >
                  <CornerDownRight size={17} />
                  <span>子任务</span>
                </button>
              </div>
            ) : null}
          </nav>

          {sidebarPinnedThreads.length > 0 ? (
            <section className="pinned-thread-section" aria-label="置顶">
              <div className="section-label pinned-thread-label">置顶</div>
              <PinnedThreadList
                threads={sidebarPinnedThreads}
                activeID={activeThreadID}
                pendingThreadID={visiblePendingThreadID}
                pendingAskThreadIDs={pendingAskThreadIDs}
                archiveConfirmThreadID={archiveConfirmThreadID}
                onSelect={(id) => void selectThread(id)}
                onSelectChildAgent={(agent) => void selectChildAgent(agent)}
                onTogglePinned={(thread) => void toggleThreadPinned(thread)}
                onArchive={(thread) => void archiveThread(thread)}
                onClearArchiveConfirm={(id) =>
                  setArchiveConfirmThreadID((current) => (current === id ? undefined : current))
                }
              />
            </section>
          ) : null}

          <section className="project-list" aria-label="项目">
            <div className="project-section-header" ref={projectMenuRef}>
              <div className="section-label">项目</div>
              <button
                className="project-add-button"
                aria-label="添加项目"
                aria-haspopup="menu"
                aria-expanded={projectMenuOpen}
                onClick={() => setProjectMenuOpen((open) => !open)}
              >
                <FolderPlus size={20} />
              </button>
              {projectMenuOpen ? (
                <div className="project-add-menu" role="menu">
                  <button role="menuitem" onClick={() => void createBlankProject()}>
                    <FolderPlus size={22} />
                    <span>新建空白项目</span>
                  </button>
                  <button role="menuitem" onClick={() => void chooseProjectFolder()}>
                    <FolderOpen size={22} />
                    <span>使用现有文件夹</span>
                  </button>
                </div>
              ) : null}
            </div>
            {state.projects.length === 0 ? <div className="project-empty-note">还没有项目</div> : null}
            <ProjectList
              projects={state.projects}
              activeID={state.activeProjectId}
              pendingProjectID={visiblePendingProjectID}
              collapsedProjectIDs={collapsedProjectIDs}
              collapsingProjectIDs={collapsingProjectIDs}
              threads={state.threads}
              activeThreadID={activeThreadID}
              pendingThreadID={visiblePendingThreadID}
              pendingAskThreadIDs={pendingAskThreadIDs}
              archiveConfirmThreadID={archiveConfirmThreadID}
              onSelectProject={(id) => void openProject(id)}
              onToggleProjectCollapsed={toggleProjectCollapsed}
              onStartNewThread={(id) => void startNewThreadForProject(id)}
              onSelectThread={(id) => void selectThread(id)}
              onSelectChildAgent={(agent) => void selectChildAgent(agent)}
              onToggleThreadPinned={(thread) => void toggleThreadPinned(thread)}
              onArchiveThread={(thread) => void archiveThread(thread)}
              onClearArchiveConfirm={(id) =>
                setArchiveConfirmThreadID((current) => (current === id ? undefined : current))
              }
            />
          </section>
          <div className="sidebar-settings">
            <button
              className="settings-button"
              type="button"
              disabled={!state.initialized}
              onClick={() => {
                setProjectMenuOpen(false);
                setRuntimeMenuOpen(false);
                setCodexRuntimeMenu(null);
                setSettingsOpen(true);
              }}
            >
              <Settings size={18} />
              <span>设置</span>
            </button>
          </div>
        </div>
      </aside>

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

      <main
        className={`conversation-pane${environmentPanelVisible ? " environment-panel-visible" : ""}${
          environmentPanelReserved ? " environment-panel-reserved" : ""
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
            {workspaceMode ? (
              <span className="workspace-title-icon" aria-hidden="true">
                <WorkspaceToolIcon view={workspaceMode} size={18} />
              </span>
            ) : null}
            <h1>{activeTitle}</h1>
          </div>
          <div className="title-actions">
            {debugControlsVisible && ENABLE_SWISS_STYLE_TOGGLE ? (
              <button
                className={`launch-preview-button style-toggle-button${swissStyleEnabled ? " active" : ""}`}
                type="button"
                aria-label={swissStyleEnabled ? "关闭瑞士国际主义风格" : "开启瑞士国际主义风格"}
                aria-pressed={swissStyleEnabled}
                title="开发专用：切换瑞士国际主义风格"
                onClick={() => setSwissStyleEnabled((enabled) => !enabled)}
              >
                <Square size={15} />
                <span>Swiss</span>
              </button>
            ) : null}
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
                    copied={runDebugCopied}
                    onCopy={() => void copyRunDebugInfo()}
                    onClose={() => setRunDebugOpen(false)}
                  />
                ) : null}
              </div>
            ) : null}
            <button
              ref={environmentToggleRef}
              className={`icon-button environment-toggle-button${environmentPanelVisible ? " active" : ""}`}
              type="button"
              aria-label={environmentPanelVisible ? "隐藏环境信息" : "显示环境信息"}
              aria-pressed={environmentPanelVisible}
              onClick={toggleEnvironmentPanel}
            >
              <Info size={18} />
            </button>
            <button
              className={`icon-button workspace-toggle-button${bottomPanelOpen ? " active" : ""}`}
              type="button"
              aria-label={bottomPanelOpen ? "关闭底部栏" : "打开底部栏"}
              aria-pressed={bottomPanelOpen}
              onClick={() => setBottomPanelOpen((open) => !open)}
            >
              <PanelBottomOpen size={18} />
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

        {debugControlsVisible && ENABLE_TURN_PROGRESS_EXPERIMENT && turnProgressPreviewOpen ? (
          <TurnProgressPreviewOverlay onClose={() => setTurnProgressPreviewOpen(false)} />
        ) : null}

        {environmentPanelNode}

        {pendingViewSwitch?.visible ? <ViewSwitchLoading /> : null}

        {state.initialized && !previewingLaunch ? (
          <div
            className={`scroll-region${emptyConversation && !showingWorkspaceMode ? " empty-scroll-region" : ""}${
              showingWorkspaceMode ? " workspace-scroll-region" : ""
            }${splitConversation ? " split-scroll-region" : ""}`}
            onScroll={(event) => handleConversationScroll(event.currentTarget)}
            ref={conversationScrollRef}
          >
            {workspaceMode ? (
              <WorkspaceMainPanel
                view={workspaceMode}
                activeContext={state.activeContext}
                gitStatus={state.gitStatus}
                selectedFilePath={selectedWorkspaceFile}
                onOpenRightPanel={() => {
                  ensureWorkspaceToolTab(workspaceMode);
                  activateWorkspaceTool(workspaceMode);
                  setRightPanelOpenWithMotion(true);
                }}
              />
            ) : splitConversation && state.thread && state.secondaryThread ? (
              <div className="conversation-split">
                {renderThreadConversation(state.thread, "primary")}
                {renderThreadConversation(state.secondaryThread, "secondary")}
              </div>
            ) : emptyConversation ? (
              <EmptyConversationHome title={emptyThreadTitle}>
                {renderComposer("hero")}
              </EmptyConversationHome>
            ) : (
              <div className="conversation-width">
                {turns.map((turn) => (
                  <Fragment key={turn.id}>
                    <TurnView
                      turn={turn}
                      cwd={activeThread?.cwd ?? state.activeContext?.cwd}
                      latestAgentMessageID={latestAgentMessageID}
                      onStreamFrame={scheduleStreamScroll}
                      onForkMessage={
                        activeThread ? (turnID, itemID) => void forkThreadFromMessage(activeThread, turnID, itemID) : undefined
                      }
                    />
                    {visibleAnsweredAskRequests
                      .filter((request) => request.turnID === turn.id)
                      .map((request) => (
                        <AnsweredAskUserMessage key={`answered-${request.id}`} request={request} />
                      ))}
                  </Fragment>
                ))}
                {answeredAskRequestsWithoutVisibleTurn.map((request) => (
                  <AnsweredAskUserMessage key={`answered-${request.id}`} request={request} />
                ))}
                {visibleAskRequest ? (
                  <AskUserMessage
                    key={visibleAskRequest.id}
                    request={visibleAskRequest}
                    onCancel={(request) => respondToAskRequest(request, { answers: {}, cancelled: true })}
                    onSubmit={(request, answers) => respondToAskRequest(request, { answers })}
                  />
                ) : null}
              </div>
            )}
          </div>
        ) : (
          <RuntimeLoading
            status={state.status}
            pinned={previewingLaunch}
            onExitPreview={() => setLaunchPreviewPinned(false)}
          />
        )}

        {state.initialized && !previewingLaunch && !emptyConversation && !showingWorkspaceMode && !splitConversation
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
        selectedFilePath={selectedWorkspaceFile}
        onSelectView={openWorkspaceTool}
        onShowTools={showWorkspaceToolPicker}
        onCloseTab={closeWorkspaceToolTab}
        onOpenFile={openWorkspaceFile}
        onClose={() => setRightPanelOpenWithMotion(false)}
      />
      <WorkspaceBottomPanel
        open={bottomPanelOpen}
        selectedView={workspacePanelView}
        onSelectTool={openWorkspaceTool}
        onClose={() => setBottomPanelOpen(false)}
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

    </div>
  );
}

function RunDebugPanel({
  state,
  phase,
  events,
  queuedMessages,
  guideMessages,
  composerImages,
  copied,
  onCopy,
  onClose
}: {
  state: AppState;
  phase: RunDebugPhase;
  events: RunDebugEvent[];
  queuedMessages: QueuedComposerMessage[];
  guideMessages: QueuedComposerMessage[];
  composerImages: ComposerImage[];
  copied: boolean;
  onCopy: () => void;
  onClose: () => void;
}): JSX.Element {
  const thread = activeThreadForState(state);
  const turn = phase.turn ?? activeDebugTurn(thread);
  const lastEvent = events.length > 0 ? events[events.length - 1] : undefined;
  const turnStartedAt = turn ? parseTurnTimestampMs(turn.started_at) : NaN;
  const model = state.initialized
    ? `${state.initialized.provider} / ${state.initialized.model}${state.initialized.effort ? ` / ${state.initialized.effort}` : ""}`
    : "未初始化";
  const queueDetail = [
    queuedMessages.length > 0 ? `排队 ${queuedMessages.length}` : "",
    guideMessages.length > 0 ? `引导 ${guideMessages.length}` : "",
    composerImages.length > 0 ? `图片 ${composerImages.length}` : ""
  ]
    .filter(Boolean)
    .join("，");

  return (
    <aside className="run-debug-panel" aria-label="调试信息">
      <div className="run-debug-header">
        <div>
          <span className={`run-debug-phase ${phase.tone}`}>{phase.label}</span>
          <strong>{phase.detail}</strong>
        </div>
        <div className="run-debug-actions">
          <button className="icon-button" type="button" aria-label="复制调试信息" onClick={onCopy}>
            <Copy size={15} />
          </button>
          <button className="icon-button" type="button" aria-label="关闭调试信息" onClick={onClose}>
            <X size={15} />
          </button>
        </div>
      </div>

      <div className="run-debug-scroll">
        {copied ? <div className="run-debug-copied">已复制诊断信息</div> : null}
        <section className="run-debug-section">
          <h3>当前状态</h3>
          <RunDebugRow label="运行" value={state.running ? "running" : state.status || "ready"} />
          <RunDebugRow label="模型" value={model} />
          <RunDebugRow label="工作区" value={state.activeContext?.cwd ?? thread?.cwd ?? "未连接"} />
          <RunDebugRow label="Thread" value={thread ? shortDebugID(thread.id) : "无"} />
          <RunDebugRow
            label="Turn"
            value={
              turn ? (
                <>
                  {shortDebugID(turn.id)} · {debugTurnStatusLabel(turn.status)} ·{" "}
                  {typeof turn.duration_ms === "number"
                    ? formatDuration(turn.duration_ms)
                    : turn.status === "in_progress" && Number.isFinite(turnStartedAt)
                      ? <LiveDuration startedAtMs={turnStartedAt} />
                      : "未知耗时"}
                </>
              ) : (
                "无"
              )
            }
          />
          <RunDebugRow
            label="最后事件"
            value={
              lastEvent ? (
                <>
                  {lastEvent.method} · <LiveSince atMs={lastEvent.at} />
                </>
              ) : (
                "暂无"
              )
            }
          />
          {queueDetail ? <RunDebugRow label="待发送" value={queueDetail} /> : null}
        </section>

        <section className="run-debug-section">
          <h3>本轮 Item</h3>
          {turn?.items.length ? (
            <div className="run-debug-items">
              {turn.items.map((item) => (
                <RunDebugItem key={item.id} turnID={turn.id} item={item} />
              ))}
            </div>
          ) : (
            <div className="run-debug-empty">还没有收到 turn/item。</div>
          )}
        </section>

        <section className="run-debug-section">
          <h3>事件时间线</h3>
          {events.length > 0 ? (
            <div className="run-debug-events">
              {events
                .slice(-24)
                .reverse()
                .map((event) => (
                  <div className={`run-debug-event ${event.tone}`} key={event.id}>
                    <span>{formatDebugTime(event.at)}</span>
                    <strong>{event.method}</strong>
                    <small>{event.detail}</small>
                  </div>
                ))}
            </div>
          ) : (
            <div className="run-debug-empty">暂无事件。</div>
          )}
        </section>
      </div>
    </aside>
  );
}

function RunDebugRow({ label, value }: { label: string; value: ReactNode }): JSX.Element {
  return (
    <div className="run-debug-row">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

function RunDebugItem({ turnID, item }: { turnID: string; item: ThreadItem }): JSX.Element {
  return (
    <div className={`run-debug-item ${item.status ?? "in_progress"}`}>
      <div>
        <strong>{debugItemTitle(item)}</strong>
        <span>
          {shortDebugID(item.id)} · {debugItemStatusLabel(item)}
        </span>
      </div>
      <div className="run-debug-item-meta">
        <DebugFieldLength turnID={turnID} item={item} field="text" label="text" />
        <DebugFieldLength turnID={turnID} item={item} field="arguments" label="args" />
        <DebugFieldLength turnID={turnID} item={item} field="result" label="result" />
        {item.error ? (
          <span className="error" title={item.error}>
            error: {shortDebugError(item.error)}
          </span>
        ) : null}
      </div>
    </div>
  );
}

function shortDebugError(message: string): string {
  const trimmed = message.trim();
  if (trimmed.length <= 48) {
    return trimmed;
  }
  return `${trimmed.slice(0, 45)}...`;
}

function DebugFieldLength({
  turnID,
  item,
  field,
  label
}: {
  turnID: string;
  item: ThreadItem;
  field: StreamTextField;
  label: string;
}): JSX.Element | null {
  const key = streamTextKey(turnID, item.id, field);
  const initialValue = streamTextStore.has(key) ? streamTextStore.get(key) : item[field] ?? "";
  const [length, setLength] = useState(initialValue.length);

  useEffect(() => {
    const currentValue = streamTextStore.has(key) ? streamTextStore.get(key) : item[field] ?? "";
    setLength(currentValue.length);
    return streamTextStore.subscribe(key, (value) => setLength(value.length));
  }, [field, item, key]);

  if (length === 0) {
    return null;
  }
  return (
    <span>
      {label} {length.toLocaleString()}
    </span>
  );
}

function LiveSince({ atMs }: { atMs: number }): JSX.Element {
  const nodeRef = useRef<HTMLSpanElement | null>(null);

  useEffect(() => {
    const update = (): void => {
      if (nodeRef.current) {
        nodeRef.current.textContent = `${formatDuration(Date.now() - atMs)} 前`;
      }
    };
    update();
    const timer = window.setInterval(update, 1000);
    return () => window.clearInterval(timer);
  }, [atMs]);

  return <span ref={nodeRef}>{formatDuration(Date.now() - atMs)} 前</span>;
}



function reduceServerEvent(state: AppState, event: ServerEvent): AppState {
  switch (event.kind) {
    case "notification":
      return reduceNotification(state, event.message);
    case "server-request": {
      if (event.message.method !== "item/tool/requestUserInput") {
        void window.wuu.rejectServerRequest(event.message.id, `unsupported server request: ${event.message.method}`);
        return state;
      }
      const params = event.message.params as { thread_id?: string; questions?: AskUserQuestion[] } | undefined;
      const request: AskRequestState = {
        id: event.message.id,
        threadID: typeof params?.thread_id === "string" && params.thread_id ? params.thread_id : undefined,
        questions: params?.questions ?? []
      };
      return {
        ...state,
        answeredAskRequests: state.answeredAskRequests.filter((request) => request.id !== event.message.id),
        askRequests: upsertAskRequest(state.askRequests, request)
      };
    }
    case "server-error":
      return { ...state, status: statusMessageForError(event.message, "server error") };
    case "server-exit":
      return { ...state, running: false, status: "wuu 遇到内部错误。后台服务已退出，请重启桌面端。" };
  }
}

type StreamingNotificationHandling = "state" | "stream" | "skip";

function handleStreamingNotification(event: ServerEvent, state: AppState): StreamingNotificationHandling {
  if (event.kind !== "notification") {
    return "state";
  }
  const notification = event.message;
  const params = notification.params as Record<string, unknown> | undefined;
  switch (notification.method) {
    case "item/agentMessage/delta":
      if (!notificationTargetsActiveThread(params, state)) {
        return "skip";
      }
      appendStreamDelta(params, "text");
      return "stream";
    case "item/reasoning/delta":
      if (!notificationTargetsActiveThread(params, state)) {
        return "skip";
      }
      appendStreamDelta(params, "text");
      return "stream";
    case "item/toolCall/delta":
      if (!notificationTargetsActiveThread(params, state)) {
        return "skip";
      }
      appendStreamDelta(params, "arguments");
      return "stream";
    case "item/toolCall/outputDelta":
      if (!notificationTargetsActiveThread(params, state)) {
        return "skip";
      }
      appendStreamDelta(params, "result");
      return "stream";
    case "turn/event":
      return "skip";
    case "item/started":
    case "item/completed":
      if (notificationTargetsActiveThread(params, state)) {
        syncStreamItem(params);
      }
      return "state";
    default:
      return "state";
  }
}

function serverEventShouldRefreshGit(event: ServerEvent): boolean {
  if (event.kind !== "notification") {
    return false;
  }
  return event.message.method === "turn/completed" || event.message.method === "turn/error";
}

function notificationTargetsActiveThread(params: Record<string, unknown> | undefined, state: AppState): boolean {
  const threadID = threadIDFromParams(params);
  return !threadID || threadID === state.thread?.id || threadID === state.secondaryThread?.id;
}

function appendStreamDelta(params: Record<string, unknown> | undefined, field: StreamTextField): void {
  const turnID = params?.turn_id as string | undefined;
  const itemID = params?.item_id as string | undefined;
  const delta = params?.delta as string | undefined;
  if (!turnID || !itemID || !delta) {
    return;
  }
  streamTextStore.append(streamTextKey(turnID, itemID, field), delta);
}

function syncStreamItem(params: Record<string, unknown> | undefined): void {
  const turnID = params?.turn_id as string | undefined;
  const item = params?.item as ThreadItem | undefined;
  if (!turnID || !item?.id) {
    return;
  }
  const completed = (item.status ?? "in_progress") !== "in_progress";
  const retainTextStream = completed && (item.type === "agent_message" || item.type === "reasoning");
  if (typeof item.text === "string") {
    streamTextStore.set(streamTextKey(turnID, item.id, "text"), item.text);
  }
  if (typeof item.arguments === "string") {
    streamTextStore.set(streamTextKey(turnID, item.id, "arguments"), item.arguments);
  }
  if (typeof item.result === "string") {
    streamTextStore.set(streamTextKey(turnID, item.id, "result"), item.result);
  }
  if (completed && !retainTextStream) {
    window.requestAnimationFrame(() => streamTextStore.clearItem(turnID, item.id));
  }
}

function reduceNotification(state: AppState, notification: AppServerNotification): AppState {
  const params = notification.params as Record<string, unknown> | undefined;
  switch (notification.method) {
    case "thread/started":
    case "thread/resumed": {
      const thread = params?.thread as Thread | undefined;
      if (!thread) {
        return state;
      }
      if (!threadMatchesActiveContext(thread, state.activeContext)) {
        return state;
      }
      const knownThread = state.threads.some((item) => item.id === thread.id);
      const updatesVisibleThread = state.thread?.id === thread.id || state.secondaryThread?.id === thread.id;
      const activateThread = state.thread?.id === thread.id || (state.allowThreadAutoActivation && !state.thread && !knownThread);
      return {
        ...state,
        thread: activateThread ? thread : state.thread,
        secondaryThread: state.secondaryThread?.id === thread.id ? thread : state.secondaryThread,
        allowThreadAutoActivation: activateThread ? true : state.allowThreadAutoActivation,
        threads: upsertThread(state.threads, thread),
        status: activateThread || updatesVisibleThread ? "ready" : state.status
      };
    }
    case "agent/updated": {
      const threadID = threadIDFromParams(params);
      const agent = agentFromRecord(recordValue(params, "agent"));
      if (!threadID || !agent || !isDirectChildAgent(threadID, agent)) {
        return state;
      }
      return updateThreadByID(state, threadID, (thread) => upsertThreadChildAgent(thread, agent));
    }
    case "turn/started": {
      const turn = params?.turn as Turn | undefined;
      if (!turn) {
        return state;
      }
      return updateThreadByID(state, threadIDFromParams(params), (thread) => upsertTurn(thread, turn), {
        running: true
      });
    }
    case "item/started":
    case "item/completed": {
      const item = params?.item as ThreadItem | undefined;
      const turnID = params?.turn_id as string | undefined;
      if (!item || !turnID) {
        return state;
      }
      return updateThreadByID(state, threadIDFromParams(params), (thread) => upsertTurnItem(thread, turnID, item));
    }
    case "item/agentMessage/delta":
      return applyDelta(state, params, "text");
    case "item/reasoning/delta":
      return applyDelta(state, params, "text");
    case "item/toolCall/delta":
      return applyDelta(state, params, "arguments");
    case "item/toolCall/outputDelta":
      return applyDelta(state, params, "result");
    case "turn/completed":
    case "turn/error": {
      const turn = params?.turn as Turn | undefined;
      const threadID = threadIDFromParams(params);
      if (!turn) {
        return threadID === activeThreadIDForState(state) ? { ...state, running: false } : state;
      }
      return updateThreadByID(state, threadID, (thread) => upsertTurn(thread, turn), {
        running: false,
        status: "ready"
      });
    }
    default:
      return state;
  }
}

function applyDelta(state: AppState, params: Record<string, unknown> | undefined, field: "text" | "arguments" | "result"): AppState {
  const threadID = threadIDFromParams(params);
  const turnID = params?.turn_id as string | undefined;
  const itemID = params?.item_id as string | undefined;
  const delta = params?.delta as string | undefined;
  if (!turnID || !itemID || !delta) {
    return state;
  }
  return updateThreadByID(state, threadID, (thread) =>
    updateTurnItem(thread, turnID, itemID, (item) => ({
      ...item,
      [field]: `${item[field] ?? ""}${delta}`
    }))
  );
}

function threadIDFromParams(params: Record<string, unknown> | undefined): string | undefined {
  const threadID = params?.thread_id;
  return typeof threadID === "string" && threadID ? threadID : undefined;
}

function updateThreadByID(
  state: AppState,
  threadID: string | undefined,
  update: (thread: Thread) => Thread,
  activePatch: Partial<Pick<AppState, "running" | "status">> = {}
): AppState {
  if (!threadID) {
    return state;
  }
  const primaryActive = state.thread?.id === threadID;
  const secondaryActive = state.secondaryThread?.id === threadID;
  if ((primaryActive && state.thread) || (secondaryActive && state.secondaryThread)) {
    const currentThread = primaryActive ? state.thread : state.secondaryThread;
    if (!currentThread) {
      return state;
    }
    const thread = update(currentThread);
    const patch = activeThreadIDForState(state) === threadID || activePatch.running === false ? activePatch : {};
    return {
      ...state,
      ...patch,
      thread: primaryActive ? thread : state.thread,
      secondaryThread: secondaryActive ? thread : state.secondaryThread,
      threads: upsertThread(state.threads, thread)
    };
  }
  let updated = false;
  const threads = state.threads.map((thread) => {
    if (thread.id !== threadID) {
      return thread;
    }
    updated = true;
    return update(thread);
  });
  if (!updated) {
    return state;
  }
  return { ...state, threads: sortThreads(threads) };
}

function updateThread(state: AppState, update: (thread: Thread) => Thread): AppState {
  if (!state.thread) {
    return state;
  }
  const thread = update(state.thread);
  return { ...state, thread, threads: upsertThread(state.threads, thread) };
}

function upsertThread(threads: Thread[], thread: Thread | undefined): Thread[] {
  const validThreads = sortThreads(threads);
  if (!isThread(thread)) {
    return validThreads;
  }
  if (thread.archived || thread.read_only) {
    return validThreads.filter((item) => item.id !== thread.id);
  }
  const index = validThreads.findIndex((item) => item.id === thread.id);
  if (index < 0) {
    return sortThreads([thread, ...validThreads]);
  }
  const next = validThreads.slice();
  next[index] = thread;
  return sortThreads(next);
}

function sortThreads(threads: Thread[]): Thread[] {
  return threads
    .filter((thread): thread is Thread => isThread(thread) && !thread.archived && !thread.read_only)
    .sort((left, right) => threadTime(right) - threadTime(left));
}

function pinnedThreads(threads: Thread[]): Thread[] {
  return sortThreads(threads).filter((thread) => thread.pinned);
}

function projectThreads(threads: Thread[]): Thread[] {
  return sortThreads(threads).filter((thread) => !thread.pinned);
}

function threadTime(thread: Thread): number {
  const updatedAt = Date.parse(thread.updated_at);
  if (Number.isFinite(updatedAt)) {
    return updatedAt;
  }
  const createdAt = Date.parse(thread.created_at);
  return Number.isFinite(createdAt) ? createdAt : 0;
}

function requireThread(result: { thread?: Thread }, message: string): Thread {
  if (!isThread(result.thread)) {
    throw new Error(message);
  }
  return result.thread;
}

function activeThreadForState(state: AppState): Thread | undefined {
  if (state.activePane === "secondary" && state.secondaryThread) {
    return state.secondaryThread;
  }
  return state.thread;
}

function threadForPane(state: AppState, pane: ConversationPaneID): Thread | undefined {
  return pane === "secondary" ? state.secondaryThread : state.thread;
}

function activeThreadIDForState(state: AppState): string | undefined {
  return activeThreadForState(state)?.id;
}

function latestPlanUpdateForThread(thread: Thread | undefined): PlanUpdate | undefined {
  if (!thread) {
    return undefined;
  }
  for (let turnIndex = thread.turns.length - 1; turnIndex >= 0; turnIndex--) {
    const turn = thread.turns[turnIndex];
    for (let itemIndex = turn.items.length - 1; itemIndex >= 0; itemIndex--) {
      const item = turn.items[itemIndex];
      if (item.name !== "update_plan" || !item.arguments) {
        continue;
      }
      const update = parsePlanUpdateArguments(item.arguments);
      if (update) {
        return update;
      }
    }
  }
  return undefined;
}

function parsePlanUpdateArguments(argumentsJSON: string): PlanUpdate | undefined {
  let parsed: unknown;
  try {
    parsed = JSON.parse(argumentsJSON);
  } catch {
    return undefined;
  }
  if (!isRecord(parsed) || !Array.isArray(parsed.plan)) {
    return undefined;
  }
  const plan = parsed.plan
    .map((raw): PlanUpdate["plan"][number] | undefined => {
      if (!isRecord(raw)) {
        return undefined;
      }
      const step = stringValue(raw, "step")?.trim();
      const status = stringValue(raw, "status");
      if (!step || (status !== "pending" && status !== "in_progress" && status !== "completed")) {
        return undefined;
      }
      return { step, status };
    })
    .filter((item): item is PlanUpdate["plan"][number] => Boolean(item));
  if (plan.length === 0) {
    return undefined;
  }
  const explanation = stringValue(parsed, "explanation")?.trim();
  return explanation ? { explanation, plan } : { plan };
}

function setThreadForPane(state: AppState, pane: ConversationPaneID, thread: Thread | undefined): AppState {
  if (pane === "secondary") {
    return { ...state, secondaryThread: thread };
  }
  return { ...state, thread };
}

function activeProjectID(context: RuntimeContext | undefined): string | undefined {
  return context?.kind === "project" ? context.project_id : undefined;
}

function sameRuntimeContext(left: RuntimeContext | undefined, right: RuntimeContext | undefined): boolean {
  if (!left || !right || left.kind !== right.kind) {
    return false;
  }
  if (left.kind === "project" && right.kind === "project") {
    return left.project_id === right.project_id;
  }
  return left.cwd === right.cwd;
}

function threadMatchesActiveContext(thread: Thread, context: RuntimeContext | undefined): boolean {
  return Boolean(context && thread.cwd === context.cwd);
}

function isThread(value: unknown): value is Thread {
  return Boolean(value && typeof value === "object" && typeof (value as Thread).id === "string");
}

function isThreadRunning(thread: Thread | undefined): boolean {
  if (thread?.read_only) {
    return false;
  }
  return Boolean(thread?.status === "in_progress" || thread?.turns.some((turn) => turn.status === "in_progress"));
}

function isStateActiveThreadRunning(state: AppState): boolean {
  return Boolean(state.running || isThreadRunning(activeThreadForState(state)));
}

function isAnyThreadRunning(state: AppState): boolean {
  return Boolean(
    state.running ||
      isThreadRunning(state.thread) ||
      isThreadRunning(state.secondaryThread) ||
      state.threads.some(isThreadRunning)
  );
}

function visibleAskRequestForThread(requests: AskRequestState[], threadID: string | undefined): AskRequestState | undefined {
  for (let index = requests.length - 1; index >= 0; index--) {
    const request = requests[index];
    if (!request.threadID || request.threadID === threadID) {
      return request;
    }
  }
  return undefined;
}

function pendingAskThreadIDsForRequests(requests: AskRequestState[]): Set<string> {
  const ids = new Set<string>();
  for (const request of requests) {
    if (request.threadID) {
      ids.add(request.threadID);
    }
  }
  return ids;
}

function visibleAnsweredAskRequestsForThread(
  requests: AnsweredAskRequestState[],
  threadID: string | undefined
): AnsweredAskRequestState[] {
  return requests.filter((request) => !request.threadID || request.threadID === threadID);
}

function upsertAskRequest(requests: AskRequestState[], request: AskRequestState): AskRequestState[] {
  const index = requests.findIndex((item) => item.id === request.id);
  if (index < 0) {
    return [...requests, request];
  }
  const next = requests.slice();
  next[index] = request;
  return next;
}

function removeAskRequest(requests: AskRequestState[], id: string): AskRequestState[] {
  return requests.filter((request) => request.id !== id);
}

function upsertAnsweredAskRequest(
  requests: AnsweredAskRequestState[],
  request: AnsweredAskRequestState
): AnsweredAskRequestState[] {
  const index = requests.findIndex((item) => item.id === request.id);
  if (index < 0) {
    return [...requests, request];
  }
  const next = requests.slice();
  next[index] = request;
  return next;
}

function upsertTurn(thread: Thread, turn: Turn): Thread {
  const index = thread.turns.findIndex((item) => item.id === turn.id);
  const status = turn.status === "in_progress" ? "in_progress" : "idle";
  if (index < 0) {
    return threadWithTurnSummary({ ...thread, turns: [...thread.turns, turn], status }, turn);
  }
  const turns = thread.turns.slice();
  turns[index] = { ...turn, items: mergeTurnItems(turns[index], turn) };
  return threadWithTurnSummary({ ...thread, turns, status }, turn);
}

function mergeTurnItems(previous: Turn, next: Turn): ThreadItem[] {
  const nextByID = new Map(next.items.map((item) => [item.id, item]));
  const used = new Set<string>();
  const merged: ThreadItem[] = [];
  for (const item of previous.items) {
    const nextItem = nextByID.get(item.id);
    if (nextItem) {
      merged.push(nextItem);
      used.add(nextItem.id);
      continue;
    }
    if (item.type !== "user_message") {
      merged.push(item);
      used.add(item.id);
    }
  }
  for (const item of next.items) {
    if (!used.has(item.id)) {
      merged.push(item);
    }
  }
  return merged;
}

function threadWithTurnSummary(thread: Thread, turn: Turn): Thread {
  const preview = hasText(thread.preview) ? thread.preview : turnPreview(turn);
  return {
    ...thread,
    preview,
    updated_at: laterTimestamp(thread.updated_at, turn.completed_at ?? turn.started_at)
  };
}

function turnPreview(turn: Turn): string {
  const userItem = turn.items.find((item) => item.type === "user_message");
  if (!userItem) {
    return "";
  }
  const text = userItem.text?.trim();
  if (text) {
    return text;
  }
  const images = userItem.images ?? [];
  if (images.length === 1) {
    return "[Image #1]";
  }
  if (images.length > 1) {
    return `[${images.length} images]`;
  }
  return "";
}

function hasText(value: string): boolean {
  return value.trim() !== "";
}

function laterTimestamp(current: string, candidate: string | null | undefined): string {
  if (!candidate) {
    return current;
  }
  const currentTime = Date.parse(current);
  const candidateTime = Date.parse(candidate);
  if (!Number.isFinite(candidateTime)) {
    return current;
  }
  return !Number.isFinite(currentTime) || candidateTime > currentTime ? candidate : current;
}

function updateTurnItem(thread: Thread, turnID: string, itemID: string, update: (item: ThreadItem) => ThreadItem): Thread {
  const turns = thread.turns.map((turn) => {
    if (turn.id !== turnID) {
      return turn;
    }
    const index = turn.items.findIndex((item) => item.id === itemID);
    if (index < 0) {
      return turn;
    }
    const items = turn.items.slice();
    items[index] = update(items[index]);
    return { ...turn, items };
  });
  return { ...thread, turns };
}

function upsertTurnItem(thread: Thread, turnID: string, item: ThreadItem): Thread {
  const turns = thread.turns.map((turn) => {
    if (turn.id !== turnID) {
      return turn;
    }
    const index = turn.items.findIndex((existing) => existing.id === item.id);
    if (index < 0) {
      return { ...turn, items: [...turn.items, item] };
    }
    const items = turn.items.slice();
    items[index] = item;
    return { ...turn, items };
  });
  return { ...thread, turns };
}

function upsertThreadChildAgent(thread: Thread, agent: Agent): Thread {
  const current = thread.child_agents ?? [];
  const index = current.findIndex((item) => item.id === agent.id);
  const nextAgent = mergeAgentSummary(index >= 0 ? current[index] : undefined, agent);
  const next = current.slice();
  if (index < 0) {
    next.push(nextAgent);
  } else {
    next[index] = nextAgent;
  }
  return { ...thread, child_agents: sortChildAgents(next) };
}

function mergeAgentSummary(current: Agent | undefined, incoming: Agent): Agent {
  if (!current) {
    return incoming;
  }
  return {
    ...current,
    ...incoming,
    nested_count: incoming.nested_count ?? current.nested_count,
    nested_running_count: incoming.nested_running_count ?? current.nested_running_count,
    started_at: incoming.started_at ?? current.started_at,
    completed_at: incoming.completed_at ?? current.completed_at
  };
}

function turnNoticeDisplay(turn: Turn, hasAssistantOutput: boolean): UserFacingErrorDisplay | undefined {
  const rawMessage = turn.error?.message;
  const baseDisplay =
    turn.status === "interrupted"
      ? userFacingErrorForMessage("context canceled", "turn")
      : isCancellationMessage((rawMessage ?? "").toLowerCase())
        ? userFacingErrorForMessage(rawMessage, "turn")
        : turn.status === "failed"
          ? userFacingErrorForMessage(rawMessage, "turn")
          : undefined;
  if (!baseDisplay) {
    return undefined;
  }
  if (baseDisplay.category === "cancelled") {
    return {
      ...baseDisplay,
      title: hasAssistantOutput ? "回复已中断" : "已停止",
      detail: hasAssistantOutput ? "已保留已生成内容，可以继续发送消息。" : "这次请求已停止，没有生成回复内容。"
    };
  }
  return {
    ...baseDisplay,
    detail: hasAssistantOutput ? `${baseDisplay.detail} 已保留已生成内容。` : baseDisplay.detail
  };
}

function turnHasAssistantOutput(turn: Turn): boolean {
  return turn.items.some((item) => {
    if (item.type !== "agent_message") {
      return false;
    }
    return streamFieldValue(turn.id, item, "text").trim().length > 0;
  });
}

function TurnNotice({ display }: { display: UserFacingErrorDisplay }): JSX.Element {
  const Icon = turnNoticeIcon(display);
  return (
    <aside className={`turn-notice ${display.tone}`} role={display.tone === "error" || display.tone === "auth" ? "alert" : "status"}>
      <span className="turn-notice-icon" aria-hidden="true">
        <Icon size={17} />
      </span>
      <span className="turn-notice-copy">
        <strong>{display.title}</strong>
        <span>{display.detail}</span>
      </span>
    </aside>
  );
}

function turnNoticeIcon(display: UserFacingErrorDisplay): typeof AlertCircle {
  if (display.category === "cancelled") {
    return Square;
  }
  if (display.tone === "auth") {
    return ShieldCheck;
  }
  if (display.tone === "warning") {
    return Info;
  }
  return AlertCircle;
}

function TurnView({
  turn,
  cwd,
  latestAgentMessageID,
  onStreamFrame,
  onForkMessage
}: {
  turn: Turn;
  cwd?: string;
  latestAgentMessageID?: string;
  onStreamFrame: () => void;
  onForkMessage?: (turnID: string, itemID: string) => void;
}): JSX.Element {
  const renderedItems: JSX.Element[] = [];
  let processEntries: TurnProcessEntry[] = [];
  let statusInserted = false;
  const actionableAgentMessageID = turn.status === "completed" ? actionableAgentMessageItemID(turn) : undefined;
  const primaryAgentMessageID =
    actionableAgentMessageID ??
    (turn.status === "in_progress" || turn.status === "failed" || turn.status === "interrupted"
      ? latestAgentMessageItemIDForTurn(turn)
      : undefined);
  const processAutoCollapse = turn.status === "completed" && actionableAgentMessageID !== undefined;

  function renderThreadItem(item: ThreadItem, streaming: boolean): JSX.Element | null {
    return (
      <ThreadItemView
        key={item.id}
        turnID={turn.id}
        turnStatus={turn.status}
        item={item}
        cwd={cwd}
        streaming={streaming}
        actionableAgentMessageID={actionableAgentMessageID}
        latestAgentMessageID={latestAgentMessageID}
        onStreamFrame={onStreamFrame}
        onForkMessage={onForkMessage}
      />
    );
  }

  function insertStatus(): void {
    if (statusInserted) {
      return;
    }
    processEntries.push({
      key: `${turn.id}-status`,
      kind: "status",
      element: <TurnStatusLine key={`${turn.id}-status`} turn={turn} />
    });
    statusInserted = true;
  }

  function appendProcessEntry(entry: TurnProcessEntry | null): void {
    if (entry) {
      processEntries.push(entry);
    }
  }

  function flushProcessEntries(): void {
    if (processEntries.length === 0) {
      return;
    }
    const entries = processEntries;
    processEntries = [];
    const onlyCompletedStatus = processAutoCollapse && entries.every((entry) => entry.kind === "status");
    if (onlyCompletedStatus) {
      return;
    }
    if (!processAutoCollapse && entries.length === 1 && entries[0].kind === "status") {
      renderedItems.push(entries[0].element);
      return;
    }
    renderedItems.push(
      <TurnProcessGroup key={`${turn.id}-process-${renderedItems.length}`} turn={turn} entries={entries} autoCollapse={processAutoCollapse} />
    );
  }

  for (let index = 0; index < turn.items.length; index++) {
    const item = turn.items[index];
    if (item.type === "user_message") {
      flushProcessEntries();
      const rendered = renderThreadItem(item, false);
      if (rendered) {
        renderedItems.push(rendered);
      }
      continue;
    }

    const isPrimaryAgentMessage = item.id === primaryAgentMessageID;
    if (!isPrimaryAgentMessage) {
      insertStatus();
    }

    if (item.type === "tool_call" || item.type === "collab_agent_tool_call") {
      const group = [item];
      let nextIndex = index + 1;
      while (
        nextIndex < turn.items.length &&
        (turn.items[nextIndex].type === "tool_call" || turn.items[nextIndex].type === "collab_agent_tool_call")
      ) {
        group.push(turn.items[nextIndex]);
        nextIndex++;
      }
      appendProcessEntry({
        key: `${item.id}-activity`,
        kind: "activity",
        element: <ToolActivityRow key={`${item.id}-activity`} items={group} collapseWhenIdle={processAutoCollapse} />
      });
      index = nextIndex - 1;
      continue;
    }

    const rendered = renderThreadItem(item, turn.status === "in_progress" && item.status === "in_progress");
    if (!rendered) {
      continue;
    }
    if (isPrimaryAgentMessage) {
      insertStatus();
      flushProcessEntries();
      renderedItems.push(rendered);
    } else {
      appendProcessEntry({ key: item.id, kind: "item", element: rendered });
    }
  }

  if (!statusInserted && turn.status === "in_progress") {
    insertStatus();
  }
  flushProcessEntries();
  const notice = turnNoticeDisplay(turn, turnHasAssistantOutput(turn));

  return (
    <section className="turn">
      {renderedItems}
      {notice ? <TurnNotice display={notice} /> : null}
    </section>
  );
}

type TurnProcessEntry = {
  key: string;
  kind: "status" | "activity" | "item";
  element: JSX.Element;
};

function TurnProcessGroup({
  turn,
  entries,
  autoCollapse
}: {
  turn: Turn;
  entries: TurnProcessEntry[];
  autoCollapse: boolean;
}): JSX.Element {
  const [expanded, setExpanded] = useState(!autoCollapse);
  const previousAutoCollapseRef = useRef(autoCollapse);
  const detailsID = `${turn.id}-process-details`;
  const className = `turn-process-group${expanded ? " expanded" : " collapsed"}${autoCollapse ? " auto-collapsed" : ""}${
    turn.status === "in_progress" ? " running" : ""
  }`;
  const processCount = entries.filter((entry) => entry.kind !== "status").length;
  const metaParts = turnProcessMetaParts(turn, processCount);

  useEffect(() => {
    const previousAutoCollapse = previousAutoCollapseRef.current;
    previousAutoCollapseRef.current = autoCollapse;
    if (autoCollapse && !previousAutoCollapse) {
      setExpanded(false);
    }
    if (!autoCollapse && previousAutoCollapse) {
      setExpanded(true);
    }
  }, [autoCollapse]);

  return (
    <div className={className}>
      <button
        className="turn-process-toggle"
        type="button"
        aria-expanded={expanded}
        aria-controls={detailsID}
        onClick={() => setExpanded((open) => !open)}
      >
        <ListIcon size={15} />
        <span className="turn-process-copy">
          <span>过程记录</span>
          {metaParts.map((part) => (
            <span key={part}>{part}</span>
          ))}
        </span>
        <ChevronDown className="turn-process-chevron" size={15} />
      </button>
      <div className="turn-process-details" id={detailsID} aria-hidden={!expanded}>
        <div className="turn-process-stack">{entries.map((entry) => entry.element)}</div>
      </div>
    </div>
  );
}

function turnProcessMetaParts(turn: Turn, processCount: number): string[] {
  const parts: string[] = [];
  if (processCount > 0) {
    parts.push(`${processCount} 项`);
  }
  if (turn.status === "in_progress") {
    parts.push("运行中");
  } else if (turn.status === "failed") {
    parts.push("失败");
  } else if (turn.status === "interrupted") {
    parts.push("已停止");
  }
  if (typeof turn.duration_ms === "number") {
    parts.push(formatDuration(turn.duration_ms));
  }
  return parts;
}

function TurnStatusLine({ turn }: { turn: Turn }): JSX.Element {
  const completedDuration = typeof turn.duration_ms === "number" ? turn.duration_ms : undefined;
  const startedAt = parseTurnTimestampMs(turn.started_at);
  const liveDuration = completedDuration === undefined && turn.status === "in_progress" && Number.isFinite(startedAt);
  const liveNow = useLiveNow(liveDuration);
  const elapsedMs = completedDuration ?? (liveDuration ? Math.max(0, liveNow - startedAt) : 0);
  const showDuration = completedDuration !== undefined || liveDuration;
  const content = turnProgressContent(turn, elapsedMs);
  const campaign =
    ENABLE_TURN_PROGRESS_EXPERIMENT && liveDuration ? turnProgressCampaign(turn.id, elapsedMs) : undefined;

  return (
    <div
      className={`turn-progress ${turn.status}${campaign ? " has-campaign" : ""}`}
      role={liveDuration ? "status" : undefined}
      aria-live={liveDuration ? "polite" : undefined}
    >
      <div className="turn-progress-header">
        <div className="turn-progress-label">
          <Clock size={17} />
          <span className="turn-progress-copy">
            <span className="turn-progress-title">
              <span>{content.label}</span>
              {showDuration ? <span className="turn-progress-duration">{formatDuration(elapsedMs)}</span> : null}
            </span>
            {content.detail ? <span className="turn-progress-detail">{content.detail}</span> : null}
          </span>
        </div>
      </div>
      <div className="turn-progress-rule">{campaign ? <TurnProgressCampaignScene campaign={campaign} /> : null}</div>
    </div>
  );
}

function TurnProgressPreviewOverlay({ onClose }: { onClose: () => void }): JSX.Element {
  const [startedAt, setStartedAt] = useState(() => Date.now());
  const [complete, setComplete] = useState(false);
  const now = usePreviewNow(!complete);
  const previewElapsedMs = Math.min(TURN_PROGRESS_PREVIEW_MS, Math.max(0, now - startedAt));
  const previewRatio = previewElapsedMs / TURN_PROGRESS_PREVIEW_MS;
  const previewComplete = previewElapsedMs >= TURN_PROGRESS_PREVIEW_MS;
  const campaignElapsedMs = previewComplete
    ? TURN_PROGRESS_CAMPAIGN_MS
    : Math.min(TURN_PROGRESS_CAMPAIGN_MS - 1, previewRatio * TURN_PROGRESS_CAMPAIGN_MS);
  const campaign = turnProgressCampaign("turn-progress-preview", Math.min(TURN_PROGRESS_CAMPAIGN_MS - 1, campaignElapsedMs));

  useEffect(() => {
    if (!complete && previewComplete) {
      setComplete(true);
    }
  }, [complete, previewComplete]);

  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent): void {
      if (event.key === "Escape") {
        onClose();
      }
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [onClose]);

  function restart(): void {
    setStartedAt(Date.now());
    setComplete(false);
  }

  const progressStyle = { width: `${previewRatio * 100}%` } as CSSProperties;

  return (
    <div className="turn-progress-preview-backdrop" role="dialog" aria-modal="true" aria-label="完整预览等待动画">
      <div className="turn-progress-preview-panel">
        <div className="turn-progress-preview-header">
          <div>
            <h2>完整预览</h2>
            <p>
              {formatDuration(campaignElapsedMs)} / {formatDuration(TURN_PROGRESS_CAMPAIGN_MS)}
              <span>{TURN_PROGRESS_ERA_LABELS[campaign.currentEra]}</span>
              <span>{formatDuration(TURN_PROGRESS_PREVIEW_MS)} 预览</span>
              <span>{Math.round(TURN_PROGRESS_PREVIEW_SPEED)}x</span>
            </p>
          </div>
          <div className="turn-progress-preview-actions">
            <button className="icon-button" type="button" aria-label="重播" title="重播" onClick={restart}>
              <RefreshCw size={16} />
            </button>
            <button className="icon-button" type="button" aria-label="关闭" title="关闭" onClick={onClose}>
              <X size={17} />
            </button>
          </div>
        </div>
        <div className="turn-progress-preview-stage">
          <div className="turn-progress-preview-rule">
            <TurnProgressCampaignScene campaign={campaign} />
          </div>
          <div className="turn-progress-preview-track" aria-hidden="true">
            <span style={progressStyle} />
          </div>
        </div>
      </div>
    </div>
  );
}

function TurnProgressCampaignScene({ campaign }: { campaign: TurnProgressCampaign }): JSX.Element {
  const currentLayerActive = campaign.currentLayer === "a";
  return (
    <span className="turn-progress-campaign" aria-hidden="true">
      <TurnProgressSceneLayer
        era={currentLayerActive ? campaign.currentEra : campaign.nextEra}
        variant={campaign.variant}
        state={currentLayerActive ? "current" : "next"}
        transitionProgress={campaign.transitionProgress}
      />
      <TurnProgressSceneLayer
        era={currentLayerActive ? campaign.nextEra : campaign.currentEra}
        variant={campaign.variant}
        state={currentLayerActive ? "next" : "current"}
        transitionProgress={campaign.transitionProgress}
      />
    </span>
  );
}

function TurnProgressSceneLayer({
  era,
  variant,
  state,
  transitionProgress
}: {
  era: TurnProgressEra;
  variant: number;
  state: "current" | "next";
  transitionProgress: number;
}): JSX.Element {
  const entering = state === "next";
  const progress = entering ? transitionProgress : 1 - transitionProgress;
  const opacity = entering ? transitionProgress : 1 - transitionProgress;
  const yOffset = entering ? (1 - progress) * 6 : -transitionProgress * 5;
  const scale = entering ? 0.982 + progress * 0.018 : 1 + transitionProgress * 0.012;
  const blur = entering ? (1 - progress) * 1.1 : transitionProgress * 1.1;
  const style = {
    "--scene-opacity": opacity,
    "--scene-y": `${yOffset}px`,
    "--scene-scale": scale,
    "--scene-blur": `${blur}px`
  } as CSSProperties;

  return (
    <span className={`turn-progress-scene era-${era} variant-${variant} scene-${state}`} style={style}>
      <span className="civilization-prop prop-left" />
      <span className="civilization-prop prop-mid" />
      <span className="civilization-prop prop-right" />
      <span className="civilization-fire" />
      <span className="civilization-banner banner-left" />
      <span className="civilization-banner banner-right" />
      <span className="battle-ground" />
      <span className="battle-dust dust-left" />
      <span className="battle-dust dust-right" />
      <span className="battle-front front-left" />
      <span className="battle-front front-right" />
      <span className="battle-impact impact-one" />
      <span className="battle-impact impact-two" />
      <span className="battle-tracer tracer-one" />
      <span className="battle-tracer tracer-two" />
      <span className="battle-squad squad-left" />
      <span className="battle-squad squad-left-rear" />
      <span className="battle-squad squad-right" />
      <span className="battle-squad squad-right-rear" />
      <span className="battle-smoke smoke-left" />
      <span className="battle-smoke smoke-right" />
      <span className="battle-barrage barrage-one" />
      <span className="battle-barrage barrage-two" />
      <span className="era-projectile projectile-one" />
      <span className="era-projectile projectile-two" />
      <span className="era-vehicle" />
      <span className="era-rocket" />
      <span className="era-planet" />
      <span className="era-ship ship-one" />
      <span className="era-ship ship-two" />
      <span className="fight-spark spark-one" />
      <span className="fight-spark spark-two" />
      <Stickman className="stickman-a" />
      <Stickman className="stickman-b" />
      <Stickman className="stickman-crowd crowd-one" />
      <Stickman className="stickman-crowd crowd-two" />
      <Stickman className="stickman-crowd crowd-three" />
      <Stickman className="stickman-crowd crowd-four" />
    </span>
  );
}

function Stickman({ className }: { className: string }): JSX.Element {
  return (
    <span className={`stickman ${className}`}>
      <span className="stickman-figure">
        <span className="stickman-head" />
        <span className="stickman-body" />
        <span className="stickman-arm arm-front" />
        <span className="stickman-arm arm-back" />
        <span className="stickman-leg leg-front" />
        <span className="stickman-leg leg-back" />
        <span className="stickman-weapon" />
      </span>
    </span>
  );
}

function useLiveNow(active: boolean): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) {
      return;
    }
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [active]);

  return active ? now : Date.now();
}

function usePreviewNow(active: boolean): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) {
      return;
    }
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), 120);
    return () => window.clearInterval(timer);
  }, [active]);

  return now;
}

function turnProgressCampaign(turnID: string, elapsedMs: number): TurnProgressCampaign {
  let hash = 0;
  for (let index = 0; index < turnID.length; index++) {
    hash = (hash * 31 + turnID.charCodeAt(index)) >>> 0;
  }
  const campaignMs = elapsedMs % TURN_PROGRESS_CAMPAIGN_MS;
  const eraIndex = Math.floor(campaignMs / TURN_PROGRESS_ERA_MS);
  const eraElapsedMs = campaignMs % TURN_PROGRESS_ERA_MS;
  const nextEraIndex = (eraIndex + 1) % TURN_PROGRESS_ERAS.length;
  const rawTransitionProgress =
    eraElapsedMs < TURN_PROGRESS_ERA_MS - TURN_PROGRESS_TRANSITION_MS
      ? 0
      : Math.min(1, (eraElapsedMs - (TURN_PROGRESS_ERA_MS - TURN_PROGRESS_TRANSITION_MS)) / TURN_PROGRESS_TRANSITION_MS);
  const transitionProgress = rawTransitionProgress * rawTransitionProgress * (3 - 2 * rawTransitionProgress);
  return {
    currentEra: TURN_PROGRESS_ERAS[eraIndex] ?? TURN_PROGRESS_ERAS[0],
    nextEra: TURN_PROGRESS_ERAS[nextEraIndex] ?? TURN_PROGRESS_ERAS[0],
    currentLayer: eraIndex % 2 === 0 ? "a" : "b",
    transitionProgress,
    variant: hash % 3
  };
}

function LiveDuration({ startedAtMs }: { startedAtMs: number }): JSX.Element {
  const nodeRef = useRef<HTMLSpanElement | null>(null);

  useEffect(() => {
    const update = (): void => {
      if (nodeRef.current) {
        nodeRef.current.textContent = formatDuration(Math.max(0, Date.now() - startedAtMs));
      }
    };
    update();
    const timer = window.setInterval(update, 1000);
    return () => window.clearInterval(timer);
  }, [startedAtMs]);

  return <span ref={nodeRef}>{formatDuration(Math.max(0, Date.now() - startedAtMs))}</span>;
}

function turnProgressContent(turn: Turn, elapsedMs: number): TurnProgressContent {
  if (turn.status === "failed") {
    const display = userFacingErrorForMessage(turn.error?.message, "turn");
    return { label: display.title, detail: display.detail };
  }
  if (turn.status === "interrupted") {
    return { label: "已停止", detail: "这次请求已停止" };
  }
  if (turn.status !== "in_progress") {
    return { label: "已处理" };
  }

  const runningTool = turn.items.find(
    (item) =>
      (item.type === "tool_call" || item.type === "collab_agent_tool_call") &&
      (item.status ?? "in_progress") === "in_progress"
  );
  if (runningTool) {
    return { label: "正在处理", detail: `正在调用 ${readableToolName(runningTool.name)}` };
  }

  const latestItem = latestDebugItem(turn);
  if (!latestItem) {
    return {
      label: "正在思考",
      detail: waitingDetail(elapsedMs, "已收到请求，正在等待模型回应")
    };
  }
  if (latestItem.type === "agent_message") {
    const hasText = debugStreamFieldLength(turn.id, latestItem, "text") > 0;
    return {
      label: hasText ? "正在生成回复" : "正在思考",
      detail: hasText ? "正在输出回答" : waitingDetail(elapsedMs, "正在组织回答")
    };
  }
  if (latestItem.type === "reasoning") {
    return {
      label: "正在思考",
      detail: waitingDetail(elapsedMs, "正在组织回答")
    };
  }
  if (latestItem.type === "tool_call" || latestItem.type === "collab_agent_tool_call") {
    return { label: "正在处理", detail: "工具已返回，正在整理结果" };
  }
  if (latestItem.type === "context_compaction") {
    return { label: "正在处理", detail: "正在整理上下文" };
  }
  if (latestItem.type === "error") {
    return { label: "正在处理", detail: "收到错误信息，正在收尾" };
  }

  return { label: "正在处理", detail: waitingDetail(elapsedMs, "请求正在处理中") };
}

function waitingDetail(elapsedMs: number, defaultDetail: string): string {
  if (elapsedMs >= 30_000) {
    return "这个请求比平常更久，仍在等待响应";
  }
  if (elapsedMs >= 8_000) {
    return "请求已开始，正在继续处理";
  }
  return defaultDetail;
}

function latestAgentMessageItemID(turns: Turn[]): string | undefined {
  for (let turnIndex = turns.length - 1; turnIndex >= 0; turnIndex--) {
    const itemID = latestAgentMessageItemIDForTurn(turns[turnIndex]);
    if (itemID) {
      return itemID;
    }
  }
  return undefined;
}

function latestAgentMessageItemIDForTurn(turn: Turn): string | undefined {
  for (let itemIndex = turn.items.length - 1; itemIndex >= 0; itemIndex--) {
    const item = turn.items[itemIndex];
    if (item.type === "agent_message") {
      return item.id;
    }
  }
  return undefined;
}

function actionableAgentMessageItemID(turn: Turn): string | undefined {
  let latestAgentMessageID: string | undefined;
  let latestPostToolAgentMessageID: string | undefined;
  let hasToolCall = false;

  for (const item of turn.items) {
    if (item.type === "tool_call" || item.type === "collab_agent_tool_call") {
      hasToolCall = true;
      latestPostToolAgentMessageID = undefined;
      continue;
    }
    if (item.type === "agent_message") {
      latestAgentMessageID = item.id;
      if (hasToolCall) {
        latestPostToolAgentMessageID = item.id;
      }
    }
  }

  return hasToolCall ? latestPostToolAgentMessageID : latestAgentMessageID;
}

function ThreadItemView({
  turnID,
  turnStatus,
  item,
  cwd,
  streaming,
  actionableAgentMessageID,
  latestAgentMessageID,
  onStreamFrame,
  onForkMessage
}: {
  turnID: string;
  turnStatus: Turn["status"];
  item: ThreadItem;
  cwd?: string;
  streaming: boolean;
  actionableAgentMessageID?: string;
  latestAgentMessageID?: string;
  onStreamFrame: () => void;
  onForkMessage?: (turnID: string, itemID: string) => void;
}): JSX.Element | null {
  switch (item.type) {
    case "user_message": {
      const text = item.text ?? "";
      const copyable = text.trim() !== "";
      return (
        <div className={`user-message-block${copyable ? " user-message-block-with-actions" : ""}`}>
          <div className="message user-message">
            {item.images?.length ? <MessageImageGrid images={item.images} /> : null}
            {text ? <RichContent text={text} cwd={cwd} /> : null}
          </div>
          {copyable ? (
            <div className="message-actions user-message-actions" aria-label="用户消息操作">
              <MessageCopyButton getText={() => text} className="message-action-button" iconSize={15} />
            </div>
          ) : null}
        </div>
      );
    }
    case "agent_message": {
      const streamKeyValue = streamTextKey(turnID, item.id, "text");
      const agentText = streamTextStore.has(streamKeyValue) ? streamTextStore.get(streamKeyValue) : (item.text ?? "");
      const copyable = streaming || agentText.trim() !== "";
      const actionsVisible = turnStatus === "completed" && item.id === actionableAgentMessageID && copyable;
      const actionsPersistent = actionsVisible && item.id === latestAgentMessageID;
      return (
        <article
          className={`agent-block${
            actionsVisible ? ` agent-block-with-actions${actionsPersistent ? " agent-actions-persistent" : ""}` : ""
          }`}
        >
          <div className="agent-text">
            <AgentMessageContent
              turnID={turnID}
              item={item}
              cwd={cwd}
              streaming={streaming}
              onStreamFrame={onStreamFrame}
            />
          </div>
          {actionsVisible ? (
            <AgentMessageActions
              getText={() => streamFieldValue(turnID, item, "text")}
              onFork={onForkMessage ? () => onForkMessage(turnID, item.id) : undefined}
            />
          ) : null}
        </article>
      );
    }
    case "reasoning":
      return (
        <article className="reasoning-block">
          <Brain size={16} />
          <ReasoningContent turnID={turnID} item={item} streaming={streaming} onStreamFrame={onStreamFrame} />
        </article>
      );
    case "tool_call":
    case "collab_agent_tool_call":
      return <ToolActivityRow items={[item]} />;
    case "context_compaction":
      return <div className="system-line">{item.text}</div>;
    case "error":
      return <TurnNotice display={userFacingErrorForMessage(item.error, "turn")} />;
    default:
      return null;
  }
}

function AgentMessageActions({ getText, onFork }: { getText: () => string; onFork?: () => void }): JSX.Element {
  const [feedback, setFeedback] = useState<"liked" | "disliked" | null>(null);

  return (
    <div className="message-actions agent-message-actions" aria-label="助手消息操作">
      <MessageCopyButton getText={getText} className="message-action-button" iconSize={15} />
      <button
        className="message-action-button"
        type="button"
        aria-label="赞"
        aria-pressed={feedback === "liked"}
        title="赞"
        onClick={() => setFeedback((current) => (current === "liked" ? null : "liked"))}
      >
        <ThumbsUp size={15} />
      </button>
      <button
        className="message-action-button"
        type="button"
        aria-label="踩"
        aria-pressed={feedback === "disliked"}
        title="踩"
        onClick={() => setFeedback((current) => (current === "disliked" ? null : "disliked"))}
      >
        <ThumbsDown size={15} />
      </button>
      <button className="message-action-button" type="button" aria-label="分叉" title="分叉" disabled={!onFork} onClick={onFork}>
        <GitFork size={15} />
      </button>
    </div>
  );
}

function MessageCopyButton({
  getText,
  className = "",
  iconSize = 14
}: {
  getText: () => string;
  className?: string;
  iconSize?: number;
}): JSX.Element {
  const [copyState, setCopyState] = useState<"idle" | "copied" | "failed">("idle");
  const resetTimerRef = useRef<number | undefined>(undefined);
  const label = copyState === "copied" ? "已复制消息" : copyState === "failed" ? "复制失败" : "复制消息";

  useEffect(() => {
    return () => {
      if (resetTimerRef.current !== undefined) {
        window.clearTimeout(resetTimerRef.current);
      }
    };
  }, []);

  function showCopyState(nextState: "copied" | "failed"): void {
    if (resetTimerRef.current !== undefined) {
      window.clearTimeout(resetTimerRef.current);
    }
    setCopyState(nextState);
    resetTimerRef.current = window.setTimeout(() => {
      setCopyState("idle");
      resetTimerRef.current = undefined;
    }, 1200);
  }

  async function handleCopy(): Promise<void> {
    const text = getText();
    if (text.trim() === "") {
      showCopyState("failed");
      return;
    }
    try {
      const clipboard = navigator.clipboard;
      if (!clipboard?.writeText) {
        throw new Error("Clipboard API unavailable");
      }
      await clipboard.writeText(text);
      showCopyState("copied");
    } catch {
      showCopyState("failed");
    }
  }

  return (
    <button
      className={`message-copy-button ${className} ${copyState}`}
      type="button"
      aria-label={label}
      title={label}
      onClick={() => void handleCopy()}
    >
      {copyState === "copied" ? (
        <Check size={iconSize} />
      ) : copyState === "failed" ? (
        <AlertCircle size={iconSize} />
      ) : (
        <Copy size={iconSize} />
      )}
    </button>
  );
}

function MessageImageGrid({ images }: { images: InputImage[] }): JSX.Element {
  return (
    <div className="message-images">
      {images.map((image, index) => (
        <img className="message-image" key={`${image.media_type}-${index}`} src={imageSource(image)} alt={`Image ${index + 1}`} />
      ))}
    </div>
  );
}

function AgentMessageContent({
  turnID,
  item,
  cwd,
  streaming,
  onStreamFrame
}: {
  turnID: string;
  item: ThreadItem;
  cwd?: string;
  streaming: boolean;
  onStreamFrame: () => void;
}): JSX.Element {
  const streamKeyValue = streamTextKey(turnID, item.id, "text");
  const hasBufferedStream = streamTextStore.has(streamKeyValue);
  const liveStream = streaming || hasBufferedStream;

  return (
    <StreamingMarkdown
      streamKey={streamKeyValue}
      initialText={hasBufferedStream ? streamTextStore.seedValue(streamKeyValue) : item.text}
      cwd={cwd}
      final={!streaming}
      live={liveStream}
      onFrame={onStreamFrame}
      onSettled={() => {
        streamTextStore.clearItem(turnID, item.id);
        onStreamFrame();
      }}
    />
  );
}

function ReasoningContent({
  turnID,
  item,
  streaming,
  onStreamFrame
}: {
  turnID: string;
  item: ThreadItem;
  streaming: boolean;
  onStreamFrame: () => void;
}): JSX.Element {
  const streamKeyValue = streamTextKey(turnID, item.id, "text");
  const hasBufferedStream = streamTextStore.has(streamKeyValue);
  const liveStream = streaming || hasBufferedStream;

  return (
    <StreamingMarkdown
      streamKey={streamKeyValue}
      initialText={hasBufferedStream ? streamTextStore.seedValue(streamKeyValue) : item.text}
      className="streaming-markdown reasoning-stream"
      final={!streaming}
      live={liveStream}
      onFrame={onStreamFrame}
      onSettled={() => {
        streamTextStore.clearItem(turnID, item.id);
        onStreamFrame();
      }}
    />
  );
}

function runDebugPhaseForState(state: AppState): RunDebugPhase {
  const thread = activeThreadForState(state);
  const turn = activeDebugTurn(thread);
  const askRequest = visibleAskRequestForThread(state.askRequests, thread?.id);
  if (askRequest) {
    return {
      label: "等待用户选择",
      detail: `${askRequest.questions.length} 个问题需要响应`,
      tone: "warning",
      turn
    };
  }
  if (!state.initialized) {
    return {
      label: "运行时未就绪",
      detail: state.status || "等待初始化",
      tone: state.status === "connecting" || state.status === "opening" ? "running" : "warning",
      turn
    };
  }
  if (state.running && !turn) {
    return {
      label: "正在发送请求",
      detail: "还没收到 turn/started",
      tone: "running"
    };
  }
  if (turn?.status === "in_progress") {
    const runningTool = turn.items.find(
      (item) =>
        (item.type === "tool_call" || item.type === "collab_agent_tool_call") &&
        (item.status ?? "in_progress") === "in_progress"
    );
    if (runningTool) {
      return {
        label: "正在调用工具",
        detail: readableToolName(runningTool.name),
        tone: "running",
        turn,
        activeItem: runningTool
      };
    }

    const latestItem = latestDebugItem(turn);
    if (!latestItem) {
      return {
        label: "等待模型响应",
        detail: "turn 已开始，尚未收到回复 item",
        tone: "running",
        turn
      };
    }
    if (latestItem.type === "agent_message") {
      const length = debugStreamFieldLength(turn.id, latestItem, "text");
      return {
        label: length > 0 ? "正在生成回复" : "回复已开始",
        detail: length > 0 ? `已收到 ${length.toLocaleString()} 字` : "等待首个回复片段",
        tone: "running",
        turn,
        activeItem: latestItem
      };
    }
    if (latestItem.type === "reasoning") {
      const length = debugStreamFieldLength(turn.id, latestItem, "text");
      return {
        label: "模型正在思考",
        detail: length > 0 ? `已收到 ${length.toLocaleString()} 字思考内容` : "等待推理片段",
        tone: "running",
        turn,
        activeItem: latestItem
      };
    }
    if (latestItem.type === "tool_call" || latestItem.type === "collab_agent_tool_call") {
      return {
        label: "工具已返回",
        detail: "等待模型继续处理工具结果",
        tone: "running",
        turn,
        activeItem: latestItem
      };
    }
    return {
      label: "本轮处理中",
      detail: debugItemTitle(latestItem),
      tone: "running",
      turn,
      activeItem: latestItem
    };
  }
  if (turn?.status === "failed") {
    return {
      label: "处理失败",
      detail: turn.error?.message ?? "本轮返回失败状态",
      tone: "error",
      turn
    };
  }
  if (turn?.status === "interrupted") {
    return {
      label: "已停止",
      detail: "本轮已被中断",
      tone: "warning",
      turn
    };
  }
  if (turn?.status === "completed") {
    return {
      label: "已完成",
      detail: turn.duration_ms === undefined ? "本轮完成" : `耗时 ${formatDuration(turn.duration_ms)}`,
      tone: "success",
      turn
    };
  }
  if (state.running) {
    return {
      label: "运行中",
      detail: state.status || "等待事件",
      tone: "running",
      turn
    };
  }
  return {
    label: state.status === "ready" ? "空闲" : "当前状态",
    detail: state.status === "ready" ? "可以发送新消息" : state.status,
    tone: state.status === "ready" ? "idle" : "warning",
    turn
  };
}

function activeDebugTurn(thread: Thread | undefined): Turn | undefined {
  const turns = thread?.turns ?? [];
  for (let index = turns.length - 1; index >= 0; index--) {
    if (turns[index].status === "in_progress") {
      return turns[index];
    }
  }
  return turns.length > 0 ? turns[turns.length - 1] : undefined;
}

function latestDebugItem(turn: Turn): ThreadItem | undefined {
  for (let index = turn.items.length - 1; index >= 0; index--) {
    const item = turn.items[index];
    if (item.type !== "user_message") {
      return item;
    }
  }
  return undefined;
}

function streamFieldValue(turnID: string, item: ThreadItem, field: StreamTextField): string {
  const key = streamTextKey(turnID, item.id, field);
  return streamTextStore.has(key) ? streamTextStore.get(key) : (item[field] ?? "");
}

function debugStreamFieldLength(turnID: string, item: ThreadItem, field: StreamTextField): number {
  return streamFieldValue(turnID, item, field).length;
}

function runDebugEventFromServerEvent(
  event: ServerEvent,
  deltaSeen: Set<string>
): Omit<RunDebugEvent, "id" | "at"> | undefined {
  switch (event.kind) {
    case "server-request":
      return {
        source: "server",
        method: event.message.method,
        detail: "服务端正在等待客户端响应",
        tone: "warning"
      };
    case "server-error":
      return {
        source: "server",
        method: "server/error",
        detail: event.message,
        tone: "error"
      };
    case "server-exit":
      return {
        source: "server",
        method: "server/exit",
        detail: `app-server 退出：${event.code ?? "unknown"}`,
        tone: "error"
      };
    case "notification":
      return runDebugEventFromNotification(event.message, deltaSeen);
  }
}

function runDebugEventFromNotification(
  notification: AppServerNotification,
  deltaSeen: Set<string>
): Omit<RunDebugEvent, "id" | "at"> | undefined {
  const params = isRecord(notification.params) ? notification.params : undefined;
  const threadID = stringValue(params, "thread_id");
  const turnID = stringValue(params, "turn_id");
  const itemID = stringValue(params, "item_id");

  if (isDeltaNotification(notification.method)) {
    const key = `${notification.method}:${turnID ?? ""}:${itemID ?? ""}`;
    if (deltaSeen.has(key)) {
      return undefined;
    }
    deltaSeen.add(key);
    const delta = stringValue(params, "delta") ?? "";
    return {
      source: "server",
      method: debugNotificationMethodLabel(notification.method),
      detail: `首个片段 ${delta.length.toLocaleString()} 字`,
      tone: "running",
      threadID,
      turnID,
      itemID
    };
  }

  if (notification.method === "turn/event") {
    const payload = recordValue(params, "event");
    const eventType = stringValue(payload, "type") ?? "event";
    if (isHighVolumeStreamEvent(eventType)) {
      return undefined;
    }
    return {
      source: "server",
      method: `event/${eventType}`,
      detail: streamEventDebugDetail(payload),
      tone: streamEventTone(eventType),
      threadID,
      turnID
    };
  }

  if (notification.method === "item/started" || notification.method === "item/completed") {
    const item = threadItemFromRecord(recordValue(params, "item"));
    if (!item) {
      return undefined;
    }
    return {
      source: "server",
      method: notification.method,
      detail: `${debugItemTitle(item)} · ${debugItemStatusLabel(item)}`,
      tone: item.status === "failed" || item.error ? "error" : notification.method === "item/completed" ? "success" : "running",
      threadID,
      turnID,
      itemID: item.id
    };
  }

  if (notification.method === "turn/started") {
    const turn = turnFromRecord(recordValue(params, "turn"));
    return {
      source: "server",
      method: notification.method,
      detail: turn ? `本轮开始：${shortDebugID(turn.id)}` : "本轮开始",
      tone: "running",
      threadID,
      turnID: turn?.id ?? turnID
    };
  }

  if (notification.method === "turn/completed" || notification.method === "turn/error") {
    const turn = turnFromRecord(recordValue(params, "turn"));
    const failed = notification.method === "turn/error" || turn?.status === "failed";
    return {
      source: "server",
      method: notification.method,
      detail: failed ? stringValue(params, "error") ?? "本轮失败" : "本轮完成",
      tone: failed ? "error" : "success",
      threadID,
      turnID: turn?.id ?? turnID
    };
  }

  if (notification.method === "thread/started" || notification.method === "thread/resumed") {
    const thread = threadFromRecord(recordValue(params, "thread"));
    return {
      source: "server",
      method: notification.method,
      detail: thread ? `Thread ${shortDebugID(thread.id)}` : "Thread 已更新",
      tone: "info",
      threadID: thread?.id ?? threadID
    };
  }

  return undefined;
}

function isDeltaNotification(method: string): boolean {
  return (
    method === "item/agentMessage/delta" ||
    method === "item/reasoning/delta" ||
    method === "item/toolCall/delta" ||
    method === "item/toolCall/outputDelta"
  );
}

function isHighVolumeStreamEvent(eventType: string): boolean {
  return eventType === "content_delta" || eventType === "thinking_delta" || eventType === "tool_use_delta";
}

function debugNotificationMethodLabel(method: string): string {
  switch (method) {
    case "item/agentMessage/delta":
      return "reply/first-delta";
    case "item/reasoning/delta":
      return "reasoning/first-delta";
    case "item/toolCall/delta":
      return "tool-args/first-delta";
    case "item/toolCall/outputDelta":
      return "tool-output/first-delta";
    default:
      return method;
  }
}

function streamEventDebugDetail(payload: JsonRecord | undefined): string {
  const eventType = stringValue(payload, "type") ?? "event";
  const toolCall = recordValue(payload, "tool_call");
  const toolName = stringValue(toolCall, "name");
  const stopReason = stringValue(payload, "stop_reason");
  const error = stringValue(payload, "error");
  if (error) {
    return error;
  }
  if (toolName) {
    return readableToolName(toolName);
  }
  if (stopReason) {
    return `stop_reason=${stopReason}`;
  }
  return eventType;
}

function streamEventTone(eventType: string): RunDebugEventTone {
  if (eventType === "error") {
    return "error";
  }
  if (eventType === "done") {
    return "success";
  }
  if (eventType === "reconnect") {
    return "warning";
  }
  if (eventType === "tool_use_start" || eventType === "tool_use_end" || eventType === "lifecycle") {
    return "running";
  }
  return "info";
}

function threadItemFromRecord(record: JsonRecord | undefined): ThreadItem | undefined {
  if (!record || typeof record.id !== "string" || typeof record.type !== "string") {
    return undefined;
  }
  return record as ThreadItem;
}

function turnFromRecord(record: JsonRecord | undefined): Turn | undefined {
  if (!record || typeof record.id !== "string" || !Array.isArray(record.items)) {
    return undefined;
  }
  return record as Turn;
}

function threadFromRecord(record: JsonRecord | undefined): Thread | undefined {
  if (!record || typeof record.id !== "string" || !Array.isArray(record.turns)) {
    return undefined;
  }
  return record as Thread;
}

function agentFromRecord(record: JsonRecord | undefined): Agent | undefined {
  const id = stringValue(record, "id");
  const status = stringValue(record, "status");
  if (!id || !status) {
    return undefined;
  }
  return {
    id,
    type: stringValue(record, "type"),
    task_name: stringValue(record, "task_name"),
    agent_path: stringValue(record, "agent_path"),
    parent_id: stringValue(record, "parent_id"),
    description: stringValue(record, "description"),
    status,
    result: stringValue(record, "result"),
    error: stringValue(record, "error"),
    input_tokens: numberValue(record, "input_tokens"),
    output_tokens: numberValue(record, "output_tokens"),
    nested_count: numberValue(record, "nested_count"),
    nested_running_count: numberValue(record, "nested_running_count"),
    started_at: stringValue(record, "started_at"),
    completed_at: stringValue(record, "completed_at")
  };
}

function isDirectChildAgent(threadID: string, agent: Agent): boolean {
  if (agent.parent_id === threadID) {
    return true;
  }
  return agentPathDepth(agent.agent_path) === 2;
}

function agentPathDepth(path: string | undefined): number {
  const trimmed = path?.trim().replace(/^\/+|\/+$/g, "") ?? "";
  return trimmed ? trimmed.split("/").length : 0;
}

function debugItemTitle(item: ThreadItem): string {
  switch (item.type) {
    case "user_message":
      return "用户消息";
    case "agent_message":
      return "回复";
    case "reasoning":
      return "思考";
    case "tool_call":
    case "collab_agent_tool_call":
      return `工具：${readableToolName(item.name)}`;
    case "context_compaction":
      return "上下文压缩";
    case "error":
      return "错误";
    default:
      return item.type;
  }
}

function debugItemStatusLabel(item: ThreadItem): string {
  if (item.status === "failed" || item.error) {
    return "失败";
  }
  if ((item.status ?? "in_progress") === "in_progress") {
    return "进行中";
  }
  return "完成";
}

function debugTurnStatusLabel(status: Turn["status"]): string {
  switch (status) {
    case "in_progress":
      return "进行中";
    case "completed":
      return "完成";
    case "failed":
      return "失败";
    case "interrupted":
      return "已停止";
  }
}

function shortDebugID(id: string): string {
  if (id.length <= 12) {
    return id;
  }
  return `${id.slice(0, 6)}…${id.slice(-4)}`;
}

function formatDebugTime(atMs: number): string {
  return new Date(atMs).toLocaleTimeString("zh-CN", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false
  });
}

function buildRunDebugSnapshot({
  state,
  events,
  queuedMessages,
  guideMessages,
  composerImages
}: {
  state: AppState;
  events: RunDebugEvent[];
  queuedMessages: QueuedComposerMessage[];
  guideMessages: QueuedComposerMessage[];
  composerImages: ComposerImage[];
}): string {
  const phase = runDebugPhaseForState(state);
  const thread = activeThreadForState(state);
  const turn = phase.turn ?? activeDebugTurn(thread);
  const lines = [
    `phase: ${phase.label} (${phase.detail})`,
    `status: ${state.status}`,
    `running: ${String(state.running)}`,
    `provider: ${state.initialized?.provider ?? "none"}`,
    `model: ${state.initialized?.model ?? "none"}`,
    `effort: ${state.initialized?.effort ?? ""}`,
    `cwd: ${state.activeContext?.cwd ?? thread?.cwd ?? ""}`,
    `thread: ${thread?.id ?? ""}`,
    `turn: ${turn?.id ?? ""}`,
    `turn_status: ${turn?.status ?? ""}`,
    `turn_error: ${turn?.error?.message ?? ""}`,
    `queued_messages: ${queuedMessages.length}`,
    `guide_messages: ${guideMessages.length}`,
    `composer_images: ${composerImages.length}`
  ];

  lines.push("");
  lines.push("items:");
  if (turn?.items.length) {
    for (const item of turn.items) {
      lines.push(
        `- ${item.id} ${item.type} ${item.status ?? "in_progress"} ${item.name ?? ""} text=${debugStreamFieldLength(
          turn.id,
          item,
          "text"
        )} args=${debugStreamFieldLength(turn.id, item, "arguments")} result=${debugStreamFieldLength(turn.id, item, "result")} error=${
          item.error ?? ""
        }`
      );
    }
  } else {
    lines.push("- none");
  }

  lines.push("");
  lines.push("events:");
  for (const event of events.slice(-40)) {
    lines.push(
      `- ${new Date(event.at).toISOString()} ${event.source} ${event.method} ${event.detail} thread=${event.threadID ?? ""} turn=${
        event.turnID ?? ""
      } item=${event.itemID ?? ""}`
    );
  }
  return lines.join("\n");
}

function parseTurnTimestampMs(value: string | null | undefined): number {
  if (!value) {
    return NaN;
  }
  return Date.parse(value);
}

function formatDuration(ms: number): string {
  const totalSeconds = Math.max(0, Math.floor(ms / 1000));
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  if (hours > 0) {
    return `${hours}h ${minutes}m ${seconds}s`;
  }
  if (minutes > 0) {
    return `${minutes}m ${seconds}s`;
  }
  return `${seconds}s`;
}
