// 侧聊（SideThread）的渲染端状态模块。
//
// Side thread 的事实源有两个：协议层（SideThreadSummary /
// SideThreadMessage / SideThreadEvent）和本模块维护的渲染端状态。
// 协议对象是不可变的快照；本模块只保存 UI 关心的可变字段（展开
// 状态、草稿、流式标记、宽度），并提供与协议事件对接的 reducer。
//
// 设计依据：docs/side-thread.md §5「打开与聚焦」、§8「生命周期」、
// §9「侧聊头部」。V1 一个 main thread 最多对应一个 side thread。

import type {
  SideThreadEvent,
  SideThreadMessage,
  SideThreadStatus,
  SideThreadSummary,
} from "../shared/protocol";

// 每个 main thread 对应的侧聊状态。Record<string, SideThreadState>
// 作为顶层索引，懒创建。
export type SideThreadEntryState = {
  // 侧聊面板是否在该 Tab 内展开。收起只隐藏分栏，不清空历史，
  // 不停止主任务。再次执行 /side 时恢复。
  open: boolean;
  // 后端侧聊摘要。null 表示「该 main thread 尚未发送过任何侧聊
  // 消息，后端还没有真正创建 side thread」（懒初始化，参见设计 §5）。
  summary: SideThreadSummary | null;
  // 本地维护的侧聊消息列表。后端是事实源，本地是视图缓存；事件流
  // 通过 reducer 增量更新。
  messages: SideThreadMessage[];
  // 侧聊输入框的草稿文本。收起 / 切换 Tab 都不清空，回到该 Tab
  // 仍能看到上次未发送的内容。
  draft: string;
  // 当前是否在接收后端的流式响应。仅用来决定侧聊面板自身的指示
  // 器，不会锁住主对话。
  streaming: boolean;
  // 最近一次侧聊错误。显示在侧聊面板内部，绝不传到主对话。
  lastError?: string;
};

export const SIDE_THREAD_DEFAULT_WIDTH = 400;
export const SIDE_THREAD_MIN_WIDTH = 320;
export const SIDE_THREAD_MAX_WIDTH = 640;

// 顶层 store：按 main thread id 索引；附加 panel 宽度与一个
// side_thread_id → main_thread_id 的反向索引（用于把后端事件路
// 由回正确的面板）。
export type SideThreadStoreState = {
  byThread: Record<string, SideThreadEntryState>;
  sideIdToMain: Record<string, string>;
  width: number;
};

export function createEmptySideThreadEntry(): SideThreadEntryState {
  return {
    open: false,
    summary: null,
    messages: [],
    draft: "",
    streaming: false
  };
}

export function createInitialSideThreadStore(
  width: number = SIDE_THREAD_DEFAULT_WIDTH
): SideThreadStoreState {
  return {
    byThread: {},
    sideIdToMain: {},
    width: clampSideThreadWidth(width)
  };
}

export function clampSideThreadWidth(value: number): number {
  if (!Number.isFinite(value)) {
    return SIDE_THREAD_DEFAULT_WIDTH;
  }
  if (value < SIDE_THREAD_MIN_WIDTH) {
    return SIDE_THREAD_MIN_WIDTH;
  }
  if (value > SIDE_THREAD_MAX_WIDTH) {
    return SIDE_THREAD_MAX_WIDTH;
  }
  return Math.round(value);
}

// 工具：从 store 中按 main thread id 取状态，没有就返回 undefined
// （调用方负责按需 createEmptySideThreadEntry）。
export function getSideThreadEntry(
  store: SideThreadStoreState,
  mainThreadId: string
): SideThreadEntryState | undefined {
  return store.byThread[mainThreadId];
}

export function ensureSideThreadEntry(
  store: SideThreadStoreState,
  mainThreadId: string
): { store: SideThreadStoreState; entry: SideThreadEntryState } {
  const existing = store.byThread[mainThreadId];
  if (existing) {
    return { store, entry: existing };
  }
  const entry = createEmptySideThreadEntry();
  return {
    store: {
      ...store,
      byThread: { ...store.byThread, [mainThreadId]: entry }
    },
    entry
  };
}

// ============================================================================
// Action 集合
// ============================================================================

export type SideThreadAction =
  | { type: "open"; mainThreadId: string }
  | { type: "close"; mainThreadId: string }
  | { type: "toggle"; mainThreadId: string }
  | { type: "setDraft"; mainThreadId: string; draft: string }
  | { type: "setSummary"; mainThreadId: string; summary: SideThreadSummary | null }
  | { type: "appendMessage"; mainThreadId: string; message: SideThreadMessage }
  | {
      type: "updateMessage";
      mainThreadId: string;
      messageId: string;
      patch: Partial<SideThreadMessage>;
    }
  | { type: "setStreaming"; mainThreadId: string; streaming: boolean }
  | { type: "setError"; mainThreadId: string; error: string | undefined }
  | { type: "applyEvent"; event: SideThreadEvent }
  | { type: "setWidth"; width: number }
  // 主 thread 被删除 / 切换走时调用。删除会同时丢掉侧聊消息和草
  // 稿；toggle / close 不走这里。
  | { type: "dropThread"; mainThreadId: string };

// ============================================================================
// 主 reducer
// ============================================================================

export function reduceSideThreadStore(
  store: SideThreadStoreState,
  action: SideThreadAction
): SideThreadStoreState {
  switch (action.type) {
    case "open":
      return updateEntry(store, action.mainThreadId, (entry) => ({
        ...entry,
        open: true
      }));
    case "close":
      return updateEntry(store, action.mainThreadId, (entry) => ({
        ...entry,
        open: false
      }));
    case "toggle":
      return updateEntry(store, action.mainThreadId, (entry) => ({
        ...entry,
        open: !entry.open
      }));
    case "setDraft":
      return updateEntry(store, action.mainThreadId, (entry) => ({
        ...entry,
        draft: action.draft
      }));
    case "setSummary":
      return applySetSummary(store, action.mainThreadId, action.summary);
    case "appendMessage":
      return updateEntry(store, action.mainThreadId, (entry) => ({
        ...entry,
        messages: [...entry.messages, action.message]
      }));
    case "updateMessage":
      return updateEntry(store, action.mainThreadId, (entry) => ({
        ...entry,
        messages: entry.messages.map((m) =>
          m.id === action.messageId ? { ...m, ...action.patch } : m
        )
      }));
    case "setStreaming":
      return updateEntry(store, action.mainThreadId, (entry) => ({
        ...entry,
        streaming: action.streaming
      }));
    case "setError":
      return updateEntry(store, action.mainThreadId, (entry) => ({
        ...entry,
        lastError: action.error
      }));
    case "applyEvent":
      return applySideThreadEvent(store, action.event);
    case "setWidth":
      return { ...store, width: clampSideThreadWidth(action.width) };
    case "dropThread":
      return dropSideThread(store, action.mainThreadId);
    default:
      return store;
  }
}

// ============================================================================
// 内部辅助
// ============================================================================

function updateEntry(
  store: SideThreadStoreState,
  mainThreadId: string,
  fn: (entry: SideThreadEntryState) => SideThreadEntryState
): SideThreadStoreState {
  const ensured = ensureSideThreadEntry(store, mainThreadId);
  const nextEntry = fn(ensured.entry);
  if (nextEntry === ensured.entry) {
    return ensured.store;
  }
  return {
    ...ensured.store,
    byThread: {
      ...ensured.store.byThread,
      [mainThreadId]: nextEntry
    }
  };
}

function applySetSummary(
  store: SideThreadStoreState,
  mainThreadId: string,
  summary: SideThreadSummary | null
): SideThreadStoreState {
  const ensured = ensureSideThreadEntry(store, mainThreadId);
  const entry: SideThreadEntryState = { ...ensured.entry, summary };
  let nextSideIdToMain = ensured.store.sideIdToMain;
  if (summary) {
    nextSideIdToMain = {
      ...ensured.store.sideIdToMain,
      [summary.side_thread_id]: mainThreadId
    };
  }
  return {
    ...ensured.store,
    byThread: { ...ensured.store.byThread, [mainThreadId]: entry },
    sideIdToMain: nextSideIdToMain
  };
}

function dropSideThread(
  store: SideThreadStoreState,
  mainThreadId: string
): SideThreadStoreState {
  const entry = store.byThread[mainThreadId];
  if (!entry) {
    return store;
  }
  const { [mainThreadId]: _removed, ...restByThread } = store.byThread;
  let nextSideIdToMain = store.sideIdToMain;
  if (entry.summary) {
    const { [entry.summary.side_thread_id]: _bySideId, ...restSideIdToMain } =
      store.sideIdToMain;
    nextSideIdToMain = restSideIdToMain;
  }
  return {
    ...store,
    byThread: restByThread,
    sideIdToMain: nextSideIdToMain
  };
}

function applySideThreadEvent(
  store: SideThreadStoreState,
  event: SideThreadEvent
): SideThreadStoreState {
  const mainThreadId = event.main_thread_id;
  const nextSideIdToMain =
    store.sideIdToMain[event.side_thread_id] === mainThreadId
      ? store.sideIdToMain
      : { ...store.sideIdToMain, [event.side_thread_id]: mainThreadId };

  const baseStore: SideThreadStoreState = {
    ...store,
    sideIdToMain: nextSideIdToMain
  };

  switch (event.type) {
    case "status": {
      const statusPatch: Partial<SideThreadEntryState> = {
        streaming: isStreamingStatus(event.status),
        summary: mergeSummaryStatus(
          baseStore.byThread[mainThreadId]?.summary ?? null,
          event.status,
          event.main_task_summary
        )
      };
      return updateEntry(baseStore, mainThreadId, (entry) => ({
        ...entry,
        ...statusPatch,
        // status 事件携带的最新摘要一定是真相源，清掉旧 error。
        lastError: undefined
      }));
    }
    case "delta": {
      const ensured = ensureSideThreadEntry(baseStore, mainThreadId);
      return updateEntry(ensured.store, mainThreadId, (entry) => {
        const existingIndex = entry.messages.findIndex(
          (m) => m.id === event.message_id
        );
        if (existingIndex < 0) {
          const fresh: SideThreadMessage = {
            id: event.message_id,
            side_thread_id: event.side_thread_id,
            role: "assistant",
            text: event.text_delta,
            status: "streaming",
            created_at: new Date().toISOString()
          };
          return {
            ...entry,
            streaming: true,
            messages: [...entry.messages, fresh]
          };
        }
        return {
          ...entry,
          streaming: true,
          messages: entry.messages.map((m, index) =>
            index === existingIndex
              ? {
                  ...m,
                  text: m.text + event.text_delta,
                  status: "streaming"
                }
              : m
          )
        };
      });
    }
    case "message": {
      const ensured = ensureSideThreadEntry(baseStore, mainThreadId);
      return updateEntry(ensured.store, mainThreadId, (entry) => {
        const existingIndex = entry.messages.findIndex(
          (m) => m.id === event.message.id
        );
        if (existingIndex < 0) {
          return {
            ...entry,
            streaming: false,
            messages: [...entry.messages, event.message]
          };
        }
        return {
          ...entry,
          streaming: false,
          messages: entry.messages.map((m, index) =>
            index === existingIndex ? event.message : m
          )
        };
      });
    }
    case "error": {
      return updateEntry(baseStore, mainThreadId, (entry) => ({
        ...entry,
        streaming: false,
        lastError: event.error_message,
        messages: entry.messages.map((m) =>
          m.id === event.message_id
            ? { ...m, status: "failed", error_message: event.error_message }
            : m
        )
      }));
    }
    default:
      return baseStore;
  }
}

function isStreamingStatus(status: SideThreadStatus): boolean {
  return status === "running";
}

function mergeSummaryStatus(
  current: SideThreadSummary | null,
  status: SideThreadStatus,
  mainTaskSummary: SideThreadSummary["main_task_summary"] | undefined
): SideThreadSummary | null {
  if (!current) {
    // status 事件先于 setSummary 到达时无法构造完整摘要；保留
    // null，等真正的 setSummary 落地。
    return null;
  }
  return {
    ...current,
    status,
    main_task_summary: mainTaskSummary ?? current.main_task_summary,
    updated_at: new Date().toISOString()
  };
}