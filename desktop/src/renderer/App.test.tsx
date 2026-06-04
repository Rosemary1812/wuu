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
 * Default collapse state of the front content:
 *   - in_progress: false (the user wants to watch the work)
 *   - buggy: false (the user needs to see what went wrong)
 *   - completed + ≤1 final_answer: true (the normal "I scrolled past
 *     this turn" case)
 *
 * These tests exercise `buildAssistantTurnDisplay`, the pure
 * function that decides which items go to front vs body, whether the
 * turn is buggy, and what the front's default collapse state should
 * be. The render layer (`AssistantTurnShell`) maps that display
 * object directly to the DOM, so verifying the function output
 * verifies the layout.
 */
import { describe, expect, it } from "vitest";
import { createElement, type JSX } from "react";
import type { ThreadItem, Turn, TurnStatus } from "../shared/protocol";
import { buildAssistantTurnDisplay } from "./App";

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

  it("missing final: commentary + tool_call + commentary, no final_answer → front expanded, orange border + 'no final reply' banner", () => {
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
    // a final answer → bug, no divider, front forced expanded.
    expect(display?.isBuggy).toBe(true);
    expect(display?.bugMessage).toBe("这次请求没有产生最终回复");
    expect(display?.showDivider).toBe(false);
    expect(display?.frontDefaultCollapsed).toBe(false);
  });

  it("multi final: commentary + final_answer_1 + commentary + final_answer_2 → front expanded, both final_answers in body, orange border + 'multiple final replies' banner", () => {
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
    expect(display?.frontDefaultCollapsed).toBe(false);
  });

  it("pure tool: only tool_calls, no text — in_progress forces expand, completed stays collapsed (pure tool is not a bug)", () => {
    const toolA = makeToolCall("read_file");
    const toolB = makeToolCall("grep");

    // in_progress: the user wants to watch the work, front is forced
    // expanded. No final_answer exists yet, but pure tool with no
    // text at all is allowed in either state. Two consecutive tool
    // calls collapse into a single ToolActivityTimeline group entry
    // (count = 2) so the front has exactly one element.
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
    expect(inProgressDisplay?.frontDefaultCollapsed).toBe(false);

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
  });
});
