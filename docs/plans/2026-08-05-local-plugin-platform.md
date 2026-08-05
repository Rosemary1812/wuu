# Wuu Local Plugin Platform

## Goal

Make plugins a practical product surface:

- A developer can create, build, validate, and test a plugin without changing Wuu.
- A user can install a downloaded plugin from a local folder or archive, inspect it, enable it, configure it, and remove it.
- One plugin package can contribute declarative assets and executable behavior to both the Go core and Electron desktop.
- Plugins can customize themes, settings, selected UI surfaces, commands, tools, prompts, and typed Agent lifecycle seams.

The first complete version is local-first. It does not require a marketplace, publisher signing, remote update service, dependency resolver, or a general-purpose security sandbox.

## Product Contract

### Trust Model

Wuu supports two package modes:

1. **Declarative packages** contain data that Wuu reads: Skills, prompt commands, MCP declarations, themes, and settings schemas.
2. **Code packages** additionally contain a core runtime or desktop client bundle. Enabling one is equivalent to enabling locally installed application code. Wuu shows that boundary plainly and continues to use exact package fingerprints for approval and change detection.

Permission names describe the surfaces a code package requests. The first version does not claim that arbitrary native code is an operating-system sandbox.

### Package Layout

```text
example-plugin/
├── plugin.json
├── dist/
│   ├── runtime
│   └── desktop.js
├── skills/
├── prompts/
└── assets/
```

`plugin.json` remains compatible with current Skills, MCP, hooks, commands, and runtime declarations and adds these stable fields:

```json
{
  "schemaVersion": 1,
  "id": "example-plugin",
  "name": "Example Plugin",
  "version": "1.0.0",
  "runtime": {
    "protocol": "wuu-plugin-v1",
    "command": "./dist/runtime"
  },
  "desktop": {
    "entry": "./dist/desktop.js"
  },
  "contributes": {
    "commands": [],
    "themes": [],
    "settings": {}
  }
}
```

Rules:

- Runtime and desktop artifacts are self-contained. The first version does not resolve dependencies between installed plugins.
- Every regular file in the installed package stays inside the package root and participates in the package fingerprint. Changing a transitive runtime script, desktop chunk, stylesheet, or asset invalidates the prior approval.
- `schemaVersion: 1` and `wuu-plugin-v1` are exact compatibility anchors. Unsupported values fail visibly.
- Existing `platforms` and `minimumWuuVersion` declarations are enforced before approval or activation. Runtime commands must be executable on the current host; a TypeScript runtime may require a declared external Node command, while distributed plugins should package a platform executable when they need a zero-dependency end-user install.
- User plugins install under `$WUU_HOME/plugins/<id>`; project plugins may continue to live under `<workspace>/.wuu/plugins/<id>`.
- A project package identity includes the current Workspace ID. Approval, disabled state, settings, and runtime instances never transfer to another workspace merely because its plugin ID and files match.

### Local Installation

The core owns installation so every shell gets the same behavior.

- Install from a directory by validating and atomically copying it into the user plugin directory.
- Install from `.zip` by validating entries, rejecting traversal and links, extracting into a staging directory, then atomically publishing the package.
- Replace an existing package only after the new generation validates.
- Installation and update perform static validation only; new code never executes before approval. An update remains pending while the active approved generation keeps running. Approval activates the new generation atomically; rejection or activation failure preserves the prior generation.
- Uninstall removes the installed user package and stops its active surfaces.
- Refresh rediscovers project packages and externally changed user packages.

Desktop exposes Install, Update from local file/folder, Enable, Disable, and Uninstall. CLI exposes equivalent commands for automation and development.

## Core Plugin Host

### Lifecycle

Each active plugin owns a generation with a disposable registration scope. A generation tracks:

- runtime process;
- tools, prompt sections, commands, hooks, and other registrations;
- status and last error;
- workspace and thread instances;
- cleanup functions.

Activation builds a replacement generation before swapping it into use. Deactivation disposes registrations and processes in reverse order. A failed core runtime is isolated from unrelated plugin chains and is restartable from the catalog.

### Runtime API

Extend the existing JSON-lines protocol rather than loading third-party Go code into the core process.

The host offers versioned methods for:

- initialization and declared capabilities;
- tool definition and tool execution;
- prompt-section contribution;
- direct command execution;
- Agent, turn, step, request, message, and tool lifecycle interception;
- workspace/thread context;
- namespaced settings reads and change notifications;
- structured activity and diagnostics;
- graceful shutdown.

Initial Agent seams:

- `session.start`, `session.stop`;
- `chat.message`, `chat.request`, `chat.request_error`;
- `turn.start`, `turn.end`, `turn.stopping`;
- `step.start`, `step.end`;
- `tool.definition`, `tool.execute.before`, `tool.execute.after`;
- `shell.env`.

Tool registration is complete only when definition, execution, cancellation, result normalization, and activity metadata use the same registration.

Seam semantics are part of the protocol contract:

- lifecycle boundary events are observational and cannot rewrite state;
- message, request, tool-definition, tool-before, tool-after, and shell-environment seams are ordered transforms;
- `tool.execute.before` may continue, reject, or request host approval;
- transforms run in deterministic plugin-ID order unless an explicit numeric priority is declared;
- every call receives cancellation and a host deadline;
- timeout, protocol failure, or invalid output fails only that plugin call by default and records diagnostics; a seam may explicitly declare fail-closed behavior when bypassing it would violate its purpose;
- one plugin instance processes calls serially unless it declares concurrent handling during initialization.

### Core Registries

The Go host provides reversible registries instead of adding more package-specific branches to `runtime.NewSession`:

- tools;
- prompt sections;
- commands;
- lifecycle middleware;
- activity kinds;
- settings namespaces.

Registrations have deterministic order and explicit process, workspace, or thread scope. Built-in behavior can migrate onto the same registries incrementally; wholesale runtime replacement is not required for the first release.

## Desktop Plugin Host

### Client Bundle Loading

An enabled desktop entry is loaded as a content-addressed browser bundle through a Wuu-owned Electron protocol. The renderer does not receive Node or unrestricted Electron APIs.

The bundle registers itself with the Wuu plugin host and receives a stable SDK containing:

- React and supported hooks;
- Wuu UI primitives;
- slot registration;
- theme registration;
- settings access;
- namespaced storage;
- locale registration;
- namespaced app-server RPC;
- lifecycle disposal.

CSS and other assets are attributed to the plugin generation and removed when it unloads. Development refresh replaces one generation without restarting the whole app when possible.

Desktop code plugins are trusted same-renderer code, not isolated applications. Wuu guarantees cleanup of host-owned registrations, stores, styles, and assets; it cannot guarantee cleanup of arbitrary globals or recovery from a bundle that blocks the renderer. The shell therefore records plugin boot progress, suppresses the last crashing plugin on the next launch, offers a third-party-plugin safe mode, and uses renderer restart as the hard recovery path.

### UI Slots

The first public slots are deliberately useful and bounded:

- `sidebar.primary` and `sidebar.footer`;
- `workspace.header`;
- `conversation.header`;
- `conversation.message.before` and `conversation.message.after`;
- `conversation.composer.toolbar`;
- `conversation.empty`;
- `turn.activity`;
- `details.panel`;
- `settings.section`;
- `catalog.section`.

Slots define root/workspace/thread scope, single/list/keyed behavior, ordering, public props, and an error boundary. Registrations disappear automatically when their plugin generation unloads.

### Themes

A declarative theme provides:

- stable id and localized name;
- light or dark base scheme;
- semantic CSS token overrides;
- optional syntax-highlighting tokens.

Theme registration, preview, selection, persistence, fallback, and unload are host-owned. Arbitrary global CSS is reserved for trusted desktop code packages; ordinary themes use tokens.

### Settings

The Go core owns plugin settings schema validation, user/workspace layering, persistence, and change notifications so `wuu exec`, Desktop, and future shells observe the same values. Desktop renders the setting controls and may host an optional custom section.

Each plugin may own one namespaced settings schema with:

- JSON-compatible defaults and validation;
- user and workspace values;
- live or restart-required fields;
- generated settings controls;
- an optional custom `settings.section` component;
- change notifications to core and desktop entries.

Plugin settings are stored separately from Wuu's built-in settings and survive disable/enable. Uninstall asks whether to retain or remove them.

## Developer Experience

Ship a small TypeScript SDK and a generator:

```text
wuu plugin create
wuu plugin validate <path>
wuu plugin pack <path>
wuu plugin install <path-or-zip>
wuu plugin inspect <id-or-path>
wuu plugin update <id> <path-or-zip>
wuu plugin enable <id>
wuu plugin disable <id>
wuu plugin refresh
wuu plugin restart <id>
wuu plugin settings get|set <id> [key] [value]
wuu plugin remove <id>
wuu plugin list
```

The generated plugin includes:

- manifest;
- optional runtime and desktop entries;
- shared types;
- build scripts;
- one Tool, one UI slot, one setting, and one theme example;
- unit tests;
- a local development command that rebuilds and refreshes Wuu.

## End-to-End Acceptance Plugin

The platform is complete only when a standalone example plugin, without modifying Wuu source, can:

1. install from both a folder and a `.zip`;
2. appear in the plugin catalog with version, contributions, and runtime status;
3. be enabled, disabled, refreshed, and uninstalled;
4. register a theme and persist the user's selection;
   - verify its semantic tokens change rendered Desktop surfaces;
   - verify preview cancellation restores the prior theme;
   - verify disable or uninstall falls back when the selected theme disappears;
5. register a validated setting visible in Settings and consume its value in both desktop and runtime code;
6. add a sidebar or composer UI contribution;
7. register a slash command;
8. register a model-visible executable Tool and return a structured result;
9. observe at least one Agent lifecycle seam;
10. survive app restart with its install, approval, enable state, and settings intact;
11. fail activation without preventing Wuu or unrelated plugins from starting;
12. unload all UI and core contributions after disable or uninstall.
13. invalidate approval when any transitive package file changes;
14. update without executing pending code, keep the old generation active until approval, and preserve it after rejection or failed activation;
15. keep approvals, settings, and active instances separate when two workspaces contain the same project plugin ID;
16. pass contract tests for every public Agent seam and UI slot, not only the surfaces exercised by the example plugin.

## Delivery Phases

### Phase 1 — Contract and package lifecycle

- Stabilize the manifest additions.
- Implement whole-package fingerprints, workspace-bound project identity, folder/zip installation, uninstall, pending updates, atomic replacement, and package diagnostics.
- Expose management through app-server, CLI, and Desktop.

### Phase 2 — Core runtime SDK

- Add disposable registries and generation lifecycle.
- Complete Tool registration/execution and typed lifecycle seams.
- Add settings delivery and diagnostics to the runtime protocol.

### Phase 3 — Desktop host

- Add the client module loader and public SDK.
- Add slots, themes, plugin settings, storage, locale, and lifecycle handling.

### Phase 4 — Developer kit

- Add generator, validator, packer, example plugin, and author documentation.

### Phase 5 — Product verification

- Run core, app-server, CLI, Desktop, production-build, and end-to-end plugin tests.
- Verify the real local install and restart path.
- Perform an independent architecture and product-path review before declaring the goal complete.

## Explicit Non-Goals

- Centralized marketplace or search service.
- Publisher accounts or remote publishing.
- Automatic background updates.
- General dependency resolution between plugins.
- Code signing or notarization of community plugins.
- A claim that arbitrary native plugin code is sandboxed.
- Rewriting every built-in Wuu feature as a plugin before the public extension path ships.
