import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const environmentCSS = readFileSync(
  resolve(__dirname, "styles/environment.css"),
  "utf-8",
);

function cssRule(selector: string): string {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = environmentCSS.match(
    new RegExp(`^${escapedSelector}\\s*\\{([\\s\\S]*?)\\n\\}`, "m"),
  );
  if (!match) {
    throw new Error(`missing CSS rule for ${selector}`);
  }
  return match[1] ?? "";
}

describe("environment panel subagent rows", () => {
  it("overlays the close button beside the first content row", () => {
    const header = cssRule(".environment-panel-header");
    expect(header).toMatch(/position:\s*absolute/);
    expect(header).toMatch(/top:\s*15px/);
    expect(header).toMatch(/right:\s*14px/);
    expect(cssRule(".environment-plan-item:first-child")).toMatch(/padding-right:\s*34px/);
    expect(
      cssRule(".environment-panel-header + .environment-panel-body .environment-row:first-child"),
    ).toMatch(/padding-right:\s*40px/);
  });

  it("keeps the status label on a stable right-side axis", () => {
    expect(cssRule(".subagent-row")).toMatch(
      /grid-template-columns:\s*minmax\(0,\s*1fr\)/,
    );

    const main = cssRule(".subagent-row-main");
    expect(main).toMatch(/grid-column:\s*1/);
    expect(main).toMatch(
      /grid-template-columns:\s*minmax\(0,\s*1fr\)\s*minmax\(42px,\s*auto\)/,
    );
    expect(main).toMatch(/padding:\s*0 8px/);

    const status = cssRule(".subagent-row-status");
    expect(status).toMatch(/justify-content:\s*end/);
    expect(status).toMatch(/min-width:\s*42px/);
    expect(status).toMatch(/background:\s*transparent/);
  });

  it("aligns a larger avatar with the regular environment row icons", () => {
    const avatar = cssRule(".subagent-row .participant-chip-avatar");
    expect(avatar).toMatch(/width:\s*20px/);
    expect(avatar).toMatch(/height:\s*20px/);
    expect(cssRule(".environment-row")).toMatch(/padding:\s*0 8px/);
    expect(cssRule(".subagent-row-main")).toMatch(/padding:\s*0 8px/);
  });

  it("keeps the running status dot static", () => {
    expect(cssRule(".subagent-row.running .subagent-row-status::before")).not.toMatch(
      /animation/,
    );
    expect(environmentCSS).not.toMatch(/@keyframes\s+subagent-status-pulse/);
  });

  it("puts keyboard focus on the row instead of the inner button outline", () => {
    expect(cssRule(".subagent-row-main:focus-visible")).toMatch(/outline:\s*0/);
    expect(environmentCSS).toMatch(
      /\.subagent-row:hover,\s*\.subagent-row:has\(\.subagent-row-action:focus-visible\),\s*\.subagent-row:has\(\.subagent-row-main:focus-visible\)\s*\{[\s\S]*?box-shadow:\s*inset 0 0 0 1px var\(--ink-overlay-8\)/,
    );
  });

  it("reserves action-button space only when actions are visible", () => {
    expect(environmentCSS).toMatch(
      /\.subagent-row:hover \.subagent-row-main,\s*\.subagent-row:has\(\.subagent-row-action:focus-visible\) \.subagent-row-main\s*\{[\s\S]*?padding-right:\s*56px/,
    );
    expect(cssRule(".subagent-row-actions")).toMatch(/grid-area:\s*1 \/ 1/);
    expect(environmentCSS).not.toMatch(
      /\.subagent-row:has\(\.subagent-row-actions\) \.subagent-row-main/,
    );
  });
});
