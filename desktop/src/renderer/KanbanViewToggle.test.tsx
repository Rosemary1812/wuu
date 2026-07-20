import { act, createElement } from "react";
import { createRoot } from "react-dom/client";
import { describe, expect, it, vi } from "vitest";
import { KanbanViewToggle } from "./KanbanViewToggle";

describe("KanbanViewToggle", () => {
  it("marks the current view and switches between messages and board", () => {
    const container = document.createElement("div");
    const root = createRoot(container);
    const onChange = vi.fn();

    act(() => {
      root.render(createElement(KanbanViewToggle, { mode: "message", onChange }));
    });

    const tabs = Array.from(container.querySelectorAll<HTMLButtonElement>('[role="tab"]'));
    expect(tabs).toHaveLength(2);
    expect(tabs[0]?.getAttribute("aria-selected")).toBe("true");
    expect(tabs[1]?.getAttribute("aria-selected")).toBe("false");

    act(() => tabs[1]?.click());
    expect(onChange).toHaveBeenCalledWith("board");

    act(() => {
      root.render(createElement(KanbanViewToggle, { mode: "board", onChange }));
    });
    expect(tabs[0]?.getAttribute("aria-selected")).toBe("false");
    expect(tabs[1]?.getAttribute("aria-selected")).toBe("true");

    act(() => tabs[0]?.click());
    expect(onChange).toHaveBeenLastCalledWith("message");
    act(() => root.unmount());
  });
});
