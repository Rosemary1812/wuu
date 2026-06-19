import { CornerDownRight, Github, X } from "lucide-react";
import { type FormEvent as ReactFormEvent, useState } from "react";
import type { GitCommitResult, GitPullRequestResult, GitStatusResult } from "../shared/protocol";
import { humanizeBranchTitle } from "./RuntimeHelpers";

export function CommitChangesDialog({
  gitStatus,
  branch,
  onCancel,
  onCommit
}: {
  gitStatus?: GitStatusResult;
  branch?: string;
  onCancel: () => void;
  onCommit: (params: { message: string; includeUnstaged: boolean }) => Promise<GitCommitResult>;
}): JSX.Element {
  const [message, setMessage] = useState("");
  const [includeUnstaged, setIncludeUnstaged] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const diff = gitStatus?.diff ?? { files: 0, additions: 0, deletions: 0 };
  const staged = gitStatus?.staged_diff ?? { files: 0, additions: 0, deletions: 0 };
  const hasChanges = Boolean(gitStatus?.is_repo && (gitStatus.dirty_count > 0 || diff.files > 0 || staged.files > 0));

  async function submit(event: ReactFormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    if (!hasChanges || submitting) {
      return;
    }
    setSubmitting(true);
    setError("");
    try {
      await onCommit({ message, includeUnstaged });
      onCancel();
    } catch (commitError) {
      setError(commitError instanceof Error ? commitError.message : "提交失败");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="modal-backdrop environment-modal-backdrop">
      <form className="environment-dialog" onSubmit={(event) => void submit(event)}>
        <div className="environment-dialog-header">
          <span className="environment-dialog-icon">
            <CornerDownRight className="icon-lg" />
          </span>
          <button className="icon-button" type="button" aria-label="关闭" onClick={onCancel}>
            <X className="icon" />
          </button>
        </div>
        <h2>提交更改</h2>
        <div className="environment-dialog-summary">
          <span>分支</span>
          <strong>{branch ?? "未知"}</strong>
          <span>更改</span>
          <strong>
            {diff.files} 个文件 <span className="additions">+{diff.additions.toLocaleString()}</span>{" "}
            <span className="deletions">-{diff.deletions.toLocaleString()}</span>
          </strong>
        </div>
        <label className="environment-toggle">
          <input
            type="checkbox"
            checked={includeUnstaged}
            onChange={(event) => setIncludeUnstaged(event.currentTarget.checked)}
          />
          <span>包含未暂存的更改</span>
        </label>
        <label className="environment-field">
          <span>提交消息</span>
          <input value={message} placeholder="留空以自动生成提交消息" onChange={(event) => setMessage(event.target.value)} />
        </label>
        {error ? <div className="environment-dialog-error">{error}</div> : null}
        <div className="environment-dialog-footer">
          <button className="secondary-button" type="button" onClick={onCancel}>
            取消
          </button>
          <button className="primary-button" type="submit" disabled={!hasChanges || submitting}>
            继续
          </button>
        </div>
      </form>
    </div>
  );
}

export function PullRequestDialog({
  gitStatus,
  disabledReason,
  onCancel,
  onCreate
}: {
  gitStatus?: GitStatusResult;
  disabledReason: string;
  onCancel: () => void;
  onCreate: (params: { title: string; body: string; draft: boolean }) => Promise<GitPullRequestResult>;
}): JSX.Element {
  const [title, setTitle] = useState(() => humanizeBranchTitle(gitStatus?.branch ?? ""));
  const [body, setBody] = useState("");
  const [draft, setDraft] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const [result, setResult] = useState<GitPullRequestResult | undefined>(undefined);
  const existingURL = gitStatus?.pr_url ?? result?.url;
  const blocked = Boolean(disabledReason && !existingURL);

  async function submit(event: ReactFormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    if (blocked || submitting) {
      return;
    }
    if (existingURL) {
      window.open(existingURL, "_blank", "noopener,noreferrer");
      return;
    }
    setSubmitting(true);
    setError("");
    try {
      const created = await onCreate({ title, body, draft });
      setResult(created);
    } catch (createError) {
      setError(createError instanceof Error ? createError.message : "创建拉取请求失败");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="modal-backdrop environment-modal-backdrop">
      <form className="environment-dialog" onSubmit={(event) => void submit(event)}>
        <div className="environment-dialog-header">
          <span className="environment-dialog-icon">
            <Github className="icon-lg" />
          </span>
          <button className="icon-button" type="button" aria-label="关闭" onClick={onCancel}>
            <X className="icon" />
          </button>
        </div>
        <h2>{existingURL ? "拉取请求" : "创建拉取请求"}</h2>
        {blocked ? <div className="environment-dialog-error">{disabledReason}</div> : null}
        {existingURL ? (
          <div className="environment-pr-result">
            <span>{result?.already_exists ? "已有 PR" : "PR 已准备好"}</span>
            <button className="secondary-button" type="button" onClick={() => window.open(existingURL, "_blank", "noopener,noreferrer")}>
              打开 PR
            </button>
          </div>
        ) : (
          <>
            <label className="environment-field">
              <span>标题</span>
              <input value={title} placeholder="使用分支名作为标题" onChange={(event) => setTitle(event.target.value)} />
            </label>
            <label className="environment-field">
              <span>说明</span>
              <textarea value={body} placeholder="可留空，让 gh 使用提交内容" onChange={(event) => setBody(event.target.value)} />
            </label>
            <label className="environment-toggle">
              <input type="checkbox" checked={draft} onChange={(event) => setDraft(event.currentTarget.checked)} />
              <span>创建为草稿</span>
            </label>
          </>
        )}
        {error ? <div className="environment-dialog-error">{error}</div> : null}
        <div className="environment-dialog-footer">
          <button className="secondary-button" type="button" onClick={onCancel}>
            关闭
          </button>
          <button className="primary-button" type="submit" disabled={blocked || submitting}>
            {existingURL ? "打开" : "继续"}
          </button>
        </div>
      </form>
    </div>
  );
}
