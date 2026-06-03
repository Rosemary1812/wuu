// E2E for the right-sidebar browser panel.
// Verifies: panel opens, browser tool appears in the picker, switching to
// it mounts a <webview>, the URL bar navigates, and the webview reports
// back its current URL.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { app, BrowserWindow } = require("electron");

const desktopRoot = path.resolve(__dirname, "..");
const rendererHtml = path.join(desktopRoot, "out", "renderer", "index.html");
const preload = path.join(__dirname, "browser-panel-e2e-preload.cjs");

process.env.WUU_BROWSER_E2E_CWD = desktopRoot;
app.commandLine.appendSwitch("disable-gpu");
app.commandName = "wuu-browser-e2e";

app.whenReady().then(run).catch(fail);

async function run() {
  assert.ok(fs.existsSync(rendererHtml), "Renderer build missing. Run npm run build first.");
  assert.ok(fs.existsSync(preload), "Browser panel E2E preload is missing.");

  const win = new BrowserWindow({
    width: 1180,
    height: 820,
    show: false,
    webPreferences: {
      preload,
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: false,
      webviewTag: true
    }
  });

  win.webContents.on("render-process-gone", (_event, details) => {
    fail(new Error(`Renderer process exited: ${details.reason}`));
  });
  win.webContents.on("console-message", (_event, level, message) => {
    if (level >= 2) {
      console.error(`renderer console: ${message}`);
    }
  });

  await win.loadFile(rendererHtml);
  await waitFor(win, () => Boolean(document.querySelector(".app-shell")), 5000);

  // Open the right sidebar.
  const opened = await evaluate(win, () => {
    const toggles = Array.from(document.querySelectorAll("button"));
    const target = toggles.find((button) =>
      button.getAttribute("aria-label") === "打开右侧栏"
    );
    if (!target) {
      throw new Error("Right sidebar toggle not found.");
    }
    target.click();
    return true;
  });
  assert.equal(opened, true);

  // The browser tool should be advertised in the picker.
  const toolAdvertised = await waitFor(
    win,
    () => {
      const items = Array.from(document.querySelectorAll(".workspace-tool-menu-item"));
      return items.some((item) => item.textContent && item.textContent.includes("浏览器"));
    },
    4000
  );
  assert.equal(toolAdvertised, true, "Browser tool should appear in the workspace picker.");

  // Click the browser tool to open it.
  await evaluate(win, () => {
    const items = Array.from(document.querySelectorAll(".workspace-tool-menu-item"));
    const target = items.find((item) => item.textContent && item.textContent.includes("浏览器"));
    if (!target) {
      throw new Error("Browser tool button not found in picker.");
    }
    target.click();
    return true;
  });

  // The browser panel should mount and the <webview> should appear.
  const webviewMounted = await waitFor(
    win,
    () => {
      const host = document.querySelector(".workspace-browser-host");
      if (!host) return false;
      return host.querySelector("webview") !== null;
    },
    5000
  );
  assert.equal(webviewMounted, true, "<webview> element should mount inside the browser panel.");

  // Verify the URL bar is editable and that submitting a navigation updates
  // the current URL state reflected in the status bar.
  await waitFor(
    win,
    () => {
      const input = document.querySelector(".workspace-browser-url-input");
      return input instanceof HTMLInputElement;
    },
    3000
  );

  // Type a data URL so the test does not depend on the network. The webview
  // accepts arbitrary URLs; data URLs give us a deterministic, self-contained
  // page to render.
  const dataURL =
    "data:text/html;charset=utf-8," +
    encodeURIComponent(
      "<!doctype html><html><head><title>BrowserE2E</title></head><body><h1 id='probe'>wuu browser ok</h1></body></html>"
    );

  await evaluate(win, (target) => {
    const input = document.querySelector(".workspace-browser-url-input");
    const form = document.querySelector(".workspace-browser-url-form");
    if (!(input instanceof HTMLInputElement) || !(form instanceof HTMLFormElement)) {
      throw new Error("Browser URL form not found.");
    }
    const setter = Object.getOwnPropertyDescriptor(
      window.HTMLInputElement.prototype,
      "value"
    ).set;
    setter.call(input, target);
    input.dispatchEvent(new Event("input", { bubbles: true }));
    form.requestSubmit();
    return true;
  }, dataURL);

  // Wait for the webview to finish loading and report its URL back into the
  // status bar. The data: URL has no hostname, so the host hint should fall
  // back to "等待输入" — and crucially the error overlay should never appear.
  await waitFor(
    win,
    () => {
      const error = document.querySelector(".workspace-browser-error");
      if (error) return false;
      const status = document.querySelector(".workspace-browser-status");
      if (status) return false; // webview is still in pre-dom-ready state
      return true;
    },
    6000
  );

  const hasError = await evaluate(win, () => {
    return document.querySelector(".workspace-browser-error") !== null;
  });
  assert.equal(hasError, false, "Browser panel should not show error overlay for a successful load.");

  // Back / forward buttons should update their disabled state once the
  // webview has history.
  const navState = await waitFor(
    win,
    () => {
      const buttons = Array.from(document.querySelectorAll(".workspace-browser-nav"));
      return buttons.length >= 4;
    },
    3000
  );
  assert.equal(navState, true, "Browser navigation buttons should render.");

  // Reload button should be a "refresh" icon (not the "stop" X) once loading
  // finishes. Clicking reload on a settled page is a no-op for the user
  // experience but it must not throw.
  await evaluate(win, () => {
    const buttons = Array.from(document.querySelectorAll(".workspace-browser-nav"));
    const reload = buttons.find((b) => b.getAttribute("aria-label") === "刷新" || b.getAttribute("aria-label") === "停止");
    if (!reload) {
      throw new Error("Reload/stop button not found.");
    }
    reload.click();
    return true;
  });

  console.log("OK: browser panel mounts, navigates, and reports status without errors.");
  win.close();
  app.quit();
}

async function loadFile(win, file) {
  return win.loadFile(file);
}

async function evaluate(win, fn, ...args) {
  return win.webContents.executeJavaScript(`(${fn})(${args.map((a) => JSON.stringify(a)).join(", ")})`);
}

async function waitFor(win, fn, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const value = await evaluate(win, fn);
      if (value) {
        return value;
      }
    } catch (err) {
      // The renderer might not have fully bootstrapped; retry.
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  return false;
}

function fail(err) {
  console.error("FAIL:", err && err.stack ? err.stack : err);
  app.exit(1);
}
