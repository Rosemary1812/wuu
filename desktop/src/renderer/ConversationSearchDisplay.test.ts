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
});
