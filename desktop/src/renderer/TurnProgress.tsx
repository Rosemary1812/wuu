import { RefreshCw, X } from "lucide-react";
import { type CSSProperties, useEffect, useRef, useState } from "react";

export type TurnProgressEra =
  | "sticks"
  | "swords"
  | "fortress"
  | "cannon"
  | "factory"
  | "armor"
  | "rockets"
  | "orbit"
  | "galaxy";

export type TurnProgressCampaign = {
  currentEra: TurnProgressEra;
  nextEra: TurnProgressEra;
  currentLayer: "a" | "b";
  transitionProgress: number;
  variant: number;
};

const TURN_PROGRESS_ERA_MS = 4 * 60 * 1000;
const TURN_PROGRESS_TRANSITION_MS = 30 * 1000;
const TURN_PROGRESS_PREVIEW_MS = 72 * 1000;
const TURN_PROGRESS_ERAS: TurnProgressEra[] = [
  "sticks",
  "swords",
  "fortress",
  "cannon",
  "factory",
  "armor",
  "rockets",
  "orbit",
  "galaxy"
];
const TURN_PROGRESS_PREVIEW_SPEED = (TURN_PROGRESS_ERA_MS * TURN_PROGRESS_ERAS.length) / TURN_PROGRESS_PREVIEW_MS;
const TURN_PROGRESS_CAMPAIGN_MS = TURN_PROGRESS_ERA_MS * TURN_PROGRESS_ERAS.length;
const TURN_PROGRESS_ERA_LABELS: Record<TurnProgressEra, string> = {
  sticks: "木棍",
  swords: "刀剑",
  fortress: "城堡",
  cannon: "火炮",
  factory: "工厂",
  armor: "装甲",
  rockets: "火箭",
  orbit: "轨道",
  galaxy: "星际"
};

export function TurnProgressPreviewOverlay({ onClose }: { onClose: () => void }): JSX.Element {
  const [startedAt, setStartedAt] = useState(() => Date.now());
  const [complete, setComplete] = useState(false);
  const now = usePreviewNow(!complete);
  const previewElapsedMs = Math.min(TURN_PROGRESS_PREVIEW_MS, Math.max(0, now - startedAt));
  const previewRatio = previewElapsedMs / TURN_PROGRESS_PREVIEW_MS;
  const previewComplete = previewElapsedMs >= TURN_PROGRESS_PREVIEW_MS;
  const campaignElapsedMs = previewComplete
    ? TURN_PROGRESS_CAMPAIGN_MS
    : Math.min(TURN_PROGRESS_CAMPAIGN_MS - 1, previewRatio * TURN_PROGRESS_CAMPAIGN_MS);
  const campaign = turnProgressCampaign("turn-progress-preview", Math.min(TURN_PROGRESS_CAMPAIGN_MS - 1, campaignElapsedMs));

  useEffect(() => {
    if (!complete && previewComplete) {
      setComplete(true);
    }
  }, [complete, previewComplete]);

  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent): void {
      if (event.key === "Escape") {
        onClose();
      }
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [onClose]);

  function restart(): void {
    setStartedAt(Date.now());
    setComplete(false);
  }

  const progressStyle = { width: `${previewRatio * 100}%` } as CSSProperties;

  return (
    <div className="turn-progress-preview-backdrop" role="dialog" aria-modal="true" aria-label="完整预览等待动画">
      <div className="turn-progress-preview-panel">
        <div className="turn-progress-preview-header">
          <div>
            <h2>完整预览</h2>
            <p>
              {formatDuration(campaignElapsedMs)} / {formatDuration(TURN_PROGRESS_CAMPAIGN_MS)}
              <span>{TURN_PROGRESS_ERA_LABELS[campaign.currentEra]}</span>
              <span>{formatDuration(TURN_PROGRESS_PREVIEW_MS)} 预览</span>
              <span>{Math.round(TURN_PROGRESS_PREVIEW_SPEED)}x</span>
            </p>
          </div>
          <div className="turn-progress-preview-actions">
            <button className="icon-button" type="button" aria-label="重播" title="重播" onClick={restart}>
              <RefreshCw className="icon" />
            </button>
            <button className="icon-button" type="button" aria-label="关闭" title="关闭" onClick={onClose}>
              <X className="icon" />
            </button>
          </div>
        </div>
        <div className="turn-progress-preview-stage">
          <div className="turn-progress-preview-rule">
            <TurnProgressCampaignScene campaign={campaign} />
          </div>
          <div className="turn-progress-preview-track" aria-hidden="true">
            <span style={progressStyle} />
          </div>
        </div>
      </div>
    </div>
  );
}

export function TurnProgressCampaignScene({ campaign }: { campaign: TurnProgressCampaign }): JSX.Element {
  const currentLayerActive = campaign.currentLayer === "a";
  return (
    <span className="turn-progress-campaign" aria-hidden="true">
      <TurnProgressSceneLayer
        era={currentLayerActive ? campaign.currentEra : campaign.nextEra}
        variant={campaign.variant}
        state={currentLayerActive ? "current" : "next"}
        transitionProgress={campaign.transitionProgress}
      />
      <TurnProgressSceneLayer
        era={currentLayerActive ? campaign.nextEra : campaign.currentEra}
        variant={campaign.variant}
        state={currentLayerActive ? "next" : "current"}
        transitionProgress={campaign.transitionProgress}
      />
    </span>
  );
}

function TurnProgressSceneLayer({
  era,
  variant,
  state,
  transitionProgress
}: {
  era: TurnProgressEra;
  variant: number;
  state: "current" | "next";
  transitionProgress: number;
}): JSX.Element {
  const entering = state === "next";
  const progress = entering ? transitionProgress : 1 - transitionProgress;
  const opacity = entering ? transitionProgress : 1 - transitionProgress;
  const yOffset = entering ? (1 - progress) * 6 : -transitionProgress * 5;
  const scale = entering ? 0.982 + progress * 0.018 : 1 + transitionProgress * 0.012;
  const blur = entering ? (1 - progress) * 1.1 : transitionProgress * 1.1;
  const style = {
    "--scene-opacity": opacity,
    "--scene-y": `${yOffset}px`,
    "--scene-scale": scale,
    "--scene-blur": `${blur}px`
  } as CSSProperties;

  return (
    <span className={`turn-progress-scene era-${era} variant-${variant} scene-${state}`} style={style}>
      <span className="civilization-prop prop-left" />
      <span className="civilization-prop prop-mid" />
      <span className="civilization-prop prop-right" />
      <span className="civilization-fire" />
      <span className="civilization-banner banner-left" />
      <span className="civilization-banner banner-right" />
      <span className="battle-ground" />
      <span className="battle-dust dust-left" />
      <span className="battle-dust dust-right" />
      <span className="battle-front front-left" />
      <span className="battle-front front-right" />
      <span className="battle-impact impact-one" />
      <span className="battle-impact impact-two" />
      <span className="battle-tracer tracer-one" />
      <span className="battle-tracer tracer-two" />
      <span className="battle-squad squad-left" />
      <span className="battle-squad squad-left-rear" />
      <span className="battle-squad squad-right" />
      <span className="battle-squad squad-right-rear" />
      <span className="battle-smoke smoke-left" />
      <span className="battle-smoke smoke-right" />
      <span className="battle-barrage barrage-one" />
      <span className="battle-barrage barrage-two" />
      <span className="era-projectile projectile-one" />
      <span className="era-projectile projectile-two" />
      <span className="era-vehicle" />
      <span className="era-rocket" />
      <span className="era-planet" />
      <span className="era-ship ship-one" />
      <span className="era-ship ship-two" />
      <span className="fight-spark spark-one" />
      <span className="fight-spark spark-two" />
      <Stickman className="stickman-a" />
      <Stickman className="stickman-b" />
      <Stickman className="stickman-crowd crowd-one" />
      <Stickman className="stickman-crowd crowd-two" />
      <Stickman className="stickman-crowd crowd-three" />
      <Stickman className="stickman-crowd crowd-four" />
    </span>
  );
}

function Stickman({ className }: { className: string }): JSX.Element {
  return (
    <span className={`stickman ${className}`}>
      <span className="stickman-figure">
        <span className="stickman-head" />
        <span className="stickman-body" />
        <span className="stickman-arm arm-front" />
        <span className="stickman-arm arm-back" />
        <span className="stickman-leg leg-front" />
        <span className="stickman-leg leg-back" />
        <span className="stickman-weapon" />
      </span>
    </span>
  );
}

export function useLiveNow(active: boolean): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) {
      return;
    }
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [active]);

  return active ? now : Date.now();
}

function usePreviewNow(active: boolean): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) {
      return;
    }
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), 120);
    return () => window.clearInterval(timer);
  }, [active]);

  return now;
}

export function turnProgressCampaign(turnID: string, elapsedMs: number): TurnProgressCampaign {
  let hash = 0;
  for (let index = 0; index < turnID.length; index++) {
    hash = (hash * 31 + turnID.charCodeAt(index)) >>> 0;
  }
  const campaignMs = elapsedMs % TURN_PROGRESS_CAMPAIGN_MS;
  const eraIndex = Math.floor(campaignMs / TURN_PROGRESS_ERA_MS);
  const eraElapsedMs = campaignMs % TURN_PROGRESS_ERA_MS;
  const nextEraIndex = (eraIndex + 1) % TURN_PROGRESS_ERAS.length;
  const rawTransitionProgress =
    eraElapsedMs < TURN_PROGRESS_ERA_MS - TURN_PROGRESS_TRANSITION_MS
      ? 0
      : Math.min(1, (eraElapsedMs - (TURN_PROGRESS_ERA_MS - TURN_PROGRESS_TRANSITION_MS)) / TURN_PROGRESS_TRANSITION_MS);
  const transitionProgress = rawTransitionProgress * rawTransitionProgress * (3 - 2 * rawTransitionProgress);
  return {
    currentEra: TURN_PROGRESS_ERAS[eraIndex] ?? TURN_PROGRESS_ERAS[0],
    nextEra: TURN_PROGRESS_ERAS[nextEraIndex] ?? TURN_PROGRESS_ERAS[0],
    currentLayer: eraIndex % 2 === 0 ? "a" : "b",
    transitionProgress,
    variant: hash % 3
  };
}

export function LiveDuration({ startedAtMs }: { startedAtMs: number }): JSX.Element {
  const nodeRef = useRef<HTMLSpanElement | null>(null);

  useEffect(() => {
    const update = (): void => {
      if (nodeRef.current) {
        nodeRef.current.textContent = formatDuration(Math.max(0, Date.now() - startedAtMs));
      }
    };
    update();
    const timer = window.setInterval(update, 1000);
    return () => window.clearInterval(timer);
  }, [startedAtMs]);

  return <span ref={nodeRef}>{formatDuration(Math.max(0, Date.now() - startedAtMs))}</span>;
}

export function formatDuration(ms: number): string {
  const totalSeconds = Math.max(0, Math.floor(ms / 1000));
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  if (hours > 0) {
    return `${hours}h ${minutes}m ${seconds}s`;
  }
  if (minutes > 0) {
    return `${minutes}m ${seconds}s`;
  }
  return `${seconds}s`;
}
