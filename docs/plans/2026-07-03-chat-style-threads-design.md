# Chat-Style DM & Group Threads — Design

Status: approved direction (2026-07-03). Extends
`docs/plans/2026-07-03-resident-named-agents.md`.

## 1. Product model

Three thread kinds, each with a fixed rendering mode decided at creation:

| Kind | Detection | Rendering |
|---|---|---|
| DM | `dm_participant_id` non-empty | Chat view |
| Group chat | `group: true` (new wire field) | Chat view |
| Work session | neither | Existing transcript view (unchanged) |

Sidebar gains a 群聊 section between 置顶 and 对话:

```
置顶
群聊
  # all          ← default channel, whole roster is implicitly member
  # <group…>
对话             ← unchanged work sessions
<projects…>
```

## 2. Chat view semantics

- **Only tool messages are messages.** The chat stream renders exactly:
  user messages (`user_message`), participant messages posted via the
  `post_message` / `decline` tools (`participant_message`), and
  envelope meta rows (`envelope_meta` user messages, collapsed).
  Everything else — agent final answers, thinking, tool calls, plans,
  diffs — is skipped entirely (not hidden via CSS; not in the DOM).
  This applies to DM too: a resident replies in its DM by calling
  `post_message` with no `thread_id`. (Adjudicated 2026-07-03: the
  resident design doc §2/§4.5/§5/§6 now specifies this same contract —
  assistant text is never the DM reply channel.)
- **No process view, no escape hatch.** The chat view offers no way to
  expand the working transcript. Debugging happens elsewhere (reveal
  session file).
- **No streaming.** Tool messages are published whole; the view renders
  complete bubbles and follows the bottom on arrival.
- **Typing indicator.** While the counterpart is working the stream
  shows an avatar + three-dot indicator row:
  - DM: thread status running (same signal that drives the roster busy
    dot).
  - Group: any member whose resident DM thread is running shows its own
    indicator row (reuses `busyDMParticipantIDs`).
- **Visual continuity.** Chat view lives inside the same
  `conversation-width` shell, uses the existing design tokens, the
  existing user-bubble palette on the right, and participant bubbles
  with avatars on the left. It must feel like the same app, not an
  embedded messenger.

## 3. Group thread runtime contract (backend work)

Group threads have **no main agent**. They are envelope routers:

1. **Wire field** — `Thread.group bool` (`json:"group,omitempty"`), set
   once at creation. `thread/start` accepts `{"group": true,
   "title"?: string}`.
2. **`#all` channel** — the server ensures a group thread titled `all`
   exists (idempotent, created on demand). Its membership is implicit:
   every named participant in the roster is a member; explicit
   `thread_members` rows are not required for routing in `all`.
3. **User turns don't run a provider.** `turn/start` on a `group`
   thread records the user message, routes envelopes to members
   (mentioned → `addressed: true`), and completes the turn immediately
   with no model call. Mentions continue to use the existing
   `mentions: []string` param and `AddThreadMember` semantics for
   non-`all` groups.
4. **Members serialization** — group threads serialize
   `members: []ParticipantSummary` on the wire `Thread` (the frontend
   chips UI and chat avatars read it). For `all`, members mirror the
   roster.
5. **envelope_meta alignment** — `envelopeMetaRecord` gains
   `source_thread_title string json:"source_thread_title,omitempty"`
   so envelope rows in DM chat can name the source group. The wire
   shape stays an **array** of records; the frontend adapts to the
   array (see §5).

Update (2026-07-03): agent-driven group management is no longer just
direction — the tool contract (`create_group` / `add_group_member`,
granted to all residents) and the default team-builder agent Andy are
specified in `2026-07-03-sidebar-groups-andy-workspaces.md` §3-§4.
Sidebar-wise, that doc §2 also revises §1 above: the 群聊 section joins
`sectionOrder` (reorderable, collapsible, hover-+ to create a group)
instead of being anchored between 置顶 and 对话.

## 4. Frontend architecture

New `ChatThreadView` component, a sibling of `ConversationTurnList`,
selected in `App.tsx` by thread kind. It flattens `thread.turns` into a
message list with a whitelist filter, so the transcript pipeline
(`TurnView` / `ThreadItemView`) is untouched — zero regression surface
for work sessions.

- Message row: 28px round avatar (participant `avatar_image` data URL,
  else emoji `avatar`, else initial) + name line + bubble. User rows
  right-aligned, reusing the user-message palette, no avatar.
  `decline` post_kind renders as a muted inline line, not a bubble.
- Auto-follow: scroll to bottom when the message count grows and the
  user is already near the bottom.
- Composer: the existing `ComposerView` (with @-mention autocomplete)
  is reused as-is.

## 5. Phasing

- **Phase 1 (frontend only, no backend dependency):**
  1. Adapt `EnvelopeMeta` to the backend's array shape.
  2. `ChatThreadView` + chat CSS + typing indicator.
  3. DM threads render via `ChatThreadView`.
- **Phase 2 (after backend lands §3):** sidebar 群聊 section, `# all`,
  new-group creation, group chat rendering, member typing indicators.
  The UI is written but gated on data: with no `group` threads present
  the section shows only a disabled `# all` placeholder.
