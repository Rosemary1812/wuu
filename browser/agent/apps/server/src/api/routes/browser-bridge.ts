/**
 * @license
 * Copyright 2025 BrowserOS
 * SPDX-License-Identifier: AGPL-3.0-or-later
 */

import { type Context, Hono } from 'hono'
import type { Browser } from '../../browser/browser'
import type { CdpTarget } from '../../browser/backends/types'

type BrowserBridgeDeps = {
  browser: Pick<Browser, 'isCdpConnected' | 'listTargets' | 'getActiveTarget'>
}

type BrowserBridgeTab = {
  pageId: number
  targetId: string
  tabId?: number
  url: string
  title: string
  isActive: boolean
  windowId?: number
}

const EXCLUDED_TARGET_URL_PREFIXES = [
  'chrome-untrusted://',
  'devtools://',
]

function isBridgeTarget(target: CdpTarget): boolean {
  return (
    target.type === 'page' &&
    !EXCLUDED_TARGET_URL_PREFIXES.some((prefix) =>
      target.url.startsWith(prefix),
    )
  )
}

function toBridgeTab(
  target: CdpTarget,
  pageId: number,
  activeTargetId: string | null,
): BrowserBridgeTab {
  return {
    pageId,
    targetId: target.id,
    ...(target.tabId !== undefined ? { tabId: target.tabId } : {}),
    url: target.url,
    title: target.title,
    isActive: target.id === activeTargetId,
    ...(target.windowId !== undefined ? { windowId: target.windowId } : {}),
  }
}

function cdpUnavailable(c: Context) {
  return c.json({ error: 'Browser CDP is not connected' }, 503)
}

export function createBrowserBridgeRoutes({ browser }: BrowserBridgeDeps) {
  const pageIdsByTargetId = new Map<string, number>()
  let nextPageId = 1

  function getPageId(targetId: string): number {
    let pageId = pageIdsByTargetId.get(targetId)
    if (pageId === undefined) {
      pageId = nextPageId++
      pageIdsByTargetId.set(targetId, pageId)
    }
    return pageId
  }

  function forgetMissingTargets(targets: CdpTarget[]) {
    const currentTargetIds = new Set(targets.map((target) => target.id))
    for (const targetId of pageIdsByTargetId.keys()) {
      if (!currentTargetIds.has(targetId)) pageIdsByTargetId.delete(targetId)
    }
  }

  return new Hono()
    .get('/tabs', async (c) => {
      if (!browser.isCdpConnected()) return cdpUnavailable(c)

      const [targets, activeTarget] = await Promise.all([
        browser.listTargets(),
        browser.getActiveTarget(),
      ])
      const pageTargets = targets.filter(isBridgeTarget)
      forgetMissingTargets(pageTargets)

      const activeTargetId = activeTarget?.id ?? null
      const tabs = pageTargets.map((target) =>
        toBridgeTab(target, getPageId(target.id), activeTargetId),
      )
      const activeTab = tabs.find((tab) => tab.isActive) ?? null

      return c.json({ tabs, activeTab })
    })
    .get('/active-tab', async (c) => {
      if (!browser.isCdpConnected()) return cdpUnavailable(c)

      const activeTarget = await browser.getActiveTarget()
      if (!activeTarget || !isBridgeTarget(activeTarget)) {
        return c.json({ activeTab: null })
      }

      return c.json({
        activeTab: toBridgeTab(
          activeTarget,
          getPageId(activeTarget.id),
          activeTarget.id,
        ),
      })
    })
}
