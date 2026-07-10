/**
 * Tests for JumpToLatestPill — the self-contained "跳到最新" pill (issue #5).
 * The pill owns its scroll listener against the containerRef it is given,
 * shows only when the container is scrolled away from the bottom, and
 * smooth-scrolls that same container (and only it) on click. jsdom does no
 * layout, so scroll geometry is stubbed directly on the container node.
 */
import { act } from "react";
import { createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { JumpToLatestPill } from "./JumpToLatestPill";
import {
  WINDOW_RESIZE_SETTLE_DELAY_MS,
  WINDOW_RESIZING_CLASS,
} from "./WindowResizeState";

let mountedRoots: Root[] = [];
let mountedContainers: HTMLElement[] = [];
let restoreResizeObserver: (() => void) | undefined;

afterEach(() => {
  for (const root of mountedRoots) {
    act(() => {
      root.unmount();
    });
  }
  for (const container of mountedContainers) {
    container.remove();
  }
  mountedRoots = [];
  mountedContainers = [];
  restoreResizeObserver?.();
  restoreResizeObserver = undefined;
  document.documentElement.classList.remove(WINDOW_RESIZING_CLASS);
  vi.useRealTimers();
});

type ScrollGeometry = {
  scrollHeight: number;
  clientHeight: number;
  scrollTop: number;
};

function scrollContainer(geometry: ScrollGeometry): {
  node: HTMLDivElement;
  scrollTo: ReturnType<typeof vi.fn>;
  setScrollTop: (top: number) => void;
} {
  const node = document.createElement("div");
  document.body.appendChild(node);
  mountedContainers.push(node);
  let scrollTop = geometry.scrollTop;
  Object.defineProperty(node, "scrollHeight", {
    configurable: true,
    get: () => geometry.scrollHeight,
  });
  Object.defineProperty(node, "clientHeight", {
    configurable: true,
    get: () => geometry.clientHeight,
  });
  Object.defineProperty(node, "scrollTop", {
    configurable: true,
    get: () => scrollTop,
    set: (value: number) => {
      scrollTop = value;
    },
  });
  const scrollTo = vi.fn();
  Object.defineProperty(node, "scrollTo", {
    configurable: true,
    value: scrollTo,
  });
  return {
    node,
    scrollTo,
    setScrollTop: (top: number) => {
      scrollTop = top;
    },
  };
}

function stubRect(node: HTMLElement, rect: Partial<DOMRect>): void {
  node.getBoundingClientRect = () =>
    ({
      left: 0,
      top: 0,
      right: 0,
      bottom: 0,
      width: 0,
      height: 0,
      x: 0,
      y: 0,
      toJSON: () => ({}),
      ...rect,
    }) as DOMRect;
}

function mountPill(node: HTMLElement): HTMLElement {
  const host = document.createElement("div");
  node.appendChild(host);
  const root = createRoot(host);
  act(() => {
    root.render(
      createElement(JumpToLatestPill, { containerRef: { current: node } }),
    );
  });
  mountedRoots.push(root);
  return host;
}

type MockResizeObserverRecord = {
  callback: ResizeObserverCallback;
  observed: Set<Element>;
};

function installMockResizeObserver(): MockResizeObserverRecord[] {
  const records: MockResizeObserverRecord[] = [];
  const globalWithResizeObserver = globalThis as typeof globalThis & {
    ResizeObserver?: typeof ResizeObserver;
  };
  const realResizeObserver = globalWithResizeObserver.ResizeObserver;
  class MockResizeObserver {
    private readonly record: MockResizeObserverRecord;

    constructor(callback: ResizeObserverCallback) {
      this.record = { callback, observed: new Set() };
      records.push(this.record);
    }

    observe(target: Element): void {
      this.record.observed.add(target);
    }

    unobserve(target: Element): void {
      this.record.observed.delete(target);
    }

    disconnect(): void {
      this.record.observed.clear();
    }
  }
  globalWithResizeObserver.ResizeObserver =
    MockResizeObserver as typeof ResizeObserver;
  restoreResizeObserver = () => {
    if (realResizeObserver) {
      globalWithResizeObserver.ResizeObserver = realResizeObserver;
    } else {
      Reflect.deleteProperty(globalWithResizeObserver, "ResizeObserver");
    }
  };
  return records;
}

function flushResizeObservers(records: MockResizeObserverRecord[]): void {
  for (const record of records) {
    const entries = Array.from(record.observed).map((target) => ({
      target,
      contentRect: {
        x: 0,
        y: 0,
        top: 0,
        left: 0,
        right: 0,
        bottom: 0,
        width: 0,
        height: 0,
        toJSON: () => ({}),
      },
    })) as ResizeObserverEntry[];
    record.callback(entries, {} as ResizeObserver);
  }
}

describe("JumpToLatestPill", () => {
  it("stays hidden at the bottom and appears when scrolled away", () => {
    const { node, setScrollTop } = scrollContainer({
      scrollHeight: 1000,
      clientHeight: 400,
      scrollTop: 600, // exactly at the bottom
    });
    const host = mountPill(node);
    expect(host.querySelector(".jump-to-latest-pill")).toBeNull();

    act(() => {
      setScrollTop(0); // 600px from the bottom > 80px threshold
      node.dispatchEvent(new Event("scroll"));
    });
    const pill = host.querySelector<HTMLButtonElement>(".jump-to-latest-pill");
    expect(pill).not.toBeNull();
    expect(pill?.className).toContain("jump-to-latest-pill-sticky-centered");
  });

  it("shows immediately when mounted into an already scrolled-away container", () => {
    const { node } = scrollContainer({
      scrollHeight: 1000,
      clientHeight: 400,
      scrollTop: 0,
    });
    const host = mountPill(node);
    expect(host.querySelector(".jump-to-latest-pill")).not.toBeNull();
  });

  it("smooth-scrolls its own container to the bottom on click", () => {
    const { node, scrollTo } = scrollContainer({
      scrollHeight: 1000,
      clientHeight: 400,
      scrollTop: 0,
    });
    const host = mountPill(node);
    act(() => {
      host
        .querySelector<HTMLButtonElement>(".jump-to-latest-pill")
        ?.click();
    });
    expect(scrollTo).toHaveBeenCalledWith({ top: 1000, behavior: "smooth" });
  });

  it("hides again once the container scrolls back within the threshold", () => {
    const { node, setScrollTop } = scrollContainer({
      scrollHeight: 1000,
      clientHeight: 400,
      scrollTop: 0,
    });
    const host = mountPill(node);
    expect(host.querySelector(".jump-to-latest-pill")).not.toBeNull();
    act(() => {
      setScrollTop(560); // 40px from the bottom, inside the 80px threshold
      node.dispatchEvent(new Event("scroll"));
    });
    expect(host.querySelector(".jump-to-latest-pill")).toBeNull();
  });

  it("anchors above the composer via a body portal when bottomAnchor is given", () => {
    const { node } = scrollContainer({
      scrollHeight: 1000,
      clientHeight: 400,
      scrollTop: 0, // scrolled away → visible
    });
    // scroll region occupies the top portion of the viewport; its own bottom
    // (800) sits well ABOVE the composer — the exact case the sticky variant
    // got wrong.
    stubRect(node, { left: 100, top: 56, bottom: 800, width: 600, height: 744 });
    const anchor = document.createElement("div");
    document.body.appendChild(anchor);
    mountedContainers.push(anchor);
    stubRect(anchor, { left: 100, top: 700, bottom: 780, width: 600, height: 80 });
    Object.defineProperty(window, "innerHeight", {
      configurable: true,
      value: 900,
    });

    const host = document.createElement("div");
    node.appendChild(host);
    const root = createRoot(host);
    act(() => {
      root.render(
        createElement(JumpToLatestPill, {
          containerRef: { current: node },
          bottomAnchor: anchor,
        }),
      );
    });
    mountedRoots.push(root);

    // Portaled to <body>, NOT rendered inside the scroll container.
    expect(host.querySelector(".jump-to-latest-pill")).toBeNull();
    const pill = document.body.querySelector<HTMLButtonElement>(
      ".jump-to-latest-pill-anchored",
    );
    expect(pill).not.toBeNull();
    // left = container.left + container.width / 2 = 100 + 300
    expect(pill?.style.left).toBe("400px");
    // bottom = innerHeight - anchor.top + gap(12) = 900 - 700 + 12
    expect(pill?.style.bottom).toBe("212px");
  });

  it("notifies parent via onScrolledAwayChange when the scroll-away boolean flips", () => {
    // Regression (2026-07-10): the main conversation used to keep the jump
    // pill and the active-plan progress pill in two stacked slots, because
    // the jump pill's scrolled-away state lived inside the component. The
    // parent needs that signal to swap a sibling progress pill in and out of
    // the same composer-adjacent slot. Verify the callback fires on mount
    // AND whenever the boolean flips via a real scroll event.
    const { node, setScrollTop } = scrollContainer({
      scrollHeight: 1000,
      clientHeight: 400,
      scrollTop: 0, // distanceFromBottom = 600 > 80 → scrolledAway = true
    });
    const onScrolledAwayChange = vi.fn();
    const host = document.createElement("div");
    node.appendChild(host);
    const root = createRoot(host);
    act(() => {
      root.render(
        createElement(JumpToLatestPill, {
          containerRef: { current: node },
          onScrolledAwayChange,
        }),
      );
    });
    mountedRoots.push(root);
    expect(onScrolledAwayChange).toHaveBeenLastCalledWith(true);

    // Scroll back to the bottom — the boolean should flip to false.
    setScrollTop(600);
    act(() => {
      node.dispatchEvent(new Event("scroll"));
    });
    expect(onScrolledAwayChange).toHaveBeenLastCalledWith(false);

    // Scroll away again — should flip back to true.
    setScrollTop(0);
    act(() => {
      node.dispatchEvent(new Event("scroll"));
    });
    expect(onScrolledAwayChange).toHaveBeenLastCalledWith(true);
  });

  it("re-evaluates visibility on container resize (thread panel drag)", () => {
    // F1 resize: when the thread panel is dragged, the scroll container's
    // clientHeight changes. The pill must re-evaluate its scrolled-away
    // state — otherwise a container shrink could leave the pill stuck
    // "scrolled away" at the new (now-taller) bottom.
    const { node } = scrollContainer({
      scrollHeight: 1000,
      clientHeight: 100, // narrow initially
      scrollTop: 0, // user is at the top
    });
    const host = mountPill(node);
    // distanceFromBottom = 1000 - 0 - 100 = 900 > 80 → visible
    expect(host.querySelector(".jump-to-latest-pill")).not.toBeNull();

    // Container grows: clientHeight 100 → 1000. distanceFromBottom = 0.
    Object.defineProperty(node, "clientHeight", {
      configurable: true,
      get: () => 1000,
    });
    // jsdom has no native ResizeObserver; the component re-evaluates on
    // scroll, so we dispatch a scroll event to simulate the observer firing.
    act(() => {
      node.dispatchEvent(new Event("scroll"));
    });
    expect(host.querySelector(".jump-to-latest-pill")).toBeNull();
  });

  it("defers container resize measurement while the window is resizing", () => {
    vi.useFakeTimers();
    const resizeObservers = installMockResizeObserver();
    let clientHeight = 100;
    let liveResizeMetricReads = 0;
    const { node } = scrollContainer({
      scrollHeight: 1000,
      clientHeight,
      scrollTop: 0,
    });
    Object.defineProperty(node, "scrollHeight", {
      configurable: true,
      get: () => {
        if (document.documentElement.classList.contains(WINDOW_RESIZING_CLASS)) {
          liveResizeMetricReads += 1;
        }
        return 1000;
      },
    });
    Object.defineProperty(node, "clientHeight", {
      configurable: true,
      get: () => {
        if (document.documentElement.classList.contains(WINDOW_RESIZING_CLASS)) {
          liveResizeMetricReads += 1;
        }
        return clientHeight;
      },
    });
    const host = mountPill(node);
    expect(host.querySelector(".jump-to-latest-pill")).not.toBeNull();

    act(() => {
      document.documentElement.classList.add(WINDOW_RESIZING_CLASS);
      clientHeight = 1000;
      flushResizeObservers(resizeObservers);
    });

    expect(liveResizeMetricReads).toBe(0);
    expect(host.querySelector(".jump-to-latest-pill")).not.toBeNull();

    act(() => {
      document.documentElement.classList.remove(WINDOW_RESIZING_CLASS);
      vi.advanceTimersByTime(WINDOW_RESIZE_SETTLE_DELAY_MS + 1);
    });

    expect(host.querySelector(".jump-to-latest-pill")).toBeNull();
  });
});
