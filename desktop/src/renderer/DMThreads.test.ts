import { describe, expect, it } from "vitest";
import type { Thread } from "../shared/protocol";
import {
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
      dm_participant_id: "participant-1",
      updated_at: "2026-01-01T00:00:00Z",
    });
    const newer = makeThread({
      id: "dm-newer",
      dm_participant_id: "participant-1",
      updated_at: "2026-02-01T00:00:00Z",
    });
    expect(findDMThread([older, newer], "participant-1")?.id).toBe("dm-newer");
  });

  it("ignores archived threads even when newer", () => {
    const archived = makeThread({
      id: "dm-archived",
      dm_participant_id: "participant-1",
      updated_at: "2026-03-01T00:00:00Z",
      archived: true,
    });
    const live = makeThread({
      id: "dm-live",
      dm_participant_id: "participant-1",
      updated_at: "2026-01-01T00:00:00Z",
    });
    expect(findDMThread([archived, live], "participant-1")?.id).toBe("dm-live");
  });

  it("returns undefined when only archived DMs exist for the participant", () => {
    const archived = makeThread({
      id: "dm-archived",
      dm_participant_id: "participant-1",
      archived: true,
    });
    expect(findDMThread([archived], "participant-1")).toBeUndefined();
  });

  it("returns undefined when no DM threads match the participant", () => {
    const otherDM = makeThread({
      id: "dm-other",
      dm_participant_id: "participant-other",
    });
    const nonDM = makeThread({ id: "regular" });
    expect(findDMThread([otherDM, nonDM], "participant-1")).toBeUndefined();
  });

  it("returns undefined for empty or invalid participant id", () => {
    const dm = makeThread({ dm_participant_id: "participant-1" });
    expect(findDMThread([dm], "")).toBeUndefined();
    // The picker must not match against every thread when given a bogus
    // participant id; otherwise an empty sidebar list could shadow an
    // active DM.
    expect(findDMThread([], "participant-1")).toBeUndefined();
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