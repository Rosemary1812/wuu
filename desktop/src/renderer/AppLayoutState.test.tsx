import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  RIGHT_PANEL_MOTION_MS,
  SIDEBAR_DEFAULT_WIDTH,
  SIDEBAR_MIN_WIDTH,
  SIDEBAR_MOTION_MS,
  WORKSPACE_RIGHT_PANEL_DEFAULT_WIDTH,
  useAppLayoutState
} from "./AppLayoutState";
import { WINDOW_RESIZING_CLASS } from "./WindowResizeState";

interface Harness {
  sidebarWidth: ReturnType<typeof useAppLayoutState>["sidebarWidth"];
  sidebarCollapsed: ReturnType<typeof useAppLayoutState>["sidebarCollapsed"];
  workspaceRightPanelWidth: ReturnType<
    typeof useAppLayoutState
  >["workspaceRightPanelWidth"];
  rightPanelAnimating: ReturnType<typeof useAppLayoutState>["rightPanelAnimating"];
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
      sidebarWidth: hook.sidebarWidth,
      sidebarCollapsed: hook.sidebarCollapsed,
      workspaceRightPanelWidth: hook.workspaceRightPanelWidth,
      rightPanelAnimating: hook.rightPanelAnimating,
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
  vi.useRealTimers();
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

  it("clears right-panel animation after the motion window", () => {
    vi.useFakeTimers();
    renderHookHarness();
    expect(latest).not.toBeNull();
    expect(latest!.rightPanelAnimating).toBe(false);

    act(() => {
      latest!.setRightPanelOpenWithMotion(true);
    });
    expect(latest!.rightPanelAnimating).toBe(true);

    act(() => {
      vi.advanceTimersByTime(RIGHT_PANEL_MOTION_MS);
    });
    expect(latest!.rightPanelAnimating).toBe(false);
  });

  it("paces sidebar layout motion as a drawer transition", () => {
    expect(SIDEBAR_MOTION_MS).toBeGreaterThanOrEqual(320);
    expect(SIDEBAR_MOTION_MS).toBeLessThanOrEqual(400);
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

describe("useAppLayoutState initial widths", () => {
  // localStorage.getItem returns null for a missing key, and Number(null) is
  // 0 — a naive Number() conversion clamps a fresh profile to the minimum
  // width, parking the sidebar exactly on the collapse threshold.
  it("falls back to the defaults when nothing is stored", () => {
    renderHookHarness();
    expect(latest!.sidebarWidth).toBe(SIDEBAR_DEFAULT_WIDTH);
    expect(latest!.workspaceRightPanelWidth).toBe(WORKSPACE_RIGHT_PANEL_DEFAULT_WIDTH);
  });

  it("falls back to the default when the stored width is not numeric", () => {
    window.localStorage.setItem("wuu.desktop.sidebarWidth", "garbage");
    renderHookHarness();
    expect(latest!.sidebarWidth).toBe(SIDEBAR_DEFAULT_WIDTH);
  });

  it("keeps a stored in-range width", () => {
    window.localStorage.setItem("wuu.desktop.sidebarWidth", "420");
    renderHookHarness();
    expect(latest!.sidebarWidth).toBe(420);
  });

  it("holds at the minimum width before the collapse intent threshold", () => {
    window.localStorage.setItem("wuu.desktop.sidebarWidth", "220");
    renderHookHarness();
    expect(latest!.sidebarWidth).toBe(220);
    expect(latest!.sidebarCollapsed).toBe(false);

    act(() => {
      latest!.startSidebarResize(makePointerDownEvent(220));
    });
    act(() => {
      window.dispatchEvent(
        Object.assign(new Event("pointermove"), {
          clientX: SIDEBAR_MIN_WIDTH - 12,
        })
      );
    });
    expect(latest!.sidebarCollapsed).toBe(false);

    act(() => {
      window.dispatchEvent(new Event("pointerup", { bubbles: true }));
    });
    expect(latest!.sidebarCollapsed).toBe(false);
    expect(latest!.sidebarWidth).toBe(SIDEBAR_MIN_WIDTH);
  });

  it("collapses mid-drag once the pointer crosses the collapse intent threshold", () => {
    window.localStorage.setItem("wuu.desktop.sidebarWidth", "220");
    renderHookHarness();
    expect(latest!.sidebarWidth).toBe(220);
    expect(latest!.sidebarCollapsed).toBe(false);

    act(() => {
      latest!.startSidebarResize(makePointerDownEvent(220));
    });
    act(() => {
      window.dispatchEvent(
        Object.assign(new Event("pointermove"), {
          clientX: SIDEBAR_MIN_WIDTH - 40,
        })
      );
    });
    expect(latest!.sidebarCollapsed).toBe(true);

    act(() => {
      window.dispatchEvent(new Event("pointerup", { bubbles: true }));
    });
    expect(latest!.sidebarCollapsed).toBe(true);
    expect(latest!.sidebarWidth).toBe(220);
  });
});
