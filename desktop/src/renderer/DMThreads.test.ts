import { describe, expect, it } from "vitest";
import type { Thread, Turn } from "../shared/protocol";
import {
  busyDMParticipantIDs,
  findDMThread,
  isDMThread,
  scratchThreadSummaries,
  summarizeThreadsForSidebar,
} from "./AppState";

function makeThread(
  overrides: Partial<Thread> = {},
): Thread {
  return {
    id: "thread",
    preview: "preview",
    title: "title",
    model_provider: "test",
    model: "test-model",
    cwd: "/repo",
    workspace_kind: "project",
    status: "idle",
    pinned: false,
    archived: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    turns: [],
    ...overrides,
  };
}

const projects = [
  {
    id: "project-1",
    name: "wuu",
    path: "/repo/wuu",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  },
];

describe("isDMThread", () => {
  it("returns true when dm_participant_id is a non-empty string", () => {
    expect(isDMThread({ dm_participant_id: "participant-1" })).toBe(true);
  });

  it("returns false when dm_participant_id is missing or empty", () => {
    expect(isDMThread({})).toBe(false);
    expect(isDMThread({ dm_participant_id: undefined })).toBe(false);
    expect(isDMThread({ dm_participant_id: "" })).toBe(false);
  });
});

describe("findDMThread", () => {
  it("picks the latest non-archived DM for a participant", () => {
    const older = makeThread({
      id: "dm-older",
      workspace_kind: "dm",
      dm_participant_id: "participant-1",
      updated_at: "2026-01-01T00:00:00Z",
    });
    const newer = makeThread({
      id: "dm-newer",
      workspace_kind: "dm",
      dm_participant_id: "participant-1",
      updated_at: "2026-02-01T00:00:00Z",
    });
    expect(findDMThread([older, newer], "participant-1")?.id).toBe("dm-newer");
  });

  it("ignores archived threads even when newer", () => {
    const archived = makeThread({
      id: "dm-archived",
      workspace_kind: "dm",
      dm_participant_id: "participant-1",
      updated_at: "2026-03-01T00:00:00Z",
      archived: true,
    });
    const live = makeThread({
      id: "dm-live",
      workspace_kind: "dm",
      dm_participant_id: "participant-1",
      updated_at: "2026-01-01T00:00:00Z",
    });
    expect(findDMThread([archived, live], "participant-1")?.id).toBe("dm-live");
  });

  it("returns undefined when only archived DMs exist for the participant", () => {
    const archived = makeThread({
      id: "dm-archived",
      workspace_kind: "dm",
      dm_participant_id: "participant-1",
      archived: true,
    });
    expect(findDMThread([archived], "participant-1")).toBeUndefined();
  });

  it("returns undefined when no DM threads match the participant", () => {
    const otherDM = makeThread({
      id: "dm-other",
      workspace_kind: "dm",
      dm_participant_id: "participant-other",
    });
    const nonDM = makeThread({ id: "regular" });
    expect(findDMThread([otherDM, nonDM], "participant-1")).toBeUndefined();
  });

  it("returns undefined for empty or invalid participant id", () => {
    const dm = makeThread({
      workspace_kind: "dm",
      dm_participant_id: "participant-1",
    });
    expect(findDMThread([dm], "")).toBeUndefined();
    // The picker must not match against every thread when given a bogus
    // participant id; otherwise an empty sidebar list could shadow an
    // active DM.
    expect(findDMThread([], "participant-1")).toBeUndefined();
  });

  it("only matches DM-kind threads; legacy project-cwd DMs are skipped", () => {
    // Legacy DM: tagged with dm_participant_id but persists a project cwd
    // (pre per-agent-home-dir). Even when this is the newest candidate, it
    // must be skipped so a fresh correctly-homed DM is created instead.
    const legacyStray = makeThread({
      id: "dm-legacy",
      cwd: "/repo/wuu",
      workspace_kind: "project",
      dm_participant_id: "participant-1",
      updated_at: "2026-05-01T00:00:00Z",
    });
    // Even when the legacy is the only candidate, findDMThread must return
    // undefined so openParticipantDM starts a new thread with the correct
    // home directory instead of resurrecting the mis-homed legacy.
    expect(findDMThread([legacyStray], "participant-1")).toBeUndefined();

    // Mixed list: newer "dm"-kind wins, the legacy is skipped even when newer.
    const newerLegacy = makeThread({
      id: "dm-legacy-newer",
      cwd: "/repo/wuu",
      workspace_kind: "project",
      dm_participant_id: "participant-1",
      updated_at: "2026-06-01T00:00:00Z",
    });
    const olderProper = makeThread({
      id: "dm-proper",
      cwd: "/home/user/.wuu/agents/participant-1/home",
      workspace_kind: "dm",
      dm_participant_id: "participant-1",
      updated_at: "2026-01-01T00:00:00Z",
    });
    expect(findDMThread([newerLegacy, olderProper], "participant-1")?.id).toBe(
      "dm-proper",
    );

    // Missing workspace_kind must also be treated as not-a-proper-DM, so a
    // server snapshot that has not yet populated the new field does not
    // accidentally revive a pre-fix legacy thread.
    const missingKind = makeThread({
      id: "dm-missing-kind",
      cwd: "/repo/wuu",
      dm_participant_id: "participant-1",
      updated_at: "2026-07-01T00:00:00Z",
    });
    expect(
      findDMThread([missingKind], "participant-1"),
    ).toBeUndefined();
  });
});

describe("findDMThread proper-kind fixtures", () => {
  it("requires workspace_kind \"dm\" on fixtures to be selectable", () => {
    // Re-confirm the happy path still resolves when fixtures carry the new
    // workspace_kind field, so a future test fixture tightening does not
    // silently hide regressions in findDMThread.
    const proper = makeThread({
      id: "dm-proper",
      workspace_kind: "dm",
      dm_participant_id: "participant-1",
      updated_at: "2026-02-01T00:00:00Z",
    });
    expect(findDMThread([proper], "participant-1")?.id).toBe("dm-proper");
  });
});

function makeRunningTurn(id = "turn-running"): Turn {
  return { id, items: [], items_view: "full", status: "in_progress" };
}

describe("busyDMParticipantIDs", () => {
  it("marks a participant busy when its DM thread status is in_progress", () => {
    const dm = makeThread({
      id: "dm-running",
      workspace_kind: "dm",
      dm_participant_id: "participant-1",
      status: "in_progress",
    });
    expect(busyDMParticipantIDs([dm])).toEqual(new Set(["participant-1"]));
  });

  it("marks a participant busy when a turn in its DM thread is in_progress", () => {
    const dm = makeThread({
      id: "dm-turn-running",
      workspace_kind: "dm",
      dm_participant_id: "participant-1",
      turns: [makeRunningTurn()],
    });
    expect(busyDMParticipantIDs([dm])).toEqual(new Set(["participant-1"]));
  });

  it("returns an empty set when the DM thread is idle", () => {
    const dm = makeThread({
      id: "dm-idle",
      workspace_kind: "dm",
      dm_participant_id: "participant-1",
    });
    expect(busyDMParticipantIDs([dm]).size).toBe(0);
  });

  it("ignores archived DM threads even when running", () => {
    const dm = makeThread({
      id: "dm-archived",
      workspace_kind: "dm",
      dm_participant_id: "participant-1",
      status: "in_progress",
      archived: true,
    });
    expect(busyDMParticipantIDs([dm]).size).toBe(0);
  });

  it("ignores legacy project-kind DM threads even when running", () => {
    const legacy = makeThread({
      id: "dm-legacy",
      workspace_kind: "project",
      dm_participant_id: "participant-1",
      status: "in_progress",
    });
    expect(busyDMParticipantIDs([legacy]).size).toBe(0);
  });

  it("ignores running threads that are not DMs", () => {
    const regular = makeThread({
      id: "regular-running",
      status: "in_progress",
    });
    expect(busyDMParticipantIDs([regular]).size).toBe(0);
  });

  it("handles undefined thread lists", () => {
    expect(busyDMParticipantIDs(undefined).size).toBe(0);
  });

  it("collects each busy participant once across multiple DM threads", () => {
    const first = makeThread({
      id: "dm-a",
      workspace_kind: "dm",
      dm_participant_id: "participant-1",
      status: "in_progress",
    });
    const second = makeThread({
      id: "dm-b",
      workspace_kind: "dm",
      dm_participant_id: "participant-1",
      turns: [makeRunningTurn("turn-b")],
    });
    const other = makeThread({
      id: "dm-c",
      workspace_kind: "dm",
      dm_participant_id: "participant-2",
      status: "in_progress",
    });
    expect(busyDMParticipantIDs([first, second, other])).toEqual(
      new Set(["participant-1", "participant-2"]),
    );
  });
});

describe("scratchThreadSummaries DM filtering", () => {
  it("excludes DM threads from the 对话 scratch list", () => {
    const regular = makeThread({
      id: "regular",
      cwd: "/nowhere",
      workspace_kind: "scratch",
    });
    const dm = makeThread({
      id: "dm",
      cwd: "/nowhere",
      workspace_kind: "scratch",
      dm_participant_id: "participant-1",
    });
    const summaries = summarizeThreadsForSidebar([regular, dm]);
    const scratch = scratchThreadSummaries(summaries, projects);
    const ids = scratch.map((thread) => thread.id);
    expect(ids).toContain("regular");
    expect(ids).not.toContain("dm");
  });
});