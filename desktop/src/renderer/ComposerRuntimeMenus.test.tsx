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

  it("opens the model panel from the trigger with a single click", () => {
    const onToggleMenu = vi.fn();
    renderPicker(null, runtimeWithEffort(), onToggleMenu);

    const trigger = document.querySelector<HTMLButtonElement>(".codex-runtime-trigger");
    expect(trigger?.getAttribute("aria-expanded")).toBe("false");
    expect(trigger?.textContent).toContain("Claude Sonnet");
    expect(trigger?.textContent).toContain("Medium");

    act(() => trigger?.click());

    expect(onToggleMenu).toHaveBeenCalledWith("model");
  });

  it("opens the model panel directly with search, provider groups, and the effort slider", () => {
    renderPicker("model", runtimeWithEffort());

    const menu = document.querySelector<HTMLElement>(".codex-model-menu");
    expect(menu).not.toBeNull();
    // The intermediate main menu is gone.
    expect(document.querySelector(".codex-main-menu")).toBeNull();

    const search = menu?.querySelector<HTMLInputElement>(".select-menu-search input");
    expect(search).not.toBeNull();
    expect(search?.placeholder).toBe("搜索模型");

    const groupLabels = Array.from(menu?.querySelectorAll<HTMLElement>(".codex-model-group-label") ?? []);
    expect(groupLabels.map((label) => label.textContent)).toEqual(["work"]);
    const modelItems = Array.from(menu?.querySelectorAll<HTMLButtonElement>(".codex-model-item") ?? []);
    expect(modelItems.map((item) => item.textContent?.trim())).toEqual(["Claude Sonnet"]);
    expect(modelItems[0]?.getAttribute("aria-checked")).toBe("true");

    const slider = menu?.querySelector<HTMLInputElement>(".codex-effort-slider");
    expect(slider?.type).toBe("range");
    expect(slider?.min).toBe("0");
    expect(slider?.max).toBe("3");
    expect(slider?.value).toBe("2");
    expect(menu?.textContent).toContain("Medium");

    // The capsule is a bare pill: no level text on it — the heading above
    // live-shows the current level, and small dots mark the available stops.
    expect(menu?.querySelector(".codex-effort-marks")).toBeNull();
    expect(menu?.querySelector(".codex-effort-mark")).toBeNull();
    expect(menu?.querySelector(".codex-effort-current")?.textContent).toBe("Medium");
    expect(menu?.querySelectorAll(".codex-effort-stop")).toHaveLength(3);
    const wrap = menu?.querySelector<HTMLElement>(".codex-effort-slider-wrap");
    expect(wrap?.style.getPropertyValue("--effort-slider-fill")).toBe("75%");
    expect(wrap?.style.getPropertyValue("--effort-slider-pos")).toBe("75%");
    // The drag pearl sits on the current stop; mid-scale is not charged.
    const capsule = menu?.querySelector<HTMLElement>(".codex-effort-capsule");
    expect(capsule?.querySelector(".codex-effort-knob")).not.toBeNull();
    expect(capsule?.classList.contains("maxed")).toBe(false);
    expect(capsule?.querySelector(".codex-effort-capsule-sheen")).toBeNull();
  });

  it("builds permission labels in the active language", () => {
    setActiveLocale("en-US");

    expect(permissionModeOption("standard")).toMatchObject({
      label: "Full trust within workspace",
      chipLabel: "Standard",
    });
  });

  it("hides the effort slider when the model does not expose reasoning levels", () => {
    const initialized = runtimeWithEffort();
    initialized.variant = "";
    initialized.providers![0].models = [{ id: "claude-sonnet", display_name: "Claude Sonnet" }];

    renderPicker("model", initialized);

    expect(document.querySelector(".codex-effort-slider")).toBeNull();
    const menu = document.querySelector<HTMLElement>(".codex-model-menu");
    expect(menu?.querySelectorAll(".codex-model-item")).toHaveLength(1);
  });

  it("filters models by name or provider through the search box", () => {
    const initialized = runtimeWithEffort();
    initialized.providers![0].models = [
      { id: "claude-sonnet", display_name: "Claude Sonnet", supported_efforts: ["low", "medium", "high"] },
      { id: "claude-opus", display_name: "Claude Opus", supported_efforts: ["low", "medium", "high"] }
    ];
    renderPicker("model", initialized);

    const search = document.querySelector<HTMLInputElement>(".select-menu-search input")!;
    const setSearchValue = (value: string): void => {
      const valueSetter = Object.getOwnPropertyDescriptor(
        HTMLInputElement.prototype,
        "value",
      )?.set;
      valueSetter?.call(search, value);
      search.dispatchEvent(new Event("input", { bubbles: true }));
    };
    act(() => setSearchValue("opus"));

    const items = Array.from(document.querySelectorAll<HTMLButtonElement>(".codex-model-item"));
    expect(items.map((item) => item.textContent?.trim())).toEqual(["Claude Opus"]);

    act(() => setSearchValue("no-such-model"));
    expect(document.querySelector(".composer-menu-empty")?.textContent).toBe("没有匹配的模型");
  });

  it("previews the dragged effort in the heading and commits once on release", () => {
    const onSelectEffort = vi.fn();
    renderPicker("model", runtimeWithEffort(), vi.fn(), onSelectEffort);

    const slider = document.querySelector<HTMLInputElement>(".codex-effort-slider")!;
    act(() => {
      const valueSetter = Object.getOwnPropertyDescriptor(
        HTMLInputElement.prototype,
        "value",
      )?.set;
      valueSetter?.call(slider, "3");
      slider.dispatchEvent(new Event("input", { bubbles: true }));
      slider.dispatchEvent(new Event("pointerup", { bubbles: true }));
    });

    expect(onSelectEffort).toHaveBeenCalledTimes(1);
    expect(onSelectEffort).toHaveBeenCalledWith("high");
    expect(document.querySelector(".codex-effort-current")?.textContent).toBe("High");
    // Landing on the top level switches on the charged state.
    const capsule = document.querySelector(".codex-effort-capsule");
    expect(capsule?.classList.contains("maxed")).toBe(true);
    expect(capsule?.querySelector(".codex-effort-capsule-sheen")).not.toBeNull();
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

  it("uses the target model default instead of carrying effort across models, with optimistic highlighting", () => {
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
      document.querySelectorAll<HTMLButtonElement>(".codex-model-menu .codex-model-item")
    );
    const modelB = choices.find((choice) => choice.textContent?.includes("Model B"))!;
    act(() => modelB?.click());

    expect(onSelectModel).toHaveBeenCalledWith("work", "model-b", "medium");
    // Optimistic in-panel state: the clicked row highlights and the slider
    // snaps to the target model's default before the stream round-trip.
    expect(modelB.getAttribute("aria-checked")).toBe("true");
    const slider = document.querySelector<HTMLInputElement>(".codex-effort-slider");
    expect(slider?.value).toBe("1");
  });
});
