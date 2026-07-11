import { describe, expect, it } from "vitest";
import { CUAFrameDecoder } from "./cuaFrameStreams";

function envelope(metadata: object, payload: Buffer): Buffer {
  const encoded = Buffer.from(JSON.stringify(metadata));
  const header = Buffer.alloc(8);
  header.writeUInt32BE(encoded.length, 0);
  header.writeUInt32BE(payload.length, 4);
  return Buffer.concat([header, encoded, payload]);
}

describe("CUAFrameDecoder", () => {
  it("decodes fragmented binary frames without mixing payloads", () => {
    const decoder = new CUAFrameDecoder();
    const first = envelope({ event: "started" }, Buffer.alloc(0));
    const second = envelope({ event: "frame", revision: 7 }, Buffer.from("jpeg"));
    expect(decoder.push(first.subarray(0, 5))).toEqual([]);
    const frames = decoder.push(Buffer.concat([first.subarray(5), second]));
    expect(frames).toHaveLength(2);
    expect(frames[1]?.metadata.revision).toBe(7);
    expect(frames[1]?.payload.toString()).toBe("jpeg");
  });

  it("rejects oversized envelopes", () => {
    const decoder = new CUAFrameDecoder();
    const header = Buffer.alloc(8);
    header.writeUInt32BE(2 * 1024 * 1024, 0);
    expect(() => decoder.push(header)).toThrow("invalid CUA frame envelope size");
  });

  it("decodes user input control events", () => {
    const decoder = new CUAFrameDecoder();
    const frames = decoder.push(envelope({ event: "user_input" }, Buffer.alloc(0)));
    expect(frames[0]?.metadata.event).toBe("user_input");
  });

  it("preserves the frame capture mode", () => {
    const decoder = new CUAFrameDecoder();
    const frames = decoder.push(envelope({ event: "frame", capture_mode: "visible_fallback" }, Buffer.from("jpeg")));
    expect(frames[0]?.metadata.capture_mode).toBe("visible_fallback");
  });

  it("decodes explicit capture health states", () => {
    const decoder = new CUAFrameDecoder();
    const frames = decoder.push(envelope({ event: "capture_status", status: "suspended" }, Buffer.alloc(0)));
    expect(frames[0]?.metadata.status).toBe("suspended");
  });
});
