import type { CodexPetHint, Thread } from "../shared/protocol";

// Heuristic patterns: thread preview is the closest signal we have to the
// latest commentary/finaltext without walking every turn's items. v2 can
// descend into Thread.turns[].items for the literal latest text; for a
// light hint this approximation is enough.
const FAILED_RE = /\b(failed|error)\b|失败|错误/i;
const REVIEW_RE = /权限|批准|确认|permission|approve|review/i;

export type ActiveSessionHintInput = {
  thread?: Thread | null;
  secondaryThread?: Thread | null;
  threads?: readonly Thread[] | null;
};

export function deriveActiveSessionHint(
  input: ActiveSessionHintInput,
): CodexPetHint | null {
  const all: Thread[] = [];
  if (input.thread) all.push(input.thread);
  if (input.secondaryThread) all.push(input.secondaryThread);
  for (const thread of input.threads ?? []) all.push(thread);

  const byID = new Map<string, Thread>();
  for (const thread of all) {
    if (thread && thread.id && !byID.has(thread.id)) byID.set(thread.id, thread);
  }

  const candidates = Array.from(byID.values()).filter((thread) => !thread.archived);
  if (candidates.length === 0) return null;

  const scored = candidates.map((thread) => {
    const preview = (thread.preview ?? "").trim();
    const title = (thread.title ?? "").trim();
    const failed = FAILED_RE.test(preview) || FAILED_RE.test(title);
    const needsReview = !failed && REVIEW_RE.test(preview);
    const running = thread.status === "in_progress";

    let priority: number;
    let hintStatus: CodexPetHint["status"];
    let attention = false;
    if (failed) {
      priority = 4;
      hintStatus = "failed";
      attention = true;
    } else if (needsReview) {
      priority = 3;
      hintStatus = "needs_review";
      attention = true;
    } else if (running) {
      priority = 2;
      hintStatus = "running";
      attention = false;
    } else {
      priority = 1;
      hintStatus = "idle";
      attention = false;
    }

    // The currently focused thread wins ties on equal priority.
    if (input.thread && thread.id === input.thread.id) priority += 0.5;

    const updatedAt = thread.updated_at ? Date.parse(thread.updated_at) || 0 : 0;
    return {
      thread,
      priority,
      hintStatus,
      attention,
      title: title || "对话",
      preview,
      updatedAt,
    };
  });

  scored.sort((left, right) => {
    if (right.priority !== left.priority) return right.priority - left.priority;
    return right.updatedAt - left.updatedAt;
  });

  const top = scored[0];
  return {
    thread_id: top.thread.id,
    title: top.title,
    status: top.hintStatus,
    preview: top.preview,
    attention: top.attention,
    updated_at: top.updatedAt || Date.now(),
  };
}