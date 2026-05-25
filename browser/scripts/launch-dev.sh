#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: browser/scripts/launch-dev.sh [--dry-run] [--profile-dir DIR] [--url URL]

Launch the local Wuu Browser development build.

Environment overrides:
  WUU_BROWSEROS_REPO       BrowserOS reference checkout.
  WUU_CHROMIUM_SRC         Chromium source checkout.
  WUU_BROWSER_APP          Built BrowserOS/Wuu Browser .app path on macOS.
  WUU_BROWSER_EXTENSION    Wuu/BrowserOS agent extension directory.
  WUU_BROWSER_SERVER_RESOURCES
                           BrowserOS server resources directory. Defaults to
                           the bundled resources inside WUU_BROWSER_APP.
  WUU_BROWSER_PROFILE_DIR  Browser profile directory.
  WUU_BROWSER_START_URL    Initial URL. Defaults to the Wuu workbench tab.
  WUU_BIN                  Wuu native runtime binary used by the browser tab.
                           Defaults to a dev build under browser/.cache.
  WUU_SOURCE_ROOT          Wuu source tree used by the browser-hosted app-server.
                           Defaults to this repository root.

Port overrides:
  WUU_BROWSER_CDP_PORT        Defaults to 9100.
  WUU_BROWSER_SERVER_PORT     Defaults to 9105.
  WUU_BROWSER_PROXY_PORT      Defaults to 9205.
  WUU_BROWSER_EXTENSION_PORT  Defaults to 9305.
USAGE
}

dry_run=false
profile_dir="${WUU_BROWSER_PROFILE_DIR:-}"
default_extension_id="bflpfmnmnokmjhmgnolecpppdbdophmk"
start_url="${WUU_BROWSER_START_URL:-chrome-extension://${default_extension_id}/app.html#/home}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run)
      dry_run=true
      shift
      ;;
    --profile-dir)
      profile_dir="${2:-}"
      if [[ -z "${profile_dir}" ]]; then
        echo "--profile-dir requires a directory" >&2
        exit 2
      fi
      shift 2
      ;;
    --url)
      start_url="${2:-}"
      if [[ -z "${start_url}" ]]; then
        echo "--url requires a URL" >&2
        exit 2
      fi
      shift 2
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

browseros_repo="${WUU_BROWSEROS_REPO:-${repo_root}/.worktrees/browseros}"
chromium_src="${WUU_CHROMIUM_SRC:-${repo_root}/.worktrees/chromium/src}"

if [[ ! -d "${browseros_repo}" && -d "${HOME}/wuu-browseros" ]]; then
  browseros_repo="${HOME}/wuu-browseros"
fi

if [[ ! -d "${chromium_src}" && -d "${HOME}/browseros-chromium/src" ]]; then
  chromium_src="${HOME}/browseros-chromium/src"
fi

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "launch-dev currently supports macOS BrowserOS Dev app launches only." >&2
  echo "Windows/Linux launch scripts should be added with the packaging work." >&2
  exit 2
fi

app_path="${WUU_BROWSER_APP:-}"
if [[ -z "${app_path}" ]]; then
  for candidate in \
    "${chromium_src}/out/Default_arm64/BrowserOS Dev.app" \
    "${chromium_src}/out/Default/BrowserOS Dev.app" \
    "${chromium_src}/out/Default_x64/BrowserOS Dev.app"; do
    if [[ -d "${candidate}" ]]; then
      app_path="${candidate}"
      break
    fi
  done
fi

extension_dir="${WUU_BROWSER_EXTENSION:-}"
if [[ -z "${extension_dir}" ]]; then
  for candidate in \
    "${repo_root}/browser/agent/apps/agent/dist/chrome-mv3-dev" \
    "${browseros_repo}/packages/browseros-agent/apps/agent/dist/chrome-mv3-dev"; do
    if [[ -d "${candidate}" ]]; then
      extension_dir="${candidate}"
      break
    fi
  done
fi

if [[ -z "${app_path}" || ! -d "${app_path}" ]]; then
  echo "Browser app not found." >&2
  echo "Set WUU_BROWSER_APP or build BrowserOS Dev under the Chromium checkout." >&2
  exit 1
fi

if [[ ! -d "${extension_dir}" ]]; then
  echo "Wuu/BrowserOS agent extension not found." >&2
  echo "Build browser/agent/apps/agent or set WUU_BROWSER_EXTENSION." >&2
  exit 1
fi

server_resources_dir="${WUU_BROWSER_SERVER_RESOURCES:-${app_path}/Contents/Resources/BrowserOSServer/default/resources}"
if [[ ! -x "${server_resources_dir}/bin/browseros_server" ]]; then
  echo "BrowserOS server binary not found or not executable: ${server_resources_dir}/bin/browseros_server" >&2
  echo "Build browser/agent server resources or set WUU_BROWSER_SERVER_RESOURCES." >&2
  exit 1
fi

if [[ -z "${profile_dir}" ]]; then
  profile_dir="$(mktemp -d -t wuu-browser-dev.XXXXXX)"
fi

cdp_port="${WUU_BROWSER_CDP_PORT:-9100}"
server_port="${WUU_BROWSER_SERVER_PORT:-9105}"
proxy_port="${WUU_BROWSER_PROXY_PORT:-9205}"
extension_port="${WUU_BROWSER_EXTENSION_PORT:-9305}"

export WUU_SOURCE_ROOT="${WUU_SOURCE_ROOT:-${repo_root}}"
wuu_bin="${WUU_BIN:-${repo_root}/browser/.cache/wuu-dev}"

mkdir -p "${repo_root}/browser/.cache"

if [[ -z "${WUU_BIN:-}" && "${dry_run}" != "true" ]]; then
  echo "Building Wuu native runtime for browser dev..."
  (cd "${repo_root}" && go build -o "${wuu_bin}" ./cmd/wuu)
fi

if [[ "${dry_run}" != "true" && ! -x "${wuu_bin}" ]]; then
  echo "Wuu native runtime binary not found or not executable: ${wuu_bin}" >&2
  exit 1
fi

export WUU_BIN="${wuu_bin}"
printf '%s\n' "${profile_dir}" > "${repo_root}/browser/.cache/last-profile"

args=(
  "--no-first-run"
  "--no-default-browser-check"
  "--use-mock-keychain"
  "--show-component-extension-options"
  "--disable-browseros-extensions"
  "--disable-browseros-server-updater"
  "--browseros-server-resources-dir=${server_resources_dir}"
  "--remote-debugging-port=${cdp_port}"
  "--browseros-cdp-port=${cdp_port}"
  "--browseros-server-port=${server_port}"
  "--browseros-proxy-port=${proxy_port}"
  "--browseros-extension-port=${extension_port}"
  "--user-data-dir=${profile_dir}"
  "--load-extension=${extension_dir}"
  "${start_url}"
)

cmd=(open -na "${app_path}" --args "${args[@]}")

echo "Wuu Browser Dev launch"
echo "  app:       ${app_path}"
echo "  extension: ${extension_dir}"
echo "  server:    ${server_resources_dir}"
echo "  profile:   ${profile_dir}"
echo "  start URL: ${start_url}"
echo "  Wuu source:${WUU_SOURCE_ROOT}"
echo "  Wuu binary:${WUU_BIN}"
echo "  CDP:       http://127.0.0.1:${cdp_port}"
echo "  API:       http://127.0.0.1:${server_port}"

if [[ "${dry_run}" == "true" ]]; then
  printf 'dry-run command:'
  printf ' %q' "${cmd[@]}"
  printf '\n'
  exit 0
fi

"${cmd[@]}"

echo "Launched. Inspect with: make browser-status"
