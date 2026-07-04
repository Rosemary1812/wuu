/**
 * Collapsed-sidebar hover drawer close-on-window-exit regression test.
 *
 * User report: with the sidebar collapsed, hovering the left edge opens the
 * drawer overlay — but moving the mouse straight out of the app window (or
 * switching focus to another app) left the drawer stranded open, because the
 * sidebar's pointerleave never fired. The drawer must close when the pointer
 * leaves the window or the app loses focus.
 */
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  InitializeResult,
  ServerEvent,
  WuuDesktopApi,
} from "../shared/protocol";

vi.mock("@xterm/xterm", () => ({
  Terminal: vi.fn().mockImplementation(() => ({
    loadAddon: vi.fn(),
    open: vi.fn(),
    write: vi.fn(),
    dispose: vi.fn(),
    onData: vi.fn(() => ({ dispose: vi.fn() })),
    onResize: vi.fn(() => ({ dispose: vi.fn() })),
  })),
}));

vi.mock("@xterm/addon-fit", () => ({
  FitAddon: vi.fn().mockImplementation(() => ({ fit: vi.fn() })),
}));

import { App } from "./App";

let container: HTMLDivElement;
let root: Root | null = null;
let serverEventHandlers: Array<(event: ServerEvent) => void> = [];

const workspace = "/tmp/wuu-drawer-test";

function initialized(): InitializeResult {
  return {
    protocol_version: "wuu-app-server/v0.1",
    provider: "fake",
    model: "fake-model",
    workspace_root: workspace,
    permissions: { mode: "standard" },
    providers: [
      { name: "fake", type: "openai-compatible", model: "fake-model" },
    ],
    advanced_settings: {
      max_steps: 64,
      max_context_tokens: 0,
      temperature: 0,
      disable_auto_compact: false,
    },
  };
}

function installWindowStubs(): void {
  class MockResizeObserver {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  }
  (globalThis as { ResizeObserver?: typeof ResizeObserver }).ResizeObserver =
    MockResizeObserver as typeof ResizeObserver;
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  });
}

function installWuuApi(): void {
  const projectState = (): {
    projects: never[];
    active_context: { kind: "no_project"; cwd: string };
  } => ({
    projects: [],
    active_context: { kind: "no_project", cwd: workspace },
  });
  const api = {
    listProjects: vi
      .fn()
      .mockImplementation(() => Promise.resolve(projectState())),
    selectNoProject: vi
      .fn()
      .mockImplementation(() => Promise.resolve(projectState())),
    initialize: vi.fn().mockResolvedValue(initialized()),
    listThreads: vi.fn().mockResolvedValue({ threads: [] }),
    resumeThread: vi.fn().mockResolvedValue({ thread: undefined }),
    getActiveGoalSummary: vi.fn().mockResolvedValue(null),
    gitStatus: vi.fn().mockResolvedValue({
      is_repo: false,
      dirty_count: 0,
      files: [],
    }),
    onServerEvent: vi.fn((handler: (event: ServerEvent) => void) => {
      serverEventHandlers.push(handler);
      return () => {
        serverEventHandlers = serverEventHandlers.filter(
          (item) => item !== handler,
        );
      };
    }),
    onWindowResizeState: vi.fn(() => () => {}),
    onTerminalEvent: vi.fn(() => () => {}),
    respondToServerRequest: vi.fn().mockResolvedValue(undefined),
    rejectServerRequest: vi.fn().mockResolvedValue(undefined),
  } as unknown as WuuDesktopApi;
  Object.defineProperty(window, "wuu", {
    configurable: true,
    value: api,
  });
}

async function flushAsync(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

function appShell(): HTMLElement | null {
  return container.querySelector<HTMLElement>(".app-shell");
}

async function renderCollapsedApp(): Promise<void> {
  window.localStorage.setItem("wuu.desktop.sidebarCollapsed", "true");
  await act(async () => {
    root = createRoot(container);
    root.render(<App />);
  });
  await flushAsync();
  expect(appShell()?.classList.contains("sidebar-collapsed")).toBe(true);
}

async function openDrawerViaHoverZone(): Promise<void> {
  const zone = container.querySelector<HTMLElement>(".sidebar-hover-zone");
  expect(zone).not.toBeNull();
  await act(async () => {
    // React synthesizes onPointerEnter from a delegated pointerover whose
    // relatedTarget lies outside the element.
    zone?.dispatchEvent(
      new MouseEvent("pointerover", { bubbles: true, relatedTarget: null }),
    );
  });
  expect(appShell()?.classList.contains("sidebar-drawer-open")).toBe(true);
}

describe("collapsed sidebar hover drawer", () => {
  beforeEach(() => {
    installWindowStubs();
    serverEventHandlers = [];
    container = document.createElement("div");
    document.body.appendChild(container);
    window.localStorage.clear();
    installWuuApi();
  });

  afterEach(() => {
    act(() => {
      root?.unmount();
    });
    root = null;
    container.remove();
    Reflect.deleteProperty(globalThis, "ResizeObserver");
    delete (globalThis as { wuu?: WuuDesktopApi }).wuu;
  });

  it("closes the drawer when the pointer leaves the window", async () => {
    await renderCollapsedApp();
    await openDrawerViaHoverZone();

    // Leaving the window entirely: mouseout with no relatedTarget.
    await act(async () => {
      window.dispatchEvent(
        new MouseEvent("mouseout", { relatedTarget: null }),
      );
    });

    expect(appShell()?.classList.contains("sidebar-drawer-open")).toBe(false);
    expect(appShell()?.classList.contains("sidebar-drawer-closing")).toBe(
      true,
    );
  });

  it("keeps the drawer open when mouseout stays inside the window", async () => {
    await renderCollapsedApp();
    await openDrawerViaHoverZone();

    // Moving between elements inside the window: relatedTarget is set.
    await act(async () => {
      window.dispatchEvent(
        new MouseEvent("mouseout", { relatedTarget: document.body }),
      );
    });

    expect(appShell()?.classList.contains("sidebar-drawer-open")).toBe(true);
  });

  it("closes the drawer when the window loses focus", async () => {
    await renderCollapsedApp();
    await openDrawerViaHoverZone();

    await act(async () => {
      window.dispatchEvent(new Event("blur"));
    });

    expect(appShell()?.classList.contains("sidebar-drawer-open")).toBe(false);
    expect(appShell()?.classList.contains("sidebar-drawer-closing")).toBe(
      true,
    );
  });
});
