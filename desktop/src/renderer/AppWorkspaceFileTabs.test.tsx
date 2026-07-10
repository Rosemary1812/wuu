import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  InitializeResult,
  ServerEvent,
  Thread,
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
  WorkspaceMonacoEditor: ({ path }: { path: string }): JSX.Element => (
    <div className="workspace-monaco-editor" data-path={path} />
  ),
}));

import { App } from "./App";

let container: HTMLDivElement;
let root: Root | null = null;
let serverEventHandlers: Array<(event: ServerEvent) => void> = [];

const workspace = "/tmp/wuu-artifact-tab-test";

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

function completedThread(): Thread {
  return {
    id: "thread-artifact-tabs",
    preview: "artifact conversation",
    model_provider: "fake",
    model: "fake-model",
    cwd: workspace,
    status: "idle",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    turns: [
      {
        id: "turn-1",
        items_view: "full",
        status: "completed",
        items: [
          {
            id: "item-user",
            type: "user_message",
            role: "user",
            status: "completed",
            text: "Show me the document.",
          },
          {
            id: "item-agent",
            type: "agent_message",
            role: "assistant",
            phase: "final_answer",
            status: "completed",
            text: "Open [README.md](README.md) beside this conversation.",
          },
        ],
      },
    ],
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
  const thread = completedThread();
  const api = {
    listProjects: vi.fn().mockResolvedValue({
      projects: [],
      active_context: { kind: "no_project", cwd: workspace },
    }),
    selectNoProject: vi.fn().mockResolvedValue({
      projects: [],
      active_context: { kind: "no_project", cwd: workspace },
    }),
    initialize: vi.fn().mockResolvedValue(initialized()),
    listThreads: vi.fn().mockResolvedValue({ threads: [thread] }),
    resumeThread: vi.fn().mockResolvedValue({ thread }),
    getActiveGoalSummary: vi.fn().mockResolvedValue(null),
    gitStatus: vi.fn().mockResolvedValue({
      is_repo: false,
      dirty_count: 0,
      files: [],
    }),
    listWorkspaceDirectory: vi.fn().mockResolvedValue({
      root: workspace,
      path: "",
      entries: [{ kind: "file", name: "README.md", path: "README.md" }],
      truncated: false,
    }),
    readWorkspaceFile: vi.fn().mockResolvedValue({
      root: workspace,
      path: "README.md",
      absolute_path: `${workspace}/README.md`,
      size_bytes: 16,
      mtime_ms: 1000,
      sha256: "a".repeat(64),
      binary: false,
      truncated: false,
      text: "# Artifact\n",
    }),
    writeWorkspaceFile: vi.fn(),
    revealWorkspaceItem: vi.fn().mockResolvedValue(undefined),
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

describe("workspace file tabs", () => {
  beforeEach(() => {
    installWindowStubs();
    installWuuApi();
    Element.prototype.scrollIntoView = vi.fn();
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

  it("opens a document beside the active conversation instead of replacing it", async () => {
    await act(async () => {
      root = createRoot(container);
      root.render(<App />);
    });
    await flushAsync();

    const fileLink = container.querySelector<HTMLButtonElement>(".rich-file-link");
    expect(fileLink).not.toBeNull();
    expect(container.querySelectorAll(".session-tab")).toHaveLength(1);
    expect(container.querySelector(".session-tab.active")?.textContent).toContain(
      "artifact conversation",
    );

    await act(async () => {
      fileLink?.click();
    });
    await flushAsync();

    expect(container.querySelectorAll(".session-tab")).toHaveLength(1);
    expect(container.querySelector(".session-tab.active")?.textContent).toContain(
      "artifact conversation",
    );
    expect(container.querySelector(".rich-file-link")).not.toBeNull();
    expect(
      container.querySelector(".workspace-right-panel .workspace-tool-tab.active")?.textContent,
    ).toContain("README.md");
    const rightFilePreview = container.querySelector(
      ".workspace-right-panel .workspace-file-resource.active .workspace-file-preview",
    );
    expect(rightFilePreview).not.toBeNull();
    expect(rightFilePreview?.textContent).toContain("Artifact");

    fileLink?.focus();
    const expand = container.querySelector<HTMLButtonElement>('[aria-label="展开为全面板"]');
    await act(async () => expand?.click());
    await flushAsync();

    expect(container.querySelector(".conversation-pane")?.hasAttribute("inert")).toBe(true);
    expect(container.querySelector(".sidebar")?.hasAttribute("inert")).toBe(true);
    expect(container.querySelector(".workspace-right-panel")?.hasAttribute("inert")).toBe(false);
    expect(document.activeElement).toBe(
      container.querySelector(".workspace-tool-tab.active .workspace-tool-tab-main"),
    );

    const exit = container.querySelector<HTMLButtonElement>('[aria-label="退出全面板"]');
    await act(async () => exit?.click());
    await flushAsync();
    expect(container.querySelector(".conversation-pane")?.hasAttribute("inert")).toBe(false);
    expect(container.querySelector(".sidebar")?.hasAttribute("inert")).toBe(false);
    expect(document.activeElement).toBe(fileLink);
  });
});
