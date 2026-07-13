# Open-Source Trust and Consistency Implementation Plan

> **Execution note:** Use `executing-plans` to implement this plan task-by-task, or another equivalent execution workflow supported by the current agent runtime.

**Goal:** Make Wuu's documented install, local development, CI, security, and release paths agree with the behavior contributors and users actually receive.

**Architecture:** Keep `VERSION` as the release version source checked against package manifests, publish GoReleaser CLI archives and the macOS desktop preview into one tagged GitHub Release, and expose repository-wide checks through the root Makefile. Document the agent-specific trust boundary from the implemented core/shell split, while keeping platform-specific native validation on macOS.

**Tech Stack:** Go, GoReleaser, GNU Make, GitHub Actions, Node.js 22/npm, TypeScript/Vitest, Swift Package Manager, Markdown.

---

### Task 1: Restore a truthful release and install path

**Files:**
- Modify: `.github/workflows/release.yml`
- Modify: `.goreleaser.yaml`
- Modify: `install.sh`
- Modify: `npm/package.json`
- Modify: `docs/release.md`
- Modify: `README.md`
- Modify: `README_zh.md`

**Steps:**
1. Add a CLI release job that checks tag/package versions, runs GoReleaser without publishing, verifies every archive and checksum, and uploads the artifacts.
2. Make the final release job depend on both CLI and desktop jobs and attach both artifact sets with accurate release notes.
3. Disable Homebrew publishing until its token and release contract are deliberately supported; keep archive names aligned with both installers.
4. Synchronize the npm package version and replace the stale `wuu run` instruction with `wuu exec`.
5. Update release and install documentation to describe the verified artifacts.
6. Run workflow syntax/static checks and a GoReleaser snapshot; commit the release/install change.

### Task 2: Unify local repository checks and CI

**Files:**
- Modify: `Makefile`
- Create: `.node-version`
- Modify: `desktop/package.json`
- Modify: `clients/core/package.json`
- Modify: `clients/mobile/package.json`
- Modify: `packages/protocol/package.json`
- Create: `packages/protocol/tsconfig.json`
- Modify: `.github/workflows/ci.yml`

**Steps:**
1. Add `setup`, `dev`, `check`, `test`, `build`, `ci`, and `release-check` root targets with component-specific helper targets.
2. Pin Node 22 for contributors and declare compatible Node engines in every npm manifest.
3. Give the protocol package an explicit typecheck command and TypeScript configuration.
4. Split CI into clear Go, desktop, clients, and macOS native jobs which invoke the same Make targets used locally.
5. Run each Linux/macOS-compatible target locally and commit the development/CI change.

### Task 3: Publish the agent trust model and human contribution rules

**Files:**
- Modify: `SECURITY.md`
- Create: `docs/security-model.md`
- Create: `docs/development.md`
- Modify: `CONTRIBUTING.md`
- Delete: `.github/CODEOWNERS`

**Steps:**
1. Document file, command, network, environment, model-provider, project-instruction, MCP/hook/skill, app-server, remote-control, credential, and log trust boundaries from current behavior.
2. Keep vulnerability reporting concise in `SECURITY.md` and link to the detailed threat model.
3. Move stable human-facing setup, platform, component, validation, and AI-assisted contribution guidance out of `AGENTS.md` into development/contribution docs.
4. Remove the invalid placeholder CODEOWNERS file.
5. Check all local links and documented commands, then commit the governance/documentation change.

### Task 4: Preserve user-owned experiment claims and design drafts

**Files:**
- Verify: `README.md`
- Verify: `README_zh.md`
- Preserve: `assets/mascot/`
- Preserve: `landing/assets/mascot/`

**Steps:**
1. Record the user's confirmation that the exact cost comparison comes from a real internal experiment.
2. Keep the comparison in README and the landing page; recommend a future evidence page without blocking the current claim.
3. Keep all concept drafts because the product team may use them later.
4. Verify the cleanup commit was fully reverted and all 75 assets remain tracked.

### Task 5: Final release-readiness verification

**Files:**
- Verify all files changed above.

**Steps:**
1. Run `make ci` and `make release-check` from the repository root.
2. Confirm `git diff --check`, versions, Markdown links, workflow YAML, and release archive names.
3. Review the final diff for silent fallbacks, forced tests, unrelated changes, and conflicts with the user's pre-existing edits.
