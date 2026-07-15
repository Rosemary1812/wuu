import { useEffect, useState } from "react";
import { Archive, CircleAlert, X } from "lucide-react";

const ARCHIVE_TIP_AUTO_DISMISS_MS = 6000;

export type ArchiveTipProps = {
  threadTitle: string;
  errorMessage?: string;
  onViewArchive: () => void;
  onDismiss: () => void;
};

/**
 * Lightweight toast for an archive attempt. A successful archive links to the
 * archive page; a rejected attempt explains why the session stayed visible.
 */
export function ArchiveTip({
  threadTitle,
  errorMessage,
  onViewArchive,
  onDismiss,
}: ArchiveTipProps): JSX.Element {
  const [leaving, setLeaving] = useState(false);

  useEffect(() => {
    const timer = window.setTimeout(() => {
      setLeaving(true);
    }, ARCHIVE_TIP_AUTO_DISMISS_MS - 400);
    const finalize = window.setTimeout(onDismiss, ARCHIVE_TIP_AUTO_DISMISS_MS);
    return () => {
      window.clearTimeout(timer);
      window.clearTimeout(finalize);
    };
  }, [onDismiss]);

  const trimmedTitle = threadTitle.trim();
  const failed = Boolean(errorMessage);

  return (
    <div
      className={`archive-tip${failed ? " is-error" : ""}${leaving ? " leaving" : ""}`}
      role={failed ? "alert" : "status"}
      aria-live={failed ? "assertive" : "polite"}
    >
      {failed ? (
        <CircleAlert className="archive-tip-icon" aria-hidden="true" />
      ) : (
        <Archive className="archive-tip-icon" aria-hidden="true" />
      )}
      <span className="archive-tip-message">
        {errorMessage ? (
          <span>{errorMessage}</span>
        ) : trimmedTitle ? (
          <>
            <strong>{trimmedTitle}</strong>
            <span> 已归档</span>
          </>
        ) : (
          <span>会话已归档</span>
        )}
      </span>
      {failed ? null : (
        <button
          type="button"
          className="archive-tip-action"
          onClick={onViewArchive}
        >
          查看归档
        </button>
      )}
      <button
        type="button"
        className="archive-tip-dismiss"
        aria-label="关闭提示"
        onClick={() => {
          setLeaving(true);
          window.setTimeout(onDismiss, 200);
        }}
      >
        <X className="icon-sm" />
      </button>
    </div>
  );
}