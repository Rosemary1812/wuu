import {
  type ChangeEvent as ReactChangeEvent,
  type ClipboardEvent as ReactClipboardEvent,
  type DragEvent as ReactDragEvent,
  type KeyboardEvent as ReactKeyboardEvent,
  useEffect,
  useRef,
  useState
} from "react";
import { ChevronDown, ChevronUp, Plus, Send } from "lucide-react";
import type { InputFile, InputImage, ThreadItem, Turn } from "../shared/protocol";
import { agentHandoffDisplayItem } from "./AgentHandoff";
import {
  clipboardAttachmentFiles,
  composerFileFromFile,
  composerImageFromFile,
  isSupportedComposerAttachment
} from "./ComposerMessages";
import {
  isInternalUserNotificationItem,
  isProcessNotificationItem,
} from "./InternalUserNotification";
import {
  collapsedLongTextPreview,
  useLongTextCollapse,
} from "./LongTextCollapse";
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
import {
  ContextCompactionNotice,
  SystemEventDivider,
  TurnNotice,
} from "./TurnNotice";
import { userMessageAnchorID } from "./TurnViewHelpers";
import {
  userFacingErrorForMessage,
} from "./UserFacingErrors";
import { useI18n } from "./i18n";

export function ThreadItemView({
  turnID,
  turnStatus,
  item,
  cwd,
  onOpenFile,
  streaming,
  pendingCompanionReasoning,
  actionableAgentMessageID,
  latestAgentMessageID,
  onStreamFrame,
  onForkMessage,
  onOpenRuns,
  onEditMessage,
  editing,
  editSubmitting,
  onCancelEditMessage,
  onSubmitEditMessage,
  onOpenAgent,
  editSummaryCard,
}: {
  turnID: string;
  turnStatus: Turn["status"];
  item: ThreadItem;
  cwd?: string;
  onOpenFile?: (path: string) => void;
  streaming: boolean;
  pendingCompanionReasoning?: boolean;
  actionableAgentMessageID?: string;
  latestAgentMessageID?: string;
  onStreamFrame: () => void;
  onForkMessage?: (turnID: string, itemID: string) => void;
  onOpenRuns?: () => void;
  onEditMessage?: (turnID: string, item: ThreadItem) => void;
  editing?: boolean;
  editSubmitting?: boolean;
  onCancelEditMessage?: () => void;
  onSubmitEditMessage?: (
    turnID: string,
    item: ThreadItem,
    text: string,
    images: InputImage[],
    files: InputFile[],
  ) => void;
  onOpenAgent?: (agentID: string) => void;
  editSummaryCard?: JSX.Element;
}): JSX.Element | null {
  const { t } = useI18n();
  switch (item.type) {
    case "user_message": {
      const text = item.text ?? "";
      if (isProcessNotificationItem(item)) {
        return null;
      }
      // Item-aware gate: the name field is the reliable wire signal. Even
      // when the payload text is malformed (combined envelopes with \n\n
      // joins, <changed_file_overlap> tails), the item-level helper short-
      // circuits to a generic handoff divider rather than letting raw JSON
      // slip into the chat bubble.
      const handoff = agentHandoffDisplayItem(item);
      if (handoff) {
        return (
          <SystemEventDivider
            text={handoff.label}
            className="agent-handoff-divider"
          />
        );
      }
      if (isInternalUserNotificationItem(item)) {
        return null;
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
              onSubmit={(nextText, nextImages, nextFiles) =>
                onSubmitEditMessage?.(turnID, item, nextText, nextImages, nextFiles)
              }
            />
          ) : (
            <UserMessageBubble
              text={text}
              images={item.images ?? []}
              files={item.files ?? []}
              cwd={cwd}
              onOpenFile={onOpenFile}
            />
          )}
          {!editing && (copyable || editable) ? (
            <div
              className="message-actions user-message-actions"
              aria-label={t("message.userActions")}
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
      const isProcessText = item.phase === "commentary";
      const actionsVisible =
        turnStatus === "completed" &&
        item.id === actionableAgentMessageID &&
        copyable &&
        !isProcessText;
      const actionsPersistent =
        actionsVisible && item.id === latestAgentMessageID;
      // Only the persistent bar (latest answer, always visible) takes
      // in-flow space; historical answers get a hover overlay so no
      // invisible slot pads the turn boundary. Streaming answers render
      // no bar at all — the old streaming placeholder reserved 32px of
      // dead space under text that had nothing to offer yet.
      return (
        <article
          className={`agent-block${
            actionsVisible
              ? ` agent-block-with-action-slot agent-actions-available${actionsPersistent ? " agent-actions-persistent" : " agent-actions-overlay"}`
              : ""
          }`}
        >
          <div className="agent-text">
            <AgentMessageContent
              turnID={turnID}
              item={item}
              cwd={cwd}
              onOpenFile={onOpenFile}
              pendingCompanionReasoning={pendingCompanionReasoning}
              onStreamFrame={onStreamFrame}
            />
          </div>
          {editSummaryCard}
          {actionsVisible ? (
            <AgentMessageActions
              getText={() => streamFieldValue(turnID, item, "text")}
              onFork={
                onForkMessage ? () => onForkMessage(turnID, item.id) : undefined
              }
              onOpenRuns={onOpenRuns}
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
            cwd={cwd}
            onOpenFile={onOpenFile}
            onStreamFrame={onStreamFrame}
          />
        </article>
      );
    case "tool_call":
      return <ToolActivityRow items={[item]} />;
    case "collab_agent_tool_call":
      return <ToolActivityRow items={[item]} />;
    case "context_compaction":
      return (
        <ContextCompactionNotice
          text={item.text}
          reason={item.reason}
          status={item.status}
        />
      );
    case "error":
      return (
        <TurnNotice display={userFacingErrorForMessage(item.error, "turn")} />
      );
    default:
      return null;
  }
}

function UserMessageBubble({
  text,
  images,
  files,
  cwd,
  onOpenFile,
}: {
  text: string;
  images: InputImage[];
  files: InputFile[];
  cwd?: string;
  onOpenFile?: (path: string) => void;
}): JSX.Element {
  const { t } = useI18n();
  // Threshold + preview logic + state keying all live in
  // `./LongTextCollapse` so the chat bubble can reuse the exact same
  // numbers. The hook's `{text, expanded}` state shape is what makes
  // the toggle survive a parent re-render with a new message body
  // without flashing the previous expansion — see the module doc.
  const { collapsible, expanded, toggleExpanded } = useLongTextCollapse(text);
  const collapsed = collapsible && !expanded;
  const displayedText = collapsed ? collapsedLongTextPreview(text) : text;

  return (
    <div
      className={`message user-message${
        collapsible
          ? ` user-message-long-card ${expanded ? "expanded" : "collapsed"}`
          : ""
      }`}
    >
      {images.length ? <MessageImageGrid images={images} /> : null}
      {files.length ? <MessageFileList files={files} /> : null}
      {collapsible ? (
        <div className="user-message-raw-query">{displayedText}</div>
      ) : text ? (
        <RichContent text={text} cwd={cwd} onOpenFile={onOpenFile} />
      ) : null}
      {collapsible ? (
        <button
          type="button"
          className="user-message-expand-toggle"
          aria-expanded={expanded}
          onClick={toggleExpanded}
        >
          <span>{expanded ? t("common.collapse") : t("common.showMore")}</span>
          {expanded ? <ChevronUp aria-hidden="true" /> : <ChevronDown aria-hidden="true" />}
        </button>
      ) : null}
    </div>
  );
}

// Both helpers (collapsedUserMessagePreview + isCollapsibleUserMessage) and
// the four COLLAPSIBLE_USER_MESSAGE_* constants moved to `./LongTextCollapse`
// so the chat bubble can reuse the same thresholds and preview estimator.
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
  onSubmit?: (text: string, images: InputImage[], files: InputFile[]) => void;
}): JSX.Element {
  const { t } = useI18n();
  const [text, setText] = useState(initialText);
  const [images, setImages] = useState<InputImage[]>(item.images ?? []);
  const [files, setFiles] = useState<InputFile[]>(item.files ?? []);
  const [dragOver, setDragOver] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const hasAttachments = images.length > 0 || files.length > 0;
  const canSubmit = text.trim().length > 0 || hasAttachments;

  // Re-seed local state when the editor is reopened on a different user
  // message, or when the upstream item swaps its attachment arrays (e.g.
  // after a stream update). Without this, editing message B and then
  // cancelling back to message A would show B's draft in A.
  useEffect(() => {
    setText(initialText);
    setImages(item.images ?? []);
    setFiles(item.files ?? []);
  }, [initialText, item.id, item.images, item.files]);

  useEffect(() => {
    window.requestAnimationFrame(() => {
      const textarea = textareaRef.current;
      if (!textarea) {
        return;
      }
      // preventScroll: the conversation pane owns its scroll position, so
      // the browser's default focus-scroll must not yank the viewport to
      // bring the textarea into view — that scroll would disarm auto-follow
      // and surface the "跳到最新" pill on what is otherwise a deliberate
      // edit action. We scroll the editor into view ourselves on edit start
      // (see startEditingThreadMessageFromHistory) so this stays consistent
      // with the rest of the conversation scroll contract.
      textarea.focus({ preventScroll: true });
      textarea.setSelectionRange(textarea.value.length, textarea.value.length);
    });
  }, []);

  function submit(): void {
    if (!canSubmit || submitting) {
      return;
    }
    onSubmit?.(text, images, files);
  }

  async function addAttachmentFiles(filesToAdd: File[]): Promise<void> {
    const supported = filesToAdd.filter(isSupportedComposerAttachment);
    if (supported.length === 0) {
      return;
    }
    const imageAdditions: InputImage[] = [];
    const fileAdditions: InputFile[] = [];
    for (const file of supported) {
      if (file.type.toLowerCase().startsWith("image/")) {
        try {
          const composed = await composerImageFromFile(file);
          imageAdditions.push({ media_type: composed.media_type, data: composed.data });
        } catch {
          // Skip the individual failed image; the rest still land.
        }
      } else {
        try {
          const composed = await composerFileFromFile(file);
          fileAdditions.push({
            media_type: composed.media_type,
            data: composed.data,
            filename: composed.filename
          });
        } catch {
          // Same per-file resilience — bad PDFs shouldn't kill the batch.
        }
      }
    }
    if (imageAdditions.length > 0) {
      setImages((prev) => [...prev, ...imageAdditions]);
    }
    if (fileAdditions.length > 0) {
      setFiles((prev) => [...prev, ...fileAdditions]);
    }
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

  function handlePaste(event: ReactClipboardEvent<HTMLTextAreaElement>): void {
    if (submitting) {
      return;
    }
    const pasted = clipboardAttachmentFiles(event);
    if (pasted.length === 0) {
      return;
    }
    event.preventDefault();
    void addAttachmentFiles(pasted);
  }

  function handleFileInputChange(event: ReactChangeEvent<HTMLInputElement>): void {
    const selected = Array.from(event.currentTarget.files ?? []);
    event.currentTarget.value = "";
    if (selected.length > 0) {
      void addAttachmentFiles(selected);
    }
  }

  function handleDragOver(event: ReactDragEvent<HTMLDivElement>): void {
    if (submitting) {
      return;
    }
    if (!event.dataTransfer.types.includes("Files")) {
      return;
    }
    event.preventDefault();
    setDragOver(true);
  }

  function handleDragLeave(event: ReactDragEvent<HTMLDivElement>): void {
    if (event.currentTarget.contains(event.relatedTarget as Node | null)) {
      return;
    }
    setDragOver(false);
  }

  function handleDrop(event: ReactDragEvent<HTMLDivElement>): void {
    if (submitting) {
      return;
    }
    const dropped = Array.from(event.dataTransfer?.files ?? []);
    if (dropped.length === 0) {
      return;
    }
    event.preventDefault();
    setDragOver(false);
    void addAttachmentFiles(dropped);
  }

  function removeImage(index: number): void {
    setImages((prev) => prev.filter((_, currentIndex) => currentIndex !== index));
  }

  function removeFile(index: number): void {
    setFiles((prev) => prev.filter((_, currentIndex) => currentIndex !== index));
  }

  return (
    <div
      className={`user-message-edit${dragOver ? " user-message-edit-drop-active" : ""}`}
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
    >
      <input
        ref={fileInputRef}
        className="user-message-edit-file-input"
        type="file"
        accept="image/*,application/pdf"
        multiple
        tabIndex={-1}
        onChange={handleFileInputChange}
      />
      {images.length > 0 ? (
        <MessageImageGrid images={images} onRemove={removeImage} />
      ) : null}
      {files.length > 0 ? (
        <MessageFileList files={files} onRemove={removeFile} />
      ) : null}
      <textarea
        ref={textareaRef}
        className="user-message-edit-input"
        value={text}
        disabled={submitting}
        onChange={(event) => setText(event.target.value)}
        onKeyDown={handleKeyDown}
        onPaste={handlePaste}
        rows={Math.max(1, Math.min(8, text.split("\n").length))}
      />
      <div className="user-message-edit-toolbar">
        <button
          type="button"
          className="composer-tool-button user-message-edit-attach-button"
          aria-label={t("composer.addAttachment")}
          title={t("message.addImageOrPdf")}
          disabled={submitting}
          onClick={() => fileInputRef.current?.click()}
        >
          <Plus aria-hidden="true" />
        </button>
        <div className="user-message-edit-spacer" />
        <div className="user-message-edit-actions">
          <button
            className="user-message-edit-button secondary"
            type="button"
            disabled={submitting}
            onClick={onCancel}
          >
            {t("common.cancel")}
          </button>
          <button
            className="composer-action-button composer-send-button"
            type="button"
            aria-label={t("composer.send")}
            title={t("composer.send")}
            disabled={!canSubmit || submitting}
            onClick={submit}
          >
            <Send aria-hidden="true" />
          </button>
        </div>
      </div>
    </div>
  );
}

function AgentMessageContent({
  turnID,
  item,
  cwd,
  onOpenFile,
  pendingCompanionReasoning,
  onStreamFrame,
}: {
  turnID: string;
  item: ThreadItem;
  cwd?: string;
  onOpenFile?: (path: string) => void;
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

  const hasBufferedStream = streamTextStore.has(streamKeyValue);
  const canReleaseBufferedStream = !isLive && typeof item.text === "string" && item.text.length > 0;

  return (
    <StreamingMarkdown
      streamKey={streamKeyValue}
      initialText={
        isLive && hasBufferedStream
          ? streamTextStore.seedValue(streamKeyValue)
          : item.text
      }
      cwd={cwd}
      onOpenFile={onOpenFile}
      isLive={isLive && cursorArmed}
      phase={
        item.phase === "final_answer" ||
        (!item.phase && item.status === "in_progress")
          ? "final_answer"
          : "commentary"
      }
      onFrame={onStreamFrame}
      onSettled={
        canReleaseBufferedStream
          ? () => streamTextStore.clearItem(turnID, item.id)
          : undefined
      }
    />
  );
}

function ReasoningContent({
  turnID,
  item,
  cwd,
  onOpenFile,
  onStreamFrame,
}: {
  turnID: string;
  item: ThreadItem;
  cwd?: string;
  onOpenFile?: (path: string) => void;
  onStreamFrame: () => void;
}): JSX.Element {
  const streamKeyValue = streamTextKey(turnID, item.id, "text");
  const isLive = item.status === "in_progress";

  const hasBufferedStream = streamTextStore.has(streamKeyValue);
  const canReleaseBufferedStream = !isLive && typeof item.text === "string" && item.text.length > 0;

  return (
    <StreamingMarkdown
      streamKey={streamKeyValue}
      initialText={
        isLive && hasBufferedStream
          ? streamTextStore.seedValue(streamKeyValue)
          : item.text
      }
      className="streaming-markdown rich-content reasoning-stream"
      cwd={cwd}
      onOpenFile={onOpenFile}
      isLive={isLive}
      phase="commentary"
      onFrame={onStreamFrame}
      onSettled={
        canReleaseBufferedStream
          ? () => streamTextStore.clearItem(turnID, item.id)
          : undefined
      }
    />
  );
}
