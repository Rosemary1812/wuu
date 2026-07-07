// The chat whitelist must mirror desktop AppState.chatMessagesFromTurns:
// working-transcript items never become rows, handoff envelopes are dropped,
// adjacent envelope rows merge, and focus_meta wins regardless of item type.

import { describe, expect, it } from "vitest";
import type { ThreadItem, Turn } from "@wuu/protocol";

import {
  chatRowsFromTurns,
  envelopeRowLabel,
  focusRowLabel,
  isDeclineRow,
  taskStatusLabel,
} from "../src/lib/chatModel";
import { isAgentHandoffText } from "../src/lib/handoff";

function turn(id: string, items: Partial<ThreadItem>[]): Turn {
  return {
    id,
    items: items.map((item, index) => ({ id: `${id}-i${index}`, type: "user_message", ...item }) as ThreadItem),
    items_view: "full",
    status: "completed",
  };
}

const HANDOFF_TEXT = JSON.stringify({
  content: '<subagent_notification>{"agent_path":"a/b","status":{"status":"completed"}}</subagent_notification>',
  trigger_turn: true,
});

describe("chatRowsFromTurns", () => {
  it("renders only whitelist rows and drops the working transcript", () => {
    const rows = chatRowsFromTurns([
      turn("t1", [
        { type: "user_message", text: "帮我修个 bug" },
        { type: "reasoning", text: "thinking..." },
        { type: "tool_call", name: "bash" },
        { type: "agent_message", text: "streamed transcript" },
        { type: "participant_message", text: "修好了", participant: { id: "p1", name: "石头", kind: "resident" } },
      ]),
    ]);
    expect(rows.map((r) => r.kind)).toEqual(["user", "participant"]);
  });

  it("drops subagent handoff envelopes but keeps plain user messages", () => {
    expect(isAgentHandoffText(HANDOFF_TEXT)).toBe(true);
    expect(isAgentHandoffText("普通消息 {不是 JSON}")).toBe(false);
    expect(
      isAgentHandoffText('<subagent_notification>{"status":{}}</subagent_notification>'),
    ).toBe(true);
    const rows = chatRowsFromTurns([
      turn("t1", [
        { type: "user_message", text: HANDOFF_TEXT },
        { type: "user_message", text: "真人发言" },
      ]),
    ]);
    expect(rows).toHaveLength(1);
    expect(rows[0].kind).toBe("user");
  });

  it("merges adjacent envelope rows into one", () => {
    const meta = [{ source_thread_title: "发布群" }];
    const rows = chatRowsFromTurns([
      turn("t1", [
        { type: "user_message", text: "e1", envelope_meta: meta },
        { type: "user_message", text: "e2", envelope_meta: meta },
        { type: "user_message", text: "普通" },
        { type: "user_message", text: "e3", envelope_meta: meta },
      ]),
    ]);
    expect(rows.map((r) => r.kind)).toEqual(["envelope", "user", "envelope"]);
    const first = rows[0];
    if (first.kind !== "envelope") throw new Error("unreachable");
    expect(first.items).toHaveLength(2);
    expect(envelopeRowLabel(first.items)).toBe("收到来自「发布群」的 2 条消息");
  });

  it("focus_meta wins regardless of item type", () => {
    const rows = chatRowsFromTurns([
      turn("t1", [
        { type: "tool_call", focus_meta: { kind: "home" } },
        { type: "user_message", text: "x", focus_meta: { kind: "workspace", name: "wuu" } },
      ]),
    ]);
    expect(rows.map((r) => r.kind)).toEqual(["focus", "focus"]);
    const [a, b] = rows;
    if (a.kind !== "focus" || b.kind !== "focus") throw new Error("unreachable");
    expect(focusRowLabel(a.item)).toBe("⌂ 个人");
    expect(focusRowLabel(b.item)).toBe("⬒ wuu");
    expect(focusRowLabel({ focus_meta: { kind: "all" } } as ThreadItem)).toBe("⬒ 全部工作区");
  });

  it("keeps task cards and detects decline rows", () => {
    const rows = chatRowsFromTurns([
      turn("t1", [
        { type: "task_card", task: { id: "task1", status: "in_progress", name: "查日志" } },
        {
          type: "participant_message",
          post_kind: "decline",
          text: "已经有人在处理",
          participant: { id: "p2", name: "阿凛", kind: "resident" },
        },
      ]),
    ]);
    expect(rows.map((r) => r.kind)).toEqual(["task", "participant"]);
    const decline = rows[1];
    if (decline.kind !== "participant") throw new Error("unreachable");
    expect(isDeclineRow(decline.item)).toBe(true);
  });

  it("maps task status labels", () => {
    expect(taskStatusLabel("in_progress")).toBe("进行中");
    expect(taskStatusLabel("completed")).toBe("已完成");
    expect(taskStatusLabel("banana")).toBe("未知");
  });
});
