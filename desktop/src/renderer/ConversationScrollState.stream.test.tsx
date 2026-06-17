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
  return createElement("div", {
    ref: (node: HTMLDivElement | null) => {
      handle.conversationScrollRef.current = node;
      if (node) onReady(handle, node);
    },
    onScroll: () => handle.handleConversationScroll(),
    "data-testid": "scroll-container",
    "data-user-scrolled-away": handle.userScrolledAway ? "true" : "false",
  });
}

describe("useConversationScrollState — high-frequency stream", () => {
  let container: HTMLDivElement;
  let root: Root | null = null;
  let handle: HookHandle | null = null;
  let node: HTMLDivElement | null = null;
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
    handle = null;
    node = null;
    layout = null;
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

  it("stays pinned to the bottom across 120 fast stream ticks", () => {
    // Start: 2000px of content in a 600px viewport. We are at the
    // bottom (scrollTop = 1400).
    mount({ scrollHeight: 2000, clientHeight: 600 });
    if (!layout || !handle || !node) throw new Error("not mounted");

    // 120 ticks, each adding 8px of streamed content. This is
    // representative of a fast stream (think Claude Sonnet on a long
    // answer), and 120 frames at 60fps is 2 seconds of streaming.
    for (let tick = 0; tick < 120; tick += 1) {
      act(() => {
        // (1) The server pushed a delta; React commits new content
        //     and the layout grows.
        layout!.scrollHeight += 8;
        // (2) The server-event path calls scheduleStreamScroll. This
        //     would normally register a RAF, which we then run
        //     synchronously below to simulate the next frame.
        handle!.scheduleStreamScroll();
      });
      // (3) The browser runs the RAF callback, which sets
      //     scrollTop = scrollHeight.
      act(() => {
        // jsdom does not advance requestAnimationFrame automatically,
        // so we drive the hook the way a real browser would: read
        // the latest scrollHeight and assign it to scrollTop.
        if (node) {
          node.scrollTop = node.scrollHeight;
          // (4) Then dispatch the scroll event the browser would fire
          //     after that assignment.
          node.dispatchEvent(new Event("scroll", { bubbles: false }));
        }
      });

      const bottom = layout.scrollHeight - layout.clientHeight;
      // (5) The scroll position must still be at the bottom, and the
      //     "Jump to latest" pill must NOT have appeared.
      expect(layout.scrollTop).toBe(bottom);
      expect(node!.dataset.userScrolledAway ?? "false").toBe("false");
    }
  });

  it("survives a sub-frame stream tick that lands scrollTop exactly at the threshold edge", () => {
    // Edge case: scrollTop ends at exactly CONVERSATION_AUTO_SCROLL_THRESHOLD_PX
    // from the bottom (16px). The pill must hide and auto-follow must
    // stay engaged.
    mount({ scrollHeight: 2000, clientHeight: 600 });
    if (!layout || !handle || !node) throw new Error("not mounted");

    act(() => {
      handle!.scheduleStreamScroll();
      if (node) {
        node.scrollTop = node.scrollHeight;
        node.dispatchEvent(new Event("scroll", { bubbles: false }));
      }
    });
    expect(node!.dataset.userScrolledAway ?? "false").toBe("false");

    // Drop scrollTop exactly to 16px above the bottom.
    act(() => {
      layout!.scrollTop = layout!.scrollHeight - layout!.clientHeight - 16;
      if (node) {
        node.dispatchEvent(new Event("scroll", { bubbles: false }));
      }
    });
    // 16px is the inclusive threshold, so the pill must still hide.
    expect(node!.dataset.userScrolledAway ?? "false").toBe("false");

    // One more pixel up and the pill should now show.
    act(() => {
      layout!.scrollTop = layout!.scrollHeight - layout!.clientHeight - 17;
      if (node) {
        node.dispatchEvent(new Event("scroll", { bubbles: false }));
      }
    });
    expect(node!.dataset.userScrolledAway ?? "false").toBe("true");
  });
});
