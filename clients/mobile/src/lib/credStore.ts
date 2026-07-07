// Credential persistence: Keychain/Keystore via expo-secure-store on
// device; localStorage on web (explicitly weaker — the design doc calls
// this out, web is a dev/preview surface).

import * as SecureStore from "expo-secure-store";
import { Platform } from "react-native";
import type { Credentials } from "@wuu/remote-core";

import type { CredentialStore } from "./connection";

const KEY = "wuu.remote.credentials";

export const deviceCredentialStore: CredentialStore = {
  async load(): Promise<Credentials | null> {
    const raw =
      Platform.OS === "web"
        ? globalThis.localStorage?.getItem(KEY) ?? null
        : await SecureStore.getItemAsync(KEY);
    if (!raw) return null;
    try {
      return JSON.parse(raw) as Credentials;
    } catch {
      return null;
    }
  },
  async save(creds: Credentials): Promise<void> {
    const raw = JSON.stringify(creds);
    if (Platform.OS === "web") {
      globalThis.localStorage?.setItem(KEY, raw);
      return;
    }
    await SecureStore.setItemAsync(KEY, raw);
  },
  async clear(): Promise<void> {
    if (Platform.OS === "web") {
      globalThis.localStorage?.removeItem(KEY);
      return;
    }
    await SecureStore.deleteItemAsync(KEY);
  },
};
