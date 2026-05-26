# wuu

[中文](README_zh.md)

GUI-first AI coding agent with a Go backend, one shared core workbench/runtime
path, and two app surfaces: Electron desktop and Wuu Browser.

Named after its author (Wu) — the goal is to build a coding companion so good that every developer goes *wuuuuu!*

## Install

```bash
# Homebrew
brew install blueberrycongee/tap/wuu

# Shell script
curl -fsSL https://raw.githubusercontent.com/blueberrycongee/wuu/main/install.sh | sh

# npm
npx wuu@latest

# From source
go install github.com/blueberrycongee/wuu/cmd/wuu@latest
```

## Quick Start

```bash
wuu init                         # write .wuu.json
wuu run "describe this repo"     # one-shot CLI task
wuu app-server --workdir .       # backend used by the desktop GUI
cd desktop && npm install && npm run dev  # local desktop GUI
```

Interactive work currently runs through the Electron desktop GUI. The future
product keeps Electron desktop and Wuu Browser as first-class app surfaces over
the same core workbench and native runtime path. The `wuu` binary provides the
app-server backend plus non-interactive CLI tools.

## Versioning

- `VERSION` is the single source of truth for the next SemVer release (for example `0.1.0`).
- Local builds use `vX.Y.Z-dev` by default:

```bash
make install
wuu version --long
```

- Release flow:

```bash
# 1) update VERSION
# 2) create release tag from VERSION
make tag-release

# 3) push tag to trigger GitHub Release workflow
git push origin v$(cat VERSION)
```

When a `v*` tag is pushed, GitHub Actions + GoReleaser publishes release artifacts.

## What It Does

- Shared workbench and native runtime core used by both Electron desktop and Wuu Browser
- Electron desktop GUI backed by the Go app-server for conversations, workspace context, and session streaming
- Chromium-based Wuu Browser surface that uses the same core workbench/runtime path
- One-shot CLI task runner for non-interactive use
- Agentic tool-calling loop — reads, writes, edits, searches, and runs shell commands in your repo
- Supports OpenAI-compatible APIs (OpenAI / OpenRouter / one-api / etc.) and Anthropic Messages API
- Built-in tools: `run_shell`, `git`, `read_file`, `write_file`, `edit_file`, `list_files`, `grep`, `glob`, `web_search`, `web_fetch`
- Orchestration and session tools: `ask_user`, `spawn_agent`, `fork_agent`, `send_message`, `followup_task`, `wait_agent`, `close_agent`, `list_agents`, `load_skill`
- Managed process tools: `start_process`, `list_processes`, `stop_process`, `read_process_output`
- Scheduling tools: `schedule_cron`, `cancel_cron`, `list_cron`
- Tool availability model:
  - Main GUI/app-server session: full tool set
  - Sub-agents: no `ask_user` and no orchestration tools (`spawn_agent`, `fork_agent`, `send_message`, `followup_task`, `wait_agent`, `close_agent`, `list_agents`)
- Follow-up control: `send_message` queues short instructions for workers; `followup_task` starts a new worker turn from saved history when a task is idle
- File tools are sandboxed to the current workspace
- Session isolation with resume support
- Context compaction for long conversations

## Configuration

Config is loaded from (highest priority first):

1. `.wuu.json` (project-local)
2. `wuu.json`
3. `~/.config/wuu/config.json` (global)

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

## License

MIT
