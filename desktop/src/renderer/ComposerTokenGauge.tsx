import { useEffect, useRef, useState } from "react";

// Compact toolbar gauge. The numeric readout lives in a hover-only tooltip;
// the default render stays icon-like: a speedmark arc and sweeping needle.
const MAX_TOKENS_PER_SEC = 100;
const HIGH_SPEED_RATIO = 0.7;

const NEEDLE_LENGTH = 26;
const PIVOT_RADIUS = 5.2;
const PIVOT_INNER_RADIUS = 2.4;

const GAUGE_CENTER_X = 30;
const GAUGE_CENTER_Y = 31;
const OUTER_ARC_RADIUS = 25;
const OUTER_ARC_STROKE = 6.8;

const ARC_START_DEG = 190;
const ARC_END_DEG = 330;
const ARC_ANGLE = ARC_END_DEG - ARC_START_DEG;
const ARC_PATH_LENGTH = (Math.PI * OUTER_ARC_RADIUS * ARC_ANGLE) / 180;

function polarPoint(cx: number, cy: number, r: number, deg: number): { x: number; y: number } {
  const rad = (deg * Math.PI) / 180;
  return { x: cx + r * Math.cos(rad), y: cy + r * Math.sin(rad) };
}

function arcPath(radius: number, startDeg = ARC_START_DEG, endDeg = ARC_END_DEG): string {
  const start = polarPoint(GAUGE_CENTER_X, GAUGE_CENTER_Y, radius, startDeg);
  const end = polarPoint(GAUGE_CENTER_X, GAUGE_CENTER_Y, radius, endDeg);
  return `M ${start.x} ${start.y} A ${radius} ${radius} 0 0 1 ${end.x} ${end.y}`;
}

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
        return current + diff * 0.18;
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
  const dashOffset = ARC_PATH_LENGTH * (1 - ratio);
  const color = speedColor(displayed);
  const needleDeg = ARC_START_DEG + ARC_ANGLE * ratio;
  const rounded = Math.round(displayed * 10) / 10;
  const tooltipColor =
    displayed < 25
      ? "var(--token-gauge-low)"
      : displayed < HIGH_SPEED_RATIO * MAX_TOKENS_PER_SEC
        ? "var(--token-gauge-mid)"
        : "var(--token-gauge-high)";
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
      title={speedLabel}
    >
      <svg
        viewBox="0 0 58 40"
        width="31"
        height="20"
        className="composer-token-gauge-svg"
        aria-hidden="true"
      >
        <path
          d={arcPath(OUTER_ARC_RADIUS)}
          className="composer-token-gauge-track"
          stroke="var(--token-gauge-track)"
          strokeWidth={OUTER_ARC_STROKE}
          fill="none"
          strokeLinecap="round"
        />
        <path
          d={arcPath(OUTER_ARC_RADIUS)}
          className="composer-token-gauge-progress"
          style={{
            stroke: color,
            strokeDasharray: ARC_PATH_LENGTH,
            strokeDashoffset: dashOffset,
          }}
          strokeWidth={OUTER_ARC_STROKE}
          fill="none"
          strokeLinecap="round"
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
            d={`M ${GAUGE_CENTER_X - 3.2} ${GAUGE_CENTER_Y + 0.6} L ${GAUGE_CENTER_X + 2.3} ${GAUGE_CENTER_Y - 5.3} L ${GAUGE_CENTER_X + NEEDLE_LENGTH} ${GAUGE_CENTER_Y - 1.2} Q ${GAUGE_CENTER_X + 17} ${GAUGE_CENTER_Y + 7.2} ${GAUGE_CENTER_X + 2.4} ${GAUGE_CENTER_Y + 5.2} Z`}
            fill={color}
          />
        </g>
        <circle
          cx={GAUGE_CENTER_X}
          cy={GAUGE_CENTER_Y}
          r={PIVOT_RADIUS}
          fill={color}
          stroke="var(--token-gauge-hub-ring)"
          strokeWidth="1"
        />
        <circle
          cx={GAUGE_CENTER_X + 0.7}
          cy={GAUGE_CENTER_Y - 0.7}
          r={PIVOT_INNER_RADIUS}
          fill="var(--token-gauge-hub-ring)"
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
