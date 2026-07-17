// E2E entry (Electron main): drives a real hidden browser tab through the
// production BrowserHostCoordinator + ObservationCoordinator and verifies the
// browser PiP shows live frames, animates the synthetic pointer, honors the
// visibility-takeover hide rule, and tears down without ghosts.
// Bundled by browser-pip-e2e.cjs; do not run directly with node.
import { app, BrowserWindow, WebContentsView } from "electron";
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import type { ActivitySession } from "../src/shared/protocol";
import { ObservationCoordinator } from "../src/main/cuaActivityWindows";
import { createObservationPiPFactory } from "../src/main/browserPiPWindow";
import {
  BrowserHostCoordinator,
  defaultBrowserHostDeps,
  BROWSER_PARTITION,
  type BrowserHostWindowHandle,
  type BrowserViewHandle,
} from "../src/main/browserHostWindows";
import type { WindowRegistry } from "../src/main/windowRegistry";

const ARTIFACT_DIR = process.env.WUU_BROWSER_PIP_E2E_ARTIFACTS ?? "/tmp/wuu-browser-pip-e2e";
const WORKDIR = "/e2e";
const THREAD_ID = "thread-1";
const TAB_ID = "t1";

// A page that visibly changes every frame tick: hue rotation + a millisecond
// counter, so two captures of a live stream can never be byte-identical.
const ANIMATED_PAGE = `data:text/html,${encodeURIComponent(`<!doctype html>
<html><body style="margin:0;font:24px sans-serif">
<div id="t" style="padding:40px">0</div>
<script>
  let n = 0;
  setInterval(() => {
    n += 1;
    document.getElementById("t").textContent = String(n) + " " + Date.now();
    document.body.style.background = "hsl(" + (n * 17 % 360) + ",70%,80%)";
  }, 90);
</script></body></html>`)}`;

function fail(error: unknown): never {
  console.error(error instanceof Error ? error.stack : String(error));
  app.exit(1);
  throw error;
}

async function sleep(ms: number): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, ms));
}

async function waitFor(label: string, check: () => Promise<boolean> | boolean, timeoutMs: number): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    if (await check()) return;
    if (Date.now() > deadline) throw new Error(`timed out waiting for: ${label}`);
    await sleep(120);
  }
}

function browserActivity(overrides: Partial<ActivitySession>): ActivitySession {
  return {
    id: "activity-1",
    kind: "browser",
    thread_id: THREAD_ID,
    workdir: WORKDIR,
    plugin_id: "embedded-browser",
    target: TAB_ID,
    state: "background_controlled",
    controller: "agent",
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    ...overrides,
  };
}

function findPipWindow(mainWin: BrowserWindow): BrowserWindow | undefined {
  return BrowserWindow.getAllWindows().find(
    (win) => win !== mainWin && !win.isDestroyed() && win.isVisible(),
  );
}

app.whenReady().then(run).catch(fail);

async function run(): Promise<void> {
  mkdirSync(ARTIFACT_DIR, { recursive: true });

  const mainWin = new BrowserWindow({ width: 1000, height: 700, show: false });
  const registry = { mainWindow: () => mainWin } as unknown as WindowRegistry;
  const host = new BrowserHostCoordinator(
    registry,
    { respond: () => undefined, reject: () => undefined },
    defaultBrowserHostDeps(
      () =>
        new BrowserWindow({
          show: false,
          skipTaskbar: true,
          frame: false,
          webPreferences: { contextIsolation: true, nodeIntegration: false, sandbox: true },
        }) as unknown as BrowserHostWindowHandle,
      () =>
        new WebContentsView({
          webPreferences: { partition: BROWSER_PARTITION, contextIsolation: true, nodeIntegration: false },
        }) as unknown as BrowserViewHandle,
    ),
    () => undefined,
  );

  // Open the animated tab through the production reverse-RPC path.
  await host.handleServerRequest({
    workdir: WORKDIR,
    kind: "server-request",
    message: { id: "open-1", method: "browser/open_tab", params: { workdir: WORKDIR, tab_id: TAB_ID, initial_url: ANIMATED_PAGE } },
  });

  const coordinator = new ObservationCoordinator(
    registry,
    undefined,
    createObservationPiPFactory({ browserHost: host, isPackaged: false }),
  );
  coordinator.setActiveThread(THREAD_ID);
  coordinator.update(browserActivity({}));

  // 1. The surface appears and cross-fades from the placeholder to live frames.
  await waitFor("PiP window visible", () => Boolean(findPipWindow(mainWin)), 8_000);
  const pip = findPipWindow(mainWin);
  if (!pip) throw new Error("PiP window not found");
  await waitFor(
    "first live frame presented",
    () => pip.webContents.executeJavaScript(`document.getElementById("view")?.classList.contains("live") ?? false`),
    10_000,
  );

  // 2. Frames keep flowing: two captures of the surface differ.
  const shot1 = await pip.webContents.capturePage();
  await sleep(1_200);
  const shot2 = await pip.webContents.capturePage();
  writeFileSync(join(ARTIFACT_DIR, "pip-frame-1.png"), shot1.toPNG());
  writeFileSync(join(ARTIFACT_DIR, "pip-frame-2.png"), shot2.toPNG());
  if (shot1.toPNG().equals(shot2.toPNG())) {
    throw new Error("PiP surface is not receiving live frames (identical captures)");
  }

  // 3. A click through the production CDP path animates the synthetic pointer.
  await host.handleServerRequest({
    workdir: WORKDIR,
    kind: "server-request",
    message: { id: "click-1", method: "browser/cdp", params: { workdir: WORKDIR, tab_id: TAB_ID, method: "click", params: { x: 30, y: 40 } } },
  });
  await waitFor(
    "pointer hint rendered",
    () => pip.webContents.executeJavaScript(`document.getElementById("ptr")?.style.opacity === "1"`),
    4_000,
  );

  // 4. Visibility takeover hides the mirror (the real page is on screen).
  await host.handleServerRequest({
    workdir: WORKDIR,
    kind: "server-request",
    message: { id: "vis-1", method: "browser/set_visibility", params: { workdir: WORKDIR, tab_id: TAB_ID, visible: true } },
  });
  coordinator.update(browserActivity({ state: "foreground_controlled", controller: "user" }));
  await waitFor("PiP hidden during takeover", () => pip.isDestroyed() || !pip.isVisible(), 4_000);
  coordinator.update(browserActivity({ state: "background_controlled", controller: "agent" }));
  await waitFor("PiP restored after takeover", () => !pip.isDestroyed() && pip.isVisible(), 4_000);

  // 5. User close dismisses the surface; a newer activity update revives it.
  await pip.webContents.executeJavaScript(`document.getElementById("close").click()`);
  await waitFor("PiP dismissed after user close", () => pip.isDestroyed(), 4_000);
  coordinator.update(browserActivity({ updated_at: new Date(Date.now() + 1_000).toISOString() }));
  await waitFor("PiP revived by newer activity", () => Boolean(findPipWindow(mainWin)), 8_000);

  // 6. Closing the tab tears the surface down without a ghost.
  const revived = findPipWindow(mainWin);
  await host.handleServerRequest({
    workdir: WORKDIR,
    kind: "server-request",
    message: { id: "close-1", method: "browser/close_tab", params: { workdir: WORKDIR, tab_id: TAB_ID } },
  });
  await waitFor("PiP gone after tab close", () => !revived || revived.isDestroyed(), 4_000);
  await sleep(1_000);
  if (findPipWindow(mainWin)) {
    throw new Error("ghost PiP surface survived tab close");
  }

  console.log("browser-pip-e2e: PASS (artifacts in", ARTIFACT_DIR + ")");
  app.exit(0);
}
