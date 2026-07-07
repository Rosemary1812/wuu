/**
 * Drag-resize coverage for the thread (reply subthread) panel separator.
 * The feature shipped with clamp + hover tests but no drag test — and the real
 * regression was CSS, not this logic (a local `--thread-panel-width` on
 * `.conversation-pane` shadowed the value the drag live-writes to `.app-shell`,
 * see conversation-shell.test.ts). This locks the hook side: a pointerdown +
 * window pointermove must move the width.
 */
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useAppLayoutState } from "./AppLayoutState";

function pointerDown(clientX: number): React.PointerEvent<HTMLDivElement> {
  return {
    button: 0,
    clientX,
    pointerId: 1,
    preventDefault: vi.fn(),
  } as unknown as React.PointerEvent<HTMLDivElement>;
}

let root: Root | null = null;
afterEach(() => {
  act(() => root?.unmount());
  root = null;
});

function renderHook(rootEl: HTMLElement): { get: () => ReturnType<typeof useAppLayoutState> } {
  const container = document.createElement("div");
  document.body.appendChild(container);
  let latest: ReturnType<typeof useAppLayoutState> | null = null;
  function Harness(): null {
    latest = useAppLayoutState({
      layoutRootRef: { current: rootEl },
      onCloseProjectMenu: () => {},
    });
    return null;
  }
  act(() => {
    root = createRoot(container);
    root.render(<Harness />);
  });
  return { get: () => latest! };
}

describe("thread panel separator drag", () => {
  it("arms the resize on a primary-button pointerdown", () => {
    window.localStorage.clear();
    const rootEl = document.createElement("div");
    document.body.appendChild(rootEl);
    const hook = renderHook(rootEl);

    expect(hook.get().resizingThreadPanel).toBe(false);
    act(() => hook.get().startThreadPanelResize(pointerDown(800)));
    expect(hook.get().resizingThreadPanel).toBe(true);

    act(() => window.dispatchEvent(new Event("pointerup", { bubbles: true })));
    rootEl.remove();
  });

  it("dragging the separator left widens the panel", () => {
    window.localStorage.clear();
    // Wide window so the clamp ceiling leaves room to widen.
    Object.defineProperty(window, "innerWidth", { value: 2000, configurable: true });
    const rootEl = document.createElement("div");
    document.body.appendChild(rootEl);
    const hook = renderHook(rootEl);

    const before = hook.get().clampedThreadPanelWidth;
    // Right-anchored: dragging LEFT (800 -> 650) widens by 150.
    act(() => hook.get().startThreadPanelResize(pointerDown(800)));
    act(() =>
      window.dispatchEvent(Object.assign(new Event("pointermove"), { clientX: 650 })),
    );
    act(() => window.dispatchEvent(new Event("pointerup", { bubbles: true })));

    expect(hook.get().clampedThreadPanelWidth).toBe(before + 150);
    rootEl.remove();
  });
});
