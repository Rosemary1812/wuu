import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it } from "vitest";
import { RuntimeLoading, ViewSwitchLoading } from "./LoadingViews";

let root: Root | undefined;
let container: HTMLDivElement | undefined;

function render(view: JSX.Element): HTMLDivElement {
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  act(() => root?.render(view));
  return container;
}

afterEach(() => {
  act(() => root?.unmount());
  root = undefined;
  container?.remove();
  container = undefined;
});

describe("RuntimeLoading", () => {
  it("uses the mascot as a transparent carving inside one glass app-icon surface", () => {
    const view = render(<RuntimeLoading status="connecting" />);

    const glass = view.querySelector(".wuu-launch-glass");
    const carving = glass?.querySelector<HTMLImageElement>(
      "img.wuu-launch-carving",
    );

    expect(glass).not.toBeNull();
    expect(carving?.getAttribute("src")).toContain("mascot-face");
    expect(view.querySelector(".wuu-launch-mark")).toBeNull();
    expect(view.querySelector(".wuu-launch-rail")).toBeNull();
  });

  it("keeps the compact legacy loader for view switches", () => {
    const view = render(<ViewSwitchLoading />);

    expect(view.querySelector(".wuu-launch-mark")).not.toBeNull();
    expect(view.querySelector(".wuu-launch-rail")).not.toBeNull();
    expect(view.querySelector(".wuu-launch-glass")).toBeNull();
  });
});
