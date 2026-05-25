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
content_browser_client_patch="${browser_dir}/chromium_patches/chrome/browser/chrome_content_browser_client.cc"
chromium_branding_debug="${browser_dir}/chromium_files/chrome/app/theme/chromium/BRANDING.debug"
chromium_branding_release="${browser_dir}/chromium_files/chrome/app/theme/chromium/BRANDING.release"
chromium_updater_branding="${browser_dir}/chromium_files/chrome/updater/branding.gni"
chromium_enterprise_branding="${browser_dir}/chromium_files/chrome/enterprise_companion/branding.gni"
macos_info_additions="${browser_dir}/resources/entitlements/Info.plist.additions"
makefile="${repo_root}/Makefile"
prepare_checkouts_script="${browser_dir}/scripts/prepare-checkouts.sh"
build_dev_script="${browser_dir}/scripts/build-dev.sh"
build_agent_script="${browser_dir}/scripts/build-agent.sh"
launch_script="${browser_dir}/scripts/launch-dev.sh"
dev_verify_script="${browser_dir}/scripts/verify-dev.sh"
package_dev_macos_script="${browser_dir}/scripts/package-dev-macos.sh"
agent_wxt_config="${browser_dir}/agent/apps/agent/wxt.config.ts"
agent_app_html="${browser_dir}/agent/apps/agent/entrypoints/app/index.html"
server_main="${browser_dir}/agent/apps/server/src/main.ts"
browser_bridge_route="${browser_dir}/agent/apps/server/src/api/routes/browser-bridge.ts"

echo "Wuu Browser product default verification"

check_contains \
  "${first_run_patch}" \
  'browser_creator_->AddFirstRunTabs({GURL("chrome://wuu")});' \
  "first-run Chromium patch opens the Wuu product URL"

check_not_contains \
  "${first_run_patch}" \
  'browser_creator_->AddFirstRunTabs({GURL("chrome://browseros/wuu")});' \
  "first-run Chromium patch no longer defaults to the BrowserOS URL"

check_not_contains \
  "${first_run_patch}" \
  'browser_creator_->AddFirstRunTabs({GURL("chrome://browseros-welcome")});' \
  "first-run Chromium patch no longer opens BrowserOS welcome"

check_contains \
  "${routes_patch}" \
  'inline constexpr char kWuuBrowserHost[] = "wuu";' \
  "Chromium virtual URL host uses the Wuu product name"

check_contains \
  "${routes_patch}" \
  '{"", kAgentExtensionId, "app.html", "/home"},' \
  "chrome://wuu routes to the Wuu workbench extension page"

check_contains \
  "${routes_patch}" \
  '{"/", kAgentExtensionId, "app.html", "/home"},' \
  "chrome://wuu/ routes to the Wuu workbench extension page"

check_contains \
  "${routes_patch}" \
  '{"/wuu", kAgentExtensionId, "app.html", "/home"},' \
  "legacy chrome://browseros/wuu route remains compatible"

check_contains \
  "${routes_patch}" \
  'return std::string("chrome://") + kWuuBrowserHost + route.virtual_path;' \
  "extension URL reverse mapping shows chrome://wuu"

check_contains \
  "${content_browser_client_patch}" \
  '!browseros::IsWuuBrowserProductHost(url->host())' \
  "Chromium URL handler accepts Wuu and legacy BrowserOS hosts"

check_contains \
  "${chromium_branding_debug}" \
  "PRODUCT_FULLNAME=Wuu Browser Dev" \
  "debug Chromium branding uses Wuu Browser Dev"

check_contains \
  "${chromium_branding_debug}" \
  "MAC_BUNDLE_ID=com.wuu.browser.dev" \
  "debug Chromium branding uses the Wuu dev bundle id"

check_not_contains \
  "${chromium_branding_debug}" \
  "BrowserOS" \
  "debug Chromium branding no longer uses BrowserOS"

check_contains \
  "${chromium_branding_release}" \
  "PRODUCT_FULLNAME=Wuu Browser" \
  "release Chromium branding uses Wuu Browser"

check_contains \
  "${chromium_branding_release}" \
  "MAC_BUNDLE_ID=com.wuu.browser" \
  "release Chromium branding uses the Wuu bundle id"

check_not_contains \
  "${chromium_branding_release}" \
  "BrowserOS" \
  "release Chromium branding no longer uses BrowserOS"

check_contains \
  "${chromium_updater_branding}" \
  'browser_product_name = "Wuu Browser"' \
  "Chromium updater branding uses Wuu Browser"

check_contains \
  "${chromium_updater_branding}" \
  'mac_updater_bundle_identifier = "com.wuu.browser.Updater"' \
  "Chromium updater branding uses the Wuu updater bundle id"

check_not_contains \
  "${chromium_updater_branding}" \
  "BrowserOS" \
  "Chromium updater branding no longer uses BrowserOS"

check_contains \
  "${chromium_enterprise_branding}" \
  'enterprise_companion_product_full_name = "WuuBrowserEnterpriseCompanion"' \
  "Chromium enterprise companion branding uses Wuu Browser"

check_contains \
  "${chromium_enterprise_branding}" \
  '"com.wuu.browser.EnterpriseCompanion"' \
  "Chromium enterprise companion branding uses the Wuu bundle id"

check_not_contains \
  "${chromium_enterprise_branding}" \
  "BrowserOS" \
  "Chromium enterprise companion branding no longer uses BrowserOS"

check_contains \
  "${macos_info_additions}" \
  "<string>Wuu Browser</string>" \
  "macOS Chromium plist additions use Wuu Browser product dir"

check_not_contains \
  "${macos_info_additions}" \
  "BrowserOS" \
  "macOS Chromium plist additions no longer use BrowserOS product dir"

check_contains \
  "${agent_wxt_config}" \
  "name: 'Wuu Browser'" \
  "repo-owned extension manifest uses Wuu Browser as the visible product name"

check_contains \
  "${agent_wxt_config}" \
  "description: 'Wuu Browser workbench and assistant.'" \
  "repo-owned extension manifest describes the Wuu workbench"

check_contains \
  "${agent_wxt_config}" \
  "default_title: 'Wuu Assistant'" \
  "repo-owned extension action uses Wuu Assistant as the visible command"

check_not_contains \
  "${agent_wxt_config}" \
  "name: 'Assistant'" \
  "repo-owned extension manifest no longer exposes the generic Assistant name"

check_not_contains \
  "${agent_wxt_config}" \
  "default_title: 'Ask BrowserOS'" \
  "repo-owned extension action no longer exposes Ask BrowserOS"

check_contains \
  "${agent_app_html}" \
  '<title>Wuu Browser</title>' \
  "Wuu workbench tab title uses Wuu Browser"

check_not_contains \
  "${agent_app_html}" \
  '<title>BrowserOS</title>' \
  "Wuu workbench tab title no longer uses BrowserOS"

check_contains \
  "${launch_script}" \
  'start_url="${WUU_BROWSER_START_URL:-chrome://wuu}"' \
  "dev launch defaults to the Wuu product URL"

check_not_contains \
  "${launch_script}" \
  'start_url="${WUU_BROWSER_START_URL:-chrome://browseros/wuu}"' \
  "dev launch no longer defaults to the BrowserOS URL"

check_contains \
  "${launch_script}" \
  '${repo_root}/browser/out/Wuu Browser Dev.app' \
  "dev launch prefers the repo-staged Wuu Browser app"

check_contains \
  "${launch_script}" \
  '"--disable-browseros-server-updater"' \
  "dev launch keeps BrowserOS server updater disabled"

check_contains \
  "${makefile}" \
  "browser-prepare-checkouts:" \
  "Makefile exposes the BrowserOS/Chromium checkout preparation entry point"

check_contains \
  "${makefile}" \
  'bash browser/scripts/prepare-checkouts.sh $(ARGS)' \
  "checkout preparation can receive explicit script arguments"

check_contains \
  "${prepare_checkouts_script}" \
  "WUU_BROWSEROS_GIT_URL" \
  "checkout preparation can clone the pinned BrowserOS reference"

check_contains \
  "${prepare_checkouts_script}" \
  '.worktrees/chromium/src' \
  "checkout preparation defaults Chromium source to ignored .worktrees"

check_contains \
  "${prepare_checkouts_script}" \
  "fetch --nohooks chromium" \
  "checkout preparation can create a Chromium checkout with depot_tools"

check_contains \
  "${prepare_checkouts_script}" \
  "gclient sync -D --no-history --shallow" \
  "checkout preparation syncs Chromium dependencies"

check_contains \
  "${prepare_checkouts_script}" \
  'patch-checkout.sh" "${patch_args[@]}"' \
  "checkout preparation can apply Wuu Browser patches after setup"

check_contains \
  "${prepare_checkouts_script}" \
  '[[ "${dry_run}" == "true" ]]' \
  "checkout preparation supports dry-run planning"

check_contains \
  "${makefile}" \
  "browser-build-dev:" \
  "Makefile exposes the Wuu Browser dev build entry point"

check_contains \
  "${makefile}" \
  'bash browser/scripts/build-dev.sh $(ARGS)' \
  "dev build can receive explicit script arguments"

check_contains \
  "${build_dev_script}" \
  'build_modules="${WUU_BROWSER_BUILD_MODULES:-resources,chromium_replace,configure,compile}"' \
  "dev build uses repository-owned resources and Chromium replacement files"

check_contains \
  "${build_dev_script}" \
  'prepare_args=(--chromium-src "${chromium_src}" --apply-patches)' \
  "dev build can prepare and patch the checkout before compiling"

check_contains \
  "${build_dev_script}" \
  '-m build.browseros' \
  "dev build runs the imported BrowserOS build system from this repository"

check_contains \
  "${build_dev_script}" \
  "--package-macos" \
  "dev build can stage the macOS Wuu Browser Dev app after compiling"

check_contains \
  "${makefile}" \
  "browser-build-agent:" \
  "Makefile exposes the Wuu Browser agent asset build entry point"

check_contains \
  "${makefile}" \
  'bash browser/scripts/build-agent.sh $(ARGS)' \
  "agent asset build can receive explicit script arguments"

check_contains \
  "${build_agent_script}" \
  "bun run build:agent:dev" \
  "agent asset build can produce the browser-hosted Wuu workbench extension"

check_contains \
  "${build_agent_script}" \
  'bun scripts/build/server.ts --target="${server_target}" --no-upload --ci' \
  "agent asset build can produce local server resource bundles without upload"

check_contains \
  "${build_agent_script}" \
  'browser/agent/node_modules is missing.' \
  "agent asset build fails clearly when dependencies are not installed"

check_contains \
  "${launch_script}" \
  "WUU_BROWSER_STAGE_EXTENSION" \
  "dev launch can auto-build the repo-owned extension"

check_contains \
  "${launch_script}" \
  'bash "${repo_root}/browser/scripts/build-agent.sh" "${extension_build_args[@]}"' \
  "dev launch uses the repo-owned agent asset build entry point"

check_contains \
  "${launch_script}" \
  'Run make browser-build-agent ARGS=\"--extension\" or set WUU_BROWSER_EXTENSION.' \
  "dev launch points missing extension errors at the repo-owned build command"

check_contains \
  "${launch_script}" \
  "WUU_BROWSER_SERVER_TARGET" \
  "dev launch can select the server resource target"

check_contains \
  "${launch_script}" \
  'server_build_args=(--server --server-target "${server_target}")' \
  "dev launch stages server resources through the repo-owned agent build entry point"

check_contains \
  "${launch_script}" \
  'browseros-server-resources-${server_target}.zip' \
  "dev launch uses the selected server resource target archive"

check_not_contains \
  "${launch_script}" \
  'bun run build:server:test' \
  "dev launch no longer bypasses browser-build-agent for server resources"

check_not_contains \
  "${launch_script}" \
  'browseros-server-resources-darwin-arm64.zip' \
  "dev launch no longer hardcodes the macOS ARM64 server resource archive"

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
