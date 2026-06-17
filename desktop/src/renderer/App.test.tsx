/**
 * Tests for `buildAssistantTurnDisplay`, the pure function that maps
 * a turn's items onto the single `entries` list the shell renders.
 *
 * The shell then splits `entries` into two visual groups
 * (`process` fold + `answer` body), but that's a presentational
 * concern — the data model is one chronological list. Each entry has
 * a `position` derived from `item.phase` and a `settled` flag derived
 * from `item.status`. The shell uses `settled` to decide the fold's
 * default collapse state.
 *
 * Commentary text lives in the `process` position (it describes what
 * the model is doing, not what it is telling the user). `final_answer`
 * messages and phase-unknown live text live in the `answer` position.
 * Reasoning, tool calls, and other process types are also `process`.
 * Multi-`final_answer` turns keep all answer text in arrival order so
 * they don't expose internal stream shape.
 *
 * Empty reply: a completed turn that produced commentary but no
 * `final_answer` carries a `missingReplyMessage` — a user-facing
 * outcome, not an internal bug label.
 *
 * Live preview: the fold header shows the latest commentary text or
 * the latest tool command while the turn is `in_progress`.
 * Completed turns drop the preview; the fold header collapses to
 * the status line.
 */
import { describe, expect, it } from "vitest";
import { createElement, type JSX } from "react";
import type { ThreadItem, Turn, TurnStatus } from "../shared/protocol";
import { buildAssistantTurnDisplay } from "./AssistantTurnDisplay";

let idCounter = 0;
function nextID(prefix: string): string {
  idCounter += 1;
  return `${prefix}-${idCounter}`;
}

type BuildOptions = {
  status?: TurnStatus;
  items: ThreadItem[];
  durationMs?: number;
};

function makeTurn(options: BuildOptions): Turn {
  return {
    id: "turn-1",
    items: options.items,
    items_view: "full",
    status: options.status ?? "completed",
    duration_ms: options.durationMs,
  };
}

function makeCommentary(text: string): ThreadItem {
  return {
    id: nextID("commentary"),
    type: "agent_message",
    status: "completed",
    phase: "commentary",
    role: "assistant",
    text,
  };
}

function makeFinalAnswer(text: string): ThreadItem {
  return {
    id: nextID("final"),
    type: "agent_message",
    status: "completed",
    phase: "final_answer",
    role: "assistant",
    text,
  };
}

function makeLiveUnclassifiedAgentMessage(text: string): ThreadItem {
  return {
    id: nextID("pending-agent"),
    type: "agent_message",
    status: "in_progress",
    role: "assistant",
    text,
  };
}

function makeLegacyPendingAgentMessage(text: string): ThreadItem {
  return {
    id: nextID("pending-agent"),
    type: "agent_message",
    status: "in_progress",
    phase: "pending" as unknown as ThreadItem["phase"],
    role: "assistant",
    text,
  };
}

function makeToolCall(name = "lookup"): ThreadItem {
  return {
    id: nextID("tool"),
    type: "tool_call",
    status: "completed",
    name,
  };
}

function makeReasoning(text = "thinking..."): ThreadItem {
  return {
    id: nextID("reasoning"),
    type: "reasoning",
    status: "completed",
    text,
  };
}

// Render a stable text node per item so we can assert on identity
// (which items end up where) without invoking React's rendering
// pipeline.
function renderItem(item: ThreadItem): JSX.Element {
  return createElement("div", { "data-item-id": item.id }, textOf(item));
}

function textOf(item: ThreadItem): string {
  if (item.type === "agent_message") {
    return item.text ?? "";
  }
  if (item.type === "reasoning") {
    return item.text ?? "";
  }
  if (item.type === "tool_call" || item.type === "collab_agent_tool_call") {
    return item.name ?? "tool";
  }
  if (item.type === "context_compaction") {
    return item.text ?? "";
  }
  return "";
}

function processItemIDs(
  display: ReturnType<typeof buildAssistantTurnDisplay>,
): string[] {
  if (!display) return [];
  return display.entries
    .filter(
      (entry) => entry.position === "process" && entry.kind !== "activity",
    )
    .map((entry) => entry.item.id);
}

function activityItemIDs(
  display: ReturnType<typeof buildAssistantTurnDisplay>,
): string[] {
  if (!display) return [];
  return display.entries
    .filter((entry) => entry.position === "process" && entry.kind === "activity")
    .map((entry) => entry.item.id);
}

function answerItemIDs(
  display: ReturnType<typeof buildAssistantTurnDisplay>,
): string[] {
  if (!display) return [];
  return display.entries
    .filter((entry) => entry.position === "answer")
    .map((entry) => entry.item.id);
}

describe("assistant turn entries layout", () => {
  it("normal: commentary + tool + commentary + final_answer → process carries commentary + tool, answer carries final", () => {
    const commentaryA = makeCommentary("Let me look that up.");
    const tool = makeToolCall("read_file");
    const commentaryB = makeCommentary("Got it. The file says...");
    const final = makeFinalAnswer("Here is the answer you wanted.");
    const turn = makeTurn({ items: [commentaryA, tool, commentaryB, final] });

    const display = buildAssistantTurnDisplay(turn, undefined, renderItem);

    expect(display).toBeDefined();
    expect(processItemIDs(display)).toEqual([commentaryA.id, commentaryB.id]);
    expect(activityItemIDs(display)).toEqual([tool.id]);
    expect(answerItemIDs(display)).toEqual([final.id]);
    expect(display?.hasAnswer).toBe(true);
    expect(display?.missingReplyMessage).toBeUndefined();
  });

  it("missing final: commentary + tool + commentary, no final_answer → empty-reply notice", () => {
    const commentaryA = makeCommentary("Let me check.");
    const tool = makeToolCall("ls");
    const commentaryB = makeCommentary("Found something.");
    const turn = makeTurn({ items: [commentaryA, tool, commentaryB] });

    const display = buildAssistantTurnDisplay(turn, undefined, renderItem);

    expect(display).toBeDefined();
    expect(answerItemIDs(display)).toEqual([]);
    expect(processItemIDs(display)).toEqual([commentaryA.id, commentaryB.id]);
    expect(activityItemIDs(display)).toEqual([tool.id]);
    expect(display?.hasAnswer).toBe(false);
    // The turn started talking (commentary present) but never produced
    // a final answer → show a user-facing empty reply outcome.
    expect(display?.missingReplyMessage).toBe(
      "这轮只保留了过程记录，没有生成最终回答。",
    );
    // Completed turn → no live process preview.
    expect(display?.latestProcessPreview?.text).toBeUndefined();
  });

  it("multi final: commentary + final_1 + commentary + final_2 → answer keeps final text in order", () => {
    const commentaryA = makeCommentary("First thought.");
    const finalOne = makeFinalAnswer("First answer.");
    const commentaryB = makeCommentary("Second thought.");
    const finalTwo = makeFinalAnswer("Second answer.");
    const turn = makeTurn({
      items: [commentaryA, finalOne, commentaryB, finalTwo],
    });

    const display = buildAssistantTurnDisplay(turn, undefined, renderItem);

    expect(display).toBeDefined();
    expect(processItemIDs(display)).toEqual([commentaryA.id, commentaryB.id]);
    expect(answerItemIDs(display)).toEqual([finalOne.id, finalTwo.id]);
    expect(display?.hasAnswer).toBe(true);
    expect(display?.missingReplyMessage).toBeUndefined();
    expect(display?.latestProcessPreview?.text).toBeUndefined();
  });

  it("pure tool: only tool_calls, no text — entries carry activity, hasAnswer=false", () => {
    const toolA = makeToolCall("read_file");
    const toolB = makeToolCall("grep");

    // in_progress: activity is one grouped entry with count=2.
    const inProgressTurn = makeTurn({
      status: "in_progress",
      items: [toolA, toolB],
    });
    const inProgressDisplay = buildAssistantTurnDisplay(
      inProgressTurn,
      undefined,
      renderItem,
    );
    expect(inProgressDisplay).toBeDefined();
    expect(answerItemIDs(inProgressDisplay)).toEqual([]);
    const activityEntries = inProgressDisplay?.entries.filter(
      (e) => e.kind === "activity",
    );
    expect(activityEntries).toHaveLength(1);
    expect(activityEntries?.[0]?.count).toBe(2);
    expect(activityItemIDs(inProgressDisplay)).toEqual([toolA.id]);
    expect(inProgressDisplay?.hasAnswer).toBe(false);
    expect(inProgressDisplay?.missingReplyMessage).toBeUndefined();
    expect(inProgressDisplay?.latestProcessPreview?.text).toBe("搜索 内容");
    expect(inProgressDisplay?.latestProcessPreview?.kind).toBe("activity");

    // completed with no final answer: pure tool with no text is not a
    // bug. No empty-reply notice because there was no commentary.
    const completedTurn = makeTurn({
      status: "completed",
      items: [toolA, toolB],
      durationMs: 1500,
    });
    const completedDisplay = buildAssistantTurnDisplay(
      completedTurn,
      undefined,
      renderItem,
    );
    expect(completedDisplay).toBeDefined();
    expect(answerItemIDs(completedDisplay)).toEqual([]);
    const completedActivity = completedDisplay?.entries.filter(
      (e) => e.kind === "activity",
    );
    expect(completedActivity).toHaveLength(1);
    expect(completedActivity?.[0]?.count).toBe(2);
    expect(activityItemIDs(completedDisplay)).toEqual([toolA.id]);
    expect(completedDisplay?.hasAnswer).toBe(false);
    expect(completedDisplay?.missingReplyMessage).toBeUndefined();
    expect(completedDisplay?.latestProcessPreview?.text).toBeUndefined();
  });

  it("dedupes duplicate item ids so the same row never renders twice", () => {
    const commentary = makeCommentary("only one row");
    const final = makeFinalAnswer("the answer.");
    // Same id used twice — pathological resync shape. The shell
    // should never see two rows for the same item.
    const duplicate: ThreadItem = { ...commentary, id: "dupe" };
    const finalDup: ThreadItem = { ...final, id: "dupe-final" };
    const turn = makeTurn({
      status: "completed",
      items: [duplicate, duplicate, finalDup, finalDup],
    });
    const display = buildAssistantTurnDisplay(turn, undefined, renderItem);
    expect(display).toBeDefined();
    expect(display?.entries.filter((e) => e.item.id === "dupe")).toHaveLength(
      1,
    );
    expect(
      display?.entries.filter((e) => e.item.id === "dupe-final"),
    ).toHaveLength(1);
  });
});

describe("assistant turn fold header preview", () => {
  it("in_progress + single commentary → preview = commentary text, under 120 chars passes through unchanged", () => {
    const commentary = makeCommentary("Let me look that up.");
    const turn = makeTurn({ status: "in_progress", items: [commentary] });

    const display = buildAssistantTurnDisplay(turn, undefined, renderItem);

    expect(display).toBeDefined();
    expect(display?.latestProcessPreview?.text).toBe("Let me look that up.");
  });

  it("in_progress + phase-unknown live agent text → answer candidate, not process preview", () => {
    const pending = makeLiveUnclassifiedAgentMessage(
      "I will inspect the current prompt path.",
    );
    const turn = makeTurn({ status: "in_progress", items: [pending] });

    const display = buildAssistantTurnDisplay(turn, undefined, renderItem);

    expect(display).toBeDefined();
    expect(display?.latestProcessPreview?.text).toBeUndefined();
    expect(processItemIDs(display)).toEqual([]);
    const pendingEntry = display?.entries.find((e) => e.item.id === pending.id);
    expect(pendingEntry?.kind).toBe("answer");
    expect(answerItemIDs(display)).toEqual([pending.id]);
    expect(display?.hasAnswer).toBe(true);
  });

  it("in_progress + legacy pending live agent text → preview and process entry", () => {
    const pending = makeLegacyPendingAgentMessage(
      "I will inspect the current prompt path.",
    );
    const turn = makeTurn({ status: "in_progress", items: [pending] });

    const display = buildAssistantTurnDisplay(turn, undefined, renderItem);

    expect(display).toBeDefined();
    expect(display?.latestProcessPreview?.kind).toBe("pending");
    const pendingEntry = display?.entries.find((e) => e.item.id === pending.id);
    expect(pendingEntry?.kind).toBe("pending");
  });

  it("in_progress + multiple commentaries → preview = the LATEST commentary, in arrival order", () => {
    const commentaryA = makeCommentary("First thought.");
    const commentaryB = makeCommentary("Second thought.");
    const turn = makeTurn({
      status: "in_progress",
      items: [commentaryA, commentaryB],
    });

    const display = buildAssistantTurnDisplay(turn, undefined, renderItem);

    expect(display).toBeDefined();
    expect(display?.latestProcessPreview?.text).toBe("Second thought.");
  });

  it("in_progress + commentary interleaved with tool calls → preview = the commentary that came AFTER the tool, not before", () => {
    const commentaryA = makeCommentary("Let me look that up.");
    const tool = makeToolCall("read_file");
    const commentaryB = makeCommentary("Got it. The file says...");
    const turn = makeTurn({
      status: "in_progress",
      items: [commentaryA, tool, commentaryB],
    });

    const display = buildAssistantTurnDisplay(turn, undefined, renderItem);

    expect(display).toBeDefined();
    expect(display?.latestProcessPreview?.text).toBe("Got it. The file says...");
  });

  it("in_progress + tool after commentary → preview = latest tool action", () => {
    const commentary = makeCommentary("Let me look that up.");
    const tool = makeToolCall("read_file");
    const turn = makeTurn({
      status: "in_progress",
      items: [commentary, tool],
    });

    const display = buildAssistantTurnDisplay(turn, undefined, renderItem);

    expect(display).toBeDefined();
    expect(display?.latestProcessPreview?.text).toBe("读取 文件");
    expect(display?.latestProcessPreview?.kind).toBe("activity");
  });

  it("in_progress + long commentary → preview is truncated to 120 chars + ellipsis so the fold stays on two lines", () => {
    const longText = "a".repeat(200);
    const commentary = makeCommentary(longText);
    const turn = makeTurn({ status: "in_progress", items: [commentary] });

    const display = buildAssistantTurnDisplay(turn, undefined, renderItem);

    expect(display).toBeDefined();
    expect(display?.latestProcessPreview?.text).toBe("a".repeat(120) + "…");
  });

  it("in_progress + reasoning only (no commentary or tool action) → preview is undefined", () => {
    const reasoning = makeReasoning("thinking deeply about the problem");
    const turn = makeTurn({ status: "in_progress", items: [reasoning] });

    const display = buildAssistantTurnDisplay(turn, undefined, renderItem);

    expect(display).toBeDefined();
    expect(display?.latestProcessPreview?.text).toBeUndefined();
  });

  it("completed + commentary → preview is undefined; fold has status only, no live signal", () => {
    const commentary = makeCommentary("Let me look that up.");
    const final = makeFinalAnswer("Here is the answer.");
    const turn = makeTurn({
      status: "completed",
      items: [commentary, final],
      durationMs: 4200,
    });

    const display = buildAssistantTurnDisplay(turn, undefined, renderItem);

    expect(display).toBeDefined();
    expect(display?.latestProcessPreview?.text).toBeUndefined();
  });

  it("every entry exposes settled from item.status (single source of truth)", () => {
    const commentary = makeCommentary("settled commentary");
    const livePending = makeLiveUnclassifiedAgentMessage("streaming text");
    const turn = makeTurn({
      status: "in_progress",
      items: [commentary, livePending],
    });
    const display = buildAssistantTurnDisplay(turn, undefined, renderItem);
    expect(display).toBeDefined();
    const commentaryEntry = display?.entries.find(
      (e) => e.item.id === commentary.id,
    );
    const liveEntry = display?.entries.find(
      (e) => e.item.id === livePending.id,
    );
    expect(commentaryEntry?.settled).toBe(true);
    expect(commentaryEntry?.streaming).toBe(false);
    expect(liveEntry?.settled).toBe(false);
    expect(liveEntry?.streaming).toBe(true);
  });
});
