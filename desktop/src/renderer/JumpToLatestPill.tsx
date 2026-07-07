import { type RefObject, useEffect, useState } from "react";

/**
 * JumpToLatestPill — self-contained "scroll to bottom" pill.
 *
 * Issue #5 desktop fix: replaces the previous position-absolute pill that
 * was centered on the wrong containing block (centering on the
 * conversation-pane grid container rather than the visible scroll area,
 * so a thread-panel-resize or .subthread-panel-visible class shift moved
 * the pill off-center). This component:
 *
 *  - Subscribes to its own scroll listener on `containerRef.current` so
 *    `userScrolledAway` is computed against the same element that the
 *    click will smooth-scroll to bottom.
 *  - Renders the pill as a direct child of the scroll container, where
 *    `position: sticky; left: 50%; transform: translateX(-50%)` centers
 *    on the container's visible width — no CSS variables, no
 *    conversation-pane / thread-panel-width inheritance.
 *  - Subscribes to ResizeObserver on the container so a thread-panel
 *    resize re-evaluates the threshold (otherwise a shrinking container
 *    could leave the pill stuck in scrolled-away state at the new bottom).
 *  - Click handler smooth-scrolls the container to its own scrollHeight
 *    — Fix 2 uses the thread panel's ref, Fix 1 uses the main ref, each
 *    scrolls its own scope (no accidental main-scope jumps).
 *
 * The component is intentionally self-contained: it does not import
 * `useConversationScrollState`. The main pill could read
 * `userScrolledAway` from that hook, but coupling them would force Fix 2
 * (thread panel) to either replicate the same logic or depend on a hook
 * the thread panel can't use. Self-contained is the cleaner contract.
 */
type JumpToLatestPillProps = {
  /**
   * Ref to the scroll container this pill lives inside and smooth-scrolls
   * to bottom on click. Must be set (typically the same ref that the
   * container's outer `<div className="scroll-region">` uses). The pill
   * renders as a descendant of this element so `position: sticky;
   * left: 50%; transform: translateX(-50%)` resolves against it.
   */
  containerRef: RefObject<HTMLElement | null>;
  /**
   * Distance (in px) from the bottom of the scroll container below
   * which the pill is considered "scrolled away" and shown. Default 80
   * matches the existing auto-follow threshold.
   */
  threshold?: number;
  /**
   * Accessible label for the pill button. Default "跳到最新" matches
   * the previous inline pill.
   */
  label?: string;
};

const DEFAULT_THRESHOLD_PX = 80;

export function JumpToLatestPill({
  containerRef,
  threshold = DEFAULT_THRESHOLD_PX,
  label = "跳到最新",
}: JumpToLatestPillProps): React.ReactElement | null {
  const [scrolledAway, setScrolledAway] = useState(false);

  useEffect(() => {
    const node = containerRef.current;
    if (!node) {
      return undefined;
    }

    const update = (): void => {
      const distanceFromBottom =
        node.scrollHeight - node.scrollTop - node.clientHeight;
      setScrolledAway(distanceFromBottom > threshold);
    };

    // Initial sync after mount (covers the case where the container is
    // already scrolled up at the time the pill mounts, e.g., when the
    // user navigated away and then re-entered the conversation).
    update();
    node.addEventListener("scroll", update, { passive: true });

    // Re-evaluate on container resize. A thread-panel drag mutates the
    // scroll container's `clientHeight`; without this listener the
    // pill would stay hidden or shown based on the pre-resize distance.
    // Guarded: test environments (jsdom) have no ResizeObserver, and the
    // pill degrades gracefully to scroll-event-only updates without it.
    const resizeObserver =
      typeof ResizeObserver !== "undefined"
        ? new ResizeObserver(update)
        : undefined;
    resizeObserver?.observe(node);

    return () => {
      node.removeEventListener("scroll", update);
      resizeObserver?.disconnect();
    };
  }, [containerRef, threshold]);

  if (!scrolledAway) {
    return null;
  }

  return (
    <button
      type="button"
      className="jump-to-latest-pill jump-to-latest-pill-sticky-centered"
      aria-label={label}
      onClick={() => {
        const node = containerRef.current;
        if (!node) {
          return;
        }
        node.scrollTo({ top: node.scrollHeight, behavior: "smooth" });
      }}
    >
      <svg
        width="14"
        height="14"
        viewBox="0 0 14 14"
        fill="none"
        aria-hidden="true"
      >
        <path
          d="M7 1V11M7 11L3 7M7 11L11 7"
          stroke="currentColor"
          strokeWidth="1.5"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
      <span>{label}</span>
    </button>
  );
}
