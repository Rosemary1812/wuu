# Changelog

This file starts the maintained release record. Earlier GitHub Releases may not
have complete change notes.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versioning rules are documented in [the release guide](docs/release.md).

## [Unreleased]

## [0.4.1] - 2026-07-16

### Changed

- GitHub Releases now contain only the unsigned macOS arm64 desktop DMG and ZIP
  packages; standalone CLI archives and the npm installer are no longer
  published.

### Fixed

- The desktop app now always runs its bundled private `wuu-core` and no longer
  installs, replaces, or falls back to a standalone `wuu` CLI. A CLI installed
  separately from source can coexist with the app and use a different version.

## [0.4.0] - 2026-07-15

### Added

- Large built-in tool results are settled once at execution time into bounded,
  artifact-backed projections with an explicit recovery path to the complete
  result.
- Structured and rich tool results now keep bounded semantic indexes through
  provider requests and checkpoint compaction, including stable content
  identity and representative values.
- Compacted media references preserve the media type, decoded size, SHA-256
  identity, and image dimensions when available.

### Changed

- Request-time historical tool-output pruning has been removed. Context growth
  is now handled by stable result projection followed by checkpoint compaction,
  so ordinary tool-loop requests keep an append-only cacheable prefix.
- Archived desktop sessions are grouped by project, and conversation search,
  model menus, memory controls, and narrow usage tables use a quieter layout.
- Release versions are synchronized from `VERSION`, validated in CI, and
  published with the matching changelog section.
- The npm wrapper installs the GitHub Release matching its own package version
  instead of silently resolving the latest release.
- Public evaluation claims now have a dedicated, CI-validated evidence format
  under `evals/`; private and exploratory runs remain under ignored `bench/`.

### Fixed

- Rich tool results survive session persistence instead of losing structured
  content or attachment metadata after a restart.
- Compaction and provider projection preserve meaningful mixed structured
  results without duplicating complete payloads into active model context.
- Queued desktop turns no longer hide the preceding final reply, and sidebar
  session loading no longer drops visible history after compaction.

## [0.1.0] - 2026-07-10

### Added

- Unsigned macOS arm64 Electron desktop preview DMG/ZIP artifacts on GitHub
  Releases
- `LICENSE` (MIT) so the project is unambiguously open source
- `CONTRIBUTING.md`, `SECURITY.md`, and `CODE_OF_CONDUCT.md` for open-source
  governance
- `CHANGELOG.md` to track user-visible changes
- `.github/CODEOWNERS`, `.github/ISSUE_TEMPLATE/` (bug report, feature
  request), and `.github/PULL_REQUEST_TEMPLATE.md`
- `.gitignore` entries for `coverage/`, `*.log`, `.idea/`, `.vscode/`, and
  `.env*` environment files
