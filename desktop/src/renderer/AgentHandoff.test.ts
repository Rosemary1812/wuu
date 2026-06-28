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
  it("summarizes completed subagent handoffs for the transcript", () => {
    expect(agentHandoffDisplay(handoffText())?.label).toBe(
      "子 agent explore_current_directory 已完成，主 agent 正在整合结果"
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
