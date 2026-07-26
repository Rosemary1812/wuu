import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const workspaceCss = readFileSync(resolve(__dirname, "workspace.css"), "utf-8");
const workspacePdfCss = readFileSync(
  resolve(__dirname, "workspace-pdf-preview.css"),
  "utf-8",
);

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

describe("conversation message-flow rhythm", () => {
  it("uses one explicit top landmark without adding a structural row gap", () => {
    const flow = cssRuleBody(".conversation-width");

    expect(flow).toMatch(
      /padding-block:\s*var\(--conversation-flow-top-gap\)\s+32px;/,
    );
    expect(flow).toMatch(/row-gap:\s*0;/);
    expect(flow).not.toMatch(/var\(--wuu-grid-leading\)/);
  });

  it("pins split composers to one visible input row instead of the browser default two", () => {
    const textarea = cssRuleBody(".split-composer textarea");

    expect(textarea).toMatch(/height:\s*60px;/);
    expect(textarea).toMatch(/min-height:\s*60px;/);
  });

  it("keeps the composer clearance and debug baseline on the same landmarks", () => {
    expect(
      cssRuleBody(
        ".scroll-region:not(.empty-scroll-region):not(.workspace-scroll-region) .conversation-width",
      ),
    ).toMatch(
      /var\(--dock-composer-height\)\s*\+\s*var\(--conversation-composer-clearance\)/,
    );
    expect(cssRuleBody(".conversation-grid-rows")).toMatch(
      /top:\s*var\(--conversation-flow-top-gap\);/,
    );
  });
});

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
    expect(indicator).toMatch(
      /transition:\s*opacity\s+var\(--motion-fast\)\s+var\(--ease-out\);/,
    );
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

describe("workspace document turn glass", () => {
  it("keeps the document composer dock aligned with the main conversation composer", () => {
    const dock = cssRuleBody(".workspace-document-turn-dock");

    expect(dock).toMatch(/width:\s*min\(100%,\s*var\(--session-composer-width\)\);/);
    expect(dock).toMatch(/margin:\s*0 auto;/);
  });

  it("makes only the turn drawer glass while leaving the Composer surface alone", () => {
    const drawer = cssRuleBody(".workspace-document-turn-drawer");
    const summary = cssRuleBody(".workspace-document-turn-summary");
    const details = cssRuleBody(".workspace-document-turn-details");
    const composerFrame = cssRuleBody(".workspace-document-composer .composer-frame");

    expect(drawer).toMatch(/width:\s*calc\(100% - 48px\);/);
    expect(drawer).toMatch(/margin:\s*0 auto -6px;/);
    expect(drawer).toMatch(/background:\s*radial-gradient\(/);
    expect(drawer).toMatch(/linear-gradient\(/);
    expect(drawer).toMatch(/var\(--paper\) 42%, transparent/);
    expect(drawer).toMatch(/var\(--surface-2\) 16%, transparent/);
    expect(drawer).toMatch(/backdrop-filter:\s*blur\(24px\)\s+saturate\(1\.18\);/);
    expect(summary).toMatch(/background:\s*transparent;/);
    expect(details).toMatch(/background:\s*transparent;/);
    expect(composerFrame).toMatch(/0 18px 54px rgba\(20, 24, 28, 0\.14\)/);
    expect(workspaceCss).not.toContain(
      ".workspace-document-composer .document-composer-wrap .composer-frame",
    );
  });
});

describe("workspace readonly file preview", () => {
  it("removes editing chrome and gives the content the full available height", () => {
    expect(workspaceCss).not.toContain(".workspace-file-editor-toolbar");
    expect(workspaceCss).not.toContain(".workspace-file-save-button");
    expect(workspaceCss).not.toContain(".workspace-markdown-mode-switch");
    expect(cssRuleBody(".workspace-file-preview.readonly")).toMatch(
      /grid-template-rows:\s*minmax\(0, 1fr\);/,
    );
  });
});

describe("workspace file tree density", () => {
  it("lets the library-owned tree fill the available panel", () => {
    expect(workspaceCss).toMatch(/(?:^|\n)\.workspace-file-panel\s*\{[^}]*display:\s*flex;/);
    expect(workspaceCss).toMatch(/(?:^|\n)\.workspace-file-panel\s*\{[^}]*flex-direction:\s*column;/);
    expect(cssRuleBody(".workspace-file-tree-frame")).toMatch(/flex:\s*1 1 auto;/);
    expect(cssRuleBody(".workspace-file-tree-frame")).toMatch(/contain:\s*strict;/);
  });

  it("removes the duplicated custom tree row and search styling", () => {
    expect(workspaceCss).not.toContain(".workspace-file-tree-row");
    expect(workspaceCss).not.toContain(".workspace-file-search");
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

  it("uses the message-stream link palette and underlines only on hover", () => {
    // GitHub-style README links: a row of shields.io badges is dense
    // enough that always-underlined anchors look like a wall of strikethrough.
    const link = cssRuleBody(".workspace-markdown-reading .rich-link.rich-web-link");
    expect(link).toMatch(/color:\s*var\(--rich-link-color\);/);
    expect(link).toMatch(/text-decoration:\s*none;/);
    const hoveredLink = cssRuleBody(
      ".workspace-markdown-reading .rich-link.rich-web-link:hover",
    );
    expect(hoveredLink).toMatch(/color:\s*var\(--rich-link-hover-color\);/);
    expect(hoveredLink).toMatch(/text-decoration:\s*underline;/);
  });

  it("uses the shared theme-aware chip only for inline code", () => {
    const inlineCode = cssRuleBody(
      ".workspace-markdown-reading .rich-content :not(pre) > code",
    );

    expect(inlineCode).toMatch(/background:\s*var\(--surface-chip\);/);
    expect(workspaceCss).not.toMatch(/\.workspace-markdown-reading\s+code\s*\{/);
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
    expect(cssRuleBody(".workspace-file-editor-scroll.markdown-reading")).not.toMatch(
      /scrollbar-gutter:/,
    );
    expect(cssRuleBody(".workspace-monaco-editor")).toMatch(/height:\s*100%;/);
  });

  it("keeps PDF chrome on the app palette while preserving white document pages", () => {
    expect(workspacePdfCss).toMatch(
      /\.workspace-pdf-shell\s*\{[^}]*background:\s*var\(--paper\);/s,
    );
    expect(workspacePdfCss).toMatch(
      /\.workspace-pdf-container\s*\{[^}]*background:\s*var\(--paper\);/s,
    );
    expect(workspacePdfCss).toMatch(
      /\.pdfViewer \.page\s*\{[^}]*background-color:\s*#fff;/s,
    );
  });

  it("makes Monaco use the renderer-wide scrollbar palette", () => {
    const slider = cssRuleBody(
      ".workspace-monaco-editor .monaco-scrollable-element > .scrollbar > .slider",
    );
    const visibleSlider = cssRuleBody(
      ".workspace-monaco-editor:hover .monaco-scrollable-element > .scrollbar > .slider",
    );
    const hoveredSlider = cssRuleBody(
      ".workspace-monaco-editor .monaco-scrollable-element > .scrollbar > .slider:hover",
    );
    const activeSlider = cssRuleBody(
      ".workspace-monaco-editor .monaco-scrollable-element > .scrollbar > .slider.active",
    );

    expect(slider).toMatch(/background:\s*transparent;/);
    expect(slider).toMatch(/border-radius:\s*var\(--scrollbar-radius\);/);
    expect(visibleSlider).toMatch(/background:\s*var\(--scrollbar-thumb\);/);
    expect(hoveredSlider).toMatch(/background:\s*var\(--scrollbar-thumb-hover\);/);
    expect(activeSlider).toMatch(/background:\s*var\(--scrollbar-thumb-active\);/);
  });

  it("supports right, left, and collapsed file-tree layouts", () => {
    const split = cssRuleBody(".workspace-files-split");
    expect(split).toMatch(/display:\s*grid;/);
    expect(cssRuleBody('.workspace-files-split[data-tree-side="right"]')).toMatch(
      /grid-template-columns:\s*minmax\(0, 1fr\) minmax\(180px, var\(--workspace-file-tree-width, 320px\)\);/,
    );
    expect(cssRuleBody('.workspace-files-split[data-tree-side="left"]')).toMatch(
      /grid-template-columns:\s*minmax\(180px, var\(--workspace-file-tree-width, 320px\)\) minmax\(0, 1fr\);/,
    );
    expect(cssRuleBody(".workspace-files-split.tree-hidden")).toMatch(
      /grid-template-columns:\s*minmax\(0, 1fr\);/,
    );
    const resizer = cssRuleBody(".workspace-files-resizer");
    expect(resizer).toMatch(/cursor:\s*col-resize;/);
    expect(cssRuleBody('.workspace-files-split[data-tree-side="right"] .workspace-files-resizer')).toMatch(
      /grid-column:\s*2;[\s\S]*justify-self:\s*start;/,
    );
    expect(cssRuleBody('.workspace-files-split[data-tree-side="left"] .workspace-files-resizer')).toMatch(
      /grid-column:\s*1;[\s\S]*justify-self:\s*end;/,
    );
    expect(cssRuleBody('.workspace-files-split[data-tree-side="left"] .workspace-files-tree')).toMatch(
      /padding:\s*10px 0;/,
    );
    const resizerRule = cssRuleBody(".workspace-files-resizer::before");
    expect(resizerRule).toMatch(/inset:\s*0 auto 0 0;/);
    expect(resizerRule).toMatch(/width:\s*1px;/);
    expect(cssRuleBody(".workspace-file-tree-drag-handle")).toMatch(/cursor:\s*grab;/);
    const dockGuide = cssRuleBody(".workspace-files-split.tree-dragging::after");
    expect(dockGuide).toMatch(/width:\s*2px;/);
    expect(dockGuide).toMatch(/height:\s*72px;/);
    expect(dockGuide).toMatch(/top:\s*50%;/);
    const dragPreview = cssRuleBody(".workspace-file-tree-drag-preview");
    expect(dragPreview).toMatch(/width:\s*168px;/);
    expect(dragPreview).toMatch(/height:\s*44px;/);
    expect(dragPreview).toMatch(/border-radius:\s*12px;/);
    expect(cssRuleBody(".workspace-file-tree-reveal")).toMatch(/position:\s*absolute;/);
    expect(cssRuleBody(".workspace-file-tree-reveal.left")).toMatch(/left:\s*8px;/);
    expect(cssRuleBody(".workspace-file-tree-reveal.right")).toMatch(/right:\s*8px;/);
    expect(
      cssRuleBody(".workspace-files-tree[hidden],\n.workspace-files-resizer[hidden]"),
    ).toMatch(/display:\s*none;/);
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
    // Both rows use --surface-* tints so the file tree and review tree read
    // as the same component family; active keeps its accent fill.
    expect(activeRow).not.toMatch(/background:\s*var\(--surface-2\)/);
    expect(activeRow).toMatch(/color-mix\(in srgb,\s*var\(--wuu-accent\)/);
    expect(hoverRow).toMatch(/background:\s*var\(--surface-2\)/);

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

describe("automation detail rhythm", () => {
  it("keeps the task prompt compact and bounds manual resizing", () => {
    const textarea = cssRuleBody(".automation-detail-form textarea.settings-textarea");
    expect(textarea).toMatch(/min-height:\s*96px;/);
    expect(textarea).toMatch(/max-height:\s*220px;/);
    expect(textarea).toMatch(/resize:\s*vertical;/);
  });

  it("uses a plain status dot without an outer halo", () => {
    const status = cssRuleBody(".automation-state");
    expect(status).toMatch(/background:\s*var\(--success\);/);
    expect(status).toMatch(/box-shadow:\s*none;/);
  });
});
