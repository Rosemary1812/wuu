import { getBrowserOSAdapter } from '@/lib/browseros/adapter'
import {
  AgentPortError,
  getAgentServerUrl,
  McpPortError,
} from '@/lib/browseros/helpers'
import { env } from '@/lib/env'
import type {
  ConfigCodexModelsResult,
  ConfigModelUpdateResult,
  DesktopProject,
  FileTreeListResult,
  GitChangesResult,
  GitCommitResult,
  GitCreateBranchResult,
  GitFileDiffResult,
  GitPullRequestResult,
  GitStatusResult,
  InitializeResult,
  ProjectListResult,
  RuntimeContext,
  TerminalSessionActionResult,
  TerminalSessionEvent,
  TerminalSessionStartResult,
  Thread,
  Turn,
  WorkspaceDirectoryListResult,
  WorkspaceFileReadResult,
  WuuDesktopApi,
} from '@browseros/workbench-ui/shared/protocol'
import { ServerEventHub } from './browseros-wuu-event-hub'

type DesktopRpcResponse<T> = {
  result?: T
  error?: string
}

type WuuRpcResponse<T> = {
  result?: T
  error?: string
}

const PROJECTS_KEY = 'wuu.browseros.projects'
const ACTIVE_CONTEXT_KEY = 'wuu.browseros.activeContext'
const WUU_SERVER_RETRY_DELAYS_MS = [250, 500, 1000, 2000, 4000, 4000]

let installed = false
let activeContext: RuntimeContext | undefined = loadActiveContext()

export function installWuuBrowserOSAdapter(): void {
  if (installed) return
  installed = true
  installRenderableFileUrl()

  window.wuu = {
    listProjects,
    createBlankProject,
    chooseProjectFolder,
    selectProject,
    selectNoProject,
    gitStatus: () => desktopRpc<GitStatusResult>('git/status'),
    listGitChanges: () => desktopRpc<GitChangesResult>('git/changes'),
    readGitFileDiff: (path) =>
      desktopRpc<GitFileDiffResult>('git/file-diff', { path }),
    checkoutGitBranch: (branch) =>
      desktopRpc<GitStatusResult>('git/checkout-branch', { branch }),
    createCheckoutGitBranch: (branch) =>
      desktopRpc<GitCreateBranchResult>('git/create-checkout-branch', {
        branch,
      }),
    commitGitChanges: (params) =>
      desktopRpc<GitCommitResult>('git/commit', params),
    createPullRequest: (params) =>
      desktopRpc<GitPullRequestResult>('git/create-pr', params),
    listWorkspaceFiles: () => desktopRpc<FileTreeListResult>('file-tree/list'),
    listWorkspaceDirectory: (path) =>
      desktopRpc<WorkspaceDirectoryListResult>('file-directory/list', {
        path: path ?? '',
      }),
    readWorkspaceFile: (path) =>
      desktopRpc<WorkspaceFileReadResult>('file/read', { path }),
    startTerminalSession: (params) =>
      desktopRpc<TerminalSessionStartResult>('terminal/start', params),
    writeTerminalSession: (id, data) =>
      desktopRpc<TerminalSessionActionResult>('terminal/write', { id, data }),
    resizeTerminalSession: (id, cols, rows) =>
      desktopRpc<TerminalSessionActionResult>('terminal/resize', {
        id,
        cols,
        rows,
      }),
    stopTerminalSession: (id) =>
      desktopRpc<TerminalSessionActionResult>('terminal/stop', { id }),
    initialize: () => wuuRpc<InitializeResult>('initialize'),
    loadCodexModels: (provider) =>
      wuuRpc<ConfigCodexModelsResult>('config/codex/models', {
        provider: provider ?? '',
      }),
    updateRuntimeSettings: (provider, model, effort, connection, variant) =>
      wuuRpc<ConfigModelUpdateResult>('config/model/update', {
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
    startThread: () => wuuRpc<{ thread: Thread }>('thread/start'),
    resumeThread: (sessionId) =>
      wuuRpc<{ thread: Thread }>('thread/resume', {
        session_id: sessionId ?? '',
      }),
    forkThread: (threadId, turnId, itemId) =>
      wuuRpc<{ thread: Thread }>('thread/fork', {
        thread_id: threadId,
        turn_id: turnId ?? '',
        item_id: itemId ?? '',
      }),
    listThreads: () => wuuRpc<{ threads: Thread[] }>('thread/list'),
    pinThread: (threadId, pinned) =>
      wuuRpc<{ thread: Thread }>('thread/pin', { thread_id: threadId, pinned }),
    archiveThread: (threadId, archived) =>
      wuuRpc<{ thread: Thread }>('thread/archive', {
        thread_id: threadId,
        archived,
      }),
    startTurn: (threadId, prompt, images) =>
      wuuRpc<{ turn: Turn }>('turn/start', {
        thread_id: threadId,
        prompt,
        images: images ?? [],
      }),
    interruptTurn: (threadId) =>
      wuuRpc<{ ok: boolean }>('turn/interrupt', { thread_id: threadId }),
    respondToServerRequest,
    rejectServerRequest,
    onServerEvent: (handler) => serverEventHub.subscribe(handler),
    onTerminalEvent: (handler) => terminalEventHub.subscribe(handler),
    onWindowResizeState: (handler) => {
      handler({ resizing: false })
      return () => {}
    },
  } satisfies WuuDesktopApi
}

function installRenderableFileUrl(): void {
  const staticPort = env.VITE_BROWSEROS_SERVER_PORT
  if (staticPort) {
    setRenderableFileUrl(`http://127.0.0.1:${staticPort}`)
  }

  void getAgentServerUrl()
    .then((serverUrl) => {
      setRenderableFileUrl(serverUrl)
    })
    .catch(() => {
      if (!window.wuuRenderableFileURL) {
        window.wuuRenderableFileURL = undefined
      }
    })
}

function setRenderableFileUrl(serverUrl: string): void {
  window.wuuRenderableFileURL = (encodedPath) =>
    `${serverUrl}/wuu/file/local/${encodedPath}`
}

async function listProjects(): Promise<ProjectListResult> {
  const result = await desktopRpc<ProjectListResult>(
    'project/list',
    {
      migration_projects: loadProjects(),
      migration_active_context: activeContext,
    },
    false,
  )
  return applyProjectList(result)
}

async function chooseProjectFolder(): Promise<ProjectListResult> {
  return chooseAndAddProject({
    title: '使用现有文件夹',
    canCreateDirectories: false,
  })
}

async function createBlankProject(): Promise<ProjectListResult> {
  return chooseAndAddProject({
    title: '新建空白项目',
    canCreateDirectories: true,
  })
}

async function chooseAndAddProject(options: {
  title: string
  canCreateDirectories: boolean
}): Promise<ProjectListResult> {
  const selected = await getBrowserOSAdapter().choosePath({
    type: 'folder',
    title: options.title,
    startingDirectory: activeContext?.cwd,
    canCreateDirectories: options.canCreateDirectories,
  })
  if (!selected?.path) {
    return listProjects()
  }

  const result = await desktopRpc<ProjectListResult>(
    'project/add',
    { path: selected.path, name: selected.name },
    false,
  )
  return applyProjectList(result)
}

async function selectProject(projectId: string): Promise<ProjectListResult> {
  const result = await desktopRpc<ProjectListResult>(
    'project/select',
    { project_id: projectId },
    false,
  )
  return applyProjectList(result)
}

async function selectNoProject(
  fresh = false,
  cwd?: string,
): Promise<ProjectListResult> {
  const result = await desktopRpc<ProjectListResult>(
    'project/select-none',
    cwd ? { fresh, cwd } : { fresh },
    false,
  )
  return applyProjectList(result)
}

async function applyProjectList(
  result: ProjectListResult,
): Promise<ProjectListResult> {
  activeContext = result.active_context
  saveProjects(result.projects)
  saveActiveContext(activeContext)
  await serverEventHub.setWorkdir(activeContext?.cwd)
  await terminalEventHub.ensureConnected()
  return result
}

async function desktopRpc<T>(
  method: string,
  params?: unknown,
  requireActiveWorkdir = true,
): Promise<T> {
  const response = await fetchWuuPath('/wuu/desktop', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      workdir: requireActiveWorkdir ? requireWorkdir() : activeContext?.cwd,
      method,
      params,
    }),
  })
  const data = (await response
    .json()
    .catch(() => ({}))) as DesktopRpcResponse<T>
  if (!response.ok || data.error) {
    throw new Error(
      data.error || `Wuu desktop request failed: ${response.status}`,
    )
  }
  return data.result as T
}

async function wuuRpc<T>(method: string, params?: unknown): Promise<T> {
  const response = await fetchWuuPath('/wuu/rpc', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      workdir: requireWorkdir(),
      method,
      params,
    }),
  })
  const data = (await response.json().catch(() => ({}))) as WuuRpcResponse<T>
  if (!response.ok || data.error) {
    throw new Error(
      data.error || `Wuu bridge request failed: ${response.status}`,
    )
  }
  return data.result as T
}

async function respondToServerRequest(
  id: string,
  result: unknown,
): Promise<void> {
  await postClientResponse('/wuu/respond', {
    workdir: requireWorkdir(),
    id,
    result,
  })
}

async function rejectServerRequest(id: string, message: string): Promise<void> {
  await postClientResponse('/wuu/reject', {
    workdir: requireWorkdir(),
    id,
    message,
  })
}

async function postClientResponse(path: string, body: unknown): Promise<void> {
  const response = await fetchWuuPath(path, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(body),
  })
  if (!response.ok) {
    const data = (await response.json().catch(() => ({}))) as { error?: string }
    throw new Error(data.error || `Wuu response failed: ${response.status}`)
  }
}

async function fetchWuuPath(
  path: string,
  init?: RequestInit,
): Promise<Response> {
  let lastError: unknown

  for (const delayMs of [0, ...WUU_SERVER_RETRY_DELAYS_MS]) {
    if (delayMs > 0) {
      await sleep(delayMs)
    }

    try {
      const serverUrl = await getAgentServerUrl()
      return await fetch(`${serverUrl}${path}`, init)
    } catch (error) {
      lastError = error
      if (!isTransientWuuStartupError(error)) {
        throw error
      }
    }
  }

  throw lastError
}

function isTransientWuuStartupError(error: unknown): boolean {
  return (
    isTransientFetchError(error) ||
    error instanceof AgentPortError ||
    error instanceof McpPortError
  )
}

function isTransientFetchError(error: unknown): boolean {
  return error instanceof TypeError && /fetch/i.test(error.message)
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

function requireWorkdir(): string {
  if (!activeContext?.cwd) {
    throw new Error('Wuu workspace is not selected')
  }
  return activeContext.cwd
}

function loadProjects(): DesktopProject[] {
  try {
    const value = window.localStorage.getItem(PROJECTS_KEY)
    if (!value) return []
    const parsed = JSON.parse(value) as unknown
    if (!Array.isArray(parsed)) return []
    return parsed.filter(isDesktopProject)
  } catch {
    return []
  }
}

function saveProjects(projects: DesktopProject[]): void {
  window.localStorage.setItem(PROJECTS_KEY, JSON.stringify(projects))
}

function loadActiveContext(): RuntimeContext | undefined {
  try {
    const value = window.localStorage.getItem(ACTIVE_CONTEXT_KEY)
    return normalizeRuntimeContext(JSON.parse(value || 'null'), loadProjects())
  } catch {
    return undefined
  }
}

function saveActiveContext(context: RuntimeContext | undefined): void {
  if (!context) {
    window.localStorage.removeItem(ACTIVE_CONTEXT_KEY)
    return
  }
  window.localStorage.setItem(ACTIVE_CONTEXT_KEY, JSON.stringify(context))
}

function normalizeRuntimeContext(
  value: unknown,
  projects: DesktopProject[],
): RuntimeContext | undefined {
  if (!value || typeof value !== 'object') return undefined
  const context = value as Partial<RuntimeContext>
  if (context.kind === 'project' && typeof context.project_id === 'string') {
    const project = projects.find(
      (candidate) => candidate.id === context.project_id,
    )
    return project
      ? { kind: 'project', project_id: project.id, cwd: project.path }
      : undefined
  }
  if (context.kind === 'no_project' && typeof context.cwd === 'string') {
    return { kind: 'no_project', cwd: context.cwd }
  }
  return undefined
}

function isDesktopProject(value: unknown): value is DesktopProject {
  if (!value || typeof value !== 'object') return false
  const project = value as Partial<DesktopProject>
  return (
    typeof project.id === 'string' &&
    typeof project.name === 'string' &&
    typeof project.path === 'string' &&
    typeof project.created_at === 'string' &&
    typeof project.updated_at === 'string'
  )
}

class TerminalEventHub {
  private readonly listeners = new Set<(event: TerminalSessionEvent) => void>()
  private source: EventSource | null = null

  subscribe(handler: (event: TerminalSessionEvent) => void): () => void {
    this.listeners.add(handler)
    void this.ensureConnected()
    return () => {
      this.listeners.delete(handler)
      if (this.listeners.size === 0) {
        this.close()
      }
    }
  }

  async ensureConnected(): Promise<void> {
    if (this.source || this.listeners.size === 0) return
    const serverUrl = await getAgentServerUrl()
    if (this.source || this.listeners.size === 0) return
    this.source = new EventSource(`${serverUrl}/wuu/terminal/events`)
    this.source.addEventListener('terminal', (event) => {
      if (!event.data) return
      try {
        const payload = JSON.parse(event.data) as TerminalSessionEvent
        for (const listener of this.listeners) {
          listener(payload)
        }
      } catch {}
    })
  }

  private close(): void {
    this.source?.close()
    this.source = null
  }
}

const serverEventHub = new ServerEventHub({
  getServerUrl: getAgentServerUrl,
  getWorkdir: () => activeContext?.cwd,
})
const terminalEventHub = new TerminalEventHub()
