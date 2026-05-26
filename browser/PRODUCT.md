# Wuu Browser Product Direction

This document records stable product constraints for the Wuu Browser migration
inside Wuu's dual-surface product model. Implementation details may change as
the code moves, but these product constraints should not be weakened without an
explicit product decision.

## Final Product

Wuu's future product has two first-class app surfaces:

- Electron desktop.
- Wuu Browser.

Both surfaces must use one shared core workbench and native runtime path. The
Electron app remains the desktop-focused surface; Wuu Browser becomes the
Chromium-based browser surface.

Wuu Browser should be an installable Chromium-based browser product. The
browser artifact should be a normal browser application, such as a macOS
`.app`/`.dmg` and a Windows installer, with Linux packages as a later extension.

The browser must preserve standard Chrome-level browsing expectations:

- Tabs and tab lifecycle.
- Address bar, navigation, reload, home, history, and downloads.
- Browser settings, extension support, and DevTools.
- Normal browsing performance and startup behavior.
- Platform packaging, signing, updates, and permissions.

The first/default browser product tab is the Wuu workbench. It must feel like a
native part of the browser product, not like an external website or a temporary
demo. It must also stay aligned with the Electron desktop surface because both
surfaces are views over the same core product flow.

## Wuu Workbench Requirements

The Wuu workbench inside both Electron desktop and Wuu Browser must use the real
Wuu UI and the real Wuu native runtime. The browser surface should preserve the
current Electron desktop product shape:

- Project list and project switching.
- Native folder selection and project creation.
- Conversation threads, streaming turns, and interruption.
- Model/provider selection and settings.
- Workspace files panel.
- Git review and diff surfaces.
- Integrated terminal.
- Managed processes and local development servers.
- Tool activity, sub-agent activity, and user prompts.

Fake UI, prompt-based folder entry, simplified demo pages, or style-only
rewrites do not satisfy this requirement. If temporary scaffolding is needed
during migration, it should be clearly marked and removed once the real path is
available.

## Runtime Model

The default local capability layer is the Wuu native runtime running on the host
machine. Electron desktop and Wuu Browser must call into the same core runtime
path instead of growing separate local capability implementations.

Wuu Browser should not depend on OpenClaw, Hermes, Lima, or any VM/container
runtime for its default startup path or for normal local workbench behavior.
Those components may remain useful as optional sandbox runtimes later, but they
must not be required for:

- Opening the browser.
- Opening the default Wuu tab.
- Selecting a local project.
- Reading/writing local workspace files.
- Running Git operations.
- Running a terminal or local development server.
- Starting a normal Wuu agent turn.

Cross-platform support should be handled through native platform adapters for
macOS, Windows, and Linux. A VM is not the default answer to multi-platform
local capability support.

## BrowserOS and Chromium Strategy

BrowserOS is the reference product and build system for the browser layer. Wuu
Browser should reuse useful BrowserOS work instead of rebuilding Chromium
integration from scratch.

The repository should follow the same general Chromium source strategy:

- Pin the Chromium base with version and commit files.
- Store browser changes as patches, replacement files, resources, and build
  scripts.
- Keep the full Chromium checkout outside Git history.
- Make the product repository sufficient to fetch/apply/build the browser.

This means the repository contains Wuu Browser's browser code, but not a full
copy of unmodified Chromium source.

## Browser Agent Bridge

The long-term bridge between Wuu and the browser should let agents operate the
real browser environment, including:

- Current tab and tab list.
- Page URL, title, DOM, selected text, screenshots, and accessibility state.
- Navigation, click, type, scroll, and form operations.
- DevTools/CDP inspection and local development server debugging.
- Browser downloads, permissions, and authenticated browsing sessions.

The first bridge milestones should be concrete and verifiable. Prefer real
browser operations exposed through Chromium/BrowserOS APIs over abstract agent
layers that cannot be tested against a running browser.

## Migration Policy

The current repository remains the active work directory during migration.
Long-term, the repository may be reshaped around shared Wuu core packages plus
two app surfaces: Electron desktop and Wuu Browser.

Every migration step should move the current state closer to the installable
browser product:

- Product code and build entry points should move into this repository.
- Local reference work in external checkouts should be migrated, not left as
  undocumented manual state.
- The browser default tab should converge on the complete Wuu workbench.
- Electron desktop and Wuu Browser should converge on one shared workbench and
  runtime path instead of separate product flows.
- Default startup should become lighter by removing unnecessary VM prewarm from
  the main path.
- Verification should use real browser runtime behavior, not only static code
  inspection.

## Non-Goals

- Do not fork the workbench or native runtime into separate Electron-only and
  browser-only core implementations.
- Do not let Electron desktop and Wuu Browser drift into different product
  flows for the same user task.
- Do not ship a Chrome extension as the primary product shell.
- Do not commit the full vanilla Chromium checkout into Wuu's Git history.
- Do not make OpenClaw/Hermes/Lima VM the default Wuu runtime.
- Do not replace the full Wuu workbench with a simplified mock UI.
