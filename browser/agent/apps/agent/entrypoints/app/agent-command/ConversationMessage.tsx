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
  processItems: ProcessItem[]
  finalText: string
}

/**
 * Build the render plan for an assistant turn. Only the final text part is
 * treated as the answer; all reasoning, tools, and earlier text stay in the
 * collapsible process record.
 */
function buildAssistantRenderPlan(
  turn: AgentConversationTurn,
): AssistantRenderPlan {
  const processItems: ProcessItem[] = []
  const finalTextIndex = messageFlowFinalTextIndex(turn.parts, (part) =>
    part.kind === 'text' ? 'text' : 'process',
  )
  let finalText = ''

  turn.parts.forEach((part, partIndex) => {
    if (part.kind === 'thinking') {
      processItems.push({
        id: `${partIndex}-thinking`,
        kind: 'text',
        text: part.text,
        muted: true,
      })
      return
    }

    if (part.kind === 'text') {
      if (partIndex === finalTextIndex) {
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

    if (part.kind === 'tool-batch') {
      processItems.push(
        ...part.tools.map((tool) => ({
          id: `${partIndex}-${tool.id}`,
          kind: 'tool' as const,
          tool: toProcessTool(tool),
        })),
      )
    }
  })

  return { processItems, finalText }
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
  const { processItems, finalText } = useMemo(
    () => buildAssistantRenderPlan(turn),
    [turn],
  )
  const isLiveStreaming = streaming && !turn.done
  const stalled = useStalledTextIndicator({
    text: finalText,
    streaming: isLiveStreaming,
    lastTextDeltaAt: turn.lastTextDeltaAt,
  })
  const hasAssistantContent =
    !turn.done || processItems.length > 0 || finalText.trim().length > 0

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
            <AssistantProcess
              done={turn.done}
              hasFinalText={finalText.trim().length > 0}
              items={processItems}
              stalled={stalled}
              streaming={isLiveStreaming}
            />
            <FinalAssistantText
              stalled={stalled}
              streaming={isLiveStreaming}
              text={finalText}
            />
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
