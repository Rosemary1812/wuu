/**
 * Tests for `useConversationScrollState.userScrolledAway` — the boolean
 * that drives the "Jump to latest" pill at the bottom of the conversation
 * pane. The pill must stay hidden when:
 *
 *   1. The viewport is not tall enough to scroll (scrollHeight <= clientHeight)
 *      — there is nothing below the fold to jump to.
 *   2. The user is already parked inside the bottom band
 *      (distanceFromBottom <= CONVERSATION_AUTO_SCROLL_THRESHOLD_PX) — they
 *      are at the latest message.
 *
 * Earlier versions inferred "scrolled away" purely from scroll direction
 * (`scrolledUp = scrollTop < lastScrollTop`), which left stale
 * `userScrolledAway = true` lingering on a thread switch into a short
 * conversation whose `useLayoutEffect` could not trigger a scroll event.
 *
 * The test installs layout stubs on the element the ref callback actually
 * receives, which is the React-managed DOM node created by `createRoot`.
 * React 19's createRoot drops any pre-existing container children on the
 * first render, so we cannot pre-append a node and have the hook pick it
 * up — we have to install the stubs after mount.
 */
import { act, createElement, type ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { useConversationScrollState } from "./ConversationScrollState";
import type { Turn } from "../shared/protocol";

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

const SMOOTH_PROBE_TURNS = makeLongTurns();

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
      const max = Math.max(0, layout.scrollHeight - layout.clientHeight);
      layout.scrollTop = Math.max(0, Math.min(v, max));
    },
  });
  return layout;
}

function Probe({ activeThreadID }: { activeThreadID?: string }): ReactNode {
  const h = useConversationScrollState({
    activeThreadID,
    activePane: "primary",
    splitConversation: false,
    primaryTurns: makeLongTurns(),
    secondaryTurns: undefined,
    emptyConversation: false,
    previewingLaunch: false,
    initialized: true,
  });
  return createElement("div", {
    ref: (node: HTMLDivElement | null) => {
      h.conversationScrollRef.current = node;
    },
    onScroll: () => h.handleConversationScroll(),
    "data-testid": "scroll-container",
    "data-user-scrolled-away": h.userScrolledAway ? "true" : "false",
  }, createElement("div", {
    ref: (node: HTMLDivElement | null) => {
      h.scrollContentRef.current = node;
    },
    "data-testid": "scroll-content",
  }));
}

describe("useConversationScrollState — userScrolledAway", () => {
  let container: HTMLDivElement;
  let root: Root | null = null;
  let scrollNode: HTMLDivElement | null = null;
  let layout: StubbedLayout | null = null;

  beforeEach(() => {
    container = document.createElement("div");
    document.body.appendChild(container);
  });

  afterEach(() => {
    act(() => {
      root?.unmount();
    });
    root = null;
    scrollNode = null;
    layout = null;
    document.body.removeChild(container);
  });

  function mount(opts: {
    scrollHeight: number;
    clientHeight: number;
    initialScrollTop: number;
    activeThreadID?: string;
  }): HTMLDivElement {
    act(() => {
      root = createRoot(container);
      root.render(
        createElement(Probe, {
          activeThreadID: opts.activeThreadID ?? "thread-1",
        }),
      );
    });

    const node = container.querySelector(
      "[data-testid='scroll-container']",
    ) as HTMLDivElement | null;
    if (!node) throw new Error("Probe did not render");
    scrollNode = node;
    // Install layout stubs after React has finished its mount-time
    // useLayoutEffect, which has already attempted to scroll the
    // unstubbed element. Subsequent scroll events (and our manual
    // dispatchEvent) will read the stubbed values.
    layout = stubLayout(node, {
      scrollHeight: opts.scrollHeight,
      clientHeight: opts.clientHeight,
      scrollTop: opts.initialScrollTop,
    });
    return node;
  }

  function switchThread(activeThreadID?: string): void {
    if (!root) throw new Error("not mounted");
    act(() => {
      root!.render(createElement(Probe, { activeThreadID }));
    });
  }

  function fireScroll(): void {
    if (!scrollNode) throw new Error("not mounted");
    act(() => {
      scrollNode!.dispatchEvent(new Event("scroll", { bubbles: false }));
    });
  }

  function fireUserScroll(): void {
    if (!scrollNode) throw new Error("not mounted");
    act(() => {
      scrollNode!.dispatchEvent(new WheelEvent("wheel", { bubbles: true, deltaY: -80 }));
      scrollNode!.dispatchEvent(new Event("scroll", { bubbles: false }));
    });
  }

  function setScrollTop(top: number): void {
    if (!layout) throw new Error("not mounted");
    layout.scrollTop = top;
  }

  it("hides the pill when the conversation is shorter than the viewport", () => {
    mount({ scrollHeight: 200, clientHeight: 600, initialScrollTop: 0 });
    fireScroll();
    expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("false");
  });

  it("shows the pill when the user scrolls up out of the bottom band", () => {
    // Park the user at the bottom of the conversation first so the
    // mount-time useLayoutEffect has a stable starting position.
    mount({ scrollHeight: 2000, clientHeight: 600, initialScrollTop: 2000 - 600 });
    // Prime lastRef.
    fireScroll();
    // User wheels up: scrollTop drops 1400 → 500. distanceFromBottom
    // is now 900, well outside the 16px bottom band, so the pill
    // must show.
    setScrollTop(500);
    fireUserScroll();
    expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("true");
  });

  it("hides the pill when the user scrolls back into the bottom band", () => {
    mount({ scrollHeight: 2000, clientHeight: 600, initialScrollTop: 2000 - 600 });
    fireScroll();

    setScrollTop(500);
    fireUserScroll();
    expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("true");

    // Park the user at the very bottom of the conversation.
    setScrollTop(2000 - 600); // distanceFromBottom = 0
    fireUserScroll();
    expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("false");
  });

  it("hides the pill when the user is parked inside the bottom band", () => {
    mount({ scrollHeight: 2000, clientHeight: 600, initialScrollTop: 2000 - 600 });
    fireScroll();
    setScrollTop(500);
    fireUserScroll();
    expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("true");
    // A layout/programmatic scroll leaves the viewport 8px from the
    // bottom — well within the 16px band. This is still the latest
    // view, so the pill should hide.
    setScrollTop(2000 - 600 - 8);
    fireScroll();
    expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("false");
  });

  it("disengages auto-follow on any user scroll-up — no dead zone at the bottom", () => {
    // Regression gate for the universal scroll-up "resistance" bug:
    // even a single wheel-up that lands inside the old 16px bottom
    // band must immediately take auto-follow off, so the next
    // scheduleStreamScroll (stream tick, fold re-anchor, etc.) cannot
    // yank the user back to the bottom mid-gesture.
    mount({ scrollHeight: 2000, clientHeight: 600, initialScrollTop: 2000 - 600 });
    // Prime lastRef at the max position.
    fireScroll();
    expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("false");

    // Wheel up 8px — well inside the old 16px band. Pill must show.
    setScrollTop(2000 - 600 - 8);
    fireUserScroll();
    expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("true");

    // Scroll back to the bottom: scrolledUp is false and we're parked
    // at the latest view, so auto-follow re-engages and the pill hides.
    setScrollTop(2000 - 600);
    fireUserScroll();
    expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("false");
  });

  it("does not let a stale direction cue keep the pill visible on a non-scrollable viewport", () => {
    // Reproduce the original bug: a short thread whose useLayoutEffect
    // cannot fire a scroll event because scrollTop cannot change. The
    // direction-driven implementation would leave userScrolledAway
    // stuck at true. The position-driven implementation must read the
    // actual layout and stay false.
    mount({ scrollHeight: 200, clientHeight: 600, initialScrollTop: 0 });
    fireScroll();
    expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("false");
  });

  it("does not treat session-switch clamping as a user scroll-up", () => {
    mount({
      activeThreadID: "thread-long",
      scrollHeight: 2400,
      clientHeight: 600,
      initialScrollTop: 2400 - 600,
    });
    fireScroll();
    expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("false");

    if (!layout) throw new Error("not mounted");
    layout.scrollHeight = 900;
    switchThread("thread-short");
    expect(layout.scrollTop).toBe(300);

    // Chromium fires a scroll event after the programmatic bottom snap.
    // This is a downward clamp from the old thread's 1800px position to
    // the short thread's 300px max, not user intent, so auto-follow stays on.
    fireScroll();
    expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("false");
  });

  it("restores a thread's saved away-from-bottom position when switching back", () => {
    mount({
      activeThreadID: "thread-a",
      scrollHeight: 2400,
      clientHeight: 600,
      initialScrollTop: 2400 - 600,
    });
    fireScroll();

    setScrollTop(520);
    fireUserScroll();
    expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("true");

    if (!layout) throw new Error("not mounted");
    layout.scrollHeight = 1200;
    switchThread("thread-b");
    expect(layout.scrollTop).toBe(600);
    fireScroll();
    expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("false");

    layout.scrollHeight = 2400;
    switchThread("thread-a");
    expect(layout.scrollTop).toBe(520);
    fireScroll();
    expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("true");
  });

  it("keeps a thread's scroll snapshot while a non-conversation tab is active", () => {
    mount({
      activeThreadID: "thread-a",
      scrollHeight: 2200,
      clientHeight: 600,
      initialScrollTop: 2200 - 600,
    });
    fireScroll();

    setScrollTop(480);
    fireUserScroll();
    expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("true");

    switchThread(undefined);
    if (!layout) throw new Error("not mounted");
    layout.scrollTop = 0;
    fireScroll();
    expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("false");

    switchThread("thread-a");
    expect(layout.scrollTop).toBe(480);
    fireScroll();
    expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("true");
  });
});

type MockResizeObserverRecord = {
  callback: ResizeObserverCallback;
  observed: Set<Element>;
};

describe("useConversationScrollState — dock composer height", () => {
  let container: HTMLDivElement;
  let root: Root | null = null;
  let resizeObserverGlobal: typeof globalThis & {
    ResizeObserver?: typeof ResizeObserver;
  };
  let realResizeObserver: typeof ResizeObserver | undefined;
  let resizeObservers: MockResizeObserverRecord[];

  beforeEach(() => {
    container = document.createElement("div");
    document.body.appendChild(container);
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
    if (realResizeObserver) {
      resizeObserverGlobal.ResizeObserver = realResizeObserver;
    } else {
      Reflect.deleteProperty(resizeObserverGlobal, "ResizeObserver");
    }
    document.body.removeChild(container);
  });

  function DockComposerProbe(): ReactNode {
    const h = useConversationScrollState({
      activeThreadID: "thread-1",
      activePane: "primary",
      splitConversation: false,
      primaryTurns: makeLongTurns(),
      secondaryTurns: undefined,
      emptyConversation: false,
      previewingLaunch: false,
      initialized: true,
    });
    return createElement(
      "section",
      {
        ref: (node: HTMLElement | null) => {
          h.conversationPaneRef.current = node;
        },
        "data-testid": "conversation-pane",
      },
      createElement("div", {
        ref: (node: HTMLDivElement | null) => {
          h.conversationScrollRef.current = node;
        },
        "data-testid": "scroll-container",
      }),
      createElement(
        "footer",
        {
          ref: h.dockComposerRef,
          "data-testid": "dock-composer",
        },
        createElement("div", {
          className: "composer-frame",
          "data-testid": "composer-frame",
        }),
      ),
    );
  }

  function mountDockComposerProbe(): {
    pane: HTMLElement;
    dockComposer: HTMLElement;
    frame: HTMLElement;
  } {
    act(() => {
      root = createRoot(container);
      root.render(createElement(DockComposerProbe));
    });

    const pane = container.querySelector<HTMLElement>(
      "[data-testid='conversation-pane']",
    );
    const dockComposer = container.querySelector<HTMLElement>(
      "[data-testid='dock-composer']",
    );
    const frame = container.querySelector<HTMLElement>(
      "[data-testid='composer-frame']",
    );
    if (!pane || !dockComposer || !frame) {
      throw new Error("DockComposerProbe did not render");
    }
    return { pane, dockComposer, frame };
  }

  function stubRectHeight(node: HTMLElement, height: number): void {
    node.getBoundingClientRect = () =>
      ({
        x: 0,
        y: 0,
        top: 0,
        right: 0,
        bottom: height,
        left: 0,
        width: 800,
        height,
        toJSON: () => ({}),
      }) as DOMRect;
  }

  function flushResizeObserversFor(target: Element): void {
    act(() => {
      for (const observer of resizeObservers) {
        if (observer.observed.has(target)) {
          observer.callback([], observer as unknown as ResizeObserver);
        }
      }
    });
  }

  it("includes the expanded composer offset in the dock composer height token", () => {
    const { pane, dockComposer, frame } = mountDockComposerProbe();
    stubRectHeight(dockComposer, 168);

    flushResizeObserversFor(dockComposer);
    expect(pane.style.getPropertyValue("--dock-composer-height")).toBe("168px");

    frame.style.setProperty("--composer-expanded-offset", "284px");
    flushResizeObserversFor(frame);

    expect(pane.style.getPropertyValue("--dock-composer-height")).toBe("452px");
  });
});

/**
 * Tests for the smooth scroll behavior wired to the "跳到最新" pill.
 *
 * The pill calls `scrollConversationToBottom({ force: true, smooth: true })`
 * to reuse the rail-click jump's animated feel. The contract under test:
 *
 *   1. `smooth: true` delegates to `container.scrollTo({ behavior: "smooth" })`
 *      instead of clobbering `scrollTop` directly, so the browser animates
 *      the jump.
 *   2. When the OS asks for reduced motion, the call falls back to
 *      `behavior: "auto"` (still routed through `scrollTo`, so the rest
 *      of the auto-follow plumbing stays consistent).
 *   3. Without `smooth`, the call stays on the original instant path
 *      (`scrollTop = top`) and never invokes `scrollTo` — important for
 *      stream-tick auto-follow, which must not pay the smooth-animation
 *      cost on every render.
 *   4. Mid-animation scroll events must not trip the auto-follow
 *      re-engagement path and yank the viewport back to the bottom
 *      before the smooth animation lands. The pill must stay hidden
 *      the whole way through.
 */
describe("useConversationScrollState — smooth scroll-to-bottom", () => {
  let container: HTMLDivElement;
  let root: Root | null = null;
  let scrollNode: HTMLDivElement | null = null;
  let layout: StubbedLayout | null = null;
  let captured:
    | {
        scrollConversationToBottom: (options?: {
          force?: boolean;
          smooth?: boolean;
        }) => void;
      }
    | null = null;
  let scrollToCalls: Array<ScrollToOptions | undefined> = [];
  let originalMatchMedia: typeof window.matchMedia | undefined;
  let originalScrollTo: typeof Element.prototype.scrollTo | undefined;

  beforeEach(() => {
    container = document.createElement("div");
    document.body.appendChild(container);
    scrollToCalls = [];
    originalMatchMedia = window.matchMedia;
    originalScrollTo = Element.prototype.scrollTo;
    // jsdom does not implement `scrollTo` — record the calls so the tests
    // can assert on the options the helper actually picks. For the options
    // overload we deliberately do NOT write to `scrollTop`; in a real
    // browser `scrollTo({ behavior: "smooth" })` is asynchronous, and the
    // tests simulate the animation by manually advancing `scrollTop` and
    // dispatching scroll events.
    Element.prototype.scrollTo = function scrollTo(
      this: HTMLElement,
      options: ScrollToOptions | number,
    ) {
      if (typeof options === "number") {
        this.scrollTop = options;
        return;
      }
      scrollToCalls.push(options);
    } as typeof Element.prototype.scrollTo;
  });

  afterEach(() => {
    act(() => {
      root?.unmount();
    });
    root = null;
    scrollNode = null;
    layout = null;
    captured = null;
    document.body.removeChild(container);
    if (originalMatchMedia) {
      window.matchMedia = originalMatchMedia;
    } else {
      // jsdom ships without matchMedia; remove the stub so the next
      // suite sees the platform default.
      delete (window as unknown as { matchMedia?: typeof window.matchMedia })
        .matchMedia;
    }
    if (originalScrollTo) {
      Element.prototype.scrollTo = originalScrollTo;
    }
  });

  function setReducedMotion(matches: boolean): void {
    window.matchMedia = ((query: string) => ({
      matches,
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => false,
    })) as typeof window.matchMedia;
  }

  function mount(opts: {
    scrollHeight: number;
    clientHeight: number;
    initialScrollTop: number;
  }): {
    node: HTMLDivElement;
    scrollToBottom: (options?: { force?: boolean; smooth?: boolean }) => void;
  } {
    function SmoothProbe(): ReactNode {
      const h = useConversationScrollState({
        activeThreadID: "thread-1",
        activePane: "primary",
        splitConversation: false,
        primaryTurns: SMOOTH_PROBE_TURNS,
        secondaryTurns: undefined,
        emptyConversation: false,
        previewingLaunch: false,
        initialized: true,
      });
      captured = { scrollConversationToBottom: h.scrollConversationToBottom };
      return createElement("div", {
        ref: (node: HTMLDivElement | null) => {
          h.conversationScrollRef.current = node;
        },
        onScroll: () => h.handleConversationScroll(),
        "data-testid": "scroll-container",
        "data-user-scrolled-away": h.userScrolledAway ? "true" : "false",
      });
    }
    act(() => {
      root = createRoot(container);
      root.render(createElement(SmoothProbe));
    });
    const node = container.querySelector(
      "[data-testid='scroll-container']",
    ) as HTMLDivElement | null;
    if (!node) throw new Error("SmoothProbe did not render");
    scrollNode = node;
    layout = stubLayout(node, {
      scrollHeight: opts.scrollHeight,
      clientHeight: opts.clientHeight,
      scrollTop: opts.initialScrollTop,
    });
    if (!captured) throw new Error("SmoothProbe did not capture hooks");
    return { node, scrollToBottom: captured.scrollConversationToBottom };
  }

  function fireWheel(deltaY: number): void {
    if (!scrollNode) throw new Error("not mounted");
    act(() => {
      scrollNode!.dispatchEvent(
        new WheelEvent("wheel", { bubbles: true, deltaY }),
      );
      scrollNode!.dispatchEvent(new Event("scroll"));
    });
  }

  function fireScrollEvent(): void {
    if (!scrollNode) throw new Error("not mounted");
    act(() => {
      scrollNode!.dispatchEvent(new Event("scroll"));
    });
  }

  it("animates the jump with scrollTo({ behavior: 'smooth' }) when smooth: true", () => {
    const { scrollToBottom } = mount({
      scrollHeight: 2000,
      clientHeight: 600,
      initialScrollTop: 2000 - 600,
    });
    // Prime `lastConversationScrollTopRef` so the first wheel-up below
    // is read as a real scrolledUp against the parked-at-bottom position,
    // not against the unstubbed value left over from the mount effect.
    fireScrollEvent();
    // User scrolls up so the pill is visible.
    if (!layout) throw new Error("not mounted");
    layout.scrollTop = 500;
    fireWheel(-80);
    expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("true");
    scrollToCalls = [];

    act(() => {
      scrollToBottom({ force: true, smooth: true });
    });

    expect(scrollToCalls).toHaveLength(1);
    expect(scrollToCalls[0]).toEqual({ top: 2000, behavior: "smooth" });
    // The pill must hide the moment the user clicks it.
    expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("false");
  });

  it("falls back to behavior: 'auto' when the OS asks for reduced motion", () => {
    setReducedMotion(true);
    const { scrollToBottom } = mount({
      scrollHeight: 2000,
      clientHeight: 600,
      initialScrollTop: 500,
    });
    scrollToCalls = [];

    act(() => {
      scrollToBottom({ force: true, smooth: true });
    });

    expect(scrollToCalls).toHaveLength(1);
    expect(scrollToCalls[0]?.top).toBe(2000);
    // "auto" is the platform default for scrollTo and is the same
    // behavior as the instant path; the user-visible jump is the same
    // as not animating, but the call still routes through scrollTo so
    // the auto-follow plumbing stays consistent.
    expect(scrollToCalls[0]?.behavior).toBe("auto");
  });

  it("uses the instant path (no scrollTo) when smooth is not set", () => {
    const { scrollToBottom } = mount({
      scrollHeight: 2000,
      clientHeight: 600,
      initialScrollTop: 500,
    });
    scrollToCalls = [];

    act(() => {
      scrollToBottom({ force: true });
    });

    // The instant path writes scrollTop directly; scrollTo is not called.
    // Stream-tick auto-follow relies on this — it must not pay the
    // smooth-animation cost on every render.
    expect(scrollToCalls).toHaveLength(0);
    if (!layout) throw new Error("not mounted");
    // The stub clamps writes to [0, scrollHeight - clientHeight], so the
    // value the layout actually stores is 1400 (2000 - 600) — the proof
    // that the snap-to-bottom call happened, not the raw target.
    expect(layout.scrollTop).toBe(2000 - 600);
  });

  it("does not let mid-animation scroll events re-engage auto-follow or yank the scroll back", () => {
    // Regression guard: a smooth jump from the middle of the conversation
    // emits a stream of intermediate scroll events as the browser animates
    // toward the bottom. Without the new "suppress active, not yet at
    // latest" branch, those events would hit the auto-follow re-engagement
    // path and snap scrollTop back to scrollHeight, breaking the animation
    // and flashing the content past the user.
    const { scrollToBottom } = mount({
      scrollHeight: 2000,
      clientHeight: 600,
      initialScrollTop: 2000 - 600,
    });
    // Prime `lastConversationScrollTopRef` so the first wheel-up below
    // is read as a real scrolledUp against the parked-at-bottom position.
    fireScrollEvent();
    // Park the user somewhere in the middle so the pill is visible.
    if (!layout) throw new Error("not mounted");
    layout.scrollTop = 500;
    fireWheel(-80);
    expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("true");

    act(() => {
      scrollToBottom({ force: true, smooth: true });
    });
    // Pill hides the moment the user clicks it.
    expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("false");

    // Simulate the browser driving the smooth animation. Each intermediate
    // scrollTop is followed by a scroll event. The last value (1395) lands
    // inside the 16px bottom band, so the atLatestView branch in
    // handleConversationScroll takes over and clears the suppression flag
    // — this verifies that the smooth animation also completes correctly,
    // not just that the in-between frames behave.
    for (const intermediateTop of [600, 900, 1300, 1395]) {
      layout.scrollTop = intermediateTop;
      fireScrollEvent();
      // Pill must NOT reappear during the animation, and no extra
      // `scrollTo` call should fire (which would be a sign that the
      // auto-follow re-engagement path snapped us back to the bottom).
      expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("false");
    }
    // Only the initial scrollTo call from the pill click should have fired.
    expect(scrollToCalls).toHaveLength(1);
  });

  it("keeps automatic follow updates smooth while the jump is still moving", () => {
    const { scrollToBottom } = mount({
      scrollHeight: 2000,
      clientHeight: 600,
      initialScrollTop: 2000 - 600,
    });
    fireScrollEvent();
    if (!layout) throw new Error("not mounted");
    layout.scrollTop = 500;
    fireWheel(-80);
    expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("true");

    act(() => {
      scrollToBottom({ force: true, smooth: true });
    });
    expect(scrollToCalls).toEqual([{ top: 2000, behavior: "smooth" }]);

    act(() => {
      layout!.scrollHeight += 80;
      scrollToBottom();
    });

    expect(scrollToCalls).toEqual([
      { top: 2000, behavior: "smooth" },
      { top: 2080, behavior: "smooth" },
    ]);
    // A direct `scrollTop = scrollHeight` write here would clamp to the new
    // bottom immediately, which is the visible flash this regression guards.
    expect(layout.scrollTop).toBe(500);
    expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("false");
  });
});
