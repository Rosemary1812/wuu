import { Copy } from 'lucide-react'
import { type FC, useCallback, useMemo } from 'react'
import {
  Message,
  MessageAction,
  MessageActions,
  MessageAttachment,
  MessageAttachments,
  MessageContent,
  MessageResponse,
  MessageToolbar,
} from '@/components/ai-elements/message'
import { cn } from '@/lib/utils'
import {
  AssistantProcess,
  FinalAssistantText,
  type ProcessItem,
} from './agent-message-flow'
import type {
  ClawChatMessagePart,
  ClawChatMessage as ClawChatMessageType,
} from './claw-chat-types'

function formatCost(usd: number): string {
  if (usd < 0.005) return `$${usd.toFixed(4)}`
  return `$${usd.toFixed(2)}`
}

type AttachmentPart = Extract<ClawChatMessagePart, { type: 'attachment' }>
type MetaPart = Extract<ClawChatMessagePart, { type: 'meta' }>

interface AssistantRenderPlan {
  attachments: AttachmentPart[]
  processItems: ProcessItem[]
  finalText: string
  metaParts: MetaPart[]
}

function buildAssistantRenderPlan(
  parts: ClawChatMessagePart[],
): AssistantRenderPlan {
  const attachments: AttachmentPart[] = []
  const processItems: ProcessItem[] = []
  const metaParts: MetaPart[] = []
  const lastTextIndex = findLastTextPartIndex(parts)
  let finalText = ''

  parts.forEach((part, partIndex) => {
    if (part.type === 'attachment') {
      attachments.push(part)
      return
    }

    if (part.type === 'meta') {
      metaParts.push(part)
      return
    }

    if (part.type === 'reasoning') {
      processItems.push({
        id: `${partIndex}-reasoning`,
        kind: 'text',
        text: part.text,
        muted: true,
      })
      return
    }

    if (part.type === 'text') {
      if (partIndex === lastTextIndex) {
        finalText = part.text
      } else {
        processItems.push({
          id: `${partIndex}-text`,
          kind: 'text',
          text: part.text,
          muted: true,
        })
      }
      return
    }

    if (part.type === 'tool-call') {
      processItems.push({
        id: `${partIndex}-${part.name}`,
        kind: 'tool',
        tool: {
          id: `${partIndex}-${part.name}`,
          name: part.name,
          label: part.label,
          subject: part.subject,
          status: part.status,
          input: part.input,
          error: part.error,
          durationMs: part.durationMs,
        },
      })
    }
  })

  return { attachments, processItems, finalText, metaParts }
}

function findLastTextPartIndex(parts: ClawChatMessagePart[]): number {
  for (let index = parts.length - 1; index >= 0; index -= 1) {
    if (parts[index]?.type === 'text') return index
  }
  return -1
}

interface ClawChatMessageProps {
  message: ClawChatMessageType
}

export const ClawChatMessage: FC<ClawChatMessageProps> = ({ message }) => {
  const plan = useMemo(
    () => buildAssistantRenderPlan(message.parts),
    [message.parts],
  )
  const messageText = message.parts
    .filter((p) => p.type === 'text')
    .map((p) => p.text)
    .join('\n')
  const copyText = message.role === 'assistant' ? plan.finalText : messageText

  const handleCopy = useCallback(() => {
    if (copyText) navigator.clipboard.writeText(copyText)
  }, [copyText])

  return (
    <Message
      from={message.role}
      className="max-w-full group-[.is-user]:max-w-[80%]"
    >
      <MessageContent className="max-w-full overflow-hidden group-[.is-assistant]:w-full group-[.is-user]:max-w-full">
        {plan.attachments.length > 0 ? (
          <MessageAttachments>
            {plan.attachments.map((attachment, idx) => (
              <MessageAttachment
                // biome-ignore lint/suspicious/noArrayIndexKey: attachment order is stable within a finalized message
                key={`${attachment.kind}-${idx}`}
                data={{
                  type: 'file',
                  url: attachment.dataUrl ?? '',
                  mediaType: attachment.mediaType,
                  filename: attachment.name,
                }}
              />
            ))}
          </MessageAttachments>
        ) : null}

        {message.role === 'assistant' ? (
          <>
            <AssistantProcess
              done
              hasFinalText={plan.finalText.trim().length > 0}
              items={plan.processItems}
              stalled={false}
              streaming={false}
            />
            <FinalAssistantText
              stalled={false}
              streaming={false}
              text={plan.finalText}
            />
            {plan.metaParts.map((part) => (
              <div
                key={`${part.label}-${part.value}`}
                className="text-muted-foreground text-xs"
              >
                {part.label}: {part.value}
              </div>
            ))}
          </>
        ) : (
          <MessageResponse
            mode="static"
            parseIncompleteMarkdown={false}
            className={cn('max-w-full overflow-hidden break-words')}
          >
            {messageText}
          </MessageResponse>
        )}

        {message.role === 'assistant' && copyText ? (
          <MessageToolbar>
            <MessageActions>
              <MessageAction tooltip="Copy" onClick={handleCopy}>
                <Copy className="size-3.5" />
              </MessageAction>
            </MessageActions>
            {message.costUsd ? (
              <span className="text-[11px] text-muted-foreground/50 tabular-nums">
                {formatCost(message.costUsd)}
              </span>
            ) : null}
          </MessageToolbar>
        ) : null}
      </MessageContent>
    </Message>
  )
}
