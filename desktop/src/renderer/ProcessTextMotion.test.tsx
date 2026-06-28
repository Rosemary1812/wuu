import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it } from "vitest";
import { AnimatedProcessText } from "./ProcessTextMotion";

let container: HTMLDivElement | null = null;
let root: Root | null = null;

function mount(text: string): void {
  if (container) unmount();
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  act(() => {
    root!.render(<AnimatedProcessText text={text} />);
  });
}

function rerender(text: string): void {
  act(() => {
    root!.render(<AnimatedProcessText text={text} />);
  });
}

function unmount(): void {
  if (root) {
    act(() => {
      root!.unmount();
    });
    root = null;
  }
  if (container) {
    container.remove();
    container = null;
  }
}

afterEach(() => {
  unmount();
});

describe("AnimatedProcessText", () => {
  it("does not mark the initial text as an entering transition", () => {
    mount("正在思考");

    const current = container?.querySelector(".process-text-motion-current");
    expect(current?.textContent).toBe("正在思考");
    expect(current?.classList.contains("process-text-motion-enter")).toBe(
      false,
    );
  });

  it("marks replacement text as an entering transition", () => {
    mount("正在思考");
    rerender("思考过程");

    const current = container?.querySelector(".process-text-motion-current");
    const exit = container?.querySelector(".process-text-motion-exit");
    expect(current?.textContent).toBe("思考过程");
    expect(current?.classList.contains("process-text-motion-enter")).toBe(
      true,
    );
    expect(exit?.textContent).toBe("正在思考");
  });
});
