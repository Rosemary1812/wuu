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
  readableToolName,
  recordValue,
  stringValue,
  type JsonRecord,
} from "./ToolActivity";
import { LiveDuration, formatDuration } from "./TurnProgress";

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
    composerFiles.length > 0 ? `文件 ${composerFiles.length}` : "",
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
            <Copy className="icon" />
          </button>
          <button
            className="icon-button"
            type="button"
            aria-label="关闭调试信息"
            onClick={onClose}
          >
            <X className="icon" />
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

export function runDebugPhaseForState(state: AppState): RunDebugPhase {
  const thread = activeThreadForState(state);
  const turn = activeDebugTurn(thread);
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
