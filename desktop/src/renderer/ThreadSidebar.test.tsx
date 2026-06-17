import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { ThreadContextMenu } from "./ThreadContextMenu";
import { ThreadRowTitle } from "./ThreadSidebar";

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
});
