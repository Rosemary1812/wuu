import type { Turn } from "../shared/protocol";
import {
  agentHandoffAgentIDs,
  agentHandoffChipDisplayItems,
  isAgentHandoffItem,
} from "./AgentHandoff";

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

type TurnGroupingFacts = {
  isAgentWake: boolean;
  hasRealUserMessage: boolean;
  hasSpawnAgentCall: boolean;
  spawnedAgentIDs: string[];
  wakeAgentIDs: string[];
};

export function isAgentWakeTurn(turn: Turn): boolean {
  return turnGroupingFacts(turn).isAgentWake;
}

export function turnHasRealUserMessage(turn: Turn): boolean {
  return turnGroupingFacts(turn).hasRealUserMessage;
}

export function turnHasSpawnAgentCall(turn: Turn): boolean {
  return turnGroupingFacts(turn).hasSpawnAgentCall;
}

export function turnsHavePendingSubagents(turns: Turn[]): boolean {
  return subagentProgressForTurns(turns).remaining > 0;
}

export type SubagentTimelineProgress = {
  total: number;
  finished: number;
  remaining: number;
};

export function subagentProgressForTurns(turns: Turn[]): SubagentTimelineProgress {
  let pending = 0;
  let finished = 0;
  for (const turn of turns) {
    for (const item of turn.items) {
      if (isSpawnAgentItem(item) && spawnResultIsPending(item.result)) {
        pending += 1;
        continue;
      }
      if (item.type === "user_message" && isAgentHandoffItem(item)) {
        const completionCount = Math.max(
          1,
          agentHandoffChipDisplayItems(item).length,
        );
        const resolved = Math.min(pending, completionCount);
        pending -= resolved;
        finished += resolved;
      }
    }
  }
  return { total: pending + finished, finished, remaining: pending };
}

function turnGroupingFacts(turn: Turn): TurnGroupingFacts {
  const spawnedAgentIDs: string[] = [];
  const wakeAgentIDs: string[] = [];
  let sawAgentHandoff = false;
  let hasRealUserMessage = false;
  let hasSpawnAgentCall = false;
  for (const item of turn.items) {
    if (isSpawnAgentItem(item)) {
      hasSpawnAgentCall = true;
      const id = spawnResultAgentID(item.result);
      if (id) {
        spawnedAgentIDs.push(id);
      }
    }
    if (item.type !== "user_message") {
      continue;
    }
    if (!isAgentHandoffItem(item)) {
      hasRealUserMessage = true;
      continue;
    }
    sawAgentHandoff = true;
    wakeAgentIDs.push(...agentHandoffAgentIDs(item));
  }
  const isAgentWake = sawAgentHandoff && !hasRealUserMessage;
  return {
    isAgentWake,
    hasRealUserMessage,
    hasSpawnAgentCall,
    spawnedAgentIDs,
    wakeAgentIDs: isAgentWake ? wakeAgentIDs : [],
  };
}

function isSpawnAgentItem(item: Turn["items"][number]): boolean {
  return (
    (item.type === "tool_call" || item.type === "collab_agent_tool_call") &&
    item.name === "spawn_agent"
  );
}

function spawnResultIsPending(raw: string | undefined): boolean {
  if (!raw) return true;
  try {
    const parsed = JSON.parse(raw) as { status?: unknown };
    if (typeof parsed.status !== "string") return true;
    switch (parsed.status.trim().toLowerCase()) {
      case "completed":
      case "failed":
      case "cancelled":
      case "canceled":
      case "closed":
        return false;
      default:
        return true;
    }
  } catch {
    return true;
  }
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
  const facts = new Array<TurnGroupingFacts>(count);
  const firstSpawnIndexByAgentID = new Map<string, number>();
  const lastWakeIndexByAgentID = new Map<string, number>();
  for (let index = 0; index < count; index += 1) {
    const turnFacts = turnGroupingFacts(turns[index]);
    facts[index] = turnFacts;
    for (const id of turnFacts.spawnedAgentIDs) {
      if (!firstSpawnIndexByAgentID.has(id)) {
        firstSpawnIndexByAgentID.set(id, index);
      }
    }
    for (const id of turnFacts.wakeAgentIDs) {
      lastWakeIndexByAgentID.set(id, index);
    }
  }

  // A spawning turn at index i belongs to an earlier orchestration when an
  // agent spawned before i either wakes after i or is still running. Model
  // each agent as an open interval and merge those intervals with a prefix
  // sum. The old implementation retained a cumulative Set snapshot at every
  // turn and cloned it on each spawn/wake, making subagent-heavy histories
  // quadratic to regroup on every server event.
  const openIntervalDelta = new Int32Array(count + 1);
  for (const [agentID, spawnIndex] of firstSpawnIndexByAgentID) {
    const openFrom = spawnIndex + 1;
    const openUntil = runningIDs.has(agentID)
      ? count
      : (lastWakeIndexByAgentID.get(agentID) ?? 0);
    if (openFrom >= openUntil) {
      continue;
    }
    openIntervalDelta[openFrom] += 1;
    openIntervalDelta[openUntil] -= 1;
  }
  const earlierOrchestrationOpen = new Uint8Array(count);
  let openOrchestrationCount = 0;
  for (let index = 0; index < count; index += 1) {
    openOrchestrationCount += openIntervalDelta[index];
    earlierOrchestrationOpen[index] = openOrchestrationCount > 0 ? 1 : 0;
  }

  const joinsPrevious = new Array<boolean>(count).fill(false);
  for (let index = count - 1; index >= 0; index -= 1) {
    const turnFacts = facts[index];
    if (turnFacts.isAgentWake) {
      joinsPrevious[index] = true;
      continue;
    }
    if (!turnFacts.hasRealUserMessage) {
      continue;
    }
    if (turnFacts.hasSpawnAgentCall) {
      joinsPrevious[index] = earlierOrchestrationOpen[index] === 1;
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
