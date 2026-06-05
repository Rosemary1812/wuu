import { describe, expect, it } from "vitest";
import { userFacingErrorForMessage } from "./UserFacingErrors";

describe("userFacingErrorForMessage", () => {
  it("classifies wrapped context overflow as a provider error", () => {
    const display = userFacingErrorForMessage(
      "stream request failed: stream error (context_length_exceeded): Your input exceeds the context window",
      "turn"
    );

    expect(display.category).toBe("provider");
    expect(display.title).toBe("模型没有完成请求");
  });

  describe("recommendedActions", () => {
    it("network error offers retry + provider switch", () => {
      const display = userFacingErrorForMessage(
        "connection reset by peer",
        "turn",
      );
      const kinds = display.recommendedActions.map((a) => a.kind);
      expect(kinds).toContain("retry");
      expect(kinds).toContain("switchModel");
      // Primary action must come first — UI relies on this order.
      expect(display.recommendedActions[0]?.kind).toBe("retry");
      expect(display.recommendedActions[0]?.variant).toBe("primary");
    });

    it("auth error offers reauth + openSettings with focus payload", () => {
      const display = userFacingErrorForMessage("401 unauthorized", "turn");
      const settingsAction = display.recommendedActions.find(
        (a) => a.kind === "openSettings",
      );
      expect(settingsAction).toBeDefined();
      expect(settingsAction?.variant).toBe("secondary");
      expect(settingsAction?.payload).toEqual({ focus: "providers" });
    });

    it("provider context overflow offers compactContext + switchModel", () => {
      const display = userFacingErrorForMessage(
        "context_length_exceeded: too many tokens",
        "turn",
      );
      const kinds = display.recommendedActions.map((a) => a.kind);
      expect(kinds).toContain("compactContext");
      expect(kinds).toContain("switchModel");
    });

    it("internal error offers retry + copyDebug + submitFeedback", () => {
      const display = userFacingErrorForMessage("panic: nil pointer", "turn");
      const kinds = display.recommendedActions.map((a) => a.kind);
      expect(kinds).toContain("retry");
      expect(kinds).toContain("copyDebug");
      expect(kinds).toContain("submitFeedback");
    });

    it("cancelled error offers a single retry action", () => {
      const display = userFacingErrorForMessage("context canceled", "turn");
      expect(display.recommendedActions).toHaveLength(1);
      expect(display.recommendedActions[0]?.kind).toBe("retry");
    });

    it("every action has a non-empty label", () => {
      // Guard against silent regressions: an action with no label
      // would render as an empty <button> in the UI.
      const cases = [
        "connection refused",
        "401 unauthorized",
        "context_length_exceeded",
        "tool failed",
        "permission denied: file",
        "panic: runtime error",
      ];
      for (const message of cases) {
        const display = userFacingErrorForMessage(message, "turn");
        for (const action of display.recommendedActions) {
          expect(action.label.length, `action ${action.kind} missing label`).toBeGreaterThan(0);
        }
      }
    });
  });
});
