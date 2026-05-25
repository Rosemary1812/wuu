/**
 * @license
 * Copyright 2025 BrowserOS
 * SPDX-License-Identifier: AGPL-3.0-or-later
 */

import { type Context, Hono } from 'hono'
import type { Browser } from '../../browser/browser'
import type { CdpTarget } from '../../browser/backends/types'
import type { ConsoleLevel } from '../../browser/console-collector'

type BrowserBridgeDeps = {
  browser: Pick<
    Browser,
    | 'isCdpConnected'
    | 'listTargets'
    | 'getActiveTarget'
    | 'createTargetTab'
    | 'navigateTarget'
    | 'activateTargetTab'
    | 'closeTargetTab'
    | 'reloadTarget'
    | 'goBackTarget'
    | 'goForwardTarget'
    | 'screenshotTarget'
    | 'snapshotTarget'
    | 'enhancedSnapshotTarget'
    | 'contentTarget'
    | 'domTarget'
    | 'evaluateTarget'
    | 'getConsoleLogsTarget'
    | 'getNetworkEntriesTarget'
    | 'clickTargetAt'
    | 'typeTargetAt'
    | 'scrollTarget'
  >
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

function readUrl(value: unknown): string | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null
  const url = (value as { url?: unknown }).url
  if (typeof url !== 'string' || !url.trim()) return null

  try {
    new URL(url)
    return url
  } catch {
    return null
  }
}

async function readJson(c: Context): Promise<unknown> {
  try {
    return await c.req.json()
  } catch {
    return null
  }
}

function readRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return {}
  return value as Record<string, unknown>
}

function readFiniteNumber(
  record: Record<string, unknown>,
  key: string,
): number | null {
  const value = record[key]
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

function readPoint(record: Record<string, unknown>) {
  const x = readFiniteNumber(record, 'x')
  const y = readFiniteNumber(record, 'y')
  return x === null || y === null ? null : { x, y }
}

function readPositiveAmount(record: Record<string, unknown>): number {
  const value = readFiniteNumber(record, 'amount')
  if (value === null) return 3
  return Math.max(1, Math.min(20, Math.round(value)))
}

function readDirection(value: unknown): string | null {
  if (
    value === 'up' ||
    value === 'down' ||
    value === 'left' ||
    value === 'right'
  ) {
    return value
  }
  return null
}

function readConsoleLevel(value: unknown): ConsoleLevel | null {
  if (
    value === 'error' ||
    value === 'warning' ||
    value === 'info' ||
    value === 'debug'
  ) {
    return value
  }
  return null
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

  async function findBridgeTarget(targetId: string): Promise<CdpTarget | null> {
    const pageTargets = (await browser.listTargets()).filter(isBridgeTarget)
    forgetMissingTargets(pageTargets)
    return pageTargets.find((target) => target.id === targetId) ?? null
  }

  return new Hono()
    .post('/tabs', async (c) => {
      if (!browser.isCdpConnected()) return cdpUnavailable(c)

      const body = await readJson(c)
      const url = readUrl(body)
      if (!url) return c.json({ error: 'A valid absolute URL is required' }, 400)

      const record = body as { background?: unknown; windowId?: unknown }
      const target = await browser.createTargetTab(url, {
        background:
          typeof record.background === 'boolean' ? record.background : true,
        ...(typeof record.windowId === 'number'
          ? { windowId: record.windowId }
          : {}),
      })

      const activeTarget = await browser.getActiveTarget()
      return c.json(
        {
          tab: toBridgeTab(
            target,
            getPageId(target.id),
            activeTarget?.id ?? null,
          ),
        },
        201,
      )
    })
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
    .post('/tabs/:targetId/navigate', async (c) => {
      if (!browser.isCdpConnected()) return cdpUnavailable(c)

      const targetId = c.req.param('targetId')
      const target = await findBridgeTarget(targetId)
      if (!target) return c.json({ error: 'Tab target not found' }, 404)

      const url = readUrl(await readJson(c))
      if (!url) return c.json({ error: 'A valid absolute URL is required' }, 400)

      await browser.navigateTarget(target.id, url)

      const updated = (await findBridgeTarget(target.id)) ?? {
        ...target,
        url,
      }
      const activeTarget = await browser.getActiveTarget()

      return c.json({
        tab: toBridgeTab(
          updated,
          getPageId(updated.id),
          activeTarget?.id ?? null,
        ),
      })
    })
    .post('/tabs/:targetId/activate', async (c) => {
      if (!browser.isCdpConnected()) return cdpUnavailable(c)

      const targetId = c.req.param('targetId')
      const target = await findBridgeTarget(targetId)
      if (!target) return c.json({ error: 'Tab target not found' }, 404)

      const activated = await browser.activateTargetTab(target.id)
      return c.json({
        tab: toBridgeTab(activated, getPageId(activated.id), activated.id),
      })
    })
    .post('/tabs/:targetId/reload', async (c) => {
      if (!browser.isCdpConnected()) return cdpUnavailable(c)

      const targetId = c.req.param('targetId')
      const target = await findBridgeTarget(targetId)
      if (!target) return c.json({ error: 'Tab target not found' }, 404)

      await browser.reloadTarget(target.id)
      const updated = (await findBridgeTarget(target.id)) ?? target
      const activeTarget = await browser.getActiveTarget()

      return c.json({
        tab: toBridgeTab(
          updated,
          getPageId(updated.id),
          activeTarget?.id ?? null,
        ),
      })
    })
    .post('/tabs/:targetId/back', async (c) => {
      if (!browser.isCdpConnected()) return cdpUnavailable(c)

      const targetId = c.req.param('targetId')
      const target = await findBridgeTarget(targetId)
      if (!target) return c.json({ error: 'Tab target not found' }, 404)

      await browser.goBackTarget(target.id)
      const updated = (await findBridgeTarget(target.id)) ?? target
      const activeTarget = await browser.getActiveTarget()

      return c.json({
        tab: toBridgeTab(
          updated,
          getPageId(updated.id),
          activeTarget?.id ?? null,
        ),
      })
    })
    .post('/tabs/:targetId/forward', async (c) => {
      if (!browser.isCdpConnected()) return cdpUnavailable(c)

      const targetId = c.req.param('targetId')
      const target = await findBridgeTarget(targetId)
      if (!target) return c.json({ error: 'Tab target not found' }, 404)

      await browser.goForwardTarget(target.id)
      const updated = (await findBridgeTarget(target.id)) ?? target
      const activeTarget = await browser.getActiveTarget()

      return c.json({
        tab: toBridgeTab(
          updated,
          getPageId(updated.id),
          activeTarget?.id ?? null,
        ),
      })
    })
    .delete('/tabs/:targetId', async (c) => {
      if (!browser.isCdpConnected()) return cdpUnavailable(c)

      const targetId = c.req.param('targetId')
      const target = await findBridgeTarget(targetId)
      if (!target) return c.json({ error: 'Tab target not found' }, 404)

      await browser.closeTargetTab(target.id)
      pageIdsByTargetId.delete(target.id)
      return c.json({ ok: true })
    })
    .get('/tabs/:targetId/screenshot', async (c) => {
      if (!browser.isCdpConnected()) return cdpUnavailable(c)

      const targetId = c.req.param('targetId')
      const target = await findBridgeTarget(targetId)
      if (!target) return c.json({ error: 'Tab target not found' }, 404)

      const format = c.req.query('format') === 'jpeg' ? 'jpeg' : 'png'
      const fullPage = c.req.query('fullPage') === '1'
      const qualityParam = Number(c.req.query('quality'))
      const quality =
        format === 'jpeg' && Number.isFinite(qualityParam)
          ? Math.max(1, Math.min(100, Math.round(qualityParam)))
          : undefined

      const screenshot = await browser.screenshotTarget(target.id, {
        format,
        fullPage,
        ...(quality !== undefined ? { quality } : {}),
      })

      return c.json(screenshot)
    })
    .get('/tabs/:targetId/content', async (c) => {
      if (!browser.isCdpConnected()) return cdpUnavailable(c)

      const targetId = c.req.param('targetId')
      const target = await findBridgeTarget(targetId)
      if (!target) return c.json({ error: 'Tab target not found' }, 404)

      const selector = c.req.query('selector')?.trim() || undefined
      const text = await browser.contentTarget(target.id, selector)
      return c.json({ text })
    })
    .get('/tabs/:targetId/snapshot', async (c) => {
      if (!browser.isCdpConnected()) return cdpUnavailable(c)

      const targetId = c.req.param('targetId')
      const target = await findBridgeTarget(targetId)
      if (!target) return c.json({ error: 'Tab target not found' }, 404)

      const enhanced = c.req.query('enhanced') === '1'
      const snapshot = enhanced
        ? await browser.enhancedSnapshotTarget(target.id)
        : await browser.snapshotTarget(target.id)
      return c.json({ snapshot, enhanced })
    })
    .get('/tabs/:targetId/dom', async (c) => {
      if (!browser.isCdpConnected()) return cdpUnavailable(c)

      const targetId = c.req.param('targetId')
      const target = await findBridgeTarget(targetId)
      if (!target) return c.json({ error: 'Tab target not found' }, 404)

      const selector = c.req.query('selector')?.trim() || undefined
      const html = await browser.domTarget(target.id, { selector })
      return c.json({ html })
    })
    .post('/tabs/:targetId/evaluate', async (c) => {
      if (!browser.isCdpConnected()) return cdpUnavailable(c)

      const targetId = c.req.param('targetId')
      const target = await findBridgeTarget(targetId)
      if (!target) return c.json({ error: 'Tab target not found' }, 404)

      const body = readRecord(await readJson(c))
      const expression = body.expression
      if (typeof expression !== 'string' || !expression.trim()) {
        return c.json({ error: 'expression is required' }, 400)
      }

      const result = await browser.evaluateTarget(target.id, expression)
      return c.json(result)
    })
    .get('/tabs/:targetId/console', async (c) => {
      if (!browser.isCdpConnected()) return cdpUnavailable(c)

      const targetId = c.req.param('targetId')
      const target = await findBridgeTarget(targetId)
      if (!target) return c.json({ error: 'Tab target not found' }, 404)

      const level = readConsoleLevel(c.req.query('level')) ?? 'info'
      const search = c.req.query('search')?.trim() || undefined
      const limitParam = Number(c.req.query('limit'))
      const limit = Number.isFinite(limitParam)
        ? Math.max(1, Math.min(200, Math.round(limitParam)))
        : undefined
      const clear = c.req.query('clear') === '1'

      const result = await browser.getConsoleLogsTarget(target.id, {
        level,
        search,
        ...(limit !== undefined ? { limit } : {}),
        clear,
      })
      return c.json({
        ...result,
        returnedCount: result.entries.length,
      })
    })
    .get('/tabs/:targetId/network', async (c) => {
      if (!browser.isCdpConnected()) return cdpUnavailable(c)

      const targetId = c.req.param('targetId')
      const target = await findBridgeTarget(targetId)
      if (!target) return c.json({ error: 'Tab target not found' }, 404)

      const search = c.req.query('search')?.trim() || undefined
      const limitParam = Number(c.req.query('limit'))
      const limit = Number.isFinite(limitParam)
        ? Math.max(1, Math.min(200, Math.round(limitParam)))
        : undefined
      const failedOnly = c.req.query('failed') === '1'
      const clear = c.req.query('clear') === '1'

      const result = await browser.getNetworkEntriesTarget(target.id, {
        search,
        failedOnly,
        ...(limit !== undefined ? { limit } : {}),
        clear,
      })
      return c.json({
        ...result,
        returnedCount: result.entries.length,
      })
    })
    .post('/tabs/:targetId/click', async (c) => {
      if (!browser.isCdpConnected()) return cdpUnavailable(c)

      const targetId = c.req.param('targetId')
      const target = await findBridgeTarget(targetId)
      if (!target) return c.json({ error: 'Tab target not found' }, 404)

      const body = readRecord(await readJson(c))
      const point = readPoint(body)
      if (!point) return c.json({ error: 'x and y are required' }, 400)

      const clickCount = readFiniteNumber(body, 'clickCount')
      const button = body.button === 'right' || body.button === 'middle'
        ? body.button
        : 'left'

      await browser.clickTargetAt(target.id, point.x, point.y, {
        button,
        clickCount: clickCount === null ? 1 : Math.max(1, Math.round(clickCount)),
      })

      return c.json({ ok: true })
    })
    .post('/tabs/:targetId/type', async (c) => {
      if (!browser.isCdpConnected()) return cdpUnavailable(c)

      const targetId = c.req.param('targetId')
      const target = await findBridgeTarget(targetId)
      if (!target) return c.json({ error: 'Tab target not found' }, 404)

      const body = readRecord(await readJson(c))
      const point = readPoint(body)
      if (!point) return c.json({ error: 'x and y are required' }, 400)

      const text = body.text
      if (typeof text !== 'string') {
        return c.json({ error: 'text is required' }, 400)
      }

      await browser.typeTargetAt(
        target.id,
        point.x,
        point.y,
        text,
        body.clear === true,
      )

      return c.json({ ok: true })
    })
    .post('/tabs/:targetId/scroll', async (c) => {
      if (!browser.isCdpConnected()) return cdpUnavailable(c)

      const targetId = c.req.param('targetId')
      const target = await findBridgeTarget(targetId)
      if (!target) return c.json({ error: 'Tab target not found' }, 404)

      const body = readRecord(await readJson(c))
      const direction = readDirection(body.direction)
      if (!direction) {
        return c.json({ error: 'direction must be up, down, left, or right' }, 400)
      }

      await browser.scrollTarget(target.id, direction, readPositiveAmount(body))

      return c.json({ ok: true })
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
