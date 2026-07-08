import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { WorkspaceFileReferenceResolveResult } from "../shared/protocol";
import { RichContent, __resetRichFileReferenceResolutionCacheForTests } from "./RichContent";

let container: HTMLDivElement;
let root: Root | null = null;
let writeTextMock: ReturnType<typeof vi.fn>;
let resolveWorkspaceFileReferenceMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  writeTextMock = vi.fn().mockResolvedValue(undefined);
  // jsdom does not implement the clipboard API; inject a mock for the
  // success-path tests.
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: { writeText: writeTextMock }
  });
  resolveWorkspaceFileReferenceMock = vi.fn(async (reference: string): Promise<WorkspaceFileReferenceResolveResult> => ({
    root: "/Users/zzzz/wuu",
    reference,
    status: "missing",
  }));
  Object.defineProperty(window, "wuu", {
    configurable: true,
    value: {
      resolveWorkspaceFileReference: resolveWorkspaceFileReferenceMock,
    },
  });
  container = document.createElement("div");
  document.body.appendChild(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  root = null;
  container.remove();
  delete (window as { wuu?: unknown }).wuu;
  __resetRichFileReferenceResolutionCacheForTests();
});

function render(element: JSX.Element): void {
  act(() => {
    if (!root) {
      root = createRoot(container);
    }
    root.render(element);
  });
}

async function settleFileReferenceResolution(): Promise<void> {
  for (let index = 0; index < 3; index += 1) {
    await act(async () => {
      await Promise.resolve();
    });
  }
}

function resolvedFileReference(
  reference: string,
  path: string,
): WorkspaceFileReferenceResolveResult {
  return {
    root: "/Users/zzzz/wuu",
    reference,
    status: "resolved",
    path,
    absolute_path: `/Users/zzzz/wuu/${path}`,
  };
}

describe("RichContent code block", () => {
  it("turns line-specific bare workspace file references into clickable inline links", async () => {
    const openFile = vi.fn();
    resolveWorkspaceFileReferenceMock.mockResolvedValueOnce(
      resolvedFileReference("README_zh.md (line 19)", "README_zh.md"),
    );
    render(
      <RichContent
        text={"See README_zh.md (line 19) before editing."}
        cwd="/Users/zzzz/wuu"
        onOpenFile={openFile}
      />,
    );
    await settleFileReferenceResolution();

    const link = container.querySelector(".rich-file-link") as HTMLButtonElement | null;
    expect(link).not.toBeNull();
    expect(link?.textContent).toContain("README_zh.md (line 19)");
    expect(link?.querySelector(".rich-link-icon")).not.toBeNull();

    await act(async () => {
      link?.click();
    });

    expect(resolveWorkspaceFileReferenceMock).toHaveBeenCalledWith("README_zh.md (line 19)");
    expect(openFile).toHaveBeenCalledWith("README_zh.md");
  });

  it("still renders a red link for unresolved bare filenames (visual consistency)", async () => {
    // Even when the IPC says the file does not exist, the link is still
    // rendered. A list of "改动文件" should not mix highlighted and plain
    // entries just because one of the files was deleted; the file viewer
    // surfaces its own "not found" feedback on click.
    const openFile = vi.fn();
    render(
      <RichContent
        text={"The likely tool file is tool_search.go."}
        cwd="/Users/zzzz/wuu"
        onOpenFile={openFile}
      />
    );
    await settleFileReferenceResolution();

    const link = container.querySelector("button.rich-file-link") as HTMLButtonElement | null;
    expect(link).not.toBeNull();
    expect(link?.textContent).toContain("tool_search.go");
    expect(resolveWorkspaceFileReferenceMock).toHaveBeenCalledWith("tool_search.go");

    // Click hands the display string back to the caller; the workspace
    // panel is responsible for telling the user the file is gone.
    await act(async () => {
      link?.click();
    });
    expect(openFile).toHaveBeenCalledWith("tool_search.go");
  });

  it("turns qualified workspace file paths into clickable inline links", async () => {
    const openFile = vi.fn();
    const reference = "internal/tools/tool_discovery.go";
    resolveWorkspaceFileReferenceMock.mockResolvedValueOnce(
      resolvedFileReference(reference, reference),
    );
    render(<RichContent text={`Open ${reference} instead.`} cwd="/Users/zzzz/wuu" onOpenFile={openFile} />);
    await settleFileReferenceResolution();

    const link = container.querySelector(".rich-file-link") as HTMLButtonElement | null;
    expect(link).not.toBeNull();
    expect(link?.textContent).toContain(reference);

    await act(async () => {
      link?.click();
    });

    expect(openFile).toHaveBeenCalledWith("internal/tools/tool_discovery.go");
  });

  it("keeps complete file line ranges inside the inline file link", async () => {
    const openFile = vi.fn();
    const reference = "internal/appserver/model.go:789\u2013926";
    resolveWorkspaceFileReferenceMock.mockResolvedValueOnce(
      resolvedFileReference(reference, "internal/appserver/model.go"),
    );
    render(<RichContent text={`See ${reference} before editing.`} cwd="/Users/zzzz/wuu" onOpenFile={openFile} />);
    await settleFileReferenceResolution();

    const link = container.querySelector(".rich-file-link") as HTMLButtonElement | null;
    expect(link).not.toBeNull();
    expect(link?.textContent).toContain(reference);

    await act(async () => {
      link?.click();
    });

    expect(openFile).toHaveBeenCalledWith("internal/appserver/model.go");
  });

  it("decorates web links with an inline site icon", () => {
    render(<RichContent text={"Open https://github.com/blueberrycongee/wuu"} />);

    const link = container.querySelector("a.rich-web-link") as HTMLAnchorElement | null;
    expect(link).not.toBeNull();
    expect(link?.getAttribute("href")).toBe("https://github.com/blueberrycongee/wuu");
    expect(link?.querySelector(".rich-link-icon")).not.toBeNull();
  });

  it("does not turn inline code file names into file links", () => {
    render(<RichContent text={"Keep `README_zh.md` literal here."} cwd="/Users/zzzz/wuu" />);

    expect(container.querySelector(".rich-file-link")).toBeNull();
    expect(container.querySelector("code")?.textContent).toBe("README_zh.md");
  });

  it("wraps fenced code in a header with the language label and a copy button", () => {
    render(<RichContent text={"```typescript\nconst x = 1;\n```"} />);

    const block = container.querySelector(".rich-code-block");
    expect(block).not.toBeNull();

    const language = block?.querySelector(".rich-code-language");
    expect(language?.textContent).toBe("typescript");

    const copyButton = block?.querySelector(".rich-code-copy");
    expect(copyButton).not.toBeNull();
    // The button should advertise itself as a code copy, not the
    // generic "复制消息" label that the message-level copy uses.
    expect(copyButton?.getAttribute("aria-label")).toBe("复制代码");
  });

  it("omits the language label when the fenced code has no language", () => {
    render(<RichContent text={"```\nnaked code\n```"} />);

    const block = container.querySelector(".rich-code-block");
    expect(block).not.toBeNull();
    expect(block?.querySelector(".rich-code-language")).toBeNull();
    expect(block?.querySelector(".rich-code-copy")).not.toBeNull();
  });

  it("clicking the copy button writes the code text to the clipboard", async () => {
    render(<RichContent text={"```js\nconsole.log('hi');\n```"} />);

    const copyButton = container.querySelector(".rich-code-copy") as HTMLButtonElement | null;
    expect(copyButton).not.toBeNull();

    await act(async () => {
      copyButton?.click();
    });

    expect(writeTextMock).toHaveBeenCalledTimes(1);
    expect(writeTextMock).toHaveBeenCalledWith("console.log('hi');");
  });

  it("the copy button stays clickable (pointer-events not 'none')", () => {
    render(<RichContent text={"```typescript\nconst x = 1;\n```"} />);

    const copyButton = container.querySelector(".rich-code-copy") as HTMLElement | null;
    expect(copyButton).not.toBeNull();
    // The base .message-copy-button class sets pointer-events: none so the
    // user-message copy button stays hidden until its parent is hovered.
    // .rich-code-copy sits on its own (no .user-message-block-with-actions
    // parent), so it must explicitly opt back in — otherwise real mouse
    // clicks pass through to the <pre> underneath and the button silently
    // does nothing. (Programmatic .click() bypasses pointer-events, which
    // is why the previous test did not catch this regression.)
    const style = window.getComputedStyle(copyButton as HTMLElement);
    expect(style.pointerEvents).not.toBe("none");
  });

  it("renders a link-shaped pending placeholder while the file reference IPC is in flight", async () => {
    // Block IPC resolution until we have asserted the pending state.
    let resolveIpc: (result: WorkspaceFileReferenceResolveResult) => void = () => undefined;
    resolveWorkspaceFileReferenceMock.mockImplementationOnce(
      (_reference: string) =>
        new Promise<WorkspaceFileReferenceResolveResult>((resolve) => {
          resolveIpc = resolve;
        })
    );

    render(
      <RichContent
        text={"Open pending-link-test.ts here."}
        cwd="/Users/zzzz/wuu"
        onOpenFile={vi.fn()}
      />
    );

    // First render: the link-shaped placeholder is shown, NOT plain text and
    // NOT a clickable <button> link. This is what kills the plain-text -> red
    // link flip when switching back to a previously visited session/tab.
    const pending = container.querySelector(".rich-file-link--pending");
    expect(pending).not.toBeNull();
    expect(pending?.textContent).toContain("pending-link-test.ts");
    expect(pending?.getAttribute("aria-busy")).toBe("true");
    expect(container.querySelector("button.rich-file-link")).toBeNull();
    expect(resolveWorkspaceFileReferenceMock).toHaveBeenCalledTimes(1);

    // Resolve the IPC and let React flush. The placeholder should be
    // replaced by the real clickable link.
    await act(async () => {
      resolveIpc(
        resolvedFileReference("pending-link-test.ts", "pending-link-test.ts")
      );
    });
    await settleFileReferenceResolution();

    expect(container.querySelector(".rich-file-link--pending")).toBeNull();
    const link = container.querySelector(
      "button.rich-file-link"
    ) as HTMLButtonElement | null;
    expect(link).not.toBeNull();
    expect(link?.textContent).toContain("pending-link-test.ts");
  });

  it("reuses the cached resolution when the same file reference is re-mounted", async () => {
    const openFile = vi.fn();
    resolveWorkspaceFileReferenceMock.mockResolvedValueOnce(
      resolvedFileReference(
        "internal/cache-test.ts",
        "internal/cache-test.ts"
      )
    );

    // First mount — should resolve the reference and populate the cache.
    render(
      <RichContent
        text={"Open internal/cache-test.ts now."}
        cwd="/Users/zzzz/wuu"
        onOpenFile={openFile}
      />
    );
    await settleFileReferenceResolution();
    expect(container.querySelector("button.rich-file-link")).not.toBeNull();
    expect(resolveWorkspaceFileReferenceMock).toHaveBeenCalledTimes(1);

    // Simulate switching away from the session: unmount and clear the DOM.
    act(() => {
      root?.unmount();
    });
    root = null;
    container.innerHTML = "";

    // Re-mount with a different surrounding text but the same file reference
    // and cwd. The link should render synchronously on the first render
    // (no plain text, no pending placeholder) and IPC should NOT be called
    // a second time, because the resolution is cached.
    render(
      <RichContent
        text={"Open internal/cache-test.ts again."}
        cwd="/Users/zzzz/wuu"
        onOpenFile={openFile}
      />
    );

    const link = container.querySelector(
      "button.rich-file-link"
    ) as HTMLButtonElement | null;
    expect(link).not.toBeNull();
    expect(container.querySelector(".rich-file-link--pending")).toBeNull();
    expect(link?.textContent).toContain("internal/cache-test.ts");
    expect(resolveWorkspaceFileReferenceMock).toHaveBeenCalledTimes(1);
  });

  it("replaces the pending placeholder with a red link even when the IPC reports the file does not exist", async () => {
    // Visual consistency: a list of files (e.g. "段二改了 → a.ts, b.ts")
    // should highlight every entry. The pending placeholder is only the
    // brief moment before the IPC roundtrip completes; once it does, the
    // link stays red even for files that no longer exist on disk.
    const openFile = vi.fn();
    let resolveIpc: (result: WorkspaceFileReferenceResolveResult) => void = () => undefined;
    resolveWorkspaceFileReferenceMock.mockImplementationOnce(
      (_reference: string) =>
        new Promise<WorkspaceFileReferenceResolveResult>((resolve) => {
          resolveIpc = resolve;
        })
    );

    render(
      <RichContent
        text={"Open never-resolves.ts here."}
        cwd="/Users/zzzz/wuu"
        onOpenFile={openFile}
      />
    );

    // While in flight, the pending placeholder is shown.
    expect(container.querySelector(".rich-file-link--pending")).not.toBeNull();

    // Resolve with a "missing" result.
    await act(async () => {
      resolveIpc({
        root: "/Users/zzzz/wuu",
        reference: "never-resolves.ts",
        status: "missing",
      });
    });
    await settleFileReferenceResolution();

    // The pending placeholder is gone, but the red link stays. Clicking
    // hands the display string back to the caller, so the workspace
    // panel can show "file not found" feedback on its own.
    expect(container.querySelector(".rich-file-link--pending")).toBeNull();
    const link = container.querySelector(
      "button.rich-file-link"
    ) as HTMLButtonElement | null;
    expect(link).not.toBeNull();
    expect(link?.textContent).toContain("never-resolves.ts");

    await act(async () => {
      link?.click();
    });
    expect(openFile).toHaveBeenCalledWith("never-resolves.ts");
  });
});
