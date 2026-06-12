import { describe, expect, it } from "vitest";
import { conversationSearchVisibleSnippet } from "./ConversationSearchDisplay";

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
