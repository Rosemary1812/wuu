import { describe, expect, it } from "vitest";
import { streamTextKey, streamTextStore } from "./StreamText";

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
});

describe("streamTextStore subscriptions", () => {
  it("does not mark a key as buffered just because a component subscribed", () => {
    const key = streamTextKey("turn-sub-empty", "item", "text");
    const unsubscribe = streamTextStore.subscribe(key, () => undefined);
    expect(streamTextStore.has(key)).toBe(false);
    unsubscribe();
  });

  it("can seed a key after a component subscribed early", () => {
    const key = streamTextKey("turn-sub-seed", "item", "text");
    const calls: string[] = [];
    const unsubscribe = streamTextStore.subscribe(key, (value) => {
      calls.push(value);
    });
    streamTextStore.seed(key, "hello");
    unsubscribe();
    expect(streamTextStore.has(key)).toBe(true);
    expect(streamTextStore.seedValue(key)).toBe("hello");
    expect(calls).toEqual(["hello"]);
  });

  it("invokes value subscribers only when the value changes", () => {
    const key = streamTextKey("turn-sub", "item", "text");
    streamTextStore.set(key, "");
    const calls: string[] = [];
    const unsubscribe = streamTextStore.subscribe(key, (value) => {
      calls.push(value);
    });
    streamTextStore.append(key, "a");
    streamTextStore.append(key, "b");
    streamTextStore.set(key, "ab");
    streamTextStore.set(key, "ab");
    unsubscribe();
    streamTextStore.append(key, "c");
    expect(calls).toEqual(["a", "ab"]);
  });
});
