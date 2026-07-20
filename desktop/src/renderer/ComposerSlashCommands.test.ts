import { afterAll, afterEach, describe, expect, it, vi } from "vitest";

vi.hoisted(() => {
  vi.stubEnv("VITE_ENABLE_COLLABORATION", "true");
});

import {
  buildComposerSlashCommands,
  buildSideThreadSlashCommands,
  composerSlashPrompt,
  filterComposerSlashCommands,
  runtimeFastModelTarget
} from "./ComposerSlashCommands";
import type { InitializeResult, SkillSummary } from "../shared/protocol";
import { setActiveLocale } from "./i18n";

afterEach(() => setActiveLocale("zh-CN"));

afterAll(() => {
  vi.unstubAllEnvs();
});

function initialized(model: string, models: string[], helpMe = false): InitializeResult {
  return {
    protocol_version: "1",
    workspace_root: "/repo",
    provider: "openai",
    model,
    effort: "",
    variant: "",
    features: { helpme: helpMe },
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
  it("rebuilds built-in and side-thread commands in the active language", () => {
    setActiveLocale("en-US");

    const commands = buildComposerSlashCommands({ running: false });
    const review = commands.find((command) => command.name === "review");
    const sideReset = buildSideThreadSlashCommands()[0];

    expect(review).toMatchObject({
      title: "Review current changes",
      disabledReason: "Select a workspace first",
    });
    expect(sideReset).toMatchObject({
      title: "Reset side chat",
      tag: "Side chat",
    });
  });
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

  it("adds /compact as a local action instead of a model prompt", () => {
    const commands = buildComposerSlashCommands({
      activeContext: { kind: "project", project_id: "repo", cwd: "/repo" },
      initialized: initialized("gpt-5.5", ["gpt-5.5"]),
      running: false
    });

    const compact = filterComposerSlashCommands(commands, "compact")[0];

    expect(compact?.kind).toBe("action");
    expect(compact?.action).toBe("compact");
    expect(compact?.disabledReason).toBeUndefined();
  });

  it("disables /side until the draft has a persisted thread", () => {
    const commands = buildComposerSlashCommands({
      activeContext: { kind: "project", project_id: "repo", cwd: "/repo" },
      initialized: initialized("gpt-5.5", ["gpt-5.5"]),
      running: false,
      sideThreadDisabledReason: "先发送一条消息"
    });

    const side = filterComposerSlashCommands(commands, "side")[0];
    expect(side?.action).toBe("open-side-thread");
    expect(side?.disabledReason).toBe("先发送一条消息");
  });

  it("can disable /compact for surfaces without a compactable model context", () => {
    const commands = buildComposerSlashCommands({
      activeContext: { kind: "project", project_id: "repo", cwd: "/repo" },
      initialized: initialized("gpt-5.5", ["gpt-5.5"]),
      running: false,
      compactDisabledReason: "群聊暂不支持上下文压缩"
    });

    const compact = filterComposerSlashCommands(commands, "compact")[0];

    expect(compact?.kind).toBe("action");
    expect(compact?.disabledReason).toBe("群聊暂不支持上下文压缩");
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
      initialized: initialized("gpt-5.5", ["gpt-5.5"], true),
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

  it("hides /helpme unless the runtime feature is enabled", () => {
    const activeContext = { kind: "project", project_id: "repo", cwd: "/repo" } as const;
    const disabled = buildComposerSlashCommands({
      activeContext,
      initialized: initialized("gpt-5.5", ["gpt-5.5"]),
      running: false
    });
    const enabled = buildComposerSlashCommands({
      activeContext,
      initialized: initialized("gpt-5.5", ["gpt-5.5"], true),
      running: false
    });

    expect(filterComposerSlashCommands(disabled, "helpme")).toEqual([]);
    expect(filterComposerSlashCommands(disabled, "rescue")).toEqual([]);
    expect(filterComposerSlashCommands(enabled, "helpme").map((command) => command.name)).toEqual(["helpme"]);
  });
});
