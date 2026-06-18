import { useEffect, useRef, useState } from "react";

// Compact toolbar gauge. The numeric readout lives in a hover-only tooltip;
// the default render stays icon-like and line-based to match the composer tools.
const MAX_TOKENS_PER_SEC = 100;
const HIGH_SPEED_RATIO = 0.7;
const DISPLAY_LERP = 0.055;

const GAUGE_ARC_PATH = "M 4.5 15.5 A 7.5 7.5 0 0 1 19.5 15.5";
const GAUGE_ARC_PATH_LENGTH = 100;
const GAUGE_CENTER_X = 12;
const GAUGE_CENTER_Y = 15.5;
const NEEDLE_START_DEG = -154;
const NEEDLE_END_DEG = -26;
const NEEDLE_ANGLE = NEEDLE_END_DEG - NEEDLE_START_DEG;

function speedColor(tps: number): string {
  if (tps <= 0.05) return "var(--token-gauge-idle)";
  if (tps < 25) return "var(--token-gauge-low)";
  if (tps < HIGH_SPEED_RATIO * MAX_TOKENS_PER_SEC) return "var(--token-gauge-mid)";
  return "var(--token-gauge-high)";
}

export function ComposerTokenGauge({
  running,
  tokensPerSecond,
  source = "none",
}: {
  running: boolean;
  tokensPerSecond: number;
  source?: "real" | "estimated" | "none";
}): JSX.Element | null {
  // Displayed value tracks the target with a per-frame lerp so the needle
  // settles instead of jittering on every sliding-window update. The initial
  // value is the target itself so the gauge paints the real number on first
  // mount and tests that render synchronously can see the right value.
  const [displayed, setDisplayed] = useState(() =>
    running ? Math.max(0, tokensPerSecond) : 0,
  );
  const targetRef = useRef(0);
  targetRef.current = running ? Math.max(0, tokensPerSecond) : 0;

  useEffect(() => {
    if (!running) {
      setDisplayed(0);
      return;
    }
    let raf = 0;
    const tick = (): void => {
      setDisplayed((current) => {
        const target = targetRef.current;
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
  }, [running]);

  if (!running && displayed < 0.05) {
    return null;
  }

  const ratio = Math.max(0, Math.min(1, displayed / MAX_TOKENS_PER_SEC));
  const dashOffset = GAUGE_ARC_PATH_LENGTH * (1 - ratio);
  const color = speedColor(displayed);
  const needleDeg = NEEDLE_START_DEG + NEEDLE_ANGLE * ratio;
  const rounded = Math.round(displayed * 10) / 10;
  const tooltipColor = speedColor(displayed);
  const isEstimated = source === "estimated";
  const speedLabel = `${isEstimated ? "约 " : ""}${rounded.toFixed(1)} tok/s`;
  const ariaPrefix = isEstimated ? "估算生成速度" : "生成速度";

  return (
    <div
      className="composer-token-gauge"
      data-state={running ? "running" : "stopping"}
      role="status"
      aria-live="polite"
      aria-label={`${ariaPrefix} ${rounded.toFixed(1)} token 每秒`}
      style={{ color }}
    >
      <svg
        viewBox="0 0 24 24"
        width="18"
        height="18"
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
            d={`M ${GAUGE_CENTER_X} ${GAUGE_CENTER_Y} L 17.7 ${GAUGE_CENTER_Y}`}
          />
        </g>
        <circle
          cx={GAUGE_CENTER_X}
          cy={GAUGE_CENTER_Y}
          r="1.55"
          className="composer-token-gauge-hub"
        />
      </svg>
      <span
        className="composer-token-gauge-tooltip"
        style={{ color: tooltipColor }}
      >
        {speedLabel}
      </span>
    </div>
  );
}
