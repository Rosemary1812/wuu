import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const chatCss = readFileSync(resolve(__dirname, "chat.css"), "utf-8");

function cssRuleBody(selector: string): string {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = chatCss.match(
    new RegExp(`(?:^|\\})\\s*${escapedSelector}\\s*\\{([\\s\\S]*?)\\}`),
  );
  if (!match) {
    throw new Error(`missing CSS rule for ${selector}`);
  }
  return match[1];
}

describe("chat.css avatar status", () => {
  it("keeps the status dot outside the clipped avatar face", () => {
    expect(cssRuleBody(".chat-avatar")).not.toMatch(/overflow:\s*hidden;/);
    expect(cssRuleBody(".chat-avatar-face")).toMatch(/overflow:\s*hidden;/);
    expect(cssRuleBody(".chat-avatar-status")).toMatch(/top:\s*-1px;/);
    expect(cssRuleBody(".chat-avatar-status")).toMatch(/right:\s*-1px;/);
  });

  it("shows the status card on avatar hover", () => {
    expect(cssRuleBody(".chat-avatar-status-card")).toMatch(/opacity:\s*0;/);
    expect(cssRuleBody(".chat-avatar:hover .chat-avatar-status-card")).toMatch(
      /opacity:\s*1;/,
    );
  });
});
