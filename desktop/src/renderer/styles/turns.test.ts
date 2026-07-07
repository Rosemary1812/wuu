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
  it("overlays action buttons above the bubble edge instead of reserving flow space", () => {
    const body = cssRuleBody(".user-message-actions");
    // Out-of-flow overlay: in-flow placement reserved an invisible
    // 24px row under every bubble and bloated the turn boundary.
    expect(body).toMatch(/position:\s*absolute;/);
    expect(body).toMatch(/bottom:\s*100%;/);
    expect(body).toMatch(/right:\s*0;/);
    expect(body).toMatch(/justify-content:\s*flex-end;/);
    expect(body).toMatch(/width:\s*max-content;/);
    // The hover bridge must be padding (contiguous hit area), not offset.
    expect(body).toMatch(/padding-bottom:/);
    expect(body).not.toMatch(/justify-self/);
  });
});

describe("turns.css rich links", () => {
  it("tints inline message links with the brand accent, no underlines or icon backplates", () => {
    expect(cssRuleBody(".rich-link")).toMatch(/color:\s*var\(--rich-link-color\);/);
    expect(turnsCss).toMatch(/--rich-link-color:\s*#c73a08;/);
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

describe("turns.css turn notice positioning", () => {
  it("renders notices as a centered broken divider with side hairlines", () => {
    const body = cssRuleBody(".turn-notice");
    // Full width so the ::before/::after hairlines have room to stretch.
    expect(body).toMatch(/width:\s*100%;/);
    expect(body).toMatch(/max-width:\s*100%;/);
    expect(body).not.toMatch(/680px/);
    expect(body).toMatch(/margin-inline:\s*auto;/);
    // The side hairlines exist (not display: none) and flex to fill.
    const lines = cssRuleBody(".turn-event-notice::before,\n.turn-event-notice::after");
    expect(lines).toMatch(/content:\s*"";/);
    expect(lines).toMatch(/flex:\s*1 1 0;/);
    expect(lines).toMatch(/height:\s*1px;/);
    expect(lines).toMatch(/background:\s*var\(--wuu-hairline\);/);
    expect(lines).not.toMatch(/linear-gradient|transparent/);
  });

  it("keeps the notice text bare — tone colors the text, never a fill", () => {
    const content = cssRuleBody(".turn-event-content");
    expect(content).not.toMatch(/\bbackground\s*:/);
    expect(content).not.toMatch(/border-radius/);
    // Tone variants only recolor text; no tinted chip backgrounds.
    expect(cssRuleBody(".turn-notice.error .turn-event-content")).not.toMatch(/\bbackground\s*:/);
    // The machine-code suffix stays in the neutral gray family.
    expect(cssRuleBody(".turn-event-code")).toMatch(/color:\s*var\(--ink-muted\);/);
  });
});
