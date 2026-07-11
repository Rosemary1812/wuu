import { describe, expect, it } from "vitest";
import { CUALineDecoder } from "./cuaFrameStreams";

describe("CUALineDecoder", () => {
  it("decodes fragmented native PiP events", () => {
    const decoder = new CUALineDecoder();
    expect(decoder.push('{"event":"rea')).toEqual([]);
    expect(decoder.push('dy","width":320,"height":240}\n{"event":"user_input"}\n')).toEqual([
      { event: "ready", width: 320, height: 240 },
      { event: "user_input" },
    ]);
  });

  it("keeps an incomplete final line for the next chunk", () => {
    const decoder = new CUALineDecoder();
    expect(decoder.push('{"event":"capture_status","status":"idle"}')).toEqual([]);
    expect(decoder.push("\n")).toEqual([{ event: "capture_status", status: "idle" }]);
  });
});
