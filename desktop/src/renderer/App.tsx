import {
  AlertCircle,
  ArrowLeft,
  Brain,
  Check,
  ChevronDown,
  ChevronRight,
  Clock,
  Globe2,
  FileText,
  Folder,
  FolderX,
  FolderOpen,
  FolderPlus,
  GitBranch,
  Laptop,
  List as ListIcon,
  MessageSquarePlus,
  PanelBottomOpen,
  PanelLeftOpen,
  PanelRightOpen,
  Pencil,
  Plus,
  Search,
  Send,
  Settings,
  ShieldCheck,
  Square,
  Terminal,
  Wrench,
  X
} from "lucide-react";
import { FileTree, useFileTree, useFileTreeSelection } from "@pierre/trees/react";
import {
  type CSSProperties,
  type FormEvent as ReactFormEvent,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
  type RefObject,
  useEffect,
  useMemo,
  useRef,
  useState
} from "react";
import type { PartialOptions } from "overlayscrollbars";
import { OverlayScrollbarsComponent } from "overlayscrollbars-react";
import type {
  AppServerNotification,
  AskUserQuestion,
  CodexModelSummary,
  DesktopProject,
  FileTreeListResult,
  GitStatusResult,
  InitializeResult,
  ProjectListResult,
  RuntimeContext,
  ServerEvent,
  Thread,
  ThreadItem,
  Turn,
  WorkspaceFileReadResult
} from "../shared/protocol";
import { RichContent } from "./RichContent";
import { StreamingMarkdown } from "./StreamingMarkdown";
import { streamTextKey, streamTextStore, type StreamTextField } from "./StreamText";

type AskRequestState = {
  id: string;
  questions: AskUserQuestion[];
};

type CodexModelLoadState = {
  provider?: string;
  loading: boolean;
  error: string;
  models: CodexModelSummary[];
};

type CodexRuntimeMenu = "main" | "model" | null;

type AppState = {
  initialized?: InitializeResult;
  projects: DesktopProject[];
  activeContext?: RuntimeContext;
  activeProjectId?: string;
  gitStatus?: GitStatusResult;
  thread?: Thread;
  threads: Thread[];
  running: boolean;
  status: string;
  askRequest?: AskRequestState;
};

const initialState: AppState = {
  projects: [],
  threads: [],
  running: false,
  status: "connecting"
};

const SIDEBAR_DEFAULT_WIDTH = 326;
const SIDEBAR_MIN_WIDTH = 240;
const SIDEBAR_MAX_WIDTH = 520;
const SIDEBAR_STEP = 24;
const SIDEBAR_WIDTH_KEY = "wuu.desktop.sidebarWidth";
const SIDEBAR_COLLAPSED_KEY = "wuu.desktop.sidebarCollapsed";
const CONVERSATION_AUTO_SCROLL_THRESHOLD_PX = 48;
const ENABLE_LAUNCH_PREVIEW = Boolean((import.meta as ImportMeta & { env?: { DEV?: boolean } }).env?.DEV);

type SidebarResizeSession = {
  startX: number;
  startWidth: number;
};

type ComposerVariant = "dock" | "hero";
type WorkspacePanelView = "files" | "chat" | "browser" | "review" | "terminal";

const WORKSPACE_TOOL_ITEMS: Array<{
  id: WorkspacePanelView;
  title: string;
  subtitle: string;
}> = [
  { id: "files", title: "文件", subtitle: "浏览项目文件" },
  { id: "chat", title: "侧边聊天", subtitle: "发起侧边对话" },
  { id: "browser", title: "浏览器", subtitle: "打开网站" },
  { id: "review", title: "审查", subtitle: "查看代码更改" },
  { id: "terminal", title: "终端", subtitle: "运行项目命令" }
];

const WORKSPACE_TREE_CSS = `
  :host {
    --trees-fg-override: #34393d;
    --trees-muted-fg-override: #7a8085;
    --trees-selected-bg-override: #eeeeeb;
    --trees-hover-bg-override: #f4f4f2;
    --trees-border-color-override: transparent;
    font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    font-size: 13px;
    line-height: 1.35;
  }

  button[data-type="item"] {
    border-radius: 7px;
  }
`;

const OVERLAY_SCROLLBAR_OPTIONS = {
  scrollbars: {
    autoHide: "leave",
    autoHideDelay: 360,
    clickScroll: true,
    theme: "os-theme-wuu"
  }
} satisfies PartialOptions;

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

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

function isCodexProvider(initialized: InitializeResult): boolean {
  const summary = initialized.providers?.find((provider) => provider.name === initialized.provider);
  const type = (summary?.type ?? initialized.provider).trim().toLowerCase().replaceAll("_", "-");
  return type === "openai-codex" || type === "codex-subscription" || type === "chatgpt-codex";
}

function displayCodexModelName(model?: CodexModelSummary): string {
  return model?.display_name || model?.slug || "GPT";
}

function shortCodexModelLabel(model: string): string {
  return model.replace(/^gpt-/i, "");
}

function codexEffortLabel(effort: string): string {
  switch (effort) {
    case "":
      return "智能";
    case "none":
      return "无";
    case "minimal":
      return "最少";
    case "low":
      return "低";
    case "medium":
      return "中";
    case "high":
      return "高";
    case "xhigh":
    case "max":
      return "超高";
    default:
      return effort;
  }
}

function codexEffortOptions(model: CodexModelSummary | undefined, currentEffort: string): string[] {
  const defaults = ["low", "medium", "high", "xhigh"];
  const supported = (model?.supported_reasoning?.length ? model.supported_reasoning : defaults).filter(Boolean);
  const options = ["", ...supported];
  if (currentEffort && !options.includes(currentEffort)) {
    options.push(currentEffort);
  }
  return options;
}

function normalizedEffortForModel(currentEffort: string, model: CodexModelSummary): string {
  if (currentEffort === "") {
    return "";
  }
  const supported = model.supported_reasoning ?? [];
  if (supported.length === 0 || supported.includes(currentEffort)) {
    return currentEffort;
  }
  if (model.default_reasoning_level && supported.includes(model.default_reasoning_level)) {
    return model.default_reasoning_level;
  }
  return supported[0] ?? "";
}

export function App(): JSX.Element {
  const [state, setState] = useState<AppState>(initialState);
  const [prompt, setPrompt] = useState("");
  const [sidebarWidth, setSidebarWidth] = useState(initialSidebarWidth);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(initialSidebarCollapsed);
  const [resizingSidebar, setResizingSidebar] = useState(false);
  const [projectMenuOpen, setProjectMenuOpen] = useState(false);
  const [runtimeMenuOpen, setRuntimeMenuOpen] = useState(false);
  const [accessMenuOpen, setAccessMenuOpen] = useState(false);
  const [codexRuntimeMenu, setCodexRuntimeMenu] = useState<CodexRuntimeMenu>(null);
  const [codexModels, setCodexModels] = useState<CodexModelLoadState>({ loading: false, error: "", models: [] });
  const [modeMenuOpen, setModeMenuOpen] = useState(false);
  const [branchMenuOpen, setBranchMenuOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [projectFilter, setProjectFilter] = useState("");
  const [launchPreviewPinned, setLaunchPreviewPinned] = useState(false);
  const [rightPanelOpen, setRightPanelOpen] = useState(false);
  const [bottomPanelOpen, setBottomPanelOpen] = useState(false);
  const [workspacePanelView, setWorkspacePanelView] = useState<WorkspacePanelView>("files");
  const [workspaceMode, setWorkspaceMode] = useState<WorkspacePanelView | undefined>(undefined);
  const [selectedWorkspaceFile, setSelectedWorkspaceFile] = useState<string | undefined>(undefined);
  const conversationScrollRef = useRef<HTMLDivElement | null>(null);
  const conversationAutoFollowRef = useRef(true);
  const streamScrollFrameRef = useRef<number | undefined>(undefined);
  const resizeSessionRef = useRef<SidebarResizeSession | null>(null);
  const projectMenuRef = useRef<HTMLDivElement>(null);
  const runtimeMenuRef = useRef<HTMLDivElement>(null);
  const accessMenuRef = useRef<HTMLDivElement>(null);
  const codexRuntimeRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const root = document.documentElement;
    let resizeEndTimer: number | undefined;
    let resizing = false;

    function setResizeState(nextResizing: boolean): void {
      if (resizing === nextResizing) {
        return;
      }
      resizing = nextResizing;
      root.classList.toggle("window-resizing", nextResizing);
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
      setResizeState(false);
    };
  }, []);

  useEffect(() => {
    let mounted = true;
    const off = window.wuu.onServerEvent((event) => {
      if (!mounted) {
        return;
      }
      const handling = handleStreamingNotification(event);
      if (handling === "stream") {
        scheduleStreamScroll();
        return;
      }
      if (handling === "skip") {
        return;
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
      if ((runtimeMenuOpen || modeMenuOpen || branchMenuOpen) && !runtimeMenuRef.current?.contains(target)) {
        setRuntimeMenuOpen(false);
        setModeMenuOpen(false);
        setBranchMenuOpen(false);
      }
      if (accessMenuOpen && !accessMenuRef.current?.contains(target)) {
        setAccessMenuOpen(false);
      }
      if (codexRuntimeMenu && !codexRuntimeRef.current?.contains(target)) {
        setCodexRuntimeMenu(null);
      }
    }

    window.addEventListener("pointerdown", handlePointerDown);
    return () => window.removeEventListener("pointerdown", handlePointerDown);
  }, [accessMenuOpen, branchMenuOpen, codexRuntimeMenu, modeMenuOpen, projectMenuOpen, runtimeMenuOpen]);

  useEffect(() => {
    conversationAutoFollowRef.current = true;
    scrollConversationToBottom({ force: true });
  }, [state.thread?.id]);

  useEffect(() => {
    scheduleStreamScroll();
  }, [state.thread?.turns]);

  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent): void {
      if (settingsOpen) {
        return;
      }
      if (!event.metaKey || event.altKey || event.ctrlKey || event.shiftKey) {
        return;
      }
      const index = Number(event.key) - 1;
      if (!Number.isInteger(index) || index < 0 || index >= 8) {
        return;
      }
      const thread = state.threads[index];
      if (!thread) {
        return;
      }
      event.preventDefault();
      void selectThread(thread.id);
    }

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [settingsOpen, state.thread?.id, state.threads, state.running]);

  useEffect(() => {
    window.localStorage.setItem(SIDEBAR_WIDTH_KEY, String(sidebarWidth));
    window.localStorage.setItem(SIDEBAR_COLLAPSED_KEY, String(sidebarCollapsed));
  }, [sidebarWidth, sidebarCollapsed]);

  useEffect(() => {
    setSelectedWorkspaceFile(undefined);
  }, [state.activeContext?.cwd]);

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

  const activeProject = useMemo(
    () => state.projects.find((project) => project.id === state.activeProjectId),
    [state.activeProjectId, state.projects]
  );
  const activeTitle = workspaceMode ? workspaceModeTitle(workspaceMode) : state.thread?.preview || "新对话";
  const emptyThreadTitle =
    state.activeContext?.kind === "project"
      ? `我们应该在 ${activeProject?.name ?? "这个项目"} 中构建什么？`
      : "我们应该在 wuu 中构建什么？";
  const turns = state.thread?.turns ?? [];
  const emptyConversation = turns.length === 0;
  const previewingLaunch = ENABLE_LAUNCH_PREVIEW && launchPreviewPinned;
  const showingWorkspaceMode = state.initialized && !previewingLaunch && workspaceMode !== undefined;
  const shellClassName = `app-shell${sidebarCollapsed ? " sidebar-collapsed" : ""}${
    resizingSidebar ? " resizing-sidebar" : ""
  }${rightPanelOpen ? " right-panel-open" : ""}${bottomPanelOpen ? " bottom-panel-open" : ""}`;
  const shellStyle = {
    "--sidebar-width": `${sidebarCollapsed ? 0 : sidebarWidth}px`,
    "--workspace-right-panel-width": "360px",
    "--workspace-bottom-panel-height": "238px"
  } as CSSProperties;

  function applySidebarWidth(nextWidth: number): void {
    if (nextWidth <= SIDEBAR_MIN_WIDTH) {
      setSidebarCollapsed(true);
      return;
    }
    setSidebarCollapsed(false);
    setSidebarWidth(clamp(nextWidth, SIDEBAR_MIN_WIDTH, SIDEBAR_MAX_WIDTH));
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

  function handleConversationScroll(): void {
    const node = conversationViewport();
    if (!node) {
      return;
    }
    conversationAutoFollowRef.current = isConversationNearBottom(node);
  }

  function scrollConversationToBottom(options: { force?: boolean } = {}): void {
    const node = conversationViewport();
    if (!node || (!options.force && !conversationAutoFollowRef.current)) {
      return;
    }
    node.scrollTop = node.scrollHeight;
    conversationAutoFollowRef.current = true;
  }

  function conversationViewport(): HTMLElement | undefined {
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
    setResizingSidebar(true);
  }

  function toggleSidebar(): void {
    setSidebarCollapsed((collapsed) => !collapsed);
    setSidebarWidth((width) => (width <= SIDEBAR_MIN_WIDTH ? SIDEBAR_DEFAULT_WIDTH : width));
  }

  function openWorkspaceTool(view: WorkspacePanelView): void {
    setWorkspacePanelView(view);
    setWorkspaceMode(view);
    setRightPanelOpen(true);
  }

  function openWorkspaceFile(path: string): void {
    setWorkspacePanelView("files");
    setWorkspaceMode("files");
    setRightPanelOpen(true);
    setSelectedWorkspaceFile((current) => (current === path ? current : path));
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
        prompt={prompt}
        setPrompt={setPrompt}
        running={state.running}
        status={state.status}
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
        onSelectProject={(id) => void openProject(id)}
        onSelectNoProject={() => void useNoProject(false)}
        onSelectGitBranch={(branch) => void checkoutBranch(branch)}
        onCreateProject={() => void createBlankProject()}
        onOpenProject={() => void chooseProjectFolder()}
        onSend={() => void sendPrompt()}
        onInterrupt={() => void interrupt()}
      />
    );
  }

  async function loadRuntime(projectState: ProjectListResult): Promise<Partial<AppState>> {
    if (!projectState.active_context) {
      return emptyRuntimeState(projectState);
    }
    const [initialized, gitStatus] = await Promise.all([window.wuu.initialize(), window.wuu.gitStatus()]);
    const listed = await window.wuu.listThreads();
    const thread =
      listed.threads.length > 0
        ? requireThread(await window.wuu.resumeThread(listed.threads[0].id), "resume did not return a thread")
        : undefined;
    return {
      initialized,
      projects: projectState.projects,
      activeContext: projectState.active_context,
      activeProjectId: activeProjectID(projectState.active_context),
      gitStatus,
      thread,
      threads: thread ? upsertThread(listed.threads, thread) : listed.threads.filter(isThread),
      running: false,
      status: "ready"
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
      threads: [],
      running: false,
      status: "no-runtime"
    };
  }

  function closeProjectMenus(): void {
    setProjectMenuOpen(false);
    setRuntimeMenuOpen(false);
    setAccessMenuOpen(false);
    setCodexRuntimeMenu(null);
    setModeMenuOpen(false);
    setBranchMenuOpen(false);
    setSettingsOpen(false);
    setProjectFilter("");
  }

  async function openProject(projectId: string): Promise<void> {
    if (projectId === state.activeProjectId && state.activeContext?.kind === "project") {
      closeProjectMenus();
      return;
    }
    closeProjectMenus();
    setState((current) => ({
      ...current,
      activeContext: undefined,
      activeProjectId: projectId,
      initialized: undefined,
      thread: undefined,
      threads: [],
      running: false,
      status: "opening"
    }));
    try {
      const projectState = await window.wuu.selectProject(projectId);
      const loadedState = await loadRuntime(projectState);
      setState((current) => ({ ...current, ...loadedState }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "open project failed"
      }));
    }
  }

  async function createBlankProject(): Promise<void> {
    closeProjectMenus();
    try {
      const projectState = await window.wuu.createBlankProject();
      if (sameRuntimeContext(projectState.active_context, state.activeContext)) {
        setState((current) => ({ ...current, projects: projectState.projects }));
        return;
      }
      const loadedState = await loadRuntime(projectState);
      setState((current) => ({ ...current, ...loadedState }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "create project failed"
      }));
    }
  }

  async function chooseProjectFolder(): Promise<void> {
    closeProjectMenus();
    try {
      const projectState = await window.wuu.chooseProjectFolder();
      if (sameRuntimeContext(projectState.active_context, state.activeContext)) {
        setState((current) => ({ ...current, projects: projectState.projects }));
        return;
      }
      const loadedState = await loadRuntime(projectState);
      setState((current) => ({ ...current, ...loadedState }));
    } catch (error) {
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
    closeProjectMenus();
    setState((current) => ({
      ...current,
      activeContext: undefined,
      activeProjectId: undefined,
      initialized: undefined,
      thread: undefined,
      threads: [],
      running: false,
      status: "opening"
    }));
    try {
      const projectState = await window.wuu.selectNoProject(fresh);
      const loadedState = await loadRuntime(projectState);
      setState((current) => ({ ...current, ...loadedState }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "open no-project failed"
      }));
    }
  }

  async function checkoutBranch(branch: string): Promise<void> {
    if (!branch || state.running) {
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

  async function startNewThread(): Promise<void> {
    if (!state.activeContext || state.running) {
      return;
    }
    setPrompt("");
    if (state.activeContext.kind === "no_project" && state.thread) {
      await useNoProject(true);
      return;
    }
    setState((current) => ({
      ...current,
      thread: undefined,
      running: false,
      status: "ready"
    }));
  }

  async function selectThread(threadId: string): Promise<void> {
    if (!state.activeContext || threadId === state.thread?.id || state.running) {
      return;
    }
    setState((current) => ({ ...current, status: "loading" }));
    try {
      const thread = requireThread(await window.wuu.resumeThread(threadId), "resume did not return a thread");
      setState((current) => ({
        ...current,
        thread,
        threads: upsertThread(current.threads, thread),
        status: "ready"
      }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "load failed"
      }));
    }
  }

  async function sendPrompt(): Promise<void> {
    const text = prompt.trim();
    if (!text || !state.activeContext || !state.initialized || state.running) {
      return;
    }
    conversationAutoFollowRef.current = true;
    setPrompt("");
    setState((current) => ({ ...current, running: true, status: "thinking" }));
    try {
      const thread = state.thread ?? requireThread(await window.wuu.startThread(), "thread/start did not return a thread");
      setState((current) => ({
        ...current,
        thread,
        threads: upsertThread(current.threads, thread)
      }));
      const result = await window.wuu.startTurn(thread.id, text);
      setState((current) =>
        updateThread({ ...current, thread: current.thread?.id === thread.id ? current.thread : thread }, (currentThread) =>
          upsertTurn(currentThread, result.turn)
        )
      );
    } catch (error) {
      setState((current) => ({
        ...current,
        running: false,
        status: error instanceof Error ? error.message : "send failed"
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
      state.running ||
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
    if (!state.initialized || state.running || !isCodexProvider(state.initialized)) {
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
    if (!state.initialized || state.running) {
      return;
    }
    const nextEffort = normalizedEffortForModel(state.initialized.effort ?? "", nextModel);
    await updateRuntimeSettings(state.initialized.provider, nextModel.slug, nextEffort);
    setCodexRuntimeMenu(null);
  }

  async function selectCodexEffort(nextEffort: string): Promise<void> {
    if (!state.initialized || state.running) {
      return;
    }
    await updateRuntimeSettings(state.initialized.provider, state.initialized.model, nextEffort);
    setCodexRuntimeMenu(null);
  }

  async function interrupt(): Promise<void> {
    if (!state.thread) {
      return;
    }
    await window.wuu.interruptTurn(state.thread.id);
  }

  if (settingsOpen) {
    return (
      <>
        <SettingsView
          initialized={state.initialized}
          running={state.running}
          onBack={() => setSettingsOpen(false)}
          onSave={updateRuntimeSettings}
        />
        {state.askRequest ? <AskUserDialog request={state.askRequest} /> : null}
      </>
    );
  }

  return (
    <div className={shellClassName} style={shellStyle}>
      <aside className="sidebar">
        <div className="traffic-spacer" />
        <nav className="primary-nav" aria-label="主导航">
          <button className="nav-item" onClick={() => void startNewThread()} disabled={!state.activeContext || state.running}>
            <MessageSquarePlus size={18} />
            <span>新对话</span>
          </button>
        </nav>

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
            threads={state.threads}
            activeThreadID={state.thread?.id}
            onSelectProject={(id) => void openProject(id)}
            onSelectThread={(id) => void selectThread(id)}
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

      <main className="conversation-pane">
        <header className="titlebar">
          <div className="title-block">
            {sidebarCollapsed ? (
              <button className="icon-button sidebar-toggle-button" aria-label="展开侧边栏" onClick={toggleSidebar}>
                <PanelLeftOpen size={18} />
              </button>
            ) : null}
            {workspaceMode ? (
              <span className="workspace-title-icon" aria-hidden="true">
                <WorkspaceToolIcon view={workspaceMode} size={18} />
              </span>
            ) : null}
            <h1>{activeTitle}</h1>
          </div>
          <div className="title-actions">
            {ENABLE_LAUNCH_PREVIEW ? (
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
              className={`icon-button workspace-toggle-button${rightPanelOpen ? " active" : ""}`}
              type="button"
              aria-label={rightPanelOpen ? "关闭右侧栏" : "打开右侧栏"}
              aria-pressed={rightPanelOpen}
              onClick={() => setRightPanelOpen((open) => !open)}
            >
              <PanelRightOpen size={18} />
            </button>
          </div>
        </header>

        {state.initialized && !previewingLaunch ? (
          <div
            className={`scroll-region${emptyConversation && !showingWorkspaceMode ? " empty-scroll-region" : ""}${
              showingWorkspaceMode ? " workspace-scroll-region" : ""
            }`}
            onScroll={handleConversationScroll}
            ref={conversationScrollRef}
          >
            {workspaceMode ? (
              <WorkspaceMainPanel
                view={workspaceMode}
                activeContext={state.activeContext}
                selectedFilePath={selectedWorkspaceFile}
                onOpenRightPanel={() => {
                  setWorkspacePanelView(workspaceMode);
                  setRightPanelOpen(true);
                }}
              />
            ) : emptyConversation ? (
              <EmptyConversationHome
                title={emptyThreadTitle}
                onOpenProject={() => void chooseProjectFolder()}
                onCreateProject={() => void createBlankProject()}
                onSelectNoProject={() => void useNoProject(true)}
              >
                {renderComposer("hero")}
              </EmptyConversationHome>
            ) : (
              <div className="conversation-width">
                {turns.map((turn) => (
                  <TurnView
                    key={turn.id}
                    turn={turn}
                    cwd={state.thread?.cwd ?? state.activeContext?.cwd}
                    onStreamFrame={scheduleStreamScroll}
                  />
                ))}
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

        {state.initialized && !previewingLaunch && !emptyConversation && !showingWorkspaceMode
          ? renderComposer("dock")
          : null}
      </main>

      <WorkspaceRightPanel
        open={rightPanelOpen}
        view={workspacePanelView}
        activeContext={state.activeContext}
        selectedFilePath={selectedWorkspaceFile}
        onSelectView={openWorkspaceTool}
        onOpenFile={openWorkspaceFile}
        onClose={() => setRightPanelOpen(false)}
      />
      <WorkspaceBottomPanel
        open={bottomPanelOpen}
        selectedView={workspacePanelView}
        onSelectTool={openWorkspaceTool}
        onClose={() => setBottomPanelOpen(false)}
      />

      {state.askRequest ? <AskUserDialog request={state.askRequest} /> : null}
    </div>
  );
}

function WorkspaceRightPanel({
  open,
  view,
  activeContext,
  selectedFilePath,
  onSelectView,
  onOpenFile,
  onClose
}: {
  open: boolean;
  view: WorkspacePanelView;
  activeContext?: RuntimeContext;
  selectedFilePath?: string;
  onSelectView: (view: WorkspacePanelView) => void;
  onOpenFile: (path: string) => void;
  onClose: () => void;
}): JSX.Element {
  const activeTool = workspaceToolFor(view);

  return (
    <aside className="workspace-right-panel" aria-hidden={!open}>
      <div className="workspace-panel-header">
        <div className="workspace-panel-title">
          <WorkspaceToolIcon view={view} size={18} />
          <span>{activeTool.title}</span>
        </div>
        <button
          className="icon-button workspace-panel-close"
          type="button"
          aria-label="关闭右侧栏"
          disabled={!open}
          onClick={onClose}
        >
          <X size={17} />
        </button>
      </div>
      {open ? (
        <>
          <div className="workspace-panel-tabs" role="tablist" aria-label="右侧栏工具">
            {WORKSPACE_TOOL_ITEMS.map((item) => (
              <button
                key={item.id}
                className={item.id === view ? "active" : ""}
                type="button"
                role="tab"
                aria-selected={item.id === view}
                title={item.title}
                onClick={() => onSelectView(item.id)}
              >
                <WorkspaceToolIcon view={item.id} size={17} />
              </button>
            ))}
          </div>
          <div className="workspace-panel-body">
            {view === "files" ? (
              <WorkspaceFileTree
                activeContext={activeContext}
                open={open}
                selectedFilePath={selectedFilePath}
                onOpenFile={onOpenFile}
              />
            ) : (
              <WorkspacePanelPlaceholder view={view} />
            )}
          </div>
        </>
      ) : null}
    </aside>
  );
}

function WorkspaceBottomPanel({
  open,
  selectedView,
  onSelectTool,
  onClose
}: {
  open: boolean;
  selectedView: WorkspacePanelView;
  onSelectTool: (view: WorkspacePanelView) => void;
  onClose: () => void;
}): JSX.Element {
  return (
    <section className="workspace-bottom-panel" aria-hidden={!open}>
      <div className="workspace-bottom-header">
        <div className="workspace-bottom-title">工具</div>
        <button
          className="icon-button workspace-panel-close"
          type="button"
          aria-label="关闭底部栏"
          disabled={!open}
          onClick={onClose}
        >
          <X size={17} />
        </button>
      </div>
      {open ? (
        <OverlayScrollbarsComponent
          className="workspace-tool-grid"
          aria-label="工作区工具"
          data-overlayscrollbars-initialize
          defer
          options={OVERLAY_SCROLLBAR_OPTIONS}
        >
          {WORKSPACE_TOOL_ITEMS.map((item) => (
            <button
              key={item.id}
              className={`workspace-tool-card${item.id === selectedView ? " active" : ""}`}
              type="button"
              onClick={() => onSelectTool(item.id)}
            >
              <WorkspaceToolIcon view={item.id} size={25} />
              <strong>{item.title}</strong>
              <span>{item.subtitle}</span>
            </button>
          ))}
        </OverlayScrollbarsComponent>
      ) : null}
    </section>
  );
}

function WorkspaceFileTree({
  activeContext,
  open,
  selectedFilePath,
  onOpenFile
}: {
  activeContext?: RuntimeContext;
  open: boolean;
  selectedFilePath?: string;
  onOpenFile: (path: string) => void;
}): JSX.Element {
  const [fileTree, setFileTree] = useState<FileTreeListResult | undefined>(undefined);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  const workspaceRoot = activeContext?.cwd;

  useEffect(() => {
    if (!open || !workspaceRoot) {
      return;
    }

    let cancelled = false;
    setFileTree(undefined);
    setLoading(true);
    setError(undefined);
    void window.wuu
      .listWorkspaceFiles()
      .then((result) => {
        if (cancelled) {
          return;
        }
        setFileTree(result);
      })
      .catch((nextError) => {
        if (cancelled) {
          return;
        }
        setError(desktopApiErrorMessage(nextError, "读取文件失败"));
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [open, workspaceRoot]);

  if (!workspaceRoot) {
    return <WorkspacePanelEmpty title="没有项目" description="先选择一个项目。这个面板会显示它的文件。" />;
  }

  if (loading && !fileTree) {
    return <WorkspacePanelEmpty title="正在读取文件" description="文件树马上就绪。" />;
  }

  if (error) {
    return <WorkspacePanelEmpty title="读取失败" description={error} />;
  }

  if (!fileTree || fileTree.paths.length === 0) {
    return <WorkspacePanelEmpty title="没有文件" description={formatWorkspaceRoot(workspaceRoot)} />;
  }

  return (
    <div className="workspace-file-panel">
      <div className="workspace-file-meta">
        <span>{formatWorkspaceRoot(fileTree.root)}</span>
        <small>
          {fileTree.paths.length} 项{fileTree.truncated ? "，已截断" : ""}
        </small>
      </div>
      <WorkspaceFileTreeView
        paths={fileTree.paths}
        selectedFilePath={selectedFilePath}
        onOpenFile={onOpenFile}
      />
    </div>
  );
}

function WorkspaceFileTreeView({
  paths,
  selectedFilePath,
  onOpenFile
}: {
  paths: string[];
  selectedFilePath?: string;
  onOpenFile: (path: string) => void;
}): JSX.Element {
  const { model } = useFileTree({
    flattenEmptyDirectories: true,
    initialExpansion: 1,
    initialSelectedPaths: selectedFilePath ? [selectedFilePath] : [],
    itemHeight: 28,
    overscan: 8,
    paths,
    search: true,
    stickyFolders: true,
    unsafeCSS: WORKSPACE_TREE_CSS
  });
  const selectedPaths = useFileTreeSelection(model);
  const onOpenFileRef = useRef(onOpenFile);

  useEffect(() => {
    onOpenFileRef.current = onOpenFile;
  }, [onOpenFile]);

  useEffect(() => {
    model.resetPaths(paths);
  }, [model, paths]);

  useEffect(() => {
    const nextPath = selectedPaths[0];
    if (!nextPath || nextPath.endsWith("/")) {
      return;
    }
    onOpenFileRef.current(nextPath);
  }, [selectedPaths]);

  return (
    <FileTree
      className="workspace-file-tree"
      model={model}
      style={{ height: "100%", width: "100%" }}
    />
  );
}

function WorkspacePanelPlaceholder({ view }: { view: WorkspacePanelView }): JSX.Element {
  const tool = workspaceToolFor(view);
  return (
    <WorkspacePanelEmpty
      title={tool.title}
      description={`${tool.subtitle}会在这里打开。`}
      icon={<WorkspaceToolIcon view={view} size={24} />}
    />
  );
}

function WorkspacePanelEmpty({
  title,
  description,
  icon
}: {
  title: string;
  description: string;
  icon?: JSX.Element;
}): JSX.Element {
  return (
    <div className="workspace-panel-empty">
      <div className="workspace-panel-empty-icon">{icon ?? <FolderOpen size={24} />}</div>
      <strong>{title}</strong>
      <span>{description}</span>
    </div>
  );
}

function WorkspaceToolIcon({ view, size }: { view: WorkspacePanelView; size: number }): JSX.Element {
  switch (view) {
    case "files":
      return <FolderOpen size={size} />;
    case "chat":
      return <MessageSquarePlus size={size} />;
    case "browser":
      return <Globe2 size={size} />;
    case "review":
      return <ShieldCheck size={size} />;
    case "terminal":
      return <Terminal size={size} />;
  }
}

function workspaceToolFor(view: WorkspacePanelView): (typeof WORKSPACE_TOOL_ITEMS)[number] {
  return WORKSPACE_TOOL_ITEMS.find((item) => item.id === view) ?? WORKSPACE_TOOL_ITEMS[0];
}

function formatWorkspaceRoot(root: string): string {
  const segments = root.split(/[\\/]/).filter(Boolean);
  return segments.at(-1) ?? root;
}

function workspaceModeTitle(view: WorkspacePanelView): string {
  return view === "files" ? "打开文件" : workspaceToolFor(view).title;
}

function WorkspaceMainPanel({
  view,
  activeContext,
  selectedFilePath,
  onOpenRightPanel
}: {
  view: WorkspacePanelView;
  activeContext?: RuntimeContext;
  selectedFilePath?: string;
  onOpenRightPanel: () => void;
}): JSX.Element {
  if (view === "files") {
    return (
      <WorkspaceFilePreview
        activeContext={activeContext}
        selectedFilePath={selectedFilePath}
        onOpenRightPanel={onOpenRightPanel}
      />
    );
  }

  return (
    <div className="workspace-main-empty">
      <WorkspaceToolIcon view={view} size={34} />
      <strong>{workspaceToolFor(view).title}</strong>
      <span>{workspaceToolFor(view).subtitle}</span>
    </div>
  );
}

function WorkspaceFilePreview({
  activeContext,
  selectedFilePath,
  onOpenRightPanel
}: {
  activeContext?: RuntimeContext;
  selectedFilePath?: string;
  onOpenRightPanel: () => void;
}): JSX.Element {
  const [file, setFile] = useState<WorkspaceFileReadResult | undefined>(undefined);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  useEffect(() => {
    if (!selectedFilePath) {
      setFile(undefined);
      setError(undefined);
      setLoading(false);
      return;
    }

    let cancelled = false;
    setFile(undefined);
    setLoading(true);
    setError(undefined);
    void window.wuu
      .readWorkspaceFile(selectedFilePath)
      .then((result) => {
        if (!cancelled) {
          setFile(result);
        }
      })
      .catch((nextError) => {
        if (!cancelled) {
          setError(desktopApiErrorMessage(nextError, "打开文件失败"));
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
  }, [selectedFilePath]);

  if (!activeContext) {
    return (
      <div className="workspace-main-empty">
        <FolderX size={36} />
        <strong>没有项目</strong>
        <span>先打开一个项目，再浏览文件。</span>
      </div>
    );
  }

  if (!selectedFilePath) {
    return (
      <div className="workspace-main-empty">
        <FolderOpen size={38} />
        <strong>打开文件</strong>
        <span>从工作区目录树中选择文件</span>
        <button type="button" onClick={onOpenRightPanel}>
          显示目录树
        </button>
      </div>
    );
  }

  if (loading) {
    return (
      <div className="workspace-main-empty">
        <FileText size={36} />
        <strong>正在打开</strong>
        <span>{selectedFilePath}</span>
      </div>
    );
  }

  if (error) {
    return (
      <div className="workspace-main-empty">
        <AlertCircle size={36} />
        <strong>打开失败</strong>
        <span>{error}</span>
      </div>
    );
  }

  if (!file) {
    return (
      <div className="workspace-main-empty">
        <FileText size={36} />
        <strong>没有内容</strong>
        <span>{selectedFilePath}</span>
      </div>
    );
  }

  if (file.binary) {
    return (
      <div className="workspace-main-empty">
        <FileText size={36} />
        <strong>无法预览</strong>
        <span>{file.path} 是二进制文件。</span>
      </div>
    );
  }

  return (
    <article className="workspace-file-preview">
      <header className="workspace-file-preview-header">
        <div>
          <strong>{file.path}</strong>
          <span>
            {formatBytes(file.size_bytes)}
            {file.truncated ? " · 仅显示前 512 KB" : ""}
          </span>
        </div>
      </header>
      <OverlayScrollbarsComponent
        className="workspace-file-code-scroll"
        data-overlayscrollbars-initialize
        defer
        options={OVERLAY_SCROLLBAR_OPTIONS}
      >
        <pre className="workspace-file-code">
          <code>{file.text}</code>
        </pre>
      </OverlayScrollbarsComponent>
    </article>
  );
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  if (bytes < 1024 * 1024) {
    return `${Math.round(bytes / 102.4) / 10} KB`;
  }
  return `${Math.round(bytes / 1024 / 102.4) / 10} MB`;
}

function desktopApiErrorMessage(error: unknown, fallback: string): string {
  const message = error instanceof Error ? error.message : typeof error === "string" ? error : "";
  if (message.includes("No handler registered")) {
    return "文件接口还没被当前窗口加载。请重启桌面端后再试。";
  }
  return message || fallback;
}

function EmptyConversationHome({
  title,
  children,
  onOpenProject,
  onCreateProject,
  onSelectNoProject
}: {
  title: string;
  children: JSX.Element;
  onOpenProject: () => void;
  onCreateProject: () => void;
  onSelectNoProject: () => void;
}): JSX.Element {
  return (
    <section className="empty-home">
      <div className="empty-home-inner">
        <h2>{title}</h2>
        {children}
        <div className="empty-home-actions" aria-label="快速开始">
          <button type="button" onClick={onOpenProject}>
            <FolderOpen size={22} />
            <span>
              <strong>打开项目</strong>
              <small>从本地文件夹开始构建</small>
            </span>
          </button>
          <button type="button" onClick={onCreateProject}>
            <FolderPlus size={22} />
            <span>
              <strong>新建空白项目</strong>
              <small>为新任务准备工作区</small>
            </span>
          </button>
          <button type="button" onClick={onSelectNoProject}>
            <FolderX size={22} />
            <span>
              <strong>临时对话</strong>
              <small>不绑定项目直接开始</small>
            </span>
          </button>
        </div>
      </div>
    </section>
  );
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
      const params = event.message.params as { questions?: AskUserQuestion[] } | undefined;
      return {
        ...state,
        askRequest: {
          id: event.message.id,
          questions: params?.questions ?? []
        }
      };
    }
    case "server-error":
      return { ...state, status: event.message };
    case "server-exit":
      return { ...state, running: false, status: "app-server exited" };
  }
}

type StreamingNotificationHandling = "state" | "stream" | "skip";

function handleStreamingNotification(event: ServerEvent): StreamingNotificationHandling {
  if (event.kind !== "notification") {
    return "state";
  }
  const notification = event.message;
  const params = notification.params as Record<string, unknown> | undefined;
  switch (notification.method) {
    case "item/agentMessage/delta":
      appendStreamDelta(params, "text");
      return "stream";
    case "item/reasoning/delta":
      appendStreamDelta(params, "text");
      return "stream";
    case "item/toolCall/delta":
      appendStreamDelta(params, "arguments");
      return "stream";
    case "item/toolCall/outputDelta":
      appendStreamDelta(params, "result");
      return "stream";
    case "turn/event":
      return "skip";
    case "item/started":
    case "item/completed":
      syncStreamItem(params);
      return "state";
    default:
      return "state";
  }
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
      return { ...state, thread, threads: upsertThread(state.threads, thread), status: "ready" };
    }
    case "turn/started": {
      const turn = params?.turn as Turn | undefined;
      if (!turn) {
        return state;
      }
      return updateThread({ ...state, running: true }, (thread) => upsertTurn(thread, turn));
    }
    case "item/started":
    case "item/completed": {
      const item = params?.item as ThreadItem | undefined;
      const turnID = params?.turn_id as string | undefined;
      if (!item || !turnID) {
        return state;
      }
      return updateThread(state, (thread) => updateTurnItem(thread, turnID, item.id, () => item));
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
      if (!turn) {
        return { ...state, running: false };
      }
      return updateThread({ ...state, running: false, status: "ready" }, (thread) => upsertTurn(thread, turn));
    }
    default:
      return state;
  }
}

function applyDelta(state: AppState, params: Record<string, unknown> | undefined, field: "text" | "arguments" | "result"): AppState {
  const turnID = params?.turn_id as string | undefined;
  const itemID = params?.item_id as string | undefined;
  const delta = params?.delta as string | undefined;
  if (!turnID || !itemID || !delta) {
    return state;
  }
  return updateThread(state, (thread) =>
    updateTurnItem(thread, turnID, itemID, (item) => ({
      ...item,
      [field]: `${item[field] ?? ""}${delta}`
    }))
  );
}

function updateThread(state: AppState, update: (thread: Thread) => Thread): AppState {
  if (!state.thread) {
    return state;
  }
  const thread = update(state.thread);
  return { ...state, thread, threads: upsertThread(state.threads, thread) };
}

function upsertThread(threads: Thread[], thread: Thread | undefined): Thread[] {
  const validThreads = threads.filter(isThread);
  if (!isThread(thread)) {
    return validThreads;
  }
  const index = validThreads.findIndex((item) => item.id === thread.id);
  if (index < 0) {
    return [thread, ...validThreads];
  }
  const next = validThreads.slice();
  next[index] = thread;
  return next;
}

function requireThread(result: { thread?: Thread }, message: string): Thread {
  if (!isThread(result.thread)) {
    throw new Error(message);
  }
  return result.thread;
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

function isThread(value: unknown): value is Thread {
  return Boolean(value && typeof value === "object" && typeof (value as Thread).id === "string");
}

function upsertTurn(thread: Thread, turn: Turn): Thread {
  const index = thread.turns.findIndex((item) => item.id === turn.id);
  if (index < 0) {
    return { ...thread, turns: [...thread.turns, turn], status: turn.status === "in_progress" ? "in_progress" : thread.status };
  }
  const turns = thread.turns.slice();
  turns[index] = turn;
  return { ...thread, turns, status: turn.status === "in_progress" ? "in_progress" : "idle" };
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

function ProjectList({
  projects,
  activeID,
  threads,
  activeThreadID,
  onSelectProject,
  onSelectThread
}: {
  projects: DesktopProject[];
  activeID?: string;
  threads: Thread[];
  activeThreadID?: string;
  onSelectProject: (id: string) => void;
  onSelectThread: (id: string) => void;
}): JSX.Element {
  return (
    <div className="projects">
      {projects.map((project) => (
        <div key={project.id} className="project-group">
          <button
            className={`project-row ${project.id === activeID ? "active" : ""}`}
            onClick={() => onSelectProject(project.id)}
          >
            <Folder size={18} />
            <span>{project.name}</span>
          </button>
          {project.id === activeID ? (
            <ThreadList threads={threads} activeID={activeThreadID} onSelect={onSelectThread} />
          ) : null}
        </div>
      ))}
    </div>
  );
}

function ThreadList({
  threads,
  activeID,
  onSelect
}: {
  threads: Thread[];
  activeID?: string;
  onSelect: (id: string) => void;
}): JSX.Element {
  const visibleThreads = threads.filter((thread): thread is Thread => Boolean(thread?.id));
  return (
    <div className="thread-list">
      {visibleThreads.slice(0, 8).map((thread, index) => (
        <button
          key={thread.id}
          className={`thread-row ${thread.id === activeID ? "active" : ""}`}
          onClick={() => onSelect(thread.id)}
        >
          <span>{thread.preview || "未命名对话"}</span>
          <kbd>⌘{index + 1}</kbd>
        </button>
      ))}
    </div>
  );
}

function TurnView({ turn, cwd, onStreamFrame }: { turn: Turn; cwd?: string; onStreamFrame: () => void }): JSX.Element {
  const renderedItems: JSX.Element[] = [];
  let statusInserted = false;
  let hasAssistantWork = false;

  for (let index = 0; index < turn.items.length; index++) {
    const item = turn.items[index];
    if (item.type === "user_message") {
      renderedItems.push(
        <ThreadItemView
          key={item.id}
          turnID={turn.id}
          item={item}
          cwd={cwd}
          streaming={false}
          onStreamFrame={onStreamFrame}
        />
      );
      continue;
    }

    if (!statusInserted) {
      renderedItems.push(<TurnStatusLine key={`${turn.id}-status`} turn={turn} />);
      statusInserted = true;
    }
    hasAssistantWork = true;

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
      renderedItems.push(<ToolActivityRow key={`${item.id}-activity`} items={group} />);
      index = nextIndex - 1;
      continue;
    }

    renderedItems.push(
      <ThreadItemView
        key={item.id}
        turnID={turn.id}
        item={item}
        cwd={cwd}
        streaming={turn.status === "in_progress" && item.status === "in_progress"}
        onStreamFrame={onStreamFrame}
      />
    );
  }

  if (!statusInserted && turn.status === "in_progress") {
    renderedItems.push(<TurnStatusLine key={`${turn.id}-status`} turn={turn} />);
  }
  if (!hasAssistantWork && turn.status === "in_progress") {
    renderedItems.push(
      <div key={`${turn.id}-thinking`} className="activity-row thinking-inline">
        <Brain size={18} />
        <span>正在思考</span>
      </div>
    );
  }

  return (
    <section className="turn">
      {renderedItems}
      {turn.error ? <div className="turn-error">{turn.error.message}</div> : null}
    </section>
  );
}

function TurnStatusLine({ turn }: { turn: Turn }): JSX.Element {
  const completedDuration = typeof turn.duration_ms === "number" ? turn.duration_ms : undefined;
  const startedAt = Date.parse(turn.started_at);
  const liveDuration = completedDuration === undefined && turn.status === "in_progress" && Number.isFinite(startedAt);
  const elapsedMs =
    completedDuration ?? (Number.isFinite(startedAt) ? Math.max(0, Date.now() - startedAt) : 0);
  const statusLabel =
    turn.status === "failed" ? "处理失败" : turn.status === "interrupted" ? "已停止" : "已处理";

  return (
    <div className="turn-progress">
      <div className="turn-progress-label">
        <Clock size={17} />
        <span>
          {statusLabel} {liveDuration ? <LiveDuration startedAtMs={startedAt} /> : formatDuration(elapsedMs)}
        </span>
      </div>
      <div className="turn-progress-rule" />
    </div>
  );
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

function ThreadItemView({
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
}): JSX.Element | null {
  switch (item.type) {
    case "user_message":
      return (
        <div className="message user-message">
          <RichContent text={item.text} cwd={cwd} />
        </div>
      );
    case "agent_message":
      return (
        <article className="agent-block">
          <div className="agent-text">
            <AgentMessageContent
              turnID={turnID}
              item={item}
              cwd={cwd}
              streaming={streaming}
              onStreamFrame={onStreamFrame}
            />
          </div>
        </article>
      );
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
      return <div className="turn-error">{item.error}</div>;
    default:
      return null;
  }
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

  return (
    <StreamingMarkdown
      streamKey={streamKeyValue}
      initialText={hasBufferedStream ? streamTextStore.seedValue(streamKeyValue) : item.text}
      cwd={cwd}
      final={!streaming}
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

  return (
    <StreamingMarkdown
      streamKey={streamKeyValue}
      initialText={hasBufferedStream ? streamTextStore.seedValue(streamKeyValue) : item.text}
      className="streaming-markdown reasoning-stream"
      final={!streaming}
      onFrame={onStreamFrame}
      onSettled={() => {
        streamTextStore.clearItem(turnID, item.id);
        onStreamFrame();
      }}
    />
  );
}

type ToolActivityKind = "edit" | "create" | "search" | "read" | "list" | "command" | "agent" | "unknown";

type ToolActivitySummary = {
  kind: ToolActivityKind;
  text: string;
  fileName?: string;
  additions: number;
  deletions: number;
  running: boolean;
  failed: boolean;
};

type DiffStats = {
  additions: number;
  deletions: number;
  newFile: boolean;
};

type JsonRecord = Record<string, unknown>;

function ToolActivityRow({ items }: { items: ThreadItem[] }): JSX.Element {
  const summary = summarizeToolActivity(items);
  const sections = buildToolActivitySections(items);
  const summaryText = activitySummaryText(sections, summary);
  const [expanded, setExpanded] = useState(summary.running || summary.failed);
  const className = `activity-group${expanded ? " expanded" : ""}${summary.running ? " running" : ""}${
    summary.failed ? " failed" : ""
  }`;

  useEffect(() => {
    if (summary.running || summary.failed) {
      setExpanded(true);
    }
  }, [summary.failed, summary.running]);

  return (
    <article className={className}>
      <button
        className="activity-row activity-toggle"
        type="button"
        aria-expanded={expanded}
        onClick={() => setExpanded((open) => !open)}
      >
        <ActivityIcon kind={summary.kind} failed={summary.failed} />
        <span className="activity-copy">
          <span>{summaryText}</span>
          {summary.fileName ? <span className="activity-file">{summary.fileName}</span> : null}
          {summary.additions > 0 ? <span className="activity-add">+{summary.additions}</span> : null}
          {summary.deletions > 0 ? <span className="activity-delete">-{summary.deletions}</span> : null}
        </span>
        <ChevronDown className="activity-chevron" size={16} />
      </button>
      <div className="activity-details" aria-hidden={!expanded}>
        <div className="activity-details-inner">
          {sections.map((section) => (
            <ToolActivitySectionView key={section.id} section={section} />
          ))}
        </div>
      </div>
    </article>
  );
}

type ToolActivitySectionStatus = "running" | "completed" | "failed";

type ToolActivitySection = {
  id: string;
  kind: ToolActivityKind;
  title: string;
  subtitle?: string;
  detail?: string;
  status: ToolActivitySectionStatus;
  error?: string;
};

function ToolActivitySectionView({ section }: { section: ToolActivitySection }): JSX.Element {
  return (
    <section className="activity-detail">
      <div className="activity-detail-marker">
        <ActivityIcon kind={section.kind} failed={section.status === "failed"} />
      </div>
      <div className="activity-detail-body">
        <div className="activity-detail-title">
          <strong>{section.title}</strong>
          <span>{section.detail ?? section.subtitle}</span>
        </div>
        {section.error ? <div className="activity-detail-error">{section.error}</div> : null}
      </div>
      <span className={`activity-status ${section.status}`}>{toolSectionStatusLabel(section.status)}</span>
    </section>
  );
}

function toolStatusLabel(item: ThreadItem | undefined): string {
  if (!item) {
    return "";
  }
  if (item.status === "failed" || item.error) {
    return "失败";
  }
  if ((item.status ?? "in_progress") === "in_progress") {
    return "运行中";
  }
  return "完成";
}

function toolSectionStatusLabel(status: ToolActivitySectionStatus): string {
  switch (status) {
    case "failed":
      return "未完成";
    case "running":
      return "进行中";
    case "completed":
      return "完成";
  }
}

function buildToolActivitySections(items: ThreadItem[]): ToolActivitySection[] {
  const groups = new Map<string, ThreadItem[]>();
  for (const item of items) {
    const key = toolActivitySectionKey(item);
    groups.set(key, [...(groups.get(key) ?? []), item]);
  }
  return Array.from(groups.entries()).map(([key, grouped]) => toolActivitySectionFromItems(key, grouped));
}

function activitySummaryText(sections: ToolActivitySection[], fallback: ToolActivitySummary): string {
  if (sections.length === 0) {
    return fallback.text;
  }
  const fragments = sections.map((section) => sectionSummaryText(section)).filter(Boolean);
  if (fragments.length === 0) {
    return fallback.text;
  }
  return fallback.failed ? `未完成 · ${fragments.join("，")}` : fragments.join("，");
}

function sectionSummaryText(section: ToolActivitySection): string {
  return section.title;
}

function toolActivitySectionKey(item: ThreadItem): string {
  const name = (item.name ?? "").trim();
  switch (name) {
    case "read_file":
    case "list_files":
      return "read";
    case "grep":
    case "glob":
    case "web_search":
      return "search";
    case "edit_file":
    case "write_file":
      return "change";
    case "run_shell":
    case "git":
    case "start_process":
    case "read_process_output":
    case "stop_process":
      return "command";
    case "spawn_agent":
    case "send_message":
    case "followup_task":
    case "wait_agent":
    case "close_agent":
    case "list_agents":
      return "agent";
    default:
      return "other";
  }
}

function toolActivitySectionFromItems(key: string, items: ThreadItem[]): ToolActivitySection {
  switch (key) {
    case "read":
      return {
        id: key,
        kind: "read",
        title: `查看 ${items.length} 处`,
        detail: compactDetailText(compactToolTargets(items)),
        status: combinedToolStatus(items),
        error: firstToolError(items)
      };
    case "search":
      return {
        id: key,
        kind: "search",
        title: `搜索 ${items.length} 次`,
        detail: compactDetailText(compactSearchTargets(items)),
        status: combinedToolStatus(items),
        error: firstToolError(items)
      };
    case "change":
      return {
        id: key,
        kind: "edit",
        title: `更新 ${items.length} 个文件`,
        detail: compactDetailText(compactToolTargets(items)),
        status: combinedToolStatus(items),
        error: firstToolError(items)
      };
    case "command":
      return {
        id: key,
        kind: "command",
        title: `检查 ${items.length} 项`,
        detail: compactDetailText(compactCommandLabels(items)),
        status: combinedToolStatus(items),
        error: firstToolError(items)
      };
    case "agent":
      return {
        id: key,
        kind: "agent",
        title: `子任务 ${items.length} 项`,
        detail: compactDetailText(compactAgentLabels(items)),
        status: combinedToolStatus(items),
        error: firstToolError(items)
      };
    default:
      return {
        id: key,
        kind: "unknown",
        title: `工具 ${items.length} 项`,
        detail: compactDetailText(uniqueStrings(items.map((item) => readableToolName(item.name)))),
        status: combinedToolStatus(items),
        error: firstToolError(items)
      };
  }
}

function combinedToolStatus(items: ThreadItem[]): ToolActivitySectionStatus {
  if (items.some((item) => item.status === "failed" || item.error)) {
    return "failed";
  }
  if (items.some((item) => (item.status ?? "in_progress") === "in_progress")) {
    return "running";
  }
  return "completed";
}

function firstToolError(items: ThreadItem[]): string | undefined {
  return items.find((item) => item.error)?.error;
}

function compactToolTargets(items: ThreadItem[]): string[] {
  return uniqueStrings(
    items
      .map((item) => {
        const args = parseJSONRecord(item.arguments);
        const result = parseJSONRecord(item.result);
        const path = stringValue(result, "path") ?? stringValue(args, "path") ?? stringValue(args, "file");
        return path ? fileBaseName(path) : undefined;
      })
      .filter((value): value is string => Boolean(value))
  );
}

function compactSearchTargets(items: ThreadItem[]): string[] {
  return uniqueStrings(
    items
      .map((item) => {
        const args = parseJSONRecord(item.arguments);
        return stringValue(args, "pattern") ?? stringValue(args, "query") ?? readableToolName(item.name);
      })
      .filter((value): value is string => Boolean(value))
  );
}

function compactCommandLabels(items: ThreadItem[]): string[] {
  return uniqueStrings(items.map((item) => readableCommandLabel(item)));
}

function compactAgentLabels(items: ThreadItem[]): string[] {
  return uniqueStrings(
    items.map((item) => {
      const args = parseJSONRecord(item.arguments);
      return stringValue(args, "task_name") ?? readableToolName(item.name);
    })
  );
}

function compactDetailText(values: string[]): string | undefined {
  if (values.length === 0) {
    return undefined;
  }
  const shown = values.slice(0, 4).join("、");
  return values.length > 4 ? `${shown} 等 ${values.length} 项` : shown;
}

function readableCommandLabel(item: ThreadItem): string {
  const args = parseJSONRecord(item.arguments);
  const result = parseJSONRecord(item.result);
  const name = (item.name ?? "").trim();
  const command = stringValue(result, "command") ?? stringValue(args, "command") ?? "";
  const subcommand = stringValue(result, "subcommand") ?? stringValue(args, "subcommand") ?? "";
  if (name === "git" || command.startsWith("git ")) {
    if (subcommand === "status" || command.includes("status")) {
      return "检查 Git 状态";
    }
    if (subcommand === "diff" || command.includes("diff")) {
      return "查看代码差异";
    }
    if (subcommand === "log" || command.includes("log")) {
      return "查看提交历史";
    }
    return "执行 Git 操作";
  }
  if (/npm\s+run\s+typecheck|tsc\s+--noEmit/.test(command)) {
    return "检查类型";
  }
  if (/npm\s+run\s+build|vite\s+build|electron-vite\s+build/.test(command)) {
    return "构建应用";
  }
  if (/go\s+test|npm\s+test|pnpm\s+test|yarn\s+test/.test(command)) {
    return "运行测试";
  }
  if (name === "read_process_output") {
    return "读取后台输出";
  }
  if (name === "start_process") {
    return "启动后台任务";
  }
  if (name === "stop_process") {
    return "停止后台任务";
  }
  return "运行命令";
}

function readableToolName(name: string | undefined): string {
  switch ((name ?? "").trim()) {
    case "read_file":
      return "查看文件";
    case "list_files":
      return "查看目录";
    case "grep":
      return "搜索内容";
    case "glob":
      return "匹配文件";
    case "edit_file":
      return "编辑文件";
    case "write_file":
      return "写入文件";
    case "web_search":
      return "搜索网页";
    case "web_fetch":
      return "读取网页";
    case "run_shell":
      return "运行命令";
    case "git":
      return "Git 操作";
    default:
      return name?.trim() || "工具";
  }
}

function uniqueStrings(values: string[]): string[] {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const value of values) {
    const normalized = value.trim();
    if (!normalized || seen.has(normalized)) {
      continue;
    }
    seen.add(normalized);
    out.push(normalized);
  }
  return out;
}

function ActivityIcon({ kind, failed }: { kind: ToolActivityKind; failed: boolean }): JSX.Element {
  if (failed) {
    return <AlertCircle size={18} />;
  }
  switch (kind) {
    case "edit":
    case "create":
      return <Pencil size={18} />;
    case "search":
      return <Search size={18} />;
    case "read":
      return <FileText size={18} />;
    case "list":
      return <ListIcon size={18} />;
    case "command":
      return <Terminal size={18} />;
    case "agent":
      return <MessageSquarePlus size={18} />;
    default:
      return <Wrench size={18} />;
  }
}

function summarizeToolActivity(items: ThreadItem[]): ToolActivitySummary {
  const readFiles = new Set<string>();
  const searchedFiles = new Set<string>();
  const editedFiles = new Set<string>();
  const createdFiles = new Set<string>();
  const unknownTools = new Set<string>();
  let searchCount = 0;
  let listCount = 0;
  let commandCount = 0;
  let agentCount = 0;
  let additions = 0;
  let deletions = 0;
  let running = false;
  let failed = false;
  let primaryKind: ToolActivityKind = "unknown";

  for (const item of items) {
    const name = (item.name ?? "tool").trim() || "tool";
    const args = parseJSONRecord(item.arguments);
    const result = parseJSONRecord(item.result);
    const path = stringValue(result, "path") ?? stringValue(args, "path") ?? stringValue(args, "file");

    running = running || (item.status ?? "in_progress") === "in_progress";
    failed = failed || item.status === "failed" || Boolean(item.error);

    if (name === "read_file") {
      primaryKind = primaryKind === "unknown" ? "read" : primaryKind;
      addPath(readFiles, path);
      continue;
    }
    if (name === "grep" || name === "glob" || name === "web_search") {
      primaryKind = primaryKind === "unknown" || primaryKind === "read" ? "search" : primaryKind;
      searchCount++;
      collectResultFiles(result, searchedFiles);
      continue;
    }
    if (name === "list_files") {
      primaryKind = primaryKind === "unknown" ? "list" : primaryKind;
      listCount++;
      continue;
    }
    if (name === "run_shell" || name === "git") {
      primaryKind = primaryKind === "unknown" ? "command" : primaryKind;
      commandCount++;
      continue;
    }
    if (name === "edit_file" || name === "write_file") {
      const diff = summarizeDiff(result);
      const target = diff.newFile ? createdFiles : editedFiles;
      addPath(target, path);
      additions += diff.additions;
      deletions += diff.deletions;
      primaryKind = diff.newFile ? "create" : "edit";
      continue;
    }
    if (name === "spawn_agent" || name === "fork_agent" || name === "send_message") {
      primaryKind = primaryKind === "unknown" ? "agent" : primaryKind;
      agentCount++;
      continue;
    }
    unknownTools.add(name);
  }

  const singleChangedFile = editedFiles.size + createdFiles.size === 1 && items.length === 1;
  if (singleChangedFile) {
    const created = createdFiles.size === 1;
    const filePath = firstSetValue(created ? createdFiles : editedFiles);
    return {
      kind: created ? "create" : "edit",
      text: failed ? "编辑失败" : created ? (running ? "正在创建" : "已创建") : running ? "正在编辑" : "已编辑",
      fileName: filePath ? fileBaseName(filePath) : undefined,
      additions,
      deletions,
      running,
      failed
    };
  }

  const parts: string[] = [];
  if (createdFiles.size > 0) {
    parts.push(`已创建 ${createdFiles.size} 个文件`);
  }
  if (editedFiles.size > 0) {
    parts.push(`已编辑 ${editedFiles.size} 个文件`);
  }
  if (readFiles.size > 0) {
    parts.push(`已探索 ${readFiles.size} 个文件`);
  }
  if (searchedFiles.size > 0) {
    parts.push(`已搜索 ${searchedFiles.size} 个文件`);
  }
  if (searchCount > 0) {
    parts.push(`${searchCount} 次搜索`);
  }
  if (listCount > 0) {
    parts.push(`${listCount} 次列表`);
  }
  if (commandCount > 0) {
    parts.push(`已运行 ${commandCount} 条命令`);
  }
  if (agentCount > 0) {
    parts.push(`已启动 ${agentCount} 个子任务`);
  }
  if (parts.length === 0 && unknownTools.size > 0) {
    const names = Array.from(unknownTools).slice(0, 2).join("、");
    parts.push(`${running ? "正在调用" : "已调用"} ${names}`);
  }
  if (parts.length === 0) {
    parts.push(running ? "正在使用工具" : "已使用工具");
  }

  return {
    kind: primaryKind,
    text: `${failed ? "工具失败 · " : ""}${parts.join(" · ")}`,
    additions,
    deletions,
    running,
    failed
  };
}

function summarizeDiff(result: JsonRecord | undefined): DiffStats {
  const diff = recordValue(result, "diff");
  if (!diff) {
    return { additions: 0, deletions: 0, newFile: false };
  }
  const newFile = diff.new_file === true;
  if (newFile) {
    return { additions: numberValue(diff, "lines") ?? 0, deletions: 0, newFile };
  }

  let additions = 0;
  let deletions = 0;
  for (const hunk of arrayValue(diff, "hunks")) {
    if (!isRecord(hunk)) {
      continue;
    }
    for (const line of arrayValue(hunk, "lines")) {
      if (!isRecord(line)) {
        continue;
      }
      if (line.op === "insert") {
        additions++;
      } else if (line.op === "delete") {
        deletions++;
      }
    }
  }
  return { additions, deletions, newFile };
}

function collectResultFiles(result: JsonRecord | undefined, output: Set<string>): void {
  for (const file of arrayValue(result, "files")) {
    if (typeof file === "string" && file.trim()) {
      output.add(file.trim());
    }
  }
  for (const match of arrayValue(result, "matches")) {
    if (isRecord(match)) {
      addPath(output, stringValue(match, "file"));
    }
  }
  for (const count of arrayValue(result, "counts")) {
    if (isRecord(count)) {
      addPath(output, stringValue(count, "file"));
    }
  }
}

function parseJSONRecord(value: string | undefined): JsonRecord | undefined {
  if (!value?.trim()) {
    return undefined;
  }
  try {
    const parsed: unknown = JSON.parse(value);
    return isRecord(parsed) ? parsed : undefined;
  } catch {
    return undefined;
  }
}

function isRecord(value: unknown): value is JsonRecord {
  return Boolean(value && typeof value === "object" && !Array.isArray(value));
}

function recordValue(record: JsonRecord | undefined, key: string): JsonRecord | undefined {
  if (!record) {
    return undefined;
  }
  const value = record[key];
  return isRecord(value) ? value : undefined;
}

function arrayValue(record: JsonRecord | undefined, key: string): unknown[] {
  if (!record) {
    return [];
  }
  const value = record[key];
  return Array.isArray(value) ? value : [];
}

function stringValue(record: JsonRecord | undefined, key: string): string | undefined {
  if (!record) {
    return undefined;
  }
  const value = record[key];
  return typeof value === "string" && value.trim() ? value.trim() : undefined;
}

function numberValue(record: JsonRecord | undefined, key: string): number | undefined {
  if (!record) {
    return undefined;
  }
  const value = record[key];
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function addPath(output: Set<string>, path: string | undefined): void {
  if (path?.trim()) {
    output.add(path.trim());
  }
}

function firstSetValue(values: Set<string>): string | undefined {
  for (const value of values) {
    return value;
  }
  return undefined;
}

function fileBaseName(path: string): string {
  const normalized = path.replace(/\\/g, "/");
  const parts = normalized.split("/").filter(Boolean);
  return parts[parts.length - 1] ?? path;
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

function RuntimeLoading({
  status,
  pinned = false,
  onExitPreview
}: {
  status: string;
  pinned?: boolean;
  onExitPreview?: () => void;
}): JSX.Element {
  const isStarting = pinned || status === "connecting" || status === "opening";
  return (
    <div className="project-empty-pane">
      {isStarting ? (
        <div className="wuu-launch" role="status" aria-label={pinned ? "wuu 启动动画预览" : "wuu 正在启动"}>
          <div className="wuu-launch-mark" aria-hidden="true">
            <span>w</span>
            <span>u</span>
            <span>u</span>
          </div>
          <div className="wuu-launch-rail" aria-hidden="true" />
          {pinned && onExitPreview ? (
            <button className="wuu-launch-exit" type="button" onClick={onExitPreview}>
              退出预览
            </button>
          ) : null}
        </div>
      ) : (
        <div className="project-empty-content">
          <h2>{status}</h2>
        </div>
      )}
    </div>
  );
}

function SettingsView({
  initialized,
  running,
  onBack,
  onSave
}: {
  initialized?: InitializeResult;
  running: boolean;
  onBack: () => void;
  onSave: (provider: string, model: string) => Promise<void>;
}): JSX.Element {
  const providers = initialized?.providers ?? [];
  const [providerDraft, setProviderDraft] = useState(initialized?.provider ?? "");
  const [modelDraft, setModelDraft] = useState(initialized?.model ?? "");
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    setProviderDraft(initialized?.provider ?? "");
    setModelDraft(initialized?.model ?? "");
    setError("");
    setSaved(false);
  }, [initialized?.provider, initialized?.model]);

  function changeProvider(provider: string): void {
    setProviderDraft(provider);
    setSaved(false);
    const summary = providers.find((item) => item.name === provider);
    if (summary) {
      setModelDraft(summary.model);
    }
  }

  async function submit(event: ReactFormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    setError("");
    setSaved(false);
    try {
      await onSave(providerDraft, modelDraft);
      setSaved(true);
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : "保存失败");
    }
  }

  const disabled =
    running ||
    !providerDraft.trim() ||
    !modelDraft.trim() ||
    (providerDraft === initialized?.provider && modelDraft === initialized?.model);

  return (
    <div className="settings-shell">
      <aside className="settings-sidebar">
        <div className="traffic-spacer" />
        <button className="settings-back-button" type="button" onClick={onBack}>
          <ArrowLeft size={17} />
          <span>返回应用</span>
        </button>
        <nav className="settings-nav" aria-label="设置">
          <button className="settings-nav-item active" type="button">
            <Settings size={18} />
            <span>常规</span>
          </button>
        </nav>
      </aside>
      <OverlayScrollbarsComponent
        element="main"
        className="settings-main"
        data-overlayscrollbars-initialize
        defer
        options={OVERLAY_SCROLLBAR_OPTIONS}
      >
        <div className="settings-page">
          <h1>常规</h1>

          <section className="settings-section">
            <div>
              <h2>模型</h2>
              <p>选择 wuu 使用的 Provider 和模型。</p>
            </div>
            <form className="settings-card" onSubmit={submit}>
              <label className="settings-row">
                <span>
                  <strong>Provider</strong>
                  <small>选择当前会话运行时使用的模型服务</small>
                </span>
                {providers.length > 0 ? (
                  <select value={providerDraft} onChange={(event) => changeProvider(event.target.value)} disabled={running}>
                    {providers.map((provider) => (
                      <option key={provider.name} value={provider.name}>
                        {provider.name}
                      </option>
                    ))}
                  </select>
                ) : (
                  <input
                    value={providerDraft}
                    onChange={(event) => {
                      setProviderDraft(event.target.value);
                      setSaved(false);
                    }}
                    disabled={running}
                  />
                )}
              </label>
              <label className="settings-row">
                <span>
                  <strong>模型</strong>
                  <small>Provider 配置里的模型名称</small>
                </span>
                <input
                  value={modelDraft}
                  onChange={(event) => {
                    setModelDraft(event.target.value);
                    setSaved(false);
                  }}
                  disabled={running}
                />
              </label>
              <div className="settings-card-footer">
                {error ? <div className="settings-error">{error}</div> : null}
                {saved ? <div className="settings-saved">已保存</div> : null}
                <button type="submit" disabled={disabled}>
                  保存
                </button>
              </div>
            </form>
          </section>
        </div>
      </OverlayScrollbarsComponent>
    </div>
  );
}

function Composer({
  variant = "dock",
  prompt,
  setPrompt,
  running,
  status,
  initialized,
  gitStatus,
  projects,
  activeContext,
  activeProject,
  codexModels,
  codexRuntimeMenu,
  codexRuntimeRef,
  menuOpen,
  accessMenuOpen,
  modeMenuOpen,
  branchMenuOpen,
  menuRef,
  accessMenuRef,
  projectFilter,
  setProjectFilter,
  onToggleMenu,
  onToggleAccessMenu,
  onToggleCodexRuntimeMenu,
  onSelectCodexModel,
  onSelectCodexEffort,
  onToggleModeMenu,
  onToggleBranchMenu,
  onOpenSettings,
  onSelectProject,
  onSelectNoProject,
  onSelectGitBranch,
  onCreateProject,
  onOpenProject,
  onSend,
  onInterrupt
}: {
  variant?: ComposerVariant;
  prompt: string;
  setPrompt: (value: string) => void;
  running: boolean;
  status: string;
  initialized?: InitializeResult;
  gitStatus?: GitStatusResult;
  projects: DesktopProject[];
  activeContext?: RuntimeContext;
  activeProject?: DesktopProject;
  codexModels: CodexModelLoadState;
  codexRuntimeMenu: CodexRuntimeMenu;
  codexRuntimeRef: RefObject<HTMLDivElement>;
  menuOpen: boolean;
  accessMenuOpen: boolean;
  modeMenuOpen: boolean;
  branchMenuOpen: boolean;
  menuRef: RefObject<HTMLDivElement>;
  accessMenuRef: RefObject<HTMLDivElement>;
  projectFilter: string;
  setProjectFilter: (value: string) => void;
  onToggleMenu: () => void;
  onToggleAccessMenu: () => void;
  onToggleCodexRuntimeMenu: (menu: Exclude<CodexRuntimeMenu, null>) => void;
  onSelectCodexModel: (model: CodexModelSummary) => void;
  onSelectCodexEffort: (effort: string) => void;
  onToggleModeMenu: () => void;
  onToggleBranchMenu: () => void;
  onOpenSettings: () => void;
  onSelectProject: (id: string) => void;
  onSelectNoProject: () => void;
  onSelectGitBranch: (branch: string) => void;
  onCreateProject: () => void;
  onOpenProject: () => void;
  onSend: () => void;
  onInterrupt: () => void;
}): JSX.Element {
  const contextLabel = activeContext?.kind === "project" ? activeProject?.name ?? "项目" : "不使用项目";
  const statusText = status === "ready" ? "" : status;
  const className = `composer-wrap ${variant === "hero" ? "hero-composer-wrap" : "dock-composer-wrap"}`;
  const codexProvider = initialized ? isCodexProvider(initialized) : false;
  const content = (
    <>
      <div className="composer-shell">
        <div className="composer">
          <textarea
            value={prompt}
            placeholder="尽管问"
            onChange={(event) => setPrompt(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.shiftKey) {
                event.preventDefault();
                onSend();
              }
            }}
          />
          <div className="composer-bar">
            <button className="composer-tool-button" type="button" aria-label="打开项目" onClick={onOpenProject}>
              <Plus size={20} />
            </button>
            <div className="permission-menu-anchor" ref={accessMenuRef}>
              <button
                className="permission-chip"
                type="button"
                aria-haspopup="menu"
                aria-expanded={accessMenuOpen}
                onClick={onToggleAccessMenu}
              >
                <ShieldCheck size={16} />
                <span>完全访问权限</span>
                <ChevronDown size={15} />
              </button>
              {accessMenuOpen ? <AccessMenu /> : null}
            </div>
            <div className="composer-spacer" />
            {codexProvider && initialized ? (
              <CodexRuntimePicker
                initialized={initialized}
                state={codexModels}
                openMenu={codexRuntimeMenu}
                anchorRef={codexRuntimeRef}
                running={running}
                onToggleMenu={onToggleCodexRuntimeMenu}
                onSelectModel={onSelectCodexModel}
                onSelectEffort={onSelectCodexEffort}
              />
            ) : (
              <>
                <button className="provider-pill" type="button" onClick={onOpenSettings}>
                  {initialized?.provider ?? "provider"}
                </button>
                <button className="model-label" type="button" onClick={onOpenSettings}>
                  {initialized?.model ?? "model"}
                </button>
              </>
            )}
            {statusText ? <span className="status-label">{statusText}</span> : null}
            <button className="send-button" onClick={running ? onInterrupt : onSend} aria-label={running ? "停止" : "发送"}>
              {running ? <Square size={18} /> : <Send size={18} />}
            </button>
          </div>
        </div>
        <div className="composer-context-bar" ref={menuRef}>
          <button className="context-project-button" onClick={onToggleMenu} aria-haspopup="menu" aria-expanded={menuOpen}>
            {activeContext?.kind === "project" ? <Folder size={18} /> : <FolderX size={18} />}
            <span>{contextLabel}</span>
            <ChevronDown size={16} />
          </button>
          <button
            className="context-mode-chip"
            type="button"
            aria-haspopup="menu"
            aria-expanded={modeMenuOpen}
            onClick={onToggleModeMenu}
          >
            <Laptop size={17} />
            <span>本地模式</span>
            <ChevronDown size={15} />
          </button>
          {gitStatus?.is_repo && gitStatus.branch ? (
            <button
              className="context-branch-chip"
              type="button"
              aria-haspopup="menu"
              aria-expanded={branchMenuOpen}
              onClick={onToggleBranchMenu}
            >
              <GitBranch size={17} />
              <span>{gitStatus.branch}</span>
              {gitStatus.dirty_count > 0 ? <small>未提交：{gitStatus.dirty_count} 个文件</small> : null}
              <ChevronDown size={15} />
            </button>
          ) : null}
          {modeMenuOpen ? (
            <ModeMenu
              activeContext={activeContext}
              onSelectNoProject={onSelectNoProject}
              onOpenProject={onOpenProject}
            />
          ) : null}
          {branchMenuOpen && gitStatus?.is_repo ? (
            <BranchMenu gitStatus={gitStatus} onSelectBranch={onSelectGitBranch} />
          ) : null}
          {menuOpen ? (
            <ProjectPickerMenu
              projects={projects}
              activeContext={activeContext}
              query={projectFilter}
              setQuery={setProjectFilter}
              onSelectProject={onSelectProject}
              onSelectNoProject={onSelectNoProject}
              onCreateProject={onCreateProject}
              onOpenProject={onOpenProject}
            />
          ) : null}
        </div>
      </div>
    </>
  );
  return variant === "hero" ? <div className={className}>{content}</div> : <footer className={className}>{content}</footer>;
}

function CodexRuntimePicker({
  initialized,
  state,
  openMenu,
  anchorRef,
  running,
  onToggleMenu,
  onSelectModel,
  onSelectEffort
}: {
  initialized: InitializeResult;
  state: CodexModelLoadState;
  openMenu: CodexRuntimeMenu;
  anchorRef: RefObject<HTMLDivElement>;
  running: boolean;
  onToggleMenu: (menu: Exclude<CodexRuntimeMenu, null>) => void;
  onSelectModel: (model: CodexModelSummary) => void;
  onSelectEffort: (effort: string) => void;
}): JSX.Element {
  const currentModel = state.models.find((model) => model.slug === initialized.model);
  const effort = initialized.effort ?? "";
  const effortOptions = codexEffortOptions(currentModel, effort);
  return (
    <div className="codex-runtime-anchor" ref={anchorRef}>
      <button
        className="codex-runtime-trigger"
        type="button"
        disabled={running}
        aria-haspopup="menu"
        aria-expanded={openMenu !== null}
        onClick={() => onToggleMenu("main")}
      >
        <span>{shortCodexModelLabel(initialized.model)}</span>
        <span className="codex-runtime-effort">{codexEffortLabel(effort)}</span>
        <ChevronDown size={15} />
      </button>
      {openMenu === "main" ? (
        <CodexMainMenu
          selectedEffort={effort}
          options={effortOptions}
          currentModel={currentModel}
          fallbackModel={initialized.model}
          onSelectEffort={onSelectEffort}
          onOpenModelMenu={() => onToggleMenu("model")}
        />
      ) : null}
      {openMenu === "model" ? (
        <CodexModelMenu
          state={state}
          selectedModel={initialized.model}
          onSelectModel={onSelectModel}
        />
      ) : null}
    </div>
  );
}

function CodexMainMenu({
  selectedEffort,
  options,
  currentModel,
  fallbackModel,
  onSelectEffort,
  onOpenModelMenu
}: {
  selectedEffort: string;
  options: string[];
  currentModel?: CodexModelSummary;
  fallbackModel: string;
  onSelectEffort: (effort: string) => void;
  onOpenModelMenu: () => void;
}): JSX.Element {
  return (
    <div className="codex-runtime-menu codex-main-menu" role="menu">
      {options.map((effort) => {
        const selected = effort === selectedEffort;
        return (
          <button key={effort || "auto"} role="menuitem" type="button" onClick={() => onSelectEffort(effort)}>
            <span>{codexEffortLabel(effort)}</span>
            {selected ? <Check size={18} /> : null}
          </button>
        );
      })}
      <div className="codex-menu-separator" />
      <button role="menuitem" type="button" onClick={onOpenModelMenu}>
        <span>{currentModel ? displayCodexModelName(currentModel) : fallbackModel}</span>
        <ChevronRight className="codex-menu-chevron" size={18} />
      </button>
    </div>
  );
}

function CodexModelMenu({
  state,
  selectedModel,
  onSelectModel
}: {
  state: CodexModelLoadState;
  selectedModel: string;
  onSelectModel: (model: CodexModelSummary) => void;
}): JSX.Element {
  return (
    <div className="codex-runtime-menu codex-model-menu" role="menu">
      <div className="codex-menu-label">模型</div>
      {state.loading ? <div className="composer-menu-empty">正在加载 Codex 模型</div> : null}
      {state.error ? (
        <div className="composer-menu-note warning">
          <strong>无法读取 Codex 登录态</strong>
          <span>{state.error}</span>
        </div>
      ) : null}
      {!state.loading && !state.error && state.models.length === 0 ? (
        <div className="composer-menu-empty">没有可用模型</div>
      ) : null}
      {state.models.map((model) => {
        const selected = model.slug === selectedModel;
        return (
          <button key={model.slug} role="menuitem" type="button" onClick={() => onSelectModel(model)}>
            <span>{displayCodexModelName(model)}</span>
            {selected ? <Check size={18} /> : null}
          </button>
        );
      })}
    </div>
  );
}

function AccessMenu(): JSX.Element {
  return (
    <div className="composer-context-menu access-menu" role="menu">
      <div className="composer-menu-note">
        <strong>完全访问权限</strong>
        <span>wuu 可以读取并修改当前工作区文件，适合直接做开发任务。</span>
      </div>
    </div>
  );
}

function ModeMenu({
  activeContext,
  onSelectNoProject,
  onOpenProject
}: {
  activeContext?: RuntimeContext;
  onSelectNoProject: () => void;
  onOpenProject: () => void;
}): JSX.Element {
  return (
    <div className="composer-context-menu mode-menu" role="menu">
      <button role="menuitem" type="button" onClick={onOpenProject}>
        <FolderOpen size={18} />
        <span>打开本地项目</span>
        {activeContext?.kind === "project" ? <Check size={17} /> : null}
      </button>
      <button role="menuitem" type="button" onClick={onSelectNoProject}>
        <FolderX size={18} />
        <span>不使用项目</span>
        {activeContext?.kind === "no_project" ? <Check size={17} /> : null}
      </button>
    </div>
  );
}

function BranchMenu({
  gitStatus,
  onSelectBranch
}: {
  gitStatus: GitStatusResult;
  onSelectBranch: (branch: string) => void;
}): JSX.Element {
  const branches = gitStatus.branches ?? [];
  return (
    <div className="composer-context-menu branch-menu" role="menu">
      {gitStatus.dirty_count > 0 ? (
        <div className="composer-menu-note warning">
          <strong>有未提交更改</strong>
          <span>{gitStatus.dirty_count} 个文件会随分支切换保留；如果会覆盖本地改动，Git 会拒绝切换。</span>
        </div>
      ) : null}
      {branches.length === 0 ? <div className="composer-menu-empty">没有本地分支</div> : null}
      {branches.map((branch) => {
        const selected = branch === gitStatus.branch;
        return (
          <button
            key={branch}
            role="menuitem"
            type="button"
            disabled={selected}
            onClick={() => onSelectBranch(branch)}
          >
            <GitBranch size={18} />
            <span>{branch}</span>
            {selected ? <Check size={17} /> : null}
          </button>
        );
      })}
    </div>
  );
}

function ProjectPickerMenu({
  projects,
  activeContext,
  query,
  setQuery,
  onSelectProject,
  onSelectNoProject,
  onCreateProject,
  onOpenProject
}: {
  projects: DesktopProject[];
  activeContext?: RuntimeContext;
  query: string;
  setQuery: (value: string) => void;
  onSelectProject: (id: string) => void;
  onSelectNoProject: () => void;
  onCreateProject: () => void;
  onOpenProject: () => void;
}): JSX.Element {
  const normalizedQuery = query.trim().toLocaleLowerCase();
  const filteredProjects = normalizedQuery
    ? projects.filter((project) => project.name.toLocaleLowerCase().includes(normalizedQuery) || project.path.toLocaleLowerCase().includes(normalizedQuery))
    : projects;

  return (
    <div className="composer-project-menu" role="menu">
      <label className="project-search">
        <Search size={18} />
        <input value={query} placeholder="搜索项目" onChange={(event) => setQuery(event.target.value)} />
      </label>
      <OverlayScrollbarsComponent
        className="project-picker-list"
        data-overlayscrollbars-initialize
        defer
        options={OVERLAY_SCROLLBAR_OPTIONS}
      >
        {filteredProjects.length === 0 ? <div className="project-picker-empty">没有匹配项目</div> : null}
        {filteredProjects.map((project) => {
          const selected = activeContext?.kind === "project" && activeContext.project_id === project.id;
          return (
            <button key={project.id} role="menuitem" onClick={() => onSelectProject(project.id)}>
              <Folder size={19} />
              <span>{project.name}</span>
              {selected ? <Check size={18} /> : null}
            </button>
          );
        })}
      </OverlayScrollbarsComponent>
      <div className="project-picker-divider" />
      <button role="menuitem" onClick={onOpenProject}>
        <FolderOpen size={19} />
        <span>使用现有文件夹</span>
      </button>
      <button role="menuitem" onClick={onCreateProject}>
        <FolderPlus size={19} />
        <span>新建空白项目</span>
      </button>
      <button role="menuitem" onClick={onSelectNoProject}>
        <FolderX size={19} />
        <span>不使用项目</span>
        {activeContext?.kind === "no_project" ? <Check size={18} /> : null}
      </button>
    </div>
  );
}

function AskUserDialog({ request }: { request: AskRequestState }): JSX.Element {
  const [answers, setAnswers] = useState<Record<string, string[]>>(() => {
    const initial: Record<string, string[]> = {};
    for (const question of request.questions) {
      initial[question.question] = question.options[0] ? [question.options[0].label] : [];
    }
    return initial;
  });
  const flatAnswers = useMemo(() => {
    const output: Record<string, string> = {};
    for (const question of request.questions) {
      output[question.question] = (answers[question.question] ?? []).join(", ");
    }
    return output;
  }, [answers, request.questions]);

  function select(question: AskUserQuestion, label: string): void {
    setAnswers((current) => {
      const existing = current[question.question] ?? [];
      if (!question.multi_select) {
        return { ...current, [question.question]: [label] };
      }
      const next = existing.includes(label) ? existing.filter((item) => item !== label) : [...existing, label];
      return { ...current, [question.question]: next };
    });
  }

  return (
    <div className="modal-backdrop">
      <div className="ask-dialog">
        <div className="ask-header">
          <h2>需要确认</h2>
          <button
            className="icon-button"
            onClick={() => {
              void window.wuu.respondToServerRequest(request.id, { answers: {}, cancelled: true });
            }}
          >
            <X size={18} />
          </button>
        </div>
        <OverlayScrollbarsComponent
          className="ask-body"
          data-overlayscrollbars-initialize
          defer
          options={OVERLAY_SCROLLBAR_OPTIONS}
        >
          {request.questions.map((question) => (
            <section key={question.question} className="ask-question">
              <div className="ask-chip">{question.header}</div>
              <h3>{question.question}</h3>
              <div className="ask-options">
                {question.options.map((option) => {
                  const selected = (answers[question.question] ?? []).includes(option.label);
                  return (
                    <button
                      key={option.label}
                      className={`ask-option ${selected ? "selected" : ""}`}
                      onClick={() => select(question, option.label)}
                    >
                      <strong>{option.label}</strong>
                      <span>{option.description}</span>
                    </button>
                  );
                })}
              </div>
            </section>
          ))}
        </OverlayScrollbarsComponent>
        <div className="ask-footer">
          <button
            className="secondary-button"
            onClick={() => {
              void window.wuu.respondToServerRequest(request.id, { answers: {}, cancelled: true });
            }}
          >
            取消
          </button>
          <button
            className="primary-button"
            onClick={() => {
              void window.wuu.respondToServerRequest(request.id, { answers: flatAnswers });
            }}
          >
            提交
          </button>
        </div>
      </div>
    </div>
  );
}
