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

function lastCssRuleBody(selector: string): string {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const matches = [
    ...turnsCss.matchAll(
      new RegExp(`(?:^|\\})\\s*${escapedSelector}\\s*\\{([\\s\\S]*?)\\}`, "g"),
    ),
  ];
  if (matches.length === 0) {
    throw new Error(`missing CSS rule for ${selector}`);
  }
  return matches[matches.length - 1][1];
}

function cssRuleCount(selector: string): number {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return [
    ...turnsCss.matchAll(
      new RegExp(`(?:^|\\})\\s*${escapedSelector}\\s*\\{`, "g"),
    ),
  ].length;
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

describe("turns.css streaming block wrappers", () => {
  it("keeps the prose block gap alive inside stable streaming blocks", () => {
    // A stable block can hold several markdown siblings (paragraph +
    // list with no blank line between). The wrapper must mirror the
    // root .rich-content gap or those siblings collapse to 0px.
    expect(turnsCss).toMatch(
      /\.streaming-markdown-block\s*\{[\s\S]*?display:\s*flex;[\s\S]*?flex-direction:\s*column;[\s\S]*?gap:\s*var\(--conversation-prose-block-gap/,
    );
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

describe("turns.css message-flow typography", () => {
  it("keeps the process fold selectors in one source-of-truth block", () => {
    for (const selector of [
      ".turn-process-title",
      ".turn-process-meta",
      ".turn-process-preview",
      ".turn-process-entry",
      ".turn-process-entry-commentary",
    ]) {
      expect(cssRuleCount(selector)).toBe(1);
    }
  });

  it("names every message-flow spacing landmark with a dedicated token", () => {
    expect(lastCssRuleBody(".turn > .assistant-turn-shell")).toMatch(
      /margin-top:\s*calc\(\s*var\(--conversation-user-rule-gap\)\s*-\s*var\(--conversation-turn-gap\)\s*-\s*var\(--conversation-user-message-trailing-gap\)\s*\);/,
    );
    expect(lastCssRuleBody(".turn > .assistant-turn-shell")).toMatch(
      /padding-top:\s*var\(--conversation-rule-process-gap\);/,
    );
    expect(lastCssRuleBody(".user-message-block")).toMatch(
      /margin-bottom:\s*var\(--conversation-user-message-trailing-gap\);/,
    );
    expect(lastCssRuleBody(".turn-process-fold-body")).toMatch(
      /padding:\s*var\(--conversation-process-detail-gap\)\s*0\s*0\s*0;/,
    );
    expect(lastCssRuleBody(".turn-process-fold-body-inner")).toMatch(
      /gap:\s*var\(--conversation-process-detail-gap\);/,
    );
    expect(lastCssRuleBody(".assistant-turn-shell")).toMatch(
      /gap:\s*var\(--conversation-process-answer-gap\);/,
    );
    expect(lastCssRuleBody(".agent-block-with-action-slot")).toMatch(
      /gap:\s*var\(--conversation-answer-action-gap\);/,
    );
    expect(turnsCss).toMatch(
      /\.agent-actions-overlay\s*>\s*\.agent-message-actions\s*\{[\s\S]*?padding-top:\s*var\(--conversation-answer-hover-action-gap\);/,
    );
  });

  it("uses the activity rhythm when commentary introduces the next tool row", () => {
    expect(turnsCss).toMatch(
      /\.turn-process-entry-commentary\s*\+\s*\.turn-process-entry-activity,\s*\.turn-process-entry-commentary\s*\+\s*\.turn-process-entry-process_group\s*\{[\s\S]*?margin-top:\s*calc\(\s*var\(--conversation-activity-gap\)\s*-\s*var\(--conversation-process-detail-gap\)\s*\);/,
    );
  });

  it("uses the same process token for status headers, tool rows, and reasoning summaries", () => {
    for (const selector of [
      ".turn-process-title",
      ".turn-process-meta",
      ".turn-process-preview",
      ".activity-row",
      ".process-surface-row",
      ".turn-reasoning-summary",
    ]) {
      const body = lastCssRuleBody(selector);
      expect(body).toMatch(/font-size:\s*var\(--conversation-process-font-size\);/);
      expect(body).toMatch(/font-weight:\s*var\(--conversation-process-font-weight\);/);
      expect(body).toMatch(/line-height:\s*var\(--conversation-process-line-height\);/);
    }
  });

  it("uses the measured process-to-answer gap instead of the generic process gap", () => {
    expect(lastCssRuleBody(".assistant-turn-shell")).toMatch(
      /gap:\s*var\(--conversation-process-answer-gap\);/,
    );
  });

  it("keeps answer and commentary prose on the shared message weight", () => {
    expect(lastCssRuleBody(".turn")).toMatch(
      /font-weight:\s*var\(--conversation-message-font-weight\);/,
    );
    expect(lastCssRuleBody(".turn-process-entry-commentary")).toMatch(
      /font-weight:\s*var\(--conversation-message-font-weight\);/,
    );
  });
});
