/**
 * @license
 * Copyright 2025 BrowserOS
 * SPDX-License-Identifier: AGPL-3.0-or-later
 */

import type { Context } from 'hono'
import type { Env } from '../types'

const LOCALHOST_ADDRESSES = new Set(['127.0.0.1', '::1', '::ffff:127.0.0.1'])
const LOCALHOST_HOSTNAMES = new Set(['127.0.0.1', 'localhost', '::1', '[::1]'])

function readHostHeaderHostname(host: string): string {
  if (host.startsWith('[')) {
    const end = host.indexOf(']')
    return end === -1 ? host : host.slice(0, end + 1)
  }
  return host.split(':')[0] ?? ''
}

/**
 * Check if request originates from localhost.
 *
 * IMPORTANT: This checks the actual TCP connection IP (req.socket.remoteAddress equivalent)
 * which CANNOT be spoofed, unlike HTTP headers like Host or X-Forwarded-For.
 *
 * In Bun.serve, we use server.requestIP() to get the real client IP.
 *
 * @param c - Hono context with Bun server binding
 * @returns true if request is from localhost, false otherwise
 */
export function isLocalhostRequest(c: Context<Env>): boolean {
  const server = c.env?.server
  const request = c.req.raw

  // 1. CHECK ACTUAL TCP CONNECTION IP (cannot be spoofed)
  if (!server?.requestIP) return false

  const socketAddr = server.requestIP(request)
  if (!socketAddr || !LOCALHOST_ADDRESSES.has(socketAddr.address)) {
    return false
  }

  // 2. Also check Host header (defense in depth)
  const host = c.req.header('host')
  if (!host) return false
  const hostname = readHostHeaderHostname(host)
  if (!LOCALHOST_HOSTNAMES.has(hostname)) return false

  // 3. Check referer if present (defense in depth)
  const referer = c.req.header('referer')
  if (referer) {
    try {
      const url = new URL(referer)
      if (!LOCALHOST_HOSTNAMES.has(url.hostname)) return false
    } catch {
      return false
    }
  }

  return true
}
