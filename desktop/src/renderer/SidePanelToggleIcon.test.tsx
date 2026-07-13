import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { SidePanelToggleIcon } from "./SidePanelToggleIcon";

let container: HTMLDivElement;
let root: Root | null = null;

beforeEach(() => {
  container = document.createElement("div");
  document.body.appendChild(container);
});

afterEach(() => {
  act(() => root?.unmount());
  root = null;
  container.remove();
});

describe("SidePanelToggleIcon", () => {
  it("uses the 18px titlebar icon box with an optically full frame", () => {
    root = createRoot(container);
    act(() => root?.render(<SidePanelToggleIcon side="left" open />));

    const icon = container.querySelector("svg");
    const frame = container.querySelector(".side-panel-toggle-frame");

    expect(icon?.getAttribute("width")).toBe("18");
    expect(icon?.getAttribute("height")).toBe("18");
    expect(icon?.getAttribute("viewBox")).toBe("0 0 18 18");
    expect(Number(frame?.getAttribute("width"))).toBeGreaterThanOrEqual(14.5);
    expect(Number(frame?.getAttribute("height"))).toBeGreaterThanOrEqual(14.5);
  });
});
