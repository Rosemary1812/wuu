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
`strict_config`, `env`, `no_tools`, `timeout`, and `output_last_message`.

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
`resume --all` is part of the target surface but is not fully implemented yet.

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
--file <path>
--image <path>
--no-tools
--json
--ephemeral
--input-json
--timeout <duration>
--output-last-message <file>
```

`--config` loads a specific config file. Relative config paths are resolved
from `--workdir` when it is set, otherwise from the current directory.
`--ignore-user-config` skips `~/.config/wuu/config.json`. `--strict-config` is
accepted for automation compatibility; `wuu exec` already fails when no usable
config can be loaded. `--env KEY=VALUE` is repeatable and applies only to the
current run.

Target flags that still need implementation:

```bash
--max-turns <n>
--output-schema <schema.json>
--approval-handler <command>
--approval-socket <path>
```

Unimplemented target flags should fail clearly rather than silently changing
behavior.

## Session Inspection

Agent-facing session inspection lives under `wuu session`:

```bash
wuu session list --json
wuu session show --json <thread-id>
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

`wuu exec` runs through the normal Wuu permission model. Non-interactive runs
must fail closed when they need approval and no approval handler is available.
They must not silently approve destructive work.

## Legacy `wuu run`

`wuu run` is a legacy compatibility wrapper around `wuu exec`, so
non-interactive product behavior is defined by the app-server-backed exec path.

Legacy run-only flags such as `--max-steps`, `--temperature`, and
`--system-prompt` are not supported by the app-server path and fail clearly.
