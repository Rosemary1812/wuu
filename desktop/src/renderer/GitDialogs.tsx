import { CornerDownRight, Github } from "lucide-react";
import {
  type FormEvent as ReactFormEvent,
  useState,
} from "react";
import type { GitCommitResult, GitPullRequestResult, GitStatusResult } from "../shared/protocol";
import { humanizeBranchTitle } from "./RuntimeHelpers";
import { Modal } from "./Modal";
import { useI18n } from "./i18n";

export function CommitChangesDialog({
  gitStatus,
  branch,
  onCancel,
  onCommit,
}: {
  gitStatus?: GitStatusResult;
  branch?: string;
  onCancel: () => void;
  onCommit: (params: { message: string; includeUnstaged: boolean }) => Promise<GitCommitResult>;
}): JSX.Element {
  const { t, formatNumber } = useI18n();
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
      setError(commitError instanceof Error ? commitError.message : t("git.commit.failed"));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Modal
      ariaLabel={t("git.commit.title")}
      icon={<CornerDownRight className="icon-lg" />}
      title={t("git.commit.title")}
      onClose={onCancel}
      asForm
      onSubmit={(event) => void submit(event)}
      footer={
        <>
          <button className="secondary-button" type="button" onClick={onCancel}>
            {t("common.cancel")}
          </button>
          <button
            className="primary-button"
            type="submit"
            disabled={!hasChanges || submitting}
          >
            {t("common.continue")}
          </button>
        </>
      }
    >
      <div className="environment-dialog-summary">
        <span>{t("git.branch")}</span>
        <strong>{branch ?? t("common.unknown")}</strong>
        <span>{t("git.changes")}</span>
        <strong>
          {t(diff.files === 1 ? "git.fileCountOne" : "git.fileCount", { count: formatNumber(diff.files) })}{" "}
          <span className="additions">+{formatNumber(diff.additions)}</span>{" "}
          <span className="deletions">-{formatNumber(diff.deletions)}</span>
        </strong>
      </div>
      <label className="environment-toggle">
        <input
          type="checkbox"
          checked={includeUnstaged}
          onChange={(event) => setIncludeUnstaged(event.currentTarget.checked)}
        />
        <span>{t("git.commit.includeUnstaged")}</span>
      </label>
      <label className="environment-field">
        <span>{t("git.commit.message")}</span>
        <input
          value={message}
          placeholder={t("git.commit.messagePlaceholder")}
          onChange={(event) => setMessage(event.target.value)}
        />
      </label>
      {error ? <div className="environment-dialog-error">{error}</div> : null}
    </Modal>
  );
}

export function PullRequestDialog({
  gitStatus,
  disabledReason,
  onCancel,
  onCreate,
}: {
  gitStatus?: GitStatusResult;
  disabledReason: string;
  onCancel: () => void;
  onCreate: (params: { title: string; body: string; draft: boolean }) => Promise<GitPullRequestResult>;
}): JSX.Element {
  const { t } = useI18n();
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
      setError(createError instanceof Error ? createError.message : t("git.pr.createFailed"));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Modal
      ariaLabel={existingURL ? t("git.pr.title") : t("git.pr.createTitle")}
      icon={<Github className="icon-lg" />}
      title={existingURL ? t("git.pr.title") : t("git.pr.createTitle")}
      onClose={onCancel}
      asForm
      onSubmit={(event) => void submit(event)}
      footer={
        <>
          <button className="secondary-button" type="button" onClick={onCancel}>
            {t("common.close")}
          </button>
          <button
            className="primary-button"
            type="submit"
            disabled={blocked || submitting}
          >
            {existingURL ? t("common.open") : t("common.continue")}
          </button>
        </>
      }
    >
      {blocked ? <div className="environment-dialog-error">{disabledReason}</div> : null}
      {existingURL ? (
        <div className="environment-pr-result">
          <span>{result?.already_exists ? t("git.pr.exists") : t("git.pr.ready")}</span>
          <button
            className="secondary-button"
            type="button"
            onClick={() => window.open(existingURL, "_blank", "noopener,noreferrer")}
          >
            {t("git.pr.open")}
          </button>
        </div>
      ) : (
        <>
          <label className="environment-field">
            <span>{t("git.pr.fieldTitle")}</span>
            <input
              value={title}
              placeholder={t("git.pr.titlePlaceholder")}
              onChange={(event) => setTitle(event.target.value)}
            />
          </label>
          <label className="environment-field">
            <span>{t("git.pr.description")}</span>
            <textarea
              value={body}
              placeholder={t("git.pr.descriptionPlaceholder")}
              onChange={(event) => setBody(event.target.value)}
            />
          </label>
          <label className="environment-toggle">
            <input
              type="checkbox"
              checked={draft}
              onChange={(event) => setDraft(event.currentTarget.checked)}
            />
            <span>{t("git.pr.draft")}</span>
          </label>
        </>
      )}
      {error ? <div className="environment-dialog-error">{error}</div> : null}
    </Modal>
  );
}
