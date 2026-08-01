import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { act, createRef } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import type { ChannelRoom, DesktopProject, InitializeResult } from "../shared/protocol";
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
  sectionOrder?: string[];
  state?: AppState;
  groupChatEnabled?: boolean;
  channelMentionCount?: number;
  channelRooms?: ChannelRoom[];
  activeChannelRoomID?: string;
  activeChannelSection?: "rooms" | "agents" | "tasks" | null;
  collapsedSidebarSectionIDs?: Set<string>;
  onSelectChannelRoom?: (roomID: string) => void;
  onOpenChannelAgents?: () => void;
  onOpenChannelTasks?: () => void;
}

function collabRoom(id: string, name: string, unreadCount = 0): ChannelRoom {
  return {
    id,
    kind: "channel",
    name,
    created_by: "local-user",
    created_at: "2026-01-01T00:00:00Z",
    members: [
      {
        room_id: id,
        member_type: "human",
        member_id: "local-user",
        joined_at: "2026-01-01T00:00:00Z",
      },
    ],
    unread_count: unreadCount,
  };
}

function renderSidebar({
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
  groupChatEnabled = false,
  channelMentionCount,
  channelRooms = [],
  activeChannelRoomID,
  activeChannelSection = null,
  collapsedSidebarSectionIDs = new Set(),
  onSelectChannelRoom,
  onOpenChannelAgents,
  onOpenChannelTasks,
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
        collapsedSidebarSectionIDs={collapsedSidebarSectionIDs}
        expandedSidebarSectionIDs={new Set()}
        projectThreadsByProjectID={{}}
        projectMenuOpen={false}
        projectMenuRef={createRef<HTMLDivElement>()}
        searchOpen={false}
        debugFixturesVisible={false}
        sectionOrder={sectionOrder}
        onStartNewThread={() => {}}
        onOpenAutomationsTab={() => {}}
        onOpenSkillsTab={() => {}}
        groupChatEnabled={groupChatEnabled}
        channelMentionCount={channelMentionCount}
        channelRooms={channelRooms}
        activeChannelRoomID={activeChannelRoomID}
        activeChannelSection={activeChannelSection}
        onSelectChannelRoom={onSelectChannelRoom}
        onOpenChannelAgents={onOpenChannelAgents}
        onOpenChannelTasks={onOpenChannelTasks}
        onOpenChannels={() => {}}
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
  it("hides group chat unless the frontend flag is enabled", () => {
    renderSidebar();

    expect(container.textContent).not.toContain("群聊");
  });

  it("shows the unread human mention badge on group chat", () => {
    renderSidebar({ groupChatEnabled: true, channelMentionCount: 3 });

    expect(container.querySelector(".channel-mention-badge")?.textContent).toBe("3");
  });

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
    expect(sidebarCSS).toMatch(
      /\.sidebar-drawer-docking :is\(\.sidebar, \.settings-sidebar\)\s*\{[\s\S]*background:\s*var\(--sidebar-material-fill\);[\s\S]*background-color var\(--sidebar-motion-duration\)/,
    );
    expect(sidebarCSS).toMatch(
      /\.sidebar-drawer-docking :is\(\.sidebar, \.settings-sidebar\)::before\s*\{[\s\S]*opacity:\s*0\.5;[\s\S]*transition:\s*opacity var\(--sidebar-motion-duration\)/,
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

describe("AppSidebar 协作 section", () => {
  it("is hidden when the group chat flag is off", () => {
    renderSidebar({ channelRooms: [collabRoom("room-1", "产品体验", 2)] });

    expect(container.textContent).not.toContain("协作");
    expect(container.querySelector(".sidebar-room-row")).toBeNull();
  });

  it("lists rooms with 99+-capped unread badges, hidden for the active room", () => {
    renderSidebar({
      groupChatEnabled: true,
      channelRooms: [
        collabRoom("room-1", "产品体验", 3),
        collabRoom("room-2", "基础功能", 250),
      ],
      activeChannelRoomID: "room-1",
    });

    const rows = Array.from(
      container.querySelectorAll<HTMLElement>(".sidebar-room-row"),
    );
    const roomRows = rows.filter((row) => !row.classList.contains("sidebar-collab-entry"));
    expect(roomRows).toHaveLength(2);
    // Active room is in the canvas — its badge is intentionally absent.
    expect(roomRows[0]?.querySelector(".channel-room-unread")).toBeNull();
    expect(roomRows[0]?.getAttribute("aria-current")).toBe("page");
    expect(
      roomRows[1]?.querySelector(".channel-room-unread")?.textContent,
    ).toBe("99+");
  });

  it("selects a room through the provided handler", () => {
    const selections: string[] = [];
    renderSidebar({
      groupChatEnabled: true,
      channelRooms: [collabRoom("room-1", "产品体验")],
      onSelectChannelRoom: (roomID) => selections.push(roomID),
    });

    const row = container.querySelector<HTMLButtonElement>(".sidebar-room-row");
    act(() => row?.click());

    expect(selections).toEqual(["room-1"]);
  });

  it("opens the agents and tasks canvases and marks the active one", () => {
    let opened = "";
    renderSidebar({
      groupChatEnabled: true,
      activeChannelSection: "tasks",
      onOpenChannelAgents: () => { opened = "agents"; },
      onOpenChannelTasks: () => { opened = "tasks"; },
    });

    const entries = Array.from(
      container.querySelectorAll<HTMLButtonElement>(".sidebar-collab-entry"),
    );
    expect(entries).toHaveLength(2);
    expect(entries[0]?.getAttribute("aria-current")).toBeNull();
    expect(entries[1]?.getAttribute("aria-current")).toBe("page");

    act(() => entries[0]?.click());
    expect(opened).toBe("agents");
    act(() => entries[1]?.click());
    expect(opened).toBe("tasks");
  });

  it("surfaces the aggregate unread dot on the collapsed section header", () => {
    renderSidebar({
      groupChatEnabled: true,
      channelRooms: [collabRoom("room-1", "产品体验", 5)],
      collapsedSidebarSectionIDs: new Set(["__wuu_collab__"]),
    });

    const header = container.querySelector<HTMLElement>(
      ".collab-section .sidebar-section-row",
    );
    expect(header?.classList.contains("has-unread")).toBe(true);
    // Collapsed body keeps room rows unmounted, so no badge leaks out.
    expect(container.querySelector(".channel-room-unread")).toBeNull();
  });
});
