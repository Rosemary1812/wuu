import { app, BrowserWindow, dialog, ipcMain, net, protocol, type OpenDialogOptions } from "electron";
import { spawn as spawnChild, spawnSync, type ChildProcessWithoutNullStreams } from "node:child_process";
import {
  accessSync,
  chmodSync,
  closeSync,
  constants,
  existsSync,
  mkdirSync,
  openSync,
  readdirSync,
  readFileSync,
  readSync,
  realpathSync,
  statSync,
  writeFileSync,
  type Dirent
} from "node:fs";
import { createRequire } from "node:module";
import { homedir } from "node:os";
import { basename, dirname, extname, isAbsolute, join, relative, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import * as pty from "node-pty";
import type {
  AppServerNotification,
  AppServerRequest,
  AppServerResponse,
  ConfigCodexModelsResult,
  ConfigModelUpdateResult,
  DesktopProject,
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
  InputImage,
  InitializeResult,
  ProjectListResult,
  RuntimeContext,
  ServerEvent,
  TerminalSessionActionResult,
  TerminalSessionEvent,
  TerminalSessionStartParams,
  TerminalSessionStartResult,
  Thread,
  Turn,
  WorkspaceFileReadResult
} from "@browseros/workbench-ui/shared/protocol";

const __dirname = dirname(fileURLToPath(import.meta.url));
const requireFromMain = createRequire(import.meta.url);
const RENDERABLE_IMAGE_EXTENSIONS = new Set([".apng", ".avif", ".gif", ".jpeg", ".jpg", ".png", ".svg", ".webp"]);
const FILE_TREE_MAX_PATHS = 4000;
const FILE_PREVIEW_MAX_BYTES = 512 * 1024;
const GIT_DIFF_PREVIEW_MAX_BYTES = 512 * 1024;
const GIT_DIFF_COMMAND_MAX_BUFFER = 8 * 1024 * 1024;
const FILE_TREE_IGNORED_DIRS = new Set([
  ".git",
  ".next",
  ".turbo",
  ".vite",
  "coverage",
  "dist",
  "node_modules",
  "out",
  "target"
]);
const FILE_TREE_IGNORED_FILES = new Set([".DS_Store"]);

protocol.registerSchemesAsPrivileged([
  {
    scheme: "wuu-file",
    privileges: {
      standard: true,
      secure: true,
      supportFetchAPI: false
    }
  }
]);

type PendingRequest = {
  resolve: (value: unknown) => void;
  reject: (reason?: unknown) => void;
};

class AppServerClient {
  private child: ChildProcessWithoutNullStreams | null = null;
  private pending = new Map<string, PendingRequest>();
  private nextRequestID = 1;
  private stdoutBuffer = "";
  private disposing = false;

  constructor(
    readonly workdir: string,
    private readonly emit: (event: ServerEvent) => void
  ) {}

  request<T>(method: string, params?: unknown): Promise<T> {
    this.ensureStarted();
    const id = `client-${this.nextRequestID++}`;
    const payload: AppServerRequest = { id, method, params };
    return new Promise<T>((resolveRequest, rejectRequest) => {
      this.pending.set(JSON.stringify(id), {
        resolve: (value) => resolveRequest(value as T),
        reject: rejectRequest
      });
      this.write(payload);
    });
  }

  respond(id: string, result: unknown): void {
    this.ensureStarted();
    this.write({ id, result });
  }

  reject(id: string, message: string): void {
    this.ensureStarted();
    this.write({
      id,
      error: {
        code: "error",
        message
      }
    });
  }

  shutdown(): void {
    if (!this.child) {
      return;
    }
    try {
      this.write({ id: "shutdown", method: "shutdown" });
    } catch {
      this.child.kill();
    }
  }

  dispose(): void {
    this.disposing = true;
    for (const pending of this.pending.values()) {
      pending.reject(new Error("app-server stopped"));
    }
    this.pending.clear();
    this.shutdown();
  }

  private ensureStarted(): void {
    if (this.child && !this.child.killed) {
      return;
    }
    const command = resolveWuuCommand(this.workdir);
    this.child = spawnChild(command.command, [...command.args, "app-server", "--workdir", this.workdir], {
      cwd: command.cwd,
      env: process.env,
      stdio: ["pipe", "pipe", "pipe"]
    });

    this.child.stdout.setEncoding("utf8");
    this.child.stdout.on("data", (chunk: string) => this.readStdout(chunk));
    this.child.stderr.setEncoding("utf8");
    this.child.stderr.on("data", (chunk: string) => {
      const message = chunk.trim();
      if (message) {
        this.emit({ kind: "server-error", message });
      }
    });
    this.child.on("exit", (code) => {
      for (const pending of this.pending.values()) {
        pending.reject(new Error("app-server exited"));
      }
      this.pending.clear();
      if (!this.disposing) {
        this.emit({ kind: "server-exit", code });
      }
      this.child = null;
    });
  }

  private write(payload: unknown): void {
    if (!this.child) {
      throw new Error("app-server is not running");
    }
    this.child.stdin.write(`${JSON.stringify(payload)}\n`);
  }

  private readStdout(chunk: string): void {
    this.stdoutBuffer += chunk;
    for (;;) {
      const index = this.stdoutBuffer.indexOf("\n");
      if (index < 0) {
        return;
      }
      const line = this.stdoutBuffer.slice(0, index).trim();
      this.stdoutBuffer = this.stdoutBuffer.slice(index + 1);
      if (line) {
        this.handleLine(line);
      }
    }
  }

  private handleLine(line: string): void {
    let message: AppServerResponse | AppServerNotification | Required<AppServerRequest>;
    try {
      message = JSON.parse(line);
    } catch {
      this.emit({ kind: "server-error", message: `Invalid app-server JSON: ${line}` });
      return;
    }

    const maybeRequest = message as Required<AppServerRequest>;
    if (maybeRequest.method && maybeRequest.id !== undefined) {
      this.emit({ kind: "server-request", message: maybeRequest });
      return;
    }

    const maybeNotification = message as AppServerNotification;
    if (maybeNotification.method) {
      this.emit({ kind: "notification", message: maybeNotification });
      return;
    }

    const response = message as AppServerResponse;
    const key = JSON.stringify(response.id);
    const pending = this.pending.get(key);
    if (!pending) {
      return;
    }
    this.pending.delete(key);
    if (response.error) {
      pending.reject(new Error(response.error.message));
      return;
    }
    pending.resolve(response.result);
  }
}

type WuuCommand = {
  command: string;
  args: string[];
  cwd: string;
};

function resolveWuuCommand(workdir: string): WuuCommand {
  if (process.env.WUU_BIN) {
    return { command: process.env.WUU_BIN, args: [], cwd: workdir };
  }
  const sourceRoot = wuuSourceRoot();
  if (sourceRoot && process.env.WUU_DESKTOP_USE_GO_RUN !== "0") {
    return { command: "go", args: ["run", "./cmd/wuu"], cwd: sourceRoot };
  }
  for (const candidate of [join(workdir, "bin", "wuu"), join(workdir, "wuu")]) {
    if (existsSync(candidate)) {
      return { command: candidate, args: [], cwd: workdir };
    }
  }
  return { command: "wuu", args: [], cwd: workdir };
}

function wuuSourceRoot(): string | undefined {
  const candidates = [
    process.env.WUU_SOURCE_ROOT,
    process.cwd(),
    resolve(process.cwd(), ".."),
    app.getAppPath(),
    resolve(app.getAppPath(), ".."),
    resolve(__dirname, "..", "..", "..")
  ].filter((candidate): candidate is string => Boolean(candidate));
  return candidates.find((candidate) => existsSync(join(candidate, "go.mod")) && existsSync(join(candidate, "cmd", "wuu")));
}

type ProjectStore = {
  projects: DesktopProject[];
  active_context?: RuntimeContext;
};

let mainWindow: BrowserWindow | null = null;
let client: AppServerClient | null = null;
let projectStore: ProjectStore = { projects: [] };
let windowResizeEndTimer: NodeJS.Timeout | undefined;
let windowResizeState = false;
let terminalSessionCounter = 1;
const terminalSessions = new Map<string, TerminalSession>();

type TerminalSession = {
  id: string;
  ptyProcess: pty.IPty;
  cwd: string;
  shell: string;
  startedAt: number;
};

function projectStorePath(): string {
  return join(wuuHomePath(), "projects.json");
}

function legacyProjectStorePath(): string {
  return join(app.getPath("userData"), "projects.json");
}

function wuuHomePath(): string {
  const override = process.env.WUU_HOME?.trim();
  if (override) {
    return resolve(override);
  }
  return join(homedir(), ".wuu");
}

function loadProjectStore(): ProjectStore {
  const loaded = readProjectStoreFile(projectStorePath());
  const legacy = readProjectStoreFile(legacyProjectStorePath());
  const { store, changed } = mergeProjectStores(loaded ?? { projects: [] }, legacy);
  if (!loaded || changed) {
    writeProjectStoreFile(projectStorePath(), store);
  }
  return store;
}

function readProjectStoreFile(path: string): ProjectStore | undefined {
  try {
    const parsed = JSON.parse(readFileSync(path, "utf8")) as Partial<ProjectStore> & {
      active_project_id?: unknown;
    };
    const projects = Array.isArray(parsed.projects)
      ? parsed.projects.filter((project): project is DesktopProject => isDesktopProject(project))
      : [];
    const activeContext = normalizeRuntimeContext(parsed.active_context, projects) ?? legacyProjectContext(parsed.active_project_id, projects);
    return {
      projects,
      active_context: activeContext
    };
  } catch {
    return undefined;
  }
}

function mergeProjectStores(base: ProjectStore, incoming: ProjectStore | undefined): { store: ProjectStore; changed: boolean } {
  if (!incoming) {
    return { store: base, changed: false };
  }

  let changed = false;
  const projects = [...base.projects];
  for (const project of incoming.projects) {
    if (projects.some((candidate) => candidate.id === project.id)) {
      continue;
    }
    projects.push(project);
    changed = true;
  }

  let activeContext = base.active_context;
  if (!activeContext && incoming.active_context) {
    activeContext = normalizeRuntimeContext(incoming.active_context, projects);
    changed = Boolean(activeContext);
  }

  return { store: { projects, active_context: activeContext }, changed };
}

function isDesktopProject(value: unknown): value is DesktopProject {
  if (!value || typeof value !== "object") {
    return false;
  }
  const project = value as Partial<DesktopProject>;
  return (
    typeof project.id === "string" &&
    typeof project.name === "string" &&
    typeof project.path === "string" &&
    typeof project.created_at === "string" &&
    typeof project.updated_at === "string"
  );
}

function normalizeRuntimeContext(value: unknown, projects: DesktopProject[]): RuntimeContext | undefined {
  if (!value || typeof value !== "object") {
    return undefined;
  }
  const context = value as Partial<RuntimeContext>;
  if (context.kind === "project" && typeof context.project_id === "string") {
    const project = projects.find((candidate) => candidate.id === context.project_id);
    return project ? { kind: "project", project_id: project.id, cwd: project.path } : undefined;
  }
  if (context.kind === "no_project" && typeof context.cwd === "string") {
    const cwd = resolve(context.cwd);
    mkdirSync(cwd, { recursive: true });
    return { kind: "no_project", cwd };
  }
  return undefined;
}

function legacyProjectContext(value: unknown, projects: DesktopProject[]): RuntimeContext | undefined {
  if (typeof value !== "string") {
    return undefined;
  }
  const project = projects.find((candidate) => candidate.id === value);
  return project ? { kind: "project", project_id: project.id, cwd: project.path } : undefined;
}

function saveProjectStore(): void {
  writeProjectStoreFile(projectStorePath(), projectStore);
}

function writeProjectStoreFile(path: string, store: ProjectStore): void {
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, `${JSON.stringify(store, null, 2)}\n`);
}

function projectListResult(): ProjectListResult {
  projectStore = loadProjectStore();
  const context = ensureRuntimeContext();
  return {
    projects: projectStore.projects,
    active_context: context,
    active_project_id: context.kind === "project" ? context.project_id : undefined
  };
}

function gitStatusResult(): GitStatusResult {
  const context = ensureRuntimeContext();
  const root = gitOutput(context.cwd, ["rev-parse", "--show-toplevel"]) ?? context.cwd;
  const insideWorkTree = gitOutput(root, ["rev-parse", "--is-inside-work-tree"]) === "true";
  if (!insideWorkTree) {
    return { is_repo: false, dirty_count: 0, diff: emptyGitDiffStats(), staged_diff: emptyGitDiffStats() };
  }

  const branchName = gitOutput(root, ["branch", "--show-current"]);
  const head = gitOutput(root, ["rev-parse", "--short", "HEAD"]);
  const branch = branchName || head;
  const branches = gitOutput(root, ["for-each-ref", "--format=%(refname:short)", "refs/heads"])
    ?.split("\n")
    .map((item) => item.trim())
    .filter(Boolean);
  const porcelain = gitOutput(root, ["status", "--porcelain"]);
  const dirtyCount = porcelain ? porcelain.split("\n").filter((line) => line.trim()).length : 0;
  const upstream = gitOutput(root, ["rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"]);
  const [aheadCount, behindCount] = upstream ? gitAheadBehind(root) : [0, 0];
  const remote = upstream?.split("/")[0] || firstGitRemote(root);
  const defaultBranch = remote ? gitDefaultBranch(root, remote) : undefined;
  const ghAvailable = commandAvailable("gh", ["--version"]);
  const prURL = branchName && ghAvailable ? ghPullRequestURL(root) : undefined;

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
    pr_url: prURL
  };
}

function gitChangesResult(): GitChangesResult {
  const context = ensureRuntimeContext();
  const root = gitOutput(context.cwd, ["rev-parse", "--show-toplevel"]) ?? context.cwd;
  const insideWorkTree = gitOutput(root, ["rev-parse", "--is-inside-work-tree"]) === "true";
  if (!insideWorkTree) {
    return { is_repo: false, files: [] };
  }

  const filesByPath = new Map<string, GitChangeFile>();
  for (const file of parseGitNameStatus(gitOutput(root, ["diff", "--name-status", "--find-renames", "HEAD", "--"]) ?? "")) {
    filesByPath.set(file.path, file);
  }

  for (const file of parseGitNumstatFiles(gitOutput(root, ["diff", "--numstat", "--find-renames", "HEAD", "--"]) ?? "")) {
    const existing = filesByPath.get(file.path);
    filesByPath.set(file.path, {
      ...file,
      ...existing,
      additions: file.additions,
      deletions: file.deletions,
      binary: existing?.binary || file.binary
    });
  }

  for (const path of listUntrackedGitFiles(root)) {
    const stats = untrackedGitFileStats(root, path);
    filesByPath.set(path, {
      path,
      status: "untracked",
      additions: stats.additions,
      deletions: 0,
      binary: stats.binary
    });
  }

  return {
    is_repo: true,
    root,
    files: Array.from(filesByPath.values()).sort((left, right) => left.path.localeCompare(right.path))
  };
}

function gitFileDiffResult(path: string): GitFileDiffResult {
  const context = ensureRuntimeContext();
  const root = gitOutput(context.cwd, ["rev-parse", "--show-toplevel"]) ?? context.cwd;
  const insideWorkTree = gitOutput(root, ["rev-parse", "--is-inside-work-tree"]) === "true";
  const { relativePath, absolutePath } = resolveGitRelativePath(root, path);
  if (!insideWorkTree) {
    return emptyGitFileDiffResult(relativePath, false);
  }

  const change = gitChangesResult().files.find((file) => file.path === relativePath) ?? {
    path: relativePath,
    status: "unknown" as const,
    additions: 0,
    deletions: 0
  };

  if (change.status === "untracked") {
    return gitUntrackedFileDiffResult(root, absolutePath, change);
  }

  const rawPatch = gitDiffOutput(root, relativePath);
  const truncatedPatch = truncateTextBytes(rawPatch, GIT_DIFF_PREVIEW_MAX_BYTES);
  const binary = change.binary || rawPatch.includes("Binary files ") || rawPatch.includes("GIT binary patch");
  return {
    is_repo: true,
    path: change.path,
    old_path: change.old_path,
    status: change.status,
    additions: change.additions,
    deletions: change.deletions,
    binary,
    patch: truncatedPatch.text,
    truncated: truncatedPatch.truncated
  };
}

function checkoutGitBranch(branch: string): GitStatusResult {
  const context = ensureRuntimeContext();
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
    env: process.env
  });
  if (result.status !== 0) {
    throw new Error(result.stderr.trim() || `failed to checkout ${target}`);
  }
  return gitStatusResult();
}

function createCheckoutGitBranch(branch: string): GitCreateBranchResult {
  const context = ensureRuntimeContext();
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
  const context = ensureRuntimeContext();
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
    message
  };
}

function createPullRequest(params: GitPullRequestParams): GitPullRequestResult {
  const context = ensureRuntimeContext();
  const status = gitStatusResult();
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
    throw new Error("commit or discard local changes before opening a pull request");
  }

  const existingURL = ghPullRequestURL(context.cwd);
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
  return { status: gitStatusResult(), url, already_exists: false };
}

function validateGitBranchName(cwd: string, branch: string): void {
  if (!branch) {
    throw new Error("branch name is required");
  }
  const result = spawnSync("git", ["-C", cwd, "check-ref-format", "--branch", branch], {
    cwd,
    encoding: "utf8",
    env: process.env
  });
  if (result.status !== 0) {
    throw new Error(result.stderr.trim() || "invalid branch name");
  }
}

function gitRun(cwd: string, args: string[]): string {
  const result = spawnSync("git", ["-C", cwd, ...args], {
    cwd,
    encoding: "utf8",
    env: process.env
  });
  if (result.status !== 0) {
    throw new Error(result.stderr.trim() || result.stdout.trim() || `git ${args.join(" ")} failed`);
  }
  return result.stdout.trim();
}

function gitOutput(cwd: string, args: string[]): string | undefined {
  const result = spawnSync("git", ["-C", cwd, ...args], {
    cwd,
    encoding: "utf8",
    env: process.env
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
  const stats = parseGitNumstat(gitOutput(cwd, ["diff", "--numstat", "HEAD", "--"]) ?? "");
  if (!includeUntracked) {
    return stats;
  }
  const untracked = gitOutput(cwd, ["ls-files", "--others", "--exclude-standard"])
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
    deletions: stats.deletions
  };
}

function gitStagedDiffStats(cwd: string): GitDiffStats {
  return parseGitNumstat(gitOutput(cwd, ["diff", "--cached", "--numstat", "--"]) ?? "");
}

function gitDiffOutput(cwd: string, relativePath: string): string {
  const result = spawnSync("git", ["-C", cwd, "diff", "--no-ext-diff", "--find-renames", "--unified=3", "HEAD", "--", relativePath], {
    cwd,
    encoding: "utf8",
    env: process.env,
    maxBuffer: GIT_DIFF_COMMAND_MAX_BUFFER
  });
  if (result.status !== 0 && !result.stdout) {
    throw new Error(result.stderr.trim() || `git diff failed for ${relativePath}`);
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
    const oldPath = status === "renamed" || status === "copied" ? columns[1] : undefined;
    const path = status === "renamed" || status === "copied" ? columns[2] : columns[1];
    if (!path) {
      continue;
    }
    files.push({
      path,
      old_path: oldPath,
      status,
      additions: 0,
      deletions: 0
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
      binary: additions === "-" || deletions === "-"
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

function untrackedGitFileStats(root: string, path: string): { additions: number; binary: boolean } {
  const { absolutePath } = resolveGitRelativePath(root, path);
  try {
    const stats = statSync(absolutePath);
    if (!stats.isFile()) {
      return { additions: 0, binary: false };
    }
    const previewBuffer = readFilePreviewBuffer(absolutePath, Math.min(stats.size, FILE_PREVIEW_MAX_BYTES));
    const binary = previewBuffer.includes(0);
    return {
      additions: binary ? 0 : countTextFileLines(absolutePath),
      binary
    };
  } catch {
    return { additions: 0, binary: false };
  }
}

function gitUntrackedFileDiffResult(root: string, absolutePath: string, change: GitChangeFile): GitFileDiffResult {
  try {
    const stats = statSync(absolutePath);
    if (!stats.isFile()) {
      return emptyGitFileDiffResult(change.path, true);
    }
    const readLimit = Math.min(stats.size, GIT_DIFF_PREVIEW_MAX_BYTES + 1);
    const buffer = readFilePreviewBuffer(absolutePath, readLimit);
    const truncated = stats.size > GIT_DIFF_PREVIEW_MAX_BYTES;
    const previewBuffer = buffer.subarray(0, truncated ? GIT_DIFF_PREVIEW_MAX_BYTES : buffer.length);
    const binary = previewBuffer.includes(0);
    const patch = binary ? `Binary file ${change.path} is untracked` : buildUntrackedPatch(change.path, previewBuffer.toString("utf8"), truncated);
    return {
      is_repo: true,
      path: change.path,
      old_path: change.old_path,
      status: change.status,
      additions: change.additions,
      deletions: change.deletions,
      binary,
      patch,
      truncated
    };
  } catch {
    return emptyGitFileDiffResult(change.path, true);
  }
}

function buildUntrackedPatch(path: string, text: string, truncated: boolean): string {
  const lines = splitPatchTextLines(text);
  const patchLines = [
    `diff --git a/${path} b/${path}`,
    "new file mode 100644",
    "--- /dev/null",
    `+++ b/${path}`,
    `@@ -0,0 +1,${lines.length} @@`,
    ...lines.map((line) => `+${line}`)
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

function resolveGitRelativePath(root: string, path: string): { relativePath: string; absolutePath: string } {
  const relativePath = normalizeWorkspaceRelativePath(path);
  const absolutePath = resolve(root, relativePath);
  const relativeToRoot = relative(root, absolutePath);
  if (!relativeToRoot || relativeToRoot.startsWith("..") || isAbsolute(relativeToRoot)) {
    throw new Error("file is outside the current git repository");
  }
  return { relativePath, absolutePath };
}

function truncateTextBytes(text: string, maxBytes: number): { text: string; truncated: boolean } {
  const buffer = Buffer.from(text, "utf8");
  if (buffer.byteLength <= maxBytes) {
    return { text, truncated: false };
  }
  return {
    text: `${buffer.subarray(0, maxBytes).toString("utf8")}\n[diff truncated]\n`,
    truncated: true
  };
}

function emptyGitFileDiffResult(path: string, isRepo: boolean): GitFileDiffResult {
  return {
    is_repo: isRepo,
    path,
    status: "unknown",
    additions: 0,
    deletions: 0,
    binary: false,
    patch: "",
    truncated: false
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
    return text.endsWith("\n") ? text.split("\n").length - 1 : text.split(/\r\n|\n|\r/).length;
  } catch {
    return 0;
  }
}

function gitAheadBehind(cwd: string): [number, number] {
  const output = gitOutput(cwd, ["rev-list", "--left-right", "--count", "HEAD...@{u}"]);
  const [ahead, behind] = output?.split(/\s+/, 2).map((item) => Number(item) || 0) ?? [0, 0];
  return [ahead, behind];
}

function firstGitRemote(cwd: string): string | undefined {
  return gitOutput(cwd, ["remote"])
    ?.split("\n")
    .map((item) => item.trim())
    .find(Boolean);
}

function gitDefaultBranch(cwd: string, remote: string): string | undefined {
  const symbolic = gitOutput(cwd, ["symbolic-ref", "--short", `refs/remotes/${remote}/HEAD`]);
  if (symbolic?.startsWith(`${remote}/`)) {
    return symbolic.slice(remote.length + 1);
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
    env: process.env
  });
  return result.status === 0;
}

function ghOutput(cwd: string, args: string[]): string | undefined {
  const result = spawnSync("gh", args, {
    cwd,
    encoding: "utf8",
    env: process.env
  });
  if (result.status !== 0) {
    if (args[0] === "pr" && args[1] === "view") {
      return undefined;
    }
    throw new Error(result.stderr.trim() || result.stdout.trim() || `gh ${args.join(" ")} failed`);
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
  const topLevel = files
    .map((file) => file.split("/", 1)[0])
    .filter(Boolean);
  const sharedArea = topLevel.length > 0 && topLevel.every((item) => item === topLevel[0]) ? topLevel[0] : "";
  return sharedArea ? `Update ${sharedArea} changes` : "Update workspace changes";
}

function fileTreeListResult(): FileTreeListResult {
  const context = ensureRuntimeContext();
  const paths: string[] = [];
  const truncated = collectFileTreePaths(context.cwd, "", paths);
  return { root: context.cwd, paths, truncated };
}

function readWorkspaceFileResult(path: string): WorkspaceFileReadResult {
  const context = ensureRuntimeContext();
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
  const previewBuffer = buffer.subarray(0, truncated ? FILE_PREVIEW_MAX_BYTES : bytesRead);
  const binary = previewBuffer.includes(0);

  return {
    root: context.cwd,
    path: relativeFilePath,
    absolute_path: absolutePath,
    size_bytes: stats.size,
    binary,
    truncated,
    text: binary ? undefined : previewBuffer.toString("utf8")
  };
}

function normalizeWorkspaceRelativePath(path: string): string {
  const value = path.trim().replace(/\\/g, "/").replace(/^\/+/, "").replace(/\/+$/, "");
  if (!value || value.includes("\0") || value.split("/").some((segment) => segment === "..")) {
    throw new Error("invalid workspace file path");
  }
  return value;
}

function resolveWorkspacePath(root: string, relativeFilePath: string): string {
  const absolutePath = resolve(root, relativeFilePath);
  const relativeToRoot = relative(root, absolutePath);
  if (!relativeToRoot || relativeToRoot.startsWith("..") || isAbsolute(relativeToRoot)) {
    throw new Error("file is outside the current workspace");
  }

  const realRoot = realpathSync(root);
  const realFile = realpathSync(absolutePath);
  const realRelative = relative(realRoot, realFile);
  if (!realRelative || realRelative.startsWith("..") || isAbsolute(realRelative)) {
    throw new Error("file is outside the current workspace");
  }
  return absolutePath;
}

function collectFileTreePaths(root: string, relativeDirectory: string, paths: string[]): boolean {
  if (paths.length >= FILE_TREE_MAX_PATHS) {
    return true;
  }

  const directory = relativeDirectory ? join(root, relativeDirectory) : root;
  let entries: Dirent[];
  try {
    entries = readdirSync(directory, { withFileTypes: true });
  } catch {
    return false;
  }

  entries.sort((left, right) => {
    const leftDirectory = left.isDirectory();
    const rightDirectory = right.isDirectory();
    if (leftDirectory !== rightDirectory) {
      return leftDirectory ? -1 : 1;
    }
    return left.name.localeCompare(right.name, undefined, { sensitivity: "base" });
  });

  for (const entry of entries) {
    if (FILE_TREE_IGNORED_FILES.has(entry.name)) {
      continue;
    }

    const relativePath = relativeDirectory ? `${relativeDirectory}/${entry.name}` : entry.name;
    if (entry.isDirectory()) {
      if (FILE_TREE_IGNORED_DIRS.has(entry.name)) {
        continue;
      }
      paths.push(`${relativePath}/`);
      if (collectFileTreePaths(root, relativePath, paths)) {
        return true;
      }
      continue;
    }

    if (entry.isFile() || entry.isSymbolicLink()) {
      paths.push(relativePath);
    }

    if (paths.length >= FILE_TREE_MAX_PATHS) {
      return true;
    }
  }

  return false;
}

function ensureRuntimeContext(): RuntimeContext {
  const activeContext = projectStore.active_context;
  if (activeContext?.kind === "project") {
    const project = projectStore.projects.find((candidate) => candidate.id === activeContext.project_id);
    if (project) {
      projectStore.active_context = { kind: "project", project_id: project.id, cwd: project.path };
      return projectStore.active_context;
    }
  }
  if (projectStore.active_context?.kind === "no_project") {
    mkdirSync(projectStore.active_context.cwd, { recursive: true });
    return projectStore.active_context;
  }
  projectStore.active_context = createNoProjectContext();
  saveProjectStore();
  return projectStore.active_context;
}

function createNoProjectContext(): RuntimeContext {
  return { kind: "no_project", cwd: allocateNoProjectCwd() };
}

function allocateNoProjectCwd(): string {
  const baseDir = join(app.getPath("documents"), "Wuu", formatLocalDate(new Date()));
  mkdirSync(baseDir, { recursive: true });
  for (let index = 0; index < 1000; index += 1) {
    const name = index === 0 ? "new-chat" : `new-chat-${index + 1}`;
    const candidate = join(baseDir, name);
    if (existsSync(candidate)) {
      continue;
    }
    mkdirSync(candidate, { recursive: true });
    return candidate;
  }
  throw new Error(`failed to allocate no-project workspace under ${baseDir}`);
}

function formatLocalDate(date: Date): string {
  const year = String(date.getFullYear());
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

function projectID(projectPath: string): string {
  return Buffer.from(resolve(projectPath)).toString("base64url");
}

function projectName(projectPath: string): string {
  return basename(projectPath) || projectPath;
}

function isDirectory(projectPath: string): boolean {
  try {
    return statSync(projectPath).isDirectory();
  } catch {
    return false;
  }
}

function addProject(projectPath: string): ProjectListResult {
  projectStore = loadProjectStore();
  const resolvedPath = resolve(projectPath);
  if (!isDirectory(resolvedPath)) {
    throw new Error("selected project is not a directory");
  }
  const now = new Date().toISOString();
  const id = projectID(resolvedPath);
  const existingIndex = projectStore.projects.findIndex((project) => project.id === id);
  const project: DesktopProject = {
    id,
    name: projectName(resolvedPath),
    path: resolvedPath,
    created_at: existingIndex >= 0 ? projectStore.projects[existingIndex].created_at : now,
    updated_at: now
  };
  if (existingIndex >= 0) {
    projectStore.projects[existingIndex] = project;
  } else {
    projectStore.projects = [project, ...projectStore.projects];
  }
  projectStore.active_context = { kind: "project", project_id: id, cwd: resolvedPath };
  resetClient();
  saveProjectStore();
  return projectListResult();
}

function selectProject(projectIDToSelect: string): ProjectListResult {
  projectStore = loadProjectStore();
  const project = projectStore.projects.find((candidate) => candidate.id === projectIDToSelect);
  if (!project) {
    throw new Error("project not found");
  }
  projectStore.active_context = { kind: "project", project_id: project.id, cwd: project.path };
  resetClient();
  saveProjectStore();
  return projectListResult();
}

function selectNoProject(fresh: boolean): ProjectListResult {
  projectStore = loadProjectStore();
  if (fresh || projectStore.active_context?.kind !== "no_project") {
    projectStore.active_context = createNoProjectContext();
  }
  resetClient();
  saveProjectStore();
  return projectListResult();
}

function resetClient(): void {
  client?.dispose();
  client = null;
  cleanupTerminalSessions();
}

async function showProjectDirectoryDialog(options: OpenDialogOptions): Promise<string | undefined> {
  const result = mainWindow ? await dialog.showOpenDialog(mainWindow, options) : await dialog.showOpenDialog(options);
  if (result.canceled) {
    return undefined;
  }
  return result.filePaths[0];
}

function serverClient(): AppServerClient {
  const context = ensureRuntimeContext();
  if (!client || client.workdir !== context.cwd) {
    resetClient();
    client = new AppServerClient(context.cwd, emitServerEvent);
  }
  return client;
}

function emitServerEvent(event: ServerEvent): void {
  if (!mainWindow || mainWindow.isDestroyed() || mainWindow.webContents.isDestroyed()) {
    return;
  }
  mainWindow.webContents.send("wuu:server-event", event);
}

function emitTerminalEvent(event: TerminalSessionEvent): void {
  if (!mainWindow || mainWindow.isDestroyed() || mainWindow.webContents.isDestroyed()) {
    return;
  }
  mainWindow.webContents.send("wuu:terminal-event", event);
}

function startTerminalSession(params: TerminalSessionStartParams = {}): TerminalSessionStartResult {
  const context = ensureRuntimeContext();
  const cwd = context.cwd;
  const id = `term-${terminalSessionCounter++}`;
  const startedAt = Date.now();
  const shell = terminalShell();
  const cols = normalizeTerminalSize(params.cols, 80, 20, 500);
  const rows = normalizeTerminalSize(params.rows, 24, 6, 200);
  ensureNodePtyHelperExecutable();
  const ptyProcess = pty.spawn(shell.command, shell.args, {
    name: "xterm-256color",
    cols,
    rows,
    cwd,
    env: { ...process.env, CLICOLOR: "1", COLORTERM: "truecolor", FORCE_COLOR: "1", TERM: "xterm-256color" }
  });

  const entry: TerminalSession = { id, ptyProcess, cwd, shell: shell.command, startedAt };
  terminalSessions.set(id, entry);

  ptyProcess.onData((text) => emitTerminalEvent({ type: "data", id, text }));
  ptyProcess.onExit((event) => {
    terminalSessions.delete(id);
    emitTerminalEvent({
      type: "exit",
      id,
      exit_code: event.exitCode,
      signal: event.signal ?? null,
      duration_ms: Date.now() - startedAt,
      finished_at: new Date().toISOString()
    });
  });

  return {
    id,
    cwd,
    shell: shell.command,
    started_at: new Date(startedAt).toISOString()
  };
}

function writeTerminalSession(id: string, data: string): TerminalSessionActionResult {
  const session = terminalSessions.get(id);
  if (!session) {
    return { ok: false };
  }
  session.ptyProcess.write(data);
  return { ok: true };
}

function resizeTerminalSession(id: string, cols: number, rows: number): TerminalSessionActionResult {
  const session = terminalSessions.get(id);
  if (!session) {
    return { ok: false };
  }
  session.ptyProcess.resize(normalizeTerminalSize(cols, 80, 20, 500), normalizeTerminalSize(rows, 24, 6, 200));
  return { ok: true };
}

function stopTerminalSession(id: string): TerminalSessionActionResult {
  const session = terminalSessions.get(id);
  if (!session) {
    return { ok: false };
  }
  terminateTerminalSession(session);
  return { ok: true };
}

function cleanupTerminalSessions(): void {
  for (const session of terminalSessions.values()) {
    terminateTerminalSession(session);
  }
  terminalSessions.clear();
}

function terminateTerminalSession(session: TerminalSession): void {
  terminalSessions.delete(session.id);
  try {
    session.ptyProcess.kill();
  } catch (error) {
    emitTerminalEvent({
      type: "error",
      id: session.id,
      message: error instanceof Error ? error.message : "Failed to stop terminal session.",
      finished_at: new Date().toISOString()
    });
  }
}

function normalizeTerminalSize(value: number | undefined, fallback: number, min: number, max: number): number {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    return fallback;
  }
  return Math.max(min, Math.min(max, Math.floor(value)));
}

function terminalShell(): { command: string; args: string[] } {
  if (process.platform === "win32") {
    return { command: process.env.ComSpec || "cmd.exe", args: [] };
  }
  return { command: resolveTerminalShell(), args: ["-l"] };
}

function resolveTerminalShell(): string {
  const candidates = [
    process.env.SHELL,
    "/bin/zsh",
    "/bin/bash",
    "/bin/sh",
    "/usr/bin/zsh",
    "/usr/bin/bash",
    "/usr/bin/sh"
  ];
  for (const candidate of candidates) {
    if (isExecutableFile(candidate)) {
      return candidate;
    }
  }
  return "/bin/sh";
}

function ensureNodePtyHelperExecutable(): void {
  if (process.platform === "win32") {
    return;
  }
  let helperPath: string;
  try {
    const nodePtyMain = requireFromMain.resolve("node-pty");
    helperPath = resolve(dirname(nodePtyMain), "..", "prebuilds", `${process.platform}-${process.arch}`, "spawn-helper");
    helperPath = helperPath
      .replace("app.asar", "app.asar.unpacked")
      .replace("node_modules.asar", "node_modules.asar.unpacked");
  } catch {
    return;
  }
  try {
    accessSync(helperPath, constants.X_OK);
  } catch {
    const mode = existsSync(helperPath) ? statSync(helperPath).mode : 0o755;
    chmodSync(helperPath, mode | 0o755);
  }
}

function isExecutableFile(path: string | undefined): path is string {
  if (!path || !isAbsolute(path)) {
    return false;
  }
  try {
    if (!statSync(path).isFile()) {
      return false;
    }
    accessSync(path, constants.X_OK);
    return true;
  } catch {
    return false;
  }
}

function setWindowResizeState(resizing: boolean): void {
  if (windowResizeState === resizing) {
    return;
  }
  windowResizeState = resizing;
  if (!mainWindow || mainWindow.isDestroyed() || mainWindow.webContents.isDestroyed()) {
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
    width: 1280,
    height: 860,
    minWidth: 980,
    minHeight: 460,
    titleBarStyle: "hiddenInset",
    trafficLightPosition: { x: 18, y: 18 },
    backgroundColor: "#f6f6f4",
    webPreferences: {
      preload: join(__dirname, "../preload/index.cjs"),
      contextIsolation: true,
      nodeIntegration: false
    }
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

function registerRenderableFileProtocol(): void {
  protocol.handle("wuu-file", (request) => {
    const filePath = filePathFromRenderableURL(request.url);
    if (!filePath || !isRenderableImageFile(filePath)) {
      return new Response("Not found", { status: 404 });
    }
    return net.fetch(pathToFileURL(filePath).toString());
  });
}

function filePathFromRenderableURL(rawURL: string): string | undefined {
  try {
    const url = new URL(rawURL);
    if (url.hostname !== "local") {
      return undefined;
    }
    const encodedPath = url.pathname.replace(/^\/+/, "");
    if (!encodedPath) {
      return undefined;
    }
    return Buffer.from(encodedPath, "base64url").toString("utf8");
  } catch {
    return undefined;
  }
}

function isRenderableImageFile(filePath: string): boolean {
  try {
    return statSync(filePath).isFile() && RENDERABLE_IMAGE_EXTENSIONS.has(extname(filePath).toLowerCase());
  } catch {
    return false;
  }
}

app.whenReady().then(() => {
  projectStore = loadProjectStore();
  registerRenderableFileProtocol();

  ipcMain.handle("wuu:project-list", () => projectListResult());
  ipcMain.handle("wuu:project-select", (_event, projectIDToSelect: string) => selectProject(projectIDToSelect));
  ipcMain.handle("wuu:project-select-none", (_event, fresh?: boolean) => selectNoProject(Boolean(fresh)));
  ipcMain.handle("wuu:git-status", () => gitStatusResult());
  ipcMain.handle("wuu:git-changes", () => gitChangesResult());
  ipcMain.handle("wuu:git-file-diff", (_event, path: string) => gitFileDiffResult(path));
  ipcMain.handle("wuu:git-checkout-branch", (_event, branch: string) => checkoutGitBranch(branch));
  ipcMain.handle("wuu:git-create-checkout-branch", (_event, branch: string) => createCheckoutGitBranch(branch));
  ipcMain.handle("wuu:git-commit", (_event, params: GitCommitParams) => commitGitChanges(params ?? {}));
  ipcMain.handle("wuu:git-create-pr", (_event, params: GitPullRequestParams) => createPullRequest(params ?? {}));
  ipcMain.handle("wuu:file-tree-list", () => fileTreeListResult());
  ipcMain.handle("wuu:file-read", (_event, path: string) => readWorkspaceFileResult(path));
  ipcMain.handle("wuu:terminal-start", (_event, params?: TerminalSessionStartParams) => startTerminalSession(params));
  ipcMain.handle("wuu:terminal-write", (_event, id: string, data: string) => writeTerminalSession(id, data));
  ipcMain.handle("wuu:terminal-resize", (_event, id: string, cols: number, rows: number) =>
    resizeTerminalSession(id, cols, rows)
  );
  ipcMain.handle("wuu:terminal-stop", (_event, id: string) => stopTerminalSession(id));
  ipcMain.handle("wuu:project-choose-folder", async () => {
    const projectPath = await showProjectDirectoryDialog({
      title: "使用现有文件夹",
      buttonLabel: "使用文件夹",
      properties: ["openDirectory"]
    });
    if (!projectPath) {
      return projectListResult();
    }
    return addProject(projectPath);
  });
  ipcMain.handle("wuu:project-create-blank", async () => {
    const projectPath = await showProjectDirectoryDialog({
      title: "新建空白项目",
      buttonLabel: "创建项目",
      properties: ["openDirectory", "createDirectory"]
    });
    if (!projectPath) {
      return projectListResult();
    }
    return addProject(projectPath);
  });
  ipcMain.handle("wuu:initialize", () => serverClient().request<InitializeResult>("initialize"));
  ipcMain.handle("wuu:config-codex-models", (_event, provider?: string) =>
    serverClient().request<ConfigCodexModelsResult>("config/codex/models", { provider: provider ?? "" })
  );
  ipcMain.handle("wuu:config-model-update", (_event, provider: string, model: string, effort?: string, connection?: { base_url?: string; api_key?: string; create_provider?: boolean }) =>
    serverClient().request<ConfigModelUpdateResult>("config/model/update", {
      provider,
      model,
      ...(connection?.base_url === undefined ? {} : { base_url: connection.base_url }),
      ...(connection?.api_key === undefined ? {} : { api_key: connection.api_key }),
      ...(connection?.create_provider ? { create_provider: true } : {}),
      ...(effort === undefined ? {} : { effort })
    })
  );
  ipcMain.handle("wuu:thread-start", () => serverClient().request<{ thread: Thread }>("thread/start"));
  ipcMain.handle("wuu:thread-resume", (_event, sessionId?: string) =>
    serverClient().request<{ thread: Thread }>("thread/resume", { session_id: sessionId ?? "" })
  );
  ipcMain.handle("wuu:thread-fork", (_event, threadId: string, turnId?: string, itemId?: string) =>
    serverClient().request<{ thread: Thread }>("thread/fork", {
      thread_id: threadId,
      turn_id: turnId ?? "",
      item_id: itemId ?? ""
    })
  );
  ipcMain.handle("wuu:thread-list", () => serverClient().request<{ threads: Thread[] }>("thread/list"));
  ipcMain.handle("wuu:thread-pin", (_event, threadId: string, pinned: boolean) =>
    serverClient().request<{ thread: Thread }>("thread/pin", { thread_id: threadId, pinned })
  );
  ipcMain.handle("wuu:thread-archive", (_event, threadId: string, archived: boolean) =>
    serverClient().request<{ thread: Thread }>("thread/archive", { thread_id: threadId, archived })
  );
  ipcMain.handle("wuu:turn-start", (_event, threadId: string, prompt: string, images?: InputImage[]) =>
    serverClient().request<{ turn: Turn }>("turn/start", { thread_id: threadId, prompt, images: images ?? [] })
  );
  ipcMain.handle("wuu:turn-interrupt", (_event, threadId: string) =>
    serverClient().request<{ ok: boolean }>("turn/interrupt", { thread_id: threadId })
  );
  ipcMain.handle("wuu:respond-server-request", (_event, id: string, result: unknown) => {
    serverClient().respond(id, result);
  });
  ipcMain.handle("wuu:reject-server-request", (_event, id: string, message: string) => {
    serverClient().reject(id, message);
  });

  createWindow();

  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) {
      createWindow();
    }
  });
});

app.on("before-quit", () => {
  cleanupTerminalSessions();
  client?.shutdown();
});

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") {
    app.quit();
  }
});
