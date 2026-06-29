// Composer-toolbar context-limit meter.
//
// Sits next to the live token-speed gauge and shows the retained conversation
// context estimate against the active provider/model context ceiling. The toolbar
// keeps this aligned with the token-speed gauge: the ring shows the
// proportion, the label shows used/limit, and hover reveals the concise
// details.
//
// The progress stroke reuses the token-speed gauge's color palette so the
// two meters read as a coordinated pair: idle gray when the window is empty,
// warm amber while context is filling, accent-warm red once usage crosses
// the high-water mark. The 0.7 threshold mirrors the token-speed gauge's
// 70/100 tps break, so both meters shift into "high" at the same fraction
// of their scale.

import type { TurnContextUsage } from "./AppState";
import { useId, useRef, useState } from "react";
import { FloatingMenuPortal } from "./ComposerFloatingMenu";

type ComposerContextMeterProps = {
  // Pass the latest per-turn usage snapshot from AppState. The component
  // hides entirely when no context ceiling is known yet (e.g. before the
  // first turn finishes, or for an unknown model with no limit data).
  usage: TurnContextUsage | undefined;
};

const RING_VIEWBOX = 24;
const RING_CENTER = 12;
const RING_RADIUS = 9;
// Pre-computed 2πr so the SVG dash math is stable across the codebase.
// The viewBox is intentionally 24 with stroke-width 3 leaving room for
// the ring stroke.
const RING_CIRCUMFERENCE = 2 * Math.PI * RING_RADIUS;
const TOOLTIP_WIDTH = 212;

// Fill-level thresholds mirror the token-speed gauge's idle/mid/high
// tiers so the two meters step into "high" at the same fraction of their
// own scale. The progress stroke inherits currentColor from the SVG; the
// container keeps --ink-strong so the inline label is unaffected.
const FILL_HIGH_THRESHOLD = 0.7;

type FillLevel = "idle" | "mid" | "high";

function fillLevel(ratio: number): FillLevel {
  if (ratio <= 0) return "idle";
  if (ratio < FILL_HIGH_THRESHOLD) return "mid";
  return "high";
}

function fillColor(level: FillLevel): string {
  if (level === "idle") return "var(--token-gauge-idle)";
  if (level === "mid") return "var(--token-gauge-mid)";
  return "var(--token-gauge-high)";
}

export function ComposerContextMeter({
  usage,
}: ComposerContextMeterProps): JSX.Element | null {
  const tooltipID = useId();
  const anchorRef = useRef<HTMLDivElement>(null);
  const [tooltipOpen, setTooltipOpen] = useState(false);

  if (!usage || !Number.isFinite(usage.window) || usage.window <= 0) {
    return null;
  }
  const used = Math.max(0, usage.used);
  const ratio = Math.min(1, Math.max(0, used / usage.window));
  const percent = Math.round(ratio * 100);
  const dashOffset = RING_CIRCUMFERENCE * (1 - ratio);
  // Color tier mirrors the token-speed gauge so the two meters read as a
  // coordinated pair: gray when empty, amber while filling, warm red as
  // the window approaches its limit. The 0.7 cutoff matches the gauge's
  // 70/100 tps break so both step into "high" at the same fraction.
  const level = fillLevel(ratio);
  const ringColor = fillColor(level);
  const percentLabel = `${percent}%`;
  const valueLabel = `${formatTokenCount(used)} / ${formatTokenCount(
    usage.window,
  )}`;
  const requestContext = usage.requestContext;
  const ariaLabel =
    `上下文上限 ${formatTokenCount(usage.window)}` +
    `，估算占用 ${formatTokenCount(used)} (${percent}%)`;
  return (
    <div
      ref={anchorRef}
      className="composer-context-meter"
      tabIndex={0}
      role="status"
      aria-label={ariaLabel}
      aria-describedby={tooltipOpen ? tooltipID : undefined}
      onBlur={() => setTooltipOpen(false)}
      onFocus={() => setTooltipOpen(true)}
      onMouseEnter={() => setTooltipOpen(true)}
      onMouseLeave={() => setTooltipOpen(false)}
    >
      <svg
        viewBox={`0 0 ${RING_VIEWBOX} ${RING_VIEWBOX}`}
        width="20"
        height="20"
        className="composer-context-meter-svg"
        data-fill={level}
        // Color is set on the SVG (not the container) so the inline label
        // keeps the parent's --ink-strong text color and the progress
        // stroke can independently step through the gauge color tiers.
        style={{ color: ringColor }}
        aria-hidden="true"
      >
        <circle
          cx={RING_CENTER}
          cy={RING_CENTER}
          r={RING_RADIUS}
          className="composer-context-meter-track"
          fill="none"
          strokeWidth="2"
        />
        <circle
          cx={RING_CENTER}
          cy={RING_CENTER}
          r={RING_RADIUS}
          className="composer-context-meter-progress"
          fill="none"
          strokeWidth="2"
          strokeLinecap="round"
          strokeDasharray={RING_CIRCUMFERENCE}
          strokeDashoffset={dashOffset}
          transform={`rotate(-90 ${RING_CENTER} ${RING_CENTER})`}
        />
      </svg>
      <span className="composer-context-meter-label">{valueLabel}</span>
      {tooltipOpen ? (
        <FloatingMenuPortal
          anchorRef={anchorRef}
          owner="composer-context-meter"
          placement="above"
          align="right"
          offset={8}
          width={TOOLTIP_WIDTH}
        >
          <div
            id={tooltipID}
            className="composer-context-meter-tooltip"
            role="tooltip"
          >
            <div className="composer-context-meter-tooltip-headline">
              <span className="composer-context-meter-tooltip-label">
                上下文估算
              </span>
              <span className="composer-context-meter-tooltip-value">
                {percentLabel}
              </span>
            </div>
            <div className="composer-context-meter-tooltip-row">
              <span className="composer-context-meter-tooltip-label">已保留</span>
              <span className="composer-context-meter-tooltip-value">
                {valueLabel}
              </span>
            </div>
            {requestContext ? (
              <>
                <div className="composer-context-meter-tooltip-divider" />
                <div className="composer-context-meter-tooltip-row">
                  <span className="composer-context-meter-tooltip-label">
                    稳定前缀
                  </span>
                  <span className="composer-context-meter-tooltip-value">
                    {formatMessageShare(
                      requestContext.stablePrefix,
                      requestContext.messageCount,
                    )}
                  </span>
                </div>
                <div className="composer-context-meter-tooltip-row">
                  <span className="composer-context-meter-tooltip-label">
                    本轮前缀
                  </span>
                  <span className="composer-context-meter-tooltip-value">
                    {formatMessageShare(
                      requestContext.turnPrefix,
                      requestContext.messageCount,
                    )}
                  </span>
                </div>
                <div className="composer-context-meter-tooltip-row">
                  <span className="composer-context-meter-tooltip-label">
                    临时上下文
                  </span>
                  <span className="composer-context-meter-tooltip-value">
                    {formatTransientContext(requestContext)}
                  </span>
                </div>
                <div className="composer-context-meter-tooltip-row">
                  <span className="composer-context-meter-tooltip-label">
                    工具面
                  </span>
                  <span className="composer-context-meter-tooltip-value">
                    {formatToolSurface(requestContext)}
                  </span>
                </div>
              </>
            ) : null}
          </div>
        </FloatingMenuPortal>
      ) : null}
    </div>
  );
}

function formatMessageShare(value: number, total: number): string {
  const safeValue = Math.max(0, Math.round(value));
  const safeTotal = Math.max(0, Math.round(total));
  return `${safeValue} / ${safeTotal} 条`;
}

function formatTransientContext(
  context: NonNullable<TurnContextUsage["requestContext"]>,
): string {
  const messageCount = Math.max(
    0,
    Math.round(context.transientMessages || context.hiddenMessages || 0),
  );
  const byteLabel = formatByteCount(context.dynamicBytes);
  if (messageCount <= 0) {
    return byteLabel ? byteLabel : "0 条";
  }
  return byteLabel ? `${messageCount} 条 · ${byteLabel}` : `${messageCount} 条`;
}

function formatToolSurface(
  context: NonNullable<TurnContextUsage["requestContext"]>,
): string {
  const toolCount = Math.max(0, Math.round(context.toolCount));
  const byteLabel = formatByteCount(context.toolSchemaBytes);
  if (toolCount <= 0) {
    return byteLabel ? byteLabel : "0 个";
  }
  return byteLabel ? `${toolCount} 个 · ${byteLabel}` : `${toolCount} 个`;
}

function formatByteCount(value: number): string {
  if (!Number.isFinite(value) || value <= 0) {
    return "";
  }
  if (value >= 1_000_000) {
    return `${trimNumber(value / 1_000_000)}MB`;
  }
  if (value >= 1_000) {
    return `${trimNumber(value / 1_000)}kB`;
  }
  return `${Math.round(value)}B`;
}

function formatTokenCount(value: number): string {
  if (!Number.isFinite(value) || value <= 0) {
    return "0";
  }
  if (value >= 1_000_000) {
    const scaled = value / 1_000_000;
    return `${trimNumber(scaled)}M`;
  }
  if (value >= 1_000) {
    const scaled = value / 1_000;
    return `${trimNumber(scaled)}k`;
  }
  return String(Math.round(value));
}

function trimNumber(value: number): string {
  if (value >= 100) {
    return value.toFixed(0);
  }
  // Drop the trailing ".0" so small counts like 3k render as "3k" rather
  // than "3.0k" — the trailing digit is noise for token-volume readouts.
  return value.toFixed(1).replace(/\.0$/, "");
}
