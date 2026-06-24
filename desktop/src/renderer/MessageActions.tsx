import { AlertCircle, Check, Copy, FileText, GitFork, PencilLine, ThumbsDown, ThumbsUp, X } from "lucide-react";
import { type KeyboardEvent as ReactKeyboardEvent, useEffect, useRef, useState } from "react";
import type { InputFile, InputImage } from "../shared/protocol";
import { imageSource } from "./ComposerMessages";
import { useImagePreview } from "./ImagePreview";

export function AgentMessageActions({ getText, onFork }: { getText: () => string; onFork?: () => void }): JSX.Element {
  const [feedback, setFeedback] = useState<"liked" | "disliked" | null>(null);

  return (
    <div className="message-actions agent-message-actions" aria-label="助手消息操作">
      <MessageCopyButton getText={getText} className="message-action-button" iconSize={15} />
      <button
        className="message-action-button"
        type="button"
        aria-label="赞"
        aria-pressed={feedback === "liked"}
        title="赞"
        onClick={() => setFeedback((current) => (current === "liked" ? null : "liked"))}
      >
        <ThumbsUp className="icon" />
      </button>
      <button
        className="message-action-button"
        type="button"
        aria-label="踩"
        aria-pressed={feedback === "disliked"}
        title="踩"
        onClick={() => setFeedback((current) => (current === "disliked" ? null : "disliked"))}
      >
        <ThumbsDown className="icon" />
      </button>
      <button className="message-action-button" type="button" aria-label="分叉" title="分叉" disabled={!onFork} onClick={onFork}>
        <GitFork className="icon" />
      </button>
    </div>
  );
}

export function MessageCopyButton({
  getText,
  className = "",
  iconSize = 14,
  idleLabel = "复制消息",
  copiedLabel = "已复制消息",
  failedLabel = "复制失败"
}: {
  getText: () => string;
  className?: string;
  iconSize?: number;
  idleLabel?: string;
  copiedLabel?: string;
  failedLabel?: string;
}): JSX.Element {
  const [copyState, setCopyState] = useState<"idle" | "copied" | "failed">("idle");
  const resetTimerRef = useRef<number | undefined>(undefined);
  const label = copyState === "copied" ? copiedLabel : copyState === "failed" ? failedLabel : idleLabel;

  useEffect(() => {
    return () => {
      if (resetTimerRef.current !== undefined) {
        window.clearTimeout(resetTimerRef.current);
      }
    };
  }, []);

  function showCopyState(nextState: "copied" | "failed"): void {
    if (resetTimerRef.current !== undefined) {
      window.clearTimeout(resetTimerRef.current);
    }
    setCopyState(nextState);
    resetTimerRef.current = window.setTimeout(() => {
      setCopyState("idle");
      resetTimerRef.current = undefined;
    }, 1200);
  }

  async function handleCopy(): Promise<void> {
    const text = getText();
    if (text.trim() === "") {
      showCopyState("failed");
      return;
    }
    try {
      const clipboard = navigator.clipboard;
      if (!clipboard?.writeText) {
        throw new Error("Clipboard API unavailable");
      }
      await clipboard.writeText(text);
      showCopyState("copied");
    } catch {
      showCopyState("failed");
    }
  }

  return (
    <button
      className={`message-copy-button ${className} ${copyState}`}
      type="button"
      aria-label={label}
      title={label}
      onClick={() => void handleCopy()}
    >
      {copyState === "copied" ? (
        <Check size={iconSize} />
      ) : copyState === "failed" ? (
        <AlertCircle size={iconSize} />
      ) : (
        <Copy size={iconSize} />
      )}
    </button>
  );
}

export function MessageEditButton({
  onEdit,
  className = "",
  iconSize = 14
}: {
  onEdit: () => void;
  className?: string;
  iconSize?: number;
}): JSX.Element {
  return (
    <button
      className={`message-edit-button ${className}`}
      type="button"
      aria-label="编辑并重试"
      title="编辑并重试"
      onClick={onEdit}
    >
      <PencilLine size={iconSize} />
    </button>
  );
}

export function MessageImageGrid({
  images,
  onRemove,
}: {
  images: InputImage[];
  /** When provided, each image gets a remove button (used inside the inline editor). */
  onRemove?: (index: number) => void;
}): JSX.Element {
  const { openPreview } = useImagePreview();
  return (
    <div className={`message-images${onRemove ? " message-images-editable" : ""}`}>
      {images.map((image, index) => {
        const src = imageSource(image);
        const label = `图片 ${index + 1}`;
        const handleOpen = (): void => {
          openPreview({ src, alt: label, title: label });
        };
        const handleKeyDown = (event: ReactKeyboardEvent<HTMLImageElement>): void => {
          if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            handleOpen();
          }
        };
        return (
          <div className="message-image-frame" key={`${image.media_type}-${index}`}>
            <img
              className="message-image"
              src={src}
              alt={label}
              role="button"
              tabIndex={0}
              aria-label={`放大查看：${label}`}
              onClick={handleOpen}
              onKeyDown={handleKeyDown}
            />
            {onRemove ? (
              <button
                type="button"
                className="message-image-remove"
                aria-label={`移除图片 ${index + 1}`}
                title="移除"
                onClick={(event) => {
                  event.stopPropagation();
                  onRemove(index);
                }}
              >
                <X size={12} aria-hidden="true" />
              </button>
            ) : null}
          </div>
        );
      })}
    </div>
  );
}

export function MessageFileList({
  files,
  onRemove,
}: {
  files: InputFile[];
  /** When provided, each file gets a remove button (used inside the inline editor). */
  onRemove?: (index: number) => void;
}): JSX.Element {
  return (
    <div className={`message-files${onRemove ? " message-files-editable" : ""}`}>
      {files.map((file, index) => (
        <div className="message-file-frame" key={`${file.media_type}-${file.filename ?? index}-${index}`}>
          <div className="message-file">
            <FileText className="icon" aria-hidden="true" />
            <span>{file.filename?.trim() || `File ${index + 1}`}</span>
          </div>
          {onRemove ? (
            <button
              type="button"
              className="message-file-remove"
              aria-label={`移除文件 ${file.filename?.trim() || index + 1}`}
              title="移除"
              onClick={() => onRemove(index)}
            >
              <X size={12} aria-hidden="true" />
            </button>
          ) : null}
        </div>
      ))}
    </div>
  );
}
