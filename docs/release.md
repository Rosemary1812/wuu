# Release

Tagged releases are published by `.github/workflows/release.yml`.

## Trigger

1. Update `VERSION`, `desktop/package.json`, and `npm/package.json` to the
   release version.
2. Commit the version change.
3. Create and push a matching tag, for example `v0.1.0`.

The workflow refuses to release if the tag commit is not on `main`, or if the
tag, root `VERSION`, desktop package, and npm package versions do not match.

Before tagging, `make check-go test-go` must pass. The release workflow reruns
those gates against the tagged commit before it builds any CLI archives.
Release tooling consumes committed module manifests and does not update
`go.mod` or `go.sum`.

## GitHub Secrets

The current release workflow publishes Go CLI archives and the macOS Electron
desktop preview package to the same GitHub Release. It requires:

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

The CLI job verifies that the tag commit belongs to `main`, checks and tests the
Go core, builds macOS and Linux archives with GoReleaser, verifies their
checksums, and runs the Linux amd64 binary. The macOS desktop job builds and
verifies the unsigned arm64 desktop preview app. The GitHub Release is created
only after both jobs succeed.

The workflow verifies that the packaged core version is clean and that the DMG
and ZIP are structurally valid.

The final GitHub Release contains:

- `wuu_<version>_<os>_<arch>.tar.gz` for macOS and Linux on amd64 and arm64
- `checksums.txt`
- `wuu-<version>-mac-arm64.dmg`
- `wuu-<version>-mac-arm64.zip`
- matching `.blockmap` files
