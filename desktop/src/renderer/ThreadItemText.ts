import type { ThreadItem } from "../shared/protocol";
import {
  streamTextKey,
  streamTextStore,
  type StreamTextField,
} from "./StreamText";

export function streamFieldValue(
  turnID: string,
  item: ThreadItem,
  field: StreamTextField,
): string {
  const key = streamTextKey(turnID, item.id, field);
  return streamTextStore.has(key)
    ? streamTextStore.get(key)
    : (item[field] ?? "");
}
