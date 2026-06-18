import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  activeTurnTokenSpeed,
  appendTurnTokenSample,
  initialState,
  reduceServerEvent,
} from "./AppState";

describe("AppState server requests", () => {
  it("keeps tool approval requests pending instead of rejecting them", () => {
    const rejectServerRequest = vi.fn();
    Object.defineProperty(window, "wuu", {
      configurable: true,
      value: { rejectServerRequest },
    });

    const next = reduceServerEvent(initialState, {
      kind: "server-request",
      workdir: "/tmp/project",
      message: {
        id: "server-request-1",
        method: "tool/approval/request",
        params: {
          id: "approval-1",
          tool_name: "run_shell",
          risk: "high",
          arguments_preview: "{\"command\":\"printf hi\"}",
        },
      },
    });

    expect(rejectServerRequest).not.toHaveBeenCalled();
    expect(next.pendingToolApproval?.server_request_id).toBe("server-request-1");
    expect(next.pendingToolApproval?.tool_name).toBe("run_shell");
    expect(next.status).toBe("等待审批");
  });
});

describe("AppState token usage", () => {
  it("initializes token usage state before the first usage update", () => {
    expect(initialState.turnTokenUsage).toEqual({});
    expect(activeTurnTokenSpeed(initialState, "turn-1")).toBe(0);
  });

  it("derives token speed from cumulative output-token samples", () => {
    const first = appendTurnTokenSample(
      initialState,
      "turn-1",
      "thread-1",
      10,
      2,
      1_000,
    );
    const second = appendTurnTokenSample(
      first,
      "turn-1",
      "thread-1",
      10,
      22,
      2_000,
    );

    expect(activeTurnTokenSpeed(second, "turn-1")).toBe(20);
  });
});

describe("turn token speed", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 0, 1, 0, 0, 0));
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("returns 0 when there are no samples", () => {
    expect(activeTurnTokenSpeed(initialState, "turn-1")).toBe(0);
  });

  it("returns 0 with fewer than two samples", () => {
    const state = appendTurnTokenSample(
      initialState,
      "turn-1",
      "thread-1",
      0,
      100,
      Date.now(),
    );
    expect(activeTurnTokenSpeed(state, "turn-1")).toBe(0);
  });

  it("computes tok/s from the oldest to the newest sample", () => {
    let state = appendTurnTokenSample(
      initialState,
      "turn-1",
      "thread-1",
      0,
      100,
      Date.now(),
    );
    vi.advanceTimersByTime(500);
    state = appendTurnTokenSample(
      state,
      "turn-1",
      "thread-1",
      0,
      140,
      Date.now(),
    );
    expect(activeTurnTokenSpeed(state, "turn-1")).toBeCloseTo(80, 0);
  });

  it("drops samples older than the 2s window", () => {
    let state = appendTurnTokenSample(
      initialState,
      "turn-1",
      "thread-1",
      0,
      100,
      Date.now(),
    );
    vi.advanceTimersByTime(2500);
    state = appendTurnTokenSample(
      state,
      "turn-1",
      "thread-1",
      0,
      200,
      Date.now(),
    );
    expect(state.turnTokenUsage["turn-1"].samples).toHaveLength(1);
    expect(activeTurnTokenSpeed(state, "turn-1")).toBe(0);
  });
});
