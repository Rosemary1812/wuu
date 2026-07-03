import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { act, createRef } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { DesktopProject, InitializeResult, ParticipantProfile } from "../shared/protocol";
import {
  AppSidebar,
  reconcileSidebarSectionOrder,
  SIDEBAR_SECTION_AGENTS,
  SIDEBAR_SECTION_PINNED,
} from "./AppSidebar";
import { initialState, SCRATCH_PSEUDO_PROJECT_ID, type AppState, type ThreadSummary } from "./AppState";

let container: HTMLDivElement;
let root: Root | null = null;
const sidebarCSS = readFileSync(
  resolve(process.cwd(), "src/renderer/styles/sidebar.css"),
  "utf8",
);

beforeEach(() => {
  container = document.createElement("div");
  document.body.appendChild(container);
  window.localStorage.clear();
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  root = null;
  container.remove();
  window.localStorage.clear();
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

function makeProject(id: string, name: string, path: string): DesktopProject {
  return {
    id,
    name,
    path,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

interface RenderOptions {
  sectionOrder: string[];
  collapsedProjectIDs?: Set<string>;
  participants?: ParticipantProfile[];
  pinnedThreads?: ThreadSummary[];
  busyParticipantIDs?: Set<string>;
  sidebarProjects?: DesktopProject[];
  activeDMParticipantID?: string;
  unreadDMParticipantIDs?: Set<string>;
  dmThreadByParticipantID?: Map<string, ThreadSummary>;
  onSelectParticipant?: (participant: ParticipantProfile) => void;
  onEditParticipant?: (participant: ParticipantProfile) => void;
  onTogglePinned?: (thread: ThreadSummary) => void;
}

function renderSidebar(options: RenderOptions): void {
  const {
    sectionOrder,
    collapsedProjectIDs = new Set<string>(),
    participants = [],
    pinnedThreads = [],
    busyParticipantIDs = new Set<string>(),
    sidebarProjects = [
      makeProject(SCRATCH_PSEUDO_PROJECT_ID, "对话", ""),
      makeProject("project-1", "wuu", "/repo/wuu"),
      makeProject("project-2", "interview", "/repo/interview"),
    ],
    activeDMParticipantID,
    unreadDMParticipantIDs = new Set<string>(),
    dmThreadByParticipantID = new Map<string, ThreadSummary>(),
    onSelectParticipant = () => {},
    onEditParticipant = () => {},
    onTogglePinned = () => {},
  } = options;

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
        sidebarProjects={sidebarProjects}
        pinnedThreads={pinnedThreads}
        activeThreadID={undefined}
        activeDMParticipantID={activeDMParticipantID}
        dmThreadByParticipantID={dmThreadByParticipantID}
        unreadDMParticipantIDs={unreadDMParticipantIDs}
        participants={participants}
        busyParticipantIDs={busyParticipantIDs}
        pendingThreadID={undefined}
        pendingProjectID={undefined}
        archiveConfirmThreadID={undefined}
        collapsedProjectIDs={collapsedProjectIDs}
        expandedProjectIDs={new Set()}
        collapsingProjectIDs={new Set()}
        projectThreadsByProjectID={{}}
        projectMenuRef={createRef<HTMLDivElement>()}
        projectMenuOpen={false}
        searchOpen={false}
        debugFixturesVisible={false}
        sectionOrder={sectionOrder}
        onStartNewThread={() => {}}
        onOpenSkillsTab={() => {}}
        onToggleConversationSearch={() => {}}
        onSeedConversationFixture={() => {}}
        onSeedAgentTreeDemo={() => {}}
        onOpenChipGallery={() => {}}
        onOpenApprovalGallery={() => {}}
        onSelectThread={() => {}}
        onSelectParticipant={onSelectParticipant}
        onEditParticipant={onEditParticipant}
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

describe("reconcileSidebarSectionOrder", () => {
  it("returns the default order when no stored order is present", () => {
    expect(
      reconcileSidebarSectionOrder(undefined, ["project-1", "project-2"]),
    ).toEqual([
      SIDEBAR_SECTION_AGENTS,
      SCRATCH_PSEUDO_PROJECT_ID,
      "project-1",
      "project-2",
    ]);
  });

  it("drops unknown keys that are not in the project list", () => {
    const order = reconcileSidebarSectionOrder(
      ["__wuu_unknown__", "project-2", "project-1"],
      ["project-1", "project-2"],
    );
    expect(order).toEqual([
      SIDEBAR_SECTION_AGENTS,
      SCRATCH_PSEUDO_PROJECT_ID,
      "project-2",
      "project-1",
    ]);
  });

  it("appends newly-seen projects at the end while preserving the stored prefix", () => {
    const order = reconcileSidebarSectionOrder(
      ["project-1", SCRATCH_PSEUDO_PROJECT_ID],
      ["project-1", "project-2", "project-3"],
    );
    expect(order).toEqual([
      SIDEBAR_SECTION_AGENTS,
      "project-1",
      SCRATCH_PSEUDO_PROJECT_ID,
      "project-2",
      "project-3",
    ]);
  });

  it("strips the fixed-position pinned key if it was persisted", () => {
    const order = reconcileSidebarSectionOrder(
      [SIDEBAR_SECTION_PINNED, "project-1"],
      ["project-1"],
    );
    expect(order).toEqual([
      SIDEBAR_SECTION_AGENTS,
      SCRATCH_PSEUDO_PROJECT_ID,
      "project-1",
    ]);
  });
});

describe("AppSidebar sections", () => {
  it("renders sections in the provided sectionOrder", () => {
    renderSidebar({
      sectionOrder: [
        SIDEBAR_SECTION_AGENTS,
        "project-2",
        "project-1",
      ],
    });

    const sections = Array.from(
      container.querySelectorAll(".sidebar-main > section"),
    );
    const ariaLabels = sections.map((s) => s.getAttribute("aria-label"));
    expect(ariaLabels).toEqual([
      "Agents",
      "项目 interview",
      "项目 wuu",
    ]);
  });

  it("skips unknown keys in sectionOrder", () => {
    renderSidebar({
      sectionOrder: [
        SIDEBAR_SECTION_AGENTS,
        "__wuu_unknown__",
        "project-1",
      ],
    });

    const sections = Array.from(
      container.querySelectorAll(".sidebar-main > section"),
    );
    const ariaLabels = sections.map((s) => s.getAttribute("aria-label"));
    expect(ariaLabels).toEqual(["Agents", "项目 wuu"]);
  });

  it("renders the 对话 scratch section in the order list", () => {
    renderSidebar({
      sectionOrder: [
        SIDEBAR_SECTION_AGENTS,
        SCRATCH_PSEUDO_PROJECT_ID,
        "project-1",
      ],
    });

    const sections = Array.from(
      container.querySelectorAll(".sidebar-main > section"),
    );
    const ariaLabels = sections.map((s) => s.getAttribute("aria-label"));
    expect(ariaLabels).toEqual(["Agents", "项目", "项目 wuu"]);
  });

  it("renders the pinned section above all reorderable sections", () => {
    const pinned = makeThreadSummary("thread-pinned", "Pinned session", {
      pinned: true,
    });
    renderSidebar({
      pinnedThreads: [pinned],
      sectionOrder: [
        SIDEBAR_SECTION_AGENTS,
        "project-1",
      ],
    });

    const sections = Array.from(
      container.querySelectorAll(".sidebar-main > section"),
    );
    const ariaLabels = sections.map((s) => s.getAttribute("aria-label"));
    expect(ariaLabels).toEqual([
      "置顶",
      "Agents",
      "项目 wuu",
    ]);
  });

  it("collapsing the Agents section hides roster rows", () => {
    const participants: ParticipantProfile[] = [
      {
        id: "p-1",
        kind: "named",
        name: "Alpha",
        role: "writer",
      },
    ];

    // Expanded: row visible.
    renderSidebar({
      sectionOrder: [SIDEBAR_SECTION_AGENTS],
      participants,
    });
    expect(
      container.querySelectorAll(".participant-roster-row").length,
    ).toBe(1);

    // Collapsed: row hidden.
    renderSidebar({
      sectionOrder: [SIDEBAR_SECTION_AGENTS],
      participants,
      collapsedProjectIDs: new Set([SIDEBAR_SECTION_AGENTS]),
    });
    expect(
      container.querySelectorAll(".participant-roster-row").length,
    ).toBe(0);
  });

  it("each section header exposes aria-expanded", () => {
    const pinned = makeThreadSummary("thread-pinned", "Pinned session", {
      pinned: true,
    });
    renderSidebar({
      pinnedThreads: [pinned],
      sectionOrder: [SIDEBAR_SECTION_AGENTS, "project-1"],
    });

    // Every section header is either a `.project-row` (project / scratch /
    // pinned / agents) — all should carry aria-expanded.
    const headers = Array.from(
      container.querySelectorAll(".sidebar-main > section .project-row"),
    );
    expect(headers.length).toBeGreaterThan(0);
    for (const header of headers) {
      expect(header.getAttribute("aria-expanded")).not.toBeNull();
    }
  });

  it("Agents header does not nest action buttons inside the toggle button", () => {
    renderSidebar({
      sectionOrder: [SIDEBAR_SECTION_AGENTS],
    });

    const agentsSection = container.querySelector(
      'section[aria-label="Agents"]',
    );
    expect(agentsSection).not.toBeNull();

    const headerButton = agentsSection?.querySelector<HTMLButtonElement>(
      '.project-row[aria-label*="Agents"]',
    );
    expect(headerButton).not.toBeNull();
    // React 18 warns <button> cannot contain a nested <button>; the roster
    // … trigger and the + new-agent button must live as siblings of the
    // header button, not inside it.
    expect(headerButton?.querySelector("button")).toBeNull();

    const rosterTrigger = agentsSection?.querySelector<HTMLButtonElement>(
      'button[aria-label="团队模板操作"]',
    );
    const addButton = agentsSection?.querySelector<HTMLButtonElement>(
      'button[aria-label="新建 Agent"]',
    );
    expect(rosterTrigger).not.toBeNull();
    expect(addButton).not.toBeNull();
    expect(headerButton?.contains(rosterTrigger ?? null)).toBe(false);
    expect(headerButton?.contains(addButton ?? null)).toBe(false);
  });

  it("defines rotate(-45deg) on the pinned icon expanded state in CSS", () => {
    // The pinned section uses Pin for both collapsed and expanded states;
    // the expanded variant is rotated -45deg via CSS so the visual reads as
    // a diagonal pin.
    expect(sidebarCSS).toMatch(/\[data-project-icon-kind="pinned"\][\s\S]*?\.project-row\.expanded/);
  });

  it("agent row click fires onSelectParticipant (DM open path), not profile", () => {
    const participants: ParticipantProfile[] = [
      { id: "p-1", kind: "named", name: "Alpha", role: "writer" },
    ];
    let selected: ParticipantProfile | undefined;
    let edited: ParticipantProfile | undefined;
    renderSidebar({
      sectionOrder: [SIDEBAR_SECTION_AGENTS],
      participants,
      onSelectParticipant: (participant) => {
        selected = participant;
      },
      onEditParticipant: (participant) => {
        edited = participant;
      },
    });

    const row = container.querySelector<HTMLButtonElement>(
      ".participant-roster-row",
    );
    expect(row).not.toBeNull();
    act(() => {
      row?.click();
    });
    expect(selected?.id).toBe("p-1");
    // Profile editing is now exclusive to the right-click context menu.
    expect(edited).toBeUndefined();
  });

  it("right-clicking an agent row shows the context menu with 编辑设定", () => {
    const participants: ParticipantProfile[] = [
      { id: "p-1", kind: "named", name: "Alpha", role: "writer" },
    ];
    let edited: ParticipantProfile | undefined;
    renderSidebar({
      sectionOrder: [SIDEBAR_SECTION_AGENTS],
      participants,
      onEditParticipant: (participant) => {
        edited = participant;
      },
    });

    const row = container.querySelector<HTMLButtonElement>(
      ".participant-roster-row",
    );
    expect(row).not.toBeNull();
    act(() => {
      row?.dispatchEvent(
        new MouseEvent("contextmenu", {
          bubbles: true,
          clientX: 80,
          clientY: 200,
        }),
      );
    });
    const menu = document.body.querySelector(".thread-row-context-menu");
    expect(menu).not.toBeNull();
    const items = menu?.querySelectorAll(".thread-row-context-menu-item");
    expect(items?.length).toBeGreaterThan(0);
    expect(items?.[0].textContent).toContain("编辑设定");

    act(() => {
      items?.[0].dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    expect(edited?.id).toBe("p-1");
  });

  it("pins DM via the context menu when a DM thread exists", () => {
    const participants: ParticipantProfile[] = [
      { id: "p-1", kind: "named", name: "Alpha", role: "writer" },
    ];
    const dmThread = makeThreadSummary("dm-1", "DM with Alpha", {
      dm_participant_id: "p-1",
      pinned: false,
    });
    let toggled: ThreadSummary | undefined;
    renderSidebar({
      sectionOrder: [SIDEBAR_SECTION_AGENTS],
      participants,
      dmThreadByParticipantID: new Map([["p-1", dmThread]]),
      onTogglePinned: (thread) => {
        toggled = thread;
      },
    });

    const row = container.querySelector<HTMLButtonElement>(
      ".participant-roster-row",
    );
    act(() => {
      row?.dispatchEvent(
        new MouseEvent("contextmenu", {
          bubbles: true,
          clientX: 80,
          clientY: 200,
        }),
      );
    });
    const items = document.body
      .querySelector(".thread-row-context-menu")
      ?.querySelectorAll<HTMLButtonElement>(".thread-row-context-menu-item");
    expect(items?.[1].textContent).toContain("置顶 DM");
    expect(items?.[1].disabled).toBe(false);
    act(() => {
      items?.[1].dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    expect(toggled?.id).toBe("dm-1");
  });

  it("disables DM pin entry when no DM thread exists yet", () => {
    const participants: ParticipantProfile[] = [
      { id: "p-1", kind: "named", name: "Alpha", role: "writer" },
    ];
    renderSidebar({
      sectionOrder: [SIDEBAR_SECTION_AGENTS],
      participants,
    });

    const row = container.querySelector<HTMLButtonElement>(
      ".participant-roster-row",
    );
    act(() => {
      row?.dispatchEvent(
        new MouseEvent("contextmenu", {
          bubbles: true,
          clientX: 80,
          clientY: 200,
        }),
      );
    });
    const items = document.body
      .querySelector(".thread-row-context-menu")
      ?.querySelectorAll<HTMLButtonElement>(".thread-row-context-menu-item");
    expect(items?.[1].textContent).toContain("置顶 DM");
    expect(items?.[1].disabled).toBe(true);
  });

  it("highlights the active DM participant row and applies has-unread", () => {
    const participants: ParticipantProfile[] = [
      { id: "p-active", kind: "named", name: "Active", role: "writer" },
      { id: "p-unread", kind: "named", name: "Unread", role: "writer" },
      { id: "p-quiet", kind: "named", name: "Quiet", role: "writer" },
    ];
    renderSidebar({
      sectionOrder: [SIDEBAR_SECTION_AGENTS],
      participants,
      activeDMParticipantID: "p-active",
      unreadDMParticipantIDs: new Set(["p-unread"]),
    });

    const rows = Array.from(
      container.querySelectorAll<HTMLButtonElement>(".participant-roster-row"),
    );
    const byName = new Map<string, HTMLButtonElement>();
    for (const row of rows) {
      const name = row.querySelector(".participant-roster-name")?.textContent ?? "";
      byName.set(name, row);
    }
    expect(byName.get("Active")?.classList.contains("active")).toBe(true);
    expect(byName.get("Active")?.classList.contains("has-unread")).toBe(false);
    expect(byName.get("Unread")?.classList.contains("active")).toBe(false);
    expect(byName.get("Unread")?.classList.contains("has-unread")).toBe(true);
    expect(byName.get("Quiet")?.classList.contains("active")).toBe(false);
    expect(byName.get("Quiet")?.classList.contains("has-unread")).toBe(false);
  });
});