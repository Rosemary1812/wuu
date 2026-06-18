import { useEffect, useRef, useState } from "react";

// Gauge layout constants. The viewBox is 100x60 user units; the rendered
// size in the composer bar is set on the <svg> element below. The numeric
// readout lives in a hover-only tooltip overlay so the default rendering
// stays purely visual: a small tachometer whose needle tracks speed.
const MAX_TOKENS_PER_SEC = 100;
const REDLINE_START_RATIO = 0.7; // 70+ tok/s sits in the red zone.

const NEEDLE_LENGTH = 38;
const NEEDLE_TAIL = 8;
const NEEDLE_STROKE = 2.2;
const PIVOT_OUTER = 5.5;
const PIVOT_INNER = 2;

const ARC_CENTER_X = 50;
const ARC_CENTER_Y = 52;
const ARC_RADIUS = 38;
const ARC_STROKE = 5;
const TICK_INNER_MAJOR = ARC_RADIUS - 5;
const TICK_INNER_MINOR = ARC_RADIUS - 2.5;
const LABEL_RADIUS = ARC_RADIUS - 11;

const ARC_START_DEG = 180;
const ARC_END_DEG = 360;
const ARC_ANGLE = ARC_END_DEG - ARC_START_DEG;
const ARC_PATH_LENGTH = Math.PI * ARC_RADIUS;
const REDLINE_START_DEG = ARC_START_DEG + ARC_ANGLE * REDLINE_START_RATIO;

const TICK_COUNT = 11; // 0, 10, 20, ..., 100
const MAJOR_TICK_EVERY = 2; // labeled ticks at 0, 20, 40, 60, 80, 100

function polarPoint(cx: number, cy: number, r: number, deg: number): { x: number; y: number } {
  const rad = (deg * Math.PI) / 180;
  return { x: cx + r * Math.cos(rad), y: cy + r * Math.sin(rad) };
}

function trackPath(): string {
  const start = polarPoint(ARC_CENTER_X, ARC_CENTER_Y, ARC_RADIUS, ARC_START_DEG);
  const end = polarPoint(ARC_CENTER_X, ARC_CENTER_Y, ARC_RADIUS, ARC_END_DEG);
  return `M ${start.x} ${start.y} A ${ARC_RADIUS} ${ARC_RADIUS} 0 0 1 ${end.x} ${end.y}`;
}

function redlinePath(): string {
  const start = polarPoint(ARC_CENTER_X, ARC_CENTER_Y, ARC_RADIUS, REDLINE_START_DEG);
  const end = polarPoint(ARC_CENTER_X, ARC_CENTER_Y, ARC_RADIUS, ARC_END_DEG);
  return `M ${start.x} ${start.y} A ${ARC_RADIUS} ${ARC_RADIUS} 0 0 1 ${end.x} ${end.y}`;
}

function speedColor(tps: number): string {
  if (tps <= 0.05) return "var(--token-gauge-idle)";
  if (tps < 25) return "var(--token-gauge-low)";
  if (tps < REDLINE_START_RATIO * MAX_TOKENS_PER_SEC) return "var(--token-gauge-mid)";
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
      : displayed < REDLINE_START_RATIO * MAX_TOKENS_PER_SEC
        ? "var(--token-gauge-mid)"
        : "var(--token-gauge-high)";

  const ticks: JSX.Element[] = [];
  const labels: JSX.Element[] = [];
  for (let i = 0; i < TICK_COUNT; i++) {
    const tickDeg = ARC_START_DEG + (ARC_ANGLE * i) / (TICK_COUNT - 1);
    const isMajor = i % MAJOR_TICK_EVERY === 0;
    const inRedline = i / (TICK_COUNT - 1) >= REDLINE_START_RATIO;
    const innerR = isMajor ? TICK_INNER_MAJOR : TICK_INNER_MINOR;
    const outer = polarPoint(ARC_CENTER_X, ARC_CENTER_Y, ARC_RADIUS + 1, tickDeg);
    const inner = polarPoint(ARC_CENTER_X, ARC_CENTER_Y, innerR, tickDeg);
    ticks.push(
      <line
        key={`tick-${i}`}
        x1={inner.x}
        y1={inner.y}
        x2={outer.x}
        y2={outer.y}
        className="composer-token-gauge-tick"
        stroke={inRedline ? "var(--token-gauge-redline)" : "var(--token-gauge-tick)"}
        strokeWidth={isMajor ? 1.4 : 0.7}
        strokeLinecap="round"
      />,
    );
    if (isMajor) {
      const label = polarPoint(ARC_CENTER_X, ARC_CENTER_Y, LABEL_RADIUS, tickDeg);
      labels.push(
        <text
          key={`label-${i}`}
          x={label.x}
          y={label.y + 2.4}
          textAnchor="middle"
          className="composer-token-gauge-label"
          fill={inRedline ? "var(--token-gauge-redline)" : "var(--token-gauge-label)"}
        >
          {i * 10}
        </text>,
      );
    }
  }

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
        viewBox="0 0 100 60"
        width="112"
        height="68"
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
          d={redlinePath()}
          className="composer-token-gauge-redline"
          stroke="var(--token-gauge-redline-track)"
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
        {ticks}
        {labels}
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
          r={PIVOT_OUTER}
          fill="var(--token-gauge-hub-bg)"
          stroke="var(--token-gauge-hub-ring)"
          strokeWidth="0.8"
        />
        <circle
          cx={ARC_CENTER_X}
          cy={ARC_CENTER_Y}
          r={PIVOT_INNER}
          fill={color}
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
