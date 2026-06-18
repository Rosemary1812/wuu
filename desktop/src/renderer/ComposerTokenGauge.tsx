import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";

// Compact toolbar gauge. The numeric readout is portaled out of the 32x32
// cell and floated above it on hover, so the text never overlaps the gauge.
const MAX_TOKENS_PER_SEC = 100;
const HIGH_SPEED_THRESHOLD = 70;
const DISPLAY_LERP = 0.055;
const STALE_HOLD_MS = 1200;
const STALE_DECAY_MS = 5200;
const TOOLTIP_OFFSET_PX = 8;

const GAUGE_ARC_PATH = "M 2.5 17 A 9.5 9.5 0 0 1 21.5 17";
const GAUGE_ARC_PATH_LENGTH = 100;
const GAUGE_CENTER_X = 12;
const GAUGE_CENTER_Y = 17;
const NEEDLE_START_DEG = -154;
const NEEDLE_END_DEG = -26;
const NEEDLE_ANGLE = NEEDLE_END_DEG - NEEDLE_START_DEG;

// 3 meaningful states. The previous 4-band scheme had an "idle" and "low" pair
// in the same gray, which carried no information — collapsed to a single
// inactive state and tightened the high band to a clearly red color.
function speedColor(tps: number): string {
  if (tps <= 0.05) return "var(--token-gauge-idle)";
  if (tps < HIGH_SPEED_THRESHOLD) return "var(--token-gauge-mid)";
  return "var(--token-gauge-high)";
}

export function ComposerTokenGauge({
  running,
  tokensPerSecond,
  sampledAt,
  source = "none",
}: {
  running: boolean;
  tokensPerSecond: number;
  sampledAt?: number;
  source?: "real" | "estimated" | "none";
}): JSX.Element {
  // Displayed value tracks the target with a per-frame lerp so the needle
  // settles instead of jittering on every sliding-window update. The initial
  // value is the target itself so the gauge paints the real number on first
  // mount and tests that render synchronously can see the right value.
  const [displayed, setDisplayed] = useState(() =>
    running ? Math.max(0, tokensPerSecond) : 0,
  );
  const [hovered, setHovered] = useState(false);
  const [tooltipPos, setTooltipPos] = useState<
    { left: number; top: number } | null
  >(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const runningRef = useRef(running);
  const targetRef = useRef(0);
  const sampledAtRef = useRef<number | undefined>(sampledAt);
  runningRef.current = running;
  targetRef.current = running ? Math.max(0, tokensPerSecond) : 0;
  sampledAtRef.current = sampledAt;

  useEffect(() => {
    let raf = 0;
    const tick = (): void => {
      setDisplayed((current) => {
        let target = runningRef.current ? targetRef.current : 0;
        const sampleAge = sampledAtRef.current
          ? Date.now() - sampledAtRef.current
          : 0;
        if (target > 0 && sampleAge > STALE_HOLD_MS) {
          const decay = Math.max(
            0,
            1 - (sampleAge - STALE_HOLD_MS) / STALE_DECAY_MS,
          );
          target *= decay;
        }
        const diff = target - current;
        if (Math.abs(diff) < 0.05) {
          return target;
        }
        return current + diff * DISPLAY_LERP;
      });
      raf = window.requestAnimationFrame(tick);
    };
    raf = window.requestAnimationFrame(tick);
    return () => {
      window.cancelAnimationFrame(raf);
    };
  }, []);

  // Re-anchor the portaled tooltip while it's visible so it stays over the
  // gauge if the composer resizes or the page scrolls.
  useEffect(() => {
    if (!hovered) {
      setTooltipPos(null);
      return;
    }
    const update = (): void => {
      const el = containerRef.current;
      if (!el) return;
      const rect = el.getBoundingClientRect();
      setTooltipPos({
        left: rect.left + rect.width / 2,
        top: rect.top,
      });
    };
    update();
    window.addEventListener("scroll", update, true);
    window.addEventListener("resize", update);
    return () => {
      window.removeEventListener("scroll", update, true);
      window.removeEventListener("resize", update);
    };
  }, [hovered]);

  const ratio = Math.max(0, Math.min(1, displayed / MAX_TOKENS_PER_SEC));
  const dashOffset = GAUGE_ARC_PATH_LENGTH * (1 - ratio);
  const color = speedColor(displayed);
  const needleDeg = NEEDLE_START_DEG + NEEDLE_ANGLE * ratio;
  const rounded = Math.round(displayed * 10) / 10;
  const isEstimated = source === "estimated";
  const speedLabel = `${isEstimated ? "约 " : ""}${rounded.toFixed(1)} tok/s`;
  const ariaPrefix = isEstimated ? "估算生成速度" : "生成速度";

  const tooltipNode =
    hovered && tooltipPos ? (
      <span
        className="composer-token-gauge-tooltip"
        style={{
          position: "fixed",
          left: tooltipPos.left,
          top: tooltipPos.top,
          transform: `translate(-50%, calc(-100% - ${TOOLTIP_OFFSET_PX}px))`,
          color,
        }}
      >
        {speedLabel}
      </span>
    ) : null;

  return (
    <>
      <div
        ref={containerRef}
        className="composer-token-gauge"
        data-state={running ? "running" : "idle"}
        role="status"
        aria-live="polite"
        aria-label={`${ariaPrefix} ${rounded.toFixed(1)} token 每秒`}
        style={{ color }}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
      >
        <svg
          viewBox="0 0 24 24"
          width="20"
          height="20"
          className="composer-token-gauge-svg"
          aria-hidden="true"
        >
          <path
            d={GAUGE_ARC_PATH}
            className="composer-token-gauge-track"
            pathLength={GAUGE_ARC_PATH_LENGTH}
          />
          <path
            d={GAUGE_ARC_PATH}
            className="composer-token-gauge-progress"
            pathLength={GAUGE_ARC_PATH_LENGTH}
            style={{
              strokeDasharray: GAUGE_ARC_PATH_LENGTH,
              strokeDashoffset: dashOffset,
            }}
          />
          <g
            className="composer-token-gauge-needle"
            style={{
              transform: `rotate(${needleDeg}deg)`,
              transformOrigin: `${GAUGE_CENTER_X}px ${GAUGE_CENTER_Y}px`,
            }}
          >
            <path
              className="composer-token-gauge-needle-shape"
              d={`M ${GAUGE_CENTER_X} ${GAUGE_CENTER_Y} L 19.4 ${GAUGE_CENTER_Y}`}
            />
          </g>
          <circle
            cx={GAUGE_CENTER_X}
            cy={GAUGE_CENTER_Y}
            r="1.7"
            className="composer-token-gauge-hub"
          />
        </svg>
      </div>
      {createPortal(tooltipNode, document.body)}
    </>
  );
}
