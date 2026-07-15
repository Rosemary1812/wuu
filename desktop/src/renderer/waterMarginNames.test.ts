import { describe, expect, it } from "vitest";
import {
  WATER_MARGIN_HEROES,
  nicknameForSubagentID,
} from "./waterMarginNames";

describe("waterMarginNames pool", () => {
  it("ships at least one entry per subagent — collision space is the pool size", () => {
    expect(WATER_MARGIN_HEROES.length).toBeGreaterThanOrEqual(20);
  });

  it("every entry has a non-empty nickname and real name", () => {
    for (const entry of WATER_MARGIN_HEROES) {
      expect(entry.nickname.trim().length).toBeGreaterThan(0);
      expect(entry.realName.trim().length).toBeGreaterThan(0);
    }
  });

  it("nicknames are unique across the pool", () => {
    const seen = new Set<string>();
    for (const entry of WATER_MARGIN_HEROES) {
      seen.add(entry.nickname);
    }
    expect(seen.size).toBe(WATER_MARGIN_HEROES.length);
  });
});

describe("nicknameForSubagentID", () => {
  it("returns the same entry for the same id across calls", () => {
    const first = nicknameForSubagentID("agent-aBcDeFgHiJ12345");
    const second = nicknameForSubagentID("agent-aBcDeFgHiJ12345");
    expect(first).toEqual(second);
  });

  it("does not return the agentID itself (a real nickname was assigned)", () => {
    const result = nicknameForSubagentID("agent-aBcDeFgHiJ12345");
    expect(result.nickname).not.toBe("agent-aBcDeFgHiJ12345");
    expect(result.realName).not.toBe("agent-aBcDeFgHiJ12345");
  });

  it("exercises multiple nicknames across different ids", () => {
    // We are not trying to prove uniform distribution — birthday math says
    // collisions are common when the pool is small. This test just guards
    // against an accidental "always return the same entry" bug.
    const seen = new Set<string>();
    for (let i = 0; i < 64; i += 1) {
      seen.add(nicknameForSubagentID(`agent-${i}`).nickname);
    }
    expect(seen.size).toBeGreaterThan(1);
  });

  it("falls back gracefully on empty input instead of throwing", () => {
    const result = nicknameForSubagentID("");
    expect(result.nickname.length).toBeGreaterThan(0);
    expect(result.realName.length).toBeGreaterThan(0);
  });
});
