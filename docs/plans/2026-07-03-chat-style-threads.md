# Chat-Style DM & Group Threads Implementation Plan (Frontend)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Render DM threads (and, once the backend lands, group threads) as a chat-style message stream — avatars, per-speaker bubbles, tool-messages-only — while keeping work sessions on the existing transcript view.

**Architecture:** New `ChatThreadView` component sibling to `ConversationTurnList`, selected in `App.tsx` by thread kind (`dm_participant_id` / `group`). It flattens turns into a whitelist-filtered message list (user messages, `participant_message`, envelope meta rows); everything else never enters the DOM. No streaming: tool messages arrive whole. Design: `docs/plans/2026-07-03-chat-style-threads-design.md`. Backend contract for group threads: same doc §3 (implemented by the other machine; frontend Phase 2 is data-gated).

**Tech Stack:** React + TypeScript (desktop/), vitest + jsdom, design tokens in `desktop/src/renderer/styles/`.

All commands run from `desktop/`. Follow the user TypeScript skill (no `!` assertions, explicit return types on exports). Comments and commit messages in English; UI copy in Chinese.

---

### Task 1: Align `EnvelopeMeta` with the backend's array shape

The backend serializes `envelope_meta` as an **array** of records
(`internal/appserver/resident_router.go`, `envelopeMetaRecord`), one per
coalesced envelope: `{id?, source_thread_id, addressed, hop,
sender_participant_id?, created_at?}`. The current frontend type is a single
object with `source_thread_title` / `message_count` — wrong shape, so
`EnvelopeNotice` silently falls back today. Adapt the frontend; count =
array length. (`source_thread_title` stays in the type: the backend will
add it per the revised §3.3 contract; render falls back gracefully until
then.)

**Files:**
- Modify: `src/shared/protocol.ts` (~line 1055, `EnvelopeMeta` type; ~1088 `ThreadItem.envelope_meta`)
- Modify: `src/renderer/EnvelopeNotice.tsx`
- Test: `src/renderer/EnvelopeNotice.test.tsx`

**Step 1: Update the tests to the array shape**

In `EnvelopeNotice.test.tsx`, change every `meta` fixture from an object to
an array of records, e.g.:

```tsx
meta: [
  {
    source_thread_id: "thr-1",
    source_thread_title: "发布排期",
    addressed: true,
    hop: 1,
  },
  { source_thread_id: "thr-1", addressed: false, hop: 1 },
]
```

Assert: label reads `收到来自「发布排期」的 2 条消息` (title from the first
record that has one; count = array length; single-element array renders
`收到来自「…」的消息` without a count). Empty array / no titled record →
`收到来自其他会话的消息`.

**Step 2: Run to verify failure**

Run: `npx vitest run src/renderer/EnvelopeNotice.test.tsx`
Expected: FAIL (type errors / wrong labels).

**Step 3: Change the types**

In `protocol.ts` replace the `EnvelopeMeta` object type:

```ts
// One record per envelope coalesced into this user message. Mirrors the
// backend's envelopeMetaRecord array (resident-named-agents.md §3.3,
// 2026-07-03 revision). Message count == array length.
export type EnvelopeMetaRecord = {
  id?: string;
  source_thread_id?: string;
  // Snapshot of the source thread title at write time (backend fills it
  // per the revised contract; may be absent on older rows).
  source_thread_title?: string;
  addressed?: boolean;
  hop?: number;
  sender_participant_id?: string;
  created_at?: string;
};
export type EnvelopeMeta = EnvelopeMetaRecord[];
```

`ThreadItem.envelope_meta?: EnvelopeMeta;` stays.

**Step 4: Adapt `EnvelopeNotice`**

```tsx
const records = meta;
const title = records
  .map((record) => record.source_thread_title?.trim() ?? "")
  .find((candidate) => candidate !== "");
const source = title ? `「${title}」` : "其他会话";
const label =
  records.length > 1
    ? `收到来自${source}的 ${records.length} 条消息`
    : `收到来自${source}的消息`;
```

Guard rendering: `ThreadItemView.tsx:100` checks `item.envelope_meta` —
change to `item.envelope_meta && item.envelope_meta.length > 0` so an
empty array still renders as a normal user bubble.

**Step 5: Verify, typecheck, commit**

Run: `npx vitest run src/renderer/EnvelopeNotice.test.tsx && npm run typecheck`
Expected: PASS.

```bash
git add src/shared/protocol.ts src/renderer/EnvelopeNotice.tsx src/renderer/EnvelopeNotice.test.tsx src/renderer/ThreadItemView.tsx
git commit -m "fix: align envelope_meta with backend array shape"
```

---

### Task 2: Chat message extraction helper

Pure function that flattens `Turn[]` into chat rows. Whitelist filter — the
core "tool messages only" semantics live here, testable without DOM.

**Files:**
- Modify: `src/renderer/AppState.ts` (new export near `busyDMParticipantIDs`, ~line 1592)
- Test: `src/renderer/AppState.test.ts`

**Step 1: Write failing tests**

Add a `describe("chatMessagesFromTurns", ...)` block:

- user_message → `{kind: "user", text, images, files}` row.
- user_message with non-empty `envelope_meta` → `{kind: "envelope"}` row.
- participant_message → `{kind: "participant", participant, postKind, text}` row.
- agent_message / tool_call / plan / thinking items → **dropped**.
- decline participant_message (`post_kind: "decline"`) still surfaces (view renders it muted).
- Row ids are stable: `${turn.id}:${item.id}`.

**Step 2: Run to verify failure**

Run: `npx vitest run src/renderer/AppState.test.ts`
Expected: FAIL "chatMessagesFromTurns is not a function".

**Step 3: Implement**

```ts
export type ChatMessageRow =
  | { kind: "user"; id: string; turnID: string; item: ThreadItem }
  | { kind: "envelope"; id: string; turnID: string; item: ThreadItem }
  | { kind: "participant"; id: string; turnID: string; item: ThreadItem };

/**
 * Flatten a thread's turns into the chat-view message stream. Whitelist
 * semantics (chat-style-threads-design.md §2): only user messages,
 * envelope meta rows, and tool-posted participant messages are chat
 * messages; the agent's working transcript never reaches the DOM.
 */
export function chatMessagesFromTurns(
  turns: ReadonlyArray<Pick<Turn, "id" | "items">>,
): ChatMessageRow[] {
  const rows: ChatMessageRow[] = [];
  for (const turn of turns) {
    for (const item of turn.items ?? []) {
      const id = `${turn.id}:${item.id}`;
      if (item.type === "user_message") {
        if (item.envelope_meta && item.envelope_meta.length > 0) {
          rows.push({ kind: "envelope", id, turnID: turn.id, item });
        } else {
          rows.push({ kind: "user", id, turnID: turn.id, item });
        }
      } else if (item.type === "participant_message") {
        rows.push({ kind: "participant", id, turnID: turn.id, item });
      }
    }
  }
  return rows;
}
```

(Check the actual `Turn` items field name in `protocol.ts` before writing —
use whatever `TurnView` iterates.)

**Step 4: Verify + commit**

Run: `npx vitest run src/renderer/AppState.test.ts && npm run typecheck`

```bash
git add src/renderer/AppState.ts src/renderer/AppState.test.ts
git commit -m "feat: add chat message extraction for chat-style threads"
```

---

### Task 3: `ChatThreadView` component + styles

**Files:**
- Create: `src/renderer/ChatThreadView.tsx`
- Create: `src/renderer/styles/chat.css` (import from `styles.css` next to `participants.css`)
- Test: `src/renderer/ChatThreadView.test.tsx` (mirror the render-harness pattern of `EnvelopeNotice.test.tsx`)

**Step 1: Write failing tests**

- Renders a participant row with avatar (emoji from `participant.avatar`,
  or first character of name as fallback; `avatar_image` data URL renders
  an `<img>`), name line, and bubble text.
- Renders user rows right-aligned (`.chat-row--user`), no avatar.
- Renders envelope rows via `EnvelopeNotice`.
- `post_kind === "decline"` renders `.chat-decline-line` (muted, no bubble).
- `typingParticipants` prop non-empty → `.chat-typing-row` per participant
  with three-dot indicator and `aria-label` `{name} 正在输入`.
- Transcript items passed in turns never appear (feed a turn containing an
  `agent_message` and assert its text is absent).

**Step 2: Run to verify failure**

Run: `npx vitest run src/renderer/ChatThreadView.test.tsx`

**Step 3: Implement the component**

Props:

```tsx
export function ChatThreadView({
  turns,
  typingParticipants,
}: {
  turns: ReadonlyArray<Pick<Turn, "id" | "items">>;
  typingParticipants: ReadonlyArray<ParticipantSummary>;
}): JSX.Element
```

- `const rows = useMemo(() => chatMessagesFromTurns(turns), [turns]);`
- Layout: `<div className="chat-thread">` list; each row:
  - participant: avatar column (28px circle) + `<div className="chat-bubble-group">` with `.chat-sender-name` + `.chat-bubble` (`StreamingMarkdown` NOT needed — plain markdown render; reuse whatever `participant-message-card` body uses today, see `ThreadItemView.tsx:257`).
  - user: `.chat-row--user`, bubble reuses `--user-message-background` token; render images/files the way `UserMessageBubble` does if cheap, else text only for v1.
  - envelope: reuse `<EnvelopeNotice meta={...} text={...} />`.
- Auto-follow: `useEffect` on `rows.length` — if container scrolled near
  bottom (within 120px), `scrollTo(bottom)`. Container ref on
  `.chat-thread`.
- Typing rows appended after messages: avatar + three animated dots
  (`.chat-typing-dot` ×3, CSS keyframe opacity pulse).

**Step 4: `chat.css`**

Tokens only (`--ink-*`, `--user-message-background`, existing radii/shadows
from `turns.css` / `participants.css`). Key classes: `.chat-thread`
(column flex, gap 10px), `.chat-row` (grid `28px 1fr`, gap 8px),
`.chat-row--user` (justify-end, no avatar column), `.chat-avatar`,
`.chat-sender-name` (12px, `--ink-muted`), `.chat-bubble` (max-width
~72%, padding 8px 12px, radius 12px), `.chat-decline-line`
(`--ink-muted`, italic), `.chat-typing-dot` animation. Match the feel of
the session view — same font, same ink colors, no new palette.

**Step 5: Verify + commit**

Run: `npx vitest run src/renderer/ChatThreadView.test.tsx && npm run typecheck`

```bash
git add src/renderer/ChatThreadView.tsx src/renderer/ChatThreadView.test.tsx src/renderer/styles/chat.css src/renderer/styles.css
git commit -m "feat: add chat-style thread message view"
```

---

### Task 4: DM threads render `ChatThreadView`

**Files:**
- Modify: `src/renderer/App.tsx` (~line 7620, cached-conversation-pane render — the `ConversationTurnList` mount)

**Step 1: Wire the switch**

In the pane render, when `isDMThread(thread)`:

```tsx
{isDMThread(thread) ? (
  <ChatThreadView
    turns={threadTurns}
    typingParticipants={dmTypingParticipants}
  />
) : (
  <ConversationTurnList ... />
)}
```

`dmTypingParticipants`: when the DM thread is running
(`thread.status === "in_progress"` or an in-progress turn — reuse the
qualification in `busyDMParticipantIDs`, AppState.ts:1592), resolve the
participant summary from `participants` by `thread.dm_participant_id`;
else `[]`. Keep the composer, header, and everything else in the pane
unchanged.

**Step 2: Manual verification (no dev-server automation available — do this by hand or note it)**

`npm run typecheck && npm test` must pass. Then run the app, open a DM
from the roster, send a message: only your bubble + typing indicator show
while the resident works; its `post_message` reply appears as an
avatar bubble; no transcript items leak. Note in the commit if manual UI
verification was not possible.

**Step 3: Commit**

```bash
git add src/renderer/App.tsx
git commit -m "feat: render DM threads as chat-style message streams"
```

---

### Task 5: `Thread.group` wire field + sidebar 群聊 section (data-gated Phase 2 UI)

**Files:**
- Modify: `src/shared/protocol.ts` (Thread type, next to `dm_participant_id` ~line 734)
- Modify: `src/renderer/AppState.ts` (`isGroupThread` predicate next to `isDMThread` ~1512; exclude group threads from `scratchThreadSummaries` the way DMs are excluded)
- Modify: `src/renderer/App.tsx` + `src/renderer/ThreadSidebar.tsx` (sidebar sections)
- Test: `src/renderer/AppState.test.ts`

**Step 1: Types + predicate (TDD on the predicate)**

```ts
// protocol.ts — Thread
// group marks this thread as a chat-style group channel with no main
// agent (chat-style-threads-design.md §3). Set once at creation.
group?: boolean;
```

`isGroupThread(thread: { group?: boolean }): boolean`. Tests: true/false/
undefined; `scratchThreadSummaries` drops group threads.

**Step 2: Sidebar section**

Between 置顶 and 对话 render a 群聊 section listing
`state.threads.filter(isGroupThread)` (reuse the summary row component the
对话 section uses). When empty, render a single disabled row `# all`
with `title="等待后端支持群聊"` — the section is visible so the feature
is discoverable, but inert until the backend ships `group` threads.
Group thread titles render with a `#` prefix in the sidebar.

**Step 3: Group threads use `ChatThreadView`**

Extend the Task 4 switch: `isDMThread(thread) || isGroupThread(thread)`.
`typingParticipants` for groups = members whose IDs are in
`busyDMParticipantIDs(state.threads)` (both already exist).

**Step 4: Verify + commit**

Run: `npx vitest run src/renderer/AppState.test.ts && npm run typecheck && npm test`

```bash
git add src/shared/protocol.ts src/renderer/AppState.ts src/renderer/AppState.test.ts src/renderer/App.tsx src/renderer/ThreadSidebar.tsx
git commit -m "feat: add group chat sidebar section gated on backend group threads"
```

---

### Task 6: Full suite + push

Run: `npm run typecheck && npm test`
Expected: all green (baseline was 72 files / 742 tests before this plan).

```bash
git push origin main
```

Backend follow-ups (other machine, per design doc §3): `Thread.group` +
`thread/start {group}`, `#all` ensure, group `turn/start` with no
provider call, `members` serialization, `envelopeMetaRecord.source_thread_title`.
