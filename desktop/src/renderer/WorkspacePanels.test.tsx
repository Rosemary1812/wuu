import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { WorkspaceRightPanel } from "./WorkspacePanels";
import type { RuntimeContext } from "../shared/protocol";
import type { TurnFileDiffSelection } from "./TurnFileDiffTypes";
import { workspaceDiffViewTab, workspaceToolViewTab, type WorkspaceViewTab } from "./WorkspaceViewTabs";

// Renders the cwd it received so tests can assert which context prop
// (activeContext vs workspaceContext) actually reached the terminal panel,
// without pulling in the real xterm/node-pty-backed component.
vi.mock("./WorkspaceTerminalPanel", () => ({
  WorkspaceTerminalPanel: ({ activeContext }: { activeContext?: RuntimeContext }) => (
    <div data-testid="terminal-panel" data-cwd={activeContext?.cwd ?? ""} />
  ),
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

function makeSelection(path: string): TurnFileDiffSelection {
  return {
    path,
    additions: 1,
    deletions: 1,
    newFile: false,
    diff: {
      path,
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
  };
}

function baseProps(): Parameters<typeof WorkspaceRightPanel>[0] {
  return {
    open: true,
    present: true,
    tabs: [],
    activeTabID: undefined,
    onSelectTab: () => {},
    onOpenTool: () => {},
    onShowTools: () => {},
    onCloseTab: () => {},
    onReorderTabs: () => {},
    onOpenFile: () => {},
    onClose: () => {},
  };
}

describe("WorkspaceRightPanel", () => {
  it("renders a diff tab as a unified, closable right panel tab (folded into the tab strip)", () => {
    const onCloseTab = vi.fn();
    const diffTab = workspaceDiffViewTab({
      threadID: "thread-1",
      path: "/tmp/a.txt",
      selection: makeSelection("/tmp/a.txt"),
    });

    mount(
      <WorkspaceRightPanel
        {...baseProps()}
        tabs={[diffTab]}
        activeTabID={diffTab.id}
        onCloseTab={onCloseTab}
      />,
    );

    const panel = container?.querySelector<HTMLElement>(".workspace-right-panel.detail.diff");
    expect(panel).toBeTruthy();
    expect(panel?.querySelector(".workspace-panel-body")?.textContent).toContain("/tmp/a.txt");
    expect(panel?.querySelector(".workspace-panel-body")?.textContent).toContain("new value");

    // The diff tab shows up in the unified tab strip, not as a separate
    // pseudo-tab: it has a title (basename), a close button, and (once
    // there's more than one tab) is reorderable just like a tool tab.
    const tabButton = panel?.querySelector<HTMLButtonElement>(".workspace-tool-tab-main");
    expect(tabButton?.textContent).toContain("a.txt");
    expect(tabButton?.getAttribute("title")).toBe("/tmp/a.txt");

    act(() => {
      panel?.querySelector<HTMLButtonElement>(".turn-file-diff-close")?.click();
    });
    expect(onCloseTab).toHaveBeenCalledTimes(1);
    expect(onCloseTab).toHaveBeenCalledWith(diffTab.id);

    onCloseTab.mockClear();
    act(() => {
      panel?.querySelector<HTMLButtonElement>(".workspace-tool-tab-close")?.click();
    });
    expect(onCloseTab).toHaveBeenCalledTimes(1);
    expect(onCloseTab).toHaveBeenCalledWith(diffTab.id);
  });

  it("supports several diff tabs open side by side alongside tool tabs", () => {
    const onSelectTab = vi.fn();
    const filesTab = workspaceToolViewTab("files");
    const diffA = workspaceDiffViewTab({
      threadID: "thread-1",
      path: "a.txt",
      selection: makeSelection("a.txt"),
    });
    const diffB = workspaceDiffViewTab({
      threadID: "thread-1",
      path: "src/b.txt",
      selection: makeSelection("src/b.txt"),
    });
    const tabs: WorkspaceViewTab[] = [filesTab, diffA, diffB];

    mount(
      <WorkspaceRightPanel
        {...baseProps()}
        tabs={tabs}
        activeTabID={diffB.id}
        onSelectTab={onSelectTab}
      />,
    );

    const tabButtons = container?.querySelectorAll(".workspace-panel-tabs .workspace-tool-tab");
    expect(tabButtons?.length).toBe(3);
    // Reorderable once more than one tab is present, regardless of kind.
    expect(container?.querySelectorAll(".workspace-tool-tab.can-reorder").length).toBe(3);

    const diffBTab = container?.querySelector(".workspace-tool-tab.active");
    expect(diffBTab?.textContent).toContain("b.txt");

    const diffAButton = Array.from(
      container?.querySelectorAll<HTMLButtonElement>(".workspace-tool-tab-main") ?? [],
    ).find((button) => button.textContent?.includes("a.txt") && !button.textContent.includes("b.txt"));
    act(() => {
      diffAButton?.click();
    });
    expect(onSelectTab).toHaveBeenCalledWith(diffA.id);
  });

  it("shows the tool picker when there is no active tab, and marks open tools active", () => {
    const onOpenTool = vi.fn();
    const filesTab = workspaceToolViewTab("files");

    mount(
      <WorkspaceRightPanel
        {...baseProps()}
        tabs={[filesTab]}
        activeTabID={undefined}
        onOpenTool={onOpenTool}
      />,
    );

    const panel = container?.querySelector<HTMLElement>(".workspace-right-panel.tools");
    expect(panel).toBeTruthy();
    const picker = panel?.querySelector(".workspace-tool-menu");
    expect(picker).toBeTruthy();
    expect(picker?.querySelector(".workspace-tool-menu-item.active")?.textContent).toContain("文件");

    act(() => {
      picker?.querySelectorAll<HTMLButtonElement>(".workspace-tool-menu-item")[1]?.click();
    });
    expect(onOpenTool).toHaveBeenCalledWith("review");
  });
});

describe("WorkspaceRightPanel context routing (Bug 3: worktree-fork panel root)", () => {
  const projectContext: RuntimeContext = {
    kind: "project",
    project_id: "project-1",
    cwd: "/repo/project",
  };

  it("roots the file tree on workspaceContext, not activeContext", () => {
    const filesTab = workspaceToolViewTab("files");

    // activeContext is defined but workspaceContext is left undefined: if
    // the file tree fell back to activeContext it would render the normal
    // "loading" panel instead of the no-project empty state, since it would
    // never observe workspaceRoot as absent.
    mount(
      <WorkspaceRightPanel
        {...baseProps()}
        tabs={[filesTab]}
        activeTabID={filesTab.id}
        activeContext={projectContext}
        workspaceContext={undefined}
      />,
    );

    const panel = container?.querySelector<HTMLElement>(".workspace-right-panel");
    expect(panel?.textContent).toContain("没有项目");
  });

  it("roots the terminal on workspaceContext, not activeContext", () => {
    const terminalTab = workspaceToolViewTab("terminal");
    const worktreeContext: RuntimeContext = {
      kind: "project",
      project_id: "project-1",
      cwd: "/Users/me/.wuu/worktrees/fork-1/project",
    };

    mount(
      <WorkspaceRightPanel
        {...baseProps()}
        tabs={[terminalTab]}
        activeTabID={terminalTab.id}
        activeContext={projectContext}
        workspaceContext={worktreeContext}
      />,
    );

    const terminalPanel = container?.querySelector<HTMLElement>('[data-testid="terminal-panel"]');
    expect(terminalPanel?.getAttribute("data-cwd")).toBe(worktreeContext.cwd);
  });
});
