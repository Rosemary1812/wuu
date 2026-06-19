import { AlertCircle, Check, Copy, FileText, GitFork, ThumbsDown, ThumbsUp } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import type { InputFile, InputImage } from "../shared/protocol";
import { imageSource } from "./ComposerMessages";

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
  iconSize = 14
}: {
  getText: () => string;
  className?: string;
  iconSize?: number;
}): JSX.Element {
  const [copyState, setCopyState] = useState<"idle" | "copied" | "failed">("idle");
  const resetTimerRef = useRef<number | undefined>(undefined);
  const label = copyState === "copied" ? "已复制消息" : copyState === "failed" ? "复制失败" : "复制消息";

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

export function MessageImageGrid({ images }: { images: InputImage[] }): JSX.Element {
  return (
    <div className="message-images">
      {images.map((image, index) => (
        <img className="message-image" key={`${image.media_type}-${index}`} src={imageSource(image)} alt={`Image ${index + 1}`} />
      ))}
    </div>
  );
}

export function MessageFileList({ files }: { files: InputFile[] }): JSX.Element {
  return (
    <div className="message-files">
      {files.map((file, index) => (
        <div className="message-file" key={`${file.media_type}-${file.filename ?? index}-${index}`}>
          <FileText size={16} aria-hidden="true" />
          <span>{file.filename?.trim() || `File ${index + 1}`}</span>
        </div>
      ))}
    </div>
  );
}
