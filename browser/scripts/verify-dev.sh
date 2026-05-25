#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: browser/scripts/verify-dev.sh [--require-wuu-tab] [--require-wuu-runtime] [--require-no-vm-agents]

Verify a running Wuu Browser development instance.

Checks:
  1. Chrome DevTools Protocol endpoint responds.
  2. BrowserOS server health endpoint responds.
  3. DevTools page list is readable.
  4. Optionally, the Wuu workbench extension route is open.
  5. Optionally, the Wuu workbench can call the real native runtime.
  6. Optionally, no BrowserOS VM/OpenClaw process is running.

Environment overrides:
  WUU_BROWSER_CDP_PORT     Defaults to 9100.
  WUU_BROWSER_SERVER_PORT  Defaults to 9105.
USAGE
}

require_wuu_tab=false
require_wuu_runtime=false
require_no_vm_agents=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --require-wuu-tab)
      require_wuu_tab=true
      shift
      ;;
    --require-wuu-runtime)
      require_wuu_tab=true
      require_wuu_runtime=true
      shift
      ;;
    --require-no-vm-agents)
      require_no_vm_agents=true
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

if [[ "${require_wuu_runtime}" == "true" ]]; then
  if ! command -v bun >/dev/null 2>&1; then
    echo "bun is required for Wuu runtime verification" >&2
    exit 2
  fi

  runtime_json="$(CDP_BASE="${cdp_base}" bun - <<'JS'
const cdpBase = process.env.CDP_BASE

async function fetchJson(url) {
  const response = await fetch(url)
  if (!response.ok) {
    throw new Error(`${url} failed with ${response.status}`)
  }
  return response.json()
}

const pages = await fetchJson(`${cdpBase}/json/list`)
const page = pages.find((candidate) => {
  return candidate.type === 'page' && /app\.html#\/home|chrome:\/\/browseros\/wuu/.test(candidate.url)
})
if (!page) {
  throw new Error('Wuu workbench page was not found')
}

const ws = new WebSocket(page.webSocketDebuggerUrl)
let nextId = 0
const pending = new Map()

ws.onmessage = (event) => {
  const message = JSON.parse(event.data)
  if (!message.id || !pending.has(message.id)) return
  const { resolve, reject } = pending.get(message.id)
  pending.delete(message.id)
  if (message.error) {
    reject(new Error(JSON.stringify(message.error)))
    return
  }
  resolve(message.result)
}

await new Promise((resolve, reject) => {
  ws.onopen = resolve
  ws.onerror = reject
})

function send(method, params = {}) {
  const id = ++nextId
  ws.send(JSON.stringify({ id, method, params }))
  return new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject })
  })
}

async function evaluate(expression) {
  const result = await send('Runtime.evaluate', {
    expression,
    awaitPromise: true,
    returnByValue: true,
    timeout: 60000,
  })
  if (result.exceptionDetails) {
    throw new Error(JSON.stringify(result.exceptionDetails))
  }
  if (result.result?.subtype === 'error') {
    throw new Error(result.result.description || 'Runtime evaluation failed')
  }
  return result.result?.value
}

const result = await evaluate(`(async () => {
  const deadline = Date.now() + 15000;
  while (!window.wuu && Date.now() < deadline) {
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  if (!window.wuu) throw new Error('window.wuu is not installed');
  const project = await window.wuu.selectNoProject(false);
  const initialized = await window.wuu.initialize();
  const threadResult = await window.wuu.startThread();
  const terminal = await window.wuu.startTerminalSession({ cols: 80, rows: 24 });
  await window.wuu.stopTerminalSession(terminal.id);
  return {
    cwd: project.active_context?.cwd,
    workspaceRoot: initialized.workspace_root,
    provider: initialized.provider,
    model: initialized.model,
    threadId: threadResult.thread?.id,
    terminalId: terminal.id,
  };
})()`)

if (!result?.cwd || !result?.workspaceRoot || !result?.threadId || !result?.terminalId) {
  throw new Error(`Wuu runtime returned incomplete data: ${JSON.stringify(result)}`)
}

console.log(JSON.stringify(result))
ws.close()
JS
)" || {
    echo "Wuu native runtime verification failed." >&2
    exit 1
  }

  if ! printf '%s' "${runtime_json}" | grep -q '"threadId"'; then
    echo "Wuu native runtime verification returned unexpected output: ${runtime_json}" >&2
    exit 1
  fi

  echo "  Wuu runtime: ${runtime_json}"
fi

if [[ "${require_no_vm_agents}" == "true" ]]; then
  vm_matches="$(ps -axo pid=,command= | grep -E 'browseros-vm|openclaw|limactl' | grep -v -E 'grep|verify-dev.sh' || true)"
  if [[ -n "${vm_matches}" ]]; then
    echo "VM-backed agent processes are running unexpectedly:" >&2
    printf '%s\n' "${vm_matches}" >&2
    exit 1
  fi
  echo "  VM-backed agents: not running"
fi

if printf '%s' "${version_json}" | grep -q '"Browser"'; then
  browser_name="$(printf '%s' "${version_json}" | sed -n 's/.*"Browser"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
  if [[ -n "${browser_name}" ]]; then
    echo "  browser: ${browser_name}"
  fi
fi
