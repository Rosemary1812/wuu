const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { app, BrowserWindow } = require("electron");

const desktopRoot = path.resolve(__dirname, "..");
const repoRoot = path.resolve(desktopRoot, "..");
const rendererHtml = path.join(desktopRoot, "out", "renderer", "index.html");
const rendererUrl = process.env.WUU_E2E_RENDERER_URL || "";
const preload = path.join(__dirname, "resize-e2e-preload.cjs");
const userData = path.join(desktopRoot, "out", "e2e", "sidebar-resize-user-data");
const disableStorage = process.env.WUU_SIDEBAR_RESIZE_DISABLE_STORAGE === "true";
const disableBackdrop = process.env.WUU_SIDEBAR_RESIZE_DISABLE_BACKDROP === "true";
const disableGpu = process.env.WUU_E2E_DISABLE_GPU === "true";
const visible = process.env.WUU_E2E_VISIBLE === "true";

process.env.WUU_RESIZE_E2E_CWD = repoRoot;
fs.rmSync(userData, { recursive: true, force: true });
fs.mkdirSync(userData, { recursive: true });
app.setPath("userData", userData);
if (disableGpu) {
  app.commandLine.appendSwitch("disable-gpu");
  app.commandLine.appendSwitch("disable-software-rasterizer");
}

app.whenReady().then(run).catch(fail);

async function run() {
  if (!rendererUrl) {
    assert.ok(fs.existsSync(rendererHtml), "Renderer build is missing. Run npm run build first.");
  }
  assert.ok(fs.existsSync(preload), "Resize E2E preload is missing.");

  const win = new BrowserWindow({
    width: 1380,
    height: 860,
    show: visible,
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

  await loadRenderer(win);
  await waitFor(win, () => Boolean(document.querySelector(".conversation-pane")), 5000);
  await waitFor(win, () => Boolean(document.querySelector(".sidebar-resizer")), 3000);
  await delay(250);

  const probe = await evaluate(
    win,
    async (options) => {
    const resizer = document.querySelector(".sidebar-resizer");
    const shell = document.querySelector(".app-shell");
    if (!(resizer instanceof HTMLElement) || !(shell instanceof HTMLElement)) {
      throw new Error("Sidebar resizer not found.");
    }
    if (options.disableStorage) {
      const originalSetItem = Storage.prototype.setItem;
      Storage.prototype.setItem = function setItem(key, value) {
        if (key === "wuu.desktop.sidebarWidth" || key === "wuu.desktop.sidebarCollapsed") {
          return undefined;
        }
        return originalSetItem.call(this, key, value);
      };
    }
    if (options.disableBackdrop) {
      const style = document.createElement("style");
      style.textContent = `
        .resizing-sidebar .sidebar {
          box-shadow: none !important;
          backdrop-filter: none !important;
          -webkit-backdrop-filter: none !important;
        }
      `;
      document.head.append(style);
    }

    const samples = [];
    let previousTs;
    let sampling = true;
    const sample = (ts) => {
      samples.push({
        dt: previousTs === undefined ? 0 : ts - previousTs,
        width: Number.parseFloat(getComputedStyle(shell).getPropertyValue("--sidebar-width")) || 0,
        openWidth: Number.parseFloat(getComputedStyle(shell).getPropertyValue("--sidebar-open-width")) || 0,
        resizing: shell.classList.contains("resizing-sidebar")
      });
      previousTs = ts;
      if (sampling) {
        window.requestAnimationFrame(sample);
      }
    };
    window.requestAnimationFrame(sample);

    const rect = resizer.getBoundingClientRect();
    const startX = rect.left + rect.width / 2;
    resizer.dispatchEvent(new PointerEvent("pointerdown", { bubbles: true, button: 0, clientX: startX, pointerId: 1 }));
    for (let index = 0; index < 96; index += 1) {
      const direction = index < 48 ? 1 : -1;
      const offset = direction === 1 ? index * 3 : (96 - index) * 3;
      window.dispatchEvent(
        new PointerEvent("pointermove", {
          bubbles: true,
          button: 0,
          clientX: startX + offset,
          pointerId: 1
        })
      );
      await new Promise((resolve) => window.setTimeout(resolve, 8));
    }
    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true, button: 0, clientX: startX, pointerId: 1 }));
    await new Promise((resolve) => window.setTimeout(resolve, 160));
    sampling = false;
    await new Promise((resolve) => window.requestAnimationFrame(resolve));
    return samples;
  },
    { disableStorage, disableBackdrop }
  );

  const maxFrameMs = Math.max(...probe.map((sample) => sample.dt));
  const resizingSamples = probe.filter((sample) => sample.resizing);
  const minWidth = Math.min(...probe.map((sample) => sample.width));
  const maxWidth = Math.max(...probe.map((sample) => sample.width));
  const summary = {
    samples: probe.length,
    resizingSamples: resizingSamples.length,
    maxFrameMs: Math.round(maxFrameMs),
    minWidth,
    maxWidth,
    disableStorage,
    disableBackdrop,
    disableGpu,
    visible
  };
  console.log(JSON.stringify(summary, null, 2));
  assert.ok(resizingSamples.length > 0, "Sidebar resize marker should be set while dragging.");
  assert.ok(maxWidth - minWidth >= 90, `Sidebar drag should change width. Summary=${JSON.stringify(summary)}`);

  win.close();
  app.quit();
}

function loadRenderer(win) {
  if (rendererUrl) {
    return new Promise((resolve, reject) => {
      const finish = () => resolve();
      const failLoad = (_event, code, description) => reject(new Error(`Failed to load ${rendererUrl}: ${code} ${description}`));
      win.webContents.once("did-finish-load", finish);
      win.webContents.once("did-fail-load", failLoad);
      win.loadURL(rendererUrl).catch(reject);
    });
  }
  return loadFile(win, rendererHtml);
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

async function evaluate(win, fn, arg) {
  return win.webContents.executeJavaScript(
    `(${fn.toString()})(${JSON.stringify(arg)})`,
    true
  );
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
