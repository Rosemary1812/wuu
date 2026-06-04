/**
 * Tests for the assistant turn front / body layout.
 *
 * The assistant turn renders into two distinct regions:
 *   - front content ("过程"区): reasoning, tool calls, context compaction,
 *     and commentary. Carries the work the model did.
 *   - body ("正文"区): the user-facing reply. Carries the final_answer
 *     items in chronological order.
 *
 * Commentary renders in the front using the same visual style as
 * final_answer — the two are distinguished by structural position, not
 * by decoration. A thin divider is drawn between the two regions when
 * the turn has exactly one final_answer and the turn shape is normal.
 *
 * Buggy turn shape: a completed turn that has the wrong number of
 * final_answers gets an orange border + warning banner. Pure-tool
 * turns (no text at all) are allowed to complete without a final
 * answer, so they're not a bug. Multi-final_answer turns are always
 * a bug — there's no single primary reply.
 *
 * Fold header: the front is always default-collapsed. The header
 * alone (status row + optional commentary preview row) is enough
 * to tell the user "something is / was happening here". The
 * commentary preview is a single-line snippet of the latest
 * commentary `agent_message` text, shown only while the turn is
 * in_progress — once the turn completes, the front is no longer
 * "live" and the preview is dropped.
 *
 * These tests exercise `buildAssistantTurnDisplay`, the pure
 * function that decides which items go to front vs body, whether the
 * turn is buggy, what the front's default collapse state should be,
 * and what the commentary preview text is. The render layer
 * (`AssistantTurnShell`) maps that display object directly to the
 * DOM, so verifying the function output verifies the layout.
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
// (which items end up in front vs body) without invoking React's
// rendering pipeline.
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

function frontItemIDs(display: ReturnType<typeof buildAssistantTurnDisplay>): string[] {
  if (!display) {
    return [];
  }
  // Each front entry is either a ToolActivityTimeline group (count > 1
  // for a stretch) or a single rendered element. We only need the
  // single-element entries here, which all have a key that is the
  // item id. (Tool timeline groups use `${item.id}-activity`, but
  // those groups don't appear in any of these four scenarios.)
  return display.frontEntries
    .map((entry) => entry.key)
    .filter((key) => !key.endsWith("-activity"));
}

function timelineItemIDs(display: ReturnType<typeof buildAssistantTurnDisplay>): string[] {
  if (!display) {
    return [];
  }
  // Tool activity groups use a single entry with a `-activity` suffix;
  // the underlying item id is the prefix.
  return display.frontEntries
    .map((entry) => entry.key)
    .filter((key) => key.endsWith("-activity"))
    .map((key) => key.slice(0, -"-activity".length));
}

function bodyItemIDs(display: ReturnType<typeof buildAssistantTurnDisplay>): string[] {
  if (!display) {
    return [];
  }
  return display.finalAnswerItems.map((entry) => entry.item.id);
}

describe("assistant turn front / body layout", () => {
  it("normal: commentary + tool_call + commentary + final_answer → front collapsed, body shown, divider drawn, not buggy", () => {
    const commentaryA = makeCommentary("Let me look that up.");
    const tool = makeToolCall("read_file");
    const commentaryB = makeCommentary("Got it. The file says...");
    const final = makeFinalAnswer("Here is the answer you wanted.");
    const turn = makeTurn({ items: [commentaryA, tool, commentaryB, final] });

    const display = buildAssistantTurnDisplay(turn, undefined, renderItem);

    expect(display).toBeDefined();
    // Front content carries the two commentary items in arrival order
    // plus the single tool call. (Tool calls render through
    // ToolActivityTimeline, which is a single group entry with a
    // `-activity` suffix; the underlying item is `tool`.)
    expect(frontItemIDs(display)).toEqual([commentaryA.id, commentaryB.id]);
    expect(timelineItemIDs(display)).toEqual([tool.id]);
    // Body carries the single final_answer.
    expect(bodyItemIDs(display)).toEqual([final.id]);
    // Normal case: divider is drawn between front and body, no bug
    // visual, and the front defaults to collapsed so the user sees
    // the answer first.
    expect(display?.showDivider).toBe(true);
    expect(display?.isBuggy).toBe(false);
    expect(display?.bugMessage).toBeUndefined();
    expect(display?.frontDefaultCollapsed).toBe(true);
  });

  it("missing final: commentary + tool_call + commentary, no final_answer → front collapsed, orange border + 'no final reply' banner", () => {
    const commentaryA = makeCommentary("Let me check.");
    const tool = makeToolCall("ls");
    const commentaryB = makeCommentary("Found something.");
    const turn = makeTurn({ items: [commentaryA, tool, commentaryB] });

    const display = buildAssistantTurnDisplay(turn, undefined, renderItem);

    expect(display).toBeDefined();
    // Body is empty.
    expect(bodyItemIDs(display)).toEqual([]);
    // Front carries the two commentary items and the tool call.
    expect(frontItemIDs(display)).toEqual([commentaryA.id, commentaryB.id]);
    expect(timelineItemIDs(display)).toEqual([tool.id]);
    // The turn started talking (commentary present) but never produced
    // a final answer → bug, no divider. Front stays default-collapsed;
    // the orange border + banner is the "look at me" signal.
    expect(display?.isBuggy).toBe(true);
    expect(display?.bugMessage).toBe("这次请求没有产生最终回复");
    expect(display?.showDivider).toBe(false);
    expect(display?.frontDefaultCollapsed).toBe(true);
    // Completed turn → no live commentary preview.
    expect(display?.latestCommentaryPreview).toBeUndefined();
  });

  it("multi final: commentary + final_answer_1 + commentary + final_answer_2 → front collapsed, both final_answers in body, orange border + 'multiple final replies' banner", () => {
    const commentaryA = makeCommentary("First thought.");
    const finalOne = makeFinalAnswer("First answer.");
    const commentaryB = makeCommentary("Second thought.");
    const finalTwo = makeFinalAnswer("Second answer.");
    const turn = makeTurn({
      items: [commentaryA, finalOne, commentaryB, finalTwo],
    });

    const display = buildAssistantTurnDisplay(turn, undefined, renderItem);

    expect(display).toBeDefined();
    // Front carries the two commentary items in arrival order.
    expect(frontItemIDs(display)).toEqual([commentaryA.id, commentaryB.id]);
    // Body carries BOTH final_answers, in chronological order — there
    // is no single primary to anchor a divider under, so the shell
    // skips the divider and surfaces a bug banner instead.
    expect(bodyItemIDs(display)).toEqual([finalOne.id, finalTwo.id]);
    expect(display?.isBuggy).toBe(true);
    expect(display?.bugMessage).toBe("这次请求产生了多个最终回复");
    expect(display?.showDivider).toBe(false);
    expect(display?.frontDefaultCollapsed).toBe(true);
    // Completed turn → no live commentary preview.
    expect(display?.latestCommentaryPreview).toBeUndefined();
  });

  it("pure tool: only tool_calls, no text — front default-collapsed in both states (pure tool is not a bug)", () => {
    const toolA = makeToolCall("read_file");
    const toolB = makeToolCall("grep");

    // in_progress: front is now default-collapsed in *all* states,
    // including in_progress. The status row (e.g. "正在回复 9s") plus
    // the chevron is enough to tell the user something is happening;
    // the user clicks to see the actual tool timeline. No commentary
    // exists → no preview row in the fold header.
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
    expect(inProgressDisplay?.finalAnswerItems).toHaveLength(0);
    expect(inProgressDisplay?.frontEntries).toHaveLength(1);
    expect(inProgressDisplay?.frontEntries[0]?.count).toBe(2);
    expect(timelineItemIDs(inProgressDisplay)).toEqual([toolA.id]);
    expect(inProgressDisplay?.isBuggy).toBe(false);
    expect(inProgressDisplay?.showDivider).toBe(false);
    expect(inProgressDisplay?.frontDefaultCollapsed).toBe(true);
    expect(inProgressDisplay?.latestCommentaryPreview).toBeUndefined();

    // completed: pure tool with no text is not a bug. The front
    // content stays around so the user can expand to see the tool
    // calls, but it defaults to collapsed so the turn doesn't take
    // visual space below the next turn.
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
    expect(completedDisplay?.finalAnswerItems).toHaveLength(0);
    expect(completedDisplay?.frontEntries).toHaveLength(1);
    expect(completedDisplay?.frontEntries[0]?.count).toBe(2);
    expect(timelineItemIDs(completedDisplay)).toEqual([toolA.id]);
    expect(completedDisplay?.isBuggy).toBe(false);
    expect(completedDisplay?.showDivider).toBe(false);
    expect(completedDisplay?.frontDefaultCollapsed).toBe(true);
    expect(completedDisplay?.latestCommentaryPreview).toBeUndefined();
  });
});

describe("assistant turn fold header preview", () => {
  it("in_progress + single commentary → preview = commentary text, under 120 chars passes through unchanged", () => {
    const commentary = makeCommentary("Let me look that up.");
    const turn = makeTurn({ status: "in_progress", items: [commentary] });

    const display = buildAssistantTurnDisplay(turn, undefined, renderItem);

    expect(display).toBeDefined();
    expect(display?.frontDefaultCollapsed).toBe(true);
    expect(display?.latestCommentaryPreview).toBe("Let me look that up.");
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
    // The last commentary in arrival order is the one the user
    // currently sees in the fold header — earlier ones are hidden
    // behind the fold.
    expect(display?.latestCommentaryPreview).toBe("Second thought.");
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
    expect(display?.latestCommentaryPreview).toBe("Got it. The file says...");
  });

  it("in_progress + long commentary → preview is truncated to 120 chars + ellipsis so the fold stays on two lines", () => {
    // 200 chars of 'a' — well past the 120-char cap.
    const longText = "a".repeat(200);
    const commentary = makeCommentary(longText);
    const turn = makeTurn({ status: "in_progress", items: [commentary] });

    const display = buildAssistantTurnDisplay(turn, undefined, renderItem);

    expect(display).toBeDefined();
    expect(display?.latestCommentaryPreview).toBe("a".repeat(120) + "…");
  });

  it("in_progress + reasoning only (no commentary) → preview is undefined, fold has no second row", () => {
    const reasoning = makeReasoning("thinking deeply about the problem");
    const turn = makeTurn({ status: "in_progress", items: [reasoning] });

    const display = buildAssistantTurnDisplay(turn, undefined, renderItem);

    expect(display).toBeDefined();
    // Reasoning is not commentary — the preview row has nothing to
    // show, so the fold header renders only the status row.
    expect(display?.latestCommentaryPreview).toBeUndefined();
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
    // The turn is done — the "live signal" no longer applies. The
    // fold header collapses to just the status line.
    expect(display?.latestCommentaryPreview).toBeUndefined();
  });
});
