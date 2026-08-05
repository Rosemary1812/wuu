import { afterEach, describe, expect, it, vi } from "vitest";

afterEach(() => {
  vi.unstubAllEnvs();
  vi.resetModules();
});

describe("frontend feature flags", () => {
  it("supports explicit remote control opt-in builds", async () => {
    vi.stubEnv("VITE_ENABLE_REMOTE_CONTROL", "true");
    const { ENABLE_REMOTE_CONTROL } = await import("./FeatureFlags");

    expect(ENABLE_REMOTE_CONTROL).toBe(true);
  });

  it("keeps Ultra mode disabled unless explicitly enabled", async () => {
    vi.stubEnv("VITE_ENABLE_ULTRA_MODE", "false");
    let featureFlags = await import("./FeatureFlags");
    expect(featureFlags.ENABLE_ULTRA_MODE).toBe(false);

    vi.resetModules();
    vi.stubEnv("VITE_ENABLE_ULTRA_MODE", "true");
    featureFlags = await import("./FeatureFlags");
    expect(featureFlags.ENABLE_ULTRA_MODE).toBe(true);
  });

  it("keeps released collaboration enabled regardless of the legacy build flag", async () => {
    vi.stubEnv("VITE_ENABLE_GROUP_CHAT", "false");
    let featureFlags = await import("./FeatureFlags");
    expect(featureFlags.ENABLE_GROUP_CHAT).toBe(true);

    vi.resetModules();
    vi.stubEnv("VITE_ENABLE_GROUP_CHAT", "true");
    featureFlags = await import("./FeatureFlags");
    expect(featureFlags.ENABLE_GROUP_CHAT).toBe(true);
  });

  it("keeps voice input and BYOK polish disabled unless explicitly enabled", async () => {
    vi.stubEnv("VITE_ENABLE_VOICE_INPUT", "false");
    let featureFlags = await import("./FeatureFlags");
    expect(featureFlags.ENABLE_VOICE_INPUT).toBe(false);

    vi.resetModules();
    vi.stubEnv("VITE_ENABLE_VOICE_INPUT", "true");
    featureFlags = await import("./FeatureFlags");
    expect(featureFlags.ENABLE_VOICE_INPUT).toBe(true);
  });

  it("keeps management assistants disabled unless explicitly enabled", async () => {
    vi.stubEnv("VITE_ENABLE_MANAGEMENT_ASSISTANT", "false");
    let featureFlags = await import("./FeatureFlags");
    expect(featureFlags.ENABLE_MANAGEMENT_ASSISTANT).toBe(false);

    vi.resetModules();
    vi.stubEnv("VITE_ENABLE_MANAGEMENT_ASSISTANT", "true");
    featureFlags = await import("./FeatureFlags");
    expect(featureFlags.ENABLE_MANAGEMENT_ASSISTANT).toBe(true);
  });
});
