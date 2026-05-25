# Wuu Browser

This directory is the product entry point for Wuu Browser.

Wuu Browser is intended to become an installable Chromium-based AI browser, not
an Electron shell, a browser extension, or a developer-only assembly of local
parts. The product combines:

- Chrome-level browser experience: tabs, omnibox, navigation, extensions,
  downloads, settings, DevTools, and platform packaging.
- The full Wuu workbench as the default product tab, backed by the real Wuu
  native runtime.
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
adapt those baselines so the default tab is the Wuu workbench backed by the Wuu
native runtime.

## Local Checkouts

By default, scripts look for local source checkouts in:

```text
.worktrees/browseros
.worktrees/chromium/src
```

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

## Near-Term Milestones

1. Apply the imported BrowserOS patch/build baseline from this repository.
2. Migrate or replace the imported Wuu integration from the BrowserOS agent
   baseline.
3. Make the browser default tab render the full Wuu workbench with real native
   runtime capabilities.
4. Disable OpenClaw/Hermes/Lima VM startup on the default Wuu Browser path.
5. Add one-command development launch and screenshot/CDP verification.
6. Add packaging paths for macOS and Windows installers.
