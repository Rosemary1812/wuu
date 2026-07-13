import {
  type MutableRefObject,
  type RefObject,
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState
} from "react";
import type { Turn } from "../shared/protocol";
import type { ConversationPaneID } from "./AppState";
import {
  AUTO_FOLLOW_BOTTOM_THRESHOLD_PX,
  AUTO_FOLLOW_SCROLLBAR_HIDE_DELAY_MS,
  SCROLL_AWAY_KEYS,
  USER_SCROLL_AWAY_INTENT_WINDOW_MS,
  atLatestScrollView,
  clampScrollTop,
  eventTargetsNestedAutoFollowScroll,
  maxScrollTop,
  observeAutoFollowResizeTargets,
  setAutoFollowOverflowAnchor,
} from "./AutoFollowScroll";
import {
  createWindowResizeSettleScheduler,
  isWindowResizing,
} from "./WindowResizeState";

// Tight threshold so the conversation only re-engages auto-follow when the
// user is effectively parked at the bottom. The previous 48px band let one
// mouse-wheel notch land inside the band and silently re-arm auto-follow,
// which made slow scroll-up get yanked back to the bottom mid-gesture.
const CONVERSATION_AUTO_SCROLL_THRESHOLD_PX = AUTO_FOLLOW_BOTTOM_THRESHOLD_PX;
const CONVERSATION_SCROLLBAR_HIDE_DELAY_MS = AUTO_FOLLOW_SCROLLBAR_HIDE_DELAY_MS;
const CONVERSATION_USER_SCROLL_AWAY_INTENT_WINDOW_MS =
  USER_SCROLL_AWAY_INTENT_WINDOW_MS;

function cssPixelValue(value: string): number {
  const parsed = Number.parseFloat(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
}

function dockComposerVisualHeight(node: HTMLElement): number {
  const layoutHeight = Math.ceil(node.getBoundingClientRect().height);
  const frame = node.querySelector<HTMLElement>(".composer-frame");
  if (!frame) {
    return layoutHeight;
  }
  const expandedOffset = cssPixelValue(
    frame.style.getPropertyValue("--composer-expanded-offset") ||
      window.getComputedStyle(frame).getPropertyValue("--composer-expanded-offset")
  );
  return layoutHeight + expandedOffset;
}

type ConversationScrollSnapshot = {
  scrollTop: number;
  autoFollow: boolean;
};

export function useConversationScrollState({
  activeThreadID,
  activePane,
  splitConversation,
  primaryTurns,
  secondaryTurns,
  emptyConversation,
  previewingLaunch,
  initialized
}: {
  activeThreadID?: string;
  activePane: ConversationPaneID;
  splitConversation: boolean;
  primaryTurns?: Turn[];
  secondaryTurns?: Turn[];
  emptyConversation: boolean;
  previewingLaunch: boolean;
  initialized: boolean;
}): {
  conversationScrollRef: RefObject<HTMLDivElement | null>;
  /** Wrapper inside the conversation viewport. */
  scrollContentRef: RefObject<HTMLDivElement | null>;
  splitPaneRefs: MutableRefObject<Record<ConversationPaneID, HTMLElement | null>>;
  conversationPaneRef: RefObject<HTMLElement | null>;
  dockComposerRef: (node: HTMLElement | null) => void;
  /**
   * The live dock-composer element (set by dockComposerRef). Exposed so the
   * "跳到最新" pill can anchor itself just above the composer by direct
   * measurement — see JumpToLatestPill's anchored mode.
   */
  dockComposerNode: HTMLElement | null;
  scheduleStreamScroll: () => void;
  handleConversationScroll: (scrolledNode?: HTMLElement) => void;
  scrollConversationToBottom: (options?: {
    force?: boolean;
    /**
     * Animate the jump with `scrollTo({ behavior: "smooth" })` so the
     * viewport glides to the bottom instead of flashing past the content.
     * Used by the "跳到最新" pill to match the rail-click jump feel.
     * Falls back to the instant path when the OS asks for reduced motion.
     */
    smooth?: boolean;
  }) => void;
  enableConversationAutoFollow: () => void;
  /**
   * Pause auto-follow so a programmatic scroll (e.g. query-history
   * jump) doesn't get pulled back to the bottom by the next stream
   * tick. Auto-follow resumes naturally once the user scrolls back
   * near the bottom.
   */
  disableConversationAutoFollow: () => void;
  /**
   * True when the user has scrolled up out of the bottom band. Drives
   * the "Jump to latest" pill: hidden when false, shown when true.
   */
  userScrolledAway: boolean;
} {
  const conversationScrollRef = useRef<HTMLDivElement | null>(null);
  const splitPaneRefs = useRef<Record<ConversationPaneID, HTMLElement | null>>({
    primary: null,
    secondary: null
  });
  const conversationPaneRef = useRef<HTMLElement | null>(null);
  const [dockComposerNode, setDockComposerNode] = useState<HTMLElement | null>(null);
  const dockComposerRef = useCallback((node: HTMLElement | null) => {
    setDockComposerNode(node);
  }, []);
  const dockComposerHeightRef = useRef(0);
  const conversationAutoFollowRef = useRef(true);
  // Mirrors the ref for the UI: true when the user has scrolled up out of
  // the bottom band. Drives the "Jump to latest" pill visibility.
  const [userScrolledAway, setUserScrolledAway] = useState(false);
  // Single source of truth for transitions into / out of auto-follow. Keeps
  // the ref (read synchronously by scroll handlers) and the state (read by
  // the UI) in lockstep.
  const setAutoFollow = useCallback((next: boolean): void => {
    if (conversationAutoFollowRef.current !== next) {
      conversationAutoFollowRef.current = next;
      setUserScrolledAway(!next);
    }
  }, []);
  const lastConversationScrollTopRef = useRef(0);
  const programmaticScrollTopRef = useRef<number | undefined>(undefined);
  const suppressAutoFollowRearmRef = useRef(false);
  const userScrollAwayIntentRef = useRef(false);
  const userScrollAwayIntentTimerRef = useRef<number | undefined>(undefined);
  const userScrollAwayStartTopRef = useRef<number | undefined>(undefined);
  const touchLastYRef = useRef<number | undefined>(undefined);
  const threadScrollSnapshotsRef = useRef(
    new Map<string, ConversationScrollSnapshot>()
  );
  const streamScrollFrameRef = useRef<number | undefined>(undefined);
  const liveResizeScrollFrameRef = useRef<number | undefined>(undefined);
  const resizeSettleStreamScrollRef = useRef<ReturnType<
    typeof createWindowResizeSettleScheduler
  > | null>(null);
  const conversationScrollbarHideTimerRef = useRef<number | undefined>(undefined);
  const scrollContentRef = useRef<HTMLDivElement | null>(null);

  function conversationViewport(): HTMLElement | undefined {
    if (splitConversation) {
      return splitPaneRefs.current[activePane] ?? undefined;
    }
    return conversationScrollRef.current ?? undefined;
  }

  function showConversationScrollbar(node: HTMLElement): void {
    if (
      node.classList.contains("empty-scroll-region") ||
      node.classList.contains("workspace-scroll-region") ||
      node.scrollHeight <= node.clientHeight
    ) {
      return;
    }
    node.classList.add("scrollbar-visible");
    if (conversationScrollbarHideTimerRef.current !== undefined) {
      window.clearTimeout(conversationScrollbarHideTimerRef.current);
    }
    conversationScrollbarHideTimerRef.current = window.setTimeout(() => {
      conversationScrollbarHideTimerRef.current = undefined;
      node.classList.remove("scrollbar-visible");
    }, CONVERSATION_SCROLLBAR_HIDE_DELAY_MS);
  }

  function rememberThreadScrollSnapshot(
    threadID: string,
    node: HTMLElement,
    autoFollow: boolean
  ): void {
    threadScrollSnapshotsRef.current.set(threadID, {
      scrollTop: clampScrollTop(node, node.scrollTop),
      autoFollow: node.scrollHeight <= node.clientHeight ? true : autoFollow
    });
  }

  function rememberActiveThreadScrollSnapshot(
    node: HTMLElement,
    autoFollow: boolean
  ): void {
    if (!activeThreadID) {
      return;
    }
    rememberThreadScrollSnapshot(activeThreadID, node, autoFollow);
  }

  const clearUserScrollAwayIntent = useCallback((): void => {
    userScrollAwayIntentRef.current = false;
    userScrollAwayStartTopRef.current = undefined;
    touchLastYRef.current = undefined;
    if (userScrollAwayIntentTimerRef.current !== undefined) {
      window.clearTimeout(userScrollAwayIntentTimerRef.current);
      userScrollAwayIntentTimerRef.current = undefined;
    }
  }, []);

  const markUserScrollAwayIntent = useCallback((startTop?: number): void => {
    userScrollAwayIntentRef.current = true;
    if (startTop !== undefined) {
      userScrollAwayStartTopRef.current = startTop;
    }
    if (userScrollAwayIntentTimerRef.current !== undefined) {
      window.clearTimeout(userScrollAwayIntentTimerRef.current);
    }
    userScrollAwayIntentTimerRef.current = window.setTimeout(() => {
      userScrollAwayIntentRef.current = false;
      userScrollAwayStartTopRef.current = undefined;
      userScrollAwayIntentTimerRef.current = undefined;
    }, CONVERSATION_USER_SCROLL_AWAY_INTENT_WINDOW_MS);
  }, []);

  function applyProgrammaticScroll(
    node: HTMLElement,
    top: number,
    autoFollow: boolean,
    options: { revealScrollbar?: boolean; smooth?: boolean } = {}
  ): void {
    clearUserScrollAwayIntent();
    const smooth = options.smooth === true;
    // Match the rail-click jump in `scrollAnchorIntoContainer`: when the
    // user has not asked the OS to reduce motion, animate the scroll.
    // The reduced-motion fallback uses the platform default ("auto"),
    // which is the same one as the instant path — no extra branch.
    // The optional chain must cover the whole `matchMedia(...).matches`
    // expression — jsdom does not implement `matchMedia`, so the bare
    // `?.(...)` followed by `.matches` would throw on the undefined result.
    const prefersReducedMotion =
      typeof window !== "undefined" &&
      window.matchMedia?.("(prefers-reduced-motion: reduce)").matches === true;
    const behavior: ScrollBehavior =
      smooth && !prefersReducedMotion ? "smooth" : "auto";
    if (smooth) {
      // Hold auto-follow re-engagement off until the smooth animation
      // actually lands at the bottom (or the user takes manual control).
      // The new "suppress active, not yet at latest" branch in
      // handleConversationScroll reads this flag to keep the in-flight
      // autoFollow state untouched while the browser is animating.
      suppressAutoFollowRearmRef.current = true;
      // `scrollTo({ behavior: "smooth" })` is asynchronous, so `node.scrollTop`
      // is still the pre-scroll value here. The scroll event handler will
      // see the marker on the first event and treat the animation as the
      // single user-visible motion.
      node.scrollTo({ top, behavior });
      programmaticScrollTopRef.current = top;
      // Leave `lastConversationScrollTopRef` at its previous value so the
      // first frame of the animation is read as `scrolledDown`, which
      // routes through the new suppression branch instead of the
      // instant auto-follow re-engagement path.
    } else {
      suppressAutoFollowRearmRef.current = false;
      node.scrollTop = top;
      const actualTop = clampScrollTop(node, node.scrollTop);
      if (Math.abs(node.scrollTop - actualTop) > 1) {
        node.scrollTop = actualTop;
      }
      programmaticScrollTopRef.current = actualTop;
      lastConversationScrollTopRef.current = actualTop;
    }
    const nextAutoFollow =
      node.scrollHeight <= node.clientHeight ? true : autoFollow;
    setAutoFollow(nextAutoFollow);
    setAutoFollowOverflowAnchor(node, nextAutoFollow);
    rememberActiveThreadScrollSnapshot(node, nextAutoFollow);
    if (options.revealScrollbar) {
      showConversationScrollbar(node);
    }
  }

  const scrollConversationToBottom = useCallback(
    (
      options: { force?: boolean; smooth?: boolean } = {}
    ): void => {
      const node = conversationViewport();
      if (!node || (!options.force && !conversationAutoFollowRef.current)) {
        return;
      }
      const continueSmoothJump =
        !options.force &&
        suppressAutoFollowRearmRef.current &&
        conversationAutoFollowRef.current;
      applyProgrammaticScroll(node, node.scrollHeight, true, {
        revealScrollbar: true,
        smooth: options.smooth === true || continueSmoothJump,
      });
    },
    [activePane, activeThreadID, setAutoFollow, splitConversation]
  );

  const scheduleLiveResizeScroll = useCallback((): void => {
    if (
      liveResizeScrollFrameRef.current !== undefined ||
      !conversationAutoFollowRef.current
    ) {
      return;
    }
    liveResizeScrollFrameRef.current = window.requestAnimationFrame(() => {
      liveResizeScrollFrameRef.current = undefined;
      const node = conversationViewport();
      if (
        !node ||
        !isWindowResizing() ||
        !conversationAutoFollowRef.current
      ) {
        return;
      }
      // Chromium clamps oversized scroll targets to the real bottom. Using a
      // sentinel avoids reading scrollHeight/clientHeight during live resize,
      // and rAF coalescing caps the work at one scroll write per paint.
      node.scrollTop = Number.MAX_SAFE_INTEGER;
      lastConversationScrollTopRef.current = node.scrollTop;
    });
  }, [activePane, splitConversation]);

  const scheduleStreamScroll = useCallback((): void => {
    if (!activeThreadID) {
      return;
    }
    if (!conversationAutoFollowRef.current) {
      return;
    }
    if (isWindowResizing()) {
      scheduleLiveResizeScroll();
      if (!resizeSettleStreamScrollRef.current) {
        resizeSettleStreamScrollRef.current =
          createWindowResizeSettleScheduler(scrollConversationToBottom);
      }
      resizeSettleStreamScrollRef.current.schedule();
      return;
    }
    if (streamScrollFrameRef.current !== undefined) {
      return;
    }
    streamScrollFrameRef.current = window.requestAnimationFrame(() => {
      scrollConversationToBottom();
      streamScrollFrameRef.current = window.requestAnimationFrame(() => {
        streamScrollFrameRef.current = undefined;
        scrollConversationToBottom();
      });
    });
  }, [scheduleLiveResizeScroll, scrollConversationToBottom]);

  useEffect(() => {
    resizeSettleStreamScrollRef.current?.cancel();
    resizeSettleStreamScrollRef.current = null;
  }, [scrollConversationToBottom]);

  const enableConversationAutoFollow = useCallback((): void => {
    suppressAutoFollowRearmRef.current = false;
    setAutoFollow(true);
    const node = conversationViewport();
    if (node) {
      setAutoFollowOverflowAnchor(node, true);
      rememberActiveThreadScrollSnapshot(node, true);
    }
  }, [activePane, activeThreadID, setAutoFollow, splitConversation]);

  const disableConversationAutoFollow = useCallback((): void => {
    suppressAutoFollowRearmRef.current = true;
    setAutoFollow(false);
    const node = conversationViewport();
    if (node) {
      setAutoFollowOverflowAnchor(node, false);
      rememberActiveThreadScrollSnapshot(node, false);
    }
  }, [activePane, activeThreadID, setAutoFollow, splitConversation]);

  function handleConversationScroll(scrolledNode?: HTMLElement): void {
    const node = scrolledNode ?? conversationViewport();
    if (!node) {
      return;
    }
    if (isWindowResizing()) {
      if (conversationAutoFollowRef.current) {
        scheduleStreamScroll();
      }
      return;
    }
    showConversationScrollbar(node);

    const programmaticTop = programmaticScrollTopRef.current;
    if (programmaticTop !== undefined) {
      programmaticScrollTopRef.current = undefined;
      if (Math.abs(node.scrollTop - programmaticTop) <= 1) {
        lastConversationScrollTopRef.current = clampScrollTop(
          node,
          node.scrollTop
        );
        if (
          conversationAutoFollowRef.current &&
          !userScrollAwayIntentRef.current &&
          !atLatestScrollView(node, CONVERSATION_AUTO_SCROLL_THRESHOLD_PX)
        ) {
          scrollConversationToBottom({
            smooth: suppressAutoFollowRearmRef.current,
          });
          return;
        }
        rememberActiveThreadScrollSnapshot(
          node,
          conversationAutoFollowRef.current
        );
        return;
      }
    }

    // Position-driven userScrolledAway (drives the "Jump to latest" pill)
    // and intent-driven auto-follow.
    //
    // The previous logic re-armed auto-follow whenever the user landed
    // inside the bottom band (distanceFromBottom <= 16px), regardless of
    // scroll direction. That created a dead zone: any wheel-up landing
    // inside the band left auto-follow engaged, so the next stream tick
    // (or `onCollapseComplete` re-anchor after a fold shrink) yanked
    // scrollTop back to scrollHeight and the user felt the scroll as
    // "resistant" — most visibly during model output but universally
    // any time something triggered `scheduleStreamScroll` while the user
    // was inside the band.
    //
    // User intent overrides position: any user-initiated upward scroll
    // disarms auto-follow, regardless of how small the delta is. But
    // do not derive that solely from `previousScrollTop`: when the
    // conversation was hidden while content streamed, the old scrollTop can
    // be lower than the remounted bottom, making the first user scroll-up
    // look like a downward move.
    //
    // Still, only disarm after the viewport has actually left the absolute
    // bottom. A nested reasoning/process scroll can emit an upward wheel
    // without moving the outer conversation; that should keep following.
    //
    // layout-driven scrollTop clamps (for example a completed process fold
    // shrinking above the viewport) can also move scrollTop upward while
    // the viewport is still at the latest content. Those must keep
    // auto-follow armed; otherwise the next streaming or settle frame will
    // stop sticking to the bottom even though the user never scrolled away.
    const previousScrollTop = lastConversationScrollTopRef.current;
    const scrolledUp = node.scrollTop < previousScrollTop;
    const scrolledDown = node.scrollTop > previousScrollTop;
    const userScrollAwayIntent = userScrollAwayIntentRef.current;
    lastConversationScrollTopRef.current = clampScrollTop(node, node.scrollTop);

    const atLatestView = atLatestScrollView(
      node,
      CONVERSATION_AUTO_SCROLL_THRESHOLD_PX
    );
    const scrollAwayStartTop = userScrollAwayStartTopRef.current;
    const movedAboveUserIntentStart =
      userScrollAwayIntent &&
      scrollAwayStartTop !== undefined &&
      node.scrollTop < scrollAwayStartTop - 1;
    const movedAbovePreviousScroll = userScrollAwayIntent && scrolledUp;
    let nextAutoFollow = conversationAutoFollowRef.current;
    if (
      (movedAboveUserIntentStart || movedAbovePreviousScroll) &&
      node.scrollTop < maxScrollTop(node) - 1
    ) {
      suppressAutoFollowRearmRef.current = false;
      userScrollAwayStartTopRef.current = undefined;
      nextAutoFollow = false;
      setAutoFollow(false);
      setAutoFollowOverflowAnchor(node, false);
    } else if (
      conversationAutoFollowRef.current &&
      scrolledUp &&
      !atLatestView &&
      node.scrollTop < maxScrollTop(node) - 1
    ) {
      // Native scrollbar drags and some platform scroll paths can arrive as
      // a bare scroll event. If the viewport moved upward away from latest
      // content, treat it as user control even without a prior wheel/key/touch.
      suppressAutoFollowRearmRef.current = false;
      nextAutoFollow = false;
      setAutoFollow(false);
      setAutoFollowOverflowAnchor(node, false);
    } else if (atLatestView && suppressAutoFollowRearmRef.current) {
      // Query-history / turn-rail jumps are programmatic smooth scrolls.
      // The browser can emit an unchanged or tiny upward scroll event while
      // the viewport is still inside the bottom band. If that re-arms
      // auto-follow, the next scroll/layout signal yanks the viewport back to
      // the bottom before the jump reaches its target. Only an actual downward
      // move back to the latest content should clear this jump guard.
      if (scrolledDown) {
        suppressAutoFollowRearmRef.current = false;
        nextAutoFollow = true;
        setAutoFollow(true);
        setAutoFollowOverflowAnchor(node, true);
      } else {
        nextAutoFollow = false;
        setAutoFollow(false);
        setAutoFollowOverflowAnchor(node, false);
      }
    } else if (suppressAutoFollowRearmRef.current) {
      // "跳到最新" smooth scroll (or any other smooth programmatic jump)
      // is in flight, and the viewport has not yet reached the bottom
      // band. The previous branch already handled the atLatestView case;
      // this branch covers the in-between frames.
      //
      // The smooth animation produces a stream of `scrolledDown` scroll
      // events as the viewport glides to the bottom. If we let branch 4
      // fire here, it would call `applyProgrammaticScroll(..., true)`
      // and instantly snap the scroll back to scrollHeight, breaking the
      // animation. Instead, track the position only and let the flag be
      // cleared by the atLatestView branch above when the animation
      // actually lands at the bottom (or by the user-initiated branches
      // when the user takes manual control).
      //
      // Critically, do NOT call `setAutoFollow` here: doing so would
      // flip `userScrolledAway` back to `true` mid-animation and make
      // the "跳到最新" pill reappear while the viewport is still
      // gliding to the bottom. The pill was hidden when the user
      // clicked it; it should stay hidden until the animation lands.
      nextAutoFollow = conversationAutoFollowRef.current;
    } else if (atLatestView) {
      suppressAutoFollowRearmRef.current = false;
      if (
        conversationAutoFollowRef.current ||
        scrolledDown ||
        node.scrollHeight <= node.clientHeight
      ) {
        nextAutoFollow = true;
        setAutoFollow(true);
        setAutoFollowOverflowAnchor(node, true);
      } else {
        // A layout shrink can clamp an already-away viewport to the new max
        // scrollTop without user intent. Keep the user's away state unless
        // they actively scroll down to latest or content no longer scrolls.
        nextAutoFollow = false;
        setAutoFollow(false);
        setAutoFollowOverflowAnchor(node, false);
      }
    } else if (conversationAutoFollowRef.current && !userScrollAwayIntent) {
      suppressAutoFollowRearmRef.current = false;
      if (isWindowResizing()) {
        nextAutoFollow = true;
        scheduleStreamScroll();
        rememberActiveThreadScrollSnapshot(node, nextAutoFollow);
        return;
      }
      // A raw scroll event that leaves the latest view is ambiguous: it can
      // be a scrollbar drag, a platform scroll path without wheel/key/touch
      // preflight, or a stale baseline after the conversation remounts. The
      // durable auto-follow signals already call scrollConversationToBottom
      // directly (stream frames, turn snapshots, resize observers, fold
      // collapse), so do not yank the viewport back from this fallback path.
      nextAutoFollow = false;
      setAutoFollow(false);
      setAutoFollowOverflowAnchor(node, false);
    } else {
      suppressAutoFollowRearmRef.current = false;
    }
    rememberActiveThreadScrollSnapshot(node, nextAutoFollow);
  }

  useLayoutEffect(() => {
    const node = conversationViewport();
    if (!activeThreadID || !node) {
      programmaticScrollTopRef.current = undefined;
      lastConversationScrollTopRef.current = 0;
      setAutoFollow(true);
      return undefined;
    }

    const snapshot = threadScrollSnapshotsRef.current.get(activeThreadID);
    if (snapshot && !snapshot.autoFollow) {
      applyProgrammaticScroll(node, snapshot.scrollTop, false);
    } else {
      applyProgrammaticScroll(node, node.scrollHeight, true);
    }
    return undefined;
  }, [activePane, activeThreadID, setAutoFollow, splitConversation]);

  useLayoutEffect(() => {
    if (!activeThreadID) {
      return;
    }
    // Turn snapshots can add non-token content (for example a gray process
    // row). Re-anchor before paint so the bottom never flashes at old scrollTop.
    if (isWindowResizing()) {
      scheduleStreamScroll();
      return;
    }
    scrollConversationToBottom();
  }, [
    activeThreadID,
    primaryTurns,
    scheduleStreamScroll,
    scrollConversationToBottom,
    secondaryTurns,
  ]);

  useLayoutEffect(() => {
    const node = conversationViewport();
    if (!node) {
      return undefined;
    }
    const handleWheel = (event: WheelEvent): void => {
      if (eventTargetsNestedAutoFollowScroll(event.target, node)) {
        if (event.deltaY < 0) {
          markUserScrollAwayIntent(clampScrollTop(node, node.scrollTop));
        }
        return;
      }
      if (event.deltaY < 0) {
        markUserScrollAwayIntent(clampScrollTop(node, node.scrollTop));
      }
    };
    const handlePointerDown = (event: PointerEvent): void => {
      if (eventTargetsNestedAutoFollowScroll(event.target, node)) {
        return;
      }
      if (event.target === node) {
        markUserScrollAwayIntent(clampScrollTop(node, node.scrollTop));
      }
    };
    const handleKeyDown = (event: KeyboardEvent): void => {
      if (eventTargetsNestedAutoFollowScroll(event.target, node)) {
        return;
      }
      if (SCROLL_AWAY_KEYS.has(event.key)) {
        markUserScrollAwayIntent(clampScrollTop(node, node.scrollTop));
      }
    };
    const handleTouchStart = (event: TouchEvent): void => {
      if (eventTargetsNestedAutoFollowScroll(event.target, node)) {
        touchLastYRef.current = event.touches[0]?.clientY;
        return;
      }
      touchLastYRef.current = event.touches[0]?.clientY;
    };
    const handleTouchMove = (event: TouchEvent): void => {
      if (eventTargetsNestedAutoFollowScroll(event.target, node)) {
        const currentY = event.touches[0]?.clientY;
        const previousY = touchLastYRef.current;
        if (
          currentY !== undefined &&
          previousY !== undefined &&
          currentY > previousY
        ) {
          markUserScrollAwayIntent(clampScrollTop(node, node.scrollTop));
        }
        touchLastYRef.current = currentY;
        return;
      }
      const currentY = event.touches[0]?.clientY;
      const previousY = touchLastYRef.current;
      if (
        currentY !== undefined &&
        previousY !== undefined &&
        currentY > previousY
      ) {
        markUserScrollAwayIntent(clampScrollTop(node, node.scrollTop));
      }
      touchLastYRef.current = currentY;
    };
    const handleTouchEnd = (): void => {
      touchLastYRef.current = undefined;
    };
    node.addEventListener("wheel", handleWheel, { passive: true });
    node.addEventListener("pointerdown", handlePointerDown);
    node.addEventListener("touchstart", handleTouchStart, { passive: true });
    node.addEventListener("touchmove", handleTouchMove, { passive: true });
    node.addEventListener("touchend", handleTouchEnd);
    node.addEventListener("touchcancel", handleTouchEnd);
    node.addEventListener("keydown", handleKeyDown);
    return () => {
      node.removeEventListener("wheel", handleWheel);
      node.removeEventListener("pointerdown", handlePointerDown);
      node.removeEventListener("touchstart", handleTouchStart);
      node.removeEventListener("touchmove", handleTouchMove);
      node.removeEventListener("touchend", handleTouchEnd);
      node.removeEventListener("touchcancel", handleTouchEnd);
      node.removeEventListener("keydown", handleKeyDown);
    };
  });

  useLayoutEffect(() => {
    const node = conversationViewport();
    if (!node || typeof ResizeObserver === "undefined") {
      return undefined;
    }
    scrollContentRef.current?.style.removeProperty("transform");
    const windowResizeScroll = createWindowResizeSettleScheduler(() => {
      // The resize has settled. Commit the real scrollTop so the scrollbar
      // position, the "jump to latest" pill, and the turn-rail anchor line
      // up with the new viewport without moving text during the live drag.
      scrollConversationToBottom();
    });
    const resizeObserver = new ResizeObserver(() => {
      if (isWindowResizing()) {
        scheduleLiveResizeScroll();
        windowResizeScroll.schedule();
        return;
      }
      scrollContentRef.current?.style.removeProperty("transform");
      scrollConversationToBottom();
    });
    observeAutoFollowResizeTargets(node, resizeObserver);
    window.addEventListener("resize", scheduleLiveResizeScroll);
    return () => {
      windowResizeScroll.cancel();
      resizeObserver.disconnect();
      window.removeEventListener("resize", scheduleLiveResizeScroll);
      scrollContentRef.current?.style.removeProperty("transform");
    };
  }, [
    activePane,
    activeThreadID,
    emptyConversation,
    initialized,
    previewingLaunch,
    primaryTurns,
    secondaryTurns,
    scrollConversationToBottom,
    scheduleLiveResizeScroll,
    splitConversation
  ]);

  useLayoutEffect(() => {
    const node = dockComposerNode;
    const pane = conversationPaneRef.current;
    let windowResizeHeight: ReturnType<
      typeof createWindowResizeSettleScheduler
    > | undefined;
    const applyHeight = (nextHeight: number): void => {
      const nextValue = `${nextHeight}px`;
      if (
        dockComposerHeightRef.current === nextHeight &&
        pane?.style.getPropertyValue("--dock-composer-height") === nextValue
      ) {
        return;
      }
      const wasVisible = dockComposerHeightRef.current > 0;
      const isVisible = nextHeight > 0;
      const visibilityChanged = wasVisible !== isVisible;
      dockComposerHeightRef.current = nextHeight;
      pane?.style.setProperty("--dock-composer-height", nextValue);
      // Only re-scroll on a visibility transition (composer hidden → visible
      // or vice versa), not on every continuous resize from typing or focus
      // changes. Continuous resize firing scrollConversationToBottom used to
      // fight the user whenever they tried to scroll up.
      if (visibilityChanged && isVisible && conversationAutoFollowRef.current) {
        scrollConversationToBottom();
      }
    };

    if (!node) {
      applyHeight(0);
      return;
    }

    const updateHeight = (): void => {
      if (isWindowResizing()) {
        windowResizeHeight?.schedule();
        return;
      }
      const nextHeight = dockComposerVisualHeight(node);
      applyHeight(nextHeight);
    };

    windowResizeHeight = createWindowResizeSettleScheduler(updateHeight);
    updateHeight();
    const resizeObserver = new ResizeObserver(updateHeight);
    resizeObserver.observe(node);
    const frame = node.querySelector<HTMLElement>(".composer-frame");
    if (frame) {
      resizeObserver.observe(frame);
    }
    return () => {
      windowResizeHeight?.cancel();
      resizeObserver.disconnect();
    };
  }, [
    dockComposerNode,
    emptyConversation,
    previewingLaunch,
    initialized,
    scrollConversationToBottom
  ]);

  useEffect(() => {
    return () => {
      if (streamScrollFrameRef.current !== undefined) {
        window.cancelAnimationFrame(streamScrollFrameRef.current);
        streamScrollFrameRef.current = undefined;
      }
      if (liveResizeScrollFrameRef.current !== undefined) {
        window.cancelAnimationFrame(liveResizeScrollFrameRef.current);
        liveResizeScrollFrameRef.current = undefined;
      }
      resizeSettleStreamScrollRef.current?.cancel();
      resizeSettleStreamScrollRef.current = null;
      if (conversationScrollbarHideTimerRef.current !== undefined) {
        window.clearTimeout(conversationScrollbarHideTimerRef.current);
      }
      clearUserScrollAwayIntent();
    };
  }, [clearUserScrollAwayIntent]);

  return {
    conversationScrollRef,
    scrollContentRef,
    splitPaneRefs,
    conversationPaneRef,
    dockComposerRef,
    dockComposerNode,
    scheduleStreamScroll,
    handleConversationScroll,
    scrollConversationToBottom,
    enableConversationAutoFollow,
    disableConversationAutoFollow,
    userScrolledAway
  };
}
