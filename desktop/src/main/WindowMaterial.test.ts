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
  it("keeps the native sidebar material clear instead of covering it with a white veil", () => {
    expect(mainSource).toContain('vibrancy: "sidebar"');
    expect(sidebarCSS).toMatch(
      /--sidebar-material-fill:\s*rgba\(255, 255, 255, 0\.12\);/,
    );
    expect(sidebarCSS).toMatch(
      /\.sidebar\s*\{[\s\S]*?background:\s*var\(--sidebar-material-fill\);/,
    );
    expect(sidebarCSS).toMatch(
      /\.sidebar::before\s*\{[\s\S]*?opacity:\s*0\.42;/,
    );
    expect(sidebarCSS).toMatch(
      /\.sidebar\s*\{[\s\S]*?backdrop-filter:\s*none;/,
    );
  });

  it("adds a dark fill when the app theme cannot change the native macOS material", () => {
    expect(themeCSS).toMatch(
      /:root\[data-theme="dark"\][\s\S]*?--sidebar-material-fill:\s*rgba\(20, 22, 24, 0\.9\);/,
    );
  });
});
