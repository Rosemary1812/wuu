import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const settingsCSS = readFileSync(resolve(__dirname, "styles/settings.css"), "utf-8");

function cssRule(selector: string): string {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = settingsCSS.match(new RegExp(`^${escapedSelector}\\s*\\{([\\s\\S]*?)\\n\\}`, "m"));
  if (!match) {
    throw new Error(`missing CSS rule for ${selector}`);
  }
  return match[1] ?? "";
}

describe("settings usage responsive layout", () => {
  it("keeps the model table out of a nested horizontal scroller", () => {
    expect(cssRule(".settings-usage-table-wrap")).toMatch(/overflow:\s*visible;/);
    expect(cssRule(".settings-usage-table-wrap")).not.toMatch(/overflow-x:\s*auto;/);
    expect(cssRule(".settings-usage-table")).not.toMatch(/min-width:\s*640px;/);
  });

  it("folds model metrics when the usage content itself gets narrow", () => {
    expect(cssRule(".settings-usage-page")).toMatch(/container-type:\s*inline-size;/);
    expect(settingsCSS).toMatch(
      /@container settings-usage \(max-width:\s*520px\)[\s\S]*?\.settings-usage-table tbody tr\s*\{[\s\S]*?grid-template-columns:\s*repeat\(3,\s*minmax\(0,\s*1fr\)\);/,
    );
    expect(settingsCSS).toMatch(
      /\.settings-usage-table tbody td:nth-child\(2\)::before\s*\{[\s\S]*?content:\s*"输入";/,
    );
  });

  it("keeps usage values compact and truncates them as a final fallback", () => {
    expect(cssRule(".settings-usage-stat-value")).toMatch(/font-size:\s*18px;/);
    expect(cssRule(".settings-usage-stat-value")).toMatch(/text-overflow:\s*ellipsis;/);
    expect(cssRule(".settings-usage-stat-value")).toMatch(/white-space:\s*nowrap;/);
    expect(cssRule(".settings-usage-number")).toMatch(/max-width:\s*100%;/);
    expect(cssRule(".settings-usage-number")).toMatch(/text-overflow:\s*ellipsis;/);
  });

  it("uses a shared shimmer for the usage skeleton instead of isolated gray blocks", () => {
    expect(settingsCSS).toMatch(/@keyframes settings-usage-skeleton-shimmer/);
    expect(settingsCSS).toMatch(
      /\.settings-usage-skeleton-line,[\s\S]*?background:\s*linear-gradient[\s\S]*?background-size:\s*200%\s+100%/,
    );
    expect(cssRule(".settings-usage-skeleton-grid")).toMatch(/grid-template-rows:\s*repeat\(7/);
  });
});
