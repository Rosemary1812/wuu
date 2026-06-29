<h1 align="center">wuu</h1>

<p align="center">
  <strong>Desktop-first AI coding agent for real repositories.</strong>
</p>

<p align="center">
  Work with Wuu in the desktop app, or call the same agent from scripts, CI, and other tools with <code>wuu exec</code>.
</p>

<p align="center">
  <a href="https://github.com/blueberrycongee/wuu/releases"><img alt="Release" src="https://img.shields.io/github/v/release/blueberrycongee/wuu?style=flat-square"></a>
  <a href="https://github.com/blueberrycongee/wuu/actions/workflows/ci.yml"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/blueberrycongee/wuu/ci.yml?branch=main&style=flat-square&label=ci"></a>
  <a href="https://www.npmjs.com/package/@blueberrycongee/wuu"><img alt="npm" src="https://img.shields.io/npm/v/@blueberrycongee/wuu?style=flat-square"></a>
  <a href="https://github.com/blueberrycongee/wuu/blob/main/LICENSE"><img alt="License" src="https://img.shields.io/github/license/blueberrycongee/wuu?style=flat-square"></a>
</p>

<p align="center">
  <a href="README.md">English</a> |
  <a href="README_zh.md">简体中文</a> |
  <a href="docs/exec.md">Exec docs</a> |
  <a href="CONTRIBUTING.md">Contributing</a>
</p>

---

## What is Wuu?

Wuu is an open source AI coding agent with a desktop interface and an automation-friendly command line entrypoint. It can inspect and edit files, run checks, review changes, attach local files or screenshots, and resume project sessions later.

It is built for developers who want an agent that works inside an actual repository instead of a detached chat window.

## Features

- **Desktop-first workflow**: use the Electron desktop app for interactive project work.
- **Scriptable agent runs**: use `wuu exec` from shell scripts, CI jobs, or other agents.
- **Bring your own model**: use Anthropic or OpenAI-compatible providers such as OpenAI, OpenRouter, one-api, and local gateways.
- **Project sessions**: list, resume, fork, search, archive, and delete saved sessions.
- **Repo-aware tools**: read, edit, search, patch, and run commands in the current workspace.
- **Attachments**: pass PDFs, text files, and screenshots into a task.
- **Machine-readable output**: stream JSONL from `wuu exec --json` for automation.

## Installation

Use one of the following methods to install the `wuu` command:

```bash
# Homebrew
brew install blueberrycongee/tap/wuu

# npm
npm install -g @blueberrycongee/wuu

# Install script
curl -fsSL https://raw.githubusercontent.com/blueberrycongee/wuu/main/install.sh | sh

# From source
go install github.com/blueberrycongee/wuu/cmd/wuu@latest
```

You can also run the npm package without installing it globally:

```bash
npx @blueberrycongee/wuu@latest --version
```

## Quickstart

Create a project config, then ask Wuu to work in the current repository:

```bash
wuu init
wuu exec "describe this repo"
wuu exec "fix the failing test and verify it"
```

Attach local files or screenshots when they are part of the task:

```bash
wuu exec --file report.pdf "summarize this PDF"
wuu exec --image screenshot.png "find the UI issue"
```

Resume or inspect previous work:

```bash
wuu exec resume --last "continue from the previous task"
wuu session list --json
```

## Desktop App

The desktop app is the main interactive experience for Wuu. To run it from this repository:

```bash
cd desktop
npm install
npm run dev
```

The installed `wuu` command is still useful when you want non-interactive runs, automation, or a backend process for the desktop shell.

## Configuration

Wuu reads project config from `.wuu.json` and global config from `~/.config/wuu/config.json`.

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

Then set the matching API key in your shell:

```bash
export OPENROUTER_API_KEY="..."
export ANTHROPIC_API_KEY="..."
```

## Documentation

- [`wuu exec`](docs/exec.md): automation entrypoint, JSONL output, attachments, resume, and review commands.
- [`app-server` protocol](docs/app-server-protocol.md): protocol used by the desktop app and external shells.
- [`jsonl-events`](docs/jsonl-events.md): event stream reference for automation.
- [`CONTRIBUTING.md`](CONTRIBUTING.md): development setup and contribution guidelines.

## Project Status

Wuu is pre-1.0 and moving quickly. Expect behavior and configuration to change as the desktop app, automation entrypoint, and provider support settle.

## License

MIT
