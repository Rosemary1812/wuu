import {
  type RefObject,
  useCallback,
  useEffect,
  useState,
} from "react";
import { createPortal } from "react-dom";
import { observeAutoFollowResizeTargets } from "./AutoFollowScroll";
import {
  createWindowResizeSettleScheduler,
  isWindowResizing,
} from "./WindowResizeState";
import { useI18n } from "./i18n";

/**
 * JumpToLatestPill — self-contained "scroll to bottom" pill.
 *
 * Issue #5 desktop fix: replaces the previous position-absolute pill that
 * was centered on the wrong containing block. This component subscribes to
 * its own scroll listener on `containerRef.current` so `scrolledAway` is
 * computed against the same element the click smooth-scrolls to bottom, and
 * subscribes to a ResizeObserver so a thread-panel resize re-evaluates the
 * threshold.
 *
 * Two positioning modes:
 *
 *  - Default (no `bottomAnchorRef`): the pill renders as a direct child of
 *    the scroll container with `position: sticky; left: 50%` — centered on
 *    the container's visible width. Used by the reply-subthread panel, where
 *    the scroll body's bottom edge sits exactly at the panel composer's top
 *    (they are flex siblings), so sticky-bottom lands the pill correctly.
 *
 *  - Anchored (`bottomAnchorRef` given): the pill is PORTALED to
 *    document.body and positioned `fixed`, measured to sit just above the
 *    anchor element (the dock composer). The main conversation's scroll
 *    container (`.scroll-region`) does NOT line up with the floating dock
 *    composer in the chat/group view — the sticky-bottom variant pinned the
 *    pill to the scroll container's own bottom, which floated mid-screen.
 *    Anchoring to the composer element itself is correct by construction,
 *    independent of the scroll container's height, `--dock-composer-height`,
 *    or the nested chat-thread overflow. Portaling to body escapes the
 *    scroll region's `contain: layout paint`, which would otherwise trap a
 *    `position: fixed` child.
 */
type JumpToLatestPillProps = {
  /**
   * Ref to the scroll container this pill watches and smooth-scrolls to
   * bottom on click.
   */
  containerRef: RefObject<HTMLElement | null>;
  /**
   * Optional element the pill should float just above (the dock composer).
   * When this prop is PRESENT the pill switches to the portaled/measured
   * "anchored" mode (even while the element is transiently null before mount);
   * when the prop is omitted entirely it stays an in-container sticky pill.
   */
  bottomAnchor?: HTMLElement | null;
  /**
   * Distance (in px) from the bottom of the scroll container below which the
   * pill is considered "scrolled away" and shown. Default 80 matches the
   * existing auto-follow threshold.
   */
  threshold?: number;
  /**
   * Accessible label for the pill button. Defaults to the localized label.
   */
  label?: string;
  /**
   * Fires whenever the pill's "scrolled away from bottom" boolean flips.
   * Used by callers that need to coordinate other composer-adjacent chrome
   * (e.g. swap a sibling progress pill out of the same slot while the user
   * is mid-conversation). Pass a stable callback (e.g. a state setter) —
   * the effect re-runs only when the boolean actually changes.
   */
  onScrolledAwayChange?: (scrolledAway: boolean) => void;
};

const DEFAULT_THRESHOLD_PX = 80;
const PILL_BOTTOM_GAP_PX = 12;
const COMPOSER_FRAME_SELECTOR = ".composer-frame";

type PillPosition = { left: number; bottom: number };

export function JumpToLatestPill({
  containerRef,
  bottomAnchor,
  threshold = DEFAULT_THRESHOLD_PX,
  label,
  onScrolledAwayChange,
}: JumpToLatestPillProps): React.ReactElement | null {
  const { t } = useI18n();
  const accessibleLabel = label ?? t("conversation.jumpToLatest");
  const [scrolledAway, setScrolledAway] = useState(false);
  const [position, setPosition] = useState<PillPosition | null>(null);
  // The prop being PRESENT (even as null) selects anchored/portaled mode. The
  // subthread panel omits it and keeps the in-container sticky pill.
  const anchored = bottomAnchor !== undefined;

  // Mirror the scrolled-away boolean to the parent whenever it flips. The
  // parent uses this to swap a sibling progress pill out of the same
  // composer-adjacent slot — see App.tsx. Effect-based so we only fire on
  // real transitions, not on every scroll/resize tick.
  useEffect(() => {
    onScrolledAwayChange?.(scrolledAway);
  }, [scrolledAway, onScrolledAwayChange]);

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
    const resizeSettleUpdate = createWindowResizeSettleScheduler(update);
    const scheduleUpdate = (): void => {
      if (isWindowResizing()) {
        resizeSettleUpdate.schedule();
        return;
      }
      update();
    };

    // Initial sync after mount (covers the case where the container is
    // already scrolled up at the time the pill mounts, e.g., when the
    // user navigated away and then re-entered the conversation).
    scheduleUpdate();
    node.addEventListener("scroll", scheduleUpdate, { passive: true });

    // Re-evaluate on container and content resize. A thread-panel drag mutates
    // the container's `clientHeight`, while expanding or collapsing a fold
    // mutates its child's height and therefore the container's `scrollHeight`.
    // Observing both prevents a transient fold layout from leaving the pill
    // stuck in its pre-settle state.
    // Guarded: test environments (jsdom) have no ResizeObserver, and the
    // pill degrades gracefully to scroll-event-only updates without it.
    const resizeObserver =
      typeof ResizeObserver !== "undefined"
        ? new ResizeObserver(scheduleUpdate)
        : undefined;
    if (resizeObserver) {
      observeAutoFollowResizeTargets(node, resizeObserver);
    }
    const childObserver =
      typeof MutationObserver !== "undefined"
        ? new MutationObserver(() => {
            if (resizeObserver) {
              observeAutoFollowResizeTargets(node, resizeObserver);
            }
            scheduleUpdate();
          })
        : undefined;
    childObserver?.observe(node, { childList: true });

    return () => {
      resizeSettleUpdate.cancel();
      node.removeEventListener("scroll", scheduleUpdate);
      childObserver?.disconnect();
      resizeObserver?.disconnect();
    };
  }, [containerRef, threshold]);

  const recomputePosition = useCallback((): void => {
    const container = containerRef.current;
    if (!container || !bottomAnchor) {
      return;
    }
    const containerRect = container.getBoundingClientRect();
    const visualAnchor =
      bottomAnchor.querySelector<HTMLElement>(COMPOSER_FRAME_SELECTOR) ?? bottomAnchor;
    const anchorRect = visualAnchor.getBoundingClientRect();
    setPosition({
      // Centered on the scroll container's visible width (issue #5 intent).
      left: containerRect.left + containerRect.width / 2,
      // Sit PILL_BOTTOM_GAP_PX above the composer's top edge, expressed as a
      // CSS `bottom` (distance from the viewport bottom).
      bottom: Math.max(
        PILL_BOTTOM_GAP_PX,
        window.innerHeight - anchorRect.top + PILL_BOTTOM_GAP_PX,
      ),
    });
  }, [containerRef, bottomAnchor]);

  // Measured positioning for the anchored (portaled) variant. Recomputes on
  // scroll, window resize, and container/composer resize (typing grows the
  // composer, moving its top edge). The composer frame is observed separately
  // because expanded mode moves it upward without growing the outer anchor.
  useEffect(() => {
    if (!anchored || !scrolledAway || !bottomAnchor) {
      return undefined;
    }
    const resizeSettleRecompute =
      createWindowResizeSettleScheduler(recomputePosition);
    const recomputeWhenStable = (): void => {
      if (isWindowResizing()) {
        resizeSettleRecompute.schedule();
        return;
      }
      recomputePosition();
    };
    recomputeWhenStable();
    let frame = 0;
    const schedule = (): void => {
      if (isWindowResizing()) {
        resizeSettleRecompute.schedule();
        return;
      }
      if (frame) {
        return;
      }
      frame = window.requestAnimationFrame(() => {
        frame = 0;
        recomputeWhenStable();
      });
    };
    const container = containerRef.current;
    container?.addEventListener("scroll", schedule, { passive: true });
    window.addEventListener("resize", schedule);
    const resizeObserver =
      typeof ResizeObserver !== "undefined"
        ? new ResizeObserver(schedule)
        : undefined;
    if (container) {
      resizeObserver?.observe(container);
    }
    const observeAnchorTargets = (): void => {
      resizeObserver?.observe(bottomAnchor);
      const visualAnchor =
        bottomAnchor.querySelector<HTMLElement>(COMPOSER_FRAME_SELECTOR);
      if (visualAnchor) {
        resizeObserver?.observe(visualAnchor);
      }
    };
    observeAnchorTargets();
    const anchorChildObserver =
      typeof MutationObserver !== "undefined"
        ? new MutationObserver(() => {
            observeAnchorTargets();
            schedule();
          })
        : undefined;
    anchorChildObserver?.observe(bottomAnchor, { childList: true, subtree: true });
    return () => {
      resizeSettleRecompute.cancel();
      if (frame) {
        window.cancelAnimationFrame(frame);
      }
      container?.removeEventListener("scroll", schedule);
      window.removeEventListener("resize", schedule);
      anchorChildObserver?.disconnect();
      resizeObserver?.disconnect();
    };
  }, [anchored, bottomAnchor, scrolledAway, recomputePosition, containerRef]);

  if (!scrolledAway) {
    return null;
  }

  const scrollToBottom = (): void => {
    const node = containerRef.current;
    if (!node) {
      return;
    }
    node.scrollTo({ top: node.scrollHeight, behavior: "smooth" });
  };

  const pillBody = (
    <>
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
    </>
  );

  if (anchored) {
    if (!bottomAnchor || !position) {
      return null;
    }
    return createPortal(
      <button
        type="button"
        className="jump-to-latest-pill jump-to-latest-pill-anchored"
        aria-label={label}
        style={{
          left: `${position.left}px`,
          bottom: `${position.bottom}px`,
        }}
        onClick={scrollToBottom}
      >
        {pillBody}
      </button>,
      document.body,
    );
  }

  return (
    <button
      type="button"
      className="jump-to-latest-pill jump-to-latest-pill-sticky-centered"
      aria-label={accessibleLabel}
      onClick={scrollToBottom}
    >
      {pillBody}
    </button>
  );
}
