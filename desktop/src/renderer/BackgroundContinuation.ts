import type { Agent, Thread, ThreadItem, Turn } from "../shared/protocol";
import { isInternalUserNotificationItem } from "./InternalUserNotification";

export type BackgroundContinuationKind = "agent" | "process" | "mixed";

export type BackgroundContinuationState = {
  waiting: boolean;
  kind?: BackgroundContinuationKind;
};

export function isAutomaticBackgroundContinuationTurn(turn: Turn | undefined): boolean {
  return Boolean(
    turn?.kind === "internal" &&
      turn.items.some(
        (item) =>
          item.type === "user_message" && isInternalUserNotificationItem(item),
      ),
  );
}

export function hasFollowingBackgroundContinuation(
  turns: ReadonlyArray<Turn>,
  index: number,
): boolean {
  return isAutomaticBackgroundContinuationTurn(turns[index + 1]);
}

export function backgroundContinuationState(
  thread: Pick<Thread, "turns" | "child_agents" | "background_waiting"> | undefined,
): BackgroundContinuationState {
  const latestTurn = thread?.turns.at(-1);
  if (!thread || !latestTurn || latestTurn.status !== "completed") {
    return { waiting: false };
  }

  if (thread.background_waiting !== undefined) {
    const kind = backgroundContinuationKind(thread, thread.background_waiting);
    return kind ? { waiting: true, kind } : { waiting: false };
  }

  const kind = backgroundContinuationKind(thread);

  if (!kind) {
    return { waiting: false };
  }
  return { waiting: true, kind };
}

function backgroundContinuationKind(
  thread: Pick<Thread, "turns" | "child_agents">,
  processWaiting?: boolean,
): BackgroundContinuationKind | undefined {
  const latestTurn = thread.turns.at(-1);
  const waitingForAgent =
    thread.child_agents?.some(isLiveBackgroundAgent) === true ||
    latestTurn?.items.some(startsBackgroundAgent) === true;
  const waitingForProcess =
    processWaiting ?? latestTurn?.items.some(startsResumingBackgroundProcess) === true;
  return waitingForAgent && waitingForProcess
    ? "mixed"
    : waitingForAgent
      ? "agent"
      : waitingForProcess
        ? "process"
        : undefined;
}

function isLiveBackgroundAgent(
  agent: Pick<Agent, "status" | "nested_running_count">,
): boolean {
  if ((agent.nested_running_count ?? 0) > 0) {
    return true;
  }
  switch (agent.status.trim().toLowerCase()) {
    case "pending":
    case "queued":
    case "running":
    case "waiting_children":
      return true;
    default:
      return false;
  }
}

function startsBackgroundAgent(item: ThreadItem): boolean {
  if (item.name !== "spawn_agent") {
    return false;
  }
  const result = parseRecord(item.result);
  if (!isLiveStatus(result?.status)) {
    return false;
  }
  const args = parseRecord(item.arguments);
  return args?.run_in_background === true || result?.run_in_background === true;
}

function startsResumingBackgroundProcess(item: ThreadItem): boolean {
  const args = parseRecord(item.arguments);
  if (args?.action !== "start_background" || args.completion_mode === "detached") {
    return false;
  }
  const result = parseRecord(item.result);
  return (
    result?.action === "start_background" &&
    isLiveStatus(result.status) &&
    (nonEmptyString(result.id) !== undefined ||
      nonEmptyString(result.process_id) !== undefined)
  );
}

function parseRecord(value: string | undefined): Record<string, unknown> | undefined {
  if (!value?.trim()) {
    return undefined;
  }
  try {
    const parsed: unknown = JSON.parse(value);
    return parsed !== null && typeof parsed === "object" && !Array.isArray(parsed)
      ? (parsed as Record<string, unknown>)
      : undefined;
  } catch {
    return undefined;
  }
}

function isLiveStatus(value: unknown): boolean {
  return value === "pending" || value === "queued" || value === "starting" || value === "running";
}

function nonEmptyString(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() !== "" ? value : undefined;
}
