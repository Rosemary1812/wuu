import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { act, createRef } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type {
  InitializeResult,
  ParticipantProfile,
  ParticipantSaveParams,
} from "../shared/protocol";
import { AppSidebar, SIDEBAR_SECTION_AGENTS } from "./AppSidebar";
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

// Drives React's controlled onChange handler for the SidebarNameDialog
// input: setting `input.value` directly bypasses the controlled state
// and the dialog's submit button stays disabled. Mirrors the helper in
// AppSidebarSections.test.tsx.
function setControlledInputValue(input: HTMLInputElement, value: string): void {
  const setter = Object.getOwnPropertyDescriptor(
    HTMLInputElement.prototype,
    "value",
  )?.set;
  setter?.call(input, value);
  input.dispatchEvent(new Event("input", { bubbles: true }));
}

function renderSidebar({
  projectMenuOpen = false,
  pinnedThreads = [],
  participants = [],
  busyParticipantIDs = new Set<string>(),
  onTogglePinned = () => {},
  onImportParticipants = () => {},
  onExportParticipants = () => {},
  onSelectParticipant = () => {},
  onCreateParticipant = async () => null as never,
  onStartNewThread = () => {},
}: {
  projectMenuOpen?: boolean;
  pinnedThreads?: ThreadSummary[];
  participants?: ParticipantProfile[];
  busyParticipantIDs?: Set<string>;
  onTogglePinned?: (thread: ThreadSummary) => void;
  onImportParticipants?: (file: File) => void;
  onExportParticipants?: () => void;
  onSelectParticipant?: (participant: ParticipantProfile) => void;
  onCreateParticipant?: (
    params: ParticipantSaveParams,
  ) => Promise<ParticipantProfile>;
  onStartNewThread?: () => void;
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
        sidebarProjects={[
          {
            id: SCRATCH_PSEUDO_PROJECT_ID,
            name: "对话",
            path: "",
            created_at: "2026-01-01T00:00:00Z",
            updated_at: "2026-01-01T00:00:00Z",
          },
        ]}
        pinnedThreads={pinnedThreads}
        groupThreads={[]}
        activeThreadID={undefined}
        activeDMParticipantID={undefined}
        dmThreadByParticipantID={new Map()}
        unreadDMParticipantIDs={new Set()}
        participants={participants}
        busyParticipantIDs={busyParticipantIDs}
        pendingThreadID={undefined}
        pendingProjectID={undefined}
        archiveConfirmThreadID={undefined}
        collapsedSidebarSectionIDs={new Set()}
        expandedSidebarSectionIDs={new Set()}
        collapsingSidebarSectionIDs={new Set()}
        projectThreadsByProjectID={{}}
        projectMenuOpen={projectMenuOpen}
        projectMenuRef={createRef<HTMLDivElement>()}
        searchOpen={false}
        debugFixturesVisible={false}
        sectionOrder={[SIDEBAR_SECTION_AGENTS, SCRATCH_PSEUDO_PROJECT_ID]}
        onStartNewThread={onStartNewThread}
        onOpenSkillsTab={() => {}}
        onToggleConversationSearch={() => {}}
        onSeedConversationFixture={() => {}}
        onSeedAgentTreeDemo={() => {}}
        onOpenChipGallery={() => {}}
        onSelectThread={() => {}}
        onSelectParticipant={onSelectParticipant}
        onEditParticipant={() => {}}
        onCreateParticipant={onCreateParticipant}
        onImportParticipants={onImportParticipants}
        onExportParticipants={onExportParticipants}
        onTogglePinned={onTogglePinned}
        onArchiveThread={() => {}}
        onDeleteThread={() => {}}
        onRenameThread={() => {}}
        onClearArchiveConfirm={() => {}}
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
    expect(sidebarCSS).toContain("--sidebar-motion-ease: cubic-bezier(0.25, 1, 0.5, 1)");
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

  // R1: this is the sidebar's own "新对话" entry (as opposed to a
  // project row's hover "+" or the session-tab strip's "+"). App.tsx wires
  // it to startNewThread({ resetToNoProject: true }) so it always lands on
  // a fresh no-project draft, regardless of which project is active; this
  // test just pins that the button fires the callback AppSidebar was given
  // (the reset decision itself is unit-tested in AppState.test.ts via
  // shouldResetToNoProjectForNewThread).
  it("fires onStartNewThread when the primary-nav 新对话 button is clicked", () => {
    let started = 0;
    renderSidebar({ onStartNewThread: () => { started += 1; } });

    const primaryNav = container.querySelector(".primary-nav");
    const newThreadButton = Array.from(
      primaryNav?.querySelectorAll("button.nav-item") ?? [],
    ).find((button) => button.textContent?.includes("新对话"));
    expect(newThreadButton).toBeTruthy();

    act(() => {
      (newThreadButton as HTMLButtonElement).click();
    });
    expect(started).toBe(1);
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

describe("AppSidebar participant roster", () => {
  const participants: ParticipantProfile[] = [
    {
      id: "p-image",
      kind: "named",
      name: "Image Agent",
      role: "writer",
      avatar_image: "data:image/png;base64,AAA",
    },
    {
      id: "p-plain",
      kind: "named",
      name: "Plain Agent",
      role: "reader",
    },
    {
      id: "p-bare",
      kind: "named",
      name: "Bare Agent",
    },
  ];

  it("shows an overflow menu with import/export items that fire callbacks", () => {
    let importedWith: File | undefined;
    let exported = 0;
    renderSidebar({
      participants,
      onImportParticipants: (file) => {
        importedWith = file;
      },
      onExportParticipants: () => {
        exported += 1;
      },
    });

    // Bare icon buttons should be gone — the import/export actions live in
    // the overflow menu triggered by the team-template trigger button.
    expect(
      container.querySelector('button[aria-label="导入团队模板"]'),
    ).toBeNull();
    expect(
      container.querySelector('button[aria-label="导出团队模板"]'),
    ).toBeNull();

    const trigger = container.querySelector<HTMLButtonElement>(
      'button[aria-label="团队模板操作"]',
    );
    expect(trigger).not.toBeNull();
    expect(trigger?.getAttribute("aria-expanded")).toBe("false");

    act(() => {
      trigger?.click();
    });
    expect(trigger?.getAttribute("aria-expanded")).toBe("true");

    const menu = container.querySelector(".participant-roster-menu .project-add-menu");
    expect(menu).not.toBeNull();
    const items = menu?.querySelectorAll<HTMLButtonElement>('button[role="menuitem"]');
    expect(items?.length).toBe(2);
    expect(items?.[0].textContent).toContain("导入团队模板");
    expect(items?.[1].textContent).toContain("导出团队模板");

    // The hidden file input is the import path. Clicking the menu item
    // dispatches a click() on the input, so drive the same code path by
    // synthesising a File on the input directly — that exercises the
    // callback that App.tsx wires in.
    const fileInput = container.querySelector<HTMLInputElement>(
      ".participant-roster-file-input",
    );
    expect(fileInput).not.toBeNull();
    const file = new File(["{}"], "team.json", { type: "application/json" });
    act(() => {
      Object.defineProperty(fileInput, "files", {
        configurable: true,
        value: [file],
      });
      fileInput?.dispatchEvent(new Event("change", { bubbles: true }));
    });
    expect(importedWith?.name).toBe("team.json");

    act(() => {
      items?.[1].click();
    });
    expect(exported).toBe(1);
  });

  it("renders the avatar column only for uploaded images and shows only the name", () => {
    renderSidebar({ participants });

    const rows = Array.from(
      container.querySelectorAll<HTMLButtonElement>(".participant-roster-row"),
    );
    const byName = new Map<string, HTMLButtonElement>();
    for (const row of rows) {
      const name = row.querySelector(".participant-roster-name")?.textContent ?? "";
      byName.set(name, row);
    }

    const imageRow = byName.get("Image Agent");
    expect(imageRow?.querySelector("img.participant-roster-avatar-image")).not.toBeNull();
    expect(imageRow?.querySelector(".participant-roster-avatar-image")?.getAttribute("src"))
      .toBe("data:image/png;base64,AAA");

    // Without an uploaded avatar there is no placeholder glyph and no
    // reserved avatar column — the name flows right after the status dot.
    const plainRow = byName.get("Plain Agent");
    expect(plainRow?.querySelector(".participant-roster-avatar")).toBeNull();

    const bareRow = byName.get("Bare Agent");
    expect(bareRow?.querySelector(".participant-roster-avatar")).toBeNull();

    // Rows carry no tagline/role meta line: the roster shows the name only.
    for (const row of rows) {
      expect(row.querySelector(".participant-roster-meta")).toBeNull();
    }
  });

  it("marks busy participants with a busy status dot", () => {
    renderSidebar({
      participants,
      busyParticipantIDs: new Set(["p-plain"]),
    });

    const rows = Array.from(
      container.querySelectorAll<HTMLButtonElement>(".participant-roster-row"),
    );
    const statusByName = new Map<string, string | null | undefined>();
    for (const row of rows) {
      const name = row.querySelector(".participant-roster-name")?.textContent ?? "";
      statusByName.set(
        name,
        row.querySelector(".participant-roster-status")?.getAttribute("data-status"),
      );
    }

    expect(statusByName.get("Image Agent")).toBe("online");
    expect(statusByName.get("Plain Agent")).toBe("busy");
    expect(statusByName.get("Bare Agent")).toBe("online");
  });

  it("renders the empty-state CTA when there are no participants", async () => {
    // The empty-state row opens the NewParticipantDialog — a self-contained
    // popup that collects every field (name + role + tagline + model +
    // avatar) and calls onCreateParticipant with full save params. The
    // test asserts:
    //   1. clicking the row reveals the floating dialog
    //   2. filling the name field enables submit
    //   3. submitting the dialog forwards the trimmed name to
    //      onCreateParticipant and closes (awaited — the dialog only
    //      closes once the parent's save promise resolves)
    const created: ParticipantSaveParams[] = [];
    renderSidebar({
      onCreateParticipant: async (params) => {
        created.push(params);
        // The dialog treats any truthy return as a successful save and
        // closes itself. Return a minimal saved-profile stub.
        return {
          id: `p-${params.name}`,
          kind: "named",
          name: params.name,
          role: params.role,
          tagline: params.tagline,
          model: params.model,
        };
      },
    });

    const empty = container.querySelector<HTMLButtonElement>(
      ".participant-roster-row.empty",
    );
    expect(empty).not.toBeNull();
    expect(empty?.textContent).toContain("添加 Agent");
    expect(document.body.querySelector(".new-participant-dialog")).toBeNull();
    act(() => {
      empty?.click();
    });

    const dialog = document.body.querySelector(".new-participant-dialog");
    expect(dialog).not.toBeNull();
    expect(
      dialog?.querySelector(".new-participant-title")?.textContent,
    ).toBe("新建 Agent");
    const input = dialog?.querySelector<HTMLInputElement>(
      'input[data-field="name"]',
    );
    expect(input).not.toBeNull();
    expect(input?.getAttribute("placeholder")).toBe("例如 Noel");
    setControlledInputValue(input!, "  Noel  ");
    const submitButton = Array.from(
      dialog?.querySelectorAll("button[type=submit]") ?? [],
    ).find((el) => /\u521b\u5efa/.test(el.textContent ?? ""));
    expect(submitButton).not.toBeUndefined();
    await act(async () => {
      submitButton?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      // Let the parent's save promise resolve so the dialog's onClose
      // fires and the portal node unmounts before the final assertion.
      await Promise.resolve();
    });
    expect(created.map((c) => c.name)).toEqual(["Noel"]);
    expect(document.body.querySelector(".new-participant-dialog")).toBeNull();
  });
});
