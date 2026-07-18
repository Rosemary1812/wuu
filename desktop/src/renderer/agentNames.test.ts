import { describe, expect, it } from "vitest";
import {
  AGENT_NAMES,
  type AgentNameCategory,
  agentNameForSubagentID,
  fnv1aUTF8,
  selectAgentName,
} from "./agentNames";

const EXPECTED_CATEGORY_COUNTS: Record<AgentNameCategory, number> = {
  "water-margin": 16,
  "three-body": 16,
  tarot: 22,
  zodiac: 12,
  scientist: 15,
  mathematician: 15,
};

describe("agentNames pool", () => {
  it("ships the agreed 96 names with exact category quotas", () => {
    expect(AGENT_NAMES).toHaveLength(96);

    const counts = Object.fromEntries(
      Object.keys(EXPECTED_CATEGORY_COUNTS).map((category) => [
        category,
        AGENT_NAMES.filter((entry) => entry.category === category).length,
      ]),
    );
    expect(counts).toEqual(EXPECTED_CATEGORY_COUNTS);
  });

  it("keeps every display identity complete, short, and globally unique", () => {
    const seen = new Set<string>();
    for (const entry of AGENT_NAMES) {
      expect(entry.displayName.trim().length).toBeGreaterThan(0);
      expect(entry.displayName.length).toBeLessThanOrEqual(4);
      expect(entry.category.trim().length).toBeGreaterThan(0);
      expect(entry.source.trim().length).toBeGreaterThan(0);
      if (entry.secondaryName !== undefined) {
        expect(entry.secondaryName.trim().length).toBeGreaterThan(0);
      }
      seen.add(entry.displayName);
    }
    expect(seen.size).toBe(AGENT_NAMES.length);
  });

  it("keeps zodiac display names free of the 座 suffix", () => {
    const zodiacNames = AGENT_NAMES.filter((entry) => entry.category === "zodiac");
    expect(zodiacNames).toHaveLength(12);
    expect(zodiacNames.every((entry) => !entry.displayName.endsWith("座"))).toBe(true);
  });
});

describe("fnv1aUTF8", () => {
  it("matches known ASCII and UTF-8 vectors", () => {
    expect(fnv1aUTF8("")).toBe(0x811c9dc5);
    expect(fnv1aUTF8("hello")).toBe(0x4f9f2cab);
    expect(fnv1aUTF8("智子")).toBe(0x7f1ca64c);
  });
});

describe("agentNameForSubagentID", () => {
  it("returns the same entry for the same id across calls", () => {
    const first = agentNameForSubagentID("agent-aBcDeFgHiJ12345");
    const second = agentNameForSubagentID("agent-aBcDeFgHiJ12345");
    expect(first).toEqual(second);
  });

  it("keeps established id mappings stable across releases", () => {
    expect(agentNameForSubagentID("agent-aBcDeFgHiJ12345").displayName).toBe("天秤");
    expect(agentNameForSubagentID("agent-0").displayName).toBe("薛定谔");
    expect(agentNameForSubagentID("agent-17").displayName).toBe("巨蟹");
    expect(agentNameForSubagentID("agent-63").displayName).toBe("居里夫人");
  });

  it("exercises multiple names across different ids", () => {
    const seen = new Set<string>();
    for (let i = 0; i < 64; i += 1) {
      seen.add(agentNameForSubagentID(`agent-${i}`).displayName);
    }
    expect(seen.size).toBeGreaterThan(1);
  });

  it("handles empty input deterministically", () => {
    expect(agentNameForSubagentID("")).toEqual(agentNameForSubagentID("agent"));
  });

  it("does not depend on candidate array order", () => {
    const reversed = [...AGENT_NAMES].reverse();
    for (let i = 0; i < 128; i += 1) {
      expect(selectAgentName(`agent-${i}`, reversed)).toEqual(
        selectAgentName(`agent-${i}`, AGENT_NAMES),
      );
    }
  });

  it("only keeps an old mapping or moves it to an appended entry", () => {
    const oldPool = AGENT_NAMES.slice(0, -1);
    const appended = AGENT_NAMES.at(-1);
    expect(appended).toBeDefined();

    let moved = 0;
    for (let i = 0; i < 1024; i += 1) {
      const before = selectAgentName(`agent-${i}`, oldPool);
      const after = selectAgentName(`agent-${i}`, AGENT_NAMES);
      expect(after === before || after === appended).toBe(true);
      if (after === appended) {
        moved += 1;
      }
    }
    expect(moved).toBeGreaterThan(0);
  });
});
