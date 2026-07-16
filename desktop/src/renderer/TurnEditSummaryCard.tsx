import { useState } from "react";
import type { ThreadItem, Turn } from "../shared/protocol";
import {
  extractToolDiffPreview,
  ToolDiffPreview,
  type ToolDiffPreviewFileDiff,
} from "./ToolDiffPreview";
import type { TurnFileDiffSelection } from "./TurnFileDiffTypes";
import { useI18n } from "./i18n";

type FileEdit = {
  path: string;
  item: ThreadItem;
  diff?: ToolDiffPreviewFileDiff;
  additions: number;
  deletions: number;
  newFile: boolean;
  action: "create" | "update" | "delete" | "rename";
  snapshotText?: string;
  afterSha?: string;
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

function artifactAction(
  value: string | undefined,
  newFile: boolean,
): FileEdit["action"] {
  if (newFile || value === "add" || value === "create") return "create";
  if (value === "delete") return "delete";
  if (value === "move" || value === "rename") return "rename";
  return "update";
}

function normalizedArtifactPath(path: string): string {
  return path.replace(/\\/g, "/").replace(/^\.\//, "");
}

function addedFileTextFromPatch(patchText: string, path: string): string | undefined {
  const lines = patchText.replace(/\r\n?/g, "\n").split("\n");
  const wanted = normalizedArtifactPath(path);
  for (let index = 0; index < lines.length; index++) {
    const match = /^\*\*\* Add File: (.+)$/.exec(lines[index]);
    if (!match || normalizedArtifactPath(match[1].trim()) !== wanted) continue;
    const content: string[] = [];
    for (index += 1; index < lines.length; index++) {
      if (lines[index].startsWith("*** ")) break;
      if (!lines[index].startsWith("+")) return undefined;
      content.push(lines[index].slice(1));
    }
    return content.length > 0 ? `${content.join("\n")}\n` : "";
  }
  return undefined;
}

function artifactSnapshotText(item: ThreadItem, path: string): string | undefined {
  const args = parseJSON(item.arguments);
  if (!isRecord(args)) return undefined;
  if (item.name === "write_file") {
    return stringValue(args, "content");
  }
  if (item.name === "apply_patch") {
    const patchText =
      stringValue(args, "patchText") ??
      stringValue(args, "patch_text") ??
      stringValue(args, "patch");
    return patchText ? addedFileTextFromPatch(patchText, path) : undefined;
  }
  return undefined;
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
    const newFile = diffRecord?.new_file === true || file.action === "add";
    edits.push({
      path,
      item,
      diff,
      additions: stats.additions,
      deletions: stats.deletions,
      newFile,
      action: artifactAction(stringValue(file, "action"), newFile),
      snapshotText: newFile ? artifactSnapshotText(item, path) : undefined,
      afterSha: stringValue(file, "new_file_sha"),
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
      action: "create",
      snapshotText: artifactSnapshotText(item, path),
      afterSha: stringValue(result, "new_file_sha"),
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
    action: artifactAction(stringValue(result, "action"), newFile),
    snapshotText: artifactSnapshotText(item, path),
    afterSha: stringValue(result, "new_file_sha"),
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
      // Keep the latest operation as the source of the stable artifact view.
      existing.item = edit.item;
      existing.diff = edit.diff;
      existing.snapshotText = edit.snapshotText ?? existing.snapshotText;
      existing.afterSha = edit.afterSha ?? existing.afterSha;
      if (existing.action !== "create") existing.action = edit.action;
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
  onOpenFile,
  onOpenFileDiff,
}: {
  turn: Turn;
  cwd?: string;
  onOpenFile?: (path: string) => void;
  onOpenFileDiff?: (selection: TurnFileDiffSelection) => void;
}): JSX.Element | null {
  const { t, formatNumber } = useI18n();
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
          {t(
            edits.length === 1 ? "turnEdits.countOne" : "turnEdits.count",
            { count: formatNumber(edits.length) },
          )}
        </span>
      </div>
      <div className="turn-edit-summary-list">
        {visibleEdits.map((edit) => {
          const canOpenFile = Boolean(onOpenFile || onOpenFileDiff);
          const rowContent = (
            <>
              <span className="turn-edit-summary-file">
                <span className="turn-edit-summary-name" title={edit.path}>
                  {fileDisplayName(edit.path)}
                </span>
              </span>
              <span className="turn-edit-summary-stats">
                {edit.additions > 0 ? (
                  <span className="turn-edit-summary-add">+{edit.additions}</span>
                ) : null}
                {edit.deletions > 0 ? (
                  <span className="turn-edit-summary-delete">-{edit.deletions}</span>
                ) : null}
              </span>
            </>
          );
          return (
            <ToolDiffPreview
              diff={edit.diff}
              item={edit.item}
              key={edit.path}
            >
              {canOpenFile ? (
                <button
                  className="turn-edit-summary-row is-clickable"
                  type="button"
                  aria-label={t("turnEdits.openFile", { path: edit.path })}
                  onClick={() => {
                    if (onOpenFile) {
                      onOpenFile(edit.path);
                      return;
                    }
                    onOpenFileDiff?.({
                      artifactID: edit.item.id,
                      path: edit.path,
                      cwd,
                      action: edit.action,
                      diff: edit.diff,
                      snapshotText: edit.snapshotText,
                      afterSha: edit.afterSha,
                      additions: edit.additions,
                      deletions: edit.deletions,
                      newFile: edit.newFile,
                    });
                  }}
                >
                  {rowContent}
                </button>
              ) : (
                <div className="turn-edit-summary-row">{rowContent}</div>
              )}
            </ToolDiffPreview>
          );
        })}
        {hiddenCount > 0 ? (
          <div className="turn-edit-summary-more">
            <span>
              {t(hiddenCount === 1 ? "turnEdits.moreFileOne" : "turnEdits.moreFiles", {
                count: formatNumber(hiddenCount),
              })}
            </span>
            <button
              className="turn-edit-summary-more-button"
              type="button"
              onClick={() =>
                setVisibleCount((current) =>
                  Math.min(current + FILE_BATCH_SIZE, edits.length),
                )
              }
            >
              {t(nextCount === 1 ? "turnEdits.showMoreOne" : "turnEdits.showMore", {
                count: formatNumber(nextCount),
              })}
            </button>
          </div>
        ) : null}
      </div>
    </div>
  );
}
