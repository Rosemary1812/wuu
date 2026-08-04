import { act, createRef } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { InitializeResult } from "../shared/protocol";
import { permissionModeOption, RuntimePicker } from "./ComposerRuntimeMenus";
import type { CodexRuntimeMenu } from "./ComposerTypes";
import { setActiveLocale } from "./i18n";

describe("RuntimePicker", () => {
  let container: HTMLDivElement;
  let root: Root | null = null;

  beforeEach(() => {
    container = document.createElement("div");
    document.body.appendChild(container);
  });

  afterEach(() => {
    act(() => root?.unmount());
    root = null;
    setActiveLocale("zh-CN");
    document
      .querySelectorAll('[data-floating-menu-owner="codex-runtime"]')
      .forEach((element) => element.remove());
    container.remove();
  });

  function renderPicker(
    openMenu: CodexRuntimeMenu,
    initialized: InitializeResult,
    onToggleMenu = vi.fn(),
    onSelectEffort = vi.fn(),
    onSelectModel = vi.fn(),
    anchorRef = createRef<HTMLDivElement>()
  ): void {
    act(() => {
      root ??= createRoot(container);
      root.render(
        <RuntimePicker
          variant="dock"
          initialized={initialized}
          state={{ loading: false, error: "", models: [] }}
          openMenu={openMenu}
          anchorRef={anchorRef}
          running={false}
          onToggleMenu={onToggleMenu}
          onSelectModel={onSelectModel}
          onSelectEffort={onSelectEffort}
        />
      );
    });
  }

  function runtimeWithEffort(): InitializeResult {
    return {
      protocol_version: "wuu-app-server/v0.1",
      provider: "work",
      model: "claude-sonnet",
      variant: "medium",
      workspace_root: "/tmp/project",
      providers: [
        {
          name: "work",
          type: "anthropic",
          model: "claude-sonnet",
          models: [
            {
              id: "claude-sonnet",
              display_name: "Claude Sonnet",
              supported_efforts: ["low", "medium", "high"]
            }
          ]
        }
      ]
    };
  }

  it("shows compact Model and Effort rows without unsupported controls", () => {
    const onToggleMenu = vi.fn();
    renderPicker("main", runtimeWithEffort(), onToggleMenu);

    const menu = document.querySelector<HTMLElement>(".codex-main-menu");
    const rows = Array.from(menu?.querySelectorAll<HTMLButtonElement>("button") ?? []);

    expect(rows.map((row) => row.textContent?.trim())).toEqual([
      "ModelClaude Sonnet",
      "Effort中"
    ]);
    expect(menu?.textContent).not.toContain("Speed");
    expect(menu?.textContent).not.toContain("Advanced");

    act(() => rows[0]?.click());
    act(() => rows[1]?.click());
    expect(onToggleMenu).toHaveBeenNthCalledWith(1, "model");
    expect(onToggleMenu).toHaveBeenNthCalledWith(2, "effort");
  });

  it("builds permission labels in the active language", () => {
    setActiveLocale("en-US");

    expect(permissionModeOption("standard")).toMatchObject({
      label: "Full trust within workspace",
      chipLabel: "Standard",
    });
  });

  it("hides Effort when the model does not expose reasoning levels", () => {
    const initialized = runtimeWithEffort();
    initialized.variant = "";
    initialized.providers![0].models = [{ id: "claude-sonnet", display_name: "Claude Sonnet" }];

    renderPicker("main", initialized);

    const menu = document.querySelector<HTMLElement>(".codex-main-menu");
    expect(menu?.querySelectorAll("button")).toHaveLength(1);
    expect(menu?.textContent).toBe("ModelClaude Sonnet");
  });

  it("keeps effort choices in a dedicated submenu", () => {
    const onSelectEffort = vi.fn();
    renderPicker("effort", runtimeWithEffort(), vi.fn(), onSelectEffort);

    const choices = Array.from(
      document.querySelectorAll<HTMLButtonElement>(".codex-effort-menu button")
    );
    const high = choices.find((choice) => choice.textContent?.trim() === "高");

    act(() => high?.click());
    expect(onSelectEffort).toHaveBeenCalledWith("high");
  });

  it("flips the model menu below the trigger when the window top has too little room", () => {
    const initialized = runtimeWithEffort();
    const anchorRef = createRef<HTMLDivElement>();
    renderPicker(null, initialized, vi.fn(), vi.fn(), vi.fn(), anchorRef);
    vi.spyOn(anchorRef.current as HTMLDivElement, "getBoundingClientRect").mockReturnValue({
      x: 700,
      y: 40,
      top: 40,
      left: 700,
      right: 880,
      bottom: 70,
      width: 180,
      height: 30,
      toJSON: () => ({}),
    });

    renderPicker("model", initialized, vi.fn(), vi.fn(), vi.fn(), anchorRef);

    const layer = document.querySelector<HTMLElement>(
      '[data-floating-menu-owner="codex-runtime"]'
    );
    expect(layer?.classList.contains("floating-menu-below")).toBe(true);
    expect(layer?.style.top).toBe("78px");
    expect(layer?.style.bottom).toBe("");
    expect(layer?.style.getPropertyValue("--floating-menu-available-height")).toBe(
      `${window.innerHeight - 86}px`
    );
  });

  it("uses the target model default instead of carrying effort across models", () => {
    const initialized = runtimeWithEffort();
    initialized.model = "model-a";
    initialized.variant = "max";
    initialized.providers![0].model = "model-a";
    initialized.providers![0].models = [
      {
        id: "model-a",
        display_name: "Model A",
        default_effort: "medium",
        supported_efforts: ["medium", "max"]
      },
      {
        id: "model-b",
        display_name: "Model B",
        default_effort: "medium",
        supported_efforts: ["medium", "max"]
      }
    ];
    const onSelectModel = vi.fn();

    renderPicker("model", initialized, vi.fn(), vi.fn(), onSelectModel);

    const choices = Array.from(
      document.querySelectorAll<HTMLButtonElement>(".codex-model-menu button")
    );
    const modelB = choices.find((choice) => choice.textContent?.includes("Model B"));
    act(() => modelB?.click());

    expect(onSelectModel).toHaveBeenCalledWith("work", "model-b", "medium");
  });
});
