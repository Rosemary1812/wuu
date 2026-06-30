import { ChevronDown, GitFork } from "lucide-react";
import type { Thread } from "../shared/protocol";
import { MessageCopyButton } from "./MessageActions";

export function ForkWorktreeNotice({
  thread,
}: {
  thread: Thread;
}): JSX.Element | null {
  const worktree = thread.worktree;
  if (!worktree || !thread.forked_from_id) {
    return null;
  }

  const log = worktreeCreationLog(thread);

  return (
    <section className="fork-worktree-notice" aria-label="从对话中派生">
      <div className="fork-worktree-divider">
        <span />
        <strong>
          <GitFork className="icon" aria-hidden="true" />
          从对话中派生
        </strong>
        <span />
      </div>
      <details className="fork-worktree-card" open>
        <summary className="fork-worktree-summary">
          <span className="fork-worktree-chip">
            <GitFork className="icon" aria-hidden="true" />
            已创建工作树
          </span>
          <ChevronDown className="fork-worktree-chevron icon" aria-hidden="true" />
        </summary>
        <div className="fork-worktree-code-block">
          <MessageCopyButton
            getText={() => log}
            className="fork-worktree-copy"
            iconSize={13}
            idleLabel="复制工作树记录"
            copiedLabel="已复制工作树记录"
            failedLabel="复制失败"
          />
          <pre className="fork-worktree-code">
            <code>{log}</code>
          </pre>
        </div>
      </details>
    </section>
  );
}

function worktreeCreationLog(thread: Thread): string {
  const worktree = thread.worktree;
  if (!worktree) {
    return "";
  }
  const head = worktree.base_head?.trim();
  const lines = [
    "[info] Starting worktree creation",
    head ? `Preparing worktree (detached HEAD ${shortSHA(head)})` : "",
    `[info] Base repository ${worktree.base_repo || thread.cwd}`,
    `Worktree created at ${worktree.path}`,
    `Fork session ${thread.id}`,
  ].filter(Boolean);
  return lines.join("\n");
}

function shortSHA(value: string): string {
  return value.length > 8 ? value.slice(0, 8) : value;
}
