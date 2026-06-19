import { describe, expect, it } from "vitest";
import {
  buildComposerSlashCommands,
  composerSlashPrompt,
  filterComposerSlashCommands,
  runtimeFastModelTarget
} from "./ComposerSlashCommands";
import type { InitializeResult, SkillSummary } from "../shared/protocol";

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

function skill(overrides: Partial<SkillSummary> & Pick<SkillSummary, "name">): SkillSummary {
  return {
    name: overrides.name,
    source: overrides.source ?? "bundled",
    user_invocable: overrides.user_invocable ?? true,
    disable_model_invoke: overrides.disable_model_invoke ?? false,
    description: overrides.description,
    when_to_use: overrides.when_to_use,
    trigger_condition: overrides.trigger_condition,
    argument_hint: overrides.argument_hint,
    model: overrides.model,
    context: overrides.context,
    agent: overrides.agent,
    paths: overrides.paths,
    examples: overrides.examples
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

  it("adds user-invocable skills to slash results", () => {
    const commands = buildComposerSlashCommands({
      activeContext: { kind: "project", project_id: "repo", cwd: "/repo" },
      initialized: initialized("gpt-5.5", ["gpt-5.5"]),
      running: false,
      skills: [
        skill({
          name: "slides",
          description: "Create slide decks",
          argument_hint: "topic"
        }),
        skill({
          name: "internal-only",
          user_invocable: false
        })
      ]
    });

    const visible = filterComposerSlashCommands(commands, "slides");

    expect(visible.map((command) => command.name)).toEqual(["slides"]);
    expect(visible[0]?.kind).toBe("skill");
    expect(composerSlashPrompt(visible[0]!, "")).toBe("/slides ");
    expect(composerSlashPrompt(visible[0]!, "quarterly roadmap")).toBe("/slides quarterly roadmap");
    expect(filterComposerSlashCommands(commands, "internal-only")).toEqual([]);
  });
});
