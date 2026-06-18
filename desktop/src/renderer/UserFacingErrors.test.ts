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
    it("network error offers only currently wired debug action", () => {
      const display = userFacingErrorForMessage(
        "connection reset by peer",
        "turn",
      );
      const kinds = display.recommendedActions.map((a) => a.kind);
      expect(kinds).toEqual(["copyDebug"]);
    });

    it("auth error offers settings because reconnect is not wired yet", () => {
      const display = userFacingErrorForMessage("401 unauthorized", "turn");
      const settingsAction = display.recommendedActions.find(
        (a) => a.kind === "openSettings",
      );
      expect(settingsAction).toBeDefined();
      expect(settingsAction?.variant).toBe("primary");
      expect(settingsAction?.payload).toEqual({ focus: "providers" });
    });

    it("provider context overflow offers only currently wired debug action", () => {
      const raw = "context_length_exceeded: too many tokens";
      const display = userFacingErrorForMessage(
        raw,
        "turn",
      );
      const kinds = display.recommendedActions.map((a) => a.kind);
      expect(kinds).toEqual(["copyDebug"]);
      expect(display.recommendedActions[0]?.payload).toMatchObject({
        category: "provider",
        context: "turn",
        message: raw,
      });
    });

    it("internal error offers only currently wired debug action", () => {
      const display = userFacingErrorForMessage("panic: nil pointer", "turn");
      const kinds = display.recommendedActions.map((a) => a.kind);
      expect(kinds).toEqual(["copyDebug"]);
    });

    it("cancelled error has no action until retry is wired", () => {
      const display = userFacingErrorForMessage("context canceled", "turn");
      expect(display.recommendedActions).toHaveLength(0);
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
