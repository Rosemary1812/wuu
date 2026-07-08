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
    expect(cssRuleBody(".workspace-file-preview.editor-only")).toMatch(
      /grid-template-rows:\s*minmax\(0,\s*1fr\);/,
    );
    expect(cssRuleBody(".workspace-file-editor-scroll")).toMatch(/overflow:\s*hidden;/);
    expect(cssRuleBody(".workspace-code-editor,\n.workspace-monaco-editor")).toMatch(/height:\s*100%;/);
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
