/**
 * Tests for the assistant turn front / body layout.
 *
 * The assistant turn renders into two distinct regions:
 *   - front content ("过程"区): reasoning, tool calls, context compaction,
 *     pending assistant text, and commentary. Carries the work the model did.
 *   - body ("正文"区): the user-facing reply. Carries final_answer
 *     text segments in arrival order.
 *
 * Commentary renders in the front/process lane. While a turn is still
 * live, the latest process record appears as a compact preview in the
 * fold header instead of being treated as body text. A thin divider is
 * drawn between the two regions when the completed turn has final_answer
 * body text.
 *
 * Empty reply: a completed turn that produced commentary but no
 * final_answer gets a compact "没有生成回复" notice. Pure-tool turns
 * (no text at all) are allowed to complete without a final answer.
 * Multi-final_answer turns keep all body text in order instead of
 * exposing an internal structure issue to the user.
 *
 * Fold header: the front is expanded while there is no final answer
 * yet, then default-collapses as soon as final_answer body appears.
 * The process preview is a single-line snippet of the latest pending
 * text, commentary, or tool call, shown only while the turn is
 * in_progress — once the turn completes, the front is no longer
 * "live" and the preview is dropped.
 *
 * These tests exercise `buildAssistantTurnDisplay`, the pure
 * function that decides which items go to front vs body, whether the
 * turn needs an empty-reply notice, what the front's default collapse state should be,
 * and what the process preview text is. The render layer
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

function makeLiveUnclassifiedAgentMessage(text: string): ThreadItem {
  return {
    id: nextID("pending-agent"),
    type: "agent_message",
    status: "in_progress",
    role: "assistant",
    text,
  };
}

function makePendingAgentMessage(text: string): ThreadItem {
  return {
    id: nextID("pending-agent"),
    type: "agent_message",
    status: "in_progress",
    phase: "pending",
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
  it("normal: commentary + tool_call + commentary + final_answer → front collapsed, body shown, divider drawn", () => {
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
    // Normal case: divider is drawn between front and body, no empty
    // reply notice, and the front defaults to collapsed so the user sees
    // the answer first.
    expect(display?.showDivider).toBe(true);
    expect(display?.missingReplyMessage).toBeUndefined();
    expect(display?.frontDefaultCollapsed).toBe(true);
  });

  it("missing final: commentary + tool_call + commentary, no final_answer → front collapsed, empty-reply notice", () => {
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
    // a final answer → show a user-facing empty reply outcome.
    expect(display?.missingReplyMessage).toBe(
      "这轮只保留了过程记录，没有生成最终回答。",
    );
    expect(display?.showDivider).toBe(false);
    expect(display?.frontDefaultCollapsed).toBe(false);
    // Completed turn → no live process preview.
    expect(display?.latestProcessPreview?.text).toBeUndefined();
  });

  it("multi final: commentary + final_answer_1 + commentary + final_answer_2 → body keeps final text in order", () => {
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
    // Body keeps both final_answer text segments. Multiple finals are
    // treated as a stream shape detail, not a user-facing warning state.
    expect(bodyItemIDs(display)).toEqual([finalOne.id, finalTwo.id]);
    expect(display?.missingReplyMessage).toBeUndefined();
    expect(display?.showDivider).toBe(true);
    expect(display?.frontDefaultCollapsed).toBe(true);
    // Completed turn → no live process preview.
    expect(display?.latestProcessPreview?.text).toBeUndefined();
  });

  it("pure tool: only tool_calls, no text — front default-collapsed in both states (pure tool is not a bug)", () => {
    const toolA = makeToolCall("read_file");
    const toolB = makeToolCall("grep");

    // in_progress before any final answer: process stays expanded so
    // the user can see what the model is doing.
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
    expect(inProgressDisplay?.missingReplyMessage).toBeUndefined();
    expect(inProgressDisplay?.showDivider).toBe(false);
    expect(inProgressDisplay?.frontDefaultCollapsed).toBe(false);
    expect(inProgressDisplay?.latestProcessPreview?.text).toBe("搜索 内容");
    expect(inProgressDisplay?.latestProcessPreview?.kind).toBe("activity");

    // completed with no final answer: pure tool with no text is not a
    // bug. Since there is no body to replace the process, the process
    // remains expanded by default.
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
    expect(completedDisplay?.missingReplyMessage).toBeUndefined();
    expect(completedDisplay?.showDivider).toBe(false);
    expect(completedDisplay?.frontDefaultCollapsed).toBe(false);
    expect(completedDisplay?.latestProcessPreview?.text).toBeUndefined();
  });
});

describe("assistant turn fold header preview", () => {
  it("in_progress + single commentary → preview = commentary text, under 120 chars passes through unchanged", () => {
    const commentary = makeCommentary("Let me look that up.");
    const turn = makeTurn({ status: "in_progress", items: [commentary] });

    const display = buildAssistantTurnDisplay(turn, undefined, renderItem);

    expect(display).toBeDefined();
    expect(display?.frontDefaultCollapsed).toBe(false);
    expect(display?.latestProcessPreview?.text).toBe("Let me look that up.");
  });

  it("in_progress + pending live agent text → preview and expanded process entry, not body", () => {
    const pending = makePendingAgentMessage(
      "I will inspect the current prompt path.",
    );
    const turn = makeTurn({ status: "in_progress", items: [pending] });

    const display = buildAssistantTurnDisplay(turn, undefined, renderItem);

    expect(display).toBeDefined();
    expect(display?.latestProcessPreview?.text).toBe(
      "I will inspect the current prompt path.",
    );
    expect(display?.latestProcessPreview?.kind).toBe("pending");
    expect(display?.frontEntries).toHaveLength(1);
    expect(display?.frontEntries[0]?.kind).toBe("pending");
    expect(display?.finalAnswerItems).toHaveLength(0);
    expect(display?.frontDefaultCollapsed).toBe(false);
  });

  it("in_progress + legacy unclassified live agent text → treated as pending", () => {
    const pending = makeLiveUnclassifiedAgentMessage(
      "I will inspect the current prompt path.",
    );
    const turn = makeTurn({ status: "in_progress", items: [pending] });

    const display = buildAssistantTurnDisplay(turn, undefined, renderItem);

    expect(display).toBeDefined();
    expect(display?.latestProcessPreview?.kind).toBe("pending");
    expect(display?.frontEntries).toHaveLength(1);
    expect(display?.frontEntries[0]?.kind).toBe("pending");
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
    // 200 chars of 'a' — well past the 120-char cap.
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
    // The turn is done — the "live signal" no longer applies. The
    // fold header collapses to just the status line.
    expect(display?.latestProcessPreview?.text).toBeUndefined();
  });
});
