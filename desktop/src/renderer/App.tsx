/// <reference path="../shared/jsx-compat.d.ts" />

import {
  AlertCircle,
  Archive,
  Bug,
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
  Image as ImageIcon,
  Info,
  Laptop,
  ListChecks,
  List as ListIcon,
  MessageSquarePlus,
  MoreHorizontal,
  Pencil,
  Pin,
  Plus,
  Search,
  Send,
  Settings,
  ShieldCheck,
  Square,
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
  type ReactNode,
  Fragment,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  closestCenter,
  DndContext,
  DragOverlay,
  PointerSensor,
  useSensor,
  useSensors,
  type DragCancelEvent,
  type DragEndEvent,
  type DragStartEvent,
} from "@dnd-kit/core";
import { restrictToHorizontalAxis } from "@dnd-kit/modifiers";
import {
  arrayMove,
  horizontalListSortingStrategy,
  SortableContext,
  useSortable,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import type {
  Agent,
  AppServerNotification,
  AskUserQuestion,
  AskUserResponse,
  DesktopProject,
  GitCommitResult,
  GitPullRequestResult,
  GitStatusResult,
  InitializeResult,
  PlanUpdate,
  ProjectListResult,
  RuntimeConnectionUpdate,
  RuntimeContext,
  ServerEvent,
  Thread,
  ThreadItem,
  ThreadSearchResultItem,
  Turn,
} from "../shared/protocol";
import {
  messageFlowFinalTextIndex,
  messageFlowStatusLabel,
} from "./message-flow-display";
import {
  AnsweredAskUserMessage,
  AskUserMessage,
  type AnsweredAskRequestState,
  type AskRequestState,
} from "./AskUserMessages";
import {
  composerImageFromFile,
  createComposerMessage,
  inputImagesFromComposer,
  mergeGuideMessages,
  type ComposerImage,
  type QueuedComposerMessage,
} from "./ComposerMessages";
import {
  Composer,
  SplitPaneComposer,
  isInsideFloatingMenu,
  type CodexModelLoadState,
  type CodexRuntimeMenu,
  type ComposerVariant,
  type FloatingMenuOwner,
} from "./ComposerView";
import {
  createAgentTreeDemo,
  createConversationFixture,
  type ConversationFixtureKind,
} from "./ConversationFixtures";
import {
  EnvironmentPanel,
  buildEnvironmentSourceItems,
  type EnvironmentPanelMenu,
  type EnvironmentPanelMotionState,
} from "./EnvironmentPanel";
import { agentHandoffDisplay } from "./AgentHandoff";
import { CollapsibleDetails } from "./CollapsibleMotion";
import { CommitChangesDialog, PullRequestDialog } from "./GitDialogs";
import { RichContent } from "./RichContent";
import {
  EmptyConversationHome,
  RuntimeLoading,
  ViewSwitchLoading,
} from "./LoadingViews";
import {
  AgentMessageActions,
  MessageCopyButton,
  MessageImageGrid,
} from "./MessageActions";
import {
  isCodexProvider,
  pullRequestUnavailableReason,
} from "./RuntimeHelpers";
import { SettingsView } from "./SettingsView";
import { SidePanelToggleIcon } from "./SidePanelToggleIcon";
import { SkillsCatalog } from "./SkillsCatalog";
import { StreamingMarkdown } from "./StreamingMarkdown";
import {
  streamTextKey,
  streamTextStore,
  type StreamTextField,
} from "./StreamText";
import { threadDisplayTitle } from "./ThreadTitles";
import {
  ToolActivityRow,
  ToolActivityTimeline,
  isRecord,
  numberValue,
  readableToolName,
  recordValue,
  stringValue,
  type JsonRecord,
} from "./ToolActivity";
import {
  LiveDuration,
  TurnProgressCampaignScene,
  TurnProgressPreviewOverlay,
  formatDuration,
  turnProgressCampaign,
  useLiveNow,
} from "./TurnProgress";
import {
  mergeTurnItemsInOrder,
  orderedTurnItems,
  upsertTurnItemInOrder,
} from "./TurnOrdering";
import { sortChildAgents } from "./ThreadAgents";
import { PinnedThreadList, ProjectList } from "./ThreadSidebar";
import {
  isCancellationMessage,
  rawErrorMessage,
  statusMessageForError,
  statusToneClass,
  userFacingErrorForMessage,
  type UserFacingErrorDisplay,
} from "./UserFacingErrors";
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
const CONVERSATION_SEARCH_EXIT_MS = 180;

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

type RunDebugPhaseTone = "idle" | "running" | "success" | "warning" | "error";

type ConversationSearchState = {
  open: boolean;
  closing: boolean;
  query: string;
  loading: boolean;
  error: string;
  results: ThreadSearchResultItem[];
  selectedIndex: number;
};

type ConversationSearchResultSection = {
  title: string;
  results: ThreadSearchResultItem[];
  startIndex: number;
};

type ComposerDraftState = {
  prompt: string;
  images: ComposerImage[];
};

function emptyComposerDraft(): ComposerDraftState {
  return { prompt: "", images: [] };
}

function initialSplitComposerDrafts(): Record<
  ConversationPaneID,
  ComposerDraftState
> {
  return {
    primary: emptyComposerDraft(),
    secondary: emptyComposerDraft(),
  };
}

function cloneComposerDraft(draft: ComposerDraftState): ComposerDraftState {
  return {
    prompt: draft.prompt,
    images: draft.images.map((image) => ({ ...image })),
  };
}

type SessionTab =
  | {
      id: string;
      kind: "draft";
      context: RuntimeContext;
      title: string;
      prompt: string;
      images: ComposerImage[];
      createdAt: number;
    }
  | {
      id: string;
      kind: "thread";
      context: RuntimeContext;
      threadID: string;
      title: string;
      prompt: string;
      images: ComposerImage[];
    }
  | {
      id: string;
      kind: "file";
      context: RuntimeContext;
      path: string;
      title: string;
    }
  | {
      id: string;
      kind: "skills";
      context: RuntimeContext;
      title: string;
    };

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
  sessionTabs: SessionTab[];
  activeSessionTabID: string;
  threads: Thread[];
  running: boolean;
  status: string;
  askRequests: AskRequestState[];
  answeredAskRequests: AnsweredAskRequestState[];
};

const INITIAL_DRAFT_SESSION_TAB_ID = "draft:initial";

const initialState: AppState = {
  projects: [],
  activePane: "primary",
  allowThreadAutoActivation: false,
  sessionTabs: [],
  activeSessionTabID: "",
  threads: [],
  running: false,
  status: "connecting",
  askRequests: [],
  answeredAskRequests: [],
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
const CONVERSATION_SEARCH_RESULT_LIMIT = 40;
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
const ENABLE_SWISS_STYLE_TOGGLE = Boolean(RENDERER_ENV?.DEV);
const ENABLE_CONVERSATION_FIXTURES = Boolean(RENDERER_ENV?.DEV);
const ENABLE_PLAN_PANEL_DEBUG = Boolean(RENDERER_ENV?.DEV);
const ENABLE_TURN_PROGRESS_EXPERIMENT = false;

function prefersReducedMotion(): boolean {
  return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

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

function initialSwissStyleEnabled(): boolean {
  return (
    ENABLE_SWISS_STYLE_TOGGLE &&
    window.localStorage.getItem(SWISS_STYLE_KEY) === "true"
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
  const [conversationSearch, setConversationSearch] =
    useState<ConversationSearchState>({
      open: false,
      closing: false,
      query: "",
      loading: false,
      error: "",
      results: [],
      selectedIndex: 0,
    });
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
  const [runDebugOpen, setRunDebugOpen] = useState(false);
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
  const [swissStyleEnabled, setSwissStyleEnabled] = useState(
    initialSwissStyleEnabled,
  );
  const [draggingSessionTabID, setDraggingSessionTabID] = useState<
    string | undefined
  >(undefined);
  const [draggingSessionTabWidth, setDraggingSessionTabWidth] = useState<
    number | undefined
  >(undefined);
  const conversationScrollRef = useRef<HTMLDivElement | null>(null);
  const splitPaneRefs = useRef<Record<ConversationPaneID, HTMLElement | null>>({
    primary: null,
    secondary: null,
  });
  const conversationPaneRef = useRef<HTMLElement | null>(null);
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
  const projectMenuRef = useRef<HTMLDivElement>(null);
  const runtimeMenuRef = useRef<HTMLDivElement>(null);
  const accessMenuRef = useRef<HTMLDivElement>(null);
  const codexRuntimeRef = useRef<HTMLDivElement>(null);
  const environmentToggleRef = useRef<HTMLButtonElement>(null);
  const environmentPanelRef = useRef<HTMLDivElement>(null);
  const runDebugRef = useRef<HTMLDivElement>(null);
  const conversationSearchRef = useRef<HTMLDivElement>(null);
  const conversationSearchInputRef = useRef<HTMLInputElement>(null);
  const conversationSearchRequestRef = useRef(0);
  const conversationSearchCloseTimerRef = useRef<number | undefined>(
    undefined,
  );
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
  const draftSessionTabCounterRef = useRef(0);
  const effectiveSidebarWidth = sidebarCollapsed ? 0 : sidebarWidth;
  const clampedWorkspaceRightPanelWidth = clampWorkspaceRightPanelWidth(
    workspaceRightPanelWidth,
    effectiveSidebarWidth,
  );
  const debugControlsVisible = ENABLE_DEBUG_CONTROLS && debugControlsEnabled;
  const sessionTabSensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
  );
  const currentSessionTab = activeSessionTab(state);
  const activeWorkspaceFile =
    currentSessionTab?.kind === "file" &&
    sameRuntimeContext(currentSessionTab.context, state.activeContext)
      ? currentSessionTab.path
      : undefined;
  const activeThread = activeThreadForState(state);
  const activeThreadID = activeThread?.id;
  const activePlanUpdate = latestPlanUpdateForThread(activeThread);
  const splitConversation = Boolean(
    state.thread && state.secondaryThread && !workspaceMode,
  );

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
      if (conversationSearchCloseTimerRef.current !== undefined) {
        window.clearTimeout(conversationSearchCloseTimerRef.current);
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
    state.secondaryThread?.turns,
  ]);

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

  useEffect(() => {
    if (!conversationSearch.open || conversationSearch.closing) {
      return undefined;
    }
    const delay = conversationSearch.query.trim() ? 140 : 0;
    const timer = window.setTimeout(() => {
      void refreshConversationSearchThreads(conversationSearch.query);
    }, delay);
    return () => window.clearTimeout(timer);
  }, [
    conversationSearch.closing,
    conversationSearch.open,
    conversationSearch.query,
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
    window.requestAnimationFrame(() =>
      scrollConversationToBottom({ force: true }),
    );
  }, [activeThreadID, state.askRequests]);

  useEffect(() => {
    const enabled =
      debugControlsVisible && ENABLE_SWISS_STYLE_TOGGLE && swissStyleEnabled;
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
    setLaunchPreviewPinned(false);
    setRunDebugOpen(false);
    setTurnProgressPreviewOpen(false);
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
  const visibleAskRequest = visibleAskRequestForThread(
    state.askRequests,
    activeThreadID,
  );
  const visibleAnsweredAskRequests = visibleAnsweredAskRequestsForThread(
    state.answeredAskRequests,
    activeThreadID,
  );
  const answeredAskRequestsWithoutVisibleTurn =
    visibleAnsweredAskRequests.filter(
      (request) =>
        !request.turnID || !turns.some((turn) => turn.id === request.turnID),
    );
  const emptyConversation =
    !showingSkillsCatalog &&
    turns.length === 0 &&
    !visibleAskRequest &&
    visibleAnsweredAskRequests.length === 0;
  const showingWorkspaceMode =
    state.initialized && !previewingLaunch && workspaceMode !== undefined;
  const sidebarPinnedThreads = pinnedThreads(state.threads);
  const conversationSearchResults = conversationSearch.results;
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
  const pendingAskThreadIDs = pendingAskThreadIDsForRequests(state.askRequests);
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
    "--environment-panel-width": "328px",
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
        composerImages,
        queuedMessages,
        guideMessages,
      }),
    [
      activeProject,
      activeWorkspaceFile,
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
    if (visibleAnsweredAskRequests.length === 0) {
      return;
    }
    conversationAutoFollowRef.current = true;
    window.requestAnimationFrame(() =>
      scrollConversationToBottom({ force: true }),
    );
  }, [visibleAnsweredAskRequests.length]);

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
      askRequests: [],
      answeredAskRequests: [],
    }));
  }

  function toggleConversationSearch(): void {
    if (conversationSearch.open) {
      closeConversationSearch();
      return;
    }
    openConversationSearch();
  }

  function openConversationSearch(): void {
    if (!state.activeContext) {
      return;
    }
    if (conversationSearchCloseTimerRef.current !== undefined) {
      window.clearTimeout(conversationSearchCloseTimerRef.current);
      conversationSearchCloseTimerRef.current = undefined;
    }
    setProjectMenuOpen(false);
    setRuntimeMenuOpen(false);
    setAccessMenuOpen(false);
    setModeMenuOpen(false);
    setBranchMenuOpen(false);
    setCodexRuntimeMenu(null);
    setConversationSearch((current) => ({
      ...current,
      open: true,
      closing: false,
      loading: true,
      error: "",
      selectedIndex: 0,
    }));
    window.requestAnimationFrame(() => conversationSearchInputRef.current?.focus());
  }

  function closeConversationSearch(): void {
    if (!conversationSearch.open && !conversationSearch.closing) {
      return;
    }
    conversationSearchRequestRef.current += 1;
    if (conversationSearchCloseTimerRef.current !== undefined) {
      window.clearTimeout(conversationSearchCloseTimerRef.current);
      conversationSearchCloseTimerRef.current = undefined;
    }
    const closeImmediately = prefersReducedMotion();
    setConversationSearch((current) => ({
      ...current,
      open: false,
      closing: !closeImmediately,
      loading: false,
      error: "",
    }));
    if (closeImmediately) {
      return;
    }
    conversationSearchCloseTimerRef.current = window.setTimeout(() => {
      conversationSearchCloseTimerRef.current = undefined;
      setConversationSearch((current) =>
        current.open ? current : { ...current, closing: false },
      );
    }, CONVERSATION_SEARCH_EXIT_MS);
  }

  async function refreshConversationSearchThreads(
    query = conversationSearch.query,
  ): Promise<void> {
    const sourceContext = appStateRef.current.activeContext;
    if (!sourceContext) {
      return;
    }
    const requestID = conversationSearchRequestRef.current + 1;
    conversationSearchRequestRef.current = requestID;
    setConversationSearch((current) => ({
      ...current,
      loading: true,
      error: "",
    }));
    try {
      const search = await window.wuu.searchThreads(
        query,
        CONVERSATION_SEARCH_RESULT_LIMIT,
      );
      if (
        requestID !== conversationSearchRequestRef.current ||
        !sameRuntimeContext(sourceContext, appStateRef.current.activeContext)
      ) {
        return;
      }
      const threads = search.results.map((result) => result.thread);
      setState((current) => ({
        ...current,
        threads: mergeListedThreads(current.threads, threads),
      }));
      setConversationSearch((current) => ({
        ...current,
        results: search.results,
        loading: false,
        error: "",
        selectedIndex: Math.max(
          0,
          Math.min(current.selectedIndex, search.results.length - 1),
        ),
      }));
    } catch (error) {
      if (
        requestID !== conversationSearchRequestRef.current ||
        !sameRuntimeContext(sourceContext, appStateRef.current.activeContext)
      ) {
        return;
      }
      setConversationSearch((current) => ({
        ...current,
        loading: false,
        error: error instanceof Error ? error.message : "搜索会话失败",
      }));
    }
  }

  function selectConversationSearchResult(result: ThreadSearchResultItem): void {
    closeConversationSearch();
    void selectThread(result.thread.id);
  }

  function handleConversationSearchKeyDown(
    event: ReactKeyboardEvent<HTMLInputElement>,
  ): void {
    if (event.key === "Escape") {
      event.preventDefault();
      closeConversationSearch();
      return;
    }
    if (event.key === "ArrowDown" && conversationSearchResults.length > 0) {
      event.preventDefault();
      setConversationSearch((current) => ({
        ...current,
        selectedIndex: (current.selectedIndex + 1) % conversationSearchResults.length,
      }));
      return;
    }
    if (event.key === "ArrowUp" && conversationSearchResults.length > 0) {
      event.preventDefault();
      setConversationSearch((current) => ({
        ...current,
        selectedIndex:
          (current.selectedIndex - 1 + conversationSearchResults.length) %
          conversationSearchResults.length,
      }));
      return;
    }
    if (event.metaKey && /^[1-9]$/.test(event.key)) {
      const index = Number(event.key) - 1;
      const result = conversationSearchResults[index];
      if (result) {
        event.preventDefault();
        selectConversationSearchResult(result);
      }
      return;
    }
    const selectedResult =
      conversationSearchResults[
        Math.max(
          0,
          Math.min(
            conversationSearch.selectedIndex,
            conversationSearchResults.length - 1,
          ),
        )
      ];
    if (event.key === "Enter" && selectedResult) {
      event.preventDefault();
      selectConversationSearchResult(selectedResult);
    }
  }

  function useSkillFromCatalog(name: string): void {
    if (!state.activeContext) {
      return;
    }
    draftSessionTabCounterRef.current += 1;
    const draft = {
      prompt: `/${name} `,
      images: [],
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
      askRequests: [],
      answeredAskRequests: [],
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

  async function attachComposerImageFiles(files: File[]): Promise<void> {
    if (files.length === 0) {
      return;
    }
    try {
      const images = await Promise.all(
        files.map((file) => composerImageFromFile(file)),
      );
      setComposerImages((current) => [...current, ...images]);
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "图片粘贴失败",
      }));
    }
  }

  function removeComposerImage(id: string): void {
    setComposerImages((current) => current.filter((image) => image.id !== id));
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

  async function attachSplitComposerImageFiles(
    pane: ConversationPaneID,
    files: File[],
  ): Promise<void> {
    if (files.length === 0) {
      return;
    }
    try {
      const images = await Promise.all(
        files.map((file) => composerImageFromFile(file)),
      );
      updateSplitComposerDraft(pane, (draft) => ({
        ...draft,
        images: [...draft.images, ...images],
      }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "图片粘贴失败",
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
      status: `已排队 ${next.length} 条`,
    }));
  }

  function removeQueuedMessage(id: string): void {
    queueDrainPausedRef.current = false;
    setQueuedMessagesNow(
      queuedMessagesRef.current.filter((message) => message.id !== id),
    );
    void drainQueuedMessages();
  }

  function removeGuideMessage(id: string): void {
    queueDrainPausedRef.current = false;
    setGuideMessagesNow(
      guideMessagesRef.current.filter((message) => message.id !== id),
    );
    void drainQueuedMessages();
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
    queueDrainPausedRef.current = false;
    setQueuedMessagesNow(remainingQueued);
    setGuideMessagesNow([...guideMessagesRef.current, message]);
    setState((current) => ({
      ...current,
      status: "引导已加入",
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
        status: error instanceof Error ? error.message : "interrupt failed",
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

  function renderThreadConversation(
    thread: Thread,
    pane: ConversationPaneID,
  ): JSX.Element {
    const paneTurns = thread.turns ?? [];
    const paneLatestAgentMessageID = latestAgentMessageItemID(paneTurns);
    const paneAskRequest = visibleAskRequestForThread(
      state.askRequests,
      thread.id,
    );
    const paneAnsweredAskRequests = visibleAnsweredAskRequestsForThread(
      state.answeredAskRequests,
      thread.id,
    );
    const paneAnsweredWithoutVisibleTurn = paneAnsweredAskRequests.filter(
      (request) =>
        !request.turnID ||
        !paneTurns.some((turn) => turn.id === request.turnID),
    );
    const active = state.activePane === pane;
    const closeLabel = pane === "secondary" ? "关闭右侧对话" : "关闭左侧对话";
    const draft = splitComposerDrafts[pane] ?? emptyComposerDraft();
    const paneRunning = isThreadRunning(thread);
    const paneReadOnly = Boolean(thread.read_only);
    const paneStatus = paneReadOnly
      ? paneRunning
        ? "子任务运行中"
        : "子任务会话只读"
      : paneRunning
        ? "运行中"
        : active && state.status !== "ready"
          ? state.status
          : "";
    return (
      <section
        className={`conversation-split-pane${active ? " active" : ""}`}
        aria-label={pane === "secondary" ? "分叉对话" : "源对话"}
        onPointerDown={() => activateConversationPane(pane)}
      >
        <div className="conversation-split-header">
          <div className="conversation-split-title">
            <span>{pane === "secondary" ? "分叉" : "源会话"}</span>
            <strong>
              {threadDisplayTitle(thread, state.threads, "新对话")}
            </strong>
          </div>
          <button
            className="icon-button conversation-split-close"
            type="button"
            aria-label={closeLabel}
            title={closeLabel}
            onClick={() => closeConversationPane(pane)}
          >
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
                  onForkMessage={(turnID, itemID) =>
                    void forkThreadFromMessage(thread, turnID, itemID)
                  }
                />
                {paneAnsweredAskRequests
                  .filter((request) => request.turnID === turn.id)
                  .map((request) => (
                    <AnsweredAskUserMessage
                      key={`answered-${request.id}`}
                      request={request}
                    />
                  ))}
              </Fragment>
            ))}
            {paneAnsweredWithoutVisibleTurn.map((request) => (
              <AnsweredAskUserMessage
                key={`answered-${request.id}`}
                request={request}
              />
            ))}
            {paneAskRequest ? (
              <AskUserMessage
                key={paneAskRequest.id}
                request={paneAskRequest}
                onCancel={(request) =>
                  respondToAskRequest(request, { answers: {}, cancelled: true })
                }
                onSubmit={(request, answers) =>
                  respondToAskRequest(request, { answers })
                }
              />
            ) : null}
          </div>
        </div>
        <SplitPaneComposer
          prompt={draft.prompt}
          setPrompt={(value) => setSplitComposerPrompt(pane, value)}
          images={draft.images}
          running={(!paneReadOnly && paneRunning) || viewSwitchPending}
          readOnly={paneReadOnly}
          status={paneStatus}
          onPasteImageFiles={(files) =>
            void attachSplitComposerImageFiles(pane, files)
          }
          onRemoveImage={(id) => removeSplitComposerImage(pane, id)}
          onSend={() => void sendPromptForPane(pane)}
          onInterrupt={() => void interruptPane(pane)}
        />
      </section>
    );
  }

  function renderSessionTabs(): JSX.Element {
    const draggingTab = draggingSessionTabID
      ? state.sessionTabs.find((tab) => tab.id === draggingSessionTabID)
      : undefined;
    return (
      <div className="session-tab-strip" aria-label="已打开的工作对象">
        <DndContext
          sensors={sessionTabSensors}
          collisionDetection={closestCenter}
          modifiers={[restrictToHorizontalAxis]}
          onDragStart={startSessionTabDrag}
          onDragEnd={endSessionTabDrag}
          onDragCancel={cancelSessionTabDrag}
        >
          <SortableContext
            items={state.sessionTabs.map((tab) => tab.id)}
            strategy={horizontalListSortingStrategy}
          >
            <div className="session-tab-scroll">
              {state.sessionTabs.map((tab) => {
                const active = tab.id === state.activeSessionTabID;
                const tabThread =
                  tab.kind === "thread"
                    ? threadForTab(state, tab.threadID)
                    : undefined;
                const running = isThreadRunning(tabThread);
                const pendingSwitch =
                  pendingViewSwitch?.visible &&
                  pendingViewSwitch.kind === "thread" &&
                  tab.kind === "thread"
                    ? pendingViewSwitch.targetID === tab.threadID
                    : false;
                const label = sessionTabLabel(tab, state);
                const closeLabel =
                  tab.kind === "draft" ? "关闭新对话" : `关闭 ${label}`;
                return (
                  <SortableSessionTab
                    key={tab.id}
                    id={tab.id}
                    active={active}
                    running={running}
                    pendingSwitch={pendingSwitch}
                    label={label}
                    closeLabel={closeLabel}
                    reorderable={state.sessionTabs.length > 1}
                    onSelect={() => void selectSessionTab(tab.id)}
                    onClose={() => void closeSessionTab(tab.id)}
                  />
                );
              })}
            </div>
          </SortableContext>
          <DragOverlay
            dropAnimation={{
              duration: 150,
              easing: "cubic-bezier(0.16, 1, 0.3, 1)",
            }}
          >
            {draggingTab ? (
              <SessionTabDragPreview
                active={draggingTab.id === state.activeSessionTabID}
                label={sessionTabLabel(draggingTab, state)}
                running={
                  draggingTab.kind === "thread"
                    ? isThreadRunning(threadForTab(state, draggingTab.threadID))
                    : false
                }
                width={draggingSessionTabWidth}
              />
            ) : null}
          </DragOverlay>
        </DndContext>
        <button
          className="icon-button workspace-panel-add session-tab-new"
          type="button"
          aria-label="新建对话"
          title="新建对话"
          disabled={!state.activeContext}
          onClick={() => void startNewThread()}
        >
          <Plus size={19} />
        </button>
      </div>
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

  function startSessionTabDrag(event: DragStartEvent): void {
    const tabID = String(event.active.id);
    setDraggingSessionTabID(tabID);
    setDraggingSessionTabWidth(event.active.rect.current.initial?.width);
  }

  function endSessionTabDrag(event: DragEndEvent): void {
    const activeID = String(event.active.id);
    const overID = event.over ? String(event.over.id) : undefined;
    if (overID && activeID !== overID) {
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
    finishSessionTabDrag();
  }

  function cancelSessionTabDrag(_event: DragCancelEvent): void {
    finishSessionTabDrag();
  }

  function finishSessionTabDrag(): void {
    setDraggingSessionTabID(undefined);
    setDraggingSessionTabWidth(undefined);
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
      askRequests: [],
      answeredAskRequests: [],
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
      askRequests: [],
      answeredAskRequests: [],
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
      clearPendingComposerMessages();
      const nextTab =
        activeSessionTab(state)?.kind === "draft" &&
        !prompt.trim() &&
        composerImages.length === 0
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
    };
  }

  function restorePrimaryComposerDraft(draft: ComposerDraftState): void {
    setPrompt(draft.prompt);
    setComposerImages(draft.images.map((image) => ({ ...image })));
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
            askRequests: [],
            answeredAskRequests: [],
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
            askRequests: [],
            answeredAskRequests: [],
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
            askRequests: [],
            answeredAskRequests: [],
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
          askRequests: [],
          answeredAskRequests: [],
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
            askRequests: [],
            answeredAskRequests: [],
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
            askRequests: [],
            answeredAskRequests: [],
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
            askRequests: [],
            answeredAskRequests: [],
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
      composerImages.length === 0
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
      askRequests: [],
      answeredAskRequests: [],
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
      askRequests: [],
      answeredAskRequests: [],
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
          askRequests: [],
          answeredAskRequests: [],
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
        : { prompt, images: composerImages.map((image) => ({ ...image })) };
      setPrompt("");
      setComposerImages([]);
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
    const activeContext = currentState.activeContext;
    conversationAutoFollowRef.current = true;
    resetRunDebugEvents({
      source: "client",
      method: "client/send",
      detail:
        images.length > 0
          ? `已提交输入，包含 ${images.length} 张图片`
          : "已提交输入",
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
      const result = await window.wuu.startTurn(thread.id, text, images);
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
    if (
      !message ||
      !targetThread ||
      !currentState.activeContext ||
      !currentState.initialized
    ) {
      return;
    }
    if (isThreadRunning(targetThread)) {
      setState((current) => ({
        ...current,
        activePane: pane,
        status: "该分支正在运行",
      }));
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
      detail:
        images.length > 0
          ? `已提交输入，包含 ${images.length} 张图片`
          : "已提交输入",
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
      const result = await window.wuu.startTurn(targetThread.id, text, images);
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

  async function drainQueuedMessages(): Promise<void> {
    if (drainingQueueRef.current || queueDrainPausedRef.current) {
      return;
    }
    const currentState = appStateRef.current;
    if (
      isAnyThreadRunning(currentState) ||
      !currentState.activeContext ||
      !currentState.initialized
    ) {
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
        status: "队列暂停",
      }));
    }
  }

  async function updateRuntimeSettings(
    provider: string,
    model: string,
    effort?: string,
    connection?: RuntimeConnectionUpdate,
    variant?: string,
  ): Promise<void> {
    const nextProvider = provider.trim();
    const nextModel = model.trim();
    const nextEffort = effort === undefined ? undefined : effort.trim();
    const nextVariant = variant === undefined ? undefined : variant.trim();
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
        !connectionChanged)
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
      );
      setState((current) => {
        const initialized = current.initialized
          ? {
              ...current.initialized,
              provider: updated.provider,
              model: updated.model,
              effort: updated.effort ?? "",
              variant: updated.variant ?? "",
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

  async function respondToAskRequest(
    request: AskRequestState,
    response: AskUserResponse,
  ): Promise<void> {
    try {
      await window.wuu.respondToServerRequest(request.id, response);
      const currentThread = activeThreadForState(appStateRef.current);
      const answeredRequest: AnsweredAskRequestState = {
        ...request,
        threadID: request.threadID ?? currentThread?.id,
        turnID: activeDebugTurn(currentThread)?.id,
        answers: response.answers ?? {},
        cancelled: response.cancelled === true,
      };
      setState((current) => ({
        ...current,
        askRequests: removeAskRequest(current.askRequests, request.id),
        answeredAskRequests: upsertAnsweredAskRequest(
          current.answeredAskRequests,
          answeredRequest,
        ),
      }));
    } catch (error) {
      setState((current) => ({
        ...current,
        askRequests: upsertAskRequest(current.askRequests, request),
        status: desktopApiErrorMessage(error, "提交选择失败"),
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
  const conversationSearchSections = conversationSearchResultSections(
    conversationSearchResults,
    conversationSearch.query,
  );
  const conversationSearchStatusText = conversationSearch.loading
    ? "正在搜索"
    : conversationSearch.query.trim()
      ? `${conversationSearchResults.length} 个结果`
      : "最近会话";

  return (
    <div className={shellClassName} style={shellStyle}>
      <aside className="sidebar">
        <div className="sidebar-content">
          <div className="traffic-spacer" />
          <nav className="primary-nav" aria-label="主导航">
            <button
              className="nav-item"
              onClick={() => void startNewThread()}
              disabled={!state.activeContext}
            >
              <MessageSquarePlus size={18} />
              <span>新对话</span>
            </button>
            <button
              className="nav-item"
              onClick={openSkillsTab}
              disabled={!state.activeContext}
            >
              <Wrench size={18} />
              <span>Skills</span>
            </button>
            <button
              className="nav-item conversation-search-trigger"
              type="button"
              aria-haspopup="dialog"
              aria-expanded={conversationSearch.open}
              onClick={toggleConversationSearch}
              disabled={!state.activeContext}
            >
              <Search size={18} />
              <span>搜索会话</span>
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
                  setArchiveConfirmThreadID((current) =>
                    current === id ? undefined : current,
                  )
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
                  <button
                    role="menuitem"
                    onClick={() => void createBlankProject()}
                  >
                    <FolderPlus size={22} />
                    <span>新建空白项目</span>
                  </button>
                  <button
                    role="menuitem"
                    onClick={() => void chooseProjectFolder()}
                  >
                    <FolderOpen size={22} />
                    <span>使用现有文件夹</span>
                  </button>
                </div>
              ) : null}
            </div>
            {state.projects.length === 0 ? (
              <div className="project-empty-note">还没有项目</div>
            ) : null}
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
                setArchiveConfirmThreadID((current) =>
                  current === id ? undefined : current,
                )
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

      {conversationSearch.open || conversationSearch.closing ? (
        <div
          className={`conversation-search-overlay${
            conversationSearch.closing ? " closing" : ""
          }`}
          onPointerDown={(event) => {
            if (event.target === event.currentTarget) {
              closeConversationSearch();
            }
          }}
        >
          <div
            className="conversation-search-dialog"
            role="dialog"
            aria-modal="true"
            aria-label="搜索会话"
            ref={conversationSearchRef}
          >
            <div className="conversation-search-input-wrap">
              <Search size={18} aria-hidden="true" />
              <input
                ref={conversationSearchInputRef}
                value={conversationSearch.query}
                placeholder="搜索对话内容或提问"
                onChange={(event) =>
                  setConversationSearch((current) => ({
                    ...current,
                    query: event.target.value,
                    selectedIndex: 0,
                  }))
                }
                onKeyDown={handleConversationSearchKeyDown}
              />
              {conversationSearch.query ? (
                <button
                  className="conversation-search-clear"
                  type="button"
                  aria-label="清空搜索"
                  onClick={() =>
                    setConversationSearch((current) => ({
                      ...current,
                      query: "",
                      selectedIndex: 0,
                    }))
                  }
                >
                  <X size={15} />
                </button>
              ) : null}
            </div>
            <div
              className={`conversation-search-status${
                conversationSearch.loading ? " loading" : ""
              }`}
            >
              <span className="conversation-search-status-text">
                {conversationSearchStatusText}
              </span>
              <button
                type="button"
                onClick={() => void refreshConversationSearchThreads()}
              >
                刷新
              </button>
            </div>
            {conversationSearch.error ? (
              <div className="conversation-search-error">
                {conversationSearch.error}
              </div>
            ) : null}
            <div className="conversation-search-results">
              {conversationSearchSections.map((section) => (
                <section
                  className="conversation-search-section"
                  key={section.title}
                >
                  <div className="conversation-search-section-title">
                    {section.title}
                  </div>
                  {section.results.map((result, sectionIndex) => {
                    const resultIndex = section.startIndex + sectionIndex;
                    const thread = result.thread;
                    const title = threadDisplayTitle(
                      thread,
                      state.threads,
                      "未命名对话",
                    );
                    const active = thread.id === activeThreadID;
                    const pending = visiblePendingThreadID === thread.id;
                    const selected =
                      conversationSearch.selectedIndex === resultIndex;
                    const contextLabel = conversationSearchContextLabel(
                      thread,
                      state.projects,
                    );
                    const snippet = result.snippet?.trim();
                    return (
                      <button
                        key={thread.id}
                        className={`conversation-search-result${active ? " active" : ""}${pending ? " pending" : ""}${selected ? " selected" : ""}`}
                        type="button"
                        aria-current={active ? "page" : undefined}
                        aria-selected={selected}
                        onMouseEnter={() =>
                          setConversationSearch((current) => ({
                            ...current,
                            selectedIndex: resultIndex,
                          }))
                        }
                        onClick={() => selectConversationSearchResult(result)}
                      >
                        <span className="conversation-search-result-main">
                          <span className="conversation-search-result-title">
                            {title}
                          </span>
                          {snippet ? (
                            <span className="conversation-search-result-snippet">
                              {snippet}
                            </span>
                          ) : null}
                        </span>
                        <span className="conversation-search-result-side">
                          <span className="conversation-search-result-context">
                            {contextLabel}
                          </span>
                          <span className="conversation-search-result-meta">
                            {conversationSearchThreadMeta(thread)}
                          </span>
                          {resultIndex < 9 ? (
                            <span className="conversation-search-result-shortcut">
                              ⌘{resultIndex + 1}
                            </span>
                          ) : null}
                        </span>
                      </button>
                    );
                  })}
                </section>
              ))}
              {conversationSearchResults.length === 0 ? (
                <div className="conversation-search-empty">
                  {conversationSearch.loading
                    ? "正在搜索会话"
                    : conversationSearch.query.trim()
                      ? "没有匹配的会话"
                      : "暂无会话"}
                </div>
              ) : null}
            </div>
          </div>
        </div>
      ) : null}

      <main
        className={`conversation-pane${environmentPanelVisible ? " environment-panel-visible" : ""}${
          environmentPanelReserved ? " environment-panel-reserved" : ""
        }${sessionTabsVisible ? " session-tabs-visible" : ""}`}
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
            {debugControlsVisible && ENABLE_SWISS_STYLE_TOGGLE ? (
              <button
                className={`launch-preview-button style-toggle-button${swissStyleEnabled ? " active" : ""}`}
                type="button"
                aria-label={
                  swissStyleEnabled
                    ? "关闭瑞士国际主义风格"
                    : "开启瑞士国际主义风格"
                }
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
                        activeThread
                          ? (turnID, itemID) =>
                              void forkThreadFromMessage(
                                activeThread,
                                turnID,
                                itemID,
                              )
                          : undefined
                      }
                    />
                    {visibleAnsweredAskRequests
                      .filter((request) => request.turnID === turn.id)
                      .map((request) => (
                        <AnsweredAskUserMessage
                          key={`answered-${request.id}`}
                          request={request}
                        />
                      ))}
                  </Fragment>
                ))}
                {answeredAskRequestsWithoutVisibleTurn.map((request) => (
                  <AnsweredAskUserMessage
                    key={`answered-${request.id}`}
                    request={request}
                  />
                ))}
                {visibleAskRequest ? (
                  <AskUserMessage
                    key={visibleAskRequest.id}
                    request={visibleAskRequest}
                    onCancel={(request) =>
                      respondToAskRequest(request, {
                        answers: {},
                        cancelled: true,
                      })
                    }
                    onSubmit={(request, answers) =>
                      respondToAskRequest(request, { answers })
                    }
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
  onClose,
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
    ? `${state.initialized.provider} / ${state.initialized.model}${state.initialized.variant || state.initialized.effort ? ` / ${state.initialized.variant || state.initialized.effort}` : ""}`
    : "未初始化";
  const queueDetail = [
    queuedMessages.length > 0 ? `排队 ${queuedMessages.length}` : "",
    guideMessages.length > 0 ? `引导 ${guideMessages.length}` : "",
    composerImages.length > 0 ? `图片 ${composerImages.length}` : "",
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
          <button
            className="icon-button"
            type="button"
            aria-label="复制调试信息"
            onClick={onCopy}
          >
            <Copy size={15} />
          </button>
          <button
            className="icon-button"
            type="button"
            aria-label="关闭调试信息"
            onClick={onClose}
          >
            <X size={15} />
          </button>
        </div>
      </div>

      <div className="run-debug-scroll">
        {copied ? <div className="run-debug-copied">已复制诊断信息</div> : null}
        <section className="run-debug-section">
          <h3>当前状态</h3>
          <RunDebugRow
            label="运行"
            value={state.running ? "running" : state.status || "ready"}
          />
          <RunDebugRow label="模型" value={model} />
          <RunDebugRow
            label="工作区"
            value={state.activeContext?.cwd ?? thread?.cwd ?? "未连接"}
          />
          <RunDebugRow
            label="Thread"
            value={thread ? shortDebugID(thread.id) : "无"}
          />
          <RunDebugRow
            label="Turn"
            value={
              turn ? (
                <>
                  {shortDebugID(turn.id)} · {debugTurnStatusLabel(turn.status)}{" "}
                  ·{" "}
                  {typeof turn.duration_ms === "number" ? (
                    formatDuration(turn.duration_ms)
                  ) : turn.status === "in_progress" &&
                    Number.isFinite(turnStartedAt) ? (
                    <LiveDuration startedAtMs={turnStartedAt} />
                  ) : (
                    "未知耗时"
                  )}
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
          {queueDetail ? (
            <RunDebugRow label="待发送" value={queueDetail} />
          ) : null}
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
                  <div
                    className={`run-debug-event ${event.tone}`}
                    key={event.id}
                  >
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

function RunDebugRow({
  label,
  value,
}: {
  label: string;
  value: ReactNode;
}): JSX.Element {
  return (
    <div className="run-debug-row">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

function RunDebugItem({
  turnID,
  item,
}: {
  turnID: string;
  item: ThreadItem;
}): JSX.Element {
  return (
    <div className={`run-debug-item ${item.status ?? "in_progress"}`}>
      <div>
        <strong>{debugItemTitle(item)}</strong>
        <span>
          {shortDebugID(item.id)} · {debugItemStatusLabel(item)}
        </span>
      </div>
      <div className="run-debug-item-meta">
        <DebugFieldLength
          turnID={turnID}
          item={item}
          field="text"
          label="text"
        />
        <DebugFieldLength
          turnID={turnID}
          item={item}
          field="arguments"
          label="args"
        />
        <DebugFieldLength
          turnID={turnID}
          item={item}
          field="result"
          label="result"
        />
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
  label,
}: {
  turnID: string;
  item: ThreadItem;
  field: StreamTextField;
  label: string;
}): JSX.Element | null {
  const key = streamTextKey(turnID, item.id, field);
  const initialValue = streamTextStore.has(key)
    ? streamTextStore.get(key)
    : (item[field] ?? "");
  const [length, setLength] = useState(initialValue.length);

  useEffect(() => {
    const currentValue = streamTextStore.has(key)
      ? streamTextStore.get(key)
      : (item[field] ?? "");
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
        void window.wuu.rejectServerRequest(
          event.message.id,
          `unsupported server request: ${event.message.method}`,
        );
        return state;
      }
      const params = event.message.params as
        | { thread_id?: string; questions?: AskUserQuestion[] }
        | undefined;
      const request: AskRequestState = {
        id: event.message.id,
        threadID:
          typeof params?.thread_id === "string" && params.thread_id
            ? params.thread_id
            : undefined,
        questions: params?.questions ?? [],
      };
      return {
        ...state,
        answeredAskRequests: state.answeredAskRequests.filter(
          (request) => request.id !== event.message.id,
        ),
        askRequests: upsertAskRequest(state.askRequests, request),
      };
    }
    case "server-error":
      return {
        ...state,
        status: statusMessageForError(event.message, "server error"),
      };
    case "server-exit":
      return {
        ...state,
        running: false,
        status: "wuu 遇到内部错误。后台服务已退出，请重启桌面端。",
      };
  }
}

function serverEventTargetsActiveContext(
  event: ServerEvent,
  state: AppState,
): boolean {
  return event.workdir === state.activeContext?.cwd;
}

type StreamingNotificationHandling = "state" | "stream" | "skip";

function handleStreamingNotification(
  event: ServerEvent,
  state: AppState,
): StreamingNotificationHandling {
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
    case "item/agentMessage/replace":
      if (!notificationTargetsActiveThread(params, state)) {
        return "skip";
      }
      replaceStreamText(params, "text");
      return "stream";
    case "item/reasoning/delta":
      if (!notificationTargetsActiveThread(params, state)) {
        return "skip";
      }
      appendStreamDelta(params, "text");
      return "stream";
    case "item/reasoning/replace":
      if (!notificationTargetsActiveThread(params, state)) {
        return "skip";
      }
      replaceStreamText(params, "text");
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
  return (
    event.message.method === "turn/completed" ||
    event.message.method === "turn/error"
  );
}

function notificationTargetsActiveThread(
  params: Record<string, unknown> | undefined,
  state: AppState,
): boolean {
  const threadID = threadIDFromParams(params);
  return (
    !threadID ||
    threadID === state.thread?.id ||
    threadID === state.secondaryThread?.id
  );
}

function appendStreamDelta(
  params: Record<string, unknown> | undefined,
  field: StreamTextField,
): void {
  const turnID = params?.turn_id as string | undefined;
  const itemID = params?.item_id as string | undefined;
  const delta = params?.delta as string | undefined;
  if (!turnID || !itemID || !delta) {
    return;
  }
  streamTextStore.append(streamTextKey(turnID, itemID, field), delta);
}

function replaceStreamText(
  params: Record<string, unknown> | undefined,
  field: StreamTextField,
): void {
  const turnID = params?.turn_id as string | undefined;
  const itemID = params?.item_id as string | undefined;
  const text = params?.text;
  if (!turnID || !itemID || typeof text !== "string") {
    return;
  }
  streamTextStore.set(streamTextKey(turnID, itemID, field), text);
}

function syncStreamItem(params: Record<string, unknown> | undefined): void {
  const turnID = params?.turn_id as string | undefined;
  const item = params?.item as ThreadItem | undefined;
  if (!turnID || !item?.id) {
    return;
  }
  const completed = (item.status ?? "in_progress") !== "in_progress";
  const retainTextStream =
    completed && (item.type === "agent_message" || item.type === "reasoning");
  if (typeof item.text === "string") {
    streamTextStore.set(streamTextKey(turnID, item.id, "text"), item.text);
  }
  if (typeof item.arguments === "string") {
    streamTextStore.set(
      streamTextKey(turnID, item.id, "arguments"),
      item.arguments,
    );
  }
  if (typeof item.result === "string") {
    streamTextStore.set(streamTextKey(turnID, item.id, "result"), item.result);
  }
  if (completed && !retainTextStream) {
    window.requestAnimationFrame(() =>
      streamTextStore.clearItem(turnID, item.id),
    );
  }
}

function reduceNotification(
  state: AppState,
  notification: AppServerNotification,
): AppState {
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
      const updatesVisibleThread =
        state.thread?.id === thread.id ||
        state.secondaryThread?.id === thread.id;
      const activateThread =
        state.thread?.id === thread.id ||
        (state.allowThreadAutoActivation && !state.thread && !knownThread);
      return {
        ...state,
        thread: activateThread ? thread : state.thread,
        secondaryThread:
          state.secondaryThread?.id === thread.id
            ? thread
            : state.secondaryThread,
        allowThreadAutoActivation: activateThread
          ? true
          : state.allowThreadAutoActivation,
        threads: upsertThread(state.threads, thread),
        status: activateThread || updatesVisibleThread ? "ready" : state.status,
      };
    }
    case "thread/updated": {
      const thread = params?.thread as Thread | undefined;
      if (!thread || !threadMatchesActiveContext(thread, state.activeContext)) {
        return state;
      }
      return updateThreadByID(state, thread.id, (current) => ({
        ...thread,
        turns: thread.turns.length > 0 ? thread.turns : current.turns,
        child_agents: thread.child_agents ?? current.child_agents,
      }));
    }
    case "agent/updated": {
      const threadID = threadIDFromParams(params);
      const agent = agentFromRecord(recordValue(params, "agent"));
      if (!threadID || !agent || !isDirectChildAgent(threadID, agent)) {
        return state;
      }
      return updateThreadByID(state, threadID, (thread) =>
        upsertThreadChildAgent(thread, agent),
      );
    }
    case "turn/started": {
      const turn = params?.turn as Turn | undefined;
      if (!turn) {
        return state;
      }
      return updateThreadByID(
        state,
        threadIDFromParams(params),
        (thread) => upsertTurn(thread, turn),
        {
          running: true,
        },
      );
    }
    case "item/started":
    case "item/completed": {
      const item = params?.item as ThreadItem | undefined;
      const turnID = params?.turn_id as string | undefined;
      if (!item || !turnID) {
        return state;
      }
      return updateThreadByID(state, threadIDFromParams(params), (thread) =>
        upsertTurnItem(thread, turnID, item),
      );
    }
    case "item/agentMessage/delta":
      return applyDelta(state, params, "text");
    case "item/agentMessage/replace":
      return applyReplace(state, params, "text");
    case "item/reasoning/delta":
      return applyDelta(state, params, "text");
    case "item/reasoning/replace":
      return applyReplace(state, params, "text");
    case "item/toolCall/delta":
      return applyDelta(state, params, "arguments");
    case "item/toolCall/outputDelta":
      return applyDelta(state, params, "result");
    case "turn/completed":
    case "turn/error": {
      const turn = params?.turn as Turn | undefined;
      const threadID = threadIDFromParams(params);
      if (!turn) {
        return threadID === activeThreadIDForState(state)
          ? { ...state, running: false }
          : state;
      }
      return updateThreadByID(
        state,
        threadID,
        (thread) => upsertTurn(thread, turn),
        {
          running: false,
          status: "ready",
        },
      );
    }
    default:
      return state;
  }
}

function applyDelta(
  state: AppState,
  params: Record<string, unknown> | undefined,
  field: "text" | "arguments" | "result",
): AppState {
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
      [field]: `${item[field] ?? ""}${delta}`,
    })),
  );
}

function applyReplace(
  state: AppState,
  params: Record<string, unknown> | undefined,
  field: "text" | "arguments" | "result",
): AppState {
  const threadID = threadIDFromParams(params);
  const turnID = params?.turn_id as string | undefined;
  const itemID = params?.item_id as string | undefined;
  const text = params?.text;
  if (!turnID || !itemID || typeof text !== "string") {
    return state;
  }
  return updateThreadByID(state, threadID, (thread) =>
    updateTurnItem(thread, turnID, itemID, (item) => ({
      ...item,
      [field]: text,
    })),
  );
}

function threadIDFromParams(
  params: Record<string, unknown> | undefined,
): string | undefined {
  const threadID = params?.thread_id;
  return typeof threadID === "string" && threadID ? threadID : undefined;
}

type SortableSessionTabProps = {
  id: string;
  active: boolean;
  running: boolean;
  pendingSwitch: boolean;
  label: string;
  closeLabel: string;
  reorderable: boolean;
  onSelect: () => void;
  onClose: () => void;
};

function SortableSessionTab({
  id,
  active,
  running,
  pendingSwitch,
  label,
  closeLabel,
  reorderable,
  onSelect,
  onClose,
}: SortableSessionTabProps): JSX.Element {
  const {
    attributes,
    listeners,
    setActivatorNodeRef,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({
    id,
    disabled: !reorderable,
  });
  const style: CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
  };
  return (
    <div
      ref={setNodeRef}
      className={`session-tab${active ? " active" : ""}${running ? " running" : ""}${
        pendingSwitch ? " pending-switch" : ""
      }${reorderable ? " can-reorder" : ""}${isDragging ? " dragging" : ""}`}
      style={style}
      aria-grabbed={isDragging || undefined}
    >
      <button
        ref={setActivatorNodeRef}
        className="session-tab-main"
        type="button"
        aria-current={active ? "page" : undefined}
        aria-busy={pendingSwitch}
        title={label}
        onClick={onSelect}
        {...attributes}
        {...listeners}
      >
        <span className="session-tab-status" aria-hidden="true" />
        <span className="session-tab-title">{label}</span>
      </button>
      <button
        className="session-tab-close"
        type="button"
        draggable={false}
        aria-label={closeLabel}
        title={closeLabel}
        onClick={(event) => {
          event.stopPropagation();
          onClose();
        }}
      >
        <X size={13} />
      </button>
    </div>
  );
}

function SessionTabDragPreview({
  active,
  running,
  label,
  width,
}: {
  active: boolean;
  running: boolean;
  label: string;
  width?: number;
}): JSX.Element {
  return (
    <div
      className={`session-tab session-tab-drag-overlay${active ? " active" : ""}${running ? " running" : ""}`}
      style={width ? { width } : undefined}
    >
      <div className="session-tab-main">
        <span className="session-tab-status" aria-hidden="true" />
        <span className="session-tab-title">{label}</span>
      </div>
      <div className="session-tab-close" aria-hidden="true">
        <X size={13} />
      </div>
    </div>
  );
}

function updateThreadByID(
  state: AppState,
  threadID: string | undefined,
  update: (thread: Thread) => Thread,
  activePatch: Partial<Pick<AppState, "running" | "status">> = {},
): AppState {
  if (!threadID) {
    return state;
  }
  const primaryActive = state.thread?.id === threadID;
  const secondaryActive = state.secondaryThread?.id === threadID;
  if (
    (primaryActive && state.thread) ||
    (secondaryActive && state.secondaryThread)
  ) {
    const currentThread = primaryActive ? state.thread : state.secondaryThread;
    if (!currentThread) {
      return state;
    }
    const thread = update(currentThread);
    const patch =
      activeThreadIDForState(state) === threadID ||
      activePatch.running === false
        ? activePatch
        : {};
    return {
      ...state,
      ...patch,
      thread: primaryActive ? thread : state.thread,
      secondaryThread: secondaryActive ? thread : state.secondaryThread,
      threads: upsertThread(state.threads, thread),
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

function updateThread(
  state: AppState,
  update: (thread: Thread) => Thread,
): AppState {
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
    .filter(
      (thread): thread is Thread =>
        isThread(thread) && !thread.archived && !thread.read_only,
    )
    .sort((left, right) => threadTime(right) - threadTime(left));
}

function mergeListedThreads(current: Thread[], listed: Thread[]): Thread[] {
  const currentByID = new Map(
    current.filter(isThread).map((thread) => [thread.id, thread]),
  );
  return sortThreads(
    listed.map((thread) => {
      const existing = currentByID.get(thread.id);
      if (!existing || thread.turns.length > 0 || existing.turns.length === 0) {
        return thread;
      }
      return { ...thread, turns: existing.turns };
    }),
  );
}

function conversationSearchThreadMeta(thread: Thread): string {
  const updatedAt = threadTime(thread);
  const timeLabel =
    updatedAt > 0 ? conversationSearchTimeLabel(updatedAt) : "未知时间";
  return thread.pinned ? `置顶 · ${timeLabel}` : timeLabel;
}

function conversationSearchTimeLabel(atMs: number, nowMs = Date.now()): string {
  const elapsedMs = Math.max(0, nowMs - atMs);
  if (elapsedMs < 60_000) {
    return "刚刚";
  }
  if (elapsedMs < 60 * 60_000) {
    return `${Math.floor(elapsedMs / 60_000)}分钟前`;
  }

  const date = new Date(atMs);
  const now = new Date(nowMs);
  if (sameCalendarDay(date, now)) {
    return `今天 ${formatHourMinute(date)}`;
  }

  const yesterday = new Date(now);
  yesterday.setDate(now.getDate() - 1);
  if (sameCalendarDay(date, yesterday)) {
    return `昨天 ${formatHourMinute(date)}`;
  }

  if (date.getFullYear() === now.getFullYear()) {
    return `${date.getMonth() + 1}月${date.getDate()}日`;
  }
  return `${date.getFullYear()}/${date.getMonth() + 1}/${date.getDate()}`;
}

function sameCalendarDay(left: Date, right: Date): boolean {
  return (
    left.getFullYear() === right.getFullYear() &&
    left.getMonth() === right.getMonth() &&
    left.getDate() === right.getDate()
  );
}

function formatHourMinute(date: Date): string {
  return date.toLocaleTimeString("zh-CN", {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
}

function conversationSearchResultSections(
  results: ThreadSearchResultItem[],
  query: string,
): ConversationSearchResultSection[] {
  if (query.trim()) {
    return results.length > 0
      ? [{ title: "搜索结果", results, startIndex: 0 }]
      : [];
  }
  const pinned = results.filter((result) => result.thread.pinned);
  const recent = results.filter((result) => !result.thread.pinned);
  const sections: ConversationSearchResultSection[] = [];
  if (pinned.length > 0) {
    sections.push({ title: "置顶对话", results: pinned, startIndex: 0 });
  }
  if (recent.length > 0) {
    sections.push({
      title: "最近对话",
      results: recent,
      startIndex: pinned.length,
    });
  }
  return sections;
}

function conversationSearchContextLabel(
  thread: Thread,
  projects: DesktopProject[],
): string {
  const project = projects.find((candidate) => candidate.path === thread.cwd);
  return project?.name ?? fileNameFromPath(thread.cwd) ?? "wuu";
}

function pinnedThreads(threads: Thread[]): Thread[] {
  return sortThreads(threads).filter((thread) => thread.pinned);
}

function projectThreads(threads: Thread[]): Thread[] {
  return sortThreads(threads).filter((thread) => !thread.pinned);
}

function createDraftSessionTab(
  id: string,
  context: RuntimeContext,
  draft: ComposerDraftState = emptyComposerDraft(),
): SessionTab {
  return {
    id,
    kind: "draft",
    context,
    title: "新对话",
    prompt: draft.prompt,
    images: draft.images.map((image) => ({ ...image })),
    createdAt: Date.now(),
  };
}

function createThreadSessionTab(
  thread: Thread,
  context: RuntimeContext,
  draft: ComposerDraftState = emptyComposerDraft(),
): SessionTab {
  return {
    id: threadSessionTabID(thread.id),
    kind: "thread",
    context,
    threadID: thread.id,
    title: threadDisplayTitle(thread),
    prompt: draft.prompt,
    images: draft.images.map((image) => ({ ...image })),
  };
}

function createFileSessionTab(
  context: RuntimeContext,
  path: string,
): SessionTab {
  return {
    id: fileSessionTabID(context, path),
    kind: "file",
    context,
    path,
    title: fileNameFromPath(path),
  };
}

function createSkillsSessionTab(context: RuntimeContext): SessionTab {
  return {
    id: skillsSessionTabID(context),
    kind: "skills",
    context,
    title: "Skills",
  };
}

function threadSessionTabID(threadID: string): string {
  return `thread:${threadID}`;
}

function fileSessionTabID(context: RuntimeContext, path: string): string {
  return `file:${runtimeContextKey(context)}:${encodeURIComponent(path)}`;
}

function skillsSessionTabID(context: RuntimeContext): string {
  return `skills:${runtimeContextKey(context)}`;
}

function runtimeContextKey(context: RuntimeContext): string {
  return context.kind === "project"
    ? `project:${context.project_id}`
    : `no_project:${context.cwd}`;
}

function draftSessionTabIDForContext(context: RuntimeContext): string {
  return `${INITIAL_DRAFT_SESSION_TAB_ID}:${runtimeContextKey(context)}`;
}

function draftSessionTabForContext(
  tabs: SessionTab[],
  context: RuntimeContext,
): SessionTab | undefined {
  for (let index = tabs.length - 1; index >= 0; index -= 1) {
    const tab = tabs[index];
    if (tab.kind === "draft" && sameRuntimeContext(tab.context, context)) {
      return tab;
    }
  }
  return undefined;
}

function sessionTabForLoadedRuntime(
  tabs: SessionTab[],
  context: RuntimeContext,
  thread: Thread | undefined,
): SessionTab {
  if (thread) {
    return createThreadSessionTab(
      thread,
      context,
      sessionTabDraftForThreadID(tabs, thread.id),
    );
  }
  return (
    draftSessionTabForContext(tabs, context) ??
    createDraftSessionTab(draftSessionTabIDForContext(context), context)
  );
}

function withLoadedRuntimeSessionTab(
  current: AppState,
  loadedState: Partial<AppState>,
): AppState {
  const next = {
    ...current,
    ...loadedState,
  };
  const context = loadedState.activeContext;
  if (!context) {
    return next;
  }
  const tab = sessionTabForLoadedRuntime(
    current.sessionTabs,
    context,
    loadedState.thread,
  );
  return {
    ...next,
    sessionTabs: ensureSessionTab(current.sessionTabs, tab),
    activeSessionTabID: tab.id,
  };
}

function activeSessionTab(state: AppState): SessionTab | undefined {
  return state.sessionTabs.find((tab) => tab.id === state.activeSessionTabID);
}

function ensureSessionTab(tabs: SessionTab[], tab: SessionTab): SessionTab[] {
  const index = tabs.findIndex((candidate) => candidate.id === tab.id);
  if (index < 0) {
    return [...tabs, tab];
  }
  const next = tabs.slice();
  next[index] = { ...tab };
  return next;
}

function removeSessionTab(tabs: SessionTab[], tabID: string): SessionTab[] {
  return tabs.filter((tab) => tab.id !== tabID);
}

function persistActiveSessionTabDraft(
  state: AppState,
  draft: ComposerDraftState,
): AppState {
  const activeTabID = state.activeSessionTabID;
  return {
    ...state,
    sessionTabs: state.sessionTabs.map((tab) =>
      tab.id === activeTabID && (tab.kind === "draft" || tab.kind === "thread")
        ? {
            ...tab,
            prompt: draft.prompt,
            images: draft.images.map((image) => ({ ...image })),
          }
        : tab,
    ),
  };
}

function bindActiveSessionTabToThread(
  tabs: SessionTab[],
  activeTabID: string,
  thread: Thread,
  context: RuntimeContext,
): SessionTab[] {
  const threadTab = createThreadSessionTab(thread, context);
  const existingThreadTab = tabs.find((tab) => tab.id === threadTab.id);
  if (existingThreadTab) {
    return tabs
      .filter((tab) => tab.id !== activeTabID || tab.id === threadTab.id)
      .map((tab) => (tab.id === threadTab.id ? threadTab : tab));
  }
  return tabs.map((tab) => (tab.id === activeTabID ? threadTab : tab));
}

function sessionTabDraftForThread(
  state: AppState,
  threadID: string,
): ComposerDraftState {
  return sessionTabDraftForThreadID(state.sessionTabs, threadID);
}

function sessionTabDraftForThreadID(
  tabs: SessionTab[],
  threadID: string,
): ComposerDraftState {
  const tab = tabs.find(
    (item) => item.kind === "thread" && item.threadID === threadID,
  );
  return tab ? cloneSessionTabDraft(tab) : emptyComposerDraft();
}

function cloneSessionTabDraft(tab: SessionTab): ComposerDraftState {
  if (tab.kind === "file" || tab.kind === "skills") {
    return emptyComposerDraft();
  }
  return {
    prompt: tab.prompt,
    images: tab.images.map((image) => ({ ...image })),
  };
}

function threadForTab(state: AppState, threadID: string): Thread | undefined {
  if (state.thread?.id === threadID) {
    return state.thread;
  }
  if (state.secondaryThread?.id === threadID) {
    return state.secondaryThread;
  }
  return state.threads.find((thread) => thread.id === threadID);
}

function sessionTabLabel(tab: SessionTab, state: AppState): string {
  if (tab.kind === "draft") {
    const draftTitle = tab.prompt.trim().split(/\s+/).slice(0, 8).join(" ");
    return draftTitle || tab.title;
  }
  if (tab.kind === "file") {
    return tab.title || fileNameFromPath(tab.path);
  }
  if (tab.kind === "skills") {
    return tab.title;
  }
  return threadDisplayTitle(
    threadForTab(state, tab.threadID),
    state.threads,
    tab.title || "未命名对话",
  );
}

function fileNameFromPath(path: string): string {
  return path.split(/[\\/]/).filter(Boolean).at(-1) ?? path;
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
  const tab = activeSessionTab(state);
  if (tab?.kind === "file" || tab?.kind === "skills") {
    return undefined;
  }
  if (state.activePane === "secondary" && state.secondaryThread) {
    return state.secondaryThread;
  }
  return state.thread;
}

function threadForPane(
  state: AppState,
  pane: ConversationPaneID,
): Thread | undefined {
  return pane === "secondary" ? state.secondaryThread : state.thread;
}

function activeThreadIDForState(state: AppState): string | undefined {
  return activeThreadForState(state)?.id;
}

function latestPlanUpdateForThread(
  thread: Thread | undefined,
): PlanUpdate | undefined {
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

function parsePlanUpdateArguments(
  argumentsJSON: string,
): PlanUpdate | undefined {
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
      if (
        !step ||
        (status !== "pending" &&
          status !== "in_progress" &&
          status !== "completed")
      ) {
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

function setThreadForPane(
  state: AppState,
  pane: ConversationPaneID,
  thread: Thread | undefined,
): AppState {
  if (pane === "secondary") {
    return { ...state, secondaryThread: thread };
  }
  return { ...state, thread };
}

function activeProjectID(
  context: RuntimeContext | undefined,
): string | undefined {
  return context?.kind === "project" ? context.project_id : undefined;
}

function sameRuntimeContext(
  left: RuntimeContext | undefined,
  right: RuntimeContext | undefined,
): boolean {
  if (!left || !right || left.kind !== right.kind) {
    return false;
  }
  if (left.kind === "project" && right.kind === "project") {
    return left.project_id === right.project_id;
  }
  return left.cwd === right.cwd;
}

function threadMatchesActiveContext(
  thread: Thread,
  context: RuntimeContext | undefined,
): boolean {
  return Boolean(context && thread.cwd === context.cwd);
}

function isThread(value: unknown): value is Thread {
  return Boolean(
    value &&
    typeof value === "object" &&
    typeof (value as Thread).id === "string",
  );
}

function isThreadRunning(thread: Thread | undefined): boolean {
  return Boolean(
    thread?.status === "in_progress" ||
    thread?.turns.some((turn) => turn.status === "in_progress"),
  );
}

function isStateActiveThreadRunning(state: AppState): boolean {
  return Boolean(state.running || isThreadRunning(activeThreadForState(state)));
}

function isAnyThreadRunning(state: AppState): boolean {
  return Boolean(
    state.running ||
    isThreadRunning(state.thread) ||
    isThreadRunning(state.secondaryThread) ||
    state.threads.some(isThreadRunning),
  );
}

function visibleAskRequestForThread(
  requests: AskRequestState[],
  threadID: string | undefined,
): AskRequestState | undefined {
  for (let index = requests.length - 1; index >= 0; index--) {
    const request = requests[index];
    if (!request.threadID || request.threadID === threadID) {
      return request;
    }
  }
  return undefined;
}

function pendingAskThreadIDsForRequests(
  requests: AskRequestState[],
): Set<string> {
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
  threadID: string | undefined,
): AnsweredAskRequestState[] {
  return requests.filter(
    (request) => !request.threadID || request.threadID === threadID,
  );
}

function upsertAskRequest(
  requests: AskRequestState[],
  request: AskRequestState,
): AskRequestState[] {
  const index = requests.findIndex((item) => item.id === request.id);
  if (index < 0) {
    return [...requests, request];
  }
  const next = requests.slice();
  next[index] = request;
  return next;
}

function removeAskRequest(
  requests: AskRequestState[],
  id: string,
): AskRequestState[] {
  return requests.filter((request) => request.id !== id);
}

function upsertAnsweredAskRequest(
  requests: AnsweredAskRequestState[],
  request: AnsweredAskRequestState,
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
    return threadWithTurnSummary(
      {
        ...thread,
        turns: [...thread.turns, { ...turn, items: orderedTurnItems(turn.items) }],
        status,
      },
      turn,
    );
  }
  const turns = thread.turns.slice();
  turns[index] = { ...turn, items: mergeTurnItemsInOrder(turns[index], turn) };
  return threadWithTurnSummary({ ...thread, turns, status }, turn);
}

function threadWithTurnSummary(thread: Thread, turn: Turn): Thread {
  const preview = hasText(thread.preview) ? thread.preview : turnPreview(turn);
  return {
    ...thread,
    preview,
    updated_at: laterTimestamp(
      thread.updated_at,
      turn.completed_at ?? turn.started_at,
    ),
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

function laterTimestamp(
  current: string,
  candidate: string | null | undefined,
): string {
  if (!candidate) {
    return current;
  }
  const currentTime = Date.parse(current);
  const candidateTime = Date.parse(candidate);
  if (!Number.isFinite(candidateTime)) {
    return current;
  }
  return !Number.isFinite(currentTime) || candidateTime > currentTime
    ? candidate
    : current;
}

function updateTurnItem(
  thread: Thread,
  turnID: string,
  itemID: string,
  update: (item: ThreadItem) => ThreadItem,
): Thread {
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

function upsertTurnItem(
  thread: Thread,
  turnID: string,
  item: ThreadItem,
): Thread {
  const turns = thread.turns.map((turn) => {
    if (turn.id !== turnID) {
      return turn;
    }
    return { ...turn, items: upsertTurnItemInOrder(turn, item) };
  });
  return { ...thread, turns };
}

function upsertThreadChildAgent(thread: Thread, agent: Agent): Thread {
  const current = thread.child_agents ?? [];
  const index = current.findIndex((item) => item.id === agent.id);
  const nextAgent = mergeAgentSummary(
    index >= 0 ? current[index] : undefined,
    agent,
  );
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
    nested_running_count:
      incoming.nested_running_count ?? current.nested_running_count,
    started_at: incoming.started_at ?? current.started_at,
    completed_at: incoming.completed_at ?? current.completed_at,
  };
}

function turnNoticeDisplay(
  turn: Turn,
  hasAssistantOutput: boolean,
): UserFacingErrorDisplay | undefined {
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
      detail: hasAssistantOutput
        ? "已保留已生成内容，可以继续发送消息。"
        : "这次请求已停止，没有生成回复内容。",
    };
  }
  return {
    ...baseDisplay,
    detail: hasAssistantOutput
      ? `${baseDisplay.detail} 已保留已生成内容。`
      : baseDisplay.detail,
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

function completedAgentTextItem(turn: Turn, item: ThreadItem): boolean {
  if (item.type !== "agent_message") {
    return false;
  }
  const status =
    item.status ?? (turn.status === "in_progress" ? "in_progress" : "completed");
  return (
    status === "completed" &&
    streamFieldValue(turn.id, item, "text").trim().length > 0
  );
}

function completedAgentMessageFollows(turn: Turn, itemIndex: number): boolean {
  for (let index = itemIndex + 1; index < turn.items.length; index++) {
    const item = turn.items[index];
    if (item.type === "user_message") {
      return false;
    }
    if (item.type !== "agent_message") {
      continue;
    }
    if (streamFieldValue(turn.id, item, "text").trim().length === 0) {
      continue;
    }
    return completedAgentTextItem(turn, item);
  }
  return false;
}

function compactProcessCheckpointText(value: string): string {
  return value.replace(/\s+/g, " ").trim();
}

function TurnNotice({
  display,
}: {
  display: UserFacingErrorDisplay;
}): JSX.Element {
  const Icon = turnNoticeIcon(display);
  return (
    <aside
      className={`turn-notice ${display.tone}`}
      role={
        display.tone === "error" || display.tone === "auth" ? "alert" : "status"
      }
    >
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
  onForkMessage,
}: {
  turn: Turn;
  cwd?: string;
  latestAgentMessageID?: string;
  onStreamFrame: () => void;
  onForkMessage?: (turnID: string, itemID: string) => void;
}): JSX.Element {
  const renderedItems: JSX.Element[] = [];
  let processEntries: TurnProcessEntry[] = [];
  let processCheckpoint: TurnProcessCheckpoint | undefined;
  let statusInserted = false;
  const flowAgentMessageID =
    turn.status === "completed"
      ? messageFlowAgentMessageItemID(turn)
      : undefined;
  const actionableAgentMessageID =
    turn.status === "completed" ? flowAgentMessageID : undefined;
  const processAutoCollapse =
    turn.status === "completed" && actionableAgentMessageID !== undefined;

  function renderThreadItem(
    item: ThreadItem,
    streaming: boolean,
  ): JSX.Element | null {
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
      element: <TurnStatusLine key={`${turn.id}-status`} turn={turn} />,
    });
    statusInserted = true;
  }

  function appendProcessEntry(entry: TurnProcessEntry | null): void {
    if (entry) {
      processEntries.push(entry);
    }
  }

  function updateProcessCheckpoint(item: ThreadItem): void {
    const text = compactProcessCheckpointText(
      streamFieldValue(turn.id, item, "text"),
    );
    if (!text) {
      return;
    }
    processCheckpoint = { key: item.id, text };
  }

  function flushProcessEntries(autoCollapse = processAutoCollapse): void {
    if (processEntries.length === 0 && !processCheckpoint) {
      return;
    }
    const entries = processEntries;
    const checkpoint = processCheckpoint;
    processEntries = [];
    processCheckpoint = undefined;
    const onlyCompletedStatus =
      !checkpoint &&
      autoCollapse &&
      entries.every((entry) => entry.kind === "status");
    if (onlyCompletedStatus) {
      return;
    }
    const detailEntries = entries.filter((entry) => entry.kind !== "status");
    if (detailEntries.length === 0 && !checkpoint) {
      if (!autoCollapse) {
        const statusEntry = entries.find((entry) => entry.kind === "status");
        if (statusEntry) {
          renderedItems.push(statusEntry.element);
        }
      }
      return;
    }
    renderedItems.push(
      <TurnProcessGroup
        key={`${turn.id}-process-${renderedItems.length}`}
        turn={turn}
        entries={detailEntries}
        checkpoint={checkpoint}
        autoCollapse={autoCollapse}
        showTurnStatus={entries.some((entry) => entry.kind === "status")}
      />,
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

    if (item.type === "agent_message") {
      insertStatus();
      const rendered = renderThreadItem(
        item,
        turn.status === "in_progress" && item.status === "in_progress",
      );
      if (!rendered) {
        continue;
      }
      if (completedAgentMessageFollows(turn, index)) {
        updateProcessCheckpoint(item);
        continue;
      }
      flushProcessEntries(completedAgentTextItem(turn, item));
      renderedItems.push(rendered);
      continue;
    }

    insertStatus();

    if (item.type === "tool_call" || item.type === "collab_agent_tool_call") {
      const group = [item];
      let nextIndex = index + 1;
      while (
        nextIndex < turn.items.length &&
        (turn.items[nextIndex].type === "tool_call" ||
          turn.items[nextIndex].type === "collab_agent_tool_call")
      ) {
        group.push(turn.items[nextIndex]);
        nextIndex++;
      }
      appendProcessEntry({
        key: `${item.id}-activity`,
        kind: "activity",
        element: (
          <ToolActivityTimeline
            key={`${item.id}-activity`}
            items={group}
            collapseWhenIdle={
              processAutoCollapse ||
              completedAgentMessageFollows(turn, nextIndex - 1)
            }
            revealItems={turn.status === "in_progress"}
          />
        ),
      });
      index = nextIndex - 1;
      continue;
    }

    const rendered = renderThreadItem(
      item,
      turn.status === "in_progress" && item.status === "in_progress",
    );
    if (!rendered) {
      continue;
    }
    appendProcessEntry({ key: item.id, kind: "activity", element: rendered });
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
  kind: "status" | "activity";
  element: JSX.Element;
};

type TurnProcessCheckpoint = {
  key: string;
  text: string;
};

function TurnProcessGroup({
  turn,
  entries,
  checkpoint,
  autoCollapse,
  showTurnStatus,
}: {
  turn: Turn;
  entries: TurnProcessEntry[];
  checkpoint?: TurnProcessCheckpoint;
  autoCollapse: boolean;
  showTurnStatus: boolean;
}): JSX.Element {
  const [expanded, setExpanded] = useState(false);
  const detailsID = `${turn.id}-process-details`;
  const hasDetails = entries.length > 0;
  const className = `turn-process-group${expanded ? " expanded" : " collapsed"}${autoCollapse ? " auto-collapsed" : ""}${
    hasDetails ? "" : " no-details"
  }`;
  const processCount = entries.length;
  const completedDuration =
    typeof turn.duration_ms === "number" ? turn.duration_ms : undefined;
  const startedAt = parseTurnTimestampMs(turn.started_at);
  const liveDuration =
    showTurnStatus &&
    completedDuration === undefined &&
    turn.status === "in_progress" &&
    Number.isFinite(startedAt);
  const liveNow = useLiveNow(liveDuration);
  const elapsedMs =
    completedDuration ?? (liveDuration ? Math.max(0, liveNow - startedAt) : 0);
  const processLabel = showTurnStatus
    ? turnProgressContent(turn, elapsedMs, turnHasAssistantOutput(turn)).label
    : messageFlowStatusLabel({
        done: true,
        failed: turn.status === "failed",
        hasFinalText: turnHasAssistantOutput(turn),
        locale: "zh",
      });
  const primaryLabel = checkpoint?.text ?? processLabel;
  const metaParts = turnProcessMetaParts(
    turn,
    processCount,
    elapsedMs,
    showTurnStatus,
  );

  const toggleContent = (
    <>
      <span className="turn-process-copy">
        <span
          className={`turn-process-primary${checkpoint ? " checkpoint" : ""}`}
          title={checkpoint?.text}
        >
          <span
            className="turn-process-primary-text"
            key={checkpoint?.key ?? primaryLabel}
          >
            {primaryLabel}
          </span>
        </span>
        {metaParts.map((part) => (
          <span className="turn-process-meta" key={part}>
            {part}
          </span>
        ))}
      </span>
      {hasDetails ? (
        <ChevronDown className="turn-process-chevron" size={15} />
      ) : null}
    </>
  );

  return (
    <div className={className}>
      {hasDetails ? (
        <button
          className="turn-process-toggle"
          type="button"
          aria-expanded={expanded}
          aria-controls={detailsID}
          onClick={() => setExpanded((open) => !open)}
        >
          {toggleContent}
        </button>
      ) : (
        <div className="turn-process-toggle turn-process-toggle-static">
          {toggleContent}
        </div>
      )}
      {hasDetails ? (
        <CollapsibleDetails
          className="turn-process-details"
          id={detailsID}
          expanded={expanded}
          innerClassName="turn-process-stack"
        >
          {entries.map((entry) => entry.element)}
        </CollapsibleDetails>
      ) : null}
    </div>
  );
}

function turnProcessMetaParts(
  turn: Turn,
  processCount: number,
  elapsedMs: number,
  showTurnStatus: boolean,
): string[] {
  const parts: string[] = [];
  if (showTurnStatus && turn.status === "in_progress") {
    parts.push(formatDuration(elapsedMs));
    return parts;
  }
  if (processCount > 0) {
    parts.push(`${processCount} 项`);
  }
  if (showTurnStatus && typeof turn.duration_ms === "number") {
    parts.push(formatDuration(turn.duration_ms));
  }
  return parts;
}

function TurnStatusLine({ turn }: { turn: Turn }): JSX.Element {
  const completedDuration =
    typeof turn.duration_ms === "number" ? turn.duration_ms : undefined;
  const startedAt = parseTurnTimestampMs(turn.started_at);
  const liveDuration =
    completedDuration === undefined &&
    turn.status === "in_progress" &&
    Number.isFinite(startedAt);
  const liveNow = useLiveNow(liveDuration);
  const elapsedMs =
    completedDuration ?? (liveDuration ? Math.max(0, liveNow - startedAt) : 0);
  const content = turnProgressContent(
    turn,
    elapsedMs,
    turnHasAssistantOutput(turn),
  );

  return (
    <div
      className={`turn-progress ${turn.status}`}
      role={liveDuration ? "status" : undefined}
      aria-live={liveDuration ? "polite" : undefined}
    >
      <span className="turn-progress-title">{content.label}</span>
    </div>
  );
}

function turnProgressContent(
  turn: Turn,
  elapsedMs: number,
  hasFinalText: boolean,
): TurnProgressContent {
  if (turn.status === "interrupted") {
    return { label: "已停止", detail: "这次请求已停止" };
  }
  if (turn.status !== "in_progress") {
    return {
      label: messageFlowStatusLabel({
        done: true,
        failed: turn.status === "failed",
        hasFinalText,
        locale: "zh",
      }),
    };
  }

  const runningTool = turn.items.find(
    (item) =>
      (item.type === "tool_call" || item.type === "collab_agent_tool_call") &&
      (item.status ?? "in_progress") === "in_progress",
  );
  if (runningTool) {
    return {
      label: messageFlowStatusLabel({
        done: false,
        failed: false,
        hasFinalText,
        locale: "zh",
      }),
    };
  }

  const latestItem = latestDebugItem(turn);
  if (!latestItem) {
    return {
      label: messageFlowStatusLabel({
        done: false,
        failed: false,
        hasFinalText,
        locale: "zh",
      }),
      detail: waitingDetail(elapsedMs, "已收到请求，正在等待模型回应"),
    };
  }
  if (latestItem.type === "agent_message") {
    const hasText =
      hasFinalText || debugStreamFieldLength(turn.id, latestItem, "text") > 0;
    return {
      label: messageFlowStatusLabel({
        done: false,
        failed: false,
        hasFinalText: hasText,
        locale: "zh",
      }),
      detail: hasText ? undefined : waitingDetail(elapsedMs, "正在组织回答"),
    };
  }
  if (latestItem.type === "reasoning") {
    return {
      label: messageFlowStatusLabel({
        done: false,
        failed: false,
        hasFinalText,
        locale: "zh",
      }),
      detail: waitingDetail(elapsedMs, "正在组织回答"),
    };
  }
  if (
    latestItem.type === "tool_call" ||
    latestItem.type === "collab_agent_tool_call"
  ) {
    return {
      label: messageFlowStatusLabel({
        done: false,
        failed: false,
        hasFinalText,
        locale: "zh",
      }),
    };
  }
  if (latestItem.type === "context_compaction") {
    return {
      label: messageFlowStatusLabel({
        done: false,
        failed: false,
        hasFinalText,
        locale: "zh",
      }),
    };
  }
  if (latestItem.type === "error") {
    return {
      label: messageFlowStatusLabel({
        done: false,
        failed: false,
        hasFinalText,
        locale: "zh",
      }),
    };
  }

  return {
    label: messageFlowStatusLabel({
      done: false,
      failed: false,
      hasFinalText,
      locale: "zh",
    }),
    detail: waitingDetail(elapsedMs, "请求正在处理中"),
  };
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

function messageFlowAgentMessageItemID(turn: Turn): string | undefined {
  const explicitFinalID = explicitFinalAgentMessageItemID(turn);
  if (explicitFinalID) {
    return explicitFinalID;
  }

  const finalIndex = messageFlowFinalTextIndex(turn.items, (item) => {
    if (item.type === "agent_message") {
      return streamFieldValue(turn.id, item, "text").trim().length > 0
        ? "text"
        : "ignore";
    }
    if (
      item.type === "reasoning" ||
      item.type === "tool_call" ||
      item.type === "collab_agent_tool_call" ||
      item.type === "context_compaction"
    ) {
      return "process";
    }
    return "ignore";
  });

  return finalIndex >= 0 ? turn.items[finalIndex]?.id : undefined;
}

function explicitFinalAgentMessageItemID(turn: Turn): string | undefined {
  for (let itemIndex = turn.items.length - 1; itemIndex >= 0; itemIndex--) {
    const item = turn.items[itemIndex];
    if (item.type !== "agent_message" || item.phase !== "final_answer") {
      continue;
    }
    if (streamFieldValue(turn.id, item, "text").trim().length > 0) {
      return item.id;
    }
  }
  return undefined;
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
  onForkMessage,
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
      const handoff = agentHandoffDisplay(text);
      if (handoff) {
        return (
          <div className="agent-handoff-line" role="status">
            {handoff.label}
          </div>
        );
      }
      const copyable = text.trim() !== "";
      return (
        <div
          className={`user-message-block${copyable ? " user-message-block-with-actions" : ""}`}
        >
          <div className="message user-message">
            {item.images?.length ? (
              <MessageImageGrid images={item.images} />
            ) : null}
            {text ? <RichContent text={text} cwd={cwd} /> : null}
          </div>
          {copyable ? (
            <div
              className="message-actions user-message-actions"
              aria-label="用户消息操作"
            >
              <MessageCopyButton
                getText={() => text}
                className="message-action-button"
                iconSize={15}
              />
            </div>
          ) : null}
        </div>
      );
    }
    case "agent_message": {
      const streamKeyValue = streamTextKey(turnID, item.id, "text");
      const agentText = streamTextStore.has(streamKeyValue)
        ? streamTextStore.get(streamKeyValue)
        : (item.text ?? "");
      const copyable = streaming || agentText.trim() !== "";
      const actionsVisible =
        turnStatus === "completed" &&
        item.id === actionableAgentMessageID &&
        copyable;
      const actionsPersistent =
        actionsVisible && item.id === latestAgentMessageID;
      return (
        <article
          className={`agent-block${
            actionsVisible
              ? ` agent-block-with-actions${actionsPersistent ? " agent-actions-persistent" : ""}`
              : ""
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
              onFork={
                onForkMessage ? () => onForkMessage(turnID, item.id) : undefined
              }
            />
          ) : null}
        </article>
      );
    }
    case "reasoning":
      return (
        <article className="reasoning-block">
          <ReasoningContent
            turnID={turnID}
            item={item}
            streaming={streaming}
            onStreamFrame={onStreamFrame}
          />
        </article>
      );
    case "tool_call":
    case "collab_agent_tool_call":
      return <ToolActivityRow items={[item]} />;
    case "context_compaction":
      return <div className="system-line">{item.text}</div>;
    case "error":
      return (
        <TurnNotice display={userFacingErrorForMessage(item.error, "turn")} />
      );
    default:
      return null;
  }
}

function AgentMessageContent({
  turnID,
  item,
  cwd,
  streaming,
  onStreamFrame,
}: {
  turnID: string;
  item: ThreadItem;
  cwd?: string;
  streaming: boolean;
  onStreamFrame: () => void;
}): JSX.Element {
  const streamKeyValue = streamTextKey(turnID, item.id, "text");
  const hasBufferedStream = streamTextStore.has(streamKeyValue);
  const [streamSettled, setStreamSettled] = useState(false);
  const liveStream = (streaming || hasBufferedStream) && !streamSettled;
  const settleMode = liveStream ? "stream" : "rich";

  useEffect(() => {
    setStreamSettled(false);
  }, [streamKeyValue]);

  return (
    <StreamingMarkdown
      streamKey={streamKeyValue}
      initialText={
        hasBufferedStream
          ? streamTextStore.seedValue(streamKeyValue)
          : item.text
      }
      cwd={cwd}
      final={!streaming}
      live={liveStream}
      settleMode={settleMode}
      onFrame={onStreamFrame}
      onSettled={() => {
        setStreamSettled(true);
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
  onStreamFrame,
}: {
  turnID: string;
  item: ThreadItem;
  streaming: boolean;
  onStreamFrame: () => void;
}): JSX.Element {
  const streamKeyValue = streamTextKey(turnID, item.id, "text");
  const hasBufferedStream = streamTextStore.has(streamKeyValue);
  const liveStream = streaming || hasBufferedStream;
  const [keepStreamSurface, setKeepStreamSurface] = useState(false);
  const settleMode = liveStream || keepStreamSurface ? "stream" : "rich";

  return (
    <StreamingMarkdown
      streamKey={streamKeyValue}
      initialText={
        hasBufferedStream
          ? streamTextStore.seedValue(streamKeyValue)
          : item.text
      }
      className="streaming-markdown reasoning-stream"
      final={!streaming}
      live={liveStream}
      settleMode={settleMode}
      onFrame={onStreamFrame}
      onSettled={() => {
        setKeepStreamSurface(true);
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
      turn,
    };
  }
  if (!state.initialized) {
    return {
      label: "运行时未就绪",
      detail: state.status || "等待初始化",
      tone:
        state.status === "connecting" || state.status === "opening"
          ? "running"
          : "warning",
      turn,
    };
  }
  if (state.running && !turn) {
    return {
      label: "正在发送请求",
      detail: "还没收到 turn/started",
      tone: "running",
    };
  }
  if (turn?.status === "in_progress") {
    const runningTool = turn.items.find(
      (item) =>
        (item.type === "tool_call" || item.type === "collab_agent_tool_call") &&
        (item.status ?? "in_progress") === "in_progress",
    );
    if (runningTool) {
      return {
        label: "正在调用工具",
        detail: readableToolName(runningTool.name),
        tone: "running",
        turn,
        activeItem: runningTool,
      };
    }

    const latestItem = latestDebugItem(turn);
    if (!latestItem) {
      return {
        label: "等待模型响应",
        detail: "turn 已开始，尚未收到回复 item",
        tone: "running",
        turn,
      };
    }
    if (latestItem.type === "agent_message") {
      const length = debugStreamFieldLength(turn.id, latestItem, "text");
      return {
        label: length > 0 ? "正在生成回复" : "回复已开始",
        detail:
          length > 0
            ? `已收到 ${length.toLocaleString()} 字`
            : "等待首个回复片段",
        tone: "running",
        turn,
        activeItem: latestItem,
      };
    }
    if (latestItem.type === "reasoning") {
      const length = debugStreamFieldLength(turn.id, latestItem, "text");
      return {
        label: "模型正在思考",
        detail:
          length > 0
            ? `已收到 ${length.toLocaleString()} 字思考内容`
            : "等待推理片段",
        tone: "running",
        turn,
        activeItem: latestItem,
      };
    }
    if (
      latestItem.type === "tool_call" ||
      latestItem.type === "collab_agent_tool_call"
    ) {
      return {
        label: "工具已返回",
        detail: "等待模型继续处理工具结果",
        tone: "running",
        turn,
        activeItem: latestItem,
      };
    }
    return {
      label: "本轮处理中",
      detail: debugItemTitle(latestItem),
      tone: "running",
      turn,
      activeItem: latestItem,
    };
  }
  if (turn?.status === "failed") {
    return {
      label: "处理失败",
      detail: turn.error?.message ?? "本轮返回失败状态",
      tone: "error",
      turn,
    };
  }
  if (turn?.status === "interrupted") {
    return {
      label: "已停止",
      detail: "本轮已被中断",
      tone: "warning",
      turn,
    };
  }
  if (turn?.status === "completed") {
    return {
      label: "已完成",
      detail:
        turn.duration_ms === undefined
          ? "本轮完成"
          : `耗时 ${formatDuration(turn.duration_ms)}`,
      tone: "success",
      turn,
    };
  }
  if (state.running) {
    return {
      label: "运行中",
      detail: state.status || "等待事件",
      tone: "running",
      turn,
    };
  }
  return {
    label: state.status === "ready" ? "空闲" : "当前状态",
    detail: state.status === "ready" ? "可以发送新消息" : state.status,
    tone: state.status === "ready" ? "idle" : "warning",
    turn,
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

function streamFieldValue(
  turnID: string,
  item: ThreadItem,
  field: StreamTextField,
): string {
  const key = streamTextKey(turnID, item.id, field);
  return streamTextStore.has(key)
    ? streamTextStore.get(key)
    : (item[field] ?? "");
}

function debugStreamFieldLength(
  turnID: string,
  item: ThreadItem,
  field: StreamTextField,
): number {
  return streamFieldValue(turnID, item, field).length;
}

function runDebugEventFromServerEvent(
  event: ServerEvent,
  deltaSeen: Set<string>,
): Omit<RunDebugEvent, "id" | "at"> | undefined {
  switch (event.kind) {
    case "server-request":
      return {
        source: "server",
        method: event.message.method,
        detail: "服务端正在等待客户端响应",
        tone: "warning",
      };
    case "server-error":
      return {
        source: "server",
        method: "server/error",
        detail: event.message,
        tone: "error",
      };
    case "server-exit":
      return {
        source: "server",
        method: "server/exit",
        detail: `app-server 退出：${event.code ?? "unknown"}`,
        tone: "error",
      };
    case "notification":
      return runDebugEventFromNotification(event.message, deltaSeen);
  }
}

function runDebugEventFromNotification(
  notification: AppServerNotification,
  deltaSeen: Set<string>,
): Omit<RunDebugEvent, "id" | "at"> | undefined {
  const params = isRecord(notification.params)
    ? notification.params
    : undefined;
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
      itemID,
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
      turnID,
    };
  }

  if (
    notification.method === "item/started" ||
    notification.method === "item/completed"
  ) {
    const item = threadItemFromRecord(recordValue(params, "item"));
    if (!item) {
      return undefined;
    }
    return {
      source: "server",
      method: notification.method,
      detail: `${debugItemTitle(item)} · ${debugItemStatusLabel(item)}`,
      tone:
        item.status === "failed" || item.error
          ? "error"
          : notification.method === "item/completed"
            ? "success"
            : "running",
      threadID,
      turnID,
      itemID: item.id,
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
      turnID: turn?.id ?? turnID,
    };
  }

  if (
    notification.method === "turn/completed" ||
    notification.method === "turn/error"
  ) {
    const turn = turnFromRecord(recordValue(params, "turn"));
    const failed =
      notification.method === "turn/error" || turn?.status === "failed";
    return {
      source: "server",
      method: notification.method,
      detail: failed
        ? (stringValue(params, "error") ?? "本轮失败")
        : "本轮完成",
      tone: failed ? "error" : "success",
      threadID,
      turnID: turn?.id ?? turnID,
    };
  }

  if (
    notification.method === "thread/started" ||
    notification.method === "thread/resumed"
  ) {
    const thread = threadFromRecord(recordValue(params, "thread"));
    return {
      source: "server",
      method: notification.method,
      detail: thread ? `Thread ${shortDebugID(thread.id)}` : "Thread 已更新",
      tone: "info",
      threadID: thread?.id ?? threadID,
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
  return (
    eventType === "content_delta" ||
    eventType === "thinking_delta" ||
    eventType === "tool_use_delta"
  );
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
  if (
    eventType === "tool_use_start" ||
    eventType === "tool_use_end" ||
    eventType === "lifecycle"
  ) {
    return "running";
  }
  return "info";
}

function threadItemFromRecord(
  record: JsonRecord | undefined,
): ThreadItem | undefined {
  if (
    !record ||
    typeof record.id !== "string" ||
    typeof record.type !== "string"
  ) {
    return undefined;
  }
  return record as ThreadItem;
}

function turnFromRecord(record: JsonRecord | undefined): Turn | undefined {
  if (
    !record ||
    typeof record.id !== "string" ||
    !Array.isArray(record.items)
  ) {
    return undefined;
  }
  return record as Turn;
}

function threadFromRecord(record: JsonRecord | undefined): Thread | undefined {
  if (
    !record ||
    typeof record.id !== "string" ||
    !Array.isArray(record.turns)
  ) {
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
    completed_at: stringValue(record, "completed_at"),
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
    hour12: false,
  });
}

function buildRunDebugSnapshot({
  state,
  events,
  queuedMessages,
  guideMessages,
  composerImages,
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
    `variant: ${state.initialized?.variant ?? ""}`,
    `cwd: ${state.activeContext?.cwd ?? thread?.cwd ?? ""}`,
    `thread: ${thread?.id ?? ""}`,
    `turn: ${turn?.id ?? ""}`,
    `turn_status: ${turn?.status ?? ""}`,
    `turn_error: ${turn?.error?.message ?? ""}`,
    `queued_messages: ${queuedMessages.length}`,
    `guide_messages: ${guideMessages.length}`,
    `composer_images: ${composerImages.length}`,
  ];

  lines.push("");
  lines.push("items:");
  if (turn?.items.length) {
    for (const item of turn.items) {
      lines.push(
        `- ${item.id} ${item.type} ${item.status ?? "in_progress"} ${item.name ?? ""} text=${debugStreamFieldLength(
          turn.id,
          item,
          "text",
        )} args=${debugStreamFieldLength(turn.id, item, "arguments")} result=${debugStreamFieldLength(turn.id, item, "result")} error=${
          item.error ?? ""
        }`,
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
      } item=${event.itemID ?? ""}`,
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
