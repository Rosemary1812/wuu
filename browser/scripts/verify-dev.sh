#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: browser/scripts/verify-dev.sh [--require-wuu-tab]

Verify a running Wuu Browser development instance.

Checks:
  1. Chrome DevTools Protocol endpoint responds.
  2. BrowserOS server health endpoint responds.
  3. DevTools page list is readable.
  4. Optionally, the Wuu workbench extension route is open.

Environment overrides:
  WUU_BROWSER_CDP_PORT     Defaults to 9100.
  WUU_BROWSER_SERVER_PORT  Defaults to 9105.
USAGE
}

require_wuu_tab=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --require-wuu-tab)
      require_wuu_tab=true
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

cdp_port="${WUU_BROWSER_CDP_PORT:-9100}"
server_port="${WUU_BROWSER_SERVER_PORT:-9105}"
cdp_base="http://127.0.0.1:${cdp_port}"
server_base="http://127.0.0.1:${server_port}"

need_curl() {
  if ! command -v curl >/dev/null 2>&1; then
    echo "curl is required for dev browser verification" >&2
    exit 2
  fi
}

fetch() {
  local url="$1"
  curl -fsS --max-time 3 "${url}"
}

need_curl

echo "Wuu Browser Dev verification"
echo "  CDP:    ${cdp_base}"
echo "  server: ${server_base}"

version_json="$(fetch "${cdp_base}/json/version")" || {
  echo "CDP endpoint is not reachable. Is Wuu Browser Dev running?" >&2
  exit 1
}
echo "  CDP version: ok"

health_json="$(fetch "${server_base}/health")" || {
  echo "BrowserOS server health endpoint is not reachable." >&2
  exit 1
}
echo "  server health: ${health_json}"

pages_json="$(fetch "${cdp_base}/json/list")" || {
  echo "CDP page list is not reachable." >&2
  exit 1
}
page_count="$(printf '%s' "${pages_json}" | grep -o '"id"' | wc -l | tr -d ' ')"
echo "  CDP pages: ${page_count}"

if [[ "${require_wuu_tab}" == "true" ]]; then
  if printf '%s' "${pages_json}" | grep -Eq 'app\.html#/home|chrome://browseros/wuu'; then
    echo "  Wuu tab: present"
  else
    echo "Wuu workbench tab was not found in CDP page list." >&2
    exit 1
  fi
fi

if printf '%s' "${version_json}" | grep -q '"Browser"'; then
  browser_name="$(printf '%s' "${version_json}" | sed -n 's/.*"Browser"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
  if [[ -n "${browser_name}" ]]; then
    echo "  browser: ${browser_name}"
  fi
fi
