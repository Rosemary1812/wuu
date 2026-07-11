import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { MESSAGE_FLOW_FONT_SIZE_RANGE } from "../shared/protocol";
import { applyMessageFlowFontSize } from "./MessageFlowFontSizeSection";

const STYLE_PROP = "--conversation-message-font-size" as const;

function readStamp(): string {
  return document.documentElement.style.getPropertyValue(STYLE_PROP);
}

function clearStamp(): void {
  document.documentElement.style.removeProperty(STYLE_PROP);
}

beforeEach(clearStamp);
afterEach(clearStamp);

describe("applyMessageFlowFontSize", () => {
  it("stamps a value inside the range on <html>", () => {
    applyMessageFlowFontSize(16);
    expect(readStamp()).toBe("16px");
  });

  it("clamps values above the configured maximum", () => {
    applyMessageFlowFontSize(99);
    expect(readStamp()).toBe(`${MESSAGE_FLOW_FONT_SIZE_RANGE.max}px`);
  });

  it("clamps values below the configured minimum", () => {
    applyMessageFlowFontSize(8);
    expect(readStamp()).toBe(`${MESSAGE_FLOW_FONT_SIZE_RANGE.min}px`);
  });

  it("falls back to the configured default for non-finite values", () => {
    applyMessageFlowFontSize(Number.NaN);
    expect(readStamp()).toBe(`${MESSAGE_FLOW_FONT_SIZE_RANGE.default}px`);
    applyMessageFlowFontSize(Number.POSITIVE_INFINITY);
    expect(readStamp()).toBe(`${MESSAGE_FLOW_FONT_SIZE_RANGE.default}px`);
  });

  it("overwrites a previous stamp on subsequent calls", () => {
    applyMessageFlowFontSize(13);
    applyMessageFlowFontSize(20);
    expect(readStamp()).toBe(`${MESSAGE_FLOW_FONT_SIZE_RANGE.max}px`);
  });

  it("supports the half-step resolution the slider emits", () => {
    applyMessageFlowFontSize(14.5);
    expect(readStamp()).toBe("14.5px");
    applyMessageFlowFontSize(15.5);
    expect(readStamp()).toBe("15.5px");
  });
});
