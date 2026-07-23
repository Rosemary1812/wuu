const systemNotificationsKey = "wuu.channels.systemNotifications";

export function channelSystemNotificationsEnabled(): boolean {
  try {
    return window.localStorage.getItem(systemNotificationsKey) === "true";
  } catch {
    return false;
  }
}

export function setChannelSystemNotificationsEnabled(enabled: boolean): void {
  try {
    window.localStorage.setItem(systemNotificationsKey, String(enabled));
  } catch {
    // The preference remains off when storage is unavailable.
  }
}
