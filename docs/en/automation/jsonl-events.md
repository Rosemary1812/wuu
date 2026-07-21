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
- `provider_state`
- `request_context`
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
  "permissions": {}
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

### `provider_state`

Per-step diagnostic snapshot of the live provider transport. Surfaces the
current provider, protocol, transport, replay mode, response-id reuse,
connection reuse, and any transport-fallback state.

```json
{
  "type": "provider_state",
  "thread_id": "thread-id",
  "turn_id": "turn-id",
  "step_index": 1,
  "provider": "anthropic",
  "protocol": "messages",
  "transport": "https",
  "replay_mode": "off",
  "previous_response_id_used": false,
  "connection_reused": true,
  "diagnostic": "",
  "transport_failure_phase": "",
  "fallback_transport": "",
  "events_emitted": ["lifecycle", "content_delta"],
  "fallback_active": false,
  "fallback_reason": "",
  "input_items": 12,
  "full_input_items": 0,
  "delta_input_items": 12
}
```

### `request_context`

Per-step snapshot of the composed request the runner sent to the provider:
message counts and bytes by segment, tool surface hash, stable-prefix
hashing, and the prompt-cache key. Lets offline tooling reproduce or audit
what the model saw on this step.

```json
{
  "type": "request_context",
  "thread_id": "thread-id",
  "turn_id": "turn-id",
  "step_index": 1,
  "transient_messages": 0,
  "content_bytes": 4128,
  "block_kinds": ["text", "tool_use"],
  "block_kind_counts": {"text": 4, "tool_use": 1},
  "block_kind_bytes": {"text": 2048, "tool_use": 2080},
  "segment_lifecycle_counts": {"turn": 1, "stable": 2},
  "segment_placement_counts": {"system": 1, "history": 4},
  "segment_cache_policy_counts": {"cached": 3, "fresh": 2},
  "message_count": 5,
  "system_messages": 1,
  "hidden_messages": 0,
  "tool_count": 12,
  "stable_prefix": "…",
  "turn_prefix": "…",
  "dynamic_context_bytes": 4128,
  "system_bytes": 512,
  "stable_prefix_bytes": 2048,
  "turn_prefix_bytes": 1024,
  "message_bytes": 4128,
  "tool_schema_bytes": 8192,
  "loadable_tool_count": 24,
  "loadable_tool_schema_bytes": 24576,
  "loadable_tool_surface_hash": "sha256:…",
  "system_hash": "sha256:…",
  "stable_prefix_hash": "sha256:…",
  "turn_prefix_hash": "sha256:…",
  "tool_surface_hash": "sha256:…",
  "prompt_cache_key": "provider-specific"
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
