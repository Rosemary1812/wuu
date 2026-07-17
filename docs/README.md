# Documentation

This directory contains maintained documentation for using, integrating, and
developing wuu. Start with the section that matches what you are trying to do.

## Use wuu

- [`wuu exec`](exec.md) — run wuu from a terminal, script, CI job, or another agent.
- [Configuration model](configuration-model-zh.md) — understand configuration
  sources, ownership, trust boundaries, and platform-specific behavior (Chinese).

## Build an integration

- [App-server protocol](app-server-protocol.md) — embed the Go core in another shell.
- [JSONL events](jsonl-events.md) — consume the event stream from `wuu exec --json`.
- [Claude Code stream compatibility](compat/cc-stream-json.md) — compare wuu JSONL
  with Claude Code's `stream-json` format.

## Develop and release wuu

- [Development guide](development.md) — set up the repository and run checks.
- [Contributing guide](../CONTRIBUTING.md) — report issues and submit changes.
- [Release guide](release.md) — prepare and verify a release.

## Design and security

- [Security model](security-model.md) — review trust boundaries and security rules.
- [Desktop design notes](../desktop/DESIGN.md) — follow durable interaction and
  visual design decisions for the Electron shell.

Implementation plans and task notes are temporary working material, not maintained
project documentation. Local files under `docs/plans/` are ignored and must not be
committed as permanent references.
