/**
 * Tests for StreamingMarkdown.
 *
 * Contract: render the markdown progressively; during streaming, the
 * surface contains the partial text and a visible caret (Streamdown's
 * caret, owned by the renderer, not by us); after settling, the full
 * text is rendered with no caret remaining.
 */
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { StreamingMarkdown, splitIntoStableBlocks } from "./StreamingMarkdown";
import { streamTextKey, streamTextStore } from "./StreamText";

// Mock the deepest renderer so jsdom does not try to load the real
// Shiki or Mermaid workers. The mock also lets the caret test assert
// against a known stable class instead of Streamdown's internal one.
// The wrapped `<p class="rich-paragraph">` mirrors the real
// component override in `RichContent.tsx`, so caret-positioning
// assertions have a meaningful ancestor to walk up to.
vi.mock("streamdown", async () => {
  const React = await import("react");
  return {
    Streamdown: ({
      children,
      isAnimating
    }: {
      children?: import("react").ReactNode;
      isAnimating?: boolean;
    }) => {
      // Mirror Streamdown's caret: a SPAN rendered inside the parsed
      // text when isAnimating is true, omitted otherwise. The caret
      // sits inside the paragraph wrapper so the streaming test can
      // walk up the DOM and find the rich-paragraph ancestor.
      const caret = isAnimating
        ? React.createElement("span", {
            "data-mock-caret": "true",
            className: "streamdown-caret-mock",
            "aria-hidden": "true"
          })
        : null;
      return React.createElement(
        "div",
        { "data-mock-streamdown": "true" },
        React.createElement(
          "p",
          { className: "rich-paragraph" },
          children,
          caret
        )
      );
    }
  };
});

vi.mock("@streamdown/code", () => ({
  createCodePlugin: () => ({
    name: "shiki",
    type: "code-highlighter",
    getSupportedLanguages: () => [],
    getThemes: () => ["github-light", "github-dark"],
    highlight: () => null,
    supportsLanguage: () => false
  })
}));

vi.mock("@streamdown/mermaid", () => ({
  createMermaidPlugin: () => ({
    name: "mermaid",
    type: "diagram",
    language: "mermaid",
    getMermaid: () => ({
      initialize: () => undefined,
      render: async () => ({ svg: "" })
    })
  })
}));

// jsdom doesn't implement layout. Stub getBoundingClientRect so React
// doesn't crash on layout queries.
beforeAll(() => {
  Element.prototype.getBoundingClientRect = function (): DOMRect {
    return {
      x: 0, y: 0, top: 0, left: 0, right: 0, bottom: 0,
      width: 0, height: 0,
      toJSON() { return this; }
    } as DOMRect;
  };
});

let container: HTMLDivElement | null = null;
let root: Root | null = null;

function mount(props: Parameters<typeof StreamingMarkdown>[0]): void {
  if (container) unmount();
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  act(() => {
    root!.render(<StreamingMarkdown {...props} />);
  });
}

function unmount(): void {
  if (root) {
    act(() => {
      root!.unmount();
    });
    root = null;
  }
  if (container) {
    container.remove();
    container = null;
  }
}

afterEach(() => {
  unmount();
  for (const itemID of ["s1", "s2", "s3", "s4", "s5", "s6", "s7", "s8"]) {
    streamTextStore.clearItem("turn", itemID);
  }
});

describe("StreamingMarkdown", () => {
  it("renders the visible text during streaming", async () => {
    const key = streamTextKey("turn", "s1", "text");
    streamTextStore.seed(key, "");
    mount({ streamKey: key, initialText: "", isLive: true, phase: "final_answer" });

    await act(async () => {
      streamTextStore.append(key, "**hi**");
      // Wait for full reveal: ~3 frames at 2 chars/frame in jsdom.
      await new Promise((resolve) => setTimeout(resolve, 300));
    });

    const surface = document.querySelector(".streaming-markdown") as HTMLElement;
    expect(surface).toBeTruthy();
    // The streaming layer hands the partial text to the renderer. The
    // renderer is mocked here; its parsing behavior is Streamdown's
    // contract, not ours, so the assertion is the raw markdown text
    // reaches the surface, not a parsed <strong>.
    expect(surface.textContent).toContain("**hi**");
  });

  it("shows a caret during streaming", async () => {
    const key = streamTextKey("turn", "s2", "text");
    streamTextStore.seed(key, "Hello world");
    mount({ streamKey: key, initialText: "Hello", isLive: true, phase: "final_answer" });

    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 50));
    });

    const surface = document.querySelector(".streaming-markdown") as HTMLElement;
    const caret = surface.querySelector("[data-mock-caret]") as HTMLElement | null;
    expect(caret).toBeTruthy();
    expect(caret?.tagName).toBe("SPAN");
    // The caret should sit inside the rendered paragraph.
    expect(caret?.closest(".rich-paragraph")).toBeTruthy();
  });

  it("uses the same live caret treatment for commentary text", async () => {
    const key = streamTextKey("turn", "s8", "text");
    streamTextStore.seed(key, "Working through it");
    mount({ streamKey: key, initialText: "Working", isLive: true, phase: "commentary" });

    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 80));
    });

    const surface = document.querySelector(".streaming-markdown") as HTMLElement;
    expect(surface.classList.contains("streaming-commentary-live")).toBe(false);
    expect(surface.querySelector("[data-mock-caret]")).toBeTruthy();
  });

  it("does not use a clip-path mask (no .streaming-cover)", async () => {
    const key = streamTextKey("turn", "s3", "text");
    streamTextStore.seed(key, "Hello world");
    mount({ streamKey: key, initialText: "Hello", isLive: true, phase: "final_answer" });

    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 80));
    });

    const surface = document.querySelector(".streaming-markdown") as HTMLElement;
    expect(surface.querySelector(".streaming-cover")).toBeNull();
  });

  it("renders the full text immediately when not live", () => {
    const key = streamTextKey("turn", "s4", "text");
    streamTextStore.seed(key, "Hello world");
    mount({ streamKey: key, initialText: "Hello world", isLive: false, phase: "final_answer" });

    const surface = document.querySelector(".streaming-markdown") as HTMLElement;
    expect(surface.textContent).toContain("Hello world");
  });

  it("drops the caret once isLive flips to false", async () => {
    const key = streamTextKey("turn", "s5", "text");
    streamTextStore.seed(key, "Hello world");
    mount({ streamKey: key, initialText: "Hello world", isLive: false, phase: "final_answer" });

    // isLive=false immediately snaps visible to the full text and
    // Streamdown drops its caret. The settled DOM is what the user
    // sees after the streaming surface has finished.
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 50));
    });

    const surface = document.querySelector(".streaming-markdown") as HTMLElement;
    expect(surface.textContent).toContain("Hello world");
    expect(surface.getAttribute("data-stream-state")).toBe("settled");
    expect(surface.querySelector("[data-mock-caret]")).toBeNull();
  });

  it("notifies once when isLive flips off and the cursor is caught up", async () => {
    const key = streamTextKey("turn", "s6", "text");
    streamTextStore.set(key, "Done");
    let settledCount = 0;
    mount({
      streamKey: key,
      initialText: "",
      isLive: false,
      phase: "final_answer",
      onSettled: () => {
        settledCount += 1;
      }
    });

    // isLive=false immediately calls syncImmediate + trySettle, so the
    // settled callback fires synchronously in the mount's commit pass.
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 50));
    });

    expect(settledCount).toBe(1);
  });

  it("renders streamed prose inside the surface", async () => {
    const key = streamTextKey("turn", "s7", "text");
    streamTextStore.seed(key, "- first item");
    mount({ streamKey: key, initialText: "", isLive: true, phase: "final_answer" });

    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 300));
    });

    const surface = document.querySelector(".streaming-markdown") as HTMLElement;
    // Streamdown owns list parsing; the streaming layer's job is to
    // hand the source text over. We assert the raw text reaches the
    // surface, not that it is wrapped in an <li>.
    expect(surface.textContent).toContain("- first item");
  });
});

describe("splitIntoStableBlocks", () => {
  it("returns the whole text as tail when there are no blank lines", () => {
    const result = splitIntoStableBlocks("a single paragraph still typing");
    expect(result.blocks).toEqual([]);
    expect(result.tail).toBe("a single paragraph still typing");
  });

  it("splits each `\\n\\n`-separated paragraph into its own block", () => {
    const text = "first paragraph\n\nsecond paragraph\n\nthird in progress";
    const result = splitIntoStableBlocks(text);
    expect(result.blocks).toEqual([
      "first paragraph\n\n",
      "second paragraph\n\n"
    ]);
    expect(result.tail).toBe("third in progress");
  });

  it("keeps an unclosed fenced code block in the tail", () => {
    // A `\n\n` inside an open ``` fence must NOT be a boundary —
    // the block isn't yet stable.
    const text =
      "intro paragraph\n\n```ts\nconst a = 1;\n\nconst b = 2;\nstill typing";
    const result = splitIntoStableBlocks(text);
    expect(result.blocks).toEqual(["intro paragraph\n\n"]);
    expect(result.tail).toBe(
      "```ts\nconst a = 1;\n\nconst b = 2;\nstill typing"
    );
  });

  it("treats a closed fenced code block as a single stable block", () => {
    const text =
      "intro\n\n```ts\nconst a = 1;\n\nconst b = 2;\n```\n\nafter";
    const result = splitIntoStableBlocks(text);
    expect(result.blocks).toEqual([
      "intro\n\n",
      "```ts\nconst a = 1;\n\nconst b = 2;\n```\n\n"
    ]);
    expect(result.tail).toBe("after");
  });

  it("ignores backticks that aren't at the start of a line", () => {
    // A ``` mid-line is part of inline content (or a heading), not a
    // fence opener.
    const text = "use the ```triple backtick``` syntax\n\ntail";
    const result = splitIntoStableBlocks(text);
    expect(result.blocks).toEqual(["use the ```triple backtick``` syntax\n\n"]);
    expect(result.tail).toBe("tail");
  });

  it("preserves the concatenation invariant: blocks + tail === text", () => {
    const text =
      "# heading\n\nparagraph one\n\n```ts\nfn()\n```\n\n- list item\n- another\n\ntail";
    const result = splitIntoStableBlocks(text);
    expect(result.blocks.join("") + result.tail).toBe(text);
  });
});
