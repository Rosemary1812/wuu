import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ThreadContextMenu } from "./ThreadContextMenu";

let root: Root | undefined;
let container: HTMLDivElement | undefined;

afterEach(() => {
  if (root) {
    act(() => root?.unmount());
  }
  container?.remove();
  document.body.querySelectorAll(".thread-row-context-menu").forEach((menu) => menu.remove());
  root = undefined;
  container = undefined;
  vi.restoreAllMocks();
});

describe("ThreadContextMenu", () => {
  it("reuses viewport placement so a session menu stays visible near the window edge", () => {
    vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockReturnValue(180);
    vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockReturnValue(200);
    vi.spyOn(window, "innerWidth", "get").mockReturnValue(1000);
    vi.spyOn(window, "innerHeight", "get").mockReturnValue(800);

    container = document.createElement("div");
    document.body.appendChild(container);
    act(() => {
      root = createRoot(container!);
      root.render(
        <ThreadContextMenu
          x={950}
          y={700}
          items={[{ label: "归档", onSelect: () => {} }]}
          onClose={() => {}}
        />,
      );
    });

    const menu = document.body.querySelector<HTMLElement>(".thread-row-context-menu");
    expect(menu?.style.left).toBe("770px");
    expect(menu?.style.top).toBe("500px");
    expect(menu?.dataset.origin).toBe("bottom-right");
    expect(menu?.style.visibility).toBe("");
  });
});
