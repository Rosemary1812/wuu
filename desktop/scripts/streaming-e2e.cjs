const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { app, BrowserWindow } = require("electron");

const desktopRoot = path.resolve(__dirname, "..");
const repoRoot = path.resolve(desktopRoot, "..");
const rendererHtml = path.join(desktopRoot, "out", "renderer", "index.html");
const preload = path.join(__dirname, "streaming-e2e-preload.cjs");

process.env.WUU_STREAM_E2E_CWD = repoRoot;
app.commandLine.appendSwitch("disable-gpu");
app.commandLine.appendSwitch("disable-software-rasterizer");

app.whenReady().then(run).catch(fail);

async function run() {
  assert.ok(fs.existsSync(rendererHtml), "Renderer build is missing. Run npm run build first.");
  assert.ok(fs.existsSync(preload), "Streaming E2E preload is missing.");

  const win = new BrowserWindow({
    width: 1100,
    height: 820,
    show: false,
    webPreferences: {
      contextIsolation: true,
      nodeIntegration: false,
      preload,
      sandbox: false
    }
  });

  win.webContents.on("render-process-gone", (_event, details) => {
    fail(new Error(`Renderer process exited: ${details.reason}`));
  });

  await loadFile(win, rendererHtml);
  await waitFor(win, () => Boolean(document.querySelector(".conversation-pane")), 5000);

  const now = new Date().toISOString();
  const thread = {
    id: "thread-streaming-e2e",
    preview: "Streaming E2E",
    model_provider: "e2e",
    model: "mock-stream",
    cwd: repoRoot,
    status: "in_progress",
    created_at: now,
    updated_at: now,
    turns: []
  };
  const userItem = {
    id: "user-streaming-e2e",
    type: "user_message",
    status: "completed",
    text: "Write a long streaming response."
  };
  const agentItem = {
    id: "agent-streaming-e2e",
    type: "agent_message",
    status: "in_progress",
    text: ""
  };
  const turn = {
    id: "turn-streaming-e2e",
    items: [userItem, agentItem],
    items_view: "full",
    status: "in_progress",
    started_at: now
  };
  const fullText = Array.from({ length: 260 }, (_value, index) => `streaming-word-${index}`).join(" ");

  emitNotification(win, "thread/resumed", { thread });
  emitNotification(win, "turn/started", { turn });
  emitNotification(win, "item/started", { turn_id: turn.id, item: agentItem });
  emitNotification(win, "item/agentMessage/delta", { turn_id: turn.id, item_id: agentItem.id, delta: fullText });
  emitNotification(win, "item/completed", {
    turn_id: turn.id,
    item: { ...agentItem, status: "completed", text: fullText }
  });
  emitNotification(win, "turn/completed", {
    turn: {
      ...turn,
      status: "completed",
      completed_at: new Date().toISOString(),
      duration_ms: 100,
      items: [userItem, { ...agentItem, status: "completed", text: fullText }]
    }
  });

  await waitFor(win, () => Boolean(document.querySelector(".agent-text .streaming-markdown")), 3000);
  const partial = await waitFor(
    win,
    () => {
      const snapshot = streamingSnapshot();
      return snapshot.hasStreaming && snapshot.textLength > 0 && snapshot.textLength < window.__STREAMING_E2E_FULL_LENGTH__
        ? snapshot
        : null;
    },
    3000,
    { fullLength: fullText.length }
  );
  assert.equal(partial.hasStaticFallback, false, "Assistant content should not switch to static RichContent fallback.");

  const final = await waitFor(
    win,
    () => {
      const snapshot = streamingSnapshot();
      return snapshot.hasStreaming && snapshot.textLength >= window.__STREAMING_E2E_FULL_LENGTH__ ? snapshot : null;
    },
    16000,
    { fullLength: fullText.length }
  );
  assert.equal(final.hasStaticFallback, false, "Assistant content should remain on StreamingMarkdown after settling.");
  assert.equal(final.text, fullText, "StreamingMarkdown should eventually show the complete response.");

  await delay(120);
  const settled = await evaluate(win, () => streamingSnapshot(), { fullLength: fullText.length });
  assert.equal(settled.hasStreaming, true, "Assistant content should still use StreamingMarkdown after completion.");
  assert.equal(settled.hasStaticFallback, false, "No static fallback should be rendered after stream completion.");

  console.log("streaming markdown e2e passed");
  app.exit(0);
}

function emitNotification(win, method, params) {
  win.webContents.send("test:server-event", {
    kind: "notification",
    message: { method, params }
  });
}

function loadFile(win, file) {
  return new Promise((resolve, reject) => {
    win.webContents.once("did-fail-load", (_event, _code, description) => reject(new Error(description)));
    win.webContents.once("did-finish-load", () => resolve());
    win.loadFile(file);
  });
}

async function waitFor(win, predicate, timeoutMs, options = {}) {
  const started = Date.now();
  let lastValue;
  while (Date.now() - started < timeoutMs) {
    lastValue = await evaluate(win, predicate, options);
    if (lastValue) {
      return lastValue;
    }
    await delay(40);
  }
  throw new Error(`Timed out waiting for condition. Last value: ${JSON.stringify(lastValue)}`);
}

async function evaluate(win, fn, options = {}) {
  const source = `(() => {
    window.__STREAMING_E2E_FULL_LENGTH__ = ${Number(options.fullLength ?? 0)};
    window.streamingSnapshot = () => {
      const streaming = document.querySelector(".agent-text .streaming-markdown");
      const staticFallback = document.querySelector(".agent-text > .rich-content:not(.streaming-markdown)");
      const text = streaming?.textContent ?? "";
      return {
        hasStreaming: Boolean(streaming),
        hasStaticFallback: Boolean(staticFallback),
        text,
        textLength: text.length
      };
    };
    return (${fn.toString()})();
  })()`;
  return win.webContents.executeJavaScript(source, true);
}

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function fail(error) {
  console.error(error);
  app.exit(1);
}
