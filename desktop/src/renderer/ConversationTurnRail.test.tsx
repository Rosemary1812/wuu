import { act, type RefObject } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it } from "vitest";
import type { ThreadItem, Turn } from "../shared/protocol";
import {
  conversationTurnRailWindow,
  ConversationTurnRail,
} from "./ConversationTurnRail";
import type { QueryHistoryEntry } from "./QueryHistoryPopover";

let container: HTMLDivElement | undefined;
let root: Root | undefined;

function userMessage(id: string, text: string): ThreadItem {
  return {
    id,
    type: "user_message",
    status: "completed",
    text,
  };
}

function agentMessage(id: string): ThreadItem {
  return {
    id,
    type: "agent_message",
    status: "completed",
    phase: "final_answer",
    text: "reply",
  };
}

function turn(id: string, query: string): Turn {
  return {
    id,
    items_view: "full",
    status: "completed",
    items: [userMessage(`user-${id}`, query), agentMessage(`agent-${id}`)],
  };
}

function renderRail({
  turns,
  activeTurnID,
  scrollContainerRef,
  onSelectQueryHistory,
}: {
  turns?: Turn[];
  activeTurnID?: string;
  scrollContainerRef?: RefObject<HTMLDivElement | null>;
  onSelectQueryHistory: (entry: QueryHistoryEntry) => void;
}): void {
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  act(() => {
    root?.render(
      <ConversationTurnRail
        turns={turns ?? [turn("turn-1", "first query"), turn("turn-2", "second query")]}
        activeTurnID={activeTurnID}
        scrollContainerRef={scrollContainerRef}
        onSelectQueryHistory={onSelectQueryHistory}
      />,
    );
  });
}

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
  root = undefined;
  container = undefined;
});

describe("ConversationTurnRail", () => {
  it("routes bar clicks through query-history selection", () => {
    let selected: QueryHistoryEntry | undefined;
    renderRail({
      onSelectQueryHistory: (entry) => {
        selected = entry;
      },
    });

    const bars = container?.querySelectorAll<HTMLElement>(
      ".conversation-turn-rail-bar[role='button']",
    );
    expect(bars).toHaveLength(2);

    act(() => {
      bars?.[1]?.click();
    });

    expect(selected).toEqual({
      turnID: "turn-2",
      itemID: "user-turn-2",
      text: "second query",
    });
  });

  it("stays hidden for an empty conversation", () => {
    renderRail({
      turns: [],
      onSelectQueryHistory: () => {},
    });

    expect(container?.querySelector(".conversation-turn-rail")).toBeNull();
    expect(container?.querySelector(".conversation-turn-rail-bar")).toBeNull();
  });

  it("caps many turns to the latest visible window by default", () => {
    renderRail({
      turns: Array.from({ length: 40 }, (_, index) =>
        turn(`turn-${index + 1}`, `query ${index + 1}`),
      ),
      onSelectQueryHistory: () => {},
    });

    const bars = container?.querySelectorAll<HTMLElement>(
      ".conversation-turn-rail-bar[role='button']",
    );
    expect(bars).toHaveLength(36);
    expect(bars?.[0]?.getAttribute("aria-label")).toBe(
      "跳转到第 5 轮对话",
    );
    expect(bars?.[35]?.getAttribute("aria-label")).toBe(
      "跳转到第 40 轮对话",
    );
  });

  it("scrolls the conversation when the user wheels over the rail", () => {
    const scrollNode = document.createElement("div");
    scrollNode.scrollTop = 40;
    renderRail({
      scrollContainerRef: { current: scrollNode },
      onSelectQueryHistory: () => {},
    });

    const rail = container?.querySelector<HTMLElement>(".conversation-turn-rail");
    const event = new WheelEvent("wheel", {
      bubbles: true,
      cancelable: true,
      deltaY: 80,
    });
    act(() => {
      rail?.dispatchEvent(event);
    });

    expect(scrollNode.scrollTop).toBe(120);
    expect(event.defaultPrevented).toBe(true);
  });

  it("centers the capped window around the focused turn", () => {
    const turns = Array.from({ length: 20 }, (_, index) =>
      turn(`turn-${index + 1}`, `query ${index + 1}`),
    );

    expect(
      conversationTurnRailWindow(turns, "turn-2", 5).turns.map(
        (entry) => entry.id,
      ),
    ).toEqual(["turn-1", "turn-2", "turn-3", "turn-4", "turn-5"]);
    expect(
      conversationTurnRailWindow(turns, "turn-10", 5).turns.map(
        (entry) => entry.id,
      ),
    ).toEqual(["turn-8", "turn-9", "turn-10", "turn-11", "turn-12"]);
  });
});
