import { useEffect, useRef, useState } from "react";

// Toolbar gauge. The numeric readout is rendered inline next to the dial so
// the user can see the current rate without hovering. The 32px-tall toolbar
// cell is wide enough for the gauge + label; the label container reserves
// vertical room for the flames effect that appears above the text.
const MAX_TOKENS_PER_SEC = 100;
const HIGH_SPEED_THRESHOLD = 70;
export const FLAME_SPEED_THRESHOLD = 90;
const FLAME_IGNITE_MS = 800;
const FLAME_EXTINGUISH_MS = 400;
const DISPLAY_LERP = 0.055;
const STALE_HOLD_MS = 1200;
const STALE_DECAY_MS = 5200;

const GAUGE_ARC_PATH = "M 2.5 17 A 9.5 9.5 0 0 1 21.5 17";
const GAUGE_ARC_PATH_LENGTH = 100;
const GAUGE_CENTER_X = 12;
const GAUGE_CENTER_Y = 17;
const NEEDLE_START_DEG = -154;
const NEEDLE_END_DEG = -26;
const NEEDLE_ANGLE = NEEDLE_END_DEG - NEEDLE_START_DEG;

// Flame state machine, factored out so the logic can be unit-tested without
// driving a React render loop. Four states make the transition rules
// explicit and avoid the "overloaded crossedAt" trap where a single
// timestamp is reused for both the ignite dwell and the extinguish dwell.
export type FlameState =
  | { kind: "off" }
  | { kind: "warming"; crossedAt: number }
  | { kind: "on" }
  | { kind: "cooling"; crossedAt: number };

export const INITIAL_FLAME_STATE: FlameState = { kind: "off" };

export function computeFlameState(
  current: FlameState,
  target: number,
  now: number,
): FlameState {
  const aboveFlame = target >= FLAME_SPEED_THRESHOLD;
  switch (current.kind) {
    case "off":
      if (aboveFlame) {
        return { kind: "warming", crossedAt: now };
      }
      return current;
    case "warming":
      if (!aboveFlame) {
        return { kind: "off" };
      }
      if (now - current.crossedAt >= FLAME_IGNITE_MS) {
        return { kind: "on" };
      }
      return current;
    case "on":
      if (!aboveFlame) {
        return { kind: "cooling", crossedAt: now };
      }
      return current;
    case "cooling":
      if (aboveFlame) {
        return { kind: "on" };
      }
      if (now - current.crossedAt >= FLAME_EXTINGUISH_MS) {
        return { kind: "off" };
      }
      return current;
  }
}

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
  // React state mirrors the state machine so the JSX re-renders on
  // transitions. The machine itself runs in the RAF tick.
  const [flamesActive, setFlamesActive] = useState(false);
  const runningRef = useRef(running);
  const targetRef = useRef(0);
  const sampledAtRef = useRef<number | undefined>(sampledAt);
  const flameStateRef = useRef<FlameState>(INITIAL_FLAME_STATE);
  runningRef.current = running;
  targetRef.current = running ? Math.max(0, tokensPerSecond) : 0;
  sampledAtRef.current = sampledAt;

  useEffect(() => {
    let raf = 0;
    const tick = (): void => {
      const now = Date.now();
      // Resolve the same decayed target the displayed value sees. Using the
      // raw target for flame detection would keep flames lit while the
      // displayed value is already decaying toward 0, which feels wrong.
      let resolvedTarget = runningRef.current ? targetRef.current : 0;
      const sampleAge = sampledAtRef.current
        ? now - sampledAtRef.current
        : 0;
      if (resolvedTarget > 0 && sampleAge > STALE_HOLD_MS) {
        const decay = Math.max(
          0,
          1 - (sampleAge - STALE_HOLD_MS) / STALE_DECAY_MS,
        );
        resolvedTarget *= decay;
      }
      setDisplayed((current) => {
        const diff = resolvedTarget - current;
        if (Math.abs(diff) < 0.05) {
          return resolvedTarget;
        }
        return current + diff * DISPLAY_LERP;
      });
      const nextFlame = computeFlameState(
        flameStateRef.current,
        resolvedTarget,
        now,
      );
      if (nextFlame.kind === "on" && flameStateRef.current.kind !== "on") {
        setFlamesActive(true);
      } else if (
        nextFlame.kind !== "on" &&
        flameStateRef.current.kind === "on"
      ) {
        setFlamesActive(false);
      }
      flameStateRef.current = nextFlame;
      raf = window.requestAnimationFrame(tick);
    };
    raf = window.requestAnimationFrame(tick);
    return () => {
      window.cancelAnimationFrame(raf);
    };
  }, []);

  const ratio = Math.max(0, Math.min(1, displayed / MAX_TOKENS_PER_SEC));
  const dashOffset = GAUGE_ARC_PATH_LENGTH * (1 - ratio);
  const color = speedColor(displayed);
  const needleDeg = NEEDLE_START_DEG + NEEDLE_ANGLE * ratio;
  const rounded = Math.round(displayed * 10) / 10;
  const isEstimated = source === "estimated";
  const speedLabel = `${isEstimated ? "约 " : ""}${rounded.toFixed(1)} tok/s`;
  const ariaPrefix = isEstimated ? "估算生成速度" : "生成速度";

  return (
    <div
      className="composer-token-gauge"
      data-state={running ? "running" : "idle"}
      data-flames={flamesActive ? "on" : "off"}
      role="status"
      aria-live="polite"
      aria-label={`${ariaPrefix} ${rounded.toFixed(1)} token 每秒`}
      style={{ color }}
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
      <span
        className={
          flamesActive
            ? "composer-token-gauge-label composer-token-gauge-label-shake"
            : "composer-token-gauge-label"
        }
      >
        {flamesActive ? (
          <span className="composer-token-gauge-flames" aria-hidden="true">
            <span className="composer-token-gauge-flame" />
            <span className="composer-token-gauge-flame" />
            <span className="composer-token-gauge-flame" />
          </span>
        ) : null}
        {speedLabel}
      </span>
    </div>
  );
}
