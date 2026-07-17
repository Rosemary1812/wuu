import { afterEach, describe, expect, it } from "vitest";
import type { ThreadItem, Turn, TurnError } from "../shared/protocol";
import { turnEventForItem, turnEventForTurn } from "./TurnEvents";
import {
  userFacingErrorForMessage,
  userFacingErrorForMissingReply,
} from "./UserFacingErrors";
import { setActiveLocale } from "./i18n";

afterEach(() => setActiveLocale("zh-CN"));

describe("userFacingErrorForMessage", () => {
  it("classifies wrapped context overflow as a provider error", () => {
    const display = userFacingErrorForMessage(
      "stream request failed: stream error (context_length_exceeded): Your input exceeds the context window",
      "turn"
    );

    expect(display.category).toBe("provider");
    // Known diagnostic identifiers are translated into one user-facing label.
    expect(display.title).toBe("上下文超出窗口");
  });

  it("shows the HTTP status code with a Chinese phrase as the title for network errors", () => {
    const display = userFacingErrorForMessage(
      "upstream returned 503 service unavailable",
      "turn",
    );
    expect(display.category).toBe("network");
    expect(display.title).toBe("503 服务不可用");
  });

  it("shows the HTTP status code with a Chinese phrase as the title for auth errors", () => {
    const display = userFacingErrorForMessage("401 unauthorized", "turn");
    expect(display.category).toBe("auth");
    expect(display.title).toBe("401 未授权");
    expect(display.detail).toContain("Provider");
  });

  it("uses a keyword-based title when no HTTP code is present in the error", () => {
    const display = userFacingErrorForMessage("connection reset by peer", "turn");
    expect(display.category).toBe("network");
    expect(display.title).toBe("连接被重置");
  });

  it("shows partial Responses stream closes as a specific provider-stream state", () => {
    const display = userFacingErrorForMessage(
      "stream request failed: websocket stream closed after provider event: websocket stream closed before response.completed",
      "turn",
    );

    expect(display.category).toBe("provider");
    expect(display.title).toBe("回答未完整返回");
    expect(display.detail).toContain("response.completed");
    expect(display.detail).toContain("这次回答可能不完整");
  });

  it("falls back to the category title when the message has no specific identifier", () => {
    const display = userFacingErrorForMessage("login required", "turn");
    expect(display.category).toBe("auth");
    // "login required" matches the auth classifier but no HTTP code or
    // keyword matcher fires, so we fall back to the category name.
    expect(display.title).toBe("认证失败");
  });

  it("uses the internal category title for unrecognized errors", () => {
    const display = userFacingErrorForMessage("panic: nil pointer", "turn");
    expect(display.category).toBe("internal");
    expect(display.title).toBe("wuu 内部错误");
  });

  it("localizes generated labels while leaving provider input available for classification", () => {
    setActiveLocale("en-US");

    const display = userFacingErrorForMessage("connection reset by peer", "turn");

    expect(display.category).toBe("network");
    expect(display.title).toBe("Connection reset");
    expect(display.detail).toContain("provider status");
  });

  describe("structured TurnError input from the Go core", () => {
    it("models a 404 as one read-only user-facing message", () => {
      const display = userFacingErrorForMessage(
        {
          message: 'HTTP 404: {"error":{"code":"internal_error","message":"resource not found"}}',
          code: "internal_error",
          category: "internal",
          provider: "compatible",
          status_code: 404,
        },
        "turn",
      );

      expect(display.title).toBe("404 资源不存在");
      expect(display).not.toHaveProperty("code");
      expect(display).not.toHaveProperty("recommendedActions");
    });

    it("keeps an untranslatable structured code out of the visible model", () => {
      const display = userFacingErrorForMessage(
        {
          message: "some raw provider body",
          code: "insufficient_quota",
          category: "provider",
        },
        "turn",
      );
      expect(display.title).toBe("模型服务异常");
      expect(display).not.toHaveProperty("code");
      expect(display.category).toBe("provider");
    });

    it("uses structured status_code as a provider chip fallback when code is missing", () => {
      const display = userFacingErrorForMessage(
        {
          message: "gateway returned a provider error without a parseable code",
          category: "provider",
          status_code: 429,
        },
        "turn",
      );

      expect(display.category).toBe("provider");
      expect(display.title).toBe("429 触发限流");
    });

    it("uses structured partial-stream facts without exposing the code", () => {
      const display = userFacingErrorForMessage(
        {
          message:
            "stream request failed: websocket stream closed after provider event: websocket stream closed before response.completed",
          code: "stream_closed_before_response.completed",
          category: "provider",
          provider: "openai-codex",
        },
        "turn",
      );

      expect(display.title).toBe("回答未完整返回");
      expect(display).not.toHaveProperty("code");
      expect(display.category).toBe("provider");
      expect(display.detail).toContain("这次回答可能不完整");
    });

    it("renders an HTTP-sourced invalid_request with the status phrase and error tone", () => {
      const display = userFacingErrorForMessage(
        {
          message: 'HTTP 400: {"error":{"type":"invalid_request_error","message":"max_tokens must be positive"}}',
          code: "invalid_request_error",
          category: "invalid_request",
          provider: "compatible",
          status_code: 400,
        },
        "turn",
      );

      expect(display.category).toBe("invalid_request");
      expect(display.tone).toBe("error");
      expect(display.title).toBe("400 请求无效");
      expect(display.detail).toBe("Provider 认为这次请求参数无效。原始错误已留在调试信息中。");
    });

    it("falls back to the localized invalid-request title for stream-sourced 400s", () => {
      const display = userFacingErrorForMessage(
        {
          message: "stream request failed: stream error (invalid_request_error)",
          code: "invalid_request_error",
          category: "invalid_request",
        },
        "turn",
      );

      expect(display.category).toBe("invalid_request");
      expect(display.tone).toBe("error");
      expect(display.title).toBe("请求参数无效");
      expect(display.detail).toBe("Provider 认为这次请求参数无效。原始错误已留在调试信息中。");
    });

    it("degrades an unknown wire category to the internal-error rendering", () => {
      // A newer Go core may emit a category this renderer has not
      // learned yet; it must never produce a blank, tone-less display.
      const display = userFacingErrorForMessage(
        {
          message: "some future failure mode",
          category: "quota_exhausted" as unknown as TurnError["category"],
        },
        "turn",
      );

      expect(display.category).toBe("internal");
      expect(display.tone).toBe("error");
      expect(display.title).toBe("wuu 内部错误");
      expect(display.detail).toBe("没有完成这次请求。调试信息可用于排查。");
    });

    it("falls back to the string classifier when the structured input omits `category`", () => {
      // An older Go core may not yet send the `category` field; the
      // front-end must still produce a sensible display from the
      // message alone. "context_length_exceeded" maps to provider.
      const display = userFacingErrorForMessage(
        {
          message: "stream request failed: stream error (context_length_exceeded)",
          code: "context_length_exceeded",
        },
        "turn",
      );
      expect(display.category).toBe("provider");
      expect(display.title).toBe("上下文超出窗口");
      expect(display).not.toHaveProperty("code");
    });
  });
});

describe("TurnEvents", () => {
  it("maps manual interruption to one user-stopped turn event", () => {
    const turn: Turn = {
      id: "turn-1",
      items: [],
      items_view: "full",
      status: "interrupted",
    };

    const event = turnEventForTurn(turn, false);

    expect(event?.kind).toBe("user_stopped");
    expect(event?.source).toBe("turn");
    expect(event?.presentation).toBe("notice");
  });

  it("adds preserved-output detail once for partial Responses stream failures", () => {
    const turn: Turn = {
      id: "turn-1",
      items: [],
      items_view: "full",
      status: "failed",
      error: {
        message:
          "stream request failed: websocket stream closed after provider event: websocket stream closed before response.completed",
        code: "stream_closed_before_response.completed",
        category: "provider",
      },
    };

    const event = turnEventForTurn(turn, true);

    expect(event?.presentation).toBe("notice");
    if (event?.presentation !== "notice") {
      throw new Error("expected notice event");
    }
    expect(event.notice.title).toBe("回答未完整返回");
    expect(event.notice.detail).toContain("这次回答可能不完整");
    expect(event.notice.detail.match(/已保留已生成内容/g)).toHaveLength(1);
  });

  it("maps in-progress compaction to a compaction event instead of an error notice", () => {
    const item: ThreadItem = {
      id: "compact-1",
      type: "context_compaction",
      status: "in_progress",
    };

    const event = turnEventForItem(item);

    expect(event?.kind).toBe("context_compacting");
    expect(event?.presentation).toBe("context_compaction");
  });

  it("maps a completed turn with commentary but no final answer to a missing_final_answer event", () => {
    // `hasMissingReply` is computed by the caller from
    // `AssistantTurnDisplay.missingReplyMessage`. The display builder
    // only sets that flag for a completed turn that produced
    // `commentary` items but never a `final_answer`. When the caller
    // forwards the flag, the event pipeline should short-circuit to a
    // soft warning chip instead of falling through to the cancelled /
    // failed branches.
    const turn: Turn = {
      id: "turn-1",
      items: [],
      items_view: "full",
      status: "completed",
    };

    const event = turnEventForTurn(turn, false, true);

    expect(event?.kind).toBe("missing_final_answer");
    expect(event?.source).toBe("turn");
    expect(event?.presentation).toBe("notice");
    if (event?.presentation !== "notice") {
      throw new Error("expected notice event");
    }
    expect(event.notice.title).toBe("无最终回答");
    expect(event.notice.tone).toBe("warning");
  });
});

describe("userFacingErrorForMissingReply", () => {
  it("returns a warning-toned display with soft no-final-answer copy", () => {
    const display = userFacingErrorForMissingReply();
    // Soft yellow warning — visually distinct from error (red) and
    // auth (brown) chips. Signals "this turn ended without a final
    // answer" without implying the user or the model did something
    // wrong.
    expect(display.tone).toBe("warning");
    expect(display.title).toBe("无最终回答");
    expect(display.detail).toBe("这轮只保留了过程记录，没有生成最终回答。");
  });
});
