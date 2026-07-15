import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const sideThreadCss = readFileSync(resolve(__dirname, "side-thread.css"), "utf-8");

describe("side thread message-flow spacing", () => {
  it("keeps its compact top inset more specific than the shared conversation flow", () => {
    expect(sideThreadCss).toMatch(
      /\.side-thread-panel__body\s+\.side-thread-panel__conversation\s*\{[\s\S]*?padding:\s*4px\s+0\s+0;/,
    );
    expect(sideThreadCss).not.toMatch(
      /(?:^|\})\s*\.side-thread-panel__conversation\s*\{/,
    );
  });
});
