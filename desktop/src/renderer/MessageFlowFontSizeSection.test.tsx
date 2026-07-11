import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { applyMessageFlowFontSize } from "./MessageFlowFontSizeSection";

beforeEach(() => {
  document.documentElement.style.removeProperty(
    "--conversation-message-font-size",
  );
});

afterEach(() => {
  document.documentElement.style.removeProperty(
    "--conversation-message-font-size",
  );
});

describe("applyMessageFlowFontSize", () => {
  it("stamps the small step at 13px on <html>", () => {
    applyMessageFlowFontSize("small");
    expect(
      document.documentElement.style.getPropertyValue(
        "--conversation-message-font-size",
      ),
    ).toBe("13px");
  });

  it("stamps the medium step at 14px on <html>", () => {
    applyMessageFlowFontSize("medium");
    expect(
      document.documentElement.style.getPropertyValue(
        "--conversation-message-font-size",
      ),
    ).toBe("14px");
  });

  it("stamps the large step at 16px on <html>", () => {
    applyMessageFlowFontSize("large");
    expect(
      document.documentElement.style.getPropertyValue(
        "--conversation-message-font-size",
      ),
    ).toBe("16px");
  });

  it("overwrites a previous value when the user re-picks", () => {
    applyMessageFlowFontSize("small");
    applyMessageFlowFontSize("large");
    expect(
      document.documentElement.style.getPropertyValue(
        "--conversation-message-font-size",
      ),
    ).toBe("16px");
  });

  it("removes the inline stamp when the property is undefined", () => {
    // Defensive: assert that an unknown value does not silently leave a
    // stale inline stamp behind. The TypeScript type prevents this at
    // compile time, but the runtime check guards against the "as any"
    // escape hatch in IPC bridges.
    document.documentElement.style.setProperty(
      "--conversation-message-font-size",
      "13px",
    );
    document.documentElement.style.removeProperty(
      "--conversation-message-font-size",
    );
    expect(
      document.documentElement.style.getPropertyValue(
        "--conversation-message-font-size",
      ),
    ).toBe("");
  });
});
