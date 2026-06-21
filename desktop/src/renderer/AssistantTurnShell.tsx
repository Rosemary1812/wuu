import { ChevronRight, Info, Play } from "lucide-react";
import {
  type SyntheticEvent,
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import type { ThreadItem, Turn } from "../shared/protocol";
import type {
  AssistantTurnDisplay,
  TurnEntry,
  TurnProcessPreview,
} from "./AssistantTurnDisplay";
import { CollapsibleDetails } from "./CollapsibleMotion";
import { ToolActivityTimeline } from "./ToolActivity";
import { ThreadItemView } from "./ThreadItemView";
import { LightweightStreamingText } from "./LightweightStreamingText";
import {
  buildToolActivityProcessSegments,
  type ToolActivityProcessSegment,
} from "./ToolActivityHelpers";
import { ContextCompactionNotice, TurnNotice } from "./TurnNotice";
import { parseTurnTimestampMs } from "./RunDebugPanel";
import { formatDuration, useLiveNow } from "./TurnProgress";
import {
  turnHasAssistantOutput,
  turnProgressContent,
} from "./TurnViewHelpers";
import type { UserFacingErrorAction } from "./UserFacingErrors";
import { userFacingErrorForMessage } from "./UserFacingErrors";

const REASONING_AUTO_SCROLL_THRESHOLD_PX = 16;
const REASONING_SCROLLBAR_HIDE_DELAY_MS = 700;

export function AssistantTurnShell({
  turn,
  display,
  cwd,
  actionableAgentMessageID,
  latestAgentMessageID,
  onStreamFrame,
  onForkMessage,
  onCollapseComplete,
  onNoticeAction,
}: {
  turn: Turn;
  display: AssistantTurnDisplay;
  cwd?: string;
  actionableAgentMessageID?: string;
  latestAgentMessageID?: string;
  onStreamFrame: () => void;
  onForkMessage?: (turnID: string, itemID: string) => void;
  onCollapseComplete?: () => void;
  onNoticeAction: (action: UserFacingErrorAction) => void;
}): JSX.Element {
  const processEntries = display.entries.filter(
    (entry) => entry.position === "process",
  );
  const answerEntries = display.entries.filter(
    (entry) => entry.position === "answer",
  );

  // Collapse the process fold once the turn is fully settled: the final
  // text is on screen and streaming has stopped. Keeping the fold open
  // while the answer region streams prevents the fold from snapping
  // shut the instant final_answer begins streaming, which would create
  // a visible layout shift next to the still-revealing preview text.
  // The collapse transition itself is handled separately (rule 8 keeps
  // the fold reachable so the user can re-expand it).
  const defaultCollapsed =
    turn.status === "completed" && answerEntries.length > 0;

  const hasProcess =
    processEntries.length > 0 || Boolean(display.latestProcessPreview);
  const hasAnswer = answerEntries.length > 0;

  const className = [
    "assistant-turn-shell",
    hasProcess ? "has-process" : "",
    hasAnswer ? "has-answer" : "",
    display.missingReplyMessage ? "missing-reply-turn" : "",
  ]
    .filter(Boolean)
    .join(" ");

  const entryProps = {
    turn,
    cwd,
    actionableAgentMessageID,
    latestAgentMessageID,
    onStreamFrame,
    onForkMessage,
    onCollapseComplete,
    onNoticeAction,
  };

  return (
    <div className={className}>
      {hasProcess ? (
        <TurnProcessFold
          entries={processEntries}
          defaultCollapsed={defaultCollapsed}
          latestPreview={display.latestProcessPreview}
          {...entryProps}
        />
      ) : null}
      {hasAnswer ? (
        <div className="turn-answer-body">
          {answerEntries.map((entry) => (
            <EntryRenderer key={entry.key} entry={entry} {...entryProps} />
          ))}
        </div>
      ) : null}
      {display.missingReplyMessage ? (
        <aside
          className="turn-notice warning assistant-turn-missing-reply"
          role="status"
        >
          <span className="turn-notice-icon" aria-hidden="true">
            <Info className="icon-sm" />
          </span>
          <span className="turn-notice-copy">
            <strong>没有生成回复</strong>
            <span>{display.missingReplyMessage}</span>
          </span>
        </aside>
      ) : null}
    </div>
  );
}

function TurnProcessFold({
  turn,
  entries,
  defaultCollapsed,
  latestPreview,
  cwd,
  actionableAgentMessageID,
  latestAgentMessageID,
  onStreamFrame,
  onForkMessage,
  onCollapseComplete,
  onNoticeAction,
}: {
  turn: Turn;
  entries: TurnEntry[];
  defaultCollapsed: boolean;
  latestPreview?: TurnProcessPreview;
  cwd?: string;
  actionableAgentMessageID?: string;
  latestAgentMessageID?: string;
  onStreamFrame: () => void;
  onForkMessage?: (turnID: string, itemID: string) => void;
  /**
   * Fires once the fold has finished collapsing so the conversation
   * scroll container can re-anchor `scrollTop = scrollHeight`. The
   * fold collapse drops scrollHeight by the fold body's height, and
   * without this callback the browser silently clamps `scrollTop`
   * to the new max, which the user perceives as the scroll bar
   * jumping upward at turn-settle.
   */
  onCollapseComplete?: () => void;
  onNoticeAction: (action: UserFacingErrorAction) => void;
}): JSX.Element {
  const [expanded, setExpanded] = useState(!defaultCollapsed);
  const previousDefaultCollapsed = useRef(defaultCollapsed);
  const previousExpanded = useRef(expanded);
  const detailsID = `${turn.id}-process-fold`;

  const processCount = entries.reduce(
    (total, entry) => total + (entry.count ?? 1),
    0,
  );
  const completedDuration =
    typeof turn.duration_ms === "number" ? turn.duration_ms : undefined;
  const startedAt = parseTurnTimestampMs(turn.started_at);
  const liveDuration =
    completedDuration === undefined &&
    turn.status === "in_progress" &&
    Number.isFinite(startedAt);
  const liveNow = useLiveNow(liveDuration);
  const elapsedMs =
    completedDuration ?? (liveDuration ? Math.max(0, liveNow - startedAt) : 0);
  const processLabel = turnProgressContent(
    turn,
    elapsedMs,
    turnHasAssistantOutput(turn),
  ).label;
  const metaParts = turnProcessMetaParts(turn, processCount, elapsedMs);

  // Once the parent (Shell) flips `defaultCollapsed` from false → true,
  // collapse the fold; never re-open it automatically. The user is the
  // only one who expands it from there.
  useEffect(() => {
    if (!previousDefaultCollapsed.current && defaultCollapsed) {
      setExpanded(false);
    }
    previousDefaultCollapsed.current = defaultCollapsed;
  }, [defaultCollapsed]);

  // Watch the expanded → collapsed transition and fire the callback
  // once the CSS transition has settled (slightly longer than
  // --collapse-motion-duration so the fold height has reached its
  // final value before the caller re-anchors scrollTop). Without
  // this, the browser would silently clamp scrollTop to the new
  // max as scrollHeight drops by the fold body's height, which the
  // user perceives as the scroll bar jumping upward at turn-settle.
  useEffect(() => {
    if (previousExpanded.current && !expanded) {
      const timeoutId = window.setTimeout(() => {
        onCollapseComplete?.();
      }, 280);
      previousExpanded.current = expanded;
      return () => window.clearTimeout(timeoutId);
    }
    previousExpanded.current = expanded;
    return undefined;
  }, [expanded, onCollapseComplete]);

  const hasDetails = entries.length > 0;
  const hasPreview = Boolean(latestPreview);

  const toggleContent = (
    <>
      <Play
        className="turn-process-glyph icon-xs"
        aria-hidden
        fill="currentColor"
      />
      <span className="turn-process-header">
        <span className="turn-process-title">{processLabel}</span>
        {metaParts.map((part) => (
          <span className="turn-process-meta" key={part}>
            {part}
          </span>
        ))}
      </span>
      {hasPreview ? (
        <span
          className={`turn-process-preview turn-process-preview-${latestPreview?.kind ?? "process"}${
            turn.status === "in_progress" ? " is-live" : ""
          }`}
        >
          <span className="turn-process-live-dot" aria-hidden />
          <LightweightStreamingText
            text={latestPreview?.text ?? ""}
            live={turn.status === "in_progress"}
            className="turn-process-preview-text"
          />
        </span>
      ) : null}
    </>
  );

  // The outer element is a plain <div> instead of a native <details>.
// Native <details> closes instantly with no height transition, so the
// moment the turn settles the fold body snaps from full height to zero
// and the message bubble reflows visibly. We drive the open/closed
// state ourselves and animate it through CollapsibleDetails
// (grid-template-rows + opacity + transform). a11y is preserved with
// role="button" + aria-expanded + aria-controls + an Enter/Space
// keyboard handler, matching what <details>/<summary> gave us for free.
return (
    <div
      className={`turn-process-fold${expanded ? " expanded" : " collapsed"}${
        hasDetails ? "" : " no-details"
      }${hasPreview ? " has-preview" : ""}`}
      id={detailsID}
    >
      <div
        role="button"
        tabIndex={0}
        aria-expanded={expanded}
        aria-controls={`${detailsID}-body`}
        onClick={() => setExpanded(!expanded)}
        onKeyDown={(event) => {
          if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            setExpanded(!expanded);
          }
        }}
        className="turn-process-toggle"
      >
        {toggleContent}
      </div>
      <CollapsibleDetails
        id={`${detailsID}-body`}
        expanded={expanded}
        innerClassName="turn-process-fold-body"
      >
        {hasDetails ? (
          <div className="turn-process-fold-body-inner">
            {entries.map((entry) => (
              <div
                className={`turn-process-entry turn-process-entry-${entry.kind}`}
                key={entry.key}
              >
                <EntryRenderer
                  key={entry.key}
                  entry={entry}
                  turn={turn}
                  cwd={cwd}
                  actionableAgentMessageID={actionableAgentMessageID}
                  latestAgentMessageID={latestAgentMessageID}
                  onStreamFrame={onStreamFrame}
                  onForkMessage={onForkMessage}
                  onNoticeAction={onNoticeAction}
                />
              </div>
            ))}
          </div>
        ) : null}
      </CollapsibleDetails>
    </div>
  );
}

function EntryRenderer({
  entry,
  turn,
  cwd,
  actionableAgentMessageID,
  latestAgentMessageID,
  onStreamFrame,
  onForkMessage,
  onNoticeAction,
}: {
  entry: TurnEntry;
  turn: Turn;
  cwd?: string;
  actionableAgentMessageID?: string;
  latestAgentMessageID?: string;
  onStreamFrame: () => void;
  onForkMessage?: (turnID: string, itemID: string) => void;
  onNoticeAction: (action: UserFacingErrorAction) => void;
}): JSX.Element | null {
  const { item, kind, streaming } = entry;
  if (kind === "process_cluster") {
    return (
      <ProcessClusterRow
        entry={entry}
        turn={turn}
        cwd={cwd}
        onStreamFrame={onStreamFrame}
        onNoticeAction={onNoticeAction}
      />
    );
  }
  if (kind === "activity") {
    if (item.type === "tool_call" || item.type === "collab_agent_tool_call") {
      // Catch-up: when the agent message starts streaming, the tool
      // title should snap to full (LightweightStreamingText live=false)
      // so the user's eye follows the body text rather than a still-
      // filling title above it.
      const toolFakeStreaming =
        streaming &&
        !turn.items.some(
          (i) =>
            i.type === "agent_message" && i.status === "in_progress",
        );
      return (
        <ToolActivityTimeline
          items={entry.items ?? [item]}
          revealItems={streaming}
          streaming={toolFakeStreaming}
        />
      );
    }
    return null;
  }
  if (item.type === "reasoning") {
    // Per the message-display policy (rule 3): reasoning is in the
    // process region, but its content is folded by default. Show a
    // single-line status row ("正在思考" while streaming, "查看思考
    // 过程" once settled) and let the user expand to read the trail.
    // Reasoning never collapses the outer fold on its own, and the
    // user's expanded/collapsed choice persists across re-renders.
    return (
      <ReasoningFold
        item={item}
        streaming={streaming}
        turnID={turn.id}
        turnStatus={turn.status}
        cwd={cwd}
        onStreamFrame={onStreamFrame}
        onNoticeAction={onNoticeAction}
      />
    );
  }
  if (item.type === "agent_message") {
    return (
      <ThreadItemView
        turnID={turn.id}
        turnStatus={turn.status}
        item={item}
        cwd={cwd}
        streaming={streaming}
        actionableAgentMessageID={actionableAgentMessageID}
        latestAgentMessageID={latestAgentMessageID}
        onStreamFrame={onStreamFrame}
        onForkMessage={onForkMessage}
        onNoticeAction={onNoticeAction}
      />
    );
  }
  if (item.type === "context_compaction") {
    return <ContextCompactionNotice text={item.text} />;
  }
  if (item.type === "error") {
    return (
      <TurnNotice
        display={userFacingErrorForMessage(item.error ?? "", "turn")}
        onAction={onNoticeAction}
      />
    );
  }
  return null;
}

function ProcessClusterRow({
  entry,
  turn,
  cwd,
  onStreamFrame,
  onNoticeAction,
}: {
  entry: TurnEntry;
  turn: Turn;
  cwd?: string;
  onStreamFrame: () => void;
  onNoticeAction: (action: UserFacingErrorAction) => void;
}): JSX.Element {
  const items = entry.items ?? [entry.item];
  const toolItems = items.filter(isToolActivityItem);
  const reasoningItems = items.filter(
    (clusterItem) => clusterItem.type === "reasoning",
  );
  const toolSegments = buildToolActivityProcessSegments(toolItems);
  const hasReasoning = reasoningItems.length > 0;
  const hasToolDetails = toolItems.length > 1;
  const hasDetails = hasReasoning || hasToolDetails;
  const reasoningStreaming =
    turn.status === "in_progress" &&
    reasoningItems.some((clusterItem) => clusterItem.status === "in_progress");
  const errors = toolSegments
    .map((segment) => segment.error)
    .filter((error): error is string => Boolean(error));
  const failed = toolSegments.some((segment) => segment.status === "failed");
  const className = `process-cluster-row${
    entry.streaming ? " running is-streaming" : ""
  }${failed ? " failed" : ""}`;

  const summary = (
    <span className="process-cluster-summary-line">
      {toolSegments.map((segment, index) => (
        <ProcessClusterSegmentView
          key={segment.id}
          segment={segment}
          separator={index > 0}
        />
      ))}
      {hasReasoning ? (
        <span className="process-cluster-segment process-cluster-reasoning-segment">
          {toolSegments.length > 0 ? (
            <span className="process-cluster-separator">·</span>
          ) : null}
          <span className="process-cluster-reasoning-label">
            {reasoningStreaming ? "正在思考" : "思考过程"}
          </span>
        </span>
      ) : null}
    </span>
  );

  const errorBlock =
    errors.length > 0 ? (
      <div className="process-cluster-errors">
        {errors.map((message, index) => (
          <div className="activity-detail-error" key={`error-${index}`}>
            {message}
          </div>
        ))}
      </div>
    ) : null;

  if (!hasDetails) {
    return (
      <article className={className}>
        {summary}
        {errorBlock}
      </article>
    );
  }

  return (
    <details className={`process-cluster-fold${failed ? " failed" : ""}`}>
      <summary className={className}>
        {summary}
        <ChevronRight
          className="process-cluster-chevron icon-xs"
          aria-hidden
        />
      </summary>
      {errorBlock}
      <div className="process-cluster-body">
        {hasToolDetails ? (
          <div className="process-cluster-tool-list">
            <ToolActivityTimeline items={toolItems} />
          </div>
        ) : null}
        {hasReasoning ? (
          <div className="process-cluster-reasoning-list">
            {reasoningItems.map((reasoningItem) => (
              <ThreadItemView
                key={reasoningItem.id}
                turnID={turn.id}
                turnStatus={turn.status}
                item={reasoningItem}
                cwd={cwd}
                streaming={
                  turn.status === "in_progress" &&
                  reasoningItem.status === "in_progress"
                }
                onStreamFrame={onStreamFrame}
                onNoticeAction={onNoticeAction}
              />
            ))}
          </div>
        ) : null}
      </div>
    </details>
  );
}

function ProcessClusterSegmentView({
  segment,
  separator,
}: {
  segment: ToolActivityProcessSegment;
  separator: boolean;
}): JSX.Element {
  return (
    <span
      className={`process-cluster-segment process-cluster-segment-${segment.kind}`}
    >
      {separator ? <span className="process-cluster-separator">·</span> : null}
      {typeof segment.count === "number" ? (
        <>
          <span>{segment.countPrefix}</span>
          <AnimatedProcessCount value={segment.count} />
          <span>{segment.countSuffix}</span>
        </>
      ) : (
        <span>{segment.text}</span>
      )}
    </span>
  );
}

function AnimatedProcessCount({ value }: { value: number }): JSX.Element {
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
    <span className={`process-cluster-count${changing ? " is-changing" : ""}`}>
      {value}
    </span>
  );
}

function isToolActivityItem(item: ThreadItem): boolean {
  return item.type === "tool_call" || item.type === "collab_agent_tool_call";
}

function ReasoningFold({
  item,
  streaming,
  turnID,
  turnStatus,
  cwd,
  onStreamFrame,
  onNoticeAction,
}: {
  item: ThreadItem;
  streaming: boolean;
  turnID: string;
  turnStatus: Turn["status"];
  cwd?: string;
  onStreamFrame: () => void;
  onNoticeAction: (action: UserFacingErrorAction) => void;
}): JSX.Element {
  const label = streaming ? "正在思考" : "查看思考过程";
  // Only the currently-streaming reasoning item carries the shimmer
  // sweep — settled items read as static gray prose, matching the
  // other "查看 X" tool rows. The shimmer is the visual signal that
  // "the agent is thinking on this row right now."
  const textClass = `turn-reasoning-summary-text${
    streaming ? " is-streaming" : ""
  }`;
  const reasoningScrollRef = useRef<HTMLDivElement | null>(null);
  const reasoningAutoFollowRef = useRef(true);
  const lastReasoningScrollTopRef = useRef(0);
  const reasoningScrollFrameRef = useRef<number | undefined>(undefined);
  const reasoningScrollbarHideTimerRef = useRef<number | undefined>(undefined);

  const showReasoningScrollbar = useCallback((node: HTMLElement): void => {
    if (node.scrollHeight <= node.clientHeight) {
      return;
    }
    node.classList.add("scrollbar-visible");
    if (reasoningScrollbarHideTimerRef.current !== undefined) {
      window.clearTimeout(reasoningScrollbarHideTimerRef.current);
    }
    reasoningScrollbarHideTimerRef.current = window.setTimeout(() => {
      reasoningScrollbarHideTimerRef.current = undefined;
      node.classList.remove("scrollbar-visible");
    }, REASONING_SCROLLBAR_HIDE_DELAY_MS);
  }, []);

  const scrollReasoningToBottom = useCallback((): void => {
    const node = reasoningScrollRef.current;
    if (!node || !reasoningAutoFollowRef.current) {
      return;
    }
    node.scrollTop = node.scrollHeight;
    lastReasoningScrollTopRef.current = node.scrollTop;
    showReasoningScrollbar(node);
  }, [showReasoningScrollbar]);

  const scheduleReasoningScroll = useCallback((): void => {
    if (!reasoningAutoFollowRef.current) {
      return;
    }
    const node = reasoningScrollRef.current;
    if (!node) {
      return;
    }
    const distanceFromBottom = Math.max(
      0,
      node.scrollHeight - node.scrollTop - node.clientHeight,
    );
    const userMovedAway =
      node.scrollHeight > node.clientHeight &&
      distanceFromBottom > REASONING_AUTO_SCROLL_THRESHOLD_PX &&
      node.scrollTop < lastReasoningScrollTopRef.current;
    if (userMovedAway) {
      lastReasoningScrollTopRef.current = node.scrollTop;
      reasoningAutoFollowRef.current = false;
      return;
    }
    if (reasoningScrollFrameRef.current !== undefined) {
      return;
    }
    reasoningScrollFrameRef.current = window.requestAnimationFrame(() => {
      reasoningScrollFrameRef.current = undefined;
      scrollReasoningToBottom();
    });
  }, [scrollReasoningToBottom]);

  const handleReasoningScrollNode = useCallback(
    (node: HTMLElement): void => {
      showReasoningScrollbar(node);
      const distanceFromBottom = Math.max(
        0,
        node.scrollHeight - node.scrollTop - node.clientHeight,
      );
      const isScrollable = node.scrollHeight > node.clientHeight;
      const scrolledUp = node.scrollTop < lastReasoningScrollTopRef.current;
      lastReasoningScrollTopRef.current = node.scrollTop;
      const atLatestView =
        !isScrollable ||
        distanceFromBottom <= REASONING_AUTO_SCROLL_THRESHOLD_PX;
      if (atLatestView) {
        reasoningAutoFollowRef.current = true;
      } else if (scrolledUp) {
        reasoningAutoFollowRef.current = false;
      }
    },
    [showReasoningScrollbar],
  );

  useLayoutEffect(() => {
    const node = reasoningScrollRef.current;
    if (!node) {
      return undefined;
    }
    const handleScroll = (): void => {
      handleReasoningScrollNode(node);
    };
    node.addEventListener("scroll", handleScroll, { passive: true });
    return () => {
      node.removeEventListener("scroll", handleScroll);
    };
  }, [handleReasoningScrollNode]);

  useLayoutEffect(() => {
    const node = reasoningScrollRef.current;
    if (!node || typeof ResizeObserver === "undefined") {
      return undefined;
    }
    const content = node.firstElementChild;
    const resizeObserver = new ResizeObserver(() => {
      scheduleReasoningScroll();
    });
    resizeObserver.observe(node);
    if (content instanceof HTMLElement) {
      resizeObserver.observe(content);
    }
    return () => {
      resizeObserver.disconnect();
    };
  }, [scheduleReasoningScroll]);

  const handleReasoningStreamFrame = useCallback((): void => {
    onStreamFrame();
    scheduleReasoningScroll();
  }, [onStreamFrame, scheduleReasoningScroll]);

  useEffect(() => {
    return () => {
      if (reasoningScrollFrameRef.current !== undefined) {
        window.cancelAnimationFrame(reasoningScrollFrameRef.current);
      }
      if (reasoningScrollbarHideTimerRef.current !== undefined) {
        window.clearTimeout(reasoningScrollbarHideTimerRef.current);
      }
    };
  }, []);

  // When the user opens this fold, land at the latest reasoning. After
  // that, keep following only while the user stays near the bottom.
  const handleToggle = (event: SyntheticEvent<HTMLDetailsElement>) => {
    const details = event.currentTarget;
    if (!details.open) return;
    const body = details.querySelector(
      ".turn-reasoning-body",
    ) as HTMLElement | null;
    if (!body) return;
    reasoningAutoFollowRef.current = true;
    let settled = false;
    const snapToBottom = (transitionEvent?: Event) => {
      const propertyName = (transitionEvent as TransitionEvent | undefined)
        ?.propertyName;
      if (propertyName && propertyName !== "grid-template-rows") {
        return;
      }
      if (settled) return;
      settled = true;
      body.removeEventListener("transitionend", snapToBottom);
      scrollReasoningToBottom();
    };
    body.addEventListener("transitionend", snapToBottom);
    // Fallback when transitionend never fires (reduced motion, or the
    // grid already settled before the listener attached).
    window.setTimeout(snapToBottom, 280);
  };
  return (
    <details
      className="turn-reasoning-fold"
      onToggle={handleToggle}
    >
      <summary className="turn-reasoning-summary">
        <span className={textClass}>{label}</span>
        <ChevronRight
          className="turn-reasoning-chevron icon-xs"
          aria-hidden
        />
      </summary>
      <div className="turn-reasoning-body">
        <div className="turn-reasoning-body-inner">
          <div
            className="turn-reasoning-scroll"
            ref={reasoningScrollRef}
          >
            <ThreadItemView
              turnID={turnID}
              turnStatus={turnStatus}
              item={item}
              cwd={cwd}
              streaming={streaming}
              onStreamFrame={handleReasoningStreamFrame}
              onNoticeAction={onNoticeAction}
            />
          </div>
        </div>
      </div>
    </details>
  );
}

function turnProcessMetaParts(
  turn: Turn,
  processCount: number,
  elapsedMs: number,
): string[] {
  const parts: string[] = [];
  if (turn.status === "in_progress") {
    parts.push(formatDuration(elapsedMs));
    return parts;
  }
  if (processCount > 0) {
    parts.push(`${processCount} 项`);
  }
  if (typeof turn.duration_ms === "number") {
    parts.push(formatDuration(turn.duration_ms));
  }
  return parts;
}
