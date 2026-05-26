import { ChevronDown } from 'lucide-react'
import { type FC, useEffect, useMemo, useState } from 'react'
import {
  MessageResponse,
  type MessageResponseProps,
} from '@/components/ai-elements/message'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { cn } from '@/lib/utils'

type ProcessToolStatus =
  | 'pending'
  | 'running'
  | 'completed'
  | 'failed'
  | 'error'

export interface ProcessTool {
  id?: string
  name: string
  label: string
  subject?: string
  status: ProcessToolStatus
  input?: unknown
  error?: string
  durationMs?: number
}

export type ProcessItem =
  | {
      id: string
      kind: 'text'
      text: string
      muted?: boolean
    }
  | {
      id: string
      kind: 'tool'
      tool: ProcessTool
    }

interface AssistantProcessProps {
  items: ProcessItem[]
  done: boolean
  streaming: boolean
  hasFinalText: boolean
  stalled: boolean
}

interface FinalAssistantTextProps {
  text: string
  streaming: boolean
  stalled: boolean
}

const STALL_THRESHOLD_MS = 3000
const STALLED_TAIL_CHARS = 8
const UNSAFE_TAIL_CHARS = /[`*_#[\]()<>{}|\\]/

export function useStalledTextIndicator({
  text,
  streaming,
  lastTextDeltaAt,
  thresholdMs = STALL_THRESHOLD_MS,
}: {
  text: string
  streaming: boolean
  lastTextDeltaAt?: number
  thresholdMs?: number
}): boolean {
  const [stalled, setStalled] = useState(false)

  useEffect(() => {
    if (!streaming || !text.trim() || !lastTextDeltaAt) {
      setStalled(false)
      return
    }

    const elapsed = Date.now() - lastTextDeltaAt
    if (elapsed >= thresholdMs) {
      setStalled(true)
      return
    }

    setStalled(false)
    const timer = window.setTimeout(
      () => setStalled(true),
      thresholdMs - elapsed,
    )
    return () => window.clearTimeout(timer)
  }, [lastTextDeltaAt, streaming, text, thresholdMs])

  return stalled
}

export function FinalAssistantText({
  text,
  streaming,
  stalled,
}: FinalAssistantTextProps) {
  const content = useMemo(
    () => (stalled ? decorateStalledMarkdownTail(text) : text),
    [stalled, text],
  )
  const hasDecoratedTail = content !== text
  if (!text.trim()) return null

  return (
    <MessageResponse
      key={`${streaming ? 'streaming' : 'static'}-${hasDecoratedTail}`}
      mode={streaming ? 'streaming' : 'static'}
      parseIncompleteMarkdown={streaming}
      isAnimating={streaming}
      className={cn(
        'max-w-full overflow-hidden break-words text-sm leading-7',
        'prose-li:leading-7 prose-p:leading-7',
        '[&_[data-streamdown="code-block"]]:!w-full [&_[data-streamdown="code-block"]]:!max-w-full [&_[data-streamdown="code-block"]]:overflow-x-auto',
        '[&_[data-streamdown="table-wrapper"]]:!w-full [&_[data-streamdown="table-wrapper"]]:!max-w-full [&_[data-streamdown="table-wrapper"]]:overflow-x-auto',
        '[&_table]:w-max [&_table]:min-w-full',
        streaming && 'agent-streaming-output',
        hasDecoratedTail && 'agent-stalled-output',
      )}
    >
      {content}
    </MessageResponse>
  )
}

export const AssistantProcess: FC<AssistantProcessProps> = ({
  items,
  done,
  streaming,
  hasFinalText,
  stalled,
}) => {
  const [open, setOpen] = useState(!done)
  const failed = items.some(
    (item) => item.kind === 'tool' && isFailedTool(item.tool.status),
  )

  useEffect(() => {
    setOpen(!done)
  }, [done])

  const statusLabel = processStatusLabel({
    done,
    failed,
    hasFinalText,
    stalled,
  })

  if (items.length === 0) {
    if (done) return null
    return (
      <div className="text-left text-muted-foreground text-xs leading-5">
        <span className="agent-process-status-running font-medium">
          {statusLabel}
        </span>
      </div>
    )
  }

  const countLabel = `${items.length} ${items.length === 1 ? 'item' : 'items'}`

  return (
    <Collapsible
      className="w-full text-muted-foreground"
      onOpenChange={setOpen}
      open={open}
    >
      <CollapsibleTrigger className="group flex w-full items-center gap-2 text-left text-xs leading-5 outline-none transition-colors hover:text-foreground">
        <span
          className={cn(
            'font-medium',
            !done && 'agent-process-status-running',
            failed && done && 'text-destructive',
          )}
        >
          {statusLabel}
        </span>
        {countLabel ? (
          <span className="text-muted-foreground/55">· {countLabel}</span>
        ) : null}
        <ChevronDown className="size-3.5 transition-transform group-data-[state=open]:rotate-180" />
      </CollapsibleTrigger>
      <CollapsibleContent className="data-[state=closed]:fade-out-0 data-[state=closed]:slide-out-to-top-1 data-[state=open]:slide-in-from-top-1 outline-none data-[state=closed]:animate-out data-[state=open]:animate-in">
        <div className="mt-2 space-y-2.5">
          {items.map((item) => {
            if (item.kind === 'text') {
              return (
                <ProcessMarkdown
                  key={item.id}
                  mode={streaming && !done ? 'streaming' : 'static'}
                >
                  {item.text}
                </ProcessMarkdown>
              )
            }

            return <ProcessToolRow key={item.id} tool={item.tool} />
          })}
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}

function ProcessMarkdown({
  children,
  mode,
}: {
  children: string
  mode: MessageResponseProps['mode']
}) {
  return (
    <MessageResponse
      mode={mode}
      parseIncompleteMarkdown={mode === 'streaming'}
      className="[&_[data-streamdown='code-block']]:!max-w-full max-w-full overflow-hidden break-words text-muted-foreground/75 text-sm leading-6 [&_*]:text-muted-foreground/75 [&_[data-streamdown='code-block']]:overflow-x-auto"
    >
      {children}
    </MessageResponse>
  )
}

function ProcessToolRow({ tool }: { tool: ProcessTool }) {
  const failed = isFailedTool(tool.status)
  return (
    <div className="space-y-1 text-xs leading-5">
      <code
        className={cn(
          'block whitespace-pre-wrap break-words font-mono text-muted-foreground/75',
          failed && 'text-destructive/85',
        )}
      >
        {formatToolCommand(tool)}
      </code>
      {tool.error ? (
        <div className="text-destructive/85">{tool.error}</div>
      ) : null}
      {tool.durationMs != null ? (
        <div className="text-muted-foreground/45 tabular-nums">
          {(tool.durationMs / 1000).toFixed(1)}s
        </div>
      ) : null}
    </div>
  )
}

function processStatusLabel({
  done,
  failed,
  hasFinalText,
  stalled,
}: {
  done: boolean
  failed: boolean
  hasFinalText: boolean
  stalled: boolean
}) {
  if (done) return failed ? 'Activity failed' : 'Activity log'
  if (stalled) return 'Still generating'
  if (hasFinalText) return 'Replying'
  return 'Working'
}

function isFailedTool(status: ProcessToolStatus) {
  return status === 'failed' || status === 'error'
}

export function formatToolCommand(tool: ProcessTool): string {
  const input = formatUnknown(tool.input)
  if (input) return `${tool.name} ${input}`
  if (tool.subject) return `${tool.name} ${tool.subject}`
  if (tool.label && tool.label !== tool.name)
    return `${tool.name} ${tool.label}`
  return tool.name
}

function formatUnknown(value: unknown): string {
  if (value == null) return ''
  if (typeof value === 'string') return value
  try {
    return JSON.stringify(value)
  } catch {
    return String(value)
  }
}

export function decorateStalledMarkdownTail(markdown: string): string {
  const end = markdown.search(/\s*$/)
  const tailEnd = end < 0 ? markdown.length : end
  const trailing = markdown.slice(tailEnd)
  const body = markdown.slice(0, tailEnd)
  const chars = Array.from(body)
  const lastBoundaryIndex = findReadableTailBoundary(chars)
  const tailStart =
    lastBoundaryIndex >= 0
      ? lastBoundaryIndex + 1
      : Math.max(0, chars.length - STALLED_TAIL_CHARS)
  const tail = chars.slice(tailStart).join('')

  if (!tail.trim() || tail.includes('\n') || UNSAFE_TAIL_CHARS.test(tail)) {
    return markdown
  }

  const before = chars.slice(0, tailStart).join('')
  return `${before}<span>${escapeHtml(tail)}</span>${trailing}`
}

function findReadableTailBoundary(chars: string[]): number {
  const minIndex = Math.max(0, chars.length - 12)
  for (let index = chars.length - 1; index >= minIndex; index -= 1) {
    if (/\s/.test(chars[index] ?? '')) return index
  }
  return -1
}

function escapeHtml(value: string): string {
  return value
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
}
