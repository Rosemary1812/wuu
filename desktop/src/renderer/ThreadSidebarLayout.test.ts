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
    expect(body).toMatch(/var\(--workspace-panel-motion-duration\)/);
    expect(body).toMatch(/var\(--workspace-panel-motion-ease\)/);

    // Globalize no longer squeezes the grid (that rewrapped the conversation
    // and relaid the panel content on every frame). The panel is promoted to
    // a full-window fixed sheet, laid out at final width from the first
    // frame, sliding transform-only between its dock slot and the window.
    expect(sidebarCSS).not.toMatch(
      /\.app-shell\.right-panel-globalized\s*\{[^}]*grid-template-columns/,
    );
    // Enter timing lives on the open state, exit timing on parked; the
    // arming/docking teleport states are transition-free; the sheet parks
    // exactly over its dock slot (translate by 100% - dock width).
    expect(sidebarCSS).toMatch(
      /\[data-sheet="open"\]\s*\{[^}]*transition:\s*transform\s+var\(--sheet-enter-duration\)\s+var\(--sheet-enter-easing\)/,
    );
    expect(sidebarCSS).toMatch(
      /\[data-sheet="parked"\]\s*\{[^}]*transition:\s*transform\s+var\(--sheet-exit-duration\)\s+var\(--sheet-exit-easing\)/,
    );
    expect(sidebarCSS).toMatch(
      /calc\(100% - var\(--workspace-right-panel-width, 360px\)\)/,
    );
    expect(sidebarCSS).toMatch(
      /\[data-sheet="arming"\],\s*\n\.app-shell \.workspace-right-panel\[data-sheet="docking"\]\s*\{\s*\n\s*transition:\s*none;/,
    );
    expect(sidebarCSS).toMatch(
      /\[data-sheet="parked"\]\s*\{\s*\n\s*position:\s*fixed;\s*\n\s*inset:\s*0;/,
    );
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

    const toggleRegion = cssRule(".globalized-sidebar-toggle-region");
    expect(toggleRegion).toMatch(
      /top:\s*calc\(\(48px - 1px - 30px\) \/ 2\);/,
    );
    expect(toggleRegion).toMatch(/z-index:\s*150;/);
    expect(toggleRegion).toMatch(/-webkit-app-region:\s*drag;/);

    const toggle = cssRule(
      ".globalized-sidebar-toggle-region .globalized-sidebar-toggle",
    );
    expect(toggle).toMatch(/width:\s*30px;/);
    expect(toggle).toMatch(/height:\s*30px;/);
    expect(toggle).toMatch(/-webkit-app-region:\s*no-drag;/);
    expect(cssRule(".globalized-sidebar-toggle *")).toMatch(
      /-webkit-app-region:\s*no-drag;/,
    );

    const tabbar = cssRule(
      ".app-shell.right-panel-globalized .workspace-panel-tabbar",
    );
    expect(tabbar).toMatch(
      /padding-left:\s*max\(8px, var\(--window-controls-inset-left\)\);/,
    );
    const hitHole = cssRule(".workspace-panel-sidebar-hit-hole");
    expect(hitHole).toMatch(/flex:\s*0 0 38px;/);
    expect(hitHole).toMatch(/-webkit-app-region:\s*no-drag;/);
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

    // The 86px reservation now rides --window-controls-inset-left so it
    // collapses to the neutral 10px on platforms without traffic lights.
    expect(body).toMatch(
      /padding-left:\s*clamp\(\s*10px,\s*calc\(\s*var\(--window-controls-inset-left\)\s*-\s*\(100vw\s*-\s*var\(--workspace-right-panel-width,\s*360px\)\)\s*\),\s*max\(10px,\s*var\(--window-controls-inset-left\)\)\s*\);/,
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
