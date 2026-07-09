# Release

Tagged releases are published by `.github/workflows/release.yml`.

## Trigger

1. Update `VERSION` and `desktop/package.json` to the release version.
2. Commit the version change.
3. Create and push a matching tag, for example `v0.1.0`.

The workflow refuses to release if the tag, root `VERSION`, and desktop package
version do not match.

## GitHub Secrets

The CLI release still uses GoReleaser and requires:

- `GITHUB_TOKEN` (provided by GitHub Actions)
- `HOMEBREW_TAP_TOKEN`

The desktop macOS release requires code-signing secrets:

- `MAC_CSC_LINK`: base64-encoded Developer ID Application `.p12`
- `MAC_CSC_KEY_PASSWORD`: password for the `.p12`

It also requires one notarization credential set.

Preferred App Store Connect API key secrets:

- `APPLE_API_KEY`
- `APPLE_API_KEY_ID`
- `APPLE_API_ISSUER`

Fallback Apple ID secrets:

- `APPLE_ID`
- `APPLE_APP_SPECIFIC_PASSWORD`
- `APPLE_TEAM_ID`

## Output

The macOS desktop job builds and verifies the signed/notarized arm64 desktop
app before the CLI release job runs. If the desktop job fails, the GitHub
Release is not published.

The final release contains:

- GoReleaser CLI archives and checksums
- `wuu-<version>-mac-arm64.dmg`
- `wuu-<version>-mac-arm64.zip`
- matching `.blockmap` files
