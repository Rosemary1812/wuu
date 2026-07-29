import type { Turn } from "../shared/protocol";
import { agentHandoffAgentIDs, isAgentHandoffItem } from "./AgentHandoff";

/**
 * Subagent orchestration produces several real turns for what reads as one
 * conversational beat: the main turn spawns a background agent and settles,
 * then each child completion wakes a fresh synthetic turn
 * (appserver startSyntheticTurn) whose only user item is the
 * `wuu_agent_notification` envelope. Grouping is the presentation-layer
 * inverse of that split: a contiguous run of turns that belongs to one
 * orchestration renders through a single shell (one timer, one process
 * fold, one action bar) while every underlying turn keeps its own identity
 * for streaming keys, fork and edit.
 *
 * Membership rules (evaluated back-to-front):
 *   - a wake turn (carries an agent-handoff user item and no real user
 *     message) always joins the previous group;
 *   - a plain user turn joins the previous group when the orchestration was
 *     still open when it started — observed as "the next turn joins this
 *     turn's group" (a completion wake can only exist when something was
 *     pending), or, for the list tail, via the caller's live hint
 *     (`runningAgentIDs` / `lastGroupOpen`: the thread still has running
 *     child agents);
 *   - a user turn that itself spawns an agent is an orchestration root and
 *     starts its own group — UNLESS an earlier orchestration was still open
 *     when it started. "Still open" is attributed by agent identity: a
 *     later wake reports on an agent spawned before this turn, or (at the
 *     list tail) an agent spawned before this turn is still running.
 *     Contiguous groups cannot keep interleaved orchestrations apart — the
 *     earlier agent's wake would land inside this turn's group anyway — so
 *     the whole run renders as one block, matching the in-turn steer model.
 */
export type TurnGroup = {
  /** Identity of the group's first turn: React key + scroll anchor. */
  id: string;
  turns: Turn[];
};

export function isAgentWakeTurn(turn: Turn): boolean {
  let sawHandoff = false;
  for (const item of turn.items) {
    if (item.type !== "user_message") continue;
    if (isAgentHandoffItem(item)) {
      sawHandoff = true;
    } else {
      return false;
    }
  }
  return sawHandoff;
}

export function turnHasRealUserMessage(turn: Turn): boolean {
  return turn.items.some(
    (item) => item.type === "user_message" && !isAgentHandoffItem(item),
  );
}

export function turnHasSpawnAgentCall(turn: Turn): boolean {
  return turn.items.some(
    (item) =>
      item.type === "collab_agent_tool_call" && item.name === "spawn_agent",
  );
}

/** Agent identities spawned by this turn, read from spawn_agent results. */
function turnSpawnedAgentIDs(turn: Turn): string[] {
  const ids: string[] = [];
  for (const item of turn.items) {
    if (item.type !== "collab_agent_tool_call" || item.name !== "spawn_agent") {
      continue;
    }
    const id = spawnResultAgentID(item.result);
    if (id) {
      ids.push(id);
    }
  }
  return ids;
}

function spawnResultAgentID(result: string | undefined): string | undefined {
  const trimmed = result?.trim();
  if (!trimmed) {
    return undefined;
  }
  try {
    const parsed: unknown = JSON.parse(trimmed);
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      const id = (parsed as { agent_id?: unknown }).agent_id;
      if (typeof id === "string" && id.trim()) {
        return id.trim();
      }
    }
  } catch {
    // A failed spawn can return plain error text; no agent to attribute.
  }
  return undefined;
}

function turnWakeAgentIDs(turn: Turn): string[] {
  const ids: string[] = [];
  for (const item of turn.items) {
    if (item.type !== "user_message") continue;
    ids.push(...agentHandoffAgentIDs(item));
  }
  return ids;
}

export function groupConversationTurns(
  turns: Turn[],
  options?: {
    /** Legacy live hint: the thread has running child agents. Derived from
     *  `runningAgentIDs` when omitted. */
    lastGroupOpen?: boolean;
    /** IDs of the thread's currently running child agents. Enables agent
     *  attribution for spawning tail turns (no wake evidence yet). */
    runningAgentIDs?: readonly string[];
  },
): TurnGroup[] {
  const count = turns.length;
  if (count === 0) return [];

  const runningIDs = new Set(options?.runningAgentIDs ?? []);
  const lastGroupOpen = options?.lastGroupOpen ?? runningIDs.size > 0;

  // spawnedBefore[i]: agent ids spawned by turns 0..i-1. Sets are shared
  // between indexes until a spawn actually lands, so this stays O(spawns).
  const spawnedBefore: Array<ReadonlySet<string>> = new Array(count);
  let spawnedSoFar: ReadonlySet<string> = new Set<string>();
  for (let index = 0; index < count; index += 1) {
    spawnedBefore[index] = spawnedSoFar;
    const spawned = turnSpawnedAgentIDs(turns[index]);
    if (spawned.length > 0) {
      const next = new Set(spawnedSoFar);
      for (const id of spawned) next.add(id);
      spawnedSoFar = next;
    }
  }

  // wakedAfter[i]: agent ids reported by wake turns at indexes > i.
  const wakedAfter: Array<ReadonlySet<string>> = new Array(count);
  let wakedSoFar: ReadonlySet<string> = new Set<string>();
  for (let index = count - 1; index >= 0; index -= 1) {
    wakedAfter[index] = wakedSoFar;
    const waked = isAgentWakeTurn(turns[index])
      ? turnWakeAgentIDs(turns[index])
      : [];
    if (waked.length > 0) {
      const next = new Set(wakedSoFar);
      for (const id of waked) next.add(id);
      wakedSoFar = next;
    }
  }

  const earlierOrchestrationOpen = (index: number): boolean => {
    const earlier = spawnedBefore[index];
    if (earlier.size === 0) {
      return false;
    }
    for (const id of earlier) {
      if (wakedAfter[index].has(id) || runningIDs.has(id)) {
        return true;
      }
    }
    return false;
  };

  const joinsPrevious = new Array<boolean>(count).fill(false);
  for (let index = count - 1; index >= 0; index -= 1) {
    const turn = turns[index];
    if (isAgentWakeTurn(turn)) {
      joinsPrevious[index] = true;
      continue;
    }
    if (!turnHasRealUserMessage(turn)) {
      continue;
    }
    if (turnHasSpawnAgentCall(turn)) {
      joinsPrevious[index] = earlierOrchestrationOpen(index);
      continue;
    }
    const nextJoins =
      index + 1 < count ? joinsPrevious[index + 1] : lastGroupOpen;
    joinsPrevious[index] = nextJoins;
  }

  const groups: TurnGroup[] = [];
  for (let index = 0; index < count; index += 1) {
    const turn = turns[index];
    const current = groups[groups.length - 1];
    if (current && joinsPrevious[index]) {
      current.turns.push(turn);
    } else {
      groups.push({ id: turn.id, turns: [turn] });
    }
  }
  return groups;
}
