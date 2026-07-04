import { describe, expect, it } from "vitest";
import type {
  RuntimeContext,
  ServerEvent,
  Thread,
  Turn,
} from "../shared/protocol";
import {
  busyDMParticipantIDs,
  initialState,
  reduceServerEvent,
  serverEventTargetsActiveContext,
  serverEventTargetsGlobalThread,
  threadIsGlobalCollaboration,
  type AppState,
} from "./AppState";

// Regression coverage for issue #9: global-collaboration threads (DM/group)
// run under whichever app-server client started them, so their events are
// stamped with a workdir that need not match the active context's cwd. They
// must bypass the workspace filters or the roster's busy/unread state — all
// derived from state.threads — goes stale until the next project switch.

const ACTIVE_CWD = "/repo/project";
const AGENT_HOME = "/Users/me/.wuu/agents/andy/home";

function makeThread(overrides: Partial<Thread>): Thread {
  return {
    id: "thread-1",
    preview: "preview",
    model_provider: "fake",
    model: "fake-model",
    cwd: ACTIVE_CWD,
    status: "idle",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    turns: [],
    ...overrides,
  };
}

function dmThread(overrides: Partial<Thread> = {}): Thread {
  return makeThread({
    id: "dm-andy",
    preview: "dm",
    cwd: AGENT_HOME,
    workspace_kind: "dm",
    dm_participant_id: "andy",
    ...overrides,
  });
}

function runningTurn(id = "turn-1"): Turn {
  return {
    id,
    items_view: "full",
    status: "in_progress",
    items: [],
  } as Turn;
}

function completedTurn(id = "turn-1"): Turn {
  return {
    id,
    items_view: "full",
    status: "completed",
    items: [],
  } as Turn;
}

function stateWith(threads: Thread[], activeThread?: Thread): AppState {
  const context: RuntimeContext = {
    kind: "project",
    project_id: "proj",
    cwd: ACTIVE_CWD,
  };
  return {
    ...initialState,
    activeContext: context,
    thread: activeThread,
    threads,
  };
}

function notification(
  method: string,
  params: Record<string, unknown>,
  workdir: string,
): ServerEvent {
  return { kind: "notification", workdir, message: { method, params } };
}

describe("threadIsGlobalCollaboration", () => {
  it("classifies DM, group, and workspace_kind=dm threads as global", () => {
    expect(threadIsGlobalCollaboration(dmThread())).toBe(true);
    expect(
      threadIsGlobalCollaboration(makeThread({ group: true })),
    ).toBe(true);
    expect(
      threadIsGlobalCollaboration(
        makeThread({ workspace_kind: "dm", dm_participant_id: undefined }),
      ),
    ).toBe(true);
  });

  it("treats plain project/scratch threads as workspace-scoped", () => {
    expect(threadIsGlobalCollaboration(makeThread({}))).toBe(false);
    expect(
      threadIsGlobalCollaboration(makeThread({ workspace_kind: "scratch" })),
    ).toBe(false);
  });
});

describe("serverEventTargetsGlobalThread", () => {
  it("passes a DM thread lifecycle event even when it is not yet in state", () => {
    const state = stateWith([]);
    const event = notification(
      "thread/started",
      { thread: dmThread() },
      AGENT_HOME,
    );
    // The workdir gate alone would drop it (agent home != active cwd)...
    expect(serverEventTargetsActiveContext(event, state)).toBe(false);
    // ...but the collaboration bypass lets it through so the roster updates.
    expect(serverEventTargetsGlobalThread(event, state)).toBe(true);
  });

  it("passes turn/item events for a known DM thread by thread_id", () => {
    const state = stateWith([dmThread()]);
    const event = notification(
      "turn/completed",
      { thread_id: "dm-andy", turn: completedTurn() },
      AGENT_HOME,
    );
    expect(serverEventTargetsActiveContext(event, state)).toBe(false);
    expect(serverEventTargetsGlobalThread(event, state)).toBe(true);
  });

  it("does NOT pass a background project session's events (no regression)", () => {
    const projectThread = makeThread({
      id: "proj-bg",
      cwd: "/other/project",
    });
    const state = stateWith([projectThread]);
    const event = notification(
      "turn/completed",
      { thread_id: "proj-bg", turn: completedTurn() },
      "/other/project",
    );
    expect(serverEventTargetsActiveContext(event, state)).toBe(false);
    expect(serverEventTargetsGlobalThread(event, state)).toBe(false);
  });

  it("does not pass turn/item events for an unknown thread_id", () => {
    const state = stateWith([dmThread()]);
    const event = notification(
      "turn/completed",
      { thread_id: "ghost", turn: completedTurn() },
      AGENT_HOME,
    );
    expect(serverEventTargetsGlobalThread(event, state)).toBe(false);
  });

  it("ignores non-notification events", () => {
    const state = stateWith([dmThread()]);
    const event: ServerEvent = {
      kind: "server-error",
      workdir: AGENT_HOME,
      message: "boom",
    };
    expect(serverEventTargetsGlobalThread(event, state)).toBe(false);
  });
});

describe("reducer bypass for global-collaboration threads", () => {
  it("applies thread/updated for a DM thread whose cwd != active context", () => {
    const state = stateWith([dmThread({ status: "idle" })]);
    const updated = dmThread({ status: "in_progress" });
    const next = reduceServerEvent(
      state,
      notification("thread/updated", { thread: updated }, AGENT_HOME),
    );
    expect(next.threads.find((t) => t.id === "dm-andy")?.status).toBe(
      "in_progress",
    );
  });

  it("still drops thread/updated for a background project session", () => {
    const projectThread = makeThread({
      id: "proj-bg",
      cwd: "/other/project",
      status: "idle",
    });
    const state = stateWith([projectThread]);
    const updated = makeThread({
      id: "proj-bg",
      cwd: "/other/project",
      status: "in_progress",
    });
    const next = reduceServerEvent(
      state,
      notification("thread/updated", { thread: updated }, "/other/project"),
    );
    expect(next.threads.find((t) => t.id === "proj-bg")?.status).toBe("idle");
  });
});

describe("auto-activation guard", () => {
  it("upserts a backgrounded DM thread without hijacking the main pane", () => {
    const state: AppState = {
      ...stateWith([]),
      thread: undefined,
      allowThreadAutoActivation: true,
    };
    const next = reduceServerEvent(
      state,
      notification("thread/started", { thread: dmThread() }, AGENT_HOME),
    );
    expect(next.threads.some((t) => t.id === "dm-andy")).toBe(true);
    expect(next.thread).toBeUndefined();
  });

  it("still auto-activates a matching project thread", () => {
    const state: AppState = {
      ...stateWith([]),
      thread: undefined,
      allowThreadAutoActivation: true,
    };
    const projectThread = makeThread({ id: "proj-1", cwd: ACTIVE_CWD });
    const next = reduceServerEvent(
      state,
      notification("thread/started", { thread: projectThread }, ACTIVE_CWD),
    );
    expect(next.thread?.id).toBe("proj-1");
  });
});

describe("issue #9 end-to-end: background DM busy badge clears", () => {
  it("clears the participant busy indicator once the background DM turn completes", () => {
    const busy = dmThread({ status: "in_progress", turns: [runningTurn()] });
    const state = stateWith([busy]);
    // Sidebar reads busy from state.threads, so it starts lit.
    expect(busyDMParticipantIDs(state.threads).has("andy")).toBe(true);

    const event = notification(
      "turn/completed",
      { thread_id: "dm-andy", turn: completedTurn() },
      AGENT_HOME,
    );
    // The App.tsx dispatch gate: workspace filter fails, collaboration bypass
    // passes, so the event reaches the reducer instead of being dropped.
    const passesGate =
      serverEventTargetsActiveContext(event, state) ||
      serverEventTargetsGlobalThread(event, state);
    expect(passesGate).toBe(true);

    const next = reduceServerEvent(state, event);
    expect(busyDMParticipantIDs(next.threads).has("andy")).toBe(false);
  });
});
