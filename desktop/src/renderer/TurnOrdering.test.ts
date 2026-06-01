import { describe, expect, it } from "vitest";
import type { ThreadItem, Turn } from "../shared/protocol";
import {
  mergeTurnItemsInOrder,
  orderedTurnItems,
  upsertTurnItemInOrder,
} from "./TurnOrdering";

function item(id: string, type: ThreadItem["type"] = "tool_call"): ThreadItem {
  return {
    id,
    type,
    status: "in_progress",
  };
}

function turn(items: ThreadItem[]): Turn {
  return {
    id: "thread-turn-0001",
    items,
    items_view: "full",
    status: "in_progress",
  };
}

describe("turn item ordering", () => {
  it("keeps app-server item ids in creation order", () => {
    expect(
      orderedTurnItems([
        item("thread-turn-0001-item-3"),
        item("thread-turn-0001-item-1"),
        item("thread-turn-0001-item-2"),
      ]).map((entry) => entry.id),
    ).toEqual([
      "thread-turn-0001-item-1",
      "thread-turn-0001-item-2",
      "thread-turn-0001-item-3",
    ]);
  });

  it("orders items when batched item events arrive out of sequence", () => {
    let current = turn([item("thread-turn-0001-item-1", "user_message")]);
    current = {
      ...current,
      items: upsertTurnItemInOrder(current, item("thread-turn-0001-item-4")),
    };
    current = {
      ...current,
      items: upsertTurnItemInOrder(
        current,
        item("thread-turn-0001-item-2", "agent_message"),
      ),
    };
    current = {
      ...current,
      items: upsertTurnItemInOrder(current, item("thread-turn-0001-item-3")),
    };

    expect(current.items.map((entry) => entry.id)).toEqual([
      "thread-turn-0001-item-1",
      "thread-turn-0001-item-2",
      "thread-turn-0001-item-3",
      "thread-turn-0001-item-4",
    ]);
  });

  it("uses completed turn snapshots to correct prior render order", () => {
    const previous = turn([
      item("thread-turn-0001-item-1", "user_message"),
      item("thread-turn-0001-item-4"),
      item("thread-turn-0001-item-2", "agent_message"),
      item("thread-turn-0001-item-3"),
    ]);
    const next = turn([
      item("thread-turn-0001-item-1", "user_message"),
      item("thread-turn-0001-item-2", "agent_message"),
      item("thread-turn-0001-item-3"),
      item("thread-turn-0001-item-4"),
    ]);

    expect(mergeTurnItemsInOrder(previous, next).map((entry) => entry.id)).toEqual(
      [
        "thread-turn-0001-item-1",
        "thread-turn-0001-item-2",
        "thread-turn-0001-item-3",
        "thread-turn-0001-item-4",
      ],
    );
  });

  it("preserves order for nonstandard ids", () => {
    expect(
      orderedTurnItems([
        item("custom-b"),
        item("thread-turn-0001-item-1"),
        item("custom-a"),
      ]).map((entry) => entry.id),
    ).toEqual(["custom-b", "thread-turn-0001-item-1", "custom-a"]);
  });
});
