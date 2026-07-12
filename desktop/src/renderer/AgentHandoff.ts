export type AgentHandoffDisplay = {
  label: string;
};

type AgentHandoffEnvelope = {
  content?: unknown;
  trigger_turn?: unknown;
};

type AgentNotificationPayload = {
  agent_path?: unknown;
  status?: {
    agent_id?: unknown;
    task_name?: unknown;
    status?: unknown;
  };
};

const NOTIFICATION_OPEN = "<subagent_notification>";
const NOTIFICATION_CLOSE = "</subagent_notification>";

// The protocol-level name the backend stamps on inter-agent self-addressed
// user_message items. Mirrors the `name == "wuu_agent_notification"` branch
// of `IsAgentNotification` in internal/context/context.go: this is the one
// reliable wire signal, and we use it as the primary gate before falling
// back to text sniffing — text sniffing breaks on `\n\n`-joined envelopes
// (combineAgentCompletionMessages) and on <changed_file_overlap> tails
// (AgentCompletionChatMessage's overlap warning).
export const AGENT_NOTIFICATION_NAME = "wuu_agent_notification";

// Duck type covering ThreadItem and the mobile chatModel row types. We
// intentionally do NOT import ThreadItem here so the helper stays cheap to
// pull into non-protocol callers (mobile, snapshot reducers).
type HandoffItem = {
  name?: string;
  text?: string;
};

export function isAgentHandoffText(text: string | undefined): boolean {
  return parseAgentHandoff(text) !== undefined;
}

export function agentHandoffDisplay(text: string | undefined): AgentHandoffDisplay | undefined {
  const handoff = parseAgentHandoff(text);
  if (!handoff || !handoff.triggerTurn) {
    return undefined;
  }

  const { payload } = handoff;
  const status = stringValue(payload.status?.status);
  return { label: handoffStatusLabel(status) };
}

// Primary gate for ThreadItem-shaped items: the `name` field is the wire
// signal the backend stamps on every agent self-addressed user_message
// (single or combined envelope). A name match is enough to classify the
// item as a handoff regardless of payload parseability — see
// `agentHandoffDisplayItem` for why we deliberately do NOT parse `text`
// in the name-hit branch.
export function isAgentHandoffItem(item: HandoffItem | undefined): boolean {
  if (!item) {
    return false;
  }
  if (item.name === AGENT_NOTIFICATION_NAME) {
    return true;
  }
  return isAgentHandoffText(item.text);
}

// Display resolver for ThreadItem-shaped items. The name-hit branch
// returns HANDOFF_GENERIC_LABEL WITHOUT touching `item.text` — important
// because backend has shipped two wire shapes where the JSON envelope is
// followed by non-JSON garbage:
//
//   1. combineAgentCompletionMessages joins ≥2 envelopes with "\n\n",
//      producing a string no JSON.parse can consume.
//   2. AgentCompletionChatMessage appends "<changed_file_overlap>..."
//      after the envelope when CompletionOverlapWarnings is non-empty.
//
// In both cases the only reliable signal is `item.name`. We deliberately
// return the generic label instead of attempting to extract a status
// from unparseable text — this is the same fallback the legacy parser
// used for unknown statuses (see handoffStatusLabel default branch).
export function agentHandoffDisplayItem(
  item: HandoffItem | undefined,
): AgentHandoffDisplay | undefined {
  if (!item) {
    return undefined;
  }
  if (item.name === AGENT_NOTIFICATION_NAME) {
    return { label: HANDOFF_GENERIC_LABEL };
  }
  return agentHandoffDisplay(item.text);
}

function parseAgentHandoff(
  text: string | undefined,
): { payload: AgentNotificationPayload; triggerTurn: boolean } | undefined {
  const trimmed = text?.trim();
  if (!trimmed) {
    return undefined;
  }

  const directPayload = parseNotificationPayload(trimmed);
  if (directPayload) {
    return { payload: directPayload, triggerTurn: false };
  }

  const envelope = parseJSON<AgentHandoffEnvelope>(trimmed);
  if (!envelope || typeof envelope.content !== "string") {
    return undefined;
  }

  const payload = parseNotificationPayload(envelope.content);
  return payload ? { payload, triggerTurn: envelope.trigger_turn === true } : undefined;
}

function parseNotificationPayload(content: string): AgentNotificationPayload | undefined {
  const trimmed = content.trim();
  if (!trimmed.startsWith(NOTIFICATION_OPEN) || !trimmed.endsWith(NOTIFICATION_CLOSE)) {
    return undefined;
  }
  const raw = trimmed.slice(NOTIFICATION_OPEN.length, trimmed.length - NOTIFICATION_CLOSE.length).trim();
  return parseJSON<AgentNotificationPayload>(raw);
}

const HANDOFF_GENERIC_LABEL = "subagent 更新了任务状态";

function handoffStatusLabel(status: string): string {
  switch (status) {
    case "pending":
    case "queued":
      return "subagent 等待执行任务";
    case "running":
      return "subagent 正在执行任务";
    case "completed":
      return "subagent 完成了任务";
    case "failed":
      return "subagent 任务失败";
    case "cancelled":
      return "subagent 任务已取消";
    default:
      return HANDOFF_GENERIC_LABEL;
  }
}

function stringValue(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

function parseJSON<T>(text: string): T | undefined {
  try {
    return JSON.parse(text) as T;
  } catch {
    return undefined;
  }
}
