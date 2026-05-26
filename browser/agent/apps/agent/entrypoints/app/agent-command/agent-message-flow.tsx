import { ChevronDown } from 'lucide-react'
import { type FC, useEffect, useMemo, useState } from 'react'
import {
  MessageResponse,
  type MessageResponseProps,
} from '@/components/ai-elements/message'
import { Collapsible, CollapsibleTrigger } from '@/components/ui/collapsible'
import {
  formatMessageFlowCommand,
  isMessageFlowFailedStatus,
  messageFlowStatusLabel,
} from '@/lib/message-flow-display'
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
    if (!done) {
      setOpen(true)
      return
    }
    const timer = window.setTimeout(() => setOpen(false), 140)
    return () => window.clearTimeout(timer)
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
      <div
        aria-hidden={!open}
        className="max-h-[1600px] overflow-hidden opacity-100 outline-none transition-[max-height,opacity,transform] duration-[260ms] ease-[cubic-bezier(0.16,1,0.3,1)] data-[state=closed]:max-h-0 data-[state=closed]:-translate-y-1 data-[state=open]:translate-y-0 data-[state=closed]:opacity-0"
        data-state={open ? 'open' : 'closed'}
      >
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
      </div>
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
      <div
        className={cn(
          'whitespace-pre-wrap break-words text-muted-foreground/75',
          failed && 'text-destructive/85',
        )}
      >
        {formatToolCommand(tool)}
      </div>
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
  return messageFlowStatusLabel({ done, failed, hasFinalText, stalled })
}

function isFailedTool(status: ProcessToolStatus) {
  return isMessageFlowFailedStatus(status)
}

export function formatToolCommand(tool: ProcessTool): string {
  return formatMessageFlowCommand({
    name: tool.name,
    input: tool.input,
    subject: tool.subject,
    label: tool.label,
  })
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
