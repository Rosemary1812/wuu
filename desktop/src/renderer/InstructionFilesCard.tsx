import { ChevronRight, FileText, Info, X } from "lucide-react";
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
  const { result, loading, error } = entry;
  const files = result?.files ?? [];
  const globalFiles = files.filter((file) => file.scope === "global");
  const projectFiles = files.filter((file) => file.scope !== "global");
  const hasFiles = files.length > 0;

  return (
    <article className="instruction-files-card" aria-label="指令文件">
      <div className="instruction-files-card-inner">
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

        {loading ? <InstructionFilesState text="正在读取已加载的指令文件" /> : null}
        {!loading && error ? <InstructionFilesState tone="error" text={error} /> : null}
        {!loading && !error && !hasFiles ? (
          <InstructionFilesState text="当前会话没有加载任何指令文件（AGENTS.md / CLAUDE.md）。" />
        ) : null}

        {!loading && !error && hasFiles ? (
          <div className="instruction-files-groups">
            <InstructionFilesGroup label="全局" files={globalFiles} />
            <InstructionFilesGroup label="项目" files={projectFiles} />
          </div>
        ) : null}
      </div>
    </article>
  );
}

function InstructionFilesGroup({ label, files }: { label: string; files: InstructionFile[] }): JSX.Element | null {
  if (files.length === 0) {
    return null;
  }
  return (
    <section className="instruction-files-group">
      <span className="instruction-files-group-label">{label}</span>
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
      {/* 完整路径只保留在悬停提示里：行内的文件名已经标识了文件，路径
        * 文本会把同一语义写第二遍。 */}
      <button
        className="instruction-file-toggle"
        type="button"
        aria-expanded={expanded}
        title={file.path}
        onClick={() => setExpanded((value) => !value)}
      >
        <ChevronRight className="icon instruction-file-chevron" />
        <FileText className="icon instruction-file-icon" />
        <span className="instruction-file-name">{file.name}</span>
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
