import {
  CornerDownRight,
  FileText,
  MoreHorizontal,
  Paperclip,
  Pencil,
  Send,
  Square,
  Trash2,
  X
} from "lucide-react";
import { type KeyboardEvent as ReactKeyboardEvent, useRef, useState } from "react";
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
            <X size={13} />
          </button>
        </div>
      ))}
      {files.map((file, index) => (
        <div className="composer-file-attachment" key={file.id}>
          <FileText size={16} aria-hidden="true" />
          <span>{file.filename?.trim() || `PDF ${index + 1}`}</span>
          <button type="button" aria-label={`移除文件 ${index + 1}`} onClick={() => onRemoveFile(file.id)}>
            <X size={13} />
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
  const statusText = status === "ready" ? "" : status;
  const { resetQueryHistoryNavigation, handleQueryHistoryKeyDown } = useComposerQueryHistory({
    disabled: readOnly || hasAttachments,
    prompt,
    queryHistory,
    queryHistorySessionID,
    setPrompt,
    textareaRef
  });

  function handleKeyDown(event: ReactKeyboardEvent<HTMLTextAreaElement>): void {
    if (readOnly || isComposerTextComposing(event)) {
      return;
    }
    if (handleQueryHistoryKeyDown(event)) {
      return;
    }
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      resetQueryHistoryNavigation();
      onSend();
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
              onClick={onSend}
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
  const total = guideMessages.length + queuedMessages.length;
  if (total === 0) {
    return null;
  }

  return (
    <div className="composer-queue-strip" aria-label="待发送消息">
      <div className="composer-queue-items">
        {guideMessages.map((message) => (
          <ComposerQueueItem
            key={message.id}
            message={message}
            kind="guide"
            onEdit={() => onEditGuideMessage(message.id)}
            onRemove={() => onRemoveGuideMessage(message.id)}
          />
        ))}
        {queuedMessages.map((message) => (
          <ComposerQueueItem
            key={message.id}
            message={message}
            kind="queue"
            onGuide={() => onGuideQueuedMessage(message.id)}
            onEdit={() => onEditQueuedMessage(message.id)}
            onRemove={() => onRemoveQueuedMessage(message.id)}
          />
        ))}
      </div>
    </div>
  );
}

function ComposerQueueItem({
  message,
  kind,
  onGuide,
  onEdit,
  onRemove
}: {
  message: QueuedComposerMessage;
  kind: "guide" | "queue";
  onGuide?: () => void;
  onEdit: () => void;
  onRemove: () => void;
}): JSX.Element {
  const [menuOpen, setMenuOpen] = useState(false);
  const closeLabel = kind === "guide" ? "关闭引导" : "关闭排队";

  return (
    <div className={`composer-queue-item ${kind}`}>
      <CornerDownRight className="composer-queue-corner" size={18} aria-hidden="true" />
      <strong>{queuedMessagePreview(message)}</strong>
      {kind === "guide" ? (
        <span className="composer-queue-guide active">
          <CornerDownRight size={16} aria-hidden="true" />
          引导
        </span>
      ) : (
        <button className="composer-queue-guide" type="button" aria-label="作为引导发送" onClick={onGuide}>
          <CornerDownRight size={16} aria-hidden="true" />
          <span>引导</span>
        </button>
      )}
      <button
        className="composer-queue-icon"
        type="button"
        aria-label="移除待发送消息"
        onClick={onRemove}
      >
        <Trash2 size={16} />
      </button>
      <div className="composer-queue-menu-anchor">
        <button
          className="composer-queue-icon"
          type="button"
          aria-label="待发送消息操作"
          aria-haspopup="menu"
          aria-expanded={menuOpen}
          onClick={() => setMenuOpen((open) => !open)}
        >
          <MoreHorizontal size={18} />
        </button>
        {menuOpen ? (
          <div className="composer-queue-menu" role="menu">
            <button
              type="button"
              role="menuitem"
              onClick={() => {
                setMenuOpen(false);
                onEdit();
              }}
            >
              <Pencil size={16} aria-hidden="true" />
              <span>编辑消息</span>
            </button>
            <button
              type="button"
              role="menuitem"
              onClick={() => {
                setMenuOpen(false);
                onRemove();
              }}
            >
              <CornerDownRight size={16} aria-hidden="true" />
              <span>{closeLabel}</span>
            </button>
          </div>
        ) : null}
      </div>
    </div>
  );
}
