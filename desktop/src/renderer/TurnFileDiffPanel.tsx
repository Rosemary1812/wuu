import { X } from "lucide-react";
import { ToolDiffContent } from "./ToolDiffPreview";
import type { TurnFileDiffSelection } from "./TurnFileDiffTypes";

export function TurnFileDiffPanel({
  selection,
  onClose,
}: {
  selection?: TurnFileDiffSelection;
  onClose: () => void;
}): JSX.Element | null {
  if (!selection) return null;

  return (
    <section
      className="turn-file-diff-panel"
      aria-label={`${selection.path} 的代码差异`}
    >
      <div className="turn-file-diff-header">
        <div className="turn-file-diff-heading">
          <span className="turn-file-diff-kicker">本轮文件变更</span>
          <strong className="turn-file-diff-path" title={selection.path}>
            {selection.path}
          </strong>
          <span className="turn-file-diff-meta">
            {selection.newFile ? <span>新建文件</span> : null}
            {selection.additions > 0 ? (
              <span className="turn-edit-summary-add">
                +{selection.additions}
              </span>
            ) : null}
            {selection.deletions > 0 ? (
              <span className="turn-edit-summary-delete">
                -{selection.deletions}
              </span>
            ) : null}
            {selection.diff.truncated ? <span>已截断</span> : null}
          </span>
        </div>
        <button
          className="icon-button turn-file-diff-close"
          type="button"
          aria-label="关闭 diff 面板"
          onClick={onClose}
        >
          <X className="icon" />
        </button>
      </div>
      <div className="turn-file-diff-body">
        <ToolDiffContent diff={selection.diff} />
      </div>
    </section>
  );
}
