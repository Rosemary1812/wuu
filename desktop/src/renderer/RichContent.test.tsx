import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { WorkspaceFileReferenceResolveResult } from "../shared/protocol";
import { ImagePreviewProvider } from "./ImagePreview";
import { RichContent, __resetRichFileReferenceResolutionCacheForTests } from "./RichContent";

let container: HTMLDivElement;
let root: Root | null = null;
let writeTextMock: ReturnType<typeof vi.fn>;
let resolveWorkspaceFileReferenceMock: ReturnType<typeof vi.fn>;
let openExternalMock: ReturnType<typeof vi.fn>;

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
  openExternalMock = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(window, "wuu", {
    configurable: true,
    value: {
      openExternal: openExternalMock,
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

function renderWithImagePreview(element: JSX.Element): void {
  render(<ImagePreviewProvider>{element}</ImagePreviewProvider>);
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
  it("renders an explicit markdown-link file reference as a clickable workspace file link", () => {
    const openFile = vi.fn();
    render(
      <RichContent
        text={"See [README_zh.md (line 19)](README_zh.md) before editing."}
        cwd="/Users/zzzz/wuu"
        onOpenFile={openFile}
      />,
    );

    const link = container.querySelector(".rich-file-link") as HTMLButtonElement | null;
    expect(link).not.toBeNull();
    expect(link?.textContent).toContain("README_zh.md (line 19)");
    expect(link?.querySelector(".rich-link-icon")).not.toBeNull();

    act(() => {
      link?.click();
    });

    expect(openFile).toHaveBeenCalledWith("README_zh.md");
    expect(resolveWorkspaceFileReferenceMock).not.toHaveBeenCalled();
  });

  it("keeps explicit markdown-link file references as plain text when no file opener is wired", () => {
    render(
      <RichContent
        text={"See [README_zh.md (line 19)](README_zh.md) before editing."}
        cwd="/Users/zzzz/wuu"
      />,
    );

    expect(container.querySelector(".rich-file-link")).toBeNull();
    expect(container.textContent).toContain("README_zh.md (line 19)");
    expect(resolveWorkspaceFileReferenceMock).not.toHaveBeenCalled();
  });

  it("opens line-suffixed file links with a canonical selection fragment", () => {
    const openFile = vi.fn();
    render(
      <RichContent
        text={"See [the parser](src/parser.ts:12:4-18:9)."}
        cwd="/Users/zzzz/wuu"
        onOpenFile={openFile}
      />,
    );

    const link = container.querySelector<HTMLButtonElement>(".rich-file-link");
    expect(link?.textContent).toContain("the parser");
    act(() => link?.click());
    expect(openFile).toHaveBeenCalledWith("src/parser.ts#L12,4-L18,9");
  });

  it("keeps same-document anchors inside the rendered Markdown", () => {
    const openFile = vi.fn();
    const scrollIntoView = vi.fn();
    Object.defineProperty(Element.prototype, "scrollIntoView", {
      configurable: true,
      value: scrollIntoView,
    });
    render(
      <RichContent
        text={"[Jump](#install-notes)\n\n## Install Notes"}
        cwd="/Users/zzzz/wuu"
        onOpenFile={openFile}
      />,
    );

    const anchor = container.querySelector<HTMLAnchorElement>(".rich-anchor-link");
    const heading = container.querySelector<HTMLElement>("#install-notes");
    expect(anchor?.getAttribute("href")).toBe("#install-notes");
    expect(heading?.textContent).toContain("Install Notes");
    act(() => anchor?.click());
    expect(scrollIntoView).toHaveBeenCalledTimes(1);
    expect(openFile).not.toHaveBeenCalled();
  });

  it("does not auto-link bare workspace file paths in prose", () => {
    // Only `[label](path)` markdown links route to a file reference; bare
    // paths in prose stay plain text. A list like "see X, then Y, then Z"
    // should not acquire click affordances for items the model did not
    // explicitly mark as references.
    const openFile = vi.fn();
    render(
      <RichContent
        text={"The likely tool file is tool_search.go."}
        cwd="/Users/zzzz/wuu"
        onOpenFile={openFile}
      />,
    );

    expect(container.querySelector(".rich-file-link")).toBeNull();
    expect(resolveWorkspaceFileReferenceMock).not.toHaveBeenCalled();
  });

  it("renders a resolved bare image path as a preview without turning it into a file link", async () => {
    const reference = "clients/mobile/assets/icon.png";
    resolveWorkspaceFileReferenceMock.mockImplementation(async (candidate: string) =>
      candidate === reference
        ? resolvedFileReference(reference, reference)
        : { root: "/Users/zzzz/wuu", reference: candidate, status: "missing" },
    );

    renderWithImagePreview(
      <RichContent
        text={`已生成 ${reference}，可以先看这张。`}
        cwd="/Users/zzzz/wuu"
        onOpenFile={vi.fn()}
      />,
    );
    await settleFileReferenceResolution();

    const image = container.querySelector(".rich-auto-image-reference img.rich-image") as HTMLImageElement | null;
    expect(image).not.toBeNull();
    expect(image?.getAttribute("src")).toMatch(/^wuu-file:\/\/local\//);
    expect(container.querySelector(".rich-file-link")).toBeNull();
    expect(container.querySelector(".rich-image-caption")?.textContent).toBe(reference);
    expect(container.textContent).toContain(reference);
    expect(resolveWorkspaceFileReferenceMock).toHaveBeenCalledWith(reference, "/Users/zzzz/wuu");
  });

  it("keeps missing bare image paths as text only", async () => {
    renderWithImagePreview(
      <RichContent text="还没生成 missing-icon.png。" cwd="/Users/zzzz/wuu" />,
    );
    await settleFileReferenceResolution();

    expect(container.querySelector(".rich-auto-image-reference")).toBeNull();
    expect(container.textContent).toContain("missing-icon.png");
  });

  it("does not preview image paths inside inline code", async () => {
    renderWithImagePreview(
      <RichContent text="Keep `icon.png` literal here." cwd="/Users/zzzz/wuu" />,
    );
    await settleFileReferenceResolution();

    expect(container.querySelector(".rich-auto-image-reference")).toBeNull();
    expect(resolveWorkspaceFileReferenceMock).not.toHaveBeenCalled();
  });

  it("renders a qualified-path markdown-link as a clickable workspace file link", () => {
    const openFile = vi.fn();
    const reference = "internal/tools/tool_discovery.go";
    render(
      <RichContent
        text={`Open [${reference}](${reference}) instead.`}
        cwd="/Users/zzzz/wuu"
        onOpenFile={openFile}
      />,
    );

    const link = container.querySelector(".rich-file-link") as HTMLButtonElement | null;
    expect(link).not.toBeNull();
    expect(link?.textContent).toContain(reference);

    act(() => {
      link?.click();
    });

    expect(openFile).toHaveBeenCalledWith(reference);
  });

  it("carries a line marker in the visible label but resolves the click to the bare path", () => {
    const openFile = vi.fn();
    const target = "internal/appserver/model.go";
    render(
      <RichContent
        text={`See [model.go:789\u2013926](${target}) before editing.`}
        cwd="/Users/zzzz/wuu"
        onOpenFile={openFile}
      />,
    );

    const link = container.querySelector(".rich-file-link") as HTMLButtonElement | null;
    expect(link).not.toBeNull();
    expect(link?.textContent).toContain("model.go:789\u2013926");

    act(() => {
      link?.click();
    });

    expect(openFile).toHaveBeenCalledWith(target);
  });

  it("decorates web links with an inline site icon", () => {
    render(<RichContent text={"Open https://github.com/blueberrycongee/wuu"} />);

    const link = container.querySelector("a.rich-web-link") as HTMLAnchorElement | null;
    expect(link).not.toBeNull();
    expect(link?.getAttribute("href")).toBe("https://github.com/blueberrycongee/wuu");
    expect(link?.querySelector(".rich-link-icon")).not.toBeNull();
    expect(link?.hasAttribute("target")).toBe(false);

    act(() => link?.click());
    expect(openExternalMock).toHaveBeenCalledWith("https://github.com/blueberrycongee/wuu");
  });

  it("does not turn inline code file names into file links", () => {
    render(<RichContent text={"Keep `README_zh.md` literal here."} cwd="/Users/zzzz/wuu" />);

    expect(container.querySelector(".rich-file-link")).toBeNull();
    expect(container.querySelector("code")?.textContent).toBe("README_zh.md");
  });

  it("does not turn shell-error-format paths into file links", () => {
    // `program: path: error message` is the standard shell separator shape.
    // The path is sandwiched by `: ` (colon-space), which is not a
    // user-intentional file reference. Clicking the rendered link would
    // open a missing file and contradict the surrounding error.
    render(
      <RichContent
        text={"bash: scripts/desktop-dev.sh: No such file or directory"}
        cwd="/Users/zzzz/wuu"
      />
    );

    expect(container.querySelector(".rich-file-link")).toBeNull();
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

describe("RichContent raw HTML and heading levels", () => {
  it("escapes inline HTML by default so chat output cannot inject DOM", () => {
    // Chat messages come from the model — without rehype-raw, an LLM that
    // emits <h1>title</h1> in a fenced answer should render as literal
    // text, not as an actual <h1> the user might mistake for a UI heading.
    render(<RichContent text={"<h1>injected</h1>"} />);

    expect(container.querySelector("h1")).toBeNull();
    expect(container.textContent).toContain("<h1>injected</h1>");
  });

  it("renders inline HTML as DOM elements when allowRawHtml is true", () => {
    // The workspace file preview enables this so README badges, centered
    // <div>s, and other GitHub-rendered HTML show up the same way locally
    // as they do on github.com. The h1-h6 components still override the
    // tag to a styled <p>, but a <div> the user wrote passes through
    // untouched, which is what the README badge row relies on.
    render(<RichContent text={'<div class="badge">x</div>'} allowRawHtml />);

    const badge = container.querySelector(".rich-content .badge");
    expect(badge).not.toBeNull();
    expect(badge?.textContent).toBe("x");
  });

  it("keeps a workspace <div align=\"center\"> wrapper when allowRawHtml is on", () => {
    render(
      <RichContent
        text={'<div align="center"><a href="https://example.com">badge</a></div>'}
        allowRawHtml
      />,
    );

    const wrapper = container.querySelector('div[align="center"]');
    expect(wrapper).not.toBeNull();
    expect(wrapper?.querySelector("a")).not.toBeNull();
  });

  it("differentiates heading levels with rich-heading--hN modifier classes", () => {
    render(<RichContent text={"# alpha\n\n## beta\n\n### gamma"} />);

    expect(container.querySelector(".rich-heading.rich-heading--h1")?.textContent).toContain("alpha");
    expect(container.querySelector(".rich-heading.rich-heading--h2")?.textContent).toContain("beta");
    expect(container.querySelector(".rich-heading.rich-heading--h3")?.textContent).toContain("gamma");
  });
});
