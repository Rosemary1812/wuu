import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const workspaceCss = readFileSync(resolve(__dirname, "workspace.css"), "utf-8");

function cssRuleBody(selector: string): string {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = workspaceCss.match(
    new RegExp(`(?:^|\\})\\s*${escapedSelector}\\s*\\{([\\s\\S]*?)\\}`),
  );
  if (!match) {
    throw new Error(`missing CSS rule for ${selector}`);
  }
  return match[1];
}

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
});
