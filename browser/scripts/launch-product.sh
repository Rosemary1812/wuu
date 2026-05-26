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
By default this uses the persistent Wuu Browser profile at
~/Library/Application Support/Wuu Browser and the real macOS keychain, so
migrated cookies, bookmarks, history, and passwords are visible. Set
WUU_BROWSER_DISABLE_PROFILE_EXTENSIONS=1 to suppress profile extensions for
focused Workbench testing, or WUU_BROWSER_PRODUCT_TEMP_PROFILE=1 for an
isolated temporary profile.
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
export WUU_BROWSER_PRODUCT_PROFILE_DIR="${WUU_BROWSER_PRODUCT_PROFILE_DIR:-${HOME}/Library/Application Support/Wuu Browser}"
export WUU_BROWSER_LAUNCH_LABEL="${WUU_BROWSER_LAUNCH_LABEL:-Wuu Browser product preview launch}"
export WUU_BROWSER_DISABLE_PROFILE_EXTENSIONS="${WUU_BROWSER_DISABLE_PROFILE_EXTENSIONS:-0}"

if [[ "${WUU_BROWSER_PRODUCT_TEMP_PROFILE:-0}" == "1" ]]; then
  export WUU_BROWSER_PROFILE_MODE="${WUU_BROWSER_PROFILE_MODE:-temp}"
  export WUU_BROWSER_USE_MOCK_KEYCHAIN="${WUU_BROWSER_USE_MOCK_KEYCHAIN:-1}"
else
  export WUU_BROWSER_PROFILE_MODE="${WUU_BROWSER_PROFILE_MODE:-product}"
fi

exec bash "${script_dir}/launch-dev.sh" "$@"
