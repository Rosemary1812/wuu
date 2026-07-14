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
        data-queued-ids={props.queuedMessages.map((message) => message.id).join(",")}
      >
        <textarea
          aria-label="composer-probe-input"
          value={props.prompt}
          onChange={(event) => props.setPrompt(event.currentTarget.value)}
        />
        <button type="button" onClick={props.onSend}>
          send
        </button>
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

function installWuuApi(): { queuedClientIDs: string[] } {
  const queuedClientIDs: string[] = [];
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
    queueTurn: vi.fn().mockImplementation(
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
  return { queuedClientIDs };
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

  it("hides a queued entry while its content is being edited", async () => {
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
});
