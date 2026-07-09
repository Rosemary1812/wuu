/**
 * Sidebar collapse-state independence regression test.
 *
 * User report: collapsing 置顶, then interacting with a project, made
 * 置顶 passively expand. Root cause: the App effect that prunes
 * collapsedSidebarSectionIDs against state.projects stripped every
 * pseudo-section key (置顶 / Agents / 群聊 / 对话) whenever the project
 * list got a fresh array identity (any runtime reload), silently
 * re-expanding those sections. A second bug in the same model:
 * toggleSidebarSectionCollapsed had no pseudo-section branch for 群聊, so its
 * header click ran the project expand/collapse state machine whose
 * semantics don't match the group section's collapsed-set-only render
 * (the first click was a visual no-op).
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

vi.mock("./WorkspaceMonacoEditor", () => ({
  WorkspaceMonacoEditor: (): JSX.Element => (
    <div className="workspace-monaco-editor" data-testid="mock-monaco-editor" />
  ),
}));

import { App } from "./App";

let container: HTMLDivElement;
let root: Root | null = null;
let serverEventHandlers: Array<(event: ServerEvent) => void> = [];

const workspace = "/tmp/wuu-collapse-test";

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
  // Fresh arrays on every call — mirrors production, where each
  // project-state reload replaces state.projects with a new identity.
  const projectState = (): {
    projects: never[];
    active_context: { kind: "no_project"; cwd: string };
  } => ({
    projects: [],
    active_context: { kind: "no_project", cwd: workspace },
  });
  const api = {
    listProjects: vi.fn().mockImplementation(() => Promise.resolve(projectState())),
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

function sectionHeader(label: string): HTMLButtonElement | null {
  return container.querySelector<HTMLButtonElement>(
    `section[aria-label="${label}"] .sidebar-section-row`,
  );
}

function conversationSectionHeader(): HTMLButtonElement | null {
  return container.querySelector<HTMLButtonElement>(
    'button[aria-label="收起 对话 的会话"], button[aria-label="展开 对话 的会话"]',
  );
}

async function clickHeader(label: string): Promise<void> {
  const header = sectionHeader(label);
  expect(header).not.toBeNull();
  await act(async () => {
    header?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  });
  await flushAsync();
}

async function clickConversationHeader(): Promise<void> {
  const header = conversationSectionHeader();
  expect(header).not.toBeNull();
  await act(async () => {
    header?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  });
  await flushAsync();
}

describe("sidebar collapse-state independence", () => {
  beforeEach(() => {
    installWindowStubs();
    serverEventHandlers = [];
    container = document.createElement("div");
    document.body.appendChild(container);
    window.localStorage.clear();
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

  it("keeps 置顶 and 群聊 collapsed across a project-state reload", async () => {
    installWuuApi();
    await act(async () => {
      root = createRoot(container);
      root.render(<App />);
    });
    await flushAsync();

    expect(sectionHeader("置顶")?.getAttribute("aria-expanded")).toBe("true");
    expect(sectionHeader("群聊")?.getAttribute("aria-expanded")).toBe("true");

    // Collapse both pseudo sections.
    await clickHeader("置顶");
    expect(sectionHeader("置顶")?.getAttribute("aria-expanded")).toBe("false");
    await clickHeader("群聊");
    expect(sectionHeader("群聊")?.getAttribute("aria-expanded")).toBe("false");

    // Trigger a project-state reload (fresh state.projects identity):
    // the 对话 header's new-session button routes through
    // selectNoProject, exactly like the reported repro path.
    const newSession = container.querySelector<HTMLButtonElement>(
      'button[aria-label="在 对话 中新建会话"]',
    );
    expect(newSession).not.toBeNull();
    await act(async () => {
      newSession?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    await flushAsync();

    // Collapse state of unrelated sections must be untouched.
    expect(sectionHeader("置顶")?.getAttribute("aria-expanded")).toBe("false");
    expect(sectionHeader("群聊")?.getAttribute("aria-expanded")).toBe("false");

    // And both toggle back open independently.
    await clickHeader("群聊");
    expect(sectionHeader("群聊")?.getAttribute("aria-expanded")).toBe("true");
    expect(sectionHeader("置顶")?.getAttribute("aria-expanded")).toBe("false");
  });

  it("collapses the active 对话 section on the first header click", async () => {
    installWuuApi();
    await act(async () => {
      root = createRoot(container);
      root.render(<App />);
    });
    await flushAsync();

    expect(conversationSectionHeader()?.getAttribute("aria-expanded")).toBe(
      "true",
    );

    await clickConversationHeader();
    expect(conversationSectionHeader()?.getAttribute("aria-expanded")).toBe(
      "false",
    );

    await clickConversationHeader();
    expect(conversationSectionHeader()?.getAttribute("aria-expanded")).toBe(
      "true",
    );
  });
});
