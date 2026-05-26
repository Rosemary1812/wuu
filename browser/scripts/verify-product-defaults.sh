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

check_nearby_contains() {
  local file="$1"
  local anchor="$2"
  local pattern="$3"
  local label="$4"
  if grep -F -A 4 -- "${anchor}" "${file}" | grep -Fq -- "${pattern}"; then
    printf 'ok: %s\n' "${label}"
    return
  fi
  printf 'fail: %s\n' "${label}" >&2
  printf '  anchor: %s\n' "${anchor}" >&2
  printf '  missing nearby pattern: %s\n' "${pattern}" >&2
  failures=$((failures + 1))
}

check_file_absent() {
  local file="$1"
  local label="$2"
  if [[ -e "${file}" ]]; then
    printf 'fail: %s\n' "${label}" >&2
    printf '  unexpected file: %s\n' "${file}" >&2
    failures=$((failures + 1))
    return
  fi
  printf 'ok: %s\n' "${label}"
}

first_run_patch="${browser_dir}/chromium_patches/chrome/browser/chrome_browser_main.cc"
routes_patch="${browser_dir}/chromium_patches/chrome/browser/browseros/core/browseros_constants.h"
content_browser_client_patch="${browser_dir}/chromium_patches/chrome/browser/chrome_content_browser_client.cc"
browser_actions_patch="${browser_dir}/chromium_patches/chrome/browser/ui/browser_actions.cc"
browser_commands_patch="${browser_dir}/chromium_patches/chrome/browser/ui/browser_commands.cc"
startup_creator_patch="${browser_dir}/chromium_patches/chrome/browser/ui/startup/startup_browser_creator_impl.cc"
extension_registrar_patch="${browser_dir}/chromium_patches/chrome/browser/extensions/chrome_extension_registrar_delegate.cc"
tab_renderer_data_header_patch="${browser_dir}/chromium_patches/chrome/browser/ui/tabs/tab_renderer_data.h"
tab_renderer_data_patch="${browser_dir}/chromium_patches/chrome/browser/ui/tabs/tab_renderer_data.cc"
tab_strip_model_patch="${browser_dir}/chromium_patches/chrome/browser/ui/tabs/tab_strip_model.cc"
browser_tab_strip_controller_patch="${browser_dir}/chromium_patches/chrome/browser/ui/views/tabs/browser_tab_strip_controller.cc"
horizontal_tab_view_patch="${browser_dir}/chromium_patches/chrome/browser/ui/views/tabs/tab.cc"
vertical_tab_view_patch="${browser_dir}/chromium_patches/chrome/browser/ui/views/tabs/vertical/vertical_tab_view.cc"
web_app_tabbed_utils_patch="${browser_dir}/chromium_patches/chrome/browser/ui/web_applications/web_app_tabbed_utils.cc"
browseros_action_utils_patch="${browser_dir}/chromium_patches/chrome/browser/browseros/core/browseros_action_utils.h"
browseros_prefs_patch="${browser_dir}/chromium_patches/chrome/browser/browseros/core/browseros_prefs.cc"
ui_features_patch="${browser_dir}/chromium_patches/chrome/browser/ui/ui_features.cc"
customize_toolbar_handler_patch="${browser_dir}/chromium_patches/chrome/browser/ui/webui/side_panel/customize_chrome/customize_toolbar/customize_toolbar_handler.cc"
customize_toolbar_mojom_patch="${browser_dir}/chromium_patches/chrome/browser/ui/webui/side_panel/customize_chrome/customize_toolbar/customize_toolbar.mojom"
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
launch_product_script="${browser_dir}/scripts/launch-product.sh"
migrate_doubao_profile_script="${browser_dir}/scripts/migrate-doubao-profile.sh"
dev_verify_script="${browser_dir}/scripts/verify-dev.sh"
package_dev_macos_script="${browser_dir}/scripts/package-dev-macos.sh"
package_macos_script="${browser_dir}/scripts/package-macos.sh"
stage_macos_icons_script="${browser_dir}/scripts/stage-wuu-mac-icons.sh"
agent_wxt_config="${browser_dir}/agent/apps/agent/wxt.config.ts"
agent_app_html="${browser_dir}/agent/apps/agent/entrypoints/app/index.html"
agent_app_routes="${browser_dir}/agent/apps/agent/entrypoints/app/App.tsx"
agent_background="${browser_dir}/agent/apps/agent/entrypoints/background/index.ts"
agent_sidebar_layout="${browser_dir}/agent/apps/agent/entrypoints/app/layout/SidebarLayout.tsx"
agent_sidebar_navigation="${browser_dir}/agent/apps/agent/components/sidebar/SidebarNavigation.tsx"
agent_sidebar_branding="${browser_dir}/agent/apps/agent/components/sidebar/SidebarBranding.tsx"
agent_sidebar_footer="${browser_dir}/agent/apps/agent/components/sidebar/SidebarUserFooter.tsx"
server_main="${browser_dir}/agent/apps/server/src/main.ts"
server_api="${browser_dir}/agent/apps/server/src/api/server.ts"
request_auth="${browser_dir}/agent/apps/server/src/api/utils/request-auth.ts"
wuu_route="${browser_dir}/agent/apps/server/src/api/routes/wuu.ts"
browser_bridge_route="${browser_dir}/agent/apps/server/src/api/routes/browser-bridge.ts"
wuu_tool="${repo_root}/internal/tools/tool_browser.go"
wuu_toolkit="${repo_root}/internal/tools/toolkit.go"
wuu_prompt="${repo_root}/internal/config/config.go"
browseros_source="${browser_dir}/BROWSEROS_SOURCE.md"

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
  'inline constexpr char kWuuBrowserURL[] = "chrome://wuu";' \
  "Chromium default workbench URL uses the Wuu product URL"

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
  "${browser_commands_patch}" \
  'GURL(browseros::kWuuBrowserURL)' \
  "empty browser windows open the Wuu workbench tab"

check_contains \
  "${startup_creator_patch}" \
  'StartupTab(GURL(browseros::kWuuBrowserURL))' \
  "default startup opens the Wuu workbench tab"

check_contains \
  "${extension_registrar_patch}" \
  'ReloadPendingWuuWorkbenchTabs(profile_);' \
  "Workbench tabs recover after the agent extension activates"

check_contains \
  "${tab_renderer_data_header_patch}" \
  'bool can_close = true;' \
  "tab renderer data carries close eligibility"

check_contains \
  "${tab_renderer_data_patch}" \
  'can_close == other.can_close' \
  "tab renderer data refreshes when close eligibility changes"

check_contains \
  "${tab_strip_model_patch}" \
  'IsProtectedWuuWorkbenchTab(this, index) && !closing_all_' \
  "the first Workbench tab cannot be closed individually"

check_contains \
  "${browser_tab_strip_controller_patch}" \
  'data.can_close = web_app::IsTabClosable(model_, model_index);' \
  "tab strip propagates close eligibility to tab views"

check_contains \
  "${horizontal_tab_view_patch}" \
  'const bool should_show_close_button = data_.can_close;' \
  "horizontal tabs hide the close affordance for protected tabs"

check_contains \
  "${vertical_tab_view_patch}" \
  'if (!tab_data_.can_close || pinned_) {' \
  "vertical tabs hide the close affordance for protected tabs"

check_contains \
  "${web_app_tabbed_utils_patch}" \
  'return tab_strip_model->closing_all();' \
  "protected Workbench tabs still close during whole-window shutdown"

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
  "${agent_wxt_config}" \
  "page: 'app.html#/home'" \
  "extension options page opens the Wuu workbench"

check_not_contains \
  "${agent_wxt_config}" \
  "chrome_url_overrides" \
  "new tabs use Chromium's native new tab page"

check_not_contains \
  "${agent_wxt_config}" \
  "page: 'app.html#/settings'" \
  "extension options page no longer opens legacy BrowserOS AI settings"

check_not_contains \
  "${agent_background}" \
  "details.reason === chrome.runtime.OnInstalledReason.INSTALL" \
  "extension install no longer creates an extra Workbench tab"

check_contains \
  "${agent_app_routes}" \
  'path="settings/*" element={<WuuHomeRedirect />}' \
  "legacy settings routes redirect to the Wuu workbench"

check_contains \
  "${agent_app_routes}" \
  'path="agents/*" element={<WuuHomeRedirect />}' \
  "legacy agents routes redirect to the Wuu workbench"

check_contains \
  "${agent_app_routes}" \
  'path="connect-apps" element={<WuuHomeRedirect />}' \
  "legacy connect-apps route redirects to the Wuu workbench"

check_contains \
  "${agent_app_routes}" \
  'path="scheduled" element={<WuuHomeRedirect />}' \
  "legacy scheduled tasks route redirects to the Wuu workbench"

check_not_contains \
  "${agent_app_routes}" \
  "NewTabChat" \
  "Wuu Browser app routes no longer mount legacy new-tab chat"

check_not_contains \
  "${agent_app_routes}" \
  "AgentCommandHome" \
  "Wuu Browser app routes no longer mount legacy BrowserOS agent home"

check_not_contains \
  "${agent_app_routes}" \
  "AISettingsPage" \
  "Wuu Browser app routes no longer mount legacy BrowserOS AI settings"

check_not_contains \
  "${agent_background}" \
  "toggleSidePanel" \
  "extension action no longer toggles the legacy side panel"

check_not_contains \
  "${agent_background}" \
  "onOpenSidePanelWithSearch" \
  "background script no longer opens the legacy search side panel"

check_not_contains \
  "${agent_background}" \
  "setupLlmProvidersSyncToBackend" \
  "background script no longer syncs legacy BrowserOS AI providers"

check_not_contains \
  "${agent_background}" \
  "scheduledJobRuns" \
  "background script no longer runs legacy BrowserOS scheduled AI jobs"

check_contains \
  "${agent_sidebar_layout}" \
  "Wuu Browser" \
  "mobile shell branding uses Wuu Browser"

check_not_contains \
  "${agent_sidebar_layout}" \
  "BrowserOS" \
  "mobile shell branding no longer uses BrowserOS"

check_not_contains \
  "${agent_sidebar_navigation}" \
  "Connect Apps" \
  "sidebar no longer exposes legacy connect-apps navigation"

check_not_contains \
  "${agent_sidebar_navigation}" \
  "Scheduled Tasks" \
  "sidebar no longer exposes legacy scheduled task navigation"

check_not_contains \
  "${agent_sidebar_navigation}" \
  "Agents" \
  "sidebar no longer exposes legacy BrowserOS agents navigation"

check_not_contains \
  "${agent_sidebar_navigation}" \
  "Settings" \
  "sidebar no longer exposes legacy BrowserOS settings navigation"

check_not_contains \
  "${agent_sidebar_branding}" \
  "BrowserOS" \
  "sidebar branding no longer uses BrowserOS"

check_contains \
  "${agent_sidebar_branding}" \
  "Wuu Browser" \
  "sidebar branding uses Wuu Browser"

check_not_contains \
  "${agent_sidebar_footer}" \
  "About BrowserOS" \
  "sidebar footer no longer links BrowserOS help"

check_contains \
  "${agent_sidebar_footer}" \
  "About Wuu" \
  "sidebar footer points at Wuu"

check_nearby_contains \
  "${ui_features_patch}" \
  "BASE_FEATURE(kThirdPartyLlmPanel," \
  "base::FEATURE_DISABLED_BY_DEFAULT" \
  "legacy BrowserOS Chat native toolbar feature is disabled by default"

check_nearby_contains \
  "${ui_features_patch}" \
  "BASE_FEATURE(kClashOfGpts," \
  "base::FEATURE_DISABLED_BY_DEFAULT" \
  "legacy BrowserOS Council native toolbar feature is disabled by default"

check_not_contains \
  "${browser_actions_patch}" \
  "kActionBrowserOSAgent" \
  "Chromium native toolbar no longer registers the BrowserOS Assistant action"

check_not_contains \
  "${browser_actions_patch}" \
  'SetText(u"Assistant")' \
  "Chromium native toolbar no longer exposes Assistant text"

check_not_contains \
  "${browser_actions_patch}" \
  "Ask BrowserOS" \
  "Chromium native toolbar no longer exposes Ask BrowserOS tooltip"

check_not_contains \
  "${browseros_action_utils_patch}" \
  "kActionBrowserOSAgent" \
  "BrowserOS native action treatment no longer includes Assistant"

check_contains \
  "${browseros_prefs_patch}" \
  "registry->RegisterBooleanPref(prefs::kShowLLMChat, false);" \
  "legacy BrowserOS Chat toolbar visibility pref defaults off"

check_contains \
  "${browseros_prefs_patch}" \
  "registry->RegisterBooleanPref(prefs::kShowLLMHub, false);" \
  "legacy BrowserOS Council toolbar visibility pref defaults off"

check_file_absent \
  "${customize_toolbar_handler_patch}" \
  "Chromium customize toolbar no longer exposes BrowserOS AI actions"

check_file_absent \
  "${customize_toolbar_mojom_patch}" \
  "Chromium customize toolbar mojo no longer exposes BrowserOS AI action ids"

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
  "${launch_script}" \
  'stage-wuu-mac-icons.sh" --sign "${app_path}"' \
  "dev launch stages Wuu macOS icons before opening the app"

check_contains \
  "${launch_script}" \
  'profile_mode="${WUU_BROWSER_PROFILE_MODE:-temp}"' \
  "dev launch defaults to isolated temporary profiles"

check_contains \
  "${launch_script}" \
  '--product-profile|--persistent-profile)' \
  "dev launch exposes a persistent Wuu Browser profile mode"

check_contains \
  "${launch_script}" \
  'use_mock_keychain=0' \
  "persistent profile mode can use the real macOS keychain"

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
  'build_modules="${WUU_BROWSER_BUILD_MODULES:-}"' \
  "dev build accepts explicit module overrides from the environment"

check_contains \
  "${build_dev_script}" \
  'ninja_targets="${WUU_BROWSER_NINJA_TARGETS:-}"' \
  "dev build accepts explicit Ninja target overrides from the environment"

check_contains \
  "${build_dev_script}" \
  'build_modules="$(default_build_modules)"' \
  "dev build uses platform-aware default build modules"

check_contains \
  "${build_dev_script}" \
  'Darwin) modules="sparkle_setup,${modules}"' \
  "dev build prepares Sparkle for macOS browser builds"

check_contains \
  "${build_dev_script}" \
  'resources,bundled_extensions,chromium_replace,configure,compile' \
  "dev build uses repository-owned resources, bundled extensions, and Chromium replacement files"

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
  "${build_dev_script}" \
  "--ninja-targets" \
  "dev build exposes narrow Ninja target selection for local Chromium iteration"

check_contains \
  "${makefile}" \
  "browser-package-macos:" \
  "Makefile exposes the Wuu Browser product package entry point"

check_contains \
  "${makefile}" \
  'bash browser/scripts/package-macos.sh $(ARGS)' \
  "product package can receive explicit script arguments"

check_contains \
  "${makefile}" \
  "browser-launch-product:" \
  "Makefile exposes the Wuu Browser product preview launch entry point"

check_contains \
  "${makefile}" \
  'bash browser/scripts/launch-product.sh $(ARGS)' \
  "product preview launch can receive explicit script arguments"

check_contains \
  "${makefile}" \
  "browser-migrate-doubao-profile:" \
  "Makefile exposes the Doubao credential recovery entry point"

check_contains \
  "${makefile}" \
  'bash browser/scripts/migrate-doubao-profile.sh $(ARGS)' \
  "Doubao credential recovery can receive explicit script arguments"

check_contains \
  "${migrate_doubao_profile_script}" \
  'mode=dry-run' \
  "Doubao credential recovery is dry-run by default"

check_contains \
  "${migrate_doubao_profile_script}" \
  'copy_keychain_secret' \
  "Doubao credential recovery copies keychain data through a local helper"

check_contains \
  "${migrate_doubao_profile_script}" \
  'cp -pR "${target_profile}" "${backup_profile}"' \
  "Doubao credential recovery backs up the target Wuu Browser profile"

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

check_contains \
  "${launch_script}" \
  "--disable-browseros-extensions" \
  "local launch disables bundled BrowserOS extensions before loading repo assets"

check_contains \
  "${launch_script}" \
  '"--load-extension=${extension_dir}"' \
  "local launch loads the repo-owned Workbench extension"

check_contains \
  "${launch_script}" \
  'cleanup_matching_processes "${repo_root}/browser/out/Wuu Browser.app"' \
  "local launch stops stale product preview browser instances"

check_contains \
  "${launch_product_script}" \
  'WUU_BROWSER_APP="${WUU_BROWSER_APP:-${repo_root}/browser/out/Wuu Browser.app}"' \
  "product preview launch uses the production-named local app"

check_contains \
  "${launch_product_script}" \
  'WUU_BROWSER_STAGE_EXTENSION="${WUU_BROWSER_STAGE_EXTENSION:-1}"' \
  "product preview launch stages the latest repo-owned extension by default"

check_contains \
  "${launch_product_script}" \
  'WUU_BROWSER_STAGE_SERVER_RESOURCES="${WUU_BROWSER_STAGE_SERVER_RESOURCES:-1}"' \
  "product preview launch stages the latest repo-owned server resources by default"

check_contains \
  "${launch_product_script}" \
  'WUU_BROWSER_PRODUCT_PROFILE_DIR="${WUU_BROWSER_PRODUCT_PROFILE_DIR:-${HOME}/Library/Application Support/Wuu Browser}"' \
  "product preview launch targets the persistent Wuu Browser profile by default"

check_contains \
  "${launch_product_script}" \
  'WUU_BROWSER_PROFILE_MODE="${WUU_BROWSER_PROFILE_MODE:-product}"' \
  "product preview launch defaults to persistent product profile mode"

check_contains \
  "${launch_product_script}" \
  'WUU_BROWSER_PRODUCT_TEMP_PROFILE' \
  "product preview launch keeps temporary profiles as an explicit opt-in"

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
  "${package_dev_macos_script}" \
  'stage-wuu-mac-icons.sh" "${output_app}"' \
  "macOS dev packaging stages Wuu icon resources"

check_contains \
  "${package_macos_script}" \
  'plutil -replace CFBundleDisplayName -string "Wuu Browser"' \
  "macOS product packaging applies Wuu Browser visible app name"

check_contains \
  "${package_macos_script}" \
  'plutil -replace CFBundleIdentifier -string "com.wuu.browser"' \
  "macOS product packaging applies Wuu Browser bundle id"

check_contains \
  "${package_macos_script}" \
  '--allow-dev-source' \
  "macOS product packaging requires an explicit flag for dev-source previews"

check_contains \
  "${package_macos_script}" \
  '--update-enabled' \
  "macOS product packaging has an explicit update-ready gate"

check_contains \
  "${package_macos_script}" \
  'codesign_checked "codesign verification"' \
  "macOS product packaging keeps successful signing output concise"

check_contains \
  "${package_macos_script}" \
  'Move browser, extension, and server update feeds to Wuu-owned endpoints before using --update-enabled.' \
  "macOS product packaging blocks BrowserOS update feeds for update-ready releases"

check_contains \
  "${package_macos_script}" \
  'hdiutil create' \
  "macOS product packaging can create a product DMG"

check_contains \
  "${stage_macos_icons_script}" \
  'browser/resources/icons/product_logo_32.png' \
  "macOS icon staging replaces Chromium runtime product logo"

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

check_contains \
  "${server_main}" \
  "host: '127.0.0.1'" \
  "server application binds the local HTTP server to loopback"

check_not_contains \
  "${server_main}" \
  "host: '0.0.0.0'" \
  "server application no longer binds the local HTTP server to all interfaces"

check_contains \
  "${server_api}" \
  "host = '127.0.0.1'" \
  "HTTP server default host is loopback"

check_not_contains \
  "${server_api}" \
  "host = '0.0.0.0'" \
  "HTTP server default host no longer exposes all interfaces"

check_contains \
  "${request_auth}" \
  "return isLocalhost && isTrustedAppOrigin(origin)" \
  "trusted app origins also require a loopback client socket"

check_contains \
  "${wuu_route}" \
  "WUU_BROWSER_BRIDGE_URL" \
  "Wuu Browser injects the Browser Bridge URL into the Wuu runtime"

check_contains \
  "${wuu_toolkit}" \
  "NewBrowserTool(e)" \
  "Wuu toolkit can register browser bridge tools"

check_contains \
  "${wuu_tool}" \
  'Name:        "browser"' \
  "Wuu exposes Browser Bridge as a single browser tool"

check_contains \
  "${wuu_tool}" \
  "browser bridge url must be loopback" \
  "Wuu browser tool rejects non-loopback bridge URLs"

check_contains \
  "${wuu_prompt}" \
  "When the browser tool is available in Wuu Browser" \
  "Wuu system prompt teaches when to use browser validation"

check_contains \
  "${browseros_source}" \
  "License and release boundary" \
  "BrowserOS source documentation defines the release boundary"

check_contains \
  "${browseros_source}" \
  "AGPL-3.0-or-later" \
  "BrowserOS-derived assets keep their AGPL license boundary visible"

check_contains \
  "${browseros_source}" \
  "Do not describe those artifacts as MIT-only" \
  "Wuu Browser releases are not documented as MIT-only artifacts"

check_contains \
  "${browseros_source}" \
  "Keep Wuu Desktop release notes separate" \
  "Wuu Desktop and Wuu Browser release obligations stay separate"

check_not_contains \
  "${server_main}" \
  'configureVmRuntime({ resourcesDir })' \
  "server default startup does not configure the VM runtime"

if [[ "${failures}" -ne 0 ]]; then
  echo "Product default verification failed." >&2
  exit 1
fi

echo "Product defaults are aligned."
