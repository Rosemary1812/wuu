import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type PointerEvent as ReactPointerEvent,
  type RefObject,
} from "react";
import type { Turn } from "../shared/protocol";
import { queryTextForUserItem } from "./AppState";
import type { QueryHistoryEntry } from "./QueryHistoryPopover";
import { RichContent } from "./RichContent";
import { firstUserMessageText, turnReplySnippet } from "./TurnViewHelpers";

// Rail geometry. The macOS Dock magnification model drives the numbers:
// a default bar is short, the hovered bar grows by ~3x, and the two
// neighbours grow by ~1.5x. The spring easing is applied via CSS
// (cubic-bezier with slight overshoot) so React stays out of the
// animation loop.
const RAIL_BAR_DEFAULT_WIDTH = 18;
const RAIL_BAR_ADJACENT_WIDTH = 22;
const RAIL_BAR_HOVERED_WIDTH = 40;
const WHEEL_LINE_DELTA_PX = 16;
export const CONVERSATION_TURN_RAIL_VISIBLE_LIMIT = 36;

export function conversationTurnRailWindow(
  turns: readonly Turn[],
  focusTurnID: string | undefined,
  limit = CONVERSATION_TURN_RAIL_VISIBLE_LIMIT,
): { turns: Turn[]; startIndex: number } {
  const normalizedLimit = Math.max(1, Math.floor(limit));
  if (turns.length <= normalizedLimit) {
    return { turns: [...turns], startIndex: 0 };
  }

  const focusIndex = focusTurnID
    ? turns.findIndex((turn) => turn.id === focusTurnID)
    : -1;
  const centerIndex = focusIndex >= 0 ? focusIndex : turns.length - 1;
  const halfWindow = Math.floor(normalizedLimit / 2);
  const maxStart = Math.max(0, turns.length - normalizedLimit);
  const startIndex = Math.min(
    Math.max(0, centerIndex - halfWindow),
    maxStart,
  );

  return {
    turns: turns.slice(startIndex, startIndex + normalizedLimit),
    startIndex,
  };
}

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
  scrollContainerRef,
  getScrollContainer,
  maxVisibleTurns = CONVERSATION_TURN_RAIL_VISIBLE_LIMIT,
  onWheelScrollAway,
  onDragScrollAway,
  onSelectQueryHistory,
}: {
  turns: Turn[];
  activeTurnID?: string;
  scrollContainerRef?: RefObject<HTMLElement | null>;
  getScrollContainer?: () => HTMLElement | null;
  maxVisibleTurns?: number;
  onWheelScrollAway?: () => void;
  onDragScrollAway?: () => void;
  onSelectQueryHistory: (entry: QueryHistoryEntry) => void;
}): JSX.Element | null {
  const [hoveredTurnID, setHoveredTurnID] = useState<string | undefined>();
  const [viewportTurnID, setViewportTurnID] = useState<string | undefined>();
  // Turn currently "carried" by an in-flight pointer drag. Reuses the same
  // hovered/adjacent visual treatment, so dragging feels like the cursor's
  // magnetised hover is sliding across the rail instead of snapping.
  const [draggingTurnID, setDraggingTurnID] = useState<string | undefined>();
  const containerRef = useRef<HTMLDivElement | null>(null);
  const dragStateRef = useRef<{
    pointerId: number;
    startClientY: number;
    startScrollTop: number;
    railHeight: number;
    maxScrollTop: number;
  } | null>(null);
  // Mirror of dragStateRef so the mousemove magnet handler can see whether
  // a drag is in progress and stop fighting the pointer-drag-driven
  // highlight. Without this, mousemove keeps setting `hoveredTurnID` to
  // the same bar under a stationary cursor, hiding the dragging
  // highlight that's also trying to track the cursor.
  const isDraggingRef = useRef(false);
  const resolveScrollContainer = useCallback(
    () => getScrollContainer?.() ?? scrollContainerRef?.current ?? null,
    [getScrollContainer, scrollContainerRef],
  );

  const isEmpty = turns.length === 0;
  const windowFocusTurnID = viewportTurnID ?? activeTurnID;
  const { turns: visibleTurns, startIndex } = useMemo(
    () => conversationTurnRailWindow(turns, windowFocusTurnID, maxVisibleTurns),
    [maxVisibleTurns, turns, windowFocusTurnID],
  );
  // A pointer drag reuses the same hover/adjacent visual treatment. Dragging
  // wins over hover so a stale hover state cannot pin the highlight to the bar
  // where the press started while the pointer moves through the rail.
  const focusedTurnID = draggingTurnID ?? hoveredTurnID;
  const hoveredIndex = visibleTurns.findIndex((t) => t.id === focusedTurnID);
  const adjacentIndices = new Set<number>();
  if (hoveredIndex >= 0) {
    if (hoveredIndex > 0) {
      adjacentIndices.add(hoveredIndex - 1);
    }
    if (hoveredIndex < visibleTurns.length - 1) {
      adjacentIndices.add(hoveredIndex + 1);
    }
  }

  useEffect(() => {
    if (!hoveredTurnID) {
      return;
    }
    if (!visibleTurns.some((turn) => turn.id === hoveredTurnID)) {
      setHoveredTurnID(undefined);
    }
  }, [hoveredTurnID, visibleTurns]);

  useEffect(() => {
    if (!draggingTurnID) {
      return;
    }
    if (!visibleTurns.some((turn) => turn.id === draggingTurnID)) {
      setDraggingTurnID(undefined);
    }
  }, [draggingTurnID, visibleTurns]);

  useEffect(() => {
    const scrollNode = resolveScrollContainer();
    if (!scrollNode || turns.length === 0) {
      setViewportTurnID(undefined);
      return;
    }

    let frameID: number | undefined;
    const updateViewportTurn = () => {
      frameID = undefined;
      const nextTurnID = visibleTurnIDForScrollNode(scrollNode, turns);
      setViewportTurnID((current) =>
        current === nextTurnID ? current : nextTurnID,
      );
    };
    const scheduleUpdate = () => {
      if (frameID !== undefined) {
        return;
      }
      frameID = window.requestAnimationFrame(updateViewportTurn);
    };

    updateViewportTurn();
    scrollNode.addEventListener("scroll", scheduleUpdate, { passive: true });
    window.addEventListener("resize", scheduleUpdate);

    return () => {
      if (frameID !== undefined) {
        window.cancelAnimationFrame(frameID);
      }
      scrollNode.removeEventListener("scroll", scheduleUpdate);
      window.removeEventListener("resize", scheduleUpdate);
    };
  }, [resolveScrollContainer, turns]);

  // Magnet the hover to the bar closest to the mouse Y. This keeps the
  // hover continuous when the mouse crosses the gap between bars (the
  // 8px gap in the flex container would otherwise be a dead zone).
  useEffect(() => {
    const container = containerRef.current;
    if (!container) {
      return;
    }

    function handleMouseMove(event: MouseEvent) {
      if (!container || isDraggingRef.current) {
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

  useEffect(() => {
    const container = containerRef.current;
    if (!container) {
      return;
    }

    function handleWheel(event: WheelEvent): void {
      const scrollNode = resolveScrollContainer();
      if (!scrollNode) {
        return;
      }
      const deltaY = wheelEventDeltaYPixels(event, scrollNode);
      if (deltaY === 0) {
        return;
      }

      const previousScrollTop = scrollNode.scrollTop;
      if (deltaY < 0 && previousScrollTop > 0) {
        onWheelScrollAway?.();
      }
      scrollNode.scrollTop = previousScrollTop + deltaY;
      if (scrollNode.scrollTop !== previousScrollTop) {
        event.preventDefault();
      }
    }

    container.addEventListener("wheel", handleWheel, { passive: false });
    return () => container.removeEventListener("wheel", handleWheel);
  }, [onWheelScrollAway, resolveScrollContainer]);

  // Press-and-drag scrolling: the user presses anywhere on the rail, then
  // moves the mouse up/down. We translate vertical pointer movement into a
  // scrollTop change on the actual conversation scroller, so the rail
  // behaves like a thin scrollbar thumb. Upward drags also disable
  // auto-follow so streaming never yanks the viewport back to the bottom
  // mid-gesture.
  useEffect(() => {
    const railContainer: HTMLDivElement | null = containerRef.current;
    if (!railContainer) {
      return;
    }
    const container = railContainer;

    function pointerY(event: PointerEvent): number {
      return event.clientY;
    }

    function closestTurnIDAt(clientY: number): string | undefined {
      const barElements = container.querySelectorAll<HTMLElement>(
        ".conversation-turn-rail-bar",
      );
      let bestID: string | undefined;
      let bestDistance = Infinity;
      for (const bar of barElements) {
        const rect = bar.getBoundingClientRect();
        const centerY = rect.top + rect.height / 2;
        const distance = Math.abs(clientY - centerY);
        if (distance < bestDistance) {
          bestDistance = distance;
          bestID = bar.getAttribute("data-turn-id") ?? undefined;
        }
      }
      if (!bestID || bestID === "placeholder") {
        return undefined;
      }
      return bestID;
    }

    function handlePointerDown(event: PointerEvent): void {
      if (event.button !== 0) {
        return;
      }
      const scrollNode = resolveScrollContainer();
      if (!scrollNode) {
        return;
      }
      const railRect = container.getBoundingClientRect();
      const railHeight = Math.max(1, railRect.height);
      const maxScrollTop = Math.max(
        0,
        scrollNode.scrollHeight - scrollNode.clientHeight,
      );
      dragStateRef.current = {
        pointerId: event.pointerId,
        startClientY: pointerY(event),
        startScrollTop: scrollNode.scrollTop,
        railHeight,
        maxScrollTop,
      };
      isDraggingRef.current = true;
      setHoveredTurnID(undefined);
      // Lock the visual highlight to whichever bar the pointer is over so
      // the user sees the same "this bar is being dragged" feedback as
      // the hover state, and the highlight then follows the pointer as
      // we translate the drag into scrollTop changes.
      setDraggingTurnID(closestTurnIDAt(pointerY(event)));
      try {
        container.setPointerCapture(event.pointerId);
      } catch {
        // Pointer capture is best-effort; native listeners still get the
        // move events while the pointer is held inside the rail.
      }
      event.preventDefault();
      if (event.clientY < railRect.top || event.clientY > railRect.bottom) {
        const ratio = Math.min(
          1,
          Math.max(0, (event.clientY - railRect.top) / railHeight),
        );
        scrollNode.scrollTop = ratio * maxScrollTop;
        // The scroll container just jumped; refresh the highlight so it
        // lines up with the bar now under the cursor (rail content may
        // have shifted under a stationary mouse).
        setDraggingTurnID(closestTurnIDAt(pointerY(event)));
      }
      if (event.clientY > railRect.top + railRect.height / 2) {
        onDragScrollAway?.();
      }
    }

    function handlePointerMove(event: PointerEvent): void {
      const drag = dragStateRef.current;
      if (!drag || drag.pointerId !== event.pointerId) {
        return;
      }
      const scrollNode = resolveScrollContainer();
      if (!scrollNode) {
        return;
      }
      const deltaY = pointerY(event) - drag.startClientY;
      const railRatio = deltaY / drag.railHeight;
      const nextScrollTop = Math.min(
        Math.max(0, drag.startScrollTop + railRatio * drag.maxScrollTop),
        drag.maxScrollTop,
      );
      if (scrollNode.scrollTop !== nextScrollTop) {
        scrollNode.scrollTop = nextScrollTop;
        event.preventDefault();
      }
      // Slide the highlight to whichever bar is now under the cursor so
      // the user gets continuous hover-style feedback as the rail scrolls.
      // Always recompute — even when scrollTop didn't change — because the
      // pointer can move within the rail without triggering a scroll, and
      // that still needs the bar under the cursor to light up.
      setDraggingTurnID(closestTurnIDAt(pointerY(event)));
    }

    function endDrag(event: PointerEvent): void {
      const drag = dragStateRef.current;
      if (!drag || drag.pointerId !== event.pointerId) {
        return;
      }
      dragStateRef.current = null;
      isDraggingRef.current = false;
      if (container.hasPointerCapture(event.pointerId)) {
        container.releasePointerCapture(event.pointerId);
      }
      setDraggingTurnID(undefined);
      setHoveredTurnID(undefined);
    }

    container.addEventListener("pointerdown", handlePointerDown);
    container.addEventListener("pointermove", handlePointerMove);
    container.addEventListener("pointerup", endDrag);
    container.addEventListener("pointercancel", endDrag);
    return () => {
      container.removeEventListener("pointerdown", handlePointerDown);
      container.removeEventListener("pointermove", handlePointerMove);
      container.removeEventListener("pointerup", endDrag);
      container.removeEventListener("pointercancel", endDrag);
    };
  }, [onDragScrollAway, resolveScrollContainer]);

  // Click a bar through the same query-history selection path used by
  // the docked environment-panel list. That parent path disables
  // auto-follow before jumping, so streaming cannot snap the view back
  // to the bottom immediately after the click.
  function handleBarClick(turn: Turn) {
    for (const item of turn.items ?? []) {
      const text = queryTextForUserItem(item);
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
      {visibleTurns.map((turn, index) => {
          const globalIndex = startIndex + index;
          const isHovered = turn.id === focusedTurnID;
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
              aria-label={`跳转到第 ${globalIndex + 1} 轮对话`}
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

function wheelEventDeltaYPixels(
  event: WheelEvent,
  scrollNode: HTMLElement,
): number {
  if (event.deltaY !== 0) {
    if (event.deltaMode === WheelEvent.DOM_DELTA_LINE) {
      return event.deltaY * WHEEL_LINE_DELTA_PX;
    }
    if (event.deltaMode === WheelEvent.DOM_DELTA_PAGE) {
      return event.deltaY * scrollNode.clientHeight;
    }
    return event.deltaY;
  }
  return event.deltaX;
}

function visibleTurnIDForScrollNode(
  scrollNode: HTMLElement,
  turns: readonly Turn[],
): string | undefined {
  if (turns.length === 0) {
    return undefined;
  }
  const distanceFromBottom = Math.max(
    0,
    scrollNode.scrollHeight - scrollNode.scrollTop - scrollNode.clientHeight,
  );
  if (distanceFromBottom <= 4) {
    return turns[turns.length - 1]?.id;
  }

  const activePane =
    scrollNode.querySelector<HTMLElement>(
      '.cached-conversation-pane[data-active="true"]',
    ) ?? scrollNode;
  const turnNodes = Array.from(
    activePane.querySelectorAll<HTMLElement>(".turn[data-turn-id]"),
  );
  if (turnNodes.length === 0) {
    return undefined;
  }

  const viewportRect = scrollNode.getBoundingClientRect();
  const anchorY = viewportRect.top + Math.min(viewportRect.height * 0.38, 220);
  let fallbackTurnID: string | undefined;
  for (const node of turnNodes) {
    const turnID = node.dataset.turnId;
    if (!turnID) {
      continue;
    }
    const rect = node.getBoundingClientRect();
    if (rect.bottom < viewportRect.top) {
      fallbackTurnID = turnID;
      continue;
    }
    if (rect.bottom >= anchorY) {
      return turnID;
    }
    fallbackTurnID = turnID;
  }
  return fallbackTurnID ?? turns[turns.length - 1]?.id;
}

function TurnHoverPreview({ turn }: { turn: Turn }): JSX.Element {
  const firstUserText = firstUserMessageText(turn);
  const snippet = turnReplySnippet(turn);
  const body = snippet ? snippet.text : "暂无回复";
  return (
    <div className="conversation-turn-rail-preview" role="tooltip">
      {firstUserText ? (
        <div className="conversation-turn-rail-preview-query">
          <RichContent text={firstUserText} />
        </div>
      ) : null}
      <div className="conversation-turn-rail-preview-body">
        <RichContent text={body} />
      </div>
    </div>
  );
}
