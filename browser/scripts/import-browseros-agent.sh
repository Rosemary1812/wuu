#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: browser/scripts/import-browseros-agent.sh [--dry-run]

Import BrowserOS agent/server source into this Wuu Browser product repository.

Environment overrides:
  WUU_BROWSEROS_REPO  BrowserOS reference checkout. Defaults to .worktrees/browseros,
                      then falls back to ~/wuu-browseros when present.

Imported from packages/browseros-agent:
  apps/agent/
  apps/server/
  apps/cli/
  packages/
  scripts/
  tools/dev/
  root workspace manifests and configuration files

Not imported:
  node_modules
  dist/.output/.wxt build outputs
  local environment files
  eval benchmark data
  Claude/agent-local instruction files
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
dest_dir="${repo_root}/browser/agent"

browseros_repo="${WUU_BROWSEROS_REPO:-${repo_root}/.worktrees/browseros}"
if [[ ! -d "${browseros_repo}" && -d "${HOME}/wuu-browseros" ]]; then
  browseros_repo="${HOME}/wuu-browseros"
fi

source_dir="${browseros_repo}/packages/browseros-agent"

if [[ ! -d "${source_dir}" ]]; then
  echo "BrowserOS packages/browseros-agent not found: ${source_dir}" >&2
  echo "Set WUU_BROWSEROS_REPO to a BrowserOS checkout." >&2
  exit 1
fi

if ! command -v rsync >/dev/null 2>&1; then
  echo "rsync is required to import BrowserOS agent source" >&2
  exit 2
fi

rsync_flags=(-a --delete --omit-dir-times)
if [[ "${dry_run}" == "true" ]]; then
  rsync_flags+=(--dry-run --itemize-changes)
fi

excludes=(
  --exclude 'node_modules/'
  --exclude 'dist/'
  --exclude '.output/'
  --exclude '.wxt/'
  --exclude '.devtools/'
  --exclude '.agents/'
  --exclude '.claude/'
  --exclude '.config/'
  --exclude '.vscode/'
  --exclude '.turbo/'
  --exclude 'coverage/'
  --exclude 'logs/'
  --exclude 'apps/agent/generated/'
  --exclude '__pycache__/'
  --exclude '*.pyc'
  --exclude '.DS_Store'
  --exclude '.env.development'
  --exclude '.env.production'
  --exclude 'CLAUDE.md'
  --exclude 'AGENTS.md'
  --exclude 'apps/agent/entrypoints/background/index.ts'
  --exclude 'apps/server/.gitignore'
  --exclude 'apps/eval/'
  --exclude 'tools/dogfood/'
)

mkdir -p "${dest_dir}"

echo "BrowserOS agent source: ${source_dir}"
echo "Wuu Browser agent dest: ${dest_dir}"

rsync "${rsync_flags[@]}" "${excludes[@]}" "${source_dir}/" "${dest_dir}/"

echo "Import complete."
