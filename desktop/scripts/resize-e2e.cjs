const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { app, BrowserWindow } = require("electron");

const desktopRoot = path.resolve(__dirname, "..");
const repoRoot = path.resolve(desktopRoot, "..");
const rendererHtml = path.join(desktopRoot, "out", "renderer", "index.html");
const preload = path.join(__dirname, "resize-e2e-preload.cjs");

process.env.WUU_RESIZE_E2E_CWD = repoRoot;
app.commandLine.appendSwitch("disable-gpu");
app.commandLine.appendSwitch("disable-software-rasterizer");

app.whenReady().then(run).catch(fail);

async function run() {
  assert.ok(fs.existsSync(rendererHtml), "Renderer build is missing. Run npm run build first.");
  assert.ok(fs.existsSync(preload), "Resize E2E preload is missing.");

  const win = new BrowserWindow({
    width: 1380,
    height: 860,
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

  const runDebugVisible = await evaluate(win, () => Boolean(document.querySelector(".run-debug-button")));
  assert.equal(runDebugVisible, false, "Production desktop builds must not expose the internal run debug panel.");

  await evaluate(win, () => {
    const button = Array.from(document.querySelectorAll(".workspace-toggle-button")).find((candidate) =>
      candidate.getAttribute("aria-label")?.includes("底部栏")
    );
    if (!(button instanceof HTMLButtonElement)) {
      throw new Error("Bottom panel toggle button not found.");
    }
    button.click();
  });
  const workspaceToolLabels = await waitFor(
    win,
    () => {
      const labels = Array.from(document.querySelectorAll(".workspace-tool-card strong"))
        .map((node) => node.textContent?.trim())
        .filter(Boolean);
      return labels.length > 0 ? labels : null;
    },
    1000
  );
  assert.deepEqual(
    workspaceToolLabels,
    ["文件", "审查"],
    "Workspace tool picker should only expose implemented user-facing tools."
  );
  await evaluate(win, () => {
    const button = Array.from(document.querySelectorAll(".workspace-toggle-button")).find((candidate) =>
      candidate.getAttribute("aria-label")?.includes("底部栏")
    );
    if (!(button instanceof HTMLButtonElement)) {
      throw new Error("Bottom panel toggle button not found.");
    }
    button.click();
  });

  const primaryScroll = await evaluate(win, () => {
    const node = document.querySelector(".scroll-region");
    return {
      exists: node instanceof HTMLElement,
      tagName: node?.tagName ?? "",
      overlayInitialized: node instanceof HTMLElement && node.hasAttribute("data-overlayscrollbars-initialize"),
      overflowY: node instanceof HTMLElement ? getComputedStyle(node).overflowY : ""
    };
  });
  assert.equal(primaryScroll.exists, true, "Primary conversation scroll region should exist.");
  assert.equal(primaryScroll.tagName, "DIV", "Primary conversation scroll region should use a native element.");
  assert.equal(
    primaryScroll.overlayInitialized,
    false,
    "Primary conversation scroll region should not be driven by OverlayScrollbars during live window resize."
  );
  assert.match(primaryScroll.overflowY, /auto|scroll/, "Primary conversation scroll region should use native scrolling.");

  const beforeEnvironmentResize = await waitFor(win, () => environmentSnapshot(), 5000);
  assert.equal(beforeEnvironmentResize.visible, true, "Environment panel should start visible above the wide-window breakpoint.");
  assert.ok(
    beforeEnvironmentResize.scrollPaddingRight > 300,
    "Scroll region should reserve inline environment panel space before resize."
  );
  assert.ok(
    beforeEnvironmentResize.composerPaddingRight > 300,
    "Composer should reserve inline environment panel space before resize."
  );

  win.setSize(1280, 640);
  const duringEnvironmentResize = await waitFor(
    win,
    () => {
      const snapshot = environmentSnapshot();
      return snapshot && snapshot.scrollPaddingRight <= 1 && snapshot.composerPaddingRight <= 1 ? snapshot : null;
    },
    120
  );
  assert.match(
    duringEnvironmentResize.scrollTransitionDuration,
    /^0s(, 0s)*$/,
    "Scroll region padding must not depend on a resize marker to avoid animation."
  );
  assert.match(
    duringEnvironmentResize.composerTransitionDuration,
    /^0s(, 0s)*$/,
    "Composer padding must not depend on a resize marker to avoid animation."
  );
  assert.ok(
    duringEnvironmentResize.scrollPaddingRight <= 1,
    "Scroll region should release inline environment panel space before resize end."
  );
  assert.ok(
    duringEnvironmentResize.composerPaddingRight <= 1,
    "Composer should release inline environment panel space before resize end."
  );

  await waitFor(win, () => !document.documentElement.classList.contains("window-resizing"), 1000);
  win.setSize(1280, 860);
  await delay(180);
  await waitFor(win, () => !document.documentElement.classList.contains("window-resizing"), 1000);

  const resizeObserverName = await evaluate(win, () => ResizeObserver.name);
  assert.notEqual(resizeObserverName, "FileTreeResizeObserverGate", "ResizeObserver must not be gated during live window resize.");

  const appShellIdleTransition = await evaluate(win, () => getComputedStyle(document.querySelector(".app-shell")).transitionProperty);
  assert.match(
    appShellIdleTransition,
    /grid-template-columns/,
    "App shell should keep normal panel open/close layout transitions while idle."
  );

  await evaluate(win, () => {
    const button = Array.from(document.querySelectorAll(".workspace-toggle-button")).find((candidate) =>
      candidate.getAttribute("aria-label")?.includes("右侧栏")
    );
    if (!(button instanceof HTMLButtonElement)) {
      throw new Error("Right panel toggle button not found.");
    }
    button.click();
  });

  const before = await waitFor(win, () => treeSnapshot(), 5000);
  assert.ok(before.frameHeight > 500, "Initial file tree frame should be tall enough for resize verification.");
  assert.ok(before.renderedRows > 20, "Initial file tree should render virtualized rows.");

  await evaluate(win, () => {
    const probe = { samples: [] };
    window.__resizeProbe = probe;
    const startedAt = performance.now();
    const sample = () => {
      const snapshot = window.treeSnapshot?.();
      if (snapshot) {
        probe.samples.push(snapshot);
      }
      if (performance.now() - startedAt < 240) {
        window.requestAnimationFrame(sample);
      }
    };
    window.requestAnimationFrame(sample);
  });
  win.setSize(980, 560);
  await delay(280);
  const resizeProbe = await evaluate(win, () => window.__resizeProbe);
  const liveResizeSamples = resizeProbe.samples.filter((sample) => sample.resizing);
  const during =
    liveResizeSamples.find(
      (sample) =>
        sample.frameHeight < before.frameHeight - 180 &&
        Math.abs(sample.virtualWindowHeight - before.virtualWindowHeight) >= 84 &&
        sample.renderedRows < before.renderedRows
    ) ??
    liveResizeSamples.find(
      (sample) => sample.frameHeight < before.frameHeight - 180
    ) ?? resizeProbe.samples.find((sample) => sample.frameHeight < before.frameHeight - 180);

  assert.ok(liveResizeSamples.length > 0, "Programmatic BrowserWindow resizing should set the window resize marker.");
  assert.ok(during, "Resize probe should capture a shrunken file tree frame during programmatic resize.");

  assert.equal(during.resizing, true, "Window resize marker should be set by programmatic BrowserWindow resizing.");
  assert.equal(during.shellTransitionProperty, "none", "App shell transitions should be disabled only during live resize.");
  assert.ok(during.viewportHeight < before.viewportHeight - 180, "Renderer viewport should shrink with the Electron window.");
  assert.ok(during.frameHeight < before.frameHeight - 180, "File tree frame should shrink with the Electron window.");
  assert.ok(
    during.scrollHeight < before.scrollHeight - 180,
    "Virtualized file tree scroll frame should shrink with the resized container."
  );
  assert.ok(
    Math.abs(during.frameHeight - during.scrollHeight - (before.frameHeight - before.scrollHeight)) <= 2,
    "Virtualized file tree scroll frame should preserve its internal chrome while tracking container height."
  );
  assert.ok(
    Math.abs(during.virtualWindowHeight - before.virtualWindowHeight) >= 84,
    "Virtualized file tree content should recompute before resize end."
  );
  assert.ok(during.renderedRows < before.renderedRows, "Rendered virtual rows should decrease while the window is still resizing.");
  await waitFor(win, () => !document.documentElement.classList.contains("window-resizing"), 1000);

  console.log("resize e2e passed");
  app.exit(0);
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
  const source = `(() => {
    window.treeSnapshot = () => {
      const frame = document.querySelector(".workspace-file-tree-frame");
      const host = frame?.querySelector("file-tree-container");
      const root = host?.shadowRoot;
      const scroll = root?.querySelector('[data-file-tree-virtualized-scroll="true"]');
      const virtualWindow = root?.querySelector('[data-file-tree-virtualized-sticky="true"]');
      if (!frame || !host || !root || !scroll || !virtualWindow) {
        return null;
      }
      return {
        frameHeight: frame.getBoundingClientRect().height,
        renderedRows: root.querySelectorAll('[data-type="item"]').length,
        resizing: document.documentElement.classList.contains("window-resizing"),
        shellTransitionProperty: getComputedStyle(document.querySelector(".app-shell")).transitionProperty,
        scrollHeight: scroll.getBoundingClientRect().height,
        viewportHeight: window.innerHeight,
        viewportWidth: window.innerWidth,
        virtualWindowHeight: Number.parseFloat(virtualWindow.style.height || "0")
      };
    };
    window.environmentSnapshot = () => {
      const pane = document.querySelector(".conversation-pane");
      const scroll = document.querySelector(".scroll-region");
      const composer = document.querySelector(".dock-composer-wrap");
      if (!pane || !scroll || !composer) {
        return null;
      }
      const scrollStyle = getComputedStyle(scroll);
      const composerStyle = getComputedStyle(composer);
      return {
        visible: pane.classList.contains("environment-panel-visible"),
        resizing: document.documentElement.classList.contains("window-resizing"),
        scrollPaddingRight: Number.parseFloat(scrollStyle.paddingRight || "0"),
        composerPaddingRight: Number.parseFloat(composerStyle.paddingRight || "0"),
        scrollTransitionProperty: scrollStyle.transitionProperty,
        scrollTransitionDuration: scrollStyle.transitionDuration,
        composerTransitionProperty: composerStyle.transitionProperty,
        composerTransitionDuration: composerStyle.transitionDuration
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
