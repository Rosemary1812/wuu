import { isAgentHandoffItem } from "./AgentHandoff";

export const PROCESS_NOTIFICATION_NAME = "wuu_process_notification";

type InternalUserNotificationItem = {
  name?: string;
  text?: string;
};

export function isProcessNotificationText(text: string | undefined): boolean {
  const trimmed = text?.trim();
  return Boolean(
    trimmed?.startsWith("<process_notification>") &&
      trimmed.endsWith("</process_notification>"),
  );
}

export function isProcessNotificationItem(
  item: InternalUserNotificationItem | undefined,
): boolean {
  if (!item) {
    return false;
  }
  if (item.name === PROCESS_NOTIFICATION_NAME) {
    return true;
  }
  return isProcessNotificationText(item.text);
}

export function isInternalUserNotificationItem(
  item: InternalUserNotificationItem | undefined,
): boolean {
  return isAgentHandoffItem(item) || isProcessNotificationItem(item);
}
