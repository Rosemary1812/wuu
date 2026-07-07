// Read receipts + message reactions, renderer side.
// Mirrors the backend message_marks wire (MessageMarkWire in Go): every mark
// is one (message seq, participant, kind). "seen" carries a lifecycle status;
// "reaction" carries a key. See 2026-07-04-read-receipts-and-reactions.md.

export type SeenStatus = "in_progress" | "completed" | "failed";

export interface MessageMark {
  seq: number;
  participant_id: string;
  kind: "seen" | "reaction";
  status?: SeenStatus;
  reaction?: string;
  at_ms?: number;
}

// Placeholder emoji for each reaction key. The model only ever sends the key;
// this map is the swappable skin — replace with custom art later without
// touching the backend or the key vocabulary (design §6). The user finds bare
// emoji cheap, so these are a stand-in until real assets land.
export const REACTION_EMOJI: Record<string, string> = {
  eyes: "👀",
  nice: "👍",
  shrug: "🤷",
  sideeye: "🙄",
  smug: "😏",
  whoa: "😲",
};

export function reactionGlyph(key: string): string {
  return REACTION_EMOJI[key] ?? "•";
}

// Human-readable labels (a11y name + tooltip) for the hover toolbar's one-click
// reaction buttons. Keys mirror the backend vocabulary; the glyph (REACTION_EMOJI)
// is the swappable skin.
export const REACTION_LABEL: Record<string, string> = {
  eyes: "看到了",
  nice: "赞同",
  shrug: "无所谓",
  sideeye: "存疑",
  smug: "得意",
  whoa: "惊讶",
};

// The reaction keys a human may stamp from the chat UI, in display order. Kept
// in lockstep with the backend vocabulary (reactionKeys in tool_participant_react.go)
// so the picker never offers a key the server would reject.
export const REACTION_KEYS = [
  "eyes",
  "nice",
  "shrug",
  "sideeye",
  "smug",
  "whoa",
] as const;

// Per-message aggregation of who saw it (by lifecycle status) and who reacted.
export interface SeenAggregate {
  completed: string[]; // participant ids whose turn finished — the real "seen"
  inProgress: string[]; // currently processing
  failed: string[]; // started then errored/interrupted
}

export interface ReactionAggregate {
  key: string;
  glyph: string;
  participantIds: string[];
}

export interface MessageMarksView {
  seen: SeenAggregate;
  reactions: ReactionAggregate[];
}

function emptySeen(): SeenAggregate {
  return { completed: [], inProgress: [], failed: [] };
}

// aggregateMarksBySeq folds a flat mark list into per-seq views. A participant
// appears in exactly one seen bucket (its latest status wins, since the store
// keeps one seen row per participant). Reactions preserve first-seen key order
// for stable rendering.
export function aggregateMarksBySeq(
  marks: readonly MessageMark[],
): Map<number, MessageMarksView> {
  const bySeq = new Map<number, MessageMarksView>();
  const reactionIndex = new Map<number, Map<string, ReactionAggregate>>();

  const viewFor = (seq: number): MessageMarksView => {
    let v = bySeq.get(seq);
    if (!v) {
      v = { seen: emptySeen(), reactions: [] };
      bySeq.set(seq, v);
      reactionIndex.set(seq, new Map());
    }
    return v;
  };

  for (const mark of marks) {
    if (!mark || typeof mark.seq !== "number" || mark.seq <= 0) {
      continue;
    }
    const view = viewFor(mark.seq);
    if (mark.kind === "seen") {
      const pid = mark.participant_id;
      // A participant sits in one bucket; drop any prior placement first so a
      // later status (in_progress → completed → failed) replaces it.
      view.seen.completed = view.seen.completed.filter((p) => p !== pid);
      view.seen.inProgress = view.seen.inProgress.filter((p) => p !== pid);
      view.seen.failed = view.seen.failed.filter((p) => p !== pid);
      if (mark.status === "completed") view.seen.completed.push(pid);
      else if (mark.status === "failed") view.seen.failed.push(pid);
      else if (mark.status === "in_progress") view.seen.inProgress.push(pid);
    } else if (mark.kind === "reaction" && mark.reaction) {
      const idx = reactionIndex.get(mark.seq)!;
      let agg = idx.get(mark.reaction);
      if (!agg) {
        agg = {
          key: mark.reaction,
          glyph: reactionGlyph(mark.reaction),
          participantIds: [],
        };
        idx.set(mark.reaction, agg);
        view.reactions.push(agg);
      }
      if (!agg.participantIds.includes(mark.participant_id)) {
        agg.participantIds.push(mark.participant_id);
      }
    }
  }
  return bySeq;
}

// Progress-ring model for one message. `total` is the number of members the
// message was broadcast to (its readership). The ring fills to
// completed/total; `failed` renders as a distinct segment so a ring that never
// completes reads as "someone crashed", not "still working".
export interface RingModel {
  total: number;
  completed: number;
  inProgress: number;
  failed: number;
  fraction: number; // completed / total, clamped 0..1
  allSeen: boolean;
  anyFailed: boolean;
}

export function ringModel(seen: SeenAggregate, total: number): RingModel {
  const completed = seen.completed.length;
  const failed = seen.failed.length;
  const inProgress = seen.inProgress.length;
  const denom = total > 0 ? total : completed + failed + inProgress;
  const fraction = denom > 0 ? Math.min(1, completed / denom) : 0;
  return {
    total: denom,
    completed,
    inProgress,
    failed,
    fraction,
    allSeen: denom > 0 && completed >= denom,
    anyFailed: failed > 0,
  };
}
