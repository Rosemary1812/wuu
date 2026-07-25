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

describe("conversation search shortcut layout", () => {
  it("gives the longer Windows shortcut its own content-sized grid track", () => {
    expect(cssRule(".conversation-search-input-wrap")).toMatch(
      /grid-template-columns:\s*24px\s*minmax\(0,\s*1fr\)\s*28px/,
    );
    expect(
      cssRule(':root[data-platform="win32"] .conversation-search-input-wrap'),
    ).toMatch(
      /grid-template-columns:\s*24px\s*minmax\(0,\s*1fr\)\s*max-content/,
    );
  });

  it("keeps result shortcuts quiet until their row is highlighted", () => {
    expect(cssRule(".conversation-search-result-shortcut")).toMatch(
      /opacity:\s*0/,
    );
    expect(
      cssRule(".conversation-search-result.selected .conversation-search-result-shortcut"),
    ).toMatch(/opacity:\s*1/);
  });
});

describe("conversation preview rhythm", () => {
  it("uses tighter spacing within a turn than between turns", () => {
    expect(cssRule(".conversation-search-preview-turn-group")).toMatch(
      /gap:\s*10px/,
    );
    expect(cssRule(".conversation-search-preview-turns")).toMatch(
      /gap:\s*24px/,
    );
  });
});
