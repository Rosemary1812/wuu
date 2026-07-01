import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const turnsCss = readFileSync(resolve(__dirname, "turns.css"), "utf-8");

function cssRuleBody(selector: string): string {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = turnsCss.match(new RegExp(`(?:^|\\})\\s*${escapedSelector}\\s*\\{([\\s\\S]*?)\\}`));
  if (!match) {
    throw new Error(`missing CSS rule for ${selector}`);
  }
  return match[1];
}

describe("turns.css user message actions", () => {
  it("keeps action buttons aligned to the user bubble edge", () => {
    expect(turnsCss).toMatch(
      /\.user-message-actions\s*\{[\s\S]*?justify-self:\s*end;[\s\S]*?justify-content:\s*flex-end;[\s\S]*?width:\s*max-content;/,
    );
  });
});

describe("turns.css rich links", () => {
  it("keeps inline message links blue without underlines or icon backplates", () => {
    expect(cssRuleBody(".rich-link")).toMatch(/color:\s*#176dc2;/);
    expect(cssRuleBody(".rich-link")).toMatch(/text-decoration-line:\s*none;/);
    expect(cssRuleBody(".rich-link-favicon-frame")).not.toMatch(/\bbackground\s*:/);
    expect(cssRuleBody(".rich-link-favicon")).not.toMatch(/\bbackground\s*:/);
  });

  it("keeps file links on the same text rhythm as surrounding prose", () => {
    expect(turnsCss).toMatch(
      /\.rich-web-link,\s*\.rich-file-link\s*\{[\s\S]*?display:\s*inline;/,
    );
    expect(cssRuleBody(".rich-file-link")).toMatch(/appearance:\s*none;/);
    expect(cssRuleBody(".rich-file-link")).toMatch(/line-height:\s*inherit;/);
    expect(cssRuleBody(".rich-file-link")).toMatch(/background:\s*transparent;/);
    expect(cssRuleBody(".rich-file-link:active")).toMatch(/background:\s*transparent;/);
  });
});
