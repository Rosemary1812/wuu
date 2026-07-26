import { describe, expect, it } from "vitest";
import type { RuntimeContext, Thread } from "../shared/protocol";
import { skillsAssistantPrompt, userVisibleThreads } from "./SkillsAssistant";

describe("Skills assistant surface context", () => {
  it("scopes the raw query to the Skills catalog and active workspace", () => {
    const context: RuntimeContext = {
      kind: "project",
      cwd: "/tmp/wuu",
      project_id: "project-1",
    };

    const prompt = skillsAssistantPrompt("Make a release review Skill", context);

    expect(prompt).toContain('"surface": "skills_catalog"');
    expect(prompt).toContain('"cwd": "/tmp/wuu"');
    expect(prompt).toContain("<user_query>\nMake a release review Skill\n</user_query>");
  });

  it("keeps ephemeral assistant threads out of user-facing history", () => {
    const visible = thread("thread-visible");
    const ephemeral = { ...thread("ephemeral-1"), ephemeral: true };

    expect(userVisibleThreads([visible, ephemeral])).toEqual([visible]);
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
