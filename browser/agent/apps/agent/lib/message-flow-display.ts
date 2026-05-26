export type MessageFlowLocale = 'en' | 'zh'

export type MessageFlowStatusInput = {
  done: boolean
  failed: boolean
  hasFinalText: boolean
  stalled?: boolean
  locale?: MessageFlowLocale
}

export type MessageFlowCommandInput = {
  name?: string
  input?: unknown
  subject?: string
  label?: string
}

export type MessageFlowPartRole = 'text' | 'process' | 'ignore'

export function messageFlowFinalTextIndex<T>(
  parts: readonly T[],
  roleForPart: (part: T, index: number) => MessageFlowPartRole,
): number {
  let finalTextIndex = -1
  let lastProcessIndex = -1

  parts.forEach((part, index) => {
    const role = roleForPart(part, index)
    if (role === 'process') {
      lastProcessIndex = index
      return
    }
    if (role === 'text') {
      finalTextIndex = index
    }
  })

  return finalTextIndex > lastProcessIndex ? finalTextIndex : -1
}

export function messageFlowStatusLabel({
  done,
  failed,
  hasFinalText,
  stalled = false,
  locale = 'en',
}: MessageFlowStatusInput): string {
  if (locale === 'zh') {
    if (done) {
      return failed ? '过程失败' : '过程记录'
    }
    if (stalled) {
      return '仍在生成'
    }
    return hasFinalText ? '正在回复' : '正在处理'
  }

  if (done) {
    return failed ? 'Activity failed' : 'Activity log'
  }
  if (stalled) {
    return 'Still generating'
  }
  return hasFinalText ? 'Replying' : 'Working'
}

export function isMessageFlowFailedStatus(status: string | undefined): boolean {
  return status === 'failed' || status === 'error'
}

export function formatMessageFlowCommand({
  name,
  input,
  subject,
  label,
}: MessageFlowCommandInput): string {
  const toolName = name?.trim() || 'tool'
  const displayLabel = formatDisplayLabel(label, toolName)
  const displayDetail = subject?.trim() || formatInputDetail(input)

  if (displayLabel) {
    if (displayDetail && !isDuplicateDetail(displayLabel, displayDetail)) {
      return `${displayLabel} ${displayDetail}`
    }
    return displayLabel
  }

  if (displayDetail) {
    return `${toolName} ${displayDetail}`
  }

  const formattedInput = formatUnknownInput(input)
  if (formattedInput) {
    return `${toolName} ${formattedInput}`
  }
  return toolName
}

function formatDisplayLabel(
  label: string | undefined,
  toolName: string,
): string {
  const trimmed = label?.trim()
  if (!trimmed) return ''
  if (trimmed === toolName && /^[a-z_]+$/.test(trimmed)) return ''
  return trimmed
}

function formatInputDetail(value: unknown): string {
  if (typeof value === 'string') return truncate(value.trim(), 90)
  if (!value || typeof value !== 'object' || Array.isArray(value)) return ''

  const input = value as Record<string, unknown>
  const command = stringField(input, 'command', 'cmd')
  if (command) return truncate(command, 90)

  const path = stringField(input, 'path', 'file', 'filename')
  if (path) return basename(path)

  const query = stringField(input, 'pattern', 'query', 'q')
  if (query) return truncate(query, 70)

  return ''
}

function formatUnknownInput(value: unknown): string {
  if (value == null) {
    return ''
  }
  if (typeof value === 'string') {
    return value.trim()
  }
  try {
    return JSON.stringify(value)
  } catch {
    return String(value)
  }
}

function stringField(
  input: Record<string, unknown>,
  ...keys: string[]
): string {
  for (const key of keys) {
    const value = input[key]
    if (typeof value === 'string' && value.trim()) return value.trim()
  }
  return ''
}

function basename(path: string): string {
  const parts = path.split(/[/\\]/).filter(Boolean)
  return truncate(parts[parts.length - 1] ?? path, 90)
}

function truncate(text: string, max: number): string {
  return text.length > max ? `${text.slice(0, max - 1)}…` : text
}

function isDuplicateDetail(label: string, detail: string): boolean {
  const normalizedLabel = normalizeDisplayText(label)
  const normalizedDetail = normalizeDisplayText(detail)
  return (
    normalizedDetail === normalizedLabel ||
    normalizedDetail.startsWith(`${normalizedLabel} `)
  )
}

function normalizeDisplayText(text: string): string {
  return text
    .toLowerCase()
    .replace(/\([^)]*\)/g, '')
    .replace(/[^\p{L}\p{N}]+/gu, ' ')
    .trim()
}
