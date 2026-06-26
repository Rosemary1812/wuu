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

function renderUserMessage(text: string): void {
  render({
    item: {
      id: "user-1",
      type: "user_message",
      text,
    },
    turnStatus: "completed",
    streaming: false,
  });
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

  it("does not collapse a short user message and hides the expand toggle", () => {
    renderUserMessage("短 query,够短");

    const textWrapper = container?.querySelector(".user-message-text");
    expect(textWrapper).not.toBeNull();
    expect(textWrapper?.classList.contains("user-message-text-clamped")).toBe(
      false,
    );
    expect(container?.querySelector(".user-message-expand-toggle")).toBeNull();
  });

  it("collapses a long character-pasted user message and surfaces the toggle", () => {
    renderUserMessage("a".repeat(500));

    const textWrapper = container?.querySelector(".user-message-text");
    expect(textWrapper?.classList.contains("user-message-text-clamped")).toBe(
      true,
    );
    const toggle = container?.querySelector<HTMLButtonElement>(
      ".user-message-expand-toggle",
    );
    expect(toggle).not.toBeNull();
    expect(toggle?.getAttribute("aria-expanded")).toBe("false");
    expect(toggle?.textContent ?? "").toContain("展开全文");
  });

  it("collapses a long newline-pasted user message and surfaces the toggle", () => {
    renderUserMessage("\n".repeat(8));

    const textWrapper = container?.querySelector(".user-message-text");
    expect(textWrapper?.classList.contains("user-message-text-clamped")).toBe(
      true,
    );
    expect(container?.querySelector(".user-message-expand-toggle")).not.toBeNull();
  });

  it("toggles the user message between collapsed and expanded on click", () => {
    renderUserMessage("a".repeat(500));

    const toggle = container?.querySelector<HTMLButtonElement>(
      ".user-message-expand-toggle",
    );
    if (!toggle) throw new Error("expected expand toggle");
    expect(toggle.getAttribute("aria-expanded")).toBe("false");

    act(() => {
      toggle.click();
    });
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    expect(toggle.textContent ?? "").toContain("收起");
    let textWrapper = container?.querySelector(".user-message-text");
    expect(textWrapper?.classList.contains("user-message-text-clamped")).toBe(
      false,
    );

    act(() => {
      toggle.click();
    });
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    expect(toggle.textContent ?? "").toContain("展开全文");
    textWrapper = container?.querySelector(".user-message-text");
    expect(textWrapper?.classList.contains("user-message-text-clamped")).toBe(
      true,
    );
  });
});
