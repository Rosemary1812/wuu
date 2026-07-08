/**
 * Tests for `PreviewTurnGroup` — the row renderer used by
 * `ConversationSearchOverlay` on the right pane of cmd+P search.
 *
 * Each `Turn` in this codebase bundles the user's opening message with
 * every `agent_message` / `reasoning` / `tool_call` that followed it
 * (see `turnsFromPersistedHistoryInScope` in `internal/appserver/model.go`).
 * The preview used to render the whole turn as one row with a single role,
 * which hid either the user query or the assistant's reply depending on
 * who spoke last. The renderer now emits up to two rows per turn (one
 * per non-empty `user_message` / `agent_message`) so the visible timeline
 * matches the actual conversation. Role distinction lives in CSS — the
 * user row is right-aligned with a chat-bubble background and the
 * assistant row reads flush left — and `nowrap` + `ellipsis` keeps every
 * row exactly one line tall.
 *
 * Real React via `react-dom/client` + `act`, no `@testing-library/react`
 * dependency — matches the TurnSourcesRow / AssistantTurnShell pattern.
 */
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it } from "vitest";
import type { ThreadItem, Turn } from "../shared/protocol";
import { PreviewTurnGroup } from "./ConversationSearchOverlay";

let mountedRoots: Root[] = [];

afterEach(() => {
  for (const root of mountedRoots) {
    act(() => root.unmount());
  }
  mountedRoots = [];
});

function userItem(text: string): ThreadItem {
  return { id: `u-${text}`, type: "user_message", text };
}

function agentItem(
  text: string,
  phase?: ThreadItem["phase"],
): ThreadItem {
  return { id: `a-${text}`, type: "agent_message", text, phase };
}

function toolItem(): ThreadItem {
  return { id: "t-1", type: "tool_call" };
}

function turnWith(items: ThreadItem[], id = "turn-1"): Turn {
  return {
    id,
    items,
    items_view: "full",
    status: "completed",
  };
}

function renderTurn(turn: Turn, query = ""): HTMLDivElement {
  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => {
    root.render(createElement(PreviewTurnGroup, { turn, query }));
  });
  mountedRoots.push(root);
  return container;
}

function rows(container: HTMLDivElement): HTMLElement[] {
  return Array.from(
    container.querySelectorAll<HTMLElement>(
      ".conversation-search-preview-turn",
    ),
  );
}

describe("PreviewTurnGroup", () => {
  it("renders only the user row when the agent has not replied yet", () => {
    const container = renderTurn(turnWith([userItem("帮我看看这个 bug")]));
    const rendered = rows(container);
    expect(rendered).toHaveLength(1);
    expect(rendered[0].dataset.role).toBe("user");
    expect(rendered[0].textContent).toContain("帮我看看这个 bug");
  });

  it("renders only the assistant row when the turn has no user_message", () => {
    const container = renderTurn(
      turnWith([agentItem("好的，让我看看。", "final_answer")]),
    );
    const rendered = rows(container);
    expect(rendered).toHaveLength(1);
    expect(rendered[0].dataset.role).toBe("assistant");
  });

  it("renders the user row before the assistant row when the turn has both", () => {
    const container = renderTurn(
      turnWith([
        userItem("Q1 — 为什么 cmd+P 右边看不见我的 query？"),
        agentItem("A1 — 因为之前每条 turn 合并成一行", "final_answer"),
      ]),
    );
    const rendered = rows(container);
    expect(rendered.map((el) => el.dataset.role)).toEqual([
      "user",
      "assistant",
    ]);
    expect(rendered[0].textContent).toContain("Q1");
    expect(rendered[1].textContent).toContain("A1");
  });

  it("prefers the final_answer agent_message for the assistant row", () => {
    const container = renderTurn(
      turnWith([
        userItem("Q2"),
        agentItem("commentary 先说两句", "commentary"),
        agentItem("final 答案", "final_answer"),
      ]),
    );
    const rendered = rows(container);
    const assistant = rendered.find((el) => el.dataset.role === "assistant");
    expect(assistant?.textContent).toContain("final 答案");
    expect(assistant?.textContent).not.toContain("commentary 先说两句");
  });

  it("falls back to the last agent_message when no final_answer is tagged", () => {
    const container = renderTurn(
      turnWith([
        userItem("Q3"),
        agentItem("先说一句"),
        agentItem("最后一句"),
      ]),
    );
    const rendered = rows(container);
    const assistant = rendered.find((el) => el.dataset.role === "assistant");
    expect(assistant?.textContent).toContain("最后一句");
  });

  it("renders nothing for turns that only carry tool_calls", () => {
    const container = renderTurn(turnWith([toolItem()]));
    expect(rows(container)).toHaveLength(0);
  });
});