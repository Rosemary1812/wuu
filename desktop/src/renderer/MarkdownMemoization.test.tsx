import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { RichContent } from "./RichContent";
import { StreamingMarkdown } from "./StreamingMarkdown";
import { streamTextKey, streamTextStore } from "./StreamText";

const markdownRender = vi.hoisted(() => ({ count: 0 }));

// Mock the deepest renderer so the test can count how many times
// MarkdownContent actually re-renders. With Vercel's Streamdown in
// place, react-markdown is no longer imported; we mock streamdown
// instead. The plugin packages are stubbed so jsdom does not try to
// load Shiki or Mermaid language workers, which it cannot.
vi.mock("streamdown", async () => {
  const React = await import("react");
  return {
    Streamdown: ({ children }: { children?: import("react").ReactNode }) => {
      markdownRender.count += 1;
      return React.createElement(
        "div",
        { "data-mock-streamdown": "true" },
        children
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

const longMarkdown = Array.from(
  { length: 80 },
  (_, index) => `## Section ${index + 1}

This paragraph includes **bold text**, \`inline code\`, and enough prose to
represent a long completed conversation item.

- First item
- Second item

`
).join("");

let container: HTMLDivElement;
let root: Root | null = null;

beforeEach(() => {
  markdownRender.count = 0;
  container = document.createElement("div");
  document.body.appendChild(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  root = null;
  container.remove();
  streamTextStore.clearItem("turn", "memo");
});

function render(element: JSX.Element): void {
  act(() => {
    if (!root) {
      root = createRoot(container);
    }
    root.render(element);
  });
}

describe("Markdown memoization", () => {
  it("does not re-render user-message markdown when only parent draft state changes", () => {
    function Harness({ draft }: { draft: string }): JSX.Element {
      return (
        <>
          <textarea readOnly value={draft} />
          <RichContent text={longMarkdown} cwd="/tmp/project" />
        </>
      );
    }

    render(<Harness draft="" />);
    expect(markdownRender.count).toBe(1);

    render(<Harness draft="typing in the composer" />);
    expect(markdownRender.count).toBe(1);
  });

  it("does not re-render settled assistant markdown when only parent draft state changes", async () => {
    const key = streamTextKey("turn", "memo", "text");

    function Harness({ draft }: { draft: string }): JSX.Element {
      return (
        <>
          <textarea readOnly value={draft} />
          <StreamingMarkdown
            streamKey={key}
            initialText={longMarkdown}
            isLive={false}
            phase="final_answer"
          />
        </>
      );
    }

    render(<Harness draft="" />);

    await act(async () => {
      await new Promise((resolve) => window.setTimeout(resolve, 240));
    });

    const countAfterCursorSettles = markdownRender.count;
    expect(countAfterCursorSettles).toBeGreaterThan(0);

    render(<Harness draft="typing in the composer" />);
    expect(markdownRender.count).toBe(countAfterCursorSettles);
  });
});
