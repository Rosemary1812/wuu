// Conversation-list logic, mirrored from desktop AppState semantics:
// which threads the mobile app shows (DM + group only — the product
// decision: no project sessions on the phone), how they sort, and the
// purely client-local unread state.

import type { Thread } from "@wuu/protocol";

/** Strict DM predicate (mirrors findDMThread): legacy DM threads that were
 *  mis-homed into a project workspace are excluded. */
export function isDMThread(t: Thread): boolean {
  return Boolean(t.dm_participant_id) && t.workspace_kind === "dm";
}

export function isGroupThread(t: Thread): boolean {
  return t.group === true;
}

export function isChatThread(t: Thread): boolean {
  return (isDMThread(t) || isGroupThread(t)) && !t.archived && !t.read_only;
}

export function isThreadRunning(t: Thread): boolean {
  if (t.status === "in_progress") return true;
  return (t.turns ?? []).some((turn) => turn.status === "in_progress");
}

export function threadDisplayTitle(t: Thread): string {
  return t.title?.trim() || t.preview?.trim() || "未命名对话";
}

/** Two-band ordering (mirrors AppState.sortThreads): running threads first
 *  by created_at desc (stable while streaming), finished by updated_at desc. */
export function sortChatThreads(threads: Thread[]): Thread[] {
  const running = threads.filter(isThreadRunning);
  const finished = threads.filter((t) => !isThreadRunning(t));
  const stamp = (value?: string): number => {
    const ms = value ? Date.parse(value) : NaN;
    return Number.isNaN(ms) ? 0 : ms;
  };
  running.sort((a, b) => stamp(b.created_at) - stamp(a.created_at));
  finished.sort(
    (a, b) => stamp(b.updated_at ?? b.created_at) - stamp(a.updated_at ?? a.created_at),
  );
  return [...running, ...finished];
}

/** The turn id unread detection keys on: the newest COMPLETED turn (a turn
 *  that is still streaming never marks a thread unread). */
export function latestCompletedTurnID(t: Thread): string | null {
  const turns = t.turns ?? [];
  for (let i = turns.length - 1; i >= 0; i--) {
    if (turns[i].status !== "in_progress") return turns[i].id;
  }
  return null;
}

/** Unread is purely client-local: the newest completed turn differs from
 *  what the user last viewed, and the thread is not mid-run. */
export function isThreadUnread(t: Thread, lastViewed: Readonly<Record<string, string>>): boolean {
  if (isThreadRunning(t)) return false;
  const latest = latestCompletedTurnID(t);
  if (!latest) return false;
  return lastViewed[t.id] !== latest;
}
