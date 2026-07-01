<h1 align="center">wuu</h1>

<p align="center">Open-source, BYOK AI coding agent — a Go core with a desktop app, a scriptable CLI, and built-in multi-agent orchestration.</p>

<p align="center">
  <a href="https://github.com/blueberrycongee/wuu/actions/workflows/ci.yml"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/blueberrycongee/wuu/ci.yml?branch=main&style=flat-square&label=ci"></a>
  <a href="https://github.com/blueberrycongee/wuu/blob/main/LICENSE"><img alt="License" src="https://img.shields.io/github/license/blueberrycongee/wuu?style=flat-square"></a>
</p>

<p align="center">
  <a href="README.md">English</a> |
  <a href="README_zh.md">简体中文</a> |
  <a href="docs/exec.md">Docs</a> |
  <a href="CONTRIBUTING.md">Contributing</a>
</p>

---

**wuu** is an open-source AI coding agent that works in your local repository. It reads and edits files, runs commands, reviews changes, and resumes sessions — all through a BYOK model that works with Anthropic and any OpenAI-compatible provider.

Beyond single-turn tasks, wuu can plan multi-step work, delegate to specialized subagents, run durable workflows, apply task-specific skills, and remember context across sessions. Use the desktop app for interactive work, or reach for `wuu exec` from scripts, CI, and other agents.

## Installation

Wuu does not have published release binaries yet. Install the CLI from source:

```bash
go install github.com/blueberrycongee/wuu/cmd/wuu@latest
```

Or run it directly from a checkout:

```bash
git clone https://github.com/blueberrycongee/wuu.git
cd wuu
go run ./cmd/wuu --version
```

## Quickstart

```bash
wuu init
wuu exec "describe this repo"
wuu exec "fix the failing test"
```

Attach local files when they are part of the task:

```bash
wuu exec --file report.pdf "summarize this PDF"
wuu exec --image screenshot.png "find the UI issue"
```

Resume or inspect sessions:

```bash
wuu exec resume --last "continue"
wuu session list --json
```

## Features

**Repository work**
- **File operations** — read, edit, and inspect files inside the working repository
- **Shell execution** — run commands, capture output, and iterate on failures
- **Attachments** — pass local files (`--file`) and screenshots (`--image`) directly to a turn
- **Sessions** — resume previous turns, list history, and fork from a checkpoint

**Agent orchestration**
- **Subagents** — delegate to specialized agents (planner, worker, reviewer, debugger, QA, and more) for parallel or isolated work
- **Workflows** — durable multi-step runs with phases, worker spawns, and recovery
- **Skills** — task-specific instruction sets for focused work like planning, reviewing, or frontend design
- **Persistent memory** — agent profiles that remember preferences and context across sessions
- **Scheduled tasks** — run prompts or workflows on cron schedules

**Providers and integration**
- **BYOK / multi-provider** — bring your own API key; works with Anthropic and OpenAI-compatible gateways (OpenAI, OpenRouter, one-api, local)
- **JSONL output** — scriptable, streamable output for CI and other agents
- **Desktop app** — source-built UI for interactive use alongside the CLI

## Architecture

Wuu is split into a reusable **Go core** and a thin **shell**:

- The **Go core** (`internal/`, `cmd/wuu/`) provides the agent runtime, providers, tool loop, sessions, and config. It runs as a subprocess via `wuu app-server`.
- The **current shell** is the Electron desktop in `desktop/`, which spawns the core and owns the UI and native integrations.
- **Future shells** (VS Code extension, JetBrains plugin, etc.) can consume the same core by spawning `wuu app-server` — no need to import or fork the Go code.

See the [`app-server` protocol](docs/app-server-protocol.md) for the JSON-RPC interface.

## Desktop App

The desktop app is developed in `desktop/`. Run it from a source checkout:

```bash
cd desktop
npm install
npm run dev
```

## CLI and Automation

`wuu exec` is the non-interactive entrypoint. It is useful for scripts, CI, review jobs, and other agents.

```bash
wuu exec --json "review the current diff"
wuu exec --file plan.md "implement this plan"
wuu exec review --uncommitted
```

See [`docs/exec.md`](docs/exec.md) for JSONL output, attachments, resume, fork, review, and automation options.

## Providers

Wuu supports Anthropic and OpenAI-compatible providers such as OpenAI, OpenRouter, one-api, and local gateways. Bring your own API key — set the matching environment variable and point wuu at any compatible endpoint.

Project config usually lives in `.wuu.json`; global config can live in `~/.config/wuu/config.json`.

```json
{
  "default_provider": "openrouter",
  "providers": {
    "openrouter": {
      "type": "openai-compatible",
      "base_url": "https://openrouter.ai/api/v1",
      "api_key_env": "OPENROUTER_API_KEY",
      "model": "openai/gpt-4.1-mini"
    },
    "anthropic": {
      "type": "anthropic",
      "api_key_env": "ANTHROPIC_API_KEY",
      "model": "claude-sonnet-4-20250514"
    }
  }
}
```

Then set the matching environment variable:

```bash
export OPENROUTER_API_KEY="..."
export ANTHROPIC_API_KEY="..."
```

## Documentation

- [`wuu exec`](docs/exec.md)
- [`app-server` protocol](docs/app-server-protocol.md)
- [`jsonl-events`](docs/jsonl-events.md)
- [Contributing](CONTRIBUTING.md)

## Status

Wuu is pre-1.0 and under active development. Release binaries are not published yet. Interfaces, configuration, and desktop behavior may change.

## License

MIT
