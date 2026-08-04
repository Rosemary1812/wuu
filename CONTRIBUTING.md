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
the problem you are trying to solve, not just the solution. Small bug fixes,
tests, documentation, accessibility improvements, and focused refactors can go
straight to a pull request. Open an issue first for new product flows, protocol
changes, large dependency or architecture changes, security model changes, and
work that affects compatibility across shells.

## Submitting code changes

### Development setup

- Go: use the version in `go.mod`
- Node.js: use Node 22 or newer; `.node-version` matches CI
- Install repository dependencies: `make setup`
- Start the desktop development path: `make dev`
- Run the cross-platform local gate: `make ci`
- On macOS, also run `make build-macos` when desktop packaging parity with CI is required

See [the development guide](docs/en/project/development.md) for component commands,
supported platforms, CI checks, architecture boundaries, and restart behavior.
`AGENTS.md` contains additional automation instructions but is not required
reading for human contributors.

The current supported build targets are macOS and Linux for the CLI and arm64
macOS for the desktop preview. Mobile and remote control are experimental and
do not yet have a stable public mobile release.

### AI-assisted contributions

AI-assisted pull requests are welcome. The author remains responsible for the
design, license compliance, security, and correctness of every line. Review the
complete diff, remove unrelated generated changes, and run the relevant real
tests. Do not submit raw model output or claim checks passed when they were not
run.

### Commit conventions

- One logical change per commit; do not bundle unrelated edits
- Commit messages in English, conventional-commits style:
  - `feat(scope): ...` for new features
  - `fix(scope): ...` for bug fixes
  - `chore: ...` for housekeeping
  - `docs: ...` for documentation only
  - `refactor(scope): ...` for refactors with no behavior change
- Reference the relevant issue or design doc in the body when applicable

### Versions and release notes

- Add user-visible changes to the `[Unreleased]` section of `CHANGELOG.md`.
- Do not edit product package versions by hand; `VERSION` is synchronized with
  `make release-prepare RELEASE_VERSION=<version>`.
- Compatible fixes use a patch release. Features and compatibility-sensitive
  protocol, configuration, data, or behavior changes use a minor release while
  wuu remains pre-1.0.
- Only maintainers create release tags. See [the release guide](docs/en/project/release.md).

### Pull request process

1. Branch from `main`; do not touch unrelated files in the same PR
2. Run the relevant component commands, then `make ci` when practical
3. Open the PR using the [pull request template](.github/PULL_REQUEST_TEMPLATE.md)
4. Address review feedback with additional commits; avoid force-push after review starts
5. A maintainer will squash-merge on approval

## Project structure

- `cmd/wuu/` — CLI entry point and the `wuu exec` / `wuu app-server` subcommands
- `internal/` — Go core: agent runtime, providers, tool loop, sessions, config
- `desktop/` — Electron shell (renderer + main process)
- `packages/protocol/` — shared app-server protocol types
- `clients/core/` — remote-control client core
- `clients/mobile/` — Expo mobile shell
- `docs/` — Maintained user, protocol, development, and design documentation; see
  [`docs/README.md`](docs/README.md) for the index
- `prototypes/` — Throwaway design exploration; not shipped
