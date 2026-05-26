import { describe, expect, it } from 'bun:test'
import { WUU_SERVER_API_VERSION } from '@browseros/shared/constants/runtime-contract'
import { createHealthRoute } from '../../src/api/routes/health'
import type { Browser } from '../../src/browser/browser'

describe('health route', () => {
  it('exposes version and runtime contract metadata without local paths', async () => {
    const browser = {
      isCdpConnected: () => true,
    } as unknown as Browser

    const app = createHealthRoute({
      browser,
      version: '0.0.94',
      browserosVersion: '0.43.0.0',
      chromiumVersion: '142.0.0.0',
    })

    const res = await app.request('http://localhost/')
    const body = await res.json()

    expect(body).toMatchObject({
      status: 'ok',
      cdpConnected: true,
      contract: {
        serverApiVersion: WUU_SERVER_API_VERSION,
      },
      versions: {
        server: '0.0.94',
        browseros: '0.43.0.0',
        chromium: '142.0.0.0',
      },
    })
    expect(JSON.stringify(body)).not.toContain('/Users/')
  })
})
