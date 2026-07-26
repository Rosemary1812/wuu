import {
  ChevronDown,
  ChevronUp,
  CornerDownRight,
  CornerUpLeft,
  FileText,
  ListTodo,
  Paperclip,
  PencilLine,
  Send,
  Square,
  X
} from "lucide-react";
import { type DragEvent as ReactDragEvent, type KeyboardEvent as ReactKeyboardEvent, useEffect, useId, useRef, useState } from "react";
import { useOptionalImagePreview } from "./ImagePreview";
import { isComposerTextComposing } from "./ComposerSlashCommands";
import {
  WORKSPACE_FILE_DRAG_MIME,
  appendWorkspacePathToPrompt,
  clipboardAttachmentFiles,
  imageSource,
  queuedMessageFullPreview,
  queuedMessagePreview,
  type ComposerFile,
  type ComposerImage,
  type QueuedComposerMessage
} from "./ComposerMessages";
import { useComposerQueryHistory } from "./ComposerQueryHistory";
import { composerStatusIsLiveProgress, composerStatusText } from "./ComposerTypes";
import { useI18n } from "./i18n";
import { Tooltip } from "./Tooltip";
import { TruncatedText } from "./TruncatedText";

export function ComposerAttachmentStrip({
  files,
  images,
  onRemoveFile,
  onRemoveImage,
  removable = true
}: {
  files: ComposerFile[];
  images: ComposerImage[];
  onRemoveFile?: (id: string) => void;
  onRemoveImage?: (id: string) => void;
  removable?: boolean;
}): JSX.Element | null {
  const { t } = useI18n();
  const imagePreview = useOptionalImagePreview();
  if (images.length === 0 && files.length === 0) {
    return null;
  }
  return (
    <div className="composer-attachments">
      {images.map((image, index) => {
        const src = imageSource(image);
        const label = t("composer.imageNumber", { number: index + 1 });
        const handleOpen = (): void => {
          imagePreview?.openPreview({ src, alt: label, title: label });
        };
        const handleKeyDown = (event: ReactKeyboardEvent<HTMLImageElement>): void => {
          if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            handleOpen();
          }
        };
        return (
          <div className="composer-image-attachment" key={image.id}>
            <img
              src={src}
              alt={label}
              role="button"
              tabIndex={0}
              aria-label={t("composer.enlargeNamed", { name: label })}
              onClick={handleOpen}
              onKeyDown={handleKeyDown}
            />
            {removable ? (
              <button
                type="button"
                aria-label={t("composer.removeImage", { number: index + 1 })}
                onClick={(event) => {
                  event.stopPropagation();
                  onRemoveImage?.(image.id);
                }}
              >
                <X className="icon-xs" />
              </button>
            ) : null}
          </div>
        );
      })}
      {files.map((file, index) => (
        <div className="composer-file-attachment" key={file.id}>
          <FileText className="icon" aria-hidden="true" />
          <span>{file.filename?.trim() || t("composer.pdfNumber", { number: index + 1 })}</span>
          {removable ? (
            <button type="button" aria-label={t("composer.removeFile", { number: index + 1 })} onClick={() => onRemoveFile?.(file.id)}>
              <X className="icon-xs" />
            </button>
          ) : null}
        </div>
      ))}
    </div>
  );
}

export function SplitPaneComposer({
  prompt,
  setPrompt,
  files,
  images,
  running,
  readOnly,
  status,
  statusLiveProgress,
  queryHistorySessionID,
  queryHistory = [],
  onPasteAttachmentFiles,
  onRemoveFile,
  onRemoveImage,
  onSend,
  onInterrupt
}: {
  prompt: string;
  setPrompt: (value: string) => void;
  files: ComposerFile[];
  images: ComposerImage[];
  running: boolean;
  readOnly: boolean;
  status: string;
  statusLiveProgress?: boolean;
  queryHistorySessionID?: string;
  queryHistory?: string[];
  onPasteAttachmentFiles: (files: File[]) => void;
  onRemoveFile: (id: string) => void;
  onRemoveImage: (id: string) => void;
  onSend: () => void;
  onInterrupt: () => void;
}): JSX.Element {
  const { t } = useI18n();
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const attachmentInputRef = useRef<HTMLInputElement>(null);
  const [dropActive, setDropActive] = useState(false);
  const hasAttachments = images.length > 0 || files.length > 0;
  const hasDraft = prompt.trim().length > 0 || hasAttachments;
  // Match the dock composer: the button is a stop control only while running
  // with an empty input. Once there is a draft, it flips to send (queuing
  // mid-turn) so a typed follow-up is never blocked by the stop state.
  const showStop = running && !hasDraft;
  const sendLabel = running ? t("composer.queueSend") : t("composer.send");
  const statusText = composerStatusText(status);
  const statusIsLiveProgress = composerStatusIsLiveProgress(statusLiveProgress);
  const { resetQueryHistoryNavigation, handleQueryHistoryKeyDown } = useComposerQueryHistory({
    disabled: readOnly || hasAttachments,
    prompt,
    queryHistory,
    queryHistorySessionID,
    setPrompt,
    textareaRef
  });

  function focusComposerSoon(): void {
    window.requestAnimationFrame(() => textareaRef.current?.focus());
  }

  function focusComposerAtEndSoon(): void {
    window.requestAnimationFrame(() => {
      const textarea = textareaRef.current;
      if (!textarea) {
        return;
      }
      textarea.focus();
      const end = textarea.value.length;
      textarea.setSelectionRange(end, end);
    });
  }

  // Mirrors the dock composer: a drop carries either a workspace path
  // reference (inserted as plain text) or external files (forwarded to the
  // attachment pipeline). Anything else keeps its native behavior.
  function handleDragOver(event: ReactDragEvent<HTMLDivElement>): void {
    if (readOnly) {
      return;
    }
    const types = Array.from(event.dataTransfer.types ?? []);
    if (!types.includes(WORKSPACE_FILE_DRAG_MIME) && !types.includes("Files")) {
      return;
    }
    event.preventDefault();
    event.dataTransfer.dropEffect = "copy";
    setDropActive(true);
  }

  function handleDragLeave(event: ReactDragEvent<HTMLDivElement>): void {
    if (event.currentTarget.contains(event.relatedTarget as Node | null)) {
      return;
    }
    setDropActive(false);
  }

  function handleDrop(event: ReactDragEvent<HTMLDivElement>): void {
    setDropActive(false);
    if (readOnly) {
      return;
    }
    const workspacePath = event.dataTransfer.getData(WORKSPACE_FILE_DRAG_MIME);
    if (workspacePath) {
      event.preventDefault();
      resetQueryHistoryNavigation();
      setPrompt(appendWorkspacePathToPrompt(prompt, workspacePath));
      focusComposerAtEndSoon();
      return;
    }
    const dropped = Array.from(event.dataTransfer.files ?? []);
    if (dropped.length === 0) {
      return;
    }
    event.preventDefault();
    onPasteAttachmentFiles(dropped);
  }

  function submitComposer(): void {
    resetQueryHistoryNavigation();
    onSend();
    focusComposerSoon();
  }

  function handleKeyDown(event: ReactKeyboardEvent<HTMLTextAreaElement>): void {
    if (readOnly) {
      return;
    }
    if (isComposerTextComposing(event)) {
      return;
    }
    if (handleQueryHistoryKeyDown(event)) {
      return;
    }
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      submitComposer();
    }
  }

  return (
    <footer className="split-composer">
      <div
        className={`split-composer-shell${dropActive ? " split-composer-shell-drop-active" : ""}`}
        onDragOver={handleDragOver}
        onDragLeave={handleDragLeave}
        onDrop={handleDrop}
      >
        <ComposerAttachmentStrip files={files} images={images} onRemoveFile={onRemoveFile} onRemoveImage={onRemoveImage} />
        <input
          ref={attachmentInputRef}
          className="composer-file-input"
          type="file"
          accept="image/*,application/pdf"
          multiple
          tabIndex={-1}
          onChange={(event) => {
            const selected = Array.from(event.currentTarget.files ?? []);
            event.currentTarget.value = "";
            if (selected.length > 0) {
              onPasteAttachmentFiles(selected);
            }
          }}
        />
        <textarea
          ref={textareaRef}
          value={prompt}
          placeholder={readOnly ? t("composer.readOnly") : hasAttachments ? t("composer.addDescription") : t("composer.continueBranch")}
          disabled={readOnly}
          aria-readonly={readOnly}
          onChange={(event) => {
            resetQueryHistoryNavigation();
            setPrompt(event.target.value);
          }}
          onPaste={(event) => {
            if (readOnly) {
              return;
            }
            const pasted = clipboardAttachmentFiles(event);
            if (pasted.length === 0) {
              return;
            }
            event.preventDefault();
            onPasteAttachmentFiles(pasted);
          }}
          onKeyDown={handleKeyDown}
        />
        <div className="split-composer-bar">
          <button
            className="composer-action-button composer-attach-button"
            type="button"
            aria-label={t("composer.addAttachment")}
            title={t("composer.addAttachment")}
            disabled={readOnly}
            onClick={() => attachmentInputRef.current?.click()}
          >
            <Paperclip aria-hidden="true" />
          </button>
          {statusText ? (
            <span className="split-composer-status">
              <TruncatedText
                className={`split-composer-status-text${statusIsLiveProgress ? " live-progress-chip" : ""}`}
                text={statusText}
              />
            </span>
          ) : (
            <span />
          )}
          {showStop ? (
            <button
              className="composer-action-button composer-stop-button"
              type="button"
              onClick={onInterrupt}
              aria-label={t("composer.stop")}
              title={t("composer.stop")}
            >
              <Square aria-hidden="true" />
            </button>
          ) : (
            <button
              className="composer-action-button composer-send-button"
              type="button"
              onClick={submitComposer}
              aria-label={sendLabel}
              title={sendLabel}
              disabled={readOnly || !hasDraft}
            >
              <Send aria-hidden="true" />
            </button>
          )}
        </div>
      </div>
    </footer>
  );
}

type QueueRowKind = "guide" | "queue";

type QueueRowEntry = {
  key: string;
  message: QueuedComposerMessage;
  kind: QueueRowKind;
};

function buildQueueRows(
  guideMessages: QueuedComposerMessage[],
  queuedMessages: QueuedComposerMessage[]
): QueueRowEntry[] {
  return [
    ...guideMessages.map((message) => ({
      key: `guide-${message.id}`,
      message,
      kind: "guide" as const
    })),
    ...queuedMessages.map((message) => ({
      key: `queue-${message.id}`,
      message,
      kind: "queue" as const
    }))
  ].sort((left, right) => {
    const leftPosition = left.message.heldPosition;
    const rightPosition = right.message.heldPosition;
    if (leftPosition !== undefined && rightPosition !== undefined) {
      return leftPosition - rightPosition;
    }
    if (leftPosition !== undefined) return -1;
    if (rightPosition !== undefined) return 1;
    return 0;
  });
}

export function ComposerQueueStrip({
  guideMessages,
  queuedMessages,
  expanded: controlledExpanded,
  onExpandedChange,
  onRemoveGuideMessage,
  onRemoveQueuedMessage,
  onGuideQueuedMessage,
  onEditGuideMessage,
  onEditQueuedMessage
}: {
  guideMessages: QueuedComposerMessage[];
  queuedMessages: QueuedComposerMessage[];
  expanded?: boolean;
  onExpandedChange?: (expanded: boolean) => void;
  onRemoveGuideMessage: (id: string) => void;
  onRemoveQueuedMessage: (id: string) => void;
  onGuideQueuedMessage: (id: string) => void;
  onEditGuideMessage: (id: string) => void;
  onEditQueuedMessage: (id: string) => void;
}): JSX.Element | null {
  const { t } = useI18n();
  const [internalExpanded, setInternalExpanded] = useState(false);
  const pendingDetailsID = useId();
  const expanded = controlledExpanded ?? internalExpanded;
  const rows = buildQueueRows(guideMessages, queuedMessages);
  if (rows.length === 0) {
    return null;
  }

  const hasHeldMessages = rows.some((row) => row.message.held);
  const latestMessage = rows.at(-1)?.message;
  const detailsID = `composer-pending-message-details-${pendingDetailsID}`;

  function setExpanded(next: boolean): void {
    if (controlledExpanded === undefined) {
      setInternalExpanded(next);
    }
    onExpandedChange?.(next);
  }

  return (
    <section
      className={`composer-pending-drawer composer-accessory-drawer${expanded ? " expanded" : ""}${hasHeldMessages ? " is-held" : ""}`}
    >
      <div className="composer-pending-summary">
        <button
          type="button"
          className="composer-pending-summary-select composer-drawer-summary-select"
          aria-controls={detailsID}
          aria-expanded={expanded}
          onClick={() => setExpanded(!expanded)}
        >
          <span className="composer-pending-icon" aria-hidden="true">
            <ListTodo className="icon-sm" />
          </span>
          <Tooltip
            content={latestMessage ? queuedMessageFullPreview(latestMessage) : undefined}
            disabled={
              expanded ||
              !latestMessage ||
              queuedMessageFullPreview(latestMessage) === queuedMessagePreview(latestMessage)
            }
          >
            <span
              className="composer-pending-preview"
              role="status"
              aria-live="polite"
            >
              {expanded
                ? hasHeldMessages
                  ? t("composer.heldNotice")
                  : t("composer.pendingMessages")
                : latestMessage
                  ? queuedMessagePreview(latestMessage)
                  : ""}
            </span>
          </Tooltip>
        </button>
        <button
          type="button"
          className="composer-pending-toggle composer-input-header-action"
          aria-controls={detailsID}
          aria-expanded={expanded}
          aria-label={t(expanded ? "composer.collapsePending" : "composer.expandPending")}
          title={t(expanded ? "composer.collapsePending" : "composer.expandPending")}
          onClick={() => setExpanded(!expanded)}
        >
          {expanded ? (
            <ChevronDown className="icon-sm" aria-hidden="true" />
          ) : (
            <ChevronUp className="icon-sm" aria-hidden="true" />
          )}
        </button>
      </div>
      {expanded ? (
        <div className="composer-pending-details" id={detailsID}>
          <ol className="composer-queue-list" aria-label={t("composer.pendingMessages")}>
            {rows.map((row, index) => (
              <ComposerQueueItem
                key={row.key}
                position={index + 1}
                message={row.message}
                kind={row.kind}
                onGuide={
                  row.kind === "queue" || row.message.held
                    ? () => onGuideQueuedMessage(row.message.id)
                    : undefined
                }
                onEdit={() =>
                  row.kind === "queue"
                    ? onEditQueuedMessage(row.message.id)
                    : onEditGuideMessage(row.message.id)
                }
                onRemove={() =>
                  row.kind === "queue"
                    ? onRemoveQueuedMessage(row.message.id)
                    : onRemoveGuideMessage(row.message.id)
                }
              />
            ))}
          </ol>
        </div>
      ) : null}
    </section>
  );
}

function ComposerQueueItem({
  position,
  message,
  kind,
  onGuide,
  onEdit,
  onRemove
}: {
  position: number;
  message: QueuedComposerMessage;
  kind: QueueRowKind;
  onGuide?: () => void;
  onEdit: () => void;
  onRemove: () => void;
}): JSX.Element {
  const { t } = useI18n();
  const editContentLabel = kind === "guide"
    ? t("composer.editGuideContent", { position })
    : t("composer.editQueuedContent", { position });
  const editLabel = kind === "guide"
    ? t("composer.editGuide", { position })
    : t("composer.editQueuedMessage", { position });
  return (
    <li className={`composer-queue-row ${kind}`} data-position={position}>
      <span className="composer-queue-index" aria-hidden="true">
        {position}
      </span>
      <Tooltip
        content={queuedMessageFullPreview(message)}
        disabled={queuedMessageFullPreview(message) === queuedMessagePreview(message)}
      >
        <button
          type="button"
          className="composer-queue-preview"
          aria-label={editContentLabel}
          onClick={onEdit}
        >
          {queuedMessagePreview(message)}
        </button>
      </Tooltip>
      <div className="composer-queue-actions composer-input-header-actions">
        <button
          type="button"
          className="composer-queue-action composer-input-header-action edit"
          aria-label={editLabel}
          title={t("common.edit")}
          onClick={onEdit}
        >
          <PencilLine className="icon-sm" aria-hidden="true" />
        </button>
        {onGuide ? (
          <button
            type="button"
            className="composer-queue-action composer-input-header-action"
            aria-label={
              message.held
                ? t("composer.continueHeld", { position })
                : t("composer.convertToGuide", { position })
            }
            title={
              message.held
                ? t("composer.continueHeldTitle")
                : t("composer.convertToGuideTitle")
            }
            onClick={onGuide}
          >
            <CornerDownRight className="icon-sm" aria-hidden="true" />
          </button>
        ) : kind === "guide" ? (
          <button
            type="button"
            className="composer-queue-action composer-input-header-action"
            aria-label={t("composer.cancelGuide", { position })}
            title={t("composer.cancelGuideTitle")}
            onClick={onRemove}
          >
            <CornerUpLeft className="icon-sm" aria-hidden="true" />
          </button>
        ) : null}
        <button
          type="button"
          className="composer-queue-action composer-input-header-action danger"
          aria-label={t("composer.removeQueuedMessage", { position })}
          title={t("common.remove")}
          onClick={onRemove}
        >
          <X className="icon-sm" aria-hidden="true" />
        </button>
      </div>
    </li>
  );
}
