import { ChevronRight, Info, Play } from "lucide-react";
import {
  type SyntheticEvent,
  useCallback,
  useEffect,
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
import { ThreadItemView } from "./ThreadItemView";
import { LightweightStreamingText } from "./LightweightStreamingText";
import { ContextCompactionNotice, TurnNotice } from "./TurnNotice";
import { parseTurnTimestampMs } from "./RunDebugPanel";
import { formatDuration, useLiveNow } from "./TurnProgress";
import { ProcessSurface } from "./ProcessSurface";
import {
  turnHasAssistantOutput,
  turnProgressContent,
} from "./TurnViewHelpers";
import type { UserFacingErrorAction } from "./UserFacingErrors";
import { userFacingErrorForMessage } from "./UserFacingErrors";
import {
  AUTO_FOLLOW_NESTED_SCROLL_ATTR,
  useAutoFollowScrollContainer,
} from "./AutoFollowScroll";

/**
 * How long to wait after a turn completes (turn.status → completed)
 * before auto-collapsing the process fold. Decouples the "turn is
 * done" signal (label / timer / preview update at t=0) from the
 * fold body collapse (starts at t=settling-delay), so the user
 * sees one motion at a time instead of five state changes on the
 * same frame.
 */
const PROCESS_FOLD_SETTLING_DELAY_MS = 600;

export function AssistantTurnShell({
  turn,
  display,
  cwd,
  actionableAgentMessageID,
  latestAgentMessageID,
  onStreamFrame,
  onForkMessage,
  onRequestLatest,
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
  onRequestLatest?: () => void;
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
    onRequestLatest,
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
  onRequestLatest,
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
  onRequestLatest?: () => void;
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
  const [settled, setSettled] = useState(turn.status !== "in_progress");
  // Tracks whether the user has manually toggled the fold during the
  // current turn. If so, the auto-collapse on turn completion is
  // suppressed — the user has expressed their own intent for the
  // fold state, and we shouldn't override it.
  const userToggledRef = useRef(false);
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

  // Two-stage auto-collapse on turn completion. The fold's "settle"
  // (label / timer / preview update) and "collapse" (body shrinks)
  // are decoupled so the user sees one motion at a time instead of
  // five state changes on the same frame.
  //   t=0      : turn.status flips in_progress → completed
  //   t=settle : fold body starts collapsing
  // If the user manually toggles the fold during the settle window,
  // the auto-collapse is suppressed — they've expressed their own
  // intent for the fold state.
  useEffect(() => {
    if (turn.status === "in_progress") {
      setSettled(false);
      userToggledRef.current = false;
      return;
    }
    if (settled || userToggledRef.current) {
      return;
    }
    setSettled(true);
    const collapseTimer = window.setTimeout(() => {
      setExpanded(false);
    }, PROCESS_FOLD_SETTLING_DELAY_MS);
    return () => window.clearTimeout(collapseTimer);
  }, [turn.status, settled]);

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
      }, 440);
      previousExpanded.current = expanded;
      return () => window.clearTimeout(timeoutId);
    }
    previousExpanded.current = expanded;
    return undefined;
  }, [expanded, onCollapseComplete]);

  // User-initiated toggle: record the intent so the auto-collapse
  // step above doesn't fight the user. Without this, manually opening
  // a fold during the settle window would still get slammed shut by
  // the timer.
  const handleToggle = useCallback(() => {
    userToggledRef.current = true;
    if (!expanded) {
      onRequestLatest?.();
    }
    setExpanded((prev) => !prev);
  }, [expanded, onRequestLatest]);

  const hasDetails = entries.length > 0;
  const visiblePreview = expanded ? undefined : latestPreview;
  const hasPreview = Boolean(visiblePreview);

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
          className={`turn-process-preview turn-process-preview-${visiblePreview?.kind ?? "process"}${
            turn.status === "in_progress" ? " is-live" : ""
          }`}
        >
          <span className="turn-process-live-dot" aria-hidden />
          <LightweightStreamingText
            text={visiblePreview?.text ?? ""}
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
        onClick={handleToggle}
        onKeyDown={(event) => {
          if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            handleToggle();
          }
        }}
        className="turn-process-toggle"
      >
        {toggleContent}
      </div>
      {hasDetails || hasPreview ? (
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
                    onRequestLatest={onRequestLatest}
                    onNoticeAction={onNoticeAction}
                  />
                </div>
              ))}
            </div>
          ) : null}
        </CollapsibleDetails>
      ) : null}
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
  onRequestLatest,
  onNoticeAction,
}: {
  entry: TurnEntry;
  turn: Turn;
  cwd?: string;
  actionableAgentMessageID?: string;
  latestAgentMessageID?: string;
  onStreamFrame: () => void;
  onForkMessage?: (turnID: string, itemID: string) => void;
  onRequestLatest?: () => void;
  onNoticeAction: (action: UserFacingErrorAction) => void;
}): JSX.Element | null {
  const { item, kind, streaming } = entry;
  if (kind === "activity" || kind === "process_cluster") {
    return (
      <ProcessSurface
        processItems={entry.items ?? [item]}
        streaming={streaming}
        onRequestLatest={onRequestLatest}
        renderReasoningItem={(processItem, isStreaming) => (
          <ThreadItemView
            turnID={turn.id}
            turnStatus={turn.status}
            item={processItem}
            cwd={cwd}
            streaming={isStreaming}
            onStreamFrame={onStreamFrame}
            onNoticeAction={onNoticeAction}
          />
        )}
      />
    );
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
        onRequestLatest={onRequestLatest}
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
    return <ContextCompactionNotice text={item.text} status={item.status} />;
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

function ReasoningFold({
  item,
  streaming,
  turnID,
  turnStatus,
  cwd,
  onStreamFrame,
  onRequestLatest,
  onNoticeAction,
}: {
  item: ThreadItem;
  streaming: boolean;
  turnID: string;
  turnStatus: Turn["status"];
  cwd?: string;
  onStreamFrame: () => void;
  onRequestLatest?: () => void;
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
  const [open, setOpen] = useState(false);
  const reasoningScroll = useAutoFollowScrollContainer();

  const handleReasoningStreamFrame = useCallback((): void => {
    onStreamFrame();
    reasoningScroll.scheduleScrollToBottom();
  }, [onStreamFrame, reasoningScroll]);

  // When the user opens this fold, land at the latest reasoning. After
  // that, keep following only while the user stays near the bottom.
  const handleToggle = useCallback((event: SyntheticEvent<HTMLDetailsElement>) => {
    const details = event.currentTarget;
    const nextOpen = details.open;
    setOpen(nextOpen);
    if (!nextOpen) return;
    const body = details.querySelector(
      ".turn-reasoning-body",
    ) as HTMLElement | null;
    if (!body) return;
    onRequestLatest?.();
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
      reasoningScroll.scrollToBottom({ force: true, revealScrollbar: true });
    };
    body.addEventListener("transitionend", snapToBottom);
    // Fallback when transitionend never fires (reduced motion, or the
    // grid already settled before the listener attached).
    window.setTimeout(snapToBottom, 280);
  }, [onRequestLatest, reasoningScroll]);
  return (
    <details
      className="turn-reasoning-fold"
      open={open}
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
            ref={reasoningScroll.scrollRef}
            {...{ [AUTO_FOLLOW_NESTED_SCROLL_ATTR]: "true" }}
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
