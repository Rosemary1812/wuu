# `wuu exec`

`wuu exec` is the agent-friendly text entrypoint for Wuu.

Wuu has no TUI. Use Electron for human interaction and `wuu exec` for agents,
scripts, CI, and automation.

## Goal

`wuu exec` lets a caller drive the same Go core, app-server protocol, session
store, tool system, and permission model used by the Electron desktop. It is
not a terminal UI and it is not a second runtime.

The intended loop is:

```text
agent or script sends a task
-> wuu exec starts or resumes a Wuu thread through app-server
-> the normal tool loop runs
-> stdout/stderr or JSONL expose progress and final state
-> another agent or Electron can resume the same session
```

## Basic Usage

```bash
wuu exec "fix the failing test and verify it"
wuu exec --json "review this PR"
wuu exec --file report.pdf "summarize and update the code"
wuu exec --image screenshot.png "find the UI problem"
wuu exec --timeout 20m --output-last-message result.md "summarize this repo"
```

`wuu exec` supports prompt text as positional arguments:

```bash
wuu exec "describe this repo"
```

It supports stdin as the prompt:

```bash
wuu exec - < task.md
printf "describe this repo" | wuu exec
```

When both positional text and piped stdin are present, stdin is passed as
additional context:

```bash
wuu exec "use this log to fix the bug" < error.log
```

The prompt delivered to the agent is:

```text
use this log to fix the bug

<stdin>
...
</stdin>
```

Empty input fails before a turn is started.

`--input-json` reads a machine input object from stdin:

```bash
wuu exec --input-json <<'JSON'
{
  "prompt": "use this log to fix the bug",
  "stdin": "panic: boom",
  "files": ["report.pdf"],
  "images": ["screenshot.png"],
  "workdir": "/repo",
  "json": true,
  "ephemeral": true
}
JSON
```

`prompt` and `stdin` are combined the same way as positional prompt plus piped
stdin. `files` and `images` behave like repeated `--file` and `--image` flags.
The object can also set `provider`, `model`, `effort`, `variant`,
`permission_mode`, `config`, `profile`, `ignore_user_config`,
`strict_config`, `env`, `allow_tools`, `deny_tools`, `approval_handler`,
`approval_socket`, `approve`, `approvals`, `max_turns`,
`output_schema`, `no_tools`, `timeout`, and `output_last_message`.

## Resume

```bash
wuu exec resume --last "continue from the failure"
wuu exec resume <thread-id> "continue this session"
wuu exec fork <thread-id> "try a different direction"
wuu exec review --uncommitted
wuu exec review --base main
wuu exec review --commit <sha>
```

`resume --last` asks app-server to resume the latest visible session for the
current workspace. `resume <thread-id>` resumes a specific session.

`fork <thread-id>` creates a new session through app-server `thread/fork`, then
starts the requested turn in that fork.

`review` builds a scoped review task and runs it through the same exec path.
The agent inspects the requested diff or commit with normal repository tools.

## Attachments

Local files are attached with `--file`:

```bash
wuu exec --file report.pdf "summarize this PDF and update the code"
```

Local images are attached with `--image`:

```bash
wuu exec --image screenshot.png "find the UI issue"
```

Both flags are repeatable. Relative attachment paths are resolved from
`--workdir` when it is set, otherwise from the current directory. Attachments
are sent as structured app-server `turn/start` fields, not pasted into the
prompt.

## Output Modes

Default mode is automation-safe:

- stdout contains only the final agent message.
- stderr contains run metadata such as provider, model, workspace, thread id,
  turn id, tool progress, and trace path.
- stdout does not contain banners, progress lines, terminal control codes, or
  debug logs.

JSONL mode is enabled with `--json`:

```bash
wuu exec --json "review this change"
```

In JSONL mode:

- stdout is JSONL.
- every stdout line is one JSON object.
- diagnostics and debug logs must not pollute stdout.
- the final event is `result`.

See [jsonl-events.md](jsonl-events.md) for the event contract.

## Exit Codes

`wuu exec` uses stable exit codes:

- `0`: completed successfully.
- `1`: agent turn failed.
- `2`: CLI arguments, config, or input validation failed.
- `3`: permission denied or non-interactive approval could not be obtained.
- `4`: timeout.
- `5`: interrupted.
- `6`: app-server protocol error.
- `7`: provider or model error.
- `8`: tool execution failed and the agent did not recover.

Scripts should use exit codes instead of parsing natural-language error text.

## Supported Flags

Current implemented flags:

```bash
--provider <name>
--model <model>
--effort <level>
--variant <name>
--permission-mode <mode>
--workdir <dir>
--config <path>
--profile <name>
--ignore-user-config
--strict-config
--env KEY=VALUE
--allow-tool <name>
--deny-tool <name>
--approval-handler <command>
--approval-socket <path>
--approve <approval-key>
--approvals <auto|strict|prompt>
--file <path>
--image <path>
--no-tools
--json
--ephemeral
--input-json
--max-turns <n>
--output-schema <schema.json>
--timeout <duration>
--output-last-message <file>
```

`--config` loads and explicitly trusts one config file. Relative paths are
resolved from `--workdir` when it is set, otherwise from the current directory.
`--ignore-user-config` skips the user config and explicitly trusts the first
project config (`.wuu.json`, then `wuu.json`) plus its project settings layers.
Both options are intended for controlled automation: a trusted file may choose
provider endpoints, credential environment variables, memory paths, hooks, and
MCP servers. `--strict-config` is accepted for automation compatibility; `wuu
exec` already fails when no usable config can be loaded. `--env KEY=VALUE` is
repeatable and applies only to the current run. `--max-turns` caps the
model/tool loop for the current user turn.
`--output-schema` reads a JSON Schema file, instructs the agent to return only
JSON, validates the final answer locally, and gives the agent a limited number
of correction turns when the result does not match the schema. JSONL `result`
events include `structured_result` after successful validation.

With neither option, `wuu exec` uses the user config at
`~/.wuu/config.json` (or `WUU_HOME/config.json`) as the trusted base. It then
deep-merges project sources in this order: `.wuu.json` (or `wuu.json`),
`.wuu/settings.json`, and `.wuu/settings.local.json`. Objects merge recursively;
scalars and arrays replace.

Normal startup ignores `default_provider`, `providers`, `memory`,
`agent.model_roles`, and `agent.permission_mode` from every project source,
with a stderr warning. Those settings stay user-owned because they control
where credentials and model context are sent, which files outside the workspace
become model context, and how much local authority the agent receives. This
does not disable Wuu's global memory: user-configured memory under `~/.wuu` or
`WUU_HOME` remains readable and writable. It only stops the repository from
redirecting that discovery to arbitrary paths. Set `WUU_DEBUG` to log which
safe project layers were applied.

After layering, `wuu exec` also reads a Claude Code project-level
`<workdir>/.mcp.json` if present and merges its **approved** servers into
`mcp_servers`. Parsing is intentionally loose (unknown fields ignored). Servers
are not loaded until approved via the `mcp_json` section (`enable_all`,
`enabled`, `disabled`) — recommended in `.wuu/settings.local.json`, mirroring
Claude Code's `enableAllProjectMcpServers` / `enabledMcpjsonServers` /
`disabledMcpjsonServers`. Remote entries map `type: "http"` to the streamable
HTTP transport and `type: "sse"` to legacy SSE, same as Claude Code.
`${VAR}` / `${VAR:-default}` references are expanded.
On a native `mcp_servers` name clash the native entry wins; `disabled` wins over
`enabled`/`enable_all`. Unapproved servers print one aggregated stderr hint
(de-duplicated across reloads); a missing `.mcp.json` changes nothing.

`--allow-tool` and `--deny-tool` are repeatable one-run tool policy overrides.
They affect only the current exec run and do not write back to configuration.
A tool cannot be both allowed and denied in the same run.

`--approval-handler <command>` lets automation respond to app-server approval
requests. Wuu sends one JSON object to the command on stdin:

```json
{
  "id": "server-1",
  "method": "tool/approval/request",
  "params": {
    "id": "approval-id",
    "tool_name": "write_file",
    "risk": "high",
    "arguments_preview": "..."
  }
}
```

The command must write a JSON approval response to stdout:

```json
{"decision":"approved","reason":"policy allowed this run"}
```

It may also return a JSON-RPC-like object with a `result` field containing the
same response shape.

`--approval-socket <path>` uses the same request and response JSON over a Unix
socket, one newline-delimited object per approval request. `--approval-handler`
and `--approval-socket` are mutually exclusive.

## Approvals

**By default a delegated exec run just flows.** exec is built for callers —
usually other agents — that hand over a task and expect the repo toolchain to
run unattended. `--approvals` picks the mode:

- `auto` (the default): exec resolves `approval_policy` to `never`.
  Ask-classified actions such as unclassified shell commands (`go vet`,
  `make test`, project scripts) execute without a round-trip, the same way
  `full_access` resolves them. This does not loosen the hard limits:
  destructive commands stay denied by the command policy, and the tool
  layer's always-on protections (unsafe git, sensitive paths, env dumps,
  package/network mutation) never turn off. Per-tool `require_approval`
  rules in config still generate requests, answered by `--approve` grants or
  denied with a grant recipe.
- `strict`: keep `approval_policy: on_request`. Requests nothing answers are
  denied with a grant recipe — the fail-closed mode for CI or untrusted
  configurations.
- `prompt`: keep `on_request` and ask the human on the controlling terminal:
  `[y]es once / [a]lways this session / [N]o`. Without a terminal the flag
  warns on stderr and requests are denied.

Providing `--approval-handler <command>` or `--approval-socket <path>` also
keeps `on_request`, with automation answering each request. A configured
`auto_review` reviewer (the LLM guardian) keeps its review flow too; an
explicit `--approvals auto` overrides it. Note that a bare
`approval_policy: "on_request"` in the config file cannot opt into strictness
for exec runs — config loading fills that field from the permission-mode
preset, so exec cannot tell it apart from a derived default; use
`--approvals strict` (or input-json `"approvals": "strict"`).

In a review flow, `--approve <key>` (repeatable) pre-grants exactly the named
calls, matched by the stable `approval_key` (`<tool>:<arguments-sha256>` over
the call's binding fields, so a rephrased retry of the same command still
matches). Denied runs exit with status `permission_denied` and the final
`result` event carries `blocked_approvals:
[{approval_key, tool_name, arguments_preview, reason}]`, so a strict-mode
caller can decide and rerun:

```bash
wuu exec --json --approvals strict "run go vet and fix what it reports"
# → result: status "permission_denied" + blocked_approvals with the key

wuu exec resume --last --approvals strict --approve bash:2fd4e1c6... "vet is approved, continue"
```

Broad alternatives remain `--allow-tool <name>` (allow a whole tool for the
run) and `--permission-mode full_access`.

## Session Inspection

Agent-facing session inspection lives under `wuu session`:

```bash
wuu session list --json
wuu session show --json --last
wuu session show --json <thread-id>
wuu session trace --json --last
wuu session trace --json <thread-id>
wuu session search --json <query>
wuu session archive --json <thread-id>
wuu session delete --json <thread-id>
```

`list`, `show`, `trace`, and `search` are read-only and expose session
metadata, persisted history, trace replay data, and search results for
automation. `archive` hides a session from default lists without deleting its
persisted data. `delete` removes the session, its durable history, and any
workspace-scoped artifacts Wuu can locate for that thread.

## Named Group Chat

The desktop app is where humans run named agents in group chats. `wuu exec`
exposes the same named group-chat surface headlessly so CI and agents can drive
and assert it without the GUI. A scripted `actions` array in the `--input-json`
payload runs an ordered sequence of steps against the app server: each step is
either a human/orchestrator RPC (build a group, add members, open a reply,
escalate to a task) or a turn run AS a named participant (the only path that
mounts the group-chat tool surface — `post_message`, `manage_participant`).

Every step emits `action_started` and `action_completed` (or `action_failed`)
JSONL events; a named turn additionally emits its `participant_turn_started`
and the usual tool/subagent events. A step can bind a value from its result into
a variable with `save_as` and reference it in a later step with `$name`
(including woven into a named turn's prompt). `expect` asserts a dotted-path
field on the step result and fails the sequence on mismatch.

```bash
wuu exec --input-json <<'JSON'
{
  "json": true,
  "actions": [
    { "action": "create_group",
      "params": { "title": "Ship the fix" },
      "save_as": { "group": "thread.id" } },

    { "action": "add_group_member",
      "params": { "thread_id": "$group", "participant_id": "prt-ada" } },

    { "action": "participant_turn", "as": "prt-ada",
      "params": { "thread_id": "$group", "task_name": "ada_status",
                  "prompt": "Post a status result to thread $group." },
      "save_as": { "anchor": "anchor_item_id" } },

    { "action": "open_reply",
      "params": { "thread_id": "$group", "anchor_item_id": "$anchor" },
      "save_as": { "cth": "subthread.id" } },

    { "action": "participant_turn", "as": "prt-ada",
      "params": { "thread_id": "$group", "task_name": "ada_reply",
                  "prompt": "Answer inside the reply thread $cth." } },

    { "action": "post_subthread",
      "params": { "thread_id": "$group", "subthread_id": "$cth",
                  "text": "what about the retry path?" } },

    { "action": "escalate_task",
      "params": { "thread_id": "$group", "subthread_id": "$cth",
                  "title": "Ship the retry fix" },
      "expect": { "subthread.status": "task",
                  "subthread.lead_participant_id": "prt-ada" } },

    { "action": "participant_turn", "as": "prt-ada",
      "params": { "thread_id": "$group", "task_name": "ada_fork",
                  "prompt": "Fork a copy of yourself to help." } }
  ]
}
JSON
```

Actions split by how they reach the app server:

- Directly-callable RPCs (`action` maps to an existing app-server method):
  `create_group`, `add_group_member`, `remove_group_member`,
  `save_participant`, `list_participants`, `retire_participant`, `open_reply`,
  `list_replies`, `resolve_reply`, `escalate_task`,
  `post_subthread`.
- Named turns (`post_message` / `participant_turn`, run with `as` set to a named
  participant id): run a turn AS that participant so its deterministic provider
  can invoke the resident tools. `post_message` is the speak-in-a-group case;
  `participant_turn` is the general label for any other turn AS that
  participant, including forking a copy via `manage_participant`. Which tool the
  turn actually invokes is decided by the agent, not by the action name.

Notes for scripting named turns:

- A named agent runs one task at a time. Running two turns as the SAME
  participant back to back is supported — exec briefly waits out the prior run's
  drain — but give each turn a distinct `task_name` (lowercase letters, digits,
  underscores) so their agent paths do not collide.
- Escalation and reply/subthread posting are human-side RPCs by design; agents
  only reach a reply by posting into it from a named turn. `escalate_task`
  records the task lead; named turns on the escalated task can read the lead
  via `subthread.lead_participant_id` to identify who owns the task.

The full lifecycle above — build group, pull named members, named response,
open reply, weak-isolation round trip, escalate to a task with a lead, and fork —
is exercised end to end against the real app server with a deterministic provider
by `TestExecGroupChatEndToEndRegression` in `internal/exec`, which runs in CI
with no live API.

## Safety

`wuu exec` runs through the normal Wuu permission model. The safety floor for
unattended runs is the layer that cannot be approved away: destructive
commands are denied by the command policy, and the tool hard protections
(unsafe git, sensitive paths, env dumps, package/network mutation) are always
on. Above that floor, unattended runs default to flowing —
`--approvals auto` resolves `approval_policy` to `never` — so delegation
works without a babysitter. When a caller opts into a review flow
(`--approvals strict`, `--approvals prompt`, a handler, a socket, or an
`auto_review` reviewer), requests that nothing answers fail closed with a
grant recipe, and every decision is recorded as `approval_requested` /
`approval_resolved` events.
