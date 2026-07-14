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

  it("aligns thread list footer text with the navigation body column", () => {
    expect(cssRule(".sidebar-content")).toMatch(/--sidebar-row-control-pad-x:\s*8px/);
    expect(cssRule(".thread-list-footer")).toMatch(
      /padding-left:\s*calc\([\s\S]*var\(--sidebar-nav-icon-col\)[\s\S]*var\(--sidebar-nav-column-gap\)[\s\S]*-\s*var\(--sidebar-row-control-pad-x\)[\s\S]*\)/,
    );
    expect(cssRule(".thread-list-more")).toMatch(/padding:\s*0 var\(--sidebar-row-control-pad-x\)/);
    expect(cssRule(".thread-list-collapse-btn")).toMatch(/padding:\s*0 var\(--sidebar-row-control-pad-x\)/);
  });
});

describe("globalized right panel chrome", () => {
  it("animates docked and full-panel layouts with the shared structural motion", () => {
    const body = cssRule(".app-shell.right-panel-animating");

    expect(body).toMatch(/grid-template-columns/);
    expect(body).toMatch(/var\(--workspace-panel-motion-duration,\s*240ms\)/);
    expect(body).toMatch(/var\(--workspace-panel-motion-ease\)/);
  });

  it("keeps the sidebar available as a drawer over the focused workspace", () => {
    const conversation = cssRule(
      ".app-shell.right-panel-globalized .conversation-pane",
    );

    expect(conversation).toMatch(/overflow:\s*hidden;/);
    expect(conversation).toMatch(/pointer-events:\s*none;/);
    expect(sidebarCSS).not.toMatch(
      /\.app-shell\.right-panel-globalized \.sidebar,\s*\n\.app-shell\.right-panel-globalized \.conversation-pane/,
    );
  });

  it("hides the docked right-panel resizer while the panel fills the window", () => {
    const body = cssRule(
      ".app-shell.right-panel-globalized .workspace-right-panel-resizer",
    );

    expect(body).toMatch(/display:\s*none;/);
    expect(body).toMatch(/pointer-events:\s*none;/);
  });

  it("reserves traffic-light space only when a collapsed-sidebar right panel reaches the window edge", () => {
    const body = cssRule(
      ".app-shell.sidebar-collapsed.right-panel-open:not(.right-panel-globalized) .workspace-panel-tabbar",
    );

    expect(body).toMatch(
      /padding-left:\s*clamp\(\s*10px,\s*calc\(86px\s*-\s*\(100vw\s*-\s*var\(--workspace-right-panel-width,\s*360px\)\)\),\s*86px\s*\);/,
    );
    expect(body).not.toMatch(/padding-left:\s*86px;/);
  });
});

describe("panel resizer feedback", () => {
  it("lights the full height of both sidebar edges", () => {
    expect(cssRule(".sidebar-resizer::before")).toMatch(/inset:\s*0;/);
    expect(cssRule(".workspace-right-panel-resizer::before")).toMatch(/inset:\s*0;/);
  });
});
