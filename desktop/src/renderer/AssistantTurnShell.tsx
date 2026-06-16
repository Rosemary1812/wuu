import { Info, Play } from "lucide-react";
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
      {/* Text-first status row. The previous Brain / MessageCircle
          avatar slots and ChevronDown are gone — the assistant flow
          no longer carries a "mascot" placeholder or a fold
          affordance. Status lives on its own line, prefixed with a
          play-triangle glyph (▶) for the visual cue that this row
          describes a tool run / live command, matching the prototype.
      */}
      <Play
        className="turn-process-glyph"
        size={12}
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
          className={`turn-process-preview turn-process-preview-${latestProcessPreview?.kind ?? "process"}${
            turn.status === "in_progress" ? " is-live" : ""
          }`}
        >
          <span className="turn-process-live-dot" aria-hidden />
          <span className="turn-process-preview-text">
            {latestProcessPreview?.text}
          </span>
        </span>
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
