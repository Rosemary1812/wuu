import { useEffect, useMemo, useState } from "react";
import type {
  MessageMarkNotification,
  MessageMarkWire,
  ServerEvent,
} from "../shared/protocol";
import { aggregateMarksBySeq, type MessageMarksView } from "./MessageMarks";

// Replace the same (message, participant, kind) mark in place; a seen row
// advancing in_progress → completed, or a swapped reaction, is one evolving
// entry, never a duplicate — mirroring the store's primary key.
export function upsertMark(
  list: readonly MessageMarkWire[],
  mark: MessageMarkWire,
): MessageMarkWire[] {
  const next = list.filter(
    (x) =>
      !(
        x.seq === mark.seq &&
        x.participant_id === mark.participant_id &&
        x.kind === mark.kind
      ),
  );
  next.push(mark);
  return next;
}

// useThreadMarks loads a thread's read receipts + reactions and keeps them
// live: it fetches thread/marks when the thread (re)activates, then applies
// message/mark notifications incrementally. It no-ops when disabled or when the
// Electron bridge is absent (unit tests), so it is safe to mount unconditionally.
export function useThreadMarks(
  threadID: string | undefined,
  enabled: boolean,
): ReadonlyMap<number, MessageMarksView> {
  const [marks, setMarks] = useState<readonly MessageMarkWire[]>([]);

  useEffect(() => {
    const api = typeof window !== "undefined" ? window.wuu : undefined;
    if (!enabled || !threadID || !api?.getThreadMarks || !api.onServerEvent) {
      setMarks([]);
      return;
    }
    let cancelled = false;
    setMarks([]);
    void api
      .getThreadMarks(threadID)
      .then((res) => {
        if (!cancelled) {
          setMarks(res?.marks ?? []);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setMarks([]);
        }
      });
    const off = api.onServerEvent((event: ServerEvent) => {
      if (
        event.kind !== "notification" ||
        event.message.method !== "message/mark"
      ) {
        return;
      }
      const params = event.message.params as MessageMarkNotification | undefined;
      if (!params || params.thread_id !== threadID) {
        return;
      }
      setMarks((prev) => upsertMark(prev, params));
    });
    return () => {
      cancelled = true;
      off?.();
    };
  }, [threadID, enabled]);

  return useMemo(() => aggregateMarksBySeq(marks), [marks]);
}
