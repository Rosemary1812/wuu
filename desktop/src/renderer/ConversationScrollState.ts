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
    conversationAutoFollowRef.current = true;
  }, []);

  const disableConversationAutoFollow = useCallback((): void => {
    conversationAutoFollowRef.current = false;
  }, []);

  function handleConversationScroll(scrolledNode?: HTMLElement): void {
    const node = scrolledNode ?? conversationViewport();
    if (!node) {
      return;
    }
    showConversationScrollbar(node);

    // Direction-sensitive auto-follow: the moment the user scrolls UP, drop
    // out of auto-follow even if they are still inside the bottom band. Only
    // re-engage when they explicitly scroll DOWN and reach the bottom band
    // again. This is the pattern ChatGPT / Claude.ai / Slack use, and it
    // removes the "slow scroll-up gets silently yanked back to bottom"
    // symptom that the symmetric distance-from-bottom check produced.
    const distanceFromBottom = Math.max(
      0,
      node.scrollHeight - node.scrollTop - node.clientHeight
    );
    const scrolledUp = node.scrollTop < lastConversationScrollTopRef.current;
    lastConversationScrollTopRef.current = node.scrollTop;

    if (scrolledUp) {
      conversationAutoFollowRef.current = false;
      return;
    }
    if (distanceFromBottom <= CONVERSATION_AUTO_SCROLL_THRESHOLD_PX) {
      conversationAutoFollowRef.current = true;
    }
  }

  useLayoutEffect(() => {
    conversationAutoFollowRef.current = true;
    scrollConversationToBottom({ force: true });
  }, [activeThreadID, scrollConversationToBottom]);

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
    disableConversationAutoFollow
  };
}
