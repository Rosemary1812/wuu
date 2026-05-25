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

Imported assets intentionally include BrowserOS product code, build scripts,
Chromium patches, replacement files, resources, and patch tooling. They do not
include a full Chromium source checkout, local build outputs, virtual
environments, logs, or downloaded runtime caches.

When refreshing BrowserOS assets, run:

```bash
make browser-import-browseros ARGS="--dry-run"
make browser-import-browseros
```

After import, inspect the diff before committing. Do not blindly preserve
BrowserOS defaults that conflict with Wuu Browser's product model, especially
default VM startup and first-run onboarding.
