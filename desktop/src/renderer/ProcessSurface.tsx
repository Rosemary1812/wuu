import {
  useEffect,
  useRef,
  useState,
  type JSX,
  type SyntheticEvent,
} from "react";
import { ChevronRight } from "lucide-react";
import type { ThreadItem } from "../shared/protocol";
import {
  buildToolActivityProcessSegments,
  type ToolActivityProcessSegment,
} from "./ToolActivityHelpers";
import { ToolActivityTimeline } from "./ToolActivity";

/**
 * Unified render surface for the process region of a single turn.
 *
 * Motivation (the "process cluster flicker"): the prior architecture
 * dispatched a single in-flight tool call to <ToolActivityTimeline> and
 * the moment a second process item arrived to a brand-new
 * <ProcessClusterRow>. The two components share no DOM, so React
 * unmounted the first card and remounted a fresh cluster on a single
 * frame. The new cluster mounted with `is-streaming` instantly flipping
 * the text to `color: transparent` + `background-clip: text`, which
 * read as a one-frame flicker before the shimmer gradient sweep
 * "re-illuminated" the glyphs.
 *
 * ProcessSurface fixes the unmount/remount by owning the entire
 * process-region lifecycle. From the first tool call to the final
 * settled state, the caller passes a flat `processItems` list and a
 * `streaming` flag; the surface decides internally whether to render a
 * flat row (single tool) or a fold (multiple items, reasoning, or
 * both). The same React component instance is reused across every
 * transition, so React never has to swap DOM trees.
 *
 * The shimmer pattern (an overlay element rather than
 * `color: transparent`) is wired in via the `is-streaming` class on
 * the row. Commit 4 of the redesign adds the matching CSS; the class
 * names below are stable so the CSS pass is independent.
 */
type ProcessSurfaceProps = {
  /**
   * Flat list of every process-region item for this turn. Order is the
   * wire order. The surface filters by type internally; the caller does
   * not have to pre-cluster.
   */
  processItems: ThreadItem[];
  /**
   * True while any process item is still receiving deltas. Drives the
   * shimmer overlay and the auto-expand-while-running transition. The
   * caller flips this atomically with `turn.status`.
   */
  streaming: boolean;
  /**
   * Optional render hook for reasoning items in the expanded body.
   * The surface is decoupled from the reasoning fold's scroll and
   * auto-follow machinery — the parent already has the ThreadItemView
   * that knows how to render a reasoning item with full behavior.
   * Pass `undefined` to omit reasoning items from the body (the
   * summary line still mentions "思考过程" / "正在思考").
   */
  renderReasoningItem?: (
    item: ThreadItem,
    streaming: boolean,
  ) => JSX.Element | null;
};

const TOOL_ACTIVITY_ITEM_TYPES = new Set<string>([
  "tool_call",
  "collab_agent_tool_call",
]);

function isToolActivityItem(item: ThreadItem): boolean {
  return TOOL_ACTIVITY_ITEM_TYPES.has(item.type);
}

export function ProcessSurface({
  processItems,
  streaming,
  renderReasoningItem,
}: ProcessSurfaceProps): JSX.Element {
  const toolItems = processItems.filter(isToolActivityItem);
  const reasoningItems = processItems.filter(
    (item) => item.type === "reasoning",
  );
  const toolSegments = buildToolActivityProcessSegments(toolItems);
  const hasReasoning = reasoningItems.length > 0;
  const hasMultipleTools = toolItems.length > 1;
  const hasDetails = hasReasoning || hasMultipleTools;
  const hasErrors = toolSegments.some((segment) => Boolean(segment.error));
  const failed = toolSegments.some((segment) => segment.status === "failed");
  const reasoningStreaming =
    streaming &&
    reasoningItems.some((item) => item.status === "in_progress");

  // Expand/collapse state. While streaming, the fold auto-opens so the
  // user can see what is running. Once the user clicks the summary we
  // stop auto-controlling their choice for the rest of the surface's
  // lifetime. Initial value matches streaming, so the first render
  // already reflects the auto-expand decision without an extra
  // setState round-trip.
  const [expanded, setExpanded] = useState(streaming && hasDetails);
  const userInteractedRef = useRef(false);

  useEffect(() => {
    if (!hasDetails) {
      setExpanded(false);
      return;
    }
    if (!userInteractedRef.current) {
      // No user interaction yet — keep the fold in sync with streaming.
      setExpanded(streaming);
    }
  }, [streaming, hasDetails]);

  const handleToggle = (
    event: SyntheticEvent<HTMLDetailsElement>,
  ): void => {
    userInteractedRef.current = true;
    setExpanded(event.currentTarget.open);
  };

  const className = `process-surface${
    hasDetails ? " has-details" : " no-details"
  }${streaming ? " is-streaming" : ""}${failed ? " failed" : ""}`;

  const summaryLine = (
    <span className="process-surface-summary-line">
      {toolSegments.map((segment, index) => (
        <ProcessSurfaceSegmentView
          key={segment.id}
          segment={segment}
          separator={index > 0}
        />
      ))}
      {hasReasoning ? (
        <span className="process-surface-segment process-surface-reasoning-segment">
          {toolSegments.length > 0 ? (
            <span className="process-surface-separator">·</span>
          ) : null}
          <span className="process-surface-reasoning-label">
            {reasoningStreaming ? "正在思考" : "思考过程"}
          </span>
        </span>
      ) : null}
    </span>
  );

  const errorBlock =
    hasErrors && toolSegments.some((segment) => Boolean(segment.error)) ? (
      <div className="process-surface-errors">
        {toolSegments
          .map((segment) => segment.error)
          .filter((message): message is string => Boolean(message))
          .map((message, index) => (
            <div
              className="activity-detail-error"
              key={`error-${index}`}
            >
              {message}
            </div>
          ))}
      </div>
    ) : null;

  if (!hasDetails) {
    // Single tool call with no reasoning: no fold needed, render the
    // summary inline. There is no expandable body, so a native <details>
    // would just add an inert disclosure triangle. The root <div
    // class="process-surface"> is the stable identity that survives the
    // 1-tool → 2-tool transition above.
    return (
      <div className={className}>
        <article className="process-surface-inline">{summaryLine}</article>
      </div>
    );
  }

  return (
    <div className={className}>
      <details
        className={`process-surface-fold${expanded ? " expanded" : " collapsed"}${
          failed ? " failed" : ""
        }`}
        open={expanded}
        onToggle={handleToggle}
      >
        <summary
          className={`process-surface-row${streaming ? " is-streaming" : ""}${
            failed ? " failed" : ""
          }`}
        >
          {summaryLine}
          <ChevronRight
            className="process-surface-chevron icon-xs"
            aria-hidden
          />
        </summary>
        {errorBlock}
        <div className="process-surface-body">
          {hasMultipleTools ? (
            <div className="process-surface-tool-list">
              <ToolActivityTimeline
                items={toolItems}
                revealItems={streaming}
                streaming={streaming}
              />
            </div>
          ) : null}
          {hasReasoning && renderReasoningItem ? (
            <div className="process-surface-reasoning-list">
              {reasoningItems.map((item) => (
                <span
                  key={item.id}
                  className="process-surface-reasoning-item"
                >
                  {renderReasoningItem(
                    item,
                    streaming && item.status === "in_progress",
                  )}
                </span>
              ))}
            </div>
          ) : null}
        </div>
      </details>
    </div>
  );
}

function ProcessSurfaceSegmentView({
  segment,
  separator,
}: {
  segment: ToolActivityProcessSegment;
  separator: boolean;
}): JSX.Element {
  return (
    <span
      className={`process-surface-segment process-surface-segment-${segment.kind}`}
    >
      {separator ? (
        <span className="process-surface-separator">·</span>
      ) : null}
      {typeof segment.count === "number" ? (
        <>
          <span>{segment.countPrefix}</span>
          <ProcessSurfaceAnimatedCount value={segment.count} />
          <span>{segment.countSuffix}</span>
        </>
      ) : (
        <span>{segment.text}</span>
      )}
    </span>
  );
}

function ProcessSurfaceAnimatedCount({
  value,
}: {
  value: number;
}): JSX.Element {
  const previousValue = useRef(value);
  const [changing, setChanging] = useState(false);

  useEffect(() => {
    if (previousValue.current === value) {
      return undefined;
    }
    previousValue.current = value;
    setChanging(true);
    const timeoutId = window.setTimeout(() => {
      setChanging(false);
    }, 180);
    return () => window.clearTimeout(timeoutId);
  }, [value]);

  return (
    <span
      className={`process-surface-count${changing ? " is-changing" : ""}`}
    >
      {value}
    </span>
  );
}
