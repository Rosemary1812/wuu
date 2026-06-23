import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { ThreadContextMenu } from "./ThreadContextMenu";
import { ScratchThreadSection, ThreadRowTitle } from "./ThreadSidebar";
import type { Thread } from "../shared/protocol";

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

describe("ScratchThreadSection", () => {
  function makeScratchThread(id: string): Thread {
    return {
      id,
      preview: `Preview ${id}`,
      model_provider: "openai",
      model: "gpt-4",
      cwd: "/tmp/scratch",
      workspace_kind: "scratch",
      status: "idle",
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
      turns: [],
    };
  }

  function renderSection(props: {
    onCreateScratchThread: () => void;
    threads?: Thread[];
  }): { button: HTMLButtonElement | null } {
    act(() => {
      root = createRoot(container);
      root.render(
        <ScratchThreadSection
          threads={props.threads ?? []}
          activeID={undefined}
          pendingThreadID={undefined}
          archiveConfirmThreadID={undefined}
          lastViewedTurnByThreadID={{}}
          onSelect={() => {}}
          onSelectChildAgent={() => {}}
          onToggleThreadPinned={() => {}}
          onArchiveThread={() => {}}
          onClearArchiveConfirm={() => {}}
          onCreateScratchThread={props.onCreateScratchThread}
        />
      );
    });
    return {
      button: container.querySelector(
        ".project-add-button",
      ) as HTMLButtonElement | null,
    };
  }

  it("renders the '对话' section label", () => {
    renderSection({ onCreateScratchThread: vi.fn() });
    expect(
      container.querySelector(".scratch-thread-label")?.textContent,
    ).toBe("对话");
  });

  it("invokes onCreateScratchThread when the '+' button is clicked", () => {
    const onCreate = vi.fn();
    const { button } = renderSection({ onCreateScratchThread: onCreate });
    expect(button).not.toBeNull();
    act(() => {
      button!.click();
    });
    expect(onCreate).toHaveBeenCalledTimes(1);
  });

  it("renders an empty hint when there are no scratch threads", () => {
    renderSection({ onCreateScratchThread: vi.fn() });
    expect(
      container.querySelector(".scratch-thread-empty-note")?.textContent,
    ).toBe("还没有对话");
  });

  it("renders a show-more button when there are more than 8 scratch threads", () => {
    const threads: Thread[] = Array.from({ length: 12 }, (_, i) =>
      makeScratchThread(`scratch-${i}`)
    );
    renderSection({ onCreateScratchThread: vi.fn(), threads });
    const showMore = container.querySelector(
      ".thread-list-more"
    ) as HTMLButtonElement | null;
    expect(showMore).not.toBeNull();
    expect(showMore?.textContent ?? "").toContain("显示剩余 4 条");
  });

  it("loads 10 more scratch threads when the show-more button is clicked", () => {
    const threads: Thread[] = Array.from({ length: 12 }, (_, i) =>
      makeScratchThread(`scratch-${i}`)
    );
    renderSection({ onCreateScratchThread: vi.fn(), threads });
    expect(container.querySelectorAll(".thread-row").length).toBe(8);
    const showMore = container.querySelector(
      ".thread-list-more"
    ) as HTMLButtonElement | null;
    expect(showMore).not.toBeNull();
    act(() => {
      showMore!.click();
    });
    expect(container.querySelectorAll(".thread-row").length).toBe(12);
  });

  it("does not render a collapse button while still at the initial visible count", () => {
    const threads: Thread[] = Array.from({ length: 20 }, (_, i) =>
      makeScratchThread(`scratch-${i}`)
    );
    renderSection({ onCreateScratchThread: vi.fn(), threads });
    expect(container.querySelector(".thread-list-collapse-btn")).toBeNull();
  });

  it("renders a collapse button after expanding and resets to initial on click", () => {
    const threads: Thread[] = Array.from({ length: 20 }, (_, i) =>
      makeScratchThread(`scratch-${i}`)
    );
    renderSection({ onCreateScratchThread: vi.fn(), threads });
    expect(container.querySelectorAll(".thread-row").length).toBe(8);
    const showMore = container.querySelector(
      ".thread-list-more"
    ) as HTMLButtonElement | null;
    act(() => {
      showMore!.click();
    });
    // 8 + 10 = 18 visible after one expansion.
    expect(container.querySelectorAll(".thread-row").length).toBe(18);
    const collapse = container.querySelector(
      ".thread-list-collapse-btn"
    ) as HTMLButtonElement | null;
    expect(collapse).not.toBeNull();
    expect(collapse?.textContent ?? "").toContain("收起");
    act(() => {
      collapse!.click();
    });
    expect(container.querySelectorAll(".thread-row").length).toBe(8);
    // Once reset, the collapse button disappears because nothing is expanded.
    expect(container.querySelector(".thread-list-collapse-btn")).toBeNull();
  });
});
