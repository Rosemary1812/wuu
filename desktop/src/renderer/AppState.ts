import type {
  Agent,
  AppServerNotification,
  AppServerRequest,
  DesktopProject,
  GitStatusResult,
  InitializeResult,
  ParticipantSummary,
  PendingToolApproval,
  PlanUpdate,
  RuntimeContext,
  ServerEvent,
  Thread,
  ThreadItem,
  Turn,
} from "../shared/protocol";
import type { ComposerFile, ComposerImage } from "./ComposerMessages";
import { isAgentHandoffText } from "./AgentHandoff";
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

export type TurnStreamStatus = {
  text: string;
  liveProgress: boolean;
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
  // turnRequestContext tracks the latest request-shape telemetry emitted
  // during a turn. It explains cache-sensitive context structure without
  // treating provider cache counters as retained conversation size.
  turnRequestContext: Record<string, TurnRequestContextDigest>;
  // turnStreamStatus tracks transient transport lifecycle chips for live turns.
  // It is UI-only state, cleared as soon as the stream reconnects or the turn
  // settles. liveProgress is structural, so chips do not infer animation from
  // Chinese display text.
  turnStreamStatus: Record<string, TurnStreamStatus>;
  // turnStreamTransport remembers the last provider-reported transport for a
  // turn so later reconnect lifecycle events can say "WebSocket" or "SSE"
  // without coupling UI text to provider names.
  turnStreamTransport: Record<string, string>;
  // lastViewedTurnByThreadID remembers the most recent turn that the user
  // has actually been on the thread for. It is the source of truth for the
  // sidebar / session-tab "has-unread" indicator — a thread is unread when
  // its latest completed turn ID is not in this map (or the entry is older).
  // Active-tab tracking is what advances this map; running threads are never
  // flagged unread because they have not finished yet.
  lastViewedTurnByThreadID: Record<string, string>;
};

export type ThreadTurnSummary = Pick<
  Turn,
  "id" | "status" | "started_at" | "completed_at" | "duration_ms"
>;

export type ThreadSummary = Omit<
  Thread,
  "turns" | "browser_state" | "listening_ports"
> & {
  turns: ThreadTurnSummary[];
  turn_count: number;
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
  // contextTokens is the retained conversation estimate produced by the
  // agent loop after request-only context has been excluded. It is what the
  // composer context meter displays.
  contextTokens?: number;
  // contextWindowTokens is the resolved runtime window size for the active
  // model at the time of the latest real (provider-reported) usage sample.
  // Streaming estimates preserve it from the previous real sample so the
  // context meter does not flicker to "unknown" between provider reports.
  contextWindowTokens?: number;
  model?: string;
};

export type TurnRequestContextDigest = {
  stepIndex: number;
  messageCount: number;
  stablePrefix: number;
  turnPrefix: number;
  transientMessages: number;
  hiddenMessages: number;
  toolCount: number;
  stablePrefixBytes: number;
  turnPrefixBytes: number;
  messageBytes: number;
  dynamicBytes: number;
  toolSchemaBytes: number;
  promptCacheKey?: string;
  stablePrefixHash?: string;
  turnPrefixHash?: string;
  toolSurfaceHash?: string;
  loadableToolSurfaceHash?: string;
};

export type TurnContextUsage = {
  turnID: string;
  // used is the retained conversation estimate after request-only context
  // has been excluded. Raw provider input/cache usage is not a proxy for
  // this number because it can include one-off tool context.
  used: number;
  window: number;
  inputTokens: number;
  cacheCreationTokens: number;
  cacheReadTokens: number;
  requestContext?: TurnRequestContextDigest;
};

type TurnTokenSpeedSnapshot = {
  tokensPerSecond: number;
  source: TokenSpeedSource;
  sampledAt?: number;
};

type ContextUsageFallback = {
  model?: string;
  contextWindowTokens?: number;
};

const TOKEN_SPEED_WINDOW_MS = 2000;
const STREAM_TEXT_FIELDS: StreamTextField[] = ["text", "arguments", "result"];

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
  turnRequestContext: {},
  turnStreamStatus: {},
  turnStreamTransport: {},
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
            pendingToolApproval: pendingToolApprovalWithOwner(
              approval,
              state,
            ),
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
        status: "wuu 内部错误",
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
    permission: stringValue(params, "permission"),
    permission_patterns: stringArrayValue(params, "permission_patterns"),
    permission_always: stringArrayValue(params, "permission_always"),
    permission_rule: stringValue(params, "permission_rule"),
    capability: stringValue(params, "capability"),
    capability_object: stringValue(params, "capability_object"),
    capability_action: stringValue(params, "capability_action"),
    capability_rule: stringValue(params, "capability_rule"),
    model_next_action: stringValue(params, "model_next_action"),
  };
}

function pendingToolApprovalWithOwner(
  approval: PendingToolApproval,
  state: AppState,
): PendingToolApproval {
  const callID = approval.call_id;
  if (!callID) {
    return approval;
  }
  const matches: Array<{ threadID: string; turnID: string }> = [];
  const seenThreadIDs = new Set<string>();
  for (const thread of [state.thread, state.secondaryThread, ...state.threads]) {
    if (!thread || seenThreadIDs.has(thread.id)) {
      continue;
    }
    seenThreadIDs.add(thread.id);
    for (const turn of thread.turns) {
      if (turnOwnsToolCall(turn, callID)) {
        matches.push({ threadID: thread.id, turnID: turn.id });
        break;
      }
    }
  }
  if (matches.length !== 1) {
    return approval;
  }
  const [match] = matches;
  return {
    ...approval,
    thread_id: match.threadID,
    turn_id: match.turnID,
  };
}

function turnOwnsToolCall(turn: Turn, callID: string): boolean {
  return turn.items.some((item) => item.id === callID || item.source_id === callID);
}

function stringArrayValue(record: JsonRecord, key: string): string[] | undefined {
  const value = record[key];
  if (!Array.isArray(value)) {
    return undefined;
  }
  const strings = value
    .filter((item): item is string => typeof item === "string" && item.trim() !== "")
    .map((item) => item.trim());
  return strings.length > 0 ? strings : undefined;
}

function serverEventTargetsActiveContext(
  event: ServerEvent,
  state: AppState,
): boolean {
  return event.workdir === state.activeContext?.cwd;
}

type StreamingNotificationHandling =
  | "state"
  | "stream"
  | "stream-state"
  | "background-stream"
  | "skip";

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
    case "item/agentMessage/delta": {
      const active = notificationTargetsActiveThread(params, state);
      if (!active && !notificationTargetsKnownThread(params, state)) {
        return "skip";
      }
      const hasVisibleText = appendStreamDelta(params, "text");
      return streamHandlingForThread(active, hasVisibleText);
    }
    case "item/agentMessage/replace": {
      const active = notificationTargetsActiveThread(params, state);
      if (!active && !notificationTargetsKnownThread(params, state)) {
        return "skip";
      }
      const hasVisibleText = replaceStreamText(params, "text");
      return streamHandlingForThread(active, hasVisibleText);
    }
    case "item/reasoning/delta": {
      const active = notificationTargetsActiveThread(params, state);
      if (!active && !notificationTargetsKnownThread(params, state)) {
        return "skip";
      }
      const hasVisibleText = appendStreamDelta(params, "text");
      return streamHandlingForThread(active, hasVisibleText);
    }
    case "item/reasoning/replace": {
      const active = notificationTargetsActiveThread(params, state);
      if (!active && !notificationTargetsKnownThread(params, state)) {
        return "skip";
      }
      const hasVisibleText = replaceStreamText(params, "text");
      return streamHandlingForThread(active, hasVisibleText);
    }
    case "item/toolCall/delta": {
      const active = notificationTargetsActiveThread(params, state);
      if (!active && !notificationTargetsKnownThread(params, state)) {
        return "skip";
      }
      appendStreamDelta(params, "arguments");
      return active ? "stream" : "background-stream";
    }
    case "item/toolCall/outputDelta": {
      const active = notificationTargetsActiveThread(params, state);
      if (!active && !notificationTargetsKnownThread(params, state)) {
        return "skip";
      }
      appendStreamDelta(params, "result");
      return active ? "stream" : "background-stream";
    }
    case "turn/event":
      return "skip";
    case "turn/usage":
      return "state";
    case "item/started":
    case "item/completed":
      if (!notificationTargetsKnownThread(params, state)) {
        return "skip";
      }
      syncStreamItem(params);
      return "state";
    default:
      return "state";
  }
}

function streamHandlingForThread(
  active: boolean,
  hasVisibleText: boolean,
): StreamingNotificationHandling {
  if (!active) {
    return "background-stream";
  }
  return hasVisibleText ? "stream-state" : "stream";
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

function notificationTargetsKnownThread(
  params: Record<string, unknown> | undefined,
  state: AppState,
): boolean {
  const threadID = threadIDFromParams(params);
  return (
    !threadID ||
    threadID === state.thread?.id ||
    threadID === state.secondaryThread?.id ||
    state.threads.some((thread) => thread.id === threadID)
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
  streamTextStore.replace(key, text);
  return field === "text" && wasEmpty && text.trim().length > 0;
}

function syncStreamItem(params: Record<string, unknown> | undefined): void {
  const turnID = params?.turn_id as string | undefined;
  const item = params?.item as ThreadItem | undefined;
  if (!turnID || !item?.id) {
    return;
  }
  const completed = (item.status ?? "in_progress") !== "in_progress";
  const hasFinalText = typeof item.text === "string" && item.text.length > 0;
  const retainTextStream =
    completed &&
    (item.type === "agent_message" || item.type === "reasoning") &&
    !hasFinalText;
  if (typeof item.text === "string") {
    // Snapshots can arrive behind the delta stream, including completed
    // snapshots. Never let an older snapshot shrink the text the user is
    // already reading; release the stream cache only after snapshots catch up.
    if (streamSnapshotCoversCachedValue(turnID, item.id, "text", item.text)) {
      streamTextStore.set(streamTextKey(turnID, item.id, "text"), item.text);
    }
  }
  if (typeof item.arguments === "string") {
    if (
      streamSnapshotCoversCachedValue(
        turnID,
        item.id,
        "arguments",
        item.arguments,
      )
    ) {
      streamTextStore.set(
        streamTextKey(turnID, item.id, "arguments"),
        item.arguments,
      );
    }
  }
  if (typeof item.result === "string") {
    if (streamSnapshotCoversCachedValue(turnID, item.id, "result", item.result)) {
      streamTextStore.set(streamTextKey(turnID, item.id, "result"), item.result);
    }
  }
  if (
    completed &&
    !retainTextStream &&
    itemStreamSnapshotsCoverCachedValues(turnID, item)
  ) {
    window.requestAnimationFrame(() =>
      streamTextStore.clearItem(turnID, item.id),
    );
  }
}

function streamSnapshotCoversCachedValue(
  turnID: string,
  itemID: string,
  field: StreamTextField,
  snapshotValue: string,
): boolean {
  const key = streamTextKey(turnID, itemID, field);
  if (!streamTextStore.has(key)) {
    return true;
  }
  return snapshotValue.length >= streamTextStore.get(key).length;
}

function itemStreamSnapshotsCoverCachedValues(
  turnID: string,
  item: ThreadItem,
): boolean {
  for (const field of STREAM_TEXT_FIELDS) {
    const key = streamTextKey(turnID, item.id, field);
    if (!streamTextStore.has(key)) {
      continue;
    }
    const value = item[field];
    if (typeof value !== "string") {
      return false;
    }
    if (!streamSnapshotCoversCachedValue(turnID, item.id, field, value)) {
      return false;
    }
  }
  return true;
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
      releaseSettledTurnStreams(turn);
      return clearTurnStreamStatus(
        updateThreadByID(
          state,
          threadID,
          (thread) => upsertTurn(thread, turn),
          {
            running: false,
            status: "ready",
          },
        ),
        turn.id,
        { clearTransport: true },
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
        numberValue(params, "context_window_tokens") ?? 0,
        stringValue(params, "model"),
        numberValue(params, "context_tokens") ?? 0,
      );
    }
    case "turn/event": {
      const turnID = stringValue(params, "turn_id");
      const event = recordValue(params, "event");
      const digest = requestContextDigestFromRecord(
        recordValue(event, "request_context"),
      );
      if (!turnID) {
        return state;
      }
      const providerState = recordValue(event, "provider_state");
      const stateWithTransport = updateTurnStreamTransport(
        state,
        turnID,
        providerState,
      );
      const providerStatus = streamStatusFromProviderState(providerState);
      const stateWithProviderStatus =
        providerStatus === undefined
          ? stateWithTransport
          : setTurnStreamStatus(stateWithTransport, turnID, providerStatus);
      const lifecycleStatus = streamStatusFromLifecycle(
        recordValue(event, "lifecycle"),
        stateWithProviderStatus.turnStreamTransport[turnID],
      );
      const stateWithLifecycle =
        lifecycleStatus === undefined
          ? stateWithProviderStatus
          : setTurnStreamStatus(stateWithProviderStatus, turnID, lifecycleStatus);
      if (!digest) {
        return stateWithLifecycle;
      }
      return {
        ...stateWithLifecycle,
        turnRequestContext: {
          ...stateWithLifecycle.turnRequestContext,
          [turnID]: digest,
        },
      };
    }
    default:
      return state;
  }
}

function setTurnStreamStatus(
  state: AppState,
  turnID: string,
  status: TurnStreamStatus | null,
): AppState {
  if (!status || status.text.trim() === "") {
    return clearTurnStreamStatus(state, turnID);
  }
  const existing = state.turnStreamStatus[turnID];
  if (
    existing?.text === status.text &&
    existing.liveProgress === status.liveProgress
  ) {
    return state;
  }
  return {
    ...state,
    turnStreamStatus: {
      ...state.turnStreamStatus,
      [turnID]: status,
    },
  };
}

function clearTurnStreamStatus(
  state: AppState,
  turnID: string,
  options: { clearTransport?: boolean } = {},
): AppState {
  const hasStatus = state.turnStreamStatus[turnID] !== undefined;
  const hasTransport = state.turnStreamTransport[turnID] !== undefined;
  if (!hasStatus && !(options.clearTransport && hasTransport)) {
    return state;
  }
  const nextStatus = hasStatus
    ? { ...state.turnStreamStatus }
    : state.turnStreamStatus;
  if (hasStatus) {
    delete nextStatus[turnID];
  }
  const nextTransport =
    options.clearTransport && hasTransport
      ? { ...state.turnStreamTransport }
      : state.turnStreamTransport;
  if (options.clearTransport && hasTransport) {
    delete nextTransport[turnID];
  }
  return {
    ...state,
    turnStreamStatus: nextStatus,
    turnStreamTransport: nextTransport,
  };
}

function updateTurnStreamTransport(
  state: AppState,
  turnID: string,
  providerState: JsonRecord | undefined,
): AppState {
  const transport = transportFromProviderState(providerState);
  if (!transport || state.turnStreamTransport[turnID] === transport) {
    return state;
  }
  return {
    ...state,
    turnStreamTransport: {
      ...state.turnStreamTransport,
      [turnID]: transport,
    },
  };
}

function transportFromProviderState(
  providerState: JsonRecord | undefined,
): string | undefined {
  if (!providerState) {
    return undefined;
  }
  const fallbackTransport = normalizedTransport(
    stringValue(providerState, "fallback_transport"),
  );
  if (booleanValue(providerState, "fallback_active") === true && fallbackTransport) {
    return fallbackTransport;
  }
  return normalizedTransport(stringValue(providerState, "transport"));
}

function streamStatusFromProviderState(
  providerState: JsonRecord | undefined,
): TurnStreamStatus | undefined {
  if (!providerState) {
    return undefined;
  }
  const diagnostic = stringValue(providerState, "diagnostic");
  const fallbackActive = booleanValue(providerState, "fallback_active") === true;
  if (diagnostic !== "provider_transport_failure" && !fallbackActive) {
    return undefined;
  }
  const failedTransport = failedTransportLabelFromProviderState(providerState);
  const fallbackTransport = transportLabel(
    stringValue(providerState, "fallback_transport"),
  );
  const failurePhase = stringValue(providerState, "transport_failure_phase");
  const eventsEmitted =
    booleanValue(providerState, "events_emitted") === true ||
    failurePhase === "after_message_stream_start";
  if (eventsEmitted) {
    return {
      text: `${transportSubject(failedTransport)}中断`,
      liveProgress: false,
    };
  }
  if (fallbackTransport) {
    return {
      text: failedTransport
        ? `${failedTransport} 不可用，已切到 ${fallbackTransport}`
        : `消息流已切到 ${fallbackTransport}`,
      liveProgress: false,
    };
  }
  return {
    text: `${transportSubject(failedTransport)}中断`,
    liveProgress: false,
  };
}

function streamStatusFromLifecycle(
  lifecycle: JsonRecord | undefined,
  transport: string | undefined,
): TurnStreamStatus | null | undefined {
  if (!lifecycle) {
    return undefined;
  }
  const phase = stringValue(lifecycle, "phase");
  if (phase !== "reconnecting" && phase !== "failed") {
    return null;
  }
  const subject = transportSubject(transportLabel(transport));
  if (phase === "failed") {
    return {
      text: `${subject}恢复失败`,
      liveProgress: false,
    };
  }
  const retryCount =
    positiveInteger(numberValue(lifecycle, "retry_count")) ??
    retryCountFromAttempt(numberValue(lifecycle, "attempt"));
  const maxRetries = positiveInteger(numberValue(lifecycle, "max_retries"));
  if (maxRetries) {
    return {
      text: `${subject}重连中 ${retryCount}/${maxRetries}`,
      liveProgress: true,
    };
  }
  return {
    text: `${subject}重连中，第 ${retryCount} 次`,
    liveProgress: true,
  };
}

function failedTransportLabelFromProviderState(
  providerState: JsonRecord,
): string | undefined {
  const failedTransport = transportLabel(
    stringValue(providerState, "failed_transport"),
  );
  if (failedTransport) {
    return failedTransport;
  }
  const fallbackReason = stringValue(providerState, "fallback_reason")
    ?.trim()
    .toLowerCase();
  if (
    fallbackReason?.includes("websocket") ||
    fallbackReason?.includes("web socket")
  ) {
    return "WebSocket";
  }
  return transportLabel(stringValue(providerState, "transport"));
}

function transportSubject(transport: string | undefined): string {
  return transport ? `${transport} 消息流` : "消息流";
}

function transportLabel(transport: string | undefined): string | undefined {
  switch (normalizedTransport(transport)) {
    case "websocket":
    case "websocket-cached":
    case "ws":
      return "WebSocket";
    case "sse":
    case "http":
    case "https":
      return "HTTP";
    default:
      return undefined;
  }
}

function normalizedTransport(transport: string | undefined): string | undefined {
  const normalized = transport?.trim().toLowerCase();
  return normalized ? normalized : undefined;
}

function booleanValue(record: JsonRecord, key: string): boolean | undefined {
  const value = record[key];
  return typeof value === "boolean" ? value : undefined;
}

function retryCountFromAttempt(attempt: number | undefined): number {
  const safeAttempt = positiveInteger(attempt);
  return safeAttempt ? Math.max(1, safeAttempt - 1) : 1;
}

function positiveInteger(value: number | undefined): number | undefined {
  if (value === undefined) {
    return undefined;
  }
  const integer = Math.floor(value);
  return integer > 0 ? integer : undefined;
}

function releaseSettledTurnStreams(turn: Turn): void {
  if (turn.status === "in_progress") {
    return;
  }
  for (const item of turn.items) {
    if (item.status === "in_progress") {
      continue;
    }
    for (const field of STREAM_TEXT_FIELDS) {
      const value = item[field];
      if (
        typeof value === "string" &&
        value.length > 0 &&
        streamSnapshotCoversCachedValue(turn.id, item.id, field, value)
      ) {
        streamTextStore.clearField(turn.id, item.id, field);
      }
    }
  }
}

function requestContextDigestFromRecord(
  record: JsonRecord | undefined,
): TurnRequestContextDigest | undefined {
  if (!record) {
    return undefined;
  }
  const stepIndex = numberValue(record, "step_index");
  const messageCount = numberValue(record, "message_count");
  const stablePrefix = numberValue(record, "stable_prefix");
  const turnPrefix = numberValue(record, "turn_prefix");
  if (
    stepIndex === undefined ||
    messageCount === undefined ||
    stablePrefix === undefined ||
    turnPrefix === undefined
  ) {
    return undefined;
  }
  return {
    stepIndex,
    messageCount,
    stablePrefix,
    turnPrefix,
    transientMessages: numberValue(record, "transient_messages") ?? 0,
    hiddenMessages: numberValue(record, "hidden_messages") ?? 0,
    toolCount: numberValue(record, "tool_count") ?? 0,
    stablePrefixBytes: numberValue(record, "stable_prefix_bytes") ?? 0,
    turnPrefixBytes: numberValue(record, "turn_prefix_bytes") ?? 0,
    messageBytes: numberValue(record, "message_bytes") ?? 0,
    dynamicBytes: numberValue(record, "dynamic_bytes") ?? 0,
    toolSchemaBytes: numberValue(record, "tool_schema_bytes") ?? 0,
    promptCacheKey: stringValue(record, "prompt_cache_key"),
    stablePrefixHash: stringValue(record, "stable_prefix_hash"),
    turnPrefixHash: stringValue(record, "turn_prefix_hash"),
    toolSurfaceHash: stringValue(record, "tool_surface_hash"),
    loadableToolSurfaceHash: stringValue(
      record,
      "loadable_tool_surface_hash",
    ),
  };
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

function conversationPaneThreadsByID(
  threads: Thread[],
  primaryThread?: Thread,
  secondaryThread?: Thread,
): Map<string, Thread> {
  const byID = new Map<string, Thread>();
  for (const thread of threads) {
    byID.set(thread.id, thread);
  }
  if (primaryThread) {
    byID.set(primaryThread.id, primaryThread);
  }
  if (secondaryThread) {
    byID.set(secondaryThread.id, secondaryThread);
  }
  return byID;
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

function summarizeAgentForSidebar(agent: Agent): Agent {
  return {
    id: agent.id,
    type: agent.type,
    task_name: agent.task_name,
    agent_profile: agent.agent_profile,
    agent_path: agent.agent_path,
    parent_id: agent.parent_id,
    description: agent.description,
    status: agent.status,
    nested_count: agent.nested_count,
    nested_running_count: agent.nested_running_count,
    started_at: agent.started_at,
    completed_at: agent.completed_at,
    pinned: agent.pinned,
    archived: agent.archived,
    participant: agent.participant,
  };
}

function summarizeTurnForSidebar(turn: Turn): ThreadTurnSummary {
  return {
    id: turn.id,
    status: turn.status,
    started_at: turn.started_at,
    completed_at: turn.completed_at,
    duration_ms: turn.duration_ms,
  };
}

function summarizeThreadForSidebar(thread: Thread): ThreadSummary {
  return {
    id: thread.id,
    parent_id: thread.parent_id,
    agent_path: thread.agent_path,
    preview: thread.preview,
    title: thread.title,
    model_provider: thread.model_provider,
    model: thread.model,
    cwd: thread.cwd,
    workspace_kind: thread.workspace_kind,
    status: thread.status,
    read_only: thread.read_only,
    pinned: thread.pinned,
    dm_participant_id: thread.dm_participant_id,
    group: thread.group,
    members: thread.members,
    archived: thread.archived,
    forked_from_id: thread.forked_from_id,
    forked_from_turn_id: thread.forked_from_turn_id,
    forked_from_item_id: thread.forked_from_item_id,
    worktree: thread.worktree,
    created_at: thread.created_at,
    updated_at: thread.updated_at,
    turns: thread.turns.map(summarizeTurnForSidebar),
    turn_count: thread.turns.length,
    child_agents: thread.child_agents?.map(summarizeAgentForSidebar),
  };
}

function summarizeThreadsForSidebar(threads: Thread[]): ThreadSummary[] {
  return threads.map(summarizeThreadForSidebar);
}

function sortThreadSummaries(threads: ThreadSummary[]): ThreadSummary[] {
  const valid = threads.filter(
    (thread): thread is ThreadSummary =>
      Boolean(thread && typeof thread.id === "string") &&
      !thread.archived &&
      !thread.read_only,
  );
  const running = valid.filter(isThreadRunning);
  const settled = valid.filter((thread) => !isThreadRunning(thread));
  running.sort((left, right) => threadCreatedTime(right) - threadCreatedTime(left));
  settled.sort((left, right) => threadTime(right) - threadTime(left));
  return [...running, ...settled];
}

function threadCreatedTime(thread: Pick<Thread, "created_at" | "updated_at">): number {
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
  const projectPath = threadProjectPath(thread);
  const project = projects.find((candidate) => candidate.path === projectPath);
  return project?.name ?? fileNameFromPath(projectPath) ?? "wuu";
}

function pinnedThreads(threads: Thread[]): Thread[] {
  return sortThreads(threads).filter((thread) => thread.pinned);
}

function pinnedThreadSummaries(threads: ThreadSummary[]): ThreadSummary[] {
  return sortThreadSummaries(threads).filter((thread) => thread.pinned);
}

function projectThreads(threads: Thread[]): Thread[] {
  return sortThreads(threads).filter((thread) => !thread.pinned);
}

function projectThreadSummaries(threads: ThreadSummary[]): ThreadSummary[] {
  return sortThreadSummaries(threads).filter((thread) => !thread.pinned);
}

export function threadProjectPath(
  thread: Pick<Thread, "cwd" | "worktree">,
): string {
  return thread.worktree?.base_repo?.trim() || thread.cwd;
}

export function threadBelongsToProject(
  thread: Pick<Thread, "cwd" | "worktree">,
  project: Pick<DesktopProject, "path">,
): boolean {
  return sameDesktopPath(threadProjectPath(thread), project.path);
}

function threadBelongsToAnyProject(
  thread: Pick<Thread, "cwd" | "worktree">,
  projects: Pick<DesktopProject, "path">[],
): boolean {
  return projects.some((project) => threadBelongsToProject(thread, project));
}

function sameDesktopPath(left: string, right: string): boolean {
  return cleanDesktopPath(left) === cleanDesktopPath(right);
}

function cleanDesktopPath(path: string): string {
  const trimmed = path.trim();
  const withoutTrailingSlash = trimmed.replace(/\/+$/, "");
  return withoutTrailingSlash || trimmed;
}

// SCRATCH_PSEUDO_PROJECT_ID is the synthetic project id used to render the
// scratch (no-project) conversation group inside the unified sidebar tree.
// Threads whose cwd does not belong to a registered DesktopProject (i.e.
// isScratchThread returns true) are bucketed under this id so the sidebar
// can render them through the same ProjectList code path as real projects.
// The DesktopProject entry carrying this id is built in App.tsx and never
// sent from the app-server — it lives only on the renderer side.
export const SCRATCH_PSEUDO_PROJECT_ID = "__wuu_scratch__";

export function isScratchThread(
  thread: Pick<Thread, "workspace_kind" | "cwd" | "worktree">,
  projects: DesktopProject[],
): boolean {
  if (threadBelongsToAnyProject(thread, projects)) {
    return false;
  }
  if (thread.workspace_kind === "scratch") {
    return true;
  }
  if (thread.workspace_kind === "project") {
    return false;
  }
  // DM threads live under the agent roster, never under the 对话 scratch
  // group. Their cwd (~/.wuu/agents/<id>/home) does not match any registered
  // project, so the fallthrough below would otherwise bucket them here.
  if (thread.workspace_kind === "dm") {
    return false;
  }
  const projectPath = threadProjectPath(thread);
  return !projects.some((project) => project.path === projectPath);
}

/**
 * Resolve the RuntimeContext a thread should open under, independent of
 * whichever sidebar group the user clicked it from (对话 scratch list, 置顶,
 * 群聊, or the agent roster's DM tab). Precedence mirrors isScratchThread:
 * a cwd match against a registered project always wins, even when the
 * thread's own workspace_kind says otherwise (e.g. a thread created before
 * a project was registered at that path). Everything else — scratch, dm,
 * group, or unrecognized — resolves to a no_project context rooted at the
 * thread's own cwd, which is what makes the workspace panel (file tree /
 * terminal / git) follow the thread instead of staying on the previously
 * active project.
 */
export function resolveThreadRuntimeContext(
  thread: Thread,
  projects: DesktopProject[],
): RuntimeContext {
  const project = projects.find((candidate) =>
    threadBelongsToProject(thread, candidate),
  );
  if (project) {
    return { kind: "project", project_id: project.id, cwd: project.path };
  }
  return { kind: "no_project", cwd: thread.cwd };
}

/**
 * Derive the RuntimeContext the workspace panel's file tree, file preview,
 * and terminal should root at. This is ordinarily just the active
 * RuntimeContext, but a worktree-fork thread's own cwd (Thread.cwd) points
 * at a git worktree directory distinct from the project root that
 * resolveThreadRuntimeContext resolves the thread to (threadProjectPath
 * prefers worktree.base_repo, so the *context* stays pinned to the base
 * project while the *thread* itself runs out of the worktree). When the
 * active thread's cwd differs from the active context's cwd, the panel
 * should follow the thread instead — kind/project_id are preserved so
 * labels (project name, etc.) stay stable; only cwd moves.
 *
 * The diff/review panel deliberately does NOT use this: gitStatus is
 * fetched from the app-server bound to activeContext's own workdir, so
 * feeding it the thread's cwd would silently show diff data for the wrong
 * directory. Callers should keep passing activeContext straight through to
 * the diff view.
 */
export function workspacePanelContext(
  activeContext: RuntimeContext | undefined,
  thread: Thread | undefined,
): RuntimeContext | undefined {
  if (!thread || !thread.cwd || thread.cwd === activeContext?.cwd) {
    return activeContext;
  }
  if (!activeContext) {
    return { kind: "no_project", cwd: thread.cwd };
  }
  return { ...activeContext, cwd: thread.cwd };
}

export function scratchThreads(
  threads: Thread[],
  projects: DesktopProject[],
): Thread[] {
  return sortThreads(threads).filter(
    (thread) => !thread.pinned && isScratchThread(thread, projects),
  );
}

export function scratchThreadSummaries(
  threads: ThreadSummary[],
  projects: DesktopProject[],
): ThreadSummary[] {
  return sortThreadSummaries(threads).filter(
    (thread) =>
      !thread.pinned &&
      !thread.dm_participant_id &&
      !thread.group &&
      isScratchThread(thread, projects),
  );
}

/**
 * Predicate that mirrors the wire-level Thread.dm_participant_id field. A
 * thread is a "DM" when it was started (or already exists) as a 1:1
 * conversation with a named participant. DMs are bucketed under the agent
 * roster and never under the 对话 scratch group or any project.
 */
export function isDMThread(
  thread: { dm_participant_id?: string },
): boolean {
  return typeof thread.dm_participant_id === "string" &&
    thread.dm_participant_id.length > 0;
}

/**
 * Predicate that mirrors the wire-level Thread.group field
 * (chat-style-threads-design.md §3). Group threads are chat-style
 * channels with no main agent — bucketed under the sidebar's 群聊
 * section, never under 对话 or any project.
 */
export function isGroupThread(thread: { group?: boolean }): boolean {
  return thread.group === true;
}

/**
 * Group threads for the sidebar's 群聊 section. Same move/remove
 * semantics as the 对话 list: pinning MOVES the thread under 置顶 (so
 * pinned ones are excluded here — no duplicates), and archiving removes
 * it entirely (sortThreadSummaries drops archived threads).
 */
export function groupThreadSummaries(
  threads: ThreadSummary[],
): ThreadSummary[] {
  return sortThreadSummaries(threads).filter(
    (thread) => !thread.pinned && isGroupThread(thread),
  );
}

type DMThreadCandidate = {
  id: string;
  archived?: boolean;
  updated_at: string;
  created_at: string;
  dm_participant_id?: string;
  workspace_kind?: "project" | "scratch" | "dm";
};

/**
 * Pick the latest non-archived DM thread for a given participant id. The
 * picker matches by dm_participant_id AND requires workspace_kind === "dm"
 * so legacy stray DMs (created before per-agent home dirs existed, when DMs
 * were seeded with the active project cwd and carry workspace_kind "project")
 * are deliberately skipped — a fresh, correctly-homed DM thread must be
 * created via startThread instead, or turn execution will keep running in
 * the wrong project directory. Among the survivors the picker prefers the
 * thread with the largest updated_at, falling back to created_at when
 * updated_at is missing. Archived threads are ignored so re-archiving a DM
 * does not resurrect it as the active target. Returns undefined when no
 * live DM exists, which is the signal for the sidebar to create a fresh
 * thread via startThread.
 *
 * Note: `isDMThread` deliberately only checks dm_participant_id. It is used
 * to EXCLUDE DM-tagged threads from project/scratch sidebar sections, and
 * legacy stray threads must remain excluded there too. findDMThread is the
 * reverse direction (picking one DM to re-open) and therefore needs the
 * stricter workspace_kind check.
 */
export function findDMThread<T extends DMThreadCandidate>(
  threads: readonly T[] | undefined,
  participantID: string,
): T | undefined {
  if (typeof participantID !== "string" || participantID.length === 0) {
    return undefined;
  }
  if (!threads) {
    return undefined;
  }
  let best: T | undefined;
  let bestTime = -Infinity;
  for (const thread of threads) {
    if (!thread || thread.dm_participant_id !== participantID) continue;
    if (thread.archived) continue;
    // Only proper DM-kind threads qualify: legacy strays (workspace_kind
    // "project" or missing) intentionally fall through so a fresh
    // per-agent-homed DM is created via startThread instead of resurrecting
    // the mis-homed legacy.
    if (thread.workspace_kind !== "dm") continue;
    const time = threadTime(thread);
    if (time > bestTime) {
      best = thread;
      bestTime = time;
    }
  }
  return best;
}

type SessionTabParticipantCandidate = {
  id: string;
  dm_participant_id?: string;
  updated_at: string;
  created_at: string;
};

/**
 * Find an already-open session tab whose thread belongs to the given DM
 * participant (issue #3: the same agent, e.g. "Andy", could end up with two
 * content-different, indistinguishable tabs). thread/start is now an
 * idempotent find-or-create on the server, but openParticipantDM should
 * still prefer focusing a tab that is already open locally instead of
 * round-tripping to the server at all — and this also guards against any
 * pre-existing duplicate tabs left over from before the server-side fix, or
 * a cross-window race, by always converging on one tab per participant.
 * Only "thread"-kind tabs are considered; ties (more than one open tab for
 * the same participant) are broken by the associated thread's most recent
 * activity, mirroring findDMThread.
 */
export function sessionTabForParticipant(
  tabs: readonly SessionTab[],
  threads: readonly SessionTabParticipantCandidate[] | undefined,
  participantID: string,
): (SessionTab & { kind: "thread" }) | undefined {
  if (typeof participantID !== "string" || participantID.length === 0) {
    return undefined;
  }
  if (!threads || threads.length === 0) {
    return undefined;
  }
  const threadByID = new Map(threads.map((thread) => [thread.id, thread]));
  let best: (SessionTab & { kind: "thread" }) | undefined;
  let bestTime = -Infinity;
  for (const tab of tabs) {
    if (tab.kind !== "thread") continue;
    const thread = threadByID.get(tab.threadID);
    if (!thread || thread.dm_participant_id !== participantID) continue;
    const time = threadTime(thread);
    if (time > bestTime) {
      best = tab;
      bestTime = time;
    }
  }
  return best;
}

/**
 * Collect participant IDs whose resident DM thread is currently running.
 * A resident named agent's DM thread IS the agent's brain, so the roster's
 * busy dot follows the thread's running state rather than per-run child
 * agent events (see docs/plans/2026-07-03-resident-named-agents.md §7.2).
 * Applies the same qualification as findDMThread: non-archived,
 * workspace_kind "dm" — legacy mis-homed strays never drive the busy dot.
 */
export function busyDMParticipantIDs(
  threads:
    | readonly (DMThreadCandidate & {
        status: Thread["status"];
        turns: Array<Pick<Turn, "status">>;
      })[]
    | undefined,
): Set<string> {
  const ids = new Set<string>();
  for (const thread of threads ?? []) {
    if (!thread?.dm_participant_id) continue;
    if (thread.archived) continue;
    if (thread.workspace_kind !== "dm") continue;
    if (isThreadRunning(thread)) {
      ids.add(thread.dm_participant_id);
    }
  }
  return ids;
}

export type ChatMessageRow =
  | { kind: "user"; id: string; turnID: string; item: ThreadItem }
  | { kind: "envelope"; id: string; turnID: string; item: ThreadItem }
  | { kind: "participant"; id: string; turnID: string; item: ThreadItem }
  | { kind: "focus"; id: string; turnID: string; item: ThreadItem };

/**
 * Flatten a thread's turns into the chat-view message stream. Whitelist
 * semantics (chat-style-threads-design.md §2): only user messages,
 * envelope meta rows, tool-posted participant messages, and workspace-
 * focus divider rows are chat messages; the agent's working transcript
 * never reaches the DOM.
 *
 * The focus_meta check runs before the `item.type` branches and does
 * not gate on a particular type value — the wire contract only commits
 * to focus_meta riding on *some* item in the stream (mirroring how
 * envelope_meta rides on a user_message), so any item carrying it
 * becomes a "focus" row regardless of what type the backend tags it
 * with.
 */
export function chatMessagesFromTurns(
  turns: ReadonlyArray<Pick<Turn, "id" | "items">>,
): ChatMessageRow[] {
  const rows: ChatMessageRow[] = [];
  for (const turn of turns) {
    for (const item of turn.items ?? []) {
      const id = `${turn.id}:${item.id}`;
      if (item.focus_meta) {
        rows.push({ kind: "focus", id, turnID: turn.id, item });
      } else if (item.type === "user_message") {
        if (item.envelope_meta && item.envelope_meta.length > 0) {
          rows.push({ kind: "envelope", id, turnID: turn.id, item });
        } else {
          rows.push({ kind: "user", id, turnID: turn.id, item });
        }
      } else if (item.type === "participant_message") {
        rows.push({ kind: "participant", id, turnID: turn.id, item });
      }
    }
  }
  return rows;
}

/**
 * Resolve @Name mentions in a prompt to participant IDs for the
 * turn/start `mentions` param (docs/plans/2026-07-03-resident-named-
 * agents.md §3.1). Whole-word matching: "@Noel" never resolves to a
 * roster entry named "Noe" because the lookahead requires the name to
 * end at whitespace, end-of-text, or CJK/latin punctuation.
 */
export function mentionedParticipantIDsFromText(
  text: string,
  participants: ReadonlyArray<{ id: string; name: string }>,
): string[] {
  const source = text.trim();
  if (source === "") {
    return [];
  }
  const ids = new Set<string>();
  const candidates = [...participants]
    .filter((participant) => participant.name.trim() !== "")
    .sort((a, b) => b.name.length - a.name.length);
  for (const participant of candidates) {
    const escaped = escapeRegExp(participant.name.trim());
    const pattern = new RegExp(`(^|\\s)@${escaped}(?=$|\\s|[,.!?，。；：、])`);
    if (pattern.test(source)) {
      ids.add(participant.id);
    }
  }
  return [...ids];
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/**
 * Resolve the chat-style composer's "work focus" chip value for a
 * thread: the in-session override (once the user has touched the menu
 * for this thread) if present, otherwise the thread's own echoed
 * `focus_workspace` from the last resume, defaulting to "" (全部工作区)
 * when the thread has never set one.
 */
export function chatFocusValueForThread(
  thread: Pick<Thread, "id" | "focus_workspace"> | undefined,
  overrides: Readonly<Record<string, string>>,
): string {
  if (!thread) {
    return "";
  }
  return overrides[thread.id] ?? thread.focus_workspace ?? "";
}

/**
 * Compute the value to send as turn/start's optional `focus_workspace`
 * param. `overrideValue` is the composer's current in-session chip
 * selection for the target thread — undefined means the user never
 * touched the chip this session, so nothing is sent (the thread keeps
 * whatever focus it already has). When the user did pick a value, it is
 * only sent if it actually differs from the thread's own last-known
 * `focus_workspace`; re-selecting the same option (or a value that has
 * since caught up via a resume) sends nothing, keeping ordinary chat
 * turns free of the extra param.
 */
export function focusWorkspaceSendValue(
  thread: Pick<Thread, "focus_workspace"> | undefined,
  overrideValue: string | undefined,
): string | undefined {
  if (overrideValue === undefined) {
    return undefined;
  }
  const current = thread?.focus_workspace ?? "";
  return overrideValue === current ? undefined : overrideValue;
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

function openForkThreadAsPrimary(
  state: AppState,
  {
    sourceThread,
    forkThread,
    context,
    sourceDraft,
    splitDrafts,
  }: {
    sourceThread: Thread;
    forkThread: Thread;
    context: RuntimeContext;
    sourceDraft: ComposerDraftState;
    splitDrafts?: Partial<Record<ConversationPaneID, ComposerDraftState>>;
  },
): AppState {
  const source =
    state.secondaryThread?.id === sourceThread.id
      ? state.secondaryThread
      : state.thread?.id === sourceThread.id
        ? state.thread
        : sourceThread;
  let tabs = state.sessionTabs;
  if (state.thread && state.thread.id !== source.id && splitDrafts?.primary) {
    tabs = ensureSessionTab(
      tabs,
      createThreadSessionTab(state.thread, context, splitDrafts.primary),
    );
  }
  if (
    state.secondaryThread &&
    state.secondaryThread.id !== source.id &&
    splitDrafts?.secondary
  ) {
    tabs = ensureSessionTab(
      tabs,
      createThreadSessionTab(
        state.secondaryThread,
        context,
        splitDrafts.secondary,
      ),
    );
  }
  tabs = ensureSessionTab(
    tabs,
    createThreadSessionTab(source, context, sourceDraft),
  );
  const forkTab = createThreadSessionTab(forkThread, context);
  return {
    ...state,
    thread: forkThread,
    secondaryThread: undefined,
    activePane: "primary",
    allowThreadAutoActivation: true,
    sessionTabs: ensureSessionTab(tabs, forkTab),
    activeSessionTabID: forkTab.id,
    threads: upsertThread(upsertThread(state.threads, source), forkThread),
    running: isThreadRunning(forkThread),
    status: "ready",
  };
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

function threadTime(thread: Pick<Thread, "updated_at" | "created_at">): number {
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
      const text = queryTextForUserItem(item);
      if (text) {
        queries.push(text);
      }
    }
  }
  return queries;
}

function queryTextForUserItem(item: ThreadItem): string | undefined {
  if (item.type !== "user_message") {
    return undefined;
  }
  const text = (item.text ?? "").trim();
  if (!text || isAgentHandoffText(text)) {
    return undefined;
  }
  return text;
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

// Gates `latestPlanUpdateForThread` on the thread still running a turn.
// The floating "跳到最新" pill cluster's progress chip should track only a
// plan that is actively in flight — once the turn completes, the message
// flow's own completed-turn action row takes over that vertical space, and
// a stale plan chip would sit on top of it. `latestPlanUpdateForThread`
// itself stays turn-status-agnostic: the environment side panel (opened
// explicitly by the user) intentionally keeps showing the most recent plan
// as a completed checklist after the turn finishes.
function activePlanUpdateForThread(
  thread: Thread | undefined,
): PlanUpdate | undefined {
  if (!isThreadRunning(thread)) {
    return undefined;
  }
  return latestPlanUpdateForThread(thread);
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
  return Boolean(context && threadProjectPath(thread) === context.cwd);
}

function isThread(value: unknown): value is Thread {
  return Boolean(
    value &&
    typeof value === "object" &&
    typeof (value as Thread).id === "string",
  );
}

function isThreadRunning(
  thread:
    | {
        status: Thread["status"];
        turns: Array<Pick<Turn, "status">>;
      }
    | undefined,
): boolean {
  return Boolean(
    thread?.status === "in_progress" ||
    thread?.turns.some((turn) => turn.status === "in_progress"),
  );
}

function latestCompletedTurnID(thread: {
  turns: Array<Pick<Turn, "id" | "status">>;
}): string | undefined {
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
  thread:
    | {
        status: Thread["status"];
        turns: Array<Pick<Turn, "id" | "status">>;
      }
    | undefined,
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

function turnStreamStatusForThread(
  state: AppState,
  thread: Thread | undefined,
): TurnStreamStatus | undefined {
  const turnID = activeTurnIDForThread(thread);
  return turnID ? state.turnStreamStatus[turnID] : undefined;
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
    participant: participantFromRecord(recordValue(record, "participant")),
  };
}

// participantFromRecord validates the participant payload embedded in
// agent/updated notifications. A payload missing id or name is treated
// as absent so the renderer falls back to the legacy label chain.
function participantFromRecord(
  record: JsonRecord | undefined,
): ParticipantSummary | undefined {
  const id = stringValue(record, "id");
  const name = stringValue(record, "name");
  if (!id || !name) {
    return undefined;
  }
  return {
    id,
    name,
    kind: stringValue(record, "kind") ?? "",
    role: stringValue(record, "role"),
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
  contextWindowTokens: number = 0,
  model?: string,
  contextTokens: number = 0,
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
  // A real usage snapshot is the authoritative source for both the speed
  // samples and the context ceiling. If Go omitted the ceiling (zero
  // or undefined) we still want the meter to keep showing the last known
  // value rather than collapse to "unknown" — most providers emit usage
  // and ceiling together, but a transient omission should not erase state.
  const resolvedWindow =
    contextWindowTokens && contextWindowTokens > 0
      ? contextWindowTokens
      : previous?.contextWindowTokens;
  const resolvedModel = model || previous?.model;
  const resolvedContextTokens =
    contextTokens && contextTokens > 0 ? contextTokens : previous?.contextTokens;
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
        ...(resolvedContextTokens
          ? { contextTokens: resolvedContextTokens }
          : {}),
        ...(resolvedWindow ? { contextWindowTokens: resolvedWindow } : {}),
        ...(resolvedModel ? { model: resolvedModel } : {}),
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
        ...(previous?.contextTokens
          ? { contextTokens: previous.contextTokens }
          : {}),
        // Streaming estimates are byte-deltas, not real usage; they do not
        // know the context window. Carry the previously-resolved window
        // forward so the meter stays visible while we wait for the next
        // real provider usage snapshot.
        ...(previous?.contextWindowTokens
          ? { contextWindowTokens: previous.contextWindowTokens }
          : {}),
        ...(previous?.model ? { model: previous.model } : {}),
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

function activeTurnContextUsage(
  state: AppState,
  turnID?: string,
): TurnContextUsage | undefined {
  if (!turnID) {
    return undefined;
  }
  const usage = state.turnTokenUsage?.[turnID];
  if (
    !usage?.contextWindowTokens ||
    usage.contextWindowTokens <= 0 ||
    !usage.contextTokens ||
    usage.contextTokens <= 0
  ) {
    return undefined;
  }
  return {
    turnID,
    used: usage.contextTokens,
    window: usage.contextWindowTokens,
    inputTokens: usage.inputTokens,
    cacheCreationTokens: usage.cacheCreationTokens,
    cacheReadTokens: usage.cacheReadTokens,
    requestContext: state.turnRequestContext?.[turnID],
  };
}

// latestContextUsageForThread walks the thread's turns from newest to
// oldest and returns the most recent retained-context estimate with a
// known context ceiling. Raw provider input/cache usage is intentionally
// ignored here: it can include request-only tool context that should not
// be shown as current conversation occupancy.
//
// When no real usage is available, falls back to the current runtime
// ceiling even before a thread exists, so the meter renders at 0% from
// the moment a model/provider with a trusted limit is picked.
// Returns undefined only when the model has no known ceiling — we'd
// rather hide the meter than mislead the user with a guessed size.
function latestContextUsageForThread(
  state: AppState,
  thread: Thread | undefined,
  fallback: ContextUsageFallback = {},
): TurnContextUsage | undefined {
  const model = thread?.model || fallback.model || "";
  const currentModel = normalizeModelID(model);
  if (thread) {
    for (let i = thread.turns.length - 1; i >= 0; i -= 1) {
      const turn = thread.turns[i];
      const usage = state.turnTokenUsage?.[turn.id];
      if (
        !usage?.contextWindowTokens ||
        usage.contextWindowTokens <= 0 ||
        !usage.contextTokens ||
        usage.contextTokens <= 0
      ) {
        const turnUsageModel = turn.usage_model;
        if (
          turnUsageModel &&
          currentModel &&
          normalizeModelID(turnUsageModel) !== currentModel
        ) {
          continue;
        }
        const contextTokens = turn.context_tokens ?? 0;
        if (contextTokens <= 0) {
          continue;
        }
        const inputTokens = turn.input_tokens ?? 0;
        const cacheCreationTokens = turn.cache_creation_tokens ?? 0;
        const cacheReadTokens = turn.cache_read_tokens ?? 0;
        const turnWindow =
          fallback.contextWindowTokens && fallback.contextWindowTokens > 0
            ? fallback.contextWindowTokens
            : undefined;
        if (!turnWindow) {
          continue;
        }
        return {
          turnID: turn.id,
          used: contextTokens,
          window: turnWindow,
          inputTokens,
          cacheCreationTokens,
          cacheReadTokens,
          requestContext: state.turnRequestContext?.[turn.id],
        };
      }
      if (
        usage.model &&
        currentModel &&
        normalizeModelID(usage.model) !== currentModel
      ) {
        continue;
      }
      return {
        turnID: turn.id,
        used: usage.contextTokens,
        window: usage.contextWindowTokens,
        inputTokens: usage.inputTokens,
        cacheCreationTokens: usage.cacheCreationTokens,
        cacheReadTokens: usage.cacheReadTokens,
        requestContext: state.turnRequestContext?.[turn.id],
      };
    }
  }
  const fallbackWindow =
    fallback.contextWindowTokens && fallback.contextWindowTokens > 0
      ? fallback.contextWindowTokens
      : undefined;
  if (!fallbackWindow) {
    return undefined;
  }
  return {
    turnID: "",
    used: 0,
    window: fallbackWindow,
    inputTokens: 0,
    cacheCreationTokens: 0,
    cacheReadTokens: 0,
  };
}

function normalizeModelID(model: string | undefined): string {
  return (model ?? "").trim().toLowerCase();
}

export {
  activePlanUpdateForThread,
  activeProjectID,
  activeSessionTab,
  activeThreadForState,
  activeThreadIDForState,
  activeTurnContextUsage,
  latestContextUsageForThread,
  activeTurnIDForThread,
  activeTurnTokenSpeed,
  activeTurnTokenSpeedSnapshot,
  turnStreamStatusForThread,
  agentFromRecord,
  appendStreamingTokenSample,
  appendTurnTokenSample,
  bindActiveSessionTabToThread,
  cloneComposerDraft,
  cloneSessionTabDraft,
  composerSubmissionDetail,
  conversationPaneThreadsByID,
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
  openForkThreadAsPrimary,
  parsePlanUpdateArguments,
  persistActiveSessionTabDraft,
  pinnedThreads,
  pinnedThreadSummaries,
  projectThreads,
  projectThreadSummaries,
  queryTextForUserItem,
  queryTextsForThread,
  reduceNotification,
  reduceServerEvent,
  removeSessionTab,
  replaceStreamText,
  requireThread,
  runtimeContextKey,
  sameRuntimeContext,
  serverEventShouldRefreshGit,
  serverEventTargetsActiveContext,
  sessionTabDraftForThread,
  sessionTabDraftForThreadID,
  sessionTabForLoadedRuntime,
  sessionTabLabel,
  setThreadForPane,
  skillsSessionTabID,
  sortThreads,
  summarizeThreadsForSidebar,
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
  upsertThread,
  upsertThreadChildAgent,
  upsertTurn,
  upsertTurnItem,
  withLoadedRuntimeSessionTab,
};

export type { AppState, ComposerDraftState, ConversationPaneID, SessionTab };
