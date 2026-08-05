import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ExtensionInventoryRecord, SkillSummary, WuuDesktopApi } from "../shared/protocol";
import { SkillsCatalog } from "./SkillsCatalog";

vi.mock("./RichContent", () => ({
  RichContent: ({ text }: { text: string }) => <div data-testid="rich-content">{text}</div>,
}));

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
  it("refreshes the complete extension catalog through the parent runtime", async () => {
    installSkillList([]);
    const refreshedSkills: SkillSummary[] = [{
      name: "fresh-skill",
      description: "Discovered after refresh",
      source: "plugin:fresh",
      user_invocable: true,
      disable_model_invoke: false,
    }];
    const onRefreshCatalog = vi.fn().mockResolvedValue(refreshedSkills);

    await act(async () => {
      root = createRoot(container);
      root.render(<SkillsCatalog onRefreshCatalog={onRefreshCatalog} />);
    });
    await act(async () => {
      container.querySelector<HTMLButtonElement>(".catalog-refresh")?.click();
    });

    expect(onRefreshCatalog).toHaveBeenCalledOnce();
    expect(container.textContent).toContain("fresh-skill");
  });

  it("separates official and personal skills and gives each a complete artwork", async () => {
    installSkillList([
      {
        name: "browser",
        description: "Navigate and observe web pages. Use when no safer interface is available.",
        source: "bundled",
        user_invocable: true,
        disable_model_invoke: false,
      },
      {
        name: "write",
        description: "Rewrite prose",
        source: "user",
        user_invocable: true,
        disable_model_invoke: false,
      },
    ]);

    await act(async () => {
      root = createRoot(container);
      root.render(<SkillsCatalog />);
    });

    expect(container.textContent).toContain("官方技能");
    expect(container.textContent).toContain("你的技能");
    expect(container.querySelector(".catalog-search input[type=\"search\"]")).toBeTruthy();
    expect(container.textContent).toContain("Navigate and observe web pages.");
    expect(container.textContent).not.toContain("Use when no safer interface is available.");
    expect(container.querySelector('[data-skill-artwork="official-browser"]')).toBeTruthy();
    const customArtwork = container.querySelector('[data-skill-artwork="custom-skill"]');
    expect(customArtwork).toBeTruthy();
    expect(customArtwork?.className).toContain("skill-artwork-palette-");
    expect(customArtwork?.getAttribute("data-skill-motif")).toBeTruthy();
    expect(customArtwork?.querySelector(".lucide-wrench")).toBeNull();
  });

  it("lists installed plugins and tags plugin-provided skills", async () => {
    installSkillList([
      {
        name: "cua-mac",
        description: "Observe and control native macOS apps",
        source: "plugin:cua-mac",
        path: "/bundle/plugins/cua-mac/skills/cua-mac/SKILL.md",
        user_invocable: true,
        disable_model_invoke: false,
      },
    ]);

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
            {
              id: "plugin:user:community-tools",
              name: "community-tools",
              description: "Community-maintained utilities.",
              kind: "plugin",
              provenance: {
                kind: "plugin",
                source: "community",
                scope: "user",
                plugin_id: "community-tools",
                official: false,
              },
              state: "read_only",
            },
          ]}
        />,
      );
    });

    expect(container.textContent).toContain("插件");
    expect(container.textContent).toContain("Control macOS apps through Accessibility.");
    expect(container.textContent).toContain("官方");
    expect(container.textContent).toContain("插件 · cua-mac");
    expect(container.querySelector('[data-skill-artwork="custom-plugin"]')).toBeTruthy();
    // Non-plugin inventory records (the plugin's MCP server) stay out of the
    // plugin list.
    expect(container.textContent).not.toContain("computer");
  });

  it("shows package permissions and grants a pending plugin through the update callback", async () => {
    installSkillList([]);
    const onUpdateExtensionPackage = vi.fn().mockResolvedValue(undefined);
    const extensionInventory = [
      {
        id: "plugin:project:docs",
        name: "docs",
        description: "Project documentation commands",
        kind: "plugin",
        provenance: {
          kind: "plugin",
          source: "project",
          scope: "project",
          plugin_id: "docs",
          official: false,
        },
        state: "pending",
        approval_state: "pending",
        runtime_state: "inactive",
        enabled: false,
        fingerprint: "sha256:docs",
        requested_permissions: ["file.read", "command.prompt"],
        contributions: {
          commands: [{ id: "ask-docs", title: "Ask docs", kind: "prompt_template", template: "Ask {{args}}" }],
          settings: [],
          themes: [],
        },
      },
    ] as unknown as ExtensionInventoryRecord[];

    await act(async () => {
      root = createRoot(container);
      root.render(
        <SkillsCatalog
          extensionInventory={extensionInventory}
          onUpdateExtensionPackage={onUpdateExtensionPackage}
        />,
      );
    });

    expect(container.textContent).toContain("待授权");
    expect(container.textContent).toContain("file.read");
    expect(container.textContent).toContain("命令 1 · 设置 0 · 主题 0");

    const grantButton = [...container.querySelectorAll("button")]
      .find((button) => button.textContent === "授权并启用");
    await act(async () => {
      grantButton?.click();
    });

    expect(onUpdateExtensionPackage).toHaveBeenCalledWith({
      id: "plugin:project:docs",
      fingerprint: "sha256:docs",
      action: "grant",
    });
  });

  it("surfaces changed fingerprints and runtime failures", async () => {
    installSkillList([]);
    const extensionInventory = [
      {
        id: "plugin:project:changed",
        name: "changed",
        kind: "plugin",
        provenance: { kind: "plugin", source: "project", scope: "project", plugin_id: "changed" },
        state: "changed",
        approval_state: "changed",
        runtime_state: "inactive",
        enabled: false,
      },
      {
        id: "plugin:user:broken",
        name: "broken",
        kind: "plugin",
        provenance: { kind: "plugin", source: "user", scope: "user", plugin_id: "broken" },
        state: "granted",
        approval_state: "granted",
        runtime_state: "failed",
        enabled: true,
        last_error: "Plugin process exited before initialize",
      },
    ] as unknown as ExtensionInventoryRecord[];

    await act(async () => {
      root = createRoot(container);
      root.render(<SkillsCatalog extensionInventory={extensionInventory} />);
    });

    expect(container.textContent).toContain("内容已更改");
    expect(container.textContent).toContain("启动失败");
    expect(container.textContent).toContain("Plugin process exited before initialize");
  });

  it("opens a skill preview dialog from a skill row", async () => {
    installSkillList([
      {
        name: "bug-fix",
        description: "Fix a bug from a report",
        when_to_use: "Use when the user reports a crash",
        trigger_condition: "Bug reports and stack traces",
        source: "bundled",
        argument_hint: "Describe the failing behavior",
        examples: ["Fix this stack trace"],
        verification_checklist: ["Run the targeted test"],
        user_invocable: true,
        disable_model_invoke: false,
      },
    ]);

    await act(async () => {
      root = createRoot(container);
      root.render(<SkillsCatalog />);
    });

    await act(async () => {
      skillButton("bug-fix")?.click();
    });

    expect(document.querySelector('[role="dialog"]')).toBeTruthy();
    expect(document.body.textContent).toContain("# Bug Fix Workflow");
    expect(document.body.textContent).toContain("Read the report and inspect the code");
    expect(document.body.textContent).not.toContain("来源");
    expect(document.body.textContent).not.toContain("路径");
  });

  it("closes the preview and reports the selected skill when trying it", async () => {
    const onTrySkill = vi.fn();
    installSkillList([
      {
        name: "write",
        description: "Rewrite prose",
        source: "user",
        user_invocable: true,
        disable_model_invoke: false,
      },
    ]);

    await act(async () => {
      root = createRoot(container);
      root.render(<SkillsCatalog onTrySkill={onTrySkill} />);
    });

    await act(async () => {
      skillButton("write")?.click();
    });
    await act(async () => {
      buttonByText("立即试用")?.click();
    });

    expect(onTrySkill).toHaveBeenCalledWith(expect.objectContaining({ name: "write" }));
    expect(document.querySelector('[role="dialog"]')).toBeNull();
  });

  it("does not offer Try now for a non-user-invocable skill", async () => {
    const onTrySkill = vi.fn();
    installSkillList([
      {
        name: "internal-review",
        description: "Model-only review workflow",
        source: "bundled",
        user_invocable: false,
        disable_model_invoke: false,
      },
    ]);

    await act(async () => {
      root = createRoot(container);
      root.render(<SkillsCatalog onTrySkill={onTrySkill} />);
    });
    await act(async () => {
      skillButton("internal-review")?.click();
    });

    expect(document.querySelector('[role="dialog"]')).toBeTruthy();
    expect(buttonByText("立即试用")).toBeUndefined();
    expect(onTrySkill).not.toHaveBeenCalled();
  });
});

function installSkillList(skills: SkillSummary[]): void {
  const stub: Partial<WuuDesktopApi> = {
    listSkills: vi.fn().mockResolvedValue({ skills }),
    readSkillContent: vi.fn().mockResolvedValue({
      content: [
        "---",
        "name: bug-fix",
        "---",
        "# Bug Fix Workflow",
        "",
        "Read the report and inspect the code.",
      ].join("\n"),
    }),
  };
  (globalThis as { wuu?: WuuDesktopApi }).wuu = stub as WuuDesktopApi;
  (window as unknown as { wuu: WuuDesktopApi }).wuu = stub as WuuDesktopApi;
}

function skillButton(name: string): HTMLButtonElement | undefined {
  return Array.from(container.querySelectorAll<HTMLButtonElement>("button")).find(
    (button) => button.textContent?.includes(name),
  );
}

function buttonByText(text: string): HTMLButtonElement | undefined {
  return Array.from(document.querySelectorAll<HTMLButtonElement>("button")).find(
    (button) => button.textContent === text,
  );
}
