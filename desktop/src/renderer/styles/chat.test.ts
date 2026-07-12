import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const chatCss = readFileSync(resolve(__dirname, "chat.css"), "utf-8");
const chatCssRules = chatCss.replace(/\/\*[\s\S]*?\*\//g, "");

function cssRuleBody(selector: string): string {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = chatCssRules.match(
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

describe("chat.css inline divider", () => {
  it("provides the shared line-flanked chat notice shape", () => {
    expect(chatCss).toMatch(
      /\.chat-inline-divider\s*\{[\s\S]*display:\s*flex;[\s\S]*align-items:\s*center;/,
    );
    expect(chatCss).toMatch(
      /\.chat-inline-divider::before,\s*\.chat-inline-divider::after\s*\{[\s\S]*flex:\s*1 1 auto;[\s\S]*height:\s*1px;/,
    );
    expect(
      cssRuleBody(".chat-inline-divider::before,\n.chat-inline-divider::after"),
    ).toMatch(/background:\s*var\(--wuu-hairline\);/);
  });
});

describe("chat.css bubble containment", () => {
  it("lets the bubble wrap unbreakable inline tokens so text never pierces its rounded background", () => {
    // Regression for the "long error chain pierces the chat bubble" bug:
    // a synthesized user_message containing paths like
    // `internal/appserver/turn_handlers.go:2452` or JSON-ish tags like
    // `<subagent_notification>` used to paint past the bubble's right
    // rounded corner because the bubble had no `overflow-wrap` rule.
    // `anywhere` is preferred over `break-word` because it lets the line
    // break at any character when no softer break point exists; the
    // companion `word-break: break-word` keeps older WebKit renderers
    // honest. This test pins the rule so a future refactor of `.chat-bubble`
    // can't silently drop the containment again.
    const bubble = cssRuleBody(".chat-bubble");
    expect(bubble).toMatch(/overflow-wrap:\s*anywhere;/);
    expect(bubble).toMatch(/word-break:\s*break-word;/);
    // The flex chain above the bubble (`.chat-bubble-group`) must stay
    // shrinkable, otherwise the max-width on the group is meaningless and
    // long unbreakable content can still push the column past the chat
    // pane. Pinning `min-width: 0` here keeps the regression scoped.
    const group = cssRuleBody(".chat-bubble-group");
    expect(group).toMatch(/min-width:\s*0;/);
  });
});

describe("chat.css reaction picker", () => {
  it("opens downward from the first visible bubble so the picker is not clipped", () => {
    const toolbar = cssRuleBody(".chat-row--top-bubble .chat-bubble-toolbar");
    expect(toolbar).toMatch(/bottom:\s*auto;/);
    expect(toolbar).toMatch(/top:\s*calc\(100%\s*-\s*4px\);/);

    const picker = cssRuleBody(".chat-row--top-bubble .chat-reaction-picker");
    expect(picker).toMatch(/top:\s*calc\(100%\s*\+\s*6px\);/);
    expect(picker).toMatch(/bottom:\s*auto;/);
    expect(picker).not.toMatch(/bottom:\s*calc/);
  });

  it("does not draw a gray rounded hover backplate inside sticker art", () => {
    const option = cssRuleBody(".chat-bubble-toolbar .chat-reaction-picker-option");
    expect(option).toMatch(/background:\s*transparent;/);

    const hover = cssRuleBody(
      ".chat-bubble-toolbar .chat-reaction-picker-option:hover,\n.chat-bubble-toolbar .chat-reaction-picker-option:focus-visible",
    );
    expect(hover).toMatch(/background:\s*transparent;/);
    expect(hover).not.toMatch(/var\(--menu-hover\)/);
  });
});

describe("chat.css bubble long-text collapse", () => {
  it("turns the bubble into a column flex so the toggle stacks under the preview", () => {
    // Long-card variant of `.chat-bubble`. The base bubble is a plain
    // block; once `chat-bubble--long-card` is on, the raw-query div and
    // the expand toggle stack as flex children so `align-self` on the
    // toggle actually lands it on the bubble's "empty" side. Without
    // `min-width: 0` the flex track would inherit the bubble's
    // `min-content` width and the longest unbreakable token in the
    // preview would push the column past the bubble's rounded edge
    // again — same bug class as the recent fix on `.chat-bubble`
    // itself.
    const card = cssRuleBody(".chat-bubble--long-card");
    expect(card).toMatch(/display:\s*flex;/);
    expect(card).toMatch(/flex-direction:\s*column;/);
    expect(card).toMatch(/min-width:\s*0;/);
  });

  it("wraps unbreakable tokens in the preview so a long-file-path never escapes the bubble", () => {
    // The preview replaces `<RichContent>` with a pre-wrap text node,
    // so the same overflow rules from the recent `.chat-bubble` fix
    // have to land here too. Without `overflow-wrap: anywhere` a path
    // like `internal/appserver/turn_handlers.go:2452` would still
    // pierce the bubble's right edge while the card is folded.
    const raw = cssRuleBody(".chat-bubble-raw-query");
    expect(raw).toMatch(/white-space:\s*pre-wrap;/);
    expect(raw).toMatch(/overflow-wrap:\s*anywhere;/);
    expect(raw).toMatch(/word-break:\s*break-word;/);
  });

  it("aligns the expand toggle to the bubble's empty edge (right for user, left for participant)", () => {
    // The toggle is a flex child of the long-card column, so
    // `align-self` is what lands it flush against the bubble's tail
    // corner. Reversed values would push the toggle into the avatar
    // column on participant bubbles or crowd the corner on outgoing
    // user bubbles — both would read as "the button is in the wrong
    // place" and break the chat-style rhythm.
    const userToggle = cssRuleBody(".chat-row--user .chat-bubble-expand-toggle");
    expect(userToggle).toMatch(/align-self:\s*flex-end;/);
    const participantToggle = cssRuleBody(
      ".chat-row--participant .chat-bubble-expand-toggle",
    );
    expect(participantToggle).toMatch(/align-self:\s*flex-start;/);
  });

  it("styles the toggle as a quiet ghost button that brightens on hover/focus", () => {
    // Mirror the visual treatment of `.user-message-expand-toggle` in
    // turns.css so the affordance reads as the same control across
    // surfaces — quiet by default, ink-muted text, ink on
    // hover/focus, transparent background so the bubble's color
    // shows through.
    const toggle = cssRuleBody(".chat-bubble-expand-toggle");
    expect(toggle).toMatch(/background:\s*transparent;/);
    expect(toggle).toMatch(/border:\s*0;/);
    expect(toggle).toMatch(/color:\s*var\(--ink-muted\);/);
    expect(toggle).toMatch(/cursor:\s*pointer;/);
    const hover = cssRuleBody(
      ".chat-bubble-expand-toggle:hover,\n.chat-bubble-expand-toggle:focus-visible",
    );
    expect(hover).toMatch(/color:\s*var\(--ink\);/);
  });
});
