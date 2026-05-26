import { type FC, useMemo } from 'react'
import {
  Message,
  MessageAttachment,
  MessageAttachments,
  MessageContent,
} from '@/components/ai-elements/message'
import type {
  AgentConversationTurn,
  ToolEntry,
} from '@/lib/agent-conversations/types'
import { messageFlowFinalTextIndex } from '@/lib/message-flow-display'
import { FileCardStrip } from './agent-conversation.file-card-strip'
import {
  AssistantProcess,
  FinalAssistantText,
  type ProcessItem,
  useStalledTextIndicator,
} from './agent-message-flow'

interface ConversationMessageProps {
  turn: AgentConversationTurn
  streaming: boolean
  /**
   * Forwarded to the inline file-card strip's "View" / "+N"
   * button. Wired up by AgentCommandConversation so the strip can
   * deep-link straight into the Outputs rail at the matching turn
   * group. `null` here disables the strip's deep-link affordance
   * — the cards still open the preview Sheet directly.
   */
  onOpenOutputsRail?: ((turnId?: string | null) => void) | null
  /**
   * Render only the trailing FileCardStrip for this turn — used
   * when the turn's user / assistant text is already rendered
   * elsewhere (e.g. by `ClawChatMessage` from persisted history)
   * but the produced-files affordance would otherwise be lost.
   */
  stripOnly?: boolean
}

interface AssistantRenderPlan {
  blocks: AssistantRenderBlock[]
  finalText: string
  hasText: boolean
  latestTextBlockId?: string
}

type AssistantRenderBlock =
  | {
      id: string
      kind: 'process'
      items: ProcessItem[]
    }
  | {
      id: string
      kind: 'text'
      text: string
    }

export function buildAssistantRenderPlan(
  turn: AgentConversationTurn,
  liveTimeline: boolean,
): AssistantRenderPlan {
  const blocks: AssistantRenderBlock[] = []
  let pendingProcessItems: ProcessItem[] = []
  const finalTextIndex = liveTimeline
    ? -1
    : messageFlowFinalTextIndex(turn.parts, (part) =>
        part.kind === 'text' ? 'text' : 'process',
      )
  let finalText = ''
  let hasText = false
  let latestTextBlockId: string | undefined

  const flushProcessItems = () => {
    if (pendingProcessItems.length === 0) return
    blocks.push({
      id: `${pendingProcessItems[0]?.id ?? blocks.length}-process`,
      kind: 'process',
      items: pendingProcessItems,
    })
    pendingProcessItems = []
  }

  turn.parts.forEach((part, partIndex) => {
    if (part.kind === 'thinking') {
      pendingProcessItems.push({
        id: `${partIndex}-thinking`,
        kind: 'text',
        text: part.text,
        muted: true,
      })
      return
    }

    if (part.kind === 'text') {
      hasText = hasText || part.text.trim().length > 0
      if (liveTimeline || partIndex === finalTextIndex) {
        flushProcessItems()
        const id = `${partIndex}-text`
        blocks.push({ id, kind: 'text', text: part.text })
        latestTextBlockId = id
      }
      if (partIndex === finalTextIndex) {
        finalText = part.text
      } else if (!liveTimeline) {
        pendingProcessItems.push({
          id: `${partIndex}-text`,
          kind: 'text',
          text: part.text,
          muted: true,
        })
      }
      return
    }

    if (part.kind === 'tool-batch') {
      pendingProcessItems.push(
        ...part.tools.map((tool) => ({
          id: `${partIndex}-${tool.id}`,
          kind: 'tool' as const,
          tool: toProcessTool(tool),
        })),
      )
    }
  })

  flushProcessItems()

  return { blocks, finalText, hasText, latestTextBlockId }
}

function toProcessTool(tool: ToolEntry) {
  return {
    id: tool.id,
    name: tool.name,
    label: tool.label,
    subject: tool.subject,
    status: tool.status,
    input: tool.input,
    error: tool.error,
    durationMs: tool.durationMs,
  }
}

export const ConversationMessage: FC<ConversationMessageProps> = ({
  turn,
  streaming,
  onOpenOutputsRail,
  stripOnly,
}) => {
  const isLiveStreaming = streaming && !turn.done
  const { blocks, finalText, hasText, latestTextBlockId } = useMemo(
    () => buildAssistantRenderPlan(turn, isLiveStreaming),
    [isLiveStreaming, turn],
  )
  const latestText = useMemo(() => {
    for (let index = blocks.length - 1; index >= 0; index -= 1) {
      const block = blocks[index]
      if (block.kind === 'text') return block.text
    }
    return finalText
  }, [blocks, finalText])
  const stalled = useStalledTextIndicator({
    text: latestText,
    streaming: isLiveStreaming,
    lastTextDeltaAt: turn.lastTextDeltaAt,
  })
  const hasAssistantContent =
    !turn.done || blocks.length > 0 || finalText.trim().length > 0

  if (stripOnly) {
    if (!turn.producedFiles || turn.producedFiles.length === 0) return null
    return (
      <FileCardStrip
        turnId={turn.turnId ?? null}
        files={turn.producedFiles}
        onOpenRail={onOpenOutputsRail ?? (() => {})}
      />
    )
  }

  return (
    <div className="space-y-3">
      <Message from="user">
        <MessageContent>
          {turn.userAttachments && turn.userAttachments.length > 0 && (
            <MessageAttachments>
              {turn.userAttachments.map((attachment) => (
                <MessageAttachment
                  key={attachment.id}
                  data={{
                    type: 'file',
                    url: attachment.dataUrl ?? '',
                    mediaType: attachment.mediaType,
                    filename: attachment.name,
                  }}
                />
              ))}
            </MessageAttachments>
          )}
          {turn.userText && (
            <pre className="whitespace-pre-wrap font-sans text-sm">
              {turn.userText}
            </pre>
          )}
        </MessageContent>
      </Message>

      {hasAssistantContent ? (
        <Message from="assistant">
          <MessageContent className="w-full gap-3">
            {blocks.length === 0 && !turn.done ? (
              <AssistantProcess
                done={false}
                hasFinalText={false}
                items={[]}
                stalled={stalled}
                streaming={isLiveStreaming}
              />
            ) : null}
            {blocks.map((block) =>
              block.kind === 'process' ? (
                <AssistantProcess
                  key={block.id}
                  done={turn.done}
                  hasFinalText={hasText || finalText.trim().length > 0}
                  items={block.items}
                  stalled={stalled}
                  streaming={isLiveStreaming}
                />
              ) : (
                <FinalAssistantText
                  key={block.id}
                  stalled={stalled && block.id === latestTextBlockId}
                  streaming={isLiveStreaming && block.id === latestTextBlockId}
                  text={block.text}
                />
              ),
            )}
          </MessageContent>
        </Message>
      ) : null}

      {turn.producedFiles && turn.producedFiles.length > 0 ? (
        <FileCardStrip
          turnId={turn.turnId ?? null}
          files={turn.producedFiles}
          onOpenRail={onOpenOutputsRail ?? (() => {})}
        />
      ) : null}
    </div>
  )
}
