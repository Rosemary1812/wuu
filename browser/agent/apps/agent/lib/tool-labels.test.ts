import { expect, test } from 'bun:test'
import { buildToolLabel } from './tool-labels'

test('buildToolLabel labels current filesystem tools for activity display', () => {
  expect(
    buildToolLabel('filesystem_grep', { pattern: 'ThreadItemView' }),
  ).toEqual({
    label: 'Searched code',
    subject: 'ThreadItemView',
  })
  expect(buildToolLabel('filesystem_read', { path: 'src/App.tsx' })).toEqual({
    label: 'Read file',
    subject: 'App.tsx',
  })
  expect(
    buildToolLabel('filesystem_bash', { command: 'npm run typecheck' }),
  ).toEqual({
    label: 'Ran',
    subject: 'npm run typecheck',
  })
})
