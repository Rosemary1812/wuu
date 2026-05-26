#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: browser/scripts/launch-product.sh [launch-dev args...]

Launch the production-named local Wuu Browser app with repository-owned agent
assets. This is a product-preview launch path: it uses Wuu Browser.app while
loading the latest local Workbench extension and server resources from this
repository.

Environment overrides are the same as browser/scripts/launch-dev.sh.
USAGE
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"

export WUU_BROWSER_APP="${WUU_BROWSER_APP:-${repo_root}/browser/out/Wuu Browser.app}"
export WUU_BROWSER_STAGE_EXTENSION="${WUU_BROWSER_STAGE_EXTENSION:-1}"
export WUU_BROWSER_STAGE_SERVER_RESOURCES="${WUU_BROWSER_STAGE_SERVER_RESOURCES:-1}"
export WUU_BROWSER_PROFILE_PREFIX="${WUU_BROWSER_PROFILE_PREFIX:-wuu-browser-product}"
export WUU_BROWSER_LAUNCH_LABEL="${WUU_BROWSER_LAUNCH_LABEL:-Wuu Browser product preview launch}"

exec bash "${script_dir}/launch-dev.sh" "$@"
