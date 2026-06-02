import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { ThreadRowTitle } from "./ThreadSidebar";

let container: HTMLDivElement;
let root: Root | null = null;

beforeAll(() => {
  if (typeof globalThis.matchMedia !== "function") {
    Object.defineProperty(globalThis, "matchMedia", {
      writable: true,
      value: (query: string) => ({
        matches: false,
        media: query,
        onchange: null,
        addEventListener: () => {},
        removeEventListener: () => {},
        addListener: () => {},
        removeListener: () => {},
        dispatchEvent: () => false
      })
    });
  }
});

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

  it("uses the swap count as the React key so each swap remounts the span", () => {
    // Different keys on subsequent renders prove the remount strategy works.
    render({ title: "alpha" });
    const initialKey = (container.querySelector(".thread-row-title") as HTMLElement | null)?.getAttribute("data-title-swap") ?? null;
    expect(initialKey).toBeNull();

    act(() => {
      root!.render(<ThreadRowTitle title="beta" />);
    });
    const firstSwapKey = (container.querySelector(".thread-row-title") as HTMLElement | null)?.getAttribute("data-title-swap");
    expect(firstSwapKey).toBe("1");

    act(() => {
      root!.render(<ThreadRowTitle title="gamma" />);
    });
    const secondSwapKey = (container.querySelector(".thread-row-title") as HTMLElement | null)?.getAttribute("data-title-swap");
    expect(secondSwapKey).toBe("2");
  });
});
