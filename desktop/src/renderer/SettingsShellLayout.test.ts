import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const settingsCSS = readFileSync(resolve(__dirname, "styles/settings.css"), "utf-8");

function cssRule(selector: string): string {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = settingsCSS.match(new RegExp(`^${escapedSelector}\\s*\\{([\\s\\S]*?)\\n\\}`, "m"));
  if (!match) {
    throw new Error(`missing CSS rule for ${selector}`);
  }
  return match[1] ?? "";
}

describe("settings shell titlebar", () => {
  it("aligns its sidebar toggle with the main conversation titlebar", () => {
    expect(cssRule(".settings-titlebar")).toMatch(/padding:\s*0 24px;/);
  });

  it("keeps its collapse toggle clear of the macOS traffic lights", () => {
    expect(cssRule(".settings-shell.sidebar-collapsed .settings-titlebar")).toMatch(
      /padding-left:\s*86px;/,
    );
  });
});
