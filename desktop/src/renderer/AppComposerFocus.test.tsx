import { act, type ComponentProps } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  DesktopProject,
  InitializeResult,
  RuntimeContext,
  ServerEvent,
  Thread,
  WuuDesktopApi,
} from "../shared/protocol";

vi.mock("./ComposerView", async (importOriginal) => {
  const original = await importOriginal<typeof import("./ComposerView")>();
  type ComposerProps = ComponentProps<typeof original.Composer>;
  return {
    ...original,
    Composer: (props: ComposerProps): JSX.Element => {
      const variant = props.variant ?? "dock";
      const label = props.mainConversation
        ? `main composer ${variant}`
        : "side composer";
      return (
        <div
          data-main-conversation-composer={
            props.mainConversation ? variant : undefined
          }
        >
          <textarea
            aria-label={label}
            value={props.prompt}
            onChange={(event) => props.setPrompt(event.currentTarget.value)}
            onKeyDown={(event) => {
              if (event.key !== "Enter") return;
              event.preventDefault();
              if (props.prompt.trim() === "/new") {
                props.onStartNewThread();
              } else if (props.prompt.trim() === "/side") {
                props.onOpenSideThread?.();
              } else {
                props.onSend();
              }
            }}
          />
        </div>
      );
    },
  };
});

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
  WorkspaceMonacoEditor: (): JSX.Element => <div />,
}));

import { App } from "./App";

const scratchCwd = "/tmp/wuu-composer-focus/scratch";
const projectCwd = "/tmp/wuu-composer-focus/project";
const project: DesktopProject = {
  id: "project-focus",
  name: "Focus Project",
  path: projectCwd,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

let container: HTMLDivElement;
let root: Root | null = null;
let serverEventHandlers: Array<(event: ServerEvent) => void> = [];
let releaseProjectSelection: (() => void) | null = null;
let releaseThreadStart: (() => void) | null = null;

function initialized(cwd: string): InitializeResult {
  return {
    protocol_version: "wuu-app-server/v0.1",
    provider: "fake",
    model: "fake-model",
    workspace_root: cwd,
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

function persistedThread(): Thread {
  return {
    id: "thread-focus",
    preview: "focus continuity",
    model_provider: "fake",
    model: "fake-model",
    cwd: scratchCwd,
    workspace_kind: "scratch",
    status: "idle",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-02T00:00:00Z",
    turns: [
      {
        id: "turn-existing",
        status: "completed",
        items_view: "full",
        items: [
          {
            id: "item-existing",
            type: "user_message",
            status: "completed",
            text: "existing query",
          },
        ],
      },
    ],
  };
}

function newThread(): Thread {
  return {
    id: "thread-new",
    preview: "new query",
    model_provider: "fake",
    model: "fake-model",
    cwd: scratchCwd,
    workspace_kind: "scratch",
    status: "idle",
    created_at: "2026-01-03T00:00:00Z",
    updated_at: "2026-01-03T00:00:00Z",
    turns: [],
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
  Object.defineProperty(window, "requestAnimationFrame", {
    configurable: true,
    value: (callback: FrameRequestCallback) => {
      window.setTimeout(() => callback(0), 0);
      return 1;
    },
  });
}

function installWuuApi(
  options: {
    withThread?: boolean;
    deferProjectSelection?: boolean;
    rejectProjectSelection?: boolean;
    rejectNoProjectSelection?: boolean;
    deferThreadStart?: boolean;
    rejectThreadStart?: boolean;
    rejectTurnStart?: boolean;
  } = {},
): void {
  let activeContext: RuntimeContext = { kind: "no_project", cwd: scratchCwd };
  const thread = persistedThread();
  const api = {
    listProjects: vi.fn().mockImplementation(() =>
      Promise.resolve({ projects: [project], active_context: activeContext }),
    ),
    selectProject: vi.fn().mockImplementation(async () => {
      if (options.deferProjectSelection) {
        await new Promise<void>((resolve) => {
          releaseProjectSelection = resolve;
        });
      }
      if (options.rejectProjectSelection) {
        throw new Error("project selection failed");
      }
      activeContext = {
        kind: "project",
        project_id: project.id,
        cwd: projectCwd,
      };
      return { projects: [project], active_context: activeContext };
    }),
    selectNoProject: vi.fn().mockImplementation(() => {
      if (options.rejectNoProjectSelection) {
        return Promise.reject(new Error("no-project selection failed"));
      }
      activeContext = { kind: "no_project", cwd: scratchCwd };
      return Promise.resolve({ projects: [project], active_context: activeContext });
    }),
    initialize: vi.fn().mockImplementation(() =>
      Promise.resolve(initialized(activeContext.cwd)),
    ),
    listThreads: vi.fn().mockImplementation(() =>
      Promise.resolve({
        threads:
          options.withThread && activeContext.kind === "no_project" ? [thread] : [],
      }),
    ),
    listArchivedThreads: vi.fn().mockResolvedValue({ threads: [] }),
    resumeThread: vi.fn().mockResolvedValue({ thread }),
    startThread: vi.fn().mockImplementation(async () => {
      if (options.deferThreadStart) {
        await new Promise<void>((resolve) => {
          releaseThreadStart = resolve;
        });
      }
      if (options.rejectThreadStart) {
        throw new Error("thread start failed");
      }
      return { thread: newThread() };
    }),
    startTurn: options.rejectTurnStart
      ? vi.fn().mockRejectedValue(new Error("turn start failed"))
      : vi.fn().mockResolvedValue({
          turn: {
            id: "turn-started",
            status: "in_progress",
            items_view: "full",
            items: [],
          },
        }),
    getActiveGoalSummary: vi.fn().mockResolvedValue(null),
    gitStatus: vi.fn().mockResolvedValue({
      is_repo: false,
      dirty_count: 0,
      files: [],
    }),
    openSideThread: vi.fn().mockResolvedValue({ summary: null }),
    getSideThreadHistory: vi.fn().mockResolvedValue(null),
    sendSideThreadMessage: vi.fn().mockResolvedValue({
      user_message_id: "side-user",
      summary: null,
    }),
    interruptSideThread: vi.fn().mockResolvedValue({ ok: true }),
    resetSideThread: vi.fn().mockResolvedValue({ ok: true }),
    onSideThreadEvent: vi.fn(() => () => {}),
    onServerEvent: vi.fn((handler: (event: ServerEvent) => void) => {
      serverEventHandlers.push(handler);
      return () => {
        serverEventHandlers = serverEventHandlers.filter(
          (candidate) => candidate !== handler,
        );
      };
    }),
    onWindowResizeState: vi.fn(() => () => {}),
    onTerminalEvent: vi.fn(() => () => {}),
    respondToServerRequest: vi.fn().mockResolvedValue(undefined),
    rejectServerRequest: vi.fn().mockResolvedValue(undefined),
  } as unknown as WuuDesktopApi;
  Object.defineProperty(window, "wuu", { configurable: true, value: api });
}

async function flushAsync(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await new Promise((resolve) => window.setTimeout(resolve, 0));
    await Promise.resolve();
  });
}

async function renderApp(
  withThread: boolean,
  options: {
    deferProjectSelection?: boolean;
    rejectProjectSelection?: boolean;
    rejectNoProjectSelection?: boolean;
    deferThreadStart?: boolean;
    rejectThreadStart?: boolean;
    rejectTurnStart?: boolean;
  } = {},
): Promise<void> {
  installWuuApi({ withThread, ...options });
  await act(async () => {
    root = createRoot(container);
    root.render(<App />);
  });
  await flushAsync();
}

function mainComposer(variant: "hero" | "dock"): HTMLTextAreaElement {
  const textarea = container.querySelector<HTMLTextAreaElement>(
    `textarea[aria-label="main composer ${variant}"]`,
  );
  if (!textarea) throw new Error(`${variant} composer not rendered`);
  return textarea;
}

async function waitForMainComposerFocus(
  variant: "hero" | "dock",
): Promise<void> {
  await act(async () => {
    await vi.waitFor(() => {
      expect(document.activeElement).toBe(mainComposer(variant));
    });
  });
}

async function waitForSideComposerFocus(): Promise<void> {
  await act(async () => {
    await vi.waitFor(() => {
      const side = container.querySelector<HTMLTextAreaElement>(
        'textarea[aria-label="side composer"]',
      );
      expect(side).not.toBeNull();
      expect(document.activeElement).toBe(side);
    });
  });
}

async function enterCommand(
  textarea: HTMLTextAreaElement,
  command: string,
): Promise<void> {
  await act(async () => {
    const setter = Object.getOwnPropertyDescriptor(
      HTMLTextAreaElement.prototype,
      "value",
    )?.set;
    setter?.call(textarea, command);
    textarea.dispatchEvent(new Event("input", { bubbles: true }));
  });
  await act(async () => {
    textarea.dispatchEvent(
      new KeyboardEvent("keydown", { key: "Enter", bubbles: true }),
    );
  });
  await flushAsync();
}

describe("main composer focus continuity", () => {
  beforeEach(() => {
    installWindowStubs();
    serverEventHandlers = [];
    releaseProjectSelection = null;
    releaseThreadStart = null;
    container = document.createElement("div");
    document.body.appendChild(container);
    window.localStorage.clear();
  });

  afterEach(() => {
    act(() => root?.unmount());
    root = null;
    container.remove();
    Reflect.deleteProperty(globalThis, "ResizeObserver");
    delete (globalThis as { wuu?: WuuDesktopApi }).wuu;
  });

  it("focuses the hero composer after /new", async () => {
    await renderApp(true);
    const dock = mainComposer("dock");
    dock.focus();

    await enterCommand(dock, "/new");

    await waitForMainComposerFocus("hero");
  });

  it("focuses the hero composer from the new-tab button", async () => {
    await renderApp(true);
    const button = container.querySelector<HTMLButtonElement>(
      'button.session-tab-new[aria-label="新建对话"]',
    );
    if (!button) throw new Error("new-tab button not rendered");
    button.focus();

    await act(async () => button.click());
    await flushAsync();

    await waitForMainComposerFocus("hero");
  });

  it("focuses the hero composer from a project's new-conversation button", async () => {
    await renderApp(true);
    const button = container.querySelector<HTMLButtonElement>(
      'button[aria-label="在 Focus Project 中新建会话"]',
    );
    if (!button) throw new Error("project new-conversation button not rendered");
    button.focus();

    await act(async () => button.click());
    await flushAsync();

    await waitForMainComposerFocus("hero");
  });

  it("waits for the destination hero before focusing across projects", async () => {
    await renderApp(false, { deferProjectSelection: true });
    // Resolve animation frames synchronously to model the race where the
    // project action settles before React commits the destination state.
    Object.defineProperty(window, "requestAnimationFrame", {
      configurable: true,
      value: (callback: FrameRequestCallback) => {
        callback(0);
        return 1;
      },
    });
    const button = container.querySelector<HTMLButtonElement>(
      'button[aria-label="在 Focus Project 中新建会话"]',
    );
    if (!button) throw new Error("project new-conversation button not rendered");
    button.focus();

    await act(async () => button.click());
    expect(document.activeElement).toBe(button);

    if (!releaseProjectSelection) {
      throw new Error("project selection was not deferred");
    }
    releaseProjectSelection();
    await flushAsync();

    await waitForMainComposerFocus("hero");
  });

  it("does not focus the old hero when project selection fails", async () => {
    await renderApp(false, { rejectProjectSelection: true });
    const button = container.querySelector<HTMLButtonElement>(
      'button[aria-label="在 Focus Project 中新建会话"]',
    );
    if (!button) throw new Error("project new-conversation button not rendered");
    button.focus();

    await act(async () => button.click());
    await flushAsync();

    expect(document.activeElement).toBe(button);
  });

  it("does not focus the old hero when no-project selection fails", async () => {
    await renderApp(false, { rejectNoProjectSelection: true });
    const button = container.querySelector<HTMLButtonElement>(
      'button[aria-label="在 对话 中新建会话"]',
    );
    if (!button) throw new Error("scratch new-conversation button not rendered");
    button.focus();

    await act(async () => button.click());
    await flushAsync();

    expect(document.activeElement).toBe(button);
  });

  it("does not steal focus changed during an asynchronous project switch", async () => {
    await renderApp(false, { deferProjectSelection: true });
    const button = container.querySelector<HTMLButtonElement>(
      'button[aria-label="在 Focus Project 中新建会话"]',
    );
    if (!button) throw new Error("project new-conversation button not rendered");
    button.focus();
    await act(async () => button.click());

    const other = document.createElement("button");
    document.body.appendChild(other);
    other.focus();
    if (!releaseProjectSelection) {
      throw new Error("project selection was not deferred");
    }
    releaseProjectSelection();
    await flushAsync();

    expect(document.activeElement).toBe(other);
    other.remove();
  });

  it("does not steal focus after a non-focusable user interaction", async () => {
    await renderApp(false, { deferProjectSelection: true });
    const button = container.querySelector<HTMLButtonElement>(
      'button[aria-label="在 Focus Project 中新建会话"]',
    );
    if (!button) throw new Error("project new-conversation button not rendered");
    button.focus();
    await act(async () => button.click());

    const surface = document.createElement("div");
    document.body.appendChild(surface);
    surface.dispatchEvent(new Event("pointerdown", { bubbles: true }));
    button.blur();
    if (!releaseProjectSelection) {
      throw new Error("project selection was not deferred");
    }
    releaseProjectSelection();
    await flushAsync();

    expect(document.activeElement).not.toBe(mainComposer("hero"));
    surface.remove();
  });

  it("focuses the side composer after /side", async () => {
    await renderApp(true);
    const dock = mainComposer("dock");
    dock.focus();

    await enterCommand(dock, "/side");

    await waitForSideComposerFocus();
  });

  it("hands focus from the hero composer to the dock on the first query", async () => {
    await renderApp(false);
    const hero = mainComposer("hero");
    hero.focus();

    await enterCommand(hero, "first query");

    await waitForMainComposerFocus("dock");
  });

  it("shows the first query while thread creation is still pending", async () => {
    await renderApp(false, { deferThreadStart: true });
    const hero = mainComposer("hero");

    await enterCommand(hero, "first query before thread start");

    expect(container.textContent).toContain("first query before thread start");
    expect(container.querySelector(".turn-process-title")?.textContent).toBe(
      "正在处理",
    );
    expect(mainComposer("dock")).not.toBeNull();
    expect(window.wuu.startTurn).not.toHaveBeenCalled();

    if (!releaseThreadStart) {
      throw new Error("thread start was not deferred");
    }
    releaseThreadStart();
    await flushAsync();
  });

  it("removes the pending query and restores the hero draft when thread creation fails", async () => {
    await renderApp(false, { rejectThreadStart: true });
    const hero = mainComposer("hero");

    await enterCommand(hero, "query that fails before thread start");

    expect(mainComposer("hero").value).toBe("query that fails before thread start");
    expect(container.querySelector(".turn-process-title")).toBeNull();
  });

  it("restores focus to the hero when the first query fails", async () => {
    await renderApp(false, { rejectTurnStart: true });
    const hero = mainComposer("hero");
    hero.focus();

    await enterCommand(hero, "first query");

    await waitForMainComposerFocus("hero");
  });

  it("does not hand focus to the dock after the user focuses another control", async () => {
    await renderApp(false);
    const hero = mainComposer("hero");
    const sentinel = document.createElement("button");
    container.appendChild(sentinel);
    hero.focus();

    await act(async () => {
      const setter = Object.getOwnPropertyDescriptor(
        HTMLTextAreaElement.prototype,
        "value",
      )?.set;
      setter?.call(hero, "first query");
      hero.dispatchEvent(new Event("input", { bubbles: true }));
    });
    await act(async () => {
      hero.dispatchEvent(
        new KeyboardEvent("keydown", { key: "Enter", bubbles: true }),
      );
      sentinel.focus();
    });
    await flushAsync();

    expect(document.activeElement).toBe(sentinel);
  });
});
