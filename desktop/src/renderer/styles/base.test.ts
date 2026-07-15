import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const baseCss = readFileSync(resolve(__dirname, "base.css"), "utf-8");
const sidebarCss = readFileSync(resolve(__dirname, "sidebar.css"), "utf-8");
const environmentCss = readFileSync(resolve(__dirname, "environment.css"), "utf-8");
const themeCss = readFileSync(resolve(__dirname, "theme.css"), "utf-8");

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

  it("keeps the shared collapse motion when auto-collapse fires mid-resize", () => {
    // Auto-collapse only ever happens during a window resize, and the blanket
    // .window-resizing suppression would swallow its transition entirely — the
    // sidebar would snap away. Reusing the shared sidebar motion here is a
    // deliberate reversal of an earlier "stay glued to the window edge" rule:
    // grid columns always sum to 100%, so nothing reflows mid-transition.
    expect(baseCss).toMatch(
      /\.window-resizing\s+\.app-shell\.sidebar-animating\s*\{[\s\S]*?grid-template-columns\s+var\(--sidebar-motion-duration\)[\s\S]*?var\(--sidebar-motion-ease\)\s*!important/,
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

/**
 * Neutral-hue regression guard.
 *
 * Each component style file declares its own :root token block. Until mid
 * 2026 those blocks quietly redefined their own neutral hues: the sidebar
 * tilted green (#666f69, sage hex), the environment search tilted green
 * (#7c8380, #909692, #878e8a, rgba(105,115,109,...)), and a few glass
 * shadows drifted cool-blue. Side-by-side they read as three materials —
 * visibly a faintly dirty edge between adjacent surfaces.
 *
 * The fix is to point every non-semantic neutral at --ink-soft /
 * --ink-muted / --ink-overlay-* / --surface-*. The list below locks that
 * in: if a future change reintroduces a literal grey outside the base ink
 * family, the assertion fails and forces a conscious decision.
 *
 * Semantic tones that are deliberately NOT neutral stay allowed:
 * child-agent sage (rgba(95,126,104,...) and rgba(120,160,132,...)),
 * status reds/oranges, brand vermillion, sage environment tones. Only
 * structural text + overlay shades must stay aligned with the ink ladder.
 */
describe("component :root blocks stay in the shared neutral family", () => {
  // Hex / rgba literals that previously hid a green or off-neutral cast in
  // the sidebar / environment :root block. They should NOT reappear in the
  // component block after the 2026-07-15 neutral rewrite.
  const FORBIDDEN_NEUTRALS: RegExp[] = [
    /#666f69\b/,
    /#68716c\b/,
    /#9aa39d\b/,
    /#98a29c\b/,
    /#7c8380\b/,
    /#909692\b/,
    /#7d8580\b/,
    /#878e8a\b/,
    /#676c70\b/,
    /#9aa0a3\b/,
    /#efefec\b/,
    /#69736d\b/,
    /#262b2f\b/,
    /#747b77\b/,
    /#707874\b/,
    /#7f858a\b/,
    /#e9ebe7\b/,
    /#f0f1ef\b/,
    /#e9ece8\b/,
    /#e7e8e5\b/,
    /#b7bdba\b/,
    /#6d736f\b/,
    /#a8aeaa\b/,
    /#8f9591\b/,
    /#969c98\b/,
    /#2e3338\b/,
    /#9aa09c\b/,
    /#8f959a\b/,
    /#a2a8ad\b/,
    /#2b2f34\b/,
    /#dde0e3\b/,
    /#24282c\b/,
    /#2a2f33\b/,
    /rgba\(\s*32,\s*38,\s*34\b/,
    /rgba\(\s*45,\s*50,\s*54\b/,
    /rgba\(\s*68,\s*76,\s*70\b/,
    /rgba\(\s*105,\s*115,\s*109\b/,
    /rgba\(\s*160,\s*170,\s*164\b/,
  ];

  function findViolations(css: string, fileName: string): string[] {
    const violations: string[] = [];
    for (const pat of FORBIDDEN_NEUTRALS) {
      const matches = css.match(pat);
      if (matches) {
        violations.push(matches[0]);
        // Surface the file in the failure so the next editor knows where to look.
        // eslint-disable-next-line no-console
        console.warn(
          `[${fileName}] reintroduces off-neutral literal ${matches[0]} (matched ${pat.source})`,
        );
      }
    }
    return violations;
  }

  it("sidebar :root neutral literals route through shared ink tokens", () => {
    expect(findViolations(sidebarCss, "sidebar.css")).toEqual([]);
  });

  it("environment :root neutral literals route through shared ink tokens", () => {
    expect(findViolations(environmentCss, "environment.css")).toEqual([]);
  });

  it("dark-theme sidebar/environment overrides align with the light block", () => {
    // The dark block in theme.css should ride on the same shared tokens as
    // the light block. The forbidden-neutral list is the same check, scoped
    // to the sidebar + environment sections so we don't have to scan
    // unrelated dark redefinitions (workspace tabs, settings heatmap, brand
    // vermillion — those keep their per-component hues on purpose).
    const sidebarSection = themeCss.match(
      /\/\* sidebar\.css \*\/[\s\S]*?(?=\n\n[^\n])/,
    )?.[0] ?? "";
    const envSection = themeCss.match(
      /\/\* environment\.css \*\/[\s\S]*?(?=\n\n[^\n])/,
    )?.[0] ?? "";
    expect(findViolations(sidebarSection, "theme.css#sidebar")).toEqual([]);
    expect(findViolations(envSection, "theme.css#environment")).toEqual([]);
  });

  it("status / brand hues preserved where intended", () => {
    // Smoke check that the values we expect to keep (semantic tones) are
    // still declared. If a future refactor drops --env-tone-stable, the
    // deletions-text danger, or the brand accent, this test surfaces the
    // regression. Brand vermillion lives in base.css; the env-tone palette
    // lives in environment.css; the env-deletions-text danger lives in
    // environment.css (light) and theme.css (dark).
    const combined = environmentCss + "\n" + baseCss + "\n" + themeCss;
    for (const pat of [/#2f8a56\b/, /#c42020\b/, /#ff3d00\b/, /#f0705f\b/]) {
      expect(combined).toMatch(pat);
    }
  });
});
