import {
  closeSync,
  openSync,
  readdirSync,
  readSync,
  realpathSync,
  statSync,
  type Dirent,
} from "node:fs";
import { isAbsolute, join, relative, resolve } from "node:path";
import type {
  FileTreeListResult,
  RuntimeContext,
  WorkspaceDirectoryListResult,
  WorkspaceFileReadResult,
} from "../shared/protocol";

const FILE_TREE_MAX_PATHS = 4000;
const FILE_PREVIEW_MAX_BYTES = 512 * 1024;
const FILE_TREE_IGNORED_DIRS = new Set([
  ".git",
  ".next",
  ".turbo",
  ".vite",
  "coverage",
  "dist",
  "node_modules",
  "out",
  "target",
]);
const FILE_TREE_IGNORED_FILES = new Set([".DS_Store"]);

export class WorkspaceFileService {
  constructor(private readonly getRuntimeContext: () => RuntimeContext) {}

  fileTreeList(): FileTreeListResult {
    return fileTreeListResult(this.getRuntimeContext());
  }

  directoryList(path?: string): WorkspaceDirectoryListResult {
    return workspaceDirectoryListResult(this.getRuntimeContext(), path);
  }

  readFile(path: string): WorkspaceFileReadResult {
    return readWorkspaceFileResult(this.getRuntimeContext(), path);
  }
}

function fileTreeListResult(context: RuntimeContext): FileTreeListResult {
  const paths: string[] = [];
  const truncated = collectFileTreePaths(context.cwd, "", paths);
  return { root: context.cwd, paths, truncated };
}

function workspaceDirectoryListResult(
  context: RuntimeContext,
  path?: string,
): WorkspaceDirectoryListResult {
  const relativeDirectoryPath = normalizeWorkspaceDirectoryPath(path ?? "");
  const absoluteDirectoryPath = resolveWorkspaceDirectoryPath(
    context.cwd,
    relativeDirectoryPath,
  );
  const stats = statSync(absoluteDirectoryPath);
  if (!stats.isDirectory()) {
    throw new Error("selected path is not a directory");
  }

  let entries: Dirent[];
  try {
    entries = readdirSync(absoluteDirectoryPath, { withFileTypes: true });
  } catch {
    entries = [];
  }

  entries.sort(compareFileTreeEntries);

  const visibleEntries = [];
  let truncated = false;
  for (const entry of entries) {
    if (FILE_TREE_IGNORED_FILES.has(entry.name)) {
      continue;
    }

    const relativePath = relativeDirectoryPath
      ? `${relativeDirectoryPath}/${entry.name}`
      : entry.name;
    if (entry.isDirectory()) {
      if (FILE_TREE_IGNORED_DIRS.has(entry.name)) {
        continue;
      }
      visibleEntries.push({
        name: entry.name,
        path: `${relativePath}/`,
        kind: "directory" as const,
      });
    } else if (entry.isFile() || entry.isSymbolicLink()) {
      visibleEntries.push({
        name: entry.name,
        path: relativePath,
        kind: "file" as const,
      });
    }

    if (visibleEntries.length >= FILE_TREE_MAX_PATHS) {
      truncated = true;
      break;
    }
  }

  return {
    root: context.cwd,
    path: relativeDirectoryPath,
    entries: visibleEntries,
    truncated,
  };
}

function readWorkspaceFileResult(
  context: RuntimeContext,
  path: string,
): WorkspaceFileReadResult {
  const relativeFilePath = normalizeWorkspaceRelativePath(path);
  const absolutePath = resolveWorkspacePath(context.cwd, relativeFilePath);
  const stats = statSync(absolutePath);
  if (!stats.isFile()) {
    throw new Error("selected path is not a file");
  }

  const readLimit = Math.min(stats.size, FILE_PREVIEW_MAX_BYTES + 1);
  const buffer = Buffer.alloc(readLimit);
  const descriptor = openSync(absolutePath, "r");
  let bytesRead = 0;
  try {
    bytesRead = readSync(descriptor, buffer, 0, readLimit, 0);
  } finally {
    closeSync(descriptor);
  }
  const truncated = stats.size > FILE_PREVIEW_MAX_BYTES;
  const previewBuffer = buffer.subarray(
    0,
    truncated ? FILE_PREVIEW_MAX_BYTES : bytesRead,
  );
  const binary = previewBuffer.includes(0);

  return {
    root: context.cwd,
    path: relativeFilePath,
    absolute_path: absolutePath,
    size_bytes: stats.size,
    binary,
    truncated,
    text: binary ? undefined : previewBuffer.toString("utf8"),
  };
}

function normalizeWorkspaceRelativePath(path: string): string {
  const value = path
    .trim()
    .replace(/\\/g, "/")
    .replace(/^\/+/, "")
    .replace(/\/+$/, "");
  if (
    !value ||
    value.includes("\0") ||
    value.split("/").some((segment) => segment === "..")
  ) {
    throw new Error("invalid workspace file path");
  }
  return value;
}

function normalizeWorkspaceDirectoryPath(path: string): string {
  const value = path
    .trim()
    .replace(/\\/g, "/")
    .replace(/^\/+/, "")
    .replace(/\/+$/, "");
  if (
    value.includes("\0") ||
    value.split("/").some((segment) => segment === "..")
  ) {
    throw new Error("invalid workspace directory path");
  }
  return value;
}

function resolveWorkspacePath(root: string, relativeFilePath: string): string {
  const absolutePath = resolve(root, relativeFilePath);
  const relativeToRoot = relative(root, absolutePath);
  if (
    !relativeToRoot ||
    relativeToRoot.startsWith("..") ||
    isAbsolute(relativeToRoot)
  ) {
    throw new Error("file is outside the current workspace");
  }

  const realRoot = realpathSync(root);
  const realFile = realpathSync(absolutePath);
  const realRelative = relative(realRoot, realFile);
  if (
    !realRelative ||
    realRelative.startsWith("..") ||
    isAbsolute(realRelative)
  ) {
    throw new Error("file is outside the current workspace");
  }
  return absolutePath;
}

function resolveWorkspaceDirectoryPath(
  root: string,
  relativeDirectoryPath: string,
): string {
  const absolutePath = relativeDirectoryPath
    ? resolve(root, relativeDirectoryPath)
    : root;
  const relativeToRoot = relative(root, absolutePath);
  if (
    relativeToRoot &&
    (relativeToRoot.startsWith("..") || isAbsolute(relativeToRoot))
  ) {
    throw new Error("directory is outside the current workspace");
  }

  const realRoot = realpathSync(root);
  const realDirectory = realpathSync(absolutePath);
  const realRelative = relative(realRoot, realDirectory);
  if (
    realRelative &&
    (realRelative.startsWith("..") || isAbsolute(realRelative))
  ) {
    throw new Error("directory is outside the current workspace");
  }
  return absolutePath;
}

function compareFileTreeEntries(left: Dirent, right: Dirent): number {
  const leftDirectory = left.isDirectory();
  const rightDirectory = right.isDirectory();
  if (leftDirectory !== rightDirectory) {
    return leftDirectory ? -1 : 1;
  }
  return left.name.localeCompare(right.name, undefined, {
    sensitivity: "base",
  });
}

function collectFileTreePaths(
  root: string,
  relativeDirectory: string,
  paths: string[],
): boolean {
  if (paths.length >= FILE_TREE_MAX_PATHS) {
    return true;
  }

  const directoriesToRead = [relativeDirectory];
  for (let index = 0; index < directoriesToRead.length; index += 1) {
    const currentRelativeDirectory = directoriesToRead[index];
    const directory = currentRelativeDirectory
      ? join(root, currentRelativeDirectory)
      : root;
    let entries: Dirent[];
    try {
      entries = readdirSync(directory, { withFileTypes: true });
    } catch {
      continue;
    }

    entries.sort(compareFileTreeEntries);

    for (const entry of entries) {
      if (FILE_TREE_IGNORED_FILES.has(entry.name)) {
        continue;
      }

      const relativePath = currentRelativeDirectory
        ? `${currentRelativeDirectory}/${entry.name}`
        : entry.name;
      if (entry.isDirectory()) {
        if (FILE_TREE_IGNORED_DIRS.has(entry.name)) {
          continue;
        }
        paths.push(`${relativePath}/`);
        directoriesToRead.push(relativePath);
      } else if (entry.isFile() || entry.isSymbolicLink()) {
        paths.push(relativePath);
      }

      if (paths.length >= FILE_TREE_MAX_PATHS) {
        return true;
      }
    }
  }

  return false;
}
