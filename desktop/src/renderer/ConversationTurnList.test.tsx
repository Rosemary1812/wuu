import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { Turn } from "../shared/protocol";
import {
  ConversationTurnList,
  TURN_LIST_COLLAPSE_THRESHOLD,
  TURN_LIST_RECENT_FULL_TURNS,
} from "./ConversationTurnList";
import { userMessageAnchorID } from "./TurnViewHelpers";

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

function makeTurn(index: number, status: Turn["status"] = "completed"): Turn {
  return {
    id: `turn-${index}`,
    items_view: "full",
    status,
    items: [
      {
        id: `user-${index}`,
        type: "user_message",
        text: `User question ${index}`,
      },
      {
        id: `agent-${index}`,
        type: "agent_message",
        status: status === "in_progress" ? "in_progress" : "completed",
        phase: "final_answer",
        text: `Agent answer ${index}`,
      },
    ],
  };
}

function mountTurns(turns: Turn[], forcedFullTurnIDs?: string[]): void {
  render(
    <ConversationTurnList
      threadID="thread-1"
      turns={turns}
      forcedFullTurnIDs={forcedFullTurnIDs}
      renderTurn={(turn) => (
        <section
          className="turn"
          data-testid="full-turn"
          data-turn-id={turn.id}
        >
          {turn.id}
        </section>
      )}
    />,
  );
}

function pendingSpawnTurn(): Turn {
  const turn = makeTurn(1);
  return {
    ...turn,
    items: [
      ...turn.items.slice(0, 1),
      {
        id: "spawn-1",
        type: "collab_agent_tool_call",
        name: "spawn_agent",
        status: "completed",
        result: JSON.stringify({
          agent_id: "worker-1",
          task_name: "sleep_two_minutes",
          status: "running",
        }),
      },
      ...turn.items.slice(1),
    ],
  };
}

describe("ConversationTurnList", () => {
  it("routes a single pending spawn turn through the orchestration renderer", () => {
    const turn = pendingSpawnTurn();
    render(
      <ConversationTurnList
        threadID="thread-1"
        turns={[turn]}
        renderTurn={() => <div data-testid="ordinary-turn" />}
        renderTurnGroup={(turns) => (
          <div data-testid="orchestration-turn">{turns[0]?.id}</div>
        )}
      />,
    );

    expect(container.querySelector('[data-testid="orchestration-turn"]')).not.toBeNull();
    expect(container.querySelector('[data-testid="ordinary-turn"]')).toBeNull();
  });

  it("keeps an ordinary single turn on the ordinary renderer", () => {
    const turn = makeTurn(1);
    render(
      <ConversationTurnList
        threadID="thread-1"
        turns={[turn]}
        renderTurn={() => <div data-testid="ordinary-turn" />}
        renderTurnGroup={() => <div data-testid="orchestration-turn" />}
      />,
    );

    expect(container.querySelector('[data-testid="ordinary-turn"]')).not.toBeNull();
    expect(container.querySelector('[data-testid="orchestration-turn"]')).toBeNull();
  });

  it("renders all turns fully below the collapse threshold", () => {
    const turns = Array.from(
      { length: TURN_LIST_COLLAPSE_THRESHOLD },
      (_, index) => makeTurn(index),
    );

    mountTurns(turns);

    expect(container.querySelectorAll('[data-testid="full-turn"]')).toHaveLength(
      turns.length,
    );
    expect(container.querySelector(".turn-collapsed")).toBeNull();
  });

  it("keeps old turn anchors while full-rendering only recent turns", () => {
    const turns = Array.from(
      { length: TURN_LIST_COLLAPSE_THRESHOLD + 10 },
      (_, index) => makeTurn(index),
    );

    mountTurns(turns);

    expect(container.querySelectorAll('[data-testid="full-turn"]')).toHaveLength(
      TURN_LIST_RECENT_FULL_TURNS,
    );
    expect(container.querySelectorAll(".turn-collapsed").length).toBe(
      turns.length - TURN_LIST_RECENT_FULL_TURNS,
    );
    expect(
      container.querySelector(
        `#${userMessageAnchorID("turn-0", "user-0")}`,
      ),
    ).not.toBeNull();
  });

  it("expands an old collapsed turn on demand", () => {
    const turns = Array.from(
      { length: TURN_LIST_COLLAPSE_THRESHOLD + 10 },
      (_, index) => makeTurn(index),
    );
    mountTurns(turns);

    const firstCollapsed = container.querySelector<HTMLButtonElement>(
      ".turn-collapsed-button",
    );
    expect(firstCollapsed).not.toBeNull();

    act(() => {
      firstCollapsed?.click();
    });

    expect(
      container.querySelector('[data-testid="full-turn"][data-turn-id="turn-0"]'),
    ).not.toBeNull();
  });

  it("always full-renders in-progress and forced turns", () => {
    const turns = Array.from(
      { length: TURN_LIST_COLLAPSE_THRESHOLD + 10 },
      (_, index) =>
        index === 2 ? makeTurn(index, "in_progress") : makeTurn(index),
    );

    mountTurns(turns, ["turn-3"]);

    expect(
      container.querySelector('[data-testid="full-turn"][data-turn-id="turn-2"]'),
    ).not.toBeNull();
    expect(
      container.querySelector('[data-testid="full-turn"][data-turn-id="turn-3"]'),
    ).not.toBeNull();
  });
});
