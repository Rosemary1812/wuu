# Wuu Browser

This directory is the product entry point for Wuu Browser, one of Wuu's two
first-class app surfaces.

Wuu's future product model is dual-surface: Electron desktop and Wuu Browser
both stay first-class, but they must share one core workbench and native runtime
path. Wuu Browser is intended to become an installable Chromium-based AI
browser, not a browser extension or a developer-only assembly of local parts.
The browser surface combines:

- Chrome-level browser experience: tabs, omnibox, navigation, extensions,
  downloads, settings, DevTools, and platform packaging.
- The full Wuu workbench as the default product tab, backed by the same real
  Wuu native runtime and core flow used by Electron desktop.
- A future browser bridge that lets the Wuu agent inspect and operate browser
  tabs, web pages, DevTools/CDP state, and local development servers.

## Repository Model

Wuu Browser follows the BrowserOS-style Chromium source model:

- Keep the browser product code, patches, resources, build scripts, package
  scripts, and verification flow in this repository.
- Pin the Chromium base with `CHROMIUM_VERSION` and `BASE_COMMIT`.
- Store Chromium changes as `chromium_patches/`, `series_patches/`, replacement
  resources, and build tooling.
- Do not commit the full vanilla Chromium checkout into this Git repository.
  A real Chromium source checkout is prepared locally by scripts under an
  ignored worktree/cache path.

This keeps the open-source repository complete as a product while avoiding a
multi-million-file Chromium mirror inside Wuu's normal Git history.

## Current Layout

```text
browser/
  CHROMIUM_VERSION       Pinned Chromium version inherited from BrowserOS.
  BASE_COMMIT            Pinned Chromium base commit.
  PRODUCT.md             Product and engineering constraints for Wuu Browser.
  chromium_patches/      Wuu Browser patches applied to Chromium source.
  series_patches/        Ordered patch series when individual file patches are insufficient.
  resources/             Browser branding, entitlements, and packaged resources.
  agent/                 BrowserOS agent/server source used by the browser UI and bridge.
  scripts/               Local sync, status, build, launch, and verification entry points.
```

The first migration steps make the browser product model explicit and bring the
BrowserOS patch/build/agent baselines into this repository. Later steps should
adapt those baselines so the default tab is the Wuu workbench backed by the same
Wuu native runtime path as the Electron desktop app.

## Local Checkouts

By default, scripts look for local source checkouts in:

```text
.worktrees/browseros
.worktrees/chromium/src
```

Prepare or refresh those local checkouts from the repository-owned pins:

```bash
make browser-prepare-checkouts ARGS="--dry-run"
make browser-prepare-checkouts ARGS="--apply-patches"
```

The prepare command clones or updates the BrowserOS reference checkout, checks
out the pinned Chromium `BASE_COMMIT`, runs `gclient sync`, and can immediately
apply the tracked Wuu Browser patches. The checkouts stay under ignored
`.worktrees/` paths so the product repository carries the patch/build system
without committing the full Chromium source tree.

During the current migration, the scripts also recognize the existing local
reference checkouts if present:

```text
~/wuu-browseros
~/browseros-chromium/src
```

Override either location when needed:

```bash
WUU_BROWSEROS_REPO=/path/to/BrowserOS \
WUU_CHROMIUM_SRC=/path/to/chromium/src \
make browser-status
```

Verify stable product defaults in tracked browser patches and launch scripts:

```bash
make browser-verify-product-defaults
```

The default internal product URL is `chrome://wuu`. The old
`chrome://browseros/wuu` route is kept only as a compatibility alias while the
BrowserOS-derived patch namespace is still being migrated.

The default Wuu Browser path does not start OpenClaw, Hermes, or Lima. VM-backed
agents are opt-in for future sandbox/runtime work through `WUU_ENABLE_VM_AGENTS=1`.

Build the local development browser from a prepared checkout:

```bash
make browser-build-dev ARGS="--dry-run"
make browser-build-dev ARGS="--prepare --package-macos"
```

The dev build command runs the repository-owned BrowserOS build modules against
the selected Chromium checkout. By default it expects patches to already be
applied and runs `resources,bundled_extensions,chromium_replace,configure,compile`
plus `sparkle_setup` on macOS; pass `--prepare` when starting from a clean
checkout. On macOS, `--package-macos` then stages
`browser/out/Wuu Browser Dev.app`.

For native Chromium UI iteration after a full dev app has already been built,
prefer a narrow Ninja target instead of rebuilding the default `chrome` and
`chromedriver` targets:

```bash
make browser-build-dev ARGS="--modules compile --ninja-targets libchrome_dll.dylib --package-macos"
```

This is the fast path for C++ browser-chrome changes such as toolbar, sidebar,
bookmark bar, or tab strip behavior. The compile module falls back to the
Chromium checkout's bundled `ninja` when `autoninja` is not on `PATH` or the
checkout's `depot_tools` wrapper is not bootstrapped, so a prepared checkout can
build without a separate shell setup step.

Build the repo-owned Wuu workbench extension and local server resources:

```bash
make browser-build-agent ARGS="--dry-run --all"
make browser-build-agent ARGS="--extension"
```

The extension build produces
`browser/agent/apps/agent/dist/chrome-mv3-dev`, which is the default extension
loaded by `make browser-launch-dev`. If that repo-owned dist is missing and
`WUU_BROWSER_EXTENSION` is not set, `browser-launch-dev` builds it before
launching instead of requiring the external BrowserOS checkout. The same launch
path stages server resources through `browser-build-agent --server`, with
`WUU_BROWSER_SERVER_TARGET` available when the host target needs to be
overridden. `browser-launch-dev` also stops existing Wuu Browser Dev/BrowserOS
Dev launches that use temporary `wuu-browser-dev` profiles before starting a
new instance, so repeated verification does not leave stacks of browser
processes behind. Pass `ARGS="--no-cleanup-existing"` or set
`WUU_BROWSER_CLEANUP_EXISTING=0` when debugging multiple dev instances
intentionally.

Stage the local macOS development app under this repository:

```bash
make browser-package-dev-macos
```

This creates `browser/out/Wuu Browser Dev.app` from the current Chromium dev
build, applies Wuu Browser visible bundle metadata, and ad-hoc signs the staged
app for local launch. For component-build Chromium outputs, the required build
root `.dylib` files are copied into the staged app bundle so it can launch
outside the original Chromium `out/` directory. Add `ARGS="--dmg"` to also
create a local development DMG. Some internal build-system executable/framework
names may still inherit BrowserOS until the remaining build modules are renamed.

Stage a production-named local macOS app:

```bash
make browser-package-macos
make browser-package-macos ARGS="--dmg"
```

This creates `browser/out/Wuu Browser.app` and, with `--dmg`,
`browser/out/Wuu Browser.dmg`. The default signing mode is ad-hoc, so the
artifact is suitable for local install and smoke testing but is not a signed,
notarized public release. If the only available Chromium source app is a dev
build, pass `ARGS="--allow-dev-source"` to make that local preview explicit.

Do not treat auto-update as enabled unless the package command succeeds with
`ARGS="--update-enabled"`. That gate intentionally fails while browser,
extension, or server update feeds still point at BrowserOS infrastructure, or
while release signing inputs are missing. A true update-ready release needs
Wuu-owned Sparkle appcasts, extension update manifests, server OTA feeds, and
Developer ID signing/notarization.

Verify the running dev browser, Wuu native runtime, prompt-to-turn startup,
native project folder selection wiring, selected-project local file/Git/terminal
operations, and first Browser Bridge tab observation/debug/action path:

```bash
make browser-verify-dev ARGS="--require-wuu-runtime --require-wuu-turn-start --require-project-folder-picker --require-project-local-ops --require-no-vm-agents --require-browser-bridge"
```

## Patch Workflow

Inspect the current Chromium checkout without changing it:

```bash
make browser-patch-check
```

Apply the repository-owned patches to a clean checkout:

```bash
make browser-patch-apply ARGS="--src /path/to/chromium/src --reset"
```

The apply command runs `series_patches/` first, then `chromium_patches/`, matching
the BrowserOS build order. It refuses to mutate a dirty Chromium checkout unless
`--allow-dirty` is passed explicitly.

## Near-Term Milestones

1. Keep the BrowserOS patch/build baseline applyable from this repository.
2. Migrate or replace the imported Wuu integration from the BrowserOS agent
   baseline.
3. Keep the browser default tab rendering the full Wuu workbench with real native
   runtime capabilities.
4. Keep OpenClaw/Hermes/Lima VM startup disabled on the default Wuu Browser path.
5. Keep one-command development launch, runtime/CDP verification, and the first
   Browser Bridge tab create/activate/navigate/back/forward/reload/close/
   screenshot/snapshot/DOM/evaluate/console/network/click/type/scroll path
   green.
6. Finish Wuu-owned update publishing, signed/notarized macOS release
   packaging, and Windows installer outputs.
