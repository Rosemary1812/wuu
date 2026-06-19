/// <reference path="../shared/jsx-compat.d.ts" />

import type { ThreadItem, Turn } from "../shared/protocol";
import { buildAssistantTurnDisplay } from "./AssistantTurnDisplay";
import { AssistantTurnShell } from "./AssistantTurnShell";
import { ThreadItemView } from "./ThreadItemView";
import { TurnNotice, turnNoticeDisplay } from "./TurnNotice";
import {
  latestAgentMessageItemID,
  messageFlowAgentMessageItemID,
  scrollToUserMessage,
  turnAnchorID,
  turnHasAssistantOutput,
} from "./TurnViewHelpers";
import type { UserFacingErrorAction } from "./UserFacingErrors";

export { latestAgentMessageItemID, scrollToUserMessage };

export function TurnView({
  turn,
  cwd,
  latestAgentMessageID,
  onStreamFrame,
  onForkMessage,
  onEditMessage,
  editingMessage,
  onCancelEditMessage,
  onSubmitEditMessage,
  onNoticeAction,
}: {
  turn: Turn;
  cwd?: string;
  latestAgentMessageID?: string;
  onStreamFrame: () => void;
  onForkMessage?: (turnID: string, itemID: string) => void;
  onEditMessage?: (turnID: string, item: ThreadItem) => void;
  editingMessage?: { turnID: string; itemID: string; submitting: boolean };
  onCancelEditMessage?: () => void;
  onSubmitEditMessage?: (turnID: string, item: ThreadItem, text: string) => void;
  onNoticeAction: (action: UserFacingErrorAction) => void;
}): JSX.Element {
  const actionableAgentMessageID =
    turn.status === "completed"
      ? messageFlowAgentMessageItemID(turn)
      : undefined;

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
        streaming={streaming}
        pendingCompanionReasoning={pendingCompanionReasoning}
        actionableAgentMessageID={actionableAgentMessageID}
        latestAgentMessageID={latestAgentMessageID}
        onStreamFrame={onStreamFrame}
        onForkMessage={onForkMessage}
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
        onNoticeAction={onNoticeAction}
      />
    );
  }

  const userItems = turn.items.filter((item) => item.type === "user_message");
  const assistantDisplay = buildAssistantTurnDisplay(
    turn,
    actionableAgentMessageID,
    renderThreadItem,
  );
  const notice = turnNoticeDisplay(turn, turnHasAssistantOutput(turn));

  return (
    <section className="turn" id={turnAnchorID(turn.id)} data-turn-id={turn.id}>
      {userItems.map((item) => renderThreadItem(item, false))}
      {assistantDisplay ? (
        <AssistantTurnShell
          turn={turn}
          display={assistantDisplay}
          cwd={cwd}
          actionableAgentMessageID={actionableAgentMessageID}
          latestAgentMessageID={latestAgentMessageID}
          onStreamFrame={onStreamFrame}
          onForkMessage={onForkMessage}
          onNoticeAction={onNoticeAction}
        />
      ) : null}
      {notice ? <TurnNotice display={notice} onAction={onNoticeAction} /> : null}
    </section>
  );
}
