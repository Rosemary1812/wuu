import {
  Bot,
  Brain,
  ChevronDown,
  Clock3,
  Folder,
  Info,
  MessageSquarePlus,
  Package,
  PanelLeftOpen,
  Plus,
  Search,
  Send,
  Settings,
  Square,
  Wrench,
  X
} from "lucide-react";
import {
  type CSSProperties,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
  useEffect,
  useMemo,
  useRef,
  useState
} from "react";
import type {
  AppServerNotification,
  AskUserQuestion,
  InitializeResult,
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
  thread?: Thread;
  threads: Thread[];
  running: boolean;
  status: string;
  askRequest?: AskRequestState;
};

const initialState: AppState = {
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
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const resizeSessionRef = useRef<SidebarResizeSession | null>(null);

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
        const initialized = await window.wuu.initialize();
        const listed = await window.wuu.listThreads();
        const thread =
          listed.threads.length > 0
            ? (await window.wuu.resumeThread(listed.threads[0].id)).thread
            : (await window.wuu.startThread()).thread;
        if (!mounted) {
          return;
        }
        setState((current) => ({
          ...current,
          initialized,
          thread,
          threads: upsertThread(listed.threads, thread),
          status: "ready"
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

  async function startNewThread(): Promise<void> {
    const result = await window.wuu.startThread();
    setState((current) => ({
      ...current,
      thread: result.thread,
      threads: upsertThread(current.threads, result.thread),
      running: false,
      status: "ready"
    }));
  }

  async function selectThread(threadId: string): Promise<void> {
    if (threadId === state.thread?.id || state.running) {
      return;
    }
    setState((current) => ({ ...current, status: "loading" }));
    try {
      const result = await window.wuu.resumeThread(threadId);
      setState((current) => ({
        ...current,
        thread: result.thread,
        threads: upsertThread(current.threads, result.thread),
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
    if (!text || !state.thread || state.running) {
      return;
    }
    setPrompt("");
    setState((current) => ({ ...current, running: true, status: "thinking" }));
    try {
      const result = await window.wuu.startTurn(state.thread.id, text);
      setState((current) => updateThread(current, (thread) => upsertTurn(thread, result.turn)));
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
          <button className="nav-item" onClick={() => void startNewThread()}>
            <MessageSquarePlus size={18} />
            <span>新对话</span>
          </button>
          <button className="nav-item">
            <Search size={18} />
            <span>搜索</span>
          </button>
          <button className="nav-item">
            <Package size={18} />
            <span>插件</span>
          </button>
          <button className="nav-item">
            <Clock3 size={18} />
            <span>自动化</span>
          </button>
        </nav>

        <section className="project-list" aria-label="项目">
          <div className="section-label">项目</div>
          <ProjectRow name={workspaceName(state.initialized?.workspace_root)} active />
          <ThreadList threads={state.threads} activeID={state.thread?.id} onSelect={(id) => void selectThread(id)} />
        </section>

        <div className="sidebar-footer">
          <button className="nav-item">
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
            <h1>{activeTitle}</h1>
            <button className="icon-button" aria-label="更多">
              <ChevronDown size={17} />
            </button>
          </div>
          <div className="title-actions">
            <span className="runtime-pill">{state.initialized?.provider ?? "runtime"}</span>
            <button className="icon-button" aria-label="信息">
              <Info size={18} />
            </button>
          </div>
        </header>

        <div className="scroll-region" ref={scrollRef}>
          <div className="conversation-width">
            {turns.length === 0 ? <EmptyThread /> : turns.map((turn) => <TurnView key={turn.id} turn={turn} />)}
          </div>
        </div>

        <Composer
          prompt={prompt}
          setPrompt={setPrompt}
          running={state.running}
          status={state.status}
          model={state.initialized?.model}
          onSend={() => void sendPrompt()}
          onInterrupt={() => void interrupt()}
        />
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

function upsertThread(threads: Thread[], thread: Thread): Thread[] {
  const index = threads.findIndex((item) => item.id === thread.id);
  if (index < 0) {
    return [thread, ...threads];
  }
  const next = threads.slice();
  next[index] = thread;
  return next;
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

function ThreadList({
  threads,
  activeID,
  onSelect
}: {
  threads: Thread[];
  activeID?: string;
  onSelect: (id: string) => void;
}): JSX.Element {
  return (
    <div className="thread-list">
      {threads.slice(0, 8).map((thread, index) => (
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

function ProjectRow({ name, active }: { name: string; active?: boolean }): JSX.Element {
  return (
    <button className={`project-row ${active ? "active" : ""}`}>
      <Folder size={18} />
      <span>{name}</span>
    </button>
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
      <Bot size={24} />
      <span>wuu</span>
    </div>
  );
}

function Composer({
  prompt,
  setPrompt,
  running,
  status,
  model,
  onSend,
  onInterrupt
}: {
  prompt: string;
  setPrompt: (value: string) => void;
  running: boolean;
  status: string;
  model?: string;
  onSend: () => void;
  onInterrupt: () => void;
}): JSX.Element {
  return (
    <footer className="composer-wrap">
      <div className="composer">
        <textarea
          value={prompt}
          placeholder="要求后续变更"
          onChange={(event) => setPrompt(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter" && !event.shiftKey) {
              event.preventDefault();
              onSend();
            }
          }}
        />
        <div className="composer-bar">
          <button className="icon-button" aria-label="添加">
            <Plus size={20} />
          </button>
          <span className="permission-pill">完全访问权限</span>
          <div className="composer-spacer" />
          <span className="model-pill">{model ?? "model"}</span>
          <span className="status-label">{status}</span>
          <button className="send-button" onClick={running ? onInterrupt : onSend} aria-label={running ? "停止" : "发送"}>
            {running ? <Square size={18} /> : <Send size={18} />}
          </button>
        </div>
      </div>
    </footer>
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

function workspaceName(path?: string): string {
  if (!path) {
    return "wuu";
  }
  return path.split(/[\\/]/).filter(Boolean).at(-1) ?? "wuu";
}
