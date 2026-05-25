/**
 * @license
 * Copyright 2025 BrowserOS
 */

import { describe, expect, it, mock } from 'bun:test'
import { createBrowserBridgeRoutes } from '../../../src/api/routes/browser-bridge'
import type { CdpTarget } from '../../../src/browser/backends/types'

const firstTarget: CdpTarget = {
  id: 'target-1',
  type: 'page',
  tabId: 101,
  url: 'chrome-extension://browseros/app.html#/home',
  title: 'Wuu',
  windowId: 7,
}

const secondTarget: CdpTarget = {
  id: 'target-2',
  type: 'page',
  tabId: 102,
  url: 'https://example.test/',
  title: 'Example',
  windowId: 7,
}

describe('createBrowserBridgeRoutes', () => {
  it('returns browser tab state from CDP targets', async () => {
    const browser = {
      isCdpConnected: mock(() => true),
      listTargets: mock(async () => [
        firstTarget,
        secondTarget,
        {
          id: 'iframe-1',
          type: 'iframe',
          tabId: 102,
          url: 'chrome-untrusted://new-tab-page/',
          title: 'New tab frame',
          windowId: 7,
        },
      ]),
      getActiveTarget: mock(async () => firstTarget),
    }
    const route = createBrowserBridgeRoutes({ browser })

    const response = await route.request('/tabs')

    expect(response.status).toBe(200)
    expect(await response.json()).toEqual({
      tabs: [
        {
          pageId: 1,
          targetId: 'target-1',
          tabId: 101,
          url: 'chrome-extension://browseros/app.html#/home',
          title: 'Wuu',
          isActive: true,
          windowId: 7,
        },
        {
          pageId: 2,
          targetId: 'target-2',
          tabId: 102,
          url: 'https://example.test/',
          title: 'Example',
          isActive: false,
          windowId: 7,
        },
      ],
      activeTab: {
        pageId: 1,
        targetId: 'target-1',
        tabId: 101,
        url: 'chrome-extension://browseros/app.html#/home',
        title: 'Wuu',
        isActive: true,
        windowId: 7,
      },
    })
  })

  it('returns the active browser tab directly', async () => {
    const browser = {
      isCdpConnected: mock(() => true),
      listTargets: mock(async () => [firstTarget, secondTarget]),
      getActiveTarget: mock(async () => secondTarget),
    }
    const route = createBrowserBridgeRoutes({ browser })

    const response = await route.request('/active-tab')

    expect(response.status).toBe(200)
    expect(await response.json()).toMatchObject({
      activeTab: {
        pageId: 1,
        targetId: 'target-2',
        tabId: 102,
        url: 'https://example.test/',
        title: 'Example',
        isActive: true,
      },
    })
    expect(browser.listTargets).not.toHaveBeenCalled()
  })

  it('does not query pages when CDP is disconnected', async () => {
    const browser = {
      isCdpConnected: mock(() => false),
      listTargets: mock(async () => [firstTarget]),
      getActiveTarget: mock(async () => firstTarget),
    }
    const route = createBrowserBridgeRoutes({ browser })

    const response = await route.request('/tabs')

    expect(response.status).toBe(503)
    expect(await response.json()).toEqual({
      error: 'Browser CDP is not connected',
    })
    expect(browser.listTargets).not.toHaveBeenCalled()
    expect(browser.getActiveTarget).not.toHaveBeenCalled()
  })
})
