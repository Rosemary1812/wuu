import type {
  LoadingFailedEvent,
  RequestWillBeSentEvent,
  ResponseReceivedEvent,
} from '@browseros/cdp-protocol/domains/network'
import type { CdpBackend } from './backends/types'

export interface NetworkEntry {
  requestId: string
  url: string
  method?: string
  type?: string
  status?: number
  statusText?: string
  mimeType?: string
  failed: boolean
  errorText?: string
  blockedReason?: string
  timestamp: number
}

export interface GetNetworkEntriesOptions {
  search?: string
  failedOnly?: boolean
  limit?: number
  clear?: boolean
}

export interface GetNetworkEntriesResult {
  entries: NetworkEntry[]
  totalCount: number
}

const DEFAULT_LIMIT = 50
const MAX_LIMIT = 200
const MAX_ENTRIES = 500

export class NetworkCollector {
  private readonly buffers = new Map<number, NetworkEntry[]>()
  private readonly requestIndex = new Map<number, Map<string, NetworkEntry>>()
  private readonly sessionToPage = new Map<string, number>()
  private readonly pageToSession = new Map<number, string>()

  constructor(cdp: CdpBackend) {
    cdp.onSessionEvent('Network.requestWillBeSent', (params, sessionId) => {
      const pageId = this.sessionToPage.get(sessionId)
      if (pageId === undefined) return
      this.handleRequest(pageId, params as RequestWillBeSentEvent)
    })

    cdp.onSessionEvent('Network.responseReceived', (params, sessionId) => {
      const pageId = this.sessionToPage.get(sessionId)
      if (pageId === undefined) return
      this.handleResponse(pageId, params as ResponseReceivedEvent)
    })

    cdp.onSessionEvent('Network.loadingFailed', (params, sessionId) => {
      const pageId = this.sessionToPage.get(sessionId)
      if (pageId === undefined) return
      this.handleFailure(pageId, params as LoadingFailedEvent)
    })

    cdp.onSessionEvent('Page.frameNavigated', (params, sessionId) => {
      const pageId = this.sessionToPage.get(sessionId)
      if (pageId === undefined) return
      const frame = (params as { frame: { parentId?: string } }).frame
      if (!frame.parentId) this.clear(pageId)
    })
  }

  attach(pageId: number, sessionId: string): void {
    if (!this.buffers.has(pageId)) this.buffers.set(pageId, [])
    if (!this.requestIndex.has(pageId)) this.requestIndex.set(pageId, new Map())

    const oldSession = this.pageToSession.get(pageId)
    if (oldSession && oldSession !== sessionId) {
      this.sessionToPage.delete(oldSession)
    }
    this.sessionToPage.set(sessionId, pageId)
    this.pageToSession.set(pageId, sessionId)
  }

  detach(pageId: number): void {
    const sessionId = this.pageToSession.get(pageId)
    if (sessionId) this.sessionToPage.delete(sessionId)
    this.pageToSession.delete(pageId)
    this.buffers.delete(pageId)
    this.requestIndex.delete(pageId)
  }

  getEntries(
    pageId: number,
    opts?: GetNetworkEntriesOptions,
  ): GetNetworkEntriesResult {
    const buffer = this.buffers.get(pageId) ?? []
    let entries = buffer

    if (opts?.failedOnly) {
      entries = entries.filter(
        (entry) =>
          entry.failed ||
          (typeof entry.status === 'number' && entry.status >= 400),
      )
    }

    if (opts?.search) {
      const term = opts.search.toLowerCase()
      entries = entries.filter((entry) =>
        [entry.url, entry.method, entry.type, entry.errorText, entry.statusText]
          .filter(Boolean)
          .some((value) => String(value).toLowerCase().includes(term)),
      )
    }

    const totalCount = entries.length
    const limit = Math.min(opts?.limit ?? DEFAULT_LIMIT, MAX_LIMIT)
    const result = entries.slice(-limit)

    if (opts?.clear) this.clear(pageId)

    return { entries: result, totalCount }
  }

  private clear(pageId: number): void {
    this.buffers.set(pageId, [])
    this.requestIndex.set(pageId, new Map())
  }

  private handleRequest(pageId: number, event: RequestWillBeSentEvent): void {
    const entry: NetworkEntry = {
      requestId: String(event.requestId),
      url: event.request.url,
      method: event.request.method,
      type: event.type,
      failed: false,
      timestamp: event.wallTime * 1000,
    }
    this.addOrReplaceEntry(pageId, entry)
  }

  private handleResponse(pageId: number, event: ResponseReceivedEvent): void {
    const entry = this.getOrCreateEntry(pageId, String(event.requestId), {
      url: event.response.url,
      type: event.type,
      timestamp: event.timestamp,
    })
    entry.url = event.response.url
    entry.type = event.type
    entry.status = event.response.status
    entry.statusText = event.response.statusText
    entry.mimeType = event.response.mimeType
    entry.failed = event.response.status >= 400
    entry.timestamp = event.timestamp
  }

  private handleFailure(pageId: number, event: LoadingFailedEvent): void {
    const entry = this.getOrCreateEntry(pageId, String(event.requestId), {
      type: event.type,
      timestamp: event.timestamp,
    })
    entry.type = event.type
    entry.failed = true
    entry.errorText = event.errorText
    entry.blockedReason = event.blockedReason
    entry.timestamp = event.timestamp
  }

  private getOrCreateEntry(
    pageId: number,
    requestId: string,
    fallback: Partial<NetworkEntry>,
  ): NetworkEntry {
    let index = this.requestIndex.get(pageId)
    if (!index) {
      index = new Map()
      this.requestIndex.set(pageId, index)
    }

    const existing = index.get(requestId)
    if (existing) return existing

    const entry: NetworkEntry = {
      requestId,
      url: fallback.url ?? '',
      type: fallback.type,
      failed: false,
      timestamp: fallback.timestamp ?? Date.now(),
    }
    this.addOrReplaceEntry(pageId, entry)
    return entry
  }

  private addOrReplaceEntry(pageId: number, entry: NetworkEntry): void {
    let buffer = this.buffers.get(pageId)
    if (!buffer) {
      buffer = []
      this.buffers.set(pageId, buffer)
    }

    let index = this.requestIndex.get(pageId)
    if (!index) {
      index = new Map()
      this.requestIndex.set(pageId, index)
    }

    const existing = index.get(entry.requestId)
    if (existing) {
      Object.assign(existing, entry)
      return
    }

    if (buffer.length >= MAX_ENTRIES) {
      const removed = buffer.shift()
      if (removed) index.delete(removed.requestId)
    }

    buffer.push(entry)
    index.set(entry.requestId, entry)
  }
}
