// Contract test for the skin layer.
//
// The skin axis works only if skin-flame.css stays a pure token
// overlay: two blocks (flame light / flame dark), each carrying enough
// attribute-selector specificity to beat base.css `:root` and
// theme.css `:root[data-theme="dark"]` regardless of import order, and
// nothing in it that styles components directly. These tests pin that
// shape so the layer cannot silently grow component rules or lose a
// block.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const skinCss = readFileSync(resolve(__dirname, "skin-flame.css"), "utf-8");
const stylesEntry = readFileSync(resolve(__dirname, "../styles.css"), "utf-8");

function topLevelSelectors(css: string): string[] {
  const noComments = css.replace(/\/\*[\s\S]*?\*\//g, "");
  const selectors: string[] = [];
  let depth = 0;
  let current = "";
  for (const char of noComments) {
    if (char === "{") {
      if (depth === 0) {
        selectors.push(current.trim());
        current = "";
      }
      depth++;
    } else if (char === "}") {
      depth--;
    } else if (depth === 0) {
      current += char;
    }
  }
  return selectors.filter(Boolean);
}

describe("skin-flame.css token overlay", () => {
  it("is imported by the styles entry point", () => {
    expect(stylesEntry).toContain('@import "./styles/skin-flame.css"');
  });

  it("contains exactly the flame light and flame dark blocks", () => {
    expect(topLevelSelectors(skinCss).sort()).toEqual([
      ':root[data-theme="dark"][data-skin="flame"]',
      ':root[data-theme="light"][data-skin="flame"]',
    ]);
  });

  it("declares only custom properties (no component styling)", () => {
    const declarations = skinCss
      .replace(/\/\*[\s\S]*?\*\//g, "")
      .match(/[^;{}]+:[^;{}]+;/g) ?? [];
    for (const declaration of declarations) {
      expect(declaration.trim()).toMatch(/^--/);
    }
  });

  it("keeps the landing-page vermillion as the flame accent", () => {
    expect(skinCss).toMatch(/--wuu-accent:\s*#ff3d00/);
  });

  it("overrides the skin-gated empty-home hooks in both blocks", () => {
    const mascotHooks = skinCss.match(/--empty-home-mascot-display:\s*block/g);
    expect(mascotHooks).toHaveLength(2);
  });
});
