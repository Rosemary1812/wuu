import { describe, expect, it } from "vitest";
import type { ServerEvent, SideThreadEvent } from "../shared/protocol";
import { sideThreadEventFromServerEvent } from "./sideThreadEvents";

const delta: SideThreadEvent = {
  type: "delta",
  side_thread_id: "side-1",
  main_thread_id: "main-1",
  message_id: "message-1",
  text_delta: "hello"
};

describe("sideThreadEventFromServerEvent", () => {
  it("extracts sideThread/event notification params", () => {
    const event: ServerEvent = {
      workdir: "/repo",
      kind: "notification",
      message: { method: "sideThread/event", params: delta }
    };
    expect(sideThreadEventFromServerEvent(event)).toBe(delta);
  });

  it("ignores unrelated server notifications", () => {
    const event: ServerEvent = {
      workdir: "/repo",
      kind: "notification",
      message: { method: "turn/started", params: delta }
    };
    expect(sideThreadEventFromServerEvent(event)).toBeUndefined();
  });

  it("ignores malformed side-thread notifications", () => {
    const event: ServerEvent = {
      workdir: "/repo",
      kind: "notification",
      message: {
        method: "sideThread/event",
        params: { type: "delta", side_thread_id: "side-1" }
      }
    };
    expect(sideThreadEventFromServerEvent(event)).toBeUndefined();
  });
});
