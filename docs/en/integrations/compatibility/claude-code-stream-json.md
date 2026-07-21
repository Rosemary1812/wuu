# Protocol Comparison: `wuu exec --json` vs Claude Code `--output-format stream-json`

Status: reference / design note. This document only compares the two protocols and
sketches a compatibility mode. It does not change any code.

## Why this document exists

wuu is GUI-first. Its CLI (`wuu exec`) exists mainly so other agents and orchestration
frameworks can drive the same Go core the desktop app uses. A large fraction of the
existing harness ecosystem is written against Claude Code's (cc) `-p
--output-format stream-json` protocol, and LLM priors lean the same way. Before deciding
whether to add a cc-compatible output mode to wuu, we need a precise, code-sourced map of
how far apart the two event streams are.

All claims below cite code. Paths are relative to the repo root
(`/Users/blueberrycongee/blueberrycongee/wuu`). The cc side is read from the vendored
source map under `thirdparty/claude-code-sourcemap/src` (a decompiled dump; treat it as
"best available", not upstream-authoritative). Anything not verifiable in code is marked
**(unconfirmed)**.

---

## 1. wuu side — `wuu exec --json`

### 1.1 Entry point and emission mechanism

- CLI flags are defined in `cmd/wuu/main.go:1400-1437`. Relevant ones:
  - `--json` (bool) → `cmd/wuu/main.go:1430` ("emit machine-readable JSONL to stdout").
  - `--output-schema <schema.json>` → `cmd/wuu/main.go:1436`.
  - `--output-last-message <file>` → `cmd/wuu/main.go:1433`.
  - `--max-turns <n>` → `cmd/wuu/main.go:1435`.
  - Usage text: `cmd/wuu/main.go:1907-1930`.
- The run loop is `exec.Run` in `internal/exec/runner.go:32-195`.
- Every JSONL line is written by `emitJSON` (`internal/exec/runner.go:978-984`):
  `json.NewEncoder(opts.Stdout).Encode(payload)` — one JSON object per line, newline
  delimited. **It is a no-op unless `opts.JSON` is true** (`runner.go:979`), so in the
  default (non-JSON) mode none of these events are produced; stdout gets only the final
  message and stderr gets metadata (`runner.go:186-193`, and the `else`/stderr branches in
  each `emit*` function, e.g. `runner.go:449-452`, `460`, `468`).
- Design contract (human-authored, not the emitter): `docs/en/automation/jsonl-events.md`, `docs/en/automation/exec.md`.
  Note: `docs/en/automation/jsonl-events.md:32-63` lists a *target* event family set; the list below is
  what the code **actually emits** today. Families present in the doc but not emitted by
  `runner.go` (e.g. a generic `error` for non-schema failures) are called out.

### 1.2 Identity model

wuu has **no `session_id`**. It identifies work with:

- `thread_id` — a persistent wuu session id, formatted like `20260618-120000-abcdef`
  (`docs/en/automation/jsonl-events.md:92`; `appserver.Thread.ID`, `internal/appserver/protocol.go:1254`).
- `turn_id` — one user turn within a thread (`appserver.Turn.ID`,
  `internal/appserver/protocol.go:1313`).
- `item_id` — one thread item (tool call, message, …) within a turn
  (`appserver.ThreadItem.ID`, `internal/appserver/protocol.go:1378`).

Almost every event carries `thread_id` and usually `turn_id`.

### 1.3 Event catalog (actual emit sites)

Every payload also has a `type` string. Fields listed are exactly the map keys passed to
`emitJSON`.

| Event `type` | Emit site | Fields |
|---|---|---|
| `session_configured` | `runner.go:434-453` (`emitSessionConfigured`, called `runner.go:93`) | `protocol_version`, `provider`, `model`, `effort`, `variant`, `workspace_root`, `permissions`, `tool_policy` |
| `thread_started` / `thread_resumed` / `thread_forked` | `runner.go:455-461` (`emitThreadEvent`); which one chosen at `runner.go:100-107` | `thread_id`, `model`, `provider`, `cwd` |
| `turn_started` | `runner.go:463-469` | `thread_id`, `turn_id` |
| `agent_message_delta` | `runner.go:298` | `thread_id`, `turn_id`, `delta` (token-level text; also accumulated into `finalMessage`, `runner.go:297`) |
| `agent_message_final` | `runner.go:307` (from `AgentMessageReplace`) | `thread_id`, `turn_id`, `message` |
| `reasoning_delta` | `runner.go:314` | `thread_id`, `turn_id`, `delta` |
| `reasoning_final` | `runner.go:320` | `thread_id`, `turn_id`, `text` |
| `tool_started` | `runner.go:474` (`emitItemStarted`) | `thread_id`, `turn_id`, `item_id`, `name`, `arguments` (a JSON **string**) |
| `tool_output_delta` | `runner.go:339` | `thread_id`, `turn_id`, `item_id`, `delta` |
| `tool_completed` | `runner.go:492` (`emitItemCompleted`) | `thread_id`, `turn_id`, `item_id`, `name`, `status`, `error` |
| `command_started` | `runner.go:477` (only for command tools) | command payload (`runner.go:662-677`): `thread_id`, `turn_id`, `item_id`, `name`, `arguments`, plus `command` / `process_id` extracted from args |
| `command_output_delta` | `runner.go:510-518` | command payload + `delta` |
| `command_completed` | `runner.go:495-498` | command payload + `status`, `error` |
| `file_changed` | `runner.go:501-503` (`fileChangeEventsFromToolResult`, `runner.go:697-718`) | `tool_name`, `path`, `action`, `old_file_sha`, `new_file_sha`, `workspace_revision` (see `docs/en/automation/jsonl-events.md:309-328`) |
| `subagent_started` / `subagent_updated` / `subagent_completed` | `runner.go:520-556` (`emitSubagentUpdated`) | `thread_id`, `agent_id`, `agent_type`, `status`, `task_name`, `agent_profile`, `agent_path`, `parent_id`, `description`, `result`, `result_path`, `result_bytes`, `result_truncated`, `error`, `input_tokens`, `output_tokens`, `cache_creation_tokens`, `cache_read_tokens` |
| `usage_updated` | `runner.go:352` | `thread_id`, `turn_id`, `input_tokens`, `output_tokens`, `cache_creation_tokens`, `cache_read_tokens` |
| `plan_updated` | `runner.go:866` | `thread_id`, `turn_id`, `plan` |
| `request_context` | `runner.go:867-909` | large diagnostic snapshot: `step_index`, byte/segment counts, hashes, `prompt_cache_key`, … |
| `provider_state` | `runner.go:910-935` | large transport snapshot: `provider`, `protocol`, `transport`, `replay_mode`, fallback state, input-item counts, … |
| `turn_completed` | `runner.go:364` | `thread_id`, `turn_id`, `input_tokens`, `output_tokens`, `trace_path` |
| `turn_failed` | `runner.go:381` | `thread_id`, `turn_id`, `error` |
| `turn_interrupted` | `runner.go:594-601` | `thread_id`, `turn_id`, `reason` (`"timeout"` \| `"interrupted"`) |
| `error` | `runner.go:939-955` (`emitStructuredOutputValidation`) | `thread_id`, `turn_id`, `error`, `retrying` — **emitted only for `--output-schema` validation failures**, not as a general error channel |
| `result` | `runner.go:957-976` (`emitResult`) | `status`, `thread_id`, `turn_id`, `final_message`, `trace_path`, `error` (if any), `structured_result` (if `--output-schema` validated) |

### 1.4 First and last events

- **First:** `session_configured` (`runner.go:93`), then one of
  `thread_started`/`thread_resumed`/`thread_forked` (`runner.go:100-107`), then
  `turn_started` (`runner.go:133`).
- **Last:** exactly one `result` event ends every run (`runner.go:140,146,157,174,180,403`;
  contract in `docs/en/automation/jsonl-events.md:14`).

### 1.5 `result.status` values and error expression

- `status` ∈ `completed`, `failed`, `permission_denied`, `timeout`, `interrupted`
  (set at `runner.go:180`, `174`, `157`, `140`, `146`; also `runner.go:388-403`;
  `docs/en/automation/jsonl-events.md:466-472`).
- Errors are expressed two ways:
  1. Per-turn: `turn_failed` event (`runner.go:381`) then a `result` with the failing
     `status` and an `error` string.
  2. The optional `error` event only covers structured-output validation retries.
- There is **no** `total_cost_usd`, `usage` block, `num_turns`, or `duration_ms` on the
  `result`. Token usage lives only in separate `usage_updated` / `turn_completed` events.

### 1.6 Exit codes (granular)

Defined in `internal/exec/types.go:13-23` (documented `docs/en/automation/exec.md:154-166`):

| Code | Const | Meaning |
|---|---|---|
| 0 | `ExitOK` | completed |
| 1 | `ExitTurnFailed` | agent turn failed |
| 2 | `ExitInvalidInput` | CLI/config/input validation failed |
| 3 | `ExitPermissionDenied` | permission denied / no approval handler |
| 4 | `ExitTimeout` | timeout |
| 5 | `ExitInterrupted` | interrupted / cancelled |
| 6 | `ExitProtocol` | app-server protocol error |
| 7 | `ExitProviderModelError` | provider/model error |
| 8 | `ExitToolFailed` | tool execution failed, unrecovered |

Classification: `runner.go:388-404` (turn-error → code), `runner.go:996-1017`
(setup/protocol/context → code).

### 1.7 `--output-schema` behavior

Implemented in `internal/exec/structured_output.go`:

- `loadOutputSchema` reads and compiles a JSON Schema (Draft 2020),
  `structured_output.go:23-61`.
- The schema instruction is prepended to the prompt (`initialPrompt`,
  `structured_output.go:63-77`).
- The final message is parsed as a single JSON value and validated (`validate`,
  `structured_output.go:97-114`).
- On failure the run retries up to `outputSchemaMaxRetries = 2`
  (`structured_output.go:15`; loop `runner.go:117-178`), emitting an `error` event with
  `retrying` each time (`runner.go:171`). Exhausting retries yields `result`
  `status:"failed"` and exit `ExitTurnFailed` (`runner.go:172-175`).
- On success, `structured_result` (the parsed value) is attached to the `result` event
  (`runner.go:164-168`, `972-974`).

### 1.8 `--output-last-message` behavior

Writes the final agent message text to a file after a successful run
(`runner.go:181-185`, `writeLastMessage` at `runner.go:1028+`). It is **not** part of the
JSONL stream and does not add or change any event.

### 1.9 Session start / resume / fork are subcommands, not flags

wuu selects thread lifecycle via subcommands (`docs/en/automation/exec.md:91-108`), e.g.
`wuu exec resume --last`, `wuu exec resume <thread-id>`, `wuu exec fork <thread-id>`.
There is **no** `--session-id`, `--resume`, or `--output-format` flag on `wuu exec`.
Provider/model are chosen with `--provider` / `--model` / `--effort` / `--variant`
(`cmd/wuu/main.go:1411-1414`).

### 1.10 Permission flow

`wuu exec` makes allow-or-deny decisions without an interactive approval
exchange. It has no approval handler or socket, and its app-server client treats
server-initiated requests as protocol errors.

---

## 2. cc side — `claude -p --output-format stream-json`

### 2.1 Entry point and emission mechanism

- Headless entry: `runHeadless` in `cli/print.ts:455-974`.
- Requires print mode: `-p, --print` (`main.tsx:976`). Input must come from a prompt arg,
  stdin, or a resumable session (`cli/print.ts:779-785`).
- `stream-json` output **requires `--verbose`** in print mode; otherwise it errors and
  exits 1 (`cli/print.ts:787-793`).
- A stdout guard diverts stray non-JSON writes to stderr so the NDJSON stays clean
  (`installStreamJsonStdoutGuard`, `cli/print.ts:594-596`).
- In `stream-json` + `--verbose`, **every** `SDKMessage` produced by
  `runHeadlessStreaming` is written to stdout (`cli/print.ts:884-886`) via
  `structuredIO.write` → `ndjsonSafeStringify(message) + '\n'`
  (`cli/structuredIO.ts:465-467`).
- `--output-format` choices: `text` (default), `json`, `stream-json`
  (`main.tsx:976`). `text` prints only the final `result.result`; `json` prints the final
  result object (or the full message array with `--verbose`); `stream-json` streams
  everything (`cli/print.ts:917-957`).

### 2.2 Identity model

cc has a single `session_id` on **every** message (`getSessionId()`), and it is expected
to be a valid UUID — `--session-id <uuid>` "must be a valid UUID" (`main.tsx:1000`);
resume validity is checked with `validateUuid` (`cli/print.ts:774-776`). There is no
thread/turn/item split on the wire; turns are counted (`num_turns`) but not addressed.

### 2.3 SDKMessage union

Authoritative schema union: `entrypoints/sdk/coreSchemas.ts:1854-1881`. The members most
relevant to a headless stream:

**`system` / `init`** (first message) — schema `coreSchemas.ts:1457-1494`, built by
`buildSystemInitMessage` (`utils/messages/systemInit.ts:53-96`):
`type:"system"`, `subtype:"init"`, `cwd`, `session_id`, `tools[]` (names),
`mcp_servers[]` (`{name,status}`), `model`, `permissionMode`, `slash_commands[]`,
`apiKeySource`, `betas[]`, `claude_code_version`, `output_style`, `agents[]`, `skills[]`,
`plugins[]`, `uuid`, `fast_mode_state`.

**`assistant`** — schema `coreSchemas.ts:1347-1356`. Fields: `type:"assistant"`,
`message` (a full Anthropic **API assistant message**: `role`, `content[]` blocks,
`stop_reason`, `usage`, …), `parent_tool_use_id` (nullable), `error?`, `uuid`,
`session_id`. Yielded once per assistant message via `normalizeMessage`
(`QueryEngine.ts:761-770`). **Tool calls are `tool_use` content blocks inside this
message**, not separate events.

**`user`** — schema `coreSchemas.ts:1273-1295`. Fields: `type:"user"`, `message` (API user
message; carries `tool_result` blocks for tool outputs), `parent_tool_use_id`,
`isSynthetic?`, `tool_use_result?`, `priority?`, `timestamp?`, `uuid?`, `session_id?`.
User messages are emitted both for the prompt and for tool results (yields at
`QueryEngine.ts:739-748`, `881-891`). Replay variant adds `isReplay:true`
(`coreSchemas.ts:1297-1303`).

**`stream_event`** (partial assistant) — schema `coreSchemas.ts:1496-1504`. Fields:
`type:"stream_event"`, `event` (a raw Anthropic streaming event, e.g.
`content_block_delta`), `parent_tool_use_id`, `uuid`, `session_id`. **Only emitted with
`--include-partial-messages`** (`QueryEngine.ts:820-825`; flag `main.tsx:976`).

**`result`** (last message) — success schema `coreSchemas.ts:1407-1426`, error schema
`coreSchemas.ts:1428-1451`:

- Common: `type:"result"`, `subtype`, `duration_ms`, `duration_api_ms`, `is_error`,
  `num_turns`, `stop_reason` (nullable), `total_cost_usd`, `usage`, `modelUsage`
  (per-model map), `permission_denials[]`, `fast_mode_state?`, `uuid`, `session_id`.
- Success adds `result` (final text string) and `structured_output?`.
- Error adds `errors[]` (string array) instead of `result`.
- `subtype` values:
  - `success` (`QueryEngine.ts:618-638`, `1135-1155`)
  - `error_during_execution` (`QueryEngine.ts:1082-1117`)
  - `error_max_turns` (`QueryEngine.ts:851-873`)
  - `error_max_budget_usd` (`QueryEngine.ts:981-1001`)
  - `error_max_structured_output_retries` (`QueryEngine.ts:1024-1046`)
- `num_turns` = count of user messages in the run (`QueryEngine.ts:753-755`).

**Other `system` subtypes and side-channel messages** (all in the union at
`coreSchemas.ts:1854-1881`, most gated behind flags/features): `compact_boundary`,
`status`, `post_turn_summary`, `api_retry`, `local_command_output`, `hook_started` /
`hook_progress` / `hook_response` (only with `--include-hook-events`,
`cli/print.ts:628-674`), `tool_progress`, `auth_status`, `task_notification` /
`task_started` / `task_progress`, `session_state_changed`, files-persisted,
`tool_use_summary`, `rate_limit_event`, `elicitation_complete`, `prompt_suggestion`.

**Control protocol** (not `SDKMessage`, but interleaved on the same stdio when
`--input-format stream-json`): `control_request` / `control_response` /
`control_cancel_request` carry `can_use_tool` permission prompts, `hook_callback`,
`mcp_message`, and `elicitation` (`cli/structuredIO.ts:333-531`). Permission prompts are
**requests to the client**, answered on stdin — a fundamentally different model from
wuu's non-interactive allow-or-deny decisions.

### 2.4 Exit codes (binary)

`runHeadless` ends with `gracefulShutdownSync(result.is_error ? 1 : 0)`
(`cli/print.ts:971-973`; `gracefulShutdownSync` sets `process.exitCode`,
`utils/gracefulShutdown.ts:336-347`). Startup/validation failures call
`gracefulShutdownSync(1)` (e.g. `cli/print.ts:569,576,584,608,749,761,783,791`).
So cc exit codes are effectively **0 = success, 1 = any error** — no granular taxonomy.

### 2.5 Headless flags (from `main.tsx:976-1000`)

| Flag | Notes |
|---|---|
| `-p, --print` | headless mode (required for all below) |
| `--output-format <text\|json\|stream-json>` | `main.tsx:976` |
| `--input-format <text\|stream-json>` | `main.tsx:976`; stream-json input requires stream-json output (`main.tsx:1825`) |
| `--include-partial-messages` | needs `--print` + stream-json (`main.tsx:1850`); enables `stream_event` |
| `--include-hook-events` | needs stream-json; enables hook `system` messages |
| `--verbose` | required for stream-json in print mode |
| `--max-turns <n>` | early-exits with `error_max_turns` |
| `--max-budget-usd <amount>` | `error_max_budget_usd` |
| `--json-schema <schema>` | structured output validation |
| `--permission-mode <mode>` | choices `PERMISSION_MODES` |
| `--permission-prompt-tool <tool>` | MCP tool for permission prompts |
| `--allowedTools` / `--allowed-tools <tools...>`, `--disallowedTools`, `--tools` | tool policy |
| `--system-prompt[-file]`, `--append-system-prompt[-file]` | |
| `-c, --continue`, `-r, --resume [value]`, `--fork-session`, `--resume-session-at` | session lifecycle |
| `--session-id <uuid>`, `-n, --name` | must be a valid UUID |
| `--agents <json>`, `--agent <agent>`, `--fallback-model`, `--betas` | |
| `--dangerously-skip-permissions` | bypass all permission checks |
| `--replay-user-messages` | re-emit stdin user messages (stream-json both ways) |
| `--mcp-config`, `--strict-mcp-config`, `--add-dir`, `--settings`, `--setting-sources` | |

---

## 3. Field-by-field mapping

Legend: **=** direct map · **~** needs transform/synthesis · **wuu∅** wuu has no source ·
**cc∅** cc has no equivalent.

### 3.1 Lifecycle / envelope

| Concept | wuu | cc | Rel. | Notes |
|---|---|---|---|---|
| Session identity | `thread_id` (date-string) | `session_id` (UUID) | ~ | Different id spaces; cc consumers may assume UUID (`main.tsx:1000`). See §5. |
| Turn identity | `turn_id` | none on wire; `num_turns` counter | ~ | wuu addresses turns; cc only counts. |
| Item identity | `item_id` | `tool_use`/`tool_result` block ids inside messages | ~ | Different granularity. |
| First event | `session_configured` + `thread_started` | `system`/`init` | ~ | Fields overlap partially — see §3.2. |
| Last event | `result` | `result` | ~ | Field sets differ hugely — see §3.4. |
| Per-message stamp | `thread_id`(+`turn_id`) on each | `session_id` (+`uuid`) on each | ~ | cc adds a per-message `uuid`; wuu has none. |

### 3.2 Init / configuration

| cc `system/init` field | wuu source | Rel. |
|---|---|---|
| `cwd` | `thread_started.cwd` (`runner.go:457`) / `session_configured.workspace_root` | = |
| `model` | `session_configured.model` (`runner.go:440`) | = |
| `permissionMode` | `session_configured.permissions` (`runner.go:444`) | ~ (shape differs; wuu carries a `PermissionSummary`) |
| `tools[]` | none emitted at init | wuu∅ (tools appear only when used via `tool_started`) |
| `slash_commands[]`, `agents[]`, `skills[]`, `plugins[]`, `mcp_servers[]` | none at init | wuu∅ |
| `apiKeySource`, `betas`, `claude_code_version`, `output_style`, `fast_mode_state` | none | wuu∅ (cc-specific) |
| `session_id`, `uuid` | `thread_id` / none | ~ / wuu∅ |
| — | `session_configured.provider`, `effort`, `variant`, `protocol_version`, `tool_policy` | cc∅ |

### 3.3 Streaming content

| Concept | wuu | cc | Rel. |
|---|---|---|---|
| Assistant text (incremental) | `agent_message_delta.delta` (`runner.go:298`) | `stream_event` w/ `content_block_delta` (`--include-partial-messages`) | ~ |
| Assistant text (final/whole) | `agent_message_final.message` (`runner.go:307`) | `assistant.message.content[]` text blocks | ~ (cc ships a full API message; wuu ships plain text) |
| Reasoning / thinking | `reasoning_delta` / `reasoning_final` (`runner.go:314,320`) | `assistant.message.content[]` `thinking` blocks (+ partial `stream_event`) | ~ |
| Tool call start | `tool_started` (`runner.go:474`) | `tool_use` block inside `assistant.message.content[]` | ~ (event vs content block) |
| Tool output (incremental) | `tool_output_delta` (`runner.go:339`) | none as text; final result only | ~ / cc∅ streaming |
| Tool result (final) | `tool_completed` (`runner.go:492`) | `tool_result` block inside a `user` message | ~ |
| Command tools | `command_*` events (`runner.go:475-518`) | folded into the same `tool_use`/`tool_result` blocks | ~ (wuu splits them out) |
| File change | `file_changed` (`runner.go:501-503`) | none (implicit in tool result) | cc∅ |
| Plan | `plan_updated` (`runner.go:866`) | none (TodoWrite tool result) | cc∅ |
| Subagents | `subagent_*` (`runner.go:520-556`) | `parent_tool_use_id` threading on `assistant`/`user` msgs | ~ |
| User prompt echo | none emitted | `user` message | wuu∅ |
| Provider/request diagnostics | `provider_state`, `request_context` (`runner.go:867-935`) | none | cc∅ |
| Token usage (incremental) | `usage_updated` (`runner.go:352`) | none per-message; only in final `result.usage` | ~ |
| Rate limits | none | `rate_limit_event` | cc∅ |

### 3.4 Result

| cc `result` field | wuu source | Rel. |
|---|---|---|
| `subtype` (`success` / `error_*`) | `result.status` (`runner.go:963`) | ~ (see mapping table §5.2) |
| `is_error` | derive from `status != "completed"` | ~ |
| `result` (final text) | `result.final_message` (`runner.go:966`) | = |
| `structured_output` | `result.structured_result` (`runner.go:973`) | = |
| `errors[]` | `result.error` string (`runner.go:970`) | ~ (string → array) |
| `stop_reason` | not on wuu `result` (available on `TurnCompletedNotification.StopReason`, `protocol.go:1164`, but not emitted) | wuu∅ on result |
| `num_turns` | derivable by counting `turn_started` | ~ |
| `duration_ms` / `duration_api_ms` | not tracked/emitted | wuu∅ |
| `usage` (aggregate) | sum of `usage_updated` / `turn_completed` tokens | ~ |
| `modelUsage` (per-model map w/ cost) | no per-model cost tracking | wuu∅ |
| **`total_cost_usd`** | **not computed anywhere in wuu** | **wuu∅ — hard blocker, see §5.3** |
| `permission_denials[]` | no direct event field | wuu∅ |
| `session_id`, `uuid` | `thread_id` / none | ~ / wuu∅ |
| `fast_mode_state`, `apiKeySource` | none | wuu∅ (cc-specific) |

### 3.5 Errors and permissions

| Concept | wuu | cc | Rel. |
|---|---|---|---|
| Turn failure | `turn_failed` + `result(status:"failed")` | `result(subtype:"error_during_execution")` | ~ |
| Timeout | `turn_interrupted(reason:"timeout")` + `result(status:"timeout")`, exit 4 | no dedicated subtype/exit | cc∅ |
| Interrupt/cancel | `turn_interrupted(reason:"interrupted")` + `result(status:"interrupted")`, exit 5 | SIGINT → graceful shutdown, exit 0 (`runHeadlessStreaming` `sigintHandler`, `cli/print.ts:1027-1034`) | ~ |
| Max turns | no distinct result subtype (loop just caps) | `result(subtype:"error_max_turns")` | ~ |
| Permission request | none; exec makes allow-or-deny decisions without an interactive approval step | `control_request(can_use_tool)`; answered by client on stdin | wuu∅ |
| Permission denied | `result(status:"permission_denied")`, exit 3 | recorded in `result.permission_denials[]`; **not** a terminal error subtype | ~ |
| Schema retry failure | `error` events + `result(status:"failed")` | `result(subtype:"error_max_structured_output_retries")` | ~ |

---

## 4. Flag comparison (cc headless ↔ wuu exec)

| cc flag | wuu equivalent | Rel. |
|---|---|---|
| `-p, --print` | implicit (`wuu exec` is always headless) | ~ |
| `--output-format stream-json` | **none** — wuu only has `--json` (its own JSONL) | wuu∅ (this doc's whole point) |
| `--output-format json` | none (wuu has no single-object mode) | wuu∅ |
| `--output-format text` | default `wuu exec` mode (final message on stdout) | ~ |
| `--verbose` | n/a (wuu always emits full stream in `--json`) | — |
| `--input-format stream-json` | `--input-json` reads one input object (`cmd/wuu/main.go:1434`; `docs/en/automation/exec.md:66-88`) — **not** a streaming JSONL input channel | ~ |
| `--max-turns <n>` | `--max-turns <n>` (`cmd/wuu/main.go:1435`) | = |
| `--max-budget-usd` | none (no cost tracking) | wuu∅ |
| `--json-schema <schema>` | `--output-schema <schema.json>` (`cmd/wuu/main.go:1436`) | ~ (file vs inline; wuu also injects schema into the prompt) |
| `--permission-mode <mode>` | `--permission-mode <mode>` (`cmd/wuu/main.go:1415`) | ~ (mode vocab may differ — **unconfirmed**) |
| `--allowedTools` | none | wuu∅ |
| `--disallowedTools` | none | wuu∅ |
| `--permission-prompt-tool` | none | wuu∅ |
| `--dangerously-skip-permissions` | closest: `--permission-mode` value (**unconfirmed** which) | ~ |
| `-r, --resume [id]` | `wuu exec resume <id>` / `resume --last` subcommand | ~ |
| `-c, --continue` | `wuu exec resume --last` | ~ |
| `--fork-session` | `wuu exec fork <id>` subcommand | ~ |
| `--session-id <uuid>` | none — wuu allocates its own `thread_id`, no pre-set id | wuu∅ |
| `--model` | `--model` (`cmd/wuu/main.go:1412`) | = |
| `--fallback-model` | none | wuu∅ |
| `--system-prompt` / `--append-system-prompt` | none on `wuu exec` (config/profile-driven) | wuu∅ (**unconfirmed** if a profile flag covers it) |
| `--add-dir` | `--workdir` (single root) (`cmd/wuu/main.go:1416`) | ~ |
| `--mcp-config` | config-file driven (**unconfirmed** flag) | ~ |
| `--replay-user-messages` | none | wuu∅ |
| — | `--output-last-message <file>` | cc∅ (cc has no direct equivalent) |
| — | `--effort` / `--variant` / `--profile` / `--provider` | cc∅ (wuu multi-provider knobs) |
| — | `--file` / `--image` / `--input-json` | ~ (cc uses content blocks on stdin) |

---

## 5. Compatibility-mode proposal: `wuu exec --output-format cc-stream-json`

**Not implemented — design only.** The idea: add an *additive*, opt-in translation layer
that consumes wuu's existing app-server notification stream and emits cc-shaped
`SDKMessage` NDJSON, so cc harnesses can drive wuu unchanged. This would be a new writer
alongside `emitJSON` (`runner.go:978`), keyed off a new `--output-format` value; the native
`--json` stream stays as-is.

### 5.1 What can be synthesized cleanly

| cc message | Built from | How |
|---|---|---|
| `system/init` | `session_configured` + `thread_started` (`runner.go:93,106`) | Map `cwd`, `model`, `permissionMode`. Emit `session_id = thread_id`. Fill `tools[]`, `slash_commands[]`, `mcp_servers[]`, `agents[]` with empty arrays or best-effort (wuu doesn't list these at init — see §5.4). Constant `claude_code_version` placeholder, `apiKeySource:"none"`, `betas:[]`. |
| `user` (prompt echo) | the `--json`-invisible prompt (wuu `TurnInput.Prompt`, `runner.go:123`) | Synthesize one `user` message with `message.content` = prompt text at `turn_started`. wuu currently emits nothing here. |
| `assistant` | buffer `agent_message_delta` (`runner.go:298`) until `turn_completed`/`agent_message_final`; fold `tool_started` (`runner.go:474`) into `tool_use` content blocks; `reasoning_*` into `thinking` blocks | Reassemble an Anthropic-shaped `message.content[]`. Hard part below (§5.5). |
| `user` (tool results) | `tool_completed`/`command_completed` (`runner.go:492`) | Emit a `user` message with a `tool_result` block referencing the `tool_use` id. Requires stable id mapping wuu `item_id` → cc `tool_use_id`. |
| `stream_event` | `agent_message_delta` / `reasoning_delta` | Only if the compat mode also opts into partial messages; wrap each delta as a `content_block_delta` raw event. Straightforward text-wise. |
| `result.subtype` + `result.result` + `is_error` | `result` (`runner.go:963,966`) | Map status→subtype (§5.2); `result.result = final_message`; `is_error = status != "completed"`. |
| `result.structured_output` | `result.structured_result` (`runner.go:973`) | direct. |
| `result.num_turns` | count emitted `turn_started` | derivable. |
| `result.usage` | accumulate `usage_updated`/`turn_completed` tokens (`runner.go:352,364`) | derivable (map `input_tokens`/`output_tokens`/cache fields to cc `usage` shape). |
| `result.permission_denials[]` | no equivalent exec event | not derivable without adding a new event contract. |
| `result.errors[]` | `result.error` string (`runner.go:970`) | wrap as single-element array. |

### 5.2 Status → subtype mapping (proposed)

| wuu `result.status` | cc `result.subtype` | Caveat |
|---|---|---|
| `completed` | `success` | clean |
| `failed` | `error_during_execution` | clean-ish |
| `permission_denied` | `error_during_execution` | cc has no permission subtype; lossy. Also record in `permission_denials[]`. |
| `timeout` | `error_during_execution` | **no cc timeout subtype** — information lost unless smuggled into `errors[]` |
| `interrupted` | `error_during_execution` | same; cc's own interrupt path exits 0, so semantics diverge |
| (max-turns cap) | `error_max_turns` | wuu doesn't emit a distinct status for the `--max-turns` cap today; would need a new signal from the runner |

### 5.3 Hard blockers (cannot be done faithfully)

1. **`total_cost_usd` (and `modelUsage[*].costUSD`).** wuu core computes **no cost**. There
   is no cost tracker; the `result` event (`runner.go:957-976`) has no cost field, and no
   `usage_updated`/`turn_completed` field carries money (`protocol.go:1136-1167`). cc's
   value comes from `getTotalCost()` (`QueryEngine.ts:628`). Options, all lossy: emit
   `total_cost_usd: 0` (misleads cost-gating harnesses), or compute a client-side estimate
   from token counts × a hardcoded price table (fragile, provider-specific, wrong for
   non-Anthropic models). **Recommend emitting `0` and documenting it as unsupported.**

2. **`session_id` UUID semantics.** wuu `thread_id` is `YYYYMMDD-HHMMSS-suffix`
   (`docs/en/automation/jsonl-events.md:92`), **not a UUID**. cc consumers that validate the id
   (`validateUuid`, `cli/print.ts:774`) or feed it back to `--session-id`/`--resume` (which
   require a UUID, `main.tsx:1000`) will reject or mishandle it. A synthetic UUID could be
   minted per run, but then it no longer round-trips to `wuu exec resume <thread-id>`, so
   resume-by-session-id compat breaks either way. **No lossless choice.**

3. **`stop_reason`.** Present in wuu core (`TurnCompletedNotification.StopReason`,
   `protocol.go:1164`) but **not emitted** on any JSONL event, so the compat layer can't see
   it without a runner change; it would emit `null`. Low stakes but note it.

4. **`duration_api_ms`.** wuu tracks no API-time split. `duration_ms` (wall clock) can be
   measured by the compat writer; `duration_api_ms` would be a fabricated `0`.

5. **Assistant-message content fidelity for non-Anthropic providers (see §5.5).**

### 5.4 Medium-difficulty gaps

- **`system/init` tool/command inventory.** cc lists `tools[]`, `slash_commands[]`,
  `mcp_servers[]`, `agents[]`, `skills[]` at init (`systemInit.ts:62-84`). wuu's
  `session_configured` doesn't (`runner.go:436-446`). The data exists in app-server
  (`InitializeResult` has `ToolSurface`, `protocol.go:166`), but the exec runner doesn't
  emit it. Filling these requires either threading more of `InitializeResult` into the
  compat writer or accepting empty arrays (some harnesses render pickers from these).
- **Permission model translation.** cc expects the client to answer `can_use_tool`
  `control_request` on **stdin** (`structuredIO.ts:533-659`). Wuu exec has no
  interactive approval exchange. Supporting this would be a separate input and
  permission-protocol project, not an output translation detail.

### 5.5 The assistant-message reassembly problem

cc's core streaming unit is a **full Anthropic API assistant message** with a typed
`content[]` of `text` / `thinking` / `tool_use` blocks (`coreSchemas.ts:1347-1356`), and
tool results come back as `tool_result` blocks in a following `user` message. wuu instead
emits a flat sequence of token deltas plus separate `tool_started`/`tool_completed` events
keyed by `item_id`. To synthesize cc's shape the compat writer must:

- buffer deltas and split them into text vs thinking blocks (wuu already separates
  `agent_message_*` from `reasoning_*`, so this part is tractable),
- convert each `tool_started` into a `tool_use` block with a generated/`item_id`-derived
  `id` and parse `arguments` (a JSON **string**, `runner.go:474`) back into an object,
- pair each `tool_completed` with a `tool_result` user message referencing that id.

This is mechanical for Anthropic-backed runs. For **non-Anthropic providers** (wuu is
multi-provider — `--provider openai`, `gpt-5`, etc., `cmd/wuu/main.go:1411`; see
`provider_state.provider`, `runner.go:920`) the reconstructed `message` is only *shaped
like* an Anthropic message; block types, id formats, and reasoning representations won't
match what a real cc-on-Anthropic run produces. Harnesses that inspect
`assistant.message` internals (not just `.content[].text`) may break. **Recommend:
guarantee `result.result` text + tool_use/tool_result *structure*, but explicitly document
that `message` block fidelity is best-effort and provider-dependent.**

### 5.6 Exit-code strategy

cc callers expect 0/1 (`cli/print.ts:971`). wuu's granular 0-8 (`types.go:13-23`) already
collapses safely — any nonzero reads as "error" to a cc consumer. Recommendation: **keep
wuu's granular exit codes** even in compat mode (a cc harness only checks zero/nonzero), or
add a `--cc-exit-codes` toggle to squash to 0/1 if a strict consumer needs it. Document the
choice; do not silently change wuu's codes.

### 5.7 Summary recommendation

An additive `--output-format cc-stream-json` is **feasible for the common harness path**
(system/init → user → assistant(+tool_use) → user(tool_result) → result.success with text
and usage). The unavoidable lossy points to advertise up front:

1. `total_cost_usd` / `modelUsage` cost — **not available; emit 0**.
2. `session_id` — non-UUID `thread_id` or a non-resumable synthetic UUID; can't be both
   valid-looking and round-trippable.
3. `timeout` / `interrupted` / `permission_denied` — no matching cc `result.subtype`;
   folded into `error_during_execution` (+ `errors[]`).
4. `assistant.message` block fidelity — best-effort, and weakest for non-Anthropic
   providers.
5. Incoming permission prompts over stdin (`can_use_tool` control protocol) — unsupported;
   adding them would be a separate permission-protocol project.

---

## Appendix: primary sources

- wuu emitters: `internal/exec/runner.go` (esp. `289-419`, `434-601`, `863-984`).
- wuu options / exit codes: `internal/exec/types.go:13-129`.
- wuu structured output: `internal/exec/structured_output.go`.
- wuu app-server types: `internal/appserver/protocol.go` (`154-208`, `1100-1210`,
  `1253-1460`).
- wuu CLI flags/usage: `cmd/wuu/main.go:1400-1456`, `1907-1930`.
- wuu contract docs: `docs/en/automation/exec.md`, `docs/en/automation/jsonl-events.md`.
- cc print/headless: `thirdparty/claude-code-sourcemap/src/cli/print.ts` (`455-974`,
  `1027-1079`).
- cc structured IO / control protocol:
  `thirdparty/claude-code-sourcemap/src/cli/structuredIO.ts`.
- cc SDK message schemas:
  `thirdparty/claude-code-sourcemap/src/entrypoints/sdk/coreSchemas.ts` (`1270-1504`,
  `1854-1889`).
- cc init builder:
  `thirdparty/claude-code-sourcemap/src/utils/messages/systemInit.ts`.
- cc result construction:
  `thirdparty/claude-code-sourcemap/src/QueryEngine.ts` (`600-638`, `818-1156`).
- cc CLI flags: `thirdparty/claude-code-sourcemap/src/main.tsx:976-1000` (and validation at
  `1279-1367`, `1825-1850`).
- cc exit: `thirdparty/claude-code-sourcemap/src/utils/gracefulShutdown.ts:336-347`.
</content>
</invoke>
