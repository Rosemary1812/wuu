import type { JSX } from "react";
import type { ReactionAggregate } from "./MessageMarks";
import { reactionLabel } from "./MessageMarks";
import { reactionArt } from "./MessageReactionArt";

// Reaction chips stamped on a message bubble. Same reaction from multiple
// members aggregates into one chip with a count; hover names who. The glyph is
// the mascot sticker for the reaction key (MessageReactionArt), falling back
// to the emoji placeholder for keys without art — the model only ever sends
// the reaction key. 2026-07-04-read-receipts-and-reactions.md §6.
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
        const label = reactionLabel(r.key);
        const art = reactionArt(r.key);
        return (
          <span
            key={r.key}
            className="chat-reaction-chip"
            title={`${label} · ${who}`}
            data-reaction={r.key}
          >
            {art ? (
              <img
                className="chat-reaction-glyph"
                src={art}
                alt={label}
                draggable={false}
              />
            ) : (
              <span className="chat-reaction-glyph">{r.glyph}</span>
            )}
            {r.participantIds.length > 1 ? (
              <span className="chat-reaction-count">{r.participantIds.length}</span>
            ) : null}
          </span>
        );
      })}
    </div>
  );
}
