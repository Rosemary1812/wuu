#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: browser/scripts/package-dev-macos.sh [--source-app APP] [--output-app APP] [--dmg] [--dry-run]

Stage a local Wuu Browser Dev macOS app from an existing Chromium build.

This is a development packaging path. It copies a built BrowserOS/Chromium dev
app into browser/out, applies Wuu Browser visible bundle metadata, ad-hoc signs
the staged app for local launches, and can optionally create a local DMG.
Chromium component-build dylibs from the source build root are copied into the
staged app bundle so it can launch outside the original build directory. This
does not replace the final signed release pipeline.

Environment overrides:
  WUU_CHROMIUM_SRC        Chromium source checkout.
  WUU_BROWSER_SOURCE_APP  Built BrowserOS/Wuu Browser source .app.
  WUU_BROWSER_DEV_APP     Output Wuu Browser Dev .app path.
USAGE
}

dry_run=false
create_dmg=false
source_app="${WUU_BROWSER_SOURCE_APP:-}"
output_app="${WUU_BROWSER_DEV_APP:-}"
output_dmg=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --source-app)
      source_app="${2:-}"
      if [[ -z "${source_app}" ]]; then
        echo "--source-app requires an app path" >&2
        exit 2
      fi
      shift 2
      ;;
    --output-app)
      output_app="${2:-}"
      if [[ -z "${output_app}" ]]; then
        echo "--output-app requires an app path" >&2
        exit 2
      fi
      shift 2
      ;;
    --dmg)
      create_dmg=true
      shift
      ;;
    --output-dmg)
      output_dmg="${2:-}"
      if [[ -z "${output_dmg}" ]]; then
        echo "--output-dmg requires a dmg path" >&2
        exit 2
      fi
      create_dmg=true
      shift 2
      ;;
    --dry-run)
      dry_run=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"
chromium_src="${WUU_CHROMIUM_SRC:-${repo_root}/.worktrees/chromium/src}"

if [[ ! -d "${chromium_src}" && -d "${HOME}/browseros-chromium/src" ]]; then
  chromium_src="${HOME}/browseros-chromium/src"
fi

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "package-dev-macos currently supports macOS only." >&2
  exit 2
fi

if [[ -z "${source_app}" ]]; then
  for candidate in \
    "${chromium_src}/out/Default_arm64/BrowserOS Dev.app" \
    "${chromium_src}/out/Default/BrowserOS Dev.app" \
    "${chromium_src}/out/Default_x64/BrowserOS Dev.app" \
    "${chromium_src}/out/Default_arm64/Wuu Browser Dev.app" \
    "${chromium_src}/out/Default/Wuu Browser Dev.app" \
    "${chromium_src}/out/Default_x64/Wuu Browser Dev.app"; do
    if [[ -d "${candidate}" ]]; then
      source_app="${candidate}"
      break
    fi
  done
fi

if [[ -z "${output_app}" ]]; then
  output_app="${repo_root}/browser/out/Wuu Browser Dev.app"
fi

if [[ -z "${output_dmg}" ]]; then
  output_dmg="${repo_root}/browser/out/Wuu Browser Dev.dmg"
fi

if [[ -z "${source_app}" || ! -d "${source_app}" ]]; then
  echo "Source browser app not found." >&2
  echo "Set WUU_BROWSER_SOURCE_APP or build BrowserOS Dev under the Chromium checkout." >&2
  exit 1
fi

source_plist="${source_app}/Contents/Info.plist"
if [[ ! -f "${source_plist}" ]]; then
  echo "Source app Info.plist not found: ${source_plist}" >&2
  exit 1
fi

if [[ "${source_app}" == "${output_app}" ]]; then
  echo "Output app must differ from source app." >&2
  exit 2
fi

echo "Wuu Browser Dev macOS package"
echo "  source app: ${source_app}"
echo "  output app: ${output_app}"
if [[ "${create_dmg}" == "true" ]]; then
  echo "  output dmg: ${output_dmg}"
fi

if [[ "${dry_run}" == "true" ]]; then
  exit 0
fi

if ! command -v ditto >/dev/null 2>&1; then
  echo "ditto is required to stage the macOS app." >&2
  exit 2
fi

if ! command -v plutil >/dev/null 2>&1; then
  echo "plutil is required to update the macOS app metadata." >&2
  exit 2
fi

if ! command -v codesign >/dev/null 2>&1; then
  echo "codesign is required to ad-hoc sign the staged macOS app." >&2
  exit 2
fi

rm -rf "${output_app}"
mkdir -p "$(dirname "${output_app}")"
ditto "${source_app}" "${output_app}"

source_build_dir="$(dirname "${source_app}")"
frameworks_dir="${output_app}/Contents/Frameworks"
companion_dylibs=0
while IFS= read -r -d '' companion; do
  ditto "${companion}" "${frameworks_dir}/$(basename "${companion}")"
  companion_dylibs=$((companion_dylibs + 1))
done < <(find "${source_build_dir}" -maxdepth 1 -type f -name '*.dylib' -print0)

repacked_resources_dir="${source_build_dir}/gen/repack"
resource_packs=0
if [[ -d "${repacked_resources_dir}" ]]; then
  while IFS= read -r -d '' resource_pack; do
    pack_name="$(basename "${resource_pack}")"
    while IFS= read -r -d '' staged_pack; do
      ditto "${resource_pack}" "${staged_pack}"
      resource_packs=$((resource_packs + 1))
    done < <(find "${output_app}/Contents/Frameworks" -type f -name "${pack_name}" -print0)
  done < <(find "${repacked_resources_dir}" -maxdepth 1 -type f -name '*.pak' -print0)
fi

plist="${output_app}/Contents/Info.plist"
plutil -replace CFBundleDisplayName -string "Wuu Browser Dev" "${plist}"
plutil -replace CFBundleName -string "Wuu Browser Dev" "${plist}"
plutil -replace CFBundleIdentifier -string "com.wuu.browser.dev" "${plist}"
plutil -replace CrProductDirName -string "Wuu Browser" "${plist}"

plutil -lint "${plist}" >/dev/null

bash "${repo_root}/browser/scripts/stage-wuu-mac-icons.sh" "${output_app}"

echo "Ad-hoc signing Wuu Browser Dev app for local launch..."
codesign --force --deep --sign - "${output_app}" >/dev/null
codesign --verify --deep --strict --verbose=2 "${output_app}" >/dev/null

echo "Staged Wuu Browser Dev app: ${output_app}"
echo "  display name: $(plutil -extract CFBundleDisplayName raw -o - "${plist}")"
echo "  bundle id:    $(plutil -extract CFBundleIdentifier raw -o - "${plist}")"
echo "  executable:   $(plutil -extract CFBundleExecutable raw -o - "${plist}")"
echo "  dylibs:       ${companion_dylibs} component build libraries bundled"
echo "  resource paks:${resource_packs} Chromium resource packs refreshed"

if [[ "${create_dmg}" == "true" ]]; then
  if ! command -v hdiutil >/dev/null 2>&1; then
    echo "hdiutil is required to create a DMG." >&2
    exit 2
  fi

  rm -f "${output_dmg}"
  hdiutil create \
    -volname "Wuu Browser Dev" \
    -srcfolder "${output_app}" \
    -ov \
    -format UDZO \
    "${output_dmg}" >/dev/null
  echo "Created Wuu Browser Dev DMG: ${output_dmg}"
fi
