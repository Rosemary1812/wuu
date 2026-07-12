import { describe, expect, it } from "vitest";
import type { ActivitySession, ServerEvent } from "../shared/protocol";
import {
  activityControlMethod,
  activityVisibleForThread,
  cuaActivityFromServerEvent,
  frameStreamRetryDelay,
  nativePiPInitialBounds,
  observationKey,
} from "./cuaActivityWindows";

function activity(overrides: Partial<ActivitySession> = {}): ActivitySession {
  return {
    id: "activity-1",
    kind: "cua",
    thread_id: "thread-1",
    workdir: "/repo",
    plugin_id: "cua-mac",
    target: "com.apple.TextEdit",
    state: "active",
    controller: "agent",
    created_at: "2026-07-10T10:00:00Z",
    updated_at: "2026-07-10T10:00:01Z",
    ...overrides,
  };
}

describe("CUA native picture-in-picture", () => {
  it("scopes the native PiP to the active session", () => {
    expect(activityVisibleForThread("thread-1", "thread-1")).toBe(true);
    expect(activityVisibleForThread("thread-1", "thread-2")).toBe(false);
    expect(activityVisibleForThread("thread-1", undefined)).toBe(false);
  });

  it("places the PiP inside the Wuu window corner and active work area", () => {
    expect(nativePiPInitialBounds(
      { x: 100, y: 80, width: 1200, height: 800 },
      { x: 0, y: 0, width: 1440, height: 900 },
    )).toEqual({ x: 1028, y: 92, width: 260, height: 170 });
    expect(nativePiPInitialBounds(undefined, { x: 1440, y: 0, width: 1200, height: 900 }))
      .toEqual({ x: 2356, y: 24, width: 260, height: 170 });
  });

  it("backs off native capture restarts", () => {
    expect(frameStreamRetryDelay(1)).toBe(2000);
    expect(frameStreamRetryDelay(2)).toBe(4000);
    expect(frameStreamRetryDelay(3)).toBe(8000);
    expect(frameStreamRetryDelay(6)).toBe(16000);
  });

  it("accepts only CUA lifecycle notifications", () => {
    const event: ServerEvent = {
      workdir: "/repo",
      kind: "notification",
      message: { method: "activity/updated", params: activity() },
    };
    expect(cuaActivityFromServerEvent(event)?.id).toBe("activity-1");
    expect(cuaActivityFromServerEvent({ ...event, message: { method: "activity/updated", params: activity({ kind: "browser" }) } }))
      .toBeUndefined();
  });

  it("maps Activity controls onto RPC methods", () => {
    expect(activityControlMethod("takeover")).toBe("activity/takeover");
    expect(activityControlMethod("release")).toBe("activity/release");
    expect(activityControlMethod("stop")).toBe("activity/stop");
  });

  it("keys an observation by session and target, ignoring window identity", () => {
    expect(observationKey(activity({ target: "com.apple.TextEdit" })))
      .toBe("thread-1:com.apple.TextEdit");
    // Window identity is 0/0 on `started` and resolved on `updated`; a refinement
    // must not change the key, or a second helper would race the first on the
    // same window and trip a ScreenCaptureKit connection error.
    expect(observationKey(activity({ target: "com.apple.TextEdit", process_id: 0, window_id: 0 })))
      .toBe(observationKey(activity({ target: "com.apple.TextEdit", process_id: 42, window_id: 99 })));
  });
});
