import {
  app,
  BrowserWindow,
  dialog,
  ipcMain,
  type OpenDialogOptions,
} from "electron";
import { spawnSync } from "node:child_process";
import {
  closeSync,
  openSync,
  readdirSync,
  readFileSync,
  readSync,
  realpathSync,
  statSync,
  type Dirent,
} from "node:fs";
import {
  basename,
  dirname,
  isAbsolute,
  join,
  relative,
  resolve,
} from "node:path";
import { fileURLToPath } from "node:url";
import type {
  ConfigCodexModelsResult,
  ConfigModelUpdateResult,
  GitChangeFile,
  GitChangesResult,
  GitCommitParams,
  GitCommitResult,
  GitCreateBranchResult,
  GitDiffStats,
  GitFileDiffResult,
  GitPullRequestParams,
  GitPullRequestResult,
  FileTreeListResult,
  GitStatusResult,
  BuildInfoResult,
  CoreBuildInfo,
  DesktopBuildInfo,
  InputImage,
  InitializeResult,
  RuntimeContext,
  ServerEvent,
  TerminalSessionStartParams,
  Thread,
  Turn,
  WorkspaceDirectoryListResult,
  WorkspaceFileReadResult,
} from "../shared/protocol";
import { AppServerClientPool } from "./appServerClients";
import { ProjectManager } from "./projects";
import {
  registerRenderableFileProtocol,
  registerRenderableFileScheme,
} from "./renderableFileProtocol";
import { TerminalSessionManager } from "./terminalSessions";

const __dirname = dirname(fileURLToPath(import.meta.url));
const FILE_TREE_MAX_PATHS = 4000;
const FILE_PREVIEW_MAX_BYTES = 512 * 1024;
const GIT_DIFF_PREVIEW_MAX_BYTES = 512 * 1024;
const GIT_DIFF_COMMAND_MAX_BUFFER = 8 * 1024 * 1024;
const MAIN_WINDOW_DEFAULT_WIDTH = 1280;
const MAIN_WINDOW_DEFAULT_HEIGHT = 920;
const MAIN_WINDOW_MIN_WIDTH = 980;
const MAIN_WINDOW_MIN_HEIGHT = 920;
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

registerRenderableFileScheme();

type GitStatusOptions = {
  includePullRequestURL?: boolean;
  includeRemoteDefaultBranchFallback?: boolean;
};

let mainWindow: BrowserWindow | null = null;
const projectManager = new ProjectManager();

// Build-time globals injected by electron.vite.config.ts. TypeScript
// doesn't know about them by default; declare them so we can reference
// them inside main. The "undefined" type keeps the same declaration
// valid for unit-test contexts where the define hasn't run.
declare const __DESKTOP_VERSION__: string | undefined;
declare const __DESKTOP_BUILD_DATE__: string | undefined;

const DESKTOP_BUILD_INFO: DesktopBuildInfo = {
  version:
    typeof __DESKTOP_VERSION__ === "string"
      ? __DESKTOP_VERSION__
      : "0.0.0-test",
  date:
    typeof __DESKTOP_BUILD_DATE__ === "string"
      ? __DESKTOP_BUILD_DATE__
      : "1970-01-01T00:00:00Z",
};

// Cached core build info. Populated on the first wuu:initialize call so
// the renderer can ask for build identity via wuu:build-info without
// racing the first initialize.
let cachedCoreBuildInfo: CoreBuildInfo | undefined;
let windowResizeEndTimer: NodeJS.Timeout | undefined;
let windowResizeState = false;
const appServerClientPool = new AppServerClientPool(
  () => projectManager.ensureRuntimeContext(),
  () => projectManager.activeWorkdir(),
  (event) => emitServerEvent(event),
);
const terminalSessionManager = new TerminalSessionManager(
  () => projectManager.ensureRuntimeContext(),
  (event) => emitTerminalEvent(event),
);

function gitStatusResult(options: GitStatusOptions = {}): GitStatusResult {
  const context = projectManager.ensureRuntimeContext();
  const root =
    gitOutput(context.cwd, ["rev-parse", "--show-toplevel"]) ?? context.cwd;
  const insideWorkTree =
    gitOutput(root, ["rev-parse", "--is-inside-work-tree"]) === "true";
  if (!insideWorkTree) {
    return {
      is_repo: false,
      dirty_count: 0,
      diff: emptyGitDiffStats(),
      staged_diff: emptyGitDiffStats(),
    };
  }

  const branchName = gitOutput(root, ["branch", "--show-current"]);
  const head = gitOutput(root, ["rev-parse", "--short", "HEAD"]);
  const branch = branchName || head;
  const branches = gitOutput(root, [
    "for-each-ref",
    "--format=%(refname:short)",
    "refs/heads",
  ])
    ?.split("\n")
    .map((item) => item.trim())
    .filter(Boolean);
  const porcelain = gitOutput(root, ["status", "--porcelain"]);
  const dirtyCount = porcelain
    ? porcelain.split("\n").filter((line) => line.trim()).length
    : 0;
  const upstream = gitOutput(root, [
    "rev-parse",
    "--abbrev-ref",
    "--symbolic-full-name",
    "@{u}",
  ]);
  const [aheadCount, behindCount] = upstream ? gitAheadBehind(root) : [0, 0];
  const remote = upstream?.split("/")[0] || firstGitRemote(root);
  const defaultBranch = remote
    ? gitDefaultBranch(
        root,
        remote,
        Boolean(options.includeRemoteDefaultBranchFallback),
      )
    : undefined;
  const ghAvailable = commandAvailable("gh", ["--version"]);
  const prURL =
    options.includePullRequestURL && branchName && ghAvailable
      ? ghPullRequestURL(root)
      : undefined;

  return {
    is_repo: true,
    branch,
    branches,
    dirty_count: dirtyCount,
    detached: !branchName,
    diff: gitDiffStats(root, true),
    staged_diff: gitStagedDiffStats(root),
    upstream,
    ahead_count: aheadCount,
    behind_count: behindCount,
    remote,
    default_branch: defaultBranch,
    gh_available: ghAvailable,
    pr_url: prURL,
  };
}

function gitChangesResult(): GitChangesResult {
  const context = projectManager.ensureRuntimeContext();
  const root =
    gitOutput(context.cwd, ["rev-parse", "--show-toplevel"]) ?? context.cwd;
  const insideWorkTree =
    gitOutput(root, ["rev-parse", "--is-inside-work-tree"]) === "true";
  if (!insideWorkTree) {
    return { is_repo: false, files: [] };
  }

  const filesByPath = new Map<string, GitChangeFile>();
  for (const file of parseGitNameStatus(
    gitOutput(root, [
      "diff",
      "--name-status",
      "--find-renames",
      "HEAD",
      "--",
    ]) ?? "",
  )) {
    filesByPath.set(file.path, file);
  }

  for (const file of parseGitNumstatFiles(
    gitOutput(root, ["diff", "--numstat", "--find-renames", "HEAD", "--"]) ??
      "",
  )) {
    const existing = filesByPath.get(file.path);
    filesByPath.set(file.path, {
      ...file,
      ...existing,
      additions: file.additions,
      deletions: file.deletions,
      binary: existing?.binary || file.binary,
    });
  }

  for (const path of listUntrackedGitFiles(root)) {
    const stats = untrackedGitFileStats(root, path);
    filesByPath.set(path, {
      path,
      status: "untracked",
      additions: stats.additions,
      deletions: 0,
      binary: stats.binary,
    });
  }

  return {
    is_repo: true,
    root,
    files: Array.from(filesByPath.values()).sort((left, right) =>
      left.path.localeCompare(right.path),
    ),
  };
}

function gitFileDiffResult(path: string): GitFileDiffResult {
  const context = projectManager.ensureRuntimeContext();
  const root =
    gitOutput(context.cwd, ["rev-parse", "--show-toplevel"]) ?? context.cwd;
  const insideWorkTree =
    gitOutput(root, ["rev-parse", "--is-inside-work-tree"]) === "true";
  const { relativePath, absolutePath } = resolveGitRelativePath(root, path);
  if (!insideWorkTree) {
    return emptyGitFileDiffResult(relativePath, false);
  }

  const change = gitChangesResult().files.find(
    (file) => file.path === relativePath,
  ) ?? {
    path: relativePath,
    status: "unknown" as const,
    additions: 0,
    deletions: 0,
  };

  if (change.status === "untracked") {
    return gitUntrackedFileDiffResult(root, absolutePath, change);
  }

  const rawPatch = gitDiffOutput(root, relativePath);
  const truncatedPatch = truncateTextBytes(
    rawPatch,
    GIT_DIFF_PREVIEW_MAX_BYTES,
  );
  const binary =
    change.binary ||
    rawPatch.includes("Binary files ") ||
    rawPatch.includes("GIT binary patch");
  return {
    is_repo: true,
    path: change.path,
    old_path: change.old_path,
    status: change.status,
    additions: change.additions,
    deletions: change.deletions,
    binary,
    patch: truncatedPatch.text,
    truncated: truncatedPatch.truncated,
  };
}

function checkoutGitBranch(branch: string): GitStatusResult {
  const context = projectManager.ensureRuntimeContext();
  const current = gitStatusResult();
  const target = branch.trim();
  if (!current.is_repo) {
    throw new Error("current workspace is not a git repository");
  }
  if (!target || !current.branches?.includes(target)) {
    throw new Error("branch not found");
  }
  const result = spawnSync("git", ["-C", context.cwd, "checkout", target], {
    cwd: context.cwd,
    encoding: "utf8",
    env: process.env,
  });
  if (result.status !== 0) {
    throw new Error(result.stderr.trim() || `failed to checkout ${target}`);
  }
  return gitStatusResult();
}

function createCheckoutGitBranch(branch: string): GitCreateBranchResult {
  const context = projectManager.ensureRuntimeContext();
  const current = gitStatusResult();
  const target = branch.trim();
  if (!current.is_repo) {
    throw new Error("current workspace is not a git repository");
  }
  validateGitBranchName(context.cwd, target);
  if (current.branches?.includes(target)) {
    throw new Error("branch already exists");
  }
  gitRun(context.cwd, ["checkout", "-b", target]);
  return { status: gitStatusResult() };
}

function commitGitChanges(params: GitCommitParams): GitCommitResult {
  const context = projectManager.ensureRuntimeContext();
  const current = gitStatusResult();
  if (!current.is_repo) {
    throw new Error("current workspace is not a git repository");
  }
  if (params.include_unstaged !== false) {
    gitRun(context.cwd, ["add", "-A"]);
  }
  const stagedDiff = gitStagedDiffStats(context.cwd);
  if (stagedDiff.files === 0) {
    throw new Error("there are no staged changes to commit");
  }
  const message = params.message?.trim() || generatedCommitMessage(context.cwd);
  gitRun(context.cwd, ["commit", "-m", message]);
  const commit = gitOutput(context.cwd, ["rev-parse", "--short", "HEAD"]) ?? "";
  return {
    status: gitStatusResult(),
    commit,
    message,
  };
}

function createPullRequest(params: GitPullRequestParams): GitPullRequestResult {
  const context = projectManager.ensureRuntimeContext();
  const status = gitStatusResult({
    includePullRequestURL: true,
    includeRemoteDefaultBranchFallback: true,
  });
  if (!status.is_repo) {
    throw new Error("current workspace is not a git repository");
  }
  if (!status.gh_available) {
    throw new Error("GitHub CLI is not available");
  }
  const branch = gitOutput(context.cwd, ["branch", "--show-current"]);
  if (!branch) {
    throw new Error("pull requests require a named branch");
  }
  if (status.default_branch && branch === status.default_branch) {
    throw new Error("create a feature branch before opening a pull request");
  }
  if (status.dirty_count > 0) {
    throw new Error(
      "commit or discard local changes before opening a pull request",
    );
  }

  const existingURL = status.pr_url;
  if (existingURL) {
    return { status, url: existingURL, already_exists: true };
  }

  if (!status.upstream) {
    const remote = status.remote || "origin";
    gitRun(context.cwd, ["push", "-u", remote, branch]);
  }

  const args = ["pr", "create"];
  if (params.draft) {
    args.push("--draft");
  }
  const title = params.title?.trim();
  const body = params.body?.trim();
  if (title || body) {
    args.push("--title", title || branch, "--body", body || "");
  } else {
    args.push("--fill");
  }
  const url = ghOutput(context.cwd, args);
  if (!url) {
    throw new Error("GitHub CLI did not return a pull request URL");
  }
  return {
    status: {
      ...gitStatusResult({ includeRemoteDefaultBranchFallback: true }),
      pr_url: url,
    },
    url,
    already_exists: false,
  };
}

function validateGitBranchName(cwd: string, branch: string): void {
  if (!branch) {
    throw new Error("branch name is required");
  }
  const result = spawnSync(
    "git",
    ["-C", cwd, "check-ref-format", "--branch", branch],
    {
      cwd,
      encoding: "utf8",
      env: process.env,
    },
  );
  if (result.status !== 0) {
    throw new Error(result.stderr.trim() || "invalid branch name");
  }
}

function gitRun(cwd: string, args: string[]): string {
  const result = spawnSync("git", ["-C", cwd, ...args], {
    cwd,
    encoding: "utf8",
    env: process.env,
  });
  if (result.status !== 0) {
    throw new Error(
      result.stderr.trim() ||
        result.stdout.trim() ||
        `git ${args.join(" ")} failed`,
    );
  }
  return result.stdout.trim();
}

function gitOutput(cwd: string, args: string[]): string | undefined {
  const result = spawnSync("git", ["-C", cwd, ...args], {
    cwd,
    encoding: "utf8",
    env: process.env,
  });
  if (result.status !== 0) {
    return undefined;
  }
  return result.stdout.trim() || undefined;
}

function emptyGitDiffStats(): GitDiffStats {
  return { files: 0, additions: 0, deletions: 0 };
}

function gitDiffStats(cwd: string, includeUntracked: boolean): GitDiffStats {
  const stats = parseGitNumstat(
    gitOutput(cwd, ["diff", "--numstat", "HEAD", "--"]) ?? "",
  );
  if (!includeUntracked) {
    return stats;
  }
  const untracked = gitOutput(cwd, [
    "ls-files",
    "--others",
    "--exclude-standard",
  ])
    ?.split("\n")
    .map((item) => item.trim())
    .filter(Boolean);
  if (!untracked?.length) {
    return stats;
  }
  let additions = 0;
  for (const path of untracked.slice(0, 100)) {
    additions += countTextFileLines(resolve(cwd, path));
  }
  return {
    files: stats.files + untracked.length,
    additions: stats.additions + additions,
    deletions: stats.deletions,
  };
}

function gitStagedDiffStats(cwd: string): GitDiffStats {
  return parseGitNumstat(
    gitOutput(cwd, ["diff", "--cached", "--numstat", "--"]) ?? "",
  );
}

function gitDiffOutput(cwd: string, relativePath: string): string {
  const result = spawnSync(
    "git",
    [
      "-C",
      cwd,
      "diff",
      "--no-ext-diff",
      "--find-renames",
      "--unified=3",
      "HEAD",
      "--",
      relativePath,
    ],
    {
      cwd,
      encoding: "utf8",
      env: process.env,
      maxBuffer: GIT_DIFF_COMMAND_MAX_BUFFER,
    },
  );
  if (result.status !== 0 && !result.stdout) {
    throw new Error(
      result.stderr.trim() || `git diff failed for ${relativePath}`,
    );
  }
  return result.stdout;
}

function parseGitNumstat(output: string): GitDiffStats {
  const stats = emptyGitDiffStats();
  for (const line of output.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) {
      continue;
    }
    const [additions, deletions] = trimmed.split(/\s+/, 3);
    stats.files += 1;
    if (additions !== "-") {
      stats.additions += Number(additions) || 0;
    }
    if (deletions !== "-") {
      stats.deletions += Number(deletions) || 0;
    }
  }
  return stats;
}

function parseGitNameStatus(output: string): GitChangeFile[] {
  const files: GitChangeFile[] = [];
  for (const line of output.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) {
      continue;
    }
    const columns = trimmed.split("\t");
    const statusCode = columns[0] ?? "";
    const status = gitChangeStatus(statusCode);
    const oldPath =
      status === "renamed" || status === "copied" ? columns[1] : undefined;
    const path =
      status === "renamed" || status === "copied" ? columns[2] : columns[1];
    if (!path) {
      continue;
    }
    files.push({
      path,
      old_path: oldPath,
      status,
      additions: 0,
      deletions: 0,
    });
  }
  return files;
}

function parseGitNumstatFiles(output: string): GitChangeFile[] {
  const files: GitChangeFile[] = [];
  for (const line of output.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) {
      continue;
    }
    const columns = trimmed.split("\t");
    if (columns.length < 3) {
      continue;
    }
    const additions = columns[0];
    const deletions = columns[1];
    const path = columns.at(-1);
    if (!path) {
      continue;
    }
    files.push({
      path,
      status: "unknown",
      additions: additions === "-" ? 0 : Number(additions) || 0,
      deletions: deletions === "-" ? 0 : Number(deletions) || 0,
      binary: additions === "-" || deletions === "-",
    });
  }
  return files;
}

function gitChangeStatus(statusCode: string): GitChangeFile["status"] {
  switch (statusCode[0]) {
    case "M":
      return "modified";
    case "A":
      return "added";
    case "D":
      return "deleted";
    case "R":
      return "renamed";
    case "C":
      return "copied";
    default:
      return "unknown";
  }
}

function listUntrackedGitFiles(cwd: string): string[] {
  return (
    gitOutput(cwd, ["ls-files", "--others", "--exclude-standard"])
      ?.split("\n")
      .map((item) => item.trim())
      .filter(Boolean) ?? []
  );
}

function untrackedGitFileStats(
  root: string,
  path: string,
): { additions: number; binary: boolean } {
  const { absolutePath } = resolveGitRelativePath(root, path);
  try {
    const stats = statSync(absolutePath);
    if (!stats.isFile()) {
      return { additions: 0, binary: false };
    }
    const previewBuffer = readFilePreviewBuffer(
      absolutePath,
      Math.min(stats.size, FILE_PREVIEW_MAX_BYTES),
    );
    const binary = previewBuffer.includes(0);
    return {
      additions: binary ? 0 : countTextFileLines(absolutePath),
      binary,
    };
  } catch {
    return { additions: 0, binary: false };
  }
}

function gitUntrackedFileDiffResult(
  root: string,
  absolutePath: string,
  change: GitChangeFile,
): GitFileDiffResult {
  try {
    const stats = statSync(absolutePath);
    if (!stats.isFile()) {
      return emptyGitFileDiffResult(change.path, true);
    }
    const readLimit = Math.min(stats.size, GIT_DIFF_PREVIEW_MAX_BYTES + 1);
    const buffer = readFilePreviewBuffer(absolutePath, readLimit);
    const truncated = stats.size > GIT_DIFF_PREVIEW_MAX_BYTES;
    const previewBuffer = buffer.subarray(
      0,
      truncated ? GIT_DIFF_PREVIEW_MAX_BYTES : buffer.length,
    );
    const binary = previewBuffer.includes(0);
    const patch = binary
      ? `Binary file ${change.path} is untracked`
      : buildUntrackedPatch(
          change.path,
          previewBuffer.toString("utf8"),
          truncated,
        );
    return {
      is_repo: true,
      path: change.path,
      old_path: change.old_path,
      status: change.status,
      additions: change.additions,
      deletions: change.deletions,
      binary,
      patch,
      truncated,
    };
  } catch {
    return emptyGitFileDiffResult(change.path, true);
  }
}

function buildUntrackedPatch(
  path: string,
  text: string,
  truncated: boolean,
): string {
  const lines = splitPatchTextLines(text);
  const patchLines = [
    `diff --git a/${path} b/${path}`,
    "new file mode 100644",
    "--- /dev/null",
    `+++ b/${path}`,
    `@@ -0,0 +1,${lines.length} @@`,
    ...lines.map((line) => `+${line}`),
  ];
  if (truncated) {
    patchLines.push("+");
    patchLines.push("+[diff truncated]");
  }
  return patchLines.join("\n");
}

function splitPatchTextLines(text: string): string[] {
  if (!text) {
    return [];
  }
  const withoutFinalNewline = text.endsWith("\n") ? text.slice(0, -1) : text;
  return withoutFinalNewline ? withoutFinalNewline.split(/\r?\n/) : [];
}

function readFilePreviewBuffer(filePath: string, readLimit: number): Buffer {
  const buffer = Buffer.alloc(readLimit);
  const descriptor = openSync(filePath, "r");
  let bytesRead = 0;
  try {
    bytesRead = readSync(descriptor, buffer, 0, readLimit, 0);
  } finally {
    closeSync(descriptor);
  }
  return buffer.subarray(0, bytesRead);
}

function resolveGitRelativePath(
  root: string,
  path: string,
): { relativePath: string; absolutePath: string } {
  const relativePath = normalizeWorkspaceRelativePath(path);
  const absolutePath = resolve(root, relativePath);
  const relativeToRoot = relative(root, absolutePath);
  if (
    !relativeToRoot ||
    relativeToRoot.startsWith("..") ||
    isAbsolute(relativeToRoot)
  ) {
    throw new Error("file is outside the current git repository");
  }
  return { relativePath, absolutePath };
}

function truncateTextBytes(
  text: string,
  maxBytes: number,
): { text: string; truncated: boolean } {
  const buffer = Buffer.from(text, "utf8");
  if (buffer.byteLength <= maxBytes) {
    return { text, truncated: false };
  }
  return {
    text: `${buffer.subarray(0, maxBytes).toString("utf8")}\n[diff truncated]\n`,
    truncated: true,
  };
}

function emptyGitFileDiffResult(
  path: string,
  isRepo: boolean,
): GitFileDiffResult {
  return {
    is_repo: isRepo,
    path,
    status: "unknown",
    additions: 0,
    deletions: 0,
    binary: false,
    patch: "",
    truncated: false,
  };
}

function countTextFileLines(filePath: string): number {
  try {
    const stats = statSync(filePath);
    if (!stats.isFile() || stats.size > 1024 * 1024) {
      return 0;
    }
    const content = readFileSync(filePath);
    if (content.includes(0)) {
      return 0;
    }
    const text = content.toString("utf8");
    if (!text) {
      return 0;
    }
    return text.endsWith("\n")
      ? text.split("\n").length - 1
      : text.split(/\r\n|\n|\r/).length;
  } catch {
    return 0;
  }
}

function gitAheadBehind(cwd: string): [number, number] {
  const output = gitOutput(cwd, [
    "rev-list",
    "--left-right",
    "--count",
    "HEAD...@{u}",
  ]);
  const [ahead, behind] = output
    ?.split(/\s+/, 2)
    .map((item) => Number(item) || 0) ?? [0, 0];
  return [ahead, behind];
}

function firstGitRemote(cwd: string): string | undefined {
  return gitOutput(cwd, ["remote"])
    ?.split("\n")
    .map((item) => item.trim())
    .find(Boolean);
}

function gitDefaultBranch(
  cwd: string,
  remote: string,
  includeRemoteFallback = false,
): string | undefined {
  const symbolic = gitOutput(cwd, [
    "symbolic-ref",
    "--short",
    `refs/remotes/${remote}/HEAD`,
  ]);
  if (symbolic?.startsWith(`${remote}/`)) {
    return symbolic.slice(remote.length + 1);
  }
  if (!includeRemoteFallback) {
    return undefined;
  }
  return gitOutput(cwd, ["remote", "show", remote])
    ?.split("\n")
    .map((line) => line.trim())
    .find((line) => line.startsWith("HEAD branch:"))
    ?.replace("HEAD branch:", "")
    .trim();
}

function commandAvailable(command: string, args: string[]): boolean {
  const result = spawnSync(command, args, {
    encoding: "utf8",
    env: process.env,
  });
  return result.status === 0;
}

function ghOutput(cwd: string, args: string[]): string | undefined {
  const result = spawnSync("gh", args, {
    cwd,
    encoding: "utf8",
    env: process.env,
  });
  if (result.status !== 0) {
    if (args[0] === "pr" && args[1] === "view") {
      return undefined;
    }
    throw new Error(
      result.stderr.trim() ||
        result.stdout.trim() ||
        `gh ${args.join(" ")} failed`,
    );
  }
  return result.stdout.trim() || undefined;
}

function ghPullRequestURL(cwd: string): string | undefined {
  return ghOutput(cwd, ["pr", "view", "--json", "url", "--jq", ".url"]);
}

function generatedCommitMessage(cwd: string): string {
  const files = gitOutput(cwd, ["diff", "--cached", "--name-only"])
    ?.split("\n")
    .map((item) => item.trim())
    .filter(Boolean);
  if (!files?.length) {
    return "Update workspace changes";
  }
  if (files.length === 1) {
    return `Update ${basename(files[0])}`;
  }
  const topLevel = files.map((file) => file.split("/", 1)[0]).filter(Boolean);
  const sharedArea =
    topLevel.length > 0 && topLevel.every((item) => item === topLevel[0])
      ? topLevel[0]
      : "";
  return sharedArea
    ? `Update ${sharedArea} changes`
    : "Update workspace changes";
}

function fileTreeListResult(): FileTreeListResult {
  const context = projectManager.ensureRuntimeContext();
  const paths: string[] = [];
  const truncated = collectFileTreePaths(context.cwd, "", paths);
  return { root: context.cwd, paths, truncated };
}

function workspaceDirectoryListResult(
  path?: string,
): WorkspaceDirectoryListResult {
  const context = projectManager.ensureRuntimeContext();
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

function readWorkspaceFileResult(path: string): WorkspaceFileReadResult {
  const context = projectManager.ensureRuntimeContext();
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

async function showProjectDirectoryDialog(
  options: OpenDialogOptions,
): Promise<string | undefined> {
  const result = mainWindow
    ? await dialog.showOpenDialog(mainWindow, options)
    : await dialog.showOpenDialog(options);
  if (result.canceled) {
    return undefined;
  }
  return result.filePaths[0];
}

function emitServerEvent(event: ServerEvent): void {
  if (
    !mainWindow ||
    mainWindow.isDestroyed() ||
    mainWindow.webContents.isDestroyed()
  ) {
    return;
  }
  mainWindow.webContents.send("wuu:server-event", event);
}

function emitTerminalEvent(
  event: Parameters<TerminalSessionManager["emit"]>[0],
): void {
  if (
    !mainWindow ||
    mainWindow.isDestroyed() ||
    mainWindow.webContents.isDestroyed()
  ) {
    return;
  }
  mainWindow.webContents.send("wuu:terminal-event", event);
}

function setWindowResizeState(resizing: boolean): void {
  if (windowResizeState === resizing) {
    return;
  }
  windowResizeState = resizing;
  if (
    !mainWindow ||
    mainWindow.isDestroyed() ||
    mainWindow.webContents.isDestroyed()
  ) {
    return;
  }
  mainWindow.webContents.send("wuu:window-resize-state", { resizing });
}

function scheduleWindowResizeEnd(delay = 140): void {
  if (windowResizeEndTimer) {
    clearTimeout(windowResizeEndTimer);
  }
  windowResizeEndTimer = setTimeout(() => {
    windowResizeEndTimer = undefined;
    setWindowResizeState(false);
  }, delay);
}

function createWindow(): void {
  mainWindow = new BrowserWindow({
    width: MAIN_WINDOW_DEFAULT_WIDTH,
    height: MAIN_WINDOW_DEFAULT_HEIGHT,
    minWidth: MAIN_WINDOW_MIN_WIDTH,
    minHeight: MAIN_WINDOW_MIN_HEIGHT,
    titleBarStyle: "hiddenInset",
    trafficLightPosition: { x: 18, y: 16 },
    backgroundColor: "#f6f6f4",
    webPreferences: {
      preload: join(__dirname, "../preload/index.cjs"),
      contextIsolation: true,
      nodeIntegration: false,
      webviewTag: true,
    },
  });

  mainWindow.on("will-resize", () => {
    setWindowResizeState(true);
    scheduleWindowResizeEnd();
  });
  mainWindow.on("resize", () => {
    setWindowResizeState(true);
    scheduleWindowResizeEnd();
  });
  mainWindow.on("resized", () => {
    scheduleWindowResizeEnd(40);
  });
  mainWindow.on("closed", () => {
    if (windowResizeEndTimer) {
      clearTimeout(windowResizeEndTimer);
      windowResizeEndTimer = undefined;
    }
    windowResizeState = false;
    mainWindow = null;
  });

  if (!app.isPackaged) {
    mainWindow.webContents.on("console-message", (_event, _level, message) => {
      if (message) {
        console.error(`[renderer] ${message}`);
      }
    });
    mainWindow.webContents.on("preload-error", (_event, preloadPath, error) => {
      console.error(`[preload] ${preloadPath}: ${error.message}`);
    });
  }

  if (!app.isPackaged && process.env.ELECTRON_RENDERER_URL) {
    mainWindow.loadURL(process.env.ELECTRON_RENDERER_URL);
  } else {
    mainWindow.loadFile(join(__dirname, "../renderer/index.html"));
  }
}

app.whenReady().then(() => {
  projectManager.load();
  registerRenderableFileProtocol();

  ipcMain.handle("wuu:project-list", () => projectManager.list());
  ipcMain.handle("wuu:project-select", (_event, projectIDToSelect: string) =>
    projectManager.select(projectIDToSelect),
  );
  ipcMain.handle(
    "wuu:project-select-none",
    (_event, fresh?: boolean, cwd?: string) =>
      projectManager.selectNoProject(Boolean(fresh), cwd),
  );
  ipcMain.handle("wuu:git-status", () => gitStatusResult());
  ipcMain.handle("wuu:git-changes", () => gitChangesResult());
  ipcMain.handle("wuu:git-file-diff", (_event, path: string) =>
    gitFileDiffResult(path),
  );
  ipcMain.handle("wuu:git-checkout-branch", (_event, branch: string) =>
    checkoutGitBranch(branch),
  );
  ipcMain.handle("wuu:git-create-checkout-branch", (_event, branch: string) =>
    createCheckoutGitBranch(branch),
  );
  ipcMain.handle("wuu:git-commit", (_event, params: GitCommitParams) =>
    commitGitChanges(params ?? {}),
  );
  ipcMain.handle("wuu:git-create-pr", (_event, params: GitPullRequestParams) =>
    createPullRequest(params ?? {}),
  );
  ipcMain.handle("wuu:file-tree-list", () => fileTreeListResult());
  ipcMain.handle("wuu:file-directory-list", (_event, path?: string) =>
    workspaceDirectoryListResult(path),
  );
  ipcMain.handle("wuu:file-read", (_event, path: string) =>
    readWorkspaceFileResult(path),
  );
  ipcMain.handle(
    "wuu:terminal-start",
    (_event, params?: TerminalSessionStartParams) =>
      terminalSessionManager.start(params),
  );
  ipcMain.handle("wuu:terminal-write", (_event, id: string, data: string) =>
    terminalSessionManager.write(id, data),
  );
  ipcMain.handle(
    "wuu:terminal-resize",
    (_event, id: string, cols: number, rows: number) =>
      terminalSessionManager.resize(id, cols, rows),
  );
  ipcMain.handle("wuu:terminal-stop", (_event, id: string) =>
    terminalSessionManager.stop(id),
  );
  ipcMain.handle("wuu:project-choose-folder", async () => {
    const projectPath = await showProjectDirectoryDialog({
      title: "使用现有文件夹",
      buttonLabel: "使用文件夹",
      properties: ["openDirectory"],
    });
    if (!projectPath) {
      return projectManager.list();
    }
    return projectManager.add(projectPath);
  });
  ipcMain.handle("wuu:project-create-blank", async () => {
    const projectPath = await showProjectDirectoryDialog({
      title: "新建空白项目",
      buttonLabel: "创建项目",
      properties: ["openDirectory", "createDirectory"],
    });
    if (!projectPath) {
      return projectManager.list();
    }
    return projectManager.add(projectPath);
  });
  ipcMain.handle("wuu:initialize", async () => {
    const result = await appServerClientPool.request<InitializeResult>("initialize");
    if (result.core) {
      cachedCoreBuildInfo = result.core;
    }
    return result;
  });
  ipcMain.handle("wuu:build-info", (): BuildInfoResult => ({
    core: cachedCoreBuildInfo,
    desktop: DESKTOP_BUILD_INFO,
  }));
  ipcMain.handle("wuu:config-codex-models", (_event, provider?: string) =>
    appServerClientPool.request<ConfigCodexModelsResult>("config/codex/models", {
      provider: provider ?? "",
    }),
  );
  ipcMain.handle(
    "wuu:config-model-update",
    (
      _event,
      provider: string,
      model: string,
      effort?: string,
      connection?: {
        base_url?: string;
        api_key?: string;
        create_provider?: boolean;
      },
      variant?: string,
    ) =>
      appServerClientPool.request<ConfigModelUpdateResult>("config/model/update", {
        provider,
        model,
        ...(connection?.base_url === undefined
          ? {}
          : { base_url: connection.base_url }),
        ...(connection?.api_key === undefined
          ? {}
          : { api_key: connection.api_key }),
        ...(connection?.create_provider ? { create_provider: true } : {}),
        ...(effort === undefined ? {} : { effort }),
        ...(variant === undefined ? {} : { variant }),
      }),
  );
  ipcMain.handle("wuu:skill-list", () => appServerClientPool.request("skill/list"));
  ipcMain.handle("wuu:thread-start", () =>
    appServerClientPool.request<{ thread: Thread }>("thread/start"),
  );
  ipcMain.handle("wuu:thread-resume", (_event, sessionId?: string) =>
    appServerClientPool.request<{ thread: Thread }>("thread/resume", {
      session_id: sessionId ?? "",
    }),
  );
  ipcMain.handle(
    "wuu:thread-fork",
    (_event, threadId: string, turnId?: string, itemId?: string) =>
      appServerClientPool.request<{ thread: Thread }>("thread/fork", {
        thread_id: threadId,
        turn_id: turnId ?? "",
        item_id: itemId ?? "",
      }),
  );
  ipcMain.handle("wuu:thread-list", () =>
    appServerClientPool.request<{ threads: Thread[] }>("thread/list"),
  );
  ipcMain.handle("wuu:thread-search", (_event, query: string, limit?: number) =>
    appServerClientPool.request("thread/search", {
      query: query ?? "",
      limit: typeof limit === "number" ? limit : undefined,
    }),
  );
  ipcMain.handle(
    "wuu:thread-pin",
    (_event, threadId: string, pinned: boolean) =>
      appServerClientPool.request<{ thread: Thread }>("thread/pin", {
        thread_id: threadId,
        pinned,
      }),
  );
  ipcMain.handle(
    "wuu:thread-archive",
    (_event, threadId: string, archived: boolean) =>
      appServerClientPool.request<{ thread: Thread }>("thread/archive", {
        thread_id: threadId,
        archived,
      }),
  );
  ipcMain.handle(
    "wuu:turn-start",
    (_event, threadId: string, prompt: string, images?: InputImage[]) =>
      appServerClientPool.request<{ turn: Turn }>("turn/start", {
        thread_id: threadId,
        prompt,
        images: images ?? [],
      }),
  );
  ipcMain.handle("wuu:turn-interrupt", (_event, threadId: string) =>
    appServerClientPool.request<{ ok: boolean }>("turn/interrupt", {
      thread_id: threadId,
    }),
  );
  ipcMain.handle(
    "wuu:respond-server-request",
    (_event, id: string, result: unknown) => {
      appServerClientPool.respondToServerRequest(id, result);
    },
  );
  ipcMain.handle(
    "wuu:reject-server-request",
    (_event, id: string, message: string) => {
      appServerClientPool.rejectServerRequest(id, message);
    },
  );

  createWindow();

  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) {
      createWindow();
    }
  });
});

app.on("before-quit", () => {
  terminalSessionManager.cleanup();
  appServerClientPool.shutdown();
});

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") {
    app.quit();
  }
});
