import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const conversationShellCSS = readFileSync(
  resolve(__dirname, "styles/conversation-shell.css"),
  "utf-8",
);

function cssRule(selector: string): string {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = conversationShellCSS.match(
    new RegExp(`^${escapedSelector}\\s*\\{([\\s\\S]*?)\\n\\}`, "m"),
  );
  if (!match) {
    throw new Error(`missing CSS rule for ${selector}`);
  }
  return match[1] ?? "";
}

describe("conversation titlebar layout", () => {
  it("keeps a new conversation aligned with the standard 48px window chrome", () => {
    expect(cssRule(".conversation-pane")).toMatch(
      /grid-template-rows:\s*48px\s+minmax\(0,\s*1fr\);/,
    );
  });
});
