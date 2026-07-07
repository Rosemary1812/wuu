// The reconnect grace window: a short link drop (foreground-return
// reattach, brief wifi blip) must NOT flash the "重连中…" strip. Only a
// drop that stays down past the grace window surfaces the banner. This
// test mocks @wuu/remote-core so we can drive the onDetach/onAttach
// callbacks from the test instead of standing up a real relay.
//
// The push module is also mocked: the controller's onAttach path calls
// registerPush(), which would otherwise try to invoke expo-notifications
// native APIs that have no test environment.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// Capture the options the controller passes to RemoteClient so the test
// can fire onDetach / onAttach directly. The mock factory below closes
// over this same object reference (vi.mock hoists the factory to the top
// of the file, so it must not reference any other top-level binding).
const captured: {
  onAttach: (ev: { session: string; resumed: boolean }) => void;
  onDetach: () => void;
  started: number;
} = {
  onAttach: () => {},
  onDetach: () => {},
  started: 0,
};

vi.mock("@wuu/remote-core", () => {
  class FakeRemoteClient {
    constructor(
      _creds: unknown,
      opts: { onAttach?: typeof captured.onAttach; onDetach?: typeof captured.onDetach } = {},
    ) {
      captured.onAttach = opts.onAttach ?? (() => {});
      captured.onDetach = opts.onDetach ?? (() => {});
    }
    start(): void {
      captured.started += 1;
    }
    async call<T>(): Promise<T> {
      return undefined as unknown as T;
    }
    async stop(): Promise<void> {}
  }
  return {
    RemoteClient: FakeRemoteClient,
    pair: vi.fn(),
  };
});

vi.mock("expo-notifications", () => ({
  setNotificationHandler: vi.fn(),
  setNotificationChannelAsync: vi.fn().mockResolvedValue(undefined),
  getPermissionsAsync: vi.fn().mockResolvedValue({ status: "denied" }),
  requestPermissionsAsync: vi.fn().mockResolvedValue({ status: "denied", ios: { status: 0 } }),
  getExpoPushTokenAsync: vi.fn().mockResolvedValue({ data: "ExponentPushToken[fake]" }),
  getDevicePushTokenAsync: vi.fn().mockResolvedValue({ data: "fake-device-token", type: "ios" }),
  addPushTokenListener: vi.fn().mockReturnValue({ remove: () => {} }),
  addNotificationResponseReceivedListener: vi.fn().mockReturnValue({ remove: () => {} }),
  getLastNotificationResponseAsync: vi.fn().mockResolvedValue(null),
  AndroidImportance: { HIGH: 4 },
  PermissionStatus: { GRANTED: "granted", DENIED: "denied", UNDETERMINED: "undetermined" },
}));

vi.mock("expo-constants", () => ({
  default: { expoConfig: { extra: { eas: { projectId: "test-project" } } } },
}));

vi.mock("react-native", () => ({
  Platform: { OS: "ios" },
}));

vi.mock("expo-secure-store", () => ({
  getItemAsync: vi.fn().mockResolvedValue(null),
  setItemAsync: vi.fn().mockResolvedValue(undefined),
  deleteItemAsync: vi.fn().mockResolvedValue(undefined),
}));

// Late import: must come after the vi.mock declarations so the test sees
// the mocked modules.
import { WuuMobile } from "../src/lib/connection";
import type { Credentials } from "@wuu/remote-core";

function makeCreds(): Credentials {
  return {
    v: 1,
    device_seed: "AAAA",
    host_pub: "BBBB",
    host_name: "test-host",
    relay_url: "wss://relay.example/test",
  };
}

describe("WuuMobile reconnect grace window", () => {
  beforeEach(() => {
    captured.onAttach = () => {};
    captured.onDetach = () => {};
    captured.started = 0;
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  function bootController(): WuuMobile {
    const credStore = {
      load: vi.fn().mockResolvedValue(makeCreds()),
      save: vi.fn().mockResolvedValue(undefined),
      clear: vi.fn().mockResolvedValue(undefined),
    };
    const controller = new WuuMobile(credStore);
    void controller.startFromStoredCredentials();
    return controller;
  }

  it("flips the phase to reconnecting only after the grace window", async () => {
    const controller = bootController();
    // Boot is asynchronous: let the start-from-stored path run to the
    // point where the controller's start() has built the RemoteClient.
    await vi.advanceTimersByTimeAsync(0);
    expect(captured.started).toBe(1);
    expect(controller.store.getSnapshot().phase).toBe("connecting");

    captured.onDetach();
    // Just after detach: phase must still be "connecting" — the grace
    // window is in effect.
    await vi.advanceTimersByTimeAsync(100);
    expect(controller.store.getSnapshot().phase).toBe("connecting");
    // Halfway through the grace window: still connecting.
    await vi.advanceTimersByTimeAsync(200);
    expect(controller.store.getSnapshot().phase).toBe("connecting");
    // Past the grace window: now we surface "reconnecting".
    await vi.advanceTimersByTimeAsync(400);
    expect(controller.store.getSnapshot().phase).toBe("reconnecting");
  });

  it("cancels the pending flip when attach lands inside the grace window", async () => {
    const controller = bootController();
    await vi.advanceTimersByTimeAsync(0);

    captured.onDetach();
    // Reattach inside the window (e.g. foreground-return): phase must
    // never reach "reconnecting".
    await vi.advanceTimersByTimeAsync(200);
    captured.onAttach({ session: "s1", resumed: true });
    // Wait past the window: phase should still be "attached".
    await vi.advanceTimersByTimeAsync(700);
    expect(controller.store.getSnapshot().phase).toBe("attached");
  });

  it("cancels a still-pending flip when the link comes back up after it was about to fire", async () => {
    const controller = bootController();
    await vi.advanceTimersByTimeAsync(0);

    captured.onDetach();
    await vi.advanceTimersByTimeAsync(550);
    // Reattach a hair before the timer would have fired.
    captured.onAttach({ session: "s2", resumed: true });
    await vi.advanceTimersByTimeAsync(200);
    expect(controller.store.getSnapshot().phase).toBe("attached");
  });
});
