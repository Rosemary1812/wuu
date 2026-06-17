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

// Tight threshold so the conversation only re-engages auto-follow when the
// user is effectively parked at the bottom. The previous 48px band let one
// mouse-wheel notch land inside the band and silently re-arm auto-follow,
// which made slow scroll-up get yanked back to the bottom mid-gesture.
const CONVERSATION_AUTO_SCROLL_THRESHOLD_PX = 16;
const CONVERSATION_SCROLLBAR_HIDE_DELAY_MS = 700;

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

  const scrollConversationToBottom = useCallback(
    (options: { force?: boolean } = {}): void => {
      const node = conversationViewport();
      if (!node || (!options.force && !conversationAutoFollowRef.current)) {
        return;
      }
      node.scrollTop = node.scrollHeight;
      showConversationScrollbar(node);
      // Do NOT re-arm auto-follow here. The browser fires a scroll event after
      // the programmatic assignment, which runs handleConversationScroll and
      // re-engages auto-follow when distanceFromBottom is within the band.
      // Re-arming here caused a feedback loop where any successful
      // programmatic scroll silently re-enabled auto-follow even when the
      // user had explicitly scrolled up.
    },
    [activePane, splitConversation]
  );

  const scheduleStreamScroll = useCallback((): void => {
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
  }, [setAutoFollow]);

  const disableConversationAutoFollow = useCallback((): void => {
    setAutoFollow(false);
  }, [setAutoFollow]);

  function handleConversationScroll(scrolledNode?: HTMLElement): void {
    const node = scrolledNode ?? conversationViewport();
    if (!node) {
      return;
    }
    showConversationScrollbar(node);

    // Position-driven userScrolledAway (drives the "Jump to latest" pill).
    //
    // We do not infer "scrolled away" purely from the scroll direction. If
    // the conversation fits inside the viewport (scrollHeight <= clientHeight)
    // there is nothing below to jump to, so the pill must stay hidden even if
    // a stale lastConversationScrollTopRef would otherwise claim the user
    // once scrolled up. Similarly, if the user is parked inside the bottom
    // band, they are already at the latest message — hide the pill.
    //
    // The direction-sensitive check (drop auto-follow the moment scrollTop
    // decreases) is preserved for the *programmatic* auto-follow decision,
    // so a slow wheel-up does not get silently yanked back to the bottom by
    // the next stream tick. The two are now independent: userScrolledAway is
    // about the pill; setAutoFollow is about the stream-follow behaviour.
    const distanceFromBottom = Math.max(
      0,
      node.scrollHeight - node.scrollTop - node.clientHeight
    );
    const isScrollable = node.scrollHeight > node.clientHeight;
    const scrolledUp = node.scrollTop < lastConversationScrollTopRef.current;
    lastConversationScrollTopRef.current = node.scrollTop;

    const atLatestView =
      !isScrollable ||
      distanceFromBottom <= CONVERSATION_AUTO_SCROLL_THRESHOLD_PX;
    if (atLatestView) {
      setAutoFollow(true);
    } else if (scrolledUp) {
      setAutoFollow(false);
    }
  }

  useLayoutEffect(() => {
    setAutoFollow(true);
    scrollConversationToBottom({ force: true });
  }, [activeThreadID, scrollConversationToBottom, setAutoFollow]);

  useEffect(() => {
    scheduleStreamScroll();
  }, [primaryTurns, secondaryTurns, scheduleStreamScroll]);

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
    };
  }, []);

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
