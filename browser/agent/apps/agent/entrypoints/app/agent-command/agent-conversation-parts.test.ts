import { describe, expect, it } from 'bun:test'
import type { AssistantPart } from '@/lib/agent-conversations/types'
import {
  appendAssistantTextDelta,
  appendAssistantThinkingDelta,
} from './agent-conversation-parts'

const toolBatch: AssistantPart = {
  kind: 'tool-batch',
  tools: [
    {
      id: 'tool-1',
      name: 'read_file',
      label: 'Read file',
      status: 'completed',
    },
  ],
}

describe('agent conversation parts', () => {
  it('combines adjacent output deltas into one text part', () => {
    let parts: AssistantPart[] = []
    parts = appendAssistantTextDelta(parts, 'Hel')
    parts = appendAssistantTextDelta(parts, 'lo')

    expect(parts).toEqual([{ kind: 'text', text: 'Hello' }])
  })

  it('starts a new text part after process work instead of replaying prior text', () => {
    let parts: AssistantPart[] = []
    parts = appendAssistantTextDelta(parts, 'I will inspect it.')
    parts = [...parts, toolBatch]
    parts = appendAssistantTextDelta(parts, 'The fix is ready.')

    expect(parts).toEqual([
      { kind: 'text', text: 'I will inspect it.' },
      toolBatch,
      { kind: 'text', text: 'The fix is ready.' },
    ])
  })

  it('keeps later thinking deltas in chronological order after process work', () => {
    let parts: AssistantPart[] = []
    parts = appendAssistantThinkingDelta(parts, 'Looking at the files.')
    parts = [...parts, toolBatch]
    parts = appendAssistantThinkingDelta(parts, 'Checking the result.')

    expect(parts).toEqual([
      { kind: 'thinking', text: 'Looking at the files.', done: false },
      toolBatch,
      { kind: 'thinking', text: 'Checking the result.', done: false },
    ])
  })
})
