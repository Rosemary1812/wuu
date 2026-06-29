const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { app, BrowserWindow } = require("electron");

const desktopRoot = path.resolve(__dirname, "..");
const repoRoot = path.resolve(desktopRoot, "..");
const rendererHtml = path.join(desktopRoot, "out", "renderer", "index.html");
const preload = path.join(__dirname, "resize-e2e-preload.cjs");
const userData = path.join(desktopRoot, "out", "e2e", "window-resize-user-data");
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
  assert.ok(fs.existsSync(rendererHtml), "Renderer build is missing. Run npm run build first.");
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

  await loadFile(win, rendererHtml);
  await waitFor(win, () => Boolean(document.querySelector(".conversation-pane")), 5000);
  await delay(250);
  await evaluate(win, () => {
    const node = document.querySelector(".scroll-region");
    if (node instanceof HTMLElement) {
      node.scrollTop = Math.max(0, Math.floor(node.scrollHeight * 0.35));
      node.dispatchEvent(new Event("scroll", { bubbles: true }));
    }
  });
  await delay(120);
  await evaluate(win, () => {
    window.__wuuWindowResizePerfProbe = {
      turnScansDuringResize: 0,
      scrollTopWritesDuringResize: 0
    };
    if (!window.__wuuWindowResizePerfProbeInstalled) {
      window.__wuuWindowResizePerfProbeInstalled = true;
      const originalQuerySelectorAll = Element.prototype.querySelectorAll;
      Element.prototype.querySelectorAll = function querySelectorAllProbe(selector) {
        if (
          selector === ".turn[data-turn-id]" &&
          document.documentElement.classList.contains("window-resizing")
        ) {
          window.__wuuWindowResizePerfProbe.turnScansDuringResize += 1;
        }
        return originalQuerySelectorAll.call(this, selector);
      };

      let scrollTopProto = HTMLElement.prototype;
      let scrollTopDescriptor;
      while (scrollTopProto && !scrollTopDescriptor) {
        const descriptor = Object.getOwnPropertyDescriptor(scrollTopProto, "scrollTop");
        if (descriptor?.get && descriptor?.set && descriptor.configurable) {
          scrollTopDescriptor = descriptor;
          break;
        }
        scrollTopProto = Object.getPrototypeOf(scrollTopProto);
      }
      if (scrollTopDescriptor && scrollTopProto) {
        Object.defineProperty(scrollTopProto, "scrollTop", {
          configurable: true,
          enumerable: scrollTopDescriptor.enumerable,
          get() {
            return scrollTopDescriptor.get.call(this);
          },
          set(value) {
            if (
              document.documentElement.classList.contains("window-resizing") &&
              this instanceof HTMLElement &&
              this.matches(".scroll-region, .reasoning-scroll, .process-output-scroll")
            ) {
              window.__wuuWindowResizePerfProbe.scrollTopWritesDuringResize += 1;
            }
            return scrollTopDescriptor.set.call(this, value);
          }
        });
      }
    }
    window.__wuuWindowResizePerfSamples = [];
    window.__wuuWindowResizePerfSampling = true;
    let previousTs;
    const sample = (ts) => {
      const samples = window.__wuuWindowResizePerfSamples;
      samples.push({
        dt: previousTs === undefined ? 0 : ts - previousTs,
        resizing: document.documentElement.classList.contains("window-resizing"),
        scrollTop: document.querySelector(".scroll-region")?.scrollTop ?? 0,
        width: window.innerWidth
      });
      previousTs = ts;
      if (window.__wuuWindowResizePerfSampling) {
        window.requestAnimationFrame(sample);
      }
    };
    window.requestAnimationFrame(sample);
  });

  for (let index = 0; index < 96; index += 1) {
    const growing = index < 48;
    const offset = growing ? index * 5 : (96 - index) * 5;
    win.setSize(1180 + offset, 760 + Math.floor(offset / 8));
    await delay(8);
  }
  await delay(260);

  const result = await evaluate(win, () => {
    window.__wuuWindowResizePerfSampling = false;
    return new Promise((resolve) => {
      window.requestAnimationFrame(() => {
        resolve({
          samples: window.__wuuWindowResizePerfSamples ?? [],
          probe: window.__wuuWindowResizePerfProbe
        });
      });
    });
  });
  const samples = result.samples;
  const probe = result.probe ?? {};

  const maxFrameMs = Math.max(...samples.map((sample) => sample.dt));
  const sorted = samples.map((sample) => sample.dt).sort((left, right) => left - right);
  const p95FrameMs = sorted[Math.floor(sorted.length * 0.95)] ?? 0;
  const resizingSamples = samples.filter((sample) => sample.resizing);
  const widths = samples.map((sample) => sample.width);
  const summary = {
    samples: samples.length,
    resizingSamples: resizingSamples.length,
    maxFrameMs: Math.round(maxFrameMs),
    p95FrameMs: Math.round(p95FrameMs),
    minWidth: Math.min(...widths),
    maxWidth: Math.max(...widths),
    turnScansDuringResize: probe.turnScansDuringResize ?? 0,
    scrollTopWritesDuringResize: probe.scrollTopWritesDuringResize ?? 0,
    disableGpu,
    visible
  };
  console.log(JSON.stringify(summary, null, 2));
  assert.ok(samples.length > 20, "Window resize perf probe should collect frame samples.");
  assert.ok(resizingSamples.length > 0, "window-resizing marker should appear during BrowserWindow resize.");
  assert.ok(summary.maxWidth - summary.minWidth >= 160, `Window width should change during probe. Summary=${JSON.stringify(summary)}`);
  assert.ok(summary.p95FrameMs <= 50, `Window resize p95 frame time should stay below 50ms. Summary=${JSON.stringify(summary)}`);
  assert.ok(summary.maxFrameMs <= 120, `Window resize max frame time should stay below 120ms. Summary=${JSON.stringify(summary)}`);
  assert.equal(summary.turnScansDuringResize, 0, `Turn rail should not scan turn DOM during live window resize. Summary=${JSON.stringify(summary)}`);
  assert.equal(summary.scrollTopWritesDuringResize, 0, `Auto-follow should not write scrollTop during live window resize. Summary=${JSON.stringify(summary)}`);

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
