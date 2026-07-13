import { spawnSync } from "node:child_process";
import { readFileSync, statSync } from "node:fs";
import { basename, isAbsolute, relative, resolve } from "node:path";
import type {
  GitChangeFile,
  GitChangesResult,
  GitCommitParams,
  GitCommitResult,
  GitCreateBranchResult,
  GitDiffStats,
  GitFileDiffResult,
  GitPullRequestParams,
  GitPullRequestResult,
  GitStatusResult,
  RuntimeContext,
} from "../shared/protocol";
import {
  readFilePreviewBuffer,
  truncateTextBytes,
} from "./textPreviews";

const FILE_PREVIEW_MAX_BYTES = 512 * 1024;
const GIT_DIFF_PREVIEW_MAX_BYTES = 512 * 1024;
const GIT_DIFF_COMMAND_MAX_BUFFER = 8 * 1024 * 1024;

export class GitService {
  constructor(private readonly getRuntimeContext: () => RuntimeContext) {}

  status(options: GitStatusOptions = {}): GitStatusResult {
    return gitStatusResult(this.getRuntimeContext(), options);
  }

  changes(): GitChangesResult {
    return gitChangesResult(this.getRuntimeContext());
  }

  fileDiff(path: string): GitFileDiffResult {
    return gitFileDiffResult(this.getRuntimeContext(), path);
  }

  checkoutBranch(branch: string): GitStatusResult {
    return checkoutGitBranch(this.getRuntimeContext(), branch);
  }

  createCheckoutBranch(branch: string): GitCreateBranchResult {
    return createCheckoutGitBranch(this.getRuntimeContext(), branch);
  }

  commit(params: GitCommitParams): GitCommitResult {
    return commitGitChanges(this.getRuntimeContext(), params);
  }

  createPullRequest(params: GitPullRequestParams): GitPullRequestResult {
    return createPullRequest(this.getRuntimeContext(), params);
  }
}

type GitStatusOptions = {
  includePullRequestURL?: boolean;
  includeRemoteDefaultBranchFallback?: boolean;
};

function gitStatusResult(
  context: RuntimeContext,
  options: GitStatusOptions = {},
): GitStatusResult {
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

function gitChangesResult(context: RuntimeContext): GitChangesResult {
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

function gitFileDiffResult(
  context: RuntimeContext,
  path: string,
): GitFileDiffResult {
  const root =
    gitOutput(context.cwd, ["rev-parse", "--show-toplevel"]) ?? context.cwd;
  const insideWorkTree =
    gitOutput(root, ["rev-parse", "--is-inside-work-tree"]) === "true";
  const { relativePath, absolutePath } = resolveGitRelativePath(root, path);
  if (!insideWorkTree) {
    return emptyGitFileDiffResult(relativePath, false);
  }

  const change = gitChangesResult(context).files.find(
    (file) => file.path === relativePath,
  ) ?? {
    path: relativePath,
    status: "unknown" as const,
    additions: 0,
    deletions: 0,
  };

  if (change.status === "untracked") {
    return gitNewFileDiffResult(root, absolutePath, change);
  }

  if (change.status === "unknown" && gitPathIgnored(root, relativePath)) {
    const stats = untrackedGitFileStats(root, relativePath);
    return gitNewFileDiffResult(root, absolutePath, {
      ...change,
      status: "ignored",
      additions: stats.additions,
      binary: stats.binary,
    });
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

function checkoutGitBranch(
  context: RuntimeContext,
  branch: string,
): GitStatusResult {
  const current = gitStatusResult(context);
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
  return gitStatusResult(context);
}

function createCheckoutGitBranch(
  context: RuntimeContext,
  branch: string,
): GitCreateBranchResult {
  const current = gitStatusResult(context);
  const target = branch.trim();
  if (!current.is_repo) {
    throw new Error("current workspace is not a git repository");
  }
  validateGitBranchName(context.cwd, target);
  if (current.branches?.includes(target)) {
    throw new Error("branch already exists");
  }
  gitRun(context.cwd, ["checkout", "-b", target]);
  return { status: gitStatusResult(context) };
}

function commitGitChanges(
  context: RuntimeContext,
  params: GitCommitParams,
): GitCommitResult {
  const current = gitStatusResult(context);
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
    status: gitStatusResult(context),
    commit,
    message,
  };
}

function createPullRequest(
  context: RuntimeContext,
  params: GitPullRequestParams,
): GitPullRequestResult {
  const status = gitStatusResult(context, {
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
      ...gitStatusResult(context, { includeRemoteDefaultBranchFallback: true }),
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

function gitNewFileDiffResult(
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
      ? `Binary file ${change.path} is not tracked by Git`
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

function gitPathIgnored(root: string, relativePath: string): boolean {
  const result = spawnSync(
    "git",
    ["-C", root, "check-ignore", "--quiet", "--", relativePath],
    {
      cwd: root,
      encoding: "utf8",
      env: process.env,
    },
  );
  return result.status === 0;
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

function normalizeGitRelativePath(path: string): string {
  const normalized = path.replace(/\\/g, "/").replace(/^\/+/, "");
  if (!normalized || normalized.split("/").some((part) => part === ".." || part === "")) {
    throw new Error("invalid workspace file path");
  }
  return normalized;
}

function resolveGitRelativePath(
  root: string,
  path: string,
): { relativePath: string; absolutePath: string } {
  const relativePath = normalizeGitRelativePath(path);
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
