import type { ToolDiffPreviewFileDiff } from "./ToolDiffPreview";

export type TurnFileDiffSelection = {
  path: string;
  diff: ToolDiffPreviewFileDiff;
  additions: number;
  deletions: number;
  newFile: boolean;
};
