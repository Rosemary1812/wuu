import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ThreadItem, Turn } from "../shared/protocol";
import { streamTextKey, streamTextStore } from "./StreamText";
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

function makeParticipantMessage(): ThreadItem {
  return {
    id: "participant-1",
    type: "participant_message",
    status: "completed",
    text: "Done from the specialist.",
    agent_id: "agent-42",
    post_kind: "result",
    participant: {
      id: "agent-42",
      name: "Reviewer",
      kind: "agent",
    },
  };
}

function render({
  item,
  turnStatus,
  actionableAgentMessageID,
  latestAgentMessageID,
  streaming,
  onOpenAgent,
  onOpenSubthread,
}: {
  item: ThreadItem;
  turnStatus: Turn["status"];
  actionableAgentMessageID?: string;
  latestAgentMessageID?: string;
  streaming: boolean;
  onOpenAgent?: (agentID: string) => void;
  onOpenSubthread?: (item: ThreadItem) => void;
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
        onOpenAgent={onOpenAgent}
        onOpenSubthread={onOpenSubthread}
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

  it("reserves no action bar while streaming and mounts the persistent bar on completion", () => {
    render({
      item: makeFinalAnswer("in_progress"),
      turnStatus: "in_progress",
      streaming: true,
    });

    // Streaming answers render no bar at all — the old invisible
    // placeholder reserved 32px of dead space under live text.
    expect(container?.querySelector(".agent-message-actions")).toBeNull();

    render({
      item: makeFinalAnswer("completed"),
      turnStatus: "completed",
      actionableAgentMessageID: "final-1",
      latestAgentMessageID: "final-1",
      streaming: false,
    });

    const visibleActions = actionBar();
    expect(visibleActions.getAttribute("aria-label")).toBe("助手消息操作");
    expect(visibleActions.querySelectorAll("button")).toHaveLength(4);
    const block = container?.querySelector(".agent-block");
    expect(block?.classList.contains("agent-actions-persistent")).toBe(true);
    expect(block?.classList.contains("agent-actions-overlay")).toBe(false);
  });

  it("renders historical answers with a hover overlay bar instead of an in-flow slot", () => {
    render({
      item: makeFinalAnswer("completed"),
      turnStatus: "completed",
      actionableAgentMessageID: "final-1",
      latestAgentMessageID: "final-99",
      streaming: false,
    });

    actionBar();
    const block = container?.querySelector(".agent-block");
    expect(block?.classList.contains("agent-actions-overlay")).toBe(true);
    expect(block?.classList.contains("agent-actions-persistent")).toBe(false);
  });

  it("releases completed stream text after the settled view keeps the final answer", async () => {
    const key = streamTextKey("turn-1", "final-1", "text");
    streamTextStore.set(key, "Final answer text.");

    render({
      item: makeFinalAnswer("completed"),
      turnStatus: "completed",
      actionableAgentMessageID: "final-1",
      latestAgentMessageID: "final-1",
      streaming: false,
    });

    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 50));
    });

    expect(container?.textContent).toContain("Final answer text.");
    expect(streamTextStore.has(key)).toBe(false);
  });

  it("opens the source agent from a participant result card", () => {
    const onOpenAgent = vi.fn();

    render({
      item: makeParticipantMessage(),
      turnStatus: "completed",
      streaming: false,
      onOpenAgent,
    });

    const openLink = container?.querySelector<HTMLAnchorElement>(
      'a[aria-label="查看完整过程"]',
    );
    expect(openLink).not.toBeNull();
    expect(openLink?.textContent).toBe("查看完整过程");

    act(() => {
      openLink?.click();
    });

    expect(onOpenAgent).toHaveBeenCalledWith("agent-42");
  });

  it("renders a subagent completion handoff with the shared system event divider", () => {
    render({
      item: {
        id: "handoff-1",
        type: "user_message",
        text: JSON.stringify({
          content: `<subagent_notification>\n${JSON.stringify({
            status: { status: "completed" },
          })}\n</subagent_notification>`,
          trigger_turn: true,
        }),
      },
      turnStatus: "completed",
      streaming: false,
    });

    const divider = container?.querySelector(".agent-handoff-divider");
    expect(divider?.classList.contains("turn-event-notice")).toBe(true);
    expect(divider?.querySelector(".turn-event-title")?.textContent).toBe(
      "subagent 完成了任务",
    );
  });

  it("renders a task card and opens its subthread", () => {
    const onOpenSubthread = vi.fn();
    const item: ThreadItem = {
      id: "task-card-agent-1",
      type: "task_card",
      status: "completed",
      task: {
        id: "agent-1",
        name: "review auth flow",
        role: "reviewer",
        status: "running",
        agent_id: "agent-1",
        subthread_id: "cth-review",
        reply_count: 2,
        participant: {
          id: "prt-reviewer",
          name: "Reviewer",
          kind: "ephemeral",
          role: "reviewer",
        },
      },
    };

    render({
      item,
      turnStatus: "completed",
      streaming: false,
      onOpenSubthread,
    });

    expect(container?.querySelector(".task-card")?.textContent).toContain(
      "review auth flow",
    );
    expect(container?.querySelector(".task-card")?.textContent).toContain(
      "2 条回复",
    );
    const button = container?.querySelector<HTMLButtonElement>(
      'button[aria-label="查看过程"]',
    );
    expect(button).not.toBeNull();

    act(() => {
      button?.click();
    });

    expect(onOpenSubthread).toHaveBeenCalledWith(item);
  });
});
