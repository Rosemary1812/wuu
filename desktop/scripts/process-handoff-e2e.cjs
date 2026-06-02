const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { app, BrowserWindow } = require("electron");

const desktopRoot = path.resolve(__dirname, "..");
const repoRoot = path.resolve(desktopRoot, "..");
const rendererHtml = path.join(desktopRoot, "out", "renderer", "index.html");
const preload = path.join(__dirname, "streaming-e2e-preload.cjs");
const evidenceDir = path.join(desktopRoot, "out", "e2e");

process.env.WUU_STREAM_E2E_CWD = repoRoot;
app.commandLine.appendSwitch("disable-gpu");
app.commandLine.appendSwitch("disable-software-rasterizer");

app.whenReady().then(run).catch(fail);

async function run() {
  assert.ok(fs.existsSync(rendererHtml), "Renderer build is missing. Run npm run build first.");
  assert.ok(fs.existsSync(preload), "Streaming E2E preload is missing.");
  fs.mkdirSync(evidenceDir, { recursive: true });

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
  await waitFor(win, () => Boolean(document.querySelector(".composer textarea")), 5000);

  const threadID = await startActiveThread(win);
  const now = new Date().toISOString();
  const userItem = {
    id: "user-process-handoff-e2e",
    type: "user_message",
    status: "completed",
    text: "Alternate text, tools, then final text."
  };
  const commentaryItem = {
    id: "agent-process-handoff-commentary-e2e",
    type: "agent_message",
    phase: "commentary",
    status: "completed",
    text: "第一段说明：我会先查看文件，然后再给出最终回答。"
  };
  const toolItem = {
    id: "tool-process-handoff-e2e",
    type: "tool_call",
    status: "completed",
    name: "list",
    arguments: '{"path":"desktop/src/renderer"}',
    result: "App.tsx\nStreamingMarkdown.tsx\nToolActivity.tsx"
  };
  const finalItem = {
    id: "agent-process-handoff-final-e2e",
    type: "agent_message",
    phase: "final_answer",
    status: "in_progress",
    text: ""
  };
  const turn = {
    id: "turn-process-handoff-e2e",
    items: [userItem],
    items_view: "full",
    status: "in_progress",
    started_at: now
  };
  const finalText = "第二段最终回答应该接替上一段过程文字，而不是跳到另一个视觉位置。";

  emitNotification(win, "turn/started", { thread_id: threadID, turn });
  emitNotification(win, "item/completed", {
    thread_id: threadID,
    turn_id: turn.id,
    item: commentaryItem
  });
  emitNotification(win, "item/completed", {
    thread_id: threadID,
    turn_id: turn.id,
    item: toolItem
  });

  const processRect = await waitFor(win, processCheckpointRect, 3000);
  assert.match(processRect.text, /第一段说明/, "Process checkpoint should be visible before final text starts.");
  await capture(win, "process-handoff-before.png");

  emitNotification(win, "item/started", {
    thread_id: threadID,
    turn_id: turn.id,
    item: finalItem
  });
  emitNotification(win, "item/agentMessage/delta", {
    thread_id: threadID,
    turn_id: turn.id,
    item_id: finalItem.id,
    delta: finalText
  });

  const finalRect = await waitFor(win, finalAnswerRect, 3000);
  assert.match(finalRect.text, /第二段/, "Final text should stream after process work.");
  assert.equal(finalRect.fontSize, processRect.fontSize, "Handoff text should keep font size stable.");
  assert.equal(finalRect.lineHeight, processRect.lineHeight, "Handoff text should keep line height stable.");
  assert.ok(
    Math.abs(finalRect.left - processRect.left) <= 2,
    `Final text should keep the same horizontal anchor. Before=${processRect.left}, after=${finalRect.left}`
  );
  assert.ok(
    Math.abs(finalRect.top - processRect.top) <= 8,
    `Final text should replace process text without a vertical jump. Before=${processRect.top}, after=${finalRect.top}`
  );
  await capture(win, "process-handoff-after.png");

  console.log("process handoff e2e passed");
  app.exit(0);
}

async function startActiveThread(win) {
  const started = await waitFor(
    win,
    () => {
      const textarea = document.querySelector(".composer textarea");
      if (!(textarea instanceof HTMLTextAreaElement)) {
        return false;
      }
      textarea.focus();
      const valueSetter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")?.set;
      valueSetter?.call(textarea, "Process handoff e2e seed.");
      textarea.dispatchEvent(new Event("input", { bubbles: true }));
      const enter = new KeyboardEvent("keydown", {
        key: "Enter",
        code: "Enter",
        bubbles: true,
        cancelable: true
      });
      textarea.dispatchEvent(enter);
      return enter.defaultPrevented;
    },
    3000
  );
  assert.equal(started, true, "Composer should create an active e2e thread.");
  await waitFor(
    win,
    () => {
      const text = document.querySelector(".conversation-width")?.textContent ?? "";
      return text.includes("Process handoff e2e seed.");
    },
    3000
  );
  return "thread-immediate-title-e2e";
}

function processCheckpointRect() {
  const turns = Array.from(document.querySelectorAll(".turn"));
  const turn = turns.at(-1);
  const checkpoint = turn?.querySelector(".turn-process-checkpoint-entering");
  if (!(checkpoint instanceof HTMLElement)) {
    return null;
  }
  const rect = checkpoint.getBoundingClientRect();
  if (rect.width <= 0 || rect.height <= 0) {
    return null;
  }
  const style = getComputedStyle(checkpoint);
  return {
    text: checkpoint.textContent ?? "",
    top: rect.top,
    left: rect.left,
    width: rect.width,
    height: rect.height,
    fontSize: style.fontSize,
    lineHeight: style.lineHeight
  };
}

function finalAnswerRect() {
  const turns = Array.from(document.querySelectorAll(".turn"));
  const turn = turns.at(-1);
  const finalText = turn?.querySelector(".agent-text .streaming-plain-text");
  if (!(finalText instanceof HTMLElement) || !finalText.textContent?.trim()) {
    return null;
  }
  const rect = finalText.getBoundingClientRect();
  const style = getComputedStyle(finalText);
  return {
    text: finalText.textContent ?? "",
    top: rect.top,
    left: rect.left,
    width: rect.width,
    height: rect.height,
    fontSize: style.fontSize,
    lineHeight: style.lineHeight
  };
}

function emitNotification(win, method, params) {
  win.webContents.send("test:server-event", {
    workdir: process.env.WUU_STREAM_E2E_CWD || process.cwd(),
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

async function waitFor(win, predicate, timeoutMs) {
  const started = Date.now();
  let lastValue;
  while (Date.now() - started < timeoutMs) {
    lastValue = await evaluate(win, predicate);
    if (lastValue) {
      return lastValue;
    }
    await delay(40);
  }
  throw new Error(`Timed out waiting for condition. Last value: ${JSON.stringify(lastValue)}`);
}

async function evaluate(win, fn) {
  return win.webContents.executeJavaScript(`(${fn.toString()})()`, true);
}

async function capture(win, name) {
  const image = await win.webContents.capturePage();
  fs.writeFileSync(path.join(evidenceDir, name), image.toPNG());
}

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function fail(error) {
  console.error(error);
  app.exit(1);
}
