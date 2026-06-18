# JSONL Events

`wuu exec --json` writes machine-readable JSONL to stdout.

Wuu has no TUI. JSONL is the stable text surface for agents, scripts, CI, and
automation.

## Rules

- stdout contains JSONL only.
- each line is one valid JSON object.
- every event has a `type` field.
- diagnostics, warnings, and debug logs go to stderr.
- the final line for a run is a `result` event.
- hidden reasoning, secrets, credentials, raw provider payloads, and unredacted
  sensitive tool data must not be emitted.

## Common Fields

Events should include these fields when available:

```json
{
  "type": "event_name",
  "thread_id": "thread-id",
  "turn_id": "turn-id"
}
```

## Required Event Families

The target event family list is:

- `session_configured`
- `thread_started`
- `thread_resumed`
- `thread_forked`
- `turn_started`
- `agent_message_delta`
- `agent_message_final`
- `reasoning_delta`
- `reasoning_final`
- `plan_updated`
- `tool_started`
- `tool_output_delta`
- `tool_completed`
- `command_started`
- `command_output_delta`
- `command_completed`
- `file_changed`
- `subagent_started`
- `subagent_updated`
- `subagent_completed`
- `approval_requested`
- `approval_resolved`
- `usage_updated`
- `turn_completed`
- `turn_failed`
- `turn_interrupted`
- `error`
- `result`

The current `wuu exec` implementation emits these families from app-server
notifications, app-server client requests, and structured tool results.

## Event Shapes

### `session_configured`

Emitted after `initialize` succeeds.

```json
{
  "type": "session_configured",
  "protocol_version": "wuu-app-server/v0.1",
  "provider": "openai",
  "model": "gpt-5",
  "workspace_root": "/repo",
  "permissions": {},
  "tool_policy": {}
}
```

### `thread_started`

Emitted when a new persistent thread is created.

```json
{
  "type": "thread_started",
  "thread_id": "20260618-120000-abcdef",
  "provider": "openai",
  "model": "gpt-5",
  "cwd": "/repo"
}
```

### `thread_resumed`

Emitted when an existing thread is resumed.

```json
{
  "type": "thread_resumed",
  "thread_id": "20260618-120000-abcdef",
  "provider": "openai",
  "model": "gpt-5",
  "cwd": "/repo"
}
```

### `turn_started`

```json
{
  "type": "turn_started",
  "thread_id": "thread-id",
  "turn_id": "turn-id"
}
```

### `agent_message_delta`

```json
{
  "type": "agent_message_delta",
  "thread_id": "thread-id",
  "turn_id": "turn-id",
  "delta": "text"
}
```

### `usage_updated`

```json
{
  "type": "usage_updated",
  "thread_id": "thread-id",
  "turn_id": "turn-id",
  "input_tokens": 100,
  "output_tokens": 20
}
```

### `tool_started`

```json
{
  "type": "tool_started",
  "thread_id": "thread-id",
  "turn_id": "turn-id",
  "item_id": "item-id",
  "name": "read_file",
  "arguments": "{\"path\":\"README.md\"}"
}
```

Tool arguments must be safe to expose. Sensitive values should be redacted or
omitted.

### `tool_output_delta`

```json
{
  "type": "tool_output_delta",
  "thread_id": "thread-id",
  "turn_id": "turn-id",
  "item_id": "item-id",
  "delta": "output text"
}
```

### `tool_completed`

```json
{
  "type": "tool_completed",
  "thread_id": "thread-id",
  "turn_id": "turn-id",
  "item_id": "item-id",
  "name": "read_file",
  "status": "completed",
  "error": ""
}
```

### `command_started`

Emitted in addition to `tool_started` for command-like tools such as
`run_shell`, `run_test`, and managed process tools.

```json
{
  "type": "command_started",
  "thread_id": "thread-id",
  "turn_id": "turn-id",
  "item_id": "item-id",
  "name": "run_shell",
  "command": "go test ./...",
  "arguments": "{\"command\":\"go test ./...\"}"
}
```

### `command_output_delta`

```json
{
  "type": "command_output_delta",
  "thread_id": "thread-id",
  "turn_id": "turn-id",
  "item_id": "item-id",
  "name": "run_shell",
  "command": "go test ./...",
  "delta": "ok\n"
}
```

### `command_completed`

```json
{
  "type": "command_completed",
  "thread_id": "thread-id",
  "turn_id": "turn-id",
  "item_id": "item-id",
  "name": "run_shell",
  "command": "go test ./...",
  "status": "completed",
  "error": ""
}
```

### `file_changed`

Emitted from structured results produced by file-changing tools such as
`write_file`, `edit_file`, `apply_patch`, and checkpoint restore. The event
does not duplicate full diffs or file contents.

```json
{
  "type": "file_changed",
  "thread_id": "thread-id",
  "turn_id": "turn-id",
  "item_id": "item-id",
  "tool_name": "edit_file",
  "path": "internal/exec/runner.go",
  "action": "edit",
  "old_file_sha": "sha256:old",
  "new_file_sha": "sha256:new",
  "workspace_revision": "fs:worktree:..."
}
```

### `subagent_started`

```json
{
  "type": "subagent_started",
  "thread_id": "thread-id",
  "agent_id": "agent-id",
  "agent_type": "subagent",
  "status": "running",
  "task_name": "worker"
}
```

### `subagent_updated`

```json
{
  "type": "subagent_updated",
  "thread_id": "thread-id",
  "agent_id": "agent-id",
  "status": "running",
  "input_tokens": 100,
  "output_tokens": 20
}
```

### `subagent_completed`

```json
{
  "type": "subagent_completed",
  "thread_id": "thread-id",
  "agent_id": "agent-id",
  "status": "completed",
  "result": "summary",
  "result_path": "/path/to/report.md",
  "error": ""
}
```

### `approval_requested`

Emitted when app-server asks the non-interactive client for approval.

```json
{
  "type": "approval_requested",
  "thread_id": "thread-id",
  "turn_id": "turn-id",
  "request_id": "server-1",
  "method": "tool/approval/request",
  "request": {
    "id": "approval-id",
    "tool_name": "write_file",
    "risk": "high",
    "arguments_sha256": "...",
    "arguments_preview": "..."
  }
}
```

### `approval_resolved`

```json
{
  "type": "approval_resolved",
  "thread_id": "thread-id",
  "turn_id": "turn-id",
  "request_id": "server-1",
  "method": "tool/approval/request",
  "decision": "approved",
  "reason": "approved by handler",
  "error": ""
}
```

If approval cannot be obtained in a non-interactive run, Wuu fails closed and
the final `result` uses `status: "permission_denied"` with exit code `3`.

### `turn_completed`

```json
{
  "type": "turn_completed",
  "thread_id": "thread-id",
  "turn_id": "turn-id",
  "input_tokens": 100,
  "output_tokens": 20,
  "trace_path": "/path/to/session-trace.jsonl"
}
```

### `turn_interrupted`

Emitted when `wuu exec` interrupts the active turn because of timeout or
process cancellation.

```json
{
  "type": "turn_interrupted",
  "thread_id": "thread-id",
  "turn_id": "turn-id",
  "reason": "timeout"
}
```

### `turn_failed`

```json
{
  "type": "turn_failed",
  "thread_id": "thread-id",
  "turn_id": "turn-id",
  "error": "provider returned an error"
}
```

### `result`

The final event in a run.

```json
{
  "type": "result",
  "status": "completed",
  "thread_id": "thread-id",
  "turn_id": "turn-id",
  "final_message": "final answer",
  "structured_result": {"summary": "valid JSON when --output-schema is used"},
  "trace_path": "/path/to/session-trace.jsonl"
}
```

`structured_result` is present only when `wuu exec --output-schema` is used and
the final answer validates against the requested JSON Schema.

Allowed `status` values include:

- `completed`
- `failed`
- `permission_denied`
- `timeout`
- `interrupted`

## Compatibility

JSONL event names and core field names are automation API. Prefer additive
changes. Do not repurpose a field with a different meaning.
