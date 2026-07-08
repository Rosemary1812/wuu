import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const conversationShellCss = readFileSync(
  resolve(__dirname, "conversation-shell.css"),
  "utf-8",
);

function cssRuleBody(selector: string): string {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = conversationShellCss.match(
    new RegExp(`(?:^|\\})\\s*${escapedSelector}\\s*\\{([\\s\\S]*?)\\}`),
  );
  if (!match) {
    throw new Error(`missing CSS rule for ${selector}`);
  }
  return match[1];
}

describe("conversation shell visible center", () => {
  it("shares the environment-panel inset used by centered floating chrome", () => {
    expect(cssRuleBody(".conversation-pane")).toMatch(
      /--conversation-visible-inset-right:\s*0px;/,
    );
    expect(cssRuleBody(".conversation-pane.environment-panel-reserved")).toMatch(
      /--conversation-visible-inset-right:\s*var\(--environment-panel-reserved-width\);/,
    );
    expect(cssRuleBody(".jump-to-latest-cluster")).toMatch(
      /right:\s*var\(--conversation-visible-inset-right\);/,
    );
  });

  it("does not redeclare --thread-panel-width on the pane so it inherits the live drag value", () => {
    // Regression (2026-07-06): a local `--thread-panel-width` on
    // .conversation-pane shadowed the value App.tsx live-writes to .app-shell
    // during a separator drag, pinning the grid column at 372px so the panel
    // never resized. The pane must INHERIT it — never declare it locally.
    expect(cssRuleBody(".conversation-pane")).not.toMatch(/--thread-panel-width\s*:/);
    // The panel column still reads it, with a first-open fallback of 372px.
    expect(cssRuleBody(".conversation-pane.subthread-panel-visible")).toMatch(
      /grid-template-columns:\s*minmax\(0,\s*1fr\)\s*var\(--thread-panel-width,\s*372px\);/,
    );
  });
});

describe("conversation shell message typography tokens", () => {
  it("keeps message and process typography on the Codex-style two-token scale", () => {
    const body = cssRuleBody(".conversation-pane");

    expect(body).toMatch(/"SF Pro Text"/);
    expect(body).toMatch(/--conversation-message-font-size:\s*14px;/);
    expect(body).toMatch(/--conversation-message-font-weight:\s*445;/);
    expect(body).toMatch(
      /--conversation-process-font-size:\s*var\(--conversation-message-font-size\);/,
    );
    expect(body).toMatch(
      /--conversation-process-font-weight:\s*var\(--conversation-message-font-weight\);/,
    );
    expect(body).toMatch(/--conversation-process-line-height:\s*20px;/);
    expect(body).toMatch(/--conversation-user-rule-gap:\s*24px;/);
    expect(body).toMatch(/--conversation-user-message-trailing-gap:\s*8px;/);
    expect(body).toMatch(/--conversation-rule-process-gap:\s*18px;/);
    expect(body).toMatch(/--conversation-process-detail-gap:\s*18px;/);
    expect(body).toMatch(/--conversation-process-answer-gap:\s*25px;/);
    expect(body).toMatch(/--conversation-answer-action-gap:\s*8px;/);
    expect(body).toMatch(/--conversation-answer-hover-action-gap:\s*6px;/);
  });
});
