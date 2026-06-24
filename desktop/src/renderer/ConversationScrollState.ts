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
  observeAutoFollowResizeTargets,
  setAutoFollowOverflowAnchor,
} from "./AutoFollowScroll";

// Tight threshold so the conversation only re-engages auto-follow when the
// user is effectively parked at the bottom. The previous 48px band let one
// mouse-wheel notch land inside the band and silently re-arm auto-follow,
// which made slow scroll-up get yanked back to the bottom mid-gesture.
const CONVERSATION_AUTO_SCROLL_THRESHOLD_PX = AUTO_FOLLOW_BOTTOM_THRESHOLD_PX;
const CONVERSATION_SCROLLBAR_HIDE_DELAY_MS = AUTO_FOLLOW_SCROLLBAR_HIDE_DELAY_MS;
const CONVERSATION_USER_SCROLL_AWAY_INTENT_WINDOW_MS =
  USER_SCROLL_AWAY_INTENT_WINDOW_MS;

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
  showingWorkspaceMode,
  initialized
}: {
  activeThreadID?: string;
  activePane: ConversationPaneID;
  splitConversation: boolean;
  primaryTurns?: Turn[];
  secondaryTurns?: Turn[];
  emptyConversation: boolean;
  previewingLaunch: boolean;
  showingWorkspaceMode: boolean;
  initialized: boolean;
}): {
  conversationScrollRef: RefObject<HTMLDivElement | null>;
  splitPaneRefs: MutableRefObject<Record<ConversationPaneID, HTMLElement | null>>;
  conversationPaneRef: RefObject<HTMLElement | null>;
  dockComposerRef: (node: HTMLElement | null) => void;
  scheduleStreamScroll: () => void;
  handleConversationScroll: (scrolledNode?: HTMLElement) => void;
  scrollConversationToBottom: (options?: { force?: boolean }) => void;
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
  const userScrollAwayIntentRef = useRef(false);
  const userScrollAwayIntentTimerRef = useRef<number | undefined>(undefined);
  const touchLastYRef = useRef<number | undefined>(undefined);
  const threadScrollSnapshotsRef = useRef(
    new Map<string, ConversationScrollSnapshot>()
  );
  const streamScrollFrameRef = useRef<number | undefined>(undefined);
  const conversationScrollbarHideTimerRef = useRef<number | undefined>(undefined);

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
    touchLastYRef.current = undefined;
    if (userScrollAwayIntentTimerRef.current !== undefined) {
      window.clearTimeout(userScrollAwayIntentTimerRef.current);
      userScrollAwayIntentTimerRef.current = undefined;
    }
  }, []);

  const markUserScrollAwayIntent = useCallback((): void => {
    userScrollAwayIntentRef.current = true;
    if (userScrollAwayIntentTimerRef.current !== undefined) {
      window.clearTimeout(userScrollAwayIntentTimerRef.current);
    }
    userScrollAwayIntentTimerRef.current = window.setTimeout(() => {
      userScrollAwayIntentRef.current = false;
      userScrollAwayIntentTimerRef.current = undefined;
    }, CONVERSATION_USER_SCROLL_AWAY_INTENT_WINDOW_MS);
  }, []);

  function applyProgrammaticScroll(
    node: HTMLElement,
    top: number,
    autoFollow: boolean,
    options: { revealScrollbar?: boolean } = {}
  ): void {
    clearUserScrollAwayIntent();
    node.scrollTop = top;
    const actualTop = clampScrollTop(node, node.scrollTop);
    if (Math.abs(node.scrollTop - actualTop) > 1) {
      node.scrollTop = actualTop;
    }
    programmaticScrollTopRef.current = actualTop;
    lastConversationScrollTopRef.current = actualTop;
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
    (options: { force?: boolean } = {}): void => {
      const node = conversationViewport();
      if (!node || (!options.force && !conversationAutoFollowRef.current)) {
        return;
      }
      applyProgrammaticScroll(node, node.scrollHeight, true, {
        revealScrollbar: true
      });
    },
    [activePane, activeThreadID, setAutoFollow, splitConversation]
  );

  const scheduleStreamScroll = useCallback((): void => {
    if (!activeThreadID) {
      return;
    }
    if (!conversationAutoFollowRef.current) {
      return;
    }
    if (streamScrollFrameRef.current !== undefined) {
      return;
    }
    streamScrollFrameRef.current = window.requestAnimationFrame(() => {
      streamScrollFrameRef.current = undefined;
      scrollConversationToBottom();
    });
  }, [scrollConversationToBottom]);

  const enableConversationAutoFollow = useCallback((): void => {
    setAutoFollow(true);
    const node = conversationViewport();
    if (node) {
      setAutoFollowOverflowAnchor(node, true);
      rememberActiveThreadScrollSnapshot(node, true);
    }
  }, [activePane, activeThreadID, setAutoFollow, splitConversation]);

  const disableConversationAutoFollow = useCallback((): void => {
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
    showConversationScrollbar(node);

    const programmaticTop = programmaticScrollTopRef.current;
    if (programmaticTop !== undefined) {
      programmaticScrollTopRef.current = undefined;
      if (Math.abs(node.scrollTop - programmaticTop) <= 1) {
        lastConversationScrollTopRef.current = clampScrollTop(
          node,
          node.scrollTop
        );
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
    // layout-driven scrollTop clamps (for example a completed process fold
    // shrinking above the viewport) can also move scrollTop upward while
    // the viewport is still at the latest content. Those must keep
    // auto-follow armed; otherwise the next streaming or settle frame will
    // stop sticking to the bottom even though the user never scrolled away.
    const scrolledUp = node.scrollTop < lastConversationScrollTopRef.current;
    const userScrollAwayIntent = userScrollAwayIntentRef.current;
    lastConversationScrollTopRef.current = clampScrollTop(node, node.scrollTop);

    const atLatestView = atLatestScrollView(
      node,
      CONVERSATION_AUTO_SCROLL_THRESHOLD_PX
    );
    let nextAutoFollow = conversationAutoFollowRef.current;
    if (scrolledUp && userScrollAwayIntent) {
      nextAutoFollow = false;
      setAutoFollow(false);
      setAutoFollowOverflowAnchor(node, false);
    } else if (atLatestView) {
      nextAutoFollow = true;
      setAutoFollow(true);
      setAutoFollowOverflowAnchor(node, true);
    } else if (conversationAutoFollowRef.current && !userScrollAwayIntent) {
      nextAutoFollow = true;
      applyProgrammaticScroll(node, node.scrollHeight, true, {
        revealScrollbar: true
      });
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
    scrollConversationToBottom();
  }, [activeThreadID, primaryTurns, secondaryTurns, scrollConversationToBottom]);

  useLayoutEffect(() => {
    const node = conversationViewport();
    if (!node) {
      return undefined;
    }
    const handleWheel = (event: WheelEvent): void => {
      if (eventTargetsNestedAutoFollowScroll(event.target, node)) {
        return;
      }
      if (event.deltaY < 0) {
        markUserScrollAwayIntent();
      }
    };
    const handlePointerDown = (event: PointerEvent): void => {
      if (eventTargetsNestedAutoFollowScroll(event.target, node)) {
        return;
      }
      if (event.target === node) {
        markUserScrollAwayIntent();
      }
    };
    const handleKeyDown = (event: KeyboardEvent): void => {
      if (eventTargetsNestedAutoFollowScroll(event.target, node)) {
        return;
      }
      if (SCROLL_AWAY_KEYS.has(event.key)) {
        markUserScrollAwayIntent();
      }
    };
    const handleTouchStart = (event: TouchEvent): void => {
      if (eventTargetsNestedAutoFollowScroll(event.target, node)) {
        touchLastYRef.current = undefined;
        return;
      }
      touchLastYRef.current = event.touches[0]?.clientY;
    };
    const handleTouchMove = (event: TouchEvent): void => {
      if (eventTargetsNestedAutoFollowScroll(event.target, node)) {
        touchLastYRef.current = undefined;
        return;
      }
      const currentY = event.touches[0]?.clientY;
      const previousY = touchLastYRef.current;
      if (currentY !== undefined && previousY !== undefined && currentY > previousY) {
        markUserScrollAwayIntent();
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
  }, [
    activePane,
    activeThreadID,
    markUserScrollAwayIntent,
    splitConversation
  ]);

  useLayoutEffect(() => {
    const node = conversationViewport();
    if (!node || typeof ResizeObserver === "undefined") {
      return undefined;
    }
    const resizeObserver = new ResizeObserver(() => {
      scrollConversationToBottom();
    });
    observeAutoFollowResizeTargets(node, resizeObserver);
    return () => {
      resizeObserver.disconnect();
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
    showingWorkspaceMode,
    splitConversation
  ]);

  useLayoutEffect(() => {
    const node = dockComposerNode;
    const pane = conversationPaneRef.current;
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
      const nextHeight = Math.ceil(node.getBoundingClientRect().height);
      applyHeight(nextHeight);
    };

    updateHeight();
    const resizeObserver = new ResizeObserver(updateHeight);
    resizeObserver.observe(node);
    return () => resizeObserver.disconnect();
  }, [
    dockComposerNode,
    emptyConversation,
    previewingLaunch,
    showingWorkspaceMode,
    initialized,
    scrollConversationToBottom
  ]);

  useEffect(() => {
    return () => {
      if (streamScrollFrameRef.current !== undefined) {
        window.cancelAnimationFrame(streamScrollFrameRef.current);
        streamScrollFrameRef.current = undefined;
      }
      if (conversationScrollbarHideTimerRef.current !== undefined) {
        window.clearTimeout(conversationScrollbarHideTimerRef.current);
      }
      clearUserScrollAwayIntent();
    };
  }, [clearUserScrollAwayIntent]);

  return {
    conversationScrollRef,
    splitPaneRefs,
    conversationPaneRef,
    dockComposerRef,
    scheduleStreamScroll,
    handleConversationScroll,
    scrollConversationToBottom,
    enableConversationAutoFollow,
    disableConversationAutoFollow,
    userScrolledAway
  };
}
