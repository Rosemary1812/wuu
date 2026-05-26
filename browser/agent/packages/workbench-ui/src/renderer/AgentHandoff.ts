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

export function agentHandoffDisplay(text: string | undefined): AgentHandoffDisplay | undefined {
  const trimmed = text?.trim();
  if (!trimmed) {
    return undefined;
  }

  const envelope = parseJSON<AgentHandoffEnvelope>(trimmed);
  if (!envelope || envelope.trigger_turn !== true || typeof envelope.content !== "string") {
    return undefined;
  }

  const payload = parseNotificationPayload(envelope.content);
  if (!payload) {
    return undefined;
  }

  const name = compactAgentName(
    stringValue(payload.status?.task_name) ||
      lastPathSegment(stringValue(payload.agent_path)) ||
      stringValue(payload.status?.agent_id)
  );
  const status = stringValue(payload.status?.status);
  return { label: handoffStatusLabel(name, status) };
}

function parseNotificationPayload(content: string): AgentNotificationPayload | undefined {
  const trimmed = content.trim();
  if (!trimmed.startsWith(NOTIFICATION_OPEN) || !trimmed.endsWith(NOTIFICATION_CLOSE)) {
    return undefined;
  }
  const raw = trimmed.slice(NOTIFICATION_OPEN.length, trimmed.length - NOTIFICATION_CLOSE.length).trim();
  return parseJSON<AgentNotificationPayload>(raw);
}

function handoffStatusLabel(name: string, status: string): string {
  const subject = name ? `子 agent ${name}` : "子 agent";
  switch (status) {
    case "completed":
      return `${subject} 已完成，主 agent 正在整合结果`;
    case "failed":
      return `${subject} 失败，主 agent 正在处理结果`;
    case "cancelled":
      return `${subject} 已取消，主 agent 正在处理结果`;
    default:
      return `${subject} 已更新，主 agent 正在处理结果`;
  }
}

function compactAgentName(name: string): string {
  const normalized = name.trim().replace(/\s+/g, " ");
  if (normalized.length <= 56) {
    return normalized;
  }
  return `${normalized.slice(0, 53)}...`;
}

function lastPathSegment(path: string): string {
  const trimmed = path.trim();
  if (!trimmed) {
    return "";
  }
  const parts = trimmed.split("/").filter(Boolean);
  return parts.at(-1) ?? "";
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
