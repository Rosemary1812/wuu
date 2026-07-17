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

> [!WARNING]
> **Project status:** The mobile app has not been released yet. wuu is still pre-1.0 and evolving quickly, so features, interfaces, and behavior may change between versions. If you need a stable, production-ready tool, please evaluate carefully before adopting it.

## Start Here

| You want to... | Go to |
|---|---|
| Install and run your first task | [Install](#install) and [Quick Start](#quick-start) |
| Use the desktop app | [Desktop App](#desktop-app) |
| Drive wuu from scripts, CI, or another agent | [CLI and Automation](#cli-and-automation) and [`docs/exec.md`](docs/exec.md) |
| Connect a provider (Anthropic, OpenAI-compatible, local) | [Providers](#providers) |
| Understand or embed the Go core | [Architecture](#architecture) and the [`app-server` protocol](docs/app-server-protocol.md) |
| Contribute | [Contributing](CONTRIBUTING.md) |
| Review the security and trust boundaries | [Security model](docs/security-model.md) |
| Inspect reproducible public evaluations | [Public evaluations](evals/) |

## News

- **macOS desktop releases** — tagged GitHub Releases contain only the unsigned macOS Electron app; the CLI remains available from source for automation.
- **2026-07-10** Tagged **v0.1.0** — the first packaged desktop milestone: unsigned macOS Electron preview builds on GitHub Releases, plus open-source governance in place. See the [CHANGELOG](CHANGELOG.md) for details.

## Why wuu

- **BYOK, no lock-in** — bring your own API key; works with Anthropic and any OpenAI-compatible endpoint, including local gateways.
- **One core, many shells** — the Go core speaks JSON-RPC via `wuu app-server`; the desktop app is the first shell, and editor plugins can reuse the same core without forking it.
- **Orchestration built in** — subagents, durable goals, skills, persistent memory, and scheduled tasks are part of the runtime, not bolted on.
- **Scriptable by design** — `wuu exec` streams JSONL, so CI jobs, review bots, and other agents can drive it programmatically.
- **Sessions that persist** — resume previous turns, fork from a checkpoint, and keep context across sessions.

## Install

> [!IMPORTANT]
> wuu is pre-1.0. Tagged GitHub Releases contain only an unsigned macOS Electron desktop preview. macOS may block the app until you remove quarantine for a trusted download.

Pick **one** install method:

**macOS desktop package** (unsigned)

Download `wuu-<version>-mac-arm64.dmg` or `wuu-<version>-mac-arm64.zip`
from [GitHub Releases](https://github.com/blueberrycongee/wuu/releases).
After moving `wuu.app` to `/Applications`, macOS may block it because the app
is not signed or notarized. If macOS says the app cannot be opened, copy and
run this command:

```bash
xattr -dr com.apple.quarantine /Applications/wuu.app && open /Applications/wuu.app
```

**Install the CLI from source with Go**

```bash
go install github.com/blueberrycongee/wuu/cmd/wuu@latest
```

Standalone CLI archives are not published. A source-installed CLI is separate
from the private core bundled with the desktop app, so both can coexist and use
different versions.

Verify the install:

```bash
wuu --version
```

**Run from a checkout**

```bash
git clone https://github.com/blueberrycongee/wuu.git
cd wuu
go run ./cmd/wuu --version
```

## Quick Start

**Desktop**

Open `wuu.app`, choose a local project folder, and start a thread from the composer.

**CLI and automation**

Initialize once:

```bash
wuu init
```

This creates the user-owned config at `~/.wuu/config.json` (or
`WUU_HOME/config.json`). Provider connections and credentials are never written
to the project by this command.

Run your first tasks:

```bash
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
- **Subagents** — delegate to child agents (fresh general-purpose context, worktree-isolated workers, or context-inheriting forks) for parallel or isolated work
- **Durable goals** — long-running objectives that survive context loss and resume across sessions
- **Skills** — task-specific instruction sets for focused work like planning, reviewing, or frontend design
- **Persistent memory** — agent profiles that remember preferences and context across sessions
- **Scheduled tasks** — run prompts on cron schedules

**Providers and integration**
- **BYOK / multi-provider** — bring your own API key; works with Anthropic and OpenAI-compatible gateways (OpenAI, OpenRouter, one-api, local)
- **JSONL output** — scriptable, streamable output for CI and other agents
- **Desktop app** — packaged macOS Electron app, or source-built UI for interactive use alongside the CLI

## Architecture

Wuu is split into a reusable **Go core** and a thin **shell**:

- The **Go core** (`internal/`, `cmd/wuu/`) provides the agent runtime, providers, tool loop, sessions, and config. It runs as a subprocess via `wuu app-server`.
- The **current shell** is the Electron desktop in `desktop/`, which spawns the core and owns the UI and native integrations.
- **Future shells** (VS Code extension, JetBrains plugin, etc.) can consume the same core by spawning `wuu app-server` — no need to import or fork the Go code.

> [!TIP]
> Building a new shell or integration? Start with the [`app-server` protocol](docs/app-server-protocol.md) — it documents the full JSON-RPC interface the desktop app uses.

## Desktop App

The first packaged desktop release is a macOS Electron app on [GitHub Releases](https://github.com/blueberrycongee/wuu/releases).
The DMG/ZIP assets are currently unsigned; see the install section for the Gatekeeper
quarantine workaround.

The desktop app is developed in `desktop/`. Run it from a source checkout:

```bash
cd desktop
npm install
npm run dev
```

On macOS, the first `npm run dev` creates a local `Wuu Dev Signing` certificate
in your login keychain and uses it only for the development host. This keeps
Accessibility and Screen Recording permission stable across source rebuilds.
Set `WUU_DEV_SIGN_ID` to use an existing local Code Signing identity instead.
This development certificate is never used for packaged GitHub Release builds.

## CLI and Automation

`wuu exec` is the non-interactive entrypoint. It is useful for scripts, CI, review jobs, and other agents.

```bash
wuu exec --json "review the current diff"
wuu exec --file plan.md "implement this plan"
wuu exec review --uncommitted
```

See [`docs/exec.md`](docs/exec.md) for JSONL output, attachments, resume, fork, review, and automation options.

## Providers

Wuu supports Anthropic and OpenAI-compatible providers such as OpenAI,
OpenRouter, one-api, and local gateways. Bring your own API key — set the
matching environment variable and point wuu at any compatible endpoint.

Provider selection, models, endpoints, credential sources, global memory
settings, and permission mode live in the user config at
`~/.wuu/config.json`. Set `WUU_HOME` to relocate the whole directory; the
legacy `~/.config/wuu/config.json` is still migrated and read for backward
compatibility.

Project files (`.wuu.json`, `wuu.json`, `.wuu/settings.json`, and
`.wuu/settings.local.json`) may add project behavior such as prompt additions,
but normal startup ignores provider selection and definitions, role-specific
model selection, memory discovery, and permission mode from those files. This
keeps a repository from redirecting user credentials, routing background work
to another configured provider, or selecting arbitrary files outside the
workspace. Wuu's own global memory remains outside the workspace under the
user-controlled Wuu home and can still be read and updated normally.

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

- Browse all maintained documentation: [Documentation index](docs/README.md)
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

## License

[MIT](LICENSE)
