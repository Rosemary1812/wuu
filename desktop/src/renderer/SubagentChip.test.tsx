import { describe, expect, it } from "vitest";
import { aggregateSubagentChips } from "./SubagentChip";

describe("aggregateSubagentChips", () => {
  it("combines adjacent completions into one summary chip", () => {
    expect(
      aggregateSubagentChips([
        { label: "lint 完成了", outcome: "completed" },
        { label: "tests 完成了", outcome: "completed" },
        { label: "docs 完成了", outcome: "completed" },
      ]),
    ).toEqual([{ label: "3 个 subagent 完成了", outcome: "completed" }]);
  });

  it("keeps failed chips named instead of dissolving them into a count", () => {
    expect(
      aggregateSubagentChips([
        { label: "lint 完成了", outcome: "completed" },
        { label: "tests 失败了", outcome: "failed" },
      ]),
    ).toEqual([
      { label: "lint 完成了", outcome: "completed" },
      { label: "tests 失败了", outcome: "failed" },
    ]);
  });

  it("only summarizes adjacent completed runs", () => {
    expect(
      aggregateSubagentChips([
        { label: "lint 完成了", outcome: "completed" },
        { label: "tests 失败了", outcome: "failed" },
        { label: "docs 完成了", outcome: "completed" },
        { label: "build 完成了", outcome: "completed" },
      ]),
    ).toEqual([
      { label: "lint 完成了", outcome: "completed" },
      { label: "tests 失败了", outcome: "failed" },
      { label: "2 个 subagent 完成了", outcome: "completed" },
    ]);
  });

  it("passes a single chip through untouched", () => {
    const displays = [{ label: "lint 完成了", outcome: "completed" as const }];
    expect(aggregateSubagentChips(displays)).toEqual(displays);
  });
});
