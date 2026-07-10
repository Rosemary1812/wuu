import { act, createElement, useRef } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  SIDEBAR_DRAWER_HOVER_OPEN_DELAY_MS,
  useSidebarDrawerState,
  type SidebarDrawerStateController,
} from "./SidebarDrawerState";

let root: Root | undefined;
let container: HTMLDivElement;
let elementFromPointTarget: Element | null = null;

beforeEach(() => {
  vi.useFakeTimers();
  container = document.createElement("div");
  document.body.appendChild(container);
  Object.defineProperty(document, "elementFromPoint", {
    configurable: true,
    value: vi.fn(() => elementFromPointTarget),
  });
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  root = undefined;
  container.remove();
  elementFromPointTarget = null;
  vi.useRealTimers();
  vi.restoreAllMocks();
});

async function renderSidebarDrawerState(): Promise<{
  get: () => SidebarDrawerStateController;
  sidebar: HTMLElement;
  hoverZone: HTMLElement;
}> {
  let latest: SidebarDrawerStateController | undefined;
  let sidebar: HTMLElement | null = null;
  let hoverZone: HTMLElement | null = null;

  function Probe() {
    const appShellRef = useRef<HTMLDivElement>(null);
    const drawer = useSidebarDrawerState({
      appShellRef,
      sidebarCollapsed: true,
      resizingSidebar: false,
      activeSessionTabID: "session-a",
      motionMs: 120,
    });
    latest = drawer;
    return (
      <div ref={appShellRef}>
        <aside
          ref={(node) => {
            sidebar = node;
          }}
          className="sidebar"
        />
        <div
          ref={(node) => {
            hoverZone = node;
            drawer.sidebarHoverZoneRef.current = node;
          }}
          className="sidebar-hover-zone"
        />
      </div>
    );
  }

  await act(async () => {
    root = createRoot(container);
    root.render(createElement(Probe));
    await Promise.resolve();
  });

  if (!latest || !sidebar || !hoverZone) {
    throw new Error("sidebar drawer state was not rendered");
  }

  return {
    get: () => {
      if (!latest) {
        throw new Error("sidebar drawer state was not rendered");
      }
      return latest;
    },
    sidebar,
    hoverZone,
  };
}

describe("useSidebarDrawerState", () => {
  it("opens immediately for an explicit focus-mode navigation request", async () => {
    const hook = await renderSidebarDrawerState();
    const openSidebarDrawerNow = (
      hook.get() as SidebarDrawerStateController & {
        openSidebarDrawerNow?: () => void;
      }
    ).openSidebarDrawerNow;

    await act(async () => {
      openSidebarDrawerNow?.();
    });

    expect(hook.get().sidebarDrawerPhase).toBe("open");
  });

  it("opens after the edge hover intent delay while the pointer remains hovered", async () => {
    const hook = await renderSidebarDrawerState();
    elementFromPointTarget = hook.hoverZone;

    await act(async () => {
      window.dispatchEvent(
        new MouseEvent("pointermove", {
          bubbles: true,
          clientX: 8,
          clientY: 24,
        }),
      );
      hook.get().scheduleSidebarDrawerOpen();
      vi.advanceTimersByTime(SIDEBAR_DRAWER_HOVER_OPEN_DELAY_MS);
    });

    expect(hook.get().sidebarDrawerPhase).toBe("open");
  });

  it("finishes the close animation after the configured motion duration", async () => {
    const hook = await renderSidebarDrawerState();
    elementFromPointTarget = hook.sidebar;

    await act(async () => {
      window.dispatchEvent(
        new MouseEvent("pointermove", {
          bubbles: true,
          clientX: 8,
          clientY: 24,
        }),
      );
      hook.get().openSidebarDrawer();
    });
    expect(hook.get().sidebarDrawerPhase).toBe("open");

    await act(async () => {
      hook.get().closeSidebarDrawer();
    });
    expect(hook.get().sidebarDrawerPhase).toBe("closing");

    await act(async () => {
      vi.advanceTimersByTime(120);
    });
    expect(hook.get().sidebarDrawerPhase).toBe("closed");
  });
});
