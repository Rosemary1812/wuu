/**
 * Stress test: simulate a high-frequency token stream and verify the
 * conversation pane's scroll position stays pinned to the bottom edge.
 *
 * The setup:
 * - The conversation has more content than the viewport, so scrollTop
 *   is at scrollHeight - clientHeight when "at the bottom".
 * - We simulate 120 stream ticks in a row, each one bumping
 *   scrollHeight by a few pixels. For each tick we:
 *     1. bump the stubbed layout (the "commit" of the new content);
 *     2. call scheduleStreamScroll (what the server-event path does);
 *     3. fire the RAF callback (what the browser does next frame);
 *     4. dispatch a scroll event (what the browser does after
 *        scrollTop assignment);
 *     5. assert the resulting scroll position is still the bottom.
 *
 * If `userScrolledAway` ever flips to `true` or the scroll position
 * drifts above scrollHeight - clientHeight, the high-frequency stream
 * is racing itself and we need to fix it.
 */
import { act, createElement, type ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { useConversationScrollState } from "./ConversationScrollState";
import type { Turn } from "../shared/protocol";
import { AUTO_FOLLOW_NESTED_SCROLL_ATTR } from "./AutoFollowScroll";

function makeLongTurns(): Turn[] {
  return [
    {
      id: "turn-1",
      items: [],
      items_view: "full",
      status: "completed",
    },
  ];
}

type StubbedLayout = {
  scrollHeight: number;
  clientHeight: number;
  scrollTop: number;
};

function stubLayout(node: HTMLElement, opts: Partial<StubbedLayout>): StubbedLayout {
  const layout = {
    scrollHeight: opts.scrollHeight ?? 1000,
    clientHeight: opts.clientHeight ?? 600,
    scrollTop: opts.scrollTop ?? 0,
  };
  Object.defineProperty(node, "scrollHeight", {
    configurable: true,
    get: () => layout.scrollHeight,
  });
  Object.defineProperty(node, "clientHeight", {
    configurable: true,
    get: () => layout.clientHeight,
  });
  Object.defineProperty(node, "scrollTop", {
    configurable: true,
    get: () => layout.scrollTop,
    set: (v: number) => {
      // Real browsers clamp scrollTop into [0, scrollHeight - clientHeight]
      // when scrollTop > scrollHeight - clientHeight. Reproduce that here
      // so the test exercises the same boundary the hook would face in
      // Chromium.
      const max = Math.max(0, layout.scrollHeight - layout.clientHeight);
      layout.scrollTop = Math.max(0, Math.min(v, max));
    },
  });
  return layout;
}

type HookHandle = ReturnType<typeof useConversationScrollState> & {
  conversationScrollRef: { current: HTMLDivElement | null };
};

function Probe({
  onReady
}: {
  onReady: (handle: HookHandle, node: HTMLDivElement) => void;
}): ReactNode {
  const handle = useConversationScrollState({
    activeThreadID: "thread-1",
    activePane: "primary",
    splitConversation: false,
    primaryTurns: makeLongTurns(),
    secondaryTurns: undefined,
    emptyConversation: false,
    previewingLaunch: false,
    showingWorkspaceMode: false,
    initialized: true,
  });
  return createElement(
    "div",
    {
      ref: (node: HTMLDivElement | null) => {
        handle.conversationScrollRef.current = node;
        if (node) onReady(handle, node);
      },
      onScroll: () => handle.handleConversationScroll(),
      "data-testid": "scroll-container",
      "data-user-scrolled-away": handle.userScrolledAway ? "true" : "false",
    },
    createElement("div", { "data-testid": "scroll-content" }),
    createElement("div", {
      [AUTO_FOLLOW_NESTED_SCROLL_ATTR]: "true",
      "data-testid": "nested-scroll",
    }),
  );
}

type MockResizeObserverRecord = {
  callback: ResizeObserverCallback;
  observed: Set<Element>;
  disconnected: boolean;
};

describe("useConversationScrollState — high-frequency stream", () => {
  let container: HTMLDivElement;
  let root: Root | null = null;
  let handle: HookHandle | null = null;
  let node: HTMLDivElement | null = null;
  let layout: StubbedLayout | null = null;
  let realRequestAnimationFrame: typeof window.requestAnimationFrame;
  let realCancelAnimationFrame: typeof window.cancelAnimationFrame;
  let rafCallbacks: Map<number, FrameRequestCallback>;
  let nextRafID = 1;
  let resizeObserverGlobal: typeof globalThis & {
    ResizeObserver?: typeof ResizeObserver;
  };
  let realResizeObserver: typeof ResizeObserver | undefined;
  let resizeObservers: MockResizeObserverRecord[];

  beforeEach(() => {
    container = document.createElement("div");
    document.body.appendChild(container);
    realRequestAnimationFrame = window.requestAnimationFrame;
    realCancelAnimationFrame = window.cancelAnimationFrame;
    rafCallbacks = new Map();
    nextRafID = 1;
    window.requestAnimationFrame = ((callback: FrameRequestCallback) => {
      const handleID = nextRafID;
      nextRafID += 1;
      rafCallbacks.set(handleID, callback);
      return handleID;
    }) as typeof window.requestAnimationFrame;
    window.cancelAnimationFrame = ((handleID: number) => {
      rafCallbacks.delete(handleID);
    }) as typeof window.cancelAnimationFrame;

    resizeObserverGlobal = globalThis as typeof globalThis & {
      ResizeObserver?: typeof ResizeObserver;
    };
    realResizeObserver = resizeObserverGlobal.ResizeObserver;
    resizeObservers = [];
    class MockResizeObserver {
      private readonly record: MockResizeObserverRecord;

      constructor(callback: ResizeObserverCallback) {
        this.record = {
          callback,
          observed: new Set<Element>(),
          disconnected: false,
        };
        resizeObservers.push(this.record);
      }

      observe(target: Element): void {
        this.record.observed.add(target);
      }

      unobserve(target: Element): void {
        this.record.observed.delete(target);
      }

      disconnect(): void {
        this.record.disconnected = true;
        this.record.observed.clear();
      }
    }
    resizeObserverGlobal.ResizeObserver =
      MockResizeObserver as typeof ResizeObserver;
  });

  afterEach(() => {
    act(() => {
      root?.unmount();
    });
    root = null;
    handle = null;
    node = null;
    layout = null;
    window.requestAnimationFrame = realRequestAnimationFrame;
    window.cancelAnimationFrame = realCancelAnimationFrame;
    if (realResizeObserver) {
      resizeObserverGlobal.ResizeObserver = realResizeObserver;
    } else {
      Reflect.deleteProperty(resizeObserverGlobal, "ResizeObserver");
    }
    document.body.removeChild(container);
  });

  function mount(opts: {
    scrollHeight: number;
    clientHeight: number;
  }): void {
    act(() => {
      root = createRoot(container);
      root.render(
        createElement(Probe, {
          onReady: (h, n) => {
            handle = h;
            node = n;
          },
        }),
      );
    });
    if (!node) throw new Error("Probe did not render");
    layout = stubLayout(node, {
      scrollHeight: opts.scrollHeight,
      clientHeight: opts.clientHeight,
      // Land at the bottom of the initial scroll area.
      scrollTop: opts.scrollHeight - opts.clientHeight,
    });
  }

  function flushAnimationFrames(): void {
    const callbacks = Array.from(rafCallbacks.values());
    rafCallbacks.clear();
    const timestamp = performance.now();
    for (const callback of callbacks) {
      callback(timestamp);
    }
  }

  function flushResizeObservers(): void {
    for (const observer of resizeObservers) {
      if (!observer.disconnected) {
        observer.callback([], observer as unknown as ResizeObserver);
      }
    }
  }

  function flushScheduledScroll(): void {
    if (!node) throw new Error("not mounted");
    act(() => {
      flushAnimationFrames();
      node!.dispatchEvent(new Event("scroll", { bubbles: false }));
    });
  }

  function fireUserScroll(): void {
    if (!node) throw new Error("not mounted");
    act(() => {
      node!.dispatchEvent(new WheelEvent("wheel", { bubbles: true, deltaY: -80 }));
      node!.dispatchEvent(new Event("scroll", { bubbles: false }));
    });
  }

  function nestedScrollNode(): HTMLElement {
    const nested = container.querySelector(
      "[data-testid='nested-scroll']",
    ) as HTMLElement | null;
    if (!nested) throw new Error("nested scroll node not rendered");
    return nested;
  }

  it("stays pinned to the bottom across 120 fast stream ticks", () => {
    // Start: 2000px of content in a 600px viewport. We are at the
    // bottom (scrollTop = 1400).
    mount({ scrollHeight: 2000, clientHeight: 600 });
    if (!layout || !handle || !node) throw new Error("not mounted");
    flushScheduledScroll();

    // 120 ticks, each adding 8px of streamed content. This is
    // representative of a fast stream; 120 frames at 60fps is 2 seconds
    // of streaming.
    for (let tick = 0; tick < 120; tick += 1) {
      act(() => {
        // (1) The server pushed a delta; React commits new content
        //     and the layout grows.
        layout!.scrollHeight += 8;
        // (2) The server-event path calls scheduleStreamScroll. This
        //     registers a RAF, which we then run synchronously below
        //     to simulate the next browser frame.
        handle!.scheduleStreamScroll();
      });
      // (3) The browser runs the RAF callback, which sets
      //     scrollTop = scrollHeight. (4) Then it dispatches the
      //     scroll event after that assignment.
      flushScheduledScroll();

      const bottom = layout.scrollHeight - layout.clientHeight;
      // (5) The scroll position must still be at the bottom, and the
      //     "Jump to latest" pill must NOT have appeared.
      expect(layout.scrollTop).toBe(bottom);
      expect(node!.dataset.userScrolledAway ?? "false").toBe("false");
    }
  });

  it("keeps following when streamed content grows after the stream frame", () => {
    mount({ scrollHeight: 2000, clientHeight: 600 });
    if (!layout || !node) throw new Error("not mounted");
    flushScheduledScroll();

    act(() => {
      // Markdown block promotion, syntax highlighting, or media layout can
      // change the scrollHeight after the original stream frame has already
      // run. The conversation content observer is the durable signal that
      // the bottom needs to be re-anchored.
      layout!.scrollHeight += 120;
      flushResizeObservers();
    });
    flushScheduledScroll();

    expect(layout.scrollTop).toBe(layout.scrollHeight - layout.clientHeight);
    expect(node.dataset.userScrolledAway ?? "false").toBe("false");
  });

  it("disengages auto-follow the moment the user scrolls up — no 16px dead zone", () => {
    // Regression gate for the scroll-up "resistance" bug: any wheel-up
    // from the bottom must take auto-follow off immediately, including
    // deltas well inside the old 16px threshold band. Re-arming only
    // on a downward / settled scroll is what makes the scroll feel
    // responsive instead of springing back the moment something
    // triggers `scheduleStreamScroll`.
    mount({ scrollHeight: 2000, clientHeight: 600 });
    if (!layout || !handle || !node) throw new Error("not mounted");
    flushScheduledScroll();

    // Park at the bottom and dispatch a scroll event so lastRef is
    // primed to the max position.
    act(() => {
      handle!.scheduleStreamScroll();
    });
    flushScheduledScroll();
    expect(node!.dataset.userScrolledAway ?? "false").toBe("false");

    // Wheel up 8px — well inside the old 16px band. The pill must
    // show and auto-follow must drop.
    act(() => {
      layout!.scrollTop = layout!.scrollHeight - layout!.clientHeight - 8;
    });
    fireUserScroll();
    expect(node!.dataset.userScrolledAway ?? "false").toBe("true");

    // Wheel up another 9px (17px total). State stays away-from-latest.
    act(() => {
      layout!.scrollTop = layout!.scrollHeight - layout!.clientHeight - 17;
    });
    fireUserScroll();
    expect(node!.dataset.userScrolledAway ?? "false").toBe("true");
  });

  it("programmatic scroll-to-bottom keeps auto-follow engaged", () => {
    // Companion case to the dead-zone regression: a stream tick (or
    // fold re-anchor) that lands scrollTop at the max must keep
    // auto-follow on. The previous threshold-edge test conflated the
    // two cases; this one isolates the programmatic-scroll path.
    mount({ scrollHeight: 2000, clientHeight: 600 });
    if (!layout || !handle || !node) throw new Error("not mounted");
    flushScheduledScroll();

    // Prime the scroll-event handler at the bottom.
    act(() => {
      handle!.scheduleStreamScroll();
    });
    flushScheduledScroll();
    expect(node!.dataset.userScrolledAway ?? "false").toBe("false");

    // 60 fast stream ticks, each one bumping scrollHeight and
    // re-anchoring scrollTop = scrollHeight. None of them should
    // ever flip userScrolledAway to true.
    for (let tick = 0; tick < 60; tick += 1) {
      act(() => {
        layout!.scrollHeight += 8;
        handle!.scheduleStreamScroll();
      });
      flushScheduledScroll();
      const bottom = layout.scrollHeight - layout.clientHeight;
      expect(layout.scrollTop).toBe(bottom);
      expect(node!.dataset.userScrolledAway ?? "false").toBe("false");
    }
  });

  it("does not let content resize re-enable follow after the user scrolls away", () => {
    mount({ scrollHeight: 2000, clientHeight: 600 });
    if (!layout || !node) throw new Error("not mounted");
    flushScheduledScroll();

    act(() => {
      layout!.scrollTop = layout!.scrollHeight - layout!.clientHeight - 80;
    });
    fireUserScroll();
    expect(node.dataset.userScrolledAway ?? "false").toBe("true");

    act(() => {
      layout!.scrollHeight += 200;
      flushResizeObservers();
    });
    flushScheduledScroll();

    expect(layout.scrollTop).toBe(2000 - 600 - 80);
    expect(node.dataset.userScrolledAway ?? "false").toBe("true");
  });

  it("does not treat nested reasoning scroll as leaving the conversation bottom", () => {
    mount({ scrollHeight: 2000, clientHeight: 600 });
    if (!layout || !node) throw new Error("not mounted");
    flushScheduledScroll();
    expect(layout.scrollTop).toBe(1400);

    act(() => {
      nestedScrollNode().dispatchEvent(
        new WheelEvent("wheel", { bubbles: true, deltaY: -80 }),
      );
      // Simulate a worst-case scroll-chain/clamp where the outer
      // conversation receives a scroll event after the nested scroller
      // hits its top. The nested wheel must not count as conversation
      // scroll-away intent, so the outer container re-anchors.
      layout!.scrollTop = 1180;
      node!.dispatchEvent(new Event("scroll", { bubbles: false }));
    });

    expect(layout.scrollTop).toBe(layout.scrollHeight - layout.clientHeight);
    expect(node.dataset.userScrolledAway ?? "false").toBe("false");
  });

  it("keeps following when layout shrink clamps the viewport to the new bottom", () => {
    mount({ scrollHeight: 2200, clientHeight: 600 });
    if (!layout || !handle || !node) throw new Error("not mounted");
    flushScheduledScroll();
    expect(layout.scrollTop).toBe(1600);

    act(() => {
      // A completed process fold can shrink above the viewport. Chromium
      // clamps scrollTop from the old max to the new max and emits a scroll
      // event even though the user did not scroll up.
      layout!.scrollHeight = 1700;
      layout!.scrollTop = 1100;
      node!.dispatchEvent(new Event("scroll", { bubbles: false }));
    });
    expect(node.dataset.userScrolledAway ?? "false").toBe("false");

    act(() => {
      layout!.scrollHeight += 120;
      handle!.scheduleStreamScroll();
    });
    flushScheduledScroll();

    expect(layout.scrollTop).toBe(layout.scrollHeight - layout.clientHeight);
    expect(node.dataset.userScrolledAway ?? "false").toBe("false");
  });
});
