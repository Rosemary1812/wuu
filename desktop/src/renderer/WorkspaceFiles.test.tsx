import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { RuntimeContext, WorkspaceDirectoryListResult, WorkspaceFileReadResult } from "../shared/protocol";
import { WorkspaceFilePreview, WorkspaceFileTree } from "./WorkspaceFiles";

vi.mock("./WorkspaceMonacoEditor", () => ({
  WorkspaceMonacoEditor: ({
    path,
    text,
    readOnly,
    onChange,
    onSave,
  }: {
    path: string;
    text: string;
    readOnly?: boolean;
    onChange?: (value: string) => void;
    onSave?: () => void;
  }) => (
    <div
      className="workspace-monaco-editor"
      data-path={path}
      data-readonly={readOnly ? "true" : "false"}
      data-text={text}
    >
      <pre>{text}</pre>
      <button type="button" className="mock-editor-edit" disabled={readOnly} onClick={() => onChange?.("edited code\n")}>
        mock edit
      </button>
      <button type="button" className="mock-editor-edit-second" disabled={readOnly} onClick={() => onChange?.("second edit\n")}>
        mock edit second
      </button>
      <button type="button" className="mock-editor-save" disabled={readOnly} onClick={() => onSave?.()}>
        mock save
      </button>
    </div>
  ),
}));

vi.mock("./WorkspaceMarkdownEditor", () => ({
  WorkspaceMarkdownEditor: ({
    markdown,
    readOnly,
    onChange,
    onSave,
  }: {
    markdown: string;
    readOnly?: boolean;
    onChange?: (value: string) => void;
    onSave?: () => void;
  }) => (
    <div
      className="workspace-markdown-wysiwyg"
      data-readonly={readOnly ? "true" : "false"}
      data-text={markdown}
    >
      <pre>{markdown}</pre>
      <button type="button" className="mock-markdown-edit" disabled={readOnly} onClick={() => onChange?.("# Edited notes\n")}>
        mock markdown edit
      </button>
      <button type="button" className="mock-markdown-save" disabled={readOnly} onClick={() => onSave?.()}>
        mock markdown save
      </button>
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

function workspaceFile(overrides: Partial<WorkspaceFileReadResult> = {}): WorkspaceFileReadResult {
  return {
    root: "/repo",
    path: "src/index.ts",
    absolute_path: "/repo/src/index.ts",
    size_bytes: 12,
    mtime_ms: 1000,
    sha256: "a".repeat(64),
    binary: false,
    truncated: false,
    text: "button code",
    ...overrides,
  };
}

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
    Promise.resolve(workspaceFile({ path, absolute_path: `/repo/${path}` })),
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

async function click(selector: string): Promise<void> {
  const element = container.querySelector<HTMLButtonElement>(selector);
  if (!element) {
    throw new Error(`missing button ${selector}`);
  }
  await act(async () => {
    element.click();
    await Promise.resolve();
  });
}

async function clickButtonByText(text: string): Promise<void> {
  const button = [...container.querySelectorAll<HTMLButtonElement>("button")].find(
    (candidate) => candidate.textContent?.trim() === text,
  );
  if (!button) {
    throw new Error(`missing button ${text}`);
  }
  await act(async () => {
    button.click();
    await Promise.resolve();
  });
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

  it("opens selected non-Markdown text files in the center Monaco editor surface", async () => {
    readWorkspaceFile.mockResolvedValueOnce({
      root: "/repo",
      path: "AGENTS.txt",
      absolute_path: "/repo/AGENTS.txt",
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
        selectedFilePath="/repo/AGENTS.txt"
        onOpenRightPanel={() => {}}
      />,
    );

    await settleDirectoryLoads();

    expect(readWorkspaceFile).toHaveBeenCalledWith("AGENTS.txt", "/repo");
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

  it("marks an editable file dirty when the Monaco text changes", async () => {
    await render(
      <WorkspaceFilePreview
        activeContext={activeContext}
        selectedFilePath="/repo/src/index.ts"
        onOpenRightPanel={() => {}}
      />,
    );

    await settleDirectoryLoads();
    await click(".mock-editor-edit");

    expect(container.textContent).toContain("已修改");
    expect(container.querySelector<HTMLButtonElement>(".workspace-file-save-button")?.disabled).toBe(false);
  });

  it("saves dirty Monaco content with the latest base metadata and clears the dirty state", async () => {
    writeWorkspaceFile
      .mockResolvedValueOnce({
        status: "saved",
        file: workspaceFile({
          text: "edited code\n",
          mtime_ms: 2000,
          sha256: "b".repeat(64),
        }),
      })
      .mockResolvedValueOnce({
        status: "saved",
        file: workspaceFile({
          text: "second edit\n",
          mtime_ms: 3000,
          sha256: "c".repeat(64),
        }),
      });

    await render(
      <WorkspaceFilePreview
        activeContext={activeContext}
        selectedFilePath="/repo/src/index.ts"
        onOpenRightPanel={() => {}}
      />,
    );

    await settleDirectoryLoads();
    await click(".mock-editor-edit");
    await click(".mock-editor-save");

    expect(writeWorkspaceFile).toHaveBeenNthCalledWith(
      1,
      {
        path: "src/index.ts",
        text: "edited code\n",
        base_mtime_ms: 1000,
        base_sha256: "a".repeat(64),
      },
      "/repo",
    );
    expect(container.textContent).toContain("已保存");
    expect(container.querySelector<HTMLButtonElement>(".workspace-file-save-button")?.disabled).toBe(true);

    await click(".mock-editor-edit-second");
    await click(".mock-editor-save");

    expect(writeWorkspaceFile).toHaveBeenNthCalledWith(
      2,
      {
        path: "src/index.ts",
        text: "second edit\n",
        base_mtime_ms: 2000,
        base_sha256: "b".repeat(64),
      },
      "/repo",
    );
  });

  it("keeps dirty editor text visible and reports a conflict when the file changed on disk", async () => {
    writeWorkspaceFile.mockResolvedValueOnce({
      status: "conflict",
      file: workspaceFile({
        text: "external edit\n",
        mtime_ms: 2000,
        sha256: "d".repeat(64),
      }),
    });

    await render(
      <WorkspaceFilePreview
        activeContext={activeContext}
        selectedFilePath="/repo/src/index.ts"
        onOpenRightPanel={() => {}}
      />,
    );

    await settleDirectoryLoads();
    await click(".mock-editor-edit");
    await click(".mock-editor-save");

    expect(container.textContent).toContain("文件已在外部修改");
    expect(container.querySelector<HTMLElement>(".workspace-monaco-editor")?.dataset.text).toBe("edited code\n");
    expect(container.querySelector<HTMLButtonElement>(".workspace-file-save-button")?.disabled).toBe(false);
  });

  it("reports the active file dirty state to the shell", async () => {
    const onDirtyChange = vi.fn();
    writeWorkspaceFile.mockResolvedValueOnce({
      status: "saved",
      file: workspaceFile({
        text: "edited code\n",
        mtime_ms: 2000,
        sha256: "b".repeat(64),
      }),
    });

    await render(
      <WorkspaceFilePreview
        activeContext={activeContext}
        selectedFilePath="/repo/src/index.ts"
        onOpenRightPanel={() => {}}
        onDirtyChange={onDirtyChange}
      />,
    );

    await settleDirectoryLoads();

    expect(onDirtyChange).toHaveBeenLastCalledWith({
      root: "/repo",
      path: "src/index.ts",
      dirty: false,
    });

    await click(".mock-editor-edit");

    expect(onDirtyChange).toHaveBeenLastCalledWith({
      root: "/repo",
      path: "src/index.ts",
      dirty: true,
    });

    await click(".mock-editor-save");

    expect(onDirtyChange).toHaveBeenLastCalledWith({
      root: "/repo",
      path: "src/index.ts",
      dirty: false,
    });
  });

  it("keeps truncated text files readonly", async () => {
    readWorkspaceFile.mockResolvedValueOnce(workspaceFile({
      path: "large.log",
      absolute_path: "/repo/large.log",
      truncated: true,
      text: "partial log\n",
    }));

    await render(
      <WorkspaceFilePreview
        activeContext={activeContext}
        selectedFilePath="/repo/large.log"
        onOpenRightPanel={() => {}}
      />,
    );

    await settleDirectoryLoads();

    expect(container.textContent).toContain("只读");
    expect(container.textContent).toContain("文件较大，已截断");
    expect(container.querySelector<HTMLElement>(".workspace-monaco-editor")?.dataset.readonly).toBe("true");
    expect(container.querySelector<HTMLButtonElement>(".workspace-file-save-button")?.disabled).toBe(true);
  });

  it("keeps binary files out of the editor surface", async () => {
    readWorkspaceFile.mockResolvedValueOnce(workspaceFile({
      path: "archive.dat",
      absolute_path: "/repo/archive.dat",
      binary: true,
      text: undefined,
    }));

    await render(
      <WorkspaceFilePreview
        activeContext={activeContext}
        selectedFilePath="/repo/archive.dat"
        onOpenRightPanel={() => {}}
      />,
    );

    await settleDirectoryLoads();

    expect(container.textContent).toContain("二进制文件");
    expect(container.querySelector(".workspace-monaco-editor")).toBeNull();
  });

  it("renders image files with a renderable file URL", async () => {
    readWorkspaceFile.mockResolvedValueOnce(workspaceFile({
      path: "assets/mascot/wuu-mascot-concept-01.png",
      absolute_path: "/repo/assets/mascot/wuu-mascot-concept-01.png",
      binary: true,
      text: undefined,
      renderable_url: "wuu-file://local/encoded-image",
    }));

    await render(
      <WorkspaceFilePreview
        activeContext={activeContext}
        selectedFilePath="/repo/assets/mascot/wuu-mascot-concept-01.png"
        onOpenRightPanel={() => {}}
      />,
    );

    await settleDirectoryLoads();

    const image = container.querySelector<HTMLImageElement>(".workspace-file-image-preview img");
    expect(image?.getAttribute("src")).toBe("wuu-file://local/encoded-image");
    expect(image?.getAttribute("alt")).toBe("assets/mascot/wuu-mascot-concept-01.png");
    expect(container.textContent).not.toContain("二进制文件");
    expect(container.querySelector(".workspace-monaco-editor")).toBeNull();
  });

  it("opens Markdown files in reading mode by default", async () => {
    readWorkspaceFile.mockResolvedValueOnce(workspaceFile({
      path: "README.md",
      absolute_path: "/repo/README.md",
      text: "# Project Notes\n\n- review changes\n",
    }));

    await render(
      <WorkspaceFilePreview
        activeContext={activeContext}
        selectedFilePath="/repo/README.md"
        onOpenRightPanel={() => {}}
      />,
    );

    await settleDirectoryLoads();

    expect(container.textContent).toContain("阅读");
    expect(container.textContent).toContain("编辑");
    expect(container.textContent).toContain("源码");
    expect(container.querySelector(".workspace-markdown-reading")).not.toBeNull();
    expect(container.querySelector(".workspace-markdown-reading .rich-heading")?.textContent).toContain("Project Notes");
    expect(container.querySelector(".workspace-monaco-editor")).toBeNull();
  });

  it("switches Markdown files between WYSIWYG and Monaco source modes", async () => {
    readWorkspaceFile.mockResolvedValueOnce(workspaceFile({
      path: "README.md",
      absolute_path: "/repo/README.md",
      text: "# Project Notes\n",
    }));

    await render(
      <WorkspaceFilePreview
        activeContext={activeContext}
        selectedFilePath="/repo/README.md"
        onOpenRightPanel={() => {}}
      />,
    );

    await settleDirectoryLoads();
    await clickButtonByText("编辑");

    expect(container.querySelector<HTMLElement>(".workspace-markdown-wysiwyg")?.dataset.text).toBe("# Project Notes\n");
    expect(container.querySelector(".workspace-monaco-editor")).toBeNull();

    await clickButtonByText("源码");

    expect(container.querySelector<HTMLElement>(".workspace-monaco-editor")?.dataset.path).toBe("README.md");
    expect(container.querySelector<HTMLElement>(".workspace-monaco-editor")?.dataset.text).toBe("# Project Notes\n");
  });

  it("shares dirty Markdown content across WYSIWYG, reading, source, and save", async () => {
    readWorkspaceFile.mockResolvedValueOnce(workspaceFile({
      path: "README.md",
      absolute_path: "/repo/README.md",
      text: "# Project Notes\n",
    }));
    writeWorkspaceFile.mockResolvedValueOnce({
      status: "saved",
      file: workspaceFile({
        path: "README.md",
        absolute_path: "/repo/README.md",
        text: "# Edited notes\n",
        mtime_ms: 2000,
        sha256: "e".repeat(64),
      }),
    });

    await render(
      <WorkspaceFilePreview
        activeContext={activeContext}
        selectedFilePath="/repo/README.md"
        onOpenRightPanel={() => {}}
      />,
    );

    await settleDirectoryLoads();
    await clickButtonByText("编辑");
    await click(".mock-markdown-edit");

    expect(container.textContent).toContain("已修改");

    await clickButtonByText("阅读");
    expect(container.querySelector(".workspace-markdown-reading")?.textContent).toContain("Edited notes");

    await clickButtonByText("源码");
    expect(container.querySelector<HTMLElement>(".workspace-monaco-editor")?.dataset.text).toBe("# Edited notes\n");

    await click(".mock-editor-save");

    expect(writeWorkspaceFile).toHaveBeenCalledWith(
      {
        path: "README.md",
        text: "# Edited notes\n",
        base_mtime_ms: 1000,
        base_sha256: "a".repeat(64),
      },
      "/repo",
    );
    expect(container.textContent).toContain("已保存");
  });
});
