// Reducer semantics mirrored from desktop AppState: upserts that never lose
// local history, snapshots that never truncate streamed text, cumulative
// mark upserts, and the optimistic-send lifecycle.

import { describe, expect, it } from "vitest";
import type { Thread } from "@wuu/protocol";

import { AppStore } from "../src/lib/store";

function chatThread(partial: Partial<Thread>): Thread {
  return {
    id: "t1",
    preview: "",
    model_provider: "p",
    model: "m",
    cwd: "/",
    status: "idle",
    workspace_kind: "dm",
    dm_participant_id: "p1",
    created_at: "2026-07-07T00:00:00Z",
    updated_at: "2026-07-07T00:00:00Z",
    turns: [],
    ...partial,
  } as Thread;
}

function seeded(): AppStore {
  const store = new AppStore();
  store.setThreads([chatThread({})]);
  return store;
}

describe("thread upserts", () => {
  it("filters non-chat threads on setThreads and on upsert", () => {
    const store = new AppStore();
    store.setThreads([
      chatThread({ id: "dm" }),
      chatThread({ id: "proj", workspace_kind: "project", dm_participant_id: undefined }),
      chatThread({ id: "grp", workspace_kind: "project", dm_participant_id: undefined, group: true }),
    ]);
    expect(store.getSnapshot().threads.map((t) => t.id).sort()).toEqual(["dm", "grp"]);
  });

  it("thread/updated with empty turns keeps local history", () => {
    const store = seeded();
    store.applyNotification("turn/started", {
      thread_id: "t1",
      turn: { id: "turn-1", items: [], items_view: "full", status: "in_progress" },
    });
    store.applyNotification("thread/updated", { thread: chatThread({ turns: [] }) });
    expect(store.getSnapshot().threads[0].turns).toHaveLength(1);
  });

  it("archiving via thread/updated removes the thread", () => {
    const store = seeded();
    store.applyNotification("thread/updated", { thread: chatThread({ archived: true }) });
    expect(store.getSnapshot().threads).toHaveLength(0);
  });
});

describe("connection state", () => {
  it("surfaces and clears server synchronization failures", () => {
    const store = new AppStore();
    store.setPhase("attached");
    store.setSyncError("invalid response");
    expect(store.getSnapshot()).toMatchObject({
      phase: "attached",
      syncError: "invalid response",
    });

    store.setSyncError(null);
    expect(store.getSnapshot().syncError).toBeNull();
  });
});

describe("turn and item ingestion", () => {
  it("tracks running state through turn lifecycle", () => {
    const store = seeded();
    store.applyNotification("turn/started", {
      thread_id: "t1",
      turn: { id: "turn-1", items: [], items_view: "full", status: "in_progress" },
    });
    expect(store.getSnapshot().threads[0].status).toBe("in_progress");
    store.applyNotification("turn/completed", {
      thread_id: "t1",
      turn: { id: "turn-1", items: [], items_view: "full", status: "completed" },
    });
    expect(store.getSnapshot().threads[0].status).toBe("idle");
  });

  it("a shorter completed snapshot never truncates streamed text", () => {
    const store = seeded();
    store.applyNotification("item/started", {
      thread_id: "t1",
      turn_id: "turn-1",
      item: { id: "i1", type: "participant_message", text: "" },
    });
    store.applyNotification("item/agentMessage/delta", {
      thread_id: "t1",
      turn_id: "turn-1",
      item_id: "i1",
      delta: "很长很长的一段流式文本",
    });
    store.applyNotification("item/completed", {
      thread_id: "t1",
      turn_id: "turn-1",
      item: { id: "i1", type: "participant_message", text: "很长" },
    });
    const item = store.getSnapshot().threads[0].turns[0].items[0];
    expect(item.text).toBe("很长很长的一段流式文本");
  });

  it("replace always wins over accumulated text", () => {
    const store = seeded();
    store.applyNotification("item/started", {
      thread_id: "t1",
      turn_id: "turn-1",
      item: { id: "i1", type: "participant_message", text: "旧的长长长长文本" },
    });
    store.applyNotification("item/agentMessage/replace", {
      thread_id: "t1",
      turn_id: "turn-1",
      item_id: "i1",
      text: "新",
    });
    expect(store.getSnapshot().threads[0].turns[0].items[0].text).toBe("新");
  });

  it("flags unknown threads so the controller can refresh", () => {
    const store = new AppStore();
    const unknown: string[] = [];
    store.onUnknownThread = (id) => unknown.push(id);
    store.applyNotification("turn/started", {
      thread_id: "mystery",
      turn: { id: "turn-1", items: [], items_view: "full", status: "in_progress" },
    });
    expect(unknown).toEqual(["mystery"]);
  });
});

describe("optimistic sends", () => {
  const pendingSend = {
    clientId: "c1",
    threadId: "t1",
    text: "hi",
    atMs: 1,
    queued: true,
  };

  it("removed when turn/started carries its queue_id", () => {
    const store = seeded();
    store.addPending(pendingSend);
    store.applyNotification("turn/started", {
      thread_id: "t1",
      queue_id: "c1",
      turn: { id: "turn-1", items: [], items_view: "full", status: "in_progress" },
    });
    expect(store.getSnapshot().pending).toHaveLength(0);
  });

  it("removed when its user_message lands with source_id", () => {
    const store = seeded();
    store.addPending(pendingSend);
    store.applyNotification("item/completed", {
      thread_id: "t1",
      turn_id: "turn-1",
      item: { id: "i1", type: "user_message", text: "hi", source_id: "c1" },
    });
    expect(store.getSnapshot().pending).toHaveLength(0);
  });

  it("removed on turn/dequeued", () => {
    const store = seeded();
    store.addPending(pendingSend);
    store.applyNotification("turn/dequeued", { thread_id: "t1", queue_id: "c1" });
    expect(store.getSnapshot().pending).toHaveLength(0);
  });
});

describe("review fixes", () => {
  it("setThreads preserves loaded turns when the list entry has none", () => {
    const store = seeded();
    store.applyNotification("turn/started", {
      thread_id: "t1",
      turn: { id: "turn-1", items: [], items_view: "full", status: "in_progress" },
    });
    store.setThreads([chatThread({ turns: [] })]);
    expect(store.getSnapshot().threads[0].turns).toHaveLength(1);
  });

  it("upsertTurn uses server timestamps and only moves updated_at forward", () => {
    const store = new AppStore();
    store.setThreads([chatThread({ updated_at: "2026-07-07T10:00:00Z" })]);
    // A redelivered OLD turn must not bump the thread above newer chats.
    store.applyNotification("turn/completed", {
      thread_id: "t1",
      turn: {
        id: "turn-old",
        items: [],
        items_view: "full",
        status: "completed",
        completed_at: "2026-07-01T00:00:00Z",
      },
    });
    expect(store.getSnapshot().threads[0].updated_at).toBe("2026-07-07T10:00:00Z");
    // A genuinely newer turn moves it forward to the SERVER time.
    store.applyNotification("turn/completed", {
      thread_id: "t1",
      turn: {
        id: "turn-new",
        items: [],
        items_view: "full",
        status: "completed",
        completed_at: "2026-07-07T12:00:00Z",
      },
    });
    expect(store.getSnapshot().threads[0].updated_at).toBe("2026-07-07T12:00:00Z");
  });

  it("turn snapshots never drop locally-known items", () => {
    const store = seeded();
    store.applyNotification("item/completed", {
      thread_id: "t1",
      turn_id: "turn-1",
      item: { id: "i-local", type: "participant_message", text: "先到的消息" },
    });
    store.applyNotification("turn/completed", {
      thread_id: "t1",
      turn: {
        id: "turn-1",
        items: [{ id: "i-snap", type: "user_message", text: "快照里的" }],
        items_view: "full",
        status: "completed",
      },
    });
    const items = store.getSnapshot().threads[0].turns[0].items;
    expect(items.map((i) => i.id).sort()).toEqual(["i-local", "i-snap"]);
  });

  it("participant/updated flags the roster stale", () => {
    const store = seeded();
    let stale = 0;
    store.onParticipantsStale = () => stale++;
    store.applyNotification("participant/updated", { participant_id: "p1" });
    expect(stale).toBe(1);
  });

  it("seedLastViewed restores cursors without clobbering newer state", () => {
    const store = new AppStore();
    store.setThreads([
      chatThread({ turns: [{ id: "turn-9", items: [], items_view: "full", status: "completed" }] }),
    ]);
    store.markViewed("t1");
    store.seedLastViewed({ t1: "turn-1", other: "turn-2" });
    expect(store.getSnapshot().lastViewed).toEqual({ t1: "turn-9", other: "turn-2" });
  });
});

describe("marks", () => {
  it("upserts by (seq, participant_id, kind) — replace, never append", () => {
    const store = seeded();
    store.applyNotification("message/mark", {
      thread_id: "t1",
      seq: 5,
      participant_id: "p1",
      kind: "reaction",
      reaction: "eyes",
    });
    store.applyNotification("message/mark", {
      thread_id: "t1",
      seq: 5,
      participant_id: "p1",
      kind: "reaction",
      reaction: "nice",
    });
    store.applyNotification("message/mark", {
      thread_id: "t1",
      seq: 5,
      participant_id: "p1",
      kind: "seen",
      status: "completed",
    });
    const marks = store.getSnapshot().marks["t1"];
    expect(marks).toHaveLength(2);
    expect(marks.find((m) => m.kind === "reaction")?.reaction).toBe("nice");
  });
});

describe("unread cursor", () => {
  it("markViewed advances to the newest completed turn", () => {
    const store = new AppStore();
    store.setThreads([
      chatThread({
        turns: [
          { id: "turn-1", items: [], items_view: "full", status: "completed" },
          { id: "turn-2", items: [], items_view: "full", status: "in_progress" },
        ],
      }),
    ]);
    store.markViewed("t1");
    expect(store.getSnapshot().lastViewed["t1"]).toBe("turn-1");
  });
});
