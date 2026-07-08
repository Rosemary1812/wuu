/// <reference path="../shared/jsx-compat.d.ts" />

import {
  Bug,
  ChevronRight,
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
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
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
  ConversationSubthread,
  SubthreadUpdatedNotification,
  DesktopProject,
  GitCommitResult,
  GitPullRequestResult,
  GitStatusResult,
  InitializeResult,
  InputFile,
  InputImage,
  ParticipantProfile,
  ParticipantSaveParams,
  ParticipantSummary,
  PlanUpdate,
  PopOutInitResult,
  ProjectListResult,
  PermissionSummary,
  RuntimeAdvancedSettingsUpdate,
  RuntimeGeneralSettingsUpdate,
  RuntimeConnectionUpdate,
  RuntimeContext,
  ServerEvent,
  Thread,
  ThreadItem,
  Turn,
} from "../shared/protocol";
import {
  awaitComposerImages,
  composerFileFromFile,
  composerImagePlaceholder,
  createComposerMessage,
  createOptimisticCompactTurn,
  createOptimisticTurn,
  dropOptimisticTurn,
  earlierStartedAt,
  failOptimisticCompactTurn,
  inputFilesFromComposer,
  inputImagesFromComposer,
  isComposerImageFile,
  isPDFFile,
  replaceOptimisticTurn,
  revokeComposerImagePreview,
  type ComposerFile,
  type ComposerImage,
  type QueuedComposerMessage,
} from "./ComposerMessages";
import {
  emptyThreadPendingComposerMessages,
  findPendingComposerMessage,
  pendingComposerMessagesForThread,
  reconcilePendingComposerMessagesForThread,
  removePendingComposerMessagesByID,
  threadPendingComposerMessagesIsEmpty,
  type PendingComposerMessageRemovalScope,
  type PendingComposerMessagesByThread,
  type ThreadPendingComposerMessages,
} from "./ComposerPendingMessages";
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
import {
  createAgentTreeDemo,
  createConversationFixture,
  type ConversationFixtureKind,
} from "./ConversationFixtures";
import { ConversationSearchOverlay } from "./ConversationSearchOverlay";
import { ConversationSplitPane } from "./ConversationSplitPane";
import { useConversationScrollState } from "./ConversationScrollState";
import { useConversationSearch } from "./ConversationSearchState";
import { ConversationTurnList } from "./ConversationTurnList";
import { ChatThreadViewContainer } from "./ChatThreadViewContainer";
import { ConversationSubthreadPanel } from "./ConversationSubthreadPanel";
import { ParticipantProfilePanel } from "./ParticipantProfilePanel";
import { ConversationForkDialog, type ForkMode } from "./ConversationForkDialog";
import { ForkWorktreeNotice } from "./ForkWorktreeNotice";
import type { TurnFileDiffSelection } from "./TurnFileDiffTypes";
import { lastUserMessageAnchor } from "./TurnViewHelpers";
import {
  AppSidebar,
  reconcileSidebarSectionOrder,
  SIDEBAR_SECTION_AGENTS,
  SIDEBAR_SECTION_GROUP,
  SIDEBAR_SECTION_PINNED,
} from "./AppSidebar";
import {
  type EnvironmentPanelMenu,
  type EnvironmentPanelMotionState,
} from "./EnvironmentPanel";
import { EnvironmentSideStack } from "./EnvironmentSideStack";
import {
  activePlanUpdateForThread,
  activeProjectID,
  activeSessionTab,
  activeThreadForState,
  activeThreadIDForState,
  applySubthreadUpdatedNotification,
  latestContextUsageForThread,
  activeTurnIDForThread,
  activeTurnTokenSpeedSnapshot,
  applyLoadedRuntimeWithDraftCarry,
  appendStreamingTokenSample,
  bindActiveSessionTabToThread,
  computeBusyParticipantIDs,
  chatFocusValueForThread,
  cloneSessionTabDraft,
  cloneComposerDraft,
  composerDraftHasContent,
  composerSubmissionDetail,
  conversationPaneThreadsByID,
  createDraftSessionTab,
  createFileSessionTab,
  createBoardSessionTab,
  createSkillsSessionTab,
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
  isScratchThread,
  isStateActiveThreadRunning,
  isThread,
  isThreadRunning,
  isThreadUnread,
  latestPlanUpdateForThread,
  mergeListedThreads,
  markThreadTurnsViewed,
  mentionedParticipantIDsFromText,
  notificationTargetsActiveThread,
  openForkThreadAsPrimary,
  persistActiveSessionTabDraft,
  pinnedThreadSummaries,
  queryTextForUserItem,
  SCRATCH_PSEUDO_PROJECT_ID,
  scratchThreadSummaries,
  queryTextsForThread,
  reduceServerEvent,
  reconcileResumedThreadTurns,
  removeSessionTab,
  requireThread,
  resolveThreadRuntimeContext,
  runtimeContextKey,
  sameRuntimeContext,
  serverEventShouldRefreshGit,
  serverEventTargetsActiveContext,
  serverEventTargetsGlobalThread,
  shouldResetToNoProjectForNewThread,
  sessionTabForLoadedRuntime,
  sessionTabForParticipant,
  sessionTabDraftForThread,
  sessionTabLabel,
  setThreadForPane,
  sortThreads,
  summarizeThreadsForSidebar,
  threadBelongsToProject,
  threadForTab,
  threadNeedsResumeOnReselect,
  threadForPane,
  threadItemFromRecord,
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
  CONVERSATION_SPLIT_MAX_PERCENT,
  CONVERSATION_SPLIT_MIN_PERCENT,
  RIGHT_PANEL_MOTION_MS,
  SIDEBAR_MAX_WIDTH,
  SIDEBAR_MIN_WIDTH,
  SIDEBAR_MOTION_MS,
  THREAD_PANEL_MAX_WIDTH,
  THREAD_PANEL_MIN_WIDTH,
  WORKSPACE_RIGHT_PANEL_MAX_WIDTH,
  WORKSPACE_RIGHT_PANEL_MIN_WIDTH,
  useAppLayoutState,
} from "./AppLayoutState";
import { CommitChangesDialog, PullRequestDialog } from "./GitDialogs";
import {
  ContextCompositionCard,
  type ContextCompositionEntry,
} from "./ContextCompositionCard";
import {
  InstructionFilesCard,
  type InstructionFilesEntry,
} from "./InstructionFilesCard";
import { DesignTokensPanel } from "./DesignTokensPanel";
import { useAppDebugState } from "./AppDebugState";
import {
  EmptyStateHints,
  type EmptyStateHintAction,
} from "./EmptyStateHints";
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
import { JumpToLatestPill } from "./JumpToLatestPill";
import { SkillsCatalog } from "./SkillsCatalog";
import { TaskBoardView } from "./TaskBoardView";
import { StreamingMarkdown } from "./StreamingMarkdown";
import {
  RunDebugPanel,
  runDebugPhaseForState,
} from "./RunDebugPanel";
import { ChipGalleryPanel } from "./ChipGalleryPanel";
import { useThreadBrowserPreview } from "./ThreadBrowserPreview";
import { threadDisplayTitle } from "./ThreadTitles";
import {
  isRecord,
  numberValue,
  recordValue,
  stringValue,
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
import type { WorkspaceFileDirtyState } from "./WorkspaceFiles";
import { useWorkspaceToolState } from "./WorkspaceToolState";
import type { WorkspaceViewTab } from "./WorkspaceViewTabs";
import { desktopApiErrorMessage } from "./WorkspaceReviewHelpers";
import { ImagePreviewProvider } from "./ImagePreview";
import { WINDOW_RESIZING_CLASS } from "./WindowResizeState";

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

const VIEW_SWITCH_LOADING_DELAY_MS = 180;
const PROJECT_THREAD_COLLAPSE_MS = 190;
export const SIDEBAR_DRAWER_HOVER_OPEN_DELAY_MS = 240;
const ENVIRONMENT_PANEL_MOTION_MS = 260;
const ENVIRONMENT_PANEL_WIDTH_PX = 328;
const ENVIRONMENT_PANEL_WIDTH_CSS = `${ENVIRONMENT_PANEL_WIDTH_PX}px`;
const CONVERSATION_GRID_COLUMNS = 12;
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
type PendingViewSwitchKind = "thread" | "project" | "runtime";

type PendingViewSwitch = {
  kind: PendingViewSwitchKind;
  targetID: string;
  visible: boolean;
};

function createContextCompositionEntryID(): string {
  return `context-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
}

function createInstructionFilesEntryID(): string {
  return `instructions-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
}

type TurnProgressContent = {
  label: string;
  detail?: string;
};

const PROJECT_COLLAPSED_IDS_KEY = "wuu.desktop.collapsedProjectIDs";
const PROJECT_EXPANDED_IDS_KEY = "wuu.desktop.expandedProjectIDs";
const SIDEBAR_SECTION_ORDER_KEY = "wuu.desktop.sidebarSectionOrder";
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

type HistoryMessageEditState = {
  threadID: string;
  turnID: string;
  itemID: string;
  pane?: ConversationPaneID;
  submitting: boolean;
};

type ParticipantPanelState = {
  mode: "new" | "edit";
  participant?: ParticipantProfile;
  loading?: boolean;
  error?: string;
  saving?: boolean;
  feedbackSubmitting?: boolean;
  // feedbackReply mirrors the memory manager agent's reply_md for the last
  // feedback submission (memory/chat, participant scope).
  feedbackReply?: string;
  retiring?: boolean;
  // archived marks a successful archive; the panel shows the "已归档"
  // receipt until the scheduled close below clears it.
  archived?: boolean;
};

// How long the "已归档" receipt stays visible before the panel closes.
const PARTICIPANT_ARCHIVED_NOTICE_MS = 1_500;

type ParticipantTeamTemplate = {
  version: 1;
  participants: ParticipantSaveParams[];
};

function replaceParticipantProfile(
  participants: ParticipantProfile[],
  participant: ParticipantProfile,
): ParticipantProfile[] {
  const index = participants.findIndex((item) => item.id === participant.id);
  if (index === -1) {
    return [...participants, participant].sort((a, b) =>
      a.name.localeCompare(b.name),
    );
  }
  const next = [...participants];
  next[index] = participant;
  return next;
}

function participantTemplateEntries(value: unknown): ParticipantSaveParams[] {
  if (!value || typeof value !== "object" || !("participants" in value)) {
    throw new Error("template is missing participants");
  }
  const participantsValue = (value as { participants?: unknown }).participants;
  if (!Array.isArray(participantsValue)) {
    throw new Error("template participants must be an array");
  }
  return participantsValue.map((entry) => {
    if (!entry || typeof entry !== "object") {
      throw new Error("template participant must be an object");
    }
    const record = entry as Record<string, unknown>;
    const name = typeof record.name === "string" ? record.name.trim() : "";
    if (name === "") {
      throw new Error("template participant name is required");
    }
    return {
      name,
      role: typeof record.role === "string" ? record.role : undefined,
      tagline: typeof record.tagline === "string" ? record.tagline : undefined,
      model: typeof record.model === "string" ? record.model : undefined,
      memory: typeof record.memory === "string" ? record.memory : undefined,
    };
  });
}

type QueuedMessageEditTarget = {
  threadID: string;
  queueID: string;
};

function storedProjectIDSet(key: string): Set<string> {
  try {
    const stored = window.localStorage.getItem(key);
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

function storedSidebarSectionOrder(): string[] | undefined {
  try {
    const stored = window.localStorage.getItem(SIDEBAR_SECTION_ORDER_KEY);
    if (!stored) return undefined;
    const parsed: unknown = JSON.parse(stored);
    if (!Array.isArray(parsed)) return undefined;
    return parsed.filter(
      (id): id is string => typeof id === "string" && id.length > 0,
    );
  } catch {
    return undefined;
  }
}

function initialCollapsedProjectIDs(): Set<string> {
  return storedProjectIDSet(PROJECT_COLLAPSED_IDS_KEY);
}

function initialExpandedProjectIDs(): Set<string> {
  return storedProjectIDSet(PROJECT_EXPANDED_IDS_KEY);
}

function projectExpanded(
  projectID: string,
  activeProjectID: string | undefined,
  expandedProjectIDs: ReadonlySet<string>,
  collapsedProjectIDs: ReadonlySet<string>,
): boolean {
  return (
    expandedProjectIDs.has(projectID) ||
    (projectID === activeProjectID && !collapsedProjectIDs.has(projectID))
  );
}

function removeMissingIDs(
  ids: Set<string>,
  validIDs: ReadonlySet<string>,
): Set<string> {
  const next = new Set<string>();
  for (const id of ids) {
    if (validIDs.has(id)) {
      next.add(id);
    }
  }
  return next.size === ids.size ? ids : next;
}

function threadListsEquivalent(left: Thread[] | undefined, right: Thread[]): boolean {
  if (!left || left.length !== right.length) {
    return false;
  }
  return left.every((thread, index) => {
    const candidate = right[index];
    return (
      candidate?.id === thread.id &&
      candidate.updated_at === thread.updated_at &&
      candidate.status === thread.status &&
      candidate.pinned === thread.pinned &&
      candidate.archived === thread.archived
    );
  });
}

function threadsForDesktopProject(threads: Thread[], project: DesktopProject): Thread[] {
  return sortThreads(
    threads.filter((thread) => threadBelongsToProject(thread, project)),
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

export function App(): JSX.Element {
  const [popOutInit] = useState<PopOutInitResult | null>(() => readPopOutInit());
  const poppedOutMode = Boolean(popOutInit?.kind && popOutInit.context);
  const [state, setState] = useState<AppState>(initialState);
  const [prompt, setPrompt] = useState("");
  // Bumped each time the empty-state hint chip should refocus the hero
  // composer. A counter (not a boolean) so re-clicking the same chip
  // still fires the focus effect on the next render.
  const [heroComposerFocusTick, setHeroComposerFocusTick] = useState(0);
  const [composerImages, setComposerImages] = useState<ComposerImage[]>([]);
  const [composerFiles, setComposerFiles] = useState<ComposerFile[]>([]);
  const [goalSummary, setGoalSummary] = useState<ComposerGoalSummary | null>(
    null
  );
  const [splitComposerDrafts, setSplitComposerDrafts] = useState<
    Record<ConversationPaneID, ComposerDraftState>
  >(initialSplitComposerDrafts);
  // The split reply panel reuses the main conversation's full composer, so it
  // needs its own draft — bound to the shared dock `prompt` it would collide
  // with the dock composer as the user types in the panel.
  const [subthreadComposerDraft, setSubthreadComposerDraft] =
    useState<ComposerDraftState>(emptyComposerDraft);
  const [historyMessageEdit, setHistoryMessageEdit] =
    useState<HistoryMessageEditState | undefined>(undefined);
  const [, setQueuedMessageEditTarget] =
    useState<QueuedMessageEditTarget | undefined>(undefined);
  const [pendingComposerMessagesByThread, setPendingComposerMessagesByThread] =
    useState<PendingComposerMessagesByThread>({});
  const [projectMenuOpen, setProjectMenuOpen] = useState(false);
  const closeProjectMenu = useCallback(() => setProjectMenuOpen(false), []);
  const appShellRef = useRef<HTMLDivElement>(null);
  const settingsShellRef = useRef<HTMLDivElement>(null);
  const [sidebarDrawerPhase, setSidebarDrawerPhase] = useState<
    "closed" | "open" | "closing"
  >("closed");
  const sidebarDrawerOpenTimerRef = useRef<number | undefined>(undefined);
  const sidebarDrawerCloseTimerRef = useRef<number | undefined>(undefined);
  const sidebarHoverZoneRef = useRef<HTMLDivElement>(null);
  const sidebarHoverZoneActiveRef = useRef(false);
  // Set while a sidebar drag is in flight and kept set after a drag that ends
  // collapsed, until the pointer moves off the hover zone. Without it, the
  // hover zone can appear under the pointer after a drag-collapse and
  // pointerenter immediately pops the drawer back open.
  const sidebarDrawerSuppressedRef = useRef(false);
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
  const [collapsedProjectIDs, setCollapsedProjectIDs] = useState<Set<string>>(
    initialCollapsedProjectIDs,
  );
  const [expandedProjectIDs, setExpandedProjectIDs] = useState<Set<string>>(
    initialExpandedProjectIDs,
  );
  const [collapsingProjectIDs, setCollapsingProjectIDs] = useState<Set<string>>(
    () => new Set(),
  );
  const [projectThreadsByProjectID, setProjectThreadsByProjectID] = useState<
    Record<string, Thread[]>
  >({});
  const [cachedScratchThreads, setCachedScratchThreads] = useState<Thread[]>(
    [],
  );
  // Reorderable sidebar section order. Pinned is fixed-position and lives
  // outside this list. Reconciled against the current project list every
  // time `state.projects` changes so newly-added projects append to the end
  // and removed ones drop out.
  const [sidebarSectionOrder, setSidebarSectionOrder] = useState<string[]>(
    () =>
      reconcileSidebarSectionOrder(
        storedSidebarSectionOrder(),
        // The first render has not yet populated state.projects; reconcile
        // will be re-derived on the first effect run below.
        [],
      ),
  );
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
  const [openSubthreadPanel, setOpenSubthreadPanel] = useState<
    | {
        threadID: string;
        subthread?: ConversationSubthread;
        loading: boolean;
        error?: string;
      }
    | undefined
  >(undefined);

  // Reply subthreads (群中群) for the active chat thread, keyed by
  // anchor_item_id, feeding the chat view's reply badges / task 活动卡. Loaded
  // per active chat thread (see effect below); non-active panes never need it.
  const [chatSubthreads, setChatSubthreads] = useState<{
    threadID: string;
    // byAnchor 服务消息行的 reply 徽标;standalone task 没有锚点,由任务
    // 看板 tab 自己拉取列表展示。
    byAnchor: Map<string, ConversationSubthread>;
  } | null>(null);
  // Bump to force a reload of the active thread's subthreads (e.g. right after
  // opening a reply create-or-finds a new subthread, so its badge appears).
  const [chatSubthreadsNonce, setChatSubthreadsNonce] = useState(0);
  // Bump on every thread/subUpdated notification: an open task-board tab
  // reloads on this tick (its thread may not be the active thread, so the
  // chatSubthreadsNonce path alone would miss it).
  const [boardRefreshTick, setBoardRefreshTick] = useState(0);
  const [participants, setParticipants] = useState<ParticipantProfile[]>([]);
  const [participantPanel, setParticipantPanel] = useState<
    ParticipantPanelState | undefined
  >(undefined);
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
  const [pendingFork, setPendingFork] = useState<
    | {
        sourceThread: Thread;
        turnID: string;
        itemID: string;
      }
    | undefined
  >(undefined);
  const [pendingViewSwitch, setPendingViewSwitch] = useState<
    PendingViewSwitch | undefined
  >(undefined);
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
  const projectCollapseTimersRef = useRef(new Map<string, number>());
  const loadingProjectThreadIDsRef = useRef(new Set<string>());
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
  const queuedMessageEditTargetRef =
    useRef<QueuedMessageEditTarget | undefined>(undefined);
  const pendingComposerMessagesByThreadRef =
    useRef<PendingComposerMessagesByThread>({});
  const localDemoThreadsRef = useRef(new Map<string, Thread>());
  const cachedThreadPaneHistoryRef = useRef<string[]>([]);
  const viewSwitchRequestRef = useRef(0);
  const viewSwitchDelayTimerRef = useRef<number | undefined>(undefined);
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
  const activeThreadIsGroup = Boolean(activeThread && isGroupThread(activeThread));
  // Chat-style threads (DM + group) follow chat send semantics (issue #10):
  // the composer never surfaces the worker-thread queue strip or the stop
  // button; a send always reads as "message sent" in the transcript.
  const activeThreadIsChatStyle = Boolean(
    activeThread && (isDMThread(activeThread) || isGroupThread(activeThread)),
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
  const activePendingComposerMessages = pendingComposerMessagesForThread(
    pendingComposerMessagesByThread,
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
    const byThread = pendingComposerMessagesByThreadRef.current;
    let next = byThread;
    for (const threadID of Object.keys(byThread)) {
      const thread = threadForTab(appStateRef.current, threadID);
      if (thread) {
        next = reconcilePendingComposerMessagesForThread(next, thread);
      }
    }
    if (next !== byThread) {
      setPendingComposerMessagesByThreadNow(next);
    }
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
      setEnvironmentDialog(null);
      setPendingFork(undefined);
    },
    onSelectThread: (threadID) => void activateThread(threadID),
  });

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
  const refreshParticipants = useStableCallback(async (): Promise<
    ParticipantProfile[]
  > => {
    if (!appStateRef.current.initialized) {
      setParticipants([]);
      return [];
    }
    const result = await window.wuu.listParticipants();
    const nextParticipants = result.participants ?? [];
    setParticipants(nextParticipants);
    setParticipantPanel((current) => {
      if (!current?.participant?.id) {
        return current;
      }
      const fresh = nextParticipants.find(
        (participant) => participant.id === current.participant?.id,
      );
      return fresh ? { ...current, participant: fresh } : current;
    });
    return nextParticipants;
  });
  const activePlanTotal = activePlanPillUpdate?.plan.length ?? 0;
  const activePlanCompleted =
    activePlanPillUpdate?.plan.filter((item) => item.status === "completed").length ?? 0;
  const activePlanVisible = Boolean(activePlanPillUpdate && activePlanTotal > 0);
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
        // Patch the open panel in place (streaming growth) when the update is for
        // the subthread it is showing; a no-op otherwise.
        setOpenSubthreadPanel((prev) =>
          applySubthreadUpdatedNotification(prev, note),
        );
        // Refresh the main-stream reply-count badge for the active thread by
        // bumping the existing subthreads reload nonce.
        if (
          note?.thread_id &&
          activeThreadIDForState(appStateRef.current) === note.thread_id
        ) {
          setChatSubthreadsNonce((nonce) => nonce + 1);
        }
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
  }, [popOutInit, refreshParticipants]);

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

  // Reset the reused reply composer's draft whenever the open subthread
  // changes (or the panel closes) so an unsent draft never bleeds from one cth
  // into another. On send the draft is cleared inline; this handles switching.
  const openSubthreadID = openSubthreadPanel?.subthread?.id;
  useEffect(() => {
    setSubthreadComposerDraft(emptyComposerDraft());
  }, [openSubthreadID]);

  useEffect(() => {
    window.localStorage.setItem(
      PROJECT_COLLAPSED_IDS_KEY,
      JSON.stringify([...collapsedProjectIDs]),
    );
  }, [collapsedProjectIDs]);

  useEffect(() => {
    window.localStorage.setItem(
      PROJECT_EXPANDED_IDS_KEY,
      JSON.stringify([...expandedProjectIDs]),
    );
  }, [expandedProjectIDs]);

  useEffect(() => {
    window.localStorage.setItem(
      SIDEBAR_SECTION_ORDER_KEY,
      JSON.stringify(sidebarSectionOrder),
    );
  }, [sidebarSectionOrder]);

  useEffect(() => {
    const validProjectIDs = state.projects.map((project) => project.id);
    setSidebarSectionOrder((current) =>
      reconcileSidebarSectionOrder(current, validProjectIDs),
    );
  }, [state.projects]);

  useEffect(() => {
    // Prune collapse/expand state for projects that no longer exist —
    // but never the pseudo-section keys (置顶 / Agents / 群聊 / 对话).
    // They are legitimate members of collapsedProjectIDs and are not
    // project ids, so pruning them here silently re-expanded those
    // sections on every project-state reload (fresh state.projects
    // identity), which users saw as 置顶 "passively expanding".
    const validProjectIDs = new Set(
      state.projects.map((project) => project.id),
    );
    const validSectionIDs = new Set([
      ...validProjectIDs,
      SIDEBAR_SECTION_PINNED,
      SIDEBAR_SECTION_AGENTS,
      SIDEBAR_SECTION_GROUP,
      SCRATCH_PSEUDO_PROJECT_ID,
    ]);
    setCollapsedProjectIDs((current) =>
      removeMissingIDs(current, validSectionIDs),
    );
    setExpandedProjectIDs((current) =>
      removeMissingIDs(current, validSectionIDs),
    );
    setProjectThreadsByProjectID((current) => {
      const next: Record<string, Thread[]> = {};
      let changed = false;
      for (const [projectID, threads] of Object.entries(current)) {
        if (validProjectIDs.has(projectID)) {
          next[projectID] = threads;
        } else {
          changed = true;
        }
      }
      return changed ? next : current;
    });
  }, [state.projects]);

  useEffect(() => {
    if (state.activeContext?.kind !== "project" || !state.activeProjectId) {
      return;
    }
    const projectID = state.activeProjectId;
    const activeProject = state.projects.find(
      (project) => project.id === projectID,
    );
    if (!activeProject) {
      return;
    }
    const activeProjectThreads = threadsForDesktopProject(
      state.threads,
      activeProject,
    );
    setProjectThreadsByProjectID((current) => {
      if (threadListsEquivalent(current[projectID], activeProjectThreads)) {
        return current;
      }
      return { ...current, [projectID]: activeProjectThreads };
    });
    if (!collapsedProjectIDs.has(projectID)) {
      setExpandedProjectIDs((current) =>
        current.has(projectID) ? current : new Set(current).add(projectID),
      );
    }
  }, [
    collapsedProjectIDs,
    state.activeContext?.kind,
    state.activeProjectId,
    state.projects,
    state.threads,
  ]);

  useEffect(() => {
    if (state.activeContext?.kind !== "no_project") {
      return;
    }
    const activeScratchThreads = sortThreads(
      state.threads.filter((thread) => isScratchThread(thread, state.projects)),
    );
    setCachedScratchThreads((current) =>
      threadListsEquivalent(current, activeScratchThreads)
        ? current
        : activeScratchThreads,
    );
  }, [state.activeContext?.kind, state.projects, state.threads]);

  useEffect(() => {
    for (const project of state.projects) {
      if (
        !projectExpanded(
          project.id,
          state.activeProjectId,
          expandedProjectIDs,
          collapsedProjectIDs,
        )
      ) {
        continue;
      }
      if (project.id === state.activeProjectId) {
        continue;
      }
      if (Object.prototype.hasOwnProperty.call(projectThreadsByProjectID, project.id)) {
        continue;
      }
      void loadProjectThreads(project);
    }
  }, [
    collapsedProjectIDs,
    expandedProjectIDs,
    projectThreadsByProjectID,
    state.activeProjectId,
    state.projects,
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
      if (typeof seq !== "number" || seq <= 0) {
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
  // Aggregate participant IDs that are currently busy. The only source is the
  // participant's resident DM thread being in a running state — a resident
  // named agent's DM thread is its brain, so a running turn there means the
  // agent is thinking (docs/plans/2026-07-03-resident-named-agents.md §7.2).
  // Running child agents dispatched inside some thread do NOT light their
  // dispatcher's dot: that coupled the roster dot to whichever thread was
  // selected (ISSUE-12). See computeBusyParticipantIDs for the full rationale.
  // Named participants not in the set render as online. This drives the
  // sidebar roster and chat-style message avatars.
  const busyParticipantIDs = useMemo(
    () => computeBusyParticipantIDs({ threads: state.threads }),
    [state.threads],
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
  // Named members of the group thread that owns the open reply subthread — the
  // candidate pool for the human's "指定 Task lead" pick when 升级为 Task. The
  // lead is the sole named member granted编排权; escalation is human-click only
  // so the human must pick one. When the reply carries a weak-isolation
  // participant subset, scope the pool to those members (the ones actually
  // pushed the reply's traffic), falling back to the full named roster if the
  // intersection is empty.
  const subthreadLeadCandidates = useMemo<ParticipantSummary[]>(() => {
    if (!openSubthreadPanel) {
      return [];
    }
    const parentID = openSubthreadPanel.threadID;
    const parent = [state.thread, state.secondaryThread, ...state.threads].find(
      (thread) => thread?.id === parentID,
    );
    const named = (parent?.members ?? []).filter(
      (member) => member.kind === "named",
    );
    const subset = openSubthreadPanel.subthread?.participants;
    if (subset && subset.length > 0) {
      const inSubset = new Set(subset);
      const scoped = named.filter((member) => inSubset.has(member.id));
      if (scoped.length > 0) {
        return scoped;
      }
    }
    return named;
  }, [openSubthreadPanel, state.thread, state.secondaryThread, state.threads]);
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

  const clearSidebarDrawerCloseTimer = useCallback((): void => {
    if (sidebarDrawerCloseTimerRef.current !== undefined) {
      window.clearTimeout(sidebarDrawerCloseTimerRef.current);
      sidebarDrawerCloseTimerRef.current = undefined;
    }
  }, []);

  const clearSidebarDrawerOpenTimer = useCallback((): void => {
    if (sidebarDrawerOpenTimerRef.current !== undefined) {
      window.clearTimeout(sidebarDrawerOpenTimerRef.current);
      sidebarDrawerOpenTimerRef.current = undefined;
    }
  }, []);

  const cancelSidebarDrawerOpen = useCallback((): void => {
    sidebarHoverZoneActiveRef.current = false;
    clearSidebarDrawerOpenTimer();
  }, [clearSidebarDrawerOpenTimer]);

  const openSidebarDrawer = useCallback((): void => {
    if (resizingSidebar || sidebarDrawerSuppressedRef.current) {
      return;
    }
    clearSidebarDrawerOpenTimer();
    clearSidebarDrawerCloseTimer();
    if (sidebarCollapsed) {
      setSidebarDrawerPhase("open");
    }
  }, [
    clearSidebarDrawerCloseTimer,
    clearSidebarDrawerOpenTimer,
    resizingSidebar,
    sidebarCollapsed,
  ]);

  const scheduleSidebarDrawerOpen = useCallback((): void => {
    sidebarHoverZoneActiveRef.current = true;
    if (
      resizingSidebar ||
      sidebarDrawerSuppressedRef.current ||
      !sidebarCollapsed
    ) {
      return;
    }
    clearSidebarDrawerOpenTimer();
    // Edge hover is easy to hit accidentally; require a short dwell before
    // opening, while the sidebar body itself still keeps the drawer open
    // immediately once the user is inside it.
    sidebarDrawerOpenTimerRef.current = window.setTimeout(() => {
      sidebarDrawerOpenTimerRef.current = undefined;
      if (
        !sidebarHoverZoneActiveRef.current ||
        resizingSidebar ||
        sidebarDrawerSuppressedRef.current
      ) {
        return;
      }
      clearSidebarDrawerCloseTimer();
      setSidebarDrawerPhase("open");
    }, SIDEBAR_DRAWER_HOVER_OPEN_DELAY_MS);
  }, [
    clearSidebarDrawerCloseTimer,
    clearSidebarDrawerOpenTimer,
    resizingSidebar,
    sidebarCollapsed,
  ]);

  const closeSidebarDrawer = useCallback((): void => {
    cancelSidebarDrawerOpen();
    clearSidebarDrawerCloseTimer();
    if (!sidebarCollapsed || resizingSidebar) {
      // No closing animation mid-drag: collapsing unmounts the resizer, the
      // pointer falls through onto sidebar content, and the resulting
      // pointerleave would put the shell in .sidebar-drawer-closing — which
      // pins the sidebar as a full-height overlay while the user is still
      // dragging.
      setSidebarDrawerPhase("closed");
      return;
    }
    setSidebarDrawerPhase("closing");
    sidebarDrawerCloseTimerRef.current = window.setTimeout(() => {
      sidebarDrawerCloseTimerRef.current = undefined;
      setSidebarDrawerPhase("closed");
    }, SIDEBAR_MOTION_MS);
  }, [
    cancelSidebarDrawerOpen,
    clearSidebarDrawerCloseTimer,
    resizingSidebar,
    sidebarCollapsed,
  ]);

  useLayoutEffect(() => {
    if (!sidebarCollapsed && sidebarDrawerPhase !== "closed") {
      cancelSidebarDrawerOpen();
      clearSidebarDrawerCloseTimer();
      setSidebarDrawerPhase("closed");
    }
  }, [
    cancelSidebarDrawerOpen,
    clearSidebarDrawerCloseTimer,
    sidebarCollapsed,
    sidebarDrawerPhase,
  ]);

  useEffect(() => {
    if (!sidebarCollapsed || resizingSidebar) {
      cancelSidebarDrawerOpen();
    }
  }, [cancelSidebarDrawerOpen, resizingSidebar, sidebarCollapsed]);

  useEffect(() => {
    if (!sidebarCollapsed || sidebarDrawerPhase !== "open") {
      return undefined;
    }
    // The drawer normally closes via the sidebar's pointerleave, but Chromium
    // skips that event when the pointer exits the window fast or focus jumps
    // to another app, leaving the drawer stranded open. Close it whenever the
    // pointer leaves the window or the app loses focus.
    function handleWindowMouseOut(event: MouseEvent): void {
      if (event.relatedTarget === null) {
        closeSidebarDrawer();
      }
    }
    window.addEventListener("mouseout", handleWindowMouseOut);
    window.addEventListener("blur", closeSidebarDrawer);
    return () => {
      window.removeEventListener("mouseout", handleWindowMouseOut);
      window.removeEventListener("blur", closeSidebarDrawer);
    };
  }, [closeSidebarDrawer, sidebarCollapsed, sidebarDrawerPhase]);

  useEffect(() => {
    if (!sidebarCollapsed) {
      return undefined;
    }
    function handleWindowMouseOut(event: MouseEvent): void {
      if (event.relatedTarget === null) {
        cancelSidebarDrawerOpen();
      }
    }
    window.addEventListener("mouseout", handleWindowMouseOut);
    window.addEventListener("blur", cancelSidebarDrawerOpen);
    return () => {
      window.removeEventListener("mouseout", handleWindowMouseOut);
      window.removeEventListener("blur", cancelSidebarDrawerOpen);
    };
  }, [cancelSidebarDrawerOpen, sidebarCollapsed]);

  useEffect(() => {
    if (resizingSidebar) {
      sidebarDrawerSuppressedRef.current = true;
      return undefined;
    }
    if (!sidebarDrawerSuppressedRef.current) {
      return undefined;
    }
    if (!sidebarCollapsed) {
      sidebarDrawerSuppressedRef.current = false;
      return undefined;
    }
    // The drag ended collapsed with the pointer likely still on the hover
    // zone. Keep the drawer suppressed until the pointer moves off the zone
    // so the sidebar doesn't reopen in place right after being dragged shut.
    function handlePointerMove(event: PointerEvent): void {
      const zone = sidebarHoverZoneRef.current?.getBoundingClientRect();
      if (!zone || event.clientX > zone.right || event.clientX < zone.left) {
        sidebarDrawerSuppressedRef.current = false;
        window.removeEventListener("pointermove", handlePointerMove);
      }
    }
    window.addEventListener("pointermove", handlePointerMove);
    return () => window.removeEventListener("pointermove", handlePointerMove);
  }, [resizingSidebar, sidebarCollapsed]);

  useEffect(() => {
    if (!resizingSidebar || !sidebarCollapsed) {
      return;
    }
    // Collapsing via drag leaves focus on whatever sidebar control was last
    // clicked; `.sidebar-collapsed .sidebar:focus-within` would then pin the
    // sidebar open as a drawer overlay at the clamped minimum width. Drop
    // that focus so the sidebar actually closes.
    const sidebar = appShellRef.current?.querySelector(".sidebar");
    const active = document.activeElement;
    if (sidebar && active instanceof HTMLElement && sidebar.contains(active)) {
      active.blur();
    }
  }, [resizingSidebar, sidebarCollapsed]);

  const closeSidebarDrawerAfterNavigation = useCallback((): void => {
    if (sidebarCollapsed && sidebarDrawerPhase === "open") {
      closeSidebarDrawer();
      return;
    }
    cancelSidebarDrawerOpen();
  }, [
    cancelSidebarDrawerOpen,
    closeSidebarDrawer,
    sidebarCollapsed,
    sidebarDrawerPhase,
  ]);

  useEffect(
    () => () => {
      clearSidebarDrawerOpenTimer();
      clearSidebarDrawerCloseTimer();
    },
    [clearSidebarDrawerCloseTimer, clearSidebarDrawerOpenTimer],
  );

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
    if (
      openSubthreadPanel &&
      activeThreadID &&
      openSubthreadPanel.threadID !== activeThreadID
    ) {
      setOpenSubthreadPanel(undefined);
    }
  }, [activeThreadID, openSubthreadPanel]);

  // Load the active chat thread's reply subthreads for the chat view's reply
  // badges / task 活动卡. Best-effort and decorative: any failure leaves the
  // last-known map untouched and never disrupts the message stream. Subagent
  // task-card subthreads are also returned here but anchor on synthetic
  // task-card-<id> ids that no chat message matches, so they render nothing.
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

  function clearProjectCollapseTimer(projectID: string): void {
    const timer = projectCollapseTimersRef.current.get(projectID);
    if (timer === undefined) {
      return;
    }
    window.clearTimeout(timer);
    projectCollapseTimersRef.current.delete(projectID);
  }

  async function loadProjectThreads(project: DesktopProject): Promise<void> {
    if (loadingProjectThreadIDsRef.current.has(project.id)) {
      return;
    }
    loadingProjectThreadIDsRef.current.add(project.id);
    try {
      const listed = await window.wuu.listThreads(project.path);
      setProjectThreadsByProjectID((current) => ({
        ...current,
        [project.id]: threadsForDesktopProject(listed.threads, project),
      }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status: desktopApiErrorMessage(error, "加载项目会话失败"),
      }));
    } finally {
      loadingProjectThreadIDsRef.current.delete(project.id);
    }
  }

  function updateCachedProjectThread(thread: Thread): void {
    const projectID = appStateRef.current.projects.find(
      (project) => threadBelongsToProject(thread, project),
    )?.id;
    if (!projectID) {
      return;
    }
    setProjectThreadsByProjectID((current) => {
      const currentThreads = current[projectID];
      if (!currentThreads) {
        return current;
      }
      return {
        ...current,
        [projectID]: upsertThread(currentThreads, thread),
      };
    });
  }

  function updateCachedSidebarThread(thread: Thread): void {
    if (isScratchThread(thread, appStateRef.current.projects)) {
      setCachedScratchThreads((current) => upsertThread(current, thread));
      return;
    }
    updateCachedProjectThread(thread);
  }

  /**
   * Drop a permanently deleted thread from every sidebar cache. Unlike
   * archive (which upserts the updated snapshot and lets the archived
   * filter hide it), delete leaves no server-side thread behind, so the
   * cached copies must be removed outright from both the scratch cache
   * and every project's cached list.
   */
  function removeCachedSidebarThread(threadID: string): void {
    setCachedScratchThreads((current) =>
      current.filter((thread) => thread.id !== threadID),
    );
    setProjectThreadsByProjectID((current) => {
      let changed = false;
      const next: Record<string, Thread[]> = {};
      for (const [projectID, threads] of Object.entries(current)) {
        const filtered = threads.filter((thread) => thread.id !== threadID);
        if (filtered.length !== threads.length) {
          changed = true;
        }
        next[projectID] = filtered;
      }
      return changed ? next : current;
    });
  }

  /**
   * Returns a new thread with the matching child agent patched, or the
   * original reference when no match exists. Used by the subagent
   * pin/archive handlers so they can update `child_agents` in state
   * without a full thread list round-trip; the spread identity in
   * `setState` calls is intentional — `undefined` here would erase
   * existing thread data.
   */
  function patchChildAgentInThread(
    thread: Thread | undefined,
    agentID: string,
    patch: Partial<Agent>,
  ): Thread | undefined {
    if (!thread || !thread.child_agents) {
      return thread;
    }
    const index = thread.child_agents.findIndex((a) => a.id === agentID);
    if (index === -1) {
      return thread;
    }
    return {
      ...thread,
      child_agents: thread.child_agents.map((agent, i) =>
        i === index ? { ...agent, ...patch } : agent,
      ),
    };
  }

  function toggleProjectCollapsed(projectID: string): void {
    // The pinned/Agents pseudo section ids use collapsed-set-only
    // semantics: expanded ⇔ !collapsedProjectIDs.has(id). Skip the active
    // project auto-expand branch and the collapsing-project timer
    // animation entirely so a click is one synchronous state flip with
    // no thread-loading side effect (there are no threads to load for
    // these sections).
    if (
      projectID === SIDEBAR_SECTION_PINNED ||
      projectID === SIDEBAR_SECTION_AGENTS ||
      projectID === SIDEBAR_SECTION_GROUP
    ) {
      setCollapsedProjectIDs((current) => {
        if (!current.has(projectID)) {
          return new Set(current).add(projectID);
        }
        const next = new Set(current);
        next.delete(projectID);
        return next;
      });
      return;
    }
    const expanded =
      projectExpanded(
        projectID,
        appStateRef.current.activeProjectId,
        expandedProjectIDs,
        collapsedProjectIDs,
      ) || collapsingProjectIDs.has(projectID);
    if (!expanded || collapsingProjectIDs.has(projectID)) {
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
      setExpandedProjectIDs((current) =>
        current.has(projectID) ? current : new Set(current).add(projectID),
      );
      const project = appStateRef.current.projects.find(
        (candidate) => candidate.id === projectID,
      );
      if (
        project &&
        !Object.prototype.hasOwnProperty.call(
          projectThreadsByProjectID,
          projectID,
        )
      ) {
        void loadProjectThreads(project);
      }
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
      setExpandedProjectIDs((current) => {
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
    }, PROJECT_THREAD_COLLAPSE_MS);
    projectCollapseTimersRef.current.set(projectID, timer);
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

  // Encode each image attachment in parallel and stream both the
  // optimistic placeholder and its resolved encoded payload back to the
  // caller. The split into two callbacks is what makes the strip show the
  // image the instant the user pastes: the placeholder fires synchronously
  // with the raw File's blob URL, and the encoded callback replaces the
  // placeholder in place once `normalizeImageFileForPrompt` finishes. PDF
  // attachments stay synchronous (no useful preview, fast encode).
  //
  // Callers should *not* await this promise to drive a "load indicator"
  // — they only await it so any error from the encode can still surface in
  // the existing status/error paths. The visible feedback lives in the
  // `onImagePlaceholder` callback landing immediately on the first image.
  async function buildComposerAttachments(
    files: File[],
    onImagePlaceholder: (placeholder: ComposerImage) => void,
    onImageEncoded: (encoded: ComposerImage) => void,
    onFile: (file: ComposerFile) => void,
  ): Promise<void> {
    const imageFiles = files.filter(isComposerImageFile);
    const pdfFiles = files.filter(isPDFFile);
    await Promise.all([
      ...imageFiles.map(async (file) => {
        const placeholder = composerImagePlaceholder(file);
        onImagePlaceholder(placeholder);
        const encoded = await placeholder.encodePromise;
        if (encoded) {
          onImageEncoded(encoded);
        }
      }),
      ...pdfFiles.map(async (file) => {
        const pdf = await composerFileFromFile(file);
        onFile(pdf);
      }),
    ]);
  }

  async function attachComposerAttachmentFiles(files: File[]): Promise<void> {
    if (files.length === 0) {
      return;
    }
    const imageFiles = files.filter(isComposerImageFile);
    const pdfFiles = files.filter(isPDFFile);
    if (imageFiles.length === 0 && pdfFiles.length === 0) {
      setState((current) => ({
        ...current,
        status: "仅支持图片和 PDF",
      }));
      return;
    }
    try {
      await buildComposerAttachments(
        files,
        // Synchronous: drop the placeholder into state so the attachment
        // strip renders the raw file as a preview the moment paste lands,
        // instead of waiting for the image encode + base64 conversion to
        // finish in the background.
        (placeholder) => setComposerImages((current) => [...current, placeholder]),
        // Fires later (per image, in parallel): replace the placeholder in
        // place by id. Same `id` is preserved by composerImagePlaceholder
        // so the swap is invisible to React's keying.
        (encoded) =>
          setComposerImages((current) =>
            current.map((existing) => (existing.id === encoded.id ? encoded : existing)),
          ),
        (file) => setComposerFiles((current) => [...current, file]),
      );
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "附件添加失败",
      }));
    }
  }

  function removeComposerImage(id: string): void {
    setComposerImages((current) => {
      const removed = current.find((image) => image.id === id);
      // Free the blob URL for any optimistic placeholder we're dropping.
      // Encoded entries have no previewSrc and the helper is a no-op for
      // them.
      revokeComposerImagePreview(removed);
      return current.filter((image) => image.id !== id);
    });
  }

  function removeComposerFile(id: string): void {
    setComposerFiles((current) => current.filter((file) => file.id !== id));
  }

  // Attach/remove helpers for the split reply panel's reused composer. They
  // mirror the dock composer's attach flow (optimistic placeholder → encoded
  // swap by id) but target the dedicated subthread draft.
  async function attachSubthreadComposerAttachmentFiles(
    files: File[],
  ): Promise<void> {
    if (files.length === 0) {
      return;
    }
    const imageFiles = files.filter(isComposerImageFile);
    const pdfFiles = files.filter(isPDFFile);
    if (imageFiles.length === 0 && pdfFiles.length === 0) {
      setState((current) => ({ ...current, status: "仅支持图片和 PDF" }));
      return;
    }
    try {
      await buildComposerAttachments(
        files,
        (placeholder) =>
          setSubthreadComposerDraft((draft) => ({
            ...draft,
            images: [...draft.images, placeholder],
          })),
        (encoded) =>
          setSubthreadComposerDraft((draft) => ({
            ...draft,
            images: draft.images.map((existing) =>
              existing.id === encoded.id ? encoded : existing,
            ),
          })),
        (file) =>
          setSubthreadComposerDraft((draft) => ({
            ...draft,
            files: [...draft.files, file],
          })),
      );
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "附件添加失败",
      }));
    }
  }

  function removeSubthreadComposerImage(id: string): void {
    setSubthreadComposerDraft((draft) => {
      const removed = draft.images.find((image) => image.id === id);
      revokeComposerImagePreview(removed);
      return { ...draft, images: draft.images.filter((image) => image.id !== id) };
    });
  }

  function removeSubthreadComposerFile(id: string): void {
    setSubthreadComposerDraft((draft) => ({
      ...draft,
      files: draft.files.filter((file) => file.id !== id),
    }));
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
    const imageFiles = files.filter(isComposerImageFile);
    const pdfFiles = files.filter(isPDFFile);
    if (imageFiles.length === 0 && pdfFiles.length === 0) {
      setState((current) => ({
        ...current,
        status: "仅支持图片和 PDF",
      }));
      return;
    }
    try {
      await buildComposerAttachments(
        files,
        (placeholder) =>
          updateSplitComposerDraft(pane, (draft) => ({
            ...draft,
            images: [...draft.images, placeholder],
          })),
        // Replace the placeholder by id once the encode resolves so the
        // strip transitions from the raw file preview to the encoded data:
        // URL without disturbing ordering.
        (encoded) =>
          updateSplitComposerDraft(pane, (draft) => ({
            ...draft,
            images: draft.images.map((existing) =>
              existing.id === encoded.id ? encoded : existing,
            ),
          })),
        (file) =>
          updateSplitComposerDraft(pane, (draft) => ({
            ...draft,
            files: [...draft.files, file],
          })),
      );
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
    updateSplitComposerDraft(pane, (draft) => {
      const removed = draft.images.find((image) => image.id === id);
      // Release the blob URL of any optimistic placeholder being dropped.
      revokeComposerImagePreview(removed);
      return {
        ...draft,
        images: draft.images.filter((image) => image.id !== id),
      };
    });
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

  function setQueuedMessageEditTargetNow(
    target: QueuedMessageEditTarget | undefined,
  ): void {
    queuedMessageEditTargetRef.current = target;
    setQueuedMessageEditTarget(target);
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
    scope: PendingComposerMessageRemovalScope = "all",
  ): void {
    setPendingComposerMessagesByThreadNow(
      removePendingComposerMessagesByID(
        pendingComposerMessagesByThreadRef.current,
        threadID,
        id,
        scope,
      ),
    );
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
        removePendingComposerMessageByID(threadID, queueID, "queue");
      }
      return;
    }
    if (event.message.method === "turn/dequeued") {
      const queueID = stringValue(params, "queue_id");
      if (queueID) {
        removePendingComposerMessageByID(threadID, queueID, "queue");
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
    if (queuedMessageEditTargetRef.current?.queueID === id) {
      setQueuedMessageEditTargetNow(undefined);
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
    restorePendingComposerMessage(target.message);
    setQueuedMessageEditTargetNow({
      threadID: target.threadID,
      queueID: target.message.id,
    });
    setState((current) => ({
      ...current,
      status: `正在编辑第 ${target.index + 1} 条排队消息，发送后会保存到原位置`,
    }));
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
    setQueuedMessageEditTargetNow(undefined);
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

  function dismissContextCompositionEntry(id: string): void {
    setContextCompositionEntries((entries) =>
      entries.filter((entry) => entry.id !== id),
    );
  }

  function dismissInstructionFilesEntry(id: string): void {
    setInstructionFilesEntries((entries) =>
      entries.filter((entry) => entry.id !== id),
    );
  }

  function openInstructions(): void {
    if (!activeThread) {
      setState((current) => ({
        ...current,
        status: "没有当前对话",
      }));
      return;
    }
    const threadID = activeThread.id;
    const title = activeThread.preview || activeTitle;
    const entryID = createInstructionFilesEntryID();
    setInstructionFilesEntries((entries) => [
      ...entries,
      {
        id: entryID,
        threadID,
        title,
        loading: true,
      },
    ]);
    scheduleStreamScroll();
    void (async () => {
      try {
        const result = await window.wuu.listInstructionFiles();
        setInstructionFilesEntries((entries) =>
          entries.map((entry) =>
            entry.id === entryID
              ? { ...entry, loading: false, result, error: undefined }
              : entry,
          ),
        );
        scheduleStreamScroll();
      } catch (error) {
        setInstructionFilesEntries((entries) =>
          entries.map((entry) =>
            entry.id === entryID
              ? {
                  ...entry,
                  loading: false,
                  error: desktopApiErrorMessage(error, "无法读取指令文件"),
                }
              : entry,
          ),
        );
      }
    })();
  }

  function openContextComposition(): void {
    if (!activeThread) {
      setState((current) => ({
        ...current,
        status: "没有当前对话",
      }));
      return;
    }
    const threadID = activeThread.id;
    const title = activeThread.preview || activeTitle;
    const entryID = createContextCompositionEntryID();
    const afterTurnID = activeThread.turns.at(-1)?.id;
    setContextCompositionEntries((entries) => [
      ...entries,
      {
        id: entryID,
        threadID,
        afterTurnID,
        title,
        loading: true,
      },
    ]);
    scheduleStreamScroll();
    void (async () => {
      try {
        const result = await window.wuu.getThreadContextComposition(threadID);
        setContextCompositionEntries((entries) =>
          entries.map((entry) =>
            entry.id === entryID
              ? {
                  ...entry,
                  loading: false,
                  result,
                  error: undefined,
                }
              : entry,
          ),
        );
        scheduleStreamScroll();
      } catch (error) {
        setContextCompositionEntries((entries) =>
          entries.map((entry) =>
            entry.id === entryID
              ? {
                  ...entry,
                  loading: false,
                  error: desktopApiErrorMessage(error, "无法读取上下文组成"),
                }
              : entry,
          ),
        );
      }
    })();
  }

  function openParticipantProfile(participant: ParticipantProfile): void {
    closeConversationSearch({ immediate: true });
    closeEnvironmentPanel({ dismissed: true });
    setOpenSubthreadPanel(undefined);
    setRightPanelOpenWithMotion(false);
    setParticipantPanel({
      mode: "edit",
      participant,
      loading: false,
    });
  }

  function openNewParticipantProfile(): void {
    closeConversationSearch({ immediate: true });
    closeEnvironmentPanel({ dismissed: true });
    setOpenSubthreadPanel(undefined);
    setRightPanelOpenWithMotion(false);
    setParticipantPanel({
      mode: "new",
      loading: false,
    });
  }

  /**
   * Open (or create) the DM conversation with the given named participant
   * and surface it as the active thread. The picker prefers the latest live
   * (non-archived) DM thread tagged with `participant.id` so a returning
   * user lands in their previous conversation. When no DM exists yet we
   * start a fresh thread tagged with `dm_participant_id` on the server and
   * mirror the seed-fixture state-merge so the new thread is selected,
   * upserted into `state.threads`, and bound to a session tab.
   */
  async function openParticipantDM(
    participant: ParticipantProfile,
  ): Promise<void> {
    const currentState = appStateRef.current;
    if (!currentState.activeContext || !currentState.initialized) {
      return;
    }
    // Synchronous in-flight guard: a rapid double-click on the same agent
    // row must not produce two concurrent startThread calls. The ref is
    // set before any await so the second invocation short-circuits
    // immediately, and cleared in the finally block below regardless of
    // which branch (existing-DM or freshly-started) resolved.
    if (openingDMParticipantIDRef.current === participant.id) {
      return;
    }
    openingDMParticipantIDRef.current = participant.id;
    try {
      cancelViewSwitch();
      setArchiveConfirmThreadID(undefined);
      setWorkspaceMode(undefined);
      setPrompt("");
      setComposerImages([]);
      setComposerFiles([]);
      const existing = findDMThread(currentState.threads, participant.id);
      if (existing) {
        await activateThread(existing.id);
        return;
      }
      // Defense against issue #3: a stale threads cache can miss a DM
      // thread the server already knows about, but if a session tab for
      // this participant is already open locally, focus it directly
      // instead of asking the server to start (what used to always be) a
      // brand new, indistinguishable thread.
      const existingTab = sessionTabForParticipant(
        currentState.sessionTabs,
        currentState.threads,
        participant.id,
      );
      if (existingTab) {
        await activateThread(existingTab.threadID);
        return;
      }
      try {
        const { thread } = await window.wuu.startThread({
          dm_participant_id: participant.id,
        });
        if (
          !sameRuntimeContext(appStateRef.current.activeContext, currentState.activeContext)
        ) {
          return;
        }
        const activeContext = appStateRef.current.activeContext;
        if (!activeContext) {
          return;
        }
        const targetDraft = sessionTabDraftForThread(appStateRef.current, thread.id);
        setSplitComposerDrafts(initialSplitComposerDrafts());
        setState((current) => {
          const withDraft = persistActiveSessionTabDraft(
            current,
            currentPrimaryComposerDraft(),
          );
          return {
            ...withDraft,
            thread,
            secondaryThread: undefined,
            activePane: "primary",
            allowThreadAutoActivation: true,
            sessionTabs: ensureSessionTab(
              withDraft.sessionTabs,
              createThreadSessionTab(thread, activeContext, targetDraft),
            ),
            activeSessionTabID: threadSessionTabID(thread.id),
            threads: upsertThread(current.threads, thread),
            running: isThreadRunning(thread),
            status: "ready",
          };
        });
      } catch (error) {
        setState((current) => ({
          ...current,
          status: error instanceof Error ? error.message : "无法创建 Agent 对话",
        }));
      }
    } finally {
      openingDMParticipantIDRef.current = undefined;
    }
  }

  /**
   * Create a chat-style group thread (chat-style-threads-design.md §3)
   * from the sidebar's 群聊 inline creator and select it. Mirrors the
   * fresh-thread branch of openParticipantDM: the created thread is
   * upserted into state, bound to a session tab, and made primary.
   */
  async function createGroupThread(title: string): Promise<void> {
    const currentState = appStateRef.current;
    if (!currentState.activeContext || !currentState.initialized) {
      return;
    }
    cancelViewSwitch();
    setArchiveConfirmThreadID(undefined);
    setWorkspaceMode(undefined);
    setPrompt("");
    setComposerImages([]);
    setComposerFiles([]);
    try {
      const { thread } = await window.wuu.startThread({ group: true, title });
      if (
        !sameRuntimeContext(
          appStateRef.current.activeContext,
          currentState.activeContext,
        )
      ) {
        return;
      }
      const activeContext = appStateRef.current.activeContext;
      if (!activeContext) {
        return;
      }
      const targetDraft = sessionTabDraftForThread(appStateRef.current, thread.id);
      setSplitComposerDrafts(initialSplitComposerDrafts());
      setState((current) => {
        const withDraft = persistActiveSessionTabDraft(
          current,
          currentPrimaryComposerDraft(),
        );
        return {
          ...withDraft,
          thread,
          secondaryThread: undefined,
          activePane: "primary",
          allowThreadAutoActivation: true,
          sessionTabs: ensureSessionTab(
            withDraft.sessionTabs,
            createThreadSessionTab(thread, activeContext, targetDraft),
          ),
          activeSessionTabID: threadSessionTabID(thread.id),
          threads: upsertThread(current.threads, thread),
          running: isThreadRunning(thread),
          status: "ready",
        };
      });
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "无法创建群聊",
      }));
    }
  }

  function handleParticipantSave(params: ParticipantSaveParams): void {
    setParticipantPanel((current) =>
      current
        ? {
            ...current,
            saving: true,
            error: undefined,
          }
        : current,
    );
    void (async () => {
      try {
        const result = await window.wuu.saveParticipant(params);
        setParticipants((current) =>
          replaceParticipantProfile(current, result.participant),
        );
        setParticipantPanel({
          mode: "edit",
          participant: result.participant,
          loading: false,
        });
      } catch (error) {
        setParticipantPanel((current) =>
          current
            ? {
                ...current,
                saving: false,
                error: desktopApiErrorMessage(error, "无法保存 Agent"),
              }
            : current,
        );
      }
    })();
  }

  // 反馈直接写进该同事的身份笔记本：memory/chat（participant scope），
  // 由管理 agent 落盘并返回一句话回执（memory-redesign.md §8.2）。
  function handleParticipantFeedback(text: string): void {
    const participant = participantPanel?.participant;
    if (!participant) {
      return;
    }
    setParticipantPanel((current) =>
      current
        ? {
            ...current,
            feedbackSubmitting: true,
            feedbackReply: undefined,
            error: undefined,
          }
        : current,
    );
    void (async () => {
      try {
        const result = await window.wuu.sendMemoryChat({
          scope: "participant",
          participant_id: participant.id,
          message: `用户反馈：${text}`,
        });
        setParticipantPanel((current) =>
          current
            ? {
                ...current,
                feedbackSubmitting: false,
                feedbackReply: result.reply_md,
              }
            : current,
        );
      } catch (error) {
        setParticipantPanel((current) =>
          current
            ? {
                ...current,
                feedbackSubmitting: false,
                error: desktopApiErrorMessage(error, "无法写入反馈"),
              }
            : current,
        );
      }
    })();
  }

  function handleParticipantRetire(participantID: string): void {
    setParticipantPanel((current) =>
      current
        ? {
            ...current,
            retiring: true,
            error: undefined,
          }
        : current,
    );
    void (async () => {
      try {
        await window.wuu.retireParticipant(participantID);
        setParticipants((current) =>
          current.filter((entry) => entry.id !== participantID),
        );
        // Keep the panel open briefly with the "已归档" receipt, then close.
        setParticipantPanel((current) =>
          current ? { ...current, retiring: false, archived: true } : current,
        );
        void refreshParticipants();
        window.setTimeout(() => {
          setParticipantPanel((current) =>
            current?.archived ? undefined : current,
          );
        }, PARTICIPANT_ARCHIVED_NOTICE_MS);
      } catch (error) {
        setParticipantPanel((current) =>
          current
            ? {
                ...current,
                retiring: false,
                error: desktopApiErrorMessage(error, "无法归档 Agent"),
              }
            : current,
        );
      }
    })();
  }

  function exportParticipantTemplate(): void {
    if (participants.length === 0) {
      return;
    }
    const template: ParticipantTeamTemplate = {
      version: 1,
      participants: participants.map((participant) => ({
        name: participant.name,
        role: participant.role,
        tagline: participant.tagline,
        model: participant.model,
        memory: participant.memory,
      })),
    };
    const blob = new Blob([JSON.stringify(template, null, 2)], {
      type: "application/json",
    });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = "wuu-team-template.json";
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
  }

  function importParticipantTemplate(file: File): void {
    void (async () => {
      try {
        const parsed = JSON.parse(await file.text()) as unknown;
        const entries = participantTemplateEntries(parsed);
        const existingByName = new Map(
          participants.map((participant) => [
            participant.name.trim().toLowerCase(),
            participant,
          ]),
        );
        const saved: ParticipantProfile[] = [];
        for (const entry of entries) {
          const existing = existingByName.get(entry.name.trim().toLowerCase());
          const result = await window.wuu.saveParticipant({
            ...entry,
            id: existing?.id,
          });
          saved.push(result.participant);
          existingByName.set(
            result.participant.name.trim().toLowerCase(),
            result.participant,
          );
        }
        setParticipants((current) =>
          saved.reduce(replaceParticipantProfile, current),
        );
        setState((current) => ({
          ...current,
          status: `已导入 ${saved.length} 个 Agent`,
        }));
      } catch (error) {
        setState((current) => ({
          ...current,
          status:
            error instanceof Error ? error.message : "导入团队模板失败",
        }));
      }
    })();
  }

  // Open (or focus) the group's task-board tab. Deterministic tab id makes
  // the second click a focus, not a duplicate (ensureSessionTab dedupe).
  function openTaskBoardTab(thread: Thread): void {
    const context = appStateRef.current.activeContext;
    if (!context) {
      return;
    }
    const tab = createBoardSessionTab(thread, context);
    setState((current) => ({
      ...current,
      sessionTabs: ensureSessionTab(current.sessionTabs, tab),
      activeSessionTabID: tab.id,
    }));
  }

  // A board row click: land back in the group's chat tab with that task's
  // thread panel open. Order matters — the panel auto-closes when its
  // threadID differs from the active thread, so switch tabs first.
  async function openTaskFromBoard(
    threadID: string,
    subthreadID: string,
  ): Promise<void> {
    const tabs = appStateRef.current.sessionTabs;
    if (tabs.some((tab) => tab.id === threadSessionTabID(threadID))) {
      await selectSessionTab(threadSessionTabID(threadID));
    } else {
      await selectThread(threadID);
    }
    openConversationSubthreadByID(threadID, subthreadID);
  }

  // Open the reply panel directly by cth id (no anchor item / create-or-find):
  // used by the pop-out subthread window's boot, which already knows the exact
  // cth to render over its resumed parent thread.
  function openConversationSubthreadByID(
    threadID: string,
    subthreadID: string,
  ): void {
    setOpenSubthreadPanel({ threadID, subthread: undefined, loading: true });
    void (async () => {
      try {
        const result = await window.wuu.openConversationSubthread(threadID, {
          subthreadId: subthreadID,
        });
        setOpenSubthreadPanel({
          threadID,
          subthread: result.subthread,
          loading: false,
        });
      } catch (error) {
        setOpenSubthreadPanel({
          threadID,
          loading: false,
          error: desktopApiErrorMessage(error, "无法打开 thread"),
        });
      }
    })();
  }

  function openConversationSubthread(thread: Thread, item: ThreadItem): void {
    const subthreadID = item.task?.subthread_id;
    setEnvironmentPanelOpen(false);
    setEnvironmentPanelDismissed(true);
    setParticipantPanel(undefined);
    setOpenSubthreadPanel({
      threadID: thread.id,
      subthread: undefined,
      loading: true,
    });
    void (async () => {
      try {
        const result = await window.wuu.openConversationSubthread(thread.id, {
          subthreadId: subthreadID,
          anchorItemId: subthreadID ? undefined : item.id,
          title: item.task?.name,
          createdBy: item.participant?.id,
        });
        setOpenSubthreadPanel({
          threadID: thread.id,
          subthread: result.subthread,
          loading: false,
        });
        // A create-or-find may have just materialized this reply subthread;
        // refresh the badge map so its "N 条回复" affordance appears.
        setChatSubthreadsNonce((nonce) => nonce + 1);
      } catch (error) {
        setOpenSubthreadPanel({
          threadID: thread.id,
          loading: false,
          error: desktopApiErrorMessage(error, "无法打开 thread"),
        });
      }
    })();
  }

  function resolveOpenConversationSubthread(resolved: boolean): void {
    const current = openSubthreadPanel;
    if (!current?.subthread) {
      return;
    }
    const threadID = current.threadID;
    const subthreadID = current.subthread.id;
    setOpenSubthreadPanel({ ...current, loading: true });
    void (async () => {
      try {
        const result = await window.wuu.resolveConversationSubthread(
          threadID,
          subthreadID,
          resolved,
        );
        setOpenSubthreadPanel({
          threadID,
          subthread: result.subthread,
          loading: false,
        });
        setChatSubthreadsNonce((nonce) => nonce + 1);
      } catch (error) {
        setOpenSubthreadPanel({
          ...current,
          loading: false,
          error: desktopApiErrorMessage(error, "无法更新 thread"),
        });
      }
    })();
  }

  // Post a human message into the open reply subthread's cth from the reused
  // full composer in the split panel. The message folds into the cth
  // server-side (thread_id=cth participant_message, weak-isolation subset
  // routing) and carries the composer's 附件/截图 the same way a main-stream
  // send does; the RPC returns the refreshed subthread view so the just-sent
  // message shows immediately — cth messages carry no item/thread notification
  // of their own.
  function sendOpenConversationSubthreadMessage(): void {
    const current = openSubthreadPanel;
    if (!current?.subthread) {
      return;
    }
    const draft = subthreadComposerDraft;
    const trimmed = draft.prompt.trim();
    const files = inputFilesFromComposer(draft.files);
    if (!trimmed && draft.images.length === 0 && files.length === 0) {
      return;
    }
    const threadID = current.threadID;
    const subthreadID = current.subthread.id;
    // Clear the draft optimistically so the composer empties on send, mirroring
    // the dock composer.
    setSubthreadComposerDraft(emptyComposerDraft());
    void (async () => {
      try {
        const encodedImages = await awaitComposerImages(draft.images);
        const images = inputImagesFromComposer(encodedImages);
        const result = await window.wuu.postSubthreadMessage(
          threadID,
          subthreadID,
          trimmed,
          images,
          files,
        );
        setOpenSubthreadPanel((prev) =>
          prev &&
          prev.threadID === threadID &&
          prev.subthread?.id === subthreadID
            ? { ...prev, subthread: result.subthread, error: undefined }
            : prev,
        );
        setChatSubthreadsNonce((nonce) => nonce + 1);
      } catch (error) {
        // Restore the draft so the user does not lose their unsent text/attachments.
        setSubthreadComposerDraft((existing) =>
          existing.prompt.trim() === "" &&
          existing.images.length === 0 &&
          existing.files.length === 0
            ? draft
            : existing,
        );
        setOpenSubthreadPanel((prev) =>
          prev && prev.threadID === threadID
            ? { ...prev, error: desktopApiErrorMessage(error, "无法发送回复") }
            : prev,
        );
      }
    })();
  }

  // Promote the open reply to a task (人点击 gate). Escalation is a client RPC
  // only — agents can propose but never self-escalate. The refreshed view then
  // renders the task_card in both the panel header and the main-stream badge.
  function escalateOpenConversationSubthread(leadParticipantId: string): void {
    const current = openSubthreadPanel;
    if (!current?.subthread) {
      return;
    }
    const threadID = current.threadID;
    const subthreadID = current.subthread.id;
    const title = current.subthread.title;
    setOpenSubthreadPanel({ ...current, loading: true });
    void (async () => {
      try {
        const result = await window.wuu.escalateConversationSubthread(
          threadID,
          subthreadID,
          { title, leadParticipantId: leadParticipantId || undefined },
        );
        setOpenSubthreadPanel({
          threadID,
          subthread: result.subthread,
          loading: false,
        });
        setChatSubthreadsNonce((nonce) => nonce + 1);
      } catch (error) {
        setOpenSubthreadPanel({
          ...current,
          loading: false,
          error: desktopApiErrorMessage(error, "无法升级为 Task"),
        });
      }
    })();
  }

  // 完成 Task(人点击 gate):把一句结论冒泡回主流一条 participant_message(全员
  // 可见),该 cth 变 resolved,其 task_card 转 completed 并带上这句摘要。冒泡是
  // client RPC —— agent 只能提议,收尾必须人点。刷新后主流的 task 活动卡切成
  // result 摘要卡,故一并 bump 徽标数据源。
  function bubbleOpenConversationSubthread(summary: string): void {
    const current = openSubthreadPanel;
    if (!current?.subthread) {
      return;
    }
    const trimmed = summary.trim();
    if (!trimmed) {
      return;
    }
    const threadID = current.threadID;
    const subthreadID = current.subthread.id;
    setOpenSubthreadPanel({ ...current, loading: true });
    void (async () => {
      try {
        const result = await window.wuu.bubbleConversationSubthread(
          threadID,
          subthreadID,
          trimmed,
        );
        setOpenSubthreadPanel({
          threadID,
          subthread: result.subthread,
          loading: false,
        });
        setChatSubthreadsNonce((nonce) => nonce + 1);
      } catch (error) {
        setOpenSubthreadPanel({
          ...current,
          loading: false,
          error: desktopApiErrorMessage(error, "无法完成 Task"),
        });
      }
    })();
  }

  // React to a message inside the reply panel (贴 emoji, right-click). cth
  // messages carry their seq in the parent group thread's history, so the
  // reaction is addressed against the parent thread id.
  function reactToOpenConversationSubthreadMessage(
    item: ThreadItem,
    reaction: string,
  ): void {
    const current = openSubthreadPanel;
    if (!current) {
      return;
    }
    const seq = item.seq;
    if (typeof seq !== "number" || seq <= 0) {
      return;
    }
    void window.wuu
      .reactToMessage(current.threadID, seq, reaction)
      .catch((error) => {
        console.error("react to subthread message failed", error);
      });
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

  // The split reply panel reuses the SAME Composer as the main dock — one
// composer implementation, one experience — but the runtime-control props
// (permission-mode picker / project / branch / codex runtime model & effort
// / settings / memory / skills / instructions launchers) are intentionally
// wired to inert placeholders so none of those menus can open or change
// state mid-thread. `hideRuntimeControls` covers the model/effort chrome;
// the inert placeholders cover everything else, since Composer's prop type
// requires them regardless. It is bound to a dedicated cth draft (so it
// does not cross-write the dock composer). Send routes through
// message/postSubthread (thread_id=cth 折叠短路).
  function renderSubthreadComposer(): JSX.Element {
    const resolved = openSubthreadPanel?.subthread?.status === "resolved";
    // Stripped variant: passes the SAME Composer the main dock uses but with
    // `hideRuntimeControls` so the runtime/menu chrome (permission-mode
    // picker, project picker, branch picker, codex runtime model/effort
    // menus, settings/memory/skills launchers) is not rendered. The prop
    // bag still has to satisfy Composer's type — its runtime-control props
    // are typed required regardless of `hideRuntimeControls`, so we pass
    // inert placeholders that the host already uses no-op behavior for.
    // Cleaner Composer typing (making these optional when hidden) is a
    // separate refactor.
    return (
      <Composer
        variant="dock"
        hideRuntimeControls
        prompt={subthreadComposerDraft.prompt}
        setPrompt={(value) =>
          setSubthreadComposerDraft((draft) => ({ ...draft, prompt: value }))
        }
        files={subthreadComposerDraft.files}
        images={subthreadComposerDraft.images}
        queuedMessages={[]}
        guideMessages={[]}
        running={false}
        runtimeControlsDisabled
        tokensPerSecond={0}
        status=""
        readOnly={Boolean(resolved)}
        initialized={composerInitialized}
        projects={state.projects}
        activeContext={state.activeContext}
        activeProject={activeProject}
        codexModels={codexModels}
        codexRuntimeMenu={null}
        codexRuntimeRef={codexRuntimeRef}
        menuOpen={false}
        accessMenuOpen={false}
        branchMenuOpen={false}
        menuRef={runtimeMenuRef}
        accessMenuRef={subthreadAccessMenuRef}
        projectFilter=""
        setProjectFilter={() => {}}
        onToggleMenu={() => {}}
        onToggleAccessMenu={() => {}}
        onToggleBranchMenu={() => {}}
        onToggleCodexRuntimeMenu={() => {}}
        onSelectRuntimeModel={() => {}}
        onSelectRuntimeEffort={() => {}}
        onSelectPermissionMode={() => {}}
        onOpenSettings={() => {}}
        onOpenMemorySettings={() => {}}
        onOpenSkillsCatalog={() => {}}
        onSelectProject={() => {}}
        onSelectNoProject={() => {}}
        onSelectGitBranch={() => {}}
        onCreateProject={() => {}}
        onOpenProject={() => {}}
        onStartNewThread={() => {}}
        onOpenWorkspaceTool={() => {}}
        onOpenInstructions={() => {}}
        onPasteAttachmentFiles={(files) =>
          void attachSubthreadComposerAttachmentFiles(files)
        }
        onRemoveFile={removeSubthreadComposerFile}
        onRemoveImage={removeSubthreadComposerImage}
        onRemoveQueuedMessage={() => {}}
        onRemoveGuideMessage={() => {}}
        onGuideQueuedMessage={() => {}}
        onEditQueuedMessage={() => {}}
        onEditGuideMessage={() => {}}
        onSend={() => sendOpenConversationSubthreadMessage()}
        onInterrupt={() => {}}
        queryHistorySessionID={openSubthreadPanel?.subthread?.id}
        queryHistory={[]}
        participants={participants}
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
        streamStatus={turnStreamStatusForThread(state, thread)}
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
        onOpenFile={openWorkspaceFile}
        onOpenAgent={(agentID) => {
          const agent = thread.child_agents?.find(
            (candidate) => candidate.id === agentID,
          );
          if (agent) {
            void selectChildAgent(agent);
          }
        }}
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
        onOpenFileDiff={(selection) =>
          openTurnFileDiffPanel(thread.id, selection)
        }
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
        onPopOut={(tabID) => void popOutSessionTab(tabID)}
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

  async function popOutSessionTab(tabID: string): Promise<void> {
    const currentState = appStateRef.current;
    const tab = currentState.sessionTabs.find((item) => item.id === tabID);
    if (!tab || (tab.kind !== "thread" && tab.kind !== "draft")) {
      return;
    }
    if (poppingOutTabIDsRef.current.has(tabID)) {
      return;
    }
    poppingOutTabIDsRef.current.add(tabID);
    try {
      await window.wuu.popOutSession(
        tab.kind === "thread"
          ? {
              kind: "thread",
              threadID: tab.threadID,
              context: tab.context,
            }
          : {
              kind: "draft",
              context: tab.context,
            },
      );
      await closeSessionTab(tabID);
    } catch (error) {
      setState((current) => ({
        ...current,
        status:
          error instanceof Error ? error.message : "open detached window failed",
      }));
    } finally {
      poppingOutTabIDsRef.current.delete(tabID);
    }
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

  async function loadPopOutRuntime(
    init: PopOutInitResult,
  ): Promise<Partial<AppState>> {
    if (!init.kind || !init.context) {
      return { status: "no-runtime" };
    }
    if (init.kind === "draft") {
      const [listedProjects, initialized, listed] = await Promise.all([
        window.wuu.listProjects(),
        window.wuu.initialize(),
        window.wuu.listThreads(),
      ]);
      const listedThreads = sortThreads(listed.threads);
      const tab = createDraftSessionTab("draft:pop-out", init.context);
      return {
        initialized,
        projects: listedProjects.projects,
        activeContext: init.context,
        activeProjectId: activeProjectID(init.context),
        gitStatus: undefined,
        thread: undefined,
        secondaryThread: undefined,
        activePane: "primary",
        allowThreadAutoActivation: false,
        sessionTabs: [tab],
        activeSessionTabID: tab.id,
        threads: listedThreads,
        running: false,
        status: "ready",
      };
    }
    if (!init.threadID) {
      return { status: "no-runtime" };
    }
    const [listedProjects, initialized, listed, resumed] = await Promise.all([
      window.wuu.listProjects(),
      window.wuu.initialize(),
      window.wuu.listThreads(),
      window.wuu.resumeThread(init.threadID),
    ]);
    const listedThreads = sortThreads(listed.threads);
    const thread = reconcileResumedThreadTurns(
      requireThread(resumed, "resume did not return a thread"),
      listedThreads.find((item) => item.id === init.threadID),
    );
    const tab = createThreadSessionTab(thread, init.context);
    return {
      initialized,
      projects: listedProjects.projects,
      activeContext: init.context,
      activeProjectId: activeProjectID(init.context),
      gitStatus: undefined,
      thread,
      secondaryThread: undefined,
      activePane: "primary",
      allowThreadAutoActivation: true,
      sessionTabs: [tab],
      activeSessionTabID: tab.id,
      threads: upsertThread(listedThreads, thread),
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

  function handleEmptyStateHint(action: EmptyStateHintAction): void {
    if (action.kind === "openSettings") {
      closeProjectMenus();
      setSettingsInitialPage("providers");
      setSettingsOpen(true);
    }
  }

  // 打开 设置 → 记忆；带 participantID 时预选该同事的身份笔记本。
  function openMemorySettings(participantID?: string): void {
    closeProjectMenus();
    setSettingsMemoryFocusID(participantID);
    setSettingsInitialPage("memory");
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
    // R2: a draft the user was actively typing into follows them to the new
    // project instead of being stranded in the project they're leaving.
    const carryDraft =
      activeSessionTab(currentState)?.kind === "draft" &&
      composerDraftHasContent(outgoingDraft)
        ? outgoingDraft
        : undefined;
    try {
      const projectState = await window.wuu.selectProject(projectId);
      const loadedState = await loadRuntime(projectState, {
        resumeLatestThread: false,
      });
      if (!finishViewSwitch(requestID)) {
        return;
      }
      restoreLoadedRuntimeComposerDraft(loadedState, carryDraft);
      setState((current) => {
        const next = applyLoadedRuntimeWithDraftCarry(
          current,
          loadedState,
          outgoingDraft,
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

  /**
   * Opt-in second step of project removal: after the workspace leaves the
   * sidebar, offer to reclaim its local state directory (session artifacts,
   * goals, worktrees). Memory is archived into `.archived/` inside the
   * state dir, never hard-deleted (self-consistency invariant 3). A
   * declined dialog or a cleanup failure never undoes the removal itself.
   */
  async function offerRemovedProjectStateCleanup(
    removedProject: DesktopProject | undefined,
  ): Promise<void> {
    if (!removedProject) {
      return;
    }
    if (
      !window.confirm(
        "是否同时清理该项目的本地状态（会话/目标/工件）？记忆将保留归档。",
      )
    ) {
      return;
    }
    try {
      await window.wuu.cleanupProjectState(
        removedProject.id,
        removedProject.path,
      );
    } catch (error) {
      setState((current) => ({
        ...current,
        status:
          error instanceof Error
            ? error.message
            : "cleanup project state failed",
      }));
    }
  }

  async function removeProject(projectId: string): Promise<void> {
    const requestID = beginViewSwitch("runtime", "remove-project");
    const outgoingDraft = currentPrimaryComposerDraft();
    const removedProject = appStateRef.current.projects.find(
      (project) => project.id === projectId,
    );
    try {
      const projectState = await window.wuu.removeProject(projectId);
      if (
        sameRuntimeContext(projectState.active_context, state.activeContext)
      ) {
        // The removed workspace wasn't the active one: just drop it from the
        // sidebar list, leaving the current conversation untouched.
        if (!finishViewSwitch(requestID)) {
          return;
        }
        setState((current) => ({
          ...current,
          projects: projectState.projects,
        }));
        void offerRemovedProjectStateCleanup(removedProject);
        return;
      }
      // The active workspace was removed; ensureRuntimeContext reconciled the
      // context down to the shared 对话 workspace. Load and switch to it.
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
      void offerRemovedProjectStateCleanup(removedProject);
    } catch (error) {
      if (!finishViewSwitch(requestID)) {
        return;
      }
      setState((current) => ({
        ...current,
        status:
          error instanceof Error ? error.message : "remove workspace failed",
      }));
    }
  }

  async function relocateProject(projectId: string): Promise<void> {
    const requestID = beginViewSwitch("runtime", "relocate-project");
    const outgoingDraft = currentPrimaryComposerDraft();
    const previousCwd = appStateRef.current.activeContext?.cwd;
    const wasActive = appStateRef.current.activeProjectId === projectId;
    try {
      const projectState = await window.wuu.relocateProject(projectId);
      const newCwd = projectState.active_context?.cwd;
      // Reload the runtime only when the ACTIVE project's cwd actually moved.
      // A cancelled folder picker or relocating a non-active project just
      // refreshes the list. sameRuntimeContext can't gate this — it compares
      // by project_id, which a relocation deliberately keeps unchanged.
      if (!wasActive || newCwd === previousCwd) {
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
          error instanceof Error
            ? error.message
            : "relocate workspace failed",
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
    // R2: same carry-along as selectProjectForNewThread — a mid-typed draft
    // follows the user into the no-project context instead of being left
    // behind in the project's draft tab.
    const carryDraft =
      activeSessionTab(state)?.kind === "draft" &&
      composerDraftHasContent(outgoingDraft)
        ? outgoingDraft
        : undefined;
    try {
      const projectState = await window.wuu.selectNoProject(fresh);
      const loadedState = await loadRuntime(projectState);
      if (!finishViewSwitch(requestID)) {
        return;
      }
      restoreLoadedRuntimeComposerDraft(loadedState, carryDraft);
      setState((current) =>
        applyLoadedRuntimeWithDraftCarry(current, loadedState, outgoingDraft),
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
    if (visible) {
      closeEnvironmentPanel({ dismissed: true });
      return;
    }
    openEnvironmentPanel();
  }

  function openEnvironmentPanel(): void {
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

  async function selectSessionTab(tabID: string): Promise<void> {
    const currentState = appStateRef.current;
    const tab = currentState.sessionTabs.find((item) => item.id === tabID);
    if (!tab) {
      return;
    }
    if (tabID === currentState.activeSessionTabID) {
      if (
        tab.kind === "thread" &&
        threadNeedsResumeOnReselect(currentState, tab.threadID)
      ) {
        await selectThread(tab.threadID);
      }
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
    // board tab 与 skills 同一形态:非会话视图,仅需就位 runtime context,
    // 不涉及线程恢复与草稿。
    if (tab.kind === "skills" || tab.kind === "board") {
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
    if (fallbackTab.kind === "skills" || fallbackTab.kind === "board") {
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

  async function startNewThread(
    options: { resetToNoProject?: boolean } = {},
  ): Promise<void> {
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
      shouldResetToNoProjectForNewThread(
        state.activeContext,
        Boolean(state.thread || state.secondaryThread),
        options.resetToNoProject ?? false,
      )
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
    if (
      threadId === activeThreadID &&
      !threadNeedsResumeOnReselect(state, threadId)
    ) {
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
        // The resume snapshot can lag the client when the user just sent a
        // message and immediately switched tabs and back: the just-sent turn
        // still lives only in our in-memory copy. Salvage that tail so the
        // wholesale replace below does not drop it (message-loss-on-tab-switch).
        const reconciled = reconcileResumedThreadTurns(
          thread,
          current.threads.find((item) => item.id === thread.id),
        );
        return {
          ...withDraft,
          thread: reconciled,
          secondaryThread: undefined,
          activePane: "primary",
          allowThreadAutoActivation: true,
          sessionTabs: ensureSessionTab(
            withDraft.sessionTabs,
            createThreadSessionTab(reconciled, sourceContext, targetDraft),
          ),
          activeSessionTabID: threadSessionTabID(reconciled.id),
          threads: upsertThread(current.threads, reconciled),
          running: isThreadRunning(reconciled),
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

  // Shared "switch runtime context, then resume this thread" flow. Used
  // whenever opening a thread requires activeContext (and therefore the
  // workspace panel's file tree / terminal / git) to move to a different
  // project or no_project cwd than the one currently active — e.g. opening
  // a 对话 (scratch) thread from inside a project, or opening a 置顶/群聊/DM
  // thread whose own workspace lives elsewhere. The app-server will resume
  // any persisted session regardless of the active workdir, so the context
  // switch has to be driven from here rather than relying on the resume
  // call itself to move it.
  async function switchContextAndResumeThread(
    targetContext: RuntimeContext,
    threadID: string,
  ): Promise<void> {
    const currentState = appStateRef.current;
    setArchiveConfirmThreadID(undefined);
    setWorkspaceMode(undefined);
    const outgoingDraft = currentPrimaryComposerDraft();
    const targetDraft = sessionTabDraftForThread(currentState, threadID);
    const requestID = beginViewSwitch("thread", threadID);
    try {
      const projectState = await selectRuntimeContext(targetContext);
      const loadedState = await loadRuntime(projectState, {
        resumeLatestThread: false,
      });
      const thread = requireThread(
        await window.wuu.resumeThread(threadID),
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
            createThreadSessionTab(thread, targetContext, targetDraft),
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

  // Look up a thread object by id across every cache the sidebar draws
  // from (对话 scratch threads, every project's own list, and the
  // currently loaded state.threads). Sidebar click sites only know a
  // thread's id, not its cwd/workspace_kind, so this is how
  // resolveThreadRuntimeContext gets something to work with.
  function findKnownThread(threadID: string): Thread | undefined {
    return sidebarThreads.find((thread) => thread.id === threadID);
  }

  async function selectProjectThread(
    projectID: string,
    threadID: string,
  ): Promise<void> {
    const currentState = appStateRef.current;
    if (
      projectID === currentState.activeProjectId &&
      currentState.activeContext?.kind === "project"
    ) {
      await selectThread(threadID);
      return;
    }
    if (
      pendingViewSwitch?.kind === "thread" &&
      pendingViewSwitch.targetID === threadID
    ) {
      return;
    }
    if (projectID === SCRATCH_PSEUDO_PROJECT_ID) {
      // The 对话 pseudo-project is synthetic and never appears in
      // state.projects, so it can't be resolved to a {kind:"project"}
      // context the way real projects are below. Resolve the thread's OWN
      // context instead — a scratch thread's cwd almost never matches the
      // currently active context.
      const thread = findKnownThread(threadID);
      if (!thread) {
        return;
      }
      const targetContext = resolveThreadRuntimeContext(
        thread,
        currentState.projects,
      );
      if (sameRuntimeContext(targetContext, currentState.activeContext)) {
        await selectThread(threadID);
        return;
      }
      await switchContextAndResumeThread(targetContext, threadID);
      return;
    }
    const project = currentState.projects.find(
      (candidate) => candidate.id === projectID,
    );
    if (!project) {
      return;
    }
    const targetContext: RuntimeContext = {
      kind: "project",
      project_id: project.id,
      cwd: project.path,
    };
    await switchContextAndResumeThread(targetContext, threadID);
  }

  async function activateThread(threadID: string): Promise<void> {
    const currentState = appStateRef.current;
    const project = currentState.projects.find((candidate) =>
      sidebarProjectThreadsByProjectID[candidate.id]?.some(
        (thread) => thread.id === threadID,
      ),
    );
    if (
      project &&
      (project.id !== currentState.activeProjectId ||
        currentState.activeContext?.kind !== "project")
    ) {
      await selectProjectThread(project.id, threadID);
      return;
    }
    if (!project) {
      // Not in any real project's loaded thread list: a pinned scratch
      // thread opened via 置顶, a DM, a group thread, or anything else
      // opened straight from 群聊/the agent roster. selectThread alone
      // would resume it under the CURRENT activeContext — resolve the
      // thread's own context first so the workspace panel actually
      // follows it there.
      const thread = findKnownThread(threadID);
      const targetContext = thread
        ? resolveThreadRuntimeContext(thread, currentState.projects)
        : undefined;
      if (
        targetContext &&
        !sameRuntimeContext(targetContext, currentState.activeContext)
      ) {
        await switchContextAndResumeThread(targetContext, threadID);
        return;
      }
    }
    await selectThread(threadID);
  }

  async function selectProjectChildAgent(
    projectID: string,
    agent: Agent,
  ): Promise<void> {
    if (
      projectID === appStateRef.current.activeProjectId &&
      appStateRef.current.activeContext?.kind === "project"
    ) {
      await selectChildAgent(agent);
      return;
    }
    await selectProjectThread(projectID, agent.id);
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
        // The resume snapshot can lag the client when the user just sent a
        // message and immediately switched tabs and back: the just-sent turn
        // still lives only in our in-memory copy. Salvage that tail so the
        // wholesale replace below does not drop it (message-loss-on-tab-switch).
        const reconciled = reconcileResumedThreadTurns(
          thread,
          current.threads.find((item) => item.id === thread.id),
        );
        return {
          ...withDraft,
          thread: reconciled,
          secondaryThread: undefined,
          activePane: "primary",
          allowThreadAutoActivation: true,
          sessionTabs: ensureSessionTab(
            withDraft.sessionTabs,
            createThreadSessionTab(reconciled, sourceContext, targetDraft),
          ),
          activeSessionTabID: threadSessionTabID(reconciled.id),
          threads: upsertThread(current.threads, reconciled),
          running: isThreadRunning(reconciled),
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

  // True when the (turnID, itemID) pair points at the most recent user
  // message in `sourceThread`. Used to decide whether the fork button
  // should stay silent (latest message — common "re-try from the last
  // response" path) or whether it should pop the picker dialog (any
  // older user message).
  function isForkTargetLatest(
    sourceThread: Thread,
    turnID: string,
    itemID: string,
  ): boolean {
    const latest = lastUserMessageAnchor(sourceThread);
    return Boolean(
      latest && latest.turnID === turnID && latest.itemID === itemID,
    );
  }

  // Shared body for both the silent latest-message fork and the picker
  // dialog choice. Owns the IPC call, the post-fork state setup, and
  // the user-visible status. Re-throws on failure so the picker dialog
  // keeps itself open when the user picks again; the latest-message
  // caller swallows the throw because there is nowhere to surface it.
  async function executeForkFromMessage(
    sourceThread: Thread,
    turnID: string,
    itemID: string,
    mode: ForkMode,
  ): Promise<void> {
    if (!state.activeContext || sourceThread.read_only) {
      return;
    }
    const activeContext = state.activeContext;
    if (localDemoThreadsRef.current.has(sourceThread.id)) {
      setState((current) => ({ ...current, status: "示例会话不能分叉" }));
      return;
    }
    if (mode === "worktree") {
      let gitStatus = appStateRef.current.gitStatus;
      if (!gitStatus) {
        gitStatus = await window.wuu.gitStatus();
        if (
          !sameRuntimeContext(appStateRef.current.activeContext, activeContext)
        ) {
          return;
        }
        const refreshedStatus = gitStatus;
        setState((current) => ({ ...current, gitStatus: refreshedStatus }));
      }
      if (gitStatus.is_repo === false) {
        setState((current) => ({
          ...current,
          status: WORKTREE_FORK_NON_GIT_REASON,
        }));
        throw new Error(WORKTREE_FORK_NON_GIT_REASON);
      }
    }
    setArchiveConfirmThreadID(undefined);
    setState((current) => ({ ...current, status: "正在分叉会话" }));
    try {
      const fork = requireThread(
        await window.wuu.forkThread(sourceThread.id, turnID, itemID, mode),
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
      const splitDrafts = currentSplitConversation
        ? {
            primary: cloneComposerDraft(
              splitComposerDrafts.primary ?? emptyComposerDraft(),
            ),
            secondary: cloneComposerDraft(
              splitComposerDrafts.secondary ?? emptyComposerDraft(),
            ),
          }
        : undefined;
      const sourceDraft = currentSplitConversation
        ? cloneComposerDraft(splitDrafts?.[sourcePane] ?? emptyComposerDraft())
        : {
            prompt,
            images: composerImages.map((image) => ({ ...image })),
            files: composerFiles.map((file) => ({ ...file })),
          };
      setPrompt("");
      setComposerImages([]);
      setComposerFiles([]);
      setSplitComposerDrafts(initialSplitComposerDrafts());
      setState((current) => {
        return openForkThreadAsPrimary(current, {
          sourceThread,
          forkThread: fork,
          context: activeContext,
          sourceDraft,
          splitDrafts,
        });
      });
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "fork failed",
      }));
      throw error;
    }
  }

  async function choosePendingFork(mode: ForkMode): Promise<void> {
    const target = pendingFork;
    if (!target) {
      return;
    }
    try {
      await executeForkFromMessage(
        target.sourceThread,
        target.turnID,
        target.itemID,
        mode,
      );
      setPendingFork(undefined);
    } catch {
      // Status is already set inside executeForkFromMessage; keep the
      // dialog open so the user can pick the other option or cancel.
    }
  }

  async function forkThreadFromMessage(
    sourceThread: Thread,
    turnID: string,
    itemID: string,
  ): Promise<void> {
    if (!state.activeContext || sourceThread.read_only) {
      return;
    }
    // Forks off the most recent user message stay local and silent — that
    // matches the pre-picker behaviour and avoids an extra click for the
    // common "re-try from the last response" case.
    if (isForkTargetLatest(sourceThread, turnID, itemID)) {
      await executeForkFromMessage(sourceThread, turnID, itemID, "local");
      return;
    }
    closeConversationSearch({ immediate: true });
    setEnvironmentDialog(null);
    scheduleGitStatusRefresh(0);
    setPendingFork({ sourceThread, turnID, itemID });
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

  async function toggleThreadPinned(thread: ThreadSummary): Promise<void> {
    if (!state.activeContext) {
      return;
    }
    setArchiveConfirmThreadID(undefined);
    const localDemoThread = localDemoThreadsRef.current.get(thread.id);
    if (localDemoThread) {
      const nextThread = { ...localDemoThread, pinned: !thread.pinned };
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
      updateCachedSidebarThread(result.thread);
      setState((current) => ({
        ...current,
        thread:
          current.thread?.id === thread.id ? result.thread : current.thread,
        secondaryThread:
          current.secondaryThread?.id === thread.id
            ? result.thread
            : current.secondaryThread,
        // Mirror the server's thread/list visibility rule: DM and group
        // threads are listed for every cwd, so they always live in
        // state.threads and must be updated there too — otherwise the
        // stale copy wins the sidebar merge and the pin appears to do
        // nothing. Context-scoped threads keep the cwd guard so a pin
        // from 置顶 can't inject a foreign-context thread.
        threads:
          current.activeContext?.cwd === result.thread.cwd ||
          isDMThread(result.thread) ||
          isGroupThread(result.thread)
            ? upsertThread(current.threads, result.thread)
            : current.threads,
        status: current.status === "ready" ? "ready" : current.status,
      }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "pin thread failed",
      }));
    }
  }

  async function renameThread(
    thread: ThreadSummary,
    title: string,
  ): Promise<void> {
    const trimmed = title.trim();
    if (!trimmed) {
      return;
    }
    const localDemoThread = localDemoThreadsRef.current.get(thread.id);
    if (localDemoThread) {
      const nextThread = { ...localDemoThread, title: trimmed };
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
      const result = await window.wuu.renameThread(thread.id, trimmed);
      updateCachedSidebarThread(result.thread);
      setState((current) => ({
        ...current,
        thread:
          current.thread?.id === thread.id ? result.thread : current.thread,
        secondaryThread:
          current.secondaryThread?.id === thread.id
            ? result.thread
            : current.secondaryThread,
        threads:
          current.threads.some((item) => item.id === result.thread.id) ||
          current.activeContext?.cwd === result.thread.cwd ||
          isDMThread(result.thread) ||
          isGroupThread(result.thread)
            ? upsertThread(current.threads, result.thread)
            : current.threads,
        status: current.status === "ready" ? "ready" : current.status,
      }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status: desktopApiErrorMessage(error, "重命名对话失败"),
      }));
    }
  }

  async function removeThreadMemberByID(
    threadID: string,
    participantID: string,
  ): Promise<void> {
    try {
      const result = await window.wuu.removeThreadMember(threadID, participantID);
      updateCachedSidebarThread(result.thread);
      setState((current) => ({
        ...current,
        thread: current.thread?.id === threadID ? result.thread : current.thread,
        secondaryThread:
          current.secondaryThread?.id === threadID
            ? result.thread
            : current.secondaryThread,
        threads: upsertThread(current.threads, result.thread),
        status: current.status === "ready" ? "ready" : current.status,
      }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status:
          error instanceof Error ? error.message : "remove thread member failed",
      }));
    }
  }

  async function addThreadMemberByID(
    threadID: string,
    participantID: string,
  ): Promise<void> {
    try {
      const result = await window.wuu.addThreadMember(threadID, participantID);
      updateCachedSidebarThread(result.thread);
      setState((current) => ({
        ...current,
        thread: current.thread?.id === threadID ? result.thread : current.thread,
        secondaryThread:
          current.secondaryThread?.id === threadID
            ? result.thread
            : current.secondaryThread,
        threads: upsertThread(current.threads, result.thread),
        status: current.status === "ready" ? "ready" : current.status,
      }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status:
          error instanceof Error ? error.message : "add thread member failed",
      }));
    }
  }

  async function archiveThread(thread: ThreadSummary): Promise<void> {
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
      updateCachedSidebarThread(result.thread);
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
          // Removing by id is a no-op when the thread isn't in the list,
          // so no cwd guard is needed — and DM/group threads (listed for
          // every cwd, see thread/list) would otherwise survive here as
          // stale copies that keep the archived thread in the sidebar.
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

  /**
   * Permanently delete a conversation via `thread/delete`. The context
   * menu item already asked the user to confirm, so no press-again state
   * is involved here; the server rejects running threads as a backstop.
   * Removal mirrors the archive tab/state cleanup, but the thread is
   * also dropped from the sidebar caches because no server-side snapshot
   * exists anymore.
   */
  async function deleteThread(thread: ThreadSummary): Promise<void> {
    const isLocalDemoThread = localDemoThreadsRef.current.has(thread.id);
    if (
      !state.activeContext ||
      (!isLocalDemoThread && isThreadRunning(thread))
    ) {
      return;
    }
    clearThreadPendingComposerMessages(thread.id);
    const deletedActiveThread = thread.id === activeThreadID;
    const fallbackDraft = deletedActiveThread
      ? nextDraftSessionTab(state.activeContext)
      : undefined;
    if (deletedActiveThread) {
      setPrompt("");
      setComposerImages([]);
      setComposerFiles([]);
      setSplitComposerDrafts(initialSplitComposerDrafts());
    }
    try {
      if (isLocalDemoThread) {
        localDemoThreadsRef.current = new Map(
          [...localDemoThreadsRef.current].filter(
            ([threadID]) => threadID !== thread.id,
          ),
        );
      } else {
        await window.wuu.deleteThread(thread.id);
      }
      removeCachedSidebarThread(thread.id);
      setArchiveConfirmThreadID((current) =>
        current === thread.id ? undefined : current,
      );
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
    } catch (error) {
      setState((current) => ({
        ...current,
        status:
          error instanceof Error ? error.message : "delete thread failed",
      }));
    }
  }

  /**
   * Pin a subagent's own session. Mirrors `toggleThreadPinned` but the
   * API call goes to the underlying session id (the agent id) and the
   * result is patched back into the active thread's `child_agents` list
   * so the info panel row reflects the new state without an extra
   * thread list round-trip.
   */
  async function toggleSubagentPinned(agent: Agent): Promise<void> {
    if (!state.activeContext) {
      return;
    }
    setArchiveConfirmSubagentID(undefined);
    try {
      const result = await window.wuu.pinThread(agent.id, !agent.pinned);
      setState((current) => ({
        ...current,
        thread: patchChildAgentInThread(current.thread, agent.id, {
          pinned: result.thread.pinned,
        }),
        secondaryThread: patchChildAgentInThread(
          current.secondaryThread,
          agent.id,
          { pinned: result.thread.pinned },
        ),
        threads: current.threads.map((thread) =>
          patchChildAgentInThread(thread, agent.id, {
            pinned: result.thread.pinned,
          }) ?? thread,
        ),
        status: current.status === "ready" ? "ready" : current.status,
      }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status:
          error instanceof Error ? error.message : "pin subagent failed",
      }));
    }
  }

  /**
   * Archive a subagent's own session. Re-uses the press-again-to-confirm
   * pattern that the sidebar uses for top-level threads, but never
   * touches the active thread's tab because the subagent is not the
   * primary session. The archived subagent remains visible in the info
   * panel so the user can see the action was applied.
   */
  async function archiveSubagent(agent: Agent): Promise<void> {
    if (archiveConfirmSubagentID !== agent.id) {
      setArchiveConfirmSubagentID(agent.id);
      return;
    }
    setArchiveConfirmSubagentID(undefined);
    try {
      await window.wuu.archiveThread(agent.id, true);
      setState((current) => ({
        ...current,
        thread: patchChildAgentInThread(current.thread, agent.id, {
          archived: true,
        }),
        secondaryThread: patchChildAgentInThread(
          current.secondaryThread,
          agent.id,
          { archived: true },
        ),
        threads: current.threads.map((thread) =>
          patchChildAgentInThread(thread, agent.id, { archived: true }) ??
            thread,
        ),
        status: current.status === "ready" ? "ready" : current.status,
      }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status:
          error instanceof Error ? error.message : "archive subagent failed",
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
            ...(connection.auth_token === undefined
              ? {}
              : { auth_token: connection.auth_token.trim() }),
            ...(connection.type !== undefined && connection.type !== ""
              ? { type: connection.type }
              : {}),
            ...(connection.create_provider ? { create_provider: true } : {}),
          };
    const currentProvider = state.initialized?.providers?.find(
      (item) => item.name === nextProvider,
    );
    const connectionChanged =
      Boolean(nextConnection?.create_provider) ||
      Boolean(nextConnection?.api_key) ||
      Boolean(nextConnection?.auth_token) ||
      (nextConnection?.base_url !== undefined &&
        nextConnection.base_url !== (currentProvider?.base_url ?? ""));
    const currentPermissionMode =
      state.initialized?.permissions?.mode ?? "";
    const permissionModeChanged =
      nextPermissionMode !== undefined &&
      nextPermissionMode !== currentPermissionMode;
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

  async function updateGeneralSettings(
    settings: RuntimeGeneralSettingsUpdate,
  ): Promise<void> {
    if (!state.initialized || viewContextSwitchPending) {
      return;
    }
    try {
      const updated = await window.wuu.updateGeneralSettings(settings);
      setState((current) => {
        const initialized = current.initialized
          ? {
              ...current.initialized,
              general_settings: updated.general_settings,
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
            : "update general settings failed",
      }));
      throw error;
    }
  }

  async function removeProvider(
    provider: string,
    options?: { fallbackProvider?: string; fallbackModel?: string },
  ): Promise<void> {
    if (!state.initialized || viewContextSwitchPending) {
      return;
    }
    const target = provider.trim();
    if (!target) {
      return;
    }
    try {
      const updated = await window.wuu.removeProvider(target, options);
      setState((current) => {
        const initialized = current.initialized
          ? {
              ...current.initialized,
              provider: updated.provider ?? current.initialized.provider,
              model: updated.model ?? current.initialized.model,
              effort: updated.effort ?? current.initialized.effort,
              variant: updated.variant ?? current.initialized.variant,
              permissions:
                updated.permissions ?? current.initialized.permissions,
              extension_trust:
                updated.extension_trust ?? current.initialized.extension_trust,
              providers: updated.providers ?? current.initialized.providers,
              advanced_settings:
                updated.advanced_settings ?? current.initialized.advanced_settings,
            }
          : current.initialized;
        return {
          ...current,
          initialized,
          status: current.status === "ready" ? current.status : "ready",
        };
      });
      if (state.initialized) {
        void loadCodexModelsForProvider(updated.provider);
      }
    } catch (error) {
      setState((current) => ({
        ...current,
        status:
          error instanceof Error
            ? error.message
            : "remove provider failed",
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
    setSelectedPermissionMode(mode);
    setAccessMenuOpen(false);
  }

  async function interrupt(): Promise<void> {
    const thread = activeThreadForState(appStateRef.current);
    if (!thread) {
      return;
    }
    await window.wuu.interruptTurn(thread.id);
    clearThreadPendingComposerMessages(thread.id);
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
    clearThreadPendingComposerMessages(thread.id);
  }

  if (settingsOpen) {
    return (
      <SettingsView
        initialized={state.initialized}
        initialPage={settingsInitialPage}
        memoryFocusParticipantID={settingsMemoryFocusID}
        running={viewContextSwitchPending}
        runningProviderNames={runningProviderNames}
        participants={participants}
        usage={settingsUsage}
        usageRange={usageRange}
        setUsageRange={setUsageRange}
        showDebugControlsSetting={ENABLE_DEBUG_CONTROL_SETTING}
        debugControlsEnabled={debugControlsEnabled}
        sidebarWidth={settingsSidebarWidth}
        sidebarMinWidth={SIDEBAR_MIN_WIDTH}
        sidebarMaxWidth={SIDEBAR_MAX_WIDTH}
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
            collapsedProjectIDs={collapsedProjectIDs}
            expandedProjectIDs={expandedProjectIDs}
            collapsingProjectIDs={collapsingProjectIDs}
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
            onSelectThread={(id) => {
              closeSidebarDrawerAfterNavigation();
              void activateThread(id);
            }}
            onSelectParticipant={(participant) => void openParticipantDM(participant)}
            onEditParticipant={openParticipantProfile}
            onCreateParticipant={openNewParticipantProfile}
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
            onToggleProjectCollapsed={toggleProjectCollapsed}
            onStartNewThreadForProject={(id) => {
              if (id === SCRATCH_PSEUDO_PROJECT_ID) {
                void useNoProject(true);
              } else {
                void startNewThreadForProject(id);
              }
            }}
            onSelectProjectThread={(projectID, threadID) => {
              closeSidebarDrawerAfterNavigation();
              void selectProjectThread(projectID, threadID);
            }}
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
            <ChipGalleryPanel
              open={chipGalleryOpen}
              onClose={() => setChipGalleryOpen(false)}
            />
            {poppedOutMode ? null : (
              <>
                {activeThreadIsChatStyle && activeThread ? (
                  <button
                    className="icon-button"
                    type="button"
                    aria-label="打开任务看板"
                    title="任务看板"
                    onClick={() => openTaskBoardTab(activeThread)}
                  >
                    <ListChecks className="icon-lg" />
                  </button>
                ) : null}
                <button
                  ref={environmentToggleRef}
                  className={`icon-button environment-toggle-button${environmentPanelVisible ? " active" : ""}`}
                  type="button"
                  aria-label={
                    environmentPanelVisible
                      ? activeThreadIsGroup
                        ? "隐藏群聊信息"
                        : "隐藏环境信息"
                      : activeThreadIsGroup
                        ? "显示群聊信息"
                        : "显示环境信息"
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
              </>
            )}
          </div>
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
          onSetActiveMenu={setEnvironmentPanelMenu}
          onClose={() => closeEnvironmentPanel({ dismissed: true })}
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
          subagentSessions={activeThread?.child_agents}
          archiveConfirmSubagentID={archiveConfirmSubagentID}
          onSelectSubagent={(agent) => void selectChildAgent(agent as Agent)}
          onToggleSubagentPinned={(agent) =>
            void toggleSubagentPinned(agent as Agent)
          }
          onArchiveSubagent={(agent) => void archiveSubagent(agent as Agent)}
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
        />

        {openSubthreadPanel ? (
          <ConversationSubthreadPanel
            threadID={openSubthreadPanel.threadID}
            subthread={openSubthreadPanel.subthread}
            loading={openSubthreadPanel.loading}
            error={openSubthreadPanel.error}
            onClose={() => setOpenSubthreadPanel(undefined)}
            onResolve={resolveOpenConversationSubthread}
            onEscalate={escalateOpenConversationSubthread}
            onBubble={bubbleOpenConversationSubthread}
            onReact={reactToOpenConversationSubthreadMessage}
            onPopOut={
              // Already-detached windows don't offer a re-detach; and a pop-out
              // needs the loaded cth id + the runtime context to route by.
              !poppedOutMode &&
              openSubthreadPanel.subthread &&
              state.activeContext
                ? () =>
                    void popOutSubthread(
                      openSubthreadPanel.threadID,
                      openSubthreadPanel.subthread!.id,
                      state.activeContext!,
                    )
                : undefined
            }
            leadCandidates={subthreadLeadCandidates}
            composer={renderSubthreadComposer()}
            resolveParticipantName={resolveParticipantName}
            busyParticipantIDs={busyParticipantIDs}
            readerCount={chatReaderCount}
          />
        ) : null}
        {openSubthreadPanel ? (
          // Same separator family as the sidebar / workspace right panel:
          // drag to resize, arrows/Home/End on focus, double-click resets.
          <div
            className="thread-panel-resizer"
            role="separator"
            aria-label="调整 Thread 面板宽度"
            aria-orientation="vertical"
            aria-valuemin={THREAD_PANEL_MIN_WIDTH}
            aria-valuemax={THREAD_PANEL_MAX_WIDTH}
            aria-valuenow={clampedThreadPanelWidth}
            tabIndex={0}
            onPointerDown={startThreadPanelResize}
            onDoubleClick={resetThreadPanelWidth}
            onKeyDown={handleThreadPanelSeparatorKey}
          />
        ) : null}

        {participantPanel ? (
          <ParticipantProfilePanel
            mode={participantPanel.mode}
            participant={participantPanel.participant}
            providers={state.initialized?.providers}
            loading={participantPanel.loading}
            error={participantPanel.error}
            saving={participantPanel.saving}
            feedbackSubmitting={participantPanel.feedbackSubmitting}
            feedbackReply={participantPanel.feedbackReply}
            retiring={participantPanel.retiring}
            archived={participantPanel.archived}
            onClose={() => setParticipantPanel(undefined)}
            onSave={handleParticipantSave}
            onFeedback={handleParticipantFeedback}
            onOpenMemoryPanel={(participantID) =>
              openMemorySettings(participantID)
            }
            onRetire={handleParticipantRetire}
            forkedFromName={
              participantPanel.participant?.forked_from_id
                ? resolveParticipantName(
                    participantPanel.participant.forked_from_id,
                  )
                : undefined
            }
          />
        ) : null}

        {viewContextSwitchPending ? <ViewSwitchLoading /> : null}

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
                  <div className="conversation-split">
                    {renderConversationSplitPane(state.thread, "primary")}
                    <div
                      className="conversation-split-resizer"
                      role="separator"
                      aria-orientation="vertical"
                      aria-label="调整左右对话宽度"
                      aria-valuemin={CONVERSATION_SPLIT_MIN_PERCENT}
                      aria-valuemax={CONVERSATION_SPLIT_MAX_PERCENT}
                      aria-valuenow={Math.round(splitLeftPercent)}
                      tabIndex={0}
                      onPointerDown={startSplitResize}
                      onDoubleClick={resetSplitPercent}
                      onKeyDown={handleSplitSeparatorKey}
                    />
                    {renderConversationSplitPane(
                      state.secondaryThread,
                      "secondary",
                    )}
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
            <JumpToLatestPill
              containerRef={conversationScrollRef}
              bottomAnchor={dockComposerNode}
            />
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
        !showingSkillsCatalog &&
        !showingTaskBoard
          ? renderComposer("dock")
          : null}

        {state.initialized &&
        !previewingLaunch &&
        !emptyConversation &&
        !showingWorkspaceMode &&
        !splitConversation &&
        !showingSkillsCatalog &&
        !showingTaskBoard &&
        activePlanVisible ? (
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
  contextCompositionEntries: ContextCompositionEntry[];
  instructionFilesEntries: InstructionFilesEntry[];
  historyMessageEdit?: HistoryMessageEditState;
  onStreamFrame: () => void;
  onCollapseComplete: () => void;
  onDismissContextComposition: (id: string) => void;
  onDismissInstructions: (id: string) => void;
  canEditThreadMessage: (thread: Thread) => boolean;
  onForkMessage: (thread: Thread, turnID: string, itemID: string) => void;
  onOpenFile?: (path: string) => void;
  onOpenAgent: (agent: Agent) => void;
  onOpenSubthread: (thread: Thread, item: ThreadItem) => void;
  onReact: (thread: Thread, item: ThreadItem, reaction: string) => void;
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
  onOpenFileDiff: (thread: Thread, selection: TurnFileDiffSelection) => void;
  turnStreamStatus: Record<string, TurnStreamStatus>;
  busyParticipantIDs: ReadonlySet<string>;
  resolveParticipantName: (id: string) => string;
  chatReaderCount: number;
  /**
   * Reply subthreads (群中群) for the active chat thread, keyed by
   * anchor_item_id. Only the pane whose threadID === subthreadsThreadID
   * receives it (inactive panes are display:none, so we avoid loading and
   * threading a map for every cached thread).
   */
  subthreadsByAnchor?: ReadonlyMap<string, ConversationSubthread>;
  subthreadsThreadID?: string;
  /**
   * Per-thread composer messages queued while the agent is mid-turn.
   * Chat-style panes render the thread's `queued` entries as in-transcript
   * "发送中" bubbles (chat send semantics, issue #10); work-thread panes
   * ignore this — their queue renders in the composer strip instead.
   */
  pendingChatMessagesByThread: PendingComposerMessagesByThread;
};

const CachedConversationPanes = memo(function CachedConversationPanes({
  threadIDs,
  threadsByID,
  activeThreadID,
  activeContextCwd,
  conversationGridVisible,
  contextCompositionEntries,
  instructionFilesEntries,
  historyMessageEdit,
  onStreamFrame,
  onCollapseComplete,
  onDismissContextComposition,
  onDismissInstructions,
  canEditThreadMessage,
  onForkMessage,
  onOpenFile,
  onOpenAgent,
  onOpenSubthread,
  onReact,
  onEditMessage,
  onCancelEditMessage,
  onSubmitEditMessage,
  onNoticeAction,
  onOpenFileDiff,
  turnStreamStatus,
  busyParticipantIDs,
  resolveParticipantName,
  chatReaderCount,
  subthreadsByAnchor,
  subthreadsThreadID,
  pendingChatMessagesByThread,
}: CachedConversationPanesProps): JSX.Element {
  return (
    <div className="cached-conversation-panes">
      {threadIDs.map((threadID) => {
        const thread = threadsByID.get(threadID);
        if (!thread) return null;
        const isActive = threadID === activeThreadID;
        const threadTurns = thread.turns ?? [];
        const threadLatestAgentMessageID = latestAgentMessageItemID(threadTurns);
        const threadContextEntries = contextCompositionEntries.filter(
          (entry) => entry.threadID === threadID,
        );
        const turnIDs = new Set(threadTurns.map((turn) => turn.id));
        const entriesBeforeTurns = threadContextEntries.filter(
          (entry) => !entry.afterTurnID,
        );
        const entriesAfterMissingTurn = threadContextEntries.filter(
          (entry) => entry.afterTurnID && !turnIDs.has(entry.afterTurnID),
        );
        const entriesByAfterTurnID = new Map<string, ContextCompositionEntry[]>();
        for (const entry of threadContextEntries) {
          if (!entry.afterTurnID || !turnIDs.has(entry.afterTurnID)) {
            continue;
          }
          const existing = entriesByAfterTurnID.get(entry.afterTurnID) ?? [];
          existing.push(entry);
          entriesByAfterTurnID.set(entry.afterTurnID, existing);
        }
        const renderContextEntry = (entry: ContextCompositionEntry) => (
          <ContextCompositionCard
            entry={entry}
            key={entry.id}
            onDismiss={onDismissContextComposition}
          />
        );
        const threadInstructionCards = instructionFilesEntries
          .filter((entry) => entry.threadID === threadID)
          .map((entry) => (
            <InstructionFilesCard
              entry={entry}
              key={entry.id}
              onDismiss={onDismissInstructions}
            />
          ));
        const forkWorktreeNotice =
          thread.worktree && thread.forked_from_id ? (
            <ForkWorktreeNotice thread={thread} />
          ) : null;
        const isChatStyleThread = isDMThread(thread) || isGroupThread(thread);
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
              {isChatStyleThread ? (
                <ChatThreadViewContainer
                  // Give the chat-style windowing its own reveal state per
                  // thread (ChatThreadView.tsx's hiddenOlderCount) instead
                  // of carrying over whatever the previously active thread
                  // had scrolled open. In practice this pane is already
                  // dedicated to `threadID` for as long as it stays in the
                  // cache, so this mostly documents the intent and guards
                  // the eviction/recreate path explicitly.
                  key={threadID}
                  threadID={threadID}
                  turns={threadTurns}
                  busyParticipantIDs={busyParticipantIDs}
                  resolveParticipantName={resolveParticipantName}
                  readerCount={chatReaderCount}
                  subthreadsByAnchor={
                    threadID === subthreadsThreadID
                      ? subthreadsByAnchor
                      : undefined
                  }
                  onOpenSubthread={(item) => onOpenSubthread(thread, item)}
                  onReact={(item, reaction) => onReact(thread, item, reaction)}
                  // Sends queued while the agent is mid-turn render as
                  // in-transcript "发送中" bubbles instead of the composer
                  // queue strip (chat send semantics, issue #10).
                  pendingMessages={
                    pendingComposerMessagesForThread(
                      pendingChatMessagesByThread,
                      threadID,
                    ).queued
                  }
                />
              ) : (
              <ConversationTurnList
                threadID={thread.id}
                turns={threadTurns}
                renderBeforeTurns={[
                  ...threadInstructionCards,
                  ...entriesBeforeTurns.map(renderContextEntry),
                ]}
                renderAfterMissingTurn={
                  <>
                    {entriesAfterMissingTurn.map(renderContextEntry)}
                    {forkWorktreeNotice}
                  </>
                }
                renderAfterTurn={(turn) =>
                  (entriesByAfterTurnID.get(turn.id) ?? []).map(renderContextEntry)
                }
                forcedFullTurnIDs={
                  historyMessageEdit?.threadID === thread.id
                    ? [historyMessageEdit.turnID]
                    : undefined
                }
                renderTurn={(turn) => (
                  <TurnView
                    turn={turn}
                    cwd={thread.cwd ?? activeContextCwd}
                    onOpenFile={onOpenFile}
                    onOpenAgent={(agentID) => {
                      const agent = thread.child_agents?.find(
                        (candidate) => candidate.id === agentID,
                      );
                      if (agent) {
                        void onOpenAgent(agent);
                      }
                    }}
                    onOpenSubthread={(item) => onOpenSubthread(thread, item)}
                    latestAgentMessageID={threadLatestAgentMessageID}
                    isLatestTurn={
                      thread.turns[thread.turns.length - 1]?.id === turn.id
                    }
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
                    onOpenFileDiff={(selection) =>
                      onOpenFileDiff(thread, selection)
                    }
                    streamStatus={
                      thread.turns[thread.turns.length - 1]?.id === turn.id
                        ? turnStreamStatus[turn.id]
                        : undefined
                    }
                  />
                )}
              />
              )}
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
