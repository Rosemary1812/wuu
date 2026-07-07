// Mention semantics mirrored from desktop mentionedParticipantIDsFromText:
// whole-word matches, longest-name-first, CJK punctuation boundaries.

import { describe, expect, it } from "vitest";

import { applyMention, mentionCandidates, mentionedParticipantIDsFromText } from "../src/lib/mentions";

const roster = [
  { id: "p-noe", name: "Noe" },
  { id: "p-noel", name: "Noel" },
  { id: "p-mobai", name: "墨白" },
];

describe("mentionedParticipantIDsFromText", () => {
  it("matches whole words only, longest name first", () => {
    expect(mentionedParticipantIDsFromText("@Noel 看一下", roster)).toEqual(["p-noel"]);
    expect(mentionedParticipantIDsFromText("@Noe 看一下", roster)).toEqual(["p-noe"]);
    expect(mentionedParticipantIDsFromText("hi @Noel!", roster)).toEqual(["p-noel"]); // ! 在词界符集合里
    expect(mentionedParticipantIDsFromText("hi @Noelx", roster)).toEqual([]); // 词没结束不算
  });

  it("accepts CJK punctuation boundaries and start/end anchors", () => {
    expect(mentionedParticipantIDsFromText("@墨白，帮个忙", roster)).toEqual(["p-mobai"]);
    expect(mentionedParticipantIDsFromText("问一下 @墨白", roster)).toEqual(["p-mobai"]);
    expect(mentionedParticipantIDsFromText("邮箱是 a@墨白b.com", roster)).toEqual([]);
  });

  it("returns empty for empty text and never sends empty arrays upstream", () => {
    expect(mentionedParticipantIDsFromText("   ", roster)).toEqual([]);
    expect(mentionedParticipantIDsFromText("没有提及任何人", roster)).toEqual([]);
  });
});

describe("mention composer helpers", () => {
  it("suggests while typing @prefix at the caret", () => {
    expect(mentionCandidates("问一下 @No", roster).map((p) => p.id)).toEqual(["p-noe", "p-noel"]);
    expect(mentionCandidates("问一下 @", roster)).toHaveLength(3);
    expect(mentionCandidates("问一下 @Noel 谢谢", roster)).toEqual([]); // 已完成的提及不再建议
    expect(mentionCandidates("没有 at", roster)).toEqual([]);
  });

  it("applyMention completes the trailing token", () => {
    expect(applyMention("问一下 @No", "Noel")).toBe("问一下 @Noel ");
    expect(applyMention("@", "墨白")).toBe("@墨白 ");
  });
});
