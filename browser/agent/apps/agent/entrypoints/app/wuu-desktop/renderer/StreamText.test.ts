import { describe, expect, it } from "bun:test";
import { mergeStreamDelta } from "./StreamText";

describe("mergeStreamDelta", () => {
  it("keeps plain incremental deltas intact", () => {
    expect(mergeStreamDelta("中国", "国旗").value).toBe("中国国旗");
  });

  it("replaces the buffer when the delta is a cumulative snapshot", () => {
    expect(mergeStreamDelta("午后", "午后的光")).toEqual({
      value: "午后的光",
      mode: "overlap"
    });
  });

  it("deduplicates overlapping cumulative fragments during a stream", () => {
    let merged = mergeStreamDelta("", "午后的光落在窗");
    merged = mergeStreamDelta(merged.value, "后的光落在窗边，", merged.mode);
    merged = mergeStreamDelta(merged.value, "边，杯里的", merged.mode);
    merged = mergeStreamDelta(merged.value, "里的茶", merged.mode);
    merged = mergeStreamDelta(merged.value, "茶慢慢", merged.mode);
    merged = mergeStreamDelta(merged.value, "慢慢变凉。", merged.mode);

    expect(merged).toEqual({
      value: "午后的光落在窗边，杯里的茶慢慢变凉。",
      mode: "overlap"
    });
  });
});
