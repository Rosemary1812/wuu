import { translateCurrent } from "./i18n";

export type AgentHandoffDisplay = {
  label: string;
};

// Structured outcome so chip aggregation never has to re-derive meaning
// from localized label text.
export type SubagentChipOutcome =
  | "completed"
  | "failed"
  | "cancelled"
  | "running"
  | "pending"
  | "updated";

export type SubagentChipDisplay = {
  label: string;
  outcome: SubagentChipOutcome;
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
    return { label: handoffGenericLabel() };
  }
  return agentHandoffDisplay(item.text);
}

export function agentHandoffChipDisplayItems(
  item: HandoffItem | undefined,
): SubagentChipDisplay[] {
  if (!item || !isAgentHandoffItem(item)) {
    return [];
  }
  const payloads = parseAgentHandoffPayloads(item.text);
  if (payloads.length === 0) {
    // Name-stamped but unparseable payload: fall back to one generic chip
    // so raw JSON never reaches the chat flow.
    return item.name === AGENT_NOTIFICATION_NAME
      ? [{ label: handoffChipStatusLabel("updated"), outcome: "updated" }]
      : [];
  }
  return payloads.map((payload) => {
    const name = handoffName(payload);
    const outcome = handoffChipOutcome(stringValue(payload.status?.status));
    return {
      label: `${name} ${handoffChipStatusLabel(outcome)}`.trim(),
      outcome,
    };
  });
}

// Agent identities a wake notification reports on. TurnGrouping uses these
// to attribute a wake to the orchestration that spawned the agent; an
// unparseable envelope yields [] and the caller treats the wake as
// unattributable rather than guessing.
export function agentHandoffAgentIDs(item: HandoffItem | undefined): string[] {
  if (!item || !isAgentHandoffItem(item)) {
    return [];
  }
  const ids: string[] = [];
  for (const payload of parseAgentHandoffPayloads(item.text)) {
    const id = stringValue(payload.status?.agent_id);
    if (id) {
      ids.push(id);
    }
  }
  return ids;
}

// Completion notifications pile up while the parent agent is mid-turn: the
// backend joins ≥2 envelopes with "\n\n" (combineAgentCompletionMessages),
// a string JSON.parse cannot consume as a whole. Each segment is still a
// self-contained envelope (JSON.stringify never emits raw newlines inside a
// payload), so split and parse each one — combined delivery then stays as
// informative as notifications delivered one at a time. The same loop also
// absorbs the legacy <changed_file_overlap> text tail: the envelope segment
// parses and the tail segment is ignored.
function parseAgentHandoffPayloads(
  text: string | undefined,
): AgentNotificationPayload[] {
  const trimmed = text?.trim();
  if (!trimmed) {
    return [];
  }
  const single = parseAgentHandoff(trimmed);
  if (single) {
    return [single.payload];
  }
  const payloads: AgentNotificationPayload[] = [];
  for (const segment of trimmed.split("\n\n")) {
    const handoff = parseAgentHandoff(segment);
    if (handoff) {
      payloads.push(handoff.payload);
    }
  }
  return payloads;
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

function handoffGenericLabel(): string {
  return translateCurrent("agent.handoff.updated");
}

function handoffStatusLabel(status: string): string {
  switch (status) {
    case "pending":
    case "queued":
      return translateCurrent("agent.handoff.pending");
    case "running":
      return translateCurrent("agent.handoff.running");
    case "completed":
      return translateCurrent("agent.handoff.completed");
    case "failed":
      return translateCurrent("agent.handoff.failed");
    case "cancelled":
      return translateCurrent("agent.handoff.cancelled");
    default:
      return handoffGenericLabel();
  }
}

function handoffChipOutcome(status: string): SubagentChipOutcome {
  switch (status) {
    case "completed":
      return "completed";
    case "failed":
      return "failed";
    case "cancelled":
      return "cancelled";
    case "running":
      return "running";
    case "pending":
    case "queued":
      return "pending";
    default:
      return "updated";
  }
}

function handoffChipStatusLabel(outcome: SubagentChipOutcome): string {
  switch (outcome) {
    case "completed":
      return translateCurrent("agent.handoff.chip.completed");
    case "failed":
      return translateCurrent("agent.handoff.chip.failed");
    case "cancelled":
      return translateCurrent("agent.handoff.chip.cancelled");
    case "running":
      return translateCurrent("agent.handoff.chip.running");
    case "pending":
      return translateCurrent("agent.handoff.chip.pending");
    default:
      return translateCurrent("agent.handoff.chip.updated");
  }
}

function handoffName(payload: AgentNotificationPayload): string {
  const taskName = stringValue(payload.status?.task_name);
  if (taskName) {
    return taskName;
  }
  const segments = stringValue(payload.agent_path).split("/").filter(Boolean);
  return segments.at(-1) ?? "subagent";
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
