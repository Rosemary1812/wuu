import { describe, expect, it } from "vitest";
import type { ActivitySession, ServerEvent } from "../shared/protocol";
import {
  activitiesForThread,
  clearActivitiesForWorkdir,
  emptyActivitySessions,
  mergeActivityList,
  reduceActivitySessionEvent,
} from "./ActivitySessions";

function session(overrides: Partial<ActivitySession> = {}): ActivitySession {
  return {
    id: "activity-1",
    kind: "browser",
    thread_id: "thread-1",
    workdir: "/repo-a",
    plugin_id: "browser-use",
    target: "https://example.test",
    state: "active",
    controller: "agent",
    created_at: "2026-07-10T10:00:00Z",
    updated_at: "2026-07-10T10:00:01Z",
    ...overrides,
  };
}

function notification(method: string, activity: ActivitySession): ServerEvent {
  return {
    kind: "notification",
    workdir: activity.workdir,
    message: { method, params: activity },
  };
}

describe("ActivitySessions", () => {
  it("routes activities by workspace and thread", () => {
    let state = emptyActivitySessions();
    state = reduceActivitySessionEvent(state, notification("activity/started", session()));
    state = reduceActivitySessionEvent(state, notification("activity/started", session({ id: "activity-2", thread_id: "thread-2" })));
    state = reduceActivitySessionEvent(state, notification("activity/started", session({ id: "activity-3", workdir: "/repo-b" })));

    expect(activitiesForThread(state, "/repo-a", "thread-1").map((item) => item.id)).toEqual(["activity-1"]);
    expect(activitiesForThread(state, "/repo-a", "thread-2").map((item) => item.id)).toEqual(["activity-2"]);
    expect(activitiesForThread(state, "/repo-b", "thread-1").map((item) => item.id)).toEqual(["activity-3"]);
  });

  it("ignores out-of-order updates and never resurrects stopped activities", () => {
    let state = emptyActivitySessions();
    state = reduceActivitySessionEvent(state, notification("activity/started", session()));
    state = reduceActivitySessionEvent(state, notification("activity/stopped", session({ state: "stopped", controller: "none", updated_at: "2026-07-10T10:00:03Z" })));
    state = reduceActivitySessionEvent(state, notification("activity/updated", session({ state: "active", updated_at: "2026-07-10T10:00:02Z" })));
    state = reduceActivitySessionEvent(state, notification("activity/updated", session({ state: "active", updated_at: "2026-07-10T10:00:04Z" })));

    expect(activitiesForThread(state, "/repo-a", "thread-1")[0]).toMatchObject({ state: "stopped", controller: "none" });
  });

  it("merges list snapshots without replacing newer events", () => {
    let state = emptyActivitySessions();
    state = reduceActivitySessionEvent(state, notification("activity/control_changed", session({ controller: "user", state: "user_controlled", updated_at: "2026-07-10T10:00:05Z" })));
    state = mergeActivityList(state, "/repo-a", "thread-1", [session({ updated_at: "2026-07-10T10:00:04Z" })]);

    expect(activitiesForThread(state, "/repo-a", "thread-1")[0]).toMatchObject({ controller: "user", state: "user_controlled" });
  });

  it("hard-clears a torn-down workdir, bypassing stopped stickiness", () => {
    let state = emptyActivitySessions();
    state = reduceActivitySessionEvent(state, notification("activity/started", session()));
    state = reduceActivitySessionEvent(state, notification("activity/started", session({ id: "activity-2", thread_id: "thread-2" })));
    state = reduceActivitySessionEvent(state, notification("activity/started", session({ id: "activity-3", workdir: "/repo-b" })));
    // A stopped activity is normally sticky; the invalidate path still drops it.
    state = reduceActivitySessionEvent(state, notification("activity/stopped", session({ id: "activity-2", thread_id: "thread-2", state: "stopped", controller: "none", updated_at: "2026-07-10T10:00:09Z" })));

    const cleared = clearActivitiesForWorkdir(state, "/repo-a");
    expect(activitiesForThread(cleared, "/repo-a", "thread-1")).toEqual([]);
    expect(activitiesForThread(cleared, "/repo-a", "thread-2")).toEqual([]);
    // Other workdirs are untouched.
    expect(activitiesForThread(cleared, "/repo-b", "thread-1").map((item) => item.id)).toEqual(["activity-3"]);
  });

  it("returns the same state object when nothing matched", () => {
    let state = emptyActivitySessions();
    state = reduceActivitySessionEvent(state, notification("activity/started", session()));
    expect(clearActivitiesForWorkdir(state, "/repo-unknown")).toBe(state);
  });
});
