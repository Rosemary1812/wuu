import { describe, expect, it } from "vitest";

import { agentHandoffDisplay, isAgentHandoffText } from "./AgentHandoff";

function handoffText(status = "completed"): string {
  return JSON.stringify({
    author: "/root/explore",
    recipient: "/root",
    content: `<subagent_notification>\n${JSON.stringify({
      agent_path: "/root/explore_current_directory",
      status: {
        type: "agent_result",
        agent_id: "worker-1",
        task_name: "explore_current_directory",
        status
      }
    })}\n</subagent_notification>`,
    trigger_turn: true
  });
}

describe("agentHandoffDisplay", () => {
  it("renders completed subagent handoffs as a system event", () => {
    expect(agentHandoffDisplay(handoffText())?.label).toBe(
      "subagent 完成了任务"
    );
  });

  it("uses short system-event labels for terminal and active statuses", () => {
    expect(agentHandoffDisplay(handoffText("failed"))?.label).toBe(
      "subagent 任务失败"
    );
    expect(agentHandoffDisplay(handoffText("cancelled"))?.label).toBe(
      "subagent 任务已取消"
    );
    expect(agentHandoffDisplay(handoffText("running"))?.label).toBe(
      "subagent 正在执行任务"
    );
    expect(agentHandoffDisplay(handoffText("queued"))?.label).toBe(
      "subagent 等待执行任务"
    );
  });

  it("does not treat normal user text as an internal handoff", () => {
    expect(agentHandoffDisplay("帮我检查这个目录")).toBeUndefined();
  });

  it("requires trigger_turn so stored mailbox payloads are not hidden accidentally", () => {
    const payload = JSON.parse(handoffText());
    payload.trigger_turn = false;
    expect(agentHandoffDisplay(JSON.stringify(payload))).toBeUndefined();
  });

  it("identifies stored mailbox payloads as internal handoffs for history filters", () => {
    const payload = JSON.parse(handoffText());
    payload.trigger_turn = false;
    expect(isAgentHandoffText(JSON.stringify(payload))).toBe(true);
  });
});
