import { describe, expect, it } from "vitest";
import { buildComposerSlashCommands, filterComposerSlashCommands, runtimeFastModelTarget } from "./ComposerSlashCommands";
import type { InitializeResult } from "../shared/protocol";

function initialized(model: string, models: string[]): InitializeResult {
  return {
    protocol_version: "1",
    workspace_root: "/repo",
    provider: "openai",
    model,
    effort: "",
    variant: "",
    providers: [
      {
        name: "openai",
        type: "openai",
        model,
        models: models.map((id) => ({ id }))
      }
    ]
  };
}

describe("composer slash commands", () => {
  it("shows /fast only when the current provider exposes a fast model", () => {
    const commands = buildComposerSlashCommands({
      activeContext: { kind: "project", project_id: "repo", cwd: "/repo" },
      initialized: initialized("gpt-5.5", ["gpt-5.5", "gpt-5.5-fast"]),
      running: false
    });

    expect(filterComposerSlashCommands(commands, "fast").map((command) => command.name)).toEqual(["fast"]);
    expect(runtimeFastModelTarget(initialized("gpt-5.5", ["gpt-5.5", "gpt-5.5-fast"]))).toEqual({
      provider: "openai",
      model: "gpt-5.5-fast",
      current: false
    });
  });

  it("hides /fast when the current provider has no fast model", () => {
    const commands = buildComposerSlashCommands({
      activeContext: { kind: "project", project_id: "repo", cwd: "/repo" },
      initialized: initialized("mimo-v2.5-pro", ["mimo-v2.5-pro"]),
      running: false
    });

    expect(filterComposerSlashCommands(commands, "fast")).toEqual([]);
    expect(runtimeFastModelTarget(initialized("mimo-v2.5-pro", ["mimo-v2.5-pro"]))).toBeUndefined();
  });

  it("keeps /fast visible but disabled when already in fast mode", () => {
    const commands = buildComposerSlashCommands({
      activeContext: { kind: "project", project_id: "repo", cwd: "/repo" },
      initialized: initialized("gpt-5.5-fast", ["gpt-5.5", "gpt-5.5-fast"]),
      running: false
    });
    const fast = filterComposerSlashCommands(commands, "fast")[0];

    expect(fast?.disabledReason).toBe("当前已是快速模式");
    expect(runtimeFastModelTarget(initialized("gpt-5.5-fast", ["gpt-5.5", "gpt-5.5-fast"]))).toEqual({
      provider: "openai",
      model: "gpt-5.5-fast",
      current: true
    });
  });
});
