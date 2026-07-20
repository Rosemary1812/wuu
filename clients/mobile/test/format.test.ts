import { describe, expect, it } from "vitest";

import { formatListTimestamp } from "../src/lib/format";

describe("formatListTimestamp", () => {
  const now = new Date("2026-07-07T20:30:00");

  it("today shows time, yesterday shows 昨天, within a week shows weekday", () => {
    expect(formatListTimestamp("2026-07-07T09:05:00", now)).toBe("09:05");
    expect(formatListTimestamp("2026-07-06T23:00:00", now)).toBe("昨天");
    expect(formatListTimestamp("2026-07-03T08:00:00", now)).toBe("周五");
  });

  it("older dates show month/day, other years show full date", () => {
    expect(formatListTimestamp("2026-06-01T08:00:00", now)).toBe("6月1日");
    expect(formatListTimestamp("2025-12-31T08:00:00", now)).toBe("2025/12/31");
  });

  it("handles garbage safely", () => {
    expect(formatListTimestamp(undefined, now)).toBe("");
    expect(formatListTimestamp("not-a-date", now)).toBe("");
  });
});
