import { useState } from "react";
import type { ThreadItem, Turn } from "../shared/protocol";
import {
  extractToolDiffPreview,
  ToolDiffPreview,
  type ToolDiffPreviewFileDiff,
} from "./ToolDiffPreview";

type FileEdit = {
  path: string;
  item: ThreadItem;
  diff?: ToolDiffPreviewFileDiff;
  additions: number;
  deletions: number;
  newFile: boolean;
};

function parseJSON(value: string | undefined): unknown {
  if (!value) return undefined;
  try {
    return JSON.parse(value);
  } catch {
    return undefined;
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function stringValue(record: unknown, key: string): string | undefined {
  if (!isRecord(record)) return undefined;
  const value = record[key];
  return typeof value === "string" ? value : undefined;
}

function numberValue(record: unknown, key: string): number | undefined {
  if (!isRecord(record)) return undefined;
  const value = record[key];
  return typeof value === "number" ? value : undefined;
}

function arrayValue(record: unknown, key: string): unknown[] {
  if (!isRecord(record)) return [];
  const value = record[key];
  return Array.isArray(value) ? value : [];
}

function summarizeDiff(diff: Record<string, unknown>): {
  additions: number;
  deletions: number;
} {
  if (diff.new_file === true) {
    return {
      additions: numberValue(diff, "lines") ?? numberValue(diff, "new_lines") ?? 0,
      deletions: 0,
    };
  }
  const oldLines = numberValue(diff, "old_lines");
  const newLines = numberValue(diff, "new_lines");
  if (oldLines !== undefined || newLines !== undefined) {
    return {
      additions: newLines ?? 0,
      deletions: oldLines ?? 0,
    };
  }
  let additions = 0;
  let deletions = 0;
  for (const hunk of arrayValue(diff, "hunks")) {
    if (!isRecord(hunk)) continue;
    for (const line of arrayValue(hunk, "lines")) {
      if (!isRecord(line)) continue;
      if (line.op === "insert") additions++;
      else if (line.op === "delete") deletions++;
    }
  }
  return { additions, deletions };
}

function summarizeRisk(result: Record<string, unknown>): {
  additions: number;
  deletions: number;
} {
  const riskSummary = isRecord(result.risk_summary)
    ? result.risk_summary
    : undefined;
  if (!riskSummary) {
    return { additions: 0, deletions: 0 };
  }
  return {
    additions: numberValue(riskSummary, "added_lines") ?? 0,
    deletions: numberValue(riskSummary, "deleted_lines") ?? 0,
  };
}

function firstChangedFilePath(result: Record<string, unknown>): string | undefined {
  const changedFiles = arrayValue(result, "changed_files")
    .filter((value): value is string => typeof value === "string" && value.trim().length > 0);
  if (changedFiles.length > 0) {
    return changedFiles[0];
  }
  for (const file of arrayValue(result, "files")) {
    if (!isRecord(file)) continue;
    const path = stringValue(file, "path") ?? stringValue(file, "move_path");
    if (path) return path;
  }
  return undefined;
}

function filePathFromPatchFile(file: Record<string, unknown>): string | undefined {
  return stringValue(file, "move_path") ?? stringValue(file, "path");
}

function extractPatchFileEdits(
  item: ThreadItem,
  result: Record<string, unknown>,
): FileEdit[] {
  const edits: FileEdit[] = [];
  for (const file of arrayValue(result, "files")) {
    if (!isRecord(file)) continue;
    const path = filePathFromPatchFile(file);
    if (!path) continue;
    const diffRecord = isRecord(file.diff) ? file.diff : undefined;
    const diff = diffRecord
      ? extractToolDiffPreview(diffRecord, path)
      : undefined;
    const stats = diffRecord ? summarizeDiff(diffRecord) : { additions: 0, deletions: 0 };
    edits.push({
      path,
      item,
      diff,
      additions: stats.additions,
      deletions: stats.deletions,
      newFile: diffRecord?.new_file === true || file.action === "add",
    });
  }
  return edits;
}

function extractFileEdits(item: ThreadItem): FileEdit[] {
  const name = (item.name ?? "").trim();
  const capability = item.display?.capability?.trim();
  const isEditTool =
    name === "edit_file" ||
    name === "write_file" ||
    name === "apply_patch" ||
    capability === "file.edit";
  if (!isEditTool) return [];

  const result = parseJSON(item.result);
  if (!isRecord(result)) return [];

  const patchFileEdits = extractPatchFileEdits(item, result);
  if (patchFileEdits.length > 0) {
    return patchFileEdits;
  }

  const path =
    stringValue(result, "path") ??
    stringValue(result, "file") ??
    firstChangedFilePath(result) ??
    stringValue(isRecord(result.diff) ? result.diff : undefined, "path");
  if (!path) return [];

  const diffRecord = isRecord(result.diff) ? result.diff : undefined;
  const newFile = diffRecord?.new_file === true || result.new_file === true;
  const newFileLines = numberValue(diffRecord, "lines") ?? 0;

  // For newly-created files the backend returns { new_file: true, lines: N }
  // instead of hunks. Treat those as additions so the card still surfaces them.
  if (newFile && newFileLines > 0) {
    return [{
      path,
      item,
      diff: diffRecord ? extractToolDiffPreview(diffRecord, path) : undefined,
      additions: newFileLines,
      deletions: 0,
      newFile,
    }];
  }

  const diffStats = diffRecord ? summarizeDiff(diffRecord) : summarizeRisk(result);

  return [{
    path,
    item,
    diff: diffRecord ? extractToolDiffPreview(diffRecord, path) : undefined,
    additions: diffStats.additions,
    deletions: diffStats.deletions,
    newFile,
  }];
}

function fileDisplayName(path: string): string {
  // Show the last two segments so files in deep directories remain
  // distinguishable without taking up the full path.
  const parts = path.split(/[\\/]/);
  if (parts.length <= 2) {
    return parts.join("/");
  }
  return `...${parts.slice(-2).join("/")}`;
}

function collectTurnFileEdits(turn: Turn): FileEdit[] {
  const edits: FileEdit[] = [];
  for (const item of turn.items) {
    if (item.type !== "tool_call" && item.type !== "collab_agent_tool_call") {
      continue;
    }
    edits.push(...extractFileEdits(item));
  }
  return edits;
}

function aggregateFileEdits(edits: FileEdit[]): FileEdit[] {
  const byPath = new Map<string, FileEdit>();
  for (const edit of edits) {
    const existing = byPath.get(edit.path);
    if (existing) {
      existing.additions += edit.additions;
      existing.deletions += edit.deletions;
      // Prefer the latest item for hover preview.
      existing.item = edit.item;
      existing.diff = edit.diff;
    } else {
      byPath.set(edit.path, { ...edit });
    }
  }
  return Array.from(byPath.values());
}

export function turnHasFileEdits(turn: Turn): boolean {
  return collectTurnFileEdits(turn).length > 0;
}

const FILE_BATCH_SIZE = 3;

export function TurnEditSummaryCard({
  turn,
  cwd,
}: {
  turn: Turn;
  cwd?: string;
}): JSX.Element | null {
  const [visibleCount, setVisibleCount] = useState(FILE_BATCH_SIZE);

  if (turn.status === "in_progress") return null;

  const rawEdits = collectTurnFileEdits(turn);
  const edits = aggregateFileEdits(rawEdits);

  if (edits.length === 0) return null;

  const visibleEdits = edits.slice(0, visibleCount);
  const hiddenCount = Math.max(0, edits.length - visibleCount);
  const nextCount = Math.min(FILE_BATCH_SIZE, hiddenCount);

  return (
    <div className="turn-edit-summary-card">
      <div className="turn-edit-summary-header">
        <span className="turn-edit-summary-title">
          {edits.length === 1 ? "已编辑 1 个文件" : `已编辑 ${edits.length} 个文件`}
        </span>
      </div>
      <div className="turn-edit-summary-list">
        {visibleEdits.map((edit) => (
          <ToolDiffPreview diff={edit.diff} item={edit.item} key={edit.path}>
            <div className="turn-edit-summary-row">
              <span className="turn-edit-summary-name" title={edit.path}>
                {fileDisplayName(edit.path)}
              </span>
              <span className="turn-edit-summary-stats">
                {edit.additions > 0 ? (
                  <span className="turn-edit-summary-add">+{edit.additions}</span>
                ) : null}
                {edit.deletions > 0 ? (
                  <span className="turn-edit-summary-delete">-{edit.deletions}</span>
                ) : null}
              </span>
            </div>
          </ToolDiffPreview>
        ))}
        {hiddenCount > 0 ? (
          <div className="turn-edit-summary-more">
            <span>还有 {hiddenCount} 个文件</span>
            <button
              className="turn-edit-summary-more-button"
              type="button"
              onClick={() =>
                setVisibleCount((current) =>
                  Math.min(current + FILE_BATCH_SIZE, edits.length),
                )
              }
            >
              再显示 {nextCount} 个
            </button>
          </div>
        ) : null}
      </div>
    </div>
  );
}
