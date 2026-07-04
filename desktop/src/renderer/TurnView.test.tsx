import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ThreadItem, Turn } from "../shared/protocol";
import { ASSISTANT_TURN_PRESENTATION_STABILIZE_MS } from "./AssistantTurnPresentation";
import { TurnView } from "./TurnView";

let root: Root | undefined;
let container: HTMLDivElement | undefined;

function makeTurn(
  status: Turn["status"],
  items: ThreadItem[] = [],
  error?: string,
): Turn {
  return {
    id: "turn-1",
    items,
    items_view: "full",
    status,
    error: error ? { message: error } : undefined,
  };
}

function makeCommentary(text: string): ThreadItem {
  return {
    id: "commentary-1",
    type: "agent_message",
    status: "completed",
    phase: "commentary",
    role: "assistant",
    text,
  };
}

function makeError(error: string): ThreadItem {
  return {
    id: "error-1",
    type: "error",
    status: "failed",
    error,
  };
}

function makeReasoning(text: string, id = "reasoning-1"): ThreadItem {
  return {
    id,
    type: "reasoning",
    status: "completed",
    text,
  };
}

function render(turn: Turn): HTMLDivElement {
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  act(() => {
    root!.render(
      <TurnView
        turn={turn}
        onStreamFrame={() => {}}
        onNoticeAction={() => {}}
      />,
    );
  });
  return container;
}

function rerender(turn: Turn): void {
  act(() => {
    root!.render(
      <TurnView
        turn={turn}
        onStreamFrame={() => {}}
        onNoticeAction={() => {}}
      />,
    );
  });
}

afterEach(() => {
  vi.useRealTimers();
  act(() => {
    root?.unmount();
  });
  container?.remove();
  root = undefined;
  container = undefined;
});

describe("TurnView", () => {
  it("marks the turn root with the live status used by scroll-stable CSS", () => {
    const view = render(makeTurn("in_progress"));

    const turn = view.querySelector<HTMLElement>(".turn");
    expect(turn?.dataset.turnStatus).toBe("in_progress");
  });

  it("buffers structural process changes briefly while keeping the current text visible", () => {
    vi.useFakeTimers();
    const view = render(
      makeTurn("in_progress", [makeCommentary("checking the files")]),
    );

    expect(view.textContent).toContain("checking the files");
    expect(view.textContent).not.toContain("查看思考过程");

    rerender(
      makeTurn("in_progress", [
        makeCommentary("checking the files"),
        makeReasoning("settled reasoning"),
      ]),
    );

    expect(view.textContent).toContain("checking the files");
    expect(view.textContent).not.toContain("查看思考过程");

    act(() => {
      vi.advanceTimersByTime(ASSISTANT_TURN_PRESENTATION_STABILIZE_MS - 1);
    });
    expect(view.textContent).not.toContain("查看思考过程");

    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(view.textContent).toContain("查看思考过程");
  });

  it("coalesces repeated structural changes without extending the buffer forever", () => {
    vi.useFakeTimers();
    const view = render(makeTurn("in_progress", [makeCommentary("checking")]));

    rerender(
      makeTurn("in_progress", [
        makeCommentary("checking"),
        makeReasoning("first reasoning", "reasoning-1"),
      ]),
    );
    act(() => {
      vi.advanceTimersByTime(ASSISTANT_TURN_PRESENTATION_STABILIZE_MS / 2);
    });
    expect(view.textContent).not.toContain("思考过程");

    rerender(
      makeTurn("in_progress", [
        makeCommentary("checking"),
        makeReasoning("first reasoning", "reasoning-1"),
        makeReasoning("second reasoning", "reasoning-2"),
      ]),
    );
    act(() => {
      vi.advanceTimersByTime(ASSISTANT_TURN_PRESENTATION_STABILIZE_MS / 2);
    });

    expect(view.textContent).toContain("思考过程");
  });

  it("renders one stop notice when a manual interruption also records a cancellation error item", () => {
    const view = render(
      makeTurn("interrupted", [
        makeCommentary("partial progress"),
        makeError("context canceled"),
      ]),
    );

    expect(view.textContent).toContain("partial progress");
    expect(view.querySelectorAll(".turn-notice")).toHaveLength(1);
    expect(view.textContent?.match(/已停止/g)).toHaveLength(1);
  });

  it("renders one failure notice when a failed turn also records an error item", () => {
    const view = render(
      makeTurn(
        "failed",
        [
          makeCommentary("partial progress"),
          makeError("wait: context deadline exceeded"),
        ],
        "stream request failed: stream error (previous_response_not_found)",
      ),
    );

    expect(view.textContent).toContain("partial progress");
    expect(view.querySelectorAll(".turn-notice")).toHaveLength(1);
    // The chip title now surfaces the wrapped provider error code so a
    // screenshot carries enough signal to triage whose problem it is.
    expect(view.textContent?.match(/previous_response_not_found/g)).toHaveLength(1);
  });

  it("renders a single warning chip when a completed turn has commentary but no final answer", () => {
    // Pin the chip-pipeline contract: a completed turn whose items
    // carry only `commentary` (no `final_answer`) now surfaces a
    // warning chip via the same TurnEventNotice path that
    // cancelled / failed / context-compaction notices use. The
    // hand-rolled `.assistant-turn-missing-reply` aside is gone.
    vi.useFakeTimers();
    const view = render(
      makeTurn("completed", [makeCommentary("thinking out loud")]),
    );
    // The presentation buffer delays publishing the display for
    // ASSISTANT_TURN_PRESENTATION_STABILIZE_MS so the fold body has
    // time to settle. Advance past it so the chip mounts.
    act(() => {
      vi.advanceTimersByTime(ASSISTANT_TURN_PRESENTATION_STABILIZE_MS);
    });

    // The process records (commentary) are still visible in the fold.
    expect(view.textContent).toContain("thinking out loud");
    // One unified chip, with the warning tone class and the short
    // "无最终回答" title. The `warning` tone drives the yellow
    // background via `.turn-notice.warning .turn-event-content`.
    const notices = view.querySelectorAll(
      ".turn-notice.turn-event-notice.warning",
    );
    expect(notices).toHaveLength(1);
    expect(view.querySelector(".turn-event-title")?.textContent).toBe(
      "无最终回答",
    );
    // The legacy className and Info icon are gone — the missing-reply
    // case no longer hand-rolls its own markup.
    expect(view.querySelector(".assistant-turn-missing-reply")).toBeNull();
    expect(view.querySelector(".turn-notice-icon")).toBeNull();
  });
});

describe("TurnView optimistic placeholder", () => {
  it("shows the status label and live timer immediately for a just-sent optimistic turn", async () => {
    const { createOptimisticTurn } = await import("./ComposerMessages");
    const optimistic = createOptimisticTurn(
      { id: "queued-1", text: "帮我看看这个测试", images: [], files: [] },
      Date.now(),
    );
    const view = render(optimistic);

    // The user's message is visible right away.
    expect(view.textContent).toContain("帮我看看这个测试");
    // The process header mounts in the same frame — the user never faces
    // a bare hairline: label + elapsed timer are there from the start.
    expect(view.querySelector(".turn-process-title")?.textContent).toBe(
      "正在处理",
    );
    expect(view.querySelector(".turn-process-meta")?.textContent).toMatch(
      /^\d+s$/,
    );
  });
});
