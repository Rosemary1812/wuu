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

const CONVERSATION_AUTO_SCROLL_THRESHOLD_PX = 48;
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
  const streamScrollFrameRef = useRef<number | undefined>(undefined);
  const conversationScrollbarHideTimerRef = useRef<number | undefined>(undefined);

  function conversationViewport(): HTMLElement | undefined {
    if (splitConversation) {
      return splitPaneRefs.current[activePane] ?? undefined;
    }
    return conversationScrollRef.current ?? undefined;
  }

  function isConversationNearBottom(node: HTMLElement): boolean {
    const distanceFromBottom = Math.max(
      0,
      node.scrollHeight - node.scrollTop - node.clientHeight
    );
    return distanceFromBottom <= CONVERSATION_AUTO_SCROLL_THRESHOLD_PX;
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
      conversationAutoFollowRef.current = true;
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

  function handleConversationScroll(scrolledNode?: HTMLElement): void {
    const node = scrolledNode ?? conversationViewport();
    if (!node) {
      return;
    }
    showConversationScrollbar(node);
    conversationAutoFollowRef.current = isConversationNearBottom(node);
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
      dockComposerHeightRef.current = nextHeight;
      pane?.style.setProperty("--dock-composer-height", nextValue);
      if (nextHeight > 0 && conversationAutoFollowRef.current) {
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
    enableConversationAutoFollow
  };
}
