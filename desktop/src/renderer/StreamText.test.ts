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
