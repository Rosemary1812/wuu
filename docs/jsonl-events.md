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

Some event families are already emitted by `wuu exec`; others are target
surface area that still needs mapping from app-server notifications or tool
telemetry.

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
  "trace_path": "/path/to/session-trace.jsonl"
}
```

Allowed `status` values include:

- `completed`
- `failed`
- `timeout`
- `interrupted`

## Compatibility

JSONL event names and core field names are automation API. Prefer additive
changes. Do not repurpose a field with a different meaning.
