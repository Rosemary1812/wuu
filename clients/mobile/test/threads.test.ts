// List semantics mirrored from the desktop: strict DM predicate, two-band
// ordering, and the purely-local unread cursor.

import { describe, expect, it } from "vitest";
import type { Thread } from "@wuu/protocol";

import {
  isChatThread,
  isDMThread,
  isGroupThread,
  isThreadRunning,
  isThreadUnread,
  latestCompletedTurnID,
  sortChatThreads,
  threadDisplayTitle,
} from "../src/lib/threads";

function thread(partial: Partial<Thread>): Thread {
  return {
    id: "t",
    preview: "",
    model_provider: "p",
    model: "m",
    cwd: "/",
    status: "idle",
    created_at: "2026-07-07T00:00:00Z",
    updated_at: "2026-07-07T00:00:00Z",
    turns: [],
    ...partial,
  } as Thread;
}

describe("predicates", () => {
  it("DM membership keys on dm_participant_id alone", () => {
    expect(isDMThread(thread({ dm_participant_id: "p1", workspace_kind: "dm" }))).toBe(true);
    // thread/list entries for non-resident threads omit workspace_kind
    // entirely — the DM must still show up on the phone.
    expect(isDMThread(thread({ dm_participant_id: "p1" }))).toBe(true);
    expect(isDMThread(thread({ dm_participant_id: "p1", workspace_kind: "project" }))).toBe(true);
    expect(isDMThread(thread({ workspace_kind: "dm" }))).toBe(false);
  });

  it("chat threads exclude archived and read-only", () => {
    expect(isGroupThread(thread({ group: true }))).toBe(true);
    expect(isChatThread(thread({ group: true, archived: true }))).toBe(false);
    expect(isChatThread(thread({ group: true, read_only: true }))).toBe(false);
    expect(isChatThread(thread({ workspace_kind: "project" }))).toBe(false);
  });

  it("running considers thread status and turn status", () => {
    expect(isThreadRunning(thread({ status: "in_progress" }))).toBe(true);
    expect(
      isThreadRunning(
        thread({ turns: [{ id: "x", items: [], items_view: "full", status: "in_progress" }] }),
      ),
    ).toBe(true);
    expect(isThreadRunning(thread({}))).toBe(false);
  });

  it("display title falls back preview → 未命名对话", () => {
    expect(threadDisplayTitle(thread({ title: " 发布群 " }))).toBe("发布群");
    expect(threadDisplayTitle(thread({ preview: "早上好" }))).toBe("早上好");
    expect(threadDisplayTitle(thread({}))).toBe("未命名对话");
  });
});

describe("sortChatThreads", () => {
  it("running first by created_at desc, finished by updated_at desc", () => {
    const runningOld = thread({ id: "r-old", status: "in_progress", created_at: "2026-07-01T00:00:00Z" });
    const runningNew = thread({ id: "r-new", status: "in_progress", created_at: "2026-07-06T00:00:00Z" });
    const doneOld = thread({ id: "d-old", updated_at: "2026-07-02T00:00:00Z" });
    const doneNew = thread({ id: "d-new", updated_at: "2026-07-05T00:00:00Z" });
    const sorted = sortChatThreads([doneOld, runningOld, doneNew, runningNew]);
    expect(sorted.map((t) => t.id)).toEqual(["r-new", "r-old", "d-new", "d-old"]);
  });
});

describe("unread", () => {
  const finished = thread({
    id: "t1",
    turns: [
      { id: "turn-1", items: [], items_view: "full", status: "completed" },
      { id: "turn-2", items: [], items_view: "full", status: "completed" },
    ],
  });

  it("keys on the newest completed turn", () => {
    expect(latestCompletedTurnID(finished)).toBe("turn-2");
    expect(isThreadUnread(finished, {})).toBe(true);
    expect(isThreadUnread(finished, { t1: "turn-2" })).toBe(false);
    expect(isThreadUnread(finished, { t1: "turn-1" })).toBe(true);
  });

  it("running threads never mark unread", () => {
    const running = thread({
      id: "t1",
      turns: [
        { id: "turn-1", items: [], items_view: "full", status: "completed" },
        { id: "turn-2", items: [], items_view: "full", status: "in_progress" },
      ],
    });
    expect(isThreadUnread(running, {})).toBe(false);
    // The streaming turn is not the unread cursor either.
    expect(latestCompletedTurnID(running)).toBe("turn-1");
  });
});
