import { memo } from "react";
import type {
  Agent,
  ConversationSubthread,
  InputFile,
  InputImage,
  MessageMarkWire,
  Thread,
  ThreadItem,
} from "../shared/protocol";
import {
  chatReaderCountForThread,
  isDMThread,
  isGroupThread,
  type TurnStreamStatus,
} from "./AppState";
import { ChatThreadViewContainer } from "./ChatThreadViewContainer";
import { ConversationTurnList } from "./ConversationTurnList";
import {
  ContextCompositionCard,
  type ContextCompositionEntry,
} from "./ContextCompositionCard";
import { ForkWorktreeNotice } from "./ForkWorktreeNotice";
import {
  InstructionFilesCard,
  type InstructionFilesEntry,
} from "./InstructionFilesCard";
import {
  pendingComposerMessagesForThread as pendingComposerMessagesForThreadSnapshot,
  type PendingComposerMessagesByThread,
} from "./ComposerPendingMessages";
import {
  TurnView,
  latestAgentMessageItemID,
} from "./TurnView";
import type { TurnFileDiffSelection } from "./TurnFileDiffTypes";
import type { HistoryMessageEditState } from "./ConversationHistoryActions";

const CONVERSATION_GRID_COLUMNS = 12;

export type CachedConversationPanesProps = {
  threadIDs: string[];
  threadsByID: ReadonlyMap<string, Thread>;
  activeThreadID?: string;
  activeContextCwd?: string;
  conversationGridVisible: boolean;
  contextCompositionEntries: ContextCompositionEntry[];
  instructionFilesEntries: InstructionFilesEntry[];
  historyMessageEdit?: HistoryMessageEditState;
  onStreamFrame: () => void;
  onCollapseComplete: () => void;
  onDismissContextComposition: (id: string) => void;
  onDismissInstructions: (id: string) => void;
  canEditThreadMessage: (thread: Thread) => boolean;
  onForkMessage: (thread: Thread, turnID: string, itemID: string) => void;
  onOpenFile?: (thread: Thread, path: string) => void;
  onOpenAgent: (agent: Agent) => void;
  onOpenSubthread: (
    thread: Thread,
    item: ThreadItem,
    threadOwnerParticipantID?: string,
    existingSubthreadID?: string,
  ) => void;
  onReact: (thread: Thread, item: ThreadItem, reaction: string) => void;
  onEditMessage: (thread: Thread, turnID: string, item: ThreadItem) => void;
  onCancelEditMessage: () => void;
  onSubmitEditMessage: (
    thread: Thread,
    turnID: string,
    item: ThreadItem,
    text: string,
    images: InputImage[],
    files: InputFile[],
  ) => void;
  onOpenFileDiff: (thread: Thread, selection: TurnFileDiffSelection) => void;
  turnStreamStatus: Record<string, TurnStreamStatus>;
  busyParticipantIDs: ReadonlySet<string>;
  activeThreadMarks: readonly MessageMarkWire[];
  resolveParticipantName: (id: string) => string;
  chatReaderCount: number;
  subthreadsByAnchor?: ReadonlyMap<string, ConversationSubthread>;
  subthreadsThreadID?: string;
  pendingChatMessagesByThread: PendingComposerMessagesByThread;
};

export const CachedConversationPanes = memo(function CachedConversationPanes({
  threadIDs,
  threadsByID,
  activeThreadID,
  activeContextCwd,
  conversationGridVisible,
  contextCompositionEntries,
  instructionFilesEntries,
  historyMessageEdit,
  onStreamFrame,
  onCollapseComplete,
  onDismissContextComposition,
  onDismissInstructions,
  canEditThreadMessage,
  onForkMessage,
  onOpenFile,
  onOpenAgent,
  onOpenSubthread,
  onReact,
  onEditMessage,
  onCancelEditMessage,
  onSubmitEditMessage,
  onOpenFileDiff,
  turnStreamStatus,
  busyParticipantIDs,
  activeThreadMarks,
  resolveParticipantName,
  chatReaderCount,
  subthreadsByAnchor,
  subthreadsThreadID,
  pendingChatMessagesByThread,
}: CachedConversationPanesProps): JSX.Element {
  return (
    <div className="cached-conversation-panes">
      {threadIDs.map((threadID) => {
        const thread = threadsByID.get(threadID);
        if (!thread) return null;
        const isActive = threadID === activeThreadID;
        const threadTurns = thread.turns ?? [];
        const threadLatestAgentMessageID = latestAgentMessageItemID(threadTurns);
        const threadContextEntries = contextCompositionEntries.filter(
          (entry) => entry.threadID === threadID,
        );
        const turnIDs = new Set(threadTurns.map((turn) => turn.id));
        const entriesBeforeTurns = threadContextEntries.filter(
          (entry) => !entry.afterTurnID,
        );
        const entriesAfterMissingTurn = threadContextEntries.filter(
          (entry) => entry.afterTurnID && !turnIDs.has(entry.afterTurnID),
        );
        const entriesByAfterTurnID = new Map<string, ContextCompositionEntry[]>();
        for (const entry of threadContextEntries) {
          if (!entry.afterTurnID || !turnIDs.has(entry.afterTurnID)) {
            continue;
          }
          const existing = entriesByAfterTurnID.get(entry.afterTurnID) ?? [];
          existing.push(entry);
          entriesByAfterTurnID.set(entry.afterTurnID, existing);
        }
        const renderContextEntry = (entry: ContextCompositionEntry) => (
          <ContextCompositionCard
            entry={entry}
            key={entry.id}
            onDismiss={onDismissContextComposition}
          />
        );
        const threadInstructionCards = instructionFilesEntries
          .filter((entry) => entry.threadID === threadID)
          .map((entry) => (
            <InstructionFilesCard
              entry={entry}
              key={entry.id}
              onDismiss={onDismissInstructions}
            />
          ));
        const forkWorktreeNotice =
          thread.worktree && thread.forked_from_id ? (
            <ForkWorktreeNotice thread={thread} />
          ) : null;
        const isGroupChat = isGroupThread(thread);
        const isChatStyleThread = isDMThread(thread) || isGroupChat;
        return (
          <div
            key={threadID}
            className="cached-conversation-pane"
            data-active={isActive}
            style={isActive ? undefined : { display: "none" }}
          >
            <div className="conversation-width session-flow">
              {isActive && conversationGridVisible ? (
                <ConversationGridGuides />
              ) : null}
              {isChatStyleThread ? (
                <ChatThreadViewContainer
                  key={threadID}
                  threadID={threadID}
                  turns={threadTurns}
                  cwd={thread.cwd ?? activeContextCwd}
                  marks={isActive ? activeThreadMarks : undefined}
                  busyParticipantIDs={busyParticipantIDs}
                  resolveParticipantName={resolveParticipantName}
                  readerCount={chatReaderCountForThread(thread, chatReaderCount)}
                  threadOwnerCandidates={isGroupChat ? thread.members : undefined}
                  subthreadsByAnchor={
                    isGroupChat && threadID === subthreadsThreadID
                      ? subthreadsByAnchor
                      : undefined
                  }
                  onOpenSubthread={
                    isGroupChat
                      ? (item, ownerID, existingSubthreadID) =>
                          onOpenSubthread(
                            thread,
                            item,
                            ownerID,
                            existingSubthreadID,
                          )
                      : undefined
                  }
                  onReact={(item, reaction) => onReact(thread, item, reaction)}
                  pendingMessages={
                    pendingComposerMessagesForThreadSnapshot(
                      pendingChatMessagesByThread,
                      threadID,
                    ).queued
                  }
                />
              ) : (
                <ConversationTurnList
                  threadID={thread.id}
                  turns={threadTurns}
                  renderBeforeTurns={[
                    ...threadInstructionCards,
                    ...entriesBeforeTurns.map(renderContextEntry),
                  ]}
                  renderAfterMissingTurn={
                    <>
                      {entriesAfterMissingTurn.map(renderContextEntry)}
                      {forkWorktreeNotice}
                    </>
                  }
                  renderAfterTurn={(turn) =>
                    (entriesByAfterTurnID.get(turn.id) ?? []).map(
                      renderContextEntry,
                    )
                  }
                  forcedFullTurnIDs={
                    historyMessageEdit?.threadID === thread.id
                      ? [historyMessageEdit.turnID]
                      : undefined
                  }
                  renderTurn={(turn) => (
                    <TurnView
                      turn={turn}
                      cwd={thread.cwd ?? activeContextCwd}
                      onOpenFile={(path) => onOpenFile?.(thread, path)}
                      onOpenAgent={(agentID) => {
                        const agent = thread.child_agents?.find(
                          (candidate) => candidate.id === agentID,
                        );
                        if (agent) {
                          void onOpenAgent(agent);
                        }
                      }}
                      latestAgentMessageID={threadLatestAgentMessageID}
                      isLatestTurn={
                        thread.turns[thread.turns.length - 1]?.id === turn.id
                      }
                      onStreamFrame={onStreamFrame}
                      onCollapseComplete={onCollapseComplete}
                      onForkMessage={(turnID, itemID) =>
                        onForkMessage(thread, turnID, itemID)
                      }
                      onEditMessage={
                        canEditThreadMessage(thread)
                          ? (turnID, item) => onEditMessage(thread, turnID, item)
                          : undefined
                      }
                      editingMessage={
                        historyMessageEdit?.threadID === thread.id
                          ? historyMessageEdit
                          : undefined
                      }
                      onCancelEditMessage={onCancelEditMessage}
                      onSubmitEditMessage={(
                        turnID,
                        item,
                        text,
                        images,
                        files,
                      ) =>
                        onSubmitEditMessage(
                          thread,
                          turnID,
                          item,
                          text,
                          images,
                          files,
                        )
                      }
                      onOpenFileDiff={(selection) =>
                        onOpenFileDiff(thread, selection)
                      }
                      streamStatus={
                        thread.turns[thread.turns.length - 1]?.id === turn.id
                          ? turnStreamStatus[turn.id]
                          : undefined
                      }
                    />
                  )}
                />
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
});

function ConversationGridGuides(): JSX.Element {
  return (
    <div className="conversation-grid-guides" aria-hidden="true">
      <div className="conversation-grid-cols">
        {Array.from({ length: CONVERSATION_GRID_COLUMNS }, (_, index) => (
          <div className="conversation-grid-col" key={index}>
            <span>{index + 1}</span>
          </div>
        ))}
      </div>
      <div className="conversation-grid-rows" />
    </div>
  );
}
