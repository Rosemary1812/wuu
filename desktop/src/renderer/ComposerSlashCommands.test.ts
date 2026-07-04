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

  it("routes /memory to the memory settings action instead of instructions", () => {
    const commands = buildComposerSlashCommands({
      activeContext: { kind: "project", project_id: "repo", cwd: "/repo" },
      initialized: initialized("gpt-5.5", ["gpt-5.5"]),
      running: false
    });

    const memory = commands.find((command) => command.name === "memory");
    expect(memory?.kind).toBe("action");
    expect(memory?.action).toBe("open-memory");
    expect(memory?.disabledReason).toBeUndefined();

    const instructions = commands.find((command) => command.name === "instructions");
    expect(instructions?.action).toBe("instructions");
    expect(instructions?.aliases).not.toContain("memory");
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

  it("adds /compact as a prompt command that sends the raw slash text", () => {
    const commands = buildComposerSlashCommands({
      activeContext: { kind: "project", project_id: "repo", cwd: "/repo" },
      initialized: initialized("gpt-5.5", ["gpt-5.5"]),
      running: false
    });

    const compact = filterComposerSlashCommands(commands, "compact")[0];

    expect(compact?.kind).toBe("prompt");
    expect(compact?.disabledReason).toBeUndefined();
    // The backend detects the raw "/compact" text (isManualCompactPrompt)
    // and forces a compaction pass before the turn's first model request,
    // so the composer must send the slash text through unchanged.
    expect(composerSlashPrompt(compact!, "")).toBe("/compact ");
    expect(composerSlashPrompt(compact!, "后续只保留结论")).toBe("/compact 后续只保留结论");
  });

  it("adds /context as a local action instead of a model prompt", () => {
    const commands = buildComposerSlashCommands({
      activeContext: { kind: "project", project_id: "repo", cwd: "/repo" },
      initialized: initialized("gpt-5.5", ["gpt-5.5"]),
      running: false
    });

    const context = filterComposerSlashCommands(commands, "context")[0];

    expect(context?.kind).toBe("action");
    expect(context?.action).toBe("context");
  });

  it("keeps task slash commands as command text", () => {
    const commands = buildComposerSlashCommands({
      activeContext: { kind: "project", project_id: "repo", cwd: "/repo" },
      initialized: initialized("gpt-5.5", ["gpt-5.5"]),
      running: false
    });

    const debug = filterComposerSlashCommands(commands, "debug")[0];
    const fix = filterComposerSlashCommands(commands, "fix")[0];
    const helpme = filterComposerSlashCommands(commands, "helpme")[0];
    const commit = filterComposerSlashCommands(commands, "commit")[0];
    const pr = filterComposerSlashCommands(commands, "pr")[0];

    expect(debug?.kind).toBe("prompt");
    expect(composerSlashPrompt(debug!, "login failure")).toBe("/debug login failure");
    expect(composerSlashPrompt(fix!, "")).toBe("/fix ");
    expect(composerSlashPrompt(helpme!, "still stuck")).toBe("/helpme still stuck");
    expect(composerSlashPrompt(commit!, "")).toBe("/commit ");
    expect(composerSlashPrompt(pr!, "draft")).toBe("/pr draft");
  });
});
