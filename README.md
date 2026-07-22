<h1 align="center">wuu</h1>

<p align="center">The open-source AI coding agent for local development.</p>

<div align="center">
  <p>
    <a href="README.md">English</a> |
    <a href="README_zh.md">简体中文</a>
  </p>
  <p>
    <a href="https://github.com/blueberrycongee/wuu/actions/workflows/ci.yml"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/blueberrycongee/wuu/ci.yml?branch=main&style=flat-square&label=ci"></a>
    <a href="https://github.com/blueberrycongee/wuu/blob/main/LICENSE"><img alt="License" src="https://img.shields.io/github/license/blueberrycongee/wuu?style=flat-square"></a>
    <a href="https://github.com/blueberrycongee/wuu/releases"><img alt="GitHub release downloads" src="https://img.shields.io/github/downloads/blueberrycongee/wuu/total?style=flat-square&label=downloads"></a>
  </p>
</div>

---

<img width="2272" height="2494" alt="wuu desktop app" src="https://github.com/user-attachments/assets/2d9030aa-ca03-42b1-9333-f79cc5aff95b" />

**wuu** works directly in your local repository. It can read and edit files, run commands, review changes, and carry a task through to completion using a model provider you configure.

Use the desktop app for interactive work. For larger tasks, wuu can make a plan, delegate work to subagents, use task-specific skills, and continue from persistent sessions. `wuu exec` provides a non-interactive entrypoint for scripts, CI, and other agents.

> [!WARNING]
> wuu is an early preview and changes quickly. Packaged desktop builds currently support Apple silicon Macs.

## Download

Download the latest macOS desktop build from [GitHub Releases](https://github.com/blueberrycongee/wuu/releases/latest), move `wuu.app` to `/Applications`, and open it.

The current preview is unsigned and not notarized. If macOS blocks an official release that you trust, run:

```bash
xattr -dr com.apple.quarantine /Applications/wuu.app && open /Applications/wuu.app
```

## Get started

1. Open `wuu.app` and choose a local project folder.
2. Open **Settings → Model providers** and connect Anthropic or an OpenAI-compatible provider.
3. Start a conversation and describe the outcome you want.

For example:

```text
Explain how this repository is organized.
Fix the failing tests and verify the result.
Review the current changes for regressions.
```

See the [user guide](docs/en/getting-started/index.md) for provider setup, attachments, sessions, permissions, and common problems.

## Capabilities

- **Work in your repository** — read and edit files, search code, run commands, and inspect the resulting diff.
- **Handle longer tasks** — plan multi-step work, track durable goals, and continue across context limits.
- **Delegate work** — use subagents for focused research, parallel work, or isolated implementation.
- **Keep useful context** — resume sessions, fork from a checkpoint, and use persistent memory and skills.
- **Bring your own model** — connect Anthropic or any OpenAI-compatible endpoint, including local gateways.
- **Use files and images** — attach documents and screenshots when they are part of the task.
- **Automate workflows** — stream structured output to scripts, CI jobs, review tools, and other agents.

## Automation

`wuu exec` exposes the agent runtime as a non-interactive command:

```bash
wuu exec --json "review the current diff"
wuu exec --file plan.md "implement this plan"
wuu exec review --uncommitted
```

See [`wuu exec`](docs/en/automation/exec.md) for setup, JSONL events, attachments, session controls, and review options.

## Models and data

- You choose the model provider and credentials. Prompts and relevant context are sent to that provider.
- Provider settings and API keys stay under user control rather than coming from the repository.
- Sessions, config, logs, and other local state live under `~/.wuu` by default.
- File changes and commands run locally within the selected workspace and active permission mode.

Read the [security model](docs/en/reference/security-model.md) before using wuu with untrusted repositories or sensitive data.

## Documentation

- [User guide](docs/en/getting-started/index.md)
- [`wuu exec` automation guide](docs/en/automation/exec.md)
- [Security model](docs/en/reference/security-model.md)
- [Roadmap](ROADMAP.md)
- [Public evaluations](evals/)
- [Changelog](CHANGELOG.md)
- [Documentation index](docs/README.md)

## Contributing

Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for development and review guidelines, and [SECURITY.md](SECURITY.md) for vulnerability reports.

If you run into a problem, [open an issue](https://github.com/blueberrycongee/wuu/issues).

## Acknowledgments

wuu draws inspiration from these projects and their work on coding agents, tool loops, orchestration, and developer experience:

- [Codex](https://github.com/openai/codex)
- [OpenCode](https://github.com/anomalyco/opencode)
- [pi](https://github.com/badlogic/pi-mono)
- [Kimi Code](https://github.com/MoonshotAI/kimi-cli)

## License

[MIT](LICENSE)
