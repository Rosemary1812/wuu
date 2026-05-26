#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: browser/scripts/launch-dev.sh [--dry-run] [--profile-dir DIR] [--url URL] [--no-cleanup-existing]

Launch the local Wuu Browser development build.

Environment overrides:
  WUU_BROWSEROS_REPO       BrowserOS reference checkout.
  WUU_CHROMIUM_SRC         Chromium source checkout.
  WUU_BROWSER_APP          Built or staged BrowserOS/Wuu Browser .app path on macOS.
  WUU_BROWSER_EXTENSION    Wuu/BrowserOS agent extension directory.
  WUU_BROWSER_STAGE_EXTENSION
                           Build the repo-owned extension before launch.
                           Defaults to 1 when WUU_BROWSER_EXTENSION is unset
                           and the repo extension dist is missing.
  WUU_BROWSER_SERVER_RESOURCES
                           BrowserOS server resources directory. Defaults to
                           the bundled resources inside WUU_BROWSER_APP.
  WUU_BROWSER_SERVER_TARGET
                           Server resource target. Defaults to the host target.
  WUU_BROWSER_STAGE_SERVER_RESOURCES
                           Build and stage local server resources before launch.
                           Defaults to 1 when WUU_BROWSER_SERVER_RESOURCES is not set.
  WUU_BROWSER_PROFILE_DIR  Browser profile directory.
  WUU_BROWSER_START_URL    Initial URL. Defaults to the Wuu workbench tab.
  WUU_BIN                  Wuu native runtime binary used by the browser tab.
                           Defaults to a dev build under browser/.cache.
  WUU_SOURCE_ROOT          Wuu source tree used by the browser-hosted app-server.
                           Defaults to this repository root.
  WUU_BROWSER_CLEANUP_EXISTING
                           Stop existing Wuu Browser Dev/BrowserOS Dev launches
                           that use temporary wuu-browser-dev profiles before
                           starting a new one. Defaults to 1.

Port overrides:
  WUU_BROWSER_CDP_PORT        Defaults to 9100.
  WUU_BROWSER_SERVER_PORT     Defaults to 9105.
  WUU_BROWSER_PROXY_PORT      Defaults to 9205.
  WUU_BROWSER_EXTENSION_PORT  Defaults to 9305.
USAGE
}

dry_run=false
profile_dir="${WUU_BROWSER_PROFILE_DIR:-}"
start_url="${WUU_BROWSER_START_URL:-chrome://wuu}"
cleanup_existing="${WUU_BROWSER_CLEANUP_EXISTING:-1}"

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
    --no-cleanup-existing)
      cleanup_existing=0
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

browseros_repo="${WUU_BROWSEROS_REPO:-${repo_root}/.worktrees/browseros}"
chromium_src="${WUU_CHROMIUM_SRC:-${repo_root}/.worktrees/chromium/src}"

if [[ ! -d "${browseros_repo}" && -d "${HOME}/wuu-browseros" ]]; then
  browseros_repo="${HOME}/wuu-browseros"
fi

if [[ ! -d "${chromium_src}" && -d "${HOME}/browseros-chromium/src" ]]; then
  chromium_src="${HOME}/browseros-chromium/src"
fi

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "launch-dev currently supports macOS Wuu Browser Dev app launches only." >&2
  echo "Windows/Linux launch scripts should be added with the packaging work." >&2
  exit 2
fi

cleanup_matching_processes() {
  local pattern="$1"
  local pids
  pids="$(ps -axo pid=,args= | awk -v pattern="${pattern}" 'index($0, pattern) && $0 !~ /awk -v pattern/ {print $1}')"
  if [[ -z "${pids}" ]]; then
    return 0
  fi

  kill -TERM ${pids} 2>/dev/null || true
  sleep 1

  local remaining
  remaining="$(ps -p ${pids} -o pid= 2>/dev/null | tr '\n' ' ' || true)"
  if [[ -n "${remaining}" ]]; then
    kill -KILL ${remaining} 2>/dev/null || true
  fi
}

cleanup_wuu_dev_profile_processes() {
  local pids
  pids="$(ps -axo pid=,args= | awk '/--user-data-dir=[^ ]*wuu-browser-dev\.XXXXXX\./ && $0 !~ /awk/ {print $1}')"
  if [[ -z "${pids}" ]]; then
    return 0
  fi

  kill -TERM ${pids} 2>/dev/null || true
  sleep 1

  local remaining
  remaining="$(ps -p ${pids} -o pid= 2>/dev/null | tr '\n' ' ' || true)"
  if [[ -n "${remaining}" ]]; then
    kill -KILL ${remaining} 2>/dev/null || true
  fi
}

cleanup_dev_browsers() {
  cleanup_wuu_dev_profile_processes
  cleanup_matching_processes "${repo_root}/browser/out/Wuu Browser Dev.app"
  cleanup_matching_processes "${chromium_src}/out/Default_arm64/BrowserOS Dev.app"
  cleanup_matching_processes "${chromium_src}/out/Default/BrowserOS Dev.app"
  cleanup_matching_processes "${chromium_src}/out/Default_x64/BrowserOS Dev.app"

  find "${TMPDIR:-/tmp}" -maxdepth 1 -type d -name 'wuu-browser-dev.XXXXXX.*' -exec rm -rf {} + 2>/dev/null || true
}

host_server_target() {
  case "$(uname -m)" in
    arm64|aarch64) printf 'darwin-arm64' ;;
    x86_64|amd64) printf 'darwin-x64' ;;
    *)
      echo "Unsupported host architecture for server resources: $(uname -m)" >&2
      exit 2
      ;;
  esac
}

app_path="${WUU_BROWSER_APP:-}"
if [[ -z "${app_path}" ]]; then
  for candidate in \
    "${repo_root}/browser/out/Wuu Browser Dev.app" \
    "${chromium_src}/out/Default_arm64/BrowserOS Dev.app" \
    "${chromium_src}/out/Default/BrowserOS Dev.app" \
    "${chromium_src}/out/Default_x64/BrowserOS Dev.app" \
    "${chromium_src}/out/Default_arm64/Wuu Browser Dev.app" \
    "${chromium_src}/out/Default/Wuu Browser Dev.app" \
    "${chromium_src}/out/Default_x64/Wuu Browser Dev.app"; do
    if [[ -d "${candidate}" ]]; then
      app_path="${candidate}"
      break
    fi
  done
fi

repo_extension_dir="${repo_root}/browser/agent/apps/agent/dist/chrome-mv3-dev"
extension_dir="${WUU_BROWSER_EXTENSION:-}"
stage_extension="${WUU_BROWSER_STAGE_EXTENSION:-}"
if [[ -z "${stage_extension}" ]]; then
  if [[ -n "${WUU_BROWSER_EXTENSION:-}" || -d "${repo_extension_dir}" ]]; then
    stage_extension=0
  else
    stage_extension=1
  fi
fi

if [[ "${stage_extension}" == "1" ]]; then
  extension_build_args=(--extension)
  if [[ "${dry_run}" == "true" ]]; then
    extension_build_args+=(--dry-run)
  fi
  bash "${repo_root}/browser/scripts/build-agent.sh" "${extension_build_args[@]}"
  if [[ -z "${extension_dir}" ]]; then
    extension_dir="${repo_extension_dir}"
  fi
fi

if [[ -z "${extension_dir}" ]]; then
  for candidate in \
    "${repo_extension_dir}" \
    "${browseros_repo}/packages/browseros-agent/apps/agent/dist/chrome-mv3-dev"; do
    if [[ -d "${candidate}" ]]; then
      extension_dir="${candidate}"
      break
    fi
  done
fi

if [[ -z "${app_path}" || ! -d "${app_path}" ]]; then
  echo "Browser app not found." >&2
  echo "Run make browser-package-dev-macos, set WUU_BROWSER_APP, or build BrowserOS Dev under the Chromium checkout." >&2
  exit 1
fi

if [[ ! -d "${extension_dir}" && ! ( "${dry_run}" == "true" && "${stage_extension}" == "1" ) ]]; then
  echo "Wuu/BrowserOS agent extension not found." >&2
  echo "Run make browser-build-agent ARGS=\"--extension\" or set WUU_BROWSER_EXTENSION." >&2
  exit 1
fi

server_resources_dir="${WUU_BROWSER_SERVER_RESOURCES:-${app_path}/Contents/Resources/BrowserOSServer/default/resources}"
server_target="${WUU_BROWSER_SERVER_TARGET:-$(host_server_target)}"
stage_server_resources="${WUU_BROWSER_STAGE_SERVER_RESOURCES:-}"
if [[ -z "${stage_server_resources}" ]]; then
  if [[ -z "${WUU_BROWSER_SERVER_RESOURCES:-}" ]]; then
    stage_server_resources=1
  else
    stage_server_resources=0
  fi
fi

if [[ "${stage_server_resources}" == "1" ]]; then
  server_build_args=(--server --server-target "${server_target}")
  if [[ "${dry_run}" == "true" ]]; then
    server_build_args+=(--dry-run)
  fi

  bash "${repo_root}/browser/scripts/build-agent.sh" "${server_build_args[@]}"

  server_resource_archive="${repo_root}/browser/agent/dist/prod/server/browseros-server-resources-${server_target}.zip"
  if [[ "${dry_run}" == "true" ]]; then
    :
  elif [[ ! -f "${server_resource_archive}" ]]; then
    echo "BrowserOS server resource archive was not produced: ${server_resource_archive}" >&2
    exit 1
  else
    if ! command -v unzip >/dev/null 2>&1; then
      echo "unzip is required to stage BrowserOS server resources." >&2
      exit 2
    fi

    server_resources_root="$(dirname "${server_resources_dir}")"
    unzip -oq "${server_resource_archive}" -d "${server_resources_root}"
    chmod +x "${server_resources_dir}/bin/browseros_server"
  fi
fi

if [[ "${dry_run}" != "true" && ! -x "${server_resources_dir}/bin/browseros_server" ]]; then
  echo "BrowserOS server binary not found or not executable: ${server_resources_dir}/bin/browseros_server" >&2
  echo "Build browser/agent server resources or set WUU_BROWSER_SERVER_RESOURCES." >&2
  exit 1
fi

if [[ "${cleanup_existing}" == "1" ]]; then
  if [[ "${dry_run}" == "true" ]]; then
    echo "Would stop existing Wuu Browser Dev instances and remove stale wuu-browser-dev temp profiles."
  else
    cleanup_dev_browsers
  fi
fi

if [[ "${dry_run}" != "true" ]]; then
  bash "${repo_root}/browser/scripts/stage-wuu-mac-icons.sh" --sign "${app_path}"
fi

if [[ -z "${profile_dir}" ]]; then
  if [[ "${dry_run}" == "true" ]]; then
    tmp_root="${TMPDIR:-/tmp}"
    profile_dir="${tmp_root%/}/wuu-browser-dev.XXXXXX.dry-run"
  else
    profile_dir="$(mktemp -d -t wuu-browser-dev.XXXXXX)"
  fi
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
if [[ "${dry_run}" != "true" ]]; then
  printf '%s\n' "${profile_dir}" > "${repo_root}/browser/.cache/last-profile"
fi

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
echo "  stage ext: ${stage_extension}"
echo "  server:    ${server_resources_dir}"
echo "  srv target:${server_target}"
echo "  stage srv: ${stage_server_resources}"
echo "  profile:   ${profile_dir}"
echo "  cleanup:   ${cleanup_existing}"
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
