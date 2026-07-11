import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const conversationShellCss = readFileSync(
  resolve(__dirname, "conversation-shell.css"),
  "utf-8",
);

// Strip /* ... */ blocks before matching selectors so a `:root` rule
// preceded by a CSS file-header comment still matches `^:root`. The
// inner `{` of comments would otherwise be an obstacle.
const conversationShellCssNoComments = conversationShellCss.replace(
  /\/\*[\s\S]*?\*\//g,
  "",
);

function cssRuleBody(selector: string): string {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = conversationShellCssNoComments.match(
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
  it("keeps process typography on the Codex-style two-token scale", () => {
    // The body / process-font-size and process-font-weight aliases
    // intentionally stay in .conversation-pane — they reference the
    // message tokens which are owned at :root for the preview use case.
    const body = cssRuleBody(".conversation-pane");

    expect(body).toMatch(
      /--conversation-process-font-size:\s*var\(--conversation-message-font-size\);/,
    );
    expect(body).toMatch(
      /--conversation-process-font-weight:\s*var\(--conversation-message-font-weight\);/,
    );
    expect(body).toMatch(/--conversation-process-line-height:\s*20px;/);
    expect(body).toMatch(/--conversation-user-rule-gap:\s*24px;/);
    expect(body).toMatch(/--conversation-user-message-trailing-gap:\s*8px;/);
    expect(body).toMatch(/--conversation-rule-process-gap:\s*16px;/);
    expect(body).toMatch(/--conversation-process-detail-gap:\s*16px;/);
    expect(body).toMatch(/--conversation-process-answer-gap:\s*16px;/);
    expect(body).toMatch(/--conversation-answer-action-gap:\s*12px;/);
    expect(body).toMatch(/--conversation-answer-hover-action-gap:\s*6px;/);
  });

  it("owns the message-font-size default on :root so the user setting on <html> cascades", () => {
    // Regression (2026-07-11, message-flow font-size setting): the user
    // facing setting writes `--conversation-message-font-size` to <html
    // style="...">. A more specific declaration on .conversation-pane
    // would shadow that override and pin the size at 14px. The default
    // must live on :root; .conversation-pane must NOT redeclare it.
    const root = cssRuleBody(":root");
    expect(root).toMatch(/--conversation-message-font-size:\s*14px;/);

    const body = cssRuleBody(".conversation-pane");
    expect(body).not.toMatch(/--conversation-message-font-size\s*:/);
  });

  it("owns the message font-family and font-weight at :root so the preview outside .conversation-pane inherits the same values", () => {
    // Companion regression (2026-07-11): the settings-page slider
    // shows a live .message-flow-preview rendered OUTSIDE
    // .conversation-pane. font-family and font-weight must therefore
    // live at :root (or a selector with lower specificity than the
    // pane) so the preview reads them via standard CSS inheritance
    // rather than via a hard-coded redeclaration.
    const root = cssRuleBody(":root");
    expect(root).toMatch(/"SF Pro Text"/);
    expect(root).toMatch(/--conversation-message-font-weight:\s*445;/);

    const body = cssRuleBody(".conversation-pane");
    expect(body).not.toMatch(/--conversation-message-font-family\s*:/);
    expect(body).not.toMatch(/--conversation-message-font-weight\s*:/);
  });
});

describe("conversation shell message-flow preview", () => {
  it("renders the preview block with the live message-flow typography", () => {
    // Companion regression (2026-07-11, message-flow font-size setting):
    // the slider in Settings → 外观 → 消息流字号 shows a sample line
    // below so the user can read the chosen size in prose. The
    // .message-flow-preview class must read
    // --conversation-message-font-size (the same var the slider
    // writes) so the preview and the actual stream stay in sync.
    expect(conversationShellCssNoComments).toMatch(
      /\.message-flow-preview\s*\{[^}]*var\(--conversation-message-font-size\)/,
    );
  });

  it("renders paragraphs with the live line-height and inter-paragraph gap", () => {
    // Same idea: the preview's spacing rhythm must mirror the
    // conversation pane's, otherwise the slider's chosen size wouldn't
    // actually preview accurately.
    expect(conversationShellCssNoComments).toMatch(
      /\.message-flow-preview[^}]*var\(--conversation-reading-line-height\)/,
    );
    expect(conversationShellCssNoComments).toMatch(
      /\.message-flow-preview[^}]*var\(--conversation-message-max-width/,
    );
  });
});
