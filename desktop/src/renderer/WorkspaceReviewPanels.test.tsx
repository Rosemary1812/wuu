import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { WorkspaceReviewPanel } from "./WorkspaceReviewPanels";
import type { GitChangeFile, GitChangesResult, GitFileDiffResult } from "../shared/protocol";

vi.mock("./WorkspaceFiles", () => ({
  formatWorkspaceRoot: (root: string) => root,
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

function installGitReviewStub(files: GitChangeFile[]): void {
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
    truncated: false,
  };
  Object.defineProperty(window, "wuu", {
    configurable: true,
    value: {
      listGitChanges: vi.fn().mockResolvedValue(changes),
      readGitFileDiff: vi.fn().mockResolvedValue(diff),
    },
  });
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
    installGitReviewStub([
      {
        path: "desktop/src/renderer/styles/sidebar.css",
        status: "modified",
        additions: 4,
        deletions: 5,
      },
    ]);

    mount(<WorkspaceReviewPanel gitStatus={{ is_repo: true, branch: "main", dirty_count: 1 }} />);
    await flushReviewEffects();

    const panel = container?.querySelector<HTMLElement>(".workspace-review-panel");
    expect(panel?.classList.contains("single-file")).toBe(true);
    expect(container?.querySelector(".workspace-review-diff-panel")).toBeTruthy();
    expect(container?.querySelector(".workspace-review-tree-pane")).toBeNull();
    expect(container?.querySelector(".workspace-review-resizer")).toBeNull();
  });
});
