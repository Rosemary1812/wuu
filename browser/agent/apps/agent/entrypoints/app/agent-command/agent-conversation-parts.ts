import type { AssistantPart } from '@/lib/agent-conversations/types'

export function appendAssistantTextDelta(
  parts: AssistantPart[],
  delta: string,
): AssistantPart[] {
  if (!delta) {
    return parts
  }
  const last = parts[parts.length - 1]
  if (last?.kind === 'text') {
    return [...parts.slice(0, -1), { ...last, text: `${last.text}${delta}` }]
  }
  return [...parts, { kind: 'text', text: delta }]
}

export function appendAssistantThinkingDelta(
  parts: AssistantPart[],
  delta: string,
): AssistantPart[] {
  if (!delta) {
    return parts
  }
  const last = parts[parts.length - 1]
  if (last?.kind === 'thinking' && !last.done) {
    return [
      ...parts.slice(0, -1),
      { ...last, text: `${last.text}${delta}`, done: false },
    ]
  }
  return [...parts, { kind: 'thinking', text: delta, done: false }]
}
