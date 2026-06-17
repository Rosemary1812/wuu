import { Info, Play } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import type { Turn } from "../shared/protocol";
import type {
  AssistantTurnDisplay,
  TurnEntry,
  TurnProcessPreview,
} from "./AssistantTurnDisplay";
import { ToolActivityTimeline } from "./ToolActivity";
import { ThreadItemView } from "./ThreadItemView";
import { ContextCompactionNotice, TurnNotice } from "./TurnNotice";
import { parseTurnTimestampMs } from "./RunDebugPanel";
import { formatDuration, useLiveNow } from "./TurnProgress";
import {
  turnHasAssistantOutput,
  turnProgressContent,
} from "./TurnViewHelpers";
import type { UserFacingErrorAction } from "./UserFacingErrors";
import { userFacingErrorForMessage } from "./UserFacingErrors";

export function AssistantTurnShell({
  turn,
  display,
  cwd,
  actionableAgentMessageID,
  latestAgentMessageID,
  onStreamFrame,
  onForkMessage,
  onNoticeAction,
}: {
  turn: Turn;
  display: AssistantTurnDisplay;
  cwd?: string;
  actionableAgentMessageID?: string;
  latestAgentMessageID?: string;
  onStreamFrame: () => void;
  onForkMessage?: (turnID: string, itemID: string) => void;
  onNoticeAction: (action: UserFacingErrorAction) => void;
}): JSX.Element {
  const processEntries = display.entries.filter(
    (entry) => entry.position === "process",
  );
  const answerEntries = display.entries.filter(
    (entry) => entry.position === "answer",
  );

  // The single rule for the default collapse state: if every process
  // entry has settled AND the turn has at least one answer body, the
  // process fold can stay collapsed — the user has what they came for.
  const allProcessSettled =
    processEntries.length > 0 && processEntries.every((e) => e.settled);
  const defaultCollapsed = allProcessSettled && answerEntries.length > 0;

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
  onNoticeAction: (action: UserFacingErrorAction) => void;
}): JSX.Element {
  const [expanded, setExpanded] = useState(!defaultCollapsed);
  const previousDefaultCollapsed = useRef(defaultCollapsed);
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

  const hasDetails = entries.length > 0;
  const hasPreview = Boolean(latestPreview);

  const toggleContent = (
    <>
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
          className={`turn-process-preview turn-process-preview-${latestPreview?.kind ?? "process"}${
            turn.status === "in_progress" ? " is-live" : ""
          }`}
        >
          <span className="turn-process-live-dot" aria-hidden />
          <span className="turn-process-preview-text">{latestPreview?.text}</span>
        </span>
      ) : null}
    </>
  );

  return (
    <details
      open={expanded}
      className={`turn-process-fold${expanded ? " expanded" : " collapsed"}${
        hasDetails ? "" : " no-details"
      }${hasPreview ? " has-preview" : ""}`}
      id={detailsID}
      onToggle={(event) => setExpanded(event.currentTarget.open)}
    >
      <summary className="turn-process-toggle">{toggleContent}</summary>
      {hasDetails ? (
        <div className="turn-process-fold-body">
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
    </details>
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
  if (kind === "activity") {
    if (item.type === "tool_call" || item.type === "collab_agent_tool_call") {
      return (
        <ToolActivityTimeline items={[item]} revealItems={streaming} />
      );
    }
    return null;
  }
  if (item.type === "agent_message" || item.type === "reasoning") {
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