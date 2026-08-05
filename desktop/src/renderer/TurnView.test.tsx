import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ThreadItem, Turn } from "../shared/protocol";
import { ASSISTANT_TURN_PRESENTATION_STABILIZE_MS } from "./AssistantTurnPresentation";
import { PROCESS_NOTIFICATION_NAME } from "./InternalUserNotification";
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

function makeFinalAnswer(
  text: string,
  status: ThreadItem["status"] = "completed",
): ThreadItem {
  return {
    id: "answer-1",
    type: "agent_message",
    status,
    phase: "final_answer",
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

function render(turn: Turn, onOpenRuns?: () => void): HTMLDivElement {
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  act(() => {
    root!.render(
      <TurnView
        turn={turn}
        onStreamFrame={() => {}}
        onOpenRuns={onOpenRuns}
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

  it("hides named and legacy process notifications from the direct turn renderer", () => {
    const processText =
      '<process_notification>{"process_id":"proc-legacy"}</process_notification>';
    const view = render(
      makeTurn("completed", [
        {
          id: "process-named",
          type: "user_message",
          name: PROCESS_NOTIFICATION_NAME,
          text: '<process_notification>{"process_id":"proc-named"}</process_notification>',
        },
        {
          id: "process-legacy",
          type: "user_message",
          text: processText,
        },
        {
          id: "user-1",
          type: "user_message",
          text: "真正的用户消息",
        },
      ]),
    );

    expect(view.textContent).not.toContain("proc-named");
    expect(view.textContent).not.toContain("proc-legacy");
    expect(view.textContent).toContain("真正的用户消息");
    expect(view.querySelectorAll(".user-message-block")).toHaveLength(1);
  });

  it("keeps completed turn outputs visible after the turn is no longer latest", () => {
    const view = render(
      makeTurn("completed", [
        {
          id: "write-1",
          type: "tool_call",
          name: "write_file",
          status: "completed",
          arguments: JSON.stringify({ path: "notes/brief.md", content: "# Brief\n" }),
          result: JSON.stringify({
            path: "notes/brief.md",
            diff: { new_file: true, lines: 1 },
          }),
        },
      ]),
    );

    expect(view.textContent).toContain("本轮产出 1 项");
    expect(view.textContent).toContain("brief.md");
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

  it("publishes the completed final answer without the structural buffer delay", () => {
    vi.useFakeTimers();
    const view = render(
      makeTurn("in_progress", [makeFinalAnswer("partial reply", "in_progress")]),
    );

    expect(view.textContent).toContain("partial reply");

    rerender(
      makeTurn("completed", [makeFinalAnswer("partial reply and final text")]),
    );

    expect(view.textContent).toContain("partial reply and final text");
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

  it("preserves partial output without a notice after a manual interruption", () => {
    const view = render(
      makeTurn("interrupted", [
        makeCommentary("partial progress"),
        makeError("context canceled"),
      ]),
    );

    expect(view.textContent).toContain("partial progress");
    expect(view.querySelectorAll(".turn-notice")).toHaveLength(0);
    expect(view.textContent).not.toContain("已停止");
    expect(view.textContent).not.toContain("回复已中断");
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
    expect(view.textContent).toContain("网络异常");
    expect(view.textContent).not.toContain("previous_response_not_found");
    expect(view.querySelectorAll(".turn-notice button, .turn-notice a")).toHaveLength(0);
  });

  it("hides a transient stream error item while the turn is still retrying", () => {
    // A retryable attempt that failed terminally lands an error item, but
    // the turn keeps running (reconnect chip carries the cause). Rendering
    // the item here would pile up one settled error line per attempt.
    const view = render(
      makeTurn("in_progress", [
        makeCommentary("partial progress"),
        makeError("HTTP 429: Too Many Requests"),
      ]),
    );

    expect(view.querySelectorAll(".turn-notice")).toHaveLength(0);
    expect(view.textContent).not.toContain("429");
  });

  it("drops a transient stream error item once the turn recovered and completed", () => {
    // The retry succeeded: nothing about the superseded failure should stay
    // behind in the finished turn.
    const view = render(
      makeTurn("completed", [
        makeFinalAnswer("done"),
        makeError("HTTP 429: Too Many Requests"),
      ]),
    );

    expect(view.textContent).toContain("done");
    expect(view.querySelectorAll(".turn-notice")).toHaveLength(0);
    expect(view.textContent).not.toContain("429");
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

  it("offers the turn's command runs from the final answer actions", () => {
    const onOpenRuns = vi.fn();
    const view = render(
      makeTurn("completed", [
        {
          id: "call-1",
          type: "tool_call",
          status: "completed",
          name: "bash",
          arguments: JSON.stringify({ command: "npm test" }),
          display: { kind: "command", capability: "command.bash" },
          result: JSON.stringify({ exit_code: 0 }),
        },
        makeFinalAnswer("done"),
      ]),
      onOpenRuns,
    );

    const button = view.querySelector<HTMLButtonElement>(
      "button:has(.lucide-square-terminal)",
    );
    expect(button).not.toBeNull();
    act(() => button?.click());
    expect(onOpenRuns).toHaveBeenCalledTimes(1);
  });

  it("renders no run action when a settled turn has no final answer", () => {
    const onOpenRuns = vi.fn();
    const view = render(
      makeTurn("failed", [
        {
          id: "call-1",
          type: "tool_call",
          status: "failed",
          name: "bash",
          arguments: JSON.stringify({ command: "npm test" }),
          display: { kind: "command", capability: "command.bash" },
          result: JSON.stringify({ exit_code: 1 }),
        },
      ]),
      onOpenRuns,
    );

    // The terminal affordance lives only in the final answer's action bar;
    // without an answer there is no bar and no standalone row either.
    expect(view.querySelector(".turn-run-actions")).toBeNull();
    expect(view.querySelector("button:has(.lucide-square-terminal)")).toBeNull();
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
