const FULL_SIZE_PANEL_CONTAINER_WIDTH = 560;
const WIDE_PANEL_GROWTH_START_WIDTH = 1_000;
const FULL_SCALE_PANEL_CONTAINER_WIDTH = 1_400;
const COMPACT_ENVIRONMENT_PANEL_SCALE = 0.8;
const MAXIMUM_ENVIRONMENT_PANEL_SCALE = 1;
const MINIMUM_ENVIRONMENT_PANEL_SCALE = 0.576;

export function environmentPanelScaleForWidth(containerWidth: number): number {
  const compactScale = Math.min(
    COMPACT_ENVIRONMENT_PANEL_SCALE,
    Math.max(
      MINIMUM_ENVIRONMENT_PANEL_SCALE,
      (containerWidth / FULL_SIZE_PANEL_CONTAINER_WIDTH) *
        COMPACT_ENVIRONMENT_PANEL_SCALE,
    ),
  );
  const wideGrowth = Math.min(
    1,
    Math.max(
      0,
      (containerWidth - WIDE_PANEL_GROWTH_START_WIDTH) /
        (FULL_SCALE_PANEL_CONTAINER_WIDTH - WIDE_PANEL_GROWTH_START_WIDTH),
    ),
  );
  const scale = Math.min(
    MAXIMUM_ENVIRONMENT_PANEL_SCALE,
    compactScale + wideGrowth * (MAXIMUM_ENVIRONMENT_PANEL_SCALE - compactScale),
  );
  return Math.round(scale * 1_000) / 1_000;
}
