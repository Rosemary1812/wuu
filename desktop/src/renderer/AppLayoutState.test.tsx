import { act, createRef } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useAppLayoutState } from "./AppLayoutState";
import { WINDOW_RESIZING_CLASS } from "./WindowResizeState";

interface Harness {
  startSidebarResize: ReturnType<typeof useAppLayoutState>["startSidebarResize"];
  startRightPanelResize: ReturnType<typeof useAppLayoutState>["startRightPanelResize"];
  setRightPanelOpenWithMotion: ReturnType<
    typeof useAppLayoutState
  >["setRightPanelOpenWithMotion"];
}

let container: HTMLDivElement;
let root: Root | null = null;
let latest: Harness | null = null;

function makePointerDownEvent(clientX: number): React.PointerEvent<HTMLDivElement> {
  // The hook only reads `button`, `clientX`, and `preventDefault`, so a plain
  // object shaped like a PointerEvent is enough for the reducer path.
  return {
    button: 0,
    clientX,
    preventDefault: vi.fn()
  } as unknown as React.PointerEvent<HTMLDivElement>;
}

function renderHookHarness(): void {
  function Harness(): null {
    const hook = useAppLayoutState({
      onCloseProjectMenu: () => {}
    });
    latest = {
      startSidebarResize: hook.startSidebarResize,
      startRightPanelResize: hook.startRightPanelResize,
      setRightPanelOpenWithMotion: hook.setRightPanelOpenWithMotion
    };
    return null;
  }

  act(() => {
    root = createRoot(container);
    root.render(<Harness />);
  });
}

beforeEach(() => {
  container = document.createElement("div");
  document.body.appendChild(container);
  // The hook reads sidebar collapse / width from localStorage on mount.
  window.localStorage.clear();
  // Wipe any leftover class from a previous test (defensive: the production
  // code path only adds it during a drag, but other tests in the same file
  // could leave it behind).
  document.documentElement.classList.remove(WINDOW_RESIZING_CLASS);
  latest = null;
});

afterEach(() => {
  document.documentElement.classList.remove(WINDOW_RESIZING_CLASS);
  act(() => {
    root?.unmount();
  });
  root = null;
  container.remove();
});

describe("useAppLayoutState window-resizing class", () => {
  it("adds window-resizing to <html> while a sidebar drag is active", () => {
    renderHookHarness();
    expect(latest).not.toBeNull();

    act(() => {
      latest!.startSidebarResize(makePointerDownEvent(100));
    });
    expect(document.documentElement.classList.contains(WINDOW_RESIZING_CLASS)).toBe(true);

    act(() => {
      window.dispatchEvent(new Event("pointerup", { bubbles: true }));
    });
    expect(document.documentElement.classList.contains(WINDOW_RESIZING_CLASS)).toBe(false);
  });

  it("adds window-resizing to <html> while a right-panel drag is active", () => {
    renderHookHarness();
    expect(latest).not.toBeNull();

    // The right-panel drag is a no-op while the panel is closed, so open it
    // first via the hook's own setter.
    act(() => {
      latest!.setRightPanelOpenWithMotion(true);
    });

    act(() => {
      latest!.startRightPanelResize(makePointerDownEvent(400));
    });
    expect(document.documentElement.classList.contains(WINDOW_RESIZING_CLASS)).toBe(true);

    act(() => {
      window.dispatchEvent(new Event("pointerup", { bubbles: true }));
    });
    expect(document.documentElement.classList.contains(WINDOW_RESIZING_CLASS)).toBe(false);
  });

  it("does not add the class for non-primary-button pointerdowns on the sidebar", () => {
    renderHookHarness();
    expect(latest).not.toBeNull();

    act(() => {
      latest!.startSidebarResize({
        button: 2,
        clientX: 100,
        preventDefault: vi.fn()
      } as unknown as React.PointerEvent<HTMLDivElement>);
    });
    expect(document.documentElement.classList.contains(WINDOW_RESIZING_CLASS)).toBe(false);
  });
});