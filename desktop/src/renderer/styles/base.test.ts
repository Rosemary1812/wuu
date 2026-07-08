import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const baseCss = readFileSync(resolve(__dirname, "base.css"), "utf-8");

describe("resize drag over embedded frames", () => {
  it("neutralizes webview/iframe pointer events while a resize is active", () => {
    // The right-panel resize tracks the drag with window-level pointer
    // listeners. An Electron <webview> (browser tool) runs in its own hit-test
    // layer and would swallow those events the instant the cursor crosses it,
    // freezing the drag. The .window-resizing class is on <html> for the whole
    // drag, so disabling pointer events on frames there keeps the drag flowing.
    expect(baseCss).toMatch(
      /\.window-resizing\s+webview[\s\S]*?\{[\s\S]*?pointer-events:\s*none/,
    );
    expect(baseCss).toMatch(
      /\.window-resizing\s+iframe[\s\S]*?\{[\s\S]*?pointer-events:\s*none/,
    );
  });

  it("keeps the sidebar auto-collapse grid transition enabled during window resize", () => {
    expect(baseCss).toMatch(
      /\.window-resizing\s+\.app-shell\.sidebar-animating\s*\{[\s\S]*?transition:\s*grid-template-columns\s+var\(--sidebar-motion-duration,\s*340ms\)\s+var\(--sidebar-motion-ease\)\s*!important;/,
    );
  });
});

describe("global text selection", () => {
  it("uses a stable neutral highlight instead of the theme accent", () => {
    const selectionRule = baseCss.match(/::selection\s*\{[\s\S]*?\}/)?.[0] ?? "";

    expect(selectionRule).not.toBe("");
    expect(selectionRule).not.toContain("var(--wuu-accent)");
    expect(selectionRule).toMatch(/color:\s*inherit/);
  });
});
