import { useEffect, useState } from "react";
import { useI18n } from "./i18n";

// Toolbar gauge. The numeric readout is rendered inline next to the dial
// so the user can see the current rate without hovering. The needle and
// the text share the same currentColor so the idle/mid/high color tier
// applies to both.
const MAX_TOKENS_PER_SEC = 100;
const HIGH_SPEED_THRESHOLD = 70;
const STALE_HOLD_MS = 1200;
const STALE_DECAY_MS = 5200;
const STALE_DECAY_STEP_MS = 100;

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
  const { t, formatNumber } = useI18n();
  // CSS transitions smooth the needle and arc between samples. React only
  // updates when the target changes or a stale sample is winding down, so a
  // stable running turn does not keep the renderer on a permanent rAF loop.
  const [displayed, setDisplayed] = useState(() =>
    running ? Math.max(0, tokensPerSecond) : 0,
  );

  useEffect(() => {
    const target = running ? Math.max(0, tokensPerSecond) : 0;
    setDisplayed(target);
    if (!running || target <= 0.05 || sampledAt === undefined) {
      return undefined;
    }

    let decayTimer: number | undefined;
    let decayInterval: number | undefined;
    const updateDecay = (): boolean => {
      const decayElapsed = Date.now() - sampledAt - STALE_HOLD_MS;
      const decay = Math.max(0, 1 - decayElapsed / STALE_DECAY_MS);
      setDisplayed(target * decay);
      if (decay === 0 && decayInterval !== undefined) {
        window.clearInterval(decayInterval);
        decayInterval = undefined;
      }
      return decay > 0;
    };
    const startDecay = (): void => {
      if (updateDecay()) {
        decayInterval = window.setInterval(updateDecay, STALE_DECAY_STEP_MS);
      }
    };
    const holdRemaining = Math.max(
      0,
      sampledAt + STALE_HOLD_MS - Date.now(),
    );
    decayTimer = window.setTimeout(startDecay, holdRemaining);

    return () => {
      if (decayTimer !== undefined) {
        window.clearTimeout(decayTimer);
      }
      if (decayInterval !== undefined) {
        window.clearInterval(decayInterval);
      }
    };
  }, [running, sampledAt, tokensPerSecond]);

  const ratio = Math.max(0, Math.min(1, displayed / MAX_TOKENS_PER_SEC));
  const dashOffset = GAUGE_ARC_PATH_LENGTH * (1 - ratio);
  const color = speedColor(displayed);
  const needleDeg = NEEDLE_START_DEG + NEEDLE_ANGLE * ratio;
  const rounded = Math.round(displayed);
  const isEstimated = source === "estimated";
  const speedLabel = t(isEstimated ? "composer.speed.estimatedShort" : "composer.speed.short", { speed: formatNumber(rounded) });

  return (
    <div
      className="composer-token-gauge"
      data-state={running ? "running" : "idle"}
      role="status"
      aria-live="polite"
      aria-label={t(isEstimated ? "composer.speed.estimatedLabel" : "composer.speed.label", { speed: formatNumber(rounded) })}
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
      <span className="composer-token-gauge-label">{speedLabel}</span>
    </div>
  );
}
