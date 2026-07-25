import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ModelAliasSummary, ProviderSummary, WuuDesktopApi } from "../shared/protocol";
import { I18nProvider } from "./i18n";
import { SubagentModelAliases } from "./SubagentModelAliases";

describe("SubagentModelAliases", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);
    window.wuu = {
      initialLanguagePreference: "en-US",
      initialSystemLocale: "en-US",
    } as unknown as WuuDesktopApi;
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    vi.restoreAllMocks();
  });

  function renderComponent(props: {
    aliases?: Record<string, ModelAliasSummary>;
    providers?: ProviderSummary[];
    disabled?: boolean;
    onSave?: (update: { model_aliases?: Record<string, ModelAliasSummary> }) => Promise<void>;
  }): { onSave: typeof props.onSave } {
    const onSave = props.onSave ?? vi.fn().mockResolvedValue(undefined);
    act(() => {
      root.render(
        <I18nProvider>
          <SubagentModelAliases
            aliases={props.aliases}
            providers={props.providers ?? []}
            disabled={props.disabled}
            onSave={onSave as (update: { model_aliases?: Record<string, ModelAliasSummary> }) => Promise<void>}
          />
        </I18nProvider>,
      );
    });
    return { onSave };
  }

  function setInputValue(input: HTMLInputElement, value: string): void {
    act(() => {
      const descriptor = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value");
      descriptor?.set?.call(input, value);
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });
  }

  function selectOption(row: Element, field: string, value: string): void {
    act(() => {
      const trigger = row.querySelector(`[data-testid="subagent-alias-${field}-select"]`) as HTMLButtonElement | null;
      trigger?.click();
    });
    act(() => {
      const option = document.querySelector(`[data-value="${value}"]`) as HTMLButtonElement | null;
      option?.click();
    });
  }

  function aliasRows(): HTMLElement[] {
    return Array.from(container.querySelectorAll(".settings-alias-row"));
  }

  it("renders the hint and empty state", () => {
    renderComponent({});
    expect(container.textContent).toContain("Subagent model aliases");
    expect(container.textContent).toContain("Aliases are passed as spawn_agent.model values.");
    expect(aliasRows()).toHaveLength(0);

    const toolbar = container.querySelector(".settings-alias-toolbar");
    expect(toolbar?.querySelector(".settings-alias-heading")).not.toBeNull();
    expect(toolbar?.querySelectorAll(".settings-alias-actions button")).toHaveLength(2);
    expect(container.querySelector(".settings-row-footer")).toBeNull();
  });

  it("renders existing aliases from props", () => {
    renderComponent({
      aliases: {
        cheap: { provider: "openai", model: "gpt-5-mini" },
        frontend: { provider: "anthropic", model: "claude-sonnet", effort: "high" },
      },
      providers: [
        { name: "openai", type: "openai", model: "gpt-5" },
        { name: "anthropic", type: "anthropic", model: "claude-sonnet" },
      ],
    });
    const rows = aliasRows();
    expect(rows).toHaveLength(2);
    expect(rows[0]?.querySelector("input")?.value).toBe("cheap");
    expect(rows[1]?.querySelector("input")?.value).toBe("frontend");
  });

  it("adds a new row and saves a valid alias", async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    renderComponent({
      providers: [{ name: "openai", type: "openai", model: "gpt-5" }],
      onSave,
    });

    await act(async () => {
      const addButton = Array.from(container.querySelectorAll("button")).find(
        (button) => button.textContent === "Add alias",
      );
      addButton?.click();
    });

    const rows = aliasRows();
    expect(rows).toHaveLength(1);
    const [row] = rows;
    const [aliasInput, modelInput] = Array.from(row.querySelectorAll("input"));
    setInputValue(aliasInput, "review");
    selectOption(row, "provider", "openai");
    setInputValue(modelInput, "gpt-5");

    await act(async () => {
      const saveButton = Array.from(container.querySelectorAll("button")).find(
        (button) => button.textContent === "Save aliases",
      );
      saveButton?.click();
    });

    expect(onSave).toHaveBeenCalledOnce();
    expect(onSave).toHaveBeenCalledWith({
      model_aliases: {
        review: { provider: "openai", model: "gpt-5" },
      },
    });
    expect(container.textContent).not.toContain("Alias must start with a letter");
  });

  it("rejects duplicate aliases and empty provider/model", async () => {
    renderComponent({
      providers: [{ name: "openai", type: "openai", model: "gpt-5" }],
    });

    await act(async () => {
      Array.from(container.querySelectorAll("button"))
        .find((button) => button.textContent === "Add alias")
        ?.click();
      Array.from(container.querySelectorAll("button"))
        .find((button) => button.textContent === "Add alias")
        ?.click();
    });

    const rows = aliasRows();
    const row1 = rows[0];
    const row2 = rows[1];
    if (!row1 || !row2) throw new Error("expected two rows");

    const aliasInput1 = row1.querySelector("input")!;
    setInputValue(aliasInput1, "review");
    const aliasInput2 = row2.querySelector("input")!;
    setInputValue(aliasInput2, "review");

    await act(async () => {
      Array.from(container.querySelectorAll("button"))
        .find((button) => button.textContent === "Save aliases")
        ?.click();
    });

    expect(container.textContent).toContain("Alias name is already used.");
    expect(container.textContent).toContain("Select a provider.");
    expect(container.textContent).toContain("Enter a model.");
  });

  it("rejects invalid alias syntax", async () => {
    renderComponent({
      providers: [{ name: "openai", type: "openai", model: "gpt-5" }],
    });

    await act(async () => {
      Array.from(container.querySelectorAll("button"))
        .find((button) => button.textContent === "Add alias")
        ?.click();
    });

    const row = aliasRows()[0];
    const aliasInput = row.querySelector("input")!;
    setInputValue(aliasInput, "123-invalid");

    await act(async () => {
      Array.from(container.querySelectorAll("button"))
        .find((button) => button.textContent === "Save aliases")
        ?.click();
    });

    expect(container.textContent).toContain("Alias must start with a letter");
  });

  it("deletes a row", async () => {
    renderComponent({
      aliases: {
        cheap: { provider: "openai", model: "gpt-5-mini" },
      },
      providers: [{ name: "openai", type: "openai", model: "gpt-5" }],
    });

    await act(async () => {
      const removeButton = container.querySelector(".settings-alias-remove") as HTMLButtonElement | null;
      removeButton?.click();
    });

    expect(aliasRows()).toHaveLength(0);
  });

  it("validates the selected reasoning variant against the model catalog", async () => {
    renderComponent({
      providers: [
        {
          name: "openai",
          type: "openai",
          model: "gpt-5",
          models: [
            {
              id: "gpt-5",
              supported_efforts: ["low", "medium", "high"],
            },
          ],
        },
      ],
    });

    await act(async () => {
      Array.from(container.querySelectorAll("button"))
        .find((button) => button.textContent === "Add alias")
        ?.click();
    });

    const row = aliasRows()[0];
    const [aliasInput, modelInput] = Array.from(row.querySelectorAll("input"));
    setInputValue(aliasInput, "review");
    selectOption(row, "provider", "openai");
    setInputValue(modelInput, "gpt-5");
    selectOption(row, "reasoning", "high");

    await act(async () => {
      Array.from(container.querySelectorAll("button"))
        .find((button) => button.textContent === "Save aliases")
        ?.click();
    });

    expect(container.textContent).not.toContain("Selected reasoning is not supported");
  });

  it("saves a selected reasoning effort as an effort field", async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    renderComponent({
      providers: [
        {
          name: "openai",
          type: "openai",
          model: "gpt-5",
          models: [
            {
              id: "gpt-5",
              supported_efforts: ["low", "medium", "high"],
            },
          ],
        },
      ],
      onSave,
    });

    await act(async () => {
      Array.from(container.querySelectorAll("button"))
        .find((button) => button.textContent === "Add alias")
        ?.click();
    });

    const row = aliasRows()[0];
    const [aliasInput, modelInput] = Array.from(row.querySelectorAll("input"));
    setInputValue(aliasInput, "review");
    selectOption(row, "provider", "openai");
    setInputValue(modelInput, "gpt-5");
    selectOption(row, "reasoning", "medium");

    await act(async () => {
      Array.from(container.querySelectorAll("button"))
        .find((button) => button.textContent === "Save aliases")
        ?.click();
    });

    expect(onSave).toHaveBeenCalledWith({
      model_aliases: {
        review: { provider: "openai", model: "gpt-5", effort: "medium" },
      },
    });
  });

  it("rejects an unsupported reasoning variant", async () => {
    renderComponent({
      providers: [
        {
          name: "openai",
          type: "openai",
          model: "gpt-5",
          models: [
            {
              id: "gpt-5",
              supported_efforts: ["low", "medium"],
            },
            {
              id: "gpt-5-mini",
              supported_efforts: ["low"],
            },
          ],
        },
      ],
    });

    await act(async () => {
      Array.from(container.querySelectorAll("button"))
        .find((button) => button.textContent === "Add alias")
        ?.click();
    });

    const row = aliasRows()[0];
    const [aliasInput, modelInput] = Array.from(row.querySelectorAll("input"));
    setInputValue(aliasInput, "review");
    selectOption(row, "provider", "openai");
    setInputValue(modelInput, "gpt-5");

    // Select a valid variant, then switch to a model that does not support it.
    // The variant value should remain and be caught by validation.
    selectOption(row, "reasoning", "medium");
    setInputValue(modelInput, "gpt-5-mini");

    await act(async () => {
      Array.from(container.querySelectorAll("button"))
        .find((button) => button.textContent === "Save aliases")
        ?.click();
    });

    expect(container.textContent).toContain("Selected reasoning is not supported");
  });

  it("surfaces a save error from the update handler", async () => {
    const onSave = vi.fn().mockRejectedValue(new Error("backend refused"));
    renderComponent({
      providers: [{ name: "openai", type: "openai", model: "gpt-5" }],
      onSave,
    });

    await act(async () => {
      Array.from(container.querySelectorAll("button"))
        .find((button) => button.textContent === "Add alias")
        ?.click();
    });

    const row = aliasRows()[0];
    const [aliasInput, modelInput] = Array.from(row.querySelectorAll("input"));
    setInputValue(aliasInput, "review");
    selectOption(row, "provider", "openai");
    setInputValue(modelInput, "gpt-5");

    await act(async () => {
      Array.from(container.querySelectorAll("button"))
        .find((button) => button.textContent === "Save aliases")
        ?.click();
    });

    expect(container.textContent).toContain("backend refused");
  });
});
