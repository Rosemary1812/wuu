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
  win.webContents.on("console-message", (_event, level, message, line, sourceId) => {
    if (level >= 2) {
      console.error(`renderer console: ${message} (${sourceId}:${line})`);
    }
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
    items: [userItem],
    items_view: "full",
    status: "in_progress",
    started_at: now
  };
  const longTail = Array.from({ length: 120 }, (_value, index) => `streaming-word-${index}`).join(" ");
  const fullText = [
    "# Streaming markdown",
    "",
    "Intro with **bold text** and [a link](https://example.com).",
    "",
    "- first item",
    "- [x] completed item",
    "",
    "| Name | Value |",
    "| --- | --- |",
    "| alpha | beta |",
    "",
    "```ts",
    "const answer = 42;",
    "```",
    "",
    longTail
  ].join("\n");

  emitNotification(win, "thread/resumed", { thread });
  emitNotification(win, "turn/started", { turn });
  emitNotification(win, "item/started", { turn_id: turn.id, item: agentItem });
  emitNotification(win, "item/agentMessage/delta", { turn_id: turn.id, item_id: agentItem.id, delta: fullText });

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

  const debug = await waitFor(
    win,
    () => {
      const button = document.querySelector(".run-debug-button");
      if (!button) {
        return null;
      }
      if (!document.querySelector(".run-debug-panel")) {
        button.click();
      }
      const panel = document.querySelector(".run-debug-panel");
      const text = panel?.textContent ?? "";
      return text.includes("reply/first-delta") && text.includes("正在生成回复")
        ? {
            phase: panel?.querySelector(".run-debug-phase")?.textContent ?? "",
            text
          }
        : null;
    },
    3000,
    { fullLength: fullText.length }
  );
  assert.equal(debug.phase, "正在生成回复", "Debug panel should expose the live response phase.");
  assert.match(debug.text, /reply\/first-delta/, "Debug panel should include the first reply delta event.");

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

  const final = await waitFor(
    win,
    () => {
      const snapshot = streamingSnapshot();
      return snapshot.hasStreaming && snapshot.text.includes("streaming-word-119") ? snapshot : null;
    },
    16000,
    { fullLength: fullText.length }
  );
  assert.equal(final.hasStaticFallback, false, "Assistant content should remain on StreamingMarkdown after settling.");
  assert.ok(final.text.includes("streaming-word-119"), "StreamingMarkdown should eventually show the complete response.");
  assert.match(final.streamState, /^(settling|settled)$/, "Final text should be in a completion visual state.");

  const settled = await waitFor(
    win,
    () => {
      const snapshot = streamingSnapshot();
      return snapshot.streamState === "settled" ? snapshot : null;
    },
    3000,
    { fullLength: fullText.length }
  );
  assert.equal(settled.hasStreaming, true, "Assistant content should still use StreamingMarkdown after completion.");
  assert.equal(settled.hasStaticFallback, false, "No static fallback should be rendered after stream completion.");
  assert.equal(settled.animatedWords, 0, "Settled content should not keep running word animations.");
  assert.equal(settled.heading, "Streaming markdown", "Final assistant content should render Markdown headings.");
  assert.equal(settled.bold, "bold text", "Final assistant content should render Markdown emphasis.");
  assert.equal(settled.linkHref, "https://example.com/", "Final assistant content should render safe Markdown links.");
  assert.equal(settled.listItems, 2, "Final assistant content should render Markdown lists.");
  assert.equal(settled.checkedTasks, 1, "Final assistant content should render GFM task list items.");
  assert.equal(settled.hasTable, true, "Final assistant content should render GFM tables.");
  assert.equal(settled.code, "const answer = 42;", "Final assistant content should render fenced code blocks.");

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
      const animatedWords = streaming
        ? Array.from(streaming.querySelectorAll(".stream-word")).filter(
            (node) => getComputedStyle(node).animationName !== "none"
          ).length
        : 0;
      return {
        hasStreaming: Boolean(streaming),
        hasStaticFallback: Boolean(staticFallback),
        streamState: streaming?.getAttribute("data-stream-state") ?? null,
        animatedWords,
        text,
        textLength: text.length,
        heading: streaming?.querySelector("h1")?.textContent ?? "",
        bold: streaming?.querySelector("strong")?.textContent ?? "",
        linkHref: streaming?.querySelector("a")?.href ?? "",
        listItems: streaming?.querySelectorAll("li").length ?? 0,
        checkedTasks: streaming?.querySelectorAll('input[type="checkbox"]:checked').length ?? 0,
        hasTable: Boolean(streaming?.querySelector("table")),
        code: streaming?.querySelector("pre code")?.textContent?.trim() ?? ""
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
