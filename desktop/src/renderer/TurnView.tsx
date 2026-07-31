/// <reference path="../shared/jsx-compat.d.ts" />

import type {
  InputFile,
  InputImage,
  ThreadItem,
  Turn,
} from "../shared/protocol";
import { isAgentHandoffItem } from "./AgentHandoff";
import { buildAssistantTurnDisplay } from "./AssistantTurnDisplay";
import { useAssistantTurnPresentation } from "./AssistantTurnPresentation";
import { AssistantTurnShell } from "./AssistantTurnShell";
import { ThreadItemView } from "./ThreadItemView";
import { TurnEditSummaryCard } from "./TurnEditSummaryCard";
import type { TurnFileDiffSelection } from "./TurnFileDiffTypes";
import { TurnEventNotice, StreamReconnectNotice } from "./TurnNotice";
import { turnEventForTurn } from "./TurnEvents";
import type { TurnStreamStatus } from "./AppState";
import {
  latestAgentMessageItemID,
  messageFlowAgentMessageItemID,
  scrollToUserMessage,
  turnAnchorID,
  turnHasAssistantOutput,
} from "./TurnViewHelpers";

export { latestAgentMessageItemID, scrollToUserMessage };

export function TurnView({
  turn,
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
}: {
  turn: Turn;
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
  onOpenRuns?: () => void;
  streamStatus?: TurnStreamStatus;
  isLatestTurn?: boolean;
}): JSX.Element {
  const actionableAgentMessageID =
    turn.status === "completed"
      ? messageFlowAgentMessageItemID(turn)
      : undefined;
  // The edit summary card should sit inside the actionable answer message
  // (between its text and its action bar) when that message actually renders
  // an action bar. Otherwise it falls back to its turn-level slot below the
  // assistant shell.
  const actionableAnswerItem = actionableAgentMessageID
    ? turn.items.find(
        (item) =>
          item.id === actionableAgentMessageID &&
          item.type === "agent_message" &&
          item.phase !== "commentary" &&
          item.text?.trim(),
      )
    : undefined;
  const runActionAttachedToMessage = actionableAnswerItem != null;

  function renderThreadItem(
    item: ThreadItem,
    streaming: boolean,
    pendingCompanionReasoning?: boolean,
  ): JSX.Element | null {
    return (
      <ThreadItemView
        key={item.id}
        turnID={turn.id}
        turnStatus={turn.status}
        item={item}
        cwd={cwd}
        onOpenFile={onOpenFile}
        streaming={streaming}
        pendingCompanionReasoning={pendingCompanionReasoning}
        actionableAgentMessageID={actionableAgentMessageID}
        latestAgentMessageID={latestAgentMessageID}
        onStreamFrame={onStreamFrame}
        onForkMessage={onForkMessage}
        onOpenRuns={onOpenRuns}
        onEditMessage={onEditMessage}
        editing={
          editingMessage?.turnID === turn.id && editingMessage.itemID === item.id
        }
        editSubmitting={
          editingMessage?.turnID === turn.id && editingMessage.itemID === item.id
            ? editingMessage.submitting
            : false
        }
        onCancelEditMessage={onCancelEditMessage}
        onSubmitEditMessage={onSubmitEditMessage}
        onOpenAgent={onOpenAgent}
        editSummaryCard={
          runActionAttachedToMessage && item.id === actionableAgentMessageID ? (
            <TurnEditSummaryCard
              turn={turn}
              cwd={cwd}
              onOpenFile={onOpenFile}
              onOpenFileDiff={onOpenFileDiff}
            />
          ) : undefined
        }
      />
    );
  }

  const userItems = turn.items.filter(
    (item) => item.type === "user_message" && !isAgentHandoffItem(item),
  );
  const rawAssistantDisplay = buildAssistantTurnDisplay(
    turn,
    actionableAgentMessageID,
    renderThreadItem,
  );
  const assistantDisplay = useAssistantTurnPresentation(
    turn.id,
    rawAssistantDisplay,
  );
  // `buildAssistantTurnDisplay` already classifies "turn completed but
  // only commentary, no `final_answer`" and surfaces it as
  // `missingReplyMessage`. Forward that to the event pipeline so the
  // chip shows up in the same place as cancelled / failed notices,
  // instead of being hand-rolled inside the assistant turn shell.
  const hasMissingReply = assistantDisplay?.missingReplyMessage !== undefined;
  const event = turnEventForTurn(
    turn,
    turnHasAssistantOutput(turn),
    hasMissingReply,
  );

  return (
    <section
      className="turn"
      id={turnAnchorID(turn.id)}
      data-turn-id={turn.id}
      data-turn-status={turn.status}
    >
      {userItems.map((item) => renderThreadItem(item, false))}
      {assistantDisplay ? (
        <AssistantTurnShell
          turn={turn}
          display={assistantDisplay}
          cwd={cwd}
          onOpenFile={onOpenFile}
          actionableAgentMessageID={actionableAgentMessageID}
          latestAgentMessageID={latestAgentMessageID}
          onStreamFrame={onStreamFrame}
          onForkMessage={onForkMessage}
          onOpenRuns={onOpenRuns}
          onCollapseComplete={onCollapseComplete}
          onOpenAgent={onOpenAgent}
        />
      ) : null}
      {!runActionAttachedToMessage ? (
        <TurnEditSummaryCard
          turn={turn}
          cwd={cwd}
          onOpenFile={onOpenFile}
          onOpenFileDiff={onOpenFileDiff}
        />
      ) : null}
      {isLatestTurn && turn.status === "in_progress" && streamStatus?.liveProgress ? (
        <StreamReconnectNotice text={streamStatus.text} />
      ) : null}
      {event ? <TurnEventNotice event={event} /> : null}
    </section>
  );
}
