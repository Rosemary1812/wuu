import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { WuuDesktopApi } from "../shared/protocol";
import { SkillsCatalog } from "./SkillsCatalog";

let container: HTMLDivElement;
let root: Root | null = null;

beforeEach(() => {
  container = document.createElement("div");
  document.body.appendChild(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  root = null;
  container.remove();
  delete (globalThis as { wuu?: WuuDesktopApi }).wuu;
  delete (window as { wuu?: WuuDesktopApi }).wuu;
});

describe("SkillsCatalog", () => {
  it("lists Claude agent templates without repeating the section label", async () => {
    const stub: Partial<WuuDesktopApi> = {
      listSkills: vi.fn().mockResolvedValue({ skills: [] }),
      listAgentTemplates: vi.fn().mockResolvedValue({
        templates: [
          {
            name: "reviewer",
            description: "Review a change",
            instructions: "Inspect the diff.",
            source: "project",
            path: "/repo/.claude/agents/reviewer.md",
            permission_mode: "plan",
          },
        ],
        diagnostics: [
          {
            path: "/repo/.claude/agents/broken.md",
            message: "invalid frontmatter",
          },
        ],
      }),
    };
    (globalThis as { wuu?: WuuDesktopApi }).wuu = stub as WuuDesktopApi;
    (window as unknown as { wuu: WuuDesktopApi }).wuu = stub as WuuDesktopApi;

    await act(async () => {
      root = createRoot(container);
      root.render(<SkillsCatalog />);
    });

    expect(container.textContent).toContain("reviewer");
    expect(container.textContent).toContain("Agent 模板");
    expect(container.textContent).not.toContain("临时子代理模板");
    expect(container.textContent).toContain("invalid frontmatter");
  });

  it("lists installed plugins and tags plugin-provided skills", async () => {
    const stub: Partial<WuuDesktopApi> = {
      listSkills: vi.fn().mockResolvedValue({
        skills: [
          {
            name: "cua-mac",
            description: "Observe and control native macOS apps",
            source: "plugin:cua-mac",
            path: "/bundle/plugins/cua-mac/skills/cua-mac/SKILL.md",
          },
        ],
      }),
      listAgentTemplates: vi.fn().mockResolvedValue({ templates: [], diagnostics: [] }),
    };
    (globalThis as { wuu?: WuuDesktopApi }).wuu = stub as WuuDesktopApi;
    (window as unknown as { wuu: WuuDesktopApi }).wuu = stub as WuuDesktopApi;

    await act(async () => {
      root = createRoot(container);
      root.render(
        <SkillsCatalog
          extensionInventory={[
            {
              id: "plugin:user:cua-mac",
              name: "cua-mac",
              description: "Control macOS apps through Accessibility.",
              kind: "plugin",
              provenance: {
                kind: "plugin",
                source: "wuu",
                scope: "user",
                plugin_id: "cua-mac",
                official: true,
              },
              state: "read_only",
            },
            {
              id: "mcp:plugin:cua-mac:computer",
              name: "computer",
              kind: "mcp",
              provenance: {
                kind: "mcp",
                source: "plugin:cua-mac",
                scope: "user",
                plugin_id: "cua-mac",
              },
              state: "granted",
            },
          ]}
        />,
      );
    });

    expect(container.textContent).toContain("插件");
    expect(container.textContent).toContain("Control macOS apps through Accessibility.");
    expect(container.textContent).toContain("官方");
    expect(container.textContent).toContain("插件 · cua-mac");
    // Non-plugin inventory records (the plugin's MCP server) stay out of the
    // plugin list.
    expect(container.textContent).not.toContain("computer");
  });
});
