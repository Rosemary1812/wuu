import { Copy, X } from "lucide-react";
import { type ReactNode, useEffect, useRef, useState } from "react";
import type {
  AppServerNotification,
  ServerEvent,
  Thread,
  ThreadItem,
  Turn,
} from "../shared/protocol";
import type {
  ComposerFile,
  ComposerImage,
  QueuedComposerMessage,
} from "./ComposerMessages";
import {
  activeThreadForState,
  threadFromRecord,
  threadItemFromRecord,
  turnFromRecord,
  type AppState,
} from "./AppState";
import {
  streamTextKey,
  streamTextStore,
  type StreamTextField,
} from "./StreamText";
import { streamFieldValue } from "./ThreadItemText";
import {
  isRecord,
  numberValue,
  readableToolName,
  recordValue,
  stringValue,
  type JsonRecord,
} from "./ToolActivity";
import { LiveDuration, formatDuration } from "./TurnProgress";
import {
  formatCurrentDate,
  formatCurrentNumber,
  resolveLocalizedText,
  translateCurrent as t,
  useI18n,
} from "./i18n";

type RunDebugEventSource = "client" | "server";
type RunDebugEventTone = "info" | "running" | "success" | "warning" | "error";
type RunDebugPhaseTone = "idle" | "running" | "success" | "warning" | "error";

export type RunDebugEvent = {
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

export type RunDebugPhase = {
  label: string;
  detail: string;
  tone: RunDebugPhaseTone;
  turn?: Turn;
  activeItem?: ThreadItem;
};

export function RunDebugPanel({
  state,
  phase,
  events,
  queuedMessages,
  guideMessages,
  composerImages,
  composerFiles,
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
  composerFiles: ComposerFile[];
  copied: boolean;
  onCopy: () => void;
  onClose: () => void;
}): JSX.Element {
  useI18n();
  const thread = activeThreadForState(state);
  const turn = phase.turn ?? activeDebugTurn(thread);
  const lastEvent = events.length > 0 ? events[events.length - 1] : undefined;
  const turnStartedAt = turn ? parseTurnTimestampMs(turn.started_at) : NaN;
  const model = state.initialized
    ? `${state.initialized.provider} / ${state.initialized.model}${state.initialized.variant || state.initialized.effort ? ` / ${state.initialized.variant || state.initialized.effort}` : ""}`
    : t("runDebug.notInitialized");
  const queueDetail = [
    queuedMessages.length > 0
      ? t("runDebug.queuedCount", { count: formatCurrentNumber(queuedMessages.length) })
      : "",
    guideMessages.length > 0
      ? t("runDebug.guideCount", { count: formatCurrentNumber(guideMessages.length) })
      : "",
    composerImages.length > 0
      ? t("runDebug.imageCount", { count: formatCurrentNumber(composerImages.length) })
      : "",
    composerFiles.length > 0
      ? t("runDebug.fileCount", { count: formatCurrentNumber(composerFiles.length) })
      : "",
  ]
    .filter(Boolean)
    .join(t("runDebug.listSeparator"));
  const streamStats = streamTextStore.stats();

  return (
    <aside className="run-debug-panel" aria-label={t("runDebug.label")}>
      <div className="run-debug-header">
        <div>
          <span className={`run-debug-phase ${phase.tone}`}>{phase.label}</span>
          <strong>{phase.detail}</strong>
        </div>
        <div className="run-debug-actions">
          <button
            className="icon-button"
            type="button"
            aria-label={t("runDebug.copy")}
            onClick={onCopy}
          >
            <Copy className="icon" />
          </button>
          <button
            className="icon-button"
            type="button"
            aria-label={t("runDebug.close")}
            onClick={onClose}
          >
            <X className="icon" />
          </button>
        </div>
      </div>

      <div className="run-debug-scroll">
        {copied ? (
          <div className="run-debug-copied">{t("runDebug.copied")}</div>
        ) : null}
        <section className="run-debug-section">
          <h3>{t("runDebug.currentStatus")}</h3>
          <RunDebugRow
            label={t("runDebug.running")}
            value={
              state.running
                ? t("runDebug.runtimeStatus.running")
                : debugRuntimeStatusLabel(state.status || "ready")
            }
          />
          <RunDebugRow label={t("runDebug.model")} value={model} />
          <RunDebugRow
            label={t("runDebug.workspace")}
            value={state.activeContext?.cwd ?? thread?.cwd ?? t("runDebug.notConnected")}
          />
          <RunDebugRow
            label={t("runDebug.thread")}
            value={thread ? shortDebugID(thread.id) : t("runDebug.none")}
          />
          <RunDebugRow
            label={t("runDebug.turn")}
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
                    t("runDebug.unknownDuration")
                  )}
                </>
              ) : (
                t("runDebug.none")
              )
            }
          />
          <RunDebugRow
            label={t("runDebug.lastEvent")}
            value={
              lastEvent ? (
                <>
                  {lastEvent.method} · <LiveSince atMs={lastEvent.at} />
                </>
              ) : (
                t("runDebug.notAvailable")
              )
            }
          />
          {queueDetail ? (
            <RunDebugRow label={t("runDebug.pendingSend")} value={queueDetail} />
          ) : null}
          <RunDebugRow
            label={t("runDebug.streamCache")}
            value={t("runDebug.streamStats", {
              values: formatCurrentNumber(streamStats.valueEntryCount),
              entries: formatCurrentNumber(streamStats.entryCount),
              listeners: formatCurrentNumber(streamStats.listenerCount),
              chars: formatCurrentNumber(streamStats.totalValueLength),
            })}
          />
        </section>

        <section className="run-debug-section">
          <h3>{t("runDebug.turnItems")}</h3>
          {turn?.items.length ? (
            <div className="run-debug-items">
              {turn.items.map((item) => (
                <RunDebugItem key={item.id} turnID={turn.id} item={item} />
              ))}
            </div>
          ) : (
            <div className="run-debug-empty">{t("runDebug.noTurnItems")}</div>
          )}
        </section>

        <section className="run-debug-section">
          <h3>{t("runDebug.eventTimeline")}</h3>
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
            <div className="run-debug-empty">{t("runDebug.noEvents")}</div>
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
          label={t("runDebug.field.text")}
        />
        <DebugFieldLength
          turnID={turnID}
          item={item}
          field="arguments"
          label={t("runDebug.field.arguments")}
        />
        <DebugFieldLength
          turnID={turnID}
          item={item}
          field="result"
          label={t("runDebug.field.result")}
        />
        {item.error ? (
          <span className="error" title={item.error}>
            {t("runDebug.field.error")}: {shortDebugError(item.error)}
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
      {label} {formatCurrentNumber(length)}
    </span>
  );
}

function LiveSince({ atMs }: { atMs: number }): JSX.Element {
  const nodeRef = useRef<HTMLSpanElement | null>(null);

  useEffect(() => {
    const update = (): void => {
      if (nodeRef.current) {
        nodeRef.current.textContent = t("runDebug.durationAgo", {
          duration: formatDuration(Date.now() - atMs),
        });
      }
    };
    update();
    const timer = window.setInterval(update, 1000);
    return () => window.clearInterval(timer);
  }, [atMs]);

  return (
    <span ref={nodeRef}>
      {t("runDebug.durationAgo", { duration: formatDuration(Date.now() - atMs) })}
    </span>
  );
}

export function runDebugPhaseForState(state: AppState): RunDebugPhase {
  const thread = activeThreadForState(state);
  const turn = activeDebugTurn(thread);
  if (!state.initialized) {
    return {
      label: t("runDebug.runtimeNotReady"),
      detail: state.status
        ? debugRuntimeStatusLabel(state.status)
        : t("runDebug.waitingInitialization"),
      tone:
        state.status === "connecting" || state.status === "opening"
          ? "running"
          : "warning",
      turn,
    };
  }
  if (state.running && !turn) {
    return {
      label: t("runDebug.sendingRequest"),
      detail: t("runDebug.noTurnStarted"),
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
        label: t("runDebug.callingTool"),
        detail: readableToolName(runningTool.name),
        tone: "running",
        turn,
        activeItem: runningTool,
      };
    }

    const latestItem = latestDebugItem(turn);
    if (!latestItem) {
      return {
        label: t("runDebug.waitingModel"),
        detail: t("runDebug.turnStartedNoReply"),
        tone: "running",
        turn,
      };
    }
    if (latestItem.type === "agent_message") {
      const length = debugStreamFieldLength(turn.id, latestItem, "text");
      return {
        label:
          length > 0
            ? t("runDebug.generatingReply")
            : t("runDebug.replyStarted"),
        detail:
          length > 0
            ? t("runDebug.receivedChars", { count: formatCurrentNumber(length) })
            : t("runDebug.waitingFirstReply"),
        tone: "running",
        turn,
        activeItem: latestItem,
      };
    }
    if (latestItem.type === "reasoning") {
      const length = debugStreamFieldLength(turn.id, latestItem, "text");
      return {
        label: t("runDebug.modelThinking"),
        detail:
          length > 0
            ? t("runDebug.receivedReasoningChars", {
                count: formatCurrentNumber(length),
              })
            : t("runDebug.waitingReasoning"),
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
        label: t("runDebug.toolReturned"),
        detail: t("runDebug.waitingToolResultProcessing"),
        tone: "running",
        turn,
        activeItem: latestItem,
      };
    }
    return {
      label: t("runDebug.turnProcessing"),
      detail: debugItemTitle(latestItem),
      tone: "running",
      turn,
      activeItem: latestItem,
    };
  }
  if (turn?.status === "failed") {
    return {
      label: t("runDebug.processingFailed"),
      detail: turn.error?.message ?? t("runDebug.turnFailedStatus"),
      tone: "error",
      turn,
    };
  }
  if (turn?.status === "interrupted") {
    return {
      label: t("runDebug.stopped"),
      detail: t("runDebug.turnInterrupted"),
      tone: "warning",
      turn,
    };
  }
  if (turn?.status === "completed") {
    return {
      label: t("runDebug.completed"),
      detail:
        turn.duration_ms === undefined
          ? t("runDebug.turnCompleted")
          : t("runDebug.duration", { duration: formatDuration(turn.duration_ms) }),
      tone: "success",
      turn,
    };
  }
  if (state.running) {
    return {
      label: t("runDebug.inProgress"),
      detail: state.status
        ? debugRuntimeStatusLabel(state.status)
        : t("runDebug.waitingEvents"),
      tone: "running",
      turn,
    };
  }
  return {
    label:
      state.status === "ready"
        ? t("runDebug.idle")
        : t("runDebug.currentStatus"),
    detail:
      state.status === "ready"
        ? t("runDebug.canSend")
        : debugRuntimeStatusLabel(state.status),
    tone: state.status === "ready" ? "idle" : "warning",
    turn,
  };
}

function debugRuntimeStatusLabel(status: string): string {
  switch (status) {
    case "ready":
      return t("runDebug.runtimeStatus.ready");
    case "connecting":
      return t("runDebug.runtimeStatus.connecting");
    case "opening":
      return t("runDebug.runtimeStatus.opening");
    default:
      return resolveLocalizedText(status);
  }
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

export function latestDebugItem(turn: Turn): ThreadItem | undefined {
  for (let index = turn.items.length - 1; index >= 0; index--) {
    const item = turn.items[index];
    if (item.type !== "user_message") {
      return item;
    }
  }
  return undefined;
}

export function debugStreamFieldLength(
  turnID: string,
  item: ThreadItem,
  field: StreamTextField,
): number {
  return streamFieldValue(turnID, item, field).length;
}

export function runDebugEventFromServerEvent(
  event: ServerEvent,
  deltaSeen: Set<string>,
): Omit<RunDebugEvent, "id" | "at"> | undefined {
  switch (event.kind) {
    case "server-request":
      return {
        source: "server",
        method: event.message.method,
        detail: t("runDebug.serverWaitingClient"),
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
        detail: t("runDebug.appServerExited", { code: event.code ?? "unknown" }),
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
      detail: t("runDebug.firstDelta", {
        count: formatCurrentNumber(delta.length),
      }),
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
      detail: turn
        ? t("runDebug.turnStartedNamed", { id: shortDebugID(turn.id) })
        : t("runDebug.turnStarted"),
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
        ? (stringValue(params, "error") ?? t("runDebug.turnFailed"))
        : t("runDebug.turnCompleted"),
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
      detail: thread
        ? t("runDebug.threadNamed", { id: shortDebugID(thread.id) })
        : t("runDebug.threadUpdated"),
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
	const lifecycle = recordValue(payload, "lifecycle");
	const workflow = recordValue(lifecycle, "workflow");
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
	if (lifecycle) {
		const phase = stringValue(lifecycle, "phase") ?? "unknown";
		const attempt = numberValue(lifecycle, "attempt") ?? 0;
		const maxAttempts = numberValue(lifecycle, "max_attempts") ?? 0;
		if (!workflow) {
			return `${phase} · attempt ${attempt}/${maxAttempts}`;
		}
		const operations = numberValue(workflow, "operations") ?? 0;
		const attempts = numberValue(workflow, "attempts") ?? 0;
		const submissions = numberValue(workflow, "submissions") ?? 0;
		const recoveries =
			(numberValue(workflow, "same_payload_replays") ?? 0) +
			(numberValue(workflow, "transport_switches") ?? 0) +
			(numberValue(workflow, "credential_refreshes") ?? 0) +
			(numberValue(workflow, "payload_transforms") ?? 0);
		const waitMS = numberValue(workflow, "recovery_wait_ms") ?? 0;
		const known = numberValue(workflow, "known_submissions") ?? 0;
		const estimated = numberValue(workflow, "estimated_submissions") ?? 0;
		const unknown = numberValue(workflow, "unknown_billable_submissions") ?? 0;
		return `${phase} · attempt ${attempt}/${maxAttempts} · workflow op=${operations} att=${attempts} sub=${submissions} recovery=${recoveries} wait=${waitMS}ms cost=${known}/${estimated}/${unknown}`;
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

function debugItemTitle(item: ThreadItem): string {
  switch (item.type) {
    case "user_message":
      return t("runDebug.item.userMessage");
    case "agent_message":
      return t("runDebug.item.reply");
    case "reasoning":
      return t("runDebug.item.reasoning");
    case "tool_call":
    case "collab_agent_tool_call":
      return t("runDebug.item.tool", { tool: readableToolName(item.name) });
    case "context_compaction":
      return t("runDebug.item.compaction");
    case "error":
      return t("runDebug.item.error");
    default:
      return item.type;
  }
}

function debugItemStatusLabel(item: ThreadItem): string {
  if (item.status === "failed" || item.error) {
    return t("runDebug.status.failed");
  }
  if ((item.status ?? "in_progress") === "in_progress") {
    return t("runDebug.status.inProgress");
  }
  return t("runDebug.status.completed");
}

function debugTurnStatusLabel(status: Turn["status"]): string {
  switch (status) {
    case "in_progress":
      return t("runDebug.status.inProgress");
    case "completed":
      return t("runDebug.status.completed");
    case "failed":
      return t("runDebug.status.failed");
    case "interrupted":
      return t("runDebug.status.stopped");
  }
}

function shortDebugID(id: string): string {
  if (id.length <= 12) {
    return id;
  }
  return `${id.slice(0, 6)}…${id.slice(-4)}`;
}

function formatDebugTime(atMs: number): string {
  return formatCurrentDate(atMs, {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

export function buildRunDebugSnapshot({
  state,
  events,
  queuedMessages,
  guideMessages,
  composerImages,
  composerFiles,
}: {
  state: AppState;
  events: RunDebugEvent[];
  queuedMessages: QueuedComposerMessage[];
  guideMessages: QueuedComposerMessage[];
  composerImages: ComposerImage[];
  composerFiles: ComposerFile[];
}): string {
  const phase = runDebugPhaseForState(state);
  const thread = activeThreadForState(state);
  const turn = phase.turn ?? activeDebugTurn(thread);
  const streamStats = streamTextStore.stats();
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
    `composer_files: ${composerFiles.length}`,
    `stream_cache_entries: ${streamStats.entryCount}`,
    `stream_cache_value_entries: ${streamStats.valueEntryCount}`,
    `stream_cache_listeners: ${streamStats.listenerCount}`,
    `stream_cache_chars: ${streamStats.totalValueLength}`,
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

export function parseTurnTimestampMs(value: string | null | undefined): number {
  if (!value) {
    return NaN;
  }
  return Date.parse(value);
}
