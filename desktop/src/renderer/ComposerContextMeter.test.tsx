import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { ComposerContextMeter } from "./ComposerContextMeter";
import type { TurnContextUsage } from "./AppState";

let container: HTMLDivElement;
let root: Root | null = null;

function usageWith(
  overrides: Partial<TurnContextUsage> = {},
): TurnContextUsage {
  return {
    turnID: "turn-1",
    used: 45_000,
    window: 200_000,
    inputTokens: 30_000,
    cacheCreationTokens: 3_000,
    cacheReadTokens: 12_000,
    ...overrides,
  };
}

beforeEach(() => {
  container = document.createElement("div");
  document.body.appendChild(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  root = null;
  document
    .querySelectorAll(".floating-menu-layer")
    .forEach((node) => node.remove());
  container.remove();
});

function renderMeter(usage: TurnContextUsage | undefined): HTMLDivElement {
  act(() => {
    root = createRoot(container);
    root.render(<ComposerContextMeter usage={usage} />);
  });
  return container;
}

describe("ComposerContextMeter", () => {
  it("hides entirely when no usage snapshot is provided", () => {
    renderMeter(undefined);
    expect(container.querySelector(".composer-context-meter")).toBeNull();
  });

  it("hides when the model window is zero (unknown / unsupported)", () => {
    renderMeter(usageWith({ window: 0 }));
    expect(container.querySelector(".composer-context-meter")).toBeNull();
  });

  it("hides when the model window is negative", () => {
    renderMeter(usageWith({ window: -1 }));
    expect(container.querySelector(".composer-context-meter")).toBeNull();
  });

  it("renders the ring track and progress stroke", () => {
    renderMeter(usageWith());
    expect(
      container.querySelector(".composer-context-meter-track"),
    ).not.toBeNull();
    expect(
      container.querySelector(".composer-context-meter-progress"),
    ).not.toBeNull();
  });

  it("renders the latest used / window value inline", () => {
    renderMeter(usageWith({ used: 45_000, window: 200_000 }));
    expect(
      container.querySelector(".composer-context-meter-label")?.textContent,
    ).toBe("45k / 200k");
  });

  it("does not render any center text — the ring is the readout", () => {
    // Per the user's spec: "圆环 + hover 显明细". A small 20×20 ring
    // does not have room for legible center text, and the percentage
    // belongs to the hover tooltip and aria-label, not the visible ring.
    renderMeter(usageWith({ used: 50_000, window: 200_000 }));
    expect(container.querySelector("text")).toBeNull();
  });

  it("renders an empty ring (no fill) when used is zero", () => {
    // The catalog-fallback case: model is known (window > 0) but no
    // turn has run yet, so used is 0. The progress stroke should be
    // at its full circumference offset — nothing visible.
    renderMeter(
      usageWith({
        used: 0,
        inputTokens: 0,
        cacheCreationTokens: 0,
        cacheReadTokens: 0,
      }),
    );
    const progress = container.querySelector(
      ".composer-context-meter-progress",
    );
    expect(progress).not.toBeNull();
    const offset = Number(progress?.getAttribute("stroke-dashoffset") ?? "0");
    // Full circumference means the ring is fully hidden.
    expect(offset).toBeGreaterThan(0);
  });

  it("encodes used / window / percent in the aria label", () => {
    renderMeter(usageWith({ used: 45_000, window: 200_000 }));
    const aria =
      container
        .querySelector(".composer-context-meter")
        ?.getAttribute("aria-label") ?? "";
    expect(aria).toContain("最近占用 45k");
    expect(aria).toContain("200k");
    expect(aria).toContain("23%");
  });

  it("mounts the breakdown tooltip in a portal on focus", () => {
    renderMeter(usageWith());
    const meter = container.querySelector<HTMLElement>(
      ".composer-context-meter",
    );
    expect(
      container.querySelector(".composer-context-meter-tooltip"),
    ).toBeNull();
    act(() => {
      meter?.focus();
    });
    const tooltip = document.body.querySelector(
      ".composer-context-meter-tooltip",
    );
    expect(tooltip).not.toBeNull();
    expect(meter?.getAttribute("aria-describedby")).toBe(tooltip?.id);
    // Headline: used/window formatted as "45k / 200k".
    expect(
      tooltip?.querySelector(
        ".composer-context-meter-tooltip-headline",
      )?.textContent ?? "",
    ).toContain("45k / 200k");
    // Breakdown rows: 输入 / 缓存读取 / 新建缓存.
    const text = tooltip?.textContent ?? "";
    expect(text).toContain("30k");
    expect(text).toContain("12k");
    expect(text).toContain("3k");
    expect(text).toContain("输入");
    expect(text).toContain("缓存读取");
    expect(text).toContain("新建缓存");
  });

  it("is focusable so keyboard users can reach the tooltip", () => {
    renderMeter(usageWith());
    const root = container.querySelector(".composer-context-meter");
    expect(root?.getAttribute("tabindex")).toBe("0");
    expect(root?.getAttribute("role")).toBe("status");
  });

  it("shrinks the progress stroke-dashoffset as fill grows", () => {
    // Same window, two fill levels → larger fill → smaller strokeDashoffset
    // (the progress arc reveals more).
    const a = document.createElement("div");
    document.body.appendChild(a);
    let aRoot: Root | null = createRoot(a);
    act(() => {
      aRoot!.render(
        <ComposerContextMeter usage={usageWith({ used: 40_000 })} />,
      );
    });
    const offsetSmall = Number(
      a
        .querySelector(".composer-context-meter-progress")
        ?.getAttribute("stroke-dashoffset") ?? "0",
    );
    act(() => {
      aRoot!.unmount();
      aRoot = createRoot(a);
      aRoot!.render(
        <ComposerContextMeter usage={usageWith({ used: 160_000 })} />,
      );
    });
    const offsetLarge = Number(
      a
        .querySelector(".composer-context-meter-progress")
        ?.getAttribute("stroke-dashoffset") ?? "0",
    );
    act(() => {
      aRoot?.unmount();
      aRoot = null;
      a.remove();
    });
    expect(offsetLarge).toBeLessThan(offsetSmall);
  });
});
