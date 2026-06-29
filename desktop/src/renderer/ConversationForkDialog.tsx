import { GitBranch, GitFork, Laptop, X } from "lucide-react";
import {
  type KeyboardEvent as ReactKeyboardEvent,
  type MouseEvent as ReactMouseEvent,
  useEffect,
  useRef,
  useState,
} from "react";

export type ForkMode = "local" | "worktree";

type ForkOption = {
  mode: ForkMode;
  icon: typeof GitBranch;
  title: string;
  description: string;
};

const FORK_OPTIONS: ForkOption[] = [
  {
    mode: "local",
    icon: Laptop,
    title: "派生到本地",
    description: "在新的本地聊天中从此消息继续",
  },
  {
    mode: "worktree",
    icon: GitBranch,
    title: "派生到新工作树",
    description: "在新工作树中从此消息继续",
  },
];

// Asks the user whether a fork from a non-latest message should land in
// the same working directory ("local") or in a freshly created git
// worktree ("worktree"). Mirrors the layout pattern used by
// `CommitChangesDialog` and `PullRequestDialog` — same `.environment-dialog`
// chrome so modal look-and-feel stays consistent across the app.
//
// `onChoose` resolves when the caller has finished starting the fork; the
// dialog stays open until then so the active spinner / disabled state on
// the chosen option is the visible feedback. Cancelling is always
// available via Esc, the X button, the backdrop click, or the bottom
// secondary button.
export function ConversationForkDialog({
  onCancel,
  onChoose,
}: {
  onCancel: () => void;
  onChoose: (mode: ForkMode) => void | Promise<void>;
}): JSX.Element {
  // Tracks which option is mid-flight so only that button shows a spinner
  // and becomes non-interactive. We keep both options clickable even
  // while one is submitting — when the IPC returns, the caller closes the
  // dialog, which remounts this component.
  const [busyMode, setBusyMode] = useState<ForkMode | null>(null);
  const dialogRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    // Move focus into the dialog the moment it mounts so keyboard users
    // land somewhere sane; Tab cycles the option buttons, Esc closes.
    const node = dialogRef.current;
    if (node) {
      const firstOption = node.querySelector<HTMLButtonElement>(
        ".fork-dialog-option",
      );
      firstOption?.focus();
    }
  }, []);

  useEffect(() => {
    function handleKeyDown(event: globalThis.KeyboardEvent): void {
      if (event.key === "Escape" && busyMode === null) {
        event.preventDefault();
        onCancel();
      }
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => {
      window.removeEventListener("keydown", handleKeyDown);
    };
  }, [busyMode, onCancel]);

  async function handleChoose(
    mode: ForkMode,
  ): Promise<void> {
    if (busyMode !== null) {
      return;
    }
    setBusyMode(mode);
    try {
      await onChoose(mode);
    } finally {
      // The caller is expected to close the dialog on success, so the
      // setter is only meaningful if the promise resolves inside this
      // tick (rare). Resetting keeps the UI sane if `onChoose` throws
      // and the caller chooses not to dismiss.
      setBusyMode((current) => (current === mode ? null : current));
    }
  }

  function stopBubble(
    event: ReactMouseEvent<HTMLDivElement> | ReactKeyboardEvent<HTMLDivElement>,
  ): void {
    // Backdrop clicks should close; clicks inside the panel should not.
    event.stopPropagation();
  }

  return (
    <div
      className="modal-backdrop environment-modal-backdrop"
      onClick={() => {
        if (busyMode === null) {
          onCancel();
        }
      }}
      role="presentation"
    >
      <div
        ref={dialogRef}
        className="environment-dialog fork-dialog"
        role="dialog"
        aria-modal="true"
        aria-label="从较早消息创建分支"
        onClick={stopBubble}
        onKeyDown={stopBubble}
      >
        <div className="environment-dialog-header">
          <span className="environment-dialog-icon">
            <GitFork className="icon-lg" />
          </span>
          <button
            className="icon-button"
            type="button"
            aria-label="关闭"
            disabled={busyMode !== null}
            onClick={() => {
              if (busyMode === null) {
                onCancel();
              }
            }}
          >
            <X className="icon" />
          </button>
        </div>
        <h2>从较早消息创建分支?</h2>
        <div className="fork-dialog-options">
          {FORK_OPTIONS.map(({ mode, icon: Icon, title, description }) => {
            const isBusy = busyMode === mode;
            const disabled = busyMode !== null;
            return (
              <button
                key={mode}
                className={`fork-dialog-option${isBusy ? " is-busy" : ""}`}
                type="button"
                disabled={disabled}
                aria-label={title}
                onClick={() => void handleChoose(mode)}
              >
                <span className="fork-dialog-option-icon">
                  <Icon className="icon-lg" aria-hidden="true" />
                </span>
                <span className="fork-dialog-option-text">
                  <strong>{title}</strong>
                  <span>{description}</span>
                </span>
                {isBusy ? (
                  <span className="fork-dialog-option-spinner" aria-hidden="true" />
                ) : null}
              </button>
            );
          })}
        </div>
        <p className="fork-dialog-note">
          这会保持你当前的文件和工作树状态不变。如果后续轮次更改了文件系统，新的分支内容可能与当前磁盘上的内容不一致。
        </p>
        <div className="environment-dialog-footer">
          <button
            className="secondary-button fork-dialog-cancel"
            type="button"
            aria-label="取消"
            disabled={busyMode !== null}
            onClick={onCancel}
          >
            取消
          </button>
        </div>
      </div>
    </div>
  );
}
