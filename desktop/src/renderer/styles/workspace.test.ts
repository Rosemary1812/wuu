import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const workspaceCss = readFileSync(resolve(__dirname, "workspace.css"), "utf-8");

function cssRuleBody(selector: string): string {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  // The selector may be preceded by CSS comments between the previous rule's
  // closing brace and the new selector — \s* alone cannot cross `/* ... */`,
  // so we match any chars (lazy) up to the literal selector instead.
  const match = workspaceCss.match(
    new RegExp(`(?:^|\\})[\\s\\S]*?${escapedSelector}\\s*\\{([\\s\\S]*?)\\}`),
  );
  if (!match) {
    throw new Error(`missing CSS rule for ${selector}`);
  }
  return match[1];
}

describe("workspace right panel chrome", () => {
  it("uses a flat artifact rail that expands cleanly in full-panel mode", () => {
    const panel = cssRuleBody(".workspace-right-panel");
    const tab = cssRuleBody(".workspace-tool-tab");
    const active = cssRuleBody(".workspace-tool-tab.active");
    const indicator = cssRuleBody(".workspace-tool-tab::after");
    const activeEdge = cssRuleBody(".workspace-tool-tab.active::after");
    const spacer = cssRuleBody(".workspace-panel-tabbar-spacer");

    expect(panel).toMatch(/container-type:\s*inline-size;/);
    expect(tab).toMatch(/border:\s*0;/);
    expect(tab).toMatch(/border-radius:\s*var\(--radius-xs\);/);
    expect(tab).toMatch(/background:\s*transparent;/);
    expect(active).not.toMatch(/box-shadow:/);
    // The accent underline lives on the shared ::after pseudo so it can fade
    // in/out when the active tab changes instead of popping.
    expect(indicator).toMatch(/background:\s*var\(--wuu-accent\);/);
    expect(indicator).toMatch(/opacity:\s*0;/);
    expect(indicator).toMatch(/transition:\s*opacity\s+140ms\s+ease;/);
    expect(activeEdge).toMatch(/opacity:\s*1;/);
    expect(spacer).toMatch(/flex:\s*0\s+0\s+2px;/);
  });

  it("matches the visible session titlebar height so the pane headers align", () => {
    expect(cssRuleBody(".workspace-right-panel")).toMatch(
      /grid-template-rows:\s*48px\s+minmax\(0,\s*1fr\);/,
    );
    expect(cssRuleBody(".workspace-right-panel.detail")).toMatch(
      /grid-template-rows:\s*48px\s+minmax\(0,\s*1fr\);/,
    );
    expect(cssRuleBody(".workspace-panel-tabbar")).toMatch(/height:\s*48px;/);
  });

  it("keeps the docked panel tabbar drag-able when tabs are open", () => {
    expect(cssRuleBody(".workspace-panel-tabbar")).toMatch(
      /-webkit-app-region:\s*drag;/,
    );
    expect(cssRuleBody(".workspace-panel-tabbar button")).toMatch(
      /-webkit-app-region:\s*no-drag;/,
    );
    expect(cssRuleBody(".workspace-panel-tabbar button *")).toMatch(
      /-webkit-app-region:\s*no-drag;/,
    );
    // Opening a tab must not shrink the drag strip. The tab chip itself and
    // the gaps around it should stay drag-able; only the inner clickable
    // controls (label, close button) opt out via the button / button * rules.
    expect(cssRuleBody(".workspace-tool-tab")).not.toMatch(
      /-webkit-app-region:\s*no-drag;/,
    );
    expect(cssRuleBody(".workspace-panel-tabs")).not.toMatch(
      /-webkit-app-region:\s*no-drag;/,
    );
  });
});

describe("workspace file preview toolbar", () => {
  it("keeps the editor toolbar compact so the content area dominates", () => {
    // The toolbar hosts the dirty-state indicator, the markdown mode
    // switch, and the save button. Earlier padding pushed the chrome to
    // ~42px even for plain text files, which dwarfed the actual preview.
    const toolbar = cssRuleBody(".workspace-file-editor-toolbar");
    expect(toolbar).toMatch(/padding:\s*3px\s+10px;/);
    expect(toolbar).toMatch(/gap:\s*10px;/);

    // Save button and markdown mode switch should share one height so
    // their baselines line up without per-side vertical padding games.
    expect(cssRuleBody(".workspace-file-save-button")).toMatch(/height:\s*24px;/);
    expect(cssRuleBody(".workspace-markdown-mode-switch button")).toMatch(/height:\s*24px;/);
  });
});

describe("workspace file tree density", () => {
  it("uses compact editor-style rows, typography, and indentation", () => {
    const row = cssRuleBody(".workspace-file-tree-row");
    expect(row).toMatch(/height:\s*24px;/);
    expect(row).toMatch(/font-size:\s*var\(--font-sm\);/);
    expect(row).toMatch(/font-weight:\s*400;/);
    expect(row).toMatch(/grid-template-columns:\s*16px\s+16px\s+minmax\(0,\s*1fr\);/);
    expect(row).toMatch(/gap:\s*4px;/);
    expect(row).toMatch(/var\(--workspace-tree-depth,\s*0\)\s*\*\s*14px/);
    expect(row).toMatch(/border-radius:\s*var\(--radius-xs\);/);
  });

  it("keeps the search and surrounding tree chrome proportionate to the rows", () => {
    const search = cssRuleBody(".workspace-file-search");
    expect(search).toMatch(/height:\s*28px;/);
    expect(search).toMatch(/font-size:\s*var\(--font-sm\);/);
    expect(cssRuleBody(".workspace-file-tree-frame")).toMatch(/gap:\s*6px;/);
    expect(workspaceCss).toMatch(/(?:^|\n)\.workspace-file-panel\s*\{[^}]*gap:\s*8px;/);
  });
});

describe("workspace markdown reading prose", () => {
  it("gives heading levels a real outline instead of one flat tier", () => {
    // The file preview turns heading levels into distinct visual anchors.
    // Without this, every #/##/### collapses to the same small label and
    // long READMEs lose their structure.
    const h1 = cssRuleBody(".workspace-markdown-reading .rich-heading--h1");
    const h2 = cssRuleBody(".workspace-markdown-reading .rich-heading--h2");
    const h3 = cssRuleBody(".workspace-markdown-reading .rich-heading--h3");
    expect(h1).toMatch(/font-size:\s*28px;/);
    expect(h1).toMatch(/border-bottom:\s*1px\s+solid\s+var\(--rule\);/);
    expect(h2).toMatch(/font-size:\s*21px;/);
    expect(h3).toMatch(/font-size:\s*17px;/);
  });

  it("colors inline links with the accent and underlines only on hover", () => {
    // GitHub-style README links: a row of shields.io badges is dense
    // enough that always-underlined anchors look like a wall of strikethrough.
    const link = cssRuleBody(".workspace-markdown-reading .rich-link.rich-web-link");
    expect(link).toMatch(/color:\s*var\(--wuu-accent\);/);
    expect(link).toMatch(/text-decoration:\s*none;/);
    expect(
      cssRuleBody(".workspace-markdown-reading .rich-link.rich-web-link:hover"),
    ).toMatch(/text-decoration:\s*underline;/);
  });

  it("frames code blocks, tables, and blockquotes so they read as artifacts", () => {
    const codeBlock = cssRuleBody(".workspace-markdown-reading .rich-code-block");
    const tableWrap = cssRuleBody(".workspace-markdown-reading .rich-table-wrap");
    const blockquote = cssRuleBody(".workspace-markdown-reading .rich-blockquote");

    expect(codeBlock).toMatch(/border:\s*1px\s+solid\s+var\(--rule\);/);
    expect(codeBlock).toMatch(/border-radius:\s*8px;/);
    expect(tableWrap).toMatch(/border:\s*1px\s+solid\s+var\(--rule\);/);
    expect(blockquote).toMatch(/border-left:\s*3px\s+solid\s+var\(--wuu-accent\);/);
  });

  it("centers the README's <div align=\"center\"> badge row", () => {
    // rehype-raw passes the wrapper div through; the workspace scope
    // styles the legacy HTML attribute here instead of rewriting every
    // user's README to use markdown-only centering.
    expect(cssRuleBody('.workspace-markdown-reading div[align="center"]')).toMatch(
      /text-align:\s*center;/,
    );
  });
});

describe("workspace file preview layout", () => {
  it("lets the file preview fill the workspace viewport with its own editor scroller", () => {
    expect(cssRuleBody(".workspace-scroll-region")).toMatch(/overflow:\s*hidden;/);
    expect(cssRuleBody(".workspace-scroll-region .scroll-region-content")).toMatch(
      /height:\s*100%;/,
    );
    expect(cssRuleBody(".workspace-scroll-region .scroll-region-content")).toMatch(
      /min-height:\s*0;/,
    );
    expect(cssRuleBody(".workspace-file-preview")).toMatch(/height:\s*100%;/);
    expect(cssRuleBody(".workspace-file-preview")).toMatch(/min-height:\s*0;/);
    expect(cssRuleBody(".workspace-file-editor-scroll")).toMatch(/overflow:\s*hidden;/);
    expect(cssRuleBody(".workspace-monaco-editor")).toMatch(/height:\s*100%;/);
  });

  it("lays out file content first and the file tree as the right column", () => {
    const split = cssRuleBody(".workspace-files-split");
    expect(split).toMatch(/display:\s*grid;/);
    expect(split).toMatch(
      /grid-template-columns:\s*minmax\(0, 1fr\) 8px minmax\(180px, var\(--workspace-file-tree-width, 320px\)\);/,
    );
    expect(cssRuleBody(".workspace-files-resizer")).toMatch(/cursor:\s*col-resize;/);
    expect(cssRuleBody(".workspace-files-resizer::before")).toMatch(/width:\s*1px;/);
    expect(cssRuleBody(".workspace-files-tree .workspace-file-search")).toMatch(
      /width:\s*calc\(100% - 10px\);/,
    );
    expect(workspaceCss).not.toContain(".workspace-files-content-header");
    expect(cssRuleBody(".workspace-files-content-body")).toMatch(/height:\s*100%;/);
  });

  it("adds a restrained syntax palette for highlighted code tokens", () => {
    expect(cssRuleBody(".workspace-file-code .hljs-keyword")).toMatch(
      /color:\s*var\(--hljs-keyword\);/,
    );
    expect(cssRuleBody(".workspace-file-code .hljs-string")).toMatch(
      /color:\s*var\(--hljs-string\);/,
    );
    expect(cssRuleBody(".workspace-file-code .hljs-number")).toMatch(
      /color:\s*var\(--hljs-number\);/,
    );
    expect(cssRuleBody(".workspace-file-code .hljs-comment")).toMatch(
      /color:\s*var\(--hljs-comment\);/,
    );
  });
});

describe("workspace review diff layout", () => {
  it("keeps the diff pane primary and wraps long lines inside the available width", () => {
    expect(cssRuleBody(".workspace-review-panel.has-diff")).toMatch(
      /minmax\(0,\s*1fr\)[\s\S]*minmax\(220px,\s*min\(var\(--workspace-review-tree-width\),\s*calc\(100%\s*-\s*420px\)\)\)/,
    );

    const code = cssRuleBody(".workspace-diff-code");
    expect(code).toMatch(/width:\s*100%;/);
    expect(code).toMatch(/min-width:\s*0;/);
    expect(code).toMatch(/white-space:\s*normal;/);
    expect(code).not.toMatch(/min-width:\s*max-content;/);

    const line = cssRuleBody(".workspace-diff-line");
    expect(line).toMatch(/display:\s*block;/);
    expect(line).toMatch(/position:\s*relative;/);
    expect(line).toMatch(/min-width:\s*0;/);
    expect(line).toMatch(/padding:\s*0\s+18px\s+0\s+104px;/);
    expect(line).not.toMatch(/grid-template-columns:/);

    const lineNumber = cssRuleBody(".workspace-diff-line-number");
    expect(lineNumber).toMatch(/position:\s*absolute;/);
    expect(lineNumber).toMatch(/width:\s*52px;/);
    expect(cssRuleBody(".workspace-diff-line-number:first-child")).toMatch(/left:\s*0;/);
    expect(cssRuleBody(".workspace-diff-line-number:nth-child(2)")).toMatch(/left:\s*52px;/);

    const lineCode = cssRuleBody(".workspace-diff-line-code");
    expect(lineCode).toMatch(/display:\s*block;/);
    expect(lineCode).toMatch(/min-width:\s*0;/);
    expect(lineCode).toMatch(/white-space:\s*pre-wrap;/);
    expect(lineCode).toMatch(/overflow-wrap:\s*anywhere;/);
    expect(lineCode).toMatch(/word-break:\s*break-word;/);
  });

  it("lets a single-file review use the full panel width", () => {
    expect(cssRuleBody(".workspace-review-panel.single-file.has-diff")).toMatch(
      /grid-template-columns:\s*minmax\(0,\s*1fr\);/,
    );
    expect(
      cssRuleBody(".workspace-review-panel.single-file .workspace-review-tree-pane"),
    ).toMatch(/display:\s*none;/);
    expect(
      cssRuleBody(".workspace-review-panel.single-file .workspace-review-resizer"),
    ).toMatch(/display:\s*none;/);
  });

  it("highlights the active file in the review tree so the user can see which diff is on screen", () => {
    const activeRow = cssRuleBody(".workspace-diff-tree-row.active");
    const hoverRow = cssRuleBody(".workspace-diff-tree-row:hover");

    // Active row must NOT share the hover background — otherwise the user
    // can't tell which file's diff is currently being shown in the diff pane.
    expect(activeRow).not.toMatch(/background:\s*var\(--surface-3\)/);
    expect(activeRow).toMatch(/color-mix\(in srgb,\s*var\(--wuu-accent\)/);
    expect(hoverRow).toMatch(/background:\s*var\(--surface-3\)/);

    // The file icon and stat count in the active row should pick up the
    // accent so the row reads as "currently diffed" at a glance.
    expect(cssRuleBody(".workspace-diff-tree-row.active svg")).toMatch(
      /color:\s*var\(--wuu-accent\)/,
    );
    expect(
      cssRuleBody(".workspace-diff-tree-row.active .workspace-diff-tree-count"),
    ).toMatch(/color:\s*var\(--wuu-accent\)/);
  });

  it("keeps the review tree from eating too much horizontal space so file names don't get cut off", () => {
    // The chevron / icon columns and the per-level indent spacer are
    // sized so that a deeply nested path inside the 280px tree pane
    // still has room to render the file name + stat badge without
    // ellipsis. Going back to the wider values here would re-introduce
    // the truncation the user reported.
    expect(cssRuleBody(".workspace-diff-tree-row")).toMatch(
      /grid-template-columns:\s*14px\s+14px/,
    );
    expect(cssRuleBody(".workspace-diff-tree-row.file")).toMatch(
      /grid-template-columns:\s*14px\s+14px/,
    );
    expect(cssRuleBody(".workspace-diff-tree-spacer")).toMatch(/width:\s*12px;/);
  });
});

describe("turn file diff panel layout", () => {
  it("wraps turn diff lines inside the panel width instead of keeping a horizontal code width", () => {
    const body = cssRuleBody(".turn-file-diff-body");
    expect(body).toMatch(/overflow-y:\s*auto;/);
    expect(body).toMatch(/overflow-x:\s*hidden;/);

    const hunk = cssRuleBody(".turn-file-diff-body .tool-diff-hunk");
    expect(hunk).toMatch(/min-width:\s*0;/);
    expect(hunk).toMatch(/width:\s*100%;/);
    expect(hunk).not.toMatch(/min-width:\s*max-content;/);

    const line = cssRuleBody(".turn-file-diff-body .tool-diff-line");
    expect(line).toMatch(/display:\s*block;/);
    expect(line).toMatch(/position:\s*relative;/);
    expect(line).toMatch(/min-width:\s*0;/);
    expect(line).toMatch(/padding:\s*0\s+0\s+0\s+104px;/);
    expect(line).not.toMatch(/max-content/);

    const lineNumber = cssRuleBody(".turn-file-diff-body .tool-diff-line-number");
    expect(lineNumber).toMatch(/position:\s*absolute;/);
    expect(lineNumber).toMatch(/width:\s*52px;/);
    expect(cssRuleBody(".turn-file-diff-body .tool-diff-line-number-old")).toMatch(/left:\s*0;/);
    expect(cssRuleBody(".turn-file-diff-body .tool-diff-line-number-new")).toMatch(/left:\s*52px;/);

    const lineContent = cssRuleBody(".turn-file-diff-body .tool-diff-line-content");
    expect(lineContent).toMatch(/display:\s*block;/);
    expect(lineContent).toMatch(/min-width:\s*0;/);
    expect(lineContent).toMatch(/white-space:\s*pre-wrap;/);
    expect(lineContent).toMatch(/overflow-wrap:\s*anywhere;/);
    expect(lineContent).toMatch(/word-break:\s*break-word;/);
  });
});
