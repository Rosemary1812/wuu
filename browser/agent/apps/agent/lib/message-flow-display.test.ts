import { expect, test } from 'bun:test'
import {
  formatMessageFlowCommand,
  isMessageFlowFailedStatus,
  messageFlowFinalTextIndex,
  messageFlowStatusLabel,
} from './message-flow-display'

test('messageFlowStatusLabel keeps browser and desktop labels in one model', () => {
  expect(
    messageFlowStatusLabel({ done: false, failed: false, hasFinalText: false }),
  ).toBe('Working')
  expect(
    messageFlowStatusLabel({ done: false, failed: false, hasFinalText: true }),
  ).toBe('Replying')
  expect(
    messageFlowStatusLabel({
      done: false,
      failed: false,
      hasFinalText: true,
      stalled: true,
    }),
  ).toBe('Still generating')
  expect(
    messageFlowStatusLabel({ done: true, failed: false, hasFinalText: true }),
  ).toBe('Activity log')
  expect(
    messageFlowStatusLabel({ done: true, failed: true, hasFinalText: true }),
  ).toBe('Activity failed')

  expect(
    messageFlowStatusLabel({
      done: false,
      failed: false,
      hasFinalText: false,
      locale: 'zh',
    }),
  ).toBe('正在处理')
  expect(
    messageFlowStatusLabel({
      done: false,
      failed: false,
      hasFinalText: true,
      locale: 'zh',
    }),
  ).toBe('正在回复')
  expect(
    messageFlowStatusLabel({
      done: false,
      failed: false,
      hasFinalText: true,
      stalled: true,
      locale: 'zh',
    }),
  ).toBe('仍在生成')
  expect(
    messageFlowStatusLabel({
      done: true,
      failed: false,
      hasFinalText: true,
      locale: 'zh',
    }),
  ).toBe('过程记录')
  expect(
    messageFlowStatusLabel({
      done: true,
      failed: true,
      hasFinalText: true,
      locale: 'zh',
    }),
  ).toBe('过程失败')
})

test('formatMessageFlowCommand prefers raw input and falls back predictably', () => {
  expect(
    formatMessageFlowCommand({
      name: 'read_file',
      input: { path: 'package.json' },
    }),
  ).toBe('read_file {"path":"package.json"}')
  expect(
    formatMessageFlowCommand({ name: 'run_shell', input: 'bun test' }),
  ).toBe('run_shell bun test')
  expect(
    formatMessageFlowCommand({ name: 'grep', subject: 'message-flow' }),
  ).toBe('grep message-flow')
  expect(formatMessageFlowCommand({ name: 'tool', label: 'Tool' })).toBe(
    'tool Tool',
  )
  expect(formatMessageFlowCommand({ name: 'tool', label: 'tool' })).toBe('tool')
})

test('messageFlowFinalTextIndex selects text after the last process item', () => {
  const parts = [
    { role: 'text' },
    { role: 'process' },
    { role: 'text' },
    { role: 'ignore' },
  ] as const

  expect(messageFlowFinalTextIndex(parts, (part) => part.role)).toBe(2)
})

test('messageFlowFinalTextIndex rejects text before later process work', () => {
  const parts = [{ role: 'text' }, { role: 'process' }] as const

  expect(messageFlowFinalTextIndex(parts, (part) => part.role)).toBe(-1)
})

test('isMessageFlowFailedStatus recognizes failed tool states', () => {
  expect(isMessageFlowFailedStatus('failed')).toBe(true)
  expect(isMessageFlowFailedStatus('error')).toBe(true)
  expect(isMessageFlowFailedStatus('completed')).toBe(false)
  expect(isMessageFlowFailedStatus(undefined)).toBe(false)
})
