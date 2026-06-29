<h1 align="center">wuu</h1>

<p align="center">
  <strong>An AI coding workspace with a desktop app and scriptable agent runs.</strong>
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

Wuu keeps an AI coding agent close to your repository. Open the desktop app for an interactive session, or call `wuu exec` when the same work needs to run from a terminal, CI job, or another agent.

It can read and edit files, run commands, attach screenshots and PDFs, review changes, and resume project sessions. It works best when you give it a real signal to close the loop around: a test, a build, a screenshot, a log, or any command whose output tells the agent whether the work is done.

## A typical loop

Point Wuu at a repo, describe the change, and let it gather context before editing. As it works, Wuu can run the checks you would run yourself and use the results to keep going. When the session gets long, the thread can be resumed later from the desktop app or from `wuu exec`.

```bash
wuu init
wuu exec "find why the tests fail, fix the root cause, and run the tests again"
```

For tasks with more context, pass the artifact directly:

```bash
wuu exec --file report.pdf "summarize this and update the docs"
wuu exec --image screenshot.png "trace the UI issue and propose a fix"
```

And for automation:

```bash
wuu exec --json "review the current diff"
wuu exec resume --last "continue from the last failure"
wuu session list --json
```

## Where Wuu helps

- Exploring an unfamiliar codebase and explaining how the pieces fit together.
- Making scoped code changes, then running the checks you specify.
- Reviewing local changes, branches, or commits with repository context.
- Carrying session history across follow-up work.
- Passing files and images into an agent run without pasting them into the prompt.
- Producing JSONL output for scripts and CI.

## Install

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

You can also run the npm package directly:

```bash
npx @blueberrycongee/wuu@latest --version
```

Release binaries are available on the [GitHub Releases](https://github.com/blueberrycongee/wuu/releases) page.

## Configure a model

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

Then set the matching API key:

```bash
export OPENROUTER_API_KEY="..."
export ANTHROPIC_API_KEY="..."
```

## Desktop app

The desktop app is the main interactive surface for Wuu. To run it from this repository:

```bash
cd desktop
npm install
npm run dev
```

The `wuu` binary also exposes the app-server used by the desktop shell, so future shells and local tools can drive the same runtime.

## Docs

- [`wuu exec`](docs/exec.md): non-interactive runs, JSONL output, attachments, resume, and review commands.
- [`app-server` protocol](docs/app-server-protocol.md): the protocol used by the desktop app and external shells.
- [`jsonl-events`](docs/jsonl-events.md): event stream reference for automation.
- [`CONTRIBUTING.md`](CONTRIBUTING.md): development setup and contribution guidelines.

## Status

Wuu is pre-1.0. The desktop app, automation entrypoint, provider support, and configuration format are still evolving.

## License

MIT
