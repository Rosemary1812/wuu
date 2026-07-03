import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { act, createRef } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { InitializeResult } from "../shared/protocol";
import { AppSidebar } from "./AppSidebar";
import { initialState, type AppState, type ThreadSummary } from "./AppState";

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
  act(() => {
    root?.unmount();
  });
  root = null;
  container.remove();
});

function initialized(): InitializeResult {
  return {
    protocol_version: "wuu-app-server/v0.1",
    provider: "test",
    model: "test-model",
    workspace_root: "/repo",
  };
}

function makeThreadSummary(
  id: string,
  title: string,
  overrides: Partial<ThreadSummary> = {},
): ThreadSummary {
  return {
    id,
    preview: title,
    title,
    model_provider: "test",
    model: "test-model",
    cwd: "/repo",
    workspace_kind: "project",
    status: "idle",
    pinned: false,
    archived: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    turns: [],
    turn_count: 0,
    ...overrides,
  };
}

function renderSidebar({
  projectMenuOpen = false,
  pinnedThreads = [],
  onTogglePinned = () => {},
}: {
  projectMenuOpen?: boolean;
  pinnedThreads?: ThreadSummary[];
  onTogglePinned?: (thread: ThreadSummary) => void;
} = {}): void {
  const state: AppState = {
    ...initialState,
    initialized: initialized(),
    activeContext: {
      kind: "project",
      project_id: "project-1",
      cwd: "/repo",
    },
  };

  act(() => {
    root = createRoot(container);
    root.render(
      <AppSidebar
        state={state}
        sidebarProjects={[]}
        pinnedThreads={pinnedThreads}
        activeThreadID={undefined}
        activeParticipantID={undefined}
        participants={[]}
        pendingThreadID={undefined}
        pendingProjectID={undefined}
        archiveConfirmThreadID={undefined}
        collapsedProjectIDs={new Set()}
        expandedProjectIDs={new Set()}
        collapsingProjectIDs={new Set()}
        projectThreadsByProjectID={{}}
        projectMenuOpen={projectMenuOpen}
        projectMenuRef={createRef<HTMLDivElement>()}
        searchOpen={false}
        debugFixturesVisible={false}
        onStartNewThread={() => {}}
        onOpenSkillsTab={() => {}}
        onToggleConversationSearch={() => {}}
        onSeedConversationFixture={() => {}}
        onSeedAgentTreeDemo={() => {}}
        onOpenChipGallery={() => {}}
        onOpenApprovalGallery={() => {}}
        onSelectThread={() => {}}
        onSelectParticipant={() => {}}
        onCreateParticipant={() => {}}
        onImportParticipants={() => {}}
        onExportParticipants={() => {}}
        onTogglePinned={onTogglePinned}
        onArchiveThread={() => {}}
        onClearArchiveConfirm={() => {}}
        onToggleProjectMenu={() => {}}
        onCreateProject={() => {}}
        onOpenProjectFolder={() => {}}
        onToggleProjectCollapsed={() => {}}
        onStartNewThreadForProject={() => {}}
        onSelectProjectThread={() => {}}
        onOpenSettings={() => {}}
      />,
    );
  });
}

describe("AppSidebar layout", () => {
  it("defines a hover edge drawer for the collapsed sidebar", () => {
    expect(sidebarCSS).toContain(".sidebar-hover-zone");
    expect(sidebarCSS).toContain(
      ".sidebar-collapsed.sidebar-drawer-open .sidebar",
    );
    expect(sidebarCSS).toContain(
      ".sidebar-collapsed.sidebar-drawer-open .sidebar .sidebar-content",
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

  it("keeps the workspace add action with the primary nav", () => {
    renderSidebar({ projectMenuOpen: true });

    const primaryNav = container.querySelector(".primary-nav");
    const addWorkspaceButton = primaryNav?.querySelector(
      'button[aria-label="添加工作区"]',
    );
    const settings = container.querySelector(".sidebar-settings");

    expect(addWorkspaceButton?.classList.contains("nav-item")).toBe(true);
    expect(primaryNav?.querySelector(".project-add-menu")).not.toBeNull();
    expect(settings?.querySelector('button[aria-label="添加工作区"]')).toBeNull();
    expect(settings?.textContent).toBe("设置");
  });

  it("renders pinned sessions above the project list", () => {
    const pinned = makeThreadSummary("thread-pinned", "Pinned session", {
      pinned: true,
    });
    let toggled: ThreadSummary | undefined;
    renderSidebar({
      pinnedThreads: [pinned],
      onTogglePinned: (thread) => {
        toggled = thread;
      },
    });

    const pinnedSection = container.querySelector(
      'section[aria-label="置顶"]',
    );
    const projectSection = container.querySelector(
      'section[aria-label="项目"]',
    );
    expect(pinnedSection).not.toBeNull();
    expect(pinnedSection?.textContent).toContain("Pinned session");
    expect(
      pinnedSection?.compareDocumentPosition(projectSection as Node) ?? 0,
    ).toBe(Node.DOCUMENT_POSITION_FOLLOWING);

    const unpinButton = pinnedSection?.querySelector<HTMLButtonElement>(
      'button[aria-label="取消置顶"]',
    );
    expect(unpinButton).not.toBeNull();
    act(() => {
      unpinButton?.click();
    });
    expect(toggled?.id).toBe("thread-pinned");
  });
});
