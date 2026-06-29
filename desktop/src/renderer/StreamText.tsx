import { useSyncExternalStore } from "react";

/**
 * The set of fields on a thread item that can be streamed incrementally.
 * `text` is the visible assistant/user/reasoning body. `arguments` and
 * `result` carry the streamed JSON payload for tool calls.
 */
export type StreamTextField = "text" | "arguments" | "result";

type StreamTextListener = (value: string) => void;

export type StreamTextStats = {
  entryCount: number;
  valueEntryCount: number;
  listenerCount: number;
  pendingEntryCount: number;
  totalValueLength: number;
};

interface StreamEntry {
  /** The current accumulated text. */
  value: string;
  /** The text at the time the stream was first seeded. */
  seed: string;
  /** Whether this entry has an actual stream value, not only listeners. */
  hasValue: boolean;
  /** Latest value waiting to be published to subscribers on the next frame. */
  pendingValue: string | undefined;
  /** Subscribers for value changes. */
  valueListeners: Set<StreamTextListener>;
}

const STREAM_FIELD_KEYS = ["text", "arguments", "result"] as const satisfies readonly StreamTextField[];

/**
 * Append-only text store used by the renderer to coalesce server-streamed
 * deltas. Components read with `useSyncExternalStore`, which gives us
 * correct concurrent-mode tearing semantics and a single subscription per
 * component.
 */
class StreamTextStore {
  private entries = new Map<string, StreamEntry>();
  private dirtyEntries = new Set<StreamEntry>();
  private notifyScheduled = false;

  key(turnID: string, itemID: string, field: StreamTextField): string {
    return `${turnID}\u0000${itemID}\u0000${field}`;
  }

  has(key: string): boolean {
    return this.entries.get(key)?.hasValue ?? false;
  }

  get(key: string): string {
    const entry = this.entries.get(key);
    return entry?.hasValue ? entry.value : "";
  }

  seedValue(key: string): string {
    const entry = this.entries.get(key);
    return entry?.hasValue ? entry.seed : "";
  }

  /**
   * Seed an entry. Idempotent: subsequent seeds are no-ops so late-arriving
   * `item/started` notifications cannot clobber in-flight deltas.
   */
  seed(key: string, value: string): void {
    const existing = this.entries.get(key);
    if (existing?.hasValue) {
      return;
    }
    if (existing) {
      existing.value = value;
      existing.seed = value;
      existing.hasValue = true;
      this.notifyValue(existing, value);
      return;
    }
    const entry: StreamEntry = {
      value,
      seed: value,
      hasValue: true,
      pendingValue: undefined,
      valueListeners: new Set()
    };
    this.entries.set(key, entry);
    this.notifyValue(entry, value);
  }

  /** Replace the value at a key. Used by `*\/replace` events and sync. */
  set(key: string, value: string): void {
    this.ensureEntry(key, { hasValue: true });
    const entry = this.entries.get(key);
    if (!entry) {
      return;
    }
    const valueChanged = !entry.hasValue || entry.value !== value;
    entry.hasValue = true;
    if (!valueChanged) {
      return;
    }
    entry.value = value;
    this.notifyValue(entry, value);
  }

  /** Concatenate a delta. Used by `*\/delta` events. */
  append(key: string, delta: string): void {
    if (!delta) {
      return;
    }
    this.ensureEntry(key, { hasValue: true });
    const entry = this.entries.get(key);
    if (!entry) {
      return;
    }
    entry.hasValue = true;
    entry.value = `${entry.value}${delta}`;
    this.notifyValue(entry, entry.value);
  }

  clearItem(turnID: string, itemID: string): void {
    for (const field of STREAM_FIELD_KEYS) {
      this.clearField(turnID, itemID, field);
    }
  }

  clearField(turnID: string, itemID: string, field: StreamTextField): void {
    this.deleteEntry(this.key(turnID, itemID, field));
  }

  clearTurn(turnID: string): void {
    const prefix = `${turnID}\u0000`;
    for (const key of Array.from(this.entries.keys())) {
      if (key.startsWith(prefix)) {
        this.deleteEntry(key);
      }
    }
  }

  stats(): StreamTextStats {
    let valueEntryCount = 0;
    let listenerCount = 0;
    let pendingEntryCount = 0;
    let totalValueLength = 0;
    for (const entry of this.entries.values()) {
      if (entry.hasValue) {
        valueEntryCount += 1;
        totalValueLength += entry.value.length;
      }
      listenerCount += entry.valueListeners.size;
      if (entry.pendingValue !== undefined) {
        pendingEntryCount += 1;
      }
    }
    return {
      entryCount: this.entries.size,
      valueEntryCount,
      listenerCount,
      pendingEntryCount,
      totalValueLength,
    };
  }

  /**
   * Subscribe to value updates. Returns an unsubscribe function. Uses the
   * `useSyncExternalStore` snapshot/getServerState convention: we always
   * return the latest cached value, and React decides when to re-render.
   */
  subscribe(key: string, listener: StreamTextListener): () => void {
    this.ensureEntry(key);
    const entry = this.entries.get(key);
    if (!entry) {
      return () => undefined;
    }
    entry.valueListeners.add(listener);
    return () => {
      entry.valueListeners.delete(listener);
      this.deleteEntryIfIdle(key, entry);
    };
  }

  private ensureEntry(key: string, options?: { hasValue?: boolean }): void {
    const hasValue = options?.hasValue ?? false;
    if (this.entries.has(key)) {
      return;
    }
    this.entries.set(key, {
      value: "",
      seed: "",
      hasValue,
      pendingValue: undefined,
      valueListeners: new Set()
    });
  }

  private notifyValue(entry: StreamEntry, value: string): void {
    if (entry.valueListeners.size === 0) {
      return;
    }
    entry.pendingValue = value;
    this.dirtyEntries.add(entry);
    this.scheduleNotify();
  }

  private scheduleNotify(): void {
    if (this.notifyScheduled) {
      return;
    }
    this.notifyScheduled = true;
    const flush = (): void => {
      this.notifyScheduled = false;
      this.flushNotifications();
    };
    if (typeof window !== "undefined" && typeof window.requestAnimationFrame === "function") {
      window.requestAnimationFrame(flush);
      return;
    }
    globalThis.setTimeout(flush, 16);
  }

  private flushNotifications(): void {
    const entries = Array.from(this.dirtyEntries);
    this.dirtyEntries.clear();
    for (const entry of entries) {
      const value = entry.pendingValue ?? entry.value;
      entry.pendingValue = undefined;
      if (!entry.hasValue) {
        continue;
      }
      if (entry.valueListeners.size === 0) {
        continue;
      }
      this.notifyListeners(entry, value);
    }
  }

  private notifyListeners(entry: StreamEntry, value: string): void {
    for (const listener of entry.valueListeners) {
      listener(value);
    }
  }

  private deleteEntry(key: string): void {
    const entry = this.entries.get(key);
    if (!entry) {
      return;
    }
    this.dirtyEntries.delete(entry);
    entry.valueListeners.clear();
    this.entries.delete(key);
  }

  private deleteEntryIfIdle(key: string, entry: StreamEntry): void {
    if (entry.hasValue || entry.valueListeners.size > 0) {
      return;
    }
    this.dirtyEntries.delete(entry);
    this.entries.delete(key);
  }
}

export const streamTextStore = new StreamTextStore();

export function streamTextKey(turnID: string, itemID: string, field: StreamTextField): string {
  return streamTextStore.key(turnID, itemID, field);
}

/**
 * Hook that subscribes to a stream key. Returns the latest value or
 * `initialValue` if nothing has been written yet.
 */
export function useStreamedText(streamKey: string, initialValue = ""): string {
  return useSyncExternalStore(
    (listener) => streamTextStore.subscribe(streamKey, listener),
    () => streamTextStore.has(streamKey) ? streamTextStore.get(streamKey) : initialValue,
    () => streamTextStore.has(streamKey) ? streamTextStore.get(streamKey) : initialValue
  );
}
