import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { act, createRef } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterAll, afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.hoisted(() => {
  vi.stubEnv("VITE_ENABLE_COLLABORATION", "true");
});

import type { DesktopProject, InitializeResult } from "../shared/protocol";
import { AppSidebar } from "./AppSidebar";
import {
  initialState,
  SCRATCH_PSEUDO_PROJECT_ID,
  type AppState,
} from "./AppState";

let container: HTMLDivElement;
let root: Root | null = null;
const sidebarCSS = readFileSync(
  resolve(process.cwd(), "src/renderer/styles/sidebar.css"),
  "utf8",
);

beforeEach(() => {
  container = document.createElement("div");
  document.body.appendChild(container);
});

afterEach(() => {
  act(() => root?.unmount());
  root = null;
  container.remove();
});

afterAll(() => {
  vi.unstubAllEnvs();
});

function initialized(): InitializeResult {
  return {
    protocol_version: "wuu-app-server/v0.1",
    provider: "test",
    model: "test-model",
    workspace_root: "/repo",
  };
}

const sidebarProjects: DesktopProject[] = [
  {
    id: SCRATCH_PSEUDO_PROJECT_ID,
    name: "对话",
    path: "",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  },
  {
    id: "project-1",
    name: "wuu",
    path: "/repo/wuu",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  },
  {
    id: "project-2",
    name: "interview",
    path: "/repo/interview",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  },
];

interface RenderOptions {
  kanbanBoardVisible?: boolean;
  onOpenCollaboration?: () => void;
  sectionOrder?: string[];
  state?: AppState;
}

function renderSidebar({
  kanbanBoardVisible = false,
  onOpenCollaboration = () => {},
  sectionOrder = [SCRATCH_PSEUDO_PROJECT_ID, "project-1", "project-2"],
  state = {
    ...initialState,
    initialized: initialized(),
    activeContext: {
      kind: "project",
      project_id: "project-1",
      cwd: "/repo/wuu",
    },
  },
}: RenderOptions = {}): void {
  act(() => {
    root = createRoot(container);
    root.render(
      <AppSidebar
        state={state}
        sidebarProjects={sidebarProjects}
        pinnedThreads={[]}
        activeThreadID={undefined}
        pendingThreadID={undefined}
        pendingProjectID={undefined}
        collapsedSidebarSectionIDs={new Set()}
        expandedSidebarSectionIDs={new Set()}
        projectThreadsByProjectID={{}}
        projectMenuOpen={false}
        projectMenuRef={createRef<HTMLDivElement>()}
        searchOpen={false}
        debugFixturesVisible={false}
        sectionOrder={sectionOrder}
        onStartNewThread={() => {}}
        onOpenSkillsTab={() => {}}
        kanbanBoardVisible={kanbanBoardVisible}
        onOpenCollaboration={onOpenCollaboration}
        onToggleConversationSearch={() => {}}
        onSeedConversationFixture={() => {}}
        onSeedAgentTreeDemo={() => {}}
        onOpenChipGallery={() => {}}
        onSelectThread={() => {}}
        onTogglePinned={() => {}}
        onArchiveThread={() => {}}
        onDeleteThread={() => {}}
        onRenameThread={() => {}}
        onToggleProjectMenu={() => {}}
        onCreateProject={() => {}}
        onOpenProjectFolder={() => {}}
        onToggleSidebarSectionCollapsed={() => {}}
        onStartNewThreadForProject={() => {}}
        onSelectProjectThread={() => {}}
        onRemoveProject={() => {}}
        onRelocateProject={() => {}}
        onOpenSettings={() => {}}
      />,
    );
  });
}

describe("AppSidebar layout", () => {
  it("defines a hover edge drawer for the collapsed sidebar", () => {
    expect(sidebarCSS).toContain(".sidebar-hover-zone");
    expect(sidebarCSS).toMatch(/\.sidebar-hover-zone\s*\{[\s\S]*width:\s*14px;/);
    expect(sidebarCSS).not.toMatch(
      /\.sidebar-collapsed\s+\.sidebar-hover-zone:hover[\s\S]*background:/,
    );
    expect(sidebarCSS).toContain("--sidebar-drawer-bg: #ffffff");
    expect(sidebarCSS).not.toMatch(/--sidebar-drawer-bg:\s*rgba\(/);
    expect(sidebarCSS).toMatch(
      /\.sidebar-collapsed\.sidebar-drawer-open \.sidebar,[\s\S]*background:\s*var\(--sidebar-drawer-bg\);/,
    );
    // The ease itself now lives in base.css as a shared motion token;
    // sidebar.css only consumes it.
    expect(sidebarCSS).toContain("var(--sidebar-motion-ease)");
    expect(sidebarCSS).toContain(
      ".sidebar-collapsed.sidebar-drawer-open .sidebar",
    );
    expect(sidebarCSS).toContain(
      ".sidebar-collapsed.sidebar-drawer-closing .sidebar",
    );
    expect(sidebarCSS).toContain(
      ".sidebar-collapsed.sidebar-drawer-open .sidebar .sidebar-content",
    );
    // The collapsed rail carries the drawer's off-canvas start transform
    // (excluded while the dock<->collapse grid animation runs), so the open
    // transition is a real slide-in instead of an instant pop.
    expect(sidebarCSS).toMatch(
      /\.sidebar-collapsed:not\(\.sidebar-animating\) :is\(\.sidebar, \.settings-sidebar\)\s*\{\s*transform:\s*translate3d\(-100%, 0, 0\);/,
    );
    expect(sidebarCSS).toMatch(
      /\.sidebar-collapsed\.sidebar-drawer-open :is\(\.sidebar, \.settings-sidebar\)[\s\S]*?transition:\s*transform\s+var\(--sidebar-drawer-enter-duration\)\s+var\(--sidebar-drawer-enter-easing\);/,
    );
    // The titlebar toggle stays above the drawer (140) as a stationary click
    // target while the panel slides underneath it.
    expect(sidebarCSS).toMatch(
      /\.sidebar-collapsed :is\(\.titlebar, \.settings-titlebar\) \.sidebar-toggle-button\s*\{[^}]*position:\s*relative;[^}]*z-index:\s*150;/,
    );
    expect(sidebarCSS).toMatch(
      /\.sidebar-collapsed\.sidebar-drawer-open :is\(\.sidebar, \.settings-sidebar\),[\s\S]*?z-index:\s*140;/,
    );
    // Closing slides the panel back off-screen with the exit tokens; the old
    // whole-panel opacity fade must not come back.
    expect(sidebarCSS).toMatch(
      /\.sidebar-collapsed\.sidebar-drawer-closing \.sidebar,\s*\.sidebar-collapsed\.sidebar-drawer-closing \.settings-sidebar\s*\{\s*transform:\s*translate3d\(-100%, 0, 0\);\s*transition:\s*transform\s+var\(--sidebar-drawer-exit-duration\)\s+var\(--sidebar-drawer-exit-easing\);/,
    );
    expect(sidebarCSS).not.toMatch(
      /\.sidebar-collapsed\.sidebar-drawer-closing \.settings-sidebar\s*\{[^}]*opacity:\s*0/,
    );
  });

  it("keeps primary actions outside the scrollable sidebar list", () => {
    renderSidebar();

    const content = container.querySelector(".sidebar-content");
    const primaryNav = container.querySelector(".primary-nav");
    const scrollRegion = container.querySelector(".sidebar-main");

    expect(primaryNav?.parentElement).toBe(content);
    expect(scrollRegion?.classList.contains("scrollbar-hidden")).toBe(true);
    expect(scrollRegion?.contains(primaryNav)).toBe(false);
    expect(scrollRegion?.querySelector(".project-section")).not.toBeNull();
  });

  it("renders the brand placeholder above the primary nav", () => {
    renderSidebar();

    const content = container.querySelector(".sidebar-content");
    const brand = content?.querySelector(".sidebar-brand");
    const primaryNav = content?.querySelector(".primary-nav");

    expect(brand).not.toBeNull();
    expect(brand?.querySelector(".sidebar-brand-wordmark")?.textContent).toBe("wuu");
    // 品牌区只放 wordmark，不放"草稿占位"之类的小灰字；textContent 必须只剩 wuu，
    // 否则 draft 标注会被静悄悄塞回来。
    expect(brand?.textContent?.trim()).toBe("wuu");
    expect(brand?.querySelector(".sidebar-brand-tag")).toBeNull();
    // 品牌占位必须排在 traffic-spacer 之后、primary-nav 之前，等真正的
    // logo / lockup 落地后这个测试再一起替换。
    expect(brand?.nextElementSibling).toBe(primaryNav);
  });

  it("keeps collaboration in the fixed primary navigation and opens it", () => {
    let opened = 0;
    renderSidebar({
      kanbanBoardVisible: true,
      onOpenCollaboration: () => {
        opened += 1;
      },
    });

    const labels = Array.from(
      container.querySelectorAll<HTMLButtonElement>(".primary-nav > .nav-item"),
    ).map((button) => button.textContent?.trim());
    expect(labels).toEqual(["新对话", "搜索会话", "技能", "协作"]);

    const collaborationButton = Array.from(
      container.querySelectorAll<HTMLButtonElement>(".primary-nav > .nav-item"),
    ).find((button) => button.textContent?.trim() === "协作");
    expect(collaborationButton?.classList.contains("active")).toBe(true);
    act(() => collaborationButton?.click());
    expect(opened).toBe(1);
  });

  it("shows collaboration even before the runtime initializes", () => {
    renderSidebar({ state: initialState });

    const collaborationButton = Array.from(
      container.querySelectorAll<HTMLButtonElement>(".primary-nav > .nav-item"),
    ).find((button) => button.textContent?.trim() === "协作");
    expect(collaborationButton).toBeDefined();
  });

  it("renders only scratch and projects in the workspace order", () => {
    renderSidebar({
      sectionOrder: ["project-2", SCRATCH_PSEUDO_PROJECT_ID, "project-1"],
    });

    const sections = Array.from(
      container.querySelectorAll<HTMLElement>(
        ".sidebar-functional-group-body > section[data-section-id]",
      ),
    );
    expect(sections.map((section) => section.dataset.sectionId)).toEqual([
      "project-2",
      SCRATCH_PSEUDO_PROJECT_ID,
      "project-1",
    ]);
  });
});
