import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { ThreadItem, Turn } from "../shared/protocol";
import { TurnGroupView } from "./TurnGroupView";

let container: HTMLDivElement;
let root: Root | null = null;

beforeEach(() => {
  container = document.createElement("div");
  document.body.appendChild(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  root = null;
  container.remove();
});

function render(element: JSX.Element): void {
  act(() => {
    if (!root) {
      root = createRoot(container);
    }
    root.render(element);
  });
}

let idCounter = 0;
function nextID(prefix: string): string {
  idCounter += 1;
  return `${prefix}-${idCounter}`;
}

function userItem(text: string): ThreadItem {
  return { id: nextID("user"), type: "user_message", text };
}

function wakeItem(agentID: string): ThreadItem {
  return {
    id: nextID("wake"),
    type: "user_message",
    name: "wuu_agent_notification",
    text: `<subagent_notification>{"status":{"agent_id":"${agentID}","task_name":"${agentID.replace(/^agent-/, "")}","status":"completed"}}</subagent_notification>`,
  };
}

function spawnItem(name: string): ThreadItem {
  return {
    id: nextID("spawn"),
    type: "tool_call",
    status: "completed",
    name: "spawn_agent",
    arguments: JSON.stringify({ description: name, run_in_background: true }),
    result: JSON.stringify({
      agent_id: `agent-${name}`,
      task_name: name,
      status: "running",
    }),
  };
}

function answerItem(text: string): ThreadItem {
  return {
    id: nextID("answer"),
    type: "agent_message",
    status: "completed",
    phase: "final_answer",
    text,
  };
}

function commandItem(): ThreadItem {
  return {
    id: nextID("cmd"),
    type: "tool_call",
    status: "completed",
    name: "bash",
    arguments: JSON.stringify({ command: "npm test" }),
    display: { kind: "command", capability: "command.bash" },
    result: JSON.stringify({ exit_code: 0 }),
  };
}

function makeTurn(
  id: string,
  items: ThreadItem[],
  options: {
    status?: Turn["status"];
    startedAt?: string;
    completedAt?: string;
    durationMs?: number;
  } = {},
): Turn {
  return {
    id,
    items_view: "full",
    status: options.status ?? "completed",
    started_at: options.startedAt,
    completed_at: options.completedAt,
    duration_ms: options.durationMs,
    items,
  };
}

function mountGroup(
  turns: Turn[],
  awaiting = false,
  onOpenRuns?: (turnID: string) => void,
  interrupted = false,
): void {
  render(
    <TurnGroupView
      turns={turns}
      awaiting={awaiting}
      interrupted={interrupted}
      onStreamFrame={() => {}}
      onOpenRuns={onOpenRuns}
    />,
  );
}

function actionBars(): HTMLElement[] {
  return Array.from(
    container.querySelectorAll<HTMLElement>(".agent-message-actions"),
  );
}

function section(): HTMLElement {
  const el = container.querySelector<HTMLElement>("section.turn");
  if (!el) throw new Error("expected a turn section");
  return el;
}

function foldToggle(): HTMLElement {
  const el = container.querySelector<HTMLElement>(".turn-process-toggle");
  if (!el) throw new Error("expected a process fold toggle");
  return el;
}

describe("TurnGroupView — merged orchestration group", () => {
  it("renders a spawn turn + wake turn as one shell with one action bar", () => {
    const turns = [
      makeTurn("t1", [userItem("帮我查 X"), spawnItem("research_a"), answerItem("我先派一个子任务。")], {
        startedAt: "2026-07-29T10:00:00Z",
        completedAt: "2026-07-29T10:00:12Z",
        durationMs: 12_000,
      }),
      makeTurn("t2", [wakeItem("agent-research_a"), answerItem("汇总如下。")], {
        startedAt: "2026-07-29T10:01:20Z",
        completedAt: "2026-07-29T10:01:42Z",
        durationMs: 22_000,
      }),
    ];
    mountGroup(turns);

    // One section, one process fold, and the wake status stays in the timeline.
    expect(container.querySelectorAll("section.turn")).toHaveLength(1);
    expect(container.querySelectorAll(".turn-process-fold")).toHaveLength(1);
    expect(container.querySelector(".turn-subagent-status")?.textContent).toContain(
      "完成了",
    );
    // Only the group's final answer carries actions.
    expect(actionBars()).toHaveLength(1);
    // The wall-clock duration spans the wait: 10:00:00 → 10:01:42 = 102s.
    expect(section().dataset.turnStatus).toBe("completed");
    const title = container.querySelector(".turn-process-title");
    expect(title?.textContent).toContain("1 分 42 秒");
  });

  it("stacks the during-wait user bubble inside the same group", () => {
    const turns = [
      makeTurn("t1", [userItem("第一条"), spawnItem("a")]),
      makeTurn("t2", [userItem("等待期插一句话"), answerItem("收到。")]),
      makeTurn("t3", [wakeItem("agent-a"), answerItem("最终结果。")]),
    ];
    mountGroup(turns);
    expect(container.querySelectorAll("section.turn")).toHaveLength(1);
    expect(container.querySelectorAll(".user-message-block")).toHaveLength(2);
    expect(actionBars()).toHaveLength(1);
  });

  it("keeps the terminal affordance inside the final action bar only", async () => {
    const spawnTurn = makeTurn("t1", [
      userItem("跑一下测试"),
      commandItem(),
      spawnItem("research_a"),
      answerItem("测试已跑，等子任务。"),
    ]);
    // While the orchestration waits, the settled member's command runs must
    // not surface a standalone terminal row below the live block.
    mountGroup([spawnTurn], true, () => {});
    expect(container.querySelector(".turn-run-actions")).toBeNull();
    expect(container.querySelector(".lucide-square-terminal")).toBeNull();

    const wakeTurn = makeTurn("t2", [
      wakeItem("agent-research_a"),
      answerItem("汇总如下。"),
    ]);
    mountGroup([spawnTurn, wakeTurn], false, () => {});
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 200));
    });
    // Settled: exactly one terminal button, riding the final answer's bar.
    expect(container.querySelector(".turn-run-actions")).toBeNull();
    expect(
      container.querySelectorAll(".agent-message-actions .lucide-square-terminal"),
    ).toHaveLength(1);
  });
});

describe("TurnGroupView — awaiting between turns", () => {
  it("keeps a spawn turn live while its background agents run", () => {
    const turns = [
      makeTurn(
        "t1",
        [userItem("帮我查 X"), spawnItem("research_a"), answerItem("我先派一个子任务。")],
        {
          startedAt: "2026-07-29T10:00:00Z",
          completedAt: "2026-07-29T10:00:12Z",
          durationMs: 12_000,
        },
      ),
    ];
    mountGroup(turns, true);

    // The turn does not look finished: in_progress status, no action bar,
    // and the original spawn tool-call row carries the live shimmer while
    // the real parent turn is parked between subagent wake-ups.
    expect(section().dataset.turnStatus).toBe("in_progress");
    expect(actionBars()).toHaveLength(0);
    expect(foldToggle().getAttribute("aria-expanded")).toBe("false");
    const waiting = container.querySelector(".turn-subagent-status.is-live-gray");
    expect(waiting?.textContent).toContain("子任务 research_a");
    const shell = container.querySelector(".assistant-turn-shell");
    if (!shell || !waiting) {
      throw new Error("expected the spawn tool-call status inside the assistant shell");
    }
    expect(shell.contains(waiting)).toBe(true);
    expect(container.querySelector(".turn-process-preview-activity")).toBeNull();
  });

  it("releases to the normal single-turn path when nothing is awaiting", () => {
    const turns = [
      makeTurn("t1", [userItem("hi"), answerItem("hello")], {
        durationMs: 800,
      }),
    ];
    mountGroup(turns, false);
    expect(section().dataset.turnStatus).toBe("completed");
    expect(actionBars()).toHaveLength(1);
  });

  it("keeps a completed parent live from its pending spawn timeline state", () => {
    const turns = [
      makeTurn("t1", [
        userItem("审计文档"),
        spawnItem("audit_docs"),
        answerItem("已启动审计。"),
      ]),
    ];

    // The parent model turn has completed and the external child snapshot is
    // deliberately absent. The running spawn result itself must keep this
    // interaction open until its completion notification arrives.
    mountGroup(turns, false);

    expect(section().dataset.turnStatus).toBe("in_progress");
    expect(actionBars()).toHaveLength(0);
    expect(
      container.querySelector(".turn-subagent-status.is-live-gray")?.textContent,
    ).toContain("子任务 audit_docs");
  });

  it("keeps the waiting activity as a settled record when the orchestration is interrupted", () => {
    const turns = [
      makeTurn("t1", [
        userItem("帮我查 X"),
        spawnItem("research_a"),
        answerItem("我先派一个子任务。"),
      ]),
    ];
    mountGroup(turns, false, undefined, true);

    expect(section().dataset.turnStatus).toBe("interrupted");
    expect(actionBars()).toHaveLength(0);
    const waiting = container.querySelector(".turn-subagent-status");
    expect(waiting?.textContent).toContain("子任务 research_a");
    expect(waiting?.classList.contains("is-live-gray")).toBe(false);
    expect(container.querySelector(".turn-event-title")?.textContent).toBe("已暂停回答");
  });

  it("settles the whole group when the wake lands and completes", async () => {
    const spawnTurn = makeTurn(
      "t1",
      [userItem("帮我查 X"), spawnItem("research_a"), answerItem("我先派一个子任务。")],
      { startedAt: "2026-07-29T10:00:00Z", completedAt: "2026-07-29T10:00:12Z" },
    );
    mountGroup([spawnTurn], true);
    expect(section().dataset.turnStatus).toBe("in_progress");
    const waitingRow = container.querySelector(".turn-subagent-status");
    expect(waitingRow?.textContent).toContain("子任务 research_a");

    const wakeTurn = makeTurn(
      "t2",
      [wakeItem("agent-research_a"), answerItem("汇总如下。")],
      { startedAt: "2026-07-29T10:01:20Z", completedAt: "2026-07-29T10:01:42Z" },
    );
    mountGroup([spawnTurn, wakeTurn], false);
    expect(section().dataset.turnStatus).toBe("completed");
    // The presentation stabilizer (120ms) holds the structure change that
    // appends the wake turn's entries; let it publish before asserting.
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 200));
    });
    expect(actionBars()).toHaveLength(1);
    const completedRow = container.querySelector(".turn-subagent-status");
    expect(completedRow?.textContent).toContain(
      "完成了",
    );
    // The wait placeholder and completion share one stable timeline node, so
    // AnimatedProcessText performs its normal in-place text transition.
    expect(completedRow).toBe(waitingRow);
  });

  it("publishes a notification-only completion on the original spawn node", () => {
    const spawnTurn = makeTurn("t1", [
      userItem("帮我查 X"),
      spawnItem("research_a"),
      answerItem("我先等待。"),
    ]);
    mountGroup([spawnTurn], true);
    const spawnRow = container.querySelector(".turn-subagent-status");
    expect(spawnRow?.textContent).toBe("子任务 research_a");

    const wakeTurn = makeTurn("t2", [wakeItem("agent-research_a")]);
    mountGroup([spawnTurn, wakeTurn], false);

    const completedRow = container.querySelector(".turn-subagent-status");
    expect(completedRow).toBe(spawnRow);
    // AnimatedProcessText briefly keeps the outgoing and incoming copies in
    // the same node while the replacement slides through.
    expect(completedRow?.textContent).toContain("research_a 完成了");
  });

  it("keeps the group live without inventing a bottom status when spawn history is absent", () => {
    const turns = [
      makeTurn("t1", [userItem("审计项目"), answerItem("我先等待审计结果。")], {
        durationMs: 800,
      }),
    ];

    mountGroup(turns, true);

    expect(section().dataset.turnStatus).toBe("in_progress");
    expect(actionBars()).toHaveLength(0);
    expect(container.querySelector(".turn-subagent-status")).toBeNull();
  });

  it("updates the completed spawn in place while a peer spawn remains live", () => {
    const turns = [
      makeTurn("t1", [
        userItem("审计项目"),
        spawnItem("first"),
        spawnItem("second"),
        answerItem("等待结果。"),
      ]),
      makeTurn(
        "t2",
        [wakeItem("agent-first"), answerItem("第一个结果收到，继续等另一个。")],
        { status: "in_progress" },
      ),
    ];

    mountGroup(turns, true);

    const statuses = Array.from(
      container.querySelectorAll<HTMLElement>(".turn-subagent-status"),
    );
    expect(statuses.map((status) => status.textContent)).toEqual([
      "first 完成了",
      "子任务 second",
    ]);
    const completedStatus = statuses.find((status) =>
      status.textContent?.includes("完成了"),
    );
    const liveWait = statuses.find((status) => status.textContent?.includes("子任务 second"));
    const wakeAnswer = Array.from(container.querySelectorAll(".agent-text")).find(
      (node) => node.textContent?.includes("第一个结果收到"),
    );
    expect(liveWait?.textContent).toContain("子任务 second");
    expect(liveWait?.classList.contains("is-live-gray")).toBe(true);
    expect(
      container.querySelector(
        ".process-surface-row.is-live-gray:not(.turn-subagent-status)",
      ),
    ).toBeNull();
    if (!completedStatus || !wakeAnswer || !liveWait) {
      throw new Error("expected paired spawn statuses before the wake-up answer");
    }
    expect(
      completedStatus.compareDocumentPosition(wakeAnswer) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(
      liveWait.compareDocumentPosition(wakeAnswer) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(actionBars()).toHaveLength(0);
  });
});
