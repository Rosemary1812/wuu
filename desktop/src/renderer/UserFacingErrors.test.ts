import { describe, expect, it } from "vitest";
import type { ThreadItem, Turn } from "../shared/protocol";
import { turnEventForItem, turnEventForTurn } from "./TurnEvents";
import {
  userFacingErrorForMessage,
  userFacingErrorForMissingReply,
} from "./UserFacingErrors";

describe("userFacingErrorForMessage", () => {
  it("classifies wrapped context overflow as a provider error", () => {
    const display = userFacingErrorForMessage(
      "stream request failed: stream error (context_length_exceeded): Your input exceeds the context window",
      "turn"
    );

    expect(display.category).toBe("provider");
    // The chip title now shows the specific provider error identifier, not
    // the category name, so a screenshot carries enough signal to triage.
    expect(display.title).toBe("context_length_exceeded");
  });

  it("shows the HTTP status code as the title for network errors", () => {
    const display = userFacingErrorForMessage(
      "upstream returned 503 service unavailable",
      "turn",
    );
    expect(display.category).toBe("network");
    expect(display.title).toBe("503 unavailable");
  });

  it("shows the HTTP status code as the title for auth errors", () => {
    const display = userFacingErrorForMessage("401 unauthorized", "turn");
    expect(display.category).toBe("auth");
    expect(display.title).toBe("401 unauthorized");
    // The action label is Provider-specific, not the generic "查看设置".
    const settingsAction = display.recommendedActions.find(
      (a) => a.kind === "openSettings",
    );
    expect(settingsAction?.label).toBe("查看 Provider 设置");
    expect(settingsAction?.payload).toEqual({ focus: "providers" });
    // The detail (hover tooltip) points the user to the Provider settings.
    expect(display.detail).toContain("Provider");
  });

  it("uses a keyword-based title when no HTTP code is present in the error", () => {
    const display = userFacingErrorForMessage("connection reset by peer", "turn");
    expect(display.category).toBe("network");
    expect(display.title).toBe("connection reset");
  });

  it("shows partial Responses stream closes as a specific provider-stream state", () => {
    const display = userFacingErrorForMessage(
      "stream request failed: websocket stream closed after provider event: websocket stream closed before response.completed",
      "turn",
    );

    expect(display.category).toBe("provider");
    expect(display.title).toBe("stream_closed_before_response.completed");
    expect(display.detail).toContain("response.completed");
    expect(display.detail).toContain("这次回答可能不完整");
  });

  it("falls back to the category title when the message has no specific identifier", () => {
    const display = userFacingErrorForMessage("login required", "turn");
    expect(display.category).toBe("auth");
    // "login required" matches the auth classifier but no HTTP code or
    // keyword matcher fires, so we fall back to the category name.
    expect(display.title).toBe("auth error");
  });

  it("uses the internal category title for unrecognized errors", () => {
    const display = userFacingErrorForMessage("panic: nil pointer", "turn");
    expect(display.category).toBe("internal");
    expect(display.title).toBe("wuu 内部错误");
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

  describe("structured TurnError input from the Go core", () => {
    it("uses the structured `code` as the chip title, beating the message-classifier", () => {
      // The Go core extracts a specific code from the body (e.g.
      // "insufficient_quota") and ships it as the `code` field. The
      // front-end should display that, not the string-extracted one.
      const display = userFacingErrorForMessage(
        {
          message: "some raw provider body",
          code: "insufficient_quota",
          category: "provider",
        },
        "turn",
      );
      expect(display.title).toBe("insufficient_quota");
      expect(display.category).toBe("provider");
    });

    it("maps the 'reauth' action reason to the openSettings / focus=providers kind", () => {
      const display = userFacingErrorForMessage(
        {
          message: "401 unauthorized",
          code: "401 unauthorized",
          category: "auth",
          action: {
            reason: "reauth",
            title: "Provider 凭据或权限不足",
            message: "请检查 API 密钥",
            label: "查看 Provider 设置",
          },
        },
        "turn",
      );
      expect(display.title).toBe("401 unauthorized");
      expect(display.category).toBe("auth");
      expect(display.tone).toBe("auth");
      expect(display.detail).toBe("请检查 API 密钥");
      expect(display.recommendedActions).toHaveLength(1);
      expect(display.recommendedActions[0].kind).toBe("openSettings");
      expect(display.recommendedActions[0].payload).toEqual({
        focus: "providers",
      });
    });

    it("maps the 'compact' action reason to the compactContext kind", () => {
      const display = userFacingErrorForMessage(
        {
          message: "input too long",
          code: "context_length_exceeded",
          category: "provider",
          action: {
            reason: "compact",
            title: "上下文超出窗口",
            message: "压缩后重试",
            label: "压缩上下文",
          },
        },
        "turn",
      );
      expect(display.recommendedActions[0].kind).toBe("compactContext");
    });

    it("maps the 'wait' and 'retry' action reasons to the retry kind", () => {
      const wait = userFacingErrorForMessage(
        {
          message: "rate limit",
          code: "rate_limit_error",
          category: "provider",
          action: {
            reason: "wait",
            title: "Provider 限流",
            message: "稍后重试",
            label: "稍后重试",
          },
        },
        "turn",
      );
      const retry = userFacingErrorForMessage(
        {
          message: "timeout",
          category: "network",
          action: {
            reason: "retry",
            title: "重试",
            message: "",
            label: "重试",
          },
        },
        "turn",
      );
      expect(wait.recommendedActions[0].kind).toBe("retry");
      expect(retry.recommendedActions[0].kind).toBe("retry");
    });

    it("maps the 'view_debug' and 'copy_debug' action reasons to copyDebug with structured payload", () => {
      const display = userFacingErrorForMessage(
        {
          message: "raw error body",
          code: "internal_error",
          category: "internal",
          provider: "openai",
          status_code: 500,
          action: {
            reason: "copy_debug",
            title: "wuu 内部错误",
            message: "调试信息",
            label: "复制调试信息",
          },
        },
        "turn",
      );
      expect(display.recommendedActions[0].kind).toBe("copyDebug");
      expect(display.recommendedActions[0].payload).toMatchObject({
        category: "internal",
        context: "turn",
        code: "internal_error",
        provider: "openai",
        status_code: 500,
      });
    });

    it("uses the structured partial-stream code and detail from the Go core", () => {
      const display = userFacingErrorForMessage(
        {
          message:
            "stream request failed: websocket stream closed after provider event: websocket stream closed before response.completed",
          code: "stream_closed_before_response.completed",
          category: "provider",
          provider: "openai-codex",
          action: {
            reason: "view_debug",
            title: "部分回答未完成",
            message:
              "Provider WS 流在 response.completed 前断开；这次回答可能不完整。",
            label: "复制调试信息",
          },
        },
        "turn",
      );

      expect(display.title).toBe("stream_closed_before_response.completed");
      expect(display.category).toBe("provider");
      expect(display.detail).toContain("这次回答可能不完整");
      expect(display.recommendedActions[0].kind).toBe("copyDebug");
      expect(display.recommendedActions[0].payload).toMatchObject({
        category: "provider",
        provider: "openai-codex",
        code: "stream_closed_before_response.completed",
      });
    });

    it("falls back to category-driven actions when no structured action is provided", () => {
      // Auth without an action still opens Provider settings.
      const auth = userFacingErrorForMessage(
        { message: "401 unauthorized", category: "auth" },
        "turn",
      );
      expect(auth.recommendedActions[0].kind).toBe("openSettings");
      expect(auth.recommendedActions[0].payload).toEqual({
        focus: "providers",
      });
      // Internal without an action gets the copyDebug fallback.
      const internal = userFacingErrorForMessage(
        { message: "panic", category: "internal" },
        "turn",
      );
      expect(internal.recommendedActions[0].kind).toBe("copyDebug");
    });

    it("drops the action list for cancelled and forwards the structured detail", () => {
      const display = userFacingErrorForMessage(
        {
          message: "context canceled",
          category: "cancelled",
        },
        "turn",
      );
      expect(display.recommendedActions).toHaveLength(0);
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
      expect(display.title).toBe("context_length_exceeded");
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
    expect(event.notice.title).toBe("stream_closed_before_response.completed");
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
    expect(event.notice.recommendedActions).toEqual([]);
  });
});

describe("userFacingErrorForMissingReply", () => {
  it("returns a warning-toned display with the soft 'no final answer' copy and no actions", () => {
    const display = userFacingErrorForMissingReply();
    // Soft yellow warning — visually distinct from error (red) and
    // auth (brown) chips. Signals "this turn ended without a final
    // answer" without implying the user or the model did something
    // wrong.
    expect(display.tone).toBe("warning");
    expect(display.title).toBe("无最终回答");
    expect(display.detail).toBe("这轮只保留了过程记录，没有生成最终回答。");
    // No action — the user cannot do anything useful here except send
    // a follow-up message, which the composer already supports.
    expect(display.recommendedActions).toEqual([]);
  });
});
