import { getBrowserOSAdapter } from '@/lib/browseros/adapter'
import { getAgentServerUrl } from '@/lib/browseros/helpers'
import { CHROME_PREFS } from '@/lib/browseros/prefs'
import { env } from '@/lib/env'
import type {
  AppServerNotification,
  AppServerRequest,
  BrowserSettingsResult,
  BrowserSettingsUpdate,
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
  ServerEvent,
  TerminalSessionActionResult,
  TerminalSessionEvent,
  TerminalSessionStartResult,
  Thread,
  Turn,
  WorkspaceFileReadResult,
  WuuDesktopApi,
} from './shared/protocol'

type WuuBridgeEvent =
  | {
      type: 'notification'
      message: AppServerNotification
    }
  | {
      type: 'server-started'
      workdir: string
    }
  | {
      type: 'server-request'
      message: Required<AppServerRequest>
    }
  | {
      type: 'server-error'
      message: string
    }
  | {
      type: 'server-exit'
      code: number | null
    }

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
const WUU_FETCH_RETRY_DELAYS_MS = [200, 500, 1000, 1500]

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
    updateRuntimeSettings: (provider, model, effort) =>
      wuuRpc<ConfigModelUpdateResult>('config/model/update', {
        provider,
        model,
        ...(effort === undefined ? {} : { effort }),
      }),
    loadBrowserSettings,
    updateBrowserSettings,
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

async function loadBrowserSettings(): Promise<BrowserSettingsResult> {
  try {
    const verticalTabsPref = await getBrowserOSAdapter().getPref(
      CHROME_PREFS.VERTICAL_TABS_ENABLED,
    )
    return {
      vertical_tabs_supported: true,
      vertical_tabs_enabled: verticalTabsPref?.value !== false,
    }
  } catch {
    return {
      vertical_tabs_supported: false,
      vertical_tabs_enabled: true,
    }
  }
}

async function updateBrowserSettings(
  settings: BrowserSettingsUpdate,
): Promise<BrowserSettingsResult> {
  if (settings.vertical_tabs_enabled !== undefined) {
    const success = await getBrowserOSAdapter().setPref(
      CHROME_PREFS.VERTICAL_TABS_ENABLED,
      settings.vertical_tabs_enabled,
    )
    if (!success) {
      throw new Error('Failed to update vertical tabs setting')
    }
  }
  return loadBrowserSettings()
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
  const projects = loadProjects()
  activeContext = normalizeStoredContext(activeContext, projects)
  saveActiveContext(activeContext)
  await serverEventHub.setWorkdir(activeContext?.cwd)
  await terminalEventHub.ensureConnected()
  return projectListResult(projects, activeContext)
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

  return addProject(selected.path, selected.name)
}

async function selectProject(projectId: string): Promise<ProjectListResult> {
  const projects = loadProjects()
  const project = projects.find((candidate) => candidate.id === projectId)
  if (!project) {
    throw new Error('project not found')
  }

  activeContext = {
    kind: 'project',
    project_id: project.id,
    cwd: project.path,
  }
  saveActiveContext(activeContext)
  await serverEventHub.setWorkdir(activeContext.cwd)
  return projectListResult(projects, activeContext)
}

async function selectNoProject(fresh = false): Promise<ProjectListResult> {
  if (fresh || activeContext?.kind !== 'no_project') {
    const result = await desktopRpc<{ cwd: string }>(
      'workspace/no-project',
      { fresh },
      false,
    )
    activeContext = { kind: 'no_project', cwd: result.cwd }
  }

  saveActiveContext(activeContext)
  await serverEventHub.setWorkdir(activeContext?.cwd)
  return projectListResult(loadProjects(), activeContext)
}

async function addProject(
  projectPath: string,
  fallbackName?: string,
): Promise<ProjectListResult> {
  const projects = loadProjects()
  const id = projectId(projectPath)
  const now = new Date().toISOString()
  const existingIndex = projects.findIndex((project) => project.id === id)
  const project: DesktopProject = {
    id,
    name: fallbackName || projectName(projectPath),
    path: projectPath,
    created_at: existingIndex >= 0 ? projects[existingIndex].created_at : now,
    updated_at: now,
  }

  if (existingIndex >= 0) {
    projects[existingIndex] = project
  } else {
    projects.unshift(project)
  }

  saveProjects(projects)
  activeContext = {
    kind: 'project',
    project_id: project.id,
    cwd: project.path,
  }
  saveActiveContext(activeContext)
  await serverEventHub.setWorkdir(activeContext.cwd)
  return projectListResult(projects, activeContext)
}

function projectListResult(
  projects: DesktopProject[],
  context?: RuntimeContext,
): ProjectListResult {
  return {
    projects,
    active_context: context,
    active_project_id:
      context?.kind === 'project' ? context.project_id : undefined,
  }
}

async function desktopRpc<T>(
  method: string,
  params?: unknown,
  requireActiveWorkdir = true,
): Promise<T> {
  const serverUrl = await getAgentServerUrl()
  const response = await fetchWuu(`${serverUrl}/wuu/desktop`, {
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
  const serverUrl = await getAgentServerUrl()
  const response = await fetchWuu(`${serverUrl}/wuu/rpc`, {
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
  const serverUrl = await getAgentServerUrl()
  await postClientResponse(`${serverUrl}/wuu/respond`, {
    workdir: requireWorkdir(),
    id,
    result,
  })
}

async function rejectServerRequest(id: string, message: string): Promise<void> {
  const serverUrl = await getAgentServerUrl()
  await postClientResponse(`${serverUrl}/wuu/reject`, {
    workdir: requireWorkdir(),
    id,
    message,
  })
}

async function postClientResponse(url: string, body: unknown): Promise<void> {
  const response = await fetchWuu(url, {
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

async function fetchWuu(
  input: RequestInfo | URL,
  init?: RequestInit,
): Promise<Response> {
  let lastError: unknown

  for (const delayMs of [0, ...WUU_FETCH_RETRY_DELAYS_MS]) {
    if (delayMs > 0) {
      await sleep(delayMs)
    }

    try {
      return await fetch(input, init)
    } catch (error) {
      lastError = error
      if (!isTransientFetchError(error)) {
        throw error
      }
    }
  }

  throw lastError
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

function normalizeStoredContext(
  context: RuntimeContext | undefined,
  projects: DesktopProject[],
): RuntimeContext | undefined {
  return normalizeRuntimeContext(context, projects)
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

function projectId(projectPath: string): string {
  return base64Url(projectPath)
}

function projectName(projectPath: string): string {
  return projectPath.split(/[\\/]/).filter(Boolean).at(-1) || projectPath
}

function base64Url(value: string): string {
  const bytes = new TextEncoder().encode(value)
  let binary = ''
  for (const byte of bytes) {
    binary += String.fromCharCode(byte)
  }
  return btoa(binary)
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/g, '')
}

class ServerEventHub {
  private readonly listeners = new Set<(event: ServerEvent) => void>()
  private source: EventSource | null = null
  private workdir: string | undefined

  subscribe(handler: (event: ServerEvent) => void): () => void {
    this.listeners.add(handler)
    void this.setWorkdir(activeContext?.cwd)
    return () => {
      this.listeners.delete(handler)
      if (this.listeners.size === 0) {
        this.close()
      }
    }
  }

  async setWorkdir(workdir: string | undefined): Promise<void> {
    if (this.workdir === workdir && this.source) return
    this.close()
    this.workdir = workdir
    if (!workdir || this.listeners.size === 0) return

    const serverUrl = await getAgentServerUrl()
    if (this.workdir !== workdir || this.listeners.size === 0) return
    const url = new URL(`${serverUrl}/wuu/events`)
    url.searchParams.set('workdir', workdir)
    this.source = new EventSource(url.toString())
    this.source.onmessage = (event) => this.handleEvent(event)
    this.source.addEventListener('notification', (event) =>
      this.handleEvent(event),
    )
    this.source.addEventListener('server-request', (event) =>
      this.handleEvent(event),
    )
    this.source.addEventListener('server-error', (event) =>
      this.handleEvent(event),
    )
    this.source.addEventListener('server-exit', (event) =>
      this.handleEvent(event),
    )
  }

  private handleEvent(event: MessageEvent<string>): void {
    if (!event.data) return
    let bridgeEvent: WuuBridgeEvent
    try {
      bridgeEvent = JSON.parse(event.data) as WuuBridgeEvent
    } catch {
      return
    }

    const serverEvent = this.toServerEvent(bridgeEvent)
    if (!serverEvent) return
    for (const listener of this.listeners) {
      listener(serverEvent)
    }
  }

  private toServerEvent(event: WuuBridgeEvent): ServerEvent | null {
    if (event.type === 'notification') {
      return { kind: 'notification', message: event.message }
    }
    if (event.type === 'server-request') {
      if (!event.message.id || !event.message.method) return null
      return { kind: 'server-request', message: event.message }
    }
    if (event.type === 'server-error') {
      return { kind: 'server-error', message: event.message }
    }
    if (event.type === 'server-exit') {
      return { kind: 'server-exit', code: event.code }
    }
    return null
  }

  private close(): void {
    this.source?.close()
    this.source = null
  }
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

const serverEventHub = new ServerEventHub()
const terminalEventHub = new TerminalEventHub()
