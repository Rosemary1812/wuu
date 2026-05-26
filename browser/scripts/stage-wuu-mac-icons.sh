#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: browser/scripts/stage-wuu-mac-icons.sh [--sign] [--dry-run] APP

Stage repository-owned Wuu macOS icon resources into a local app bundle.
USAGE
}

sign_app=false
dry_run=false
app_path=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --sign)
      sign_app=true
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
      if [[ -n "${app_path}" ]]; then
        echo "Unexpected argument: $1" >&2
        usage >&2
        exit 2
      fi
      app_path="$1"
      shift
      ;;
  esac
done

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"
icon_source="${repo_root}/browser/resources/icons/mac/app.icns"
assets_source="${repo_root}/browser/resources/icons/mac/Assets.car"
product_logo_source="${repo_root}/browser/resources/icons/product_logo_32.png"

if [[ -z "${app_path}" ]]; then
  echo "APP is required." >&2
  usage >&2
  exit 2
fi

if [[ ! -d "${app_path}" ]]; then
  echo "App bundle not found: ${app_path}" >&2
  exit 1
fi

if [[ ! -f "${icon_source}" || ! -f "${assets_source}" || ! -f "${product_logo_source}" ]]; then
  echo "Wuu macOS icon resources are missing under browser/resources/icons/mac." >&2
  exit 1
fi

copy_if_changed() {
  local source="$1"
  local target="$2"

  if [[ -f "${target}" ]] && cmp -s "${source}" "${target}"; then
    return 1
  fi

  if [[ "${dry_run}" == "true" ]]; then
    echo "Would stage $(basename "${source}") -> ${target}"
    return 0
  fi

  cp "${source}" "${target}"
  return 0
}

changed=0

main_resources="${app_path}/Contents/Resources"
if [[ -d "${main_resources}" ]]; then
  if copy_if_changed "${icon_source}" "${main_resources}/app.icns"; then
    changed=1
  fi
  if copy_if_changed "${assets_source}" "${main_resources}/Assets.car"; then
    changed=1
  fi
fi

while IFS= read -r -d '' resources_dir; do
  if [[ "${resources_dir}" == "${main_resources}" ]]; then
    continue
  fi

  if [[ -f "${resources_dir}/app.icns" ]]; then
    if copy_if_changed "${icon_source}" "${resources_dir}/app.icns"; then
      changed=1
    fi
  fi

  if [[ -f "${resources_dir}/Assets.car" ]]; then
    if copy_if_changed "${assets_source}" "${resources_dir}/Assets.car"; then
      changed=1
    fi
  fi
done < <(find "${app_path}/Contents" -path '*/Contents/Resources' -type d -print0)

while IFS= read -r -d '' product_logo_target; do
  if copy_if_changed "${product_logo_source}" "${product_logo_target}"; then
    changed=1
  fi
done < <(find "${app_path}/Contents" -path '*/Resources/product_logo_32.png' -type f -print0)

if [[ "${changed}" == "1" ]]; then
  echo "Staged Wuu macOS icon resources: ${app_path}"
elif [[ "${dry_run}" == "true" ]]; then
  echo "Wuu macOS icon resources already match: ${app_path}"
fi

if [[ "${sign_app}" == "true" && "${dry_run}" != "true" ]]; then
  if ! command -v codesign >/dev/null 2>&1; then
    echo "codesign is required to sign the staged app." >&2
    exit 2
  fi

  nested_bundles=()
  while IFS= read -r -d '' nested_bundle; do
    nested_bundles+=("${nested_bundle}")
  done < <(find "${app_path}/Contents" \( -name '*.app' -o -name '*.framework' -o -name '*.xpc' \) -type d -print0)

  for ((i = ${#nested_bundles[@]} - 1; i >= 0; i--)); do
    codesign --force --deep --sign - "${nested_bundles[i]}" >/dev/null
  done

  codesign --force --sign - "${app_path}" >/dev/null
fi
