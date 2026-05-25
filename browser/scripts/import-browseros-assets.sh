#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: browser/scripts/import-browseros-assets.sh [--dry-run]

Import BrowserOS browser-layer assets into this Wuu Browser product repository.

Environment overrides:
  WUU_BROWSEROS_REPO  BrowserOS reference checkout. Defaults to .worktrees/browseros,
                      then falls back to ~/wuu-browseros when present.

Imported from packages/browseros:
  build/
  chromium_files/
  chromium_patches/
  resources/
  series_patches/
  tools/patch/
  pyproject.toml
  pyrightconfig.json
  requirements.txt
  uv.lock

Not imported:
  full Chromium source checkouts
  virtual environments
  logs
  build outputs
  downloaded binary/runtime caches
USAGE
}

dry_run=false

while [[ $# -gt 0 ]]; do
  case "$1" in
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
browser_dir="${repo_root}/browser"

browseros_repo="${WUU_BROWSEROS_REPO:-${repo_root}/.worktrees/browseros}"
if [[ ! -d "${browseros_repo}" && -d "${HOME}/wuu-browseros" ]]; then
  browseros_repo="${HOME}/wuu-browseros"
fi

source_dir="${browseros_repo}/packages/browseros"

if [[ ! -d "${source_dir}" ]]; then
  echo "BrowserOS packages/browseros not found: ${source_dir}" >&2
  echo "Set WUU_BROWSEROS_REPO to a BrowserOS checkout." >&2
  exit 1
fi

if ! command -v rsync >/dev/null 2>&1; then
  echo "rsync is required to import BrowserOS assets" >&2
  exit 2
fi

rsync_flags=(-a --delete --omit-dir-times)
if [[ "${dry_run}" == "true" ]]; then
  rsync_flags+=(--dry-run --itemize-changes)
fi

common_excludes=(
  --exclude '__pycache__/'
  --exclude '*.pyc'
  --exclude '.DS_Store'
)

copy_dir() {
  local name="$1"
  local source_path="${source_dir}/${name}/"
  local dest_path="${browser_dir}/${name}/"

  if [[ ! -d "${source_path}" ]]; then
    echo "Skipping missing BrowserOS directory: ${name}" >&2
    return
  fi

  mkdir -p "${dest_path}"
  echo "Importing ${name}/"
  rsync "${rsync_flags[@]}" "${common_excludes[@]}" \
    --exclude 'logs/' \
    --exclude '.venv/' \
    --exclude 'chromium_src/' \
    --exclude 'chromium_src_bak/' \
    --exclude 'out/' \
    --exclude 'dist/' \
    --exclude 'node_modules/' \
    --exclude 'binaries/' \
    --exclude 'branding/' \
    "${source_path}" "${dest_path}"
}

copy_file() {
  local name="$1"
  local source_path="${source_dir}/${name}"
  local dest_path="${browser_dir}/${name}"

  if [[ ! -f "${source_path}" ]]; then
    echo "Skipping missing BrowserOS file: ${name}" >&2
    return
  fi

  echo "Importing ${name}"
  if [[ "${dry_run}" == "true" ]]; then
    if [[ ! -f "${dest_path}" ]] || ! cmp -s "${source_path}" "${dest_path}"; then
      echo ">f ${name}"
    fi
  else
    cp "${source_path}" "${dest_path}"
  fi
}

echo "BrowserOS source: ${source_dir}"
echo "Wuu Browser dest: ${browser_dir}"

copy_dir build
copy_dir chromium_files
copy_dir chromium_patches
copy_dir resources
copy_dir series_patches

mkdir -p "${browser_dir}/tools"
if [[ -d "${source_dir}/tools/patch" ]]; then
  mkdir -p "${browser_dir}/tools/patch"
  echo "Importing tools/patch/"
  rsync "${rsync_flags[@]}" "${common_excludes[@]}" \
    --exclude 'bin/' \
    --exclude 'dist/' \
    --exclude 'node_modules/' \
    "${source_dir}/tools/patch/" "${browser_dir}/tools/patch/"
fi

copy_file pyproject.toml
copy_file pyrightconfig.json
copy_file requirements.txt
copy_file uv.lock

if [[ "${dry_run}" != "true" ]]; then
  mkdir -p "${browser_dir}/resources/branding"
  : > "${browser_dir}/resources/branding/.gitkeep"
fi

echo "Import complete."
