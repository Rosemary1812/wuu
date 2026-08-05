import { performance } from "node:perf_hooks";
import type { ThreadItem, Turn } from "../src/shared/protocol";
import { appendTurnTokenSample, initialState } from "../src/renderer/AppState";
import { groupConversationTurns } from "../src/renderer/TurnGrouping";

function turn(id: string, items: ThreadItem[]): Turn {
  return { id, items_view: "full", status: "completed", items };
}

function userItem(id: string): ThreadItem {
  return { id, type: "user_message", text: id };
}

function spawnItem(index: number): ThreadItem {
  return {
    id: `spawn-${index}`,
    type: "tool_call",
    name: "spawn_agent",
    status: "completed",
    result: JSON.stringify({ agent_id: `agent-${index}`, status: "running" }),
  };
}

function wakeItem(index: number): ThreadItem {
  return {
    id: `wake-${index}`,
    type: "user_message",
    name: "wuu_agent_notification",
    text: `<subagent_notification>{"status":{"agent_id":"agent-${index}","status":"completed"}}</subagent_notification>`,
  };
}

function fixture(agentCount: number): Turn[] {
  const turns: Turn[] = [];
  for (let index = 0; index < agentCount; index += 1) {
    turns.push(turn(`spawn-turn-${index}`, [userItem(`user-${index}`), spawnItem(index)]));
  }
  for (let index = 0; index < agentCount; index += 1) {
    turns.push(turn(`wake-turn-${index}`, [wakeItem(index)]));
  }
  return turns;
}

for (const agentCount of [250, 500, 1_000, 2_000]) {
  const turns = fixture(agentCount);
  const startedAt = performance.now();
  const groups = groupConversationTurns(turns);
  const elapsed = performance.now() - startedAt;
  console.log(
    JSON.stringify({
      agentCount,
      turnCount: turns.length,
      groups: groups.length,
      elapsedMs: elapsed,
    }),
  );
}

for (const telemetryCount of [500, 1_000, 2_000, 4_000]) {
  let state = initialState;
  const startedAt = performance.now();
  for (let index = 0; index < telemetryCount; index += 1) {
    state = appendTurnTokenSample(
      state,
      `telemetry-turn-${index}`,
      "thread-benchmark",
      index,
      index,
      0,
      0,
      index,
    );
  }
  const elapsed = performance.now() - startedAt;
  console.log(JSON.stringify({
    telemetryCount,
    retainedTelemetry: Object.keys(state.turnTokenUsage).length,
    elapsedMs: elapsed,
  }));
}
