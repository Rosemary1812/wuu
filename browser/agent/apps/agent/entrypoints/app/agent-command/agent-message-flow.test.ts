import { describe, expect, it } from 'bun:test'
import {
  decorateStalledMarkdownTail,
  formatToolCommand,
} from './agent-message-flow'

describe('agent-message-flow', () => {
  it('formats tool commands with raw input when available', () => {
    expect(
      formatToolCommand({
        name: 'read_file',
        label: 'Read file',
        status: 'completed',
        input: { path: 'package.json' },
      }),
    ).toBe('Read file package.json')
  })

  it('marks the latest readable text tail during a stalled stream', () => {
    expect(decorateStalledMarkdownTail('The answer is almost ready')).toBe(
      'The answer is almost <span>ready</span>',
    )
  })

  it('does not mark markdown syntax as a stalled stream tail', () => {
    expect(decorateStalledMarkdownTail('Use `npm test`')).toBe('Use `npm test`')
  })
})
