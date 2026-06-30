import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const sidebarCSS = readFileSync(resolve(__dirname, "styles/sidebar.css"), "utf-8");

function cssRule(selector: string): string {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = sidebarCSS.match(new RegExp(`^${escapedSelector}\\s*\\{([\\s\\S]*?)\\n\\}`, "m"));
  if (!match) {
    throw new Error(`missing CSS rule for ${selector}`);
  }
  return match[1] ?? "";
}

describe("project sidebar row layout", () => {
  it("keeps unread status and new-thread action on the same right-side axis", () => {
    expect(cssRule(".sidebar-content")).toMatch(/--sidebar-row-action-size:\s*24px/);

    const projectRow = cssRule(".project-row");
    expect(projectRow).toMatch(/grid-template-columns:[\s\S]*var\(--sidebar-row-action-size\)/);
    expect(projectRow).toMatch(/padding-right:\s*var\(--sidebar-row-pad-x\)/);

    expect(cssRule(".project-row-unread")).toMatch(/justify-self:\s*center/);
    expect(cssRule(".project-row .project-row-loading")).toMatch(/justify-self:\s*center/);
    expect(cssRule(".project-row-new-thread")).toMatch(/right:\s*var\(--sidebar-row-pad-x,\s*8px\)/);
    expect(cssRule(".sidebar-row-icon-button")).toMatch(/width:\s*var\(--sidebar-row-action-size,\s*24px\)/);
  });
});
