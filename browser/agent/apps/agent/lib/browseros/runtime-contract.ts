import { WUU_AGENT_RUNTIME_CONTRACT } from '@browseros/shared/constants/runtime-contract'
import { env } from '@/lib/env'
import { getBrowserOSAdapter } from './adapter'
import { getHealthCheckUrl } from './helpers'

type AgentChannel = 'local' | 'dogfood' | 'stable'

type HealthResponse = {
  contract?: {
    serverApiVersion?: number
  }
  versions?: {
    server?: string | null
    browseros?: string | null
    chromium?: string | null
  }
  runtime?: {
    localOverrides?: {
      wuuBin?: boolean
      wuuSourceRoot?: boolean
      serverResources?: boolean
    }
    vmAgentsEnabled?: boolean
  }
}

type LocalRuntimeOverrides = NonNullable<
  HealthResponse['runtime']
>['localOverrides']

export type RuntimeContractVersions = {
  agent: string | null
  browseros: string | null
  chromium: string | null
  server: string | null
  serverApi: number | null
}

export type RuntimeContractStatus =
  | {
      state: 'compatible'
      channel: AgentChannel
      versions: RuntimeContractVersions
      healthUnavailable: boolean
      localOverrides: LocalRuntimeOverrides | null
    }
  | {
      state: 'incompatible'
      channel: AgentChannel
      versions: RuntimeContractVersions
      reasons: string[]
      localOverrides: LocalRuntimeOverrides | null
    }

export function compareDottedVersions(a: string, b: string): number {
  const aParts = a.split('.').map(Number)
  const bParts = b.split('.').map(Number)
  const maxLen = Math.max(aParts.length, bParts.length)

  for (let i = 0; i < maxLen; i++) {
    const aValue = aParts[i] ?? 0
    const bValue = bParts[i] ?? 0
    if (Number.isNaN(aValue) || Number.isNaN(bValue)) {
      throw new Error(`Invalid version comparison: ${a} vs ${b}`)
    }
    if (aValue < bValue) return -1
    if (aValue > bValue) return 1
  }

  return 0
}

function resolveAgentChannel(): AgentChannel {
  if (
    env.VITE_WUU_AGENT_CHANNEL === 'local' ||
    env.VITE_WUU_AGENT_CHANNEL === 'dogfood' ||
    env.VITE_WUU_AGENT_CHANNEL === 'stable'
  ) {
    return env.VITE_WUU_AGENT_CHANNEL
  }

  return env.MODE === 'development' ? 'local' : 'stable'
}

function getAgentVersion(): string | null {
  try {
    return chrome.runtime.getManifest().version
  } catch {
    return null
  }
}

function resolveServerApiVersion(health: HealthResponse | null): number | null {
  const version = health?.contract?.serverApiVersion
  return typeof version === 'number' ? version : null
}

export function evaluateRuntimeContract(input: {
  channel?: AgentChannel
  agentVersion?: string | null
  browserosVersion?: string | null
  chromiumVersion?: string | null
  health: HealthResponse | null
  healthUnavailable?: boolean
}): RuntimeContractStatus {
  const channel = input.channel ?? resolveAgentChannel()
  const serverApiVersion = resolveServerApiVersion(input.health)
  const versions: RuntimeContractVersions = {
    agent: input.agentVersion ?? null,
    browseros:
      input.browserosVersion ?? input.health?.versions?.browseros ?? null,
    chromium: input.chromiumVersion ?? input.health?.versions?.chromium ?? null,
    server: input.health?.versions?.server ?? null,
    serverApi: serverApiVersion,
  }
  const localOverrides = input.health?.runtime?.localOverrides ?? null

  if (input.healthUnavailable) {
    return {
      state: 'compatible',
      channel,
      versions,
      healthUnavailable: true,
      localOverrides,
    }
  }

  const reasons: string[] = []
  if (
    serverApiVersion === null ||
    serverApiVersion < WUU_AGENT_RUNTIME_CONTRACT.minServerApiVersion
  ) {
    reasons.push(
      `Agent requires server API v${WUU_AGENT_RUNTIME_CONTRACT.minServerApiVersion}; current server API is v${serverApiVersion ?? 0}.`,
    )
  }

  if (
    versions.browseros &&
    compareDottedVersions(
      versions.browseros,
      WUU_AGENT_RUNTIME_CONTRACT.minBrowserOSVersion,
    ) < 0
  ) {
    reasons.push(
      `Agent requires Wuu Browser ${WUU_AGENT_RUNTIME_CONTRACT.minBrowserOSVersion} or newer; current browser is ${versions.browseros}.`,
    )
  }

  if (reasons.length > 0) {
    return {
      state: 'incompatible',
      channel,
      versions,
      reasons,
      localOverrides,
    }
  }

  return {
    state: 'compatible',
    channel,
    versions,
    healthUnavailable: false,
    localOverrides,
  }
}

async function fetchHealth(): Promise<HealthResponse> {
  const url = await getHealthCheckUrl()
  const controller = new AbortController()
  const timeout = window.setTimeout(() => controller.abort(), 3000)

  try {
    const response = await fetch(url, { signal: controller.signal })
    if (!response.ok) {
      throw new Error(`Health check failed: ${response.status}`)
    }
    return (await response.json()) as HealthResponse
  } finally {
    window.clearTimeout(timeout)
  }
}

export async function getRuntimeContractStatus(): Promise<RuntimeContractStatus> {
  const adapter = getBrowserOSAdapter()
  const [browserosVersion, chromiumVersion] = await Promise.all([
    adapter.getBrowserosVersion().catch(() => null),
    adapter.getVersion().catch(() => null),
  ])

  try {
    const health = await fetchHealth()
    return evaluateRuntimeContract({
      agentVersion: getAgentVersion(),
      browserosVersion,
      chromiumVersion,
      health,
    })
  } catch {
    return evaluateRuntimeContract({
      agentVersion: getAgentVersion(),
      browserosVersion,
      chromiumVersion,
      health: null,
      healthUnavailable: true,
    })
  }
}
