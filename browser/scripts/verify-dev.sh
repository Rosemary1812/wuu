#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: browser/scripts/verify-dev.sh [--require-wuu-tab] [--require-wuu-runtime] [--require-project-folder-picker] [--require-no-vm-agents] [--require-browser-bridge]

Verify a running Wuu Browser development instance.

Checks:
  1. Chrome DevTools Protocol endpoint responds.
  2. BrowserOS server health endpoint responds.
  3. DevTools page list is readable.
  4. Optionally, the Wuu workbench extension route is open.
  5. Optionally, the Wuu workbench can call the real native runtime.
  6. Optionally, the Wuu workbench project picker uses native browserOS.choosePath.
  7. Optionally, no BrowserOS VM/OpenClaw process is running.
  8. Optionally, the server Browser Bridge can read real tab metadata.

Environment overrides:
  WUU_BROWSER_CDP_PORT     Defaults to 9100.
  WUU_BROWSER_SERVER_PORT  Defaults to 9105.
USAGE
}

require_wuu_tab=false
require_wuu_runtime=false
require_project_folder_picker=false
require_no_vm_agents=false
require_browser_bridge=false

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
    --require-project-folder-picker)
      require_wuu_tab=true
      require_project_folder_picker=true
      shift
      ;;
    --require-no-vm-agents)
      require_no_vm_agents=true
      shift
      ;;
    --require-browser-bridge)
      require_browser_bridge=true
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
if ! printf '%s' "${version_json}" | grep -q '"webSocketDebuggerUrl"'; then
  echo "CDP endpoint returned an unexpected response. Is port ${cdp_port} already in use?" >&2
  echo "Response: ${version_json}" >&2
  exit 1
fi
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
page_count="$( (printf '%s' "${pages_json}" | grep -o '"id"' || true) | wc -l | tr -d ' ')"
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

if [[ "${require_project_folder_picker}" == "true" ]]; then
  if ! command -v bun >/dev/null 2>&1; then
    echo "bun is required for Wuu project folder picker verification" >&2
    exit 2
  fi

  picker_dir="$(mktemp -d -t wuu-picker-verify.XXXXXX)"
  picker_json="$(CDP_BASE="${cdp_base}" WUU_PICKER_VERIFY_DIR="${picker_dir}" bun - <<'JS'
const cdpBase = process.env.CDP_BASE
const pickerDir = process.env.WUU_PICKER_VERIFY_DIR
const pickerName = pickerDir.split(/[\\/]/).filter(Boolean).at(-1) ?? pickerDir

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
  const projectsKey = 'wuu.browseros.projects';
  const activeContextKey = 'wuu.browseros.activeContext';
  const previousProjects = window.localStorage.getItem(projectsKey);
  const previousActiveContext = window.localStorage.getItem(activeContextKey);

  if (!window.wuu) throw new Error('window.wuu is not installed');
  if (!chrome?.browserOS || typeof chrome.browserOS.choosePath !== 'function') {
    throw new Error('chrome.browserOS.choosePath is not installed');
  }

  const originalChoosePath = chrome.browserOS.choosePath;
  const calls = [];
  chrome.browserOS.choosePath = (options, callback) => {
    calls.push(options);
    callback({ path: ${JSON.stringify(pickerDir)}, name: ${JSON.stringify(pickerName)} });
  };

  try {
    const projects = await window.wuu.chooseProjectFolder();
    const active = projects.active_context;
    const selected = projects.projects.find((project) => project.path === ${JSON.stringify(pickerDir)});
    if (!calls.length) throw new Error('chooseProjectFolder did not call chrome.browserOS.choosePath');
    if (calls[0]?.type !== 'folder') throw new Error('choosePath was not called in folder mode');
    if (!selected) throw new Error('selected folder was not added to the project list');
    if (active?.kind !== 'project' || active.cwd !== ${JSON.stringify(pickerDir)}) {
      throw new Error('selected folder did not become the active project');
    }
    return {
      choosePathAvailable: true,
      call: calls[0],
      active,
      projectName: selected.name,
    };
  } finally {
    chrome.browserOS.choosePath = originalChoosePath;
    if (previousProjects === null) window.localStorage.removeItem(projectsKey);
    else window.localStorage.setItem(projectsKey, previousProjects);
    if (previousActiveContext === null) window.localStorage.removeItem(activeContextKey);
    else window.localStorage.setItem(activeContextKey, previousActiveContext);
    setTimeout(() => window.location.reload(), 0);
  }
})()`)

if (!result?.choosePathAvailable || result.call?.type !== 'folder') {
  throw new Error(`Project folder picker returned unexpected data: ${JSON.stringify(result)}`)
}

console.log(JSON.stringify(result))
ws.close()
JS
)" || {
    rm -rf "${picker_dir}"
    echo "Wuu project folder picker verification failed." >&2
    exit 1
  }
  rm -rf "${picker_dir}"

  echo "  Wuu project folder picker: ${picker_json}"

  if [[ "${require_browser_bridge}" == "true" ]]; then
    for _ in {1..50}; do
      pages_json="$(fetch "${cdp_base}/json/list")" || true
      if printf '%s' "${pages_json}" | grep -Eq 'app\.html#/home|chrome://browseros/wuu'; then
        break
      fi
      sleep 0.2
    done

    if ! printf '%s' "${pages_json}" | grep -Eq 'app\.html#/home|chrome://browseros/wuu'; then
      echo "Wuu workbench tab did not reload after project folder picker verification." >&2
      exit 1
    fi
  fi
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

if [[ "${require_browser_bridge}" == "true" ]]; then
  if ! command -v bun >/dev/null 2>&1; then
    echo "bun is required for Browser Bridge verification" >&2
    exit 2
  fi

  bridge_json="$(SERVER_BASE="${server_base}" REQUIRE_WUU_TAB="${require_wuu_tab}" bun - <<'JS'
const serverBase = process.env.SERVER_BASE
const requireWuuTab = process.env.REQUIRE_WUU_TAB === 'true'
const origin = serverBase

async function fetchJson(path, init = {}) {
  const response = await fetch(`${serverBase}${path}`, {
    ...init,
    headers: {
      Origin: origin,
      ...(init.body ? { 'content-type': 'application/json' } : {}),
      ...(init.headers ?? {}),
    },
  })
  if (!response.ok) {
    throw new Error(`${path} failed with ${response.status}: ${await response.text()}`)
  }
  return response.json()
}

const initial = await fetchJson('/browser-bridge/tabs')
if (!Array.isArray(initial.tabs)) {
  throw new Error(`Browser Bridge returned no tabs array: ${JSON.stringify(initial)}`)
}

if (
  requireWuuTab &&
  !initial.tabs.some((tab) => /chrome:\/\/browseros\/wuu|app\.html#\/home/.test(tab.url))
) {
  throw new Error(`Browser Bridge did not report the Wuu workbench tab: ${JSON.stringify(initial)}`)
}

const createUrl = 'data:text/html,<title>Wuu%20Bridge%20Verify</title><main>Wuu%20Browser%20Bridge%20created%20this%20tab</main>'
const created = await fetchJson('/browser-bridge/tabs', {
  method: 'POST',
  body: JSON.stringify({ url: createUrl, background: true }),
})
if (!created.tab?.targetId) {
  throw new Error(`Browser Bridge did not create a tab target: ${JSON.stringify(created)}`)
}

const navigateUrl = 'data:text/html,<title>Wuu%20Bridge%20Navigate%20Verify</title><main>Wuu%20Browser%20Bridge%20navigation%20works</main>'
const targetPath = `/browser-bridge/tabs/${encodeURIComponent(created.tab.targetId)}`
const navigated = await fetchJson(`${targetPath}/navigate`, {
  method: 'POST',
  body: JSON.stringify({ url: navigateUrl }),
})
if (!navigated.tab?.url?.startsWith('data:text/html')) {
  throw new Error(`Browser Bridge navigation returned unexpected tab: ${JSON.stringify(navigated)}`)
}

const screenshot = await fetchJson(`${targetPath}/screenshot?format=png`)
if (screenshot.mimeType !== 'image/png' || typeof screenshot.data !== 'string' || screenshot.data.length < 100) {
  throw new Error(`Browser Bridge screenshot returned unexpected data: ${JSON.stringify({
    mimeType: screenshot.mimeType,
    dataLength: typeof screenshot.data === 'string' ? screenshot.data.length : null,
  })}`)
}

const interactionHtml = encodeURIComponent(`
<!doctype html>
<meta charset="utf-8">
<title>Wuu Bridge Interaction Verify</title>
<style>
  body { margin: 0; font: 16px system-ui, sans-serif; }
  input { position: absolute; left: 40px; top: 40px; width: 260px; height: 32px; font: inherit; }
  button { position: absolute; left: 40px; top: 90px; width: 160px; height: 36px; font: inherit; }
  #status { position: absolute; left: 40px; top: 145px; }
  #spacer { height: 1800px; }
</style>
<input id="q" value="">
<button id="apply" onclick="document.getElementById('status').textContent = 'typed:' + document.getElementById('q').value">Apply</button>
<main id="status">ready</main>
<div id="spacer"></div>
<script>
  window.addEventListener('scroll', () => {
    const status = document.getElementById('status')
    const base = status.textContent.replace(/ scroll:.*/, '')
    status.textContent = base + ' scroll:' + Math.round(window.scrollY)
  })
</script>
`)
const interaction = await fetchJson('/browser-bridge/tabs', {
  method: 'POST',
  body: JSON.stringify({
    url: `data:text/html;charset=utf-8,${interactionHtml}`,
    background: false,
  }),
})
if (!interaction.tab?.targetId) {
  throw new Error(`Browser Bridge did not create interaction tab: ${JSON.stringify(interaction)}`)
}

const interactionPath = `/browser-bridge/tabs/${encodeURIComponent(interaction.tab.targetId)}`
await fetchJson(`${interactionPath}/type`, {
  method: 'POST',
  body: JSON.stringify({ x: 80, y: 56, text: 'typed-by-bridge', clear: true }),
})
await fetchJson(`${interactionPath}/click`, {
  method: 'POST',
  body: JSON.stringify({ x: 80, y: 108 }),
})
const typedContent = await fetchJson(`${interactionPath}/content?selector=%23status`)
if (!typedContent.text?.includes('typed:typed-by-bridge')) {
  throw new Error(`Browser Bridge type/click did not update page content: ${JSON.stringify(typedContent)}`)
}

await fetchJson(`${interactionPath}/scroll`, {
  method: 'POST',
  body: JSON.stringify({ direction: 'down', amount: 5 }),
})
await new Promise((resolve) => setTimeout(resolve, 200))
const scrolledContent = await fetchJson(`${interactionPath}/content?selector=%23status`)
if (!/scroll:[1-9][0-9]*/.test(scrolledContent.text ?? '')) {
  throw new Error(`Browser Bridge scroll did not update page scroll state: ${JSON.stringify(scrolledContent)}`)
}

console.log(JSON.stringify({
  initialTabs: initial.tabs.length,
  createdTargetId: created.tab.targetId,
  interactionTargetId: interaction.tab.targetId,
  screenshotChars: screenshot.data.length,
}))
JS
)" || {
    echo "Browser Bridge verification failed." >&2
    exit 1
  }

  echo "  Browser Bridge: ${bridge_json}"
fi

if printf '%s' "${version_json}" | grep -q '"Browser"'; then
  browser_name="$(printf '%s' "${version_json}" | sed -n 's/.*"Browser"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
  if [[ -n "${browser_name}" ]]; then
    echo "  browser: ${browser_name}"
  fi
fi
