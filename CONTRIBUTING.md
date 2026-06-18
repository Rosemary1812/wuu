# Contributing to wuu

Thanks for your interest in contributing. This document explains how to report
issues, suggest features, and submit code changes.

By participating, you agree to follow our [Code of Conduct](./CODE_OF_CONDUCT.md).
This project is released under the [MIT License](./LICENSE); by contributing you
agree your contributions will be licensed under the same terms.

## Reporting bugs

Open a [bug report](.github/ISSUE_TEMPLATE/bug_report.md) and include:

- A clear, descriptive title
- Steps to reproduce, with the exact command or UI flow
- Expected vs actual behavior
- Environment: OS, Go and Node versions, output of `wuu --version`
- Relevant logs (redact API keys and any credentials first)

## Suggesting features

Open a [feature request](.github/ISSUE_TEMPLATE/feature_request.md) describing
the problem you are trying to solve, not just the solution. Larger changes
should land as a design discussion before code is written; small fixes and
refactors can go straight to a pull request.

## Submitting code changes

### Development setup

- Go: see `go.mod` for the required toolchain
- Node.js: see `desktop/package.json` for the desktop shell
- Build and install the CLI: `make install`
- Run tests:
  - Go: `go test ./...`
  - Desktop: `cd desktop && npm test`

The full project layout, coding standards, and engineering rules live in
[`AGENTS.md`](./AGENTS.md). Read that before opening a pull request.

### Commit conventions

- One logical change per commit; do not bundle unrelated edits
- Commit messages in English, conventional-commits style:
  - `feat(scope): ...` for new features
  - `fix(scope): ...` for bug fixes
  - `chore: ...` for housekeeping
  - `docs: ...` for documentation only
  - `refactor(scope): ...` for refactors with no behavior change
- Reference the relevant issue or design doc in the body when applicable

### Pull request process

1. Branch from `main`; do not touch unrelated files in the same PR
2. Make sure tests pass and the diff is focused
3. Open the PR using the [pull request template](.github/PULL_REQUEST_TEMPLATE.md)
4. Address review feedback with additional commits; avoid force-push after review starts
5. A maintainer will squash-merge on approval

## Project structure

- `cmd/wuu/` — CLI entry point and the `wuu exec` / `wuu app-server` subcommands
- `internal/` — Go core: agent runtime, providers, tool loop, sessions, config
- `desktop/` — Electron shell (renderer + main process)
- `docs/` — Design docs and protocol references
- `prototypes/` — Throwaway design exploration; not shipped
