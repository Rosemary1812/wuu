import { useEffect, useRef, useState } from "react";

const MAX_TOKENS_PER_SEC = 80;
const ARC_RADIUS = 44;
const ARC_STROKE = 6;
const NEEDLE_LENGTH = ARC_RADIUS - ARC_STROKE / 2 - 3;
const ARC_CENTER_X = 50;
const ARC_CENTER_Y = 50;
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
  if (tps < 55) return "var(--token-gauge-mid)";
  return "var(--token-gauge-high)";
}

export function ComposerTokenGauge({
  running,
  tokensPerSecond,
}: {
  running: boolean;
  tokensPerSecond: number;
}): JSX.Element | null {
  // Displayed value tracks the target with a per-frame lerp so the numeric
  // readout settles instead of flickering on every sliding-window update.
  // The initial value is the target itself: when the gauge mounts while the
  // turn is already running there is nothing to interpolate from, and tests
  // that render the component synchronously need to see the real number on
  // first paint.
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
  const needle = polarPoint(ARC_CENTER_X, ARC_CENTER_Y, NEEDLE_LENGTH, needleDeg);
  const rounded = Math.round(displayed * 10) / 10;

  return (
    <div
      className="composer-token-gauge"
      data-state={running ? "running" : "stopping"}
      role="status"
      aria-live="polite"
      aria-label={`生成速度 ${rounded.toFixed(1)} token 每秒`}
      title={`实时输出速度 ${rounded.toFixed(1)} tok/s`}
    >
      <svg
        viewBox="0 0 100 56"
        width="64"
        height="36"
        className="composer-token-gauge-svg"
        aria-hidden="true"
      >
        <path
          d={trackPath()}
          className="composer-token-gauge-track"
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
          className="composer-token-gauge-needle"
          style={{ stroke: color }}
          x1={ARC_CENTER_X}
          y1={ARC_CENTER_Y}
          x2={needle.x}
          y2={needle.y}
          strokeWidth={1.6}
          strokeLinecap="round"
        />
        <circle
          className="composer-token-gauge-pivot"
          style={{ fill: color }}
          cx={ARC_CENTER_X}
          cy={ARC_CENTER_Y}
          r={2.5}
        />
      </svg>
      <div className="composer-token-gauge-readout">
        <span className="composer-token-gauge-number" style={{ color }}>
          {rounded.toFixed(1)}
        </span>
        <span className="composer-token-gauge-unit">tok/s</span>
      </div>
    </div>
  );
}
