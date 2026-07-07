// Subagent-handoff detection, mirrored from desktop AgentHandoff.ts: inter-
// agent notifications arrive as self-addressed user_message items wrapping a
// <subagent_notification> payload. They are working-transcript machinery and
// must never render as chat bubbles.

const NOTIFICATION_OPEN = "<subagent_notification>";
const NOTIFICATION_CLOSE = "</subagent_notification>";

export function isAgentHandoffText(text: string | undefined): boolean {
  const trimmed = text?.trim();
  if (!trimmed) return false;
  if (isNotificationPayload(trimmed)) return true;
  const envelope = parseJSON<{ content?: unknown }>(trimmed);
  if (!envelope || typeof envelope.content !== "string") return false;
  return isNotificationPayload(envelope.content.trim());
}

function isNotificationPayload(content: string): boolean {
  if (!content.startsWith(NOTIFICATION_OPEN) || !content.endsWith(NOTIFICATION_CLOSE)) {
    return false;
  }
  const raw = content.slice(NOTIFICATION_OPEN.length, content.length - NOTIFICATION_CLOSE.length).trim();
  // Truthiness mirrors the desktop's `payload ? ... : undefined`: a parse
  // producing null/false/0 is not a handoff payload.
  return Boolean(parseJSON<unknown>(raw));
}

function parseJSON<T>(text: string): T | undefined {
  try {
    return JSON.parse(text) as T;
  } catch {
    return undefined;
  }
}
