# BrowserOS Source Baseline

Wuu Browser reuses BrowserOS as the browser-layer reference implementation.

The current imported browser build and patch assets come from the local
BrowserOS reference checkout:

```text
repository: https://github.com/browseros-ai/BrowserOS
local path: ~/wuu-browseros
commit: 2eab1001353bdbf4660cd788c08022c813611758
branch: dev
chromium base: 4d3225104176d
chromium version: 146.0.7680.31
```

Imported browser-layer assets intentionally include BrowserOS product code,
build scripts, Chromium patches, replacement files, resources, and patch
tooling. They do not include a full Chromium source checkout, local build
outputs, virtual environments, logs, or downloaded runtime caches.

Imported agent/server assets intentionally include the BrowserOS extension app,
server source, shared packages, development scripts, and the Wuu workbench
bridge code proven in the local BrowserOS reference checkout. They do not
include `node_modules`, extension/browser build outputs, local environment
files, or eval benchmark data.

When refreshing BrowserOS assets, run:

```bash
make browser-import-browseros ARGS="--dry-run"
make browser-import-browseros
make browser-import-agent ARGS="--dry-run"
make browser-import-agent
```

After import, inspect the diff before committing. Do not blindly preserve
BrowserOS defaults that conflict with Wuu Browser's product model, especially
default VM startup and first-run onboarding.

## License and release boundary

The Wuu root project remains MIT unless a file or package declares a different
license.

The `browser/` tree is different. It includes BrowserOS-derived browser-layer
and agent/server assets. Those imported BrowserOS assets are AGPL-3.0-or-later,
as recorded by `browser/agent/package.json` and the imported source headers.

Treat Wuu Browser release artifacts as BrowserOS-derived browser releases when
they include code from this tree. Do not describe those artifacts as MIT-only
releases just because the Wuu root project is MIT.

Before a Wuu Browser release:

- Preserve imported SPDX headers and license files.
- Keep this BrowserOS baseline section up to date.
- Include the corresponding source for BrowserOS-derived code in the release
  process.
- Keep Wuu Desktop release notes separate from Wuu Browser license obligations.
- Review newly imported BrowserOS code before exposing it as Wuu product
  behavior.
