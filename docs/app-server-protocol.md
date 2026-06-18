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
Legacy `wuu run` forwards to `wuu exec` so CLI text automation uses the
app-server path.

## Core Methods

`initialize`

Returns provider, model, workspace, tool policy, permission summary, extension
trust summary, and protocol version.

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

Starts a user turn with prompt text and optional attachments.

`turn/interrupt`

Interrupts the active turn. `wuu exec` uses this for Ctrl+C and timeout
cleanup.

`shutdown`

Requests a clean app-server shutdown.

## Notifications Used By Text Clients

Text clients consume these notifications and map them to human stderr or JSONL
stdout:

- `thread/started`
- `thread/resumed`
- `turn/started`
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

## Protocol Compatibility

Changes to method names, notification names, field names, stdout/stderr
behavior, or exit code meaning are product-level compatibility changes. Treat
them as public API once automation depends on them.
