import { describe, expect, it } from 'bun:test'
import {
  compareDottedVersions,
  evaluateRuntimeContract,
} from './runtime-contract'

describe('compareDottedVersions', () => {
  it('compares dotted versions with missing patch values treated as zero', () => {
    expect(compareDottedVersions('1.2', '1.2.0')).toBe(0)
    expect(compareDottedVersions('1.2.1', '1.2.0')).toBe(1)
    expect(compareDottedVersions('1.1.9', '1.2.0')).toBe(-1)
  })
})

describe('evaluateRuntimeContract', () => {
  it('accepts a server that exposes the required API contract', () => {
    const status = evaluateRuntimeContract({
      channel: 'stable',
      agentVersion: '0.0.99',
      browserosVersion: '0.43.0.0',
      chromiumVersion: '142.0.0.0',
      health: {
        contract: { serverApiVersion: 1 },
        versions: { server: '0.0.94' },
      },
    })

    expect(status.state).toBe('compatible')
    expect(status.versions.serverApi).toBe(1)
  })

  it('blocks when the server does not expose the required API contract', () => {
    const status = evaluateRuntimeContract({
      channel: 'stable',
      agentVersion: '0.0.99',
      browserosVersion: '0.43.0.0',
      chromiumVersion: '142.0.0.0',
      health: {
        versions: { server: '0.0.93' },
      },
    })

    expect(status.state).toBe('incompatible')
    if (status.state !== 'incompatible') {
      throw new Error('Expected incompatible status')
    }
    expect(status.reasons[0]).toContain('server API')
  })

  it('does not block on temporary health unavailability', () => {
    const status = evaluateRuntimeContract({
      channel: 'local',
      agentVersion: '0.0.99',
      browserosVersion: null,
      chromiumVersion: null,
      health: null,
      healthUnavailable: true,
    })

    expect(status.state).toBe('compatible')
    if (status.state !== 'compatible') {
      throw new Error('Expected compatible status')
    }
    expect(status.healthUnavailable).toBe(true)
  })
})
