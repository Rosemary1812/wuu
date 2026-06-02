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
});
