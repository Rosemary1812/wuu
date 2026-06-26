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
});
