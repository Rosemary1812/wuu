import type { JSX } from "react";
import type { ReactionAggregate } from "./MessageMarks";

// Reaction chips stamped on a message bubble. Same reaction from multiple
// members aggregates into one chip with a count; hover names who. The glyph is
// an emoji placeholder today (swappable for custom art later — the model only
// ever sends the reaction key). 2026-07-04-read-receipts-and-reactions.md §6.
export function MessageReactions({
  reactions,
  resolveName,
}: {
  reactions: readonly ReactionAggregate[];
  resolveName?: (id: string) => string;
}): JSX.Element | null {
  if (!reactions || reactions.length === 0) {
    return null;
  }
  return (
    <div className="chat-reactions">
      {reactions.map((r) => {
        const who = r.participantIds.map((id) => resolveName?.(id) ?? id).join("、");
        return (
          <span
            key={r.key}
            className="chat-reaction-chip"
            title={who}
            data-reaction={r.key}
          >
            <span className="chat-reaction-glyph">{r.glyph}</span>
            {r.participantIds.length > 1 ? (
              <span className="chat-reaction-count">{r.participantIds.length}</span>
            ) : null}
          </span>
        );
      })}
    </div>
  );
}
