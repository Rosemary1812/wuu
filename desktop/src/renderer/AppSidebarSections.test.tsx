import { describe, expect, it } from "vitest";
import {
  reconcileSidebarSectionOrder,
  reorderSidebarSections,
  SIDEBAR_SECTION_PINNED,
} from "./AppSidebar";
import { SCRATCH_PSEUDO_PROJECT_ID } from "./AppState";

describe("reconcileSidebarSectionOrder", () => {
  it("builds the default order from scratch and project ids", () => {
    expect(
      reconcileSidebarSectionOrder(undefined, ["project-1", "project-2"]),
    ).toEqual([SCRATCH_PSEUDO_PROJECT_ID, "project-1", "project-2"]);
  });

  it("preserves stored workspace order and appends newly seen projects", () => {
    expect(
      reconcileSidebarSectionOrder(
        ["project-2", SCRATCH_PSEUDO_PROJECT_ID, "project-1"],
        ["project-1", "project-2", "project-3"],
      ),
    ).toEqual([
      "project-2",
      SCRATCH_PSEUDO_PROJECT_ID,
      "project-1",
      "project-3",
    ]);
  });

  it("drops fixed, legacy collaboration, unknown, and duplicate ids", () => {
    expect(
      reconcileSidebarSectionOrder(
        [
          "__wuu_group__",
          SIDEBAR_SECTION_PINNED,
          "project-2",
          "__wuu_agents__",
          "project-2",
          "__unknown__",
        ],
        ["project-1", "project-2"],
      ),
    ).toEqual([SCRATCH_PSEUDO_PROJECT_ID, "project-2", "project-1"]);
  });

  it("restores scratch without disturbing a stored project prefix", () => {
    expect(
      reconcileSidebarSectionOrder(["project-2", "project-1"], ["project-1", "project-2"]),
    ).toEqual([SCRATCH_PSEUDO_PROJECT_ID, "project-2", "project-1"]);
  });
});

describe("reorderSidebarSections", () => {
  it("moves one workspace section to another section's position", () => {
    expect(
      reorderSidebarSections(
        [SCRATCH_PSEUDO_PROJECT_ID, "project-1", "project-2"],
        "project-2",
        SCRATCH_PSEUDO_PROJECT_ID,
      ),
    ).toEqual(["project-2", SCRATCH_PSEUDO_PROJECT_ID, "project-1"]);
  });

  it("keeps the original order when a drag has no valid destination", () => {
    const order = [SCRATCH_PSEUDO_PROJECT_ID, "project-1"];
    expect(reorderSidebarSections(order, "project-1", null)).toBe(order);
    expect(reorderSidebarSections(order, "project-1", "unknown")).toBe(order);
    expect(reorderSidebarSections(order, "unknown", "project-1")).toBe(order);
  });
});
