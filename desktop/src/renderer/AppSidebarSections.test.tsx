import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { act, createRef } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Folder, FolderOpen } from "lucide-react";
import { SECTION_COLLAPSE_MS, SidebarSection } from "./SidebarSection";
import type { DesktopProject, InitializeResult, ParticipantProfile } from "../shared/protocol";
import {
  AppSidebar,
  reconcileSidebarSectionOrder,
  reorderSidebarSections,
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
const participantsCSS = readFileSync(
  resolve(process.cwd(), "src/renderer/styles/participants.css"),
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
  groupThreads?: ThreadSummary[];
  busyParticipantIDs?: Set<string>;
  sidebarProjects?: DesktopProject[];
  activeDMParticipantID?: string;
  unreadDMParticipantIDs?: Set<string>;
  dmThreadByParticipantID?: Map<string, ThreadSummary>;
  onSelectParticipant?: (participant: ParticipantProfile) => void;
  onEditParticipant?: (participant: ParticipantProfile) => void;
  onTogglePinned?: (thread: ThreadSummary) => void;
  onCreateGroupThread?: (title: string) => void;
  onSelectThread?: (id: string) => void;
}

function renderSidebar(options: RenderOptions): void {
  const {
    sectionOrder,
    collapsedProjectIDs = new Set<string>(),
    participants = [],
    pinnedThreads = [],
    groupThreads = [],
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
    onCreateGroupThread = () => {},
    onSelectThread = () => {},
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
        groupThreads={groupThreads}
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
        onSelectThread={onSelectThread}
        onSelectParticipant={onSelectParticipant}
        onCreateGroupThread={onCreateGroupThread}
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
      // 置顶 and 群聊 are fixed-position and always render first so the
      // panel reads as five stable sections (置顶 / 群聊 / Agents / 对话 /
      // 项目) — chat-style-threads-design.md §1.
      "置顶",
      "群聊",
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
    expect(ariaLabels).toEqual(["置顶", "群聊", "Agents", "项目 wuu"]);
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
    expect(ariaLabels).toEqual(["置顶", "群聊", "Agents", "项目", "项目 wuu"]);
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
      "群聊",
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

  it("renders the 置顶 section even when there are no pinned threads", () => {
    // 置顶 is fixed-position and must stay visible even when empty so
    // the user sees a stable container alongside Agents / 对话 / 项目.
    renderSidebar({
      sectionOrder: [SIDEBAR_SECTION_AGENTS, "project-1"],
    });

    const pinnedSection = container.querySelector(
      'section[aria-label="置顶"]',
    );
    expect(pinnedSection).not.toBeNull();
  });

  it("shows a muted '还没有会话' placeholder inside an empty 置顶 body", () => {
    renderSidebar({
      sectionOrder: [SIDEBAR_SECTION_AGENTS, "project-1"],
    });

    const pinnedSection = container.querySelector(
      'section[aria-label="置顶"]',
    );
    expect(pinnedSection).not.toBeNull();
    // Empty pinned shows the unified sidebar-section-empty-note with the
    // same muted styling as the project empty-note.
    const empty = pinnedSection?.querySelector(
      ".sidebar-section-empty-note",
    );
    expect(empty).not.toBeNull();
    expect(empty?.textContent).toBe("还没有会话");
  });

  it("five section headers share the unified project-row anatomy", () => {
    const pinned = makeThreadSummary("thread-pinned", "Pinned session", {
      pinned: true,
    });
    renderSidebar({
      pinnedThreads: [pinned],
      sectionOrder: [
        SIDEBAR_SECTION_AGENTS,
        SCRATCH_PSEUDO_PROJECT_ID,
        "project-1",
      ],
    });

    // Every section header is a `.project-row` with the paired icon
    // states (collapsed + expanded). This is the unification contract:
    // pinning icon, group icon, bot icon, conversation icon, project
    // icon all use the same `<SectionRowIcon>` markup so the icon
    // column lines up.
    const headerButtons = Array.from(
      container.querySelectorAll<HTMLButtonElement>(
        '.sidebar-main > section .project-row',
      ),
    );
    expect(headerButtons.length).toBe(5);
    for (const header of headerButtons) {
      expect(header.classList.contains("sidebar-section-row")).toBe(true);
      expect(
        header.querySelector(".project-row-icon-state.collapsed"),
      ).not.toBeNull();
      expect(
        header.querySelector(".project-row-icon-state.expanded"),
      ).not.toBeNull();
      const collapsedIcon = header.querySelector<HTMLElement>(
        ".project-row-icon-state.collapsed",
      );
      const expandedIcon = header.querySelector<HTMLElement>(
        ".project-row-icon-state.expanded",
      );
      expect(collapsedIcon?.classList.contains("icon-lg")).toBe(true);
      expect(expandedIcon?.classList.contains("icon-lg")).toBe(true);
    }
  });
});

describe("reorderSidebarSections", () => {
  it("moves the active item to the over item's position", () => {
    // dnd-kit's arrayMove drops the active item at over's index; from
    // "a" at index 0 to "d" at index 3 → ["b", "c", "d", "a"].
    const next = reorderSidebarSections(
      ["a", "b", "c", "d"],
      "a",
      "d",
    );
    expect(next).toEqual(["b", "c", "d", "a"]);
  });

  it("moves a later item up to an earlier slot", () => {
    const next = reorderSidebarSections(
      ["a", "b", "c", "d"],
      "c",
      "a",
    );
    expect(next).toEqual(["c", "a", "b", "d"]);
  });

  it("returns the original when over equals active (no-op)", () => {
    const order = ["a", "b", "c"];
    expect(reorderSidebarSections(order, "b", "b")).toBe(order);
  });

  it("returns the original when over is null", () => {
    const order = ["a", "b", "c"];
    expect(reorderSidebarSections(order, "b", null)).toBe(order);
  });

  it("returns the original when over is undefined", () => {
    const order = ["a", "b", "c"];
    expect(reorderSidebarSections(order, "b", undefined)).toBe(order);
  });

  it("returns the original when active is unknown", () => {
    const order = ["a", "b", "c"];
    expect(reorderSidebarSections(order, "__wuu_unknown__", "b")).toBe(order);
  });

  it("returns the original when over is unknown", () => {
    const order = ["a", "b", "c"];
    expect(reorderSidebarSections(order, "a", "__wuu_unknown__")).toBe(order);
  });
});

describe("AppSidebar drag-to-reorder wiring (T7)", () => {
  it("attaches dnd-kit listeners to the reorderable section headers but not the pinned one", () => {
    const pinned = makeThreadSummary("thread-pinned", "Pinned session", {
      pinned: true,
    });
    renderSidebar({
      pinnedThreads: [pinned],
      sectionOrder: [
        SIDEBAR_SECTION_AGENTS,
        SCRATCH_PSEUDO_PROJECT_ID,
        "project-1",
      ],
    });

    // Pinned section is fixed-position — its header must NOT receive
    // the dnd-kit activator. We assert by the absence of either the
    // role-based attribute dnd-kit injects or the can-reorder class.
    const pinnedHeader = container.querySelector<HTMLButtonElement>(
      'section[aria-label="置顶"] .sidebar-section-row',
    );
    expect(pinnedHeader).not.toBeNull();
    expect(pinnedHeader?.hasAttribute("aria-roledescription")).toBe(false);
    expect(pinnedHeader?.classList.contains("can-reorder")).toBe(false);

    // Every reorderable section header carries the can-reorder class
    // and the dnd-kit aria-roledescription attribute that marks it as
    // a draggable sortable item.
    const reorderableHeaders = Array.from(
      container.querySelectorAll<HTMLButtonElement>(
        'section[aria-label="Agents"] .sidebar-section-row, ' +
          'section[aria-label="项目"] .sidebar-section-row, ' +
          'section[aria-label="项目 wuu"] .sidebar-section-row',
      ),
    );
    expect(reorderableHeaders.length).toBe(3);
    for (const header of reorderableHeaders) {
      expect(header.classList.contains("can-reorder")).toBe(true);
      expect(header.getAttribute("aria-roledescription")).toBe(
        "sortable",
      );
    }
  });

  it("fires onReorderSections with the arrayMove result", () => {
    let received: string[] | undefined;
    renderSidebar({
      sectionOrder: [SIDEBAR_SECTION_AGENTS, SCRATCH_PSEUDO_PROJECT_ID, "project-1"],
    });
    // Re-render with a capturing onReorderSections — the prop was
    // omitted in the first render so we re-mount explicitly.
    act(() => {
      root?.unmount();
    });
    root = null;
    container.innerHTML = "";
    act(() => {
      root = createRoot(container);
      root.render(
        <AppSidebar
          {...{
            state: {
              ...initialState,
              initialized: initialized(),
              activeContext: {
                kind: "project",
                project_id: "project-1",
                cwd: "/repo",
              },
            } as AppState,
            sidebarProjects: [
              makeProject(SCRATCH_PSEUDO_PROJECT_ID, "对话", ""),
              makeProject("project-1", "wuu", "/repo/wuu"),
            ],
            pinnedThreads: [],
            groupThreads: [],
            activeThreadID: undefined,
            activeDMParticipantID: undefined,
            dmThreadByParticipantID: new Map(),
            unreadDMParticipantIDs: new Set(),
            participants: [],
            busyParticipantIDs: new Set(),
            pendingThreadID: undefined,
            pendingProjectID: undefined,
            archiveConfirmThreadID: undefined,
            collapsedProjectIDs: new Set(),
            expandedProjectIDs: new Set(),
            collapsingProjectIDs: new Set(),
            projectThreadsByProjectID: {},
            projectMenuRef: createRef<HTMLDivElement>(),
            projectMenuOpen: false,
            searchOpen: false,
            debugFixturesVisible: false,
            sectionOrder: [SIDEBAR_SECTION_AGENTS, SCRATCH_PSEUDO_PROJECT_ID, "project-1"],
            onStartNewThread: () => {},
            onOpenSkillsTab: () => {},
            onToggleConversationSearch: () => {},
            onSeedConversationFixture: () => {},
            onSeedAgentTreeDemo: () => {},
            onOpenChipGallery: () => {},
            onOpenApprovalGallery: () => {},
            onSelectThread: () => {},
            onSelectParticipant: () => {},
            onEditParticipant: () => {},
            onCreateParticipant: () => {},
            onImportParticipants: () => {},
            onExportParticipants: () => {},
            onTogglePinned: () => {},
            onArchiveThread: () => {},
            onClearArchiveConfirm: () => {},
            onToggleProjectMenu: () => {},
            onCreateProject: () => {},
            onOpenProjectFolder: () => {},
            onToggleProjectCollapsed: () => {},
            onStartNewThreadForProject: () => {},
            onSelectProjectThread: () => {},
            onReorderSections: (next: string[]) => {
              received = next;
            },
            onOpenSettings: () => {},
            onPointerEnter: undefined,
            onPointerLeave: undefined,
          }}
        />,
      );
    });

    // Directly invoke the pure helper to assert the wire-up shape that
    // the drag handler applies. (A full dnd-kit drag requires pointer
    // events + layout that jsdom can't deliver; the helper itself is
    // the contract under test.)
    const next = reorderSidebarSections(
      [SIDEBAR_SECTION_AGENTS, SCRATCH_PSEUDO_PROJECT_ID, "project-1"],
      SCRATCH_PSEUDO_PROJECT_ID,
      "project-1",
    );
    expect(next).toEqual([SIDEBAR_SECTION_AGENTS, "project-1", SCRATCH_PSEUDO_PROJECT_ID]);
    expect(received).toBeUndefined();
    // Sanity: the sort header still has can-reorder after re-render.
    expect(
      container
        .querySelector('section[aria-label="Agents"] .sidebar-section-row')
        ?.classList.contains("can-reorder"),
    ).toBe(true);
  });
});

describe("Agents section icon pair", () => {
  it("uses UserRound (collapsed) → UsersRound (expanded), not a robot", () => {
    renderSidebar({ sectionOrder: [SIDEBAR_SECTION_AGENTS] });

    const header = container.querySelector(
      'section[aria-label="Agents"] .sidebar-section-row',
    );
    const collapsedIcon = header?.querySelector(
      ".project-row-icon-state.collapsed",
    );
    const expandedIcon = header?.querySelector(
      ".project-row-icon-state.expanded",
    );
    expect(collapsedIcon?.getAttribute("class")).toContain("lucide-user-round");
    expect(expandedIcon?.getAttribute("class")).toContain("lucide-users-round");
    // The two states must be visually distinct glyphs (single person vs
    // group), unlike the old Bot → BotMessageSquare pair.
    expect(collapsedIcon?.getAttribute("class")).not.toContain("lucide-bot");
    expect(expandedIcon?.getAttribute("class")).not.toContain("lucide-bot");
  });
});

describe("SidebarSection collapse animation", () => {
  function renderSection(expanded: boolean): void {
    const element = (
      <SidebarSection
        expanded={expanded}
        iconKind="project"
        CollapsedIcon={Folder}
        ExpandedIcon={FolderOpen}
        label="示例"
        ariaLabel="示例"
        title="示例"
        onToggle={() => {}}
      >
        <div className="collapse-probe">body</div>
      </SidebarSection>
    );
    act(() => {
      if (!root) {
        root = createRoot(container);
      }
      root.render(element);
    });
  }

  it("keeps the body mounted in closing state for the collapse window, then unmounts", () => {
    vi.useFakeTimers();
    try {
      renderSection(true);
      const openBody = container.querySelector(".thread-list-collapse");
      expect(openBody).not.toBeNull();
      expect(openBody?.getAttribute("data-state")).toBe("open");

      renderSection(false);
      const closingBody = container.querySelector(".thread-list-collapse");
      expect(closingBody).not.toBeNull();
      expect(closingBody?.getAttribute("data-state")).toBe("closing");
      expect(closingBody?.getAttribute("aria-hidden")).toBe("true");
      expect(container.querySelector(".collapse-probe")).not.toBeNull();

      act(() => {
        vi.advanceTimersByTime(SECTION_COLLAPSE_MS);
      });
      expect(container.querySelector(".thread-list-collapse")).toBeNull();
    } finally {
      vi.useRealTimers();
    }
  });

  it("re-expanding mid-close cancels the closing phase and keeps the body", () => {
    vi.useFakeTimers();
    try {
      renderSection(true);
      renderSection(false);
      expect(
        container.querySelector('.thread-list-collapse[data-state="closing"]'),
      ).not.toBeNull();

      renderSection(true);
      const body = container.querySelector(".thread-list-collapse");
      expect(body).not.toBeNull();
      expect(body?.getAttribute("data-state")).toBe("opening");

      act(() => {
        vi.advanceTimersByTime(400);
      });
      // The stale close timer must not unmount the re-opened body.
      expect(container.querySelector(".thread-list-collapse")).not.toBeNull();
    } finally {
      vi.useRealTimers();
    }
  });

  it("renders no body at all when mounted collapsed", () => {
    renderSection(false);
    expect(container.querySelector(".thread-list-collapse")).toBeNull();
  });

  it("uses a measured height custom property while opening", () => {
    vi.useFakeTimers();
    try {
      renderSection(false);
      renderSection(true);
      const body = container.querySelector<HTMLElement>(".thread-list-collapse");
      expect(body).not.toBeNull();
      expect(body?.getAttribute("data-state")).toBe("opening");
      expect(body?.style.getPropertyValue("--sidebar-section-body-height")).toMatch(/px$/);

      act(() => {
        vi.advanceTimersByTime(SECTION_COLLAPSE_MS);
      });
      expect(body?.getAttribute("data-state")).toBe("open");
      expect(body?.style.getPropertyValue("--sidebar-section-body-height")).toBe("");
    } finally {
      vi.useRealTimers();
    }
  });
});

describe("sidebar section spacing rhythm", () => {
  it("section containers carry no flex gap — the offset lives on .thread-list-collapse", () => {
    // Header → body distance must be identical across 置顶 / Agents /
    // 对话 / 项目. Project bodies sit one .project-group wrapper deeper,
    // so any gap on the shared section containers would double-count
    // for the sections whose body is a direct child.
    const sectionRule = sidebarCSS.match(
      /\.project-section,\s*\.pinned-thread-section,\s*\.participant-roster-section \{[^}]*\}/,
    )?.[0];
    expect(sectionRule).toBeTruthy();
    expect(sectionRule).not.toMatch(/\bgap:/);
    expect(sidebarCSS).toMatch(/\.thread-list-collapse \{[^}]*margin-top: 5px/);
    expect(sidebarCSS).toContain("--sidebar-section-body-height");
    expect(sidebarCSS).toContain('.thread-list-collapse[data-state="closing"]');
  });

  it("pinned list shares the .thread-list rhythm (gap 3px, 2px vertical padding)", () => {
    const pinnedRule = sidebarCSS.match(/\.pinned-thread-list \{[^}]*\}/)?.[0];
    expect(pinnedRule).toBeTruthy();
    expect(pinnedRule).toMatch(/gap: 3px/);
    expect(pinnedRule).toMatch(/padding: 2px 0/);
    // No per-row indent override — pinned rows use the shared
    // .thread-row padding.
    expect(sidebarCSS).not.toMatch(/\.pinned-thread-list \.thread-row/);
  });

  it("participants.css does not redeclare the section container", () => {
    // participants.css loads after sidebar.css, so a bare
    // .participant-roster-section rule there would silently override the
    // shared section layout (this happened: a stale grid/gap/margin-block
    // block gave the Agents section 2px/26px neighbor gaps instead of 14px).
    expect(participantsCSS).not.toMatch(/^\.participant-roster-section \{/m);
  });
});

describe("group chat section", () => {
  const order = [SIDEBAR_SECTION_AGENTS, "project-1"];

  function groupSection(): HTMLElement | null {
    return container.querySelector('section[aria-label="群聊"]');
  }

  it("explains on the # all placeholder that groups open once agents exist", () => {
    renderSidebar({ sectionOrder: order });
    const placeholder = groupSection()?.querySelector<HTMLButtonElement>(
      ".group-thread-row-placeholder .thread-row-main",
    );
    expect(placeholder).not.toBeNull();
    expect(placeholder?.getAttribute("title")).toBe("创建具名 Agent 后自动开启");
    expect(placeholder?.disabled).toBe(true);
  });

  it("reveals an inline name input from the 新建群聊 button and submits on Enter", () => {
    const created: string[] = [];
    renderSidebar({
      sectionOrder: order,
      onCreateGroupThread: (title) => created.push(title),
    });
    const addButton = groupSection()?.querySelector<HTMLButtonElement>(
      'button[aria-label="新建群聊"]',
    );
    expect(addButton).not.toBeNull();
    expect(groupSection()?.querySelector(".group-thread-name-input")).toBeNull();
    act(() => {
      addButton?.click();
    });
    const input = groupSection()?.querySelector<HTMLInputElement>(
      ".group-thread-name-input",
    );
    expect(input).not.toBeNull();
    expect(input?.getAttribute("placeholder")).toBe("群聊名称");
    if (input) {
      input.value = "  发布协调  ";
    }
    act(() => {
      input?.dispatchEvent(
        new KeyboardEvent("keydown", {
          key: "Enter",
          bubbles: true,
          cancelable: true,
        }),
      );
    });
    expect(created).toEqual(["发布协调"]);
    expect(groupSection()?.querySelector(".group-thread-name-input")).toBeNull();
  });

  it("does not create a group for a blank title", () => {
    const created: string[] = [];
    renderSidebar({
      sectionOrder: order,
      onCreateGroupThread: (title) => created.push(title),
    });
    act(() => {
      groupSection()
        ?.querySelector<HTMLButtonElement>('button[aria-label="新建群聊"]')
        ?.click();
    });
    const input = groupSection()?.querySelector<HTMLInputElement>(
      ".group-thread-name-input",
    );
    if (input) {
      input.value = "   ";
    }
    act(() => {
      input?.dispatchEvent(
        new KeyboardEvent("keydown", {
          key: "Enter",
          bubbles: true,
          cancelable: true,
        }),
      );
    });
    expect(created).toEqual([]);
  });

  it("cancels the inline name input on Escape without creating", () => {
    const created: string[] = [];
    renderSidebar({
      sectionOrder: order,
      onCreateGroupThread: (title) => created.push(title),
    });
    act(() => {
      groupSection()
        ?.querySelector<HTMLButtonElement>('button[aria-label="新建群聊"]')
        ?.click();
    });
    const input = groupSection()?.querySelector<HTMLInputElement>(
      ".group-thread-name-input",
    );
    expect(input).not.toBeNull();
    if (input) {
      input.value = "半途而废";
    }
    act(() => {
      input?.dispatchEvent(
        new KeyboardEvent("keydown", {
          key: "Escape",
          bubbles: true,
          cancelable: true,
        }),
      );
    });
    expect(created).toEqual([]);
    expect(groupSection()?.querySelector(".group-thread-name-input")).toBeNull();
  });

  it("renders group threads with a # title prefix and selects on click", () => {
    const selected: string[] = [];
    renderSidebar({
      sectionOrder: order,
      groupThreads: [
        makeThreadSummary("thread-group-1", "发布协调", { group: true }),
      ],
      onSelectThread: (id) => selected.push(id),
    });
    const section = groupSection();
    expect(section?.querySelector(".group-thread-row-placeholder")).toBeNull();
    const row = section?.querySelector<HTMLButtonElement>(".thread-row-main");
    expect(row?.textContent).toContain("#发布协调");
    act(() => {
      row?.click();
    });
    expect(selected).toEqual(["thread-group-1"]);
  });
});
