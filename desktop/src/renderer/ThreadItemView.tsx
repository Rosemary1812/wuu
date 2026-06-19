import { type KeyboardEvent as ReactKeyboardEvent, useEffect, useRef, useState } from "react";
import type { ThreadItem, Turn } from "../shared/protocol";
import { agentHandoffDisplay } from "./AgentHandoff";
import { RichContent } from "./RichContent";
import {
  AgentMessageActions,
  MessageCopyButton,
  MessageEditButton,
  MessageFileList,
  MessageImageGrid,
} from "./MessageActions";
import { StreamingMarkdown } from "./StreamingMarkdown";
import { streamTextKey, streamTextStore } from "./StreamText";
import { streamFieldValue } from "./ThreadItemText";
import { ToolActivityRow } from "./ToolActivity";
import { ContextCompactionNotice, TurnNotice } from "./TurnNotice";
import { userMessageAnchorID } from "./TurnViewHelpers";
import {
  userFacingErrorForMessage,
  type UserFacingErrorAction,
} from "./UserFacingErrors";

export function ThreadItemView({
  turnID,
  turnStatus,
  item,
  cwd,
  streaming,
  pendingCompanionReasoning,
  actionableAgentMessageID,
  latestAgentMessageID,
  onStreamFrame,
  onForkMessage,
  onEditMessage,
  editing,
  editSubmitting,
  onCancelEditMessage,
  onSubmitEditMessage,
  onNoticeAction,
}: {
  turnID: string;
  turnStatus: Turn["status"];
  item: ThreadItem;
  cwd?: string;
  streaming: boolean;
  pendingCompanionReasoning?: boolean;
  actionableAgentMessageID?: string;
  latestAgentMessageID?: string;
  onStreamFrame: () => void;
  onForkMessage?: (turnID: string, itemID: string) => void;
  onEditMessage?: (turnID: string, item: ThreadItem) => void;
  editing?: boolean;
  editSubmitting?: boolean;
  onCancelEditMessage?: () => void;
  onSubmitEditMessage?: (turnID: string, item: ThreadItem, text: string) => void;
  onNoticeAction: (action: UserFacingErrorAction) => void;
}): JSX.Element | null {
  switch (item.type) {
    case "user_message": {
      const text = item.text ?? "";
      const handoff = agentHandoffDisplay(text);
      if (handoff) {
        return (
          <div className="agent-handoff-line" role="status">
            {handoff.label}
          </div>
        );
      }
      const copyable = text.trim() !== "";
      const editable = Boolean(
        onEditMessage &&
          (copyable || (item.images?.length ?? 0) > 0 || (item.files?.length ?? 0) > 0),
      );
      return (
        <div
          className={`user-message-block${copyable || editable ? " user-message-block-with-actions" : ""}`}
          id={userMessageAnchorID(turnID, item.id)}
          data-user-message-id={item.id}
          data-turn-id={turnID}
        >
          {editing ? (
            <UserMessageInlineEditor
              item={item}
              initialText={text}
              submitting={Boolean(editSubmitting)}
              onCancel={onCancelEditMessage}
              onSubmit={(nextText) =>
                onSubmitEditMessage?.(turnID, item, nextText)
              }
            />
          ) : (
            <div className="message user-message">
              {item.images?.length ? (
                <MessageImageGrid images={item.images} />
              ) : null}
              {item.files?.length ? <MessageFileList files={item.files} /> : null}
              {text ? <RichContent text={text} cwd={cwd} /> : null}
            </div>
          )}
          {!editing && (copyable || editable) ? (
            <div
              className="message-actions user-message-actions"
              aria-label="用户消息操作"
            >
              {copyable ? (
                <MessageCopyButton
                  getText={() => text}
                  className="message-action-button"
                  iconSize={15}
                />
              ) : null}
              {editable && onEditMessage ? (
                <MessageEditButton
                  onEdit={() => onEditMessage(turnID, item)}
                  className="message-action-button"
                  iconSize={15}
                />
              ) : null}
            </div>
          ) : null}
        </div>
      );
    }
    case "agent_message": {
      const streamKeyValue = streamTextKey(turnID, item.id, "text");
      const agentText = streamTextStore.has(streamKeyValue)
        ? streamTextStore.get(streamKeyValue)
        : (item.text ?? "");
      const copyable = streaming || agentText.trim() !== "";
      const actionsVisible =
        turnStatus === "completed" &&
        item.id === actionableAgentMessageID &&
        copyable;
      const actionsPersistent =
        actionsVisible && item.id === latestAgentMessageID;
      const isProcessText = item.phase === "commentary";
      const reserveActionSlot =
        copyable &&
        !isProcessText &&
        (streaming || actionsVisible || item.phase === "final_answer");
      return (
        <article
          className={`agent-block${
            reserveActionSlot
              ? ` agent-block-with-action-slot${actionsVisible ? " agent-actions-available" : ""}${actionsPersistent ? " agent-actions-persistent" : ""}`
              : ""
          }`}
        >
          <div className="agent-text">
            <AgentMessageContent
              turnID={turnID}
              item={item}
              cwd={cwd}
              streaming={streaming}
              pendingCompanionReasoning={pendingCompanionReasoning}
              onStreamFrame={onStreamFrame}
            />
          </div>
          {reserveActionSlot && actionsVisible ? (
            <AgentMessageActions
              getText={() => streamFieldValue(turnID, item, "text")}
              onFork={
                onForkMessage ? () => onForkMessage(turnID, item.id) : undefined
              }
            />
          ) : reserveActionSlot ? (
            <div
              className="message-actions agent-message-actions action-slot-placeholder"
              aria-hidden="true"
            />
          ) : null}
        </article>
      );
    }
    case "reasoning":
      return (
        <article className="reasoning-block">
          <ReasoningContent
            turnID={turnID}
            item={item}
            streaming={streaming}
            onStreamFrame={onStreamFrame}
          />
        </article>
      );
    case "tool_call":
    case "collab_agent_tool_call":
      return <ToolActivityRow items={[item]} />;
    case "context_compaction":
      return <ContextCompactionNotice text={item.text} />;
    case "error":
      return (
        <TurnNotice
          display={userFacingErrorForMessage(item.error, "turn")}
          onAction={onNoticeAction}
        />
      );
    default:
      return null;
  }
}

function UserMessageInlineEditor({
  item,
  initialText,
  submitting,
  onCancel,
  onSubmit,
}: {
  item: ThreadItem;
  initialText: string;
  submitting: boolean;
  onCancel?: () => void;
  onSubmit?: (text: string) => void;
}): JSX.Element {
  const [text, setText] = useState(initialText);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const hasAttachments = Boolean(item.images?.length || item.files?.length);
  const canSubmit = text.trim().length > 0 || hasAttachments;

  useEffect(() => {
    setText(initialText);
  }, [initialText, item.id]);

  useEffect(() => {
    window.requestAnimationFrame(() => {
      const textarea = textareaRef.current;
      if (!textarea) {
        return;
      }
      textarea.focus();
      textarea.setSelectionRange(textarea.value.length, textarea.value.length);
    });
  }, []);

  function submit(): void {
    if (!canSubmit || submitting) {
      return;
    }
    onSubmit?.(text);
  }

  function handleKeyDown(event: ReactKeyboardEvent<HTMLTextAreaElement>): void {
    if (event.key === "Escape") {
      event.preventDefault();
      onCancel?.();
      return;
    }
    if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
      event.preventDefault();
      submit();
    }
  }

  return (
    <div className="user-message-edit">
      {item.images?.length ? <MessageImageGrid images={item.images} /> : null}
      {item.files?.length ? <MessageFileList files={item.files} /> : null}
      <textarea
        ref={textareaRef}
        className="user-message-edit-input"
        value={text}
        disabled={submitting}
        onChange={(event) => setText(event.target.value)}
        onKeyDown={handleKeyDown}
        rows={Math.max(2, Math.min(8, text.split("\n").length))}
      />
      <div className="user-message-edit-actions">
        <button
          className="user-message-edit-button secondary"
          type="button"
          disabled={submitting}
          onClick={onCancel}
        >
          取消
        </button>
        <button
          className="user-message-edit-button primary"
          type="button"
          disabled={!canSubmit || submitting}
          onClick={submit}
        >
          发送
        </button>
      </div>
    </div>
  );
}

function AgentMessageContent({
  turnID,
  item,
  cwd,
  streaming,
  pendingCompanionReasoning,
  onStreamFrame,
}: {
  turnID: string;
  item: ThreadItem;
  cwd?: string;
  streaming: boolean;
  /**
   * True when the turn has a reasoning block that the model just finished
   * writing. The first answer item waits a short beat so the reasoning
   * cursor can fully settle before the text cursor starts animating.
   */
  pendingCompanionReasoning?: boolean;
  onStreamFrame: () => void;
}): JSX.Element {
  const streamKeyValue = streamTextKey(turnID, item.id, "text");
  // isLive is driven entirely by `item.status`: once the back-end marks
  // the item completed the surface must settle, no matter what the
  // streaming buffer looks like. This is what makes "two places
  // streaming at once" impossible — there's exactly one source of
  // liveness and it changes atomically when the back-end commits.
  const isLive = item.status === "in_progress";
  // Hold the cursor back when a just-completed reasoning block is still
  // visually settling. The reasoning and text streams are sequential on
  // the wire, but the cursor reveal and the next text's reveal can briefly
  // race in the UI.
  const [cursorArmed, setCursorArmed] = useState<boolean>(
    !pendingCompanionReasoning,
  );
  useEffect(() => {
    if (!pendingCompanionReasoning) {
      setCursorArmed(true);
      return;
    }
    // 240ms is enough to let the reasoning cursor finish its tail reveal
    // (it's bound by max cps but typically clears in ~150ms for short
    // reasoning). Tuned by hand; bump up if you can still see overlap.
    const timer = window.setTimeout(() => {
      setCursorArmed(true);
    }, 240);
    return () => {
      window.clearTimeout(timer);
    };
  }, [pendingCompanionReasoning]);

  // When the item completes, drop the buffered stream immediately. Doing
  // this here (instead of inside StreamingMarkdown's onSettled callback)
  // means the store is cleared even if the component is about to unmount
  // and the callback never fires. The store entry is no longer needed
  // for rendering, since the final text lives on `item.text`.
  useEffect(() => {
    if (!isLive) {
      streamTextStore.clearItem(turnID, item.id);
    }
  }, [turnID, item.id, isLive]);

  const hasBufferedStream = streamTextStore.has(streamKeyValue);

  return (
    <StreamingMarkdown
      streamKey={streamKeyValue}
      initialText={
        hasBufferedStream
          ? streamTextStore.seedValue(streamKeyValue)
          : item.text
      }
      cwd={cwd}
      isLive={isLive && cursorArmed}
      phase={
        item.phase === "final_answer" ||
        (!item.phase && item.status === "in_progress")
          ? "final_answer"
          : "commentary"
      }
      onFrame={onStreamFrame}
    />
  );
}

function ReasoningContent({
  turnID,
  item,
  streaming,
  onStreamFrame,
}: {
  turnID: string;
  item: ThreadItem;
  streaming: boolean;
  onStreamFrame: () => void;
}): JSX.Element {
  const streamKeyValue = streamTextKey(turnID, item.id, "text");
  const isLive = item.status === "in_progress";

  // Same clear-on-settle pattern as AgentMessageContent — we own the
  // lifecycle instead of relying on the StreamingMarkdown callback.
  useEffect(() => {
    if (!isLive) {
      streamTextStore.clearItem(turnID, item.id);
    }
  }, [turnID, item.id, isLive]);

  const hasBufferedStream = streamTextStore.has(streamKeyValue);

  return (
    <StreamingMarkdown
      streamKey={streamKeyValue}
      initialText={
        hasBufferedStream
          ? streamTextStore.seedValue(streamKeyValue)
          : item.text
      }
      className="streaming-markdown rich-content reasoning-stream"
      isLive={isLive}
      phase="commentary"
      onFrame={onStreamFrame}
    />
  );
}
