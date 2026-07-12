import { afterEach, describe, expect, it, vi } from "vitest";
import type { ActivitySession, ServerEvent } from "../shared/protocol";
import type { CUANativePiPEvent } from "./cuaFrameStreams";
import type { WindowRegistry } from "./windowRegistry";
import {
  CUAObservationCoordinator,
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

afterEach(() => {
  vi.useRealTimers();
});

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

  it("waits for the outgoing helper to close and coalesces replacements", () => {
    const events: string[] = [];
    const helpers: Array<{ target: string; finishStop?: () => void }> = [];
    const coordinator = new CUAObservationCoordinator(
      { mainWindow: () => undefined } as unknown as WindowRegistry,
      undefined,
      (next) => {
        const helper: { target: string; finishStop?: () => void } = { target: next.target ?? "" };
        helpers.push(helper);
        return {
          start: () => { events.push(`start:${helper.target}`); },
          setVisible: () => undefined,
          animateInteraction: () => undefined,
          stop: (onStopped?: () => void) => {
            events.push(`stop:${helper.target}`);
            helper.finishStop = onStopped;
          },
        };
      },
    );
    coordinator.setActiveThread("thread-1");
    coordinator.update(activity({ target: "app-a" }));
    coordinator.update(activity({ target: "app-b", updated_at: "2026-07-10T10:00:02Z" }));
    coordinator.update(activity({ target: "app-c", updated_at: "2026-07-10T10:00:03Z" }));

    expect(events).toEqual(["start:app-a", "stop:app-a"]);
    expect(helpers).toHaveLength(1);

    helpers[0].finishStop?.();
    expect(events).toEqual(["start:app-a", "stop:app-a", "start:app-c"]);
    expect(helpers.map((helper) => helper.target)).toEqual(["app-a", "app-c"]);
  });

  it("does not start an update while a user-closed helper is still stopping", () => {
    vi.useFakeTimers();
    vi.setSystemTime("2026-07-10T10:00:01.500Z");
    const events: string[] = [];
    const helpers: Array<{ target: string; finishStop?: () => void }> = [];
    const coordinator = new CUAObservationCoordinator(
      { mainWindow: () => undefined } as unknown as WindowRegistry,
      undefined,
      (next) => {
        const helper: { target: string; finishStop?: () => void } = { target: next.target ?? "" };
        helpers.push(helper);
        return {
          start: () => { events.push(`start:${helper.target}`); },
          setVisible: () => undefined,
          animateInteraction: () => undefined,
          stop: (onStopped?: () => void) => {
            events.push(`stop:${helper.target}`);
            helper.finishStop = onStopped;
          },
        };
      },
    );
    const initial = activity({ target: "app-a" });
    coordinator.setActiveThread("thread-1");
    coordinator.update(initial);
    const testCoordinator = coordinator as unknown as {
      handleNativePiPEvent: (key: string, event: CUANativePiPEvent) => void;
    };
    testCoordinator.handleNativePiPEvent(observationKey(initial), { event: "user_close" });
    coordinator.update(activity({ target: "app-b", updated_at: "2026-07-10T10:00:02Z" }));

    expect(events).toEqual(["start:app-a", "stop:app-a"]);
    expect(helpers).toHaveLength(1);

    helpers[0].finishStop?.();
    expect(events).toEqual(["start:app-a", "stop:app-a", "start:app-b"]);
    expect(helpers.map((helper) => helper.target)).toEqual(["app-a", "app-b"]);
  });
});
