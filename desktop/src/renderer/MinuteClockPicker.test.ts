import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { minuteFromClockPoint } from "./MinuteClockPicker";

const selectMenuCss = readFileSync(resolve(__dirname, "styles/select-menu.css"), "utf-8");

describe("minuteFromClockPoint", () => {
  it("maps the clock face to all minute quadrants", () => {
    expect(minuteFromClockPoint(0, -1)).toBe(0);
    expect(minuteFromClockPoint(1, 0)).toBe(15);
    expect(minuteFromClockPoint(0, 1)).toBe(30);
    expect(minuteFromClockPoint(-1, 0)).toBe(45);
  });

  it("supports precise non-sequential minute selection", () => {
    expect(minuteFromClockPoint(-Math.sqrt(3), -1)).toBe(50);
  });

  it("renders the hand with defined high-contrast theme tokens", () => {
    const hand = selectMenuCss.match(/\.minute-clock-hand\s*\{([\s\S]*?)\}/)?.[1] ?? "";
    const pin = selectMenuCss.match(/\.minute-clock-pin\s*\{([\s\S]*?)\}/)?.[1] ?? "";
    expect(hand).toContain("background: var(--ink-strong)");
    expect(hand).toContain("z-index: 2");
    expect(pin).toContain("background: var(--ink-strong)");
    expect(pin).toContain("z-index: 4");
    expect(selectMenuCss).not.toContain("var(--accent)");
  });
});
