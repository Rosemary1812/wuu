import { useEffect, useRef } from "react";

export type StreamTextField = "text" | "arguments" | "result";

type StreamTextListener = (value: string) => void;

class StreamTextStore {
  private values = new Map<string, string>();
  private seeds = new Map<string, string>();
  private listeners = new Map<string, Set<StreamTextListener>>();

  key(turnID: string, itemID: string, field: StreamTextField): string {
    return `${turnID}\u0000${itemID}\u0000${field}`;
  }

  get(key: string): string {
    return this.values.get(key) ?? "";
  }

  has(key: string): boolean {
    return this.values.has(key);
  }

  seedValue(key: string): string {
    return this.seeds.get(key) ?? "";
  }

  seed(key: string, value: string): void {
    if (this.values.has(key)) {
      return;
    }
    this.values.set(key, value);
    this.seeds.set(key, value);
  }

  set(key: string, value: string): void {
    this.ensureSeed(key);
    this.values.set(key, value);
    this.notify(key, value);
  }

  append(key: string, delta: string): void {
    if (!delta) {
      return;
    }
    this.ensureSeed(key);
    const value = `${this.values.get(key) ?? ""}${delta}`;
    this.values.set(key, value);
    this.notify(key, value);
  }

  clearItem(turnID: string, itemID: string): void {
    for (const field of ["text", "arguments", "result"] satisfies StreamTextField[]) {
      const key = this.key(turnID, itemID, field);
      this.values.delete(key);
      this.seeds.delete(key);
      this.listeners.delete(key);
    }
  }

  subscribe(key: string, listener: StreamTextListener): () => void {
    let listeners = this.listeners.get(key);
    if (!listeners) {
      listeners = new Set();
      this.listeners.set(key, listeners);
    }
    listeners.add(listener);
    return () => {
      listeners?.delete(listener);
      if (listeners?.size === 0) {
        this.listeners.delete(key);
      }
    };
  }

  private notify(key: string, value: string): void {
    const listeners = this.listeners.get(key);
    if (!listeners) {
      return;
    }
    for (const listener of listeners) {
      listener(value);
    }
  }

  private ensureSeed(key: string): void {
    if (!this.seeds.has(key)) {
      this.seeds.set(key, this.values.get(key) ?? "");
    }
  }
}

export const streamTextStore = new StreamTextStore();

export function streamTextKey(turnID: string, itemID: string, field: StreamTextField): string {
  return streamTextStore.key(turnID, itemID, field);
}

export function StreamingText({
  streamKey,
  initialText = "",
  className = "streaming-text"
}: {
  streamKey: string;
  initialText?: string;
  className?: string;
}): JSX.Element {
  const nodeRef = useRef<HTMLDivElement | null>(null);
  const frameRef = useRef<number | undefined>(undefined);
  const pendingTextRef = useRef(initialText);

  useEffect(() => {
    streamTextStore.seed(streamKey, initialText);

    const applyText = (value: string): void => {
      pendingTextRef.current = value;
      if (frameRef.current !== undefined) {
        return;
      }
      frameRef.current = window.requestAnimationFrame(() => {
        frameRef.current = undefined;
        if (nodeRef.current) {
          nodeRef.current.textContent = pendingTextRef.current;
        }
      });
    };

    applyText(streamTextStore.get(streamKey));
    const unsubscribe = streamTextStore.subscribe(streamKey, applyText);
    return () => {
      unsubscribe();
      if (frameRef.current !== undefined) {
        window.cancelAnimationFrame(frameRef.current);
        frameRef.current = undefined;
      }
    };
  }, [initialText, streamKey]);

  return <div ref={nodeRef} className={className} />;
}
