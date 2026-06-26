import { useState } from "react";
import type { Turn } from "../shared/protocol";
import { truncateReplyPreview, turnReplySnippet } from "./TurnViewHelpers";

// Rail geometry. The macOS Dock magnification model drives the numbers:
// a default bar is short, the hovered bar grows by ~3x, and the two
// neighbours grow by ~1.5x. The spring easing is applied via CSS
// (cubic-bezier with slight overshoot) so React stays out of the
// animation loop.
const RAIL_BAR_DEFAULT_WIDTH = 14;
const RAIL_BAR_ADJACENT_WIDTH = 22;
const RAIL_BAR_HOVERED_WIDTH = 40;

/**
 * Vertical rail of horizontal bars on the left edge of the message stream.
 *
 * One bar per turn. Hovering a bar magnifies it (and its two neighbours
 * slightly, in classic Dock fashion) and reveals a preview card to the
 * right with that turn's first agent reply.
 *
 * Visibility/animation is driven entirely by the `hovered` and `adjacent`
 * CSS classes plus CSS transitions; the component itself just tracks which
 * turn the cursor is over.
 */
export function ConversationTurnRail({
  turns,
  activeTurnID,
}: {
  turns: Turn[];
  activeTurnID?: string;
}): JSX.Element | null {
  const [hoveredTurnID, setHoveredTurnID] = useState<string | undefined>();

  if (turns.length === 0) {
    return null;
  }

  const hoveredIndex = turns.findIndex((t) => t.id === hoveredTurnID);
  const adjacentIndices = new Set<number>();
  if (hoveredIndex >= 0) {
    if (hoveredIndex > 0) {
      adjacentIndices.add(hoveredIndex - 1);
    }
    if (hoveredIndex < turns.length - 1) {
      adjacentIndices.add(hoveredIndex + 1);
    }
  }

  return (
    <div className="conversation-turn-rail" aria-label="对话回合导航">
      {turns.map((turn, index) => {
        const isHovered = turn.id === hoveredTurnID;
        const isAdjacent = adjacentIndices.has(index);
        const isActive = turn.id === activeTurnID;
        const className = [
          "conversation-turn-rail-bar",
          isActive && "active",
          isHovered && "hovered",
          isAdjacent && "adjacent",
        ]
          .filter(Boolean)
          .join(" ");
        return (
          <div
            key={turn.id}
            className={className}
            onMouseEnter={() => setHoveredTurnID(turn.id)}
            onMouseLeave={() => setHoveredTurnID(undefined)}
          >
            {isHovered ? <TurnHoverPreview turn={turn} /> : null}
          </div>
        );
      })}
    </div>
  );
}

function TurnHoverPreview({ turn }: { turn: Turn }): JSX.Element {
  const snippet = turnReplySnippet(turn);
  const body = snippet ? truncateReplyPreview(snippet.text) : "暂无回复";
  const replyCount = snippet?.totalAgentMessages ?? 0;
  return (
    <div className="conversation-turn-rail-preview" role="tooltip">
      <div className="conversation-turn-rail-preview-body">{body}</div>
      <div className="conversation-turn-rail-preview-footer">
        {replyCount > 0 ? `${replyCount} 条回复` : "暂无回复"}
      </div>
    </div>
  );
}