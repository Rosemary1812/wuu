import { describe, expect, it } from 'bun:test'
import {
  ServerEventHub,
  type ServerEventSource,
  type WuuBridgeEvent,
} from './browseros-wuu-event-hub'

type Deferred<T> = {
  promise: Promise<T>
  resolve: (value: T) => void
}

function deferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((nextResolve) => {
    resolve = nextResolve
  })
  return { promise, resolve }
}

class FakeEventSource implements ServerEventSource {
  onmessage: ((event: MessageEvent<string>) => void) | null = null
  readonly listeners = new Map<string, Array<(event: MessageEvent<string>) => void>>()
  closed = false

  constructor(readonly url: string) {}

  addEventListener(type: string, listener: (event: MessageEvent<string>) => void): void {
    const listeners = this.listeners.get(type) ?? []
    listeners.push(listener)
    this.listeners.set(type, listeners)
  }

  close(): void {
    this.closed = true
  }

  emit(type: string, event: WuuBridgeEvent): void {
    const message = { data: JSON.stringify(event) } as MessageEvent<string>
    for (const listener of this.listeners.get(type) ?? []) {
      listener(message)
    }
  }
}

describe('ServerEventHub', () => {
  it('keeps a single EventSource when same-workdir connects overlap', async () => {
    const serverUrl = deferred<string>()
    const sources: FakeEventSource[] = []
    const received: unknown[] = []
    const hub = new ServerEventHub({
      getWorkdir: () => '/repo',
      getServerUrl: () => serverUrl.promise,
      createEventSource: (url) => {
        const source = new FakeEventSource(url)
        sources.push(source)
        return source
      },
    })

    const unsubscribe = hub.subscribe((event) => received.push(event))
    const secondConnect = hub.setWorkdir('/repo')
    serverUrl.resolve('http://127.0.0.1:7777')

    await secondConnect
    await Promise.resolve()

    expect(sources).toHaveLength(1)
    expect(sources[0].url).toBe('http://127.0.0.1:7777/wuu/events?workdir=%2Frepo')

    sources[0].emit('notification', {
      type: 'notification',
      message: {
        method: 'item/agentMessage/delta',
        params: {
          thread_id: 'thread',
          turn_id: 'turn',
          item_id: 'item',
          delta: 'hi',
        },
      },
    })

    expect(received).toEqual([
      {
        kind: 'notification',
        message: {
          method: 'item/agentMessage/delta',
          params: {
            thread_id: 'thread',
            turn_id: 'turn',
            item_id: 'item',
            delta: 'hi',
          },
        },
      },
    ])

    unsubscribe()
    expect(sources[0].closed).toBe(true)
  })
})
