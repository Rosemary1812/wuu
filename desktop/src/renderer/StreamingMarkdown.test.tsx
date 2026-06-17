/**
 * Tests for StreamingMarkdown.
 *
 * Contract: render the markdown progressively, show a 1px cursor at
 * the end during streaming, fade the cursor out 200ms after settle,
 * then remove it from the DOM.
 */
import { afterEach, beforeAll, describe, expect, it } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { StreamingMarkdown, splitIntoStableBlocks } from "./StreamingMarkdown";
import { streamTextKey, streamTextStore } from "./StreamText";

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
  for (const itemID of ["s1", "s2", "s3", "s4", "s5", "s6", "s7"]) {
    streamTextStore.clearItem("turn", itemID);
  }
});

describe("StreamingMarkdown", () => {
  it("renders the visible text as markdown during streaming", async () => {
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
    const strong = surface.querySelector("strong");
    expect(strong?.textContent).toBe("hi");
  });

  it("shows a 1px cursor span during streaming", async () => {
    const key = streamTextKey("turn", "s2", "text");
    streamTextStore.seed(key, "Hello world");
    mount({ streamKey: key, initialText: "Hello", isLive: true, phase: "final_answer" });

    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 50));
    });

    const surface = document.querySelector(".streaming-markdown") as HTMLElement;
    const cursor = surface.querySelector(".stream-cursor") as HTMLElement | null;
    expect(cursor).toBeTruthy();
    expect(cursor?.tagName).toBe("SPAN");
    expect(cursor?.closest(".rich-paragraph")).toBeTruthy();
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

  it("removes the cursor from the DOM after the fade-out completes", async () => {
    const key = streamTextKey("turn", "s5", "text");
    streamTextStore.seed(key, "Hello world");
    mount({ streamKey: key, initialText: "Hello world", isLive: false, phase: "final_answer" });

    // Wait for the 200ms fade-out to complete.
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 300));
    });

    const surface = document.querySelector(".streaming-markdown") as HTMLElement;
    expect(surface.querySelector(".stream-cursor")).toBeNull();
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

  it("keeps the cursor inside a streaming list item", async () => {
    const key = streamTextKey("turn", "s7", "text");
    streamTextStore.seed(key, "- first item");
    mount({ streamKey: key, initialText: "", isLive: true, phase: "final_answer" });

    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 300));
    });

    const surface = document.querySelector(".streaming-markdown") as HTMLElement;
    const cursor = surface.querySelector(".stream-cursor") as HTMLElement | null;
    expect(surface.textContent).not.toContain("\uE000");
    expect(cursor?.closest("li")).toBeTruthy();
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
