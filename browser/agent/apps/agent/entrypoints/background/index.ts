import { sessionStorage } from '@/lib/auth/sessionStorage'
import { Capabilities } from '@/lib/browseros/capabilities'
import { getHealthCheckUrl, getMcpServerUrl } from '@/lib/browseros/helpers'
import { checkAndShowChangelog } from '@/lib/changelog/changelog-notifier'
import { fetchMcpTools } from '@/lib/mcp/client'
import { onServerMessage } from '@/lib/messaging/server/serverMessages'
import { authRedirectPathStorage } from '@/lib/onboarding/onboardingStorage'
import { syncOnboardingProfile } from '@/lib/onboarding/syncOnboardingProfile'
import { selectedTextStorage } from '@/lib/selected-text/selectedTextStorage'
import { stopAgentStorage } from '@/lib/stop-agent/stop-agent-storage'

export default defineBackground(() => {
  chrome.sidePanel.setOptions({ enabled: false })

  Capabilities.initialize().catch(() => null)

  chrome.action.onClicked.addListener(() => {
    openHomeTabIfMissing()
  })

  chrome.runtime.onInstalled.addListener((details) => {
    if (details.reason === chrome.runtime.OnInstalledReason.INSTALL) {
      openHomeTabIfMissing()
    }

    if (details.reason === chrome.runtime.OnInstalledReason.UPDATE) {
      checkAndShowChangelog().catch(() => null)
    }
  })

  chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
    if (message?.type === 'get-tab-id') {
      sendResponse({ tabId: sender.tab?.id })
      return true
    }

    if (message?.type === 'AUTH_SUCCESS' && sender.tab?.id) {
      const tabId = sender.tab.id
      authRedirectPathStorage
        .getValue()
        .then((redirectPath) => {
          const hash = redirectPath || '/home'
          chrome.tabs.update(tabId, {
            url: chrome.runtime.getURL(`app.html#${hash}`),
          })
          if (redirectPath) authRedirectPathStorage.removeValue()
        })
        .catch(() => {
          chrome.tabs.update(tabId, {
            url: chrome.runtime.getURL('app.html#/home'),
          })
        })
    }

    if (message?.type === 'stop-agent' && message?.conversationId) {
      stopAgentStorage.setValue({
        conversationId: message.conversationId,
        timestamp: Date.now(),
      })
    }
  })

  // Clean up selected text storage when a tab is closed
  chrome.tabs.onRemoved.addListener((tabId) => {
    const key = String(tabId)
    selectedTextStorage.getValue().then((map) => {
      if (map[key]) {
        const { [key]: _, ...rest } = map
        selectedTextStorage.setValue(rest)
      }
    })
  })

  sessionStorage.watch(async (newSession) => {
    if (newSession?.user?.id) {
      try {
        await syncOnboardingProfile(newSession.user.id)
      } catch {}
    }
  })

  onServerMessage('checkHealth', async () => {
    try {
      const url = await getHealthCheckUrl()
      const response = await fetch(url)
      return { healthy: response.ok }
    } catch {
      return { healthy: false }
    }
  })

  onServerMessage('fetchMcpTools', async () => {
    try {
      const url = await getMcpServerUrl()
      const tools = await fetchMcpTools(url)
      return { tools }
    } catch (err) {
      return {
        tools: [],
        error: err instanceof Error ? err.message : 'Failed to fetch tools',
      }
    }
  })
})

function openHomeTabIfMissing() {
  const homeUrl = chrome.runtime.getURL('app.html#/home')
  const appUrlPattern = chrome.runtime.getURL('app.html*')

  setTimeout(() => {
    chrome.tabs.query({ url: appUrlPattern }, (tabs) => {
      if (chrome.runtime.lastError) return

      const existingTabId = tabs[0]?.id
      if (existingTabId) {
        chrome.tabs.update(existingTabId, { url: homeUrl })
        return
      }

      chrome.tabs.create({ url: homeUrl })
    })
  }, 500)
}
