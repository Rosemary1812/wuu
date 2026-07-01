import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";
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
  FitAddon: vi.fn().mockImplementation(() => ({
    fit: vi.fn(),
  })),
}));

let container: HTMLDivElement;
let root: Root | null = null;
let serverEventHandlers: Array<(event: ServerEvent) => void> = [];

const workspace = "/tmp/wuu-approval-test";

function initialized(): InitializeResult {
  return {
    protocol_version: "wuu-app-server/v0.1",
    provider: "fake",
    model: "fake-model",
    workspace_root: workspace,
    tool_policy: {},
    permissions: {
      mode: "default",
      permission_profile: "workspace_write",
      approval_policy: "on_request",
      approvals_reviewer: "user",
    },
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

function threadWithToolCall(): Thread {
  return {
    id: "thread-1",
    preview: "needs approval",
    model_provider: "fake",
    model: "fake-model",
    cwd: workspace,
    status: "in_progress",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    turns: [
      {
        id: "turn-1",
        items_view: "full",
        status: "in_progress",
        items: [
          {
            id: "call-1",
            type: "tool_call",
            status: "in_progress",
            name: "run_shell",
            arguments: '{"command":"npm install"}',
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

function installWuuApi(): {
  respondToServerRequest: ReturnType<typeof vi.fn>;
} {
  const thread = threadWithToolCall();
  const respondToServerRequest = vi.fn().mockResolvedValue(undefined);
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
    listManagedProcesses: vi.fn().mockResolvedValue({ processes: [] }),
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
    respondToServerRequest,
    rejectServerRequest: vi.fn().mockResolvedValue(undefined),
  } as unknown as WuuDesktopApi;
  Object.defineProperty(window, "wuu", {
    configurable: true,
    value: api,
  });
  return { respondToServerRequest };
}

async function flushAsync(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe("App tool approval flow", () => {
  beforeEach(() => {
    installWindowStubs();
    serverEventHandlers = [];
    container = document.createElement("div");
    document.body.appendChild(container);
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

  it("shows the inline approval actions when the server requests tool approval", async () => {
    const { respondToServerRequest } = installWuuApi();

    await act(async () => {
      root = createRoot(container);
      root.render(<App />);
    });
    await flushAsync();

    expect(container.textContent).toContain("needs approval");

    await act(async () => {
      for (const handler of serverEventHandlers) {
        handler({
          kind: "server-request",
          workdir: workspace,
          message: {
            id: "server-request-1",
            method: "tool/approval/request",
            params: {
              id: "approval-1",
              tool_name: "run_shell",
              call_id: "call-1",
              risk: "high",
              policy_reason: "需要审批",
              arguments_preview: '{"command":"npm install"}',
              capability: "command.bash",
              capability_object: "npm install",
              capability_action: "execute",
            },
          },
        });
      }
    });

    expect(container.textContent).toContain("批准一次");

    const approve = Array.from(container.querySelectorAll("button")).find(
      (button) => button.textContent?.trim() === "批准一次",
    );
    expect(approve).toBeDefined();

    await act(async () => {
      approve?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    expect(respondToServerRequest).toHaveBeenCalledWith("server-request-1", {
      decision: "approved",
      reason: "user approved the requested tool call",
    });
  });
});
