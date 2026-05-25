import { spawnSync } from 'node:child_process'
import {
  accessSync,
  closeSync,
  constants,
  type Dirent,
  mkdirSync,
  openSync,
  readdirSync,
  readFileSync,
  readSync,
  realpathSync,
  statSync,
} from 'node:fs'
import { basename, isAbsolute, join, relative, resolve } from 'node:path'

type JsonValue =
  | null
  | boolean
  | number
  | string
  | JsonValue[]
  | { [key: string]: JsonValue }

type GitDiffStats = {
  files: number
  additions: number
  deletions: number
}

type GitChangeStatus =
  | 'modified'
  | 'added'
  | 'deleted'
  | 'renamed'
  | 'copied'
  | 'untracked'
  | 'unknown'

type GitChangeFile = {
  path: string
  old_path?: string
  status: GitChangeStatus
  additions: number
  deletions: number
  binary?: boolean
}

type TerminalSessionEvent =
  | {
      type: 'data'
      id: string
      text: string
    }
  | {
      type: 'exit'
      id: string
      exit_code: number | null
      signal: string | number | null
      duration_ms: number
      finished_at: string
    }
  | {
      type: 'error'
      id: string
      message: string
      finished_at: string
    }

type LocalTerminalSession = {
  id: string
  proc: ReturnType<typeof Bun.spawn>
  cwd: string
  shell: string
  startedAt: number
}

const FILE_TREE_MAX_PATHS = 4000
const FILE_PREVIEW_MAX_BYTES = 512 * 1024
const GIT_DIFF_PREVIEW_MAX_BYTES = 512 * 1024
const GIT_DIFF_COMMAND_MAX_BUFFER = 8 * 1024 * 1024
const FILE_TREE_IGNORED_DIRS = new Set([
  '.git',
  '.next',
  '.turbo',
  '.vite',
  'coverage',
  'dist',
  'node_modules',
  'out',
  'target',
])
const FILE_TREE_IGNORED_FILES = new Set(['.DS_Store'])

let terminalSessionCounter = 1
const terminalSessions = new Map<string, LocalTerminalSession>()
const terminalListeners = new Set<(event: TerminalSessionEvent) => void>()

export function addWuuTerminalListener(
  listener: (event: TerminalSessionEvent) => void,
): () => void {
  terminalListeners.add(listener)
  return () => {
    terminalListeners.delete(listener)
  }
}

export async function handleWuuDesktopRpc(
  workdir: string,
  method: string,
  params: unknown,
): Promise<JsonValue> {
  switch (method) {
    case 'workspace/no-project':
      return { cwd: allocateNoProjectCwd() }
    case 'git/status':
      return gitStatusResult(workdir) as JsonValue
    case 'git/changes':
      return gitChangesResult(workdir) as JsonValue
    case 'git/file-diff':
      return gitFileDiffResult(
        workdir,
        stringParam(params, 'path'),
      ) as JsonValue
    case 'git/checkout-branch':
      return checkoutGitBranch(
        workdir,
        stringParam(params, 'branch'),
      ) as JsonValue
    case 'git/create-checkout-branch':
      return createCheckoutGitBranch(
        workdir,
        stringParam(params, 'branch'),
      ) as JsonValue
    case 'git/commit':
      return commitGitChanges(workdir, asRecord(params)) as JsonValue
    case 'git/create-pr':
      return createPullRequest(workdir, asRecord(params)) as JsonValue
    case 'file-tree/list':
      return fileTreeListResult(workdir) as JsonValue
    case 'file/read':
      return readWorkspaceFileResult(
        workdir,
        stringParam(params, 'path'),
      ) as JsonValue
    case 'terminal/start':
      return startTerminalSession(workdir, asRecord(params)) as JsonValue
    case 'terminal/write':
      return writeTerminalSession(
        stringParam(params, 'id'),
        stringParam(params, 'data'),
      ) as JsonValue
    case 'terminal/resize':
      return resizeTerminalSession(
        stringParam(params, 'id'),
        numberParam(params, 'cols'),
        numberParam(params, 'rows'),
      ) as JsonValue
    case 'terminal/stop':
      return stopTerminalSession(stringParam(params, 'id')) as JsonValue
    default:
      throw new Error(`unsupported Wuu desktop method: ${method}`)
  }
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object'
    ? (value as Record<string, unknown>)
    : {}
}

function stringParam(value: unknown, key: string): string {
  const item = asRecord(value)[key]
  return typeof item === 'string' ? item : ''
}

function numberParam(value: unknown, key: string): number | undefined {
  const item = asRecord(value)[key]
  return typeof item === 'number' && Number.isFinite(item) ? item : undefined
}

function gitStatusResult(workdir: string) {
  const root = gitOutput(workdir, ['rev-parse', '--show-toplevel']) ?? workdir
  const insideWorkTree =
    gitOutput(root, ['rev-parse', '--is-inside-work-tree']) === 'true'
  if (!insideWorkTree) {
    return {
      is_repo: false,
      dirty_count: 0,
      diff: emptyGitDiffStats(),
      staged_diff: emptyGitDiffStats(),
    }
  }

  const branchName = gitOutput(root, ['branch', '--show-current'])
  const head = gitOutput(root, ['rev-parse', '--short', 'HEAD'])
  const branch = branchName || head
  const branches = gitOutput(root, [
    'for-each-ref',
    '--format=%(refname:short)',
    'refs/heads',
  ])
    ?.split('\n')
    .map((item) => item.trim())
    .filter(Boolean)
  const porcelain = gitOutput(root, ['status', '--porcelain'])
  const dirtyCount = porcelain
    ? porcelain.split('\n').filter((line) => line.trim()).length
    : 0
  const upstream = gitOutput(root, [
    'rev-parse',
    '--abbrev-ref',
    '--symbolic-full-name',
    '@{u}',
  ])
  const [aheadCount, behindCount] = upstream ? gitAheadBehind(root) : [0, 0]
  const remote = upstream?.split('/')[0] || firstGitRemote(root)
  const defaultBranch = remote ? gitDefaultBranch(root, remote) : undefined
  const ghAvailable = commandAvailable('gh', ['--version'])
  const prURL = branchName && ghAvailable ? ghPullRequestURL(root) : undefined

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
  }
}

function gitChangesResult(workdir: string) {
  const root = gitOutput(workdir, ['rev-parse', '--show-toplevel']) ?? workdir
  const insideWorkTree =
    gitOutput(root, ['rev-parse', '--is-inside-work-tree']) === 'true'
  if (!insideWorkTree) {
    return { is_repo: false, files: [] }
  }

  const filesByPath = new Map<string, GitChangeFile>()
  for (const file of parseGitNameStatus(
    gitOutput(root, [
      'diff',
      '--name-status',
      '--find-renames',
      'HEAD',
      '--',
    ]) ?? '',
  )) {
    filesByPath.set(file.path, file)
  }

  for (const file of parseGitNumstatFiles(
    gitOutput(root, ['diff', '--numstat', '--find-renames', 'HEAD', '--']) ??
      '',
  )) {
    const existing = filesByPath.get(file.path)
    filesByPath.set(file.path, {
      ...file,
      ...existing,
      additions: file.additions,
      deletions: file.deletions,
      binary: existing?.binary || file.binary,
    })
  }

  for (const path of listUntrackedGitFiles(root)) {
    const stats = untrackedGitFileStats(root, path)
    filesByPath.set(path, {
      path,
      status: 'untracked',
      additions: stats.additions,
      deletions: 0,
      binary: stats.binary,
    })
  }

  return {
    is_repo: true,
    root,
    files: Array.from(filesByPath.values()).sort((left, right) =>
      left.path.localeCompare(right.path),
    ),
  }
}

function gitFileDiffResult(workdir: string, path: string) {
  const root = gitOutput(workdir, ['rev-parse', '--show-toplevel']) ?? workdir
  const insideWorkTree =
    gitOutput(root, ['rev-parse', '--is-inside-work-tree']) === 'true'
  const { relativePath, absolutePath } = resolveGitRelativePath(root, path)
  if (!insideWorkTree) {
    return emptyGitFileDiffResult(relativePath, false)
  }

  const change = gitChangesResult(workdir).files.find(
    (file) => file.path === relativePath,
  ) ?? {
    path: relativePath,
    status: 'unknown' as const,
    additions: 0,
    deletions: 0,
  }

  if (change.status === 'untracked') {
    return gitUntrackedFileDiffResult(absolutePath, change)
  }

  const rawPatch = gitDiffOutput(root, relativePath)
  const truncatedPatch = truncateTextBytes(rawPatch, GIT_DIFF_PREVIEW_MAX_BYTES)
  const binary =
    change.binary ||
    rawPatch.includes('Binary files ') ||
    rawPatch.includes('GIT binary patch')
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
  }
}

function checkoutGitBranch(workdir: string, branch: string) {
  const current = gitStatusResult(workdir)
  const target = branch.trim()
  if (!current.is_repo) {
    throw new Error('current workspace is not a git repository')
  }
  if (!target || !current.branches?.includes(target)) {
    throw new Error('branch not found')
  }
  gitRun(workdir, ['checkout', target])
  return gitStatusResult(workdir)
}

function createCheckoutGitBranch(workdir: string, branch: string) {
  const current = gitStatusResult(workdir)
  const target = branch.trim()
  if (!current.is_repo) {
    throw new Error('current workspace is not a git repository')
  }
  validateGitBranchName(workdir, target)
  if (current.branches?.includes(target)) {
    throw new Error('branch already exists')
  }
  gitRun(workdir, ['checkout', '-b', target])
  return { status: gitStatusResult(workdir) }
}

function commitGitChanges(workdir: string, params: Record<string, unknown>) {
  const current = gitStatusResult(workdir)
  if (!current.is_repo) {
    throw new Error('current workspace is not a git repository')
  }
  if (params.include_unstaged !== false) {
    gitRun(workdir, ['add', '-A'])
  }
  const stagedDiff = gitStagedDiffStats(workdir)
  if (stagedDiff.files === 0) {
    throw new Error('there are no staged changes to commit')
  }
  const message =
    typeof params.message === 'string' && params.message.trim()
      ? params.message.trim()
      : generatedCommitMessage(workdir)
  gitRun(workdir, ['commit', '-m', message])
  const commit = gitOutput(workdir, ['rev-parse', '--short', 'HEAD']) ?? ''
  return {
    status: gitStatusResult(workdir),
    commit,
    message,
  }
}

function createPullRequest(workdir: string, params: Record<string, unknown>) {
  const status = gitStatusResult(workdir)
  if (!status.is_repo) {
    throw new Error('current workspace is not a git repository')
  }
  if (!status.gh_available) {
    throw new Error('GitHub CLI is not available')
  }
  const branch = gitOutput(workdir, ['branch', '--show-current'])
  if (!branch) {
    throw new Error('pull requests require a named branch')
  }
  if (status.default_branch && branch === status.default_branch) {
    throw new Error('create a feature branch before opening a pull request')
  }
  if (status.dirty_count > 0) {
    throw new Error(
      'commit or discard local changes before opening a pull request',
    )
  }

  const existingURL = ghPullRequestURL(workdir)
  if (existingURL) {
    return { status, url: existingURL, already_exists: true }
  }

  if (!status.upstream) {
    const remote = status.remote || 'origin'
    gitRun(workdir, ['push', '-u', remote, branch])
  }

  const args = ['pr', 'create']
  if (params.draft === true) {
    args.push('--draft')
  }
  const title = typeof params.title === 'string' ? params.title.trim() : ''
  const body = typeof params.body === 'string' ? params.body.trim() : ''
  if (title || body) {
    args.push('--title', title || branch, '--body', body || '')
  } else {
    args.push('--fill')
  }
  const url = ghOutput(workdir, args)
  if (!url) {
    throw new Error('GitHub CLI did not return a pull request URL')
  }
  return { status: gitStatusResult(workdir), url, already_exists: false }
}

function validateGitBranchName(cwd: string, branch: string): void {
  if (!branch) {
    throw new Error('branch name is required')
  }
  const result = spawnSync(
    'git',
    ['-C', cwd, 'check-ref-format', '--branch', branch],
    {
      cwd,
      encoding: 'utf8',
      env: process.env,
    },
  )
  if (result.status !== 0) {
    throw new Error(result.stderr.trim() || 'invalid branch name')
  }
}

function gitRun(cwd: string, args: string[]): string {
  const result = spawnSync('git', ['-C', cwd, ...args], {
    cwd,
    encoding: 'utf8',
    env: process.env,
  })
  if (result.status !== 0) {
    throw new Error(
      result.stderr.trim() ||
        result.stdout.trim() ||
        `git ${args.join(' ')} failed`,
    )
  }
  return result.stdout.trim()
}

function gitOutput(cwd: string, args: string[]): string | undefined {
  const result = spawnSync('git', ['-C', cwd, ...args], {
    cwd,
    encoding: 'utf8',
    env: process.env,
  })
  if (result.status !== 0) {
    return undefined
  }
  return result.stdout.trim() || undefined
}

function emptyGitDiffStats(): GitDiffStats {
  return { files: 0, additions: 0, deletions: 0 }
}

function gitDiffStats(cwd: string, includeUntracked: boolean): GitDiffStats {
  const stats = parseGitNumstat(
    gitOutput(cwd, ['diff', '--numstat', 'HEAD', '--']) ?? '',
  )
  if (!includeUntracked) {
    return stats
  }
  const untracked = listUntrackedGitFiles(cwd)
  if (!untracked.length) {
    return stats
  }
  let additions = 0
  for (const path of untracked.slice(0, 100)) {
    additions += countTextFileLines(resolve(cwd, path))
  }
  return {
    files: stats.files + untracked.length,
    additions: stats.additions + additions,
    deletions: stats.deletions,
  }
}

function gitStagedDiffStats(cwd: string): GitDiffStats {
  return parseGitNumstat(
    gitOutput(cwd, ['diff', '--cached', '--numstat', '--']) ?? '',
  )
}

function gitDiffOutput(cwd: string, relativePath: string): string {
  const result = spawnSync(
    'git',
    [
      '-C',
      cwd,
      'diff',
      '--no-ext-diff',
      '--find-renames',
      '--unified=3',
      'HEAD',
      '--',
      relativePath,
    ],
    {
      cwd,
      encoding: 'utf8',
      env: process.env,
      maxBuffer: GIT_DIFF_COMMAND_MAX_BUFFER,
    },
  )
  if (result.status !== 0 && !result.stdout) {
    throw new Error(
      result.stderr.trim() || `git diff failed for ${relativePath}`,
    )
  }
  return result.stdout
}

function parseGitNumstat(output: string): GitDiffStats {
  const stats = emptyGitDiffStats()
  for (const line of output.split('\n')) {
    const trimmed = line.trim()
    if (!trimmed) continue
    const [additions, deletions] = trimmed.split(/\s+/, 3)
    stats.files += 1
    if (additions !== '-') {
      stats.additions += Number(additions) || 0
    }
    if (deletions !== '-') {
      stats.deletions += Number(deletions) || 0
    }
  }
  return stats
}

function parseGitNameStatus(output: string): GitChangeFile[] {
  const files: GitChangeFile[] = []
  for (const line of output.split('\n')) {
    const trimmed = line.trim()
    if (!trimmed) continue
    const columns = trimmed.split('\t')
    const statusCode = columns[0] ?? ''
    const status = gitChangeStatus(statusCode)
    const oldPath =
      status === 'renamed' || status === 'copied' ? columns[1] : undefined
    const path =
      status === 'renamed' || status === 'copied' ? columns[2] : columns[1]
    if (!path) continue
    files.push({
      path,
      old_path: oldPath,
      status,
      additions: 0,
      deletions: 0,
    })
  }
  return files
}

function parseGitNumstatFiles(output: string): GitChangeFile[] {
  const files: GitChangeFile[] = []
  for (const line of output.split('\n')) {
    const trimmed = line.trim()
    if (!trimmed) continue
    const columns = trimmed.split('\t')
    if (columns.length < 3) continue
    const additions = columns[0]
    const deletions = columns[1]
    const path = columns.at(-1)
    if (!path) continue
    files.push({
      path,
      status: 'unknown',
      additions: additions === '-' ? 0 : Number(additions) || 0,
      deletions: deletions === '-' ? 0 : Number(deletions) || 0,
      binary: additions === '-' || deletions === '-',
    })
  }
  return files
}

function gitChangeStatus(statusCode: string): GitChangeStatus {
  switch (statusCode[0]) {
    case 'M':
      return 'modified'
    case 'A':
      return 'added'
    case 'D':
      return 'deleted'
    case 'R':
      return 'renamed'
    case 'C':
      return 'copied'
    default:
      return 'unknown'
  }
}

function listUntrackedGitFiles(cwd: string): string[] {
  return (
    gitOutput(cwd, ['ls-files', '--others', '--exclude-standard'])
      ?.split('\n')
      .map((item) => item.trim())
      .filter(Boolean) ?? []
  )
}

function untrackedGitFileStats(
  root: string,
  path: string,
): { additions: number; binary: boolean } {
  const { absolutePath } = resolveGitRelativePath(root, path)
  try {
    const stats = statSync(absolutePath)
    if (!stats.isFile()) {
      return { additions: 0, binary: false }
    }
    const previewBuffer = readFilePreviewBuffer(
      absolutePath,
      Math.min(stats.size, FILE_PREVIEW_MAX_BYTES),
    )
    const binary = previewBuffer.includes(0)
    return {
      additions: binary ? 0 : countTextFileLines(absolutePath),
      binary,
    }
  } catch {
    return { additions: 0, binary: false }
  }
}

function gitUntrackedFileDiffResult(
  absolutePath: string,
  change: GitChangeFile,
) {
  try {
    const stats = statSync(absolutePath)
    if (!stats.isFile()) {
      return emptyGitFileDiffResult(change.path, true)
    }
    const readLimit = Math.min(stats.size, GIT_DIFF_PREVIEW_MAX_BYTES + 1)
    const buffer = readFilePreviewBuffer(absolutePath, readLimit)
    const truncated = stats.size > GIT_DIFF_PREVIEW_MAX_BYTES
    const previewBuffer = buffer.subarray(
      0,
      truncated ? GIT_DIFF_PREVIEW_MAX_BYTES : buffer.length,
    )
    const binary = previewBuffer.includes(0)
    const patch = binary
      ? `Binary file ${change.path} is untracked`
      : buildUntrackedPatch(
          change.path,
          previewBuffer.toString('utf8'),
          truncated,
        )
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
    }
  } catch {
    return emptyGitFileDiffResult(change.path, true)
  }
}

function buildUntrackedPatch(
  path: string,
  text: string,
  truncated: boolean,
): string {
  const lines = splitPatchTextLines(text)
  const patchLines = [
    `diff --git a/${path} b/${path}`,
    'new file mode 100644',
    '--- /dev/null',
    `+++ b/${path}`,
    `@@ -0,0 +1,${lines.length} @@`,
    ...lines.map((line) => `+${line}`),
  ]
  if (truncated) {
    patchLines.push('+')
    patchLines.push('+[diff truncated]')
  }
  return patchLines.join('\n')
}

function splitPatchTextLines(text: string): string[] {
  if (!text) return []
  const withoutFinalNewline = text.endsWith('\n') ? text.slice(0, -1) : text
  return withoutFinalNewline ? withoutFinalNewline.split(/\r?\n/) : []
}

function readFilePreviewBuffer(filePath: string, readLimit: number): Buffer {
  const buffer = Buffer.alloc(readLimit)
  const descriptor = openSync(filePath, 'r')
  let bytesRead = 0
  try {
    bytesRead = readSync(descriptor, buffer, 0, readLimit, 0)
  } finally {
    closeSync(descriptor)
  }
  return buffer.subarray(0, bytesRead)
}

function resolveGitRelativePath(
  root: string,
  path: string,
): { relativePath: string; absolutePath: string } {
  const relativePath = normalizeWorkspaceRelativePath(path)
  const absolutePath = resolve(root, relativePath)
  const relativeToRoot = relative(root, absolutePath)
  if (
    !relativeToRoot ||
    relativeToRoot.startsWith('..') ||
    isAbsolute(relativeToRoot)
  ) {
    throw new Error('file is outside the current git repository')
  }
  return { relativePath, absolutePath }
}

function truncateTextBytes(
  text: string,
  maxBytes: number,
): { text: string; truncated: boolean } {
  const buffer = Buffer.from(text, 'utf8')
  if (buffer.byteLength <= maxBytes) {
    return { text, truncated: false }
  }
  return {
    text: `${buffer.subarray(0, maxBytes).toString('utf8')}\n[diff truncated]\n`,
    truncated: true,
  }
}

function emptyGitFileDiffResult(path: string, isRepo: boolean) {
  return {
    is_repo: isRepo,
    path,
    status: 'unknown',
    additions: 0,
    deletions: 0,
    binary: false,
    patch: '',
    truncated: false,
  }
}

function countTextFileLines(filePath: string): number {
  try {
    const stats = statSync(filePath)
    if (!stats.isFile() || stats.size > 1024 * 1024) {
      return 0
    }
    const content = readFileSync(filePath)
    if (content.includes(0)) {
      return 0
    }
    const text = content.toString('utf8')
    if (!text) {
      return 0
    }
    return text.endsWith('\n')
      ? text.split('\n').length - 1
      : text.split(/\r\n|\n|\r/).length
  } catch {
    return 0
  }
}

function gitAheadBehind(cwd: string): [number, number] {
  const output = gitOutput(cwd, [
    'rev-list',
    '--left-right',
    '--count',
    'HEAD...@{u}',
  ])
  const [ahead, behind] = output
    ?.split(/\s+/, 2)
    .map((item) => Number(item) || 0) ?? [0, 0]
  return [ahead, behind]
}

function firstGitRemote(cwd: string): string | undefined {
  return gitOutput(cwd, ['remote'])
    ?.split('\n')
    .map((item) => item.trim())
    .find(Boolean)
}

function gitDefaultBranch(cwd: string, remote: string): string | undefined {
  const symbolic = gitOutput(cwd, [
    'symbolic-ref',
    '--short',
    `refs/remotes/${remote}/HEAD`,
  ])
  if (symbolic?.startsWith(`${remote}/`)) {
    return symbolic.slice(remote.length + 1)
  }
  return gitOutput(cwd, ['remote', 'show', remote])
    ?.split('\n')
    .map((line) => line.trim())
    .find((line) => line.startsWith('HEAD branch:'))
    ?.replace('HEAD branch:', '')
    .trim()
}

function commandAvailable(command: string, args: string[]): boolean {
  const result = spawnSync(command, args, {
    encoding: 'utf8',
    env: process.env,
  })
  return result.status === 0
}

function ghOutput(cwd: string, args: string[]): string | undefined {
  const result = spawnSync('gh', args, {
    cwd,
    encoding: 'utf8',
    env: process.env,
  })
  if (result.status !== 0) {
    if (args[0] === 'pr' && args[1] === 'view') {
      return undefined
    }
    throw new Error(
      result.stderr.trim() ||
        result.stdout.trim() ||
        `gh ${args.join(' ')} failed`,
    )
  }
  return result.stdout.trim() || undefined
}

function ghPullRequestURL(cwd: string): string | undefined {
  return ghOutput(cwd, ['pr', 'view', '--json', 'url', '--jq', '.url'])
}

function generatedCommitMessage(cwd: string): string {
  const files = gitOutput(cwd, ['diff', '--cached', '--name-only'])
    ?.split('\n')
    .map((item) => item.trim())
    .filter(Boolean)
  if (!files?.length) {
    return 'Update workspace changes'
  }
  if (files.length === 1) {
    return `Update ${basename(files[0])}`
  }
  const topLevel = files.map((file) => file.split('/', 1)[0]).filter(Boolean)
  const sharedArea =
    topLevel.length > 0 && topLevel.every((item) => item === topLevel[0])
      ? topLevel[0]
      : ''
  return sharedArea
    ? `Update ${sharedArea} changes`
    : 'Update workspace changes'
}

function fileTreeListResult(workdir: string) {
  const paths: string[] = []
  const truncated = collectFileTreePaths(workdir, '', paths)
  return { root: workdir, paths, truncated }
}

function readWorkspaceFileResult(workdir: string, path: string) {
  const relativeFilePath = normalizeWorkspaceRelativePath(path)
  const absolutePath = resolveWorkspacePath(workdir, relativeFilePath)
  const stats = statSync(absolutePath)
  if (!stats.isFile()) {
    throw new Error('selected path is not a file')
  }

  const readLimit = Math.min(stats.size, FILE_PREVIEW_MAX_BYTES + 1)
  const buffer = Buffer.alloc(readLimit)
  const descriptor = openSync(absolutePath, 'r')
  let bytesRead = 0
  try {
    bytesRead = readSync(descriptor, buffer, 0, readLimit, 0)
  } finally {
    closeSync(descriptor)
  }
  const truncated = stats.size > FILE_PREVIEW_MAX_BYTES
  const previewBuffer = buffer.subarray(
    0,
    truncated ? FILE_PREVIEW_MAX_BYTES : bytesRead,
  )
  const binary = previewBuffer.includes(0)

  return {
    root: workdir,
    path: relativeFilePath,
    absolute_path: absolutePath,
    size_bytes: stats.size,
    binary,
    truncated,
    text: binary ? undefined : previewBuffer.toString('utf8'),
  }
}

function normalizeWorkspaceRelativePath(path: string): string {
  const value = path
    .trim()
    .replace(/\\/g, '/')
    .replace(/^\/+/, '')
    .replace(/\/+$/, '')
  if (
    !value ||
    value.includes('\0') ||
    value.split('/').some((segment) => segment === '..')
  ) {
    throw new Error('invalid workspace file path')
  }
  return value
}

function resolveWorkspacePath(root: string, relativeFilePath: string): string {
  const absolutePath = resolve(root, relativeFilePath)
  const relativeToRoot = relative(root, absolutePath)
  if (
    !relativeToRoot ||
    relativeToRoot.startsWith('..') ||
    isAbsolute(relativeToRoot)
  ) {
    throw new Error('file is outside the current workspace')
  }

  const realRoot = realpathSync(root)
  const realFile = realpathSync(absolutePath)
  const realRelative = relative(realRoot, realFile)
  if (
    !realRelative ||
    realRelative.startsWith('..') ||
    isAbsolute(realRelative)
  ) {
    throw new Error('file is outside the current workspace')
  }
  return absolutePath
}

function collectFileTreePaths(
  root: string,
  relativeDirectory: string,
  paths: string[],
): boolean {
  if (paths.length >= FILE_TREE_MAX_PATHS) {
    return true
  }

  const directory = relativeDirectory ? join(root, relativeDirectory) : root
  let entries: Dirent[]
  try {
    entries = readdirSync(directory, { withFileTypes: true })
  } catch {
    return false
  }

  entries.sort((left, right) => {
    const leftDirectory = left.isDirectory()
    const rightDirectory = right.isDirectory()
    if (leftDirectory !== rightDirectory) {
      return leftDirectory ? -1 : 1
    }
    return left.name.localeCompare(right.name, undefined, {
      sensitivity: 'base',
    })
  })

  for (const entry of entries) {
    if (FILE_TREE_IGNORED_FILES.has(entry.name)) {
      continue
    }

    const relativePath = relativeDirectory
      ? `${relativeDirectory}/${entry.name}`
      : entry.name
    if (entry.isDirectory()) {
      if (FILE_TREE_IGNORED_DIRS.has(entry.name)) {
        continue
      }
      paths.push(`${relativePath}/`)
      if (collectFileTreePaths(root, relativePath, paths)) {
        return true
      }
      continue
    }

    if (entry.isFile() || entry.isSymbolicLink()) {
      paths.push(relativePath)
    }

    if (paths.length >= FILE_TREE_MAX_PATHS) {
      return true
    }
  }

  return false
}

function allocateNoProjectCwd(): string {
  const home = process.env.HOME || process.cwd()
  const baseDir = join(home, 'Documents', 'Wuu', formatLocalDate(new Date()))
  mkdirSync(baseDir, { recursive: true })
  for (let index = 0; index < 1000; index += 1) {
    const name = index === 0 ? 'new-chat' : `new-chat-${index + 1}`
    const candidate = join(baseDir, name)
    try {
      statSync(candidate)
      continue
    } catch {}
    mkdirSync(candidate, { recursive: true })
    return candidate
  }
  throw new Error(`failed to allocate no-project workspace under ${baseDir}`)
}

function formatLocalDate(date: Date): string {
  const year = String(date.getFullYear())
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

function startTerminalSession(
  workdir: string,
  params: Record<string, unknown>,
) {
  const id = `term-${terminalSessionCounter++}`
  const startedAt = Date.now()
  const shell = terminalShell()
  const cols = normalizeTerminalSize(
    typeof params.cols === 'number' ? params.cols : undefined,
    80,
    20,
    500,
  )
  const rows = normalizeTerminalSize(
    typeof params.rows === 'number' ? params.rows : undefined,
    24,
    6,
    200,
  )
  const decoder = new TextDecoder()
  const proc = Bun.spawn([shell.command, ...shell.args], {
    cwd: workdir,
    terminal: {
      cols,
      rows,
      data(_terminal, data) {
        const text = decoder.decode(data, { stream: true })
        if (text) {
          emitTerminalEvent({ type: 'data', id, text })
        }
      },
    },
    env: {
      ...process.env,
      CLICOLOR: '1',
      COLORTERM: 'truecolor',
      FORCE_COLOR: '1',
      TERM: 'xterm-256color',
    },
  })

  const session: LocalTerminalSession = {
    id,
    proc,
    cwd: workdir,
    shell: shell.command,
    startedAt,
  }
  terminalSessions.set(id, session)

  void proc.exited.then((exitCode) => {
    const trailing = decoder.decode()
    if (trailing) {
      emitTerminalEvent({ type: 'data', id, text: trailing })
    }
    terminalSessions.delete(id)
    emitTerminalEvent({
      type: 'exit',
      id,
      exit_code: typeof exitCode === 'number' ? exitCode : null,
      signal: null,
      duration_ms: Date.now() - startedAt,
      finished_at: new Date().toISOString(),
    })
  })

  return {
    id,
    cwd: workdir,
    shell: shell.command,
    started_at: new Date(startedAt).toISOString(),
  }
}

function writeTerminalSession(id: string, data: string) {
  const session = terminalSessions.get(id)
  if (!session) {
    return { ok: false }
  }
  session.proc.terminal?.write(data)
  return { ok: true }
}

function resizeTerminalSession(
  id: string,
  cols: number | undefined,
  rows: number | undefined,
) {
  const session = terminalSessions.get(id)
  if (!session) {
    return { ok: false }
  }
  session.proc.terminal?.resize(
    normalizeTerminalSize(cols, 80, 20, 500),
    normalizeTerminalSize(rows, 24, 6, 200),
  )
  return { ok: true }
}

function stopTerminalSession(id: string) {
  const session = terminalSessions.get(id)
  if (!session) {
    return { ok: false }
  }
  terminalSessions.delete(id)
  try {
    session.proc.terminal?.close()
    session.proc.kill()
  } catch (error) {
    emitTerminalEvent({
      type: 'error',
      id,
      message:
        error instanceof Error
          ? error.message
          : 'Failed to stop terminal session.',
      finished_at: new Date().toISOString(),
    })
  }
  return { ok: true }
}

function emitTerminalEvent(event: TerminalSessionEvent): void {
  for (const listener of terminalListeners) {
    listener(event)
  }
}

function normalizeTerminalSize(
  value: number | undefined,
  fallback: number,
  min: number,
  max: number,
): number {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    return fallback
  }
  return Math.max(min, Math.min(max, Math.floor(value)))
}

function terminalShell(): { command: string; args: string[] } {
  if (process.platform === 'win32') {
    return { command: process.env.ComSpec || 'cmd.exe', args: [] }
  }
  return { command: resolveTerminalShell(), args: ['-l'] }
}

function resolveTerminalShell(): string {
  const candidates = [
    process.env.SHELL,
    '/bin/zsh',
    '/bin/bash',
    '/bin/sh',
    '/usr/bin/zsh',
    '/usr/bin/bash',
    '/usr/bin/sh',
  ]
  for (const candidate of candidates) {
    if (isExecutableFile(candidate)) {
      return candidate
    }
  }
  return '/bin/sh'
}

function isExecutableFile(path: string | undefined): path is string {
  if (!path || !isAbsolute(path)) {
    return false
  }
  try {
    if (!statSync(path).isFile()) {
      return false
    }
    accessSync(path, constants.X_OK)
    return true
  } catch {
    return false
  }
}
