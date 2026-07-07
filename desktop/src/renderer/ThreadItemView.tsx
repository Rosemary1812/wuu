import {
  type ChangeEvent as ReactChangeEvent,
  type ClipboardEvent as ReactClipboardEvent,
  type DragEvent as ReactDragEvent,
  type KeyboardEvent as ReactKeyboardEvent,
  useEffect,
  useRef,
  useState
} from "react";
import { ChevronDown, ChevronUp, PanelRightOpen, Paperclip, Send } from "lucide-react";
import type { InputFile, InputImage, ThreadItem, Turn } from "../shared/protocol";
import { agentHandoffDisplay } from "./AgentHandoff";
import { replyCountBadge } from "./AppState";
import {
  clipboardAttachmentFiles,
  composerFileFromFile,
  composerImageFromFile,
  inputFilesFromComposer,
  inputImagesFromComposer,
  isSupportedComposerAttachment
} from "./ComposerMessages";
import { EnvelopeNotice } from "./EnvelopeNotice";
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
import { ParticipantChip } from "./ParticipantChip";
import { ToolActivityRow } from "./ToolActivity";
import { ContextCompactionNotice, TurnNotice } from "./TurnNotice";
import { userMessageAnchorID } from "./TurnViewHelpers";
import {
  userFacingErrorForMessage,
  type UserFacingErrorAction,
} from "./UserFacingErrors";

const COLLAPSIBLE_USER_MESSAGE_LINE_THRESHOLD = 14;
const COLLAPSIBLE_USER_MESSAGE_CHAR_THRESHOLD = 1200;
const COLLAPSIBLE_USER_MESSAGE_SOFT_LINE_CHARS = 84;
const COLLAPSIBLE_USER_MESSAGE_PREVIEW_LINES = 14;

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
  onEditMessage,
  editing,
  editSubmitting,
  onCancelEditMessage,
  onSubmitEditMessage,
  onNoticeAction,
  onOpenAgent,
  onOpenSubthread,
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
  onNoticeAction: (action: UserFacingErrorAction) => void;
  onOpenAgent?: (agentID: string) => void;
  onOpenSubthread?: (item: ThreadItem) => void;
}): JSX.Element | null {
  switch (item.type) {
    case "user_message": {
      const text = item.text ?? "";
      // Envelope-routed messages (group chat → resident DM) render as a
      // collapsed meta row, never as a user bubble — a bubble would read
      // as if the user typed the forwarded content themselves.
      if (item.envelope_meta && item.envelope_meta.length > 0) {
        return <EnvelopeNotice meta={item.envelope_meta} text={text} />;
      }
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
          {actionsVisible ? (
            <AgentMessageActions
              getText={() => streamFieldValue(turnID, item, "text")}
              onFork={
                onForkMessage ? () => onForkMessage(turnID, item.id) : undefined
              }
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
      // Collab tool calls carry the acting participant. Prefix the
      // activity line with the identity chip so the reader can tell
      // which subagent the call belongs to. Legacy items without a
      // participant keep the plain row — the summary text already
      // names the task, so a fallback chip would only repeat it.
      if (item.participant) {
        return (
          <div className="collab-agent-activity">
            <ParticipantChip participant={item.participant} size="sm" />
            <ToolActivityRow items={[item]} />
          </div>
        );
      }
      return <ToolActivityRow items={[item]} />;
    case "participant_message": {
      const text = item.text ?? "";
      const postKind = item.post_kind ?? "result";
      if (postKind === "decline") {
        const participantName = item.participant?.name || "参与者";
        return (
          <div className="participant-decline-line">
            {participantName} 认为无需回应{text.trim() ? `：${text.trim()}` : ""}
          </div>
        );
      }
      const agentID = item.agent_id?.trim() ?? "";
      const canOpenAgent = agentID !== "" && Boolean(onOpenAgent);
      const hasText = text.trim() !== "";
      return (
        <article className={`participant-message-card participant-message-card--${postKind}`}>
          <header className="participant-message-card-header">
            {item.participant ? (
              <ParticipantChip participant={item.participant} size="sm" />
            ) : (
              <span className="participant-message-card-fallback">参与者</span>
            )}
          </header>
          {hasText ? (
            <div className="participant-message-card-body">
              <RichContent text={text} cwd={cwd} onOpenFile={onOpenFile} />
            </div>
          ) : null}
          {canOpenAgent ? (
            <div className="participant-message-actions">
              <a
                href={`#${agentID}`}
                className="participant-message-link"
                aria-label="查看完整过程"
                onClick={(event) => {
                  event.preventDefault();
                  onOpenAgent?.(agentID);
                }}
              >
                查看完整过程
              </a>
            </div>
          ) : null}
        </article>
      );
    }
    case "task_card":
      return (
        <TaskCardItem
          item={item}
          onOpenSubthread={
            item.task?.subthread_id ? () => onOpenSubthread?.(item) : undefined
          }
        />
      );
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
        <TurnNotice
          display={userFacingErrorForMessage(item.error, "turn")}
          onAction={onNoticeAction}
        />
      );
    default:
      return null;
  }
}

export function TaskCardItem({
  item,
  onOpenSubthread,
}: {
  item: ThreadItem;
  onOpenSubthread?: () => void;
}): JSX.Element | null {
  const task = item.task;
  if (!task) {
    return null;
  }
  const title = task.name?.trim() || task.id;
  const status = taskStatusLabel(task.status);
  const replyCount = task.reply_count ?? 0;
  return (
    <article className="task-card">
      <header className="task-card-header">
        <div className="task-card-title-group">
          <span className="task-card-title">{title}</span>
          <span className={`task-card-status task-card-status--${task.status || "unknown"}`}>
            {status}
          </span>
        </div>
        {task.participant ? (
          <ParticipantChip participant={task.participant} size="sm" />
        ) : item.participant ? (
          <ParticipantChip participant={item.participant} size="sm" />
        ) : null}
      </header>
      {task.description ? (
        <p className="task-card-description">{task.description}</p>
      ) : null}
      <footer className="task-card-footer">
        {task.role ? <span className="task-card-meta">{task.role}</span> : null}
        {replyCount > 0 ? (
          <span className="task-card-meta">{replyCountBadge(replyCount)} 条回复</span>
        ) : null}
        {onOpenSubthread ? (
          <button
            type="button"
            className="task-card-open-button"
            aria-label="查看过程"
            onClick={onOpenSubthread}
          >
            <PanelRightOpen aria-hidden="true" />
            <span>查看过程</span>
          </button>
        ) : null}
      </footer>
    </article>
  );
}

function taskStatusLabel(status: string): string {
  switch (status) {
    case "pending":
      return "待开始";
    case "queued":
      return "排队中";
    case "running":
      return "进行中";
    case "awaiting_report":
      return "待报告";
    case "completed":
      return "已完成";
    case "failed":
      return "失败";
    case "cancelled":
    case "canceled":
      return "已取消";
    default:
      return status || "未知";
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
  const [expandedState, setExpandedState] = useState({
    text,
    expanded: false,
  });
  const collapsible = isCollapsibleUserMessage(text);
  const expanded =
    collapsible && expandedState.text === text ? expandedState.expanded : false;

  const collapsed = collapsible && !expanded;
  const displayedText = collapsed ? collapsedUserMessagePreview(text) : text;

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
          onClick={() =>
            setExpandedState({
              text,
              expanded: !expanded,
            })
          }
        >
          <span>{expanded ? "收起" : "显示更多"}</span>
          {expanded ? <ChevronUp aria-hidden="true" /> : <ChevronDown aria-hidden="true" />}
        </button>
      ) : null}
    </div>
  );
}

function collapsedUserMessagePreview(text: string): string {
  let estimatedLines = 0;
  const previewLines: string[] = [];

  for (const line of text.split(/\r\n|\r|\n/)) {
    const lineEstimate = Math.max(
      1,
      Math.ceil(line.length / COLLAPSIBLE_USER_MESSAGE_SOFT_LINE_CHARS),
    );
    if (estimatedLines + lineEstimate <= COLLAPSIBLE_USER_MESSAGE_PREVIEW_LINES) {
      previewLines.push(line);
      estimatedLines += lineEstimate;
      continue;
    }

    const remainingLines = COLLAPSIBLE_USER_MESSAGE_PREVIEW_LINES - estimatedLines;
    if (remainingLines > 0) {
      previewLines.push(
        line
          .slice(0, remainingLines * COLLAPSIBLE_USER_MESSAGE_SOFT_LINE_CHARS)
          .trimEnd(),
      );
    }
    break;
  }

  const preview = previewLines.join("\n").trimEnd();
  return preview ? `${preview}...` : "...";
}

function isCollapsibleUserMessage(text: string): boolean {
  if (text.length > COLLAPSIBLE_USER_MESSAGE_CHAR_THRESHOLD) {
    return true;
  }
  let estimatedLines = 0;
  for (const line of text.split(/\r\n|\r|\n/)) {
    estimatedLines += Math.max(
      1,
      Math.ceil(line.length / COLLAPSIBLE_USER_MESSAGE_SOFT_LINE_CHARS),
    );
    if (estimatedLines > COLLAPSIBLE_USER_MESSAGE_LINE_THRESHOLD) {
      return true;
    }
  }
  return false;
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
  onSubmit?: (text: string, images: InputImage[], files: InputFile[]) => void;
}): JSX.Element {
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
      textarea.focus();
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
          className="user-message-edit-attach-button"
          aria-label="添加附件"
          title="添加图片或 PDF"
          disabled={submitting}
          onClick={() => fileInputRef.current?.click()}
        >
          <Paperclip aria-hidden="true" />
        </button>
        <div className="user-message-edit-spacer" />
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
            aria-label="发送"
            title="发送"
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
