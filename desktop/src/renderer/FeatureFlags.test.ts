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
});
