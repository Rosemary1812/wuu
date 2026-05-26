import { type ChildProcessWithoutNullStreams, spawn } from 'node:child_process'
import { existsSync, readFileSync, statSync } from 'node:fs'
import { extname, resolve } from 'node:path'
import { Hono } from 'hono'
import { stream } from 'hono/streaming'
import { logger } from '../../lib/logger'
import {
  addWuuTerminalListener,
  handleWuuDesktopRpc,
} from '../services/wuu/desktop-runtime'
import type { Env } from '../types'

type JsonValue =
  | null
  | boolean
  | number
  | string
  | JsonValue[]
  | { [key: string]: JsonValue }

type WuuRequestBody = {
  workdir?: string
  provider?: string
  model?: string
  noTools?: boolean
}

type WuuRpcBody = WuuRequestBody & {
  method?: string
  params?: JsonValue
}

type WuuDesktopRpcBody = WuuRequestBody & {
  method?: string
  params?: unknown
}

type WuuClientResponseBody = WuuRequestBody & {
  id?: string
  result?: JsonValue
  message?: string
}

type WuuMessage = {
  id?: unknown
  method?: string
  params?: unknown
  result?: unknown
  error?: {
    code?: string
    message?: string
  }
}

type WuuBridgeEvent =
  | {
      type: 'notification'
      message: WuuMessage
    }
  | {
      type: 'server-request'
      message: WuuMessage
    }
  | {
      type: 'server-error'
      message: string
    }
  | {
      type: 'server-exit'
      code: number | null
    }
  | {
      type: 'server-started'
      workdir: string
    }

type PendingRequest = {
  resolve: (value: unknown) => void
  reject: (error: Error) => void
  timer: ReturnType<typeof setTimeout>
}

type WuuCommand = {
  command: string
  args: string[]
  cwd: string
}

type WuuRoutesOptions = {
  browserBridgeUrl: string
}

const REQUEST_TIMEOUT_MS = 45_000
const HEARTBEAT_MS = 15_000
const RENDERABLE_IMAGE_CONTENT_TYPES = new Map([
  ['.apng', 'image/apng'],
  ['.avif', 'image/avif'],
  ['.gif', 'image/gif'],
  ['.jpeg', 'image/jpeg'],
  ['.jpg', 'image/jpeg'],
  ['.png', 'image/png'],
  ['.svg', 'image/svg+xml'],
  ['.webp', 'image/webp'],
])

function requestIDKey(id: unknown): string {
  return JSON.stringify(id)
}

function parseBody<T>(value: unknown): T {
  if (!value || typeof value !== 'object') {
    return {} as T
  }
  return value as T
}

function resolveWorkdir(input?: string): string {
  const trimmed = input?.trim()
  if (trimmed) {
    return resolve(trimmed)
  }
  return resolve(process.env.WUU_DEFAULT_WORKDIR || process.env.HOME || '.')
}

function renderableImageContentType(filePath: string): string | undefined {
  try {
    if (!statSync(filePath).isFile()) return undefined
  } catch {
    return undefined
  }
  return RENDERABLE_IMAGE_CONTENT_TYPES.get(extname(filePath).toLowerCase())
}

function filePathFromRenderableToken(token: string): string | undefined {
  try {
    return Buffer.from(token, 'base64url').toString('utf8')
  } catch {
    return undefined
  }
}

function resolveWuuCommand(): WuuCommand {
  const explicit = process.env.WUU_BIN?.trim()
  if (explicit) {
    return { command: explicit, args: [], cwd: process.cwd() }
  }

  const sourceRoot = process.env.WUU_SOURCE_ROOT?.trim()
  if (sourceRoot && existsSync(resolve(sourceRoot, 'cmd', 'wuu'))) {
    const devBinary = resolve(sourceRoot, 'browser', '.cache', 'wuu-dev')
    if (existsSync(devBinary)) {
      return { command: devBinary, args: [], cwd: sourceRoot }
    }
  }

  const home = process.env.HOME?.trim()
  const localCandidates = [
    home ? resolve(home, '.local/bin/wuu') : '',
    '/opt/homebrew/bin/wuu',
    '/usr/local/bin/wuu',
  ].filter(Boolean)
  for (const candidate of localCandidates) {
    if (existsSync(candidate)) {
      return { command: candidate, args: [], cwd: process.cwd() }
    }
  }

  return { command: 'wuu', args: [], cwd: process.cwd() }
}

function sessionKey(input: WuuRequestBody): string {
  return JSON.stringify({
    workdir: resolveWorkdir(input.workdir),
    provider: input.provider?.trim() || '',
    model: input.model?.trim() || '',
    noTools: Boolean(input.noTools),
  })
}

class WuuStdioSession {
  private child: ChildProcessWithoutNullStreams | null = null
  private stdoutBuffer = ''
  private nextRequestID = 1
  private pending = new Map<string, PendingRequest>()
  private listeners = new Set<(event: WuuBridgeEvent) => void>()
  private stopping = false

  constructor(
    private readonly options: Required<WuuRequestBody>,
    private readonly browserBridgeUrl: string,
  ) {}

  get workdir(): string {
    return this.options.workdir
  }

  isRunning(): boolean {
    return Boolean(this.child && !this.child.killed)
  }

  request(method: string, params?: JsonValue): Promise<unknown> {
    this.ensureStarted()
    const id = `browseros-${this.nextRequestID++}`
    const payload = { id, method, params }

    return new Promise((resolveRequest, rejectRequest) => {
      const timer = setTimeout(() => {
        this.pending.delete(requestIDKey(id))
        rejectRequest(new Error(`wuu request timed out: ${method}`))
      }, REQUEST_TIMEOUT_MS)

      this.pending.set(requestIDKey(id), {
        resolve: resolveRequest,
        reject: rejectRequest,
        timer,
      })

      this.write(payload)
    })
  }

  respond(id: string, result?: JsonValue): void {
    this.ensureStarted()
    this.write({ id, result })
  }

  reject(id: string, message: string): void {
    this.ensureStarted()
    this.write({
      id,
      error: {
        code: 'error',
        message,
      },
    })
  }

  addListener(listener: (event: WuuBridgeEvent) => void): () => void {
    this.ensureStarted()
    this.listeners.add(listener)
    listener({
      type: 'server-started',
      workdir: this.workdir,
    })
    return () => {
      this.listeners.delete(listener)
    }
  }

  stop(): void {
    this.stopping = true
    if (!this.child || this.child.killed) return
    try {
      this.write({ id: 'shutdown', method: 'shutdown' })
    } catch {
      this.child.kill()
    }
  }

  private ensureStarted(): void {
    if (this.child && !this.child.killed) return

    const command = resolveWuuCommand()
    const args = [
      ...command.args,
      'app-server',
      '--workdir',
      this.options.workdir,
    ]

    if (this.options.provider) {
      args.push('--provider', this.options.provider)
    }
    if (this.options.model) {
      args.push('--model', this.options.model)
    }
    if (this.options.noTools) {
      args.push('--no-tools')
    }

    this.stopping = false
    this.child = spawn(command.command, args, {
      cwd: command.cwd,
      env: {
        ...process.env,
        WUU_BROWSER_BRIDGE_URL:
          process.env.WUU_BROWSER_BRIDGE_URL || this.browserBridgeUrl,
      },
      stdio: ['pipe', 'pipe', 'pipe'],
    })

    this.child.stdout.setEncoding('utf8')
    this.child.stdout.on('data', (chunk: string) => this.readStdout(chunk))
    this.child.stderr.setEncoding('utf8')
    this.child.stderr.on('data', (chunk: string) => {
      const message = chunk.trim()
      if (message) {
        this.emit({ type: 'server-error', message })
      }
    })
    this.child.on('error', (error) => {
      this.rejectPending(error)
      this.emit({ type: 'server-error', message: error.message })
    })
    this.child.on('exit', (code) => {
      this.rejectPending(new Error('wuu app-server exited'))
      this.child = null
      if (!this.stopping) {
        this.emit({ type: 'server-exit', code })
      }
    })

    logger.info('Started Wuu app-server bridge', {
      workdir: this.options.workdir,
      command: command.command,
    })
  }

  private write(payload: unknown): void {
    if (!this.child) {
      throw new Error('wuu app-server is not running')
    }
    this.child.stdin.write(`${JSON.stringify(payload)}\n`)
  }

  private readStdout(chunk: string): void {
    this.stdoutBuffer += chunk
    for (;;) {
      const index = this.stdoutBuffer.indexOf('\n')
      if (index < 0) return
      const line = this.stdoutBuffer.slice(0, index).trim()
      this.stdoutBuffer = this.stdoutBuffer.slice(index + 1)
      if (line) {
        this.handleLine(line)
      }
    }
  }

  private handleLine(line: string): void {
    let message: WuuMessage
    try {
      message = JSON.parse(line)
    } catch {
      this.emit({
        type: 'server-error',
        message: `Invalid Wuu app-server JSON: ${line}`,
      })
      return
    }

    if (message.method && message.id !== undefined) {
      this.emit({ type: 'server-request', message })
      return
    }

    if (message.method) {
      this.emit({ type: 'notification', message })
      return
    }

    const key = requestIDKey(message.id)
    const pending = this.pending.get(key)
    if (!pending) return

    this.pending.delete(key)
    clearTimeout(pending.timer)

    if (message.error) {
      pending.reject(new Error(message.error.message || 'wuu app-server error'))
      return
    }

    pending.resolve(message.result)
  }

  private rejectPending(error: Error): void {
    for (const pending of this.pending.values()) {
      clearTimeout(pending.timer)
      pending.reject(error)
    }
    this.pending.clear()
  }

  private emit(event: WuuBridgeEvent): void {
    for (const listener of this.listeners) {
      listener(event)
    }
  }
}

class WuuBridgeRegistry {
  private sessions = new Map<string, WuuStdioSession>()

  get(input: WuuRequestBody, options: WuuRoutesOptions): WuuStdioSession {
    const normalized: Required<WuuRequestBody> = {
      workdir: resolveWorkdir(input.workdir),
      provider: input.provider?.trim() || '',
      model: input.model?.trim() || '',
      noTools: Boolean(input.noTools),
    }

    const key = sessionKey(normalized)
    let session = this.sessions.get(key)
    if (!session) {
      session = new WuuStdioSession(normalized, options.browserBridgeUrl)
      this.sessions.set(key, session)
    }
    return session
  }

  status(input: WuuRequestBody): { workdir: string; running: boolean } {
    const normalized = {
      ...input,
      workdir: resolveWorkdir(input.workdir),
    }
    return {
      workdir: normalized.workdir,
      running: this.sessions.get(sessionKey(normalized))?.isRunning() ?? false,
    }
  }
}

const registry = new WuuBridgeRegistry()

export function createWuuRoutes(options: WuuRoutesOptions) {
  return new Hono<Env>()
    .get('/status', (c) => {
      const status = registry.status({
        workdir: c.req.query('workdir'),
        provider: c.req.query('provider'),
        model: c.req.query('model'),
        noTools: c.req.query('noTools') === 'true',
      })
      return c.json(status)
    })
    .post('/rpc', async (c) => {
      const body = parseBody<WuuRpcBody>(await c.req.json().catch(() => ({})))
      if (!body.method?.trim()) {
        return c.json({ error: 'method is required' }, 400)
      }

      try {
        const result = await registry
          .get(body, options)
          .request(body.method.trim(), body.params)
        return c.json({ result })
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error)
        return c.json({ error: message }, 500)
      }
    })
    .post('/desktop', async (c) => {
      const body = parseBody<WuuDesktopRpcBody>(
        await c.req.json().catch(() => ({})),
      )
      if (!body.method?.trim()) {
        return c.json({ error: 'method is required' }, 400)
      }

      try {
        const result = await handleWuuDesktopRpc(
          resolveWorkdir(body.workdir),
          body.method.trim(),
          body.params,
        )
        return c.json({ result })
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error)
        return c.json({ error: message }, 500)
      }
    })
    .get('/file/local/:token', (c) => {
      const filePath = filePathFromRenderableToken(c.req.param('token'))
      if (!filePath) {
        return c.text('Not found', 404)
      }
      const contentType = renderableImageContentType(filePath)
      if (!contentType) {
        return c.text('Not found', 404)
      }

      return new Response(readFileSync(filePath), {
        headers: {
          'Cache-Control': 'no-cache',
          'Content-Type': contentType,
        },
      })
    })
    .post('/respond', async (c) => {
      const body = parseBody<WuuClientResponseBody>(
        await c.req.json().catch(() => ({})),
      )
      if (!body.id?.trim()) {
        return c.json({ error: 'id is required' }, 400)
      }

      registry.get(body, options).respond(body.id.trim(), body.result)
      return c.json({ ok: true })
    })
    .post('/reject', async (c) => {
      const body = parseBody<WuuClientResponseBody>(
        await c.req.json().catch(() => ({})),
      )
      if (!body.id?.trim()) {
        return c.json({ error: 'id is required' }, 400)
      }

      registry
        .get(body, options)
        .reject(body.id.trim(), body.message?.trim() || 'Rejected')
      return c.json({ ok: true })
    })
    .get('/events', (c) => {
      const session = registry.get(
        {
          workdir: c.req.query('workdir'),
          provider: c.req.query('provider'),
          model: c.req.query('model'),
          noTools: c.req.query('noTools') === 'true',
        },
        options,
      )

      c.header('Content-Type', 'text/event-stream')
      c.header('Cache-Control', 'no-cache')
      c.header('Connection', 'keep-alive')

      return stream(c, async (s) => {
        const encoder = new TextEncoder()

        const writeEvent = (event: WuuBridgeEvent) => {
          s.write(
            encoder.encode(
              `event: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`,
            ),
          ).catch(() => {})
        }

        const unsubscribe = session.addListener(writeEvent)
        const heartbeat = setInterval(() => {
          s.write(
            encoder.encode(
              `event: heartbeat\ndata: ${JSON.stringify({ ts: Date.now() })}\n\n`,
            ),
          ).catch(() => {})
        }, HEARTBEAT_MS)

        try {
          await new Promise<void>((resolveAbort) => {
            s.onAbort(() => resolveAbort())
          })
        } finally {
          unsubscribe()
          clearInterval(heartbeat)
        }
      })
    })
    .get('/terminal/events', (c) => {
      c.header('Content-Type', 'text/event-stream')
      c.header('Cache-Control', 'no-cache')
      c.header('Connection', 'keep-alive')

      return stream(c, async (s) => {
        const encoder = new TextEncoder()
        const unsubscribe = addWuuTerminalListener((event) => {
          s.write(
            encoder.encode(
              `event: terminal\ndata: ${JSON.stringify(event)}\n\n`,
            ),
          ).catch(() => {})
        })
        const heartbeat = setInterval(() => {
          s.write(
            encoder.encode(
              `event: heartbeat\ndata: ${JSON.stringify({ ts: Date.now() })}\n\n`,
            ),
          ).catch(() => {})
        }, HEARTBEAT_MS)

        try {
          await new Promise<void>((resolveAbort) => {
            s.onAbort(() => resolveAbort())
          })
        } finally {
          unsubscribe()
          clearInterval(heartbeat)
        }
      })
    })
}
