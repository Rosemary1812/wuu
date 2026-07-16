import { X } from "lucide-react";
import type {
  InputFile,
  InputImage,
  Thread,
  ThreadItem,
} from "../shared/protocol";
import { SplitPaneComposer } from "./ComposerView";
import {
  isThreadRunning,
  type ComposerDraftState,
  type ConversationPaneID,
  type TurnStreamStatus,
} from "./AppState";
import { ConversationTurnList } from "./ConversationTurnList";
import { threadDisplayTitle } from "./ThreadTitles";
import { TurnView, latestAgentMessageItemID } from "./TurnView";
import type { TurnFileDiffSelection } from "./TurnFileDiffTypes";
import { useI18n } from "./i18n";

export function ConversationSplitPane({
  pane,
  thread,
  threads,
  active,
  activeContextCwd,
  appStatus,
  streamStatus,
  draft,
  viewSwitchPending,
  queryHistory,
  editingMessage,
  onActivate,
  onClose,
  onBodyRef,
  onScroll,
  onSetPrompt,
  onPasteAttachmentFiles,
  onRemoveFile,
  onRemoveImage,
  onSend,
  onInterrupt,
  onForkMessage,
  onOpenFile,
  onOpenAgent,
  onEditMessage,
  onCancelEditMessage,
  onSubmitEditMessage,
  onStreamFrame,
  onOpenFileDiff,
}: {
  pane: ConversationPaneID;
  thread: Thread;
  threads: Thread[];
  active: boolean;
  activeContextCwd?: string;
  appStatus: string;
  streamStatus?: TurnStreamStatus;
  draft: ComposerDraftState;
  viewSwitchPending: boolean;
  queryHistory: string[];
  editingMessage?: { turnID: string; itemID: string; submitting: boolean };
  onActivate: () => void;
  onClose: () => void;
  onBodyRef: (node: HTMLElement | null) => void;
  onScroll: (node: HTMLElement) => void;
  onSetPrompt: (value: string) => void;
  onPasteAttachmentFiles: (files: File[]) => void;
  onRemoveFile: (id: string) => void;
  onRemoveImage: (id: string) => void;
  onSend: () => void;
  onInterrupt: () => void;
  onForkMessage: (turnID: string, itemID: string) => void;
  onOpenFile?: (path: string) => void;
  onOpenAgent?: (agentID: string) => void;
  onEditMessage?: (turnID: string, item: ThreadItem) => void;
  onCancelEditMessage?: () => void;
  onSubmitEditMessage?: (
    turnID: string,
    item: ThreadItem,
    text: string,
    images: InputImage[],
    files: InputFile[],
  ) => void;
  onStreamFrame: () => void;
  onOpenFileDiff?: (selection: TurnFileDiffSelection) => void;
}): JSX.Element {
  const { t } = useI18n();
  const paneTurns = thread.turns ?? [];
  const paneLatestAgentMessageID = latestAgentMessageItemID(paneTurns);
  const closeLabel = t(
    pane === "secondary" ? "split.closeRight" : "split.closeLeft",
  );
  const paneRunning = isThreadRunning(thread);
  const paneReadOnly = Boolean(thread.read_only);
  const paneStatus = paneReadOnly
    ? paneRunning
      ? t("app.childTaskRunning")
      : t("app.childTaskReadOnly")
    : paneRunning
      ? streamStatus?.text ?? t("runDebug.inProgress")
      : active && appStatus !== "ready"
        ? appStatus
        : "";

  return (
    <section
      className={`conversation-split-pane${active ? " active" : ""}`}
      aria-label={t(
        pane === "secondary" ? "split.forkConversation" : "split.sourceConversation",
      )}
      onPointerDown={onActivate}
    >
      <div className="conversation-split-header">
        <div className="conversation-split-title">
          <span>
            {t(pane === "secondary" ? "split.fork" : "split.source")}
          </span>
          <strong>
            {threadDisplayTitle(thread, threads, t("tabs.newConversation"))}
          </strong>
        </div>
        <button
          className="icon-button conversation-split-close"
          type="button"
          aria-label={closeLabel}
          title={closeLabel}
          onClick={onClose}
        >
          <X className="icon" />
        </button>
      </div>
      <div
        ref={onBodyRef}
        className="conversation-split-body"
        onScroll={(event) => onScroll(event.currentTarget)}
      >
        <div className="conversation-width conversation-split-width session-flow">
          <ConversationTurnList
            threadID={thread.id}
            turns={paneTurns}
            forcedFullTurnIDs={
              editingMessage ? [editingMessage.turnID] : undefined
            }
            renderTurn={(turn) => (
                <TurnView
                  turn={turn}
                  cwd={thread.cwd ?? activeContextCwd}
                  onOpenFile={onOpenFile}
                  onOpenAgent={onOpenAgent}
                  latestAgentMessageID={paneLatestAgentMessageID}
                  isLatestTurn={
                    paneTurns[paneTurns.length - 1]?.id === turn.id
                  }
                  onStreamFrame={onStreamFrame}
                  onForkMessage={onForkMessage}
                  onEditMessage={
                    onEditMessage
                      ? (turnID, item) => onEditMessage(turnID, item)
                      : undefined
                  }
                  editingMessage={editingMessage}
                  onCancelEditMessage={onCancelEditMessage}
                  onSubmitEditMessage={
                    onSubmitEditMessage
                      ? (turnID, item, text, images, files) =>
                          onSubmitEditMessage(turnID, item, text, images, files)
                      : undefined
                  }
                  onOpenFileDiff={onOpenFileDiff}
                  streamStatus={
                    paneTurns[paneTurns.length - 1]?.id === turn.id
                      ? streamStatus
                      : undefined
                  }
                />
            )}
          />
        </div>
      </div>
      <SplitPaneComposer
        prompt={draft.prompt}
        setPrompt={onSetPrompt}
        files={draft.files}
        images={draft.images}
        running={(!paneReadOnly && paneRunning) || viewSwitchPending}
        readOnly={paneReadOnly}
        status={paneStatus}
        statusLiveProgress={
          !paneReadOnly && paneRunning ? streamStatus?.liveProgress : false
        }
        onPasteAttachmentFiles={onPasteAttachmentFiles}
        onRemoveFile={onRemoveFile}
        onRemoveImage={onRemoveImage}
        onSend={onSend}
        onInterrupt={onInterrupt}
        queryHistorySessionID={thread.id}
        queryHistory={queryHistory}
      />
    </section>
  );
}
