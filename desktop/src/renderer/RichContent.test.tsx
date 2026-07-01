import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { WorkspaceFileReferenceResolveResult } from "../shared/protocol";
import { RichContent } from "./RichContent";

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

  it("does not turn unresolved bare filenames into file links", async () => {
    render(<RichContent text={"The likely tool file is tool_search.go."} cwd="/Users/zzzz/wuu" onOpenFile={vi.fn()} />);
    await settleFileReferenceResolution();

    expect(container.querySelector(".rich-file-link")).toBeNull();
    expect(container.textContent).toContain("tool_search.go");
    expect(resolveWorkspaceFileReferenceMock).toHaveBeenCalledWith("tool_search.go");
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
});
