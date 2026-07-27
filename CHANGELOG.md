# Changelog

This file starts the maintained release record. Earlier GitHub Releases may not
have complete change notes.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versioning rules are documented in [the release guide](docs/en/project/release.md).

## [Unreleased]

### Fixed

- Fixed Git attribution in Windows Git Bash shells when Git resolves without an
  `.exe` suffix.

## [0.12.2] - 2026-07-25

### Added

- Added conversation search across workspaces so matching sessions can be found
  without first switching to their project.
- Added in-app PDF previews to the workspace file viewer.

### Changed

- Redesigned the desktop skills catalog with clearer grouping, compact summaries,
  simpler headings, and distinctive deterministic artwork for custom skills.

### Fixed

- Preserved composer focus when starting a conversation in another project.
- Kept PDF pages white while making the surrounding viewer and toolbar follow the
  active light or dark theme.

## [0.12.1] - 2026-07-25

### Changed

- Simplified the main Agent's delegated-work guidance so completed subagent tasks
  are always integrated and verified before the overall task is reported complete,
  while per-message result-card instructions stay out of the stable base prompt.
- Restored concise progress updates before non-trivial tool use and during longer
  work so users can see what the Agent is doing without narrating every action.

## [0.12.0] - 2026-07-25

### Added

- Added the agent collaboration workspace with room chat, message attachments,
  Markdown rendering, room membership context, a draggable relationship graph,
  a task board, threaded replies, and jump-to-latest controls.
- Added native macOS voice input with saved preferences, optional BYOK text
  polishing, live recording feedback, and a responsive waveform beside the
  composer send control.
- Added desktop skill preview dialogs that show the underlying Skill content and
  keep the preview body scrolling inside a stable modal.
- Added cross-session Dream memory consolidation and stable continuation for
  large projected tool results.

### Removed

- Removed the Agent Templates section from the desktop skills catalog and the
  underlying runtime discovery in `internal/agenttemplate/`. The desktop
  rendered Claude Code-style `.claude/agents/*.md` files when present, but
  nothing in the desktop UI or in `spawn_agent` ever consumed the
  discovery to trigger work, so the section was effectively dead UI.
  Restoring it later is a small change once a real spawn/invoke path
  exists. Drops the `agent-template/list` IPC method, the `agent_template`
  extension kind, the `AgentTemplate*` protocol types, and the
  `agent_template_count` field on initialize.

### Fixed

- Fixed resumed goals so they start a continuation turn instead of only changing
  the banner status, and clarified idle active goals as ready to continue.
- Returned a recovery-focused error when the goal tool tries to complete a
  blocked or paused goal before the user resumes it.
- Improved channel reliability and reading behavior across repeated agent wakes,
  missed inbox checks, resize, message polling, scrolling, and composer overlap.
- Kept voice transcripts stable through polishing and direct-send flows, avoided
  duplicate transcription, and steered running turns consistently from voice,
  Enter, and the send button.
- Let completed Agent command output use the full terminal workspace width so
  long paths and diff statistics no longer wrap against an empty terminal list.
- Fixed responsive session actions, side-chat composer restoration, hero composer
  focus after send failures, conversation status alignment, plan progress width,
  and the Windows conversation search shortcut hint.
- Centered the skills catalog content within its scroll region on the desktop.
- Removed count badges from the skills catalog section headings on the desktop.

## [0.11.1] - 2026-07-23

### Fixed

- Restored the standard left-sidebar toggle in full-panel workspaces, including
  hover preview, pinned expansion, and native macOS titlebar hit testing.

## [0.11.0] - 2026-07-23

### Added

- Added a cloud agent core host contract, configurable Dream settings,
  subagent model aliases, and named-agent group chat support in the core.
- Added native macOS file actions and a dockable workspace file tree with
  document-focused turn and final-answer views.

### Changed

- Consolidated pending and document-run messages into clearer composer drawers
  with consistent progress, selection, and action behavior.
- Made project and conversation switching immediate, and allowed projects to be
  switched directly while browsing files without leaving the full-panel view.

### Fixed

- Fixed IME confirmation and composition timing so confirmed text sends once,
  while queued input remains available during agent delivery.
- Fixed workspace navigation details including real file-menu app icons,
  file-tree context actions, sidebar restoration, scrolling intent, and stream
  cursors around fenced code blocks.

### Removed

- Removed the Agent Templates section from the desktop skills catalog and the
  underlying runtime discovery in `internal/agenttemplate/`. The desktop
  rendered Claude Code-style `.claude/agents/*.md` files when present, but
  nothing in the desktop UI or in `spawn_agent` ever consumed the
  discovery to trigger work, so the section was effectively dead UI.
  Restoring it later is a small change once a real spawn/invoke path
  exists. Drops the `agent-template/list` IPC method, the `agent_template`
  extension kind, the `AgentTemplate*` protocol types, and the
  `agent_template_count` field on initialize.

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
