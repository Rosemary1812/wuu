import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const turnsCss = readFileSync(resolve(__dirname, "turns.css"), "utf-8");

describe("turns.css user message actions", () => {
  it("keeps action buttons aligned to the user bubble edge", () => {
    expect(turnsCss).toMatch(
      /\.user-message-actions\s*\{[\s\S]*?justify-self:\s*end;[\s\S]*?justify-content:\s*flex-end;[\s\S]*?width:\s*max-content;/,
    );
  });
});
