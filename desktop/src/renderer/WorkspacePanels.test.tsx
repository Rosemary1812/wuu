import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { WorkspaceRightPanel } from "./WorkspacePanels";

vi.mock("./WorkspaceTerminalPanel", () => ({
  WorkspaceTerminalPanel: () => <div data-testid="terminal-panel" />,
}));

let container: HTMLDivElement | null = null;
let root: Root | null = null;

function mount(element: JSX.Element): void {
  if (container) unmount();
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  act(() => {
    root!.render(element);
  });
}

function unmount(): void {
  if (root) {
    act(() => {
      root!.unmount();
    });
    root = null;
  }
  container?.remove();
  container = null;
}

afterEach(() => {
  unmount();
});

describe("WorkspaceRightPanel", () => {
  it("renders a turn file diff as a right panel detail view", () => {
    const onCloseTurnFileDiff = vi.fn();

    mount(
      <WorkspaceRightPanel
        open
        present
        view="turn-diff"
        openTabs={[]}
        onSelectView={() => {}}
        onShowTools={() => {}}
        onCloseTab={() => {}}
        onReorderTabs={() => {}}
        onOpenFile={() => {}}
        onClose={() => {}}
        turnFileDiffSelection={{
          path: "/tmp/a.txt",
          additions: 1,
          deletions: 1,
          newFile: false,
          diff: {
            path: "/tmp/a.txt",
            hunks: [
              {
                oldStart: 1,
                newStart: 1,
                lines: [
                  { op: "delete", content: "old value" },
                  { op: "insert", content: "new value" },
                ],
              },
            ],
          },
        }}
        onCloseTurnFileDiff={onCloseTurnFileDiff}
      />,
    );

    const panel = container?.querySelector<HTMLElement>(
      ".workspace-right-panel.detail.turn-diff",
    );
    expect(panel).toBeTruthy();
    expect(panel?.querySelector(".workspace-panel-body")?.textContent).toContain(
      "/tmp/a.txt",
    );
    expect(panel?.querySelector(".workspace-panel-body")?.textContent).toContain(
      "new value",
    );

    act(() => {
      panel?.querySelector<HTMLButtonElement>(".turn-file-diff-close")?.click();
    });

    expect(onCloseTurnFileDiff).toHaveBeenCalledTimes(1);
  });
});
