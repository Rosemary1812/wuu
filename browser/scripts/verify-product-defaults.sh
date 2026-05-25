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
  if grep -Fq -- "${pattern}" "${file}"; then
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
  if grep -Fq -- "${pattern}" "${file}"; then
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
dev_verify_script="${browser_dir}/scripts/verify-dev.sh"
package_dev_macos_script="${browser_dir}/scripts/package-dev-macos.sh"
server_main="${browser_dir}/agent/apps/server/src/main.ts"
browser_bridge_route="${browser_dir}/agent/apps/server/src/api/routes/browser-bridge.ts"

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
  '${repo_root}/browser/out/Wuu Browser Dev.app' \
  "dev launch prefers the repo-staged Wuu Browser app"

check_contains \
  "${launch_script}" \
  '"--disable-browseros-server-updater"' \
  "dev launch keeps BrowserOS server updater disabled"

check_contains \
  "${package_dev_macos_script}" \
  'plutil -replace CFBundleDisplayName -string "Wuu Browser Dev"' \
  "macOS dev packaging applies Wuu Browser visible app name"

check_contains \
  "${package_dev_macos_script}" \
  'plutil -replace CFBundleIdentifier -string "com.wuu.browser.dev"' \
  "macOS dev packaging applies Wuu Browser bundle id"

check_contains \
  "${package_dev_macos_script}" \
  'codesign --force --deep --sign - "${output_app}"' \
  "macOS dev packaging ad-hoc signs the staged app"

check_contains \
  "${package_dev_macos_script}" \
  "find \"\${source_build_dir}\" -maxdepth 1 -type f -name '*.dylib' -print0" \
  "macOS dev packaging bundles Chromium component-build dylibs"

check_contains \
  "${dev_verify_script}" \
  "--require-project-folder-picker" \
  "dev verification can require native project folder picker wiring"

check_contains \
  "${dev_verify_script}" \
  "chrome.browserOS.choosePath" \
  "dev verification checks the native BrowserOS choosePath API"

check_contains \
  "${dev_verify_script}" \
  "--require-wuu-turn-start" \
  "dev verification can require prompt-to-turn startup"

check_contains \
  "${dev_verify_script}" \
  "window.wuu.startTurn" \
  "dev verification checks Wuu prompt submission"

check_contains \
  "${dev_verify_script}" \
  "window.wuu.interruptTurn" \
  "dev verification interrupts the running Wuu turn"

check_contains \
  "${dev_verify_script}" \
  "--require-project-local-ops" \
  "dev verification can require selected-project local operations"

check_contains \
  "${dev_verify_script}" \
  "window.wuu.listWorkspaceFiles" \
  "dev verification checks Wuu file tree operations in the selected project"

check_contains \
  "${dev_verify_script}" \
  "window.wuu.gitStatus" \
  "dev verification checks Wuu Git operations in the selected project"

check_contains \
  "${dev_verify_script}" \
  "window.wuu.startTerminalSession" \
  "dev verification checks Wuu terminal startup in the selected project"

check_contains \
  "${browser_bridge_route}" \
  "/tabs/:targetId/snapshot" \
  "Browser Bridge exposes target accessibility snapshots"

check_contains \
  "${browser_bridge_route}" \
  "/tabs/:targetId/dom" \
  "Browser Bridge exposes target DOM HTML"

check_contains \
  "${browser_bridge_route}" \
  "/tabs/:targetId/evaluate" \
  "Browser Bridge exposes target JavaScript evaluation"

check_contains \
  "${browser_bridge_route}" \
  "/tabs/:targetId/console" \
  "Browser Bridge exposes target console logs"

check_contains \
  "${browser_bridge_route}" \
  "/tabs/:targetId/network" \
  "Browser Bridge exposes target network entries"

check_contains \
  "${browser_bridge_route}" \
  "/tabs/:targetId/activate" \
  "Browser Bridge exposes target activation"

check_contains \
  "${browser_bridge_route}" \
  "/tabs/:targetId/back" \
  "Browser Bridge exposes target history back"

check_contains \
  "${browser_bridge_route}" \
  "/tabs/:targetId/forward" \
  "Browser Bridge exposes target history forward"

check_contains \
  "${browser_bridge_route}" \
  "/tabs/:targetId/reload" \
  "Browser Bridge exposes target reload"

check_contains \
  "${browser_bridge_route}" \
  ".delete('/tabs/:targetId'" \
  "Browser Bridge exposes target close"

check_contains \
  "${dev_verify_script}" \
  "/snapshot?enhanced=1" \
  "dev verification checks Browser Bridge enhanced snapshots"

check_contains \
  "${dev_verify_script}" \
  "/dom?selector=%23status" \
  "dev verification checks Browser Bridge DOM reads"

check_contains \
  "${dev_verify_script}" \
  "/evaluate" \
  "dev verification checks Browser Bridge JavaScript evaluation"

check_contains \
  "${dev_verify_script}" \
  "/console?level=error" \
  "dev verification checks Browser Bridge console reads"

check_contains \
  "${dev_verify_script}" \
  "/network?search=__wuu_bridge_missing" \
  "dev verification checks Browser Bridge network reads"

check_contains \
  "${dev_verify_script}" \
  "/active-tab" \
  "dev verification checks Browser Bridge tab activation"

check_contains \
  "${dev_verify_script}" \
  "/back" \
  "dev verification checks Browser Bridge history back"

check_contains \
  "${dev_verify_script}" \
  "/forward" \
  "dev verification checks Browser Bridge history forward"

check_contains \
  "${dev_verify_script}" \
  "/reload" \
  "dev verification checks Browser Bridge reload"

check_contains \
  "${dev_verify_script}" \
  "closedCreatedTab" \
  "dev verification checks Browser Bridge tab close"

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
