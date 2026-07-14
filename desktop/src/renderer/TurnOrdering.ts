import type { ThreadItem, Turn } from "../shared/protocol";

export function orderedTurnItems(items: ThreadItem[]): ThreadItem[] {
  const indexed = items.map((item, index) => ({
    item,
    index,
    order: turnItemOrder(item.id),
  }));
  if (indexed.some((entry) => entry.order === undefined)) {
    return items.slice();
  }
  return indexed
    .sort(
      (left, right) =>
        (left.order ?? left.index) - (right.order ?? right.index) ||
        left.index - right.index,
    )
    .map((entry) => entry.item);
}

export function mergeTurnItemsInOrder(
  previous: Turn,
  next: Turn,
): ThreadItem[] {
  const nextByID = new Map(next.items.map((item) => [item.id, item]));
  const used = new Set<string>();
  const merged: ThreadItem[] = [];
  for (const item of previous.items) {
    const nextItem = nextByID.get(item.id);
    if (nextItem) {
      merged.push(nextItem);
      used.add(nextItem.id);
      continue;
    }
    if (item.type !== "user_message") {
      merged.push(item);
      used.add(item.id);
    }
  }
  for (const item of next.items) {
    if (!used.has(item.id)) {
      merged.push(item);
    }
  }
  return orderedTurnItems(merged);
}

export function upsertTurnItemInOrder(
  turn: Turn,
  item: ThreadItem,
): ThreadItem[] {
  const index = turn.items.findIndex((existing) => existing.id === item.id);
  if (index < 0) {
    return orderedTurnItems([...turn.items, item]);
  }
  const items = turn.items.slice();
  items[index] = item;
  return orderedTurnItems(items);
}

function turnItemOrder(id: string): number | undefined {
  const match = id.match(/-item-(\d+)$/);
  if (!match) {
    return undefined;
  }
  const order = Number.parseInt(match[1], 10);
  return Number.isFinite(order) ? order : undefined;
}
