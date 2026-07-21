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
    expect(sideThreadCss).toMatch(/\.side-thread-panel__body\s*\{[\s\S]*?padding:\s*12px\s+0\s+8px;/);
    expect(conversationRule).not.toMatch(/(?:^|\s)padding(?:-inline)?:/);
    expect(sideThreadCss).not.toMatch(
      /(?:^|\})\s*\.side-thread-panel__conversation\s*\{/,
    );
  });
});
