import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

// Same contract style as WindowMaterial.test.ts: the window-chrome split
// is a cross-file agreement (main process option branches, preload stamp,
// CSS corner reservations), so pin the load-bearing lines in each file.

const read = (path: string): string =>
  readFileSync(resolve(process.cwd(), path), "utf8");

const mainSource = read("src/main/index.ts");
const preloadSource = read("src/main/preload.ts");
const baseCSS = read("src/renderer/styles/base.css");
const sidebarCSS = read("src/renderer/styles/sidebar.css");
const settingsCSS = read("src/renderer/styles/settings.css");
const shellCSS = read("src/renderer/styles/conversation-shell.css");
const workspaceCSS = read("src/renderer/styles/workspace.css");
const themeCSS = read("src/renderer/styles/theme.css");

describe("window chrome platform branches", () => {
  it("keeps hiddenInset and the traffic lights exclusive to the darwin branch", () => {
    // Exactly one declaration each — inside windowFrameOptions' darwin
    // branch. A second occurrence means a window factory regressed to a
    // hardcoded mac-only chrome. (The bare identifier also appears in the
    // options Pick<>, so match the value assignments.)
    expect(mainSource.match(/titleBarStyle: "hiddenInset"/g)).toHaveLength(1);
    expect(mainSource.match(/trafficLightPosition: \{/g)).toHaveLength(1);
  });

  it("gives Windows a hidden titlebar with a themed controls overlay", () => {
    expect(mainSource).toContain('titleBarStyle: "hidden"');
    expect(mainSource).toContain("titleBarOverlay: windowsTitleBarOverlay()");
    // Theme flips re-push overlay colors to every open overlay window.
    expect(mainSource).toContain('nativeTheme.on("updated", syncThemeAcrossWindows)');
  });

  it("stamps data-platform before first paint, like data-theme", () => {
    expect(preloadSource).toContain("document.documentElement.dataset.platform");
  });
});

describe("window controls corner reservations", () => {
  it("declares both insets and flips them per platform in base.css", () => {
    expect(baseCSS).toMatch(/--window-controls-inset-left:\s*86px;/);
    expect(baseCSS).toMatch(/--window-controls-inset-right:\s*0px;/);
    expect(baseCSS).toMatch(
      /:root\[data-platform="win32"\]\s*\{[\s\S]*?--window-controls-inset-left:\s*0px;/,
    );
    expect(baseCSS).toMatch(/env\(titlebar-area-width, 100vw\)/);
  });

  it("routes every traffic-light reservation through the left inset", () => {
    // No strip may hardcode the macOS 86px reservation anymore.
    for (const css of [sidebarCSS, settingsCSS, shellCSS, workspaceCSS]) {
      expect(css).not.toMatch(/padding-left:\s*86px/);
    }
    expect(sidebarCSS).toContain("max(24px, var(--window-controls-inset-left))");
    expect(settingsCSS).toContain("max(24px, var(--window-controls-inset-left))");
  });

  it("clears the Windows overlay on the strips that touch the top-right corner", () => {
    expect(shellCSS).toContain("max(24px, var(--window-controls-inset-right))");
    expect(settingsCSS).toContain("max(24px, var(--window-controls-inset-right))");
    expect(workspaceCSS).toContain("max(8px, var(--window-controls-inset-right))");
  });

  it("gives the win32 sidebar an opaque fill in both themes", () => {
    expect(sidebarCSS).toMatch(
      /:root\[data-platform="win32"\]\s*\{[\s\S]*?--sidebar-material-fill:/,
    );
    expect(themeCSS).toMatch(
      /:root\[data-platform="win32"\]\[data-theme="dark"\]\s*\{[\s\S]*?--sidebar-material-fill:/,
    );
  });
});
