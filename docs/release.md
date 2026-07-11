# Release

Tagged releases are published by `.github/workflows/release.yml`.

## Trigger

1. Update `VERSION` and `desktop/package.json` to the release version.
2. Commit the version change.
3. Create and push a matching tag, for example `v0.1.0`.

The workflow refuses to release if the tag, root `VERSION`, and desktop package
version do not match.

Before tagging, `go mod tidy -diff` must succeed without output. CI runs the
same read-only check; release tooling consumes committed module manifests and
does not update `go.mod` or `go.sum`.

## GitHub Secrets

The current release workflow only publishes the macOS Electron desktop preview
package to GitHub Releases. It requires:

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

## macOS Privacy Permissions

Computer Use needs both **Accessibility** and **Screen & System Audio
Recording** permission. Install the app at the stable path
`/Applications/wuu.app`, start it, invoke Computer Use once so macOS registers
the app, then enable `wuu` in both lists under System Settings > Privacy &
Security. Restart `wuu` after changing Screen Recording permission.

The current preview is unsigned because the project has no Developer ID
certificate. macOS ties privacy decisions to app identity and code-signing
requirements, so installing a different unsigned preview build can require
granting these permissions again. Do not promise permission persistence across
preview updates. A stable Developer ID signature is required before treating
that behavior as an update guarantee.

## Output

The macOS desktop job builds and verifies the unsigned arm64 desktop preview
app before publishing the GitHub Release. If the desktop job fails, the GitHub
Release is not created.

The workflow verifies that the packaged core version is clean and that the DMG
and ZIP are structurally valid.

The final GitHub Release contains:

- `wuu-<version>-mac-arm64.dmg`
- `wuu-<version>-mac-arm64.zip`
- matching `.blockmap` files
