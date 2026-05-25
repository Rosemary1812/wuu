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
      createTargetTab: mock(async () => firstTarget),
      navigateTarget: mock(async () => {}),
      screenshotTarget: mock(async () => ({
        data: '',
        mimeType: 'image/png',
        devicePixelRatio: 1,
      })),
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
      createTargetTab: mock(async () => firstTarget),
      navigateTarget: mock(async () => {}),
      screenshotTarget: mock(async () => ({
        data: '',
        mimeType: 'image/png',
        devicePixelRatio: 1,
      })),
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
      createTargetTab: mock(async () => firstTarget),
      navigateTarget: mock(async () => {}),
      screenshotTarget: mock(async () => ({
        data: '',
        mimeType: 'image/png',
        devicePixelRatio: 1,
      })),
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

  it('creates a background browser tab', async () => {
    const browser = {
      isCdpConnected: mock(() => true),
      listTargets: mock(async () => [firstTarget]),
      getActiveTarget: mock(async () => firstTarget),
      createTargetTab: mock(async () => secondTarget),
      navigateTarget: mock(async () => {}),
      screenshotTarget: mock(async () => ({
        data: '',
        mimeType: 'image/png',
        devicePixelRatio: 1,
      })),
    }
    const route = createBrowserBridgeRoutes({ browser })

    const response = await route.request('/tabs', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ url: secondTarget.url }),
    })

    expect(response.status).toBe(201)
    expect(await response.json()).toMatchObject({
      tab: {
        targetId: 'target-2',
        tabId: 102,
        url: 'https://example.test/',
        isActive: false,
      },
    })
    expect(browser.createTargetTab).toHaveBeenCalledWith(secondTarget.url, {
      background: true,
    })
  })

  it('navigates an existing browser tab target', async () => {
    const navigatedTarget = {
      ...secondTarget,
      url: 'data:text/html,<title>Bridge</title>',
      title: 'Bridge',
    }
    let navigated = false
    const browser = {
      isCdpConnected: mock(() => true),
      listTargets: mock(async () => [
        firstTarget,
        navigated ? navigatedTarget : secondTarget,
      ]),
      getActiveTarget: mock(async () => firstTarget),
      createTargetTab: mock(async () => secondTarget),
      navigateTarget: mock(async () => {
        navigated = true
      }),
      screenshotTarget: mock(async () => ({
        data: '',
        mimeType: 'image/png',
        devicePixelRatio: 1,
      })),
    }
    const route = createBrowserBridgeRoutes({ browser })

    const response = await route.request('/tabs/target-2/navigate', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ url: navigatedTarget.url }),
    })

    expect(response.status).toBe(200)
    expect(await response.json()).toMatchObject({
      tab: {
        targetId: 'target-2',
        url: navigatedTarget.url,
        title: 'Bridge',
      },
    })
    expect(browser.navigateTarget).toHaveBeenCalledWith(
      'target-2',
      navigatedTarget.url,
    )
  })

  it('captures a screenshot for an existing browser tab target', async () => {
    const browser = {
      isCdpConnected: mock(() => true),
      listTargets: mock(async () => [secondTarget]),
      getActiveTarget: mock(async () => secondTarget),
      createTargetTab: mock(async () => secondTarget),
      navigateTarget: mock(async () => {}),
      screenshotTarget: mock(async () => ({
        data: 'iVBORw0KGgo=',
        mimeType: 'image/png',
        devicePixelRatio: 2,
      })),
    }
    const route = createBrowserBridgeRoutes({ browser })

    const response = await route.request('/tabs/target-2/screenshot?fullPage=1')

    expect(response.status).toBe(200)
    expect(await response.json()).toEqual({
      data: 'iVBORw0KGgo=',
      mimeType: 'image/png',
      devicePixelRatio: 2,
    })
    expect(browser.screenshotTarget).toHaveBeenCalledWith('target-2', {
      format: 'png',
      fullPage: true,
    })
  })

  it('rejects navigation without an absolute URL', async () => {
    const browser = {
      isCdpConnected: mock(() => true),
      listTargets: mock(async () => [secondTarget]),
      getActiveTarget: mock(async () => secondTarget),
      createTargetTab: mock(async () => secondTarget),
      navigateTarget: mock(async () => {}),
      screenshotTarget: mock(async () => ({
        data: '',
        mimeType: 'image/png',
        devicePixelRatio: 1,
      })),
    }
    const route = createBrowserBridgeRoutes({ browser })

    const response = await route.request('/tabs/target-2/navigate', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ url: '/relative' }),
    })

    expect(response.status).toBe(400)
    expect(await response.json()).toEqual({
      error: 'A valid absolute URL is required',
    })
    expect(browser.navigateTarget).not.toHaveBeenCalled()
  })
})
