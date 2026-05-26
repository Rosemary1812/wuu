import { describe, expect, it } from 'bun:test'
import type { AgentConversationTurn } from '@/lib/agent-conversations/types'
import { buildAssistantRenderPlan } from './ConversationMessage'

const baseTurn: AgentConversationTurn = {
  id: 'turn-1',
  userText: 'Check this',
  parts: [],
  done: false,
  timestamp: 0,
}

describe('ConversationMessage render plan', () => {
  it('keeps live assistant text and tools in chronological order', () => {
    const turn: AgentConversationTurn = {
      ...baseTurn,
      parts: [
        { kind: 'text', text: 'I will inspect it.' },
        {
          kind: 'tool-batch',
          tools: [
            {
              id: 'tool-1',
              name: 'read_file',
              label: 'Read file',
              status: 'completed',
            },
          ],
        },
        { kind: 'text', text: 'The result is clear.' },
      ],
    }

    expect(buildAssistantRenderPlan(turn, true).blocks).toEqual([
      { id: '0-text', kind: 'text', text: 'I will inspect it.' },
      {
        id: '1-tool-1-process',
        kind: 'process',
        items: [
          {
            id: '1-tool-1',
            kind: 'tool',
            tool: {
              id: 'tool-1',
              name: 'read_file',
              label: 'Read file',
              status: 'completed',
            },
          },
        ],
      },
      { id: '2-text', kind: 'text', text: 'The result is clear.' },
    ])
  })

  it('collapses non-final text into process after the turn is done', () => {
    const turn: AgentConversationTurn = {
      ...baseTurn,
      done: true,
      parts: [
        { kind: 'text', text: 'I will inspect it.' },
        {
          kind: 'tool-batch',
          tools: [
            {
              id: 'tool-1',
              name: 'read_file',
              label: 'Read file',
              status: 'completed',
            },
          ],
        },
        { kind: 'text', text: 'The result is clear.' },
      ],
    }

    const plan = buildAssistantRenderPlan(turn, false)

    expect(plan.blocks.map((block) => block.kind)).toEqual([
      'process',
      'text',
    ])
    expect(plan.finalText).toBe('The result is clear.')
  })
})
