const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { app, BrowserWindow } = require("electron");

const desktopRoot = path.resolve(__dirname, "..");
const repoRoot = path.resolve(desktopRoot, "..");
const rendererHtml = path.join(desktopRoot, "out", "renderer", "index.html");
const preload = path.join(__dirname, "resize-e2e-preload.cjs");
const userData = path.join(desktopRoot, "out", "e2e", "turn-rail-user-data");

process.env.WUU_RESIZE_E2E_CWD = repoRoot;
fs.rmSync(userData, { recursive: true, force: true });
fs.mkdirSync(userData, { recursive: true });
app.setPath("userData", userData);

if (process.env.WUU_E2E_DISABLE_GPU === "true") {
  app.commandLine.appendSwitch("disable-gpu");
  app.commandLine.appendSwitch("disable-software-rasterizer");
}

app.whenReady().then(run).catch(fail);

async function run() {
  assert.ok(fs.existsSync(rendererHtml), "Renderer build is missing. Run npm run build first.");
  assert.ok(fs.existsSync(preload), "Resize E2E preload is missing.");

  const win = new BrowserWindow({
    width: 1380,
    height: 860,
    show: process.env.WUU_E2E_VISIBLE === "true",
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
  await waitFor(win, () => Boolean(document.querySelector(".conversation-turn-rail")), 5000);
  await delay(250);

  const probe = await evaluate(win, async () => {
    const rail = document.querySelector(".conversation-turn-rail");
    if (!(rail instanceof HTMLElement)) {
      throw new Error("Turn rail not found.");
    }

    const metrics = {
      rectCalls: 0,
      rectMs: 0,
      queryCalls: 0,
      queryMs: 0
    };
    const originalRect = Element.prototype.getBoundingClientRect;
    Element.prototype.getBoundingClientRect = function getBoundingClientRectProbe() {
      const started = performance.now();
      try {
        return originalRect.call(this);
      } finally {
        metrics.rectCalls += 1;
        metrics.rectMs += performance.now() - started;
      }
    };
    const originalElementQuery = Element.prototype.querySelectorAll;
    Element.prototype.querySelectorAll = function querySelectorAllProbe(...args) {
      const started = performance.now();
      try {
        return originalElementQuery.apply(this, args);
      } finally {
        metrics.queryCalls += 1;
        metrics.queryMs += performance.now() - started;
      }
    };
    const originalDocumentQuery = Document.prototype.querySelectorAll;
    Document.prototype.querySelectorAll = function documentQuerySelectorAllProbe(...args) {
      const started = performance.now();
      try {
        return originalDocumentQuery.apply(this, args);
      } finally {
        metrics.queryCalls += 1;
        metrics.queryMs += performance.now() - started;
      }
    };

    const samples = [];
    let previousTs;
    let sampling = true;
    const sample = (ts) => {
      samples.push({
        dt: previousTs === undefined ? 0 : ts - previousTs,
        hovered: document.querySelectorAll(".conversation-turn-rail-bar.hovered").length,
        previews: document.querySelectorAll(".conversation-turn-rail-preview").length
      });
      previousTs = ts;
      if (sampling) {
        window.requestAnimationFrame(sample);
      }
    };
    window.requestAnimationFrame(sample);

    const rect = rail.getBoundingClientRect();
    const x = rect.left + rect.width / 2;
    for (let index = 0; index < 96; index += 1) {
      const y = rect.top + ((index % 48) / 47) * rect.height;
      rail.dispatchEvent(new MouseEvent("mousemove", { bubbles: true, clientX: x, clientY: y }));
      await new Promise((resolve) => window.setTimeout(resolve, 8));
    }
    rail.dispatchEvent(new MouseEvent("mouseleave", { bubbles: true, clientX: x, clientY: rect.bottom + 8 }));

    rail.dispatchEvent(
      new PointerEvent("pointerdown", {
        bubbles: true,
        button: 0,
        clientX: x,
        clientY: rect.top + rect.height / 2,
        pointerId: 1
      })
    );
    for (let index = 0; index < 96; index += 1) {
      const y = rect.top + ((index % 48) / 47) * rect.height;
      window.dispatchEvent(
        new PointerEvent("pointermove", {
          bubbles: true,
          button: 0,
          clientX: x,
          clientY: y,
          pointerId: 1
        })
      );
      await new Promise((resolve) => window.setTimeout(resolve, 8));
    }
    window.dispatchEvent(
      new PointerEvent("pointerup", {
        bubbles: true,
        button: 0,
        clientX: x,
        clientY: rect.bottom,
        pointerId: 1
      })
    );
    await new Promise((resolve) => window.setTimeout(resolve, 160));
    sampling = false;
    await new Promise((resolve) => window.requestAnimationFrame(resolve));

    Element.prototype.getBoundingClientRect = originalRect;
    Element.prototype.querySelectorAll = originalElementQuery;
    Document.prototype.querySelectorAll = originalDocumentQuery;
    return { samples, metrics };
  });

  const maxFrameMs = Math.max(...probe.samples.map((sample) => sample.dt));
  const summary = {
    samples: probe.samples.length,
    maxFrameMs: Math.round(maxFrameMs),
    rectCalls: probe.metrics.rectCalls,
    rectMs: Number(probe.metrics.rectMs.toFixed(2)),
    queryCalls: probe.metrics.queryCalls,
    queryMs: Number(probe.metrics.queryMs.toFixed(2)),
    maxPreviews: Math.max(...probe.samples.map((sample) => sample.previews))
  };
  console.log(JSON.stringify(summary, null, 2));
  assert.ok(summary.samples > 0, "Turn rail probe should collect frame samples.");

  win.close();
  app.quit();
}

function loadFile(win, file) {
  return new Promise((resolve, reject) => {
    const finish = () => resolve();
    const failLoad = (_event, code, description) => reject(new Error(`Failed to load ${file}: ${code} ${description}`));
    win.webContents.once("did-finish-load", finish);
    win.webContents.once("did-fail-load", failLoad);
    win.loadFile(file).catch(reject);
  });
}

async function evaluate(win, fn) {
  return win.webContents.executeJavaScript(`(${fn.toString()})()`, true);
}

async function waitFor(win, fn, timeoutMs) {
  const started = Date.now();
  let lastValue;
  while (Date.now() - started < timeoutMs) {
    lastValue = await evaluate(win, fn);
    if (lastValue) {
      return lastValue;
    }
    await delay(20);
  }
  throw new Error(`Timed out waiting for condition. Last value: ${JSON.stringify(lastValue)}`);
}

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function fail(error) {
  console.error(error);
  app.quit();
  process.exitCode = 1;
}
