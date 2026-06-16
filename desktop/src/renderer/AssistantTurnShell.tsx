import { Brain, ChevronDown, Info, MessageCircle } from "lucide-react";
import { Fragment, useEffect, useRef, useState } from "react";
import type { Turn } from "../shared/protocol";
import type {
  AssistantTurnDisplay,
  TurnProcessEntry,
  TurnProcessPreview,
} from "./AssistantTurnDisplay";
import { CollapsibleDetails } from "./CollapsibleMotion";
import { messageFlowStatusLabel } from "./message-flow-display";
import { parseTurnTimestampMs } from "./RunDebugPanel";
import { formatDuration, useLiveNow } from "./TurnProgress";
import {
  turnHasAssistantOutput,
  turnProgressContent,
} from "./TurnViewHelpers";

export function AssistantTurnShell({
  turn,
  display,
}: {
  turn: Turn;
  display: AssistantTurnDisplay;
}): JSX.Element {
  const hasFront =
    display.frontEntries.length > 0 || Boolean(display.latestProcessPreview);
  const hasBody = display.finalAnswerItems.length > 0;
  const className = [
    "assistant-turn-shell",
    hasFront ? " has-front" : "",
    hasBody ? " has-body" : "",
    display.missingReplyMessage ? " missing-reply-turn" : "",
  ]
    .filter(Boolean)
    .join("");

  return (
    <div className={className}>
      {hasFront ? (
        <div className="assistant-turn-front">
          <TurnProcessGroup
            turn={turn}
            entries={display.frontEntries}
            defaultCollapsed={display.frontDefaultCollapsed}
            showTurnStatus
            latestProcessPreview={display.latestProcessPreview}
          />
        </div>
      ) : null}
      {display.showDivider ? <div className="answer-divider" aria-hidden /> : null}
      <div className="assistant-turn-final">
        {display.finalAnswerItems.map((answer) => (
          <Fragment key={answer.item.id}>{answer.element}</Fragment>
        ))}
      </div>
      {display.missingReplyMessage ? (
        <aside
          className="turn-notice warning assistant-turn-missing-reply"
          role="status"
        >
          <span className="turn-notice-icon" aria-hidden="true">
            <Info size={14} />
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

function TurnProcessGroup({
  turn,
  entries,
  defaultCollapsed,
  showTurnStatus,
  latestProcessPreview,
}: {
  turn: Turn;
  entries: TurnProcessEntry[];
  defaultCollapsed: boolean;
  showTurnStatus: boolean;
  latestProcessPreview?: TurnProcessPreview;
}): JSX.Element {
  const [expanded, setExpanded] = useState(!defaultCollapsed);
  const previousDefaultCollapsed = useRef(defaultCollapsed);
  const detailsID = `${turn.id}-process-details`;
  const hasDetails = entries.length > 0;
  const hasPreview = Boolean(latestProcessPreview);
  const className = `turn-process-group${expanded ? " expanded" : " collapsed"}${
    hasDetails ? "" : " no-details"
  }${hasPreview ? " has-preview" : ""}`;
  const processCount = entries.reduce(
    (total, entry) => total + (entry.count ?? 1),
    0,
  );
  const completedDuration =
    typeof turn.duration_ms === "number" ? turn.duration_ms : undefined;
  const startedAt = parseTurnTimestampMs(turn.started_at);
  const liveDuration =
    showTurnStatus &&
    completedDuration === undefined &&
    turn.status === "in_progress" &&
    Number.isFinite(startedAt);
  const liveNow = useLiveNow(liveDuration);
  const elapsedMs =
    completedDuration ?? (liveDuration ? Math.max(0, liveNow - startedAt) : 0);
  const processLabel = showTurnStatus
    ? turnProgressContent(turn, elapsedMs, turnHasAssistantOutput(turn)).label
    : messageFlowStatusLabel({
        done: true,
        failed: turn.status === "failed",
        hasFinalText: turnHasAssistantOutput(turn),
        locale: "zh",
      });
  const metaParts = turnProcessMetaParts(
    turn,
    processCount,
    elapsedMs,
    showTurnStatus,
  );

  useEffect(() => {
    if (!previousDefaultCollapsed.current && defaultCollapsed) {
      setExpanded(false);
    }
    previousDefaultCollapsed.current = defaultCollapsed;
  }, [defaultCollapsed]);

  const toggleContent = (
    <>
      {/* Row 1 avatar slot: brain placeholder. Reserved for the
          future mascot character. */}
      <span className="turn-process-avatar" aria-hidden>
        <Brain size={15} />
      </span>
      {/* Row 2 avatar slot: speech-bubble placeholder. Only rendered
          when there's a live process preview, so a turn without
          live process text shows only the status row + brain slot. Both
          slots share col 1 of the grid below so the icons line up
          vertically with no indent between rows. */}
      {hasPreview ? (
        <span
          className="turn-process-avatar turn-process-avatar-secondary"
          aria-hidden
        >
          <MessageCircle size={15} />
        </span>
      ) : null}
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
          className={`turn-process-preview turn-process-preview-${latestProcessPreview?.kind ?? "process"}${
            turn.status === "in_progress" ? " is-live" : ""
          }`}
        >
          <span className="turn-process-preview-text">
            {latestProcessPreview?.text}
          </span>
        </span>
      ) : null}
      {hasDetails ? (
        <ChevronDown className="turn-process-chevron" size={15} />
      ) : null}
    </>
  );

  return (
    <div className={className}>
      {hasDetails ? (
        <button
          className="turn-process-toggle"
          type="button"
          aria-expanded={expanded}
          aria-controls={detailsID}
          onClick={() => setExpanded((open) => !open)}
        >
          {toggleContent}
        </button>
      ) : (
        <div className="turn-process-toggle turn-process-toggle-static">
          {toggleContent}
        </div>
      )}
      {hasDetails ? (
        <CollapsibleDetails
          className="turn-process-details"
          id={detailsID}
          expanded={expanded}
          innerClassName="turn-process-stack"
        >
          {entries.map((entry) => (
            <div
              className={`turn-process-entry turn-process-entry-${entry.kind}`}
              key={entry.key}
            >
              {entry.element}
            </div>
          ))}
        </CollapsibleDetails>
      ) : null}
    </div>
  );
}

function turnProcessMetaParts(
  turn: Turn,
  processCount: number,
  elapsedMs: number,
  showTurnStatus: boolean,
): string[] {
  const parts: string[] = [];
  if (showTurnStatus && turn.status === "in_progress") {
    parts.push(formatDuration(elapsedMs));
    return parts;
  }
  if (processCount > 0) {
    parts.push(`${processCount} 项`);
  }
  if (showTurnStatus && typeof turn.duration_ms === "number") {
    parts.push(formatDuration(turn.duration_ms));
  }
  return parts;
}
