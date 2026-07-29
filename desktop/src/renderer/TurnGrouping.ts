import type { Turn } from "../shared/protocol";
import { isAgentHandoffItem } from "./AgentHandoff";

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
 *     (`lastGroupOpen`: the thread still has running child agents);
 *   - a user turn that itself spawns an agent is an orchestration root and
 *     starts its own group even mid-chain.
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

export function groupConversationTurns(
  turns: Turn[],
  options?: { lastGroupOpen?: boolean },
): TurnGroup[] {
  const count = turns.length;
  if (count === 0) return [];

  const joinsPrevious = new Array<boolean>(count).fill(false);
  for (let index = count - 1; index >= 0; index -= 1) {
    const turn = turns[index];
    if (isAgentWakeTurn(turn)) {
      joinsPrevious[index] = true;
      continue;
    }
    if (!turnHasRealUserMessage(turn) || turnHasSpawnAgentCall(turn)) {
      continue;
    }
    const nextJoins =
      index + 1 < count ? joinsPrevious[index + 1] : Boolean(options?.lastGroupOpen);
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
