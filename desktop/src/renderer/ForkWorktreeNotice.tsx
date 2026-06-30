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
  const head = worktree.base_head?.trim();

  return (
    <section className="fork-worktree-notice" aria-label="从对话中派生的工作树">
      <details className="fork-worktree-card">
        <summary className="fork-worktree-summary">
          <span className="fork-worktree-glyph">
            <GitFork className="icon" aria-hidden="true" />
          </span>
          <span className="fork-worktree-summary-text">
            <strong>已创建工作树</strong>
            <span>从对话中派生</span>
          </span>
          <ChevronDown className="fork-worktree-chevron icon" aria-hidden="true" />
        </summary>
        <div className="fork-worktree-details">
          <dl className="fork-worktree-meta">
            <div>
              <dt>基础仓库</dt>
              <dd>{worktree.base_repo || thread.cwd}</dd>
            </div>
            {head ? (
              <div>
                <dt>基准提交</dt>
                <dd>{shortSHA(head)}</dd>
              </div>
            ) : null}
            <div>
              <dt>工作树</dt>
              <dd>{worktree.path}</dd>
            </div>
          </dl>
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
