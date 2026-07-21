# Changelog

This file starts the maintained release record. Earlier GitHub Releases may not
have complete change notes.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versioning rules are documented in [the release guide](docs/en/project/release.md).

## [Unreleased]

## [0.10.1] - 2026-07-21

### Added

- Added drag-and-drop from the workspace file tree into composers as path
  references, plus external file drops through the existing attachment checks
  and a 20 MB limit for PDF attachments.

### Fixed

- Preserved the selected final assistant response when deriving a conversation,
  including histories with compaction, provider checkpoints, or retired context
  artifacts, and unified history item projection so derivation and message
  editing use the same canonical source mapping as the visible transcript.
- Redrew the system-theme preview as one aligned split-color window, removing
  overlapping and clipped light/dark layers in Settings.

## [0.10.0] - 2026-07-21

### Added

- Added scheduled progress rechecks for managed background processes
  (`recheck_minutes` on `bash` start/update) so long silent tasks such as
  downloads wake the agent with periodic status snapshots until completion.

### Changed

- Made `bash` background observation bounded and event-driven:
  `read_background` waits return early on new output or process exit with
  pacing for continuously producing processes, and foreground commands that
  hit their timeout now keep running as managed background processes with
  their output attached instead of being killed.

## [0.9.0] - 2026-07-21

### Added

- Added durable app-server Run control for `wuu exec`, including persisted run
  state, structured-output validation, cancellation, and resume support.
- Added read-only agent tools and live agent process items to side threads.

### Changed

- Routed `wuu exec` through the app-server execution model and made JSONL
  terminal states and structured error categories reliable for automation.
- Refined desktop conversation alignment, composer focus behavior, and floating
  status controls.

### Fixed

- Rejected unsupported attachments and unknown machine-input fields before a
  run starts, while preserving the original intent of valid image inputs.

## [0.8.0] - 2026-07-20

### Changed

- Background subagent completions are now steered into the current parent turn
  and injected before the next model step, instead of waiting for the whole turn
  to end. This reduces redundant exploration while the subagent is finishing.

## [0.7.2] - 2026-07-20

### Added

- Added collapsed-sidebar preview when hovering the titlebar sidebar toggle.

### Changed

- Aligned `apply_patch` completion messages and client file-change records with
  Codex while removing redundant patch journals and metadata.

### Fixed

- Preserved trailing Markdown emphasis markers during streamed rendering.

## [0.7.1] - 2026-07-19

### Changed

- Reused composer controls in the user message edit bubble for consistent styling.

## [0.7.0] - 2026-07-19

### Added

- Added Kimi K3 Anthropic compatibility, including K3-specific thinking and empty-signature handling.
- Added context-overflow detection, recovery, and user-visible display for BYOK providers.
- Added dynamic request context compaction, reducing how much transient context enters model requests.
- Added a redesigned Settings page using a flat chassis with instant-apply controls and hover drawers.
- Added token-speed display via hover tooltip on narrow composer bars.
- Added settled agent runs in the terminal workspace surface.
- Added support for diversified subagent name pools.
- Added the ability to switch projects while background runs are still active.
- Added a bilingual user guide covering core desktop and agent workflows.

### Changed

- Unified terminal session history into a single shared history model.
- Dropped derived context ledgers from the default model projection.
- Simplified the sidebar wordmark color treatment.
- Scoped environment Git configuration to the active session workspace.
- Memoized conversation turns on server-event re-renders to improve idle performance.
- Reduced idle renderer activity across the desktop conversation view.
- Kept process summaries neutral and hidden from the main conversation stream.
- Unified composer actions into a single plus menu.

### Fixed

- Interrupted turns now show answer actions and prevent stacked query overlap.
- Turn edit summary cards now appear before answer action buttons.
- Background completion wakeups are now waited on correctly before proceeding.
- Model-aware summary output limits are recovered after compaction.
- Messages can be edited after history compaction.
- Conversation layout is settled before reveal, preventing flicker on tab switch.
- Stream follow pauses during text selection.
- Stable single-line retry error display is shown while a turn retries or recovers.
- First query is shown before thread creation completes.
- Zero-usage model buckets are hidden from the settings usage panel.
- Synchronous shell process trees are stopped correctly.
- Terminal quota replays are stopped during recovery.
- OpenAI Responses WebSocket cache TTL is extended.
- Usage number overflow is prevented in heatmap and inline displays.
- Usage heatmap intensity is restored after theme changes.

## [0.6.0] - 2026-07-17

### Added

- Embedded browser activities can now be automated through a dedicated backend
  and shown in a live macOS picture-in-picture preview.
- The desktop interface now supports English and Simplified Chinese.
- Interrupted turns preserve queued follow-up messages for the next turn.
- Git commits can optionally include WUU Agent attribution.

### Changed

- Runtime model selection is now conversation-scoped, and side chats and
  automatic titles inherit the conversation's pinned model.
- Desktop settings use a grouped-list layout, hover drawers animate in and out,
  and the macOS sidebar preserves the wallpaper tint.
- The composer stop control now uses a smaller solid glyph on a neutral surface.
- The retired `inception` context-rewrite tool has been removed while legacy
  session artifacts remain readable.

### Fixed

- OpenAI Responses WebSocket failures now use reason-specific SSE fallback
  windows, allowing transient failures to recover without a ten-minute pin.
- Truncated tool arguments and empty Anthropic thinking blocks no longer break
  provider history recovery.
- Composer focus handoffs, held-message deduplication, and jump-to-latest state
  remain stable across sends, interrupts, and conversation switches.
- Embedded browser PiP startup and window reparenting no longer race.
- Production desktop builds disable reload and developer-tools shortcuts.
- Git action locks are scoped to the active worktree.

## [0.5.2] - 2026-07-16

### Fixed

- Inline code in message conversations and workspace Markdown previews now
  uses a dark theme-aware surface instead of retaining the light chip background.

## [0.5.1] - 2026-07-16

### Changed

- The message-flow font-size preview now uses the real conversation renderer,
  so it matches message typography, spacing, and Markdown output.

### Fixed

- Theme changes now stay synchronized across all desktop windows and embedded
  terminals, editors, and Mermaid diagrams.
- Bare URLs followed by CJK punctuation no longer absorb the punctuation into
  the link target.

## [0.5.0] - 2026-07-16

### Added

- Scheduled automations now run through persisted threads and turns, support
  new-thread and heartbeat modes, and retain bounded run history.
- The core and desktop source now include Windows-aware process, shell-path,
  window-chrome, and packaging support. GitHub Releases remain macOS-only.

### Changed

- The desktop conversation and composer stay centered at readable widths on
  wide windows, while environment and group panels scale responsively.
- Internal process notifications are hidden from conversations, and command
  and search activity summaries more accurately describe completed work.

### Fixed

- Failed and interrupted turns preserve completed assistant and tool history
  across reloads, including partial response text already shown before a stop.
- Scheduled runs avoid overlapping or unbounded queued work, expired paused
  schedules are removed, and corrupt run-history files no longer block startup.
- Desktop side-panel headers retain their intended layout and typography.

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
