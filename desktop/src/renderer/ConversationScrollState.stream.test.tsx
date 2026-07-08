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
import { WINDOW_RESIZING_CLASS } from "./WindowResizeState";

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

function makeLongTurnsSnapshot(version: number): Turn[] {
  return [
    {
      id: "turn-1",
      items: [],
      items_view: "full",
      status: version % 2 === 0 ? "in_progress" : "completed",
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
  turns,
  showConversation = true,
  onReady
}: {
  turns?: Turn[];
  showConversation?: boolean;
  onReady: (handle: HookHandle, node: HTMLDivElement) => void;
}): ReactNode {
  const handle = useConversationScrollState({
    activeThreadID: "thread-1",
    activePane: "primary",
    splitConversation: false,
    primaryTurns: turns ?? makeLongTurns(),
    secondaryTurns: undefined,
    emptyConversation: false,
    previewingLaunch: false,
    showingWorkspaceMode: false,
    initialized: true,
  });
  if (!showConversation) {
    return createElement("section", {
      "data-testid": "settings-placeholder",
      "data-user-scrolled-away": handle.userScrolledAway ? "true" : "false",
    });
  }
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
    createElement("div", {
      ref: (node: HTMLDivElement | null) => {
        if (node) handle.scrollContentRef.current = node;
      },
      "data-testid": "scroll-content",
    }),
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
    turns?: Turn[];
  }): void {
    act(() => {
      root = createRoot(container);
      root.render(
        createElement(Probe, {
          turns: opts.turns,
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

  function rerenderTurns(turns: Turn[]): void {
    if (!root) throw new Error("not mounted");
    act(() => {
      root!.render(
        createElement(Probe, {
          turns,
          onReady: (h, n) => {
            handle = h;
            node = n;
          },
        }),
      );
    });
  }

  function rerenderProbe(opts: {
    turns?: Turn[];
    showConversation?: boolean;
  }): void {
    if (!root) throw new Error("not mounted");
    act(() => {
      root!.render(
        createElement(Probe, {
          turns: opts.turns,
          showConversation: opts.showConversation,
          onReady: (h, n) => {
            handle = h;
            node = n;
          },
        }),
      );
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
        const entries = Array.from(observer.observed).map((target) => {
          const height =
            target === node && layout ? layout.clientHeight : 0;
          return {
            target,
            contentRect: {
              x: 0,
              y: 0,
              top: 0,
              left: 0,
              right: 0,
              bottom: height,
              width: 0,
              height,
              toJSON: () => ({}),
            },
            borderBoxSize: [{ blockSize: height, inlineSize: 0 }],
            contentBoxSize: [{ blockSize: height, inlineSize: 0 }],
            devicePixelContentBoxSize: [{ blockSize: height, inlineSize: 0 }],
          } as unknown as ResizeObserverEntry;
        });
        observer.callback(entries, observer as unknown as ResizeObserver);
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

  it("re-anchors immediately when streamed content grows after the stream frame", () => {
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

    expect(layout.scrollTop).toBe(layout.scrollHeight - layout.clientHeight);
    expect(node.dataset.userScrolledAway ?? "false").toBe("false");
  });

  it("does not accept a stale programmatic bottom after content grows before the scroll event", () => {
    mount({ scrollHeight: 2000, clientHeight: 600 });
    if (!layout || !handle || !node) throw new Error("not mounted");
    flushScheduledScroll();

    act(() => {
      handle!.scheduleStreamScroll();
      flushAnimationFrames();
      // Chromium can deliver the scroll event from the programmatic bottom
      // assignment after React has already committed more streamed content.
      // The old bottom is no longer the latest view and must not be accepted
      // as "handled".
      layout!.scrollHeight += 96;
      node!.dispatchEvent(new Event("scroll", { bubbles: false }));
    });

    expect(layout.scrollTop).toBe(layout.scrollHeight - layout.clientHeight);
    expect(node.dataset.userScrolledAway ?? "false").toBe("false");
  });

  it("keeps one settle frame for content that grows after the first stream scroll", () => {
    mount({ scrollHeight: 2000, clientHeight: 600 });
    if (!layout || !handle || !node) throw new Error("not mounted");
    flushScheduledScroll();

    act(() => {
      handle!.scheduleStreamScroll();
      flushAnimationFrames();
      // No scroll event or ResizeObserver callback has fired yet, but the
      // text commit lands just after the first scheduled bottom scroll.
      layout!.scrollHeight += 96;
      flushAnimationFrames();
    });

    expect(layout.scrollTop).toBe(layout.scrollHeight - layout.clientHeight);
    expect(node.dataset.userScrolledAway ?? "false").toBe("false");
  });

  it("re-anchors during the turn snapshot commit before the next animation frame", () => {
    mount({
      scrollHeight: 2000,
      clientHeight: 600,
      turns: makeLongTurnsSnapshot(0),
    });
    if (!layout || !node) throw new Error("not mounted");
    flushScheduledScroll();

    // New non-token content, such as a gray process row after commentary,
    // grows the layout as part of the React commit. Waiting until the next
    // RAF lets the browser paint one frame at the old scrollTop.
    layout.scrollHeight += 96;
    rerenderTurns(makeLongTurnsSnapshot(1));

    expect(layout.scrollTop).toBe(layout.scrollHeight - layout.clientHeight);
    expect(node.dataset.userScrolledAway ?? "false").toBe("false");
  });

  it("does not re-anchor a turn snapshot commit after the user scrolls away", () => {
    mount({
      scrollHeight: 2000,
      clientHeight: 600,
      turns: makeLongTurnsSnapshot(0),
    });
    if (!layout || !node) throw new Error("not mounted");
    flushScheduledScroll();

    act(() => {
      layout!.scrollTop = layout!.scrollHeight - layout!.clientHeight - 120;
    });
    fireUserScroll();
    expect(node.dataset.userScrolledAway ?? "false").toBe("true");

    layout.scrollHeight += 96;
    rerenderTurns(makeLongTurnsSnapshot(1));

    expect(layout.scrollTop).toBe(2000 - 600 - 120);
    expect(node.dataset.userScrolledAway ?? "false").toBe("true");
  });

  it("disengages auto-follow when an upward scroll event leaves the bottom without preflight intent", () => {
    mount({ scrollHeight: 2000, clientHeight: 600 });
    if (!layout || !handle || !node) throw new Error("not mounted");
    flushScheduledScroll();
    expect(layout.scrollTop).toBe(1400);

    act(() => {
      // Native scrollbar drags and some platform scroll paths can deliver
      // only the scroll event. Once scrollTop has moved upward away from
      // the latest content, auto-follow must stop before final-answer settle
      // frames or fold-collapse re-anchors can pull the user back down.
      layout!.scrollTop = layout!.scrollHeight - layout!.clientHeight - 120;
      node!.dispatchEvent(new Event("scroll", { bubbles: false }));
    });

    expect(layout.scrollTop).toBe(2000 - 600 - 120);
    expect(node.dataset.userScrolledAway ?? "false").toBe("true");

    act(() => {
      handle!.scheduleStreamScroll();
    });
    flushScheduledScroll();

    expect(layout.scrollTop).toBe(2000 - 600 - 120);
    expect(node.dataset.userScrolledAway ?? "false").toBe("true");
  });

  it("disengages auto-follow on the first scroll after the conversation remounts with taller content", () => {
    mount({
      scrollHeight: 2000,
      clientHeight: 600,
      turns: makeLongTurnsSnapshot(0),
    });
    if (!layout || !node || !handle) throw new Error("not mounted");
    flushScheduledScroll();
    expect(layout.scrollTop).toBe(1400);

    rerenderProbe({
      turns: makeLongTurnsSnapshot(1),
      showConversation: false,
    });
    expect(
      container.querySelector("[data-testid='settings-placeholder']"),
    ).not.toBeNull();

    rerenderProbe({
      turns: makeLongTurnsSnapshot(1),
      showConversation: true,
    });
    node = container.querySelector(
      "[data-testid='scroll-container']",
    ) as HTMLDivElement | null;
    if (!node) throw new Error("conversation did not remount");
    layout = stubLayout(node, {
      scrollHeight: 2400,
      clientHeight: 600,
      scrollTop: 2400 - 600,
    });

    act(() => {
      node!.dispatchEvent(
        new WheelEvent("wheel", { bubbles: true, deltaY: -80 }),
      );
      layout!.scrollTop = 1700;
      node!.dispatchEvent(new Event("scroll", { bubbles: false }));
    });

    expect(layout.scrollTop).toBe(1700);
    expect(node.dataset.userScrolledAway ?? "false").toBe("true");

    act(() => {
      handle!.scheduleStreamScroll();
    });
    flushScheduledScroll();

    expect(layout.scrollTop).toBe(1700);
    expect(node.dataset.userScrolledAway ?? "false").toBe("true");
  });

  it("disengages scroll-only upward movement after remounting with a stale bottom baseline", () => {
    mount({
      scrollHeight: 2000,
      clientHeight: 600,
      turns: makeLongTurnsSnapshot(0),
    });
    if (!layout || !node || !handle) throw new Error("not mounted");
    flushScheduledScroll();
    expect(layout.scrollTop).toBe(1400);

    rerenderProbe({
      turns: makeLongTurnsSnapshot(1),
      showConversation: false,
    });
    expect(
      container.querySelector("[data-testid='settings-placeholder']"),
    ).not.toBeNull();

    rerenderProbe({
      turns: makeLongTurnsSnapshot(1),
      showConversation: true,
    });
    node = container.querySelector(
      "[data-testid='scroll-container']",
    ) as HTMLDivElement | null;
    if (!node) throw new Error("conversation did not remount");
    layout = stubLayout(node, {
      scrollHeight: 2400,
      clientHeight: 600,
      scrollTop: 2400 - 600,
    });

    act(() => {
      // The old baseline is below the remounted bottom. A bare scroll event
      // can still be a real upward user move even when current scrollTop is
      // numerically larger than the stale last scrollTop.
      layout!.scrollTop = 1700;
      node!.dispatchEvent(new Event("scroll", { bubbles: false }));
    });

    expect(layout.scrollTop).toBe(1700);
    expect(node.dataset.userScrolledAway ?? "false").toBe("true");

    act(() => {
      handle!.scheduleStreamScroll();
    });
    flushScheduledScroll();

    expect(layout.scrollTop).toBe(1700);
    expect(node.dataset.userScrolledAway ?? "false").toBe("true");
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

  it("keeps auto-follow disabled during smooth jump startup near the bottom", () => {
    mount({ scrollHeight: 2000, clientHeight: 600 });
    if (!layout || !handle || !node) throw new Error("not mounted");
    flushScheduledScroll();

    act(() => {
      handle!.disableConversationAutoFollow();
      // Closing the popover or layout settling can produce a scroll event
      // before smooth scrolling has moved the viewport at all. That event is
      // still at the latest view, but it must not re-arm follow mode.
      node!.dispatchEvent(new Event("scroll", { bubbles: false }));

      // A query-history jump uses smooth scrolling. Chromium can emit an
      // early upward scroll event while the viewport is still inside the
      // bottom threshold band. That must not re-arm follow mode.
      layout!.scrollTop = layout!.scrollHeight - layout!.clientHeight - 8;
      node!.dispatchEvent(new Event("scroll", { bubbles: false }));
    });
    expect(node.dataset.userScrolledAway ?? "false").toBe("true");

    act(() => {
      handle!.scheduleStreamScroll();
    });
    flushScheduledScroll();

    expect(layout.scrollTop).toBe(2000 - 600 - 8);
    expect(node.dataset.userScrolledAway ?? "false").toBe("true");
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
    if (!layout || !node || !handle) throw new Error("not mounted");
    flushScheduledScroll();
    expect(layout.scrollTop).toBe(1400);

    act(() => {
      nestedScrollNode().dispatchEvent(
        new WheelEvent("wheel", { bubbles: true, deltaY: -80 }),
      );
    });
    expect(layout.scrollTop).toBe(1400);
    expect(node.dataset.userScrolledAway ?? "false").toBe("false");

    act(() => {
      layout!.scrollHeight += 80;
      handle!.scheduleStreamScroll();
    });
    flushScheduledScroll();

    expect(layout.scrollTop).toBe(layout.scrollHeight - layout.clientHeight);
    expect(node.dataset.userScrolledAway ?? "false").toBe("false");
  });

  it("disengages auto-follow when a nested scroll chains upward into the conversation", () => {
    mount({ scrollHeight: 2000, clientHeight: 600 });
    if (!layout || !node || !handle) throw new Error("not mounted");
    flushScheduledScroll();
    expect(layout.scrollTop).toBe(1400);

    act(() => {
      nestedScrollNode().dispatchEvent(
        new WheelEvent("wheel", { bubbles: true, deltaY: -80 }),
      );
      layout!.scrollTop = 1180;
      node!.dispatchEvent(new Event("scroll", { bubbles: false }));
    });

    expect(layout.scrollTop).toBe(1180);
    expect(node.dataset.userScrolledAway ?? "false").toBe("true");

    act(() => {
      handle!.scheduleStreamScroll();
    });
    flushScheduledScroll();

    expect(layout.scrollTop).toBe(1180);
    expect(node.dataset.userScrolledAway ?? "false").toBe("true");
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

  it("does not re-enable follow when layout shrink clamps an already-away viewport to the new bottom", () => {
    mount({ scrollHeight: 2200, clientHeight: 600 });
    if (!layout || !handle || !node) throw new Error("not mounted");
    flushScheduledScroll();
    expect(layout.scrollTop).toBe(1600);

    act(() => {
      layout!.scrollTop = 1200;
    });
    fireUserScroll();
    expect(node.dataset.userScrolledAway ?? "false").toBe("true");

    act(() => {
      // The process fold can collapse after final text settles. If the
      // user was already reading above the bottom, Chromium may still clamp
      // scrollTop to the new max and emit a scroll event. That layout clamp
      // must not mean the user asked to resume following latest content.
      layout!.scrollHeight = 1700;
      layout!.scrollTop = 1100;
      node!.dispatchEvent(new Event("scroll", { bubbles: false }));
    });

    expect(layout.scrollTop).toBe(1100);
    expect(node.dataset.userScrolledAway ?? "false").toBe("true");

    act(() => {
      layout!.scrollHeight += 120;
      handle!.scheduleStreamScroll();
    });
    flushScheduledScroll();

    expect(layout.scrollTop).toBe(1100);
    expect(node.dataset.userScrolledAway ?? "false").toBe("true");
  });

  it("does not transform the conversation during live window resize, then commits the real scrollTop when the resize settles", () => {
    // User parked at the bottom of a tall conversation. During live
    // window resize we keep the message container in normal flow and
    // avoid scrollTop writes; once the drag settles we commit the real
    // scroll position in one step.
    mount({ scrollHeight: 2200, clientHeight: 600 });
    if (!layout || !handle || !node) throw new Error("not mounted");
    flushScheduledScroll();
    // Fire the ResizeObserver once on mount (class OFF) so the hook
    // captures the pre-resize baseline (600px). If we skip this, the
    // first observer fire happens after the resize has already started
    // and the "baseline" would be the post-shrink height — making the
    // shift math always land at zero.
    flushResizeObservers();
    expect(layout.scrollTop).toBe(1600);

    const contentNode = container.querySelector(
      "[data-testid='scroll-content']",
    ) as HTMLDivElement | null;
    if (!contentNode) throw new Error("scroll-content not rendered");
    expect(contentNode.style.transform).toBe("");

    // Simulate a live window drag: the main process / window emits the
    // resizing marker, the viewport shrinks, and the ResizeObserver
    // fires while we are still inside the drag.
    act(() => {
      document.documentElement.classList.add(WINDOW_RESIZING_CLASS);
      layout!.clientHeight = 400;
      flushResizeObservers();
    });

    // The real scrollTop is untouched during the drag and the message
    // container stays in normal flow, so text does not move through a
    // temporary transform while the user is resizing the window.
    expect(contentNode.style.transform).toBe("");
    expect(layout.scrollTop).toBe(1600);
    // The user is still considered "following" — the pill stays hidden
    // and the next stream tick would happily stick to the new bottom.
    expect(node.dataset.userScrolledAway ?? "false").toBe("false");

    // The drag ends: the resizing marker drops, the ResizeObserver
    // fires one more time. We commit the real scrollTop in a single
    // paint and clear the transform.
    act(() => {
      document.documentElement.classList.remove(WINDOW_RESIZING_CLASS);
      flushResizeObservers();
    });

    expect(contentNode.style.transform).toBe("");
    expect(layout.scrollTop).toBe(2200 - 400);
  });

  it("does not transform a short conversation if the turn snapshot updates during window resize", () => {
    mount({ scrollHeight: 500, clientHeight: 600 });
    if (!layout || !node) throw new Error("not mounted");
    layout.scrollTop = 0;
    flushScheduledScroll();
    flushResizeObservers();
    expect(layout.scrollTop).toBe(0);

    const contentNode = container.querySelector(
      "[data-testid='scroll-content']",
    ) as HTMLDivElement | null;
    if (!contentNode) throw new Error("scroll-content not rendered");

    act(() => {
      document.documentElement.classList.add(WINDOW_RESIZING_CLASS);
      rerenderTurns(makeLongTurnsSnapshot(2));
      layout!.clientHeight = 620;
      flushResizeObservers();
      layout!.clientHeight = 700;
      flushResizeObservers();
    });

    expect(contentNode.style.transform).toBe("");
    expect(layout.scrollTop).toBe(0);

    act(() => {
      document.documentElement.classList.remove(WINDOW_RESIZING_CLASS);
      flushResizeObservers();
    });
  });

  it("does not read scroll metrics when a layout scroll event fires during live window resize", () => {
    mount({ scrollHeight: 2000, clientHeight: 600 });
    if (!layout || !node) throw new Error("not mounted");
    flushScheduledScroll();

    let liveResizeMetricReads = 0;
    Object.defineProperty(node, "scrollHeight", {
      configurable: true,
      get: () => {
        if (document.documentElement.classList.contains(WINDOW_RESIZING_CLASS)) {
          liveResizeMetricReads += 1;
        }
        return layout!.scrollHeight;
      },
    });
    Object.defineProperty(node, "clientHeight", {
      configurable: true,
      get: () => {
        if (document.documentElement.classList.contains(WINDOW_RESIZING_CLASS)) {
          liveResizeMetricReads += 1;
        }
        return layout!.clientHeight;
      },
    });

    act(() => {
      document.documentElement.classList.add(WINDOW_RESIZING_CLASS);
      node!.dispatchEvent(new Event("scroll", { bubbles: false }));
    });

    expect(liveResizeMetricReads).toBe(0);

    act(() => {
      document.documentElement.classList.remove(WINDOW_RESIZING_CLASS);
    });
  });

  it("keeps the conversation in normal flow as the user keeps dragging past the initial viewport size", () => {
    // A second live-resize fire after a partial settle should still avoid
    // transient transforms. The real scroll position is committed after
    // the drag settles, preserving the no-scrollTop-writes-during-resize
    // performance invariant without moving text through a composited layer.
    mount({ scrollHeight: 2200, clientHeight: 600 });
    if (!layout || !handle || !node) throw new Error("not mounted");
    flushScheduledScroll();
    // Establish the pre-resize baseline (600px) before the drag starts.
    flushResizeObservers();
    expect(layout.scrollTop).toBe(1600);

    const contentNode = container.querySelector(
      "[data-testid='scroll-content']",
    ) as HTMLDivElement | null;
    if (!contentNode) throw new Error("scroll-content not rendered");

    act(() => {
      document.documentElement.classList.add(WINDOW_RESIZING_CLASS);
      layout!.clientHeight = 500;
      flushResizeObservers();
    });
    expect(contentNode.style.transform).toBe("");

    act(() => {
      layout!.clientHeight = 350;
      flushResizeObservers();
    });
    expect(contentNode.style.transform).toBe("");

    act(() => {
      document.documentElement.classList.remove(WINDOW_RESIZING_CLASS);
      flushResizeObservers();
    });
    expect(contentNode.style.transform).toBe("");
    expect(layout.scrollTop).toBe(2200 - 350);
  });
});
