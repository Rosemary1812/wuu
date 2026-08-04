import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const environmentCSS = readFileSync(
  resolve(__dirname, "styles/environment.css"),
  "utf-8",
);
const baseCSS = readFileSync(resolve(__dirname, "styles/base.css"), "utf-8");

function cssRules(selector: string, source = environmentCSS): string[] {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const matches = [
    ...source.matchAll(
      new RegExp(`^[ \\t]*${escapedSelector}\\s*\\{([\\s\\S]*?)\\n[ \\t]*\\}`, "gm"),
    ),
  ];
  if (matches.length === 0) {
    throw new Error(`missing CSS rule for ${selector}`);
  }
  return matches.map((match) => match[1] ?? "");
}

function cssRule(selector: string, source = environmentCSS): string {
  return cssRules(selector, source)[0] ?? "";
}

describe("conversation search shortcut layout", () => {
  it("uses the standard modal backdrop infrastructure instead of a search-specific blur", () => {
    const sharedBackdrop = cssRule(".app-modal-backdrop", baseCSS);

    expect(sharedBackdrop).toMatch(/background:\s*var\(--modal-backdrop-bg\)/);
    expect(sharedBackdrop).toMatch(/backdrop-filter:\s*blur\(3px\)/);
    expect(cssRule(".conversation-search-overlay")).not.toMatch(/backdrop-filter/);
    expect(environmentCSS).not.toContain("modal-overlay-bg");
  });

  it("keeps the two-pane dialog compact relative to the app window", () => {
    expect(cssRule(".conversation-search-dialog")).toMatch(
      /width:\s*min\(clamp\(560px,\s*58vw,\s*800px\),\s*calc\(100vw\s*-\s*24px\)\)/,
    );
    expect(cssRule(".conversation-search-dialog")).toMatch(
      /max-height:\s*min\(560px,\s*calc\(100vh\s*-\s*160px\)\)/,
    );
    expect(cssRule(".conversation-search-body")).toMatch(
      /grid-template-columns:\s*minmax\(220px,\s*1fr\)\s*minmax\(0,\s*1\.4fr\)/,
    );
  });

  it("does not expand the compact dialog again at the narrow-window breakpoint", () => {
    const widthDeclarations = cssRules(".conversation-search-dialog").filter((rule) =>
      /(?:^|\n)\s*width:/.test(rule),
    );

    expect(widthDeclarations).toHaveLength(1);
  });

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

describe("conversation result rhythm", () => {
  it("keeps the title row in a compact single-row frame", () => {
    expect(cssRule(".conversation-search-result")).toMatch(/display:\s*grid/);
    expect(cssRule(".conversation-search-result")).toMatch(
      /grid-template-columns:\s*minmax\(0,\s*1fr\)\s*auto/,
    );
    expect(cssRule(".conversation-search-result-main")).toMatch(
      /display:\s*grid/,
    );
    expect(cssRule(".conversation-search-result-shortcut")).toMatch(
      /grid-column:\s*2/,
    );
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

  it("allows rendered Markdown to wrap instead of flattening it to one line", () => {
    expect(cssRule(".conversation-search-preview-text")).not.toMatch(
      /white-space:\s*nowrap/,
    );
    expect(cssRule(".conversation-search-preview-text")).toMatch(
      /overflow-wrap:\s*anywhere/,
    );
  });
});
