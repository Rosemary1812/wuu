import { act, createRef } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { InitializeResult } from "../shared/protocol";
import { AppSidebar } from "./AppSidebar";
import { initialState, type AppState } from "./AppState";

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
});

function initialized(): InitializeResult {
  return {
    protocol_version: "wuu-app-server/v0.1",
    provider: "test",
    model: "test-model",
    workspace_root: "/repo",
  };
}

function renderSidebar(): void {
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
        activeThreadID={undefined}
        pendingThreadID={undefined}
        pendingProjectID={undefined}
        archiveConfirmThreadID={undefined}
        collapsedProjectIDs={new Set()}
        expandedProjectIDs={new Set()}
        collapsingProjectIDs={new Set()}
        projectThreadsByProjectID={{}}
        projectMenuOpen={false}
        projectMenuRef={createRef<HTMLDivElement>()}
        searchOpen={false}
        debugFixturesVisible={false}
        onStartNewThread={() => {}}
        onOpenSkillsTab={() => {}}
        onToggleConversationSearch={() => {}}
        onSeedConversationFixture={() => {}}
        onSeedAgentTreeDemo={() => {}}
        onOpenChipGallery={() => {}}
        onSelectThread={() => {}}
        onTogglePinned={() => {}}
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
});
