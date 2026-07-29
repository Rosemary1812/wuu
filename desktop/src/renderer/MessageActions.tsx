import { AlertCircle, Check, Copy, FileText, GitFork, PencilLine, SquareTerminal, ThumbsDown, ThumbsUp, X } from "lucide-react";
import { type KeyboardEvent as ReactKeyboardEvent, useEffect, useRef, useState } from "react";
import type { InputFile, InputImage } from "../shared/protocol";
import { imageSource } from "./ComposerMessages";
import { useImagePreview } from "./ImagePreview";
import { useI18n } from "./i18n";

export function AgentMessageActions({
  getText,
  onFork,
  onOpenRuns,
}: {
  getText: () => string;
  onFork?: () => void;
  onOpenRuns?: () => void;
}): JSX.Element {
  const { t } = useI18n();
  const [feedback, setFeedback] = useState<"liked" | "disliked" | null>(null);
  // `pulse` is a transient state used to retrigger the icon bounce/shake keyframes
  // and to render the floating "+1" / "−1" burst above the clicked button. It is
  // cleared after the animation duration so the same animation can play again on
  // the next click.
  const [pulse, setPulse] = useState<"liked" | "disliked" | null>(null);
  // `clickToken` is bumped on every feedback click so the icon and burst spans
  // can be keyed against it. This forces React to remount them on each click,
  // which restarts the CSS keyframe animations even if the user clicks the
  // same button twice in a row while the `pulse` class is still applied.
  const [clickToken, setClickToken] = useState(0);
  const pulseTimerRef = useRef<number | undefined>(undefined);

  useEffect(() => {
    return () => {
      if (pulseTimerRef.current !== undefined) {
        window.clearTimeout(pulseTimerRef.current);
      }
    };
  }, []);

  function handleFeedbackClick(kind: "liked" | "disliked"): void {
    const next = feedback === kind ? null : kind;
    setFeedback(next);
    if (pulseTimerRef.current !== undefined) {
      window.clearTimeout(pulseTimerRef.current);
    }
    setPulse(kind);
    setClickToken((t) => t + 1);
    pulseTimerRef.current = window.setTimeout(() => {
      setPulse(null);
      pulseTimerRef.current = undefined;
    }, 700);
  }

  return (
    <div className="message-actions agent-message-actions" aria-label={t("message.assistantActions")}>
      <MessageCopyButton getText={getText} className="message-action-button" iconSize={15} />
      {onOpenRuns ? <TurnRunButton onOpenRuns={onOpenRuns} /> : null}
      <button
        className={`message-action-button message-action-button--like${
          feedback === "liked" ? " is-active" : ""
        }${pulse === "liked" ? " is-pulsing" : ""}`}
        type="button"
        aria-label={t("message.like")}
        aria-pressed={feedback === "liked"}
        title={t("message.like")}
        onClick={() => handleFeedbackClick("liked")}
      >
        <ThumbsUp key={`thumbs-up-${clickToken}`} className="icon" />
        {pulse === "liked" ? (
          <span
            key={`burst-like-${clickToken}`}
            className="message-action-burst message-action-burst--like"
            aria-hidden="true"
          >
            +1
          </span>
        ) : null}
      </button>
      <button
        className={`message-action-button message-action-button--dislike${
          feedback === "disliked" ? " is-active" : ""
        }${pulse === "disliked" ? " is-pulsing" : ""}`}
        type="button"
        aria-label={t("message.dislike")}
        aria-pressed={feedback === "disliked"}
        title={t("message.dislike")}
        onClick={() => handleFeedbackClick("disliked")}
      >
        <ThumbsDown key={`thumbs-down-${clickToken}`} className="icon" />
        {pulse === "disliked" ? (
          <span
            key={`burst-dislike-${clickToken}`}
            className="message-action-burst message-action-burst--dislike"
            aria-hidden="true"
          >
            −1
          </span>
        ) : null}
      </button>
      <button className="message-action-button" type="button" aria-label={t("message.fork")} title={t("message.fork")} disabled={!onFork} onClick={onFork}>
        <GitFork className="icon" />
      </button>
    </div>
  );
}

function TurnRunButton({ onOpenRuns }: { onOpenRuns: () => void }): JSX.Element {
  const { t } = useI18n();
  const label = t("message.viewTurnRuns");
  return (
    <button
      className="message-action-button"
      type="button"
      aria-label={label}
      title={label}
      onClick={onOpenRuns}
    >
      <SquareTerminal className="icon" />
    </button>
  );
}

export function MessageCopyButton({
  getText,
  className = "",
  iconSize = 14,
  idleLabel,
  copiedLabel,
  failedLabel,
}: {
  getText: () => string;
  className?: string;
  iconSize?: number;
  idleLabel?: string;
  copiedLabel?: string;
  failedLabel?: string;
}): JSX.Element {
  const { t } = useI18n();
  const resolvedIdleLabel = idleLabel ?? t("message.copy");
  const resolvedCopiedLabel = copiedLabel ?? t("message.copied");
  const resolvedFailedLabel = failedLabel ?? t("common.copyFailed");
  const [copyState, setCopyState] = useState<"idle" | "copied" | "failed">("idle");
  const resetTimerRef = useRef<number | undefined>(undefined);
  const label = copyState === "copied" ? resolvedCopiedLabel : copyState === "failed" ? resolvedFailedLabel : resolvedIdleLabel;

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
  const { t } = useI18n();
  return (
    <button
      className={`message-edit-button ${className}`}
      type="button"
      aria-label={t("message.editAndRetry")}
      title={t("message.editAndRetry")}
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
  const { t } = useI18n();
  const { openPreview } = useImagePreview();
  return (
    <div className={`message-images${onRemove ? " message-images-editable" : ""}`}>
      {images.map((image, index) => {
        const src = imageSource(image);
        const label = t("composer.imageNumber", { number: index + 1 });
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
              aria-label={t("composer.enlargeNamed", { name: label })}
              onClick={handleOpen}
              onKeyDown={handleKeyDown}
            />
            {onRemove ? (
              <button
                type="button"
                className="message-image-remove"
                aria-label={t("composer.removeImage", { number: index + 1 })}
                title={t("common.remove")}
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
  const { t } = useI18n();
  return (
    <div className={`message-files${onRemove ? " message-files-editable" : ""}`}>
      {files.map((file, index) => (
        <div className="message-file-frame" key={`${file.media_type}-${file.filename ?? index}-${index}`}>
          <div className="message-file">
            <FileText className="icon" aria-hidden="true" />
            <span>{file.filename?.trim() || t("message.fileNumber", { number: index + 1 })}</span>
          </div>
          {onRemove ? (
            <button
              type="button"
              className="message-file-remove"
              aria-label={t("message.removeFileNamed", { name: file.filename?.trim() || index + 1 })}
              title={t("common.remove")}
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
