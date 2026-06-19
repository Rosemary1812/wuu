import {
  CornerDownRight,
  FileText,
  MoreHorizontal,
  Paperclip,
  Pencil,
  Send,
  Square,
  X
} from "lucide-react";
import { type KeyboardEvent as ReactKeyboardEvent, useEffect, useRef, useState } from "react";
import { isComposerTextComposing } from "./ComposerSlashCommands";
import {
  clipboardAttachmentFiles,
  imageSource,
  queuedMessagePreview,
  type ComposerFile,
  type ComposerImage,
  type QueuedComposerMessage
} from "./ComposerMessages";
import { useComposerQueryHistory } from "./ComposerQueryHistory";
import { composerStatusText } from "./ComposerTypes";

export function ComposerAttachmentStrip({
  files,
  images,
  onRemoveFile,
  onRemoveImage
}: {
  files: ComposerFile[];
  images: ComposerImage[];
  onRemoveFile: (id: string) => void;
  onRemoveImage: (id: string) => void;
}): JSX.Element | null {
  if (images.length === 0 && files.length === 0) {
    return null;
  }
  return (
    <div className="composer-attachments">
      {images.map((image, index) => (
        <div className="composer-image-attachment" key={image.id}>
          <img src={imageSource(image)} alt={`Image ${index + 1}`} />
          <button type="button" aria-label={`移除图片 ${index + 1}`} onClick={() => onRemoveImage(image.id)}>
            <X className="icon-xs" />
          </button>
        </div>
      ))}
      {files.map((file, index) => (
        <div className="composer-file-attachment" key={file.id}>
          <FileText className="icon" aria-hidden="true" />
          <span>{file.filename?.trim() || `PDF ${index + 1}`}</span>
          <button type="button" aria-label={`移除文件 ${index + 1}`} onClick={() => onRemoveFile(file.id)}>
            <X className="icon-xs" />
          </button>
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
  queryHistorySessionID,
  queryHistory = [],
  focusRequest = 0,
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
  queryHistorySessionID?: string;
  queryHistory?: string[];
  focusRequest?: number;
  onPasteAttachmentFiles: (files: File[]) => void;
  onRemoveFile: (id: string) => void;
  onRemoveImage: (id: string) => void;
  onSend: () => void;
  onInterrupt: () => void;
}): JSX.Element {
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const attachmentInputRef = useRef<HTMLInputElement>(null);
  const hasAttachments = images.length > 0 || files.length > 0;
  const hasDraft = prompt.trim().length > 0 || hasAttachments;
  const statusText = composerStatusText(status);
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

  useEffect(() => {
    if (focusRequest > 0 && !readOnly) {
      focusComposerSoon();
    }
  }, [focusRequest, readOnly]);

  function submitComposer(): void {
    resetQueryHistoryNavigation();
    onSend();
    focusComposerSoon();
  }

  function handleKeyDown(event: ReactKeyboardEvent<HTMLTextAreaElement>): void {
    if (readOnly || isComposerTextComposing(event)) {
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
      <div className="split-composer-shell">
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
          placeholder={readOnly ? "子任务会话只读" : hasAttachments ? "添加描述" : "继续这个分支"}
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
            aria-label="添加附件"
            title="添加附件"
            disabled={readOnly}
            onClick={() => attachmentInputRef.current?.click()}
          >
            <Paperclip aria-hidden="true" />
          </button>
          {statusText ? <span className="split-composer-status">{statusText}</span> : <span />}
          {running ? (
            <button
              className="composer-action-button composer-stop-button"
              type="button"
              onClick={onInterrupt}
              aria-label="停止"
              title="停止"
            >
              <Square aria-hidden="true" />
            </button>
          ) : (
            <button
              className="composer-action-button composer-send-button"
              type="button"
              onClick={submitComposer}
              aria-label="发送"
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
  ];
}

export function ComposerQueueStrip({
  guideMessages,
  queuedMessages,
  onRemoveGuideMessage,
  onRemoveQueuedMessage,
  onGuideQueuedMessage,
  onEditGuideMessage,
  onEditQueuedMessage
}: {
  guideMessages: QueuedComposerMessage[];
  queuedMessages: QueuedComposerMessage[];
  onRemoveGuideMessage: (id: string) => void;
  onRemoveQueuedMessage: (id: string) => void;
  onGuideQueuedMessage: (id: string) => void;
  onEditGuideMessage: (id: string) => void;
  onEditQueuedMessage: (id: string) => void;
}): JSX.Element | null {
  const rows = buildQueueRows(guideMessages, queuedMessages);
  if (rows.length === 0) {
    return null;
  }

  return (
    <ol className="composer-queue-list" aria-label="待发送消息">
      {rows.map((row, index) => (
        <ComposerQueueItem
          key={row.key}
          position={index + 1}
          message={row.message}
          kind={row.kind}
          onGuide={
            row.kind === "queue" ? () => onGuideQueuedMessage(row.message.id) : undefined
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
  const [menuOpen, setMenuOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);
  const menuButtonRef = useRef<HTMLButtonElement>(null);

  // Close on outside click (mousedown) and on Escape. The menu button itself
  // stays in the "inside" set so the toggle click doesn't race against this
  // listener; the toggle click handler still owns the open/close flip.
  useEffect(() => {
    if (!menuOpen) {
      return;
    }
    function handlePointerDown(event: MouseEvent): void {
      const target = event.target as Node | null;
      if (menuRef.current?.contains(target ?? null)) {
        return;
      }
      if (menuButtonRef.current?.contains(target ?? null)) {
        return;
      }
      setMenuOpen(false);
    }
    function handleKey(event: KeyboardEvent): void {
      if (event.key === "Escape") {
        setMenuOpen(false);
        menuButtonRef.current?.focus();
      }
    }
    document.addEventListener("mousedown", handlePointerDown);
    document.addEventListener("keydown", handleKey);
    return () => {
      document.removeEventListener("mousedown", handlePointerDown);
      document.removeEventListener("keydown", handleKey);
    };
  }, [menuOpen]);

  return (
    <li className={`composer-queue-row ${kind}`} data-position={position}>
      <span className="composer-queue-index" aria-hidden="true">
        {position}
      </span>
      <button
        type="button"
        className="composer-queue-preview"
        aria-label={`编辑排队消息 ${position}`}
        title={message.text}
        onClick={onEdit}
      >
        {queuedMessagePreview(message)}
      </button>
      <div className="composer-queue-actions">
        <button
          type="button"
          className="composer-queue-action danger"
          aria-label={`移除排队消息 ${position}`}
          onClick={onRemove}
        >
          <X className="icon-sm" aria-hidden="true" />
        </button>
        <div className="composer-queue-menu-anchor">
          <button
            ref={menuButtonRef}
            type="button"
            className="composer-queue-action"
            aria-label="待发送消息操作"
            aria-haspopup="menu"
            aria-expanded={menuOpen}
            onClick={() => setMenuOpen((open) => !open)}
          >
            <MoreHorizontal className="icon-sm" aria-hidden="true" />
          </button>
          {menuOpen ? (
            <div ref={menuRef} className="composer-queue-menu" role="menu">
              <button
                type="button"
                role="menuitem"
                onClick={() => {
                  setMenuOpen(false);
                  onEdit();
                }}
              >
                <Pencil className="icon-sm" aria-hidden="true" />
                <span>编辑消息</span>
              </button>
              {onGuide ? (
                <button
                  type="button"
                  role="menuitem"
                  onClick={() => {
                    setMenuOpen(false);
                    onGuide();
                  }}
                >
                  <CornerDownRight className="icon-sm" aria-hidden="true" />
                  <span>转为引导</span>
                </button>
              ) : null}
              <button
                type="button"
                role="menuitem"
                onClick={() => {
                  setMenuOpen(false);
                  onRemove();
                }}
              >
                <X className="icon-sm" aria-hidden="true" />
                <span>{kind === "guide" ? "取消引导" : "移除"}</span>
              </button>
            </div>
          ) : null}
        </div>
      </div>
    </li>
  );
}
