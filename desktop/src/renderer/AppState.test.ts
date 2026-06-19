import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Thread } from "../shared/protocol";
import {
  activeTurnTokenSpeed,
  activeTurnTokenSpeedSnapshot,
  appendStreamingTokenSample,
  appendTurnTokenSample,
  initialState,
  isThreadUnread,
  latestCompletedTurnID,
  markThreadTurnsViewed,
  reduceServerEvent,
  sortThreads,
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
      0,
      0,
      1_000,
    );
    const second = appendTurnTokenSample(
      first,
      "turn-1",
      "thread-1",
      10,
      22,
      4,
      8,
      2_000,
    );

    expect(activeTurnTokenSpeed(second, "turn-1")).toBe(20);
    expect(activeTurnTokenSpeedSnapshot(second, "turn-1").source).toBe("real");
    expect(second.turnTokenUsage["turn-1"].cacheCreationTokens).toBe(4);
    expect(second.turnTokenUsage["turn-1"].cacheReadTokens).toBe(8);
  });

  it("derives live token speed from streamed model output deltas", () => {
    const first = appendStreamingTokenSample(
      initialState,
      {
        thread_id: "thread-1",
        turn_id: "turn-1",
        delta: "aaaaaaaa",
      },
      1_000,
    );
    const second = appendStreamingTokenSample(
      first,
      {
        thread_id: "thread-1",
        turn_id: "turn-1",
        delta: "bbbbbbbb",
      },
      2_000,
    );

    expect(activeTurnTokenSpeed(second, "turn-1")).toBe(2);
    expect(activeTurnTokenSpeedSnapshot(second, "turn-1").source).toBe("estimated");
    expect(second.turnTokenUsage["turn-1"].outputTokens).toBe(0);
  });

  it("discards estimated samples when real provider usage arrives", () => {
    const estimated = appendStreamingTokenSample(
      initialState,
      {
        thread_id: "thread-1",
        turn_id: "turn-1",
        delta: "aaaaaaaaaaaaaaaa",
      },
      1_000,
    );
    const real = appendTurnTokenSample(
      estimated,
      "turn-1",
      "thread-1",
      10,
      3,
      0,
      0,
      1_500,
    );

    expect(activeTurnTokenSpeedSnapshot(real, "turn-1").source).toBe("real");
    expect(real.turnTokenUsage["turn-1"].samples).toEqual([
      { tokens: 3, at: 1_500 },
    ]);
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
      0,
      0,
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
      0,
      0,
      Date.now(),
    );
    vi.advanceTimersByTime(500);
    state = appendTurnTokenSample(
      state,
      "turn-1",
      "thread-1",
      0,
      140,
      0,
      0,
      Date.now(),
    );
    expect(activeTurnTokenSpeed(state, "turn-1")).toBeCloseTo(80, 0);
  });

  it("ignores unchanged usage snapshots while tools are running", () => {
    let state = appendTurnTokenSample(
      initialState,
      "turn-1",
      "thread-1",
      0,
      100,
      0,
      0,
      Date.now(),
    );
    vi.advanceTimersByTime(500);
    state = appendTurnTokenSample(
      state,
      "turn-1",
      "thread-1",
      0,
      140,
      0,
      0,
      Date.now(),
    );

    vi.advanceTimersByTime(2500);
    state = appendTurnTokenSample(
      state,
      "turn-1",
      "thread-1",
      20,
      140,
      0,
      0,
      Date.now(),
    );

    const speed = activeTurnTokenSpeedSnapshot(state, "turn-1");
    expect(speed.tokensPerSecond).toBeCloseTo(80, 0);
    expect(speed.sampledAt).toBe(new Date(2026, 0, 1, 0, 0, 0, 500).getTime());
    expect(state.turnTokenUsage["turn-1"].samples).toEqual([
      { tokens: 100, at: new Date(2026, 0, 1, 0, 0, 0).getTime() },
      { tokens: 140, at: new Date(2026, 0, 1, 0, 0, 0, 500).getTime() },
    ]);
  });

  it("drops samples older than the 2s window", () => {
    let state = appendTurnTokenSample(
      initialState,
      "turn-1",
      "thread-1",
      0,
      100,
      0,
      0,
      Date.now(),
    );
    vi.advanceTimersByTime(2500);
    state = appendTurnTokenSample(
      state,
      "turn-1",
      "thread-1",
      0,
      200,
      0,
      0,
      Date.now(),
    );
    expect(state.turnTokenUsage["turn-1"].samples).toHaveLength(1);
    expect(activeTurnTokenSpeed(state, "turn-1")).toBe(0);
  });
});

describe("AppState unread tracking", () => {
  function makeThreadWithTurns(
    threadID: string,
    turns: Array<{
      id: string;
      status: "completed" | "in_progress" | "failed" | "interrupted";
    }>,
  ): Thread {
    return {
      id: threadID,
      preview: "",
      model_provider: "fake",
      model: "fake-model",
      cwd: "/tmp",
      status: "idle",
      created_at: "2026-06-18T00:00:00Z",
      updated_at: "2026-06-18T00:00:00Z",
      turns: turns.map((t) => ({
        id: t.id,
        items: [],
        items_view: "full" as const,
        status: t.status,
      })),
    };
  }

  it("latestCompletedTurnID returns the most recent non-in_progress turn", () => {
    const thread = makeThreadWithTurns("thread-1", [
      { id: "turn-1", status: "completed" },
      { id: "turn-2", status: "completed" },
      { id: "turn-3", status: "completed" },
    ]);
    expect(latestCompletedTurnID(thread)).toBe("turn-3");
  });

  it("latestCompletedTurnID returns undefined when the latest turn is in_progress", () => {
    const thread = makeThreadWithTurns("thread-1", [
      { id: "turn-1", status: "completed" },
      { id: "turn-2", status: "in_progress" },
    ]);
    expect(latestCompletedTurnID(thread)).toBeUndefined();
  });

  it("latestCompletedTurnID returns undefined for an empty thread", () => {
    const thread = makeThreadWithTurns("thread-1", []);
    expect(latestCompletedTurnID(thread)).toBeUndefined();
  });

  it("isThreadUnread returns true for a thread with a new completed turn", () => {
    const thread = makeThreadWithTurns("thread-1", [
      { id: "turn-1", status: "completed" },
    ]);
    expect(isThreadUnread(thread, undefined)).toBe(true);
  });

  it("isThreadUnread returns false when lastViewed matches the latest turn", () => {
    const thread = makeThreadWithTurns("thread-1", [
      { id: "turn-1", status: "completed" },
    ]);
    expect(isThreadUnread(thread, "turn-1")).toBe(false);
  });

  it("isThreadUnread returns false for a running thread", () => {
    const thread = makeThreadWithTurns("thread-1", [
      { id: "turn-1", status: "in_progress" },
    ]);
    expect(isThreadUnread(thread, undefined)).toBe(false);
  });

  it("isThreadUnread returns false for an empty thread", () => {
    const thread = makeThreadWithTurns("thread-1", []);
    expect(isThreadUnread(thread, undefined)).toBe(false);
  });

  it("markThreadTurnsViewed records the latest completed turn ID", () => {
    const thread = makeThreadWithTurns("thread-1", [
      { id: "turn-1", status: "completed" },
    ]);
    const state = {
      ...initialState,
      thread,
      threads: [thread],
    };
    const next = markThreadTurnsViewed(state, "thread-1");
    expect(next.lastViewedTurnByThreadID["thread-1"]).toBe("turn-1");
  });

  it("markThreadTurnsViewed is a no-op when already current", () => {
    const thread = makeThreadWithTurns("thread-1", [
      { id: "turn-1", status: "completed" },
    ]);
    const state = {
      ...initialState,
      thread,
      threads: [thread],
      lastViewedTurnByThreadID: { "thread-1": "turn-1" },
    };
    expect(markThreadTurnsViewed(state, "thread-1")).toBe(state);
  });

  it("markThreadTurnsViewed is a no-op for a running thread", () => {
    const thread = makeThreadWithTurns("thread-1", [
      { id: "turn-1", status: "in_progress" },
    ]);
    const state = {
      ...initialState,
      thread,
      threads: [thread],
    };
    expect(markThreadTurnsViewed(state, "thread-1")).toBe(state);
  });
});

describe("AppState sortThreads (sidebar order)", () => {
  function makeSortableThread(args: {
    id: string;
    createdAt: string;
    updatedAt: string;
    status?: "idle" | "in_progress";
    turns?: Array<{ id: string; status: "completed" | "in_progress" | "failed" | "interrupted" }>;
    archived?: boolean;
    readOnly?: boolean;
  }): Thread {
    return {
      id: args.id,
      preview: "",
      model_provider: "fake",
      model: "fake-model",
      cwd: "/tmp",
      status: args.status ?? "idle",
      created_at: args.createdAt,
      updated_at: args.updatedAt,
      archived: args.archived,
      read_only: args.readOnly,
      turns: (args.turns ?? []).map((turn) => ({
        id: turn.id,
        items: [],
        items_view: "full" as const,
        status: turn.status,
      })),
    };
  }

  it("keeps running threads in created_at order regardless of updated_at jitter", () => {
    // Two in_progress threads. updated_at keeps bumping while the model
    // streams; created_at never changes. The old single-key sort shuffled
    // the rows every time either side streamed a token. The fix pins
    // running threads to a created_at order so clicking one is stable.
    const older = makeSortableThread({
      id: "thread-older",
      createdAt: "2026-06-18T00:00:00Z",
      updatedAt: "2026-06-20T12:00:00Z",
      status: "in_progress",
      turns: [{ id: "turn-1", status: "in_progress" }],
    });
    const newer = makeSortableThread({
      id: "thread-newer",
      createdAt: "2026-06-19T00:00:00Z",
      updatedAt: "2026-06-18T00:00:00Z", // stale; should be ignored while running
      status: "in_progress",
      turns: [{ id: "turn-1", status: "in_progress" }],
    });

    const sorted = sortThreads([older, newer]);
    expect(sorted.map((thread) => thread.id)).toEqual([
      "thread-newer",
      "thread-older",
    ]);

    // Even after flipping updated_at wildly, running order is unchanged.
    const flipped = sortThreads([
      { ...newer, updated_at: "2099-01-01T00:00:00Z" },
      { ...older, updated_at: "1970-01-01T00:00:00Z" },
    ]);
    expect(flipped.map((thread) => thread.id)).toEqual([
      "thread-newer",
      "thread-older",
    ]);
  });

  it("places running threads before settled threads", () => {
    const running = makeSortableThread({
      id: "thread-running",
      createdAt: "2026-06-18T00:00:00Z",
      updatedAt: "2026-06-18T00:00:00Z",
      status: "in_progress",
      turns: [{ id: "turn-1", status: "in_progress" }],
    });
    // Settled thread updated more recently than the running one. It still
    // sits below the running section — recency bubbles within the settled
    // group, not above active conversations.
    const settledRecent = makeSortableThread({
      id: "thread-settled-recent",
      createdAt: "2026-06-17T00:00:00Z",
      updatedAt: "2099-01-01T00:00:00Z",
    });
    const settledOlder = makeSortableThread({
      id: "thread-settled-older",
      createdAt: "2026-06-16T00:00:00Z",
      updatedAt: "2026-06-19T00:00:00Z",
    });

    const sorted = sortThreads([settledOlder, running, settledRecent]);
    expect(sorted.map((thread) => thread.id)).toEqual([
      "thread-running",
      "thread-settled-recent",
      "thread-settled-older",
    ]);
  });

  it("sorts settled threads by updated_at desc", () => {
    const settledA = makeSortableThread({
      id: "thread-a",
      createdAt: "2026-06-15T00:00:00Z",
      updatedAt: "2026-06-15T00:00:00Z",
    });
    const settledB = makeSortableThread({
      id: "thread-b",
      createdAt: "2026-06-15T00:00:00Z",
      updatedAt: "2026-06-20T00:00:00Z",
    });
    const settledC = makeSortableThread({
      id: "thread-c",
      createdAt: "2026-06-15T00:00:00Z",
      updatedAt: "2026-06-17T00:00:00Z",
    });
    const sorted = sortThreads([settledA, settledB, settledC]);
    expect(sorted.map((thread) => thread.id)).toEqual([
      "thread-b",
      "thread-c",
      "thread-a",
    ]);
  });

  it("detects running via any in-progress turn even when thread status is idle", () => {
    // A thread that has just received its first turn but whose own status
    // hasn't been bumped yet must still be treated as running — the
    // streaming output lives in the latest turn.
    const streaming = makeSortableThread({
      id: "thread-streaming",
      createdAt: "2026-06-18T00:00:00Z",
      updatedAt: "2026-06-18T00:00:00Z",
      status: "idle",
      turns: [{ id: "turn-1", status: "in_progress" }],
    });
    const settled = makeSortableThread({
      id: "thread-idle",
      createdAt: "2026-06-17T00:00:00Z",
      updatedAt: "2026-06-20T00:00:00Z",
    });

    const sorted = sortThreads([settled, streaming]);
    expect(sorted.map((thread) => thread.id)).toEqual([
      "thread-streaming",
      "thread-idle",
    ]);
  });

  it("drops archived and read-only threads from the sortable list", () => {
    const archived = makeSortableThread({
      id: "thread-archived",
      createdAt: "2026-06-18T00:00:00Z",
      updatedAt: "2099-01-01T00:00:00Z",
      archived: true,
    });
    const readOnly = makeSortableThread({
      id: "thread-readonly",
      createdAt: "2026-06-18T00:00:00Z",
      updatedAt: "2099-01-01T00:00:00Z",
      readOnly: true,
    });
    const normal = makeSortableThread({
      id: "thread-normal",
      createdAt: "2026-06-18T00:00:00Z",
      updatedAt: "2026-06-19T00:00:00Z",
    });

    const sorted = sortThreads([archived, readOnly, normal]);
    expect(sorted.map((thread) => thread.id)).toEqual(["thread-normal"]);
  });
});
