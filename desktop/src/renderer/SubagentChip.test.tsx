import { describe, expect, it } from "vitest";
import { aggregateSubagentChips } from "./SubagentChip";

describe("aggregateSubagentChips", () => {
  it("combines adjacent completions into one summary chip", () => {
    expect(
      aggregateSubagentChips([
        { label: "lint 完成了", shimmer: true },
        { label: "tests 完成了", shimmer: true },
        { label: "docs 完成了", shimmer: true },
      ]),
    ).toEqual([{ label: "3 个 subagent 完成了", shimmer: true }]);
  });

  it("keeps different outcomes as separate summaries", () => {
    expect(
      aggregateSubagentChips([
        { label: "lint 完成了", shimmer: true },
        { label: "tests 失败了", shimmer: true },
      ]),
    ).toEqual([
      { label: "1 个 subagent 完成了", shimmer: true },
      { label: "1 个 subagent 失败了", shimmer: true },
    ]);
  });
});
