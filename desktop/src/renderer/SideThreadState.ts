// Renderer state for the single side thread attached to each main thread.

import type {
  SideThreadEvent,
  SideThreadMessage,
  SideThreadSummary,
} from "../shared/protocol";

export type SideThreadEntryState = {
  // Closing the panel preserves history and draft for this main thread.
  open: boolean;
  // Null means no side thread has been persisted yet.
  summary: SideThreadSummary | null;
  messages: SideThreadMessage[];
  draft: string;
  streaming: boolean;
  lastError?: string;
};

export const SIDE_THREAD_DEFAULT_WIDTH = 400;
export const SIDE_THREAD_MIN_WIDTH = 320;
export const SIDE_THREAD_MAX_WIDTH = 640;

export type SideThreadStoreState = {
  byThread: Record<string, SideThreadEntryState>;
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

export type SideThreadAction =
  | { type: "open"; mainThreadId: string }
  | { type: "close"; mainThreadId: string }
  | { type: "setDraft"; mainThreadId: string; draft: string }
  | { type: "mergeSummary"; mainThreadId: string; summary: SideThreadSummary }
  | { type: "appendMessage"; mainThreadId: string; message: SideThreadMessage }
  | {
      type: "updateMessage";
      mainThreadId: string;
      messageId: string;
      patch: Partial<SideThreadMessage>;
    }
  | { type: "removeMessage"; mainThreadId: string; messageId: string }
  | {
      type: "mergeHistory";
      mainThreadId: string;
      summary: SideThreadSummary;
      messages: SideThreadMessage[];
    }
  | { type: "setStreaming"; mainThreadId: string; streaming: boolean }
  | { type: "setError"; mainThreadId: string; error: string | undefined }
  | { type: "applyEvent"; event: SideThreadEvent }
  | { type: "setWidth"; width: number };

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
    case "setDraft":
      return updateEntry(store, action.mainThreadId, (entry) => ({
        ...entry,
        draft: action.draft
      }));
    case "mergeSummary":
      return mergeSummarySnapshot(store, action.mainThreadId, action.summary);
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
    case "removeMessage":
      return updateEntry(store, action.mainThreadId, (entry) => ({
        ...entry,
        messages: entry.messages.filter((message) => message.id !== action.messageId)
      }));
    case "mergeHistory":
      return mergeHistorySnapshot(
        store,
        action.mainThreadId,
        action.summary,
        action.messages
      );
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
    default:
      return store;
  }
}

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

function mergeHistorySnapshot(
  store: SideThreadStoreState,
  mainThreadId: string,
  summary: SideThreadSummary,
  messages: SideThreadMessage[]
): SideThreadStoreState {
  return updateEntry(store, mainThreadId, (entry) => {
    const mergedSummary = newerSummary(entry.summary, summary);
    const mergedMessages = mergeMessages(messages, entry.messages);
    return {
      ...entry,
      summary: mergedSummary,
      messages: mergedMessages,
      streaming: mergedSummary.status === "running"
    };
  });
}

function mergeSummarySnapshot(
  store: SideThreadStoreState,
  mainThreadId: string,
  summary: SideThreadSummary
): SideThreadStoreState {
  return updateEntry(store, mainThreadId, (entry) => {
    const mergedSummary = newerSummary(entry.summary, summary);
    return {
      ...entry,
      summary: mergedSummary,
      streaming: mergedSummary.status === "running"
    };
  });
}

function newerSummary(
  current: SideThreadSummary | null,
  incoming: SideThreadSummary
): SideThreadSummary {
  if (current && current.revision !== incoming.revision) {
    return current.revision > incoming.revision ? current : incoming;
  }
  return current && Date.parse(current.updated_at) > Date.parse(incoming.updated_at)
    ? current
    : incoming;
}

function mergeMessages(
  persisted: SideThreadMessage[],
  local: SideThreadMessage[]
): SideThreadMessage[] {
  const localById = new Map(local.map((message) => [message.id, message]));
  const merged = persisted.map((message) => {
    const localMessage = localById.get(message.id);
    if (!localMessage) {
      return message;
    }
    if (isTerminalMessage(message) && !isTerminalMessage(localMessage)) {
      return message;
    }
    return localMessage;
  });
  const persistedIds = new Set(persisted.map((message) => message.id));
  for (const message of local) {
    if (!persistedIds.has(message.id)) {
      merged.push(message);
    }
  }
  return merged;
}

function isTerminalMessage(message: SideThreadMessage): boolean {
  return (
    message.status === "completed" ||
    message.status === "failed" ||
    message.status === "interrupted"
  );
}

function applySideThreadEvent(
  store: SideThreadStoreState,
  event: SideThreadEvent
): SideThreadStoreState {
  const mainThreadId = event.main_thread_id;

  switch (event.type) {
    case "status": {
      return updateEntry(store, mainThreadId, (entry) => {
        const summary = newerSummary(entry.summary, event.summary);
        const terminalStatus: SideThreadMessage["status"] =
          summary.status === "running" ? undefined : summary.status;
        const messages =
          terminalStatus === undefined
            ? entry.messages
            : entry.messages.map((message) =>
                message.role === "assistant" && message.status === "streaming"
                  ? { ...message, status: terminalStatus }
                  : message
              );
        return {
          ...entry,
          summary,
          messages,
          streaming: summary.status === "running",
          lastError: summary.status === "failed" ? entry.lastError : undefined
        };
      });
    }
    case "delta": {
      const ensured = ensureSideThreadEntry(store, mainThreadId);
      return updateEntry(ensured.store, mainThreadId, (entry) => {
        if (
          entry.summary &&
          (event.revision < entry.summary.revision ||
            (event.revision === entry.summary.revision &&
              entry.summary.status !== "running"))
        ) {
          return entry;
        }
        const existingIndex = entry.messages.findIndex(
          (m) => m.id === event.message_id
        );
        if (existingIndex >= 0 && isTerminalMessage(entry.messages[existingIndex])) {
          return entry;
        }
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
      const ensured = ensureSideThreadEntry(store, mainThreadId);
      return updateEntry(ensured.store, mainThreadId, (entry) => {
        if (entry.summary && event.revision < entry.summary.revision) {
          return entry;
        }
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
      return updateEntry(store, mainThreadId, (entry) => {
        if (entry.summary && event.revision < entry.summary.revision) {
          return entry;
        }
        return {
          ...entry,
          streaming: false,
          lastError: event.error_message,
          messages: entry.messages.map((m) =>
            m.id === event.message_id
              ? { ...m, status: "failed", error_message: event.error_message }
              : m
          )
        };
      });
    }
    default:
      return store;
  }
}
