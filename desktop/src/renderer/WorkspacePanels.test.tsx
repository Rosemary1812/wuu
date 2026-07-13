import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { WorkspaceRightPanel } from "./WorkspacePanels";
import type { RuntimeContext } from "../shared/protocol";
import type { TurnFileDiffSelection } from "./TurnFileDiffTypes";
import {
  workspaceDiffViewTab,
  workspaceFileViewTab,
  workspaceToolViewTab,
  type WorkspaceViewTab,
} from "./WorkspaceViewTabs";

// Renders the cwd it received so tests can assert which context prop
// (activeContext vs workspaceContext) actually reached the terminal panel,
// without pulling in the real xterm/node-pty-backed component.
vi.mock("./WorkspaceTerminalPanel", () => ({
  WorkspaceTerminalPanel: ({ activeContext }: { activeContext?: RuntimeContext }) => (
    <div data-testid="terminal-panel" data-cwd={activeContext?.cwd ?? ""} />
  ),
}));

vi.mock("./WorkspaceMonacoEditor", () => ({
  WorkspaceMonacoEditor: ({
    initialViewState,
    onViewStateChange,
    path,
    resourceID,
    text,
    onChange,
  }: {
    initialViewState?: { scrollTop: number } | null;
    onViewStateChange?: (state: { scrollTop: number }) => void;
    path: string;
    resourceID: string;
    text: string;
    onChange?: (value: string) => void;
  }) => (
    <div
      className="workspace-monaco-editor"
      data-path={path}
      data-resource-id={resourceID}
      data-text={text}
      data-view-scroll={initialViewState?.scrollTop ?? 0}
    >
      <button type="button" className="mock-editor-edit" onClick={() => onChange?.(`edited ${path}`)}>
        edit
      </button>
      <button
        type="button"
        className="mock-editor-scroll"
        onClick={() => onViewStateChange?.({ scrollTop: 42 })}
      >
        scroll
      </button>
    </div>
  ),
}));

let container: HTMLDivElement | null = null;
let root: Root | null = null;

beforeEach(() => {
  Object.defineProperty(window, "wuu", {
    configurable: true,
    value: {
      listWorkspaceDirectory: vi.fn().mockResolvedValue({
        root: "/repo/project",
        path: "",
        entries: [],
        truncated: false,
      }),
      readWorkspaceFile: vi.fn((path: string, rootPath?: string) =>
        Promise.resolve({
          root: rootPath ?? "/repo/project",
          path,
          absolute_path: `${rootPath ?? "/repo/project"}/${path}`,
          size_bytes: 12,
          mtime_ms: 1000,
          sha256: "a".repeat(64),
          binary: false,
          truncated: false,
          text: `source ${path}`,
        }),
      ),
      writeWorkspaceFile: vi.fn(),
      revealWorkspaceItem: vi.fn().mockResolvedValue(undefined),
    },
  });
});

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

function fileResource(tabID: string): HTMLElement | undefined {
  return Array.from(container?.querySelectorAll<HTMLElement>(".workspace-file-resource") ?? []).find(
    (resource) => resource.dataset.workspaceTabId === tabID,
  );
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
    globalized: false,
    onToggleGlobalize: () => {},
  };
}

describe("WorkspaceRightPanel", () => {
  it("opens navigation over a focused workspace and explains forced compact focus", () => {
    const onOpenSidebar = vi.fn();
    const props = {
      ...baseProps(),
      globalized: true,
      onOpenSidebar,
      canExitGlobalized: false,
    } as Parameters<typeof WorkspaceRightPanel>[0];
    mount(<WorkspaceRightPanel {...props} />);

    const navigation = container?.querySelector<HTMLButtonElement>(
      '[aria-label="打开导航侧栏"]',
    );
    expect(navigation).not.toBeNull();
    const navigationIcon = navigation?.querySelector("svg");
    expect(navigationIcon?.getAttribute("width")).toBe("18");
    expect(navigationIcon?.getAttribute("height")).toBe("18");
    expect(navigationIcon?.getAttribute("viewBox")).toBe("2 2 20 20");
    expect(navigationIcon?.getAttribute("stroke-width")).toBe("1.67");
    act(() => navigation?.click());
    expect(onOpenSidebar).toHaveBeenCalledTimes(1);

    const restore = container?.querySelector<HTMLButtonElement>(
      '[aria-label="窗口过窄，无法停靠右侧栏"]',
    );
    expect(restore?.disabled).toBe(true);
  });

  it("presents the workspace expansion as full-panel focus mode", () => {
    const onToggleGlobalize = vi.fn();
    mount(
      <WorkspaceRightPanel
        {...baseProps()}
        onToggleGlobalize={onToggleGlobalize}
      />,
    );

    const expand = container?.querySelector<HTMLButtonElement>('[aria-label="展开为全面板"]');
    expect(expand).not.toBeNull();
    expect(expand?.title).toBe("展开为全面板");
    act(() => expand?.click());
    expect(onToggleGlobalize).toHaveBeenCalledTimes(1);

    act(() => {
      root?.render(
        <WorkspaceRightPanel
          {...baseProps()}
          globalized
          onToggleGlobalize={onToggleGlobalize}
        />,
      );
    });
    expect(container?.querySelector('[aria-label="退出全面板"]')).not.toBeNull();
  });

  it("makes the retained workspace inert and releases its editor while closed", async () => {
    const fileTab = workspaceFileViewTab({
      context: {
        kind: "project",
        project_id: "project-1",
        cwd: "/repo/project",
      },
      path: "src/App.tsx",
    });
    mount(
      <WorkspaceRightPanel
        {...baseProps()}
        open={false}
        present={false}
        tabs={[fileTab]}
        activeTabID={fileTab.id}
      />,
    );
    await act(async () => Promise.resolve());

    const panel = container?.querySelector<HTMLElement>(".workspace-right-panel");
    expect(panel?.hasAttribute("inert")).toBe(true);
    expect(panel?.querySelector(".workspace-monaco-editor")).toBeNull();
  });

  it("renders an editable file resource inside the right workspace", async () => {
    const fileTab = workspaceFileViewTab({
      context: {
        kind: "project",
        project_id: "project-1",
        cwd: "/repo/project",
      },
      path: "src/App.tsx",
    });

    mount(
      <WorkspaceRightPanel
        {...baseProps()}
        tabs={[fileTab]}
        activeTabID={fileTab.id}
      />,
    );
    await act(async () => Promise.resolve());

    const panel = container?.querySelector<HTMLElement>(".workspace-right-panel.detail.file");
    expect(panel).toBeTruthy();
    expect(panel?.querySelector(".workspace-tool-tab.active")?.textContent).toContain("App.tsx");
    expect(panel?.querySelector(".workspace-tool-tab-main")?.getAttribute("title")).toBe("src/App.tsx");
    expect(panel?.querySelector(".workspace-file-resource.active .workspace-file-preview")).toBeTruthy();
  });

  it("keeps inactive file state without retaining inactive Monaco editors", async () => {
    const context: RuntimeContext = {
      kind: "project",
      project_id: "project-1",
      cwd: "/repo/project",
    };
    const fileA = workspaceFileViewTab({ context, path: "src/a.ts" });
    const fileB = workspaceFileViewTab({ context, path: "src/b.ts" });
    const tabs: WorkspaceViewTab[] = [fileA, fileB];

    mount(
      <WorkspaceRightPanel
        {...baseProps()}
        tabs={tabs}
        activeTabID={fileA.id}
      />,
    );
    await act(async () => Promise.resolve());
    act(() => {
      fileResource(fileA.id)?.querySelector<HTMLButtonElement>(".mock-editor-edit")?.click();
      fileResource(fileA.id)?.querySelector<HTMLButtonElement>(".mock-editor-scroll")?.click();
    });

    act(() => {
      root?.render(
        <WorkspaceRightPanel
          {...baseProps()}
          tabs={tabs}
          activeTabID={fileB.id}
        />,
      );
    });
    await act(async () => Promise.resolve());

    const fileAResource = fileResource(fileA.id);
    expect(fileAResource).toBeTruthy();
    expect(fileAResource?.hidden).toBe(true);
    expect(fileAResource?.querySelector(".workspace-monaco-editor")).toBeNull();
    expect(container?.querySelectorAll(".workspace-monaco-editor")).toHaveLength(1);
    expect(container?.querySelectorAll(".workspace-file-resource")).toHaveLength(2);

    act(() => {
      root?.render(
        <WorkspaceRightPanel
          {...baseProps()}
          tabs={tabs}
          activeTabID={fileA.id}
        />,
      );
    });
    const restoredEditor = fileResource(fileA.id)?.querySelector(".workspace-monaco-editor");
    expect(restoredEditor?.getAttribute("data-text")).toBe("edited src/a.ts");
    expect(restoredEditor?.getAttribute("data-view-scroll")).toBe("42");
  });

  it("gives same-path files in separate worktrees distinct Monaco resources", async () => {
    const primary = workspaceFileViewTab({
      context: {
        kind: "project",
        project_id: "project-1",
        cwd: "/repo/project",
      },
      path: "src/App.tsx",
    });
    const worktree = workspaceFileViewTab({
      context: {
        kind: "project",
        project_id: "project-1",
        cwd: "/repo/worktrees/feature/project",
      },
      path: "src/App.tsx",
    });

    mount(
      <WorkspaceRightPanel
        {...baseProps()}
        tabs={[primary, worktree]}
        activeTabID={primary.id}
      />,
    );
    await act(async () => Promise.resolve());

    let resourceIDs = Array.from(
      container?.querySelectorAll<HTMLElement>(".workspace-monaco-editor") ?? [],
    ).map((editor) => editor.dataset.resourceId);
    expect(resourceIDs).toEqual([primary.id]);

    act(() => {
      root?.render(
        <WorkspaceRightPanel
          {...baseProps()}
          tabs={[primary, worktree]}
          activeTabID={worktree.id}
        />,
      );
    });
    resourceIDs = Array.from(
      container?.querySelectorAll<HTMLElement>(".workspace-monaco-editor") ?? [],
    ).map((editor) => editor.dataset.resourceId);
    expect(resourceIDs).toEqual([worktree.id]);
  });

  it("does not close a dirty file resource until the user confirms discarding its edit", async () => {
    const onCloseTab = vi.fn();
    const onDirtyFileTabsChange = vi.fn();
    const confirmDiscard = vi.spyOn(window, "confirm").mockReturnValue(false);
    const fileTab = workspaceFileViewTab({
      context: {
        kind: "project",
        project_id: "project-1",
        cwd: "/repo/project",
      },
      path: "src/dirty.ts",
    });

    mount(
      <WorkspaceRightPanel
        {...baseProps()}
        tabs={[fileTab]}
        activeTabID={fileTab.id}
        onCloseTab={onCloseTab}
        onDirtyFileTabsChange={onDirtyFileTabsChange}
      />,
    );
    await act(async () => Promise.resolve());
    act(() => {
      fileResource(fileTab.id)?.querySelector<HTMLButtonElement>(".mock-editor-edit")?.click();
    });
    expect(onDirtyFileTabsChange).toHaveBeenLastCalledWith(true);
    expect(container?.querySelector(".workspace-tool-tab.dirty")).not.toBeNull();
    expect(container?.querySelector(".workspace-tab-dirty-indicator")).not.toBeNull();
    act(() => {
      container?.querySelector<HTMLButtonElement>(".workspace-tool-tab-close")?.click();
    });

    expect(confirmDiscard).toHaveBeenCalledTimes(1);
    expect(onCloseTab).not.toHaveBeenCalled();

    confirmDiscard.mockReturnValue(true);
    act(() => {
      container?.querySelector<HTMLButtonElement>(".workspace-tool-tab-close")?.click();
    });
    expect(onCloseTab).toHaveBeenCalledWith(fileTab.id);
  });

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
    expect(container?.querySelector(".workspace-panel-tabs")?.getAttribute("aria-label")).toBe(
      "产物与工具",
    );
    const semanticTabs = container?.querySelectorAll<HTMLButtonElement>(".workspace-tool-tab-main");
    expect(semanticTabs?.[0]?.getAttribute("role")).toBe("tab");
    expect(semanticTabs?.[0]?.tabIndex).toBe(-1);
    expect(semanticTabs?.[2]?.getAttribute("aria-selected")).toBe("true");
    expect(semanticTabs?.[2]?.tabIndex).toBe(0);
    semanticTabs?.[2]?.focus();
    act(() => {
      semanticTabs?.[2]?.dispatchEvent(
        new KeyboardEvent("keydown", { key: "ArrowLeft", bubbles: true }),
      );
    });
    expect(onSelectTab).toHaveBeenCalledWith(diffA.id);
    expect(document.activeElement).toBe(semanticTabs?.[1]);

    const diffAButton = Array.from(
      container?.querySelectorAll<HTMLButtonElement>(".workspace-tool-tab-main") ?? [],
    ).find((button) => button.textContent?.includes("a.txt") && !button.textContent.includes("b.txt"));
    act(() => {
      diffAButton?.click();
    });
    expect(onSelectTab).toHaveBeenCalledWith(diffA.id);
  });

  it("restores keyboard focus to the next active workspace tab after close", () => {
    const filesTab = workspaceToolViewTab("files");
    const terminalTab = workspaceToolViewTab("terminal");
    const renderPanel = (tabs: WorkspaceViewTab[], activeTabID: string): void => {
      root?.render(
        <WorkspaceRightPanel
          {...baseProps()}
          tabs={tabs}
          activeTabID={activeTabID}
          onCloseTab={(id) => {
            expect(id).toBe(terminalTab.id);
            renderPanel([filesTab], filesTab.id);
          }}
        />,
      );
    };
    mount(
      <WorkspaceRightPanel
        {...baseProps()}
        tabs={[filesTab, terminalTab]}
        activeTabID={terminalTab.id}
        onCloseTab={(id) => {
          expect(id).toBe(terminalTab.id);
          renderPanel([filesTab], filesTab.id);
        }}
      />,
    );

    const activeClose = container?.querySelector<HTMLButtonElement>(
      ".workspace-tool-tab.active .workspace-tool-tab-close",
    );
    activeClose?.focus();
    act(() => activeClose?.click());

    expect(document.activeElement).toBe(
      container?.querySelector(".workspace-tool-tab.active .workspace-tool-tab-main"),
    );
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
