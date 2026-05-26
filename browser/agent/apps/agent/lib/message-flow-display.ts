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
  const formattedInput = formatUnknownInput(input)
  if (formattedInput) {
    return `${toolName} ${formattedInput}`
  }
  if (subject?.trim()) {
    return `${toolName} ${subject.trim()}`
  }
  if (label?.trim() && label.trim() !== toolName) {
    return `${toolName} ${label.trim()}`
  }
  return toolName
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
