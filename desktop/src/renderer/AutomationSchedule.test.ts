import { describe, expect, it } from "vitest";
import {
  cronForAutomationSchedule,
  defaultCronForScheduleKind,
  nextAutomationExecution,
  parseAutomationSchedule,
} from "./AutomationSchedule";

describe("AutomationSchedule", () => {
  it.each([
    ["*/15 * * * *", "minutes"],
    ["30 * * * *", "hourly"],
    ["0 9 * * *", "daily"],
    ["0 9 * * 1-5", "weekdays"],
    ["30 18 * * 5", "weekly"],
    ["0 9 1 * *", "custom"],
    ["*/7 * * * *", "custom"],
  ])("maps %s to %s without broadening unsupported expressions", (cron, kind) => {
    expect(parseAutomationSchedule(cron).kind).toBe(kind);
  });

  it("round-trips the editable values for common schedules", () => {
    expect(cronForAutomationSchedule({
      ...parseAutomationSchedule("0 9 * * 1-5"),
      time: "18:30",
    })).toBe("30 18 * * 1-5");
    expect(cronForAutomationSchedule({
      ...parseAutomationSchedule("0 9 * * 1"),
      weekday: 5,
    })).toBe("0 9 * * 5");
  });

  it("keeps the current expression when entering advanced mode", () => {
    expect(defaultCronForScheduleKind("custom", "0 9 1 * *")).toBe("0 9 1 * *");
  });

  it("computes a concrete next local execution for common schedules", () => {
    const now = new Date("2026-07-27T14:20:00Z");
    expect(nextAutomationExecution("15 * * * *", "UTC", now)).toEqual({
      dayOffset: 0,
      weekday: 1,
      time: "15:15",
    });
    expect(nextAutomationExecution("0 9 * * 1-5", "UTC", new Date("2026-07-31T10:00:00Z")))
      .toEqual({ dayOffset: 3, weekday: 1, time: "09:00" });
  });

  it("does not guess the next execution for an advanced expression", () => {
    expect(nextAutomationExecution("0 9 1 * *", "UTC")).toBeNull();
  });
});
