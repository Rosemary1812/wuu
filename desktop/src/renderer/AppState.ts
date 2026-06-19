import type {
  Agent,
  AppServerNotification,
  AppServerRequest,
  DesktopProject,
  GitStatusResult,
  InitializeResult,
  ManagedProcess,
  PendingToolApproval,
  PlanUpdate,
  RuntimeContext,
  ServerEvent,
  Thread,
  ThreadItem,
  Turn,
} from "../shared/protocol";
import type { ComposerFile, ComposerImage } from "./ComposerMessages";
import { threadDisplayTitle } from "./ThreadTitles";
import { sortChildAgents } from "./ThreadAgents";
import {
  mergeTurnItemsInOrder,
  orderedTurnItems,
  upsertTurnItemInOrder,
} from "./TurnOrdering";
import {
  isRecord,
  type JsonRecord,
  numberValue,
  recordValue,
  stringValue,
} from "./ToolActivity";
import {
  streamTextKey,
  streamTextStore,
  type StreamTextField,
} from "./StreamText";
import { statusMessageForError } from "./UserFacingErrors";

type ConversationPaneID = "primary" | "secondary";

type ComposerDraftState = {
  prompt: string;
  images: ComposerImage[];
  files: ComposerFile[];
};

function emptyComposerDraft(): ComposerDraftState {
  return { prompt: "", images: [], files: [] };
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
    files: draft.files.map((file) => ({ ...file })),
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
      files: ComposerFile[];
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
      files: ComposerFile[];
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
  pendingToolApproval?: PendingToolApproval;
  // turnTokenUsage tracks per-turn cumulative token counts
  // pushed by the appserver's "turn/usage" notification. The samples field
  // is a rolling window used to derive a smoothed tokens-per-second read
  // for the live token-speed gauge in the composer.
  turnTokenUsage: Record<string, TurnTokenUsage>;
  // lastViewedTurnByThreadID remembers the most recent turn that the user
  // has actually been on the thread for. It is the source of truth for the
  // sidebar / session-tab "has-unread" indicator — a thread is unread when
  // its latest completed turn ID is not in this map (or the entry is older).
  // Active-tab tracking is what advances this map; running threads are never
  // flagged unread because they have not finished yet.
  lastViewedTurnByThreadID: Record<string, string>;
};

type TurnTokenSample = {
  tokens: number;
  at: number;
};

type TokenSpeedSource = "real" | "estimated" | "none";

type TurnTokenUsage = {
  threadID: string;
  inputTokens: number;
  outputTokens: number;
  cacheCreationTokens: number;
  cacheReadTokens: number;
  speedTokens: number;
  speedSource: Exclude<TokenSpeedSource, "none">;
  samples: TurnTokenSample[];
};

type TurnTokenSpeedSnapshot = {
  tokensPerSecond: number;
  source: TokenSpeedSource;
  sampledAt?: number;
};

const TOKEN_SPEED_WINDOW_MS = 2000;

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
  turnTokenUsage: {},
  lastViewedTurnByThreadID: {},
};

function reduceServerEvent(state: AppState, event: ServerEvent): AppState {
  switch (event.kind) {
    case "notification":
      return reduceNotification(state, event.message);
    case "server-request": {
      if (event.message.method === "tool/approval/request") {
        const approval = toolApprovalFromServerRequest(event.message);
        if (approval) {
          return {
            ...state,
            pendingToolApproval: approval,
            status: "等待审批",
          };
        }
      }
      void window.wuu.rejectServerRequest(
        event.message.id,
        `unsupported server request: ${event.message.method}`,
      );
      return state;
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

function toolApprovalFromServerRequest(
  request: Required<AppServerRequest>,
): PendingToolApproval | undefined {
  const params = isRecord(request.params) ? request.params : undefined;
  if (!params) {
    void window.wuu.rejectServerRequest(request.id, "tool approval request params missing");
    return undefined;
  }
  const id = stringValue(params, "id");
  const toolName = stringValue(params, "tool_name");
  if (!id || !toolName) {
    void window.wuu.rejectServerRequest(request.id, "tool approval request is invalid");
    return undefined;
  }
  return {
    server_request_id: request.id,
    id,
    tool_name: toolName,
    call_id: stringValue(params, "call_id"),
    kind: stringValue(params, "kind"),
    risk: stringValue(params, "risk"),
    policy_action: stringValue(params, "policy_action"),
    policy_reason: stringValue(params, "policy_reason"),
    classification_reason: stringValue(params, "classification_reason"),
    read_only: params.read_only === true,
    destructive: params.destructive === true,
    revision: stringValue(params, "revision"),
    arguments_sha256: stringValue(params, "arguments_sha256"),
    arguments_preview: stringValue(params, "arguments_preview"),
    approval_ref: stringValue(params, "approval_ref"),
    model_next_action: stringValue(params, "model_next_action"),
  };
}

function serverEventTargetsActiveContext(
  event: ServerEvent,
  state: AppState,
): boolean {
  return event.workdir === state.activeContext?.cwd;
}

type StreamingNotificationHandling = "state" | "stream" | "stream-state" | "skip";

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
      return appendStreamDelta(params, "text") ? "stream-state" : "stream";
    case "item/agentMessage/replace":
      if (!notificationTargetsActiveThread(params, state)) {
        return "skip";
      }
      return replaceStreamText(params, "text") ? "stream-state" : "stream";
    case "item/reasoning/delta":
      if (!notificationTargetsActiveThread(params, state)) {
        return "skip";
      }
      return appendStreamDelta(params, "text") ? "stream-state" : "stream";
    case "item/reasoning/replace":
      if (!notificationTargetsActiveThread(params, state)) {
        return "skip";
      }
      return replaceStreamText(params, "text") ? "stream-state" : "stream";
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
    case "turn/usage":
      return "state";
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

function serverEventMayAffectProcesses(event: ServerEvent): boolean {
  if (event.kind !== "notification") {
    return false;
  }
  return (
    event.message.method === "item/completed" ||
    event.message.method === "turn/completed" ||
    event.message.method === "turn/error"
  );
}

function upsertManagedProcess(
  processes: ManagedProcess[],
  process: ManagedProcess,
): ManagedProcess[] {
  const next = processes.filter((item) => item.id !== process.id);
  next.push(process);
  return next;
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
): boolean {
  const turnID = params?.turn_id as string | undefined;
  const itemID = params?.item_id as string | undefined;
  const delta = params?.delta as string | undefined;
  if (!turnID || !itemID || !delta) {
    return false;
  }
  const key = streamTextKey(turnID, itemID, field);
  const wasEmpty = streamTextStore.get(key).trim().length === 0;
  streamTextStore.append(key, delta);
  return field === "text" && wasEmpty && streamTextStore.get(key).trim().length > 0;
}

function replaceStreamText(
  params: Record<string, unknown> | undefined,
  field: StreamTextField,
): boolean {
  const turnID = params?.turn_id as string | undefined;
  const itemID = params?.item_id as string | undefined;
  const text = params?.text;
  if (!turnID || !itemID || typeof text !== "string") {
    return false;
  }
  const key = streamTextKey(turnID, itemID, field);
  const wasEmpty = streamTextStore.get(key).trim().length === 0;
  streamTextStore.set(key, text);
  return field === "text" && wasEmpty && text.trim().length > 0;
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
    case "turn/usage": {
      const turnID = stringValue(params, "turn_id");
      if (!turnID) {
        return state;
      }
      return appendTurnTokenSample(
        state,
        turnID,
        stringValue(params, "thread_id") ?? "",
        numberValue(params, "input_tokens") ?? 0,
        numberValue(params, "output_tokens") ?? 0,
        numberValue(params, "cache_creation_tokens") ?? 0,
        numberValue(params, "cache_read_tokens") ?? 0,
        Date.now(),
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
  // Two-section sort. Running threads use `created_at` as the key so that
  // streaming updates (which bump `updated_at`) do not reshuffle them —
  // clicking or switching between two running threads must leave the sidebar
  // order alone. Settled threads keep the recency-first behavior, so the most
  // recently completed conversation bubbles to the top of the settled group.
  const valid = threads.filter(
    (thread): thread is Thread =>
      isThread(thread) && !thread.archived && !thread.read_only,
  );
  const running = valid.filter(isThreadRunning);
  const settled = valid.filter((thread) => !isThreadRunning(thread));
  running.sort((left, right) => threadCreatedTime(right) - threadCreatedTime(left));
  settled.sort((left, right) => threadTime(right) - threadTime(left));
  return [...running, ...settled];
}

function threadCreatedTime(thread: Thread): number {
  const createdAt = Date.parse(thread.created_at);
  return Number.isFinite(createdAt) ? createdAt : 0;
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
    files: draft.files.map((file) => ({ ...file })),
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
    files: draft.files.map((file) => ({ ...file })),
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
            files: draft.files.map((file) => ({ ...file })),
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
    files: tab.files.map((file) => ({ ...file })),
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

function queryTextsForThread(thread: Thread | undefined): string[] {
  if (!thread) {
    return [];
  }
  const queries: string[] = [];
  for (const turn of thread.turns) {
    for (const item of turn.items) {
      if (item.type !== "user_message") {
        continue;
      }
      const text = (item.text ?? "").trim();
      if (text.length > 0) {
        queries.push(text);
      }
    }
  }
  return queries;
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

function latestCompletedTurnID(thread: Thread): string | undefined {
  // Walk newest → oldest. Most threads end with a non-in_progress turn so the
  // first hit is the answer; we still guard against an in_progress tail so a
  // thread that was reset to running does not get pinned to a stale ID.
  for (let index = thread.turns.length - 1; index >= 0; index -= 1) {
    const turn = thread.turns[index];
    if (turn.status === "in_progress") {
      return undefined;
    }
    return turn.id;
  }
  return undefined;
}

function isThreadUnread(
  thread: Thread | undefined,
  lastViewedTurnID: string | undefined,
): boolean {
  if (!thread) return false;
  if (isThreadRunning(thread)) return false;
  const lastTurnID = latestCompletedTurnID(thread);
  if (!lastTurnID) return false;
  if (!lastViewedTurnID) return true;
  return lastTurnID !== lastViewedTurnID;
}

function markThreadTurnsViewed(
  state: AppState,
  threadID: string,
): AppState {
  const thread = threadForTab(state, threadID);
  if (!thread) return state;
  const lastTurnID = latestCompletedTurnID(thread);
  if (!lastTurnID) return state;
  if (state.lastViewedTurnByThreadID[threadID] === lastTurnID) return state;
  return {
    ...state,
    lastViewedTurnByThreadID: {
      ...state.lastViewedTurnByThreadID,
      [threadID]: lastTurnID,
    },
  };
}

function activeTurnIDForThread(thread: Thread | undefined): string | undefined {
  if (!thread) {
    return undefined;
  }
  for (let index = thread.turns.length - 1; index >= 0; index -= 1) {
    const turn = thread.turns[index];
    if (turn.status === "in_progress") {
      return turn.id;
    }
  }
  return undefined;
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
  const files = userItem.files ?? [];
  if (files.length === 1) {
    return `[${files[0].filename?.trim() || "File #1"}]`;
  }
  if (files.length > 1) {
    return `[${files.length} files]`;
  }
  return "";
}

function hasText(value: string): boolean {
  return value.trim() !== "";
}

function composerSubmissionDetail(imageCount: number, fileCount: number): string {
  const parts = [
    imageCount > 0 ? `${imageCount} 张图片` : "",
    fileCount > 0 ? `${fileCount} 个文件` : "",
  ].filter(Boolean);
  return parts.length > 0 ? `已提交输入，包含 ${parts.join("、")}` : "已提交输入";
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
    agent_profile: stringValue(record, "agent_profile"),
    agent_path: stringValue(record, "agent_path"),
    parent_id: stringValue(record, "parent_id"),
    description: stringValue(record, "description"),
    status,
    result: stringValue(record, "result"),
    error: stringValue(record, "error"),
    input_tokens: numberValue(record, "input_tokens"),
    output_tokens: numberValue(record, "output_tokens"),
    cache_creation_tokens: numberValue(record, "cache_creation_tokens"),
    cache_read_tokens: numberValue(record, "cache_read_tokens"),
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

function appendTurnTokenSample(
  state: AppState,
  turnID: string,
  threadID: string,
  inputTokens: number,
  outputTokens: number,
  cacheCreationTokens: number,
  cacheReadTokens: number,
  at: number,
): AppState {
  const turnTokenUsage = state.turnTokenUsage ?? {};
  const previous = turnTokenUsage[turnID];
  const cutoff = at - TOKEN_SPEED_WINDOW_MS;
  const hasRealHistory = previous?.speedSource === "real";
  const previousSpeedTokens = hasRealHistory ? previous.speedTokens : 0;
  const speedTokens = hasRealHistory
    ? Math.max(previousSpeedTokens, outputTokens)
    : outputTokens;
  const outputIncreased = outputTokens > previousSpeedTokens;
  const shouldAppendSample = outputTokens > 0 && outputIncreased;
  const samples: TurnTokenSample[] = [];
  if (hasRealHistory) {
    for (const sample of previous.samples) {
      if (!shouldAppendSample || sample.at >= cutoff) {
        samples.push(sample);
      }
    }
  }
  if (shouldAppendSample) {
    samples.push({ tokens: speedTokens, at });
  }
  return {
    ...state,
    turnTokenUsage: {
      ...turnTokenUsage,
      [turnID]: {
        threadID,
        inputTokens,
        outputTokens,
        cacheCreationTokens,
        cacheReadTokens,
        speedTokens,
        speedSource: "real",
        samples,
      },
    },
  };
}

function appendStreamingTokenSample(
  state: AppState,
  params: Record<string, unknown> | undefined,
  at: number,
): AppState {
  const turnID = stringValue(params, "turn_id");
  const delta = stringValue(params, "delta");
  if (!turnID || !delta) {
    return state;
  }
  const estimatedTokens = estimateStreamingOutputTokens(delta);
  if (estimatedTokens <= 0) {
    return state;
  }
  const threadID = stringValue(params, "thread_id") ?? "";
  const turnTokenUsage = state.turnTokenUsage ?? {};
  const previous = turnTokenUsage[turnID];
  const cutoff = at - TOKEN_SPEED_WINDOW_MS;
  const samples: TurnTokenSample[] = [];
  if (previous) {
    for (const sample of previous.samples) {
      if (sample.at >= cutoff) {
        samples.push(sample);
      }
    }
  }
  const speedTokens =
    (previous?.speedTokens ?? previous?.outputTokens ?? 0) + estimatedTokens;
  samples.push({ tokens: speedTokens, at });
  return {
    ...state,
    turnTokenUsage: {
      ...turnTokenUsage,
      [turnID]: {
        threadID: previous?.threadID || threadID,
        inputTokens: previous?.inputTokens ?? 0,
        outputTokens: previous?.outputTokens ?? 0,
        cacheCreationTokens: previous?.cacheCreationTokens ?? 0,
        cacheReadTokens: previous?.cacheReadTokens ?? 0,
        speedTokens,
        speedSource: "estimated",
        samples,
      },
    },
  };
}

function estimateStreamingOutputTokens(text: string): number {
  let ascii = 0;
  let nonAscii = 0;
  for (const char of text) {
    const codePoint = char.codePointAt(0) ?? 0;
    if (codePoint <= 0x7f) {
      ascii += 1;
    } else {
      nonAscii += 1;
    }
  }
  return ascii / 4 + nonAscii / 1.7;
}

function activeTurnTokenSpeed(state: AppState, turnID?: string): number {
  return activeTurnTokenSpeedSnapshot(state, turnID).tokensPerSecond;
}

function activeTurnTokenSpeedSnapshot(
  state: AppState,
  turnID?: string,
): TurnTokenSpeedSnapshot {
  if (!turnID) {
    return { tokensPerSecond: 0, source: "none" };
  }
  const usage = state.turnTokenUsage?.[turnID];
  if (!usage || usage.samples.length < 2) {
    return {
      tokensPerSecond: 0,
      source: usage?.speedSource ?? "none",
      sampledAt: usage?.samples.at(-1)?.at,
    };
  }
  const first = usage.samples[0];
  const last = usage.samples[usage.samples.length - 1];
  const delta = last.tokens - first.tokens;
  const elapsed = last.at - first.at;
  if (elapsed <= 0 || delta <= 0) {
    return {
      tokensPerSecond: 0,
      source: usage.speedSource,
      sampledAt: last.at,
    };
  }
  return {
    tokensPerSecond: (delta / elapsed) * 1000,
    source: usage.speedSource,
    sampledAt: last.at,
  };
}

export {
  activeProjectID,
  activeSessionTab,
  activeThreadForState,
  activeThreadIDForState,
  activeTurnIDForThread,
  activeTurnTokenSpeed,
  activeTurnTokenSpeedSnapshot,
  agentFromRecord,
  appendStreamingTokenSample,
  appendTurnTokenSample,
  bindActiveSessionTabToThread,
  cloneComposerDraft,
  cloneSessionTabDraft,
  composerSubmissionDetail,
  conversationSearchContextLabel,
  conversationSearchThreadMeta,
  createDraftSessionTab,
  createFileSessionTab,
  createSkillsSessionTab,
  createThreadSessionTab,
  draftSessionTabForContext,
  draftSessionTabIDForContext,
  emptyComposerDraft,
  ensureSessionTab,
  fileNameFromPath,
  fileSessionTabID,
  handleStreamingNotification,
  hasText,
  initialSplitComposerDrafts,
  initialState,
  isAnyThreadRunning,
  isDirectChildAgent,
  isStateActiveThreadRunning,
  isThread,
  isThreadRunning,
  isThreadUnread,
  latestCompletedTurnID,
  latestPlanUpdateForThread,
  markThreadTurnsViewed,
  mergeAgentSummary,
  mergeListedThreads,
  notificationTargetsActiveThread,
  parsePlanUpdateArguments,
  persistActiveSessionTabDraft,
  pinnedThreads,
  projectThreads,
  queryTextsForThread,
  reduceNotification,
  reduceServerEvent,
  removeSessionTab,
  replaceStreamText,
  requireThread,
  runtimeContextKey,
  sameRuntimeContext,
  serverEventMayAffectProcesses,
  serverEventShouldRefreshGit,
  serverEventTargetsActiveContext,
  sessionTabDraftForThread,
  sessionTabDraftForThreadID,
  sessionTabForLoadedRuntime,
  sessionTabLabel,
  setThreadForPane,
  skillsSessionTabID,
  sortThreads,
  threadItemFromRecord,
  threadForPane,
  threadForTab,
  threadFromRecord,
  threadIDFromParams,
  threadMatchesActiveContext,
  threadSessionTabID,
  threadTime,
  turnFromRecord,
  turnPreview,
  updateThread,
  updateThreadByID,
  updateTurnItem,
  upsertManagedProcess,
  upsertThread,
  upsertThreadChildAgent,
  upsertTurn,
  upsertTurnItem,
  withLoadedRuntimeSessionTab,
};

export type { AppState, ComposerDraftState, ConversationPaneID, SessionTab };
