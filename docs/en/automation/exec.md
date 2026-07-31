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
`env`, `max_turns`, `output_schema`, `no_tools`, `timeout`, and
`output_last_message`.

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

Local PDF files are attached with `--file`:

```bash
wuu exec --file report.pdf "summarize this PDF and update the code"
```

Local images are attached with `--image`:

```bash
wuu exec --image screenshot.png "find the UI issue"
```

Both flags are repeatable. `--file` currently accepts PDF files only. Relative
attachment paths are resolved from `--workdir` when it is set, otherwise from
the current directory. Attachments
are sent as structured app-server `turn/start` fields, not pasted into the
prompt.

## Output Modes

Default mode is automation-safe:

- stdout contains only the final agent message.
- stderr contains run metadata such as provider, model, workspace, thread id,
  turn id, tool progress, and trace path. JSONL events also carry the execution
  `run_id` when they are associated with a Run.
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

See [JSONL events](jsonl-events.md) for the event contract.

## Exit Codes

`wuu exec` uses stable exit codes:

- `0`: completed successfully.
- `1`: agent turn failed.
- `2`: CLI arguments, config, or input validation failed.
- `3`: permission denied by the workspace boundary or tool policy.
- `4`: timeout.
- `5`: interrupted.
- `6`: app-server protocol error.
- `7`: provider or model error.
- `8`: tool execution failed and the agent did not recover.
- `9`: the target thread already has a running turn (try again later, or use another thread).

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
--env KEY=VALUE
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
MCP servers. `--env KEY=VALUE` is repeatable and applies only to the current
run. `--max-turns` caps the
model/tool loop for the current user turn.
`--output-schema` reads a JSON Schema file, instructs the agent to return only
JSON, and validates the final answer as part of the app-server execution Run.
Correction turns are created by the app-server inside that same Run, so a
structured-output invocation never starts a second lifecycle. JSONL `result`
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

## Permission modes

`wuu exec` makes allow-or-deny decisions without an interactive approval step:

- `standard` (default) confines file reach to registered workspace roots and
  permits mutations inside them;
- `read_only` keeps the same file reach and denies mutations;
- `unconfined` removes Wuu's path confinement and permits mutations.

The mode is an in-process tool boundary, not an operating-system sandbox.
Permitted child processes keep Wuu's OS identity, inherited environment, and
network stack. `standard` and `read_only` retain hard tool guards and output
redaction. `unconfined` explicitly bypasses path confinement, hard tool guards,
and tool-output redaction; use it only for short, trusted tasks. See the
[security model](../reference/security-model.md) before unattended or untrusted-repository
use.

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

## Safety

Unless `unconfined` is selected, `wuu exec` runs through the same workspace
boundary and tool guards as the desktop app. Unsafe Git operations, common
secret reads and environment dumps, and other high-risk command patterns
receive hard checks. Common credential patterns are redacted from tool output.
These controls are defense in depth, not OS isolation and not a guarantee that
every secret format is recognized.
