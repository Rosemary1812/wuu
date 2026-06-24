import {
  type MutableRefObject,
  type RefObject,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
} from "react";

export const AUTO_FOLLOW_BOTTOM_THRESHOLD_PX = 16;
export const AUTO_FOLLOW_SCROLLBAR_HIDE_DELAY_MS = 700;
export const USER_SCROLL_AWAY_INTENT_WINDOW_MS = 300;
export const AUTO_FOLLOW_NESTED_SCROLL_ATTR = "data-wuu-nested-scroll";
export const AUTO_FOLLOW_NESTED_SCROLL_SELECTOR = `[${AUTO_FOLLOW_NESTED_SCROLL_ATTR}]`;
export const SCROLL_AWAY_KEYS = new Set(["ArrowUp", "PageUp", "Home"]);

export function maxScrollTop(node: HTMLElement): number {
  return Math.max(0, node.scrollHeight - node.clientHeight);
}

export function clampScrollTop(node: HTMLElement, top: number): number {
  return Math.max(0, Math.min(top, maxScrollTop(node)));
}

export function distanceFromBottom(node: HTMLElement): number {
  return Math.max(0, node.scrollHeight - node.scrollTop - node.clientHeight);
}

export function atLatestScrollView(
  node: HTMLElement,
  threshold = AUTO_FOLLOW_BOTTOM_THRESHOLD_PX,
): boolean {
  return (
    node.scrollHeight <= node.clientHeight ||
    distanceFromBottom(node) <= threshold
  );
}

export function eventTargetsNestedAutoFollowScroll(
  target: EventTarget | null,
  root: HTMLElement,
): boolean {
  if (!(target instanceof Element)) {
    return false;
  }
  const nested = target.closest(AUTO_FOLLOW_NESTED_SCROLL_SELECTOR);
  return Boolean(nested && nested !== root);
}

export function setAutoFollowOverflowAnchor(
  node: HTMLElement,
  autoFollow: boolean,
): void {
  node.style.overflowAnchor = autoFollow ? "none" : "auto";
}

export function observeAutoFollowResizeTargets(
  node: HTMLElement,
  observer: ResizeObserver,
): void {
  observer.observe(node);
  for (const child of Array.from(node.children)) {
    if (child instanceof HTMLElement) {
      observer.observe(child);
    }
  }
}

export function useAutoFollowScrollContainer({
  bottomThreshold = AUTO_FOLLOW_BOTTOM_THRESHOLD_PX,
  observeKey,
  open,
  openScrollDelayMs = 0,
}: {
  bottomThreshold?: number;
  observeKey?: string;
  open?: boolean;
  openScrollDelayMs?: number;
} = {}): {
  scrollRef: RefObject<HTMLDivElement | null>;
  autoFollowRef: MutableRefObject<boolean>;
  scrollToBottom: (options?: {
    force?: boolean;
    revealScrollbar?: boolean;
  }) => void;
  scheduleScrollToBottom: () => void;
  handleScrollFrame: () => void;
} {
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const autoFollowRef = useRef(true);
  const lastScrollTopRef = useRef(0);
  const programmaticScrollTopRef = useRef<number | undefined>(undefined);
  const userScrollAwayIntentRef = useRef(false);
  const userScrollAwayIntentTimerRef = useRef<number | undefined>(undefined);
  const touchLastYRef = useRef<number | undefined>(undefined);
  const rafRef = useRef<number | undefined>(undefined);
  const scrollbarHideTimerRef = useRef<number | undefined>(undefined);

  const setAutoFollow = useCallback((next: boolean): void => {
    autoFollowRef.current = next;
    const node = scrollRef.current;
    if (node) {
      setAutoFollowOverflowAnchor(node, next);
    }
  }, []);

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
    }, USER_SCROLL_AWAY_INTENT_WINDOW_MS);
  }, []);

  const showScrollbar = useCallback((node: HTMLElement): void => {
    if (node.scrollHeight <= node.clientHeight) {
      return;
    }
    node.classList.add("scrollbar-visible");
    if (scrollbarHideTimerRef.current !== undefined) {
      window.clearTimeout(scrollbarHideTimerRef.current);
    }
    scrollbarHideTimerRef.current = window.setTimeout(() => {
      scrollbarHideTimerRef.current = undefined;
      node.classList.remove("scrollbar-visible");
    }, AUTO_FOLLOW_SCROLLBAR_HIDE_DELAY_MS);
  }, []);

  const scrollToBottom = useCallback(
    (options: { force?: boolean; revealScrollbar?: boolean } = {}): void => {
      const node = scrollRef.current;
      if (!node || (!options.force && !autoFollowRef.current)) {
        return;
      }
      if (options.force) {
        setAutoFollow(true);
      }
      clearUserScrollAwayIntent();
      node.scrollTop = node.scrollHeight;
      programmaticScrollTopRef.current = node.scrollTop;
      lastScrollTopRef.current = node.scrollTop;
      if (options.revealScrollbar) {
        showScrollbar(node);
      }
    },
    [clearUserScrollAwayIntent, setAutoFollow, showScrollbar],
  );

  const scheduleScrollToBottom = useCallback((): void => {
    const node = scrollRef.current;
    if (!node || !autoFollowRef.current || rafRef.current !== undefined) {
      return;
    }
    rafRef.current = window.requestAnimationFrame(() => {
      rafRef.current = undefined;
      scrollToBottom();
    });
  }, [scrollToBottom]);

  const handleScrollFrame = useCallback((): void => {
    const node = scrollRef.current;
    if (!node) {
      return;
    }
    showScrollbar(node);

    const programmaticTop = programmaticScrollTopRef.current;
    if (programmaticTop !== undefined) {
      programmaticScrollTopRef.current = undefined;
      if (Math.abs(node.scrollTop - programmaticTop) <= 1) {
        lastScrollTopRef.current = clampScrollTop(node, node.scrollTop);
        return;
      }
    }

    const scrolledUp = node.scrollTop < lastScrollTopRef.current;
    const userScrollAwayIntent = userScrollAwayIntentRef.current;
    lastScrollTopRef.current = clampScrollTop(node, node.scrollTop);

    if (scrolledUp && userScrollAwayIntent) {
      setAutoFollow(false);
      return;
    }
    if (atLatestScrollView(node, bottomThreshold)) {
      setAutoFollow(true);
      return;
    }
    if (autoFollowRef.current && !userScrollAwayIntent) {
      scheduleScrollToBottom();
    }
  }, [
    bottomThreshold,
    scheduleScrollToBottom,
    setAutoFollow,
    showScrollbar,
  ]);

  useLayoutEffect(() => {
    const node = scrollRef.current;
    if (!node) {
      return undefined;
    }
    setAutoFollowOverflowAnchor(node, autoFollowRef.current);

    const handleScroll = (): void => {
      handleScrollFrame();
    };
    const handleWheel = (event: WheelEvent): void => {
      event.stopPropagation();
      if (event.deltaY < 0) {
        markUserScrollAwayIntent();
      }
    };
    const handlePointerDown = (event: PointerEvent): void => {
      event.stopPropagation();
      if (event.target === node) {
        markUserScrollAwayIntent();
      }
    };
    const handleKeyDown = (event: KeyboardEvent): void => {
      event.stopPropagation();
      if (SCROLL_AWAY_KEYS.has(event.key)) {
        markUserScrollAwayIntent();
      }
    };
    const handleTouchStart = (event: TouchEvent): void => {
      event.stopPropagation();
      touchLastYRef.current = event.touches[0]?.clientY;
    };
    const handleTouchMove = (event: TouchEvent): void => {
      event.stopPropagation();
      const currentY = event.touches[0]?.clientY;
      const previousY = touchLastYRef.current;
      if (
        currentY !== undefined &&
        previousY !== undefined &&
        currentY > previousY
      ) {
        markUserScrollAwayIntent();
      }
      touchLastYRef.current = currentY;
    };
    const handleTouchEnd = (event: TouchEvent): void => {
      event.stopPropagation();
      touchLastYRef.current = undefined;
    };

    node.addEventListener("scroll", handleScroll, { passive: true });
    node.addEventListener("wheel", handleWheel, { passive: true });
    node.addEventListener("pointerdown", handlePointerDown);
    node.addEventListener("touchstart", handleTouchStart, { passive: true });
    node.addEventListener("touchmove", handleTouchMove, { passive: true });
    node.addEventListener("touchend", handleTouchEnd);
    node.addEventListener("touchcancel", handleTouchEnd);
    node.addEventListener("keydown", handleKeyDown);
    return () => {
      node.removeEventListener("scroll", handleScroll);
      node.removeEventListener("wheel", handleWheel);
      node.removeEventListener("pointerdown", handlePointerDown);
      node.removeEventListener("touchstart", handleTouchStart);
      node.removeEventListener("touchmove", handleTouchMove);
      node.removeEventListener("touchend", handleTouchEnd);
      node.removeEventListener("touchcancel", handleTouchEnd);
      node.removeEventListener("keydown", handleKeyDown);
    };
  }, [handleScrollFrame, markUserScrollAwayIntent]);

  useLayoutEffect(() => {
    const node = scrollRef.current;
    if (!node || typeof ResizeObserver === "undefined") {
      return undefined;
    }
    const resizeObserver = new ResizeObserver(() => {
      scheduleScrollToBottom();
    });
    observeAutoFollowResizeTargets(node, resizeObserver);
    return () => {
      resizeObserver.disconnect();
    };
  }, [observeKey, scheduleScrollToBottom]);

  useEffect(() => {
    if (!open) {
      return undefined;
    }
    setAutoFollow(true);
    lastScrollTopRef.current = 0;
    const timer = window.setTimeout(() => {
      scrollToBottom({ force: true, revealScrollbar: true });
    }, openScrollDelayMs);
    return () => {
      window.clearTimeout(timer);
    };
  }, [open, openScrollDelayMs, scrollToBottom, setAutoFollow]);

  useEffect(() => {
    return () => {
      if (rafRef.current !== undefined) {
        window.cancelAnimationFrame(rafRef.current);
      }
      if (scrollbarHideTimerRef.current !== undefined) {
        window.clearTimeout(scrollbarHideTimerRef.current);
      }
      clearUserScrollAwayIntent();
    };
  }, [clearUserScrollAwayIntent]);

  return useMemo(
    () => ({
      scrollRef,
      autoFollowRef,
      scrollToBottom,
      scheduleScrollToBottom,
      handleScrollFrame,
    }),
    [handleScrollFrame, scheduleScrollToBottom, scrollToBottom],
  );
}
