import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it } from "vitest";
import type { ThreadItem, Turn } from "../shared/protocol";
import { streamTextStore } from "./StreamText";
import { ThreadItemView } from "./ThreadItemView";

let container: HTMLDivElement | undefined;
let root: Root | undefined;

function makeFinalAnswer(status: ThreadItem["status"]): ThreadItem {
  return {
    id: "final-1",
    type: "agent_message",
    status,
    phase: "final_answer",
    text: "Final answer text.",
  };
}

function makeUserMessage(text: string, id = "user-1"): ThreadItem {
  return {
    id,
    type: "user_message",
    status: "completed",
    text,
  };
}

function render({
  item,
  turnStatus,
  actionableAgentMessageID,
  latestAgentMessageID,
  streaming,
}: {
  item: ThreadItem;
  turnStatus: Turn["status"];
  actionableAgentMessageID?: string;
  latestAgentMessageID?: string;
  streaming: boolean;
}): void {
  if (!container) {
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
  }
  act(() => {
    root!.render(
      <ThreadItemView
        turnID="turn-1"
        turnStatus={turnStatus}
        item={item}
        streaming={streaming}
        actionableAgentMessageID={actionableAgentMessageID}
        latestAgentMessageID={latestAgentMessageID}
        onStreamFrame={() => {}}
        onNoticeAction={() => {}}
      />,
    );
  });
}

function actionBar(): HTMLElement {
  const node = container?.querySelector<HTMLElement>(".agent-message-actions");
  if (!node) {
    throw new Error("expected agent action bar");
  }
  return node;
}

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
  streamTextStore.clearItem("turn-1", "final-1");
  root = undefined;
  container = undefined;
});

describe("ThreadItemView", () => {
  it("shows short user messages in full without a collapse control", () => {
    render({
      item: makeUserMessage("Short query."),
      turnStatus: "completed",
      streaming: false,
    });

    expect(container?.querySelector(".user-message-expand-toggle")).toBeNull();
    expect(container?.querySelector(".user-message-long-card")).toBeNull();
    expect(container?.textContent).toContain("Short query.");
  });

  it("collapses long wrapped user messages without explicit line breaks", () => {
    const longSingleParagraph = "pasted query ".repeat(150);

    render({
      item: makeUserMessage(longSingleParagraph),
      turnStatus: "completed",
      streaming: false,
    });

    const bubble = container?.querySelector<HTMLElement>(".user-message");
    const rawQuery = container?.querySelector<HTMLElement>(
      ".user-message-raw-query",
    );
    const toggle = container?.querySelector<HTMLButtonElement>(
      ".user-message-expand-toggle",
    );
    expect(bubble?.classList.contains("user-message-long-card")).toBe(true);
    expect(bubble?.contains(toggle ?? null)).toBe(true);
    expect(rawQuery?.textContent?.endsWith("...")).toBe(true);
    expect((rawQuery?.textContent?.length ?? 0)).toBeLessThan(
      longSingleParagraph.length,
    );
    expect(toggle?.textContent).toContain("显示更多");
  });

  it("shows long query previews as raw pasted text", () => {
    const markdownQuery = [
      "# Final plan",
      "",
      "## 0. Metadata",
      "",
      "- **Goal**: keep DefaultSystemPrompt() readable",
      "- **Reference**: default.md / gpt_5_1_prompt.md / prompt_with_apply_patch_instructions.md",
      "- **Scope**: internal/config/config.go / internal/runtime/session.go",
      "- **Step**: preserve the copied query shape",
      "- **Step**: avoid rendered markdown changing the preview",
      "- **Step**: keep a full rounded bubble",
      "- **Step**: wrap long paths and identifiers",
      "- **Step**: expose a clear show more control",
      "- **Step**: keep short queries unchanged",
      "- **Step**: keep copy and edit using the full original text",
      "- **Step**: each query starts collapsed",
      "- **Step**: expanded queries can collapse again",
    ].join("\n");

    render({
      item: makeUserMessage(markdownQuery),
      turnStatus: "completed",
      streaming: false,
    });

    const rawQuery = container?.querySelector<HTMLElement>(
      ".user-message-raw-query",
    );
    expect(rawQuery?.textContent).toContain("# Final plan");
    expect(rawQuery?.textContent).toContain("- **Goal**");
    expect(container?.querySelector(".rich-heading")).toBeNull();
  });

  it("collapses long user messages and toggles the full text", () => {
    const longText = Array.from(
      { length: 20 },
      (_, index) => `line ${index + 1}`,
    ).join("\n");

    render({
      item: makeUserMessage(longText),
      turnStatus: "completed",
      streaming: false,
    });

    const toggle = container?.querySelector<HTMLButtonElement>(
      ".user-message-expand-toggle",
    );
    const rawQuery = container?.querySelector<HTMLElement>(
      ".user-message-raw-query",
    );

    expect(rawQuery?.textContent).not.toContain("line 20");
    expect(rawQuery?.textContent?.endsWith("...")).toBe(true);
    expect(toggle?.textContent).toContain("显示更多");
    expect(toggle?.getAttribute("aria-expanded")).toBe("false");

    act(() => {
      toggle?.click();
    });

    expect(rawQuery?.textContent).toContain("line 20");
    expect(toggle?.textContent).toContain("收起");
    expect(toggle?.getAttribute("aria-expanded")).toBe("true");

    act(() => {
      toggle?.click();
    });

    expect(rawQuery?.textContent).not.toContain("line 20");
    expect(toggle?.textContent).toContain("显示更多");
  });

  it("defaults a different long user query back to collapsed", () => {
    const firstLongText = Array.from(
      { length: 20 },
      (_, index) => `first query line ${index + 1}`,
    ).join("\n");
    const secondLongText = Array.from(
      { length: 20 },
      (_, index) => `second query line ${index + 1}`,
    ).join("\n");

    render({
      item: makeUserMessage(firstLongText, "user-1"),
      turnStatus: "completed",
      streaming: false,
    });

    const firstToggle = container?.querySelector<HTMLButtonElement>(
      ".user-message-expand-toggle",
    );
    act(() => {
      firstToggle?.click();
    });

    expect(
      container?.querySelector(".user-message-raw-query")?.textContent,
    ).toContain("first query line 20");

    render({
      item: makeUserMessage(secondLongText, "user-2"),
      turnStatus: "completed",
      streaming: false,
    });

    const secondToggle = container?.querySelector<HTMLButtonElement>(
      ".user-message-expand-toggle",
    );
    expect(
      container?.querySelector(".user-message-raw-query")?.textContent,
    ).not.toContain("second query line 20");
    expect(secondToggle?.getAttribute("aria-expanded")).toBe("false");
  });

  it("keeps the final-answer action bar mounted while it becomes visible", () => {
    render({
      item: makeFinalAnswer("in_progress"),
      turnStatus: "in_progress",
      streaming: true,
    });

    const hiddenActions = actionBar();
    expect(hiddenActions.classList.contains("action-slot-placeholder")).toBe(
      true,
    );
    expect(hiddenActions.getAttribute("aria-hidden")).toBe("true");
    expect(hiddenActions.querySelectorAll("button")).toHaveLength(4);

    render({
      item: makeFinalAnswer("completed"),
      turnStatus: "completed",
      actionableAgentMessageID: "final-1",
      latestAgentMessageID: "final-1",
      streaming: false,
    });

    const visibleActions = actionBar();
    expect(visibleActions).toBe(hiddenActions);
    expect(visibleActions.classList.contains("action-slot-placeholder")).toBe(
      false,
    );
    expect(visibleActions.getAttribute("aria-label")).toBe("助手消息操作");
  });
});
