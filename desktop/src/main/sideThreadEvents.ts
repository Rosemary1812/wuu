import type { ServerEvent, SideThreadEvent } from "../shared/protocol";

const SIDE_THREAD_EVENT_METHOD = "sideThread/event";
const SIDE_THREAD_EVENT_TYPES = new Set<SideThreadEvent["type"]>([
  "status",
  "delta",
  "message",
  "error"
]);

export function sideThreadEventFromServerEvent(
  event: ServerEvent
): SideThreadEvent | undefined {
  if (
    event.kind !== "notification" ||
    event.message.method !== SIDE_THREAD_EVENT_METHOD
  ) {
    return undefined;
  }
  const params = event.message.params;
  if (
    !isRecord(params) ||
    !SIDE_THREAD_EVENT_TYPES.has(params.type as SideThreadEvent["type"]) ||
    typeof params.side_thread_id !== "string" ||
    typeof params.main_thread_id !== "string"
  ) {
    return undefined;
  }
  return params as SideThreadEvent;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
