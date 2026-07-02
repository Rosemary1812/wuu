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
    expect(cssRuleBody(".jump-to-latest-pill")).toMatch(
      /right:\s*var\(--conversation-visible-inset-right\);/,
    );
  });
});
