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

  it("decodes user-resized PiP geometry", () => {
    const decoder = new CUALineDecoder();
    expect(decoder.push('{"event":"geometry","x":20,"y":30,"width":520,"height":300}\n')).toEqual([
      { event: "geometry", x: 20, y: 30, width: 520, height: 300 },
    ]);
  });
});
