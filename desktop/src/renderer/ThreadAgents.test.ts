import { describe, expect, it } from "vitest";
import { agentStatusLabel } from "./ThreadAgents";

describe("agentStatusLabel", () => {
  it.each([
    ["pending", "等待"],
    ["queued", "排队中"],
    ["running", "运行中"],
    ["waiting_children", "等待子任务"],
    ["completed", "完成"],
    ["failed", "失败"],
    ["cancelled", "已停止"],
  ])("maps %s to %s", (status, label) => {
    expect(agentStatusLabel(status)).toBe(label);
  });
});
