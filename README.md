<h1 align="center">wuu</h1>

<p align="center">Open source AI coding agent with a desktop app and a scriptable CLI.</p>

<p align="center">
  <a href="https://github.com/blueberrycongee/wuu/releases"><img alt="Release" src="https://img.shields.io/github/v/release/blueberrycongee/wuu?style=flat-square"></a>
  <a href="https://github.com/blueberrycongee/wuu/actions/workflows/ci.yml"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/blueberrycongee/wuu/ci.yml?branch=main&style=flat-square&label=ci"></a>
  <a href="https://www.npmjs.com/package/@blueberrycongee/wuu"><img alt="npm" src="https://img.shields.io/npm/v/@blueberrycongee/wuu?style=flat-square"></a>
  <a href="https://github.com/blueberrycongee/wuu/blob/main/LICENSE"><img alt="License" src="https://img.shields.io/github/license/blueberrycongee/wuu?style=flat-square"></a>
</p>

<p align="center">
  <a href="README.md">English</a> |
  <a href="README_zh.md">简体中文</a> |
  <a href="docs/exec.md">Docs</a> |
  <a href="CONTRIBUTING.md">Contributing</a>
</p>

---

Wuu helps with software development tasks inside a local repository. It can read and edit files, run commands, review changes, attach files or screenshots, and resume previous sessions.

Use the desktop app for interactive work. Use `wuu exec` when you want the same agent from scripts, CI, or another tool.

## Installation

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

You can also run it without a global install:

```bash
npx @blueberrycongee/wuu@latest --version
```

Release binaries are available on the [releases page](https://github.com/blueberrycongee/wuu/releases).

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

## Desktop App

The desktop app is the primary interactive interface for Wuu. To run it from this repository:

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

Wuu supports Anthropic and OpenAI-compatible providers such as OpenAI, OpenRouter, one-api, and local gateways.

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

Wuu is pre-1.0 and under active development. Interfaces, configuration, and desktop behavior may change.

## License

MIT
