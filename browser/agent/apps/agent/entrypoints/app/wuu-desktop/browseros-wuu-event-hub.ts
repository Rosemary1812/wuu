import type {
  AppServerNotification,
  AppServerRequest,
  ServerEvent,
} from '@browseros/workbench-ui/shared/protocol'

export type WuuBridgeEvent =
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

export type ServerEventSource = {
  onmessage: ((event: MessageEvent<string>) => void) | null
  addEventListener: (
    type: string,
    listener: (event: MessageEvent<string>) => void,
  ) => void
  close: () => void
}

type ServerEventHubOptions = {
  getServerUrl: () => Promise<string>
  getWorkdir: () => string | undefined
  createEventSource?: (url: string) => ServerEventSource
}

export class ServerEventHub {
  private readonly listeners = new Set<(event: ServerEvent) => void>()
  private readonly getServerUrl: () => Promise<string>
  private readonly getWorkdir: () => string | undefined
  private readonly createEventSource: (url: string) => ServerEventSource
  private source: ServerEventSource | null = null
  private workdir: string | undefined
  private connectVersion = 0

  constructor(options: ServerEventHubOptions) {
    this.getServerUrl = options.getServerUrl
    this.getWorkdir = options.getWorkdir
    this.createEventSource =
      options.createEventSource ?? ((url) => new EventSource(url))
  }

  subscribe(handler: (event: ServerEvent) => void): () => void {
    this.listeners.add(handler)
    void this.setWorkdir(this.getWorkdir())
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
    const version = ++this.connectVersion
    if (!workdir || this.listeners.size === 0) return

    const serverUrl = await this.getServerUrl()
    if (
      this.connectVersion !== version ||
      this.workdir !== workdir ||
      this.listeners.size === 0 ||
      this.source
    ) {
      return
    }

    const url = new URL(`${serverUrl}/wuu/events`)
    url.searchParams.set('workdir', workdir)
    const source = this.createEventSource(url.toString())
    source.onmessage = (event) => this.handleEvent(event)
    source.addEventListener('notification', (event) =>
      this.handleEvent(event),
    )
    source.addEventListener('server-request', (event) =>
      this.handleEvent(event),
    )
    source.addEventListener('server-error', (event) =>
      this.handleEvent(event),
    )
    source.addEventListener('server-exit', (event) =>
      this.handleEvent(event),
    )

    if (
      this.connectVersion !== version ||
      this.workdir !== workdir ||
      this.listeners.size === 0 ||
      this.source
    ) {
      source.close()
      return
    }
    this.source = source
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
    this.connectVersion += 1
    this.source?.close()
    this.source = null
  }
}
