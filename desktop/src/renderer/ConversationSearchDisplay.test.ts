import { describe, expect, it } from "vitest";
import type { ThreadSearchResultItem } from "../shared/protocol";
import {
  conversationSearchResultSections,
  conversationSearchStatusText,
  conversationSearchVisibleSnippet,
} from "./ConversationSearchDisplay";

function searchResult(id: string, pinned = false): ThreadSearchResultItem {
  return {
    thread: {
      id,
      preview: id,
      model_provider: "openai",
      model: "gpt-5",
      cwd: "/tmp/wuu",
      status: "idle",
      pinned,
      created_at: "2026-01-01T00:00:00.000Z",
      updated_at: "2026-01-01T00:00:00.000Z",
      turns: [],
    },
  };
}

describe("conversationSearchResultSections", () => {
  it("uses one recent section for pinned and unpinned conversations", () => {
    const pinned = searchResult("pinned", true);
    const unpinned = searchResult("unpinned");

    expect(conversationSearchResultSections([pinned, unpinned], "")).toEqual([
      { title: "最近会话", results: [pinned, unpinned], startIndex: 0 },
    ]);
  });

  it("uses one result section when searching", () => {
    const result = searchResult("match");

    expect(conversationSearchResultSections([result], "permission")).toEqual([
      { title: "搜索结果", results: [result], startIndex: 0 },
    ]);
  });

  it("returns no sections for empty results", () => {
    expect(conversationSearchResultSections([], "")).toEqual([]);
    expect(conversationSearchResultSections([], "permission")).toEqual([]);
  });
});

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
