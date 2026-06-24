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
import {
  AUTO_FOLLOW_NESTED_SCROLL_ATTR,
  useAutoFollowScrollContainer,
} from "./AutoFollowScroll";

/**
 * How long to wait after the fold opens before snapping the reasoning
 * scroll container to the bottom. The fold body animates via
 * `grid-template-rows 0fr → 1fr` (~220ms); waiting a touch longer
 * gives the body height time to settle before we read `scrollHeight`,
 * so the first snap lands on the actual final extent instead of a
 * mid-transition value.
 */
const REASONING_FOLD_OPEN_SNAP_DELAY_MS = 280;

/**
 * Unified render surface for the process region of a single turn.
 *
 * The caller passes every process item for the current group as a flat
 * list. This component keeps one root node across single-tool,
 * multi-tool, reasoning, streaming, and settled states, so the visible
 * gray process row does not remount when the group changes shape.
 */
type ProcessSurfaceProps = {
  /**
   * Flat list of every process-region item for this turn. Order is the
   * wire order. The surface filters by type internally; the caller does
   * not have to split tools from reasoning first.
   */
  processItems: ThreadItem[];
  /**
   * True while any process item is still receiving deltas. Drives the
   * tool/reasoning reveal behavior while the fold stays compact until
   * the user opens it.
   */
  streaming: boolean;
  /**
   * True while the surrounding turn is still running. This is visual
   * only: active gray process labels keep sweeping even when an earlier
   * process item has already settled.
   */
  active?: boolean;
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
  onRequestLatest?: () => void;
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
  active,
  renderReasoningItem,
  onRequestLatest,
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
  const activeGrayText = active ?? streaming;

  // Details are opt-in. The running row itself should stay compact by
  // default; expanding it is a user request to inspect the process trail.
  const [expanded, setExpanded] = useState(false);

  useEffect(() => {
    if (!hasDetails) {
      setExpanded(false);
    }
  }, [hasDetails]);

  const handleToggle = (
    event: SyntheticEvent<HTMLDetailsElement>,
  ): void => {
    if (!hasDetails) {
      event.currentTarget.open = false;
      setExpanded(false);
      return;
    }
    const open = event.currentTarget.open;
    setExpanded(open);
    if (open) {
      onRequestLatest?.();
    }
  };

  const handleSummaryClick = (event: SyntheticEvent<HTMLElement>): void => {
    if (!hasDetails) {
      event.preventDefault();
    }
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

  return (
    <div className={className}>
      <details
        className={`process-surface-fold${hasDetails ? " has-details" : " no-details"}${
          expanded ? " expanded" : " collapsed"
        }${
          failed ? " failed" : ""
        }`}
        open={hasDetails && expanded}
        onToggle={handleToggle}
      >
        <summary
          className={`process-surface-row${
            activeGrayText ? " is-live-gray" : ""
          }${streaming ? " is-streaming" : ""}${failed ? " failed" : ""}`}
          onClick={handleSummaryClick}
        >
          {summaryLine}
          {hasDetails ? (
            <ChevronRight
              className="process-surface-chevron icon-xs"
              aria-hidden
            />
          ) : null}
        </summary>
        {hasDetails ? (
          <>
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
                  <ProcessSurfaceReasoningScroll
                    items={reasoningItems}
                    streaming={streaming}
                    renderReasoningItem={renderReasoningItem}
                    foldOpen={expanded}
                  />
                </div>
              ) : null}
            </div>
          </>
        ) : null}
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

/**
 * Reasoning items rendered inside a single scroll container so the
 * fold body stays bounded as the model produces long deliberation
 * trails. The container owns the auto-follow machinery: when the
 * content height grows (token deltas) it snaps to the bottom unless
 * the user has scrolled up to read earlier reasoning, in which case
 * we leave their scroll position alone.
 *
 * Shared by grouped reasoning/tool rows so the parent ProcessSurface
 * keeps one stable component identity while the reasoning content grows.
 */
function ProcessSurfaceReasoningScroll({
  items,
  streaming,
  renderReasoningItem,
  foldOpen,
}: {
  items: ThreadItem[];
  streaming: boolean;
  renderReasoningItem: (
    item: ThreadItem,
    isStreaming: boolean,
  ) => JSX.Element | null;
  foldOpen: boolean;
}): JSX.Element {
  const reasoningScroll = useAutoFollowScrollContainer({
    observeKey: items.map((item) => item.id).join("|"),
    open: foldOpen,
    openScrollDelayMs: REASONING_FOLD_OPEN_SNAP_DELAY_MS,
  });

  return (
    <div
      className="process-surface-reasoning-scroll"
      ref={reasoningScroll.scrollRef}
      {...{ [AUTO_FOLLOW_NESTED_SCROLL_ATTR]: "true" }}
    >
      {items.map((item) => (
        <div key={item.id} className="process-surface-reasoning-item">
          {renderReasoningItem(
            item,
            streaming && item.status === "in_progress",
          )}
        </div>
      ))}
    </div>
  );
}
