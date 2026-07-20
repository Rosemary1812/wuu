const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { app, BrowserWindow } = require("electron");

const desktopRoot = path.resolve(__dirname, "..");
const repoRoot = path.resolve(desktopRoot, "..");
const rendererHtml = path.join(desktopRoot, "out", "renderer", "index.html");
const preload = path.join(__dirname, "streaming-e2e-preload.cjs");
const userDataDir = fs.mkdtempSync(path.join(os.tmpdir(), "wuu-kanban-e2e-"));

process.env.WUU_STREAM_E2E_CWD = repoRoot;
app.setPath("userData", userDataDir);
app.commandLine.appendSwitch("disable-gpu");
app.commandLine.appendSwitch("disable-software-rasterizer");
app.on("will-quit", () => fs.rmSync(userDataDir, { recursive: true, force: true }));

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
      sandbox: false,
    },
  });

  win.webContents.on("render-process-gone", (_event, details) => {
    fail(new Error(`Renderer process exited: ${details.reason}`));
  });

  await win.loadFile(rendererHtml);
  await waitFor(win, () => Boolean(document.querySelector(".composer textarea")), 5000);

  const initial = await evaluate(win, () => {
    const collaboration = document.querySelector(".collaboration-nav-item");
    return {
      collaborationReady:
        collaboration instanceof HTMLButtonElement && !collaboration.disabled,
      inPrimaryNav: Boolean(collaboration?.closest(".primary-nav")),
      inWorkspaceList: Boolean(collaboration?.closest(".sidebar-main")),
      hasToggle: Boolean(document.querySelector(".kanban-view-toggle")),
    };
  });
  assert.equal(initial.collaborationReady, true, "The collaboration entry should be enabled after initialization.");
  assert.equal(initial.inPrimaryNav, true, "The collaboration entry should live in the fixed primary navigation.");
  assert.equal(initial.inWorkspaceList, false, "The collaboration entry must stay outside workspace and pinned lists.");
  assert.equal(initial.hasToggle, false, "Ordinary conversations must not show the message/board switch.");

  await evaluate(win, () => {
    const collaboration = document.querySelector(".collaboration-nav-item");
    if (!(collaboration instanceof HTMLButtonElement)) {
      throw new Error("Collaboration entry not found.");
    }
    collaboration.click();
  });

  const collaboration = await waitFor(
    win,
    () => {
      const tabs = Array.from(document.querySelectorAll('.kanban-view-toggle [role="tab"]'));
      if (tabs.length !== 2) return null;
      const toggle = tabs[0].closest(".kanban-view-toggle");
      const titleActionButtons = Array.from(
        document.querySelectorAll(".titlebar .title-actions > button"),
      );
      return {
        labels: tabs.map((tab) => tab.textContent?.trim()),
        selected: tabs.map((tab) => tab.getAttribute("aria-selected")),
        inComposer: Boolean(toggle?.closest(".composer-top-accessory")),
        inTitlebar: Boolean(toggle?.closest(".titlebar")),
        titleActionsVisible: titleActionButtons.every(
          (button) => button.getBoundingClientRect().width > 0,
        ),
      };
    },
    5000,
  );
  assert.deepEqual(collaboration.labels, ["消息", "看板"]);
  assert.deepEqual(collaboration.selected, ["true", "false"]);
  assert.equal(collaboration.inComposer, true, "The message/board switch should sit beside the composer.");
  assert.equal(collaboration.inTitlebar, false, "The message/board switch must not consume titlebar space.");
  assert.equal(collaboration.titleActionsVisible, true, "Titlebar panel buttons should remain visible.");

  await evaluate(win, () => {
    const boardTab = document.querySelectorAll('.kanban-view-toggle [role="tab"]')[1];
    if (!(boardTab instanceof HTMLButtonElement)) {
      throw new Error("Board tab not found.");
    }
    boardTab.click();
  });
  await waitFor(
    win,
    () => Boolean(document.querySelector(".kanban-board:not(.kanban-board-loading)")),
    5000,
  );
  assert.equal(
    await evaluate(win, () => document.querySelector('.kanban-view-toggle [aria-selected="true"]')?.textContent?.trim()),
    "看板",
  );

  await evaluate(win, () => {
    const newConversation = document.querySelector(".primary-nav .nav-item");
    if (!(newConversation instanceof HTMLButtonElement)) {
      throw new Error("New conversation entry not found.");
    }
    newConversation.click();
  });
  await waitFor(
    win,
    () =>
      Boolean(document.querySelector(".composer textarea")) &&
      !document.querySelector(".kanban-view-toggle") &&
      !document.querySelector(".kanban-board"),
    5000,
  );

  console.log("kanban os e2e passed");
  app.exit(0);
}

async function waitFor(win, predicate, timeoutMs) {
  const started = Date.now();
  let lastValue;
  while (Date.now() - started < timeoutMs) {
    lastValue = await evaluate(win, predicate);
    if (lastValue) return lastValue;
    await new Promise((resolve) => setTimeout(resolve, 40));
  }
  throw new Error(`Timed out waiting for condition. Last value: ${JSON.stringify(lastValue)}`);
}

function evaluate(win, fn) {
  return win.webContents.executeJavaScript(`(${fn.toString()})()`, true);
}

function fail(error) {
  console.error(error);
  app.exit(1);
}
