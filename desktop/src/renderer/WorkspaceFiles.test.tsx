import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { RuntimeContext, WorkspaceDirectoryListResult } from "../shared/protocol";
import { WorkspaceFilePreview, WorkspaceFileTree } from "./WorkspaceFiles";

vi.mock("./WorkspaceMonacoEditor", () => ({
  WorkspaceMonacoEditor: ({
    path,
    text,
  }: {
    path: string;
    text: string;
  }) => (
    <div className="workspace-monaco-editor" data-path={path}>
      {text}
    </div>
  ),
}));

let container: HTMLDivElement;
let root: Root | null = null;
let listWorkspaceDirectory: ReturnType<typeof vi.fn>;
let readWorkspaceFile: ReturnType<typeof vi.fn>;
let writeWorkspaceFile: ReturnType<typeof vi.fn>;
let scrollIntoView: ReturnType<typeof vi.fn>;

const activeContext: RuntimeContext = {
  kind: "project",
  project_id: "project-1",
  cwd: "/repo",
};

const directoryResults: Record<string, WorkspaceDirectoryListResult> = {
  "": {
    root: "/repo",
    path: "",
    entries: [
      { kind: "directory", name: "src", path: "src/" },
      { kind: "file", name: "README.md", path: "README.md" },
    ],
    truncated: false,
  },
  src: {
    root: "/repo",
    path: "src",
    entries: [
      { kind: "directory", name: "components", path: "src/components/" },
      { kind: "file", name: "index.ts", path: "src/index.ts" },
    ],
    truncated: false,
  },
  "src/components": {
    root: "/repo",
    path: "src/components",
    entries: [
      { kind: "file", name: "Button.tsx", path: "src/components/Button.tsx" },
    ],
    truncated: false,
  },
};

beforeEach(() => {
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  listWorkspaceDirectory = vi.fn((path?: string) =>
    Promise.resolve(directoryResults[path ?? ""]),
  );
  readWorkspaceFile = vi.fn((path: string) =>
    Promise.resolve({
      root: "/repo",
      path,
      absolute_path: `/repo/${path}`,
      size_bytes: 12,
      mtime_ms: 1000,
      sha256: "a".repeat(64),
      binary: false,
      truncated: false,
      text: "button code",
    }),
  );
  writeWorkspaceFile = vi.fn();
  Object.defineProperty(window, "wuu", {
    configurable: true,
    value: {
      listWorkspaceDirectory,
      readWorkspaceFile,
      writeWorkspaceFile,
    },
  });
  scrollIntoView = vi.fn();
  Element.prototype.scrollIntoView = scrollIntoView;
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
}

async function settleDirectoryLoads(): Promise<void> {
  for (let index = 0; index < 4; index += 1) {
    await act(async () => {
      await Promise.resolve();
    });
  }
}

describe("WorkspaceFileTree", () => {
  it("expands and scrolls to the selected workspace file path", async () => {
    await render(
      <WorkspaceFileTree
        activeContext={activeContext}
        open
        selectedFilePath="/repo/src/components/Button.tsx"
        onOpenFile={() => {}}
      />,
    );

    await settleDirectoryLoads();

    expect(listWorkspaceDirectory).toHaveBeenCalledWith("", "/repo");
    expect(listWorkspaceDirectory).toHaveBeenCalledWith("src", "/repo");
    expect(listWorkspaceDirectory).toHaveBeenCalledWith("src/components", "/repo");

    const selected = container.querySelector<HTMLButtonElement>(
      ".workspace-file-tree-row.selected",
    );
    expect(selected?.title).toBe("src/components/Button.tsx");
    expect(selected?.textContent).toContain("Button.tsx");
    expect(scrollIntoView).toHaveBeenCalledWith({
      block: "nearest",
      inline: "nearest",
    });
  });

  it("forwards a worktree root that differs from /repo through to the preload API", async () => {
    const worktreeContext: RuntimeContext = {
      kind: "project",
      project_id: "project-1",
      cwd: "/worktrees/fork-1",
    };
    listWorkspaceDirectory.mockResolvedValueOnce({
      root: "/worktrees/fork-1",
      path: "",
      entries: [{ kind: "file", name: "README.md", path: "README.md" }],
      truncated: false,
    });

    await render(
      <WorkspaceFileTree
        activeContext={worktreeContext}
        open
        onOpenFile={() => {}}
      />,
    );

    await settleDirectoryLoads();

    expect(listWorkspaceDirectory).toHaveBeenCalledWith("", "/worktrees/fork-1");
    expect(listWorkspaceDirectory).not.toHaveBeenCalledWith("", "/repo");
  });

  it("reads an absolute selected file path relative to the workspace root", async () => {
    await render(
      <WorkspaceFilePreview
        activeContext={activeContext}
        selectedFilePath="/repo/src/components/Button.tsx"
        onOpenRightPanel={() => {}}
      />,
    );

    await settleDirectoryLoads();

    expect(readWorkspaceFile).toHaveBeenCalledWith("src/components/Button.tsx", "/repo");
    expect(container.textContent).toContain("button code");
  });

  it("opens selected text files in the center Monaco editor surface", async () => {
    readWorkspaceFile.mockResolvedValueOnce({
      root: "/repo",
      path: "AGENTS.md",
      absolute_path: "/repo/AGENTS.md",
      size_bytes: 35,
      mtime_ms: 1000,
      sha256: "b".repeat(64),
      binary: false,
      truncated: false,
      text: "## Execution Autonomy\n\n- Keep going.\n",
    });

    await render(
      <WorkspaceFilePreview
        activeContext={activeContext}
        selectedFilePath="/repo/AGENTS.md"
        onOpenRightPanel={() => {}}
      />,
    );

    await settleDirectoryLoads();

    expect(readWorkspaceFile).toHaveBeenCalledWith("AGENTS.md", "/repo");
    expect(container.querySelector(".workspace-monaco-editor")).not.toBeNull();
    expect(container.querySelector(".workspace-file-preview-header")).toBeNull();
    expect(container.textContent).toContain("Execution Autonomy");
  });

  it("renders code files in Monaco without treating source as HTML", async () => {
    readWorkspaceFile.mockResolvedValueOnce({
      root: "/repo",
      path: "src/index.ts",
      absolute_path: "/repo/src/index.ts",
      size_bytes: 44,
      mtime_ms: 1000,
      sha256: "c".repeat(64),
      binary: false,
      truncated: false,
      text: 'const answer = 42;\nconsole.log("<tag>", answer);\n',
    });

    await render(
      <WorkspaceFilePreview
        activeContext={activeContext}
        selectedFilePath="/repo/src/index.ts"
        onOpenRightPanel={() => {}}
      />,
    );

    await settleDirectoryLoads();

    const content = container.querySelector<HTMLElement>(".workspace-monaco-editor");
    expect(content?.textContent).toContain('console.log("<tag>", answer);');
    expect(content?.innerHTML).not.toContain("<tag>");
  });
});
