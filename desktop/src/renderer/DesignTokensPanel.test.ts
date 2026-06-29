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
    expect(designTokensPanelSource).not.toMatch(/cssVar:\s*"--conversation-readable-width"/);
    expect(designTokensPanelSource).not.toMatch(/cssVar:\s*"--conversation-flow-padding-inline"/);
    expect(designTokensPanelSource).not.toMatch(/cssVar:\s*"--conversation-dialog-width"/);
    expect(designTokensPanelSource).not.toMatch(/cssVar:\s*"--conversation-dialog-radius"/);
  });

  it("uses a fresh storage namespace for the relaxed reading rhythm", () => {
    expect(designTokensPanelSource).toContain('const STORAGE_KEY = "wuu:design-tokens:v4"');
    expect(designTokensPanelSource).toContain("defaultValue: 1.9");
    expect(designTokensPanelSource).toContain("Math.min(token.max, Math.max(token.min, value))");
  });

  it("cleans stale inline variables that used to override the reading rhythm", () => {
    expect(designTokensPanelSource).toContain('"wuu:design-tokens:v2"');
    expect(designTokensPanelSource).toContain('"wuu:design-tokens:v3"');
    expect(designTokensPanelSource).toContain('"--conversation-prose-line-height"');
    expect(designTokensPanelSource).toContain('cssVar: "--conversation-reading-line-height"');
    expect(designTokensPanelSource).not.toMatch(/cssVar:\s*"--conversation-prose-line-height"/);
  });
});
