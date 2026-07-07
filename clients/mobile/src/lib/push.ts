// Push registration for the mobile app. The host's M2-1 push dispatch
// component needs a stable device token to ping the relay with; we obtain it
// via expo-notifications (Expo push for unified iOS/Android delivery) and
// also expose the native FCM/APNs token for deployments that prefer the raw
// provider. The register call is fire-and-forget — failure to obtain a token
// must never block the rest of the attach flow.
//
// Foreground notification handling (no banner, no sound, but do register the
// listener) is set at module load so a notification that arrives between JS
// bridge warm-up and the first attach is not dropped silently.

import { Platform } from "react-native";
import Constants from "expo-constants";
import * as Notifications from "expo-notifications";

// Quiet foreground banners: the phone's own banner is the OS notification
// surface, and the app already has the chats list as a heartbeat indicator.
Notifications.setNotificationHandler({
  handleNotification: async () => ({
    shouldPlaySound: false,
    shouldSetBadge: false,
    shouldShowBanner: true,
    shouldShowList: true,
  }),
});

export type PushPlatform = "ios" | "android";

export interface PushTokenBundle {
  expoPushToken?: string;
  devicePushToken?: string;
  platform: PushPlatform;
}

type PushTokenListener = (bundle: PushTokenBundle) => void;
type ResponseListener = (response: Notifications.NotificationResponse) => void;

const DEFAULT_CHANNEL = "default";

/** True on web (and other non-notification platforms): no token, no-op. */
function isUnsupported(): boolean {
  return Platform.OS !== "ios" && Platform.OS !== "android";
}

/** projectId is the EAS project id; on Android it is required to mint an
 *  Expo push token. Falls back to the runtime config so the dev build still
 *  works locally without a deployed EAS project. */
function resolveProjectId(): string | null {
  const cfg = (Constants as unknown as { expoConfig?: { extra?: { eas?: { projectId?: string } } } })
    .expoConfig;
  return cfg?.extra?.eas?.projectId ?? null;
}

/** Best-effort: create the Android channel so the permissions prompt is
 *  wired up before we ask for the token. Idempotent. */
async function ensureAndroidChannel(): Promise<void> {
  if (Platform.OS !== "android") return;
  try {
    await Notifications.setNotificationChannelAsync(DEFAULT_CHANNEL, {
      name: "wuu 新消息",
      importance: Notifications.AndroidImportance.HIGH,
      vibrationPattern: [0, 250, 250, 250],
      lightColor: "#FFFFFF",
    });
  } catch {
    // setNotificationChannelAsync throws on pre-26 devices; ignore — they
    // don't need a channel anyway and the token call below will still work.
  }
}

/** True only when the OS reports permission as granted. The Expo status
 *  string is normalized; on iOS we honour PROVISIONAL too (notifications
 *  deliver quietly to history without an explicit banner prompt). */
function permissionGranted(status: Notifications.PermissionStatus | undefined): boolean {
  if (!status) return false;
  if (status === Notifications.PermissionStatus.GRANTED) return true;
  if (
    Platform.OS === "ios" &&
    (Constants as unknown as { expoConfig?: { ios?: { usesProvisionalPushToken?: boolean } } })
      .expoConfig?.ios?.usesProvisionalPushToken
  ) {
    return true;
  }
  return false;
}

/** Asks the OS for push permission if not already granted. Returns whether
 *  the user has permission at the end of the call (granted OR provisional). */
export async function requestNotificationPermission(): Promise<boolean> {
  if (isUnsupported()) return false;
  await ensureAndroidChannel();
  const existing = await Notifications.getPermissionsAsync();
  if (permissionGranted(existing.status)) return true;
  const next = await Notifications.requestPermissionsAsync();
  if (Platform.OS === "ios") {
    return permissionGranted(next.ios?.status as Notifications.PermissionStatus | undefined);
  }
  return permissionGranted(next.status);
}

/** Fetches both an Expo push token (Expo's unified delivery) and the raw
 *  native FCM/APNs token. The host accepts either — Expo is preferred so
 *  a single relay can target both platforms through one provider. */
export async function fetchPushTokens(): Promise<PushTokenBundle | null> {
  if (isUnsupported()) return null;
  await ensureAndroidChannel();
  const projectId = resolveProjectId();
  let expoPushToken: string | undefined;
  if (projectId) {
    try {
      const result = await Notifications.getExpoPushTokenAsync({ projectId });
      expoPushToken = result.data;
    } catch {
      // Offline at the moment of register, or Expo service rejected: fall
      // back to the raw device token. The host will re-register later when
      // the link is back up and the controller re-attempts.
    }
  }
  let devicePushToken: string | undefined;
  try {
    const native = await Notifications.getDevicePushTokenAsync();
    devicePushToken = (native as { data: string }).data;
  } catch {
    // Some dev clients do not support device tokens; we tolerate that and
    // rely on the Expo token alone.
  }
  if (!expoPushToken && !devicePushToken) return null;
  return {
    expoPushToken,
    devicePushToken,
    platform: Platform.OS as PushPlatform,
  };
}

/** Subscribes to push-token rollovers. expo-notifications emits when the
 *  provider hands out a new token (e.g. app reinstall, OS push settings
 *  reset). The controller wires each rollover into a re-register RPC. */
export function addPushTokenRefreshListener(listener: PushTokenListener): () => void {
  if (isUnsupported()) return () => {};
  const sub = Notifications.addPushTokenListener((token: { data: string }) => {
    listener({
      expoPushToken: token.data,
      platform: Platform.OS as PushPlatform,
    });
  });
  return () => sub.remove();
}

/** Subscribes to notification responses (taps). The payload data.url
 *  field is the deep-link the host stuffed into the push. */
export function addNotificationResponseListener(listener: ResponseListener): () => void {
  if (isUnsupported()) return () => {};
  const sub = Notifications.addNotificationResponseReceivedListener(listener);
  return () => sub.remove();
}

/** Returns the response that launched the app (cold start from a tap).
 *  Resolves to null when the app was launched for any other reason. */
export async function getInitialNotificationResponse(): Promise<Notifications.NotificationResponse | null> {
  if (isUnsupported()) return null;
  try {
    return await Notifications.getLastNotificationResponseAsync();
  } catch {
    return null;
  }
}
