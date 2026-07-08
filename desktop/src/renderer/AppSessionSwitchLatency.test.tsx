/**
 * Session tab switching should feel local when the target thread is already
 * loaded. The app still resumes the thread in the background so the server
 * snapshot can refresh status/turns, but the click must not wait for that IPC
 * round trip before the active tab and conversation pane change.
 */
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  InitializeResult,
  ServerEvent,
  Thread,
  WuuDesktopApi,
} from "../shared/protocol";

vi.mock("./ConversationTurnList", () => ({
  ConversationTurnList: ({
    threadID,
    turns,
  }: {
    threadID: string;
    turns: unknown[];
  }): JSX.Element => (
    <div
      data-testid="turn-list-probe"
      data-thread-id={threadID}
      data-turn-count={turns.length}
    />
  ),
}));

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

const workspace = "/tmp/wuu-session-switch-latency-test";
const threadAID = "thread-switch-a";
const threadBID = "thread-switch-b";

type Deferred<T> = {
  promise: Promise<T>;
  resolve: (value: T) => void;
};

function deferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((promiseResolve) => {
    resolve = promiseResolve;
  });
  return { promise, resolve };
}

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

function completedThread({
  id,
  preview,
  updatedAt,
  turns = 1,
}: {
  id: string;
  preview: string;
  updatedAt: string;
  turns?: number;
}): Thread {
  return {
    id,
    preview,
    model_provider: "fake",
    model: "fake-model",
    cwd: workspace,
    status: "idle",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: updatedAt,
    turns: Array.from({ length: turns }, (_, index) => ({
      id: `${id}-turn-${index + 1}`,
      items_view: "full",
      status: "completed",
      items: [
        {
          id: `${id}-user-${index + 1}`,
          type: "user_message",
          text: `${preview} question ${index + 1}`,
        },
        {
          id: `${id}-agent-${index + 1}`,
          type: "agent_message",
          text: `${preview} answer ${index + 1}`,
        },
      ],
    })),
  };
}

function threadA(turns = 1): Thread {
  return completedThread({
    id: threadAID,
    preview: "session switch A",
    updatedAt: "2026-01-02T00:00:00Z",
    turns,
  });
}

function threadB(): Thread {
  return completedThread({
    id: threadBID,
    preview: "session switch B",
    updatedAt: "2026-01-01T00:00:00Z",
  });
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

function installWuuApi(): {
  resumeThread: ReturnType<typeof vi.fn>;
  threadsByID: Map<string, Thread>;
} {
  const threadsByID = new Map<string, Thread>([
    [threadAID, threadA()],
    [threadBID, threadB()],
  ]);
  const resumeThread = vi
    .fn()
    .mockImplementation((threadID: string) =>
      Promise.resolve({ thread: threadsByID.get(threadID) }),
    );
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
    listThreads: vi.fn().mockResolvedValue({
      threads: Array.from(threadsByID.values()),
    }),
    resumeThread,
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
  return { resumeThread, threadsByID };
}

async function flushAsync(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

function threadRowButton(previewText: string): HTMLButtonElement | undefined {
  return Array.from(
    container.querySelectorAll<HTMLButtonElement>(".thread-row-main"),
  ).find((button) => button.textContent?.includes(previewText));
}

function sessionTabButton(label: string): HTMLButtonElement | undefined {
  return Array.from(
    container.querySelectorAll<HTMLButtonElement>(".session-tab-main"),
  ).find((button) => button.textContent?.includes(label));
}

function activeSessionTabLabel(): string {
  return (
    container.querySelector(".session-tab.active .session-tab-title")
      ?.textContent ?? ""
  );
}

function activeThreadProbe(): HTMLElement | null {
  return container.querySelector('[data-testid="turn-list-probe"]');
}

describe("session tab switch latency", () => {
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

  it("switches to an already loaded same-runtime thread before resume resolves", async () => {
    const { resumeThread, threadsByID } = installWuuApi();

    await act(async () => {
      root = createRoot(container);
      root.render(<App />);
    });
    await flushAsync();

    expect(resumeThread).toHaveBeenCalledWith(threadAID);
    expect(activeSessionTabLabel()).toContain("session switch A");
    expect(activeThreadProbe()?.dataset.threadId).toBe(threadAID);

    const rowB = threadRowButton("session switch B");
    expect(rowB).toBeDefined();
    await act(async () => {
      rowB?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    await flushAsync();

    expect(activeSessionTabLabel()).toContain("session switch B");
    expect(activeThreadProbe()?.dataset.threadId).toBe(threadBID);

    const delayedResumeA = deferred<{ thread: Thread }>();
    resumeThread.mockClear();
    resumeThread.mockImplementation((threadID: string) => {
      if (threadID === threadAID) {
        return delayedResumeA.promise;
      }
      return Promise.resolve({ thread: threadsByID.get(threadID) });
    });

    const tabA = sessionTabButton("session switch A");
    expect(tabA).toBeDefined();
    await act(async () => {
      tabA?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      await Promise.resolve();
    });

    expect(resumeThread).toHaveBeenCalledWith(threadAID);
    expect(activeSessionTabLabel()).toContain("session switch A");
    expect(activeThreadProbe()?.dataset.threadId).toBe(threadAID);
    expect(activeThreadProbe()?.dataset.turnCount).toBe("1");

    delayedResumeA.resolve({ thread: threadA(2) });
    await flushAsync();

    expect(activeSessionTabLabel()).toContain("session switch A");
    expect(activeThreadProbe()?.dataset.threadId).toBe(threadAID);
    expect(activeThreadProbe()?.dataset.turnCount).toBe("2");
  });
});
