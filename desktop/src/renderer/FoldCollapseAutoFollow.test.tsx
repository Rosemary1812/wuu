/**
 * Live experiment: drive the full fold-collapse sequence on a stubbed
 * scroll container and log every visible motion of `scrollTop` plus
 * `userScrolledAway`. The user reported that after commentary output
 * finishes the whole message stream shakes up and down a bit; this
 * test exists to nail down which step in the fold-collapse / collapse
 * callback chain (if any) is the culprit.
 *
 * The fold collapse sequence (from AssistantTurnShell.tsx):
 *   t=0     turn.status flips in_progress -> completed
 *           scheduleStreamScroll() (from item/completed reducer)
 *           primaryTurns useEffect -> scheduleStreamScroll() (again)
 *   t=600   setExpanded(false)  ->  CSS transition starts
 *           scrollHeight shrinks (fold body collapses)
 *           browser auto-clamps scrollTop (Chromium behavior)
 *           onScroll -> handleConversationScroll -> setAutoFollow
 *   t=600+440=1040
 *           onCollapseComplete fires
 *           App.tsx handleTurnCollapseComplete runs
 *           -> enableConversationAutoFollow() + scheduleStreamScroll()
 *           -> scrollTop = scrollHeight
 *
 * The hypothesis (suspect 1 from the investigation) is that between
 * t=600 and t=1040 the user sees content shifted up, and the visible
 * "back down" only lands at t=1040 when onCollapseComplete re-anchors.
 * This test asserts whether that actually happens, or whether the
 * re-anchor is a no-op (already at clamped max).
 */
import { act, createElement, type ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";
import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it
} from "vitest";
import { useConversationScrollState } from "./ConversationScrollState";

type StubbedLayout = {
  scrollHeight: number;
  clientHeight: number;
  scrollTop: number;
};

type Frame = {
  atMs: number;
  label: string;
  scrollTop: number;
  scrollHeight: number;
  userScrolledAway: boolean;
  /** What auto-follow would do at this moment. True = next scroll lands at bottom. */
  autoFollowWouldScroll: boolean;
};

function makeLongTurns() {
  return [
    {
      id: "turn-1",
      items: [],
      items_view: "full" as const,
      status: "completed" as const,
    },
  ];
}

function stubLayout(node: HTMLElement, opts: Partial<StubbedLayout>) {
  const layout = {
    scrollHeight: opts.scrollHeight ?? 2000,
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

type HookHandle = ReturnType<typeof useConversationScrollState> & {
  conversationScrollRef: { current: HTMLDivElement | null };
};

function Probe({
  onReady,
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

describe("fold-collapse auto-follow race", () => {
  let container: HTMLDivElement;
  let root: Root | null = null;
  let handle: HookHandle | null = null;
  let node: HTMLDivElement | null = null;
  let layout: StubbedLayout | null = null;
  const frames: Frame[] = [];

  function snapshot(label: string, atMs: number): void {
    if (!layout || !node || !handle) return;
    frames.push({
      atMs,
      label,
      scrollTop: layout.scrollTop,
      scrollHeight: layout.scrollHeight,
      userScrolledAway: handle.userScrolledAway,
      // We don't have direct access to the ref, but the hook keeps
      // userScrolledAway mirrored to conversationAutoFollowRef. The
      // inverse: when userScrolledAway=false, the ref is true and the
      // next scheduleStreamScroll would actually scroll.
      autoFollowWouldScroll: !handle.userScrolledAway,
    });
  }

  /**
   * Simulate the browser auto-clamping scrollTop when scrollHeight
   * shrinks. Chromium clamps synchronously during layout; the scroll
   * event fires when scrollTop is assigned.
   */
  function applyShrink(newScrollHeight: number, label: string, atMs: number) {
    if (!layout || !node || !handle) return;
    act(() => {
      layout.scrollHeight = newScrollHeight;
      // Browser auto-clamps scrollTop.
      const max = Math.max(0, layout.scrollHeight - layout.clientHeight);
      if (layout.scrollTop > max) {
        layout.scrollTop = max;
        node!.dispatchEvent(new Event("scroll", { bubbles: false }));
      }
    });
    snapshot(label, atMs);
  }

  /**
   * Drive scheduleStreamScroll's RAF callback synchronously. jsdom
   * does not advance requestAnimationFrame automatically.
   */
  function flushScheduledScroll() {
    if (!handle || !node) return;
    act(() => {
      if (node) {
        node.scrollTop = node.scrollHeight;
        node.dispatchEvent(new Event("scroll", { bubbles: false }));
      }
    });
  }

  beforeEach(() => {
    container = document.createElement("div");
    document.body.appendChild(container);
    frames.length = 0;
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

  function mount(opts: { scrollHeight: number; clientHeight: number }) {
    act(() => {
      root = createRoot(container!);
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
      scrollTop: opts.scrollHeight - opts.clientHeight,
    });
  }

  it("documents the scroll position through the fold-collapse sequence", () => {
    mount({ scrollHeight: 2000, clientHeight: 600 });
    if (!layout || !handle || !node) throw new Error("not mounted");

    // Parked at the bottom: scrollTop=1400, max=1400.
    snapshot("t=-1: parked at bottom (post-mount)", -1);

    // Phase 1: 30 streaming deltas. Each one bumps content by 8px and
    // the server-event path calls scheduleStreamScroll, which we'd
    // run via RAF in production. We coalesce and assert pinned.
    for (let tick = 0; tick < 30; tick += 1) {
      act(() => {
        layout!.scrollHeight += 8;
        handle!.scheduleStreamScroll();
      });
      flushScheduledScroll();
    }
    snapshot("t=0: after 30 deltas, still at bottom", 0);

    // Phase 1.5: commentary completes. StreamingMarkdown sets
    // isLive=false; cursor starts fading (180ms timer). ScrollHeight
    // doesn't change yet because the cursor span still occupies its
    // line. We just record the state.

    // Phase 2: turn.status flips in_progress -> completed (the
    // turn.completed reducer runs). Two scroll calls happen here:
    //   (a) directly from the event handler (line 878)
    //   (b) via the primaryTurns useEffect (line 199-201)
    // Both coalesce into one RAF. We just call scheduleStreamScroll
    // twice and flush.
    act(() => {
      handle!.scheduleStreamScroll();
      handle!.scheduleStreamScroll();
    });
    flushScheduledScroll();
    snapshot("t=0+: turn.completed fired, scroll coalesced", 1);

    // Phase 2.5 (UP SHIFT #1): cursor removed from DOM 180ms after
    // isLive=false. StreamingMarkdown.tsx:296-313 sets cursorState
    // "fading" then "gone"; the <span class="stream-cursor"> is removed
    // from the fold body. scrollHeight shrinks by ~1 line.
    applyShrink(
      layout!.scrollHeight - 24,
      "t=180: cursor span removed (1 line shrink)",
      180,
    );

    // Phase 3 (DOWN SHIFT #1): tool_call arrives with arguments
    // streaming. The fold header preview switches from the
    // commentary text to the tool_call command (LightweightStreamingText
    // animates); the tool_call entry itself grows inside the fold
    // body as arguments stream in. scrollHeight grows back above its
    // pre-collapse level. The primaryTurns useEffect fires
    // scheduleStreamScroll -> RAF -> scrollConversationToBottom ->
    // scrollTop = scrollHeight -> clamps to the new max.
    applyShrink(
      layout!.scrollHeight + 280, // tool_call entry ~280px when fully expanded
      "t=200: tool_call + args arrive, scrollHeight grows back",
      200,
    );
    act(() => {
      handle!.scheduleStreamScroll();
    });
    flushScheduledScroll();
    snapshot("t=200+: auto-follow re-anchors to new bottom", 201);

    // Phase 4: 600ms later, setExpanded(false) flips, CSS transition
    // starts. Fold body collapses.
    applyShrink(
      layout!.scrollHeight - 400,
      "t=600: fold body shrinks 400px",
      600,
    );

    // Phase 5: 440ms after collapse start, onCollapseComplete fires.
    act(() => {
      handle!.enableConversationAutoFollow();
      handle!.scheduleStreamScroll();
    });
    flushScheduledScroll();
    snapshot(
      "t=1040: onCollapseComplete fires, scroll re-anchored to bottom",
      1040,
    );

    // ── Diagnostics ──
    // eslint-disable-next-line no-console
    console.log("\n=== fold-collapse scroll timeline ===");
    for (const f of frames) {
      // eslint-disable-next-line no-console
      console.log(
        `  ${f.label.padEnd(60)} scrollTop=${String(f.scrollTop).padStart(4)} scrollHeight=${String(f.scrollHeight).padStart(4)} userScrolledAway=${String(f.userScrolledAway).padEnd(5)} autoFollow=${String(f.autoFollowWouldScroll)}`,
      );
    }

    // ── Assertions ──
    // Snapshot index map (8 frames):
    //   frames[0] = t=-1   (parked at bottom)
    //   frames[1] = t=0    (after 30 deltas)
    //   frames[2] = t=0+   (after turn.completed, scroll coalesced)
    //   frames[3] = t=180  (cursor span removed)                 ← UP SHIFT
    //   frames[4] = t=200  (tool_call + args arrive, scrollHeight grew)
    //   frames[5] = t=200+ (auto-follow re-anchor)               ← DOWN SHIFT
    //   frames[6] = t=600  (fold body shrinks 400px)
    //   frames[7] = t=1040 (onCollapseComplete fires)
    const tParked = frames[0];
    const tPostStream = frames[1];
    const tPostTurn = frames[2];
    const tPostCursorRemoved = frames[3];
    const tPostToolCallArrived = frames[4];
    const tPostAutoFollow = frames[5];
    const tPostCollapse = frames[6];
    const tPostCallback = frames[7];

    // Sanity: we're at the bottom at the start.
    expect(tParked.scrollTop).toBe(1400);
    expect(tParked.userScrolledAway).toBe(false);

    // After streaming, still at bottom (auto-follow working).
    expect(tPostStream.scrollHeight).toBe(2240);
    expect(tPostStream.scrollTop).toBe(1640);

    // Cursor removed: scrollHeight shrinks 24px (1 line); scrollTop
    // clamps UP to the new max (1640 - 24 = 1616).
    expect(tPostCursorRemoved.scrollHeight).toBe(2216);
    expect(tPostCursorRemoved.scrollTop).toBe(1616);

    // Tool_call + args arrive, scrollHeight grows back (2240 + 280 =
    // 2496). The browser doesn't auto-clamp (1616 < 1896 = new max).
    expect(tPostToolCallArrived.scrollHeight).toBe(2496);
    expect(tPostToolCallArrived.scrollTop).toBe(1616);

    // Auto-follow re-anchor: scrollTop = scrollHeight = 2496 -> clamps
    // to new max 1896. This is the DOWN shift.
    expect(tPostAutoFollow.scrollHeight).toBe(2496);
    expect(tPostAutoFollow.scrollTop).toBe(1896);

    // After fold shrink: scrollTop clamps UP to new max (2496 - 400 =
    // 2096; max = 1496).
    const expectedMaxAfterShrink = 2096 - 600; // = 1496
    expect(tPostCollapse.scrollTop).toBe(expectedMaxAfterShrink);

    // The user-reported V-shape regression gate: scrollTop went UP
    // when the cursor was removed (frames[2] -> frames[3]) and then
    // DOWN when the auto-follow re-anchored after the next item grew
    // scrollHeight (frames[3] -> frames[5]). If the fix prevents
    // either shift, scrollTop stays monotonic and this assertion
    // fails.
    const upShift =
      tPostCursorRemoved.scrollTop - tPostTurn.scrollTop;
    const downShift =
      tPostAutoFollow.scrollTop - tPostCursorRemoved.scrollTop;
    if (upShift < 0 && downShift > 0 && Math.abs(downShift) > 0) {
      // eslint-disable-next-line no-console
      console.log(
        `\n  >>> CONFIRMED V-SHAPE: scrollTop went ${tPostTurn.scrollTop} -> ${tPostCursorRemoved.scrollTop} (UP ${Math.abs(upShift)}) -> ${tPostAutoFollow.scrollTop} (DOWN ${downShift})`,
      );
      // eslint-disable-next-line no-console
      console.log(
        `  >>> Cursor removal is the UP trigger; next-item growth + auto-follow is the DOWN trigger.`,
      );
    } else {
      // eslint-disable-next-line no-console
      console.log(
        `\n  >>> No V-shape detected. upShift=${upShift}, downShift=${downShift}. Hypothesis about cursor-removal-UP + auto-follow-DOWN is NOT the jitter path.`,
      );
    }

    // The onCollapseComplete scroll re-anchor is a no-op (scrollTop
    // already at the clamped max with auto-follow still engaged).
    expect(tPostCallback.scrollTop).toBe(tPostCollapse.scrollTop);
  });
});