import { describe, expect, it } from "vitest";
import {
  conversationSearchStatusText,
  conversationSearchVisibleSnippet,
} from "./ConversationSearchDisplay";

describe("conversationSearchStatusText", () => {
  it("prioritizes the loading state", () => {
    expect(
      conversationSearchStatusText({
        loading: true,
        query: "",
        resultCount: 2,
      }),
    ).toBe("正在搜索");
  });

  it("shows result count while searching", () => {
    expect(
      conversationSearchStatusText({
        loading: false,
        query: "permission",
        resultCount: 2,
      }),
    ).toBe("2 个结果");
  });

  it("shows conversation count in the recent list", () => {
    expect(
      conversationSearchStatusText({
        loading: false,
        query: "",
        resultCount: 2,
      }),
    ).toBe("2 个会话");
  });
});

describe("conversationSearchVisibleSnippet", () => {
  it("hides snippets in the recent conversation list", () => {
    expect(
      conversationSearchVisibleSnippet({
        query: "",
        snippet: "Optimize permission menu",
        title: "Optimize permission menu",
      }),
    ).toBe("");
  });

  it("hides a search snippet when it repeats the title", () => {
    expect(
      conversationSearchVisibleSnippet({
        query: "permission",
        snippet: "  Optimize   permission menu  ",
        title: "optimize permission menu",
      }),
    ).toBe("");
  });

  it("shows a search snippet when it adds context beyond the title", () => {
    expect(
      conversationSearchVisibleSnippet({
        query: "permission",
        snippet: "Menu should stay vertical, with one compact row per mode.",
        title: "Optimize permission menu",
      }),
    ).toBe("Menu should stay vertical, with one compact row per mode.");
  });

  it("caps a long search snippet at the source so the right pane stays compact", () => {
    // A 1k-character snippet simulates the leading window the search
    // backend returns for an agent's multi-section markdown reply —
    // far longer than the right pane can show. The cap should cut at
    // the last whitespace inside the limit and append an ellipsis.
    const longSnippet = "word ".repeat(200);
    const result = conversationSearchVisibleSnippet({
      query: "permission",
      snippet: longSnippet,
      title: "Different title",
    });
    expect(result.endsWith("…")).toBe(true);
    expect(result.length).toBeLessThanOrEqual(280);
  });

  it("hard-slices a long snippet when there is no whitespace to break on", () => {
    // A single 500-char token (think a base64 blob or a URL with no
    // separators) cannot be split at a word boundary, so the cap
    // falls back to a hard slice at the limit.
    const longSnippet = "a".repeat(500);
    const result = conversationSearchVisibleSnippet({
      query: "permission",
      snippet: longSnippet,
      title: "Different title",
    });
    expect(result.endsWith("…")).toBe(true);
    expect(result.length).toBeLessThanOrEqual(281);
  });
});
