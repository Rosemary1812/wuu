import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { afterAll, describe, expect, it, vi } from "vitest";

// Feature flags are evaluated at module import, so the opt-in must be hoisted.
vi.hoisted(() => {
  vi.stubEnv("VITE_ENABLE_REMOTE_CONTROL", "true");
});

import {
  ENABLE_COLLABORATION,
  ENABLE_REMOTE_CONTROL,
} from "./FeatureFlags";

afterAll(() => {
  vi.unstubAllEnvs();
});

describe("remote control feature flag", () => {
  it("supports explicit opt-in builds", () => {
    expect(ENABLE_REMOTE_CONTROL).toBe(true);
  });
});

describe("collaboration feature flag", () => {
  it("supports the explicit opt-in used by the collaboration test suite", () => {
    expect(ENABLE_COLLABORATION).toBe(true);
  });

  it("keeps production build and release scripts opted out", () => {
    const packageJSON = JSON.parse(
      readFileSync(resolve(process.cwd(), "package.json"), "utf8"),
    ) as { scripts?: Record<string, string> };

    for (const script of ["build", "pack:mac", "dist:mac", "release:desktop"]) {
      expect(packageJSON.scripts?.[script]).not.toContain(
        "VITE_ENABLE_COLLABORATION",
      );
    }
  });
});
