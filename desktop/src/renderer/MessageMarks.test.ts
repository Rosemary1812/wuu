import { describe, it, expect } from "vitest";
import {
  aggregateMarksBySeq,
  ringModel,
  reactionGlyph,
  type MessageMark,
} from "./MessageMarks";

describe("aggregateMarksBySeq", () => {
  it("places each participant in one seen bucket, latest status winning", () => {
    const marks: MessageMark[] = [
      { seq: 3, participant_id: "a", kind: "seen", status: "in_progress" },
      { seq: 3, participant_id: "a", kind: "seen", status: "completed" },
      { seq: 3, participant_id: "b", kind: "seen", status: "failed" },
    ];
    const view = aggregateMarksBySeq(marks).get(3)!;
    expect(view.seen.completed).toEqual(["a"]);
    expect(view.seen.failed).toEqual(["b"]);
    expect(view.seen.inProgress).toEqual([]);
  });

  it("aggregates reactions by key with participant lists and glyphs", () => {
    const marks: MessageMark[] = [
      { seq: 5, participant_id: "a", kind: "reaction", reaction: "shrug" },
      { seq: 5, participant_id: "b", kind: "reaction", reaction: "shrug" },
      { seq: 5, participant_id: "c", kind: "reaction", reaction: "smug" },
    ];
    const view = aggregateMarksBySeq(marks).get(5)!;
    expect(view.reactions).toHaveLength(2);
    const shrug = view.reactions.find((r) => r.key === "shrug")!;
    expect(shrug.participantIds).toEqual(["a", "b"]);
    expect(shrug.glyph).toBe("🤷");
  });

  it("ignores marks with no/invalid seq", () => {
    const marks = [
      { seq: -1, participant_id: "a", kind: "seen", status: "completed" },
    ] as MessageMark[];
    expect(aggregateMarksBySeq(marks).size).toBe(0);
  });

  it("keeps reactions on the first persisted message (seq 0)", () => {
    const marks: MessageMark[] = [
      { seq: 0, participant_id: "a", kind: "reaction", reaction: "shrug" },
      { seq: 0, participant_id: "b", kind: "reaction", reaction: "shrug" },
    ];
    const view = aggregateMarksBySeq(marks).get(0);
    expect(view).toBeDefined();
    const shrug = view!.reactions.find((r) => r.key === "shrug")!;
    expect(shrug.participantIds).toEqual(["a", "b"]);
  });
});

describe("ringModel", () => {
  it("fills to completed/total and flags all-seen", () => {
    const m = ringModel({ completed: ["a", "b"], inProgress: [], failed: [] }, 2);
    expect(m.fraction).toBe(1);
    expect(m.allSeen).toBe(true);
    expect(m.anyFailed).toBe(false);
  });

  it("surfaces a failed segment and never claims all-seen when one crashed", () => {
    const m = ringModel(
      { completed: ["a", "b", "c", "d"], inProgress: [], failed: ["e"] },
      5,
    );
    expect(m.fraction).toBeCloseTo(0.8);
    expect(m.anyFailed).toBe(true);
    expect(m.allSeen).toBe(false);
  });

  it("handles unknown total by falling back to the mark count", () => {
    const m = ringModel({ completed: ["a"], inProgress: ["b"], failed: [] }, 0);
    expect(m.total).toBe(2);
    expect(m.fraction).toBeCloseTo(0.5);
  });
});

describe("reactionGlyph", () => {
  it("maps known keys and falls back for unknown", () => {
    expect(reactionGlyph("eyes")).toBe("👀");
    expect(reactionGlyph("mystery")).toBe("•");
  });
});
