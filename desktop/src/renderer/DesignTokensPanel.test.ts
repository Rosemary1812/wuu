import { describe, expect, it } from "vitest";
import {
  CONVERSATION_DESIGN_TOKENS,
  CONVERSATION_DESIGN_TOKEN_STORAGE_KEY,
  CONVERSATION_READING_LINE_HEIGHT_CSS_VAR,
  LEGACY_CONVERSATION_DESIGN_TOKEN_CSS_VARS,
  LEGACY_CONVERSATION_DESIGN_TOKEN_STORAGE_KEYS,
} from "./ConversationDesignTokens";

const activeCssVars = CONVERSATION_DESIGN_TOKENS.map((token) => token.cssVar);
const legacyCssVars = new Set<string>(LEGACY_CONVERSATION_DESIGN_TOKEN_CSS_VARS);

describe("conversation design token registry", () => {
  it("keeps token keys and active CSS variables unique", () => {
    const activeKeys = CONVERSATION_DESIGN_TOKENS.map((token) => token.key);

    expect(new Set(activeKeys).size).toBe(activeKeys.length);
    expect(new Set(activeCssVars).size).toBe(activeCssVars.length);
  });

  it("writes session layout variables used by the live conversation flow", () => {
    expect(activeCssVars).toContain("--session-outer-width");
    expect(activeCssVars).toContain("--session-outer-padding-inline");
    expect(activeCssVars).toContain("--session-composer-width");
    expect(activeCssVars).toContain("--session-composer-radius");
  });

  it("does not write legacy conversation layout variables", () => {
    const activeLegacyOverlap = activeCssVars.filter((cssVar) =>
      legacyCssVars.has(cssVar),
    );

    expect(activeLegacyOverlap).toEqual([]);
  });

  it("uses a fresh storage namespace for the relaxed reading rhythm", () => {
    const readingToken = CONVERSATION_DESIGN_TOKENS.find(
      (token) => token.cssVar === CONVERSATION_READING_LINE_HEIGHT_CSS_VAR,
    );
    expect(CONVERSATION_DESIGN_TOKEN_STORAGE_KEY).toBe("wuu:design-tokens:v4");
    expect(LEGACY_CONVERSATION_DESIGN_TOKEN_STORAGE_KEYS).not.toContain(
      CONVERSATION_DESIGN_TOKEN_STORAGE_KEY,
    );
    expect(readingToken?.defaultValue).toBe(1.75);
  });

  it("cleans stale inline variables that used to override the reading rhythm", () => {
    expect(LEGACY_CONVERSATION_DESIGN_TOKEN_STORAGE_KEYS).toContain(
      "wuu:design-tokens:v2",
    );
    expect(LEGACY_CONVERSATION_DESIGN_TOKEN_STORAGE_KEYS).toContain(
      "wuu:design-tokens:v3",
    );
    expect(LEGACY_CONVERSATION_DESIGN_TOKEN_CSS_VARS).toContain(
      "--conversation-prose-line-height",
    );
    expect(activeCssVars).toContain(CONVERSATION_READING_LINE_HEIGHT_CSS_VAR);
    expect(activeCssVars).not.toContain("--conversation-prose-line-height");
  });
});
