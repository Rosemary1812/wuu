import { useEffect, useLayoutEffect, useRef, useState } from "react";

type LightweightStreamingTextProps = {
  /**
   * The committed target text from the back-end. Increases trigger a
   * reveal from the current visible position forward (never reset),
   * so the user sees one continuous typing animation as the snapshot
   * grows. A shorter value snaps the visible text down to the new
   * length immediately.
   */
  text: string;
  /**
   * When false, the component snaps to the full target text with no
   * animation. Use this for fold collapse or item settlement.
   */
  live: boolean;
  className?: string;
};

const PREVIEW_CONFIG = {
  /** Text at or below this length snaps to full instantly. */
  shortTextMax: 4,
  /** Hard ceiling on the reveal duration so users are never kept waiting. */
  maxDurationMs: 500,
  /** Floor for the reveal duration so very short deltas still feel intentional. */
  minDurationMs: 150
} as const;

/**
 * Compute the reveal duration for a given delta (number of characters
 * the visible text needs to advance). Short deltas get the floor so the
 * animation reads as a deliberate keystroke; long deltas get the
 * ceiling so the user is never kept waiting. The result stays well below
 * the body streaming rate so the fold header remains secondary.
 */
function computeRevealDuration(delta: number): number {
  if (delta <= 0) return 0;
  // Linear scaling: ~20 ms per character plus a 120 ms base.
  const linear = 120 + delta * 20;
  return Math.max(
    PREVIEW_CONFIG.minDurationMs,
    Math.min(PREVIEW_CONFIG.maxDurationMs, linear)
  );
}

/**
 * Lightweight streaming reveal for short, committed-snapshot strings.
 *
 * Used by the process fold header preview where the back-end delivers
 * a committed snapshot of the latest commentary or activity text
 * rather than a live stream. Reusing the streaming metaphor here keeps
 * the fold header from snapping the new text into view all at once.
 *
 * Distinct from StreamingMarkdown in two ways:
 *   - No markdown parsing, no cursor, no block-level memo. The preview
 *     surface stays lightweight on purpose; the body's StreamingMarkdown
 *     is the single live markdown surface.
 *   - The visible position is decoupled from the target on purpose:
 *     when `text` grows, the reveal continues from the current position
 *     toward the new length instead of restarting, so the user never
 *     sees the text snap backwards during fast back-end updates.
 */
export function LightweightStreamingText({
  text,
  live,
  className
}: LightweightStreamingTextProps): JSX.Element {
  const [visibleLength, setVisibleLength] = useState(0);
  const visibleRef = useRef(0);
  const rafRef = useRef<number | undefined>(undefined);

  // The RAF loop must always see the latest target. Holding the value
  // in a ref avoids re-running the effect just to update a closure.
  const textRef = useRef(text);
  useLayoutEffect(() => {
    textRef.current = text;
  }, [text]);

  const syncImmediate = (length: number): void => {
    if (rafRef.current !== undefined) {
      window.cancelAnimationFrame(rafRef.current);
      rafRef.current = undefined;
    }
    visibleRef.current = length;
    setVisibleLength(length);
  };

  useEffect(() => {
    // Settled or trivially short: snap. The short-text threshold keeps
    // animations from competing with the live dot for attention on
    // tiny previews like "OK" or "Done".
    if (!live || text.length <= PREVIEW_CONFIG.shortTextMax) {
      syncImmediate(text.length);
      return undefined;
    }

    const target = text.length;
    const startLength = visibleRef.current;

    // Back-end shrank the snapshot (e.g. `*/replace`): snap to the new
    // length so the visible text never lies about the source.
    if (startLength > target) {
      syncImmediate(target);
      return undefined;
    }

    // Already caught up: nothing to do until the back-end pushes again.
    if (startLength === target) {
      return undefined;
    }

    // Continue the reveal from the current visible position toward the
    // new target. This is the "never reset" guarantee: when text grows
    // mid-animation, we keep advancing instead of jumping backwards.
    const delta = target - startLength;
    const targetDuration = computeRevealDuration(delta);

    if (rafRef.current !== undefined) {
      window.cancelAnimationFrame(rafRef.current);
    }
    let startTs: number | undefined;

    const tick = (ts: number): void => {
      const targetNow = textRef.current.length;
      // Defensive: a mid-tick `*/replace` that shrinks the target below
      // our current visible position — leave the snap to the next effect
      // run; the loop just exits here.
      if (visibleRef.current >= targetNow) {
        rafRef.current = undefined;
        return;
      }
      if (startTs === undefined) {
        startTs = ts;
      }
      const elapsed = ts - startTs;
      const ratio = Math.min(1, elapsed / targetDuration);
      // Ease-out so the last characters land softly instead of slamming.
      const eased = 1 - Math.pow(1 - ratio, 2);
      const next = Math.min(
        targetNow,
        Math.round(startLength + delta * eased)
      );
      visibleRef.current = next;
      setVisibleLength(next);
      if (next >= targetNow) {
        rafRef.current = undefined;
        return;
      }
      rafRef.current = window.requestAnimationFrame(tick);
    };
    rafRef.current = window.requestAnimationFrame(tick);

    return () => {
      if (rafRef.current !== undefined) {
        window.cancelAnimationFrame(rafRef.current);
        rafRef.current = undefined;
      }
    };
  }, [text, live]);

  return <span className={className}>{text.slice(0, visibleLength)}</span>;
}