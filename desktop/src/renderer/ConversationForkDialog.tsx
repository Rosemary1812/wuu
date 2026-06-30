import { GitBranch, GitFork, Laptop } from "lucide-react";
import { useState } from "react";
import { Modal } from "./Modal";

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
// worktree ("worktree"). The shared `Modal` chrome handles backdrop,
// focus, Escape, and backdrop-click dismissal — this component only
// owns the option list, the busy-state spinner, and the footer
// Cancel button.
//
// `onChoose` resolves when the caller has finished starting the fork;
// the dialog stays open until then so the active spinner / disabled
// state on the chosen option is the visible feedback. Cancelling is
// always available via the Cancel button (footer) and the Modal's
// own close affordances (X, Esc, backdrop click) while not busy.
export function ConversationForkDialog({
  onCancel,
  onChoose,
}: {
  onCancel: () => void;
  onChoose: (mode: ForkMode) => void | Promise<void>;
}): JSX.Element {
  // Tracks which option is mid-flight so only that button shows a spinner
  // and becomes non-interactive. We keep both options clickable even
  // while one is submitting — when the IPC returns, the caller closes
  // the dialog, which remounts this component.
  const [busyMode, setBusyMode] = useState<ForkMode | null>(null);
  const disabled = busyMode !== null;

  async function handleChoose(mode: ForkMode): Promise<void> {
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

  return (
    <Modal
      ariaLabel="从较早消息创建分支"
      icon={<GitFork className="icon-lg" />}
      title="从较早消息创建分支？"
      onClose={onCancel}
      closeDisabled={disabled}
      panelClassName="fork-dialog"
      footer={
        <button
          className="secondary-button fork-dialog-cancel"
          type="button"
          aria-label="取消"
          disabled={disabled}
          onClick={onCancel}
        >
          取消
        </button>
      }
    >
      <div className="fork-dialog-options">
        {FORK_OPTIONS.map(({ mode, icon: Icon, title, description }) => {
          const isBusy = busyMode === mode;
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
    </Modal>
  );
}
