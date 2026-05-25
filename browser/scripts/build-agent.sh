#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: browser/scripts/build-agent.sh [options]

Build the repository-owned Wuu Browser agent/workbench assets.

Options:
  --extension          Build the browser extension workbench dist.
  --server             Build local BrowserOS server resources.
  --all                Build both extension and server resources.
  --server-target ID   Server target id. Defaults to the current host target.
                       Examples: darwin-arm64, darwin-x64, linux-x64, windows-x64.
  --install            Run bun install --frozen-lockfile before building.
  --dry-run            Print commands without executing them.

Environment:
  WUU_BROWSER_SERVER_TARGET  Server target override.
USAGE
}

dry_run=false
build_extension=false
build_server=false
install_deps=false

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"
agent_dir="${repo_root}/browser/agent"
server_target="${WUU_BROWSER_SERVER_TARGET:-}"

host_server_target() {
  local os
  local arch
  case "$(uname -s)" in
    Darwin) os="darwin" ;;
    Linux) os="linux" ;;
    MINGW*|MSYS*|CYGWIN*) os="windows" ;;
    *)
      echo "Unsupported host OS: $(uname -s)" >&2
      exit 2
      ;;
  esac

  case "$(uname -m)" in
    arm64|aarch64) arch="arm64" ;;
    x86_64|amd64) arch="x64" ;;
    *)
      echo "Unsupported host architecture: $(uname -m)" >&2
      exit 2
      ;;
  esac

  printf '%s-%s' "${os}" "${arch}"
}

print_cmd() {
  printf '+'
  printf ' %q' "$@"
  printf '\n'
}

run_in() {
  local cwd="$1"
  shift
  printf '+ cd %q &&' "${cwd}"
  printf ' %q' "$@"
  printf '\n'
  if [[ "${dry_run}" != "true" ]]; then
    (cd "${cwd}" && "$@")
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --extension)
      build_extension=true
      shift
      ;;
    --server)
      build_server=true
      shift
      ;;
    --all)
      build_extension=true
      build_server=true
      shift
      ;;
    --server-target)
      server_target="${2:-}"
      if [[ -z "${server_target}" ]]; then
        echo "--server-target requires a target id" >&2
        exit 2
      fi
      shift 2
      ;;
    --install)
      install_deps=true
      shift
      ;;
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

if [[ "${build_extension}" != "true" && "${build_server}" != "true" ]]; then
  build_extension=true
fi

if [[ -z "${server_target}" ]]; then
  server_target="$(host_server_target)"
fi

case "${server_target}" in
  darwin-arm64|darwin-x64|linux-arm64|linux-x64|windows-x64|all) ;;
  *)
    echo "Unsupported --server-target value: ${server_target}" >&2
    exit 2
    ;;
esac

if [[ "${dry_run}" != "true" && ! -d "${agent_dir}" ]]; then
  echo "Browser agent directory not found: ${agent_dir}" >&2
  exit 1
fi

if [[ "${dry_run}" != "true" ]] && ! command -v bun >/dev/null 2>&1; then
  echo "bun is required to build Wuu Browser agent assets." >&2
  exit 2
fi

echo "Wuu Browser agent asset build"
echo "  agent dir:     ${agent_dir}"
echo "  extension:     ${build_extension}"
echo "  server:        ${build_server}"
echo "  server target: ${server_target}"
echo "  install deps:  ${install_deps}"
echo "  mode:          $([[ "${dry_run}" == "true" ]] && echo dry-run || echo execute)"

if [[ "${install_deps}" == "true" ]]; then
  run_in "${agent_dir}" bun install --frozen-lockfile
elif [[ "${dry_run}" != "true" && ! -d "${agent_dir}/node_modules" ]]; then
  echo "browser/agent/node_modules is missing." >&2
  echo "Run make browser-build-agent ARGS=\"--install --extension\" first." >&2
  exit 1
fi

if [[ "${build_extension}" == "true" ]]; then
  run_in "${agent_dir}" bun run build:agent:dev
  echo "Extension output: ${agent_dir}/apps/agent/dist/chrome-mv3-dev"
fi

if [[ "${build_server}" == "true" ]]; then
  run_in "${agent_dir}" bun scripts/build/server.ts --target="${server_target}" --no-upload --ci
  echo "Server resources output: ${agent_dir}/dist/prod/server"
fi

echo "Wuu Browser agent asset build finished."
