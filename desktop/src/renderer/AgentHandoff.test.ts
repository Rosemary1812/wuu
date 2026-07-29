import { describe, expect, it } from "vitest";

import {
  AGENT_NOTIFICATION_NAME,
  agentHandoffChipDisplayItems,
  agentHandoffDisplay,
  agentHandoffDisplayItem,
  isAgentHandoffItem,
  isAgentHandoffText,
} from "./AgentHandoff";

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

  it("formats handoffs as compact named chips", () => {
    const item = {
      name: AGENT_NOTIFICATION_NAME,
      text: handoffText("completed"),
    };
    expect(agentHandoffChipDisplayItems(item)).toEqual([
      { label: "explore_current_directory 完成了", outcome: "completed" },
    ]);
  });

  it("keeps combined completion envelopes as informative as separate ones", () => {
    // combineAgentCompletionMessages joins ≥2 envelopes with "\n\n" while the
    // parent agent is mid-turn; chips must not degrade to a generic label.
    const first = JSON.parse(handoffText("completed"));
    const second = JSON.parse(handoffText("failed"));
    second.content = second.content.replaceAll(
      "explore_current_directory",
      "run_tests",
    );
    const item = {
      name: AGENT_NOTIFICATION_NAME,
      text: `${JSON.stringify(first)}\n\n${JSON.stringify(second)}`,
    };
    expect(agentHandoffChipDisplayItems(item)).toEqual([
      { label: "explore_current_directory 完成了", outcome: "completed" },
      { label: "run_tests 失败了", outcome: "failed" },
    ]);
  });

  it("falls back to one generic chip when a name-stamped payload is unparseable", () => {
    const item = { name: AGENT_NOTIFICATION_NAME, text: "{not json" };
    expect(agentHandoffChipDisplayItems(item)).toEqual([
      { label: "已更新", outcome: "updated" },
    ]);
  });

  it("ignores a legacy <changed_file_overlap> tail instead of degrading the chip", () => {
    const item = {
      name: AGENT_NOTIFICATION_NAME,
      text:
        handoffText("completed") +
        "\n\n<changed_file_overlap>\n  - foo.ts\n</changed_file_overlap>",
    };
    expect(agentHandoffChipDisplayItems(item)).toEqual([
      { label: "explore_current_directory 完成了", outcome: "completed" },
    ]);
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

// Backbone of the projection-fix change: classify handoff items by the
// `name` field the backend stamps on every agent self-addressed
// user_message, instead of relying on `text` sniffing alone. The text
// sniff broke in two production wire shapes:
//
//   1. combineAgentCompletionMessages joins ≥2 envelopes with "\n\n",
//      producing a string JSON.parse cannot consume.
//   2. AgentCompletionChatMessage appends "<changed_file_overlap>..."
//      after the envelope when CompletionOverlapWarnings is non-empty.
//
// In both cases `name === "wuu_agent_notification"` is the only reliable
// signal — the helpers below must classify them as handoffs.
describe("isAgentHandoffItem / agentHandoffDisplayItem", () => {
  it("exports AGENT_NOTIFICATION_NAME matching the backend wire constant", () => {
    expect(AGENT_NOTIFICATION_NAME).toBe("wuu_agent_notification");
  });

  it("classifies a single-envelope handoff item by its `name` field", () => {
    const item = { name: AGENT_NOTIFICATION_NAME, text: handoffText("completed") };
    expect(isAgentHandoffItem(item)).toBe(true);
    // Design choice: the name-hit branch deliberately returns the generic
    // label and does NOT parse `item.text`. This makes the helper immune
    // to corrupt payloads (combineAgentCompletionMessages \n\n joins,
    // <changed_file_overlap> tails) — see AgentHandoff.ts JSDoc.
    expect(agentHandoffDisplayItem(item)?.label).toBe("subagent 更新了任务状态");
  });

  it("classifies \\n\\n-joined envelopes (combineAgentCompletionMessages) without parsing text", () => {
    // Two complete envelopes joined with "\n\n" — exactly the shape
    // combineAgentCompletionMessages produces when ≥2 sub-agents complete
    // during a 429-stalled main thread. JSON.parse cannot consume this.
    const joined = [handoffText("completed"), handoffText("failed")].join("\n\n");
    const item = { name: AGENT_NOTIFICATION_NAME, text: joined };
    expect(isAgentHandoffItem(item)).toBe(true);
    const display = agentHandoffDisplayItem(item);
    expect(display?.label).toBe("subagent 更新了任务状态");
    // The label must never echo payload text back into the DOM.
    expect(display?.label).not.toContain("subagent_notification");
    expect(display?.label).not.toContain(joined);
  });

  it("classifies <changed_file_overlap> tail (AgentCompletionChatMessage warning) without parsing text", () => {
    // Envelope followed by the overlap-warning tail — the second
    // production shape that breaks the legacy text sniff.
    const overlapTail =
      '\n\n<changed_file_overlap>\n  - foo.ts\n  - foo.ts (again)\n</changed_file_overlap>';
    const item = {
      name: AGENT_NOTIFICATION_NAME,
      text: handoffText("completed") + overlapTail,
    };
    expect(isAgentHandoffItem(item)).toBe(true);
    const display = agentHandoffDisplayItem(item);
    expect(display?.label).toBe("subagent 更新了任务状态");
    expect(display?.label).not.toContain("changed_file_overlap");
  });

  it("falls back to text sniffing when `name` is absent (legacy wire shape)", () => {
    // Older backends that did not propagate `name` — the helper must
    // still classify the envelope via the existing <subagent_notification>
    // sniff, otherwise we'd regress for any in-flight data.
    const item = { text: handoffText("completed") };
    expect(isAgentHandoffItem(item)).toBe(true);
    expect(agentHandoffDisplayItem(item)?.label).toBe("subagent 完成了任务");
  });

  it("does not classify normal user messages as handoffs", () => {
    expect(isAgentHandoffItem({ name: "", text: "帮我检查这个目录" })).toBe(false);
    expect(isAgentHandoffItem({ name: "", text: "hello" })).toBe(false);
    expect(isAgentHandoffItem({ text: "hello" })).toBe(false);
    expect(agentHandoffDisplayItem({ name: "", text: "帮我检查这个目录" })).toBeUndefined();
  });

  it("treats missing items as not-handoff (no throw)", () => {
    expect(isAgentHandoffItem(undefined)).toBe(false);
    expect(agentHandoffDisplayItem(undefined)).toBeUndefined();
  });

  it("triggers on trigger_turn=true legacy path via text sniff even when name is absent", () => {
    // The text-only path must still produce a display when trigger_turn
    // is true — agentHandoffDisplayItem should not regress the legacy
    // flow that pre-dates the `name` propagation.
    const item = { text: handoffText("running") };
    expect(agentHandoffDisplayItem(item)?.label).toBe("subagent 正在执行任务");
  });

  it("item-aware display returns the generic label on name-hit, text-only on legacy envelopes", () => {
    // Two valid resolutions that callers must be able to distinguish:
    //   - `name` present → generic label (no parsing, immune to payload corruption).
    //   - `name` absent → text-only path parses the legacy envelope.
    const text = handoffText("completed");
    expect(agentHandoffDisplayItem({ name: AGENT_NOTIFICATION_NAME, text })?.label).toBe(
      "subagent 更新了任务状态",
    );
    expect(agentHandoffDisplayItem({ text })?.label).toBe(
      agentHandoffDisplay(text)?.label,
    );
    expect(agentHandoffDisplayItem({ text })?.label).toBe("subagent 完成了任务");
  });
});
