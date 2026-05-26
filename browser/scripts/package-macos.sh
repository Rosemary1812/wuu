#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: browser/scripts/package-macos.sh [--source-app APP] [--output-app APP] [--dmg] [--output-dmg DMG] [--sign-identity ID] [--allow-dev-source] [--update-enabled] [--dry-run]

Stage a product-named Wuu Browser macOS app from an existing Chromium build.

This packages Wuu Browser.app with the production bundle identity. By default it
uses ad-hoc signing so local installs and smoke tests work without Apple
credentials. It does not claim the app is update-ready unless --update-enabled
is passed; that mode fails fast while the browser, extension, or server update
feeds still point at BrowserOS infrastructure.

Environment overrides:
  WUU_CHROMIUM_SRC              Chromium source checkout.
  WUU_BROWSER_SOURCE_APP        Built BrowserOS/Wuu Browser source .app.
  WUU_BROWSER_APP               Output Wuu Browser .app path.
  WUU_BROWSER_DMG               Output Wuu Browser .dmg path.
  WUU_BROWSER_CODESIGN_IDENTITY Developer ID Application identity.
USAGE
}

dry_run=false
create_dmg=false
allow_dev_source=false
update_enabled=false
source_app="${WUU_BROWSER_SOURCE_APP:-}"
output_app="${WUU_BROWSER_APP:-}"
output_dmg="${WUU_BROWSER_DMG:-}"
sign_identity="${WUU_BROWSER_CODESIGN_IDENTITY:--}"

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
    --sign-identity)
      sign_identity="${2:-}"
      if [[ -z "${sign_identity}" ]]; then
        echo "--sign-identity requires a codesign identity" >&2
        exit 2
      fi
      shift 2
      ;;
    --allow-dev-source)
      allow_dev_source=true
      shift
      ;;
    --update-enabled)
      update_enabled=true
      shift
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
  echo "package-macos currently supports macOS only." >&2
  exit 2
fi

if [[ -z "${source_app}" ]]; then
  for candidate in \
    "${chromium_src}/out/Release_arm64/Wuu Browser.app" \
    "${chromium_src}/out/Release/Wuu Browser.app" \
    "${chromium_src}/out/Release_x64/Wuu Browser.app" \
    "${chromium_src}/out/Default_arm64/Wuu Browser.app" \
    "${chromium_src}/out/Default/Wuu Browser.app" \
    "${chromium_src}/out/Default_x64/Wuu Browser.app" \
    "${chromium_src}/out/Release_arm64/BrowserOS.app" \
    "${chromium_src}/out/Release/BrowserOS.app" \
    "${chromium_src}/out/Release_x64/BrowserOS.app" \
    "${chromium_src}/out/Default_arm64/BrowserOS.app" \
    "${chromium_src}/out/Default/BrowserOS.app" \
    "${chromium_src}/out/Default_x64/BrowserOS.app" \
    "${repo_root}/browser/out/Wuu Browser Dev.app" \
    "${chromium_src}/out/Default_arm64/Wuu Browser Dev.app" \
    "${chromium_src}/out/Default/Wuu Browser Dev.app" \
    "${chromium_src}/out/Default_x64/Wuu Browser Dev.app" \
    "${chromium_src}/out/Default_arm64/BrowserOS Dev.app" \
    "${chromium_src}/out/Default/BrowserOS Dev.app" \
    "${chromium_src}/out/Default_x64/BrowserOS Dev.app"; do
    if [[ -d "${candidate}" ]]; then
      source_app="${candidate}"
      break
    fi
  done
fi

if [[ -z "${output_app}" ]]; then
  output_app="${repo_root}/browser/out/Wuu Browser.app"
fi

if [[ -z "${output_dmg}" ]]; then
  output_dmg="${repo_root}/browser/out/Wuu Browser.dmg"
fi

if [[ -z "${source_app}" || ! -d "${source_app}" ]]; then
  echo "Source browser app not found." >&2
  echo "Set WUU_BROWSER_SOURCE_APP or build Wuu Browser under the Chromium checkout." >&2
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

source_display_name="$(plutil -extract CFBundleDisplayName raw -o - "${source_plist}" 2>/dev/null || true)"
source_bundle_name="$(plutil -extract CFBundleName raw -o - "${source_plist}" 2>/dev/null || true)"
source_bundle_id="$(plutil -extract CFBundleIdentifier raw -o - "${source_plist}" 2>/dev/null || true)"
source_executable="$(plutil -extract CFBundleExecutable raw -o - "${source_plist}" 2>/dev/null || true)"

source_is_dev=false
if [[ "${source_app}" == *" Dev.app" ]] || \
  [[ "${source_display_name}" == *" Dev" ]] || \
  [[ "${source_bundle_name}" == *" Dev" ]] || \
  [[ "${source_bundle_id}" == *.dev ]] || \
  [[ "${source_executable}" == *" Dev"* ]]; then
  source_is_dev=true
fi

if [[ "${source_is_dev}" == "true" && "${allow_dev_source}" != "true" ]]; then
  echo "Refusing to create a production-named app from a dev source app." >&2
  echo "Build a release Chromium app, or pass --allow-dev-source for a local product-name preview." >&2
  exit 2
fi

assert_update_ready() {
  local update_files=(
    "${repo_root}/browser/chromium_patches/chrome/browser/mac/sparkle_glue.mm"
    "${repo_root}/browser/chromium_patches/chrome/browser/browseros/server/browseros_server_constants.h"
    "${repo_root}/browser/agent/apps/agent/wxt.config.ts"
    "${repo_root}/browser/build/modules/ota/common.py"
    "${repo_root}/browser/build/config/appcast/appcast-server.xml"
    "${repo_root}/browser/build/config/appcast/appcast-server.alpha.xml"
  )

  local found_browseros_feed=false
  for file in "${update_files[@]}"; do
    if [[ -f "${file}" ]] && grep -Fq "cdn.browseros.com" "${file}"; then
      echo "Update-ready packaging blocked: ${file} still references cdn.browseros.com" >&2
      found_browseros_feed=true
    fi
  done

  if [[ "${found_browseros_feed}" == "true" ]]; then
    echo "Move browser, extension, and server update feeds to Wuu-owned endpoints before using --update-enabled." >&2
    exit 1
  fi

  if [[ "${sign_identity}" == "-" ]]; then
    echo "Update-ready packaging requires WUU_BROWSER_CODESIGN_IDENTITY or --sign-identity." >&2
    exit 1
  fi

  if [[ -z "${SPARKLE_PRIVATE_KEY:-}" ]]; then
    echo "Update-ready packaging requires SPARKLE_PRIVATE_KEY for Sparkle release artifacts." >&2
    exit 1
  fi
}

codesign_checked() {
  local label="$1"
  shift

  local output
  if output="$("$@" 2>&1)"; then
    return
  fi

  echo "${label} failed:" >&2
  echo "${output}" >&2
  exit 1
}

if [[ "${update_enabled}" == "true" ]]; then
  assert_update_ready
fi

echo "Wuu Browser macOS product package"
echo "  source app:     ${source_app}"
echo "  source channel: $(if [[ "${source_is_dev}" == "true" ]]; then echo "dev-preview"; else echo "release"; fi)"
echo "  output app:     ${output_app}"
echo "  bundle id:      com.wuu.browser"
echo "  signing:        $(if [[ "${sign_identity}" == "-" ]]; then echo "ad-hoc"; else echo "${sign_identity}"; fi)"
echo "  update-ready:   ${update_enabled}"
if [[ "${create_dmg}" == "true" ]]; then
  echo "  output dmg:     ${output_dmg}"
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
  echo "codesign is required to sign the staged macOS app." >&2
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
plutil -replace CFBundleDisplayName -string "Wuu Browser" "${plist}"
plutil -replace CFBundleName -string "Wuu Browser" "${plist}"
plutil -replace CFBundleIdentifier -string "com.wuu.browser" "${plist}"
plutil -replace CrProductDirName -string "Wuu Browser" "${plist}"

plutil -lint "${plist}" >/dev/null

bash "${repo_root}/browser/scripts/stage-wuu-mac-icons.sh" "${output_app}"

echo "Signing Wuu Browser app..."
codesign_checked "codesign" codesign --force --deep --sign "${sign_identity}" "${output_app}"
codesign_checked "codesign verification" codesign --verify --deep --strict --verbose=2 "${output_app}"

echo "Staged Wuu Browser app: ${output_app}"
echo "  display name: $(plutil -extract CFBundleDisplayName raw -o - "${plist}")"
echo "  bundle id:    $(plutil -extract CFBundleIdentifier raw -o - "${plist}")"
echo "  executable:   $(plutil -extract CFBundleExecutable raw -o - "${plist}")"
echo "  dylibs:       ${companion_dylibs} component build libraries bundled"
echo "  resource paks:${resource_packs} Chromium resource packs refreshed"

if [[ "${source_is_dev}" == "true" ]]; then
  echo "Note: this app is production-named but was staged from a dev Chromium source app."
fi

if [[ "${update_enabled}" != "true" ]]; then
  echo "Note: update-ready checks were not enabled for this package."
fi

if [[ "${create_dmg}" == "true" ]]; then
  if ! command -v hdiutil >/dev/null 2>&1; then
    echo "hdiutil is required to create a DMG." >&2
    exit 2
  fi

  rm -f "${output_dmg}"
  hdiutil create \
    -volname "Wuu Browser" \
    -srcfolder "${output_app}" \
    -ov \
    -format UDZO \
    "${output_dmg}" >/dev/null
  echo "Created Wuu Browser DMG: ${output_dmg}"
fi
