import { afterEach, describe, expect, it, vi } from "vitest";
import {
  STREAM_TEXT_NOTIFY_INTERVAL_MS,
  streamTextKey,
  streamTextStore,
} from "./StreamText";

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

function installManualRAF(): {
  flush: () => void;
  restore: () => void;
} {
  const realRAF = window.requestAnimationFrame;
  const pending: FrameRequestCallback[] = [];
  let nextHandle = 1;
  window.requestAnimationFrame = ((cb: FrameRequestCallback) => {
    pending.push(cb);
    return nextHandle++;
  }) as typeof window.requestAnimationFrame;
  return {
    flush: () => {
      const callbacks = pending.splice(0);
      for (const cb of callbacks) {
        cb(performance.now());
      }
    },
    restore: () => {
      window.requestAnimationFrame = realRAF;
    },
  };
}

describe("streamTextStore", () => {
  it("appends plain incremental deltas", () => {
    const key = streamTextKey("turn-append", "item", "text");
    streamTextStore.set(key, "");
    streamTextStore.append(key, "中国");
    streamTextStore.append(key, "国旗");

    expect(streamTextStore.get(key)).toBe("中国国旗");
  });

  it("uses explicit replacement events to reset streamed text", () => {
    const key = streamTextKey("turn-replace", "item", "text");
    streamTextStore.set(key, "");
    streamTextStore.append(key, "old partial");
    streamTextStore.set(key, "");
    streamTextStore.append(key, "new response");

    expect(streamTextStore.get(key)).toBe("new response");
  });

  it("keeps old text visible across an empty stream replace until fresh text arrives", () => {
    const key = streamTextKey("turn-replace-visible", "item", "text");
    streamTextStore.set(key, "");
    streamTextStore.append(key, "stale partial");
    streamTextStore.replace(key, "");

    expect(streamTextStore.get(key)).toBe("stale partial");

    streamTextStore.append(key, "fresh answer");

    expect(streamTextStore.get(key)).toBe("fresh answer");
  });
});

describe("streamTextStore subscriptions", () => {
  it("does not mark a key as buffered just because a component subscribed", () => {
    const key = streamTextKey("turn-sub-empty", "item", "text");
    const before = streamTextStore.stats().entryCount;
    const unsubscribe = streamTextStore.subscribe(key, () => undefined);
    expect(streamTextStore.has(key)).toBe(false);
    unsubscribe();
    expect(streamTextStore.stats().entryCount).toBe(before);
  });

  it("keeps value entries after unsubscribe so late snapshots can still read them", () => {
    const key = streamTextKey("turn-sub-value", "item", "text");
    const before = streamTextStore.stats().valueEntryCount;
    streamTextStore.set(key, "hello");
    const unsubscribe = streamTextStore.subscribe(key, () => undefined);
    unsubscribe();

    expect(streamTextStore.has(key)).toBe(true);
    expect(streamTextStore.stats().valueEntryCount).toBe(before + 1);
    streamTextStore.clearItem("turn-sub-value", "item");
  });

  it("can seed a key after a component subscribed early", () => {
    const raf = installManualRAF();
    const key = streamTextKey("turn-sub-seed", "item", "text");
    const calls: string[] = [];
    const unsubscribe = streamTextStore.subscribe(key, (value) => {
      calls.push(value);
    });
    try {
      streamTextStore.seed(key, "hello");
      expect(calls).toEqual([]);
      raf.flush();
      expect(streamTextStore.has(key)).toBe(true);
      expect(streamTextStore.seedValue(key)).toBe("hello");
      expect(calls).toEqual(["hello"]);
    } finally {
      unsubscribe();
      raf.restore();
    }
  });

  it("coalesces value subscribers to one notification per frame", () => {
    const raf = installManualRAF();
    const now = vi.spyOn(performance, "now");
    const key = streamTextKey("turn-sub", "item", "text");
    const calls: string[] = [];
    const unsubscribe = streamTextStore.subscribe(key, (value) => {
      calls.push(value);
    });
    try {
      now.mockReturnValue(100_000);
      streamTextStore.set(key, "");
      streamTextStore.append(key, "a");
      streamTextStore.append(key, "b");
      streamTextStore.set(key, "ab");
      streamTextStore.set(key, "ab");
      expect(streamTextStore.get(key)).toBe("ab");
      expect(calls).toEqual([]);
      raf.flush();
      expect(calls).toEqual(["ab"]);

      now.mockReturnValue(100_040);
      for (let i = 0; i < 100; i += 1) {
        streamTextStore.append(key, "x");
      }
      expect(streamTextStore.get(key)).toBe(`ab${"x".repeat(100)}`);
      raf.flush();
      expect(calls).toEqual(["ab", `ab${"x".repeat(100)}`]);

      unsubscribe();
      streamTextStore.append(key, "c");
      raf.flush();
      expect(calls).toEqual(["ab", `ab${"x".repeat(100)}`]);
    } finally {
      unsubscribe();
      raf.restore();
    }
  });

  it("enforces a 33ms floor between high-rate subscriber notifications", () => {
    vi.useFakeTimers();
    const raf = installManualRAF();
    const now = vi.spyOn(performance, "now");
    const key = streamTextKey("turn-sub-throttle", "item", "text");
    const calls: string[] = [];
    const unsubscribe = streamTextStore.subscribe(key, (value) => {
      calls.push(value);
    });
    try {
      now.mockReturnValue(200_000);
      streamTextStore.set(key, "a");
      raf.flush();
      expect(calls).toEqual(["a"]);

      now.mockReturnValue(200_010);
      streamTextStore.append(key, "b");
      raf.flush();
      expect(calls).toEqual(["a"]);

      vi.advanceTimersByTime(STREAM_TEXT_NOTIFY_INTERVAL_MS - 11);
      expect(calls).toEqual(["a"]);
      vi.advanceTimersByTime(1);
      expect(calls).toEqual(["a", "ab"]);
    } finally {
      unsubscribe();
      streamTextStore.clearItem("turn-sub-throttle", "item");
      raf.restore();
    }
  });
});
