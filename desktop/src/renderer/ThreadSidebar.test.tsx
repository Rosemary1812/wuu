import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { ThreadContextMenu } from "./ThreadContextMenu";
import { ProjectList, ThreadRowTitle } from "./ThreadSidebar";
import type { DesktopProject, Thread } from "../shared/protocol";
import { SCRATCH_PSEUDO_PROJECT_ID, summarizeThreadsForSidebar } from "./AppState";

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

function render(props: { title: string }): { span: HTMLSpanElement | null; getKey: () => string | null } {
  act(() => {
    root = createRoot(container);
    root!.render(<ThreadRowTitle {...props} />);
  });
  const span = container.querySelector(".thread-row-title") as HTMLSpanElement | null;
  return {
    span,
    getKey: () => (span ? span.getAttribute("data-title-swap") : null)
  };
}

describe("ThreadRowTitle", () => {
  it("renders the title text", () => {
    const { span } = render({ title: "Fix login crash" });
    expect(span?.textContent).toBe("Fix login crash");
  });

  it("does not animate on initial mount (no data-title-swap attribute)", () => {
    // The crossfade must only fire on swaps after mount. Cold-boot and
    // project-switch hydration should remain still, otherwise the entire
    // sidebar looks like a loading state.
    const { span, getKey } = render({ title: "Fix login crash" });
    expect(getKey()).toBeNull();
    expect(span?.getAttribute("data-title-swap")).toBeNull();
  });

  it("sets data-title-swap on first prop change so CSS animation fires", () => {
    // Initial mount: no attribute.
    const { getKey: getKey1 } = render({ title: "first user query" });
    expect(getKey1()).toBeNull();

    // Same prop re-render: still no attribute.
    act(() => {
      root!.render(<ThreadRowTitle title="first user query" />);
    });
    expect(getKey1()).toBeNull();

    // Different prop: counter increments, attribute is set, span remounts.
    let currentSpan: HTMLSpanElement | null = null;
    act(() => {
      root!.render(<ThreadRowTitle title="Fix login crash" />);
    });
    currentSpan = container.querySelector(".thread-row-title");
    expect(currentSpan?.getAttribute("data-title-swap")).toBe("1");
    expect(currentSpan?.textContent).toBe("Fix login crash");
  });

  it("increments data-title-swap on subsequent swaps", () => {
    render({ title: "v0" });

    act(() => {
      root!.render(<ThreadRowTitle title="v1" />);
    });
    expect(container.querySelector(".thread-row-title")?.getAttribute("data-title-swap")).toBe("1");

    act(() => {
      root!.render(<ThreadRowTitle title="v2" />);
    });
    expect(container.querySelector(".thread-row-title")?.getAttribute("data-title-swap")).toBe("2");

    act(() => {
      root!.render(<ThreadRowTitle title="v3" />);
    });
    expect(container.querySelector(".thread-row-title")?.getAttribute("data-title-swap")).toBe("3");
  });

});

describe("ThreadContextMenu", () => {
  function renderMenu(): { onSelect: ReturnType<typeof vi.fn>; onClose: ReturnType<typeof vi.fn> } {
    const onSelect = vi.fn();
    const onClose = vi.fn();
    act(() => {
      root = createRoot(container);
      root.render(
        <ThreadContextMenu
          x={10}
          y={20}
          items={[{ label: "复制 thread ID", onSelect }]}
          onClose={onClose}
        />
      );
    });
    return { onSelect, onClose };
  }

  it("renders a menu with one item per entry", () => {
    renderMenu();
    const menu = container.querySelector('[role="menu"]');
    const items = container.querySelectorAll('[role="menuitem"]');
    expect(menu).not.toBeNull();
    expect(items.length).toBe(1);
    expect(items[0]?.textContent).toBe("复制 thread ID");
  });

  it("invokes onSelect and onClose when an item is clicked", () => {
    const { onSelect, onClose } = renderMenu();
    const button = container.querySelector("button") as HTMLButtonElement | null;
    expect(button).not.toBeNull();
    act(() => {
      button!.click();
    });
    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("closes when Escape is pressed", () => {
    const { onClose } = renderMenu();
    act(() => {
      document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("renders multiple items in the order they were provided", () => {
    const onA = vi.fn();
    const onB = vi.fn();
    act(() => {
      root = createRoot(container);
      root.render(
        <ThreadContextMenu
          x={10}
          y={20}
          items={[
            { label: "A", onSelect: onA },
            { label: "B", onSelect: onB },
          ]}
          onClose={() => {}}
        />
      );
    });
    const items = container.querySelectorAll('[role="menuitem"]');
    expect(items.length).toBe(2);
    expect(items[0]?.textContent).toBe("A");
    expect(items[1]?.textContent).toBe("B");
  });

  it("invokes only the clicked item's onSelect", () => {
    const onA = vi.fn();
    const onB = vi.fn();
    act(() => {
      root = createRoot(container);
      root.render(
        <ThreadContextMenu
          x={10}
          y={20}
          items={[
            { label: "A", onSelect: onA },
            { label: "B", onSelect: onB },
          ]}
          onClose={() => {}}
        />
      );
    });
    const firstButton = container.querySelectorAll('[role="menuitem"]')[0] as HTMLButtonElement;
    act(() => {
      firstButton.click();
    });
    expect(onA).toHaveBeenCalledTimes(1);
    expect(onB).toHaveBeenCalledTimes(0);
  });
});

describe("ProjectList", () => {
  function makeProject(id: string, name: string, path: string): DesktopProject {
    return {
      id,
      name,
      path,
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    };
  }

  function makeProjectThread(
    id: string,
    cwd: string,
    title: string,
    turns: Array<{
      id: string;
      status: "completed" | "in_progress" | "failed" | "interrupted";
    }> = [],
    overrides: Partial<Thread> = {},
  ): Thread {
    return {
      id,
      preview: title,
      title,
      model_provider: "openai",
      model: "gpt-4",
      cwd,
      workspace_kind: "project",
      status: "idle",
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
      turns: turns.map((turn) => ({
        id: turn.id,
        items: [],
        items_view: "full" as const,
        status: turn.status,
      })),
      ...overrides,
    };
  }

  it("can show session lists for multiple expanded projects", () => {
    const projects = [
      makeProject("project-1", "wuu", "/repo/wuu"),
      makeProject("project-2", "interview", "/repo/interview"),
    ];
    act(() => {
      root = createRoot(container);
      root.render(
        <ProjectList
          projects={projects}
          activeID="project-1"
          pendingProjectID={undefined}
          collapsedProjectIDs={new Set()}
          expandedProjectIDs={new Set(["project-2"])}
          collapsingProjectIDs={new Set()}
          threadsByProjectID={{
            "project-1": summarizeThreadsForSidebar([
              makeProjectThread("thread-wuu", "/repo/wuu", "Wuu session"),
            ]),
            "project-2": summarizeThreadsForSidebar([
              makeProjectThread(
                "thread-wrong-project",
                "/repo/wuu",
                "Wrong duplicate",
              ),
              makeProjectThread(
                "thread-interview",
                "/repo/interview",
                "Interview session",
              ),
            ]),
          }}
          activeThreadID={undefined}
          pendingThreadID={undefined}
          archiveConfirmThreadID={undefined}
          lastViewedTurnByThreadID={{}}
          scratchPseudoProjectID={SCRATCH_PSEUDO_PROJECT_ID}
          scratchPseudoActive={false}
          onToggleProjectCollapsed={() => {}}
          onStartNewThread={() => {}}
          onSelectThread={() => {}}
          onToggleThreadPinned={() => {}}
          onArchiveThread={() => {}}
          onClearArchiveConfirm={() => {}}
        />,
      );
    });

    const projectRows = container.querySelectorAll(".project-row");
    expect(projectRows[0]?.getAttribute("aria-expanded")).toBe("true");
    expect(projectRows[1]?.getAttribute("aria-expanded")).toBe("true");
    expect(container.textContent).toContain("Wuu session");
    expect(container.textContent).toContain("Interview session");
    expect(container.textContent).not.toContain("Wrong duplicate");
  });

  it("shows project-level unread state for collapsed unread threads", () => {
    const projects = [makeProject("project-1", "wuu", "/repo/wuu")];

    act(() => {
      root = createRoot(container);
      root.render(
        <ProjectList
          projects={projects}
          activeID={undefined}
          pendingProjectID={undefined}
          collapsedProjectIDs={new Set()}
          expandedProjectIDs={new Set()}
          collapsingProjectIDs={new Set()}
          threadsByProjectID={{
            "project-1": summarizeThreadsForSidebar([
              makeProjectThread("thread-unread", "/repo/wuu", "Unread session", [
                { id: "turn-unread", status: "completed" },
              ]),
            ]),
          }}
          activeThreadID={undefined}
          pendingThreadID={undefined}
          archiveConfirmThreadID={undefined}
          lastViewedTurnByThreadID={{}}
          scratchPseudoProjectID={SCRATCH_PSEUDO_PROJECT_ID}
          scratchPseudoActive={false}
          onToggleProjectCollapsed={() => {}}
          onStartNewThread={() => {}}
          onSelectThread={() => {}}
          onToggleThreadPinned={() => {}}
          onArchiveThread={() => {}}
          onClearArchiveConfirm={() => {}}
        />,
      );
    });

    const projectRow = container.querySelector(".project-row");
    expect(projectRow?.classList.contains("has-unread")).toBe(true);
    expect(projectRow?.getAttribute("aria-label")).toContain("有未读会话");
    expect(projectRow?.querySelector(".project-row-unread")).not.toBeNull();
  });

  it("marks only the visible fork endpoint in a chained fork list", () => {
    const projects = [makeProject("project-1", "wuu", "/repo/wuu")];
    const rootThread = makeProjectThread("root-thread", "/repo/wuu", "Root session");
    const middleThread = makeProjectThread(
      "middle-thread",
      "/repo/wuu",
      "Middle session",
      [],
      { forked_from_id: rootThread.id },
    );
    const leafThread = makeProjectThread(
      "leaf-thread",
      "/repo/wuu",
      "Leaf session",
      [],
      { forked_from_id: middleThread.id },
    );

    act(() => {
      root = createRoot(container);
      root.render(
        <ProjectList
          projects={projects}
          activeID="project-1"
          pendingProjectID={undefined}
          collapsedProjectIDs={new Set()}
          expandedProjectIDs={new Set()}
          collapsingProjectIDs={new Set()}
          threadsByProjectID={{
            "project-1": summarizeThreadsForSidebar([
              rootThread,
              middleThread,
              leafThread,
            ]),
          }}
          activeThreadID={undefined}
          pendingThreadID={undefined}
          archiveConfirmThreadID={undefined}
          lastViewedTurnByThreadID={{}}
          scratchPseudoProjectID={SCRATCH_PSEUDO_PROJECT_ID}
          scratchPseudoActive={false}
          onToggleProjectCollapsed={() => {}}
          onStartNewThread={() => {}}
          onSelectThread={() => {}}
          onToggleThreadPinned={() => {}}
          onArchiveThread={() => {}}
          onClearArchiveConfirm={() => {}}
        />,
      );
    });

    expect(container.querySelectorAll(".thread-row-fork-icon").length).toBe(1);
    const middleRow = container.querySelector(
      '.thread-row-main[aria-label^="Middle session"]',
    );
    const leafRow = container.querySelector(
      '.thread-row-main[aria-label^="Middle session，分叉自其他会话"]',
    );
    expect(middleRow?.getAttribute("aria-label")).not.toContain("分叉自其他会话");
    expect(leafRow).not.toBeNull();
  });
});
