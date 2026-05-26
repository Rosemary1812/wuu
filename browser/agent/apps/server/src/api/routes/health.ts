/**
 * @license
 * Copyright 2025 BrowserOS
 * SPDX-License-Identifier: AGPL-3.0-or-later
 */

import { WUU_SERVER_API_VERSION } from '@browseros/shared/constants/runtime-contract'
import { Hono } from 'hono'
import type { Browser } from '../../browser/browser'

interface HealthDeps {
  browser?: Browser
  version?: string
  browserosVersion?: string
  chromiumVersion?: string
}

export function createHealthRoute(deps: HealthDeps = {}) {
  return new Hono().get('/', (c) => {
    const cdpConnected = deps.browser?.isCdpConnected()
    return c.json({
      status: 'ok',
      ...(cdpConnected === undefined ? {} : { cdpConnected }),
      contract: {
        serverApiVersion: WUU_SERVER_API_VERSION,
      },
      versions: {
        server: deps.version ?? null,
        browseros: deps.browserosVersion ?? null,
        chromium: deps.chromiumVersion ?? null,
      },
      runtime: {
        localOverrides: {
          wuuBin: Boolean(process.env.WUU_BIN),
          wuuSourceRoot: Boolean(process.env.WUU_SOURCE_ROOT),
          serverResources: Boolean(process.env.WUU_BROWSER_SERVER_RESOURCES),
        },
        vmAgentsEnabled: process.env.WUU_ENABLE_VM_AGENTS === '1',
      },
    })
  })
}
