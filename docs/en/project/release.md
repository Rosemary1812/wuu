# Release

Tagged releases are published by `.github/workflows/release.yml`.

## Trigger

`VERSION` is the product version source. Do not edit package versions by hand.

1. Add user-visible changes under `CHANGELOG.md`'s `[Unreleased]` section.
2. Run `make release-prepare RELEASE_VERSION=0.4.0`. The command validates the
   version, updates `VERSION`, the desktop manifest and lockfile,
   and moves the unreleased notes into a dated release section.
3. Review the diff, run `make ci release-check`, and commit the release change.
4. After the commit is on `main`, run `make tag-release` and push the annotated
   tag printed by that command.

The workflow refuses to release if the tag commit is not on `main`, if any
generated version differs from the tag, or if the matching changelog section is
missing or empty.

## Version policy

wuu uses Semantic Versioning while it is pre-1.0:

- Patch (`0.x.Y`) releases contain compatible fixes and small UI improvements.
- Minor (`0.X.0`) releases add features or change protocol, configuration,
  stored data, or user-visible behavior in a compatibility-sensitive way.
- Prereleases such as `0.4.0-rc.1` are used when packaged builds need broader
  validation before becoming the current release.

The private protocol, remote-core, and mobile packages remain at `0.0.0` until
they have an independent public release contract.

Before tagging, the Go core and desktop test suites must pass. The release
workflow reruns those gates against the tagged commit before it builds the
desktop app. Release tooling consumes committed module manifests and does not
update `go.mod` or `go.sum`.

## GitHub Secrets

The current release workflow publishes only the macOS Electron desktop preview
package. It requires:

- `GITHUB_TOKEN` (provided by GitHub Actions)

The current desktop macOS job does not require Apple signing or notarization
secrets. It builds unsigned arm64 preview artifacts because the project does
not yet have a Developer ID certificate.

The workflow sets `CSC_IDENTITY_AUTO_DISCOVERY=false` so `electron-builder`
does not try to use a runner-local signing identity.

When the project has Developer ID credentials, restore signing and
notarization gates before describing desktop assets as signed public releases.
The expected future secrets are:

- `MAC_CSC_LINK`: base64-encoded Developer ID Application `.p12`
- `MAC_CSC_KEY_PASSWORD`: password for the `.p12`

plus one notarization credential set:

- App Store Connect API key: `APPLE_API_KEY`, `APPLE_API_KEY_ID`,
  `APPLE_API_ISSUER`
- Apple ID fallback: `APPLE_ID`, `APPLE_APP_SPECIFIC_PASSWORD`,
  `APPLE_TEAM_ID`

## macOS Gatekeeper

Desktop artifacts attached to the GitHub Release are unsigned preview builds.
After downloading the DMG or ZIP and moving `wuu.app` to `/Applications`,
macOS may block the app because Apple cannot verify the developer.

For a build from a GitHub Release you trust, remove the quarantine attribute:

```bash
xattr -dr com.apple.quarantine /Applications/wuu.app
open /Applications/wuu.app
```

Do not ask users to run this for builds from untrusted sources.

## Output

The macOS desktop job verifies that the tag commit belongs to `main`, checks and
tests the Go core and desktop app, then builds and verifies the unsigned arm64
desktop preview. The workflow verifies that the packaged core version is clean
and that the DMG and ZIP are structurally valid before creating the GitHub
Release.

The app contains its required private `wuu-core` subprocess. No standalone CLI
archive is published; a separately source-installed `wuu` CLI can coexist with
the app and may have a different version.

The final GitHub Release contains only:

- `wuu-<version>-mac-arm64.dmg`
- `wuu-<version>-mac-arm64.zip`
- matching `.blockmap` files
