import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const conversationShellCSS = readFileSync(
  resolve(process.cwd(), "src/renderer/styles/conversation-shell.css"),
  "utf8",
);

const turnsCSS = readFileSync(
  resolve(process.cwd(), "src/renderer/styles/turns.css"),
  "utf8",
);

describe("conversation reading rhythm", () => {
  it("uses a visibly relaxed default line height for message prose", () => {
    expect(conversationShellCSS).toMatch(/--conversation-reading-line-height:\s*1\.9/);
    expect(conversationShellCSS).not.toContain("--conversation-prose-line-height:");
  });

  it("routes message, commentary, and markdown text through the new reading token", () => {
    expect(turnsCSS).toContain("line-height: var(--conversation-reading-line-height)");
    expect(turnsCSS).toMatch(
      /\.user-message-raw-query\s*\{[\s\S]*?line-height:\s*var\(--conversation-reading-line-height\)/,
    );
    expect(turnsCSS).toContain(
      "line-height: var(--conversation-reading-line-height, var(--line-body))",
    );
    expect(turnsCSS).not.toContain("var(--conversation-prose-line-height");
  });
});
