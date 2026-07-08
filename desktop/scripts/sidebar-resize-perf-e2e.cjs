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
  const finalSample = probe[probe.length - 1];
  const summary = {
    samples: probe.length,
    resizingSamples: resizingSamples.length,
    maxFrameMs: Math.round(maxFrameMs),
    minWidth,
    maxWidth,
    finalWidth: finalSample?.width,
    finalOpenWidth: finalSample?.openWidth,
    disableStorage,
    disableBackdrop,
    disableGpu,
    visible
  };
  console.log(JSON.stringify(summary, null, 2));
  assert.ok(resizingSamples.length > 0, "Sidebar resize marker should be set while dragging.");
  assert.ok(maxWidth - minWidth >= 90, `Sidebar drag should change width. Summary=${JSON.stringify(summary)}`);
  assert.ok(
    Math.abs((finalSample?.width ?? 0) - (finalSample?.openWidth ?? 0)) <= 1,
    `Sidebar content width should sync after drag ends. Summary=${JSON.stringify(summary)}`
  );

  await waitFor(
    win,
    () => {
      const button = Array.from(document.querySelectorAll(".side-panel-toggle-button")).find((candidate) =>
        candidate.getAttribute("aria-label")?.includes("右侧栏")
      );
      if (!(button instanceof HTMLButtonElement) || button.disabled) {
        return null;
      }
      button.click();
      return true;
    },
    3000
  );
  await waitFor(win, () => Boolean(document.querySelector(".workspace-right-panel-resizer")), 3000);

  const rightPanelProbe = await evaluate(
    win,
    async () => {
      const resizer = document.querySelector(".workspace-right-panel-resizer");
      const shell = document.querySelector(".app-shell");
      if (!(resizer instanceof HTMLElement) || !(shell instanceof HTMLElement)) {
        throw new Error("Right panel resizer not found.");
      }

      let dragging = false;
      let storageWritesDuringDrag = 0;
      const originalSetItem = Storage.prototype.setItem;
      Storage.prototype.setItem = function setItemProbe(key, value) {
        if (dragging && key === "wuu.desktop.workspaceRightPanelWidth") {
          storageWritesDuringDrag += 1;
        }
        return originalSetItem.call(this, key, value);
      };

      const samples = [];
      let previousTs;
      let sampling = true;
      const sample = (ts) => {
        samples.push({
          dt: previousTs === undefined ? 0 : ts - previousTs,
          width: Number.parseFloat(getComputedStyle(shell).getPropertyValue("--workspace-right-panel-width")) || 0,
          resizing: shell.classList.contains("resizing-right-panel")
        });
        previousTs = ts;
        if (sampling) {
          window.requestAnimationFrame(sample);
        }
      };
      window.requestAnimationFrame(sample);

      const rect = resizer.getBoundingClientRect();
      const startX = rect.left + rect.width / 2;
      dragging = true;
      resizer.dispatchEvent(new PointerEvent("pointerdown", { bubbles: true, button: 0, clientX: startX, pointerId: 2 }));
      for (let index = 0; index < 20 && !shell.classList.contains("resizing-right-panel"); index += 1) {
        await new Promise((resolve) => window.requestAnimationFrame(resolve));
      }
      for (let index = 0; index < 96; index += 1) {
        const direction = index < 48 ? -1 : 1;
        const offset = direction === -1 ? index * -3 : (96 - index) * -3;
        window.dispatchEvent(
          new PointerEvent("pointermove", {
            bubbles: true,
            button: 0,
            clientX: startX + offset,
            pointerId: 2
          })
        );
        await new Promise((resolve) => window.setTimeout(resolve, 8));
      }
      window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true, button: 0, clientX: startX, pointerId: 2 }));
      dragging = false;
      await new Promise((resolve) => window.setTimeout(resolve, 160));
      sampling = false;
      await new Promise((resolve) => window.requestAnimationFrame(resolve));
      Storage.prototype.setItem = originalSetItem;
      return { samples, storageWritesDuringDrag };
    }
  );

  const rightPanelSamples = rightPanelProbe.samples;
  const rightPanelMaxFrameMs = Math.max(...rightPanelSamples.map((sample) => sample.dt));
  const rightPanelResizingSamples = rightPanelSamples.filter((sample) => sample.resizing);
  const rightPanelMinWidth = Math.min(...rightPanelSamples.map((sample) => sample.width));
  const rightPanelMaxWidth = Math.max(...rightPanelSamples.map((sample) => sample.width));
  const rightPanelSummary = {
    samples: rightPanelSamples.length,
    resizingSamples: rightPanelResizingSamples.length,
    maxFrameMs: Math.round(rightPanelMaxFrameMs),
    minWidth: rightPanelMinWidth,
    maxWidth: rightPanelMaxWidth,
    storageWritesDuringDrag: rightPanelProbe.storageWritesDuringDrag,
    disableGpu,
    visible
  };
  console.log(JSON.stringify({ rightPanel: rightPanelSummary }, null, 2));
  assert.ok(rightPanelResizingSamples.length > 0, "Right panel resize marker should be set while dragging.");
  assert.ok(
    rightPanelMaxWidth - rightPanelMinWidth >= 90,
    `Right panel drag should change width. Summary=${JSON.stringify(rightPanelSummary)}`
  );
  assert.equal(
    rightPanelSummary.storageWritesDuringDrag,
    0,
    `Right panel drag should not persist width on every pointer move. Summary=${JSON.stringify(rightPanelSummary)}`
  );

  await waitFor(
    win,
    () => {
      const picker = document.querySelector(".workspace-panel-add");
      if (picker instanceof HTMLButtonElement) {
        picker.click();
      }
      const items = Array.from(document.querySelectorAll(".workspace-right-panel .workspace-tool-menu-item"));
      return items.some((item) => item.textContent?.includes("文件")) ? true : null;
    },
    3000
  );
  await evaluate(win, () => {
    const fileTool = Array.from(document.querySelectorAll(".workspace-right-panel .workspace-tool-menu-item"))
      .filter((candidate) => candidate instanceof HTMLButtonElement)
      .find((candidate) => candidate.textContent?.includes("文件"));
    if (!(fileTool instanceof HTMLButtonElement)) {
      throw new Error("Right panel file tool button not found.");
    }
    fileTool.click();
  });
  await waitFor(
    win,
    () => {
      const frame = document.querySelector(".workspace-file-tree-frame");
      return frame && frame.querySelectorAll(".workspace-file-tree-row").length > 20;
    },
    5000
  );

  const sidebarWithFileTreeProbe = await evaluate(
    win,
    async () => {
      const resizer = document.querySelector(".sidebar-resizer");
      const shell = document.querySelector(".app-shell");
      const rightPanel = document.querySelector(".workspace-right-panel");
      const frame = document.querySelector(".workspace-file-tree-frame");
      if (
        !(resizer instanceof HTMLElement) ||
        !(shell instanceof HTMLElement) ||
        !(rightPanel instanceof HTMLElement) ||
        !(frame instanceof HTMLElement)
      ) {
        throw new Error("Sidebar/file-tree resize probe targets not found.");
      }

      const samples = [];
      let previousTs;
      let sampling = true;
      const sample = (ts) => {
        const panelRect = rightPanel.getBoundingClientRect();
        const frameRect = frame.getBoundingClientRect();
        const frameStyle = getComputedStyle(frame);
        samples.push({
          dt: previousTs === undefined ? 0 : ts - previousTs,
          frameLeft: frameRect.left,
          frameTransform: frameStyle.transform,
          frameWillChange: frameStyle.willChange,
          panelLeft: panelRect.left,
          panelWidth: panelRect.width,
          resizing: shell.classList.contains("resizing-sidebar"),
          rightPanelWidth:
            Number.parseFloat(getComputedStyle(shell).getPropertyValue("--workspace-right-panel-width")) || 0,
          windowResizing: document.documentElement.classList.contains("window-resizing")
        });
        previousTs = ts;
        if (sampling) {
          window.requestAnimationFrame(sample);
        }
      };
      window.requestAnimationFrame(sample);

      const rect = resizer.getBoundingClientRect();
      const startX = rect.left + rect.width / 2;
      resizer.dispatchEvent(
        new PointerEvent("pointerdown", {
          bubbles: true,
          button: 0,
          clientX: startX,
          pointerId: 3
        })
      );
      for (let index = 0; index < 20 && !shell.classList.contains("resizing-sidebar"); index += 1) {
        await new Promise((resolve) => window.requestAnimationFrame(resolve));
      }
      for (let index = 0; index < 64; index += 1) {
        const direction = index < 32 ? 1 : -1;
        const offset = direction === 1 ? index * 3 : (64 - index) * 3;
        window.dispatchEvent(
          new PointerEvent("pointermove", {
            bubbles: true,
            button: 0,
            clientX: startX + offset,
            pointerId: 3
          })
        );
        await new Promise((resolve) => window.setTimeout(resolve, 8));
      }
      window.dispatchEvent(
        new PointerEvent("pointerup", {
          bubbles: true,
          button: 0,
          clientX: startX,
          pointerId: 3
        })
      );
      await new Promise((resolve) => window.setTimeout(resolve, 160));
      sampling = false;
      await new Promise((resolve) => window.requestAnimationFrame(resolve));
      return samples;
    }
  );

  const liveFileTreeSamples = sidebarWithFileTreeProbe.filter((sample) => sample.resizing);
  const panelLeftValues = liveFileTreeSamples.map((sample) => sample.panelLeft);
  const frameLeftValues = liveFileTreeSamples.map((sample) => sample.frameLeft);
  const rightPanelWidthValues = liveFileTreeSamples.map((sample) => sample.rightPanelWidth);
  const fileTreeSummary = {
    samples: sidebarWithFileTreeProbe.length,
    resizingSamples: liveFileTreeSamples.length,
    panelLeftDrift: Math.max(...panelLeftValues) - Math.min(...panelLeftValues),
    frameLeftDrift: Math.max(...frameLeftValues) - Math.min(...frameLeftValues),
    rightPanelWidthDrift: Math.max(...rightPanelWidthValues) - Math.min(...rightPanelWidthValues),
    transformSamples: liveFileTreeSamples.filter((sample) => sample.frameTransform !== "none").length,
    willChangeTransformSamples: liveFileTreeSamples.filter((sample) =>
      sample.frameWillChange.split(",").map((part) => part.trim()).includes("transform")
    ).length,
    disableGpu,
    visible
  };
  console.log(JSON.stringify({ sidebarWithFileTree: fileTreeSummary }, null, 2));
  assert.ok(liveFileTreeSamples.length > 0, "Sidebar resize marker should be set while dragging with the file tree open.");
  assert.ok(
    fileTreeSummary.panelLeftDrift <= 1,
    `Right panel should stay pinned while dragging the sidebar. Summary=${JSON.stringify(fileTreeSummary)}`
  );
  assert.ok(
    fileTreeSummary.frameLeftDrift <= 1,
    `File tree should stay pinned while dragging the sidebar. Summary=${JSON.stringify(fileTreeSummary)}`
  );
  assert.ok(
    fileTreeSummary.rightPanelWidthDrift <= 1,
    `Sidebar drag should not resize the right file panel. Summary=${JSON.stringify(fileTreeSummary)}`
  );
  assert.equal(
    fileTreeSummary.transformSamples,
    0,
    `File tree should not remain transform-promoted during sidebar drags. Summary=${JSON.stringify(fileTreeSummary)}`
  );
  assert.equal(
    fileTreeSummary.willChangeTransformSamples,
    0,
    `File tree should not keep will-change: transform during sidebar drags. Summary=${JSON.stringify(fileTreeSummary)}`
  );

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
