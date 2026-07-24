import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const sideThreadCss = readFileSync(resolve(__dirname, "side-thread.css"), "utf-8");

describe("side thread message-flow spacing", () => {
  it("keeps its compact top inset without overriding the shared horizontal flow inset", () => {
    const conversationRule = sideThreadCss.match(
      /\.side-thread-panel__body\s+\.side-thread-panel__conversation\s*\{([^}]*)\}/,
    )?.[1];

    expect(conversationRule).toMatch(/padding-block:\s*4px\s+0;/);
    expect(sideThreadCss).toMatch(
      /\.side-thread-panel__body\s*\{[\s\S]*?padding:\s*12px\s+0\s+calc\(var\(--side-thread-footer-height, 0px\) \+ 12px\);/,
    );
    expect(conversationRule).not.toMatch(/(?:^|\s)padding(?:-inline)?:/);
    expect(sideThreadCss).not.toMatch(
      /(?:^|\})\s*\.side-thread-panel__conversation\s*\{/,
    );
  });

  it("runs the scroll surface to the bottom behind a floating composer", () => {
    expect(sideThreadCss).toMatch(
      /\.side-thread-panel\s*\{[\s\S]*?grid-template-rows:\s*48px\s+minmax\(0, 1fr\);/,
    );
    expect(sideThreadCss).toMatch(
      /\.side-thread-panel__body\s*\{[\s\S]*?grid-column:\s*1;[\s\S]*?grid-row:\s*2;[\s\S]*?overflow-y:\s*auto;/,
    );
    expect(sideThreadCss).toMatch(
      /\.side-thread-panel__footer\s*\{[\s\S]*?grid-column:\s*1;[\s\S]*?grid-row:\s*2;[\s\S]*?align-self:\s*end;/,
    );
    expect(sideThreadCss).not.toMatch(
      /\.side-thread-panel__composer-host\s+\.dock-composer-wrap::before\s*\{[^}]*display:\s*none/,
    );
  });
});
