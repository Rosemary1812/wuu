import { useEffect, useRef, useState } from "react";

// Compact toolbar gauge. The numeric readout lives in a hover-only tooltip;
// the default render stays icon-like: a simple half arc and needle.
const MAX_TOKENS_PER_SEC = 100;
const HIGH_SPEED_RATIO = 0.7;

const NEEDLE_LENGTH = 15;
const NEEDLE_TAIL = 3;
const NEEDLE_STROKE = 1.5;
const PIVOT_RADIUS = 2.4;

const ARC_CENTER_X = 24;
const ARC_CENTER_Y = 24;
const ARC_RADIUS = 18;
const ARC_STROKE = 3.2;

const ARC_START_DEG = 180;
const ARC_END_DEG = 360;
const ARC_ANGLE = ARC_END_DEG - ARC_START_DEG;
const ARC_PATH_LENGTH = Math.PI * ARC_RADIUS;

function polarPoint(cx: number, cy: number, r: number, deg: number): { x: number; y: number } {
  const rad = (deg * Math.PI) / 180;
  return { x: cx + r * Math.cos(rad), y: cy + r * Math.sin(rad) };
}

function trackPath(): string {
  const start = polarPoint(ARC_CENTER_X, ARC_CENTER_Y, ARC_RADIUS, ARC_START_DEG);
  const end = polarPoint(ARC_CENTER_X, ARC_CENTER_Y, ARC_RADIUS, ARC_END_DEG);
  return `M ${start.x} ${start.y} A ${ARC_RADIUS} ${ARC_RADIUS} 0 0 1 ${end.x} ${end.y}`;
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
}: {
  running: boolean;
  tokensPerSecond: number;
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
  const tip = polarPoint(ARC_CENTER_X, ARC_CENTER_Y, NEEDLE_LENGTH, needleDeg);
  const tail = polarPoint(ARC_CENTER_X, ARC_CENTER_Y, NEEDLE_TAIL, needleDeg + 180);
  const rounded = Math.round(displayed * 10) / 10;
  const tooltipColor =
    displayed < 25
      ? "var(--token-gauge-low)"
      : displayed < HIGH_SPEED_RATIO * MAX_TOKENS_PER_SEC
        ? "var(--token-gauge-mid)"
        : "var(--token-gauge-high)";

  return (
    <div
      className="composer-token-gauge"
      data-state={running ? "running" : "stopping"}
      role="status"
      aria-live="polite"
      aria-label={`生成速度 ${rounded.toFixed(1)} token 每秒`}
      title={`${rounded.toFixed(1)} tok/s`}
    >
      <svg
        viewBox="0 0 48 28"
        width="30"
        height="18"
        className="composer-token-gauge-svg"
        aria-hidden="true"
      >
        <path
          d={trackPath()}
          className="composer-token-gauge-track"
          stroke="var(--token-gauge-track)"
          strokeWidth={ARC_STROKE}
          fill="none"
          strokeLinecap="round"
        />
        <path
          d={trackPath()}
          className="composer-token-gauge-progress"
          style={{
            stroke: color,
            strokeDasharray: ARC_PATH_LENGTH,
            strokeDashoffset: dashOffset,
          }}
          strokeWidth={ARC_STROKE}
          fill="none"
          strokeLinecap="round"
        />
        <line
          x1={tail.x}
          y1={tail.y}
          x2={tip.x}
          y2={tip.y}
          className="composer-token-gauge-needle"
          stroke={color}
          strokeWidth={NEEDLE_STROKE}
          strokeLinecap="round"
        />
        <circle
          cx={ARC_CENTER_X}
          cy={ARC_CENTER_Y}
          r={PIVOT_RADIUS}
          fill={color}
          stroke="var(--token-gauge-hub-ring)"
          strokeWidth="0.7"
        />
      </svg>
      <span
        className="composer-token-gauge-tooltip"
        style={{ color: tooltipColor }}
      >
        {rounded.toFixed(1)} tok/s
      </span>
    </div>
  );
}
