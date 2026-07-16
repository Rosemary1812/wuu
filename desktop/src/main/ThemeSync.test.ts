import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

// Same contract style as WindowChrome.test.ts: multi-window theme sync is a
// cross-file agreement (main-process broadcast, preload subscription,
// renderer boot wiring), so pin the load-bearing lines in each file.

const read = (path: string): string =>
  readFileSync(resolve(process.cwd(), path), "utf8");

const mainSource = read("src/main/index.ts");
const preloadSource = read("src/main/preload.ts");
const rendererBoot = read("src/renderer/main.tsx");

describe("multi-window theme sync", () => {
  it("persists then syncs every window on an explicit preference set", () => {
    const handlerStart = mainSource.indexOf('"wuu:theme-preference-set"');
    expect(handlerStart).toBeGreaterThan(-1);
    const handlerEnd = mainSource.indexOf("});", handlerStart);
    const handler = mainSource.slice(handlerStart, handlerEnd);
    expect(handler).toContain("setThemePreference(next)");
    expect(handler).toContain("syncThemeAcrossWindows()");
  });

  it("routes OS dark-mode flips through the same multi-window sync", () => {
    expect(mainSource).toContain(
      'nativeTheme.on("updated", syncThemeAcrossWindows)',
    );
  });

  it("broadcasts the preference on a dedicated channel", () => {
    expect(mainSource).toContain(
      'broadcastToAll("wuu:theme-preference-changed", getThemePreference())',
    );
  });

  it("re-pushes window background and the Windows overlay to themed windows", () => {
    expect(mainSource).toContain("win.setBackgroundColor(background)");
    expect(mainSource).toContain("win.setTitleBarOverlay(overlay)");
  });

  it("registers both the main window and pop-out windows for themed chrome", () => {
    // One definition plus two call sites (createWindow, createPopOutWindow).
    expect(mainSource.match(/registerThemedChromeWindow\(/g)).toHaveLength(3);
  });

  it("exposes a validated subscription with cleanup through the preload", () => {
    expect(preloadSource).toContain(
      'ipcRenderer.on("wuu:theme-preference-changed"',
    );
    expect(preloadSource).toContain(
      'ipcRenderer.removeListener("wuu:theme-preference-changed"',
    );
    expect(preloadSource).toContain("onThemePreferenceChange");
  });

  it("subscribes at renderer boot so every window follows the broadcast", () => {
    expect(rendererBoot).toContain("startThemePreferenceSync()");
  });
});
