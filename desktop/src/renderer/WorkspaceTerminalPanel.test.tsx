import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { RuntimeContext } from "../shared/protocol";
import { WorkspaceTerminalPanel } from "./WorkspaceTerminalPanel";

// Stub xterm/the fit addon so mounting the real WorkspaceTerminalPanel
// doesn't need an actual terminal renderer or ResizeObserver-driven
// layout — mirrors the pattern used by AppApprovalFlow.test.tsx.
vi.mock("@xterm/xterm", () => ({
  Terminal: vi.fn().mockImplementation(() => ({
    loadAddon: vi.fn(),
    open: vi.fn(),
    focus: vi.fn(),
    write: vi.fn(),
    writeln: vi.fn(),
    dispose: vi.fn(),
    onData: vi.fn(() => ({ dispose: vi.fn() })),
    cols: 80,
    rows: 24,
  })),
}));

vi.mock("@xterm/addon-fit", () => ({
  FitAddon: vi.fn().mockImplementation(() => ({
    fit: vi.fn(),
  })),
}));

class StubResizeObserver {
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}

let container: HTMLDivElement;
let root: Root | null = null;
let startTerminalSession: ReturnType<typeof vi.fn>;

beforeEach(() => {
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  startTerminalSession = vi.fn().mockResolvedValue({
    id: "term-1",
    cwd: "/repo",
    shell: "/bin/zsh",
    started_at: new Date().toISOString(),
  });
  Object.defineProperty(window, "wuu", {
    configurable: true,
    value: {
      startTerminalSession,
      writeTerminalSession: vi.fn(),
      resizeTerminalSession: vi.fn(),
      stopTerminalSession: vi.fn(),
      onTerminalEvent: vi.fn(() => () => {}),
    },
  });
  (globalThis as unknown as { ResizeObserver: typeof StubResizeObserver }).ResizeObserver =
    StubResizeObserver;
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  root = null;
  container.remove();
  vi.restoreAllMocks();
});

async function render(element: JSX.Element): Promise<void> {
  await act(async () => {
    root?.render(element);
    await Promise.resolve();
  });
  // Let the requestAnimationFrame-scheduled fit/resize and the
  // startSession() microtask settle.
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe("WorkspaceTerminalPanel", () => {
  it("starts the pty session rooted at the workspace context's cwd (Bug 3: worktree-fork panel root)", async () => {
    const worktreeContext: RuntimeContext = {
      kind: "project",
      project_id: "project-1",
      cwd: "/worktrees/fork-1/project",
    };

    await render(<WorkspaceTerminalPanel activeContext={worktreeContext} />);

    expect(startTerminalSession).toHaveBeenCalledWith(
      expect.objectContaining({ cwd: "/worktrees/fork-1/project" }),
    );
  });

  it("does not render a terminal without a workspace context", () => {
    act(() => {
      root?.render(<WorkspaceTerminalPanel activeContext={undefined} />);
    });

    expect(startTerminalSession).not.toHaveBeenCalled();
    expect(container.textContent).toContain("没有项目");
  });
});
