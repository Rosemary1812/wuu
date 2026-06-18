/**
 * Tests for `AssistantTurnShell`. The shell is responsible for the visual
 * split between process records (commentary, tool calls, reasoning) and
 * the final answer body. The key product decision verified here: the
 * process fold must default to expanded even after the final answer
 * arrives, so the user can scroll back through the agent's working
 * trail. The Codex-style "collapse when answer appears" behavior used
 * to hide commentary mid-stream and read as the agent forgetting what
 * it had just said; the regression these tests guard against is
 * re-introducing that collapse-on-answer behavior.
 */
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { createElement, type JSX } from "react";
import type { ThreadItem, Turn } from "../shared/protocol";
import { buildAssistantTurnDisplay } from "./AssistantTurnDisplay";
import { AssistantTurnShell } from "./AssistantTurnShell";

let idCounter = 0;
function nextID(prefix: string): string {
  idCounter += 1;
  return `${prefix}-${idCounter}`;
}

function makeTurn(
  status: Turn["status"],
  items: ThreadItem[],
  durationMs?: number,
): Turn {
  return {
    id: "turn-1",
    items,
    items_view: "full",
    status,
    duration_ms: durationMs,
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

function renderShell(display: ReturnType<typeof buildAssistantTurnDisplay> | undefined, turn: Turn): HTMLDivElement {
  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => {
    root.render(
      createElement(AssistantTurnShell, {
        turn,
        display: display!,
        onStreamFrame: () => {},
        onNoticeAction: () => {},
      }),
    );
  });
  return container;
}

function detailsElement(container: HTMLElement): HTMLDetailsElement | null {
  return container.querySelector("details.turn-process-fold");
}

function summaryElement(container: HTMLElement): HTMLElement | null {
  return container.querySelector("summary.turn-process-toggle");
}

function foldBodyVisible(container: HTMLElement): boolean {
  const details = detailsElement(container);
  if (!details) return false;
  const body = container.querySelector(".turn-process-fold-body");
  if (!body) return false;
  // <details> with `open` renders its body; closed details still have
  // the body in the DOM but the browser hides it. Use the explicit
  // `open` attribute as the source of truth.
  return details.hasAttribute("open");
}

beforeEach(() => {
  idCounter = 0;
});

afterEach(() => {
  document.body.innerHTML = "";
});

describe("AssistantTurnShell — process fold default state", () => {
  it("expands the process fold when only commentary is present", () => {
    const turn = makeTurn("completed", [
      makeCommentary("step one"),
      makeCommentary("step two"),
    ]);
    const display = buildAssistantTurnDisplay(
      turn,
      undefined,
      (item, streaming) =>
        createElement("div", null, item.text ?? ""),
    );
    const container = renderShell(display, turn);

    expect(foldBodyVisible(container)).toBe(true);
    expect(detailsElement(container)).not.toBeNull();
  });

  it("expands the process fold even when a final answer is present", () => {
    // Regression guard: the previous Codex-style design collapsed the
    // process fold as soon as `answerEntries.length > 0`, hiding
    // commentary mid-stream. The fold must stay open so the user can
    // scroll back through the working trail.
    const turn = makeTurn("completed", [
      makeCommentary("checking files"),
      makeToolCall("read_file"),
      makeCommentary("running tests"),
      makeFinalAnswer("All green."),
    ]);
    const display = buildAssistantTurnDisplay(
      turn,
      undefined,
      (item, streaming) =>
        createElement("div", null, item.text ?? ""),
    );
    const container = renderShell(display, turn);

    const details = detailsElement(container);
    expect(details).not.toBeNull();
    expect(details?.hasAttribute("open")).toBe(true);

    // And the commentary text itself must be in the rendered DOM, not
    // hidden inside a closed fold.
    expect(container.textContent).toContain("checking files");
    expect(container.textContent).toContain("running tests");
  });

  it("expands the process fold for in-progress turns (no final answer yet)", () => {
    const turn = makeTurn("in_progress", [
      makeCommentary("streaming commentary..."),
    ]);
    const display = buildAssistantTurnDisplay(
      turn,
      undefined,
      (item, streaming) =>
        createElement("div", null, item.text ?? ""),
    );
    const container = renderShell(display, turn);

    expect(foldBodyVisible(container)).toBe(true);
  });

  it("does not render a process fold when there are no process records", () => {
    const turn = makeTurn("completed", [makeFinalAnswer("just the answer")]);
    const display = buildAssistantTurnDisplay(
      turn,
      undefined,
      (item, streaming) =>
        createElement("div", null, item.text ?? ""),
    );
    const container = renderShell(display, turn);

    expect(detailsElement(container)).toBeNull();
    // But the final answer must still render in the answer body.
    expect(container.textContent).toContain("just the answer");
    expect(container.querySelector(".turn-answer-body")).not.toBeNull();
  });

  it("includes reasoning in the expanded process fold", () => {
    const turn = makeTurn("completed", [
      makeReasoning("let me think about this"),
      makeFinalAnswer("here you go"),
    ]);
    const display = buildAssistantTurnDisplay(
      turn,
      undefined,
      (item, streaming) =>
        createElement("div", null, item.text ?? ""),
    );
    const container = renderShell(display, turn);

    expect(foldBodyVisible(container)).toBe(true);
    expect(container.textContent).toContain("let me think about this");
  });

  it("lets the user collapse the fold manually after the answer arrives", () => {
    // The user should still be able to collapse the fold for long
    // turns; we only changed the default, not the toggle.
    const turn = makeTurn("completed", [
      makeCommentary("long preamble"),
      makeFinalAnswer("short answer"),
    ]);
    const display = buildAssistantTurnDisplay(
      turn,
      undefined,
      (item, streaming) =>
        createElement("div", null, item.text ?? ""),
    );
    const container = renderShell(display, turn);
    expect(foldBodyVisible(container)).toBe(true);

    const summary = summaryElement(container);
    expect(summary).not.toBeNull();
    // Toggle the <details> directly via the DOM event React listens to.
    // Using `summary.click()` triggers React's controlled `open` state
    // update on the next tick, which we capture inside act().
    act(() => {
      const details = detailsElement(container);
      const event = new Event("toggle", { bubbles: true });
      details?.dispatchEvent(event);
      // Manually drive the controlled `open` state by toggling the
      // attribute React reads on the next render. The shell's
      // `onToggle` handler flips `expanded`, so calling it directly
      // avoids depending on jsdom's <details> click behavior.
      (summary as HTMLElement).click();
    });

    expect(foldBodyVisible(container)).toBe(false);
  });
});