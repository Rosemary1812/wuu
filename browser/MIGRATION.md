# Wuu Browser Migration Plan

This plan tracks the path from the current Wuu repository to an installable Wuu
Browser product repository.

## Current Evidence

The current local migration state is split across three places:

- Wuu main repository: `/Users/blueberrycongee/wuu`
- BrowserOS reference repository: `~/wuu-browseros`
- Chromium checkout: `~/browseros-chromium/src`

The committed product repository should replace this implicit local state with
tracked product code, scripts, patches, and verification steps.

Current BrowserOS reference facts observed during migration:

- BrowserOS reference branch: `dev`
- BrowserOS reference commit: `2eab1001353b`
- BrowserOS reference has local Wuu integration commits ahead of upstream.
- BrowserOS Chromium pin:
  - `CHROMIUM_VERSION`: `146.0.7680.31`
  - `BASE_COMMIT`: `4d3225104176d`

Current Chromium checkout facts observed during migration:

- Checkout HEAD starts at BrowserOS `BASE_COMMIT`.
- The checkout is dirty because BrowserOS patches and Wuu-specific experiments
  have already been applied locally.
- The built/modified `chrome/VERSION` may differ from `browser/CHROMIUM_VERSION`
  while patches are applied; the source of truth for a clean setup remains
  `browser/CHROMIUM_VERSION` and `browser/BASE_COMMIT`.

Use `make browser-status` for the current machine-readable snapshot.

## Phase 1: Product Repository Skeleton

Status: in progress.

Goals:

- Make `/Users/blueberrycongee/wuu` the current Wuu Browser product worktree.
- Add `browser/` as the browser product entry point.
- Record product constraints, Chromium pinning, and local checkout policy.
- Add a status command that can inspect local BrowserOS and Chromium checkouts.

Non-goals:

- Do not move the full Chromium checkout into this repository.
- Do not attempt installer packaging before the dev browser path is stable.
- Do not rewrite the Wuu workbench during this phase.

## Phase 2: BrowserOS Build and Patch Tooling

Status: in progress.

Goals:

- Import or adapt BrowserOS-style build tooling into `browser/`.
- Preserve patch-based Chromium source management:
  - `CHROMIUM_VERSION`
  - `BASE_COMMIT`
  - `chromium_patches/`
  - `series_patches/`
  - resources and replacement files
- Add commands for:
  - setup/sync Chromium checkout
  - apply Wuu Browser patches
  - build local dev browser
  - launch dev browser with a clean profile

Expected outcome:

- A developer can start from this repository, prepare the browser checkout, and
  build or launch the Wuu Browser development app without relying on hidden
  manual notes.

Current import policy:

- BrowserOS browser-layer assets are imported with
  `make browser-import-browseros`.
- The import includes BrowserOS build scripts, Chromium patches, replacement
  files, resources, series patches, and patch tooling.
- The import excludes full Chromium source checkouts, virtual environments,
  local logs, build outputs, and downloaded runtime caches.
- Imported code is the starting point for Wuu Browser. Product defaults that
  conflict with Wuu Browser, such as first-run onboarding and VM prewarm, should
  be changed after import rather than treated as final behavior.

## Phase 3: Wuu Workbench Integration

Status: in progress.

Goals:

- Migrate or replace the Wuu BrowserOS integration proven in the BrowserOS
  reference checkout.
- The default/first product tab must render the full Wuu workbench.
- The workbench must talk to the real Wuu native runtime.
- Native folder picking, project switching, thread start/resume, prompt send,
  terminal, file, and Git flows should use real runtime paths.

Known source material:

- Wuu desktop renderer under `desktop/src/`.
- Imported BrowserOS agent baseline under `browser/agent/`.
- Imported BrowserOS reference Wuu integration under
  `browser/agent/apps/agent/entrypoints/app/wuu-desktop/`.
- Imported BrowserOS reference Wuu server route under
  `browser/agent/apps/server/src/api/routes/wuu.ts`.
- Imported BrowserOS reference Wuu desktop runtime service under
  `browser/agent/apps/server/src/api/services/wuu/desktop-runtime.ts`.

Current import policy:

- BrowserOS agent/server assets are imported with `make browser-import-agent`.
- The import includes the extension app, BrowserOS server source, shared
  packages, development scripts, BrowserOS CLI source, and the Wuu bridge code.
- The import excludes `node_modules`, extension/browser build outputs, local env
  files, dogfood tooling, and eval benchmark data.
- Wuu-owned adaptations such as the default background install route and server
  source ignore rules are preserved during refresh instead of overwritten by
  the BrowserOS baseline.
- The imported baseline is not the final product behavior. Wuu Browser still
  needs route/default startup changes, native Wuu runtime alignment, and VM
  default removal.

Migration rule:

- Do not keep a long-term forked copy of the Wuu renderer unless there is a
  clear build reason. Prefer a shared workbench package or generated adapter so
  the browser workbench and desktop transition path do not drift silently.

Current verification gap:

- `browser/agent/apps/agent/entrypoints/background/index.ts` now opens
  `app.html#/home` on extension install, so the source default points at the
  Wuu workbench instead of BrowserOS onboarding.
- `browser/scripts/launch-dev.sh` now starts directly at the product route,
  `chrome://browseros/wuu`, and prefers the current repository's built
  extension before falling back to the external BrowserOS reference dist.
- `make browser-verify-product-defaults` guards stable defaults: first-run
  Chromium patches and dev launch must open the Wuu workbench route instead of
  BrowserOS welcome.
- Runtime verification requires rebuilding the extension from
  `browser/agent/apps/agent` and launching a fresh profile that loads that
  built extension.
- `make browser-verify-dev ARGS="--require-project-folder-picker"` verifies
  that the browser-hosted Wuu workbench has the native
  `chrome.browserOS.choosePath` folder picker API and that the project picker
  path adds the selected folder as the active Wuu project.
- `make browser-verify-dev ARGS="--require-project-local-ops"` verifies that
  the selected browser-hosted Wuu project drives real file tree, file read,
  Git status, and terminal startup operations in the selected local directory.
- `make browser-verify-dev ARGS="--require-wuu-turn-start"` verifies that the
  browser-hosted Wuu workbench can submit a prompt to the local app-server,
  receive a real running turn, and interrupt it without waiting for model
  completion.

## Phase 4: Remove Misaligned Runtime Defaults

Status: in progress.

Goals:

- Disable OpenClaw, Hermes, and Lima VM auto-start on the default Wuu Browser
  path.
- Keep those systems only as optional sandbox runtimes if they remain useful.
- Remove fake UI and prompt-based workarounds from the Wuu default tab path.

Product invariant:

- Normal Wuu Browser startup should not launch a VM.
- Normal Wuu project/file/terminal work should not require a VM.

Current default:

- Browser server startup configures only host process agent runtimes by default.
- OpenClaw/Hermes/Lima setup and prewarm require `WUU_ENABLE_VM_AGENTS=1`.
- `make browser-verify-product-defaults` statically checks that the default
  server path does not configure the VM runtime.
- `make browser-verify-dev ARGS="--require-wuu-runtime --require-no-vm-agents"`
  verifies the running dev browser uses the Wuu runtime without VM-backed agent
  processes.

## Phase 5: Browser Bridge

Status: in progress.

Goals:

- Define a minimal native/browser bridge between Wuu runtime and Chromium.
- Start with verifiable browser operations:
  - list tabs
  - inspect active tab metadata
  - capture screenshot or accessibility state
  - navigate
  - click/type/scroll
  - connect to DevTools/CDP for local debugging
- Keep the first API small and evidence-driven.

Non-goal:

- Do not design a broad agent abstraction before the browser operations work in
  a real running Wuu Browser build.

Current first bridge:

- `GET /browser-bridge/tabs` returns the current CDP-backed tab list and active
  tab metadata using stable Wuu page IDs and Chromium tab IDs.
- `GET /browser-bridge/active-tab` returns the active tab metadata directly.
- `POST /browser-bridge/tabs` creates a new tab.
- `POST /browser-bridge/tabs/:targetId/activate` activates a tab target.
- `POST /browser-bridge/tabs/:targetId/navigate` navigates a tab target.
- `POST /browser-bridge/tabs/:targetId/back` navigates a tab target backward
  through browser history.
- `POST /browser-bridge/tabs/:targetId/forward` navigates a tab target forward
  through browser history.
- `POST /browser-bridge/tabs/:targetId/reload` reloads a tab target.
- `DELETE /browser-bridge/tabs/:targetId` closes a tab target.
- `GET /browser-bridge/tabs/:targetId/screenshot` captures a tab screenshot.
- `GET /browser-bridge/tabs/:targetId/content` reads tab text content.
- `GET /browser-bridge/tabs/:targetId/snapshot` reads a concise or enhanced
  accessibility snapshot for interaction planning.
- `GET /browser-bridge/tabs/:targetId/dom` reads DOM HTML, optionally scoped by
  CSS selector.
- `POST /browser-bridge/tabs/:targetId/evaluate` runs JavaScript in the page
  context for targeted inspection and debugging.
- `GET /browser-bridge/tabs/:targetId/console` reads console logs, warnings,
  and errors captured from the tab target.
- `GET /browser-bridge/tabs/:targetId/network` reads recent network requests,
  HTTP statuses, and failed loads captured from the tab target.
- `POST /browser-bridge/tabs/:targetId/click` clicks a coordinate.
- `POST /browser-bridge/tabs/:targetId/type` clicks and types into a coordinate.
- `POST /browser-bridge/tabs/:targetId/scroll` scrolls a tab.
- `make browser-verify-dev ARGS="--require-browser-bridge"` checks this against
  a running Wuu Browser Dev instance.

## Phase 6: Installable Product

Status: in progress.

Goals:

- Build macOS app artifacts.
- Add `.dmg` packaging path.
- Add Windows installer path.
- Preserve code signing and platform permission requirements.
- Keep Linux packaging as a later extension unless it becomes necessary sooner.

Completion evidence:

- A local user can install and open Wuu Browser as a normal browser app.
- The default tab is the Wuu workbench.
- At least one real local project flow and one real agent turn work from the
  packaged app or a packaging-equivalent build.

Current packaging path:

- `make browser-package-dev-macos` stages `browser/out/Wuu Browser Dev.app`
  from the current Chromium dev build and applies Wuu Browser visible app
  metadata, bundles component-build `.dylib` files into the staged app, then
  ad-hoc signs it for local launch.
- `make browser-package-dev-macos ARGS="--dmg"` creates a local development
  DMG from the staged app.
- `make browser-launch-dev` prefers the repo-staged Wuu Browser Dev app before
  falling back to the external BrowserOS Chromium build.

Remaining gap:

- This is a development staging path, not the final signed/notarized release
  pipeline. The internal executable/framework names can still inherit BrowserOS
  until the Chromium branding patches are fully renamed.
