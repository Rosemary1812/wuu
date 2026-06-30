import { X } from "lucide-react";
import type { InputFile, InputImage, Thread, ThreadItem } from "../shared/protocol";
import { SplitPaneComposer } from "./ComposerView";
import {
  isThreadRunning,
  type ComposerDraftState,
  type ConversationPaneID,
} from "./AppState";
import { ConversationTurnList } from "./ConversationTurnList";
import { threadDisplayTitle } from "./ThreadTitles";
import { TurnView, latestAgentMessageItemID } from "./TurnView";
import type { UserFacingErrorAction } from "./UserFacingErrors";

export function ConversationSplitPane({
  pane,
  thread,
  threads,
  active,
  activeContextCwd,
  appStatus,
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
  onEditMessage,
  onCancelEditMessage,
  onSubmitEditMessage,
  onStreamFrame,
  onNoticeAction,
}: {
  pane: ConversationPaneID;
  thread: Thread;
  threads: Thread[];
  active: boolean;
  activeContextCwd?: string;
  appStatus: string;
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
  onNoticeAction: (action: UserFacingErrorAction) => void;
}): JSX.Element {
  const paneTurns = thread.turns ?? [];
  const paneLatestAgentMessageID = latestAgentMessageItemID(paneTurns);
  const closeLabel = pane === "secondary" ? "关闭右侧对话" : "关闭左侧对话";
  const paneRunning = isThreadRunning(thread);
  const paneReadOnly = Boolean(thread.read_only);
  const paneStatus = paneReadOnly
    ? paneRunning
      ? "子任务运行中"
      : "子任务会话只读"
    : paneRunning
      ? "运行中"
      : active && appStatus !== "ready"
        ? appStatus
        : "";

  return (
    <section
      className={`conversation-split-pane${active ? " active" : ""}`}
      aria-label={pane === "secondary" ? "分叉对话" : "源对话"}
      onPointerDown={onActivate}
    >
      <div className="conversation-split-header">
        <div className="conversation-split-title">
          <span>{pane === "secondary" ? "分叉" : "源会话"}</span>
          <strong>{threadDisplayTitle(thread, threads, "新对话")}</strong>
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
                latestAgentMessageID={paneLatestAgentMessageID}
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
                onNoticeAction={onNoticeAction}
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
