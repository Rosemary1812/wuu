import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const mainSource = readFileSync(resolve(process.cwd(), "src/main/index.ts"), "utf8");
const sidebarCSS = readFileSync(
  resolve(process.cwd(), "src/renderer/styles/sidebar.css"),
  "utf8",
);
const themeCSS = readFileSync(
  resolve(process.cwd(), "src/renderer/styles/theme.css"),
  "utf8",
);

describe("macOS sidebar material", () => {
  it("lets the native sidebar material sample the wallpaper without a white veil", () => {
    expect(mainSource).toContain('vibrancy: "sidebar"');
    expect(sidebarCSS).toMatch(
      /--sidebar-material-fill:\s*rgba\(255, 255, 255, 0\.025\);/,
    );
    expect(sidebarCSS).toMatch(
      /\.sidebar,\s*\n\.settings-sidebar\s*\{[\s\S]*?background:\s*var\(--sidebar-material-fill\);/,
    );
    expect(sidebarCSS).toMatch(
      /\.sidebar::before,\s*\n\.settings-sidebar::before\s*\{[\s\S]*?opacity:\s*0\.5;/,
    );
    expect(sidebarCSS).toMatch(
      /\.sidebar,\s*\n\.settings-sidebar\s*\{[\s\S]*?backdrop-filter:\s*none;/,
    );
  });

  it("uses a stable dark surface when the native sidebar material is light", () => {
    expect(themeCSS).toMatch(
      /:root\[data-theme="dark"\][\s\S]*?--sidebar-material-fill:\s*rgba\(16, 18, 21, 0\.88\);/,
    );
  });
});
