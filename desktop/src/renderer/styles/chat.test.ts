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
