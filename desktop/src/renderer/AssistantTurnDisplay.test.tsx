import { createElement } from "react";
import { describe, expect, it } from "vitest";
import type { ThreadItem, Turn } from "../shared/protocol";
import { AGENT_NOTIFICATION_NAME } from "./AgentHandoff";
import { buildAssistantTurnDisplay } from "./AssistantTurnDisplay";

let idCounter = 0;

function nextID(prefix: string): string {
  idCounter += 1;
  return `${prefix}-${idCounter}`;
}

function makeTurn(status: Turn["status"], items: ThreadItem[]): Turn {
  return { id: "turn-1", items, items_view: "full", status };
}

function makeCommentary(text: string): ThreadItem {
  return {
    id: nextID("commentary"),
    type: "agent_message",
    status: "completed",
    phase: "commentary",
    role: "assistant",
    text,
  };
}

function makeReasoning(): ThreadItem {
  return {
    id: nextID("reasoning"),
    type: "reasoning",
    status: "completed",
    text: "thinking",
  };
}

function makeToolCall(): ThreadItem {
  return {
    id: nextID("tool"),
    type: "tool_call",
    status: "completed",
    name: "bash",
  };
}

function makeNotification(taskName: string, status = "completed"): ThreadItem {
  return {
    id: nextID("notification"),
    type: "user_message",
    name: AGENT_NOTIFICATION_NAME,
    status: "completed",
    text: JSON.stringify({
      author: `/root/${taskName}`,
      recipient: "/root",
      content: `<subagent_notification>\n${JSON.stringify({
        agent_path: `/root/${taskName}`,
        status: { task_name: taskName, status },
      })}\n</subagent_notification>`,
      trigger_turn: true,
    }),
  };
}

const stubRenderer = (): JSX.Element => createElement("div");

function build(turn: Turn) {
  const display = buildAssistantTurnDisplay(turn, undefined, stubRenderer);
  if (!display) {
    throw new Error("expected a display");
  }
  return display;
}

function chipLabels(display: ReturnType<typeof build>): string[] {
  return display.subagentChips.map((chip) => chip.label);
}

describe("buildAssistantTurnDisplay subagent chips", () => {
  it("collects every notification into the turn-level chip list", () => {
    const display = build(
      makeTurn("completed", [
        makeNotification("ok_agent_two"),
        makeCommentary("子代理 1: ok\n子代理 2: ok"),
        makeNotification("ok_agent_one"),
        makeCommentary("已确认"),
      ]),
    );
    expect(chipLabels(display)).toEqual([
      "ok_agent_two 完成了",
      "ok_agent_one 完成了",
    ]);
    // Chips never live on entries anymore — the fold header is the only
    // surface, so the message stream gains no extra rows.
    expect(display.entries.map((entry) => entry.kind)).toEqual([
      "commentary",
      "commentary",
    ]);
  });

  it("collects chips regardless of the surrounding entry kinds", () => {
    const display = build(
      makeTurn("completed", [makeToolCall(), makeNotification("lint")]),
    );
    expect(chipLabels(display)).toEqual(["lint 完成了"]);
    expect(display.entries.map((entry) => entry.kind)).toEqual(["activity"]);
  });

  it("keeps reasoning entries free of any chip attachment", () => {
    const display = build(
      makeTurn("completed", [makeReasoning(), makeNotification("lint")]),
    );
    expect(chipLabels(display)).toEqual(["lint 完成了"]);
    expect(display.entries).toHaveLength(1);
    expect(display.entries[0].item.type).toBe("reasoning");
  });

  it("returns a display for a notification-only turn so the header can host the chips", () => {
    const display = build(
      makeTurn("completed", [makeNotification("one"), makeNotification("two")]),
    );
    expect(display.entries).toHaveLength(0);
    expect(display.subagentChips).toHaveLength(2);
  });

  it("preserves failed outcomes as structured data", () => {
    const display = build(
      makeTurn("completed", [makeNotification("lint", "failed")]),
    );
    expect(display.subagentChips).toEqual([
      { label: "lint 失败了", outcome: "failed" },
    ]);
  });

  it("ignores user messages that are not agent handoffs", () => {
    const display = build(
      makeTurn("completed", [
        {
          id: nextID("user"),
          type: "user_message",
          status: "completed",
          text: "普通用户消息",
        },
        makeCommentary("回复"),
      ]),
    );
    expect(display.subagentChips).toEqual([]);
  });
});
