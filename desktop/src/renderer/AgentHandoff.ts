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
      return "subagent 更新了任务状态";
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
