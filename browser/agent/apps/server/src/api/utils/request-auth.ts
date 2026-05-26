import type { MiddlewareHandler } from 'hono'
import { isLocalhostRequest } from './security'

const LOOPBACK_HOSTS = new Set(['127.0.0.1', 'localhost', '[::1]', '::1'])
const EXTENSION_PROTOCOLS = new Set(['chrome-extension:', 'moz-extension:'])
const SAFE_MISSING_ORIGIN_METHODS = new Set(['GET', 'HEAD', 'OPTIONS'])

export type TrustedAppRequestInput = {
  origin?: string
  method: string
  isLocalhost: boolean
}

export function isTrustedAppOrigin(origin: string | undefined): boolean {
  if (!origin) return false

  try {
    const url = new URL(origin)

    if (
      (url.protocol === 'http:' || url.protocol === 'https:') &&
      LOOPBACK_HOSTS.has(url.hostname)
    ) {
      return true
    }

    return EXTENSION_PROTOCOLS.has(url.protocol)
  } catch {
    return false
  }
}

export function isTrustedAppRequest({
  origin,
  method,
  isLocalhost,
}: TrustedAppRequestInput): boolean {
  if (origin) {
    return isLocalhost && isTrustedAppOrigin(origin)
  }

  return SAFE_MISSING_ORIGIN_METHODS.has(method) && isLocalhost
}

export function requireTrustedAppOrigin(): MiddlewareHandler {
  return async (c, next) => {
    if (
      isTrustedAppRequest({
        origin: c.req.header('origin'),
        method: c.req.method,
        isLocalhost: isLocalhostRequest(c),
      })
    ) {
      return next()
    }

    return c.json({ error: 'Forbidden' }, 403)
  }
}
