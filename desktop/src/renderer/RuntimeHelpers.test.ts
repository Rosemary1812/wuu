import { afterEach, describe, expect, it } from "vitest";
import type { ProviderModelSummary, ProviderSummary } from "../shared/protocol";
import {
  codexEffortLabel,
  pullRequestUnavailableReason,
  providerModelReasoningMode,
  providerModelVariantOptions
} from "./RuntimeHelpers";
import { setActiveLocale } from "./i18n";

afterEach(() => setActiveLocale("zh-CN"));

function providerWithModel(model: ProviderModelSummary | undefined): ProviderSummary | undefined {
  if (!model) {
    return undefined;
  }
  return {
    name: "fake",
    type: "openai-compatible",
    model: model.id,
    models: [model]
  };
}

describe("codexEffortLabel", () => {
  it("uses canonical English labels in every locale", () => {
    expect(codexEffortLabel("xhigh")).toBe("Extra high");
    expect(codexEffortLabel("max")).toBe("Max");
    expect(codexEffortLabel("ultra")).toBe("Ultra");

    setActiveLocale("en-US");

    expect(codexEffortLabel("xhigh")).toBe("Extra high");
    expect(codexEffortLabel("max")).toBe("Max");
  });

  it("uses the active language for generated runtime labels", () => {
    setActiveLocale("en-US");

    expect(pullRequestUnavailableReason()).toBe("Not a Git repository");
  });
});

describe("providerModelVariantOptions", () => {
  it("returns only the default option when the model exposes no levels", () => {
    const provider = providerWithModel({
      id: "no-levels",
      supported_efforts: [],
      capabilities: { chat: true, tools: true, structured_output: true, streaming: true, system_role: true, reasoning: true }
    });
    expect(providerModelVariantOptions(provider, "no-levels", "")).toEqual(["", "none"]);
  });

  it("does not append the 'none' toggle when the model cannot reason", () => {
    const provider = providerWithModel({
      id: "no-reasoning",
      supported_efforts: [],
      capabilities: { chat: true, tools: true, structured_output: true, streaming: true, system_role: true, reasoning: false }
    });
    expect(providerModelVariantOptions(provider, "no-reasoning", "")).toEqual([""]);
  });

  it("includes every supported effort in the option list", () => {
    const provider = providerWithModel({
      id: "with-levels",
      supported_efforts: ["low", "medium", "high"],
      capabilities: { chat: true, tools: true, structured_output: true, streaming: true, system_role: true, reasoning: true }
    });
    expect(providerModelVariantOptions(provider, "with-levels", "")).toEqual(["", "low", "medium", "high"]);
  });

  it("prefers explicit variants over supported_efforts", () => {
    const provider = providerWithModel({
      id: "with-variants",
      variants: [{ id: "fast" }, { id: "thorough" }],
      supported_efforts: ["low", "medium"],
      capabilities: { chat: true, tools: true, structured_output: true, streaming: true, system_role: true, reasoning: true }
    });
    expect(providerModelVariantOptions(provider, "with-variants", "")).toEqual(["", "fast", "thorough"]);
  });

  it("preserves an unknown current variant so the user can still see it selected", () => {
    const provider = providerWithModel({
      id: "with-levels",
      supported_efforts: ["low", "medium"],
      capabilities: { chat: true, tools: true, structured_output: true, streaming: true, system_role: true, reasoning: true }
    });
    expect(providerModelVariantOptions(provider, "with-levels", "xhigh")).toEqual([
      "",
      "low",
      "medium",
      "xhigh"
    ]);
  });

  it("returns just the default option when no provider is configured", () => {
    expect(providerModelVariantOptions(undefined, "anything", "")).toEqual([""]);
  });
});

describe("providerModelReasoningMode", () => {
  it("reports 'off' when the model has no reasoning capability", () => {
    const provider = providerWithModel({
      id: "no-reasoning",
      supported_efforts: [],
      capabilities: { chat: true, tools: true, structured_output: true, streaming: true, system_role: true, reasoning: false }
    });
    expect(providerModelReasoningMode(provider, "no-reasoning")).toBe("off");
  });

  it("reports 'off' when the model has no capabilities block at all", () => {
    const provider = providerWithModel({ id: "no-caps", supported_efforts: [] });
    expect(providerModelReasoningMode(provider, "no-caps")).toBe("off");
  });

  it("reports 'toggle' when reasoning is on but no levels are exposed", () => {
    const provider = providerWithModel({
      id: "toggle-only",
      supported_efforts: [],
      capabilities: { chat: true, tools: true, structured_output: true, streaming: true, system_role: true, reasoning: true }
    });
    expect(providerModelReasoningMode(provider, "toggle-only")).toBe("toggle");
  });

  it("reports 'levels' when the model exposes adjustable efforts", () => {
    const provider = providerWithModel({
      id: "with-levels",
      supported_efforts: ["low", "medium", "high"],
      capabilities: { chat: true, tools: true, structured_output: true, streaming: true, system_role: true, reasoning: true }
    });
    expect(providerModelReasoningMode(provider, "with-levels")).toBe("levels");
  });

  it("reports 'levels' when the model exposes named variants", () => {
    const provider = providerWithModel({
      id: "with-variants",
      variants: [{ id: "fast" }, { id: "thorough" }],
      capabilities: { chat: true, tools: true, structured_output: true, streaming: true, system_role: true, reasoning: true }
    });
    expect(providerModelReasoningMode(provider, "with-variants")).toBe("levels");
  });

  it("reports 'off' when no provider is configured", () => {
    expect(providerModelReasoningMode(undefined, "anything")).toBe("off");
  });
});
