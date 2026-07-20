import { afterAll, describe, expect, it, vi } from "vitest";

// Feature flags are evaluated at module import, so the opt-in must be hoisted.
vi.hoisted(() => {
  vi.stubEnv("VITE_ENABLE_REMOTE_CONTROL", "true");
});

import { ENABLE_REMOTE_CONTROL } from "./FeatureFlags";

afterAll(() => {
  vi.unstubAllEnvs();
});

describe("remote control feature flag", () => {
  it("supports explicit opt-in builds", () => {
    expect(ENABLE_REMOTE_CONTROL).toBe(true);
  });
});
