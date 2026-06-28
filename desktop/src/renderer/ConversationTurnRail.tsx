import { useEffect, useRef, useState } from "react";
import type { Turn } from "../shared/protocol";
import type { QueryHistoryEntry } from "./QueryHistoryPopover";
import { firstUserMessageText, truncateReplyPreview, turnReplySnippet } from "./TurnViewHelpers";

// Rail geometry. The macOS Dock magnification model drives the numbers:
// a default bar is short, the hovered bar grows by ~3x, and the two
// neighbours grow by ~1.5x. The spring easing is applied via CSS
// (cubic-bezier with slight overshoot) so React stays out of the
// animation loop.
const RAIL_BAR_DEFAULT_WIDTH = 18;
const RAIL_BAR_ADJACENT_WIDTH = 22;
const RAIL_BAR_HOVERED_WIDTH = 40;

/**
 * Vertical rail of horizontal bars on the left edge of the message stream.
 *
 * One bar per turn. Hovering a bar magnifies it (and its two neighbours
 * slightly, in classic Dock fashion) and reveals a preview card to the
 * right with that turn's first agent reply.
 *
 * Hover continuity ("连贯"): the container is the hover zone
 * (pointer-events: auto) and a `mousemove` handler finds the bar
 * closest to the mouse Y position, so the hover effect stays
 * continuous when the mouse crosses the gap between bars. A transparent
 * bridge element covers the 12px gap between each bar and its preview
 * card, so the mouse can move from bar -> preview without losing the
 * hover state. The bridge is non-interactive by default (doesn't
 * block the chat content) and only becomes interactive when the bar is
 * hovered.
 *
 * The rail is hidden until the thread has turns. The empty-session welcome
 * screen should stay clean; the rail becomes useful only after there is
 * actual conversation history to navigate.
 */
export function ConversationTurnRail({
  turns,
  activeTurnID,
  onSelectQueryHistory,
}: {
  turns: Turn[];
  activeTurnID?: string;
  onSelectQueryHistory: (entry: QueryHistoryEntry) => void;
}): JSX.Element | null {
  const [hoveredTurnID, setHoveredTurnID] = useState<string | undefined>();
  const containerRef = useRef<HTMLDivElement | null>(null);

  const isEmpty = turns.length === 0;
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

  // Magnet the hover to the bar closest to the mouse Y. This keeps the
  // hover continuous when the mouse crosses the gap between bars (the
  // 8px gap in the flex container would otherwise be a dead zone).
  useEffect(() => {
    const container = containerRef.current;
    if (!container) {
      return;
    }

    function handleMouseMove(event: MouseEvent) {
      if (!container) {
        return;
      }
      const barElements = container.querySelectorAll<HTMLElement>(
        ".conversation-turn-rail-bar"
      );
      if (barElements.length === 0) {
        return;
      }

      const mouseY = event.clientY;
      let closestBar: HTMLElement | undefined;
      let closestDistance = Infinity;

      for (const bar of barElements) {
        const rect = bar.getBoundingClientRect();
        const barCenterY = rect.top + rect.height / 2;
        const distance = Math.abs(mouseY - barCenterY);
        if (distance < closestDistance) {
          closestDistance = distance;
          closestBar = bar;
        }
      }

      if (!closestBar) {
        return;
      }
      const turnID = closestBar.getAttribute("data-turn-id");
      if (!turnID || turnID === "placeholder") {
        return;
      }
      setHoveredTurnID((current) => (current === turnID ? current : turnID));
    }

    function handleMouseLeave() {
      setHoveredTurnID(undefined);
    }

    container.addEventListener("mousemove", handleMouseMove);
    container.addEventListener("mouseleave", handleMouseLeave);

    return () => {
      container.removeEventListener("mousemove", handleMouseMove);
      container.removeEventListener("mouseleave", handleMouseLeave);
    };
  }, [turns]);

  // Click a bar through the same query-history selection path used by
  // the docked environment-panel list. That parent path disables
  // auto-follow before jumping, so streaming cannot snap the view back
  // to the bottom immediately after the click.
  function handleBarClick(turn: Turn) {
    for (const item of turn.items ?? []) {
      if (item.type !== "user_message") {
        continue;
      }
      const text = (item.text ?? "").trim();
      if (!text) {
        continue;
      }
      onSelectQueryHistory({ turnID: turn.id, itemID: item.id, text });
      return;
    }
  }

  if (isEmpty) {
    return null;
  }

  return (
    <div
      ref={containerRef}
      className="conversation-turn-rail"
      aria-label="对话回合导航"
    >
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
              data-turn-id={turn.id}
              className={className}
              onClick={() => handleBarClick(turn)}
              onKeyDown={(event) => {
                if (event.key === "Enter" || event.key === " ") {
                  event.preventDefault();
                  handleBarClick(turn);
                }
              }}
              role="button"
              tabIndex={0}
              aria-label={`跳转到第 ${index + 1} 轮对话`}
            >
              <div
                className="conversation-turn-rail-bridge"
                aria-hidden="true"
              />
              {isHovered ? <TurnHoverPreview turn={turn} /> : null}
            </div>
          );
        })}
    </div>
  );
}

function TurnHoverPreview({ turn }: { turn: Turn }): JSX.Element {
  const firstUserText = firstUserMessageText(turn);
  const snippet = turnReplySnippet(turn);
  const body = snippet ? truncateReplyPreview(snippet.text) : "暂无回复";
  return (
    <div className="conversation-turn-rail-preview" role="tooltip">
      {firstUserText ? (
        <div className="conversation-turn-rail-preview-query">
          {truncateReplyPreview(firstUserText)}
        </div>
      ) : null}
      <div className="conversation-turn-rail-preview-body">{body}</div>
    </div>
  );
}
