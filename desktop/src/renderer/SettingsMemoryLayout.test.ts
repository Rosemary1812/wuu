import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const settingsCSS = readFileSync(resolve(__dirname, "styles/settings.css"), "utf-8");

describe("settings memory header layout", () => {
  it("keeps actions right aligned until the title row actually needs to wrap", () => {
    expect(settingsCSS).toMatch(
      /\.settings-memory-header\s*\{[\s\S]*?flex-wrap:\s*wrap;/,
    );
    expect(settingsCSS).toMatch(
      /\.settings-memory-actions\s*\{[\s\S]*?margin-left:\s*auto;/,
    );
    expect(settingsCSS).toMatch(
      /@media \(max-width:\s*900px\)[\s\S]*?\.settings-page-header:not\(\.settings-memory-header\)/,
    );
  });
});
