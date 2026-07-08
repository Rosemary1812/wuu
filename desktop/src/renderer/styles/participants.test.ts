import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const participantsCss = readFileSync(resolve(__dirname, "participants.css"), "utf-8");

function cssRuleBody(selector: string): string {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = participantsCss.match(
    new RegExp(`(?:^|\\})\\s*${escapedSelector}\\s*\\{([\\s\\S]*?)\\}`),
  );
  if (!match) {
    throw new Error(`missing CSS rule for ${selector}`);
  }
  return match[1];
}

describe("participants.css roster status", () => {
  it("uses the same neutral idle dot as session rows for online agents", () => {
    const online = cssRuleBody('.participant-roster-status[data-status="online"]');

    expect(online).toMatch(/background:\s*var\(--sidebar-thread-row-dot-default\)/);
    expect(online).not.toMatch(/var\(--success\)/);
  });
});
