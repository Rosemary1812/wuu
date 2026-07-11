import { useEffect, useState } from "react";
import { Archive, X } from "lucide-react";

const ARCHIVE_TIP_AUTO_DISMISS_MS = 6000;

export type ArchiveTipProps = {
  threadTitle: string;
  onViewArchive: () => void;
  onDismiss: () => void;
};

/**
 * Lightweight toast shown after a session is archived. Renders above the
 * main shell, auto-dismisses after a few seconds, and exposes a single
 * "归档" link that jumps straight to the Settings → Archive page so the
 * user can restore the session later.
 */
export function ArchiveTip({
  threadTitle,
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

  return (
    <div
      className={`archive-tip${leaving ? " leaving" : ""}`}
      role="status"
      aria-live="polite"
    >
      <Archive className="archive-tip-icon" aria-hidden="true" />
      <span className="archive-tip-message">
        {trimmedTitle ? (
          <>
            <strong>{trimmedTitle}</strong>
            <span> 已归档</span>
          </>
        ) : (
          <span>会话已归档</span>
        )}
      </span>
      <button
        type="button"
        className="archive-tip-action"
        onClick={onViewArchive}
      >
        查看归档
      </button>
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