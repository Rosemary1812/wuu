import { describe, expect, it, vi } from "vitest";
import { initialState, reduceServerEvent } from "./AppState";

describe("AppState server requests", () => {
  it("keeps tool approval requests pending instead of rejecting them", () => {
    const rejectServerRequest = vi.fn();
    Object.defineProperty(window, "wuu", {
      configurable: true,
      value: { rejectServerRequest },
    });

    const next = reduceServerEvent(initialState, {
      kind: "server-request",
      workdir: "/tmp/project",
      message: {
        id: "server-request-1",
        method: "tool/approval/request",
        params: {
          id: "approval-1",
          tool_name: "run_shell",
          risk: "high",
          arguments_preview: "{\"command\":\"printf hi\"}",
        },
      },
    });

    expect(rejectServerRequest).not.toHaveBeenCalled();
    expect(next.pendingToolApproval?.server_request_id).toBe("server-request-1");
    expect(next.pendingToolApproval?.tool_name).toBe("run_shell");
    expect(next.status).toBe("等待审批");
  });
});
