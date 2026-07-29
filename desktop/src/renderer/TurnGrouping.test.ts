import { describe, expect, it } from "vitest";
import type { ThreadItem, Turn } from "../shared/protocol";
import {
  groupConversationTurns,
  isAgentWakeTurn,
  turnHasRealUserMessage,
  turnHasSpawnAgentCall,
} from "./TurnGrouping";

function userItem(id: string): ThreadItem {
  return { id, type: "user_message", text: `message ${id}` };
}

function wakeItem(id: string): ThreadItem {
  return {
    id,
    type: "user_message",
    name: "wuu_agent_notification",
    text: "<subagent_notification>{}</subagent_notification>",
  };
}

function spawnItem(id: string): ThreadItem {
  return {
    id,
    type: "collab_agent_tool_call",
    name: "spawn_agent",
    status: "completed",
  };
}

function answerItem(id: string): ThreadItem {
  return {
    id,
    type: "agent_message",
    status: "completed",
    phase: "final_answer",
    text: `answer ${id}`,
  };
}

function makeTurn(id: string, items: ThreadItem[], status: Turn["status"] = "completed"): Turn {
  return { id, items_view: "full", status, items };
}

function groupIDs(turns: Turn[], lastGroupOpen = false): string[][] {
  return groupConversationTurns(turns, { lastGroupOpen }).map((group) =>
    group.turns.map((turn) => turn.id),
  );
}

describe("TurnGrouping classification", () => {
  it("detects a wake turn only when every user item is a handoff", () => {
    expect(isAgentWakeTurn(makeTurn("t", [wakeItem("w1"), answerItem("a1")]))).toBe(true);
    expect(isAgentWakeTurn(makeTurn("t", [wakeItem("w1"), userItem("u1")]))).toBe(false);
    expect(isAgentWakeTurn(makeTurn("t", [userItem("u1")]))).toBe(false);
    expect(isAgentWakeTurn(makeTurn("t", [answerItem("a1")]))).toBe(false);
  });

  it("detects real user messages and spawn calls", () => {
    expect(turnHasRealUserMessage(makeTurn("t", [wakeItem("w1")]))).toBe(false);
    expect(turnHasRealUserMessage(makeTurn("t", [userItem("u1")]))).toBe(true);
    expect(turnHasSpawnAgentCall(makeTurn("t", [spawnItem("s1")]))).toBe(true);
    expect(
      turnHasSpawnAgentCall(
        makeTurn("t", [{ id: "b1", type: "collab_agent_tool_call", name: "bash" }]),
      ),
    ).toBe(false);
  });
});

describe("groupConversationTurns", () => {
  it("keeps plain user turns in their own groups", () => {
    const turns = [
      makeTurn("t1", [userItem("u1"), answerItem("a1")]),
      makeTurn("t2", [userItem("u2"), answerItem("a2")]),
    ];
    expect(groupIDs(turns)).toEqual([["t1"], ["t2"]]);
  });

  it("merges a wake turn into the spawn turn's group", () => {
    const turns = [
      makeTurn("t1", [userItem("u1"), spawnItem("s1"), answerItem("a1")]),
      makeTurn("t2", [wakeItem("w1"), answerItem("a2")]),
    ];
    expect(groupIDs(turns)).toEqual([["t1", "t2"]]);
  });

  it("merges parallel completion wakes into one group", () => {
    const turns = [
      makeTurn("t1", [userItem("u1"), spawnItem("s1"), spawnItem("s2")]),
      makeTurn("t2", [wakeItem("w1")]),
      makeTurn("t3", [wakeItem("w2"), answerItem("a3")]),
    ];
    expect(groupIDs(turns)).toEqual([["t1", "t2", "t3"]]);
  });

  it("merges a user turn that arrived while the orchestration was still open", () => {
    // User message during the wait: the wake necessarily lands after it
    // (user-authored work wins the drain), so the chain proves the group
    // was open when the user turn started.
    const turns = [
      makeTurn("t1", [userItem("u1"), spawnItem("s1")]),
      makeTurn("t2", [userItem("u2"), answerItem("a2")]),
      makeTurn("t3", [wakeItem("w1"), answerItem("a3")]),
    ];
    expect(groupIDs(turns)).toEqual([["t1", "t2", "t3"]]);
  });

  it("does not merge a user turn after the orchestration already closed", () => {
    const turns = [
      makeTurn("t1", [userItem("u1"), spawnItem("s1")]),
      makeTurn("t2", [wakeItem("w1"), answerItem("a2")]),
      makeTurn("t3", [userItem("u3"), answerItem("a3")]),
    ];
    expect(groupIDs(turns)).toEqual([["t1", "t2"], ["t3"]]);
  });

  it("does not merge a user turn that spawns its own agent (root guard)", () => {
    const turns = [
      makeTurn("t1", [userItem("u1"), spawnItem("s1")]),
      makeTurn("t2", [wakeItem("w1"), answerItem("a2")]),
      // The reply after the completed orchestration starts a new
      // orchestration; its wake must not pull it back into the old group.
      makeTurn("t3", [userItem("u3"), spawnItem("s2"), answerItem("a3")]),
      makeTurn("t4", [wakeItem("w2"), answerItem("a4")]),
    ];
    expect(groupIDs(turns)).toEqual([
      ["t1", "t2"],
      ["t3", "t4"],
    ]);
  });

  it("chains sequential orchestrations that share a turn correctly", () => {
    // The wake turn itself spawns the next background agent: one group.
    const turns = [
      makeTurn("t1", [userItem("u1"), spawnItem("s1")]),
      makeTurn("t2", [wakeItem("w1"), spawnItem("s2")]),
      makeTurn("t3", [wakeItem("w2"), answerItem("a3")]),
    ];
    expect(groupIDs(turns)).toEqual([["t1", "t2", "t3"]]);
  });

  it("merges consecutive user turns that both ran during the wait", () => {
    const turns = [
      makeTurn("t1", [userItem("u1"), spawnItem("s1")]),
      makeTurn("t2", [userItem("u2"), answerItem("a2")]),
      makeTurn("t3", [userItem("u3"), answerItem("a3")]),
      makeTurn("t4", [wakeItem("w1"), answerItem("a4")]),
    ];
    expect(groupIDs(turns)).toEqual([["t1", "t2", "t3", "t4"]]);
  });

  it("uses the live lastGroupOpen hint for the list tail", () => {
    const turns = [
      makeTurn("t1", [userItem("u1"), spawnItem("s1")]),
      makeTurn("t2", [userItem("u2")], "in_progress"),
    ];
    expect(groupIDs(turns, false)).toEqual([["t1"], ["t2"]]);
    expect(groupIDs(turns, true)).toEqual([["t1", "t2"]]);
  });

  it("keeps a non-user, non-wake turn (compact) as a hard boundary", () => {
    const compactTurn: Turn = {
      id: "tc",
      kind: "compact",
      items_view: "full",
      status: "completed",
      items: [
        { id: "c1", type: "context_compaction", status: "completed", text: "Compacted" },
      ],
    };
    const turns = [
      makeTurn("t1", [userItem("u1"), spawnItem("s1")]),
      compactTurn,
      makeTurn("t3", [wakeItem("w1"), answerItem("a3")]),
    ];
    expect(groupIDs(turns)).toEqual([["t1"], ["tc", "t3"]]);
  });
});
