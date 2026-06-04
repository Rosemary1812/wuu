/**
 * QueryHistoryRail — the always-visible index of past queries.
 *
 * Mirrors the ChatGPT pattern of showing a thin column of bars on the
 * right edge of the conversation: each past user query becomes a single
 * rounded bar, stacked top-to-bottom in chronological order. The newest
 * query sits at the bottom. The rail itself is the hover target for the
 * companion `QueryHistoryPopover`, keeping the quick-jump list tied to
 * the scrollbar area instead of the full conversation body.
 *
 * The rail does not own open/close state; the parent keeps that state so
 * the same popover can be rendered as a floating panel or docked below
 * the environment panel.
 */
import type { JSX, RefObject } from "react";

import type { QueryHistoryEntry } from "./QueryHistoryPopover";

export type QueryHistoryRailProps = {
  entries: QueryHistoryEntry[];
  /**
   * Maximum number of bars to render. Anything beyond collapses into a
   * single "+N more" bar at the bottom. Pass `undefined` to disable the
   * cap (not recommended — the rail is meant to be a quick index).
   */
  maxBars?: number;
  active?: boolean;
  railRef?: RefObject<HTMLDivElement | null>;
  onHoverStart?: () => void;
  onHoverEnd?: () => void;
};

export function QueryHistoryRail({
  entries,
  maxBars,
  active = false,
  railRef,
  onHoverStart,
  onHoverEnd,
}: QueryHistoryRailProps): JSX.Element | null {
  if (entries.length === 0) {
    return null;
  }
  const visible =
    maxBars !== undefined ? entries.slice(0, maxBars) : entries;
  const hidden = entries.length - visible.length;
  return (
    <div
      ref={railRef}
      className={`query-history-rail${active ? " active" : ""}`}
      role="button"
      tabIndex={0}
      aria-label="历史输入索引"
      onMouseEnter={onHoverStart}
      onMouseLeave={onHoverEnd}
      onFocus={onHoverStart}
      onBlur={onHoverEnd}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onHoverStart?.();
        } else if (event.key === "Escape") {
          onHoverEnd?.();
          event.currentTarget.blur();
        }
      }}
    >
      {visible.map((entry) => (
        <span
          key={`${entry.turnID}:${entry.itemID}`}
          className="query-history-rail-bar"
        />
      ))}
      {hidden > 0 ? (
        <span className="query-history-rail-bar query-history-rail-overflow" />
      ) : null}
    </div>
  );
}
