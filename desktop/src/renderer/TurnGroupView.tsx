/// <reference path="../shared/jsx-compat.d.ts" />

import { Fragment, useMemo } from "react";
import type {
  InputFile,
  InputImage,
  ThreadItem,
  Turn,
} from "../shared/protocol";
import {
  buildAssistantTurnDisplay,
  type AssistantTurnDisplay,
  type TurnEntry,
  type TurnProcessPreview,
} from "./AssistantTurnDisplay";
import { useAssistantTurnPresentation } from "./AssistantTurnPresentation";
import { AssistantTurnShell } from "./AssistantTurnShell";
import { TurnRunActions } from "./MessageActions";
import { isCommandToolCall } from "./TerminalRuns";
import { ThreadItemView } from "./ThreadItemView";
import { TurnEditSummaryCard } from "./TurnEditSummaryCard";
import type { TurnFileDiffSelection } from "./TurnFileDiffTypes";
import { TurnEventNotice, StreamReconnectNotice } from "./TurnNotice";
import { turnEventForTurn } from "./TurnEvents";
import { isAgentHandoffItem } from "./AgentHandoff";
import { parseTurnTimestampMs } from "./RunDebugPanel";
import { turnHasSpawnAgentCall } from "./TurnGrouping";
import {
  messageFlowAgentMessageItemID,
  turnAnchorID,
  turnHasAssistantOutput,
} from "./TurnViewHelpers";
import { TurnView } from "./TurnView";
import type { TurnStreamStatus } from "./AppState";
import type { SubagentChipDisplay } from "./AgentHandoff";

export type TurnGroupViewProps = {
  /** One orchestration group (TurnGrouping). Length 1 for ordinary turns. */
  turns: Turn[];
  /** The thread still has running child agents and this is the last group:
   *  the orchestration is between turns, waiting for a completion wake. */
  awaiting?: boolean;
  cwd?: string;
  onOpenFile?: (path: string) => void;
  onOpenAgent?: (agentID: string) => void;
  latestAgentMessageID?: string;
  onStreamFrame: () => void;
  onForkMessage?: (turnID: string, itemID: string) => void;
  onEditMessage?: (turnID: string, item: ThreadItem) => void;
  editingMessage?: { turnID: string; itemID: string; submitting: boolean };
  onCancelEditMessage?: () => void;
  onSubmitEditMessage?: (
    turnID: string,
    item: ThreadItem,
    text: string,
    images: InputImage[],
    files: InputFile[],
  ) => void;
  onCollapseComplete?: () => void;
  onOpenFileDiff?: (selection: TurnFileDiffSelection) => void;
  onOpenRuns?: (turnID: string) => void;
  streamStatus?: TurnStreamStatus;
  isLatestTurn?: boolean;
};

export function TurnGroupView(props: TurnGroupViewProps): JSX.Element {
  const { turns, awaiting } = props;
  const first = turns[0];
  // A single-turn group with no live orchestration renders through the
  // ordinary per-turn path untouched; everything else (merged groups, and
  // the waiting-between-turns state) renders one shell for the whole run.
  const orchestrationLive =
    Boolean(awaiting) && turns.some(turnHasSpawnAgentCall);
  if (turns.length === 1 && !orchestrationLive && first) {
    const turn = first;
    return (
      <TurnView
        turn={turn}
        cwd={props.cwd}
        onOpenFile={props.onOpenFile}
        onOpenAgent={props.onOpenAgent}
        latestAgentMessageID={props.latestAgentMessageID}
        onStreamFrame={props.onStreamFrame}
        onForkMessage={props.onForkMessage}
        onEditMessage={props.onEditMessage}
        editingMessage={props.editingMessage}
        onCancelEditMessage={props.onCancelEditMessage}
        onSubmitEditMessage={props.onSubmitEditMessage}
        onCollapseComplete={props.onCollapseComplete}
        onOpenFileDiff={props.onOpenFileDiff}
        onOpenRuns={
          props.onOpenRuns ? () => props.onOpenRuns?.(turn.id) : undefined
        }
        streamStatus={props.streamStatus}
        isLatestTurn={props.isLatestTurn}
      />
    );
  }
  return <MergedTurnGroupView {...props} />;
}

function MergedTurnGroupView({
  turns,
  awaiting,
  cwd,
  onOpenFile,
  onOpenAgent,
  latestAgentMessageID,
  onStreamFrame,
  onForkMessage,
  onEditMessage,
  editingMessage,
  onCancelEditMessage,
  onSubmitEditMessage,
  onCollapseComplete,
  onOpenFileDiff,
  onOpenRuns,
  streamStatus,
  isLatestTurn,
}: TurnGroupViewProps): JSX.Element {
  const first = turns[0];
  const last = turns[turns.length - 1];
  const anyMemberInProgress = turns.some(
    (turn) => turn.status === "in_progress",
  );
  // The group reads as one live turn while any member runs OR while the
  // orchestration is parked between turns waiting for a completion wake.
  const live = anyMemberInProgress || Boolean(awaiting);
  const closed = !live && last.status === "completed";
  // Only the group's final answer carries the action bar; intermediate
  // answers (the wake-up progress reports) render as plain text.
  const actionableAgentMessageID = closed
    ? messageFlowAgentMessageItemID(last)
    : undefined;

  // The shell sees a synthetic turn: identity of the first, items of all
  // (turnProgressContent reads running tools / latest item), status and
  // timing of the whole run. Per-item rendering still resolves each
  // entry's origin turn via TurnEntry.turn.
  const shellTurn = useMemo<Turn>(() => {
    let durationMs: number | undefined;
    if (!live) {
      const startMs = parseTurnTimestampMs(first.started_at);
      const endMs = parseTurnTimestampMs(last.completed_at);
      if (Number.isFinite(startMs) && Number.isFinite(endMs) && endMs >= startMs) {
        durationMs = endMs - startMs;
      } else {
        const sum = turns.reduce(
          (acc, turn) =>
            acc + (typeof turn.duration_ms === "number" ? turn.duration_ms : 0),
          0,
        );
        durationMs = sum > 0 ? sum : undefined;
      }
    }
    return {
      ...first,
      items: turns.flatMap((turn) => turn.items),
      status: live ? "in_progress" : last.status,
      started_at: first.started_at,
      completed_at: live ? undefined : last.completed_at,
      duration_ms: durationMs,
      error: live ? undefined : last.error,
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [turns, live]);

  const display = useMemo(() => mergeTurnDisplays(turns), [turns]);
  const presented = useAssistantTurnPresentation(shellTurn.id, display);

  const hasMissingReply =
    closed && presented?.missingReplyMessage !== undefined;

  return (
    <section
      className="turn"
      id={turnAnchorID(first.id)}
      data-turn-id={first.id}
      data-turn-status={shellTurn.status}
    >
      {turns.map((member) =>
        member.items
          .filter(
            (item) => item.type === "user_message" && !isAgentHandoffItem(item),
          )
          .map((item) => (
            <ThreadItemView
              key={item.id}
              turnID={member.id}
              turnStatus={member.status}
              item={item}
              cwd={cwd}
              onOpenFile={onOpenFile}
              streaming={false}
              onStreamFrame={onStreamFrame}
              onEditMessage={onEditMessage}
              editing={
                editingMessage?.turnID === member.id &&
                editingMessage.itemID === item.id
              }
              editSubmitting={
                editingMessage?.turnID === member.id &&
                editingMessage.itemID === item.id
                  ? editingMessage.submitting
                  : false
              }
              onCancelEditMessage={onCancelEditMessage}
              onSubmitEditMessage={onSubmitEditMessage}
              onOpenAgent={onOpenAgent}
            />
          )),
      )}
      {presented ? (
        <AssistantTurnShell
          turn={shellTurn}
          display={presented}
          cwd={cwd}
          onOpenFile={onOpenFile}
          onOpenAgent={onOpenAgent}
          actionableAgentMessageID={actionableAgentMessageID}
          latestAgentMessageID={latestAgentMessageID}
          onStreamFrame={onStreamFrame}
          onForkMessage={onForkMessage}
          onOpenRuns={
            onOpenRuns ? () => onOpenRuns(last.id) : undefined
          }
          onCollapseComplete={onCollapseComplete}
          // While the orchestration is parked between turns, the answer
          // handoff must not collapse the process fold: the shimmering
          // spawn rows are exactly what tells the user the subagents are
          // still running.
          suppressAnswerHandoff={Boolean(awaiting) && !anyMemberInProgress}
        />
      ) : null}
      {turns.map((member) => {
        const hasTurnRuns =
          (member.status === "completed" || member.status === "failed") &&
          member.items.some(isCommandToolCall);
        const event = turnEventForTurn(
          member,
          turnHasAssistantOutput(member),
          member.id === last.id ? hasMissingReply : false,
        );
        return (
          <Fragment key={member.id}>
            {hasTurnRuns && onOpenRuns ? (
              <TurnRunActions onOpenRuns={() => onOpenRuns(member.id)} />
            ) : null}
            <TurnEditSummaryCard
              turn={member}
              cwd={cwd}
              onOpenFile={onOpenFile}
              onOpenFileDiff={onOpenFileDiff}
            />
            {event ? <TurnEventNotice event={event} /> : null}
          </Fragment>
        );
      })}
      {isLatestTurn &&
      last.status === "in_progress" &&
      streamStatus?.liveProgress ? (
        <StreamReconnectNotice text={streamStatus.text} />
      ) : null}
    </section>
  );
}

function mergeTurnDisplays(turns: Turn[]): AssistantTurnDisplay | undefined {
  const entries: TurnEntry[] = [];
  const chips: SubagentChipDisplay[] = [];
  let hasAnswer = false;
  let latestProcessPreview: TurnProcessPreview | undefined;
  let lastDisplay: AssistantTurnDisplay | undefined;
  for (const turn of turns) {
    const display = buildAssistantTurnDisplay(turn, undefined);
    if (!display) continue;
    lastDisplay = display;
    for (const entry of display.entries) {
      entries.push({ ...entry, turn });
    }
    chips.push(...display.subagentChips);
    hasAnswer = hasAnswer || display.hasAnswer;
    if (display.latestProcessPreview) {
      latestProcessPreview = display.latestProcessPreview;
    }
  }
  if (!lastDisplay) {
    return undefined;
  }
  return {
    entries,
    hasAnswer,
    subagentChips: chips,
    // Only the last member's missing-reply outcome matters: an
    // intermediate wake turn that produced no final answer simply
    // continued the orchestration.
    missingReplyMessage: lastDisplay.missingReplyMessage,
    latestProcessPreview,
  };
}
