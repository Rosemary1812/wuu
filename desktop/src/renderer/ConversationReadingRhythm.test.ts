import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import {
  CONVERSATION_DESIGN_TOKENS,
  CONVERSATION_READING_LINE_HEIGHT_CSS_VAR,
} from "./ConversationDesignTokens";

const conversationShellCSS = readFileSync(
  resolve(process.cwd(), "src/renderer/styles/conversation-shell.css"),
  "utf8",
);

const turnsCSS = readFileSync(
  resolve(process.cwd(), "src/renderer/styles/turns.css"),
  "utf8",
);
const readingLineHeightVarPattern = CONVERSATION_READING_LINE_HEIGHT_CSS_VAR.replace(
  /[-/\\^$*+?.()|[\]{}]/g,
  "\\$&",
);
const readingToken = CONVERSATION_DESIGN_TOKENS.find(
  (token) => token.cssVar === CONVERSATION_READING_LINE_HEIGHT_CSS_VAR,
);

describe("conversation reading rhythm", () => {
  it("uses a visibly relaxed default line height for message prose", () => {
    expect(conversationShellCSS).toContain(
      `${CONVERSATION_READING_LINE_HEIGHT_CSS_VAR}: ${readingToken?.defaultValue}`,
    );
    expect(conversationShellCSS).not.toContain("--conversation-prose-line-height:");
  });

  it("routes message, commentary, and markdown text through the new reading token", () => {
    expect(turnsCSS).toContain(
      `line-height: var(${CONVERSATION_READING_LINE_HEIGHT_CSS_VAR})`,
    );
    expect(turnsCSS).toMatch(
      new RegExp(
        `\\.user-message-raw-query\\s*\\{[\\s\\S]*?line-height:\\s*var\\(${readingLineHeightVarPattern}\\)`,
      ),
    );
    expect(turnsCSS).toContain(
      `line-height: var(${CONVERSATION_READING_LINE_HEIGHT_CSS_VAR}, var(--line-body))`,
    );
    expect(turnsCSS).not.toContain("var(--conversation-prose-line-height");
  });
});
