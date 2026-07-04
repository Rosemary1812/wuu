import { ChevronDown, ChevronRight, FileText, Info, X } from "lucide-react";
import { useState } from "react";
import type { InstructionFile, InstructionsListResult } from "../shared/protocol";

// InstructionFilesEntry mirrors ContextCompositionEntry: App owns the fetch
// and drops the result here. Keeping the card presentational makes it trivial
// to unit test the loading / error / grouped states in isolation.
export type InstructionFilesEntry = {
  id: string;
  threadID: string;
  title?: string;
  result?: InstructionsListResult;
  loading: boolean;
  error?: string;
};

export function InstructionFilesCard({
  entry,
  onDismiss,
}: {
  entry: InstructionFilesEntry;
  onDismiss?: (id: string) => void;
}): JSX.Element {
  const { result, loading, error, title } = entry;
  const files = result?.files ?? [];
  const globalFiles = files.filter((file) => file.scope === "global");
  const projectFiles = files.filter((file) => file.scope !== "global");
  const hasFiles = files.length > 0;

  return (
    <article className="instruction-files-card" aria-label="指令文件">
      <div className="instruction-files-card-inner">
        <div className="instruction-files-header">
          <div className="instruction-files-title-block">
            <h2>指令文件</h2>
            {title ? <p>{title}</p> : null}
          </div>
          {!loading && !error && hasFiles ? (
            <span className="instruction-files-count">{files.length} 个文件</span>
          ) : null}
          {onDismiss ? (
            <button
              className="icon-button instruction-files-dismiss"
              type="button"
              aria-label="移除指令文件卡片"
              onClick={() => onDismiss(entry.id)}
            >
              <X className="icon" />
            </button>
          ) : null}
        </div>

        {loading ? <InstructionFilesState text="正在读取已加载的指令文件" /> : null}
        {!loading && error ? <InstructionFilesState tone="error" text={error} /> : null}
        {!loading && !error && !hasFiles ? (
          <InstructionFilesState text="当前会话没有加载任何指令文件（AGENTS.md / CLAUDE.md）。" />
        ) : null}

        {!loading && !error && hasFiles ? (
          <div className="instruction-files-groups">
            <InstructionFilesGroup
              label="全局"
              hint="用户级，对所有项目生效"
              files={globalFiles}
            />
            <InstructionFilesGroup
              label="项目"
              hint="当前项目层级发现"
              files={projectFiles}
            />
          </div>
        ) : null}
      </div>
    </article>
  );
}

function InstructionFilesGroup({
  label,
  hint,
  files,
}: {
  label: string;
  hint: string;
  files: InstructionFile[];
}): JSX.Element | null {
  if (files.length === 0) {
    return null;
  }
  return (
    <section className="instruction-files-group">
      <header className="instruction-files-group-header">
        <span className="instruction-files-group-label">{label}</span>
        <span className="instruction-files-group-hint">{hint}</span>
      </header>
      <div className="instruction-files-list">
        {files.map((file) => (
          <InstructionFileRow file={file} key={file.path} />
        ))}
      </div>
    </section>
  );
}

function InstructionFileRow({ file }: { file: InstructionFile }): JSX.Element {
  const [expanded, setExpanded] = useState(false);
  const content = file.content ?? "";
  return (
    <div className={`instruction-file-row${expanded ? " expanded" : ""}`}>
      <button
        className="instruction-file-toggle"
        type="button"
        aria-expanded={expanded}
        onClick={() => setExpanded((value) => !value)}
      >
        {expanded ? (
          <ChevronDown className="icon instruction-file-chevron" />
        ) : (
          <ChevronRight className="icon instruction-file-chevron" />
        )}
        <FileText className="icon instruction-file-icon" />
        <span className="instruction-file-name">{file.name}</span>
        <span className="instruction-file-path" title={file.path}>
          {file.path}
        </span>
        <span className="instruction-file-size">{formatBytes(file.bytes)}</span>
      </button>
      {expanded ? (
        <pre className="instruction-file-preview">
          {content.length > 0 ? content : "（文件为空）"}
        </pre>
      ) : null}
    </div>
  );
}

function InstructionFilesState({
  text,
  tone = "neutral",
}: {
  text: string;
  tone?: "neutral" | "error";
}): JSX.Element {
  return (
    <div className={`instruction-files-state ${tone}`}>
      <Info className="icon" />
      <span>{text}</span>
    </div>
  );
}

function formatBytes(bytes: number): string {
  const safe = Math.max(0, Math.round(bytes));
  if (safe >= 1_000_000) {
    return `${(safe / 1_000_000).toFixed(1)} MB`;
  }
  if (safe >= 1_000) {
    return `${(safe / 1_000).toFixed(1)} KB`;
  }
  return `${safe} B`;
}
