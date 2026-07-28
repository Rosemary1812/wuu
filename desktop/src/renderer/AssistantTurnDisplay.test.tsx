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

function entriesWithChips(display: ReturnType<typeof build>) {
  return display.entries.filter(
    (entry) => entry.subagentChipsBefore?.length || entry.subagentChipsAfter?.length,
  );
}

describe("buildAssistantTurnDisplay subagent chips", () => {
  it("consolidates all notifications into one chip group at the first host", () => {
    const display = build(
      makeTurn("completed", [
        makeNotification("ok_agent_two"),
        makeCommentary("子代理 1: ok\n子代理 2: ok"),
        makeNotification("ok_agent_one"),
        makeCommentary("已确认"),
      ]),
    );
    const hosts = entriesWithChips(display);
    expect(hosts).toHaveLength(1);
    expect(hosts[0].kind).toBe("commentary");
    expect(hosts[0].subagentChipsBefore?.map((chip) => chip.label)).toEqual([
      "ok_agent_two 完成了",
      "ok_agent_one 完成了",
    ]);
  });

  it("anchors to the previous activity entry when a notification follows tools", () => {
    const display = build(
      makeTurn("completed", [makeToolCall(), makeNotification("lint")]),
    );
    const hosts = entriesWithChips(display);
    expect(hosts).toHaveLength(1);
    expect(hosts[0].kind).toBe("activity");
    expect(hosts[0].subagentChipsAfter?.map((chip) => chip.label)).toEqual([
      "lint 完成了",
    ]);
  });

  it("never lets a reasoning fold host chips; falls back to a bare chip row", () => {
    const display = build(
      makeTurn("completed", [makeReasoning(), makeNotification("lint")]),
    );
    const reasoning = display.entries[0];
    expect(reasoning.subagentChipsBefore).toBeUndefined();
    expect(reasoning.subagentChipsAfter).toBeUndefined();
    const last = display.entries.at(-1);
    expect(last?.kind).toBe("subagent_chips");
    expect(last?.subagentChipsAfter?.map((chip) => chip.label)).toEqual([
      "lint 完成了",
    ]);
  });

  it("renders a notification-only turn as a single bare chip row", () => {
    const display = build(
      makeTurn("completed", [makeNotification("one"), makeNotification("two")]),
    );
    expect(display.entries).toHaveLength(1);
    expect(display.entries[0].kind).toBe("subagent_chips");
    expect(display.entries[0].subagentChipsAfter).toHaveLength(2);
  });

  it("carries chips anchored after text to the text-run tail", () => {
    const display = build(
      makeTurn("completed", [
        makeCommentary("先说一句"),
        makeNotification("lint"),
        makeCommentary("继续说"),
      ]),
    );
    const [first, second] = display.entries;
    expect(first.subagentChipsAfter).toBeUndefined();
    expect(second.subagentChipsAfter?.map((chip) => chip.label)).toEqual([
      "lint 完成了",
    ]);
  });
});
