import {
  Bot,
  Brain,
  Check,
  ChevronDown,
  Folder,
  FolderX,
  FolderOpen,
  FolderPlus,
  MessageSquarePlus,
  PanelLeftOpen,
  Search,
  Send,
  Square,
  Wrench,
  X
} from "lucide-react";
import {
  type CSSProperties,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
  type RefObject,
  useEffect,
  useMemo,
  useRef,
  useState
} from "react";
import type {
  AppServerNotification,
  AskUserQuestion,
  DesktopProject,
  InitializeResult,
  ProjectListResult,
  RuntimeContext,
  ServerEvent,
  Thread,
  ThreadItem,
  Turn
} from "../shared/protocol";

type AskRequestState = {
  id: string;
  questions: AskUserQuestion[];
};

type AppState = {
  initialized?: InitializeResult;
  projects: DesktopProject[];
  activeContext?: RuntimeContext;
  activeProjectId?: string;
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

type SidebarResizeSession = {
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

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

export function App(): JSX.Element {
  const [state, setState] = useState<AppState>(initialState);
  const [prompt, setPrompt] = useState("");
  const [sidebarWidth, setSidebarWidth] = useState(initialSidebarWidth);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(initialSidebarCollapsed);
  const [resizingSidebar, setResizingSidebar] = useState(false);
  const [projectMenuOpen, setProjectMenuOpen] = useState(false);
  const [runtimeMenuOpen, setRuntimeMenuOpen] = useState(false);
  const [projectFilter, setProjectFilter] = useState("");
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const resizeSessionRef = useRef<SidebarResizeSession | null>(null);
  const projectMenuRef = useRef<HTMLDivElement>(null);
  const runtimeMenuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    let mounted = true;
    const off = window.wuu.onServerEvent((event) => {
      if (!mounted) {
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
      if (runtimeMenuOpen && !runtimeMenuRef.current?.contains(target)) {
        setRuntimeMenuOpen(false);
      }
    }

    window.addEventListener("pointerdown", handlePointerDown);
    return () => window.removeEventListener("pointerdown", handlePointerDown);
  }, [projectMenuOpen, runtimeMenuOpen]);

  useEffect(() => {
    const node = scrollRef.current;
    if (!node) {
      return;
    }
    node.scrollTop = node.scrollHeight;
  }, [state.thread?.turns]);

  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent): void {
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
  }, [state.thread?.id, state.threads, state.running]);

  useEffect(() => {
    window.localStorage.setItem(SIDEBAR_WIDTH_KEY, String(sidebarWidth));
    window.localStorage.setItem(SIDEBAR_COLLAPSED_KEY, String(sidebarCollapsed));
  }, [sidebarWidth, sidebarCollapsed]);

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
  const activeTitle = state.thread?.preview || "新对话";
  const turns = state.thread?.turns ?? [];
  const shellClassName = `app-shell${sidebarCollapsed ? " sidebar-collapsed" : ""}${
    resizingSidebar ? " resizing-sidebar" : ""
  }`;
  const shellStyle = {
    "--sidebar-width": `${sidebarCollapsed ? 0 : sidebarWidth}px`
  } as CSSProperties;

  function applySidebarWidth(nextWidth: number): void {
    if (nextWidth <= SIDEBAR_MIN_WIDTH) {
      setSidebarCollapsed(true);
      return;
    }
    setSidebarCollapsed(false);
    setSidebarWidth(clamp(nextWidth, SIDEBAR_MIN_WIDTH, SIDEBAR_MAX_WIDTH));
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

  async function loadRuntime(projectState: ProjectListResult): Promise<Partial<AppState>> {
    if (!projectState.active_context) {
      return emptyRuntimeState(projectState);
    }
    const initialized = await window.wuu.initialize();
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
      thread: undefined,
      threads: [],
      running: false,
      status: "no-runtime"
    };
  }

  function closeProjectMenus(): void {
    setProjectMenuOpen(false);
    setRuntimeMenuOpen(false);
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

  async function interrupt(): Promise<void> {
    if (!state.thread) {
      return;
    }
    await window.wuu.interruptTurn(state.thread.id);
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
            <h1>{activeTitle}</h1>
          </div>
          <div className="title-actions">
            {state.initialized ? <span className="runtime-pill">{state.initialized.provider}</span> : null}
          </div>
        </header>

        {state.initialized ? (
          <div className="scroll-region" ref={scrollRef}>
            <div className="conversation-width">
              {turns.length === 0 ? <EmptyThread /> : turns.map((turn) => <TurnView key={turn.id} turn={turn} />)}
            </div>
          </div>
        ) : (
          <RuntimeLoading status={state.status} />
        )}

        {state.initialized ? (
          <Composer
            prompt={prompt}
            setPrompt={setPrompt}
            running={state.running}
            status={state.status}
            model={state.initialized?.model}
            projects={state.projects}
            activeContext={state.activeContext}
            activeProject={activeProject}
            menuOpen={runtimeMenuOpen}
            menuRef={runtimeMenuRef}
            projectFilter={projectFilter}
            setProjectFilter={setProjectFilter}
            onToggleMenu={() => setRuntimeMenuOpen((open) => !open)}
            onSelectProject={(id) => void openProject(id)}
            onSelectNoProject={() => void useNoProject(false)}
            onCreateProject={() => void createBlankProject()}
            onOpenProject={() => void chooseProjectFolder()}
            onSend={() => void sendPrompt()}
            onInterrupt={() => void interrupt()}
          />
        ) : null}
      </main>

      {state.askRequest ? <AskUserDialog request={state.askRequest} /> : null}
    </div>
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

function TurnView({ turn }: { turn: Turn }): JSX.Element {
  return (
    <section className="turn">
      {turn.items.map((item) => (
        <ThreadItemView key={item.id} item={item} />
      ))}
      {turn.error ? <div className="turn-error">{turn.error.message}</div> : null}
    </section>
  );
}

function ThreadItemView({ item }: { item: ThreadItem }): JSX.Element | null {
  switch (item.type) {
    case "user_message":
      return <div className="message user-message">{item.text}</div>;
    case "agent_message":
      return (
        <article className="agent-block">
          <Bot size={18} />
          <div className="agent-text">{item.text}</div>
        </article>
      );
    case "reasoning":
      return (
        <article className="reasoning-block">
          <Brain size={16} />
          <div>{item.text}</div>
        </article>
      );
    case "tool_call":
      return (
        <article className="tool-card">
          <div className="tool-header">
            <Wrench size={16} />
            <span>{item.name || "tool"}</span>
            <span className={`status-dot ${item.status ?? "in_progress"}`} />
          </div>
          {item.arguments ? <pre>{item.arguments}</pre> : null}
          {item.result ? <pre className="tool-result">{item.result}</pre> : null}
        </article>
      );
    case "context_compaction":
      return <div className="system-line">{item.text}</div>;
    case "error":
      return <div className="turn-error">{item.error}</div>;
    default:
      return null;
  }
}

function EmptyThread(): JSX.Element {
  return (
    <div className="empty-thread">
      <h2>我们应该在 wuu 中构建什么?</h2>
    </div>
  );
}

function RuntimeLoading({ status }: { status: string }): JSX.Element {
  return (
    <div className="project-empty-pane">
      <div className="project-empty-content">
        <h2>{status === "connecting" || status === "opening" ? "正在启动 wuu" : status}</h2>
      </div>
    </div>
  );
}

function Composer({
  prompt,
  setPrompt,
  running,
  status,
  model,
  projects,
  activeContext,
  activeProject,
  menuOpen,
  menuRef,
  projectFilter,
  setProjectFilter,
  onToggleMenu,
  onSelectProject,
  onSelectNoProject,
  onCreateProject,
  onOpenProject,
  onSend,
  onInterrupt
}: {
  prompt: string;
  setPrompt: (value: string) => void;
  running: boolean;
  status: string;
  model?: string;
  projects: DesktopProject[];
  activeContext?: RuntimeContext;
  activeProject?: DesktopProject;
  menuOpen: boolean;
  menuRef: RefObject<HTMLDivElement>;
  projectFilter: string;
  setProjectFilter: (value: string) => void;
  onToggleMenu: () => void;
  onSelectProject: (id: string) => void;
  onSelectNoProject: () => void;
  onCreateProject: () => void;
  onOpenProject: () => void;
  onSend: () => void;
  onInterrupt: () => void;
}): JSX.Element {
  const contextLabel = activeContext?.kind === "project" ? activeProject?.name ?? "项目" : "不使用项目";
  const statusText = status === "ready" ? "" : status;
  return (
    <footer className="composer-wrap">
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
            <div className="composer-spacer" />
            <span className="model-pill">{model ?? "model"}</span>
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
    </footer>
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
      <div className="project-picker-list">
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
      </div>
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
        <div className="ask-body">
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
        </div>
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
