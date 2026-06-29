import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const designTokensPanelSource = readFileSync(
  resolve(process.cwd(), "src/renderer/DesignTokensPanel.tsx"),
  "utf8",
);

describe("DesignTokensPanel layout token contract", () => {
  it("writes session layout variables used by the live conversation flow", () => {
    expect(designTokensPanelSource).toContain('cssVar: "--session-outer-width"');
    expect(designTokensPanelSource).toContain('cssVar: "--session-outer-padding-inline"');
    expect(designTokensPanelSource).toContain('cssVar: "--session-composer-width"');
    expect(designTokensPanelSource).toContain('cssVar: "--session-composer-radius"');
  });

  it("does not write legacy conversation layout variables", () => {
    expect(designTokensPanelSource).not.toContain("--conversation-readable-width");
    expect(designTokensPanelSource).not.toContain("--conversation-flow-padding-inline");
    expect(designTokensPanelSource).not.toContain("--conversation-dialog-width");
    expect(designTokensPanelSource).not.toContain("--conversation-dialog-radius");
  });

  it("uses a fresh storage namespace for the relaxed reading rhythm", () => {
    expect(designTokensPanelSource).toContain('const STORAGE_KEY = "wuu:design-tokens:v3"');
    expect(designTokensPanelSource).toContain("defaultValue: 1.72");
    expect(designTokensPanelSource).toContain("Math.min(token.max, Math.max(token.min, value))");
  });
});
