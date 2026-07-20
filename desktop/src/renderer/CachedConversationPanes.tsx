import { memo, useCallback, useLayoutEffect, useRef, useState } from "react";
import type {
  Agent,
  InputFile,
  InputImage,
  Thread,
  ThreadItem,
  Turn,
} from "../shared/protocol";
import type { TurnStreamStatus } from "./AppState";
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
import { OPTIMISTIC_TURN_ID_PREFIX } from "./ComposerMessages";
import { TurnView } from "./TurnView";
import type { TurnFileDiffSelection } from "./TurnFileDiffTypes";
import { latestAgentMessageLocation } from "./TurnViewHelpers";
import type { HistoryMessageEditState } from "./ConversationHistoryActions";

const CONVERSATION_GRID_COLUMNS = 12;
const CONVERSATION_LAYOUT_STABLE_FRAMES = 2;
const CONVERSATION_LAYOUT_SETTLE_TIMEOUT_MS = 120;

function sameEntriesByIdentity<T>(previous: readonly T[], next: readonly T[]): boolean {
  return (
    previous.length === next.length &&
    previous.every((entry, index) => entry === next[index])
  );
}

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
  onOpenTurnRuns?: (thread: Thread, turnID: string) => void;
  turnStreamStatus: Record<string, TurnStreamStatus>;
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
  onEditMessage,
  onCancelEditMessage,
  onSubmitEditMessage,
  onOpenFileDiff,
  onOpenTurnRuns,
  turnStreamStatus,
}: CachedConversationPanesProps): JSX.Element {
  return (
    <div className="cached-conversation-panes">
      {threadIDs.map((threadID) => {
        const thread = threadsByID.get(threadID);
        if (!thread) return null;
        const isActive = threadID === activeThreadID;
        return (
          <CachedConversationPane
            key={threadID}
            thread={thread}
            isActive={isActive}
            activeContextCwd={activeContextCwd}
            conversationGridVisible={conversationGridVisible}
            contextCompositionEntries={contextCompositionEntries}
            instructionFilesEntries={instructionFilesEntries}
            historyMessageEdit={
              historyMessageEdit?.threadID === threadID
                ? historyMessageEdit
                : undefined
            }
            onStreamFrame={onStreamFrame}
            onCollapseComplete={onCollapseComplete}
            onDismissContextComposition={onDismissContextComposition}
            onDismissInstructions={onDismissInstructions}
            canEditThreadMessage={canEditThreadMessage}
            onForkMessage={onForkMessage}
            onOpenFile={onOpenFile}
            onOpenAgent={onOpenAgent}
            onEditMessage={onEditMessage}
            onCancelEditMessage={onCancelEditMessage}
            onSubmitEditMessage={onSubmitEditMessage}
            onOpenFileDiff={onOpenFileDiff}
            onOpenTurnRuns={onOpenTurnRuns}
            turnStreamStatus={turnStreamStatus}
          />
        );
      })}
    </div>
  );
});

type CachedConversationPaneProps = Omit<
  CachedConversationPanesProps,
  "threadIDs" | "threadsByID" | "activeThreadID"
> & {
  thread: Thread;
  isActive: boolean;
};

const CachedConversationPane = memo(function CachedConversationPane({
  thread,
  isActive,
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
  onEditMessage,
  onCancelEditMessage,
  onSubmitEditMessage,
  onOpenFileDiff,
  onOpenTurnRuns,
  turnStreamStatus,
}: CachedConversationPaneProps): JSX.Element {
  // A brand-new thread takes over from the already-visible optimistic first
  // turn. Hiding that same turn behind the tab-switch layout gate creates a
  // blank frame during the handoff, so keep it visible from the first paint.
  const [layoutSettled, setLayoutSettled] = useState(() =>
    thread.turns.some((turn) => turn.id.startsWith(OPTIMISTIC_TURN_ID_PREFIX)),
  );
  const paneRef = useRef<HTMLDivElement | null>(null);
  const threadRef = useRef(thread);
  threadRef.current = thread;
  const handleOpenFile = useCallback(
    (path: string) => onOpenFile?.(threadRef.current, path),
    [onOpenFile],
  );
  // Memo-safe callbacks: PaneTurnView is React.memo'd, so every function prop
  // must keep a stable identity across re-renders or the bailout never fires.
  // They read the live thread through threadRef (same pattern as
  // handleOpenFile) instead of closing over the render-scope thread.
  const handleOpenAgentByID = useCallback(
    (agentID: string) => {
      const agent = threadRef.current.child_agents?.find(
        (candidate) => candidate.id === agentID,
      );
      if (agent) {
        void onOpenAgent(agent);
      }
    },
    [onOpenAgent],
  );
  const handleForkMessage = useCallback(
    (turnID: string, itemID: string) =>
      onForkMessage(threadRef.current, turnID, itemID),
    [onForkMessage],
  );
  const handleEditMessage = useCallback(
    (turnID: string, item: ThreadItem) =>
      onEditMessage(threadRef.current, turnID, item),
    [onEditMessage],
  );
  const handleSubmitEditMessage = useCallback(
    (
      turnID: string,
      item: ThreadItem,
      text: string,
      images: InputImage[],
      files: InputFile[],
    ) =>
      onSubmitEditMessage(
        threadRef.current,
        turnID,
        item,
        text,
        images,
        files,
      ),
    [onSubmitEditMessage],
  );
  const handleOpenFileDiffSelection = useCallback(
    (selection: TurnFileDiffSelection) =>
      onOpenFileDiff(threadRef.current, selection),
    [onOpenFileDiff],
  );
  const handleOpenTurnRunsForThread = useCallback(
    (turnID: string) => onOpenTurnRuns?.(threadRef.current, turnID),
    [onOpenTurnRuns],
  );
  const threadTurns = thread.turns ?? [];
  // Location (owner turn + item) lets renderTurn scope latestAgentMessageID to
  // the one turn that can match it, so a new agent message re-renders only
  // the previous and next owner turns instead of every PaneTurnView.
  const latestAgentLocation = latestAgentMessageLocation(threadTurns);
  const threadContextEntries = contextCompositionEntries.filter(
    (entry) => entry.threadID === thread.id,
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
  const threadInstructionEntries = instructionFilesEntries.filter(
    (entry) => entry.threadID === thread.id,
  );
  const threadInstructionCards = threadInstructionEntries.map((entry) => (
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
  const latestTurn = threadTurns[threadTurns.length - 1];
  const latestTurnStreamStatus = latestTurn
    ? turnStreamStatus[latestTurn.id]
    : undefined;
  const previousLayoutContentRef = useRef({
    turns: threadTurns,
    streamStatus: latestTurnStreamStatus,
    contextEntries: threadContextEntries,
    instructionEntries: threadInstructionEntries,
    historyMessageEdit,
    forkedFromID: thread.forked_from_id,
    worktree: thread.worktree,
    cwd: thread.cwd,
  });
  const wasActiveRef = useRef(isActive);

  useLayoutEffect(() => {
    const previous = previousLayoutContentRef.current;
    const contentChanged =
      previous.turns !== threadTurns ||
      previous.streamStatus !== latestTurnStreamStatus ||
      !sameEntriesByIdentity(previous.contextEntries, threadContextEntries) ||
      !sameEntriesByIdentity(previous.instructionEntries, threadInstructionEntries) ||
      previous.historyMessageEdit !== historyMessageEdit ||
      previous.forkedFromID !== thread.forked_from_id ||
      previous.worktree !== thread.worktree ||
      previous.cwd !== thread.cwd;
    const changedWhileHidden =
      contentChanged && (!isActive || !wasActiveRef.current);

    previousLayoutContentRef.current = {
      turns: threadTurns,
      streamStatus: latestTurnStreamStatus,
      contextEntries: threadContextEntries,
      instructionEntries: threadInstructionEntries,
      historyMessageEdit,
      forkedFromID: thread.forked_from_id,
      worktree: thread.worktree,
      cwd: thread.cwd,
    };
    wasActiveRef.current = isActive;

    if (changedWhileHidden && layoutSettled) {
      setLayoutSettled(false);
    }
  }, [
    contextCompositionEntries,
    historyMessageEdit,
    isActive,
    instructionFilesEntries,
    latestTurnStreamStatus,
    layoutSettled,
    thread.cwd,
    thread.forked_from_id,
    thread.worktree,
    threadTurns,
  ]);

  useLayoutEffect(() => {
    if (!isActive || layoutSettled) {
      return undefined;
    }

    const pane = paneRef.current;
    if (!pane) {
      setLayoutSettled(true);
      return undefined;
    }

    let frame: number | undefined;
    let timeout: number | undefined;
    let finished = false;
    let previousHeight = pane.scrollHeight;
    let stableFrames = 0;

    const finish = (): void => {
      if (finished) return;
      finished = true;
      if (frame !== undefined) {
        window.cancelAnimationFrame(frame);
        frame = undefined;
      }
      if (timeout !== undefined) {
        window.clearTimeout(timeout);
        timeout = undefined;
      }
      setLayoutSettled(true);
    };

    const sampleLayout = (): void => {
      frame = undefined;
      const nextHeight = pane.scrollHeight;
      if (nextHeight === previousHeight) {
        stableFrames += 1;
      } else {
        previousHeight = nextHeight;
        stableFrames = 0;
      }
      if (stableFrames >= CONVERSATION_LAYOUT_STABLE_FRAMES) {
        finish();
        return;
      }
      frame = window.requestAnimationFrame(sampleLayout);
    };

    frame = window.requestAnimationFrame(sampleLayout);
    timeout = window.setTimeout(finish, CONVERSATION_LAYOUT_SETTLE_TIMEOUT_MS);

    return () => {
      finished = true;
      if (frame !== undefined) {
        window.cancelAnimationFrame(frame);
      }
      if (timeout !== undefined) {
        window.clearTimeout(timeout);
      }
    };
  }, [isActive, layoutSettled, thread.id]);

  return (
    <div
      aria-hidden={isActive ? undefined : true}
      className="cached-conversation-pane"
      data-active={isActive}
      data-layout-settled={layoutSettled ? "" : undefined}
      data-thread-id={thread.id}
      inert={isActive ? undefined : true}
      ref={paneRef}
    >
      <div className="conversation-width session-flow">
        {isActive && conversationGridVisible ? <ConversationGridGuides /> : null}
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
            (entriesByAfterTurnID.get(turn.id) ?? []).map(renderContextEntry)
          }
          forcedFullTurnIDs={
            historyMessageEdit ? [historyMessageEdit.turnID] : undefined
          }
          renderTurn={(turn) => (
            <PaneTurnView
              turn={turn}
              cwd={thread.cwd ?? activeContextCwd}
              onOpenFile={onOpenFile ? handleOpenFile : undefined}
              onOpenAgent={handleOpenAgentByID}
              latestAgentMessageID={
                latestAgentLocation?.turnID === turn.id
                  ? latestAgentLocation.itemID
                  : undefined
              }
              isLatestTurn={latestTurn?.id === turn.id}
              onStreamFrame={onStreamFrame}
              onCollapseComplete={onCollapseComplete}
              onForkMessage={handleForkMessage}
              canEdit={canEditThreadMessage(thread)}
              onEditMessage={handleEditMessage}
              editingMessage={historyMessageEdit}
              onCancelEditMessage={onCancelEditMessage}
              onSubmitEditMessage={handleSubmitEditMessage}
              onOpenFileDiff={handleOpenFileDiffSelection}
              onOpenTurnRuns={
                onOpenTurnRuns ? handleOpenTurnRunsForThread : undefined
              }
              streamStatus={
                latestTurn?.id === turn.id ? latestTurnStreamStatus : undefined
              }
            />
          )}
        />
      </div>
    </div>
  );
});

type PaneTurnViewProps = {
  turn: Turn;
  cwd?: string;
  latestAgentMessageID?: string;
  isLatestTurn: boolean;
  canEdit: boolean;
  editingMessage?: HistoryMessageEditState;
  streamStatus?: TurnStreamStatus;
  onOpenFile?: (path: string) => void;
  onOpenAgent: (agentID: string) => void;
  onStreamFrame: () => void;
  onCollapseComplete: () => void;
  onForkMessage: (turnID: string, itemID: string) => void;
  onEditMessage: (turnID: string, item: ThreadItem) => void;
  onCancelEditMessage: () => void;
  onSubmitEditMessage: (
    turnID: string,
    item: ThreadItem,
    text: string,
    images: InputImage[],
    files: InputFile[],
  ) => void;
  onOpenFileDiff: (selection: TurnFileDiffSelection) => void;
  onOpenTurnRuns?: (turnID: string) => void;
};

// Memo boundary for the turn tree. Server events (item/started, item/
// completed, turn/completed) rebuild the thread object but preserve the
// identity of every turn they did not touch, so a memoized per-turn wrapper
// lets hundreds of untouched TurnView subtrees bail out of the re-render
// instead of reconciling the whole conversation on every event. All props
// must be value-compared or identity-stable — every callback the pane passes
// is a useCallback reading through threadRef for exactly that reason.
const PaneTurnView = memo(function PaneTurnView({
  turn,
  cwd,
  latestAgentMessageID,
  isLatestTurn,
  canEdit,
  editingMessage,
  streamStatus,
  onOpenFile,
  onOpenAgent,
  onStreamFrame,
  onCollapseComplete,
  onForkMessage,
  onEditMessage,
  onCancelEditMessage,
  onSubmitEditMessage,
  onOpenFileDiff,
  onOpenTurnRuns,
}: PaneTurnViewProps): JSX.Element {
  // TurnView's onOpenRuns takes no argument; bind the turn id here so the
  // pane-level callback can stay identity-stable (per-turn inline closures
  // would defeat the memo).
  const handleOpenRuns = useCallback(
    () => onOpenTurnRuns?.(turn.id),
    [onOpenTurnRuns, turn.id],
  );
  return (
    <TurnView
      turn={turn}
      cwd={cwd}
      onOpenFile={onOpenFile}
      onOpenAgent={onOpenAgent}
      latestAgentMessageID={latestAgentMessageID}
      isLatestTurn={isLatestTurn}
      onStreamFrame={onStreamFrame}
      onCollapseComplete={onCollapseComplete}
      onForkMessage={onForkMessage}
      onEditMessage={canEdit ? onEditMessage : undefined}
      editingMessage={editingMessage}
      onCancelEditMessage={onCancelEditMessage}
      onSubmitEditMessage={onSubmitEditMessage}
      onOpenFileDiff={onOpenFileDiff}
      onOpenRuns={onOpenTurnRuns ? handleOpenRuns : undefined}
      streamStatus={streamStatus}
    />
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
