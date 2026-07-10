import type { ActivitySession, ServerEvent } from "../shared/protocol";

export type ActivitySessionsState = {
  byKey: Record<string, ActivitySession>;
};

const ACTIVITY_NOTIFICATION_METHODS = new Set([
  "activity/started",
  "activity/updated",
  "activity/control_changed",
  "activity/stopped",
]);

export function emptyActivitySessions(): ActivitySessionsState {
  return { byKey: {} };
}

export function reduceActivitySessionEvent(
  state: ActivitySessionsState,
  event: ServerEvent,
): ActivitySessionsState {
  if (event.kind !== "notification" || !ACTIVITY_NOTIFICATION_METHODS.has(event.message.method)) {
    return state;
  }
  const activity = activityFromUnknown(event.message.params);
  if (!activity || activity.workdir !== event.workdir) {
    return state;
  }
  return mergeActivity(state, activity);
}

export function mergeActivityList(
  state: ActivitySessionsState,
  workdir: string,
  threadID: string,
  activities: ActivitySession[],
): ActivitySessionsState {
  let next = state;
  for (const activity of activities) {
    if (activity.workdir === workdir && activity.thread_id === threadID) {
      next = mergeActivity(next, activity);
    }
  }
  return next;
}

export function activitiesForThread(
  state: ActivitySessionsState,
  workdir: string | undefined,
  threadID: string | undefined,
): ActivitySession[] {
  if (!workdir || !threadID) {
    return [];
  }
  return Object.values(state.byKey)
    .filter((activity) => activity.workdir === workdir && activity.thread_id === threadID)
    .sort((left, right) => {
      const timeDelta = Date.parse(left.created_at) - Date.parse(right.created_at);
      return timeDelta || left.id.localeCompare(right.id);
    });
}

function mergeActivity(state: ActivitySessionsState, incoming: ActivitySession): ActivitySessionsState {
  const key = activityKey(incoming);
  const current = state.byKey[key];
  if (current?.state === "stopped" && incoming.state !== "stopped") {
    return state;
  }
  if (current && eventTime(incoming.updated_at) < eventTime(current.updated_at)) {
    return state;
  }
  if (current === incoming) {
    return state;
  }
  return { byKey: { ...state.byKey, [key]: incoming } };
}

function activityKey(activity: ActivitySession): string {
  return `${activity.workdir}\u0000${activity.thread_id}\u0000${activity.id}`;
}

function eventTime(value: string): number {
  const parsed = Date.parse(value);
  return Number.isFinite(parsed) ? parsed : 0;
}

function activityFromUnknown(value: unknown): ActivitySession | undefined {
  if (!isRecord(value)) {
    return undefined;
  }
  if (
    typeof value.id !== "string" ||
    (value.kind !== "browser" && value.kind !== "cua") ||
    typeof value.thread_id !== "string" ||
    typeof value.workdir !== "string" ||
    typeof value.state !== "string" ||
    typeof value.controller !== "string" ||
    typeof value.created_at !== "string" ||
    typeof value.updated_at !== "string"
  ) {
    return undefined;
  }
  return value as ActivitySession;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
