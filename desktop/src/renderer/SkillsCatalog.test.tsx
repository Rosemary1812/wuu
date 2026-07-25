import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { SkillSummary, WuuDesktopApi } from "../shared/protocol";
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
    expect(container.textContent).toContain("Navigate and observe web pages.");
    expect(container.textContent).not.toContain("Use when no safer interface is available.");
    expect(container.querySelector('[data-skill-artwork="official-browser"]')).toBeTruthy();
    expect(container.querySelector('[data-skill-artwork="neutral-skill"]')).toBeTruthy();
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
