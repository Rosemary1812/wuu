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
  it("shows Claude agent templates as temporary subagent templates", async () => {
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
    expect(container.textContent).toContain("临时子代理模板");
    expect(container.textContent).toContain("invalid frontmatter");
  });
});

