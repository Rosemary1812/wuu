import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { WorkspaceReviewPanel } from "./WorkspaceReviewPanels";
import type { GitChangeFile, GitChangesResult, GitFileDiffResult } from "../shared/protocol";

vi.mock("./WorkspaceFiles", () => ({
  formatWorkspaceRoot: (root: string) => root,
}));

vi.mock("./WorkspaceMonacoDiffEditor", () => ({
  WorkspaceMonacoDiffEditor: ({ path }: { path: string }) => (
    <div className="workspace-monaco-diff-editor" data-path={path} />
  ),
}));

let container: HTMLDivElement | null = null;
let root: Root | null = null;
const originalWuu = (window as unknown as { wuu?: unknown }).wuu;

function mount(element: JSX.Element): void {
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  act(() => {
    root!.render(element);
  });
}

async function flushReviewEffects(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

function installGitReviewStub(files: GitChangeFile[]): {
  listGitChanges: ReturnType<typeof vi.fn>;
  readGitFileDiff: ReturnType<typeof vi.fn>;
} {
  const first = files[0];
  const changes: GitChangesResult = {
    is_repo: true,
    root: "/repo",
    files,
  };
  const diff: GitFileDiffResult = {
    is_repo: true,
    path: first?.path ?? "",
    status: first?.status ?? "modified",
    additions: first?.additions ?? 0,
    deletions: first?.deletions ?? 0,
    binary: first?.binary,
    patch: [
      `diff --git a/${first?.path ?? "file.ts"} b/${first?.path ?? "file.ts"}`,
      "@@ -1 +1 @@",
      "-old value",
      "+new value that is deliberately long enough to exercise wrapping in the review pane",
    ].join("\n"),
    original_text: "old value\n",
    modified_text: "new value\n",
    truncated: false,
  };
  const listGitChanges = vi.fn().mockResolvedValue(changes);
  const readGitFileDiff = vi.fn().mockResolvedValue(diff);
  Object.defineProperty(window, "wuu", {
    configurable: true,
    value: {
      listGitChanges,
      readGitFileDiff,
    },
  });
  return { listGitChanges, readGitFileDiff };
}

function restoreWuu(): void {
  if (originalWuu === undefined) {
    delete (window as unknown as { wuu?: unknown }).wuu;
    return;
  }
  Object.defineProperty(window, "wuu", {
    configurable: true,
    value: originalWuu,
  });
}

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  root = null;
  container?.remove();
  container = null;
  restoreWuu();
});

describe("WorkspaceReviewPanel", () => {
  it("uses the full review width for a single changed file", async () => {
    const api = installGitReviewStub([
      {
        path: "desktop/src/renderer/styles/sidebar.css",
        status: "modified",
        additions: 4,
        deletions: 5,
      },
    ]);

    mount(
      <WorkspaceReviewPanel
        gitStatus={{ is_repo: true, branch: "main", dirty_count: 1 }}
        workspaceRoot="/repo/worktree"
      />,
    );
    await flushReviewEffects();

    expect(api.listGitChanges).toHaveBeenCalledWith("/repo/worktree");
    expect(api.readGitFileDiff).toHaveBeenCalledWith(
      "desktop/src/renderer/styles/sidebar.css",
      "/repo/worktree",
    );

    const panel = container?.querySelector<HTMLElement>(".workspace-review-panel");
    expect(panel?.classList.contains("single-file")).toBe(true);
    expect(container?.querySelector(".workspace-review-diff-panel")).toBeTruthy();
    expect(container?.querySelector(".workspace-monaco-diff-editor")).toBeTruthy();
    expect(container?.querySelector(".workspace-review-tree-pane")).toBeNull();
    expect(container?.querySelector(".workspace-review-resizer")).toBeNull();
  });
});
