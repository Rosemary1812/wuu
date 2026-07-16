import { afterEach, describe, expect, it } from "vitest";
import { initialState } from "./AppState";
import {
  runDebugEventFromServerEvent,
  runDebugPhaseForState,
} from "./RunDebugPanel";
import { setActiveLocale } from "./i18n";

afterEach(() => {
  setActiveLocale("zh-CN");
});

describe("RunDebugPanel localization", () => {
  it("localizes runtime phases in English", () => {
    setActiveLocale("en-US");

    expect(runDebugPhaseForState(initialState)).toMatchObject({
      label: "Runtime not ready",
      detail: "Connecting",
      tone: "running",
    });
  });

  it("localizes server event details and locale-formats counts", () => {
    setActiveLocale("en-US");
    const deltaSeen = new Set<string>();

    expect(
      runDebugEventFromServerEvent(
        {
          kind: "server-request",
          workdir: "/repo",
          message: { id: "request-1", method: "approval/request", params: {} },
        },
        deltaSeen,
      ),
    ).toMatchObject({ detail: "The server is waiting for a client response" });

    expect(
      runDebugEventFromServerEvent(
        {
          kind: "notification",
          workdir: "/repo",
          message: {
            method: "item/agentMessage/delta",
            params: { turn_id: "turn-1", item_id: "item-1", delta: "hello" },
          },
        },
        deltaSeen,
      ),
    ).toMatchObject({ detail: "First chunk: 5 characters" });
  });
});
