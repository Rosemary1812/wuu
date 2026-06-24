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
    showingWorkspaceMode: false,
    initialized: true,
  });
  return createElement("div", {
    ref: (node: HTMLDivElement | null) => {
      h.conversationScrollRef.current = node;
    },
    onScroll: () => h.handleConversationScroll(),
    "data-testid": "scroll-container",
    "data-user-scrolled-away": h.userScrolledAway ? "true" : "false",
  });
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
    fireScroll();
    expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("true");
  });

  it("hides the pill when the user scrolls back into the bottom band", () => {
    mount({ scrollHeight: 2000, clientHeight: 600, initialScrollTop: 2000 - 600 });
    fireScroll();

    setScrollTop(500);
    fireScroll();
    expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("true");

    // Park the user at the very bottom of the conversation.
    setScrollTop(2000 - 600); // distanceFromBottom = 0
    fireScroll();
    expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("false");
  });

  it("hides the pill when the user is parked inside the bottom band", () => {
    mount({ scrollHeight: 2000, clientHeight: 600, initialScrollTop: 2000 - 600 });
    fireScroll();
    setScrollTop(500);
    fireScroll();
    expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("true");
    // 8px from the bottom — well within the 16px band.
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
    fireScroll();
    expect(scrollNode!.dataset.userScrolledAway ?? "false").toBe("true");

    // Scroll back to the bottom: scrolledUp is false and we're parked
    // at the latest view, so auto-follow re-engages and the pill hides.
    setScrollTop(2000 - 600);
    fireScroll();
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
    fireScroll();
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
    fireScroll();
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
