<h1 align="center">wuu</h1>

<p align="center">Open-source, BYOK AI coding agent — a Go core with a desktop app, a scriptable CLI, and built-in multi-agent orchestration.</p>

<div align="center">
  <p>
    <a href="README.md">English</a> |
    <a href="README_zh.md">简体中文</a>
  </p>
  <p>
    <a href="https://github.com/blueberrycongee/wuu/actions/workflows/ci.yml"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/blueberrycongee/wuu/ci.yml?branch=main&style=flat-square&label=ci"></a>
    <a href="https://github.com/blueberrycongee/wuu/blob/main/LICENSE"><img alt="License" src="https://img.shields.io/github/license/blueberrycongee/wuu?style=flat-square"></a>
    <a href="https://github.com/blueberrycongee/wuu/blob/main/go.mod"><img alt="Go version" src="https://img.shields.io/github/go-mod/go-version/blueberrycongee/wuu?style=flat-square"></a>
    <a href="https://github.com/blueberrycongee/wuu/graphs/commit-activity"><img alt="Commit activity" src="https://img.shields.io/github/commit-activity/m/blueberrycongee/wuu?style=flat-square"></a>
  </p>
</div>

---

<img width="2272" height="2494" alt="wuu desktop app" src="https://github.com/user-attachments/assets/2d9030aa-ca03-42b1-9333-f79cc5aff95b" />

**wuu** is an open-source AI coding agent that works in your local repository. It reads and edits files, runs commands, reviews changes, and resumes sessions — all through a BYOK model that works with Anthropic and any OpenAI-compatible provider.

Beyond single-turn tasks, wuu can plan multi-step work, delegate to specialized subagents, apply task-specific skills, and remember context across sessions. Use the desktop app for interactive work, or reach for `wuu exec` from scripts, CI, and other agents.

## Start Here

| You want to... | Go to |
|---|---|
| Install and run your first task | [Install](#install) and [Quick Start](#quick-start) |
| Use the desktop app | [Desktop App](#desktop-app) |
| Drive wuu from scripts, CI, or another agent | [CLI and Automation](#cli-and-automation) and [`docs/exec.md`](docs/exec.md) |
| Connect a provider (Anthropic, OpenAI-compatible, local) | [Providers](#providers) |
| Understand or embed the Go core | [Architecture](#architecture) and the [`app-server` protocol](docs/app-server-protocol.md) |
| Contribute | [Contributing](CONTRIBUTING.md) |

## News

- **2026-07-01** Tagged **v0.1.0** — the first versioned milestone: MIT license, contribution guidelines, security policy, and open-source governance in place. See the [CHANGELOG](CHANGELOG.md) for details.

## Why wuu

- **BYOK, no lock-in** — bring your own API key; works with Anthropic and any OpenAI-compatible endpoint, including local gateways.
- **One core, many shells** — the Go core speaks JSON-RPC via `wuu app-server`; the desktop app is the first shell, and editor plugins can reuse the same core without forking it.
- **Orchestration built in** — subagents, durable goals, skills, persistent memory, and scheduled tasks are part of the runtime, not bolted on.
- **Scriptable by design** — `wuu exec` streams JSONL, so CI jobs, review bots, and other agents can drive it programmatically.
- **Sessions that persist** — resume previous turns, fork from a checkpoint, and keep context across sessions.

## A Real-World Comparison

On a real frontend bug in this repository, every run started from the exact same initial prompt — deliberately vague, with everything from locating the problem to fixing it left to the agents themselves. Three wuu group-chat agents running MiniMax-M3 autonomously worked out the problem and delivered a working fix for $2.66 in API cost; the same task took a single Claude Fable 5 agent about $200 to fix, while a Claude Opus 4.8 agent spent about $20 without landing a fix. Multi-agent collaboration got inexpensive models to the same result at roughly a seventy-fifth of the cost.

## Install

> [!IMPORTANT]
> wuu is pre-1.0 and release binaries are not published yet — installing from source with `go install` is the reliable path today. The installer script fetches the latest tagged GitHub release and will work once releases are published. Interfaces, configuration, and desktop behavior may still change.

Pick **one** install method:

**Install from source with Go**

```bash
go install github.com/blueberrycongee/wuu/cmd/wuu@latest
```

**One-command installer** (downloads a release binary)

```bash
curl -fsSL https://raw.githubusercontent.com/blueberrycongee/wuu/main/install.sh | sh
```

The installer downloads to `~/.local/bin` by default. To use a different directory:

```bash
INSTALL_DIR=/usr/local/bin sh install.sh
```

**Run from a checkout**

```bash
git clone https://github.com/blueberrycongee/wuu.git
cd wuu
go run ./cmd/wuu --version
```

Verify the install:

```bash
wuu --version
```

## Quick Start

**1. Initialize**

```bash
wuu init
```

**2. Run your first tasks**

```bash
wuu exec "describe this repo"
wuu exec "fix the failing test"
```

**3. Attach local files when they are part of the task**

```bash
wuu exec --file report.pdf "summarize this PDF"
wuu exec --image screenshot.png "find the UI issue"
```

**4. Resume or inspect sessions**

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
- **Subagents** — delegate to child agents (fresh general-purpose context, worktree-isolated workers, or context-inheriting forks) for parallel or isolated work
- **Durable goals** — long-running objectives that survive context loss and resume across sessions
- **Skills** — task-specific instruction sets for focused work like planning, reviewing, or frontend design
- **Persistent memory** — agent profiles that remember preferences and context across sessions
- **Scheduled tasks** — run prompts on cron schedules

**Providers and integration**
- **BYOK / multi-provider** — bring your own API key; works with Anthropic and OpenAI-compatible gateways (OpenAI, OpenRouter, one-api, local)
- **JSONL output** — scriptable, streamable output for CI and other agents
- **Desktop app** — source-built UI for interactive use alongside the CLI

## Architecture

Wuu is split into a reusable **Go core** and a thin **shell**:

- The **Go core** (`internal/`, `cmd/wuu/`) provides the agent runtime, providers, tool loop, sessions, and config. It runs as a subprocess via `wuu app-server`.
- The **current shell** is the Electron desktop in `desktop/`, which spawns the core and owns the UI and native integrations.
- **Future shells** (VS Code extension, JetBrains plugin, etc.) can consume the same core by spawning `wuu app-server` — no need to import or fork the Go code.

> [!TIP]
> Building a new shell or integration? Start with the [`app-server` protocol](docs/app-server-protocol.md) — it documents the full JSON-RPC interface the desktop app uses.

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

Project config usually lives in `.wuu.json`; global config lives in `~/.wuu/config.json` (set `WUU_HOME` to relocate the whole directory; the legacy `~/.config/wuu/config.json` is still read for backward compatibility).

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

For another provider, the same config shape applies:

| Replace | Where |
|---|---|
| Provider config key | `providers.<provider>` |
| Provider type | `providers.<provider>.type` (`anthropic` or `openai-compatible`) |
| Endpoint URL, when needed | `providers.<provider>.base_url` |
| API key env var name | `providers.<provider>.api_key_env` |
| Model ID | `providers.<provider>.model` |

## Docs

- Drive wuu from scripts, CI, or other agents: [`wuu exec`](docs/exec.md)
- Parse the streaming output: [JSONL events](docs/jsonl-events.md)
- Embed the core in a new shell: [`app-server` protocol](docs/app-server-protocol.md)
- Consume Claude Code–compatible stream output: [cc-stream-json](docs/compat/cc-stream-json.md)
- Set up a development environment: [Contributing](CONTRIBUTING.md)

## Contributing

PRs welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for setup, review, and contribution guidelines, and [SECURITY.md](SECURITY.md) for how to report vulnerabilities.

Wuu is pre-1.0 and under active development — if you hit rough edges, [open an issue](https://github.com/blueberrycongee/wuu/issues).

## Acknowledgments

Wuu's design draws heavily from — and stands on the shoulders of — these projects. Their work on agent runtimes, tool loops, multi-agent orchestration, and developer experience shaped many of wuu's decisions and trade-offs.

- [Codex](https://github.com/openai/codex) — OpenAI's coding agent
- [OpenCode](https://github.com/sst/opencode) — the open-source terminal coding agent
- [pi](https://github.com/badlogic/pi-mono) — Mario Zechner's minimal AI agent toolkit
- [Kimi Code](https://github.com/MoonshotAI/kimi-cli) — Moonshot AI's coding agent

Thank you to the teams and communities behind these projects for the inspiration and ideas that helped make wuu possible.

## Star History

<div align="center">
  <a href="https://star-history.com/#blueberrycongee/wuu&Date">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=blueberrycongee/wuu&type=Date&theme=dark" />
      <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=blueberrycongee/wuu&type=Date" />
      <img alt="Star History Chart" src="https://api.star-history.com/svg?repos=blueberrycongee/wuu&type=Date" />
    </picture>
  </a>
</div>

## License

[MIT](LICENSE)
