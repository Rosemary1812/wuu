#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"
browser_dir="${repo_root}/browser"

failures=0

check_contains() {
  local file="$1"
  local pattern="$2"
  local label="$3"
  if grep -Fq "${pattern}" "${file}"; then
    printf 'ok: %s\n' "${label}"
    return
  fi
  printf 'fail: %s\n' "${label}" >&2
  printf '  missing pattern: %s\n' "${pattern}" >&2
  failures=$((failures + 1))
}

check_not_contains() {
  local file="$1"
  local pattern="$2"
  local label="$3"
  if grep -Fq "${pattern}" "${file}"; then
    printf 'fail: %s\n' "${label}" >&2
    printf '  forbidden pattern: %s\n' "${pattern}" >&2
    failures=$((failures + 1))
    return
  fi
  printf 'ok: %s\n' "${label}"
}

first_run_patch="${browser_dir}/chromium_patches/chrome/browser/chrome_browser_main.cc"
routes_patch="${browser_dir}/chromium_patches/chrome/browser/browseros/core/browseros_constants.h"
launch_script="${browser_dir}/scripts/launch-dev.sh"
server_main="${browser_dir}/agent/apps/server/src/main.ts"

echo "Wuu Browser product default verification"

check_contains \
  "${first_run_patch}" \
  'browser_creator_->AddFirstRunTabs({GURL("chrome://browseros/wuu")});' \
  "first-run Chromium patch opens the Wuu workbench"

check_not_contains \
  "${first_run_patch}" \
  'browser_creator_->AddFirstRunTabs({GURL("chrome://browseros-welcome")});' \
  "first-run Chromium patch no longer opens BrowserOS welcome"

check_contains \
  "${routes_patch}" \
  '{"/wuu", kAgentExtensionId, "app.html", "/home"},' \
  "chrome://browseros/wuu routes to the Wuu workbench extension page"

check_contains \
  "${launch_script}" \
  'start_url="${WUU_BROWSER_START_URL:-chrome://browseros/wuu}"' \
  "dev launch defaults to the product Wuu route"

check_contains \
  "${launch_script}" \
  '"--disable-browseros-server-updater"' \
  "dev launch keeps BrowserOS server updater disabled"

check_contains \
  "${server_main}" \
  "process.env.WUU_ENABLE_VM_AGENTS === '1'" \
  "server only enables VM-backed agents through explicit opt-in"

check_not_contains \
  "${server_main}" \
  'configureVmRuntime({ resourcesDir })' \
  "server default startup does not configure the VM runtime"

if [[ "${failures}" -ne 0 ]]; then
  echo "Product default verification failed." >&2
  exit 1
fi

echo "Product defaults are aligned."
