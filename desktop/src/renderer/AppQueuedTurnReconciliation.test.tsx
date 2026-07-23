import { act, type ComponentProps } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  InitializeResult,
  ServerEvent,
  Thread,
  WuuDesktopApi,
} from "../shared/protocol";

vi.mock("./ComposerView", async (importOriginal) => {
  const original = await importOriginal<typeof import("./ComposerView")>();
  type ComposerProps = ComponentProps<typeof original.Composer>;
  return {
    ...original,
    Composer: (props: ComposerProps): JSX.Element => (
      <div
        data-testid="composer-probe"
        data-queued-ids={props.queuedMessages
          .map((message) => message.id)
          .join(",")}
        data-guide-ids={props.guideMessages
          .map((message) => message.id)
          .join(",")}
        data-held-order={[...props.queuedMessages, ...props.guideMessages]
          .filter((message) => message.held)
          .sort(
            (left, right) =>
              (left.heldPosition ?? Number.MAX_SAFE_INTEGER) -
              (right.heldPosition ?? Number.MAX_SAFE_INTEGER),
          )
          .map((message) => message.id)
          .join(",")}
      >
        <textarea
          aria-label="composer-probe-input"
          value={props.prompt}
          onChange={(event) => props.setPrompt(event.currentTarget.value)}
        />
        <button type="button" onClick={props.onSend}>
          send
        </button>
        {props.onSteer ? (
          <button type="button" aria-label="steer" onClick={props.onSteer}>
            steer
          </button>
        ) : null}
        {props.queuedMessages[0] ? (
          <button
            type="button"
            aria-label="edit-first-queued"
            onClick={() =>
              props.onEditQueuedMessage(props.queuedMessages[0].id)
            }
          >
            edit queued
          </button>
        ) : null}
      </div>
    ),
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

const workspace = "/tmp/wuu-queued-turn-reconciliation-test";
const threadID = "thread-queued-reconciliation";

let container: HTMLDivElement;
let root: Root | null = null;
let serverEventHandlers: Array<(event: ServerEvent) => void> = [];

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

function runningThread(materializedQueueID?: string): Thread {
  return {
    id: threadID,
    preview: "queued reconciliation",
    model_provider: "fake",
    model: "fake-model",
    cwd: workspace,
    status: "in_progress",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:01Z",
    turns: [
      {
        id: materializedQueueID ? "turn-queued" : "turn-current",
        status: "in_progress",
        items_view: "full",
        items: [
          {
            id: materializedQueueID ? "item-queued-user" : "item-current-user",
            type: "user_message",
            status: "completed",
            text: materializedQueueID ? "queued follow-up" : "current request",
            source_id: materializedQueueID,
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

function installWuuApi(options: {
  queueTurn?: WuuDesktopApi["queueTurn"];
  steerTurn?: WuuDesktopApi["steerTurn"];
} = {}): {
  queuedClientIDs: string[];
  dequeuedClientIDs: string[];
} {
  const queuedClientIDs: string[] = [];
  const dequeuedClientIDs: string[] = [];
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
    listThreads: vi.fn().mockResolvedValue({ threads: [runningThread()] }),
    listArchivedThreads: vi.fn().mockResolvedValue({ threads: [] }),
    resumeThread: vi.fn().mockResolvedValue({ thread: runningThread() }),
    queueTurn: options.queueTurn ?? vi
      .fn()
      .mockImplementation(
        (
          _threadID: string,
          _prompt: string,
          _images: unknown[],
          clientID: string,
        ) => {
          queuedClientIDs.push(clientID);
          return Promise.resolve({
            queued: { id: clientID, thread_id: threadID },
          });
        },
      ),
    steerTurn: options.steerTurn ?? vi.fn().mockResolvedValue({ turn_id: "turn-current" }),
    dequeueTurn: vi
      .fn()
      .mockImplementation((_threadID: string, clientID: string) => {
        dequeuedClientIDs.push(clientID);
        return Promise.resolve({ ok: true });
      }),
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
  return { queuedClientIDs, dequeuedClientIDs };
}

async function flushAsync(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

function composerProbe(): HTMLElement {
  const probe = container.querySelector<HTMLElement>(
    '[data-testid="composer-probe"]',
  );
  if (!probe) {
    throw new Error("composer probe not rendered");
  }
  return probe;
}

describe("queued turn reconciliation", () => {
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

  it("dequeues a message before restoring it for editing", async () => {
    const { queuedClientIDs, dequeuedClientIDs } = installWuuApi();
    await act(async () => {
      root = createRoot(container);
      root.render(<App />);
    });
    await flushAsync();

    const textarea = composerProbe().querySelector("textarea");
    const send = composerProbe().querySelector("button");
    await act(async () => {
      if (!textarea || !send) throw new Error("composer controls not rendered");
      const valueSetter = Object.getOwnPropertyDescriptor(
        HTMLTextAreaElement.prototype,
        "value",
      )?.set;
      valueSetter?.call(textarea, "queued follow-up");
      textarea.dispatchEvent(new Event("input", { bubbles: true }));
      send.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    await flushAsync();

    expect(queuedClientIDs).toHaveLength(1);
    expect(composerProbe().dataset.queuedIds).toBe(queuedClientIDs[0]);

    const edit = composerProbe().querySelector<HTMLButtonElement>(
      'button[aria-label="edit-first-queued"]',
    );
    await act(async () => {
      if (!edit) throw new Error("queued edit control not rendered");
      edit.click();
    });
    await flushAsync();

    expect(composerProbe().dataset.queuedIds).toBe("");
    expect(dequeuedClientIDs).toEqual([queuedClientIDs[0]]);
    expect(composerProbe().querySelector("textarea")?.value).toBe(
      "queued follow-up",
    );
  });

  it("removes an already materialized queue entry after a missed start notification", async () => {
    const { queuedClientIDs } = installWuuApi();
    await act(async () => {
      root = createRoot(container);
      root.render(<App />);
    });
    await flushAsync();

    const textarea = composerProbe().querySelector("textarea");
    const send = composerProbe().querySelector("button");
    await act(async () => {
      if (!textarea || !send) throw new Error("composer controls not rendered");
      const valueSetter = Object.getOwnPropertyDescriptor(
        HTMLTextAreaElement.prototype,
        "value",
      )?.set;
      valueSetter?.call(textarea, "queued follow-up");
      textarea.dispatchEvent(new Event("input", { bubbles: true }));
      send.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    await flushAsync();

    expect(queuedClientIDs).toHaveLength(1);
    const queueID = queuedClientIDs[0];
    expect(composerProbe().dataset.queuedIds).toBe(queueID);

    // Simulate the live turn/started notification being missed while the
    // thread is backgrounded. A later authoritative thread snapshot already
    // contains the queued user_message with the same source_id.
    await act(async () => {
      for (const handler of serverEventHandlers) {
        handler({
          kind: "notification",
          workdir: workspace,
          message: {
            method: "thread/updated",
            params: { thread: runningThread(queueID) },
          },
        } as ServerEvent);
      }
    });
    await flushAsync();

    expect(composerProbe().dataset.queuedIds).toBe("");
  });

  it("shows a steer in pending state before the IPC response resolves", async () => {
    let resolveSteer: ((value: { turn_id: string }) => void) | undefined;
    const steerTurn = vi.fn(
      () =>
        new Promise<{ turn_id: string }>((resolve) => {
          resolveSteer = resolve;
        }),
    );
    installWuuApi({ steerTurn: steerTurn as WuuDesktopApi["steerTurn"] });
    await act(async () => {
      root = createRoot(container);
      root.render(<App />);
    });
    await flushAsync();

    const textarea = composerProbe().querySelector("textarea");
    const steer = composerProbe().querySelector<HTMLButtonElement>(
      'button[aria-label="steer"]',
    );
    await act(async () => {
      if (!textarea || !steer) throw new Error("steer controls not rendered");
      const valueSetter = Object.getOwnPropertyDescriptor(
        HTMLTextAreaElement.prototype,
        "value",
      )?.set;
      valueSetter?.call(textarea, "guide the running turn");
      textarea.dispatchEvent(new Event("input", { bubbles: true }));
      steer.click();
      await Promise.resolve();
    });

    expect(steerTurn).toHaveBeenCalledTimes(1);
    expect(composerProbe().querySelector("textarea")?.value).toBe("");
    expect(composerProbe().dataset.guideIds).not.toBe("");

    await act(async () => {
      resolveSteer?.({ turn_id: "turn-current" });
      await Promise.resolve();
    });
  });

  it("rolls back an optimistic steer and restores the draft on IPC failure", async () => {
    let rejectSteer: ((reason: Error) => void) | undefined;
    const steerTurn = vi.fn(
      () =>
        new Promise<{ turn_id: string }>((_resolve, reject) => {
          rejectSteer = reject;
        }),
    );
    installWuuApi({ steerTurn: steerTurn as WuuDesktopApi["steerTurn"] });
    await act(async () => {
      root = createRoot(container);
      root.render(<App />);
    });
    await flushAsync();

    const textarea = composerProbe().querySelector("textarea");
    const steer = composerProbe().querySelector<HTMLButtonElement>(
      'button[aria-label="steer"]',
    );
    await act(async () => {
      if (!textarea || !steer) throw new Error("steer controls not rendered");
      const valueSetter = Object.getOwnPropertyDescriptor(
        HTMLTextAreaElement.prototype,
        "value",
      )?.set;
      valueSetter?.call(textarea, "restore this guide");
      textarea.dispatchEvent(new Event("input", { bubbles: true }));
      steer.click();
      await Promise.resolve();
    });
    expect(composerProbe().dataset.guideIds).not.toBe("");

    await act(async () => {
      rejectSteer?.(new Error("IPC unavailable"));
      await Promise.resolve();
    });
    await flushAsync();

    expect(composerProbe().dataset.guideIds).toBe("");
    expect(composerProbe().querySelector("textarea")?.value).toBe(
      "restore this guide",
    );
  });

  it("restores the authoritative held order from a resumed thread notification", async () => {
    installWuuApi();
    await act(async () => {
      root = createRoot(container);
      root.render(<App />);
    });
    await flushAsync();

    await act(async () => {
      for (const handler of serverEventHandlers) {
        handler({
          kind: "notification",
          workdir: workspace,
          message: {
            method: "thread/resumed",
            params: {
              thread: runningThread(),
              held_user_messages: [
                {
                  id: "guide-1",
                  thread_id: threadID,
                  origin: "steer",
                  prompt: "Guide",
                },
                {
                  id: "queue-1",
                  thread_id: threadID,
                  origin: "queue",
                  prompt: "First",
                },
                {
                  id: "queue-2",
                  thread_id: threadID,
                  origin: "queue",
                  prompt: "Second",
                },
              ],
            },
          },
        } as ServerEvent);
      }
    });
    await flushAsync();

    expect(composerProbe().dataset.guideIds).toBe("guide-1");
    expect(composerProbe().dataset.queuedIds).toBe("queue-1,queue-2");
    expect(composerProbe().dataset.heldOrder).toBe("guide-1,queue-1,queue-2");
  });
});
