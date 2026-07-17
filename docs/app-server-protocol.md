# App-Server Protocol

The Wuu app-server protocol is the UI-neutral boundary between shells and the
Go core runtime.

Wuu has no TUI. Electron is the human shell. `wuu exec`, scripts, CI,
automation, and future IDE shells should drive Wuu through this protocol rather
than building separate agent loops.

## Transport

The current app-server transport is newline-delimited JSON over stdio.

Requests have this shape:

```json
{"id":"1","method":"initialize","params":{}}
```

Responses have this shape:

```json
{"id":"1","result":{}}
```

Errors have this shape:

```json
{"id":"1","error":{"code":"error","message":"..."}}
```

Notifications have no `id`:

```json
{"method":"turn/completed","params":{}}
```

The protocol version is reported by `initialize` as
`wuu-app-server/v0.1`.

## Required `wuu exec` Lifecycle

`wuu exec` must use the same lifecycle as Electron:

```text
initialize
thread/start or thread/resume
turn/start
consume notifications until terminal turn state
shutdown
```

It must not call `StreamRunner.RunWithCallback` directly for the target path.

`wuu exec --ultra` is an explicit, non-persistent enable override for this
lifecycle. In `--json` mode, the first `session_configured` JSONL event reports
the effective `ultra` and `max_parallel` values returned by `initialize`.

## Core Methods

This is the common subset the text entrypoint exercises end-to-end. The full
method table lives in `internal/appserver/protocol.go` (every method constant in
the `Method*` block at the top of that file is a valid JSON-RPC method, with
siblings like `config/read`, `config/model/update`, `config/general/update`,
`config/advanced/update`, `config/codex-models`, `config/provider/remove`,
`skill/list`, runtime `goal/*` controls and active summary, the rest of the
`thread/*` methods (`thread/list`, `thread/search`, `thread/pin`,
`thread/archive`, `thread/edit-message`, `thread/context-composition`,
`thread/regenerate-title`, `thread/rename`), all the `turn/*` methods
(`turn/queue`, `turn/update-queued`, `turn/dequeue`, `turn/steer`,
`turn/unsteer`), `process/list`, `process/stop`, the `mcp/*` methods
(`mcp/list`, `mcp/connect`, `mcp/disconnect`, `mcp/refresh`), and
`settings/usage`).

`initialize`

Returns provider, model, workspace, tool policy, permission summary, extension
trust summary, protocol version, and the effective `ultra` and `max_parallel`
values.

`config/read`

Returns the current runtime configuration summary. Its result includes
`ultra` and `max_parallel`.

`config/model/update`

Updates the workspace defaults for provider, model, variant/effort, and
permission mode. `model` is required for a request without `thread_id`, which
changes only the defaults inherited by threads created after the update.

When `thread_id` is present, only the explicitly provided selection fields are
applied: they are pinned to that conversation and become the workspace
defaults for future threads. Omitted selection fields inherit from the target
conversation's current selection (so `model` is optional), and no other
existing conversation changes. A permission-mode-only update, for example,
never rewrites the workspace's default model or the conversation's
variant/effort.

A targeted update that changes selection fields is rejected while that thread
owns an active turn, including when another app-server process owns its
execution lease. Model and permission mode are immutable for the admitted
turn; after it settles, the user may update that thread and the defaults for
future threads. Existing non-target threads retain their persisted selections.

The method also accepts an optional `ultra` field and returns the effective
workspace-level `ultra` and `max_parallel` values. Provider connection fields
such as `base_url`, `api_key`, and `auth_token` are always workspace provider
configuration, never thread-scoped selection; a targeted request carrying only
connection fields is processed as a workspace connection update and is allowed
while the target thread runs.

`thread/start`

Creates a new persistent conversation thread backed by normal session storage.
When called with `{"ephemeral": true}`, creates an in-memory thread that is not
written to the session store and cannot be resumed after the server exits.

`thread/resume`

Resumes an existing session. An empty session id means "most recent visible
thread" in the app-server implementation.

`thread/fork`

Creates a new thread from an existing thread, turn, or item. This is part of
the text entrypoint surface through `wuu exec fork`.

`turn/start`

Starts a user turn with prompt text and optional attachments. The turn snapshots
the target thread's persisted model and permission mode at admission. The
legacy optional `permission_mode` request field must match the thread selection;
clients change the selection through `config/model/update` before starting the
turn rather than overriding one turn in isolation.

`turn/interrupt`

Interrupts the active turn. `wuu exec` uses this for Ctrl+C and timeout
cleanup.

`shutdown`

Requests a clean app-server shutdown.

## Ultra Mode Configuration

Ultra mode is configured by `agent.ultra_mode`; it defaults to `false`.
`agent.max_parallel` controls anonymous-worker execution capacity, uses `5`
when omitted or set to zero, and is unchanged by Ultra mode. See the
[configuration model](configuration-model-zh.md) for the persistent fields.

The `ultra` member of `ConfigModelUpdateParams` is optional:

```json
{"id":"2","method":"config/model/update","params":{"ultra":true}}
```

An Ultra-only request is valid. A request may also combine `ultra` with a
provider/model update; both changes are persisted by one atomic configuration
write. Omitting `ultra` preserves the current mode. `InitializeResult`,
`ConfigReadResult`, and `ConfigModelUpdateResult` all include these readback
fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `ultra` | boolean | Current session-level Ultra setting. |
| `max_parallel` | integer | Effective anonymous-worker execution capacity after applying the default. |

### Turn boundary and inheritance

Ultra is immutable within an admitted top-level turn. A user turn snapshots
the session setting when it starts. Synthetic completion turns reuse the
effective snapshot associated with the running turn whose worker result caused
the completion; they do not switch an in-flight orchestration tree to a newer
session value.

Each worker snapshots its parent's effective value when spawned. That value is
stored with the worker and remains fixed across its lifetime, follow-up
resumption, queued execution, and descendants. Changing Ultra while a turn is
running updates session configuration and shell state only. It affects the next
user-initiated top-level turn, not the current turn or any already-spawned
subtree.

When Ultra is absent or `false`, no Ultra tool-policy block is injected and the
default worker orchestration restrictions remain in place. Clients that omit
the optional update field therefore keep pre-Ultra behavior.

## Anonymous Worker Lifecycle States

Anonymous-worker state is exposed through `Agent.status`, including in
`agent/updated` notifications. In addition to the terminal states, clients must
handle these non-terminal states:

| Status | Meaning |
| --- | --- |
| `queued` | The spawn was accepted but is waiting for worker execution capacity. It consumes no `max_parallel` slot and starts automatically when capacity opens. |
| `waiting_children` | The worker produced a final message while direct children are still non-terminal. Its result is held, it consumes no execution slot, and it resumes when child delivery arrives so it can integrate the results before one final parent delivery. |

`waiting_children` is not a completed delivery. The worker becomes terminal
only after no direct child remains live and no pending message remains to
be integrated. Parent delivery remains exactly once.

## Turn Interrupt and Tree Freeze

`turn/interrupt` means "freeze this work", not "leave background workers
running". It cancels the active root turn and its complete anonymous-worker
tree, clears queued spawns in that tree, and preserves partial worker results
as resumable state. Deliveries that arrive during the freeze are stored as
pending messages and must not trigger worker follow-ups or synthetic completion
turns.

The next **user-initiated** turn on the thread clears the freeze. The root then
receives the whole-tree status snapshot, including completed results and
cancelled workers' partial results and resume hints. This does not automatically
restart every worker; the root can resume selected workers with `send_message`.
Natural turn completion still allows asynchronous workers to finish and wake
their parent, while thread/session close remains the true termination path.

## Notifications Used By Text Clients

Text clients consume these notifications and map them to human stderr or JSONL
stdout:

- `thread/started`
- `thread/resumed`
- `thread/updated`
- `turn/started`
- `turn/queued`
- `turn/dequeued`
- `turn/event`
- `turn/usage`
- `turn/completed`
- `turn/error`
- `item/started`
- `item/completed`
- `item/agentMessage/delta`
- `item/agentMessage/replace`
- `item/reasoning/delta`
- `item/reasoning/replace`
- `item/toolCall/delta`
- `item/toolCall/outputDelta`
- `agent/updated`
- `agent/mailbox`
- `mcp/status/updated`

## Non-Interactive Client Requests

The app-server can send requests back to the client, for example approval
requests. `wuu exec` is non-interactive by default, so it must fail closed when
it cannot handle a request. Automation can opt in to handling approval requests
with `wuu exec --approval-handler <command>` or
`wuu exec --approval-socket <path>`.

Approval request handlers receive:

```json
{"id":"server-1","method":"tool/approval/request","params":{}}
```

They respond with:

```json
{"decision":"approved","reason":"approved by policy"}
```

or a JSON-RPC-like object whose `result` field contains that response shape.

## Debug Commands

The debug commands expose the same text protocol without adding a TUI. They are
for agents, scripts, and developers that need to inspect the core path directly.

```bash
wuu debug app-server initialize [--workdir DIR] [--provider NAME] [--model MODEL] [--no-tools]
wuu debug app-server send [--workdir DIR] <method> '<json>'
wuu debug protocol events [--json] [--workdir DIR] <thread-id>
```

`wuu debug app-server initialize` starts a local app-server instance, sends
`initialize`, prints the JSON result, and shuts the server down.

`wuu debug app-server send` starts a local app-server instance, sends one
method with optional JSON params, prints the JSON result, and shuts the server
down. This is the lowest-level CLI probe for app-server methods.

`wuu debug protocol events` reads the stored session trace and prints the raw
JSONL trace events. With `--json`, it wraps the events with the thread id and
trace path for machine consumers.

## Session Contract

Persistent runs must create or update normal Wuu sessions so that:

- `wuu exec` sessions can be inspected by `wuu session`.
- `wuu exec` sessions can be resumed by Electron.
- Electron sessions can be resumed by `wuu exec`.
- traces live under workspace-scoped session artifacts.

## Runtime Model Selection

Model selection has one authoritative semantic. This section is the arbitration
reference for reviews: any new execution surface (background task, derived call,
display) must resolve its model through the rules below rather than re-guessing
which state to read.

### Three concepts

1. **Selection** — the `(provider, model, variant, effort, permission)` tuple.
   It is the only persistent model semantic. Each conversation pins exactly one
   (stored on its session row). The workspace holds one additional *default*
   selection whose only jobs are to seed new conversations and to serve
   surfaces that have no conversation (settings, a new empty tab). An existing
   conversation never re-reads the workspace default at run time.

2. **Derivation** — worker model, title model, and context budgets are not
   independent state; they are a pure function `derive(conversation selection,
   role config)`. Role config (e.g. "titles always use a cheap model") is a
   workspace-level *function definition*, but its evaluation input is always the
   owning conversation's selection. Corollary: copying a budget or worker
   default from one runtime to another is a bug — a derived quantity must be
   recomputed on the owning conversation. In code this is
   `runtime.Session.DeriveThreadModel`.

3. **Override** — only two kinds, and no more are added. *Process-scoped* (e.g.
   `exec --permission-mode`) beats everything and is never persisted.
   *Call-scoped* (a spawn / participant profile's explicit pin) affects only
   that one execution and is persisted with the run for recovery. An override
   resolves its own connection (client); it never borrows the host
   conversation's connection, and the "same provider as the worker, reuse the
   connection" check compares against the conversation's own derived worker
   provider, not workspace state.

### Two global rules

- **Attribution** — every inference request belongs to some conversation (main
  turn, side thread, subagent, auto-title, compaction). Resolve from the owning
  conversation's selection; only a request with no owning conversation may read
  the workspace default.
- **Dual-write and immutability** — changing a conversation's selection applies
  immediately to that conversation and makes the explicitly-provided fields the
  default for future conversations. The reverse never holds: a workspace-default
  change never edits an existing conversation. A running execution keeps its
  admission snapshot; a selection change is rejected, not hot-swapped.

### Resolution per surface

| Surface | Resolution |
|---|---|
| main turn / side thread / compaction budget | conversation selection (and its derived budget) |
| subagent (unpinned) / auto-title | `derive_worker` / `derive_title`(conversation selection) |
| subagent (explicit pin) | call-scoped override, own connection |
| fork | copy the source conversation's selection |
| resume / continuation | restore selection from disk |
| new conversation, automation, group create | seed the workspace default |
| settings / no-conversation surface | workspace default |
| exec flag | process-scoped override, not persisted |

### Frontend expression

Display semantics obey attribution too, and the desktop keeps intent and fact
from diverging without any explanatory copy:

- **One place.** The composer model button (`RuntimePicker`) is the only place a
  conversation's model semantic appears; it shows the conversation's selection.
  No-conversation surfaces show the workspace default. No other UI restates the
  model. (The run-debug panel's model row follows the active conversation, not
  the global default.)
- **One lock.** While any execution runs in the conversation — a streaming turn
  or an unsettled background agent — the model button is disabled. So the value
  shown is always both "what will run next" and "what is running now"; intent
  and fact cannot fork, so no second annotation is needed.
- **Causality from existing elements.** The streaming reply and the background
  agent cards already show they are running; they are the reason the button is
  disabled. The backend rejection message is only a cross-process race backstop,
  not a normal path.

## Protocol Compatibility

Changes to method names, notification names, field names, stdout/stderr
behavior, or exit code meaning are product-level compatibility changes. Treat
them as public API once automation depends on them.

The Ultra additions are additive: `ConfigModelUpdateParams.ultra` is optional,
and existing clients that omit it do not change the mode. The readback fields
are additive result members; clients should continue to tolerate unknown result
fields.
