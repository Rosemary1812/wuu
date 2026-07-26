import { describe, expect, it } from "vitest";
import type { RuntimeContext, Thread } from "../shared/protocol";
import {
  managementAssistantThreadStartParams,
  retainOpenManagementAssistantSessions,
  managementAssistantRequestContext,
  userVisibleThreads,
} from "./SkillsAssistant";

describe("Skills assistant surface context", () => {
  it("starts Skills management with dedicated authority without changing automations yet", () => {
    expect(managementAssistantThreadStartParams("skills")).toEqual({
      ephemeral: true,
      managementSurface: "skills",
    });
    expect(managementAssistantThreadStartParams("automations")).toEqual({ ephemeral: true });
  });

  it("builds request-only context for the Skills catalog and active workspace", () => {
    const context: RuntimeContext = {
      kind: "project",
      cwd: "/tmp/wuu",
      project_id: "project-1",
    };

    const requestContext = managementAssistantRequestContext("skills", context);

    expect(requestContext).toContain('"surface": "skills_catalog"');
    expect(requestContext).toContain('"cwd": "/tmp/wuu"');
    expect(requestContext).toContain("Create or edit Skill files directly");
  });

  it("builds automation-management context around the cron tool", () => {
    const context: RuntimeContext = {
      kind: "no_project",
      cwd: "/tmp/wuu",
    };

    const requestContext = managementAssistantRequestContext("automations", context);

    expect(requestContext).toContain('"surface": "automations_catalog"');
    expect(requestContext).toContain("Use the cron tool to inspect current automations");
    expect(requestContext).toContain("create, update, pause, resume, or remove automations");
  });

  it("keeps ephemeral assistant threads out of user-facing history", () => {
    const visible = thread("thread-visible");
    const ephemeral = { ...thread("ephemeral-1"), ephemeral: true };

    expect(userVisibleThreads([visible, ephemeral])).toEqual([visible]);
  });

  it("retains sessions while tabs stay open and drops them only after close", () => {
    const sessions = {
      "skills:project-1": { draft: "follow up", status: "", threadID: "thread-1" },
      "automations:project-1": { draft: "", status: "ready", threadID: "thread-2" },
    };

    expect(
      retainOpenManagementAssistantSessions(
        sessions,
        new Set(["skills:project-1", "automations:project-1"]),
      ),
    ).toBe(sessions);
    expect(
      retainOpenManagementAssistantSessions(
        sessions,
        new Set(["automations:project-1"]),
      ),
    ).toEqual({
      "automations:project-1": sessions["automations:project-1"],
    });
  });
});

function thread(id: string): Thread {
  return {
    id,
    preview: "",
    model_provider: "test",
    model: "test-model",
    cwd: "/tmp/wuu",
    status: "idle",
    created_at: "2026-07-26T00:00:00Z",
    updated_at: "2026-07-26T00:00:00Z",
    turns: [],
  };
}
